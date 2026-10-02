// 本文件负责重启矩阵：确定性独立重启、A/B 落盘 C 延后、BeforeModel 不得认领在途中途事件，
// 以及恢复通告只追加在尾部且诊断仍可解读。
// 契约: docs/wiki/platform/reincarnation-notice.md#detection
// 契约: docs/wiki/platform/reincarnation-notice.md#notice-shape
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	r30Env    = "TAGENT_R30_ROUND"
	r30Dir    = "TAGENT_R30_DIR"
	r30Rounds = 30
)

// restartPlan is the deterministic schedule shared by parent and child.
func restartPlan(round int) (mode, target string) {
	if round%2 == 0 {
		mode = "ab"
	} else {
		mode = "c"
	}
	switch round % 4 {
	case 1:
		target = "post-persist"
	case 2:
		target = "post-completion"
	case 3:
		target = "post-receipted"
	default:
		target = "post-ack"
	}
	if round == r30Rounds-1 {
		target = "post-receipted"
	}
	return
}

func restartLogContents(round int, mode string) []string {
	if mode == "ab" {
		return []string{fmt.Sprintf("r30-%02d-A", round), fmt.Sprintf("r30-%02d-B", round)}
	}
	return []string{fmt.Sprintf("r30-%02d-C", round)}
}

func r30Stack(root string) (*memory.FileSegmentStore, *EventBus, *TagentAgent) {
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 500)
	mustX(err)
	mustX(store.RebuildLiveCounts())
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	mustX(err)
	bus.SetRetentionGuard(store)
	mustX(bus.ArmRetentionFromInbox())
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "r30", persistentBus: bus, contextManager: cm}
	return store, bus, ta
}

// restartChildRound runs one scheduled round IN THIS PROCESS (spawned as a child):
// recover → accept → walk the protocol → exit at the scheduled window.
func restartChildRound(round int, root string) {
	mode, target := restartPlan(round)
	store, bus, ta := r30Stack(root)
	defer store.Close()

	s, err := ta.ReconcileOutstanding()
	mustX(err)
	fmt.Printf("RECON r=%d add=%d clean=%d cont=%d quar=%d block=%d\n", round, s.ReceiptsAdded, s.Cleaned, s.Continued, s.Quarantined, s.Blocked)

	for _, body := range restartLogContents(round, mode) {
		if _, err := bus.PublishContext(context.Background(), durableMsg(body)); err != nil {
			childFatal("publish: " + err.Error())
		}
	}
	batch, err := bus.Pull(context.Background())
	mustX(err)
	if len(batch) == 0 {
		childFatal(fmt.Sprintf("round %d: empty batch after publishing %v", round, restartLogContents(round, mode)))
	}
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		childFatal("prepare failed")
	}
	for _, e := range batch {
		if !ta.contextManager.persistBusEvent(e) {
			childExit(fmt.Sprintf("round %d died on a commit conflict (claim held, replays later)", round))
		}
	}
	if target == "post-persist" {
		childExit(fmt.Sprintf("round %d died after input commits, before completion", round))
	}

	paths, byPath := groupClaimsByPath(batch)
	committed := selectedKeySet(batch)
	completions := map[string]json.RawMessage{}
	creds := map[string]reliability.ReceiptCredential{}
	for _, path := range paths {
		_, raw, err := buildEnvelopeCompletion(byPath[path], committed,
			completedOutcome(), ta.name, ta.contextManager.partitionID, time.Now().UnixMilli(),
			ta.contextManager.buildTurnAttribution(context.Background()))
		if err != nil {
			childFatal("completion build: " + err.Error())
		}
		if err := bus.RecordCompletion(path, raw); err != nil {
			childFatal("record completion: " + err.Error())
		}
		completions[path] = raw
	}
	if target == "post-completion" {
		childExit(fmt.Sprintf("round %d died after durable completions, before receipts", round))
	}
	for _, path := range paths {
		cred, err := ta.contextManager.verifyReceiptCredential(completions[path])
		if err != nil {
			childFatal("verify: " + err.Error())
		}
		creds[path] = cred
	}
	if target == "post-receipted" {
		in2, err := reliability.NewInbox(filepath.Join(root, "inbox"), 0)
		if err != nil {
			childFatal("leaf: " + err.Error())
		}
		for _, path := range paths {
			if err := in2.RecordReceipt(path, creds[path]); err != nil {
				childFatal("receipted write: " + err.Error())
			}
		}
		childExit(fmt.Sprintf("round %d died receipted, before the ack barrier", round))
	}
	for _, path := range paths {
		if err := bus.ConfirmDurable(path, creds[path]); err != nil {
			childFatal("confirm: " + err.Error())
		}
	}
	childExit(fmt.Sprintf("round %d completed the full protocol (process exit, no Close of shared roots)", round))
}

func TestDeterministicIndependentRestarts(t *testing.T) {
	if r := os.Getenv(r30Env); r != "" {
		n := 0
		if _, err := fmt.Sscanf(r, "%d", &n); err != nil || n < 1 || n > r30Rounds {
			childFatal("bad round " + r)
		}
		restartChildRound(n, os.Getenv(r30Dir))
		return
	}
	root := t.TempDir()
	totalInputs, prevInputs, totalReceipts := 0, 0, 0

	for round := 1; round <= r30Rounds; round++ {
		mode, target := restartPlan(round)
		cmd := exec.Command(os.Args[0], "-test.run", "^TestDeterministicIndependentRestarts$")
		cmd.Env = append(os.Environ(), r30Env+"="+fmt.Sprint(round), r30Dir+"="+root)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "round %d (%s/%s) child:\n%s", round, mode, target, out)
		require.Contains(t, string(out), fmt.Sprintf("RECON r=%d", round), "every round must start with a real reconcile pass")

		store, _, _ := r30Stack(root)
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 1000})
		require.NoError(t, err)
		seen := map[int64]bool{}
		var inputs, receipts int
		for _, r := range refs {
			require.False(t, seen[r.EventKey], "event key %d listed twice — no duplicate identities", r.EventKey)
			seen[r.EventKey] = true
			if r.EventType == tagentevent.TypeExternalInput {
				inputs++
			}
			if r.EventType == tagentevent.TypeInboxReceipt {
				receipts++
			}
		}
		totalInputs += len(restartLogContents(round, mode))
		require.LessOrEqual(t, inputs, totalInputs, "round %d: committed inputs must never exceed the cumulative scheduled count (%d)", round, totalInputs)
		require.GreaterOrEqual(t, inputs, prevInputs, "round %d: committed-input count is monotone across restarts (facts once laid are never lost)", round)
		prevInputs = inputs
		require.LessOrEqual(t, receipts, inputs, "receipts can never outrun committed inputs")
		envs := finishEnvelopes(t, root)
		for _, e := range envs {
			require.Regexp(t, `r30-\d\d-`, e.Raw, "an outstanding envelope must carry a scheduled round marker (provenance)")
		}
		if target != "post-ack" {
			marker := fmt.Sprintf("r30-%02d-", round)
			found := false
			for _, e := range envs {
				if strings.Contains(e.Raw, marker) {
					found = true
				}
			}
			require.True(t, found, "round %d (%s) left its own envelope(s) carrying marker %q on disk", round, target, marker)
		} else if len(envs) == 0 {
			t.Logf("round %d post-ack: inbox drained — no envelope identity claim made this round", round)
		}
		require.NoError(t, store.Close())
	}

	store, bus, ta := r30Stack(root)
	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Zero(t, s.Quarantined+s.Blocked, "30 scheduled rounds must leave NO unexplainable material: %+v", s)
	for bus.DurablePending() > 0 {
		batch, err := bus.Pull(context.Background())
		require.NoError(t, err)
		if len(batch) == 0 {
			break
		}
		require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
		for _, e := range batch {
			require.True(t, ta.contextManager.persistBusEvent(e))
		}
		ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	}

	wantAccepted := 0
	for round := 1; round <= r30Rounds; round++ {
		mode, _ := restartPlan(round)
		wantAccepted += len(restartLogContents(round, mode))
	}
	require.Equal(t, 45, wantAccepted)
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 1000})
	require.NoError(t, err)
	var inputs, receipts int
	keys := map[int64]bool{}
	for _, r := range refs {
		require.False(t, keys[r.EventKey], "final chain has a duplicate key %d", r.EventKey)
		keys[r.EventKey] = true
		switch r.EventType {
		case tagentevent.TypeExternalInput:
			inputs++
		case tagentevent.TypeInboxReceipt:
			receipts++
		}
	}
	require.Equal(t, wantAccepted, inputs, "every accepted input landed EXACTLY once across 30 restarts (totalInputs=%d)", totalInputs)
	require.Equal(t, wantAccepted, receipts, "one durable receipt per accepted envelope — none lost, none doubled (totalReceipts=%d)", totalReceipts)
	for _, body := range append(restartLogContents(1, "c"), restartLogContents(30, "ab")...) {
		found := false
		for _, r := range refs {
			full, gerr := store.GetEvent(r.EventKey)
			require.NoError(t, gerr)
			if full.EventType == tagentevent.TypeExternalInput && full.Content == body {
				found = true
			}
		}
		require.True(t, found, "content %q must be recallable from the fact chain", body)
	}

	require.Equal(t, int64(0), bus.DurablePending())
	require.Empty(t, finishEnvelopes(t, root), "inbox fully drained before the close interleave")
	require.NoError(t, bus.CloseDurable())
	require.NoError(t, store.Close())

	store2, bus2, ta2 := r30Stack(root)
	s2, err := ta2.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, ReconcileSummary{}, s2, "nothing left to recover after the close interleave")
	require.Equal(t, int64(0), bus2.DurablePending())
	refs2, err := store2.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 1000})
	require.NoError(t, err)
	require.Len(t, refs2, len(refs), "the post-close chain is byte-stable (no phantom writes)")
	tmps, err := filepath.Glob(filepath.Join(root, "inbox", "inbox-v2", "*.tmp"))
	require.NoError(t, err)
	require.Empty(t, tmps, "no unconfirmed material residue after 30 deterministic rounds")
	require.NoError(t, bus2.CloseDurable())
	require.NoError(t, store2.Close())
}

// requestCapturingModel records every model.Request it receives so that tests
// can inspect the actual message list sent to the LLM.
type requestCapturingModel struct {
	mu       sync.Mutex
	requests []*model.Request
	resp     *model.Response
}

func (m *requestCapturingModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	go func() {
		defer close(ch)
		if m.resp != nil {
			ch <- m.resp
		} else {
			<-ctx.Done()
		}
	}()
	return ch, nil
}

func (m *requestCapturingModel) Info() model.Info { return model.Info{Name: "capture-model"} }

func (m *requestCapturingModel) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func (m *requestCapturingModel) snapshotRequests() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

// TestRunEventLoop_DurableBatch_ABStoredCDeferred 钉住 两条输入经持久收件箱受理、类型化领取、写前屏障冻结并落库。
// - 执行期间到达的第三条属于下一次拉取，不得出现在本回合的模型请求里。
func TestRunEventLoop_DurableBatch_ABStoredCDeferred(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	outputCh := make(chan *trpcEvent.Event, 20)
	ta := newTestTagentAgent("durable-batch", captureModel, nil, outputCh, bus)
	cm := ta.contextManager

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, cm)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "durable-A"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "durable-B"}))
	time.Sleep(80 * time.Millisecond)

	deadline := time.After(3 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first model call")
		case <-time.After(10 * time.Millisecond):
		}
	}

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "durable-C-late"}))
	time.Sleep(50 * time.Millisecond)

	reqs := captureModel.snapshotRequests()
	require.NotEmpty(t, reqs)
	var firstContent string
	for _, m := range reqs[0].Messages {
		firstContent += "\n" + m.Content
	}
	require.NotContains(t, firstContent, "durable-C-late",
		"F5 durable: C arriving mid-turn must NOT appear in the current model request")

	refs, err := cm.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{cm.partitionID}, Limit: 100})
	require.NoError(t, err)
	storedContent := ""
	for _, ref := range refs {
		evt, gerr := cm.memStore.GetEvent(ref.EventKey)
		if gerr == nil {
			storedContent += "\n" + evt.Content
			hasA := strings.Contains(evt.Content, "durable-A")
			hasB := strings.Contains(evt.Content, "durable-B")
			require.False(t, hasA && hasB,
				"§4.4: merged echo must NOT be double-written as one fact, got %q", evt.Content)
		}
	}
	require.Contains(t, storedContent, "durable-A", "F5 durable: A must be stored via persistBusEvent")
	require.Contains(t, storedContent, "durable-B", "F5 durable: B must be stored via persistBusEvent")

	require.Eventually(t, func() bool { return bus.DurablePending() == 0 }, 3*time.Second, 20*time.Millisecond,
		"all three durable envelopes (A,B first turn; C second turn) must receipt+ack, emptying the inbox")
}

// TestBeforeModel_DoesNotClaimMidTurnEvents 钉住 回合执行期间到达的事件不得被装配阶段认领，也不得出现在本回合的模型请求里。
// - 它们属于下一次拉取与下一个回合。
func TestBeforeModel_DoesNotClaimMidTurnEvents(t *testing.T) {
	bus := NewEventBus()
	cm := newTestContextManager("f5-test", &loopMockModel{}, nil, nil, bus)
	seedProjectionWithFact(cm, "initial-fact")

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "C-mid-turn"}))
	time.Sleep(10 * time.Millisecond)

	req := &model.BeforeModelArgs{
		Request: &model.Request{Messages: []model.Message{
			{Role: model.RoleSystem, Content: "sys"},
			{Role: model.RoleUser, Content: "A+B"},
		}},
	}
	cm.assembleRequest(context.Background(), req)

	for _, m := range req.Request.Messages {
		require.NotContains(t, m.Content, "C-mid-turn",
			"F5/D1: BeforeModel must NOT claim mid-turn bus events")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	events, err := bus.Pull(ctx2)
	require.NoError(t, err)
	var found bool
	for _, e := range events {
		if e.Message != nil && e.Message.Content == "C-mid-turn" {
			found = true
		}
	}
	require.True(t, found, "F5: C must remain pending for the next turn Pull")
}

// TestRunEventLoop_ABOneTurnCNextTurn 钉住 批次语义：两条输入作为同一条来源在本回合合并处理。
// - 执行期间才到达的那条属于下一次拉取与下一个回合，不追加进当前请求。
func TestRunEventLoop_ABOneTurnCNextTurn(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus := NewEventBus()
	outputCh := make(chan *trpcEvent.Event, 20)
	cm := newTestContextManager("ab-batch", captureModel, nil, outputCh, bus)
	ta := newTestTagentAgent("ab-batch", captureModel, nil, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg-A"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg-B"}))
	go ta.runEventLoop(ctx, bus, cm)
	time.Sleep(50 * time.Millisecond)

	deadline := time.After(3 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first model call")
		case <-time.After(10 * time.Millisecond):
		}
	}

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg-C-late"}))
	time.Sleep(50 * time.Millisecond)

	reqs := captureModel.snapshotRequests()
	require.NotEmpty(t, reqs)
	firstReq := reqs[0]
	var firstContents string
	for _, m := range firstReq.Messages {
		firstContents += "\n" + m.Content
	}
	require.Contains(t, firstContents, "msg-A", "A must be in first turn")
	require.Contains(t, firstContents, "msg-B", "B must be in first turn")
	require.NotContains(t, firstContents, "msg-C-late",
		"F5/D1: C arriving mid-execution must NOT appear in the current turn")
}

// driveRecoveryGate runs the C execution gate over a request and returns the exact
// model.Request the inner model received (withRecoveryNotice applied). The one-shot
// recovery notice is now injected at the ACTUAL model invocation (the gate), not in
// assembleRequest, so the D6/F9/5.2/5.3 invariants are asserted on the gate path.
func driveRecoveryGate(t *testing.T, cm *ContextManager, req *model.Request) *model.Request {
	t.Helper()
	inner := &requestCapturingModel{resp: &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}}
	g := newExecutionGateModel(inner, cm)
	_, err := g.GenerateContent(context.Background(), req)
	require.NoError(t, err)
	captured := inner.snapshotRequests()
	require.NotEmpty(t, captured, "gate must invoke the inner model")
	return captured[len(captured)-1]
}

// TestExecutionGate_AppendsRecoveryNoticeAtTail 钉住 恢复通告追加在实际模型请求的尾部（用户角色），绝不持久化，且恰好消费一次。
func TestExecutionGate_AppendsRecoveryNoticeAtTail(t *testing.T) {
	cm := newTestContextManager("f9-test", &loopMockModel{}, nil, nil, nil)
	cm.memStore = memory.NewInMemoryStore()

	cm.recoveryMu.Lock()
	cm.recoveryNotice = "[recovery] partial restore, 2 keys missing"
	cm.recoveryMu.Unlock()

	got := driveRecoveryGate(t, cm, &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "hello"},
	}})

	require.NotEmpty(t, got.Messages)
	last := got.Messages[len(got.Messages)-1]
	require.Equal(t, model.RoleUser, last.Role, "F9: recovery notice must be user-role, not system/assistant")
	require.Equal(t, "[recovery] partial restore, 2 keys missing", last.Content,
		"F9/D6: partial recovery notice must appear at the model request tail")

	refs, err := cm.memStore.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName("f9-test")}, Limit: 100})
	require.NoError(t, err)
	require.Empty(t, refs, "F9: recovery notice (and the gate) must NOT write to the fact chain")

	got2 := driveRecoveryGate(t, cm, &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "second call"},
	}})
	for _, m := range got2.Messages {
		require.NotContains(t, m.Content, "[recovery]",
			"F9: notice must be consumed exactly once per cold start")
	}
}

// TestExecutionGate_FullRecovery_NoNotice 钉住 健康恢复不附加通告开销：恢复通告为空时请求原样通过。
func TestExecutionGate_FullRecovery_NoNotice(t *testing.T) {
	cm := newTestContextManager("full-recovery", &loopMockModel{}, nil, nil, nil)
	got := driveRecoveryGate(t, cm, &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "hello"},
	}})
	require.Len(t, got.Messages, 2, "no notice → request length unchanged")
	for _, m := range got.Messages {
		require.NotContains(t, m.Content, "[recovery]",
			"F9: full recovery must NOT inject any notice tokens")
	}
}

// TestRecoveryNotice_DiagnosticsStillReadable 钉住 一次性模型通告被消费之后，结构化的重建结果仍可取。
// - 宿主诊断不得因通告被读走而退化。
func TestRecoveryNotice_DiagnosticsStillReadable(t *testing.T) {
	cm := newTestContextManager("f9-diag", &loopMockModel{}, nil, nil, nil)
	cm.memStore = memory.NewInMemoryStore()

	cm.recoveryMu.Lock()
	cm.recovery = &RecoveryResult{Status: "partial", Mode: "snapshot", Projected: 5, Truncated: 2}
	cm.recoveryNotice = "[recovery] partial restore"
	cm.recoveryMu.Unlock()

	driveRecoveryGate(t, cm, &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "hello"},
	}})

	cm.recoveryMu.Lock()
	noticeCleared := cm.recoveryNotice == ""
	cm.recoveryMu.Unlock()
	require.True(t, noticeCleared, "5.2: recoveryNotice must be cleared after the gate (TakeRecoveryNotice)")

	result := cm.RecoveryResult()
	require.NotNil(t, result, "5.2: RecoveryResult must remain readable after notice consumption")
	require.Equal(t, "partial", result.Status, "5.2: RecoveryResult.Status must survive")
	require.Equal(t, 2, result.Truncated, "5.2: RecoveryResult.Truncated must survive")
}

// TestRecoveryNotice_SystemPromptUnchanged 钉住 恢复通告作为新增的用户角色消息追加，绝不就地改写系统提示或任何既有消息。
func TestRecoveryNotice_SystemPromptUnchanged(t *testing.T) {
	cm := newTestContextManager("f9-sys", &loopMockModel{}, nil, nil, nil)
	cm.memStore = memory.NewInMemoryStore()
	seedProjectionWithFact(cm, "fact")

	cm.recoveryMu.Lock()
	cm.recoveryNotice = "[recovery] test notice"
	cm.recoveryMu.Unlock()

	const systemContent = "original system prompt"
	orig := &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: systemContent},
		{Role: model.RoleUser, Content: "user input"},
	}}
	got := driveRecoveryGate(t, cm, orig)

	// System message must be unchanged in the request the model received.
	var systemMsgs []model.Message
	for _, m := range got.Messages {
		if m.Role == model.RoleSystem {
			systemMsgs = append(systemMsgs, m)
		}
	}
	require.Len(t, systemMsgs, 1, "5.2: exactly one system message")
	require.Equal(t, systemContent, systemMsgs[0].Content,
		"5.2/D6: recovery notice must NOT modify the system prompt content")
	require.Len(t, orig.Messages, 2, "5.2/D6: gate must not mutate the caller's request in place")
}

// TestRecoveryNotice_NotInProjection 钉住 恢复通告只注入运行时的模型请求，不得进入投影。
// - 否则从事实链重开时，会把这串一次性的运行时通告重放出来。
func TestRecoveryNotice_NotInProjection(t *testing.T) {
	cm := newTestContextManager("f9-proj", &loopMockModel{}, nil, nil, nil)
	cm.memStore = memory.NewInMemoryStore()
	factRef := seedProjectionWithFact(cm, "real-fact")

	cm.recoveryMu.Lock()
	cm.recoveryNotice = "[recovery] should not reach projection"
	cm.recoveryMu.Unlock()

	req := &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "trigger"},
	}}
	driveRecoveryGate(t, cm, req)

	refs := cm.projection.GetAll()
	require.NotEmpty(t, refs, "5.3: projection must not be empty")
	for _, r := range refs {
		require.NotEqual(t, 0, r.EventKey, "5.3: all projection entries must have valid EventKeys")
	}
	require.Equal(t, factRef.EventKey, refs[0].EventKey,
		"5.3: original fact must remain in projection")
	require.Len(t, refs, 1, "5.3: the gate must not append the notice to the projection")
}

// seedProjectionWithFact ============================================================================
// helpers (in-package, no import cycles)
// ============================================================================
func seedProjectionWithFact(cm *ContextManager, content string) memory.EventReference {
	key := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	ref := memory.EventReference{
		EventKey: key, PartitionID: cm.partitionID,
		EventType: tagentevent.TypeExternalInput, EventSummary: content,
		Timestamp: time.Now().UnixMilli(), Role: "user",
	}
	cm.projection.Append(ref)
	return ref
}

// 本文件负责执行门对凭据的态度：未验证与已拒绝的凭据一律阻断、无凭据放行、迭代器惰性消费
// 通告，且未验证凭据**不得 ack**（断点必须保留）。
// 契约: docs/wiki/platform/reincarnation-notice.md#breakpoint
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/stretchr/testify/require"
	trpcEvent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func gateOKResp() *model.Response {
	return &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}
}

func plainReq() *model.Request {
	return &model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}}
}

func newGate(t *testing.T) (*executionGateModel, *requestCapturingModel, *ContextManager) {
	t.Helper()
	cm := newTestContextManager("gate", &loopMockModel{}, nil, nil, nil)
	inner := &requestCapturingModel{resp: gateOKResp()}
	return newExecutionGateModel(inner, cm), inner, cm
}

// TestExecutionGate_BlocksUnverifiedCredential A durable turn whose credential is not yet bound MUST be blocked (no inner call).
func TestExecutionGate_BlocksUnverifiedCredential(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"}
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	_, err := g.GenerateContent(ctx, plainReq())
	require.ErrorIs(t, err, ErrExecutionCredentialUnverified)
	require.Equal(t, 0, inner.requestCount(), "blocked model must NOT be invoked")
}

// TestExecutionGate_PassesVerifiedCredential A bound credential (Verified) passes through to the inner model.
func TestExecutionGate_PassesVerifiedCredential(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"}
	cred.Bind("inv-root")
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	_, err := g.GenerateContent(ctx, plainReq())
	require.NoError(t, err)
	require.Equal(t, 1, inner.requestCount(), "verified turn must reach the model")
}

// TestExecutionGate_BlocksRejectedCredential A rejected credential (bound but downgraded by a plugin error) is blocked.
func TestExecutionGate_BlocksRejectedCredential(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"}
	cred.Bind("inv-root")
	cred.MarkRejected("plugin store error")
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	_, err := g.GenerateContent(ctx, plainReq())
	require.ErrorIs(t, err, ErrExecutionCredentialUnverified)
	require.Equal(t, 0, inner.requestCount(), "rejected turn must be blocked")
}

// TestExecutionGate_NoCredential_Passes No credential (volatile / non-durable turn) passes through unchanged.
func TestExecutionGate_NoCredential_Passes(t *testing.T) {
	g, inner, _ := newGate(t)
	_, err := g.GenerateContent(context.Background(), plainReq())
	require.NoError(t, err)
	require.Equal(t, 1, inner.requestCount())
}

// TestExecutionGate_IteratorLazilyConsumesNotice 钉住 创建惰性迭代器既不消费恢复通告也不调模型，只有真正开始迭代才算。
// - 这条判据支撑"被创建后取消的迭代器不是一次模型调用"。
// 契约: docs/wiki/platform/reincarnation-notice.md#consumption
func TestExecutionGate_IteratorLazilyConsumesNotice(t *testing.T) {
	g, inner, cm := newGate(t)
	cm.recoveryMu.Lock()
	cm.recoveryNotice = "[recovery] lazy one-shot"
	cm.recoveryMu.Unlock()

	seq, err := g.GenerateContentIter(context.Background(), plainReq())
	require.NoError(t, err)

	cm.recoveryMu.Lock()
	still := cm.recoveryNotice
	cm.recoveryMu.Unlock()
	require.Equal(t, "[recovery] lazy one-shot", still, "creating the iterator must not consume the notice")
	require.Equal(t, 0, inner.requestCount(), "creating the iterator must not call the model")

	count := 0
	seq(func(*model.Response) bool { count++; return true })
	require.Equal(t, 1, count, "iterator yielded the response")
	cm.recoveryMu.Lock()
	cleared := cm.recoveryNotice
	cm.recoveryMu.Unlock()
	require.Equal(t, "", cleared, "iterating consumes the recovery notice")
	reqs := inner.snapshotRequests()
	require.NotEmpty(t, reqs)
	got := reqs[len(reqs)-1].Messages
	require.Equal(t, "[recovery] lazy one-shot", got[len(got)-1].Content, "notice injected at actual iteration")
}

// TestExecutionGate_BlocksUnverifiedOnIterator 钉住 iterator 路径的模型入口错误极性。
// - 未核验凭据既 block 模型调用，又把 block 外显为一个 error response。
// - 静默零输出流会被当作 completed turn 收敛并 ack 掉持久输入。
func TestExecutionGate_BlocksUnverifiedOnIterator(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"}
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	seq, err := g.GenerateContentIter(ctx, plainReq())
	require.NoError(t, err)
	var got []*model.Response
	seq(func(r *model.Response) bool { got = append(got, r); return true })
	require.Len(t, got, 1, "a blocked iterator must surface exactly one error response, never silence")
	require.NotNil(t, got[0].Error, "the block must carry Response.Error so the turn reduces to failed")
	require.EqualValues(t, 0, inner.requestCount(), "blocked iterator must not call the model")
}

// iterErrInner implements model.IterModel but fails iterator creation — the
// preparation/failover-candidate failure shape the gate must not swallow.
type iterErrInner struct{ calls int }

func (m *iterErrInner) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	m.calls++
	ch := make(chan *model.Response, 1)
	ch <- gateOKResp()
	close(ch)
	return ch, nil
}

func (m *iterErrInner) GenerateContentIter(context.Context, *model.Request) (model.Seq[*model.Response], error) {
	m.calls++
	return nil, errors.New("iterator creation failed")
}

func (m *iterErrInner) Info() model.Info { return model.Info{Name: "iter-err"} }

// nilChInner is a channel-only model that returns (nil, nil) — the shape
// SwappableModel legitimately passes through, which must not hang or fake success.
type nilChInner struct{ calls int }

func (m *nilChInner) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	m.calls++
	return nil, nil
}

func (m *nilChInner) Info() model.Info { return model.Info{Name: "nil-ch"} }

// TestExecutionGate_IteratorCreationErrorSurfacesAsFailure 钉住 inner 迭代器创建失败必须以错误响应呈现。
// - 静默零产出会被归约为 completed 并 ack 持久输入——正是本门要消灭的谬误。
func TestExecutionGate_IteratorCreationErrorSurfacesAsFailure(t *testing.T) {
	cm := newTestContextManager("gate", &loopMockModel{}, nil, nil, nil)
	inner := &iterErrInner{}
	g := newExecutionGateModel(inner, cm)

	seq, err := g.GenerateContentIter(context.Background(), plainReq())
	require.NoError(t, err)
	var got []*model.Response
	seq(func(r *model.Response) bool { got = append(got, r); return true })
	require.Len(t, got, 1, "inner iterator-creation error must yield exactly one error response")
	require.NotNil(t, got[0].Error)
	require.Contains(t, got[0].Error.Message, "iterator creation failed")
	require.True(t, got[0].Done, "the error response terminates the stream")
}

// TestExecutionGate_NilChannelSurfacesAsFailure 钉住 inner 返回 (nil, nil) 时迭代路径以错误响应呈现、不挂死。
func TestExecutionGate_NilChannelSurfacesAsFailure(t *testing.T) {
	cm := newTestContextManager("gate", &loopMockModel{}, nil, nil, nil)
	inner := &nilChInner{}
	g := newExecutionGateModel(inner, cm)

	seq, err := g.GenerateContentIter(context.Background(), plainReq())
	require.NoError(t, err)
	done := make(chan struct{})
	var got []*model.Response
	go func() {
		seq(func(r *model.Response) bool { got = append(got, r); return true })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nil channel must terminate the iterator, not block forever")
	}
	require.Len(t, got, 1, "a nil stream must surface exactly one error response")
	require.NotNil(t, got[0].Error)
	require.Contains(t, got[0].Error.Message, "nil stream")
}

// credFaultStore fails StoreEvent for assistant `agent_output` events only. The durable
// input still commits via the inherited ReplayEvent, and finishDurableBatch's inbox-receipt
// StoreEvent still succeeds — so a credentialed turn's assistant store fails (→ MarkRejected)
// while the ack path remains functional. This makes the  loop guard the SOLE reason the
// envelope stays un-acked, unlike an
// always-failing store that would also break the receipt write.
type credFaultStore struct{ *memory.InMemoryStore }

func (s *credFaultStore) StoreEvent(k int64, ev memory.FullEvent) error {
	if ev.EventType == tagentevent.TypeAgentOutput {
		return errors.New("disk full")
	}
	return s.InMemoryStore.StoreEvent(k, ev)
}

// newDurableAgentWithStore mirrors newTestContextManager but injects a caller-supplied
// store into BOTH the ContextManager and its MemoryPlugin (one cm, as in prod), so a test
// can drive the real event loop against a faulting store while the durable input still
// commits via ReplayEvent.
func newDurableAgentWithStore(name string, m model.Model, store memory.MemoryStore, outputCh chan *trpcEvent.Event, bus *EventBus) *TagentAgent {
	compressor := compress.NewSmartCompressor(compress.WithMaxTokens(8000), compress.WithTokenCounter(&mockTokenCounter{tokens: 100}))
	memPlugin := plugin.NewMemoryPlugin(store)
	cm := NewContextManager(ContextManagerConfig{
		Name:         name,
		UserID:       "test-user",
		SessionID:    "test-session",
		Model:        m,
		MaxToolIters: 10,
		Compressor:   compressor,
		TokenCounter: &mockTokenCounter{tokens: 100},
		MaxTokens:    8000,
		ThresholdPct: 0.8,
		MemStore:     store,
		MemPlugin:    memPlugin,
		SessionSvc:   sessioninmemory.NewSessionService(),
		OutputCh:     outputCh,
		Bus:          bus,
		Projection:   compress.NewSessionProjection(),
		OnEvent:      func(evt *trpcEvent.Event) {},
	})
	return &TagentAgent{name: name, persistentBus: bus, activeBus: bus, contextManager: cm, outputCh: outputCh}
}

// TestRunEventLoop_UnverifiedCredentialDoesNotAck 钉住 (loop commit gate): a durable turn whose input facts committed (turnEcho installed)
func TestRunEventLoop_UnverifiedCredentialDoesNotAck(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	outputCh := make(chan *trpcEvent.Event, 20)
	store := &credFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta := newDurableAgentWithStore("cred-fault", captureModel, store, outputCh, bus)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hello-durable"}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	deadline := time.After(4 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("durable commit + gate never reached the model (batch not committed via ReplayEvent?)")
		case <-time.After(10 * time.Millisecond):
		}
	}

	watch := time.After(2 * time.Second)
	timedOut := false
	for !timedOut && bus.DurablePending() != 0 {
		select {
		case <-watch:
			timedOut = true
		case <-time.After(10 * time.Millisecond):
		}
	}
	require.NotZero(t, bus.DurablePending(), "§4.5: a framework-swallowed plugin store error must keep the credential unverified and the envelope NOT acked (fail-closed)")
	require.EqualValues(t, 1, bus.DurablePending(), "the un-acked claim must stay outstanding for replay")
}

// TestNewAttemptEchoCredential_RetryReusesFrozenBatchNoExpansion 钉住 凭据模板在批次拉取时就已冻结。
// - 同一业务回合的每次重试都新铸唯一口令，却复用那一份冻结的合并消息与已提交事实键；
// - 因此重试绝不重拉批次，也不得把批次加宽。
func TestNewAttemptEchoCredential_RetryReusesFrozenBatchNoExpansion(t *testing.T) {
	cm := &ContextManager{name: "resident", sessionID: "s1"}
	frozen := []int64{111, 222, 333}
	cm.turnEcho = &echoSpec{agent: "resident", session: "s1", mergedMessage: "A\n\n---\n\nB\n\n---\n\nC", committedKeys: frozen}

	cred1 := cm.newAttemptEchoCredential()
	cred2 := cm.newAttemptEchoCredential()

	require.NotEqual(t, cred1.AttemptToken, cred2.AttemptToken, "each attempt gets a fresh unique token")
	require.Equal(t, cred1.MergedMessage, cred2.MergedMessage, "retry reuses the frozen batch's merged input verbatim (never expanded)")
	require.Equal(t, cred1.CommittedKeys, cred2.CommittedKeys, "retry reuses the exact committed fact keys (no new inputs pulled)")
	require.Equal(t, frozen, cred1.CommittedKeys)
	require.Equal(t, "resident", cred1.Agent)
	require.Equal(t, "s1", cred1.Session)
}

// TestGate_ReceiptKeyBeforeCompactionBoundary 钉住 落在快照锚点之前的未确认回执键对扫描窗口不可见，回收仍须经信封自身的预留找到它。
// - 只重投已冻结的回执，绝不重新执行。
func TestGate_ReceiptKeyBeforeCompactionBoundary(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)
	ta.contextManager.projection = compress.NewSessionProjection()
	ta.contextManager.contextCompressor = compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2)),
		healthy, compress.NewDefaultTokenCounter(), 60, 0.8, 2,
		compress.WithRecentFullCount(2))
	env := envelopeAt(t, path)
	frozen, ferr := decodeCompletion(env.Completion)
	require.NoError(t, ferr)
	receiptKey, kerr := tagentevent.ParseEventKey(frozen.ReceiptKey)
	require.NoError(t, kerr)

	snapKey := memory.NewSnowflakeEventKey(1, time.Now().UnixMilli()+3600_000)
	require.Greater(t, snapKey, receiptKey, "the boundary must sit AFTER the outstanding receipt key")
	payload, perr := json.Marshal(&compress.CompactionPayload{
		SummaryRef:   memory.EventReference{EventKey: -1, EventType: tagentevent.TypeContextCompress},
		FullBoundary: 0,
	})
	require.NoError(t, perr)
	require.NoError(t, healthy.StoreEvent(snapKey, memory.FullEvent{
		EventKey: snapKey, PartitionID: 1, EventType: tagentevent.TypeContextCompressSummary,
		Timestamp: time.Now().UnixMilli(), Metadata: map[string]string{
			compress.CompactionMetaKey:        compress.CompactionGenV1,
			compress.CompactionPayloadMetaKey: string(payload),
		},
	}))

	ta.RebuildProjectionFromWAL()
	res := ta.contextManager.RecoveryResult()
	require.NotNil(t, res)
	require.Equal(t, "snapshot", res.Mode, "precondition: the rebuild anchored at the newer snapshot")

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.ReceiptsAdded, "direct lookup finds the key outside the scan window and re-submits ONLY the receipt")

	stored, gerr := healthy.GetEvent(receiptKey)
	require.NoError(t, gerr)
	require.Equal(t, frozen.ReceiptFact.Timestamp, stored.Timestamp,
		"§5.10: the re-submitted receipt reuses the FROZEN original time, never a re-stamp")
	require.Equal(t, frozen.ReceiptFact.Metadata, stored.Metadata, "and the frozen attribution verbatim")
	for _, ref := range ta.contextManager.projection.GetAll() {
		require.NotEqual(t, receiptKey, ref.EventKey, "the scan/projection never harvested the out-of-window receipt")
	}
}

// TestGate_CrossProcessCompletionOnlyRecovery 钉住 子进程冻结持久终局后不做任何清理便退出，父进程共享同一批目录。
// - 父进程必须登记保护、直接从冻结的字节对账（不重跑、不重打标记）、随后清理并恰好释放一次。
func TestGate_CrossProcessCompletionOnlyRecovery(t *testing.T) {
	const (
		childEnv = "TAGENT_XPROC_CHILD"
		dirEnv   = "TAGENT_XPROC_DIR"
		factEnv  = "TAGENT_XPROC_FACT"
		recEnv   = "TAGENT_XPROC_RECEIPT"
		frozenMs = int64(1700000123456)
	)
	if os.Getenv(childEnv) == "1" {
		xprocChildFreezeCompletionOnly(os.Getenv(dirEnv), frozenMs,
			mustI64(os.Getenv(factEnv)), mustI64(os.Getenv(recEnv)))
		os.Exit(0)
	}

	root := t.TempDir()
	dirStore, dirInbox := root+"/store", root+"/inbox"
	inputKey := memory.NewSnowflakeEventKey(1, frozenMs-2000)
	receiptKey := memory.NewSnowflakeEventKey(1, frozenMs-1000)
	cmd := exec.Command(os.Args[0], "-test.run", "^TestGate_CrossProcessCompletionOnlyRecovery$", "-test.v")
	cmd.Env = append(os.Environ(), childEnv+"=1", dirEnv+"="+root,
		factEnv+"="+fmt.Sprint(inputKey), recEnv+"="+fmt.Sprint(receiptKey))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child (Phase A) failed: %v\n%s", err, out)
	}

	kvStore, err := kv.NewLocalFileKV(dirStore)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, dirStore, 100)
	require.NoError(t, err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus, err := NewReliableEventBus(dirInbox)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox(), "protection is rebuilt from the on-disk envelope before forgetting")
	require.True(t, store.IsKeyProtected(inputKey), "the overdue input original is leased")
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection()}
	ta := &TagentAgent{name: "xproc", persistentBus: bus, contextManager: cm}
	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.ReceiptsAdded, "the frozen completion re-submits its own receipt — no model in this process")

	stored, gerr := store.GetEvent(receiptKey)
	require.NoError(t, gerr)
	require.Equal(t, tagentevent.TypeInboxReceipt, stored.EventType)
	require.Equal(t, frozenMs, stored.Timestamp, "the cross-process receipt carries the FROZEN time, not the recovery clock")
	require.Equal(t, "frozen-first", stored.Metadata["attribution"], "and the frozen attribution, re-used verbatim across the process boundary")
	require.Equal(t, int64(0), bus.DurablePending(), "cleaned up after the receipt+ack barrier")
	require.False(t, store.IsKeyProtected(inputKey), "released exactly once after the ack barrier")
	_, ierr := store.GetEvent(inputKey)
	require.NoError(t, ierr, "the ORIGINAL input fact is intact across the process boundary (no re-run needed, none happened)")
	require.Equal(t, 1, countReceipts(t, store), "exactly one receipt on the chain — no duplicate, no re-run")
}

// xprocChildFreezeCompletionOnly drives the real leaf protocol up to — but
// deliberately NOT past — Phase B, then returns so the caller exits(0) like a
// crashed process.
func xprocChildFreezeCompletionOnly(root string, frozenMs, factKey, receiptKey int64) {
	kvStore, err := kv.NewLocalFileKV(root + "/store")
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, root+"/store", 100)
	mustX(err)
	mustX(store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: tagentevent.TypeExternalInput,
		EventSummary: "xproc input", Content: "xproc input", Timestamp: frozenMs - 1000,
	}))
	in, err := reliability.NewInbox(root+"/inbox", 0)
	mustX(err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "xproc-1", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"x-1","type":"external_input","message":{"role":"user","content":"x"}}`)}},
	})
	mustX(err)
	_, path, err := in.ClaimNext()
	mustX(err)
	hexR := tagentevent.FormatEventKey(receiptKey)
	mustX(in.PrepareFacts(path, hexR, []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"event_key":%d}`, factKey))}))
	fact, err := buildReceiptFact(hexR, 1, "xproc-1", "xproc", frozenMs, map[string]string{"attribution": "frozen-first"})
	mustX(err)
	raw, err := freezeCompletion(completion{
		CompletionVersion: completionVersion,
		RequestID:         "xproc-1",
		ReceiptKey:        hexR,
		CompletedAtMs:     frozenMs,
		BatchResult:       batchCompleted,
		Slots:             []completionSlot{{Slot: 0, SourceID: "x-1", Disposition: slotProcessed, FactKey: tagentevent.FormatEventKey(factKey)}},
		ReceiptFact:       fact,
	})
	mustX(err)
	mustX(in.RecordCompletion(path, raw))
}

func mustX(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "xproc child:", err)
		os.Exit(2)
	}
}

func mustI64(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic("xproc env: bad int64: " + s)
	}
	return n
}

// validCompletion helper: a well-formed completion for reuse across tests.
func validCompletion(t *testing.T) completion {
	t.Helper()
	fact, err := buildReceiptFact("1a2b3c", 7, "req-1", "resident", 1700000000000, map[string]string{"rollout_id": "s1"})
	require.NoError(t, err)
	return completion{
		CompletionVersion: completionVersion,
		RequestID:         "req-1",
		ReceiptKey:        "1a2b3c",
		CompletedAtMs:     1700000000000,
		BatchResult:       batchCompleted,
		Slots: []completionSlot{
			{Slot: 0, SourceID: "src-a", Disposition: slotProcessed, FactKey: "beef"},
			{Slot: 1, SourceID: "src-b", Disposition: slotSkipped, Reason: skipReasonMeditationYield},
		},
		ReceiptFact: fact,
	}
}

// TestBatchResultFromOutcome 钉住 结局到批次结果的映射：完成与失败都成形为完成项，失败带自己的摘要。
// - 被取消的回合不成形，循环因此不会把一个未到终态的回合冻结下来。
func TestBatchResultFromOutcome(t *testing.T) {
	r, sum, ok := batchResultFromOutcome(completedOutcome())
	require.True(t, ok)
	require.Equal(t, batchCompleted, r)
	require.Empty(t, sum)

	r, sum, ok = batchResultFromOutcome(failedOutcome("server_error: boom"))
	require.True(t, ok)
	require.Equal(t, batchFailed, r)
	require.Equal(t, "server_error: boom", sum)

	_, _, ok = batchResultFromOutcome(cancelledOutcome())
	require.False(t, ok, "a cancelled turn must not form a completion")
}

// TestBuildReceiptFact_UsesFrozenKeyAndTime 钉住 回执事实必须用冻结的键与时间：每次现取新雪花 EventKey 并用 time.Now() 会让重启或重试产出不同回执。
func TestBuildReceiptFact_UsesFrozenKeyAndTime(t *testing.T) {
	reserved := int64(0x1a2b3c)
	fact, err := buildReceiptFact(tagentevent.FormatEventKey(reserved), 7, "req-1", "resident", 1700000000000,
		map[string]string{"rollout_id": "s1", "trigger_source": "user"})
	require.NoError(t, err)
	require.Equal(t, reserved, fact.EventKey, "receipt key is the RESERVED envelope key, not a fresh snowflake")
	require.EqualValues(t, tagentevent.TypeInboxReceipt, fact.EventType)
	require.EqualValues(t, 1700000000000, fact.Timestamp, "timestamp is the frozen completion time, not time.Now")
	require.Equal(t, "req-1", fact.Metadata["inbox_request_id"])
	require.Equal(t, "resident", fact.Metadata[tagentevent.MetaKeyAgentName])
	require.Equal(t, "s1", fact.Metadata["rollout_id"], "first attribution is carried into the receipt")
	require.Equal(t, "user", fact.Metadata["trigger_source"])

	fact2, err := buildReceiptFact(tagentevent.FormatEventKey(reserved), 7, "req-1", "resident", 1700000000000,
		map[string]string{"rollout_id": "s1", "trigger_source": "user"})
	require.NoError(t, err)
	require.Equal(t, fact.EventKey, fact2.EventKey)

	_, err = buildReceiptFact("!!!not-hex!!!", 7, "req", "a", 1, nil)
	require.Error(t, err, "an unparseable reserved key is a deterministic error, never a fresh-key fallback")
}

// TestFreezeCompletion_Deterministic 钉住 冻结只由输入决定：内部不取时钟、不生成键。
// - 因此相同的重试会看到逐字节相同的载荷，往返校验才可复现。
func TestFreezeCompletion_Deterministic(t *testing.T) {
	c := validCompletion(t)
	b1, err := freezeCompletion(c)
	require.NoError(t, err)
	b2, err := freezeCompletion(c)
	require.NoError(t, err)
	require.Equal(t, string(b1), string(b2), "identical inputs freeze byte-identical bytes")

	decoded, err := decodeCompletion(b1)
	require.NoError(t, err)
	require.Equal(t, completionVersion, decoded.CompletionVersion)
	require.Equal(t, batchCompleted, decoded.BatchResult)
	require.Len(t, decoded.Slots, 2)
	require.Equal(t, c.ReceiptFact.EventKey, decoded.ReceiptFact.EventKey)
}

// TestCompletionValidate_ExactlyOneDispositionPerSlot 钉住 每个槽位恰好一个格式良好的处置，且批次级字段自相一致。
// - 偏离一律判为确定性错误，绝不静默修复。
func TestCompletionValidate_ExactlyOneDispositionPerSlot(t *testing.T) {
	require.NoError(t, validCompletion(t).validate())

	procNoKey := validCompletion(t)
	procNoKey.Slots[0].FactKey = ""
	require.ErrorContains(t, procNoKey.validate(), "processed but no fact key")

	procWithReason := validCompletion(t)
	procWithReason.Slots[0].Reason = "junk"
	require.Error(t, procWithReason.validate())

	skipNoReason := validCompletion(t)
	skipNoReason.Slots[1].Reason = ""
	require.ErrorContains(t, skipNoReason.validate(), "unknown/absent reason")

	skipBadReason := validCompletion(t)
	skipBadReason.Slots[1].Reason = "i_felt_like_it"
	require.Error(t, skipBadReason.validate(), "skip reason must be from the closed enum set")

	deadEmpty := validCompletion(t)
	deadEmpty.Slots[1].Reason = "empty_input"
	require.Error(t, deadEmpty.validate(), "empty_input must no longer be a valid skip reason")

	skipWithKey := validCompletion(t)
	skipWithKey.Slots[1].FactKey = "cafe"
	require.Error(t, skipWithKey.validate())

	unknownDisp := validCompletion(t)
	unknownDisp.Slots[0].Disposition = "half_processed"
	require.ErrorContains(t, unknownDisp.validate(), "unknown disposition")

	dup := validCompletion(t)
	dup.Slots = append(dup.Slots, completionSlot{Slot: 0, SourceID: "src-a", Disposition: slotProcessed, FactKey: "beef"})
	require.ErrorContains(t, dup.validate(), "duplicate slot")

	failedNoSum := validCompletion(t)
	failedNoSum.BatchResult = batchFailed
	require.ErrorContains(t, failedNoSum.validate(), "failed batch without an error summary")

	completedWithSum := validCompletion(t)
	completedWithSum.ErrorSummary = "stray"
	require.Error(t, completedWithSum.validate())

	noKey := validCompletion(t)
	noKey.ReceiptKey = ""
	require.Error(t, noKey.validate())

	_, err := freezeCompletion(procNoKey)
	require.Error(t, err, "freeze must validate before marshalling")
}

// TestCompletion_FailedCarriesSummary 钉住 确定性模型失败冻结为"已处理但失败"，并带上有界摘要。
// - 失败要变成可回执的结果，而不是被丢弃。
func TestCompletion_FailedCarriesSummary(t *testing.T) {
	c := validCompletion(t)
	c.BatchResult = batchFailed
	c.ErrorSummary = "server_error: upstream exploded"
	b, err := freezeCompletion(c)
	require.NoError(t, err)
	decoded, err := decodeCompletion(b)
	require.NoError(t, err)
	require.Equal(t, batchFailed, decoded.BatchResult)
	require.Equal(t, "server_error: upstream exploded", decoded.ErrorSummary)
}

// TestCompletion_LargeKeyPrecisionRoundTrip 钉住 精确整数同一性必须贯穿终结：超过 2 的 53 次方的事件键要原样穿过冻结与解码。
// - 靠类型化的 64 位整数字段，且下游解码按数字读取、不经浮点改写；
// - 键一旦被浮点改写，同一性判定就悄悄失效。
func TestCompletion_LargeKeyPrecisionRoundTrip(t *testing.T) {
	big := int64(1) << 60
	fact, err := buildReceiptFact(tagentevent.FormatEventKey(big), 7, "req-big", "resident", 1700000000001, nil)
	require.NoError(t, err)
	require.Equal(t, big, fact.EventKey)
	c := validCompletion(t)
	c.ReceiptKey = tagentevent.FormatEventKey(big)
	c.ReceiptFact = fact
	c.Slots[0].FactKey = tagentevent.FormatEventKey(big + 1)

	b, err := freezeCompletion(c)
	require.NoError(t, err)
	decoded, err := decodeCompletion(b)
	require.NoError(t, err)
	require.Equal(t, big, decoded.ReceiptFact.EventKey, "large key preserved exactly, not collapsed through float")
	require.NotEqual(t, tagentevent.FormatEventKey(big), decoded.Slots[0].FactKey, "adjacent large keys remain distinct")
	require.Equal(t, tagentevent.FormatEventKey(big+1), decoded.Slots[0].FactKey)
}

func mustFact(t *testing.T, key int64, content string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(memory.FullEvent{EventKey: key, Content: content, EventType: tagentevent.TypeExternalInput})
	require.NoError(t, err)
	return b
}

// TestFinishDurableBatch_ReceiptUsesReservedKey 钉住 ack 只在完成与回执都已持久之后发生，且链上回执事件携带信封的预留键。
// - 预留键不得换成新生成的键：预留的含义就是"这条回执认领哪一个槽位"。
// 契约: docs/wiki/reliability/durable-delivery.md#release-claim-backoff
func TestFinishDurableBatch_ReceiptUsesReservedKey(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	reservedKey := batch[0].claim.ReceiptKey
	require.NotEmpty(t, reservedKey, "prepare reserves the fixed receipt key")
	for _, e := range batch {
		require.True(t, ta.contextManager.persistBusEvent(e))
	}

	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending(), "acked only after completion + receipt are durable")

	wantKey, err := tagentevent.ParseEventKey(reservedKey)
	require.NoError(t, err)
	var found bool
	refs, _ := ta.contextManager.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			require.Equal(t, wantKey, r.EventKey,
				"§5.3: receipt must carry the RESERVED key, never a fresh snowflake (fail-before vs old persistInboxReceipt)")
			found = true
		}
	}
	require.True(t, found, "receipt event present on the fact chain")
}

// TestFinishDurableBatch_ReceiptIdempotentResubmit 钉住 重投同一预留键的回执（确认丢失后的重放）必须收敛为已提交，且不得追加第二条回执事件。
// - 若每次重投都新铸一个键，幂等就无从谈起。
func TestFinishDurableBatch_ReceiptIdempotentResubmit(t *testing.T) {
	dir := t.TempDir()
	ta := durableAgent(t, dir)
	bus := ta.persistentBus
	_, err := bus.PublishContext(context.Background(), durableMsg("fact-A"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	for _, e := range batch {
		require.True(t, ta.contextManager.persistBusEvent(e))
	}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending())
	require.Equal(t, 2, ta.contextManager.memStore.GetStats().TotalEvents, "one input fact + one receipt")

	reservedKey := batch[0].claim.ReceiptKey
	require.NotEmpty(t, reservedKey)
	wantKey, err := tagentevent.ParseEventKey(reservedKey)
	require.NoError(t, err)
	committed, gerr := ta.contextManager.memStore.GetEvent(wantKey)
	require.NoError(t, gerr, "the reserved-key receipt is on the chain")
	require.NoError(t, ta.contextManager.commitReceiptFact(*committed), "re-committing the exact frozen receipt converges idempotently")
	require.Equal(t, 2, ta.contextManager.memStore.GetStats().TotalEvents, "re-commit must NOT double-write the receipt")
}

// TestBuildEnvelopeCompletion_DispositionsAndFixedKey 钉住 信封终局构造的四种处置各有固定形状。
// - 已提交的槽位冻结为已处理并带事实键；
// - 在已受理而未被选中的让出冥想槽位冻结为跳过，且不带事实键；
// - 回执事实使用信封的预留键；被取消的回合不成形。
func TestBuildEnvelopeCompletion_DispositionsAndFixedKey(t *testing.T) {
	evA := &AgentEvent{ID: "src-a", Type: tagentevent.TypeExternalInput, Source: "user",
		claim: &durableClaim{Path: "/inbox/a.json", RequestID: "rid-a", Slot: 0, ReceiptKey: "aaa", PreparedFact: mustFact(t, 111, "A")}}
	evB := &AgentEvent{ID: "src-b", Type: tagentevent.TypeExternalInput, Source: "meditation",
		claim: &durableClaim{Path: "/inbox/b.json", RequestID: "rid-b", Slot: 0, ReceiptKey: "bbb", PreparedFact: mustFact(t, 222, "B")}}
	evC := &AgentEvent{ID: "src-c", Type: tagentevent.TypeExternalInput, Source: "user",
		claim: &durableClaim{Path: "/inbox/c.json", RequestID: "rid-c", Slot: 1, ReceiptKey: "ccc", PreparedFact: mustFact(t, 333, "C")}}

	committed := selectedKeySet([]*AgentEvent{evA, evC})

	cA, _, err := buildEnvelopeCompletion([]*AgentEvent{evA}, committed, completedOutcome(), "resident", 1, 1700000000000, nil)
	require.NoError(t, err)
	require.Equal(t, batchCompleted, cA.BatchResult)
	require.Len(t, cA.Slots, 1)
	require.Equal(t, slotProcessed, cA.Slots[0].Disposition)
	require.Equal(t, tagentevent.FormatEventKey(111), cA.Slots[0].FactKey)
	require.EqualValues(t, 0xaaa, cA.ReceiptFact.EventKey, "receipt uses the reserved key")

	cB, _, err := buildEnvelopeCompletion([]*AgentEvent{evB}, committed, completedOutcome(), "resident", 1, 1700000000000, nil)
	require.NoError(t, err)
	require.Equal(t, slotSkipped, cB.Slots[0].Disposition)
	require.Equal(t, skipReasonMeditationYield, cB.Slots[0].Reason)
	require.Empty(t, cB.Slots[0].FactKey)
	require.EqualValues(t, 0xbbb, cB.ReceiptFact.EventKey)

	cF, _, err := buildEnvelopeCompletion([]*AgentEvent{evA, evC}, committed, failedOutcome("server_error: boom"), "resident", 1, 1700000000000, nil)
	require.NoError(t, err)
	require.Equal(t, batchFailed, cF.BatchResult)
	require.Equal(t, "server_error: boom", cF.ErrorSummary)
	require.Len(t, cF.Slots, 2)
	for _, s := range cF.Slots {
		require.Equal(t, slotProcessed, s.Disposition, "committed inputs processed even when the turn failed")
	}
	require.Equal(t, []int{0, 1}, []int{cF.Slots[0].Slot, cF.Slots[1].Slot}, "slots ordered ascending")

	_, _, err = buildEnvelopeCompletion([]*AgentEvent{evA}, committed, cancelledOutcome(), "resident", 1, 1700000000000, nil)
	require.Error(t, err, "a cancelled turn forms no completion")

	evNoFact := &AgentEvent{ID: "src-d", Type: tagentevent.TypeExternalInput, Source: "user",
		claim: &durableClaim{Path: "/inbox/d.json", RequestID: "rid-d", Slot: 0, ReceiptKey: "ddd"}}
	_, _, err = buildEnvelopeCompletion([]*AgentEvent{evNoFact}, selectedKeySet([]*AgentEvent{evNoFact}), completedOutcome(), "resident", 1, 1, nil)
	require.Error(t, err, "processed slot without a prepared fact must not fabricate a key")
}

type barrierTrackGuard struct {
	begins, ends, arms int
	protected          map[int64]int
}

func (g *barrierTrackGuard) ProtectKey(k int64) { g.protected[k]++ }
func (g *barrierTrackGuard) ReleaseKey(k int64) { g.protected[k]-- }
func (g *barrierTrackGuard) ArmRetention()      { g.arms++ }
func (g *barrierTrackGuard) BeginHold()         { g.begins++ }
func (g *barrierTrackGuard) EndHold()           { g.ends++ }

func TestArmRetention_RunsUnderBarrierAndBlocksOnUnreadableInbox(t *testing.T) {
	dir := t.TempDir()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	g := &barrierTrackGuard{protected: map[int64]int{}}
	bus.SetRetentionGuard(g)

	require.NoError(t, bus.ArmRetentionFromInbox())
	require.Equal(t, 1, g.begins)
	require.Equal(t, 1, g.ends, "a completed registration releases its barrier")
	require.Equal(t, 1, g.arms)

	envDir := filepath.Join(dir, "inbox-v2")
	require.NoError(t, os.Chmod(envDir, 0o000))
	defer func() { _ = os.Chmod(envDir, 0o700) }()
	require.ErrorContains(t, bus.ArmRetentionFromInbox(), "read inbox dir")
	require.Equal(t, 2, g.begins)
	require.Equal(t, 1, g.ends, "the failed inventory KEEPS its hold (explicit §5.8 block, never silently released)")
	require.Equal(t, 1, g.arms, "an incomplete view never arms the scanner gate")
}

const (
	finishCrashEnv = "TAGENT_FINISH_CRASH_AT"
	finishRootEnv  = "TAGENT_FINISH_DIR"
)

func childFinishCrash(root, spec string) {
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	mustX(err)
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	mustX(err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus.SetRetentionGuard(store)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-finish", persistentBus: bus, contextManager: cm}

	if _, err := bus.PublishContext(context.Background(), durableMsg("finish-slot-X")); err != nil {
		childFatal("publish: " + err.Error())
	}
	batch, err := bus.Pull(context.Background())
	mustX(err)
	if len(batch) != 1 {
		childFatal(fmt.Sprintf("expected 1-event batch, got %d", len(batch)))
	}
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		childFatal("prepare failed")
	}
	if !cm.persistBusEvent(batch[0]) {
		childFatal("persist failed")
	}
	if spec == "pre-finish" {
		childExit("child died after model returned, before any completion")
	}

	paths, byPath := groupClaimsByPath(batch)
	path := paths[0]
	_, raw, err := buildEnvelopeCompletion(byPath[path], selectedKeySet(batch),
		completedOutcome(), ta.name, cm.partitionID, time.Now().UnixMilli(), cm.buildTurnAttribution(context.Background()))
	if err != nil {
		childFatal("completion build: " + err.Error())
	}
	if err := bus.RecordCompletion(path, raw); err != nil {
		childFatal("record completion: " + err.Error())
	}
	if spec == "post-completion" {
		childExit("child died after the completion froze durable, before any receipt")
	}
	cred, err := cm.verifyReceiptCredential(raw)
	if err != nil {
		childFatal("verify credential: " + err.Error())
	}
	in2, err := reliability.NewInbox(filepath.Join(root, "inbox"), 0)
	if err != nil {
		childFatal("second leaf: " + err.Error())
	}
	if err := in2.RecordReceipt(path, cred); err != nil {
		childFatal("record receipt: " + err.Error())
	}
	childExit("child died after the receipted rewrite, before the ack barrier")
}

// reopenFinishParent gives the parent the FULL fresh stack (independent
// read-back: nothing shared with the child process).
func reopenFinishParent(t *testing.T, root string) (*memory.FileSegmentStore, *EventBus, *TagentAgent) {
	t.Helper()
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	require.NoError(t, err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox())
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-finish", persistentBus: bus, contextManager: cm}
	return store, bus, ta
}

func finishEnvelopes(t *testing.T, root string) []OutstandingEnvLike {
	t.Helper()
	dir := filepath.Join(root, "inbox", "inbox-v2")
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	require.NoError(t, err)
	var out []OutstandingEnvLike
	for _, e := range entries {
		raw, err := os.ReadFile(e)
		require.NoError(t, err)
		out = append(out, OutstandingEnvLike{Name: filepath.Base(e), Raw: string(raw)})
	}
	return out
}

// OutstandingEnvLike is a raw-disk envelope snapshot for cross-checks (the
// parent must verify what the CHILD left, before any recovery rewrite).
type OutstandingEnvLike struct{ Name, Raw string }

func TestCrashMatrix_FinishSideWindows(t *testing.T) {
	if spec := os.Getenv(finishCrashEnv); spec != "" {
		childFinishCrash(os.Getenv(finishRootEnv), spec)
		return
	}
	t.Run("model_returned_before_completion", func(t *testing.T) {
		root := t.TempDir()
		runFinishChild(t, root, "pre-finish")

		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Empty(t, countType(refs, tagentevent.TypeInboxReceipt), "no receipt before completion")
		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1)
		require.NotContains(t, envs[0].Raw, `"completion"`, "the crashed turn wrote NO completion")

		batch, err := bus.Pull(context.Background())
		require.NoError(t, err)
		require.Len(t, batch, 1, "the pending item is claimable again — never silently lost")
		require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
		for _, e := range batch {
			require.True(t, ta.contextManager.persistBusEvent(e))
		}
		ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
		require.Equal(t, int64(0), bus.DurablePending())
		require.Empty(t, finishEnvelopes(t, root), "acked and unlinked — processed-cleaned")
	})

	t.Run("completion_durable_no_reexecution", func(t *testing.T) {
		root := t.TempDir()
		runFinishChild(t, root, "post-completion")

		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1)
		require.Contains(t, envs[0].Raw, `"completion"`, "the frozen completion outlived the crash")

		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 1, s.ReceiptsAdded, "exactly one re-submitted receipt")
		require.Equal(t, int64(0), bus.DurablePending())

		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 1, countType(refs, tagentevent.TypeExternalInput), "input fact NOT double-written")
		require.Equal(t, 1, countType(refs, tagentevent.TypeInboxReceipt), "exactly one receipt on the chain")
		require.Empty(t, finishEnvelopes(t, root), "envelope cleaned")
	})

	t.Run("receipted_before_ack_barrier", func(t *testing.T) {
		root := t.TempDir()
		runFinishChild(t, root, "post-receipted")

		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1)
		require.Contains(t, envs[0].Raw, `"state":"receipted"`, "the receipted rewrite landed")

		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, 0, s.ReceiptsAdded, "receipt already durable — recovery adds nothing")
		require.Equal(t, int64(0), bus.DurablePending(), "the owed ack finished — barrier + exactly-once release")
		require.Empty(t, finishEnvelopes(t, root))
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 1, countType(refs, tagentevent.TypeInboxReceipt), "still exactly one receipt — no duplicate")
	})

	t.Run("cancel_never_fakes_terminal", func(t *testing.T) {
		root := t.TempDir()
		store, bus, ta := reopenFinishParent(t, root)
		defer store.Close()
		_, err := bus.PublishContext(context.Background(), durableMsg("cancel-slot-Y"))
		require.NoError(t, err)
		batch, err := bus.Pull(context.Background())
		require.NoError(t, err)
		require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
		for _, e := range batch {
			require.True(t, ta.contextManager.persistBusEvent(e))
		}
		ta.finishDurableBatch(context.Background(), batch, batch, turnOutcome{status: turnCancelled})

		envs := finishEnvelopes(t, root)
		require.Len(t, envs, 1, "the cancelled turn keeps its claim (never cleaned silently)")
		require.NotContains(t, envs[0].Raw, `"completion"`, "a cancelled turn NEVER freezes a completion")
		require.NotContains(t, envs[0].Raw, `"state":"receipted"`, "nor a receipt state")
		refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
		require.NoError(t, err)
		require.Equal(t, 0, countType(refs, tagentevent.TypeInboxReceipt), "no receipt fact for a cancelled turn")
		require.Positive(t, bus.DurablePending(), "the item stays outstanding for honest replay")
	})
}
func childFatal(msg string) {
	fmt.Fprintln(os.Stderr, "CHILD FATAL:", msg)
	os.Exit(1)
}

func childExit(msg string) {
	fmt.Println(msg)
	os.Stdout.Sync()
	os.Exit(0)
}

func runFinishChild(t *testing.T, root, spec string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashMatrix_FinishSideWindows$")
	cmd.Env = append(os.Environ(), finishCrashEnv+"="+spec, finishRootEnv+"="+root)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child must hit crash point %s:\n%s", spec, out)
	require.Contains(t, string(out), "child died")
}

func countType(refs []memory.EventReference, typ string) int {
	n := 0
	for _, r := range refs {
		if r.EventType == typ {
			n++
		}
	}
	return n
}

const (
	commitCrashEnv = "TAGENT_INPUT_CRASH_CHILD"
	commitRootEnv  = "TAGENT_INPUT_CRASH_DIR"
)

// committedInputs committedInputKeys counts external_input facts on the chain.
func committedInputs(t *testing.T, s memory.MemoryStore) []memory.EventReference {
	refs, err := s.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 100})
	require.NoError(t, err)
	var out []memory.EventReference
	for _, r := range refs {
		if r.EventType == tagentevent.TypeExternalInput {
			out = append(out, r)
		}
	}
	return out
}

func childInputCommitCrash(root string) {
	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	mustX(err)
	defer func() { _ = bus }()
	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	mustX(err)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-commit", persistentBus: bus, contextManager: cm}
	_, err = bus.PublishContext(context.Background(), durableMsg("commit-slot-A"))
	mustX(err)
	_, err = bus.PublishContext(context.Background(), durableMsg("commit-slot-B"))
	mustX(err)
	batch, err := bus.Pull(context.Background())
	mustX(err)
	if len(batch) != 2 {
		panic(fmt.Sprintf("child: expected one 2-slot batch, got %d", len(batch)))
	}
	if st, _ := ta.prepareBatchFacts(batch); st != submitOK {
		panic("child: prepare must succeed before any commit")
	}
	if !cm.persistBusEvent(batch[0]) {
		panic("child: first commit must succeed")
	}
	fmt.Println("child committed slot A, dying before slot B")
	os.Stdout.Sync()
	os.Exit(0)
}

func TestCrashWindow_InputCommit(t *testing.T) {
	if os.Getenv(commitCrashEnv) == "1" {
		childInputCommitCrash(os.Getenv(commitRootEnv))
		return
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashWindow_InputCommit$")
	cmd.Env = append(os.Environ(), commitCrashEnv+"=1", commitRootEnv+"="+root)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child must reach its crash point:\n%s", out)
	require.Contains(t, string(out), "dying before slot B")

	kvStore, err := kv.NewLocalFileKV(filepath.Join(root, "store"))
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, filepath.Join(root, "store"), 100)
	require.NoError(t, err)
	defer store.Close()

	inputs := committedInputs(t, store)
	require.Len(t, inputs, 1, "exactly ONE input committed before the crash (count reconciliation)")
	landed, gerr := store.GetEvent(inputs[0].EventKey)
	require.NoError(t, gerr)
	require.Contains(t, landed.Content, "commit-slot-A")

	bus, err := NewReliableEventBus(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "crash-commit", persistentBus: bus, contextManager: cm}

	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 2, "the crashed batch replays whole — the uncommitted slot B is still claimable")
	var sawB bool
	for _, e := range batch {
		if e.Message != nil && e.Message.Content == "commit-slot-B" {
			sawB = true
		}
	}
	require.True(t, sawB, "slot B's ORIGINAL must remain lossless in the inbox (no silent loss)")

	require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
	for _, e := range batch {
		require.True(t, cm.persistBusEvent(e), "replay commits must succeed on the frozen keys")
	}
	inputs = committedInputs(t, store)
	require.Len(t, inputs, 2, "A must NOT double-write across the crash replay; B adds exactly one")

	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending(), "the replayed turn acks only after receipt is durable")
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 100})
	require.NoError(t, err)
	var receipts int
	for _, r := range refs {
		if r.EventType == tagentevent.TypeInboxReceipt {
			receipts++
		}
	}
	require.Equal(t, 2, receipts, "one receipt per accepted envelope after the crash replay — no duplicate, none lost")

	envs, err := openEnvelopesForTest(filepath.Join(root, "inbox"))
	require.NoError(t, err)
	require.Empty(t, envs, "processed-cleaned: the acked envelope left the inbox")
}

// openEnvelopesForTest reads the inbox dir through a fresh leaf (the test must
// not reuse the live instance to claim "nothing outstanding").
func openEnvelopesForTest(dir string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "inbox-v2", "*.json"))
	if err != nil {
		return nil, err
	}
	return entries, nil
}

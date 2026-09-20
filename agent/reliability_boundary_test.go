package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

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

// ============================================================================
// F5 (D1) durable variant: inbox-v2 claims go through prepare → persistBusEvent
// ============================================================================

// TestRunEventLoop_DurableBatch_ABStoredCDeferred (1.3): durable A+B are
// accepted into the v2 inbox, claimed (typed claim), frozen by the write-before
// prepare barrier and persisted via persistBusEvent; C arriving mid-execution
// belongs to the next Pull and does NOT appear in the current turn's model
// request.
func TestRunEventLoop_DurableBatch_ABStoredCDeferred(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus, err := NewReliableEventBus(t.TempDir()) // real durable inbox (v2)
	require.NoError(t, err)
	outputCh := make(chan *event.Event, 20)
	ta := newTestTagentAgent("durable-batch", captureModel, nil, outputCh, bus)
	// One contextManager for persist, RunFlow, plugin, and assertions alike (matches
	// production's runEventLoop(ctx, persistentBus, ta.contextManager)); asserting on a
	// separate cm's store would only ever observe the pipeline's own writes, not the
	// per-message facts persistBusEvent commits (the §4.4 echo must NOT double-write).
	cm := ta.contextManager

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, cm)

	// Durable A+B arrive before the turn starts (each becomes its own envelope).
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "durable-A"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "durable-B"}))
	time.Sleep(80 * time.Millisecond)

	// Wait for first model call.
	deadline := time.After(3 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first model call")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// C arrives after the first model call starts.
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "durable-C-late"}))
	time.Sleep(50 * time.Millisecond)

	// First model request must NOT contain C.
	reqs := captureModel.snapshotRequests()
	require.NotEmpty(t, reqs)
	var firstContent string
	for _, m := range reqs[0].Messages {
		firstContent += "\n" + m.Content
	}
	require.NotContains(t, firstContent, "durable-C-late",
		"F5 durable: C arriving mid-turn must NOT appear in the current model request")

	// A and B facts must be in memStore (prepared + persisted by the durable path).
	refs, err := cm.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{cm.partitionID}, Limit: 100})
	require.NoError(t, err)
	storedContent := ""
	for _, ref := range refs {
		evt, gerr := cm.memStore.GetEvent(ref.EventKey)
		if gerr == nil {
			storedContent += "\n" + evt.Content
			// §4.4: no single fact may merge A+B (that is the double-write the precise
			// root/user/merged-content echo skip exists to prevent — only the two
			// per-message facts persistBusEvent committed should exist).
			hasA := strings.Contains(evt.Content, "durable-A")
			hasB := strings.Contains(evt.Content, "durable-B")
			require.False(t, hasA && hasB,
				"§4.4: merged echo must NOT be double-written as one fact, got %q", evt.Content)
		}
	}
	require.Contains(t, storedContent, "durable-A", "F5 durable: A must be stored via persistBusEvent")
	require.Contains(t, storedContent, "durable-B", "F5 durable: B must be stored via persistBusEvent")

	// §5.3: let C's deferred turn fully converge (prepare → commit → model → completion
	// → fixed-key receipt → ack) before the test returns. A deferred-but-unacked claim is
	// LEGITIMATELY retained on cancel, so without this the loop goroutine's in-flight
	// finishDurableBatch file writes race t.TempDir()'s RemoveAll at cleanup.
	require.Eventually(t, func() bool { return bus.DurablePending() == 0 }, 3*time.Second, 20*time.Millisecond,
		"all three durable envelopes (A,B first turn; C second turn) must receipt+ack, emptying the inbox")
}

// ============================================================================
// F5 (D1): fixed batch boundary — BeforeModel SHALL NOT claim mid-turn events
// ============================================================================
// TestBeforeModel_DoesNotClaimMidTurnEvents verifies that after the F5 fix,
// events arriving while a turn is executing are NOT pulled by BeforeModel
// (assembleRequest) and do NOT appear in the current turn's model requests.
func TestBeforeModel_DoesNotClaimMidTurnEvents(t *testing.T) {
	bus := NewEventBus()
	cm := newTestContextManager("f5-test", &loopMockModel{}, nil, nil, bus)
	seedProjectionWithFact(cm, "initial-fact")

	// Publish C while the turn is executing (before assembleRequest is called).
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "C-mid-turn"}))
	time.Sleep(10 * time.Millisecond)

	req := &model.BeforeModelArgs{
		Request: &model.Request{Messages: []model.Message{
			{Role: model.RoleSystem, Content: "sys"},
			{Role: model.RoleUser, Content: "A+B"},
		}},
	}
	// F5 fix: assembleRequest must NOT drain C from the bus.
	cm.assembleRequest(context.Background(), req)

	for _, m := range req.Request.Messages {
		require.NotContains(t, m.Content, "C-mid-turn",
			"F5/D1: BeforeModel must NOT claim mid-turn bus events")
	}

	// Verify C is still available for the next Pull (not consumed by assembleRequest).
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

// TestRunEventLoop_ABOneTurnCNextTurn is the user-confirmed batch semantics:
// A+B merged as one input in the current turn; C arriving during execution
// belongs to the NEXT Pull/turn, not appended to the current request.
func TestRunEventLoop_ABOneTurnCNextTurn(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus := NewEventBus()
	outputCh := make(chan *event.Event, 20)
	cm := newTestContextManager("ab-batch", captureModel, nil, outputCh, bus)
	ta := newTestTagentAgent("ab-batch", captureModel, nil, outputCh, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Publish A+B to disk BEFORE starting the loop so the FIRST Pull deterministically
	// claims both into one fixed batch. The prior loop-first-then-publish ordering raced
	// the two Publishes against Pull under -race (occasionally claiming only A), which
	// was a test-timing flake, not a product defect. Publish is a synchronous durable
	// enqueue, so both envelopes are pending the moment the loop's first Pull runs.
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg-A"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg-B"}))
	go ta.runEventLoop(ctx, bus, cm)
	time.Sleep(50 * time.Millisecond)

	// Wait for the first model call.
	deadline := time.After(3 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first model call")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// C arrives AFTER the first model call has started.
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg-C-late"}))
	time.Sleep(50 * time.Millisecond)

	// The FIRST model request must contain A+B but NOT C.
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

// ============================================================================
// F9 (D6): recovery notice in request tail, not in fact chain or projection
// ============================================================================

// driveRecoveryGate runs the §4.5C execution gate over a request and returns the exact
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

// TestExecutionGate_AppendsRecoveryNoticeAtTail verifies §4.5C preserved the F9 fix: the
// recovery notice is appended to the actual model request tail (user-role), never
// persisted, and consumed exactly once.
func TestExecutionGate_AppendsRecoveryNoticeAtTail(t *testing.T) {
	cm := newTestContextManager("f9-test", &loopMockModel{}, nil, nil, nil)
	cm.memStore = memory.NewInMemoryStore()

	// Simulate partial-recovery scenario.
	cm.recoveryMu.Lock()
	cm.recoveryNotice = "[recovery] partial restore, 2 keys missing"
	cm.recoveryMu.Unlock()

	got := driveRecoveryGate(t, cm, &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "hello"},
	}})

	// 1. The notice must appear as the last, user-role message.
	require.NotEmpty(t, got.Messages)
	last := got.Messages[len(got.Messages)-1]
	require.Equal(t, model.RoleUser, last.Role, "F9: recovery notice must be user-role, not system/assistant")
	require.Equal(t, "[recovery] partial restore, 2 keys missing", last.Content,
		"F9/D6: partial recovery notice must appear at the model request tail")

	// 2. The notice must NOT be stored in the fact chain (the gate never persists).
	refs, err := cm.memStore.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName("f9-test")}, Limit: 100})
	require.NoError(t, err)
	require.Empty(t, refs, "F9: recovery notice (and the gate) must NOT write to the fact chain")

	// 3. Consumed once — the second model call carries no notice.
	got2 := driveRecoveryGate(t, cm, &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "second call"},
	}})
	for _, m := range got2.Messages {
		require.NotContains(t, m.Content, "[recovery]",
			"F9: notice must be consumed exactly once per cold start")
	}
}

// TestExecutionGate_FullRecovery_NoNotice verifies healthy recovery adds no notice
// overhead: with an empty recoveryNotice the gate passes the request through unchanged.
func TestExecutionGate_FullRecovery_NoNotice(t *testing.T) {
	cm := newTestContextManager("full-recovery", &loopMockModel{}, nil, nil, nil)
	// recoveryNotice is empty (full recovery) → no injection.
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

// TestRecoveryNotice_DiagnosticsStillReadable (5.2): after TakeRecoveryNotice
// consumes the one-shot model notice, RecoveryResult() must still return the
// structured rebuild outcome so that host diagnostics are not degraded.
func TestRecoveryNotice_DiagnosticsStillReadable(t *testing.T) {
	cm := newTestContextManager("f9-diag", &loopMockModel{}, nil, nil, nil)
	cm.memStore = memory.NewInMemoryStore()

	// Set both diagnostics state and one-shot notice.
	cm.recoveryMu.Lock()
	cm.recovery = &RecoveryResult{Status: "partial", Mode: "snapshot", Projected: 5, Truncated: 2}
	cm.recoveryNotice = "[recovery] partial restore"
	cm.recoveryMu.Unlock()

	// The gate consumes the notice at the actual model call.
	driveRecoveryGate(t, cm, &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: "sys"},
		{Role: model.RoleUser, Content: "hello"},
	}})

	// After consumption: notice cleared (verified by the field directly).
	cm.recoveryMu.Lock()
	noticeCleared := cm.recoveryNotice == ""
	cm.recoveryMu.Unlock()
	require.True(t, noticeCleared, "5.2: recoveryNotice must be cleared after the gate (TakeRecoveryNotice)")

	// But diagnostics (RecoveryResult) is still readable.
	result := cm.RecoveryResult()
	require.NotNil(t, result, "5.2: RecoveryResult must remain readable after notice consumption")
	require.Equal(t, "partial", result.Status, "5.2: RecoveryResult.Status must survive")
	require.Equal(t, 2, result.Truncated, "5.2: RecoveryResult.Truncated must survive")
}

// TestRecoveryNotice_SystemPromptUnchanged (5.2): the D6 notice is appended as
// a new user-role message; it must NOT modify the system prompt or any existing
// message in-place.
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
	// The gate copies the request on injection — the caller's request stays untouched.
	require.Len(t, orig.Messages, 2, "5.2/D6: gate must not mutate the caller's request in place")
}

// TestRecoveryNotice_NotInProjection (5.3): the D6 recovery notice is injected
// only into the runtime model request; it must not enter the projection so that
// a reopen from the fact chain never replays the transient runtime notice.
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

	// The gate injects the notice into the model REQUEST only — never the projection.
	refs := cm.projection.GetAll()
	require.NotEmpty(t, refs, "5.3: projection must not be empty")
	for _, r := range refs {
		// Each projection entry should correspond to a real fact, not the notice.
		require.NotEqual(t, 0, r.EventKey, "5.3: all projection entries must have valid EventKeys")
	}
	// The real fact is still in projection; the transient notice never entered it.
	require.Equal(t, factRef.EventKey, refs[0].EventKey,
		"5.3: original fact must remain in projection")
	require.Len(t, refs, 1, "5.3: the gate must not append the notice to the projection")
}

// ============================================================================
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

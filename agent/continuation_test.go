// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

func continuationTestAgent(t *testing.T, name string, store memory.MemoryStore, m model.Model) *TagentAgent {
	t.Helper()
	if m == nil {
		m = &requestCapturingModel{resp: finalTextResponse("result", "done")}
	}
	if store == nil {
		store = memory.NewInMemoryStore()
	}
	ta, err := NewTagentAgent(&TagentConfig{Name: name, Model: m, MemoryStore: store, MemStoreBorrowed: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ta.CloseContinuationInboxes(); _ = ta.Close() })
	return ta
}

func seedContinuation(t *testing.T, ta *TagentAgent, id string, address ContinuationContext) *continuation {
	t.Helper()
	goal := NewExternalInputEvent("user", model.NewUserMessage("original irreversible action"))
	goal.Metadata[metaKeyInvocationID] = id
	c := &continuation{state: continuationCheckpoint{Version: 1, InvocationID: id, Address: address, InitialGoal: goal, InitialPending: true, State: "running"}, protected: make(map[int64]struct{}), listeners: make(map[chan *event.Event]struct{})}
	require.NoError(t, ta.persistContinuationLocked(c))
	ta.continuationRegistry().byID[id] = c
	return c
}

func continuationStoreFact(t *testing.T, ta *TagentAgent, id string, f memory.FullEvent) memory.FullEvent {
	t.Helper()
	if f.EventKey == 0 {
		f.EventKey = memory.NewSnowflakeEventKey(ta.partitionID(), 0)
	}
	f.PartitionID = ta.partitionID()
	if f.Timestamp == 0 {
		f.Timestamp = time.Now().UnixMilli()
	}
	if f.Metadata == nil {
		f.Metadata = make(map[string]string)
	}
	f.Metadata[tagentevent.MetaKeyAgentName] = ta.name
	if id != "" {
		f.Metadata[tagentevent.MetaKeyContinuationID] = id
	}
	require.NoError(t, ta.memStore.StoreEvent(f.EventKey, f))
	return f
}

func continuationDiskStore(t *testing.T) *memory.FileSegmentStore {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	k, err := kv.NewLocalFileKV(dir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(k, nil, dir, 100)
	require.NoError(t, err)
	store.SetRetentionLease(memory.NewRetentionLease())
	t.Cleanup(func() { _ = k.Close() })
	return store
}

func TestContinuationCommitsOneLogicalOutcomeAfterBackgroundCompletion(t *testing.T) {
	count := 0
	sequence := &sequenceMockModel{callCount: &count, responses: []*model.Response{
		toolCallResponse("spawn", "work"), finalTextResponse("initial", "started"), finalTextResponse("result", "finished"),
	}}
	spawned := make(chan *task.ManualDetector, 1)
	ta, err := NewTagentAgent(&TagentConfig{Name: "continuation-outcome", Model: sequence, Tools: []trpctool.Tool{&bgProbeTool{spawned: spawned}}})
	require.NoError(t, err)
	defer ta.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("start")))
	ch, err := ta.Run(ctx, inv)
	require.NoError(t, err)
	det := <-spawned
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "producer result"})
	det.Done()
	logical := 0
	var outcome, result string
	for evt := range ch {
		if value, ok := evt.StateDelta["delegation_outcome"]; ok {
			logical++
			outcome, result = string(value), string(evt.StateDelta["delegation_result"])
			require.True(t, evt.Response.Done)
			require.Nil(t, evt.Response.Choices)
		}
	}
	if logical != 1 {
		t.Logf("routing=%+v", ta.settleSinks.snapshotRouting(inv.InvocationID))
		for _, tk := range ta.taskManager.TasksSnapshot() {
			t.Logf("task=%s round=%d producer=%d status=%s", tk.ID, tk.RoundID(), tk.ProducerRound(), tk.Status())
		}
	}
	require.Equal(t, 1, logical)
	require.Equal(t, "completed", outcome)
	require.Equal(t, "finished", result)
}

func TestContinuationPrivateInboxDefaultsToActualDiskStore(t *testing.T) {
	store := continuationDiskStore(t)
	ta := continuationTestAgent(t, "disk-private", store, nil)
	require.False(t, ta.persistentBus.Durable())
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("hello")))
	ch, err := ta.Run(context.Background(), inv)
	require.NoError(t, err)
	for range ch {
	}
	c := ta.continuationRegistry().byID[inv.InvocationID]
	require.True(t, c.bus.Durable())
	require.Contains(t, c.bus.inbox.Dir(), store.GetStats().DataDir)
	require.Zero(t, c.bus.DurablePending())
	require.Empty(t, ta.projection.GetAll())
	for _, fake := range []string{"", ":memory:", "memory://owner", "file:///tmp/store"} {
		t.Run(fmt.Sprintf("exclude_%q", fake), func(t *testing.T) {
			mock := &continuationStatsStore{MemoryStore: memory.NewInMemoryStore(), dir: fake}
			probe := &TagentAgent{name: "probe", memStore: mock}
			require.Empty(t, probe.continuationInboxDir("inv"))
		})
	}
}

type continuationStatsStore struct {
	memory.MemoryStore
	dir string
}

func (s *continuationStatsStore) GetStats() memory.StoreStats {
	return memory.StoreStats{DataDir: s.dir}
}

func TestContinuationCheckpointRequiresExactConsumptionReceipt(t *testing.T) {
	ta := continuationTestAgent(t, "checkpoint-proof", nil, nil)
	c := seedContinuation(t, ta, "inv", ContinuationContext{})
	evt := c.state.InitialGoal
	fact := continuationStoreFact(t, ta, "inv", memory.FullEvent{EventType: tagentevent.TypeExternalInput, Content: evt.Message.Content})
	evt.volatileFact, evt.volatileCommitted = &fact, true
	cm := &ContextManager{name: ta.name, memStore: ta.memStore, partitionID: ta.partitionID(), continuationID: "inv", projection: compress.NewSessionProjection()}
	require.NoError(t, ta.checkpointContinuation(context.Background(), cm, []*AgentEvent{evt}))
	require.True(t, c.state.InitialPending)
	require.Empty(t, c.state.ConsumedKeys)
	wrong := continuationStoreFact(t, ta, "inv", memory.FullEvent{EventType: tagentevent.TypeInputDisposition, Metadata: map[string]string{tagentevent.MetaKeyConsumedEventKey: tagentevent.FormatEventKey(fact.EventKey + 1), tagentevent.MetaKeyInputDisposition: "processed"}})
	evt.observationReceiptKey = tagentevent.FormatEventKey(wrong.EventKey)
	require.NoError(t, ta.checkpointContinuation(context.Background(), cm, []*AgentEvent{evt}))
	require.True(t, c.state.InitialPending)
	proof := continuationStoreFact(t, ta, "inv", memory.FullEvent{EventType: tagentevent.TypeInputDisposition, Metadata: map[string]string{tagentevent.MetaKeyConsumedEventKey: tagentevent.FormatEventKey(fact.EventKey), tagentevent.MetaKeyInputDisposition: "processed"}})
	evt.observationReceiptKey = tagentevent.FormatEventKey(proof.EventKey)
	require.NoError(t, ta.checkpointContinuation(context.Background(), cm, []*AgentEvent{evt}))
	require.False(t, c.state.InitialPending)
	require.Equal(t, []int64{fact.EventKey}, c.state.ConsumedKeys)
}

func TestContinuationRecoveryFailsUnknownActionAndClosesToolProtocol(t *testing.T) {
	m := &requestCapturingModel{resp: finalTextResponse("unused", "must not run")}
	ta := continuationTestAgent(t, "unknown-action", nil, m)
	c := seedContinuation(t, ta, "inv", ContinuationContext{ParentAgent: "parent", ParentTaskID: "task", ParentTaskRound: 7})
	continuationStoreFact(t, ta, "inv", memory.FullEvent{EventType: tagentevent.TypeThinkingPlan, ToolCalls: toolCallResponse("side-effect", "do-it").Choices[0].Message.ToolCalls})
	ta.continuations = newContinuationRegistry()
	n, err := ta.RebuildContinuationsFromWAL()
	require.NoError(t, err)
	require.Equal(t, 1, n)
	recovered := ta.continuationRegistry().byID["inv"]
	require.False(t, recovered.state.InitialPending)
	require.Equal(t, "failed", recovered.state.Outcome)
	var paired bool
	for _, ref := range recovered.state.Projection {
		f, err := ta.memStore.GetEvent(ref.EventKey)
		require.NoError(t, err)
		if f.ToolID == "side-effect" {
			paired = true
			require.Contains(t, f.Content, "interrupted")
		}
	}
	require.True(t, paired)
	ch, found, err := ta.ResumeContinuation(context.Background(), "task", 7)
	require.NoError(t, err)
	require.True(t, found)
	var frames int
	for evt := range ch {
		frames++
		require.Equal(t, "failed", string(evt.StateDelta["delegation_outcome"]))
	}
	require.Equal(t, 1, frames)
	require.Zero(t, m.requestCount())
	require.Empty(t, ta.projection.GetAll())
	require.Equal(t, c.state.InitialGoal.ID, recovered.state.InitialGoal.ID)
}

func TestContinuationRecoveryReplaysExactCommittedResultWithoutOriginalGoal(t *testing.T) {
	m := &requestCapturingModel{resp: finalTextResponse("finish", "reconciled")}
	ta := continuationTestAgent(t, "raw-result-recovery", nil, m)
	seedContinuation(t, ta, "inv", ContinuationContext{ParentAgent: "parent", ParentTaskID: "parent-task", ParentTaskRound: 4})
	continuationStoreFact(t, ta, "inv", memory.FullEvent{EventType: tagentevent.TypeThinkingPlan, ToolCalls: toolCallResponse("side-effect", "do-it").Choices[0].Message.ToolCalls})
	evt := NewExternalInputEvent(SourceTask, model.NewUserMessage("EXACT original producer result"))
	evt.ID = task.SignalSourceID("child-task", 3, 1)
	evt.Metadata[metaKeyInvocationID] = "inv"
	evt.Metadata[tagentevent.MetaKeyTaskRoundOwner] = "inv"
	evt.Metadata["task_id"] = "child-task"
	evt.Metadata[tagentevent.MetaKeyTaskRoundID] = "3"
	evt.Metadata[tagentevent.MetaKeyTaskSignalSeq] = "1"
	evt.Metadata[tagentevent.MetaKeySettleFinal] = "true"
	evt.Metadata["settle_status"] = "completed"
	raw, _ := json.Marshal(taskSignalRecord{TaskID: "child-task", Kind: task.SettleCompleted, RoundID: 3, RoundOwner: "inv", Seq: 1, Final: true, Event: evt})
	continuationStoreFact(t, ta, "inv", memory.FullEvent{EventType: tagentevent.TypeTaskSignal, Content: string(raw)})
	ta.continuations = newContinuationRegistry()
	_, err := ta.RebuildContinuationsFromWAL()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, found, err := ta.ResumeContinuationByID(ctx, "inv", "parent-task", 4)
	require.NoError(t, err)
	require.True(t, found)
	var outcome string
	for evt := range ch {
		if value := evt.StateDelta["delegation_outcome"]; len(value) > 0 {
			outcome = string(value)
		}
	}
	require.Equal(t, "completed", outcome)
	require.Equal(t, 1, m.requestCount())
	reqs := m.snapshotRequests()
	require.NotEmpty(t, reqs)
	last := reqs[0].Messages[len(reqs[0].Messages)-1]
	require.Contains(t, last.Content, "EXACT original producer result")
	require.NotContains(t, last.Content, "original irreversible action")
	require.Empty(t, ta.projection.GetAll())
}

func TestContinuationRecoversDurableCompletionWithoutModelReplay(t *testing.T) {
	store := continuationDiskStore(t)
	m := &requestCapturingModel{resp: finalTextResponse("unused", "must not run")}
	ta := continuationTestAgent(t, "completion-recovery", store, m)
	c := seedContinuation(t, ta, "inv", ContinuationContext{ParentAgent: "parent", ParentTaskID: "task", ParentTaskRound: 2})
	require.NoError(t, ta.openContinuationBus(c))
	_, err := c.bus.PublishContext(context.Background(), c.state.InitialGoal)
	require.NoError(t, err)
	batch := c.bus.TryPull()
	require.Len(t, batch, 1)
	cm := &ContextManager{name: ta.name, memStore: store, bus: c.bus, partitionID: ta.partitionID(), continuationID: "inv", projection: compress.NewSessionProjection()}
	prepareTaskReceiptKeys(cm, batch)
	require.Equal(t, submitOK, ta.submitDurableBatchForCM(context.Background(), cm, batch, batch).status)
	require.True(t, consumeTaskInputs(context.Background(), cm, batch, "processed"))
	continuationStoreFact(t, ta, "inv", memory.FullEvent{EventType: tagentevent.TypeAgentOutput, Content: "already finished"})
	frozen, encoded, err := buildEnvelopeCompletion(batch, selectedKeySet(batch), completedOutcome(), ta.name, ta.partitionID(), time.Now().UnixMilli(), cm.buildTurnAttribution(context.Background()))
	require.NoError(t, err)
	require.NoError(t, c.bus.RecordCompletion(batch[0].claim.Path, encoded))
	receiptKey, err := tagentevent.ParseEventKey(frozen.ReceiptKey)
	require.NoError(t, err)
	_, err = store.GetEvent(receiptKey)
	require.Error(t, err)
	require.NoError(t, c.bus.CloseDurable())
	ta.continuations = newContinuationRegistry()
	_, err = ta.RebuildContinuationsFromWAL()
	require.NoError(t, err)
	recovered := ta.continuationRegistry().byID["inv"]
	require.Zero(t, recovered.bus.DurablePending())
	receipt, err := store.GetEvent(receiptKey)
	require.NoError(t, err)
	require.Equal(t, tagentevent.TypeInboxReceipt, receipt.EventType)
	ch, found, err := ta.ResumeContinuation(context.Background(), "task", 2)
	require.NoError(t, err)
	require.True(t, found)
	for evt := range ch {
		require.Equal(t, "already finished", string(evt.StateDelta["delegation_result"]))
	}
	require.Zero(t, m.requestCount())
}

func TestContinuationWaitingHoldsDeclarationsWithoutCalculationLease(t *testing.T) {
	count := 0
	m := &sequenceMockModel{callCount: &count, responses: []*model.Response{toolCallResponse("spawn", "work"), finalTextResponse("initial", "waiting"), finalTextResponse("finish", "finished")}}
	spawned := make(chan *task.ManualDetector, 1)
	ta, err := NewTagentAgent(&TagentConfig{Name: "waiting-lease", Model: m, Tools: []trpctool.Tool{&bgProbeTool{spawned: spawned}}})
	require.NoError(t, err)
	defer ta.Close()
	source := ta.contextManager.activeBinding()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("start")))
	ch, err := ta.Run(ctx, inv)
	require.NoError(t, err)
	det := <-spawned
	for evt := range ch {
		if evt.Response != nil && len(evt.Response.Choices) > 0 && evt.Response.Choices[0].Message.Content == "waiting" {
			break
		}
	}
	require.Eventually(t, func() bool { cms := ta.snapshotLiveCMs(); return len(cms) == 1 && cms[0].OutstandingRefs() == 0 }, time.Second, time.Millisecond)
	require.Zero(t, ta.contextManager.OutstandingRefs())
	source.mu.Lock()
	held := source.heldBy
	source.mu.Unlock()
	require.Positive(t, held)
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "done"})
	det.Done()
	for range ch {
	}
	require.Zero(t, ta.LiveCMCount())
}

func TestContinuationResultRetentionNeedsExactParentReceipt(t *testing.T) {
	store := continuationDiskStore(t)
	parent := continuationTestAgent(t, "parent-proof", store, nil)
	child := continuationTestAgent(t, "child-proof", store, nil)
	child.resident = NewResidentTopology(map[string]*TagentAgent{parent.name: parent, child.name: child})
	address := ContinuationContext{ParentAgent: parent.name, ParentInvocationID: "parent-inv", ParentTaskID: "task", ParentTaskRound: 8}
	c := seedContinuation(t, child, "child-inv", address)
	output := continuationStoreFact(t, child, "child-inv", memory.FullEvent{EventType: tagentevent.TypeAgentOutput, Content: "valuable original"})
	c.state.Projection = []memory.EventReference{continuationProjectionRef(output)}
	c.state.State, c.state.Outcome, c.state.InitialPending = "completed", "completed", false
	require.NoError(t, child.persistContinuationLocked(c))
	require.ErrorIs(t, store.DeleteEvent(output.EventKey), memory.ErrEventProtected)
	rawKey := memory.NewSnowflakeEventKey(parent.partitionID(), 0)
	receiptKey := memory.NewSnowflakeEventKey(parent.partitionID(), 0)
	evt := NewExternalInputEvent(SourceTask, model.NewUserMessage("completed child"))
	evt.ID = task.SignalSourceID("task", 8, 1)
	record := taskSignalRecord{TaskID: "task", Kind: task.SettleCompleted, RoundID: 8, RoundOwner: "parent-inv", Seq: 1, Final: true, ReceiptKey: receiptKey, Event: evt, ChildInvocationID: "child-inv"}
	raw, _ := json.Marshal(record)
	continuationStoreFact(t, parent, "", memory.FullEvent{EventKey: rawKey, EventType: tagentevent.TypeTaskSignal, Content: string(raw)})
	proof := continuationStoreFact(t, parent, "", memory.FullEvent{EventKey: receiptKey, EventType: tagentevent.TypeTaskSignalReceipt, Content: tagentevent.FormatEventKey(rawKey), Metadata: map[string]string{"task_id": "task", tagentevent.MetaKeyTaskRoundID: "8", tagentevent.MetaKeyTaskRoundOwner: "parent-inv", childInvocationIDParam: "child-inv", tagentevent.MetaKeySourceEventID: evt.ID, "signal_kind": "completed"}})
	wrong := proof
	wrong.Metadata = map[string]string{"task_id": "another-task", tagentevent.MetaKeyTaskRoundID: "8", childInvocationIDParam: "child-inv", tagentevent.MetaKeyAgentName: parent.name}
	require.Error(t, child.AcknowledgeContinuationResultWithReceipt(context.Background(), "child-inv", wrong))
	require.False(t, c.state.ResultAcked)
	require.ErrorIs(t, store.DeleteEvent(output.EventKey), memory.ErrEventProtected)
	require.NoError(t, child.AcknowledgeContinuationResultWithReceipt(context.Background(), "child-inv", proof))
	require.True(t, c.state.ResultAcked)
	require.NoError(t, store.DeleteEvent(output.EventKey))
}

func TestContinuationTransportCancellationSuspendsAndResumesOriginalRound(t *testing.T) {
	m := &recordingSequenceModel{responses: []*model.Response{toolCallResponse("spawn", "work"), finalTextResponse("initial", "waiting"), finalTextResponse("finish", "finished once")}}
	spawned := make(chan *task.ManualDetector, 1)
	ta, err := NewTagentAgent(&TagentConfig{Name: "transport-resume", Model: m, Tools: []trpctool.Tool{&bgProbeTool{spawned: spawned}}})
	require.NoError(t, err)
	defer ta.Close()
	ctx, cancel := context.WithCancel(context.Background())
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("start")))
	inv.RunOptions.RuntimeState = map[string]any{ContinuationContextKey: ContinuationContext{ParentAgent: "parent", ParentTaskID: "parent-task", ParentTaskRound: 9}}
	ch, err := ta.Run(ctx, inv)
	require.NoError(t, err)
	det := <-spawned
	for evt := range ch {
		if evt.Response != nil && len(evt.Response.Choices) > 0 && evt.Response.Choices[0].Message.Content == "waiting" {
			break
		}
	}
	require.Eventually(t, func() bool { cms := ta.snapshotLiveCMs(); return len(cms) == 1 && cms[0].OutstandingRefs() == 0 }, time.Second, time.Millisecond)
	cancel()
	for evt := range ch {
		require.Empty(t, evt.StateDelta["delegation_outcome"])
	}
	c := ta.continuationRegistry().byID[inv.InvocationID]
	require.Equal(t, "suspended", c.state.State)
	require.Empty(t, c.state.Outcome)
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "original late result"})
	det.Done()
	require.Eventually(t, func() bool { return c.bus.PendingCount() > 0 }, time.Second, time.Millisecond)
	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer resumeCancel()
	resumed, found, err := ta.ResumeContinuationByID(resumeCtx, inv.InvocationID, "parent-task", 9)
	require.NoError(t, err)
	require.True(t, found)
	var final string
	for evt := range resumed {
		if string(evt.StateDelta["delegation_outcome"]) == "completed" {
			final = string(evt.StateDelta["delegation_result"])
		}
	}
	require.Equal(t, "finished once", final)
	request := m.lastRequest()
	last := request[len(request)-1]
	require.Equal(t, 1, strings.Count(last.Content, "original late result"))
	require.NotContains(t, last.Content, "start")
}

type continuationCycleModel struct{ calls atomic.Int64 }

func (m *continuationCycleModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	n := m.calls.Add(1)
	response := finalTextResponse(fmt.Sprint(n), "delegation completed")
	if n%2 == 1 {
		response = toolCallResponse(fmt.Sprintf("delegate-%d", n), "")
		response.Choices[0].Message.ToolCalls[0].Function = model.FunctionDefinitionParam{Name: "cycle-child", Arguments: []byte(`{"request":"one invocation"}`)}
	}
	ch := make(chan *model.Response, 1)
	ch <- response
	close(ch)
	return ch, nil
}

func (*continuationCycleModel) Info() model.Info { return model.Info{Name: "continuation-cycle"} }

// TestContinuationHundredAcknowledgedCallsConverge exercises actual entry tools, private inboxes and parent receipts.
func TestContinuationHundredAcknowledgedCallsConverge(t *testing.T) {
	store := continuationDiskStore(t)
	owners := make(map[string]*TagentAgent)
	resolve := func(name string) *TagentAgent { return owners[name] }
	childModel := &requestCapturingModel{resp: finalTextResponse("child-final", "child result")}
	child, err := NewTagentAgent(&TagentConfig{Name: "cycle-child", Model: childModel, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[child.name] = child
	w := NewAgentToolWrapper(child, "one call", nil, store)
	w.SetAsyncDenseDuration(10 * time.Second)
	parentModel := &continuationCycleModel{}
	parent, err := NewTagentAgent(&TagentConfig{Name: "cycle-parent", Model: parentModel, Tools: []trpctool.Tool{w}, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve, MaxTokens: 1 << 20})
	require.NoError(t, err)
	owners[parent.name] = parent
	parent.SetDelegationOwnerCM(parent.contextManager)
	parent.SetToolParentProjection()
	defer child.Close()
	defer parent.Close()
	out, err := parent.StartLoop("cycle-user", "cycle-session")
	require.NoError(t, err)
	for i := 0; i < 100; i++ {
		parent.InjectMessage(model.NewUserMessage(fmt.Sprintf("delegation %d", i)))
		deadline := time.NewTimer(5 * time.Second)
		for {
			select {
			case evt := <-out:
				if !isFinalResponse(evt) {
					continue
				}
				require.Equal(t, "delegation completed", evt.Response.Choices[0].Message.Content)
			case <-deadline.C:
				t.Fatalf("delegation %d did not finish", i)
			}
			break
		}
		deadline.Stop()
	}
	converged := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		parent.taskRecords.delivery.mu.Lock()
		journalLeases := len(parent.taskRecords.delivery.leases)
		parent.taskRecords.delivery.mu.Unlock()
		if child.LiveCMCount() == 0 && parent.contextManager.ExecutorRefs().InFlightTurns == 0 && journalLeases == 0 && store.RetentionLease().Len() == 0 {
			converged = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !converged {
		parent.taskRecords.delivery.mu.Lock()
		t.Logf("liveCM=%d parentRefs=%+v journalLeases=%d guardKeys=%d childModels=%d parentModels=%d", child.LiveCMCount(), parent.contextManager.ExecutorRefs(), len(parent.taskRecords.delivery.leases), store.RetentionLease().Len(), childModel.requestCount(), parentModel.calls.Load())
		parent.taskRecords.delivery.mu.Unlock()
	}
	require.True(t, converged, "all accepted invocation obligations must converge")
	parent.StopLoop()
	require.Equal(t, int64(200), parentModel.calls.Load())
	require.Equal(t, 100, childModel.requestCount())
	require.Zero(t, child.contextManager.ExecutorRefs().InFlightTurns)
	require.Zero(t, child.contextManager.ExecutorRefs().SubCalls)
	entries := child.continuationRegistry().snapshot()
	require.Len(t, entries, 100)
	for _, c := range entries {
		c.mu.Lock()
		require.Equal(t, "completed", c.state.Outcome)
		require.True(t, c.state.ResultAcked)
		require.False(t, c.running)
		require.Nil(t, c.cm)
		require.Empty(t, c.protected)
		require.Zero(t, c.bus.DurablePending())
		require.Zero(t, c.bus.PendingCount())
		c.mu.Unlock()
	}
	child.settleSinks.mu.Lock()
	require.Empty(t, child.settleSinks.byInv)
	require.Empty(t, child.settleSinks.pending)
	require.Empty(t, child.settleSinks.inFlight)
	require.LessOrEqual(t, len(child.settleSinks.states), 256)
	child.settleSinks.mu.Unlock()
}

type continuationBlockedCheckpointStore struct {
	memory.MemoryStore
	entered chan struct{}
	release chan struct{}
	blocked atomic.Bool
}

func (s *continuationBlockedCheckpointStore) StoreEvent(key int64, fact memory.FullEvent) error {
	if fact.EventType == tagentevent.TypeContinuationState {
		var state continuationCheckpoint
		if json.Unmarshal([]byte(fact.Content), &state) == nil && state.ExecutionStarted && s.blocked.CompareAndSwap(false, true) {
			close(s.entered)
			<-s.release
		}
	}
	return s.MemoryStore.StoreEvent(key, fact)
}

// TestContinuationStopIsBoundedWhileAssemblyWALBlocks keeps cancellation independent of the actor's IO lock.
func TestContinuationStopIsBoundedWhileAssemblyWALBlocks(t *testing.T) {
	store := &continuationBlockedCheckpointStore{MemoryStore: memory.NewInMemoryStore(), entered: make(chan struct{}), release: make(chan struct{})}
	m := &requestCapturingModel{resp: finalTextResponse("unused", "must not execute")}
	ta := continuationTestAgent(t, "blocked-continuation-assembly", store, m)
	runDone := make(chan error, 1)
	go func() {
		_, err := ta.Run(context.Background(), trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("work"))))
		runDone <- err
	}()
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("assembly did not reach its WAL barrier")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, ta.stopContinuations(ctx), ErrExecUnconverged)
	close(store.release)
	require.ErrorIs(t, <-runDone, ErrExecClosed)
	require.NoError(t, ta.stopContinuations(context.Background()))
	require.Zero(t, m.requestCount())
	require.Zero(t, ta.LiveCMCount())
}

type continuationServiceTool struct{ started chan *task.ManualDetector }

func (*continuationServiceTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "action", Description: "start a service", InputSchema: &trpctool.Schema{Type: "object"}}
}

func (tool *continuationServiceTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, _ := task.TaskSpawnerFromContext(ctx)
	detector := task.NewManualDetector()
	detector.Emit(task.SettleSignal{Kind: task.SettleReady, Ready: true, Output: "ready"})
	result := spawner.Spawn(task.TaskSpec{Kind: "service", Mode: "service", Lifetime: task.LifetimeService, WaitFor: task.WaitReady, Desc: "long service", Declarative: &task.Declarative{Kind: "service", Desc: "long service"}}, detector)
	tool.started <- detector
	return result.Signal.Output, result.Signal.Err
}

func continuationDelegateResponse(id, name string) *model.Response {
	response := toolCallResponse(id, "")
	response.Choices[0].Message.ToolCalls[0].Function = model.FunctionDefinitionParam{Name: name, Arguments: []byte(`{"request":"start the service"}`)}
	return response
}

func continuationAwaitFinal(t *testing.T, out <-chan *event.Event) *event.Event {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case evt, open := <-out:
			require.True(t, open)
			if isFinalResponse(evt) {
				return evt
			}
		case <-deadline.C:
			t.Fatal("continuation final response did not arrive")
		}
	}
}

// TestContinuationNestedServiceHandoffFollowsCompletedParent retains a receiver after both private goals finish.
func TestContinuationNestedServiceHandoffFollowsCompletedParent(t *testing.T) {
	store := memory.NewInMemoryStore()
	owners := make(map[string]*TagentAgent)
	resolve := func(name string) *TagentAgent { return owners[name] }
	started := make(chan *task.ManualDetector, 1)
	childCount, parentCount, entryCount := 0, 0, 0
	child, err := NewTagentAgent(&TagentConfig{Name: "handoff-child", Model: &sequenceMockModel{callCount: &childCount, responses: []*model.Response{toolCallResponse("start-service", ""), finalTextResponse("child-ready", "child service ready")}}, Tools: []trpctool.Tool{&continuationServiceTool{started: started}}, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[child.name] = child
	childTool := NewAgentToolWrapper(child, "start service", nil, store)
	childTool.SetAsyncDenseDuration(time.Second)
	parent, err := NewTagentAgent(&TagentConfig{Name: "handoff-parent", Model: &sequenceMockModel{callCount: &parentCount, responses: []*model.Response{continuationDelegateResponse("delegate-child", child.name), finalTextResponse("parent-ready", "parent goal finished")}}, Tools: []trpctool.Tool{childTool}, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[parent.name] = parent
	parent.SetDelegationOwnerCM(parent.contextManager)
	parentTool := NewAgentToolWrapper(parent, "start service", nil, store)
	parentTool.SetAsyncDenseDuration(time.Second)
	entry, err := NewTagentAgent(&TagentConfig{Name: "handoff-entry", Model: &sequenceMockModel{callCount: &entryCount, responses: []*model.Response{continuationDelegateResponse("delegate-parent", parent.name), finalTextResponse("entry-ready", "service is running"), finalTextResponse("entry-terminal", "service has stopped")}}, Tools: []trpctool.Tool{parentTool}, MemoryStore: store, MemStoreBorrowed: true, ContinuationOwnerResolver: resolve})
	require.NoError(t, err)
	owners[entry.name] = entry
	entry.SetDelegationOwnerCM(entry.contextManager)
	defer child.Close()
	defer parent.Close()
	defer entry.Close()
	out, err := entry.StartLoop("handoff-user", "handoff-session")
	require.NoError(t, err)
	entry.InjectMessage(model.NewUserMessage("start"))
	require.Equal(t, "service is running", continuationAwaitFinal(t, out).Response.Choices[0].Message.Content)
	detector := <-started
	defer detector.Cancel()
	require.Eventually(t, func() bool { return child.LiveCMCount() == 0 && parent.LiveCMCount() == 0 }, time.Second, time.Millisecond)
	childScopes, parentScopes := child.continuationRegistry().snapshot(), parent.continuationRegistry().snapshot()
	require.Len(t, childScopes, 1)
	require.Len(t, parentScopes, 1)
	childScope, parentScope := childScopes[0], parentScopes[0]
	childScope.mu.Lock()
	require.Equal(t, "completed", childScope.state.Outcome)
	require.NotNil(t, childScope.state.Handoff)
	require.Equal(t, parent.name, childScope.state.Handoff.ParentAgent)
	childScope.mu.Unlock()
	parentScope.mu.Lock()
	require.Equal(t, "completed", parentScope.state.Outcome)
	require.NotNil(t, parentScope.state.Handoff)
	require.Equal(t, entry.name, parentScope.state.Handoff.ParentAgent)
	require.Len(t, parentScope.state.InheritedProducers, 1)
	parentScope.mu.Unlock()
	require.Zero(t, child.contextManager.ExecutorRefs().InFlightTurns)
	require.Zero(t, parent.contextManager.ExecutorRefs().InFlightTurns)
	detector.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "service terminal payload"})
	detector.Done()
	detector.FireStop()
	require.Equal(t, "service has stopped", continuationAwaitFinal(t, out).Response.Choices[0].Message.Content)
	entry.StopLoop()
	require.Equal(t, 2, childCount)
	require.Equal(t, 2, parentCount)
	require.Equal(t, 3, entryCount)
}

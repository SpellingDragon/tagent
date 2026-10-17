// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
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

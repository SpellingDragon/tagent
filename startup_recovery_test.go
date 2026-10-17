// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package tagent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type startupRecoveryModel struct {
	mu     sync.Mutex
	inputs []string
	called chan struct{}
}

func (m *startupRecoveryModel) Info() model.Info { return model.Info{Name: "startup-recovery"} }

func (m *startupRecoveryModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	var content []string
	for _, msg := range req.Messages {
		content = append(content, msg.Content)
	}
	m.mu.Lock()
	m.inputs = append(m.inputs, strings.Join(content, "\n"))
	m.mu.Unlock()
	if m.called != nil {
		select {
		case m.called <- struct{}{}:
		default:
		}
	}
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "recovered parent result"}}}}
	close(ch)
	return ch, nil
}

func (m *startupRecoveryModel) snapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.inputs...)
}

func putStartupFact(t *testing.T, store memory.MemoryStore, owner, kind string, payload any, metadata map[string]string) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata[tagentevent.MetaKeyAgentName] = owner
	pid := memory.PartitionIDFromName(owner)
	key := memory.NewSnowflakeEventKey(pid, 0)
	require.NoError(t, store.StoreEvent(key, memory.FullEvent{EventKey: key, PartitionID: pid, EventType: kind, Content: string(raw), Timestamp: time.Now().UnixMilli(), Metadata: metadata}))
}

func putStartupContinuation(t *testing.T, store memory.MemoryStore, owner, invID, goal string, address agent.ContinuationContext, outcome, output string, routing any) {
	t.Helper()
	input := agent.NewExternalInputEvent("user", model.NewUserMessage(goal))
	input.Metadata["invocation_id"] = invID
	state := map[string]any{
		"version": 1, "invocation_id": invID, "address": address,
		"initial_goal": input, "initial_pending": false, "state": "suspended",
		"outcome": outcome, "last_output": output, "routing": routing,
	}
	putStartupFact(t, store, owner, tagentevent.TypeContinuationState, state, map[string]string{tagentevent.MetaKeyContinuationID: invID})
}

func TestStartupRecoveryRestoresHiddenOriginalRoundBeforeAnyGoalRuns(t *testing.T) {
	dir := t.TempDir()
	mc := MemoryConfig{Type: "localfile", Path: filepath.Join(dir, "store")}
	store, _, release, err := resolveMemoryStore(mc)
	require.NoError(t, err)
	const parent = "startup-parent"
	const hidden = "startup-hidden"
	const taskID = "original-parent-task"
	const parentInv = "original-parent-scope"
	const childInv = "original-child-scope"
	const round = uint64(7)
	started := time.Now().Add(-time.Second)
	decl := task.Declarative{Kind: "subagent", Desc: "saved child delegation", AgentName: hidden, MessageBody: "ORIGINAL_CHILD_GOAL", RoundID: round, RoundOwner: parentInv, ProducerRound: round, ProducerOwner: parentInv, WaitFor: task.WaitExit, Lifetime: task.LifetimeJob, StartedAtMilli: started.UnixMilli(), TTLAnchorMilli: started.UnixMilli(), Origin: map[string]string{"invocation_id": parentInv}, Params: map[string]string{"child_invocation_id": childInv}}
	putStartupFact(t, store, parent, tagentevent.TypeTaskSpawned, decl, map[string]string{"task_id": taskID, "admission_phase": "started", "round_owner": parentInv, "round_id": "7"})
	putStartupContinuation(t, store, parent, parentInv, "ORIGINAL_PARENT_GOAL", agent.ContinuationContext{}, "", "", map[string]any{"waits": []map[string]any{{"task_id": taskID, "round_id": round, "wait_for": "exit"}}})
	putStartupContinuation(t, store, hidden, childInv, "ORIGINAL_CHILD_GOAL", agent.ContinuationContext{ParentAgent: parent, ParentInvocationID: parentInv, ParentTaskID: taskID, ParentTaskRound: round}, "completed", "SAVED_CHILD_RESULT", nil)
	require.NoError(t, release())

	parentModel := &startupRecoveryModel{called: make(chan struct{}, 4)}
	childModel := &startupRecoveryModel{}
	cfg := Config{Entry: parent, Reliability: ReliabilityConfig{BusSpillDir: filepath.Join(dir, "bus")}, Agents: map[string]AgentConfig{
		parent:              {SystemPrompt: PromptConfig{Inline: "parent"}, Memory: mc},
		hidden:              {SystemPrompt: PromptConfig{Inline: "hidden"}, Memory: mc},
		"unused-definition": {SystemPrompt: PromptConfig{Inline: "unused"}},
	}}
	entry, err := New(cfg, WithModel(parentModel), WithModelOverrides(map[string]model.Model{hidden: childModel}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, entry.Close()) })
	owners := entry.ResidentTable()
	require.NotNil(t, owners[hidden], "a persisted producer needs its exact owner even after that tool is hidden")
	require.Nil(t, owners["unused-definition"], "unrelated definitions remain unconstructed")
	require.Empty(t, entry.Tools(), "recovery ownership does not grant a visible delegation tool")
	require.Empty(t, parentModel.snapshot(), "construction only restores receivers and resources")
	require.Empty(t, childModel.snapshot())
	tk, exists := entry.TaskManager().Get(taskID)
	require.True(t, exists)
	require.Equal(t, round, tk.ProducerRound())
	require.False(t, tk.HasProducer(), "the original continuation attaches only when StartLoop is ready")

	_, err = entry.StartLoop("user", "session")
	require.NoError(t, err)
	select {
	case <-parentModel.called:
	case <-time.After(5 * time.Second):
		t.Fatal("the original parent never consumed the restored child result")
	}
	require.Eventually(t, func() bool { return tk.Status() == task.TaskCompleted }, time.Second, time.Millisecond)
	require.Equal(t, round, tk.RoundID(), "recovery attaches the accepted producer round instead of issuing Resume")
	require.Equal(t, round, tk.ProducerRound())
	require.Empty(t, childModel.snapshot(), "the completed child result is replayed without another child Goal")
	require.Contains(t, strings.Join(parentModel.snapshot(), "\n"), "SAVED_CHILD_RESULT")
	bindings, err := owners[hidden].RecoveredContinuationBindingsFromWAL()
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, childInv, bindings[0].InvocationID)
}

func TestStartupRecoveryBindsEveryDelegationBeforeStartingThreeLevelChain(t *testing.T) {
	dir := t.TempDir()
	mc := MemoryConfig{Type: "localfile", Path: filepath.Join(dir, "store")}
	store, _, release, err := resolveMemoryStore(mc)
	require.NoError(t, err)
	const parent, middle, leaf = "a-recovery-parent", "b-recovery-middle", "c-recovery-leaf"
	const parentInv, middleInv, leafInv = "parent-scope", "middle-scope", "leaf-scope"
	const parentTask, middleTask = "parent-delegation", "middle-delegation"
	const parentRound, middleRound = uint64(7), uint64(11)
	started := time.Now().Add(-time.Second)
	for _, saved := range []struct {
		owner, target, invocation, child, id string
		round                                uint64
	}{
		{parent, middle, parentInv, middleInv, parentTask, parentRound},
		{middle, leaf, middleInv, leafInv, middleTask, middleRound},
	} {
		decl := task.Declarative{Kind: "subagent", Desc: "original nested delegation", AgentName: saved.target, MessageBody: "ORIGINAL_" + saved.target, RoundID: saved.round, RoundOwner: saved.invocation, ProducerRound: saved.round, ProducerOwner: saved.invocation, WaitFor: task.WaitExit, Lifetime: task.LifetimeJob, StartedAtMilli: started.UnixMilli(), TTLAnchorMilli: started.UnixMilli(), Params: map[string]string{"child_invocation_id": saved.child}}
		putStartupFact(t, store, saved.owner, tagentevent.TypeTaskSpawned, decl, map[string]string{"task_id": saved.id, "admission_phase": "started"})
	}
	wait := func(id string, round uint64) any {
		return map[string]any{"waits": []map[string]any{{"task_id": id, "round_id": round, "wait_for": "exit"}}}
	}
	putStartupContinuation(t, store, parent, parentInv, "ORIGINAL_PARENT_GOAL", agent.ContinuationContext{}, "", "", wait(parentTask, parentRound))
	putStartupContinuation(t, store, middle, middleInv, "ORIGINAL_MIDDLE_GOAL", agent.ContinuationContext{ParentAgent: parent, ParentInvocationID: parentInv, ParentTaskID: parentTask, ParentTaskRound: parentRound}, "", "", wait(middleTask, middleRound))
	putStartupContinuation(t, store, leaf, leafInv, "ORIGINAL_LEAF_GOAL", agent.ContinuationContext{ParentAgent: middle, ParentInvocationID: middleInv, ParentTaskID: middleTask, ParentTaskRound: middleRound}, "completed", "SAVED_LEAF_RESULT", nil)
	require.NoError(t, release())

	parentModel, middleModel, leafModel := &startupRecoveryModel{}, &startupRecoveryModel{}, &startupRecoveryModel{}
	cfg := Config{Entry: parent, Reliability: ReliabilityConfig{BusSpillDir: filepath.Join(dir, "bus")}, Agents: map[string]AgentConfig{
		parent: {SystemPrompt: PromptConfig{Inline: "parent"}, Memory: mc},
		middle: {SystemPrompt: PromptConfig{Inline: "middle"}, Memory: mc},
		leaf:   {SystemPrompt: PromptConfig{Inline: "leaf"}, Memory: mc},
	}}
	entry, err := New(cfg, WithModel(parentModel), WithModelOverrides(map[string]model.Model{middle: middleModel, leaf: leafModel}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, entry.Close()) })
	owners := entry.ResidentTable()
	require.NotNil(t, owners[middle])
	require.NotNil(t, owners[leaf])
	attached, err := entry.ResumePendingDelegations()
	require.NoError(t, err)
	require.Equal(t, 1, attached)
	parentTK, exists := entry.TaskManager().Get(parentTask)
	require.True(t, exists)
	require.True(t, parentTK.HasProducer())
	middleTK, exists := owners[middle].TaskManager().Get(middleTask)
	require.True(t, exists)
	require.False(t, middleTK.HasProducer(), "the alphabetically later owner has not attached yet")
	require.Never(t, func() bool {
		bindings, readErr := owners[middle].RecoveredContinuationBindingsFromWAL()
		if readErr != nil || len(bindings) != 1 {
			return true
		}
		return bindings[0].State != "suspended" || parentTK.Status() != task.TaskSuspect || len(middleModel.snapshot()) > 0
	}, 50*time.Millisecond, time.Millisecond, "binding the parent must leave its recovered child unstarted until all owners attach")

	_, err = entry.StartLoop("user", "three-level-recovery")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(parentModel.snapshot()) == 1 }, 3*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return parentTK.Status() == task.TaskCompleted && middleTK.Status() == task.TaskCompleted }, time.Second, time.Millisecond)
	require.Equal(t, parentRound, parentTK.ProducerRound())
	require.Equal(t, middleRound, middleTK.ProducerRound())
	require.Len(t, middleModel.snapshot(), 1)
	require.Contains(t, middleModel.snapshot()[0], "SAVED_LEAF_RESULT")
	require.Contains(t, parentModel.snapshot()[0], "recovered parent result")
	require.Empty(t, leafModel.snapshot(), "the cached original leaf outcome must not run its Goal again")
}

func TestStartupRecoveryFindsHiddenOwnerFromRealExpiredTaskOutbox(t *testing.T) {
	dir := t.TempDir()
	mc := MemoryConfig{Type: "localfile", Path: filepath.Join(dir, "store")}
	store, _, release, err := resolveMemoryStore(mc)
	require.NoError(t, err)
	const parent, hidden, childInv = "outbox-parent", "outbox-hidden", "original-outbox-child"
	source, err := agent.NewTagentAgent(&agent.TagentConfig{Name: parent, Model: &startupRecoveryModel{}, MemoryStore: store, MemStoreBorrowed: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = source.Close() })
	detector := task.NewManualDetector()
	detector.FireDetach()
	var detectorDone sync.Once
	finishDetector := func() { detectorDone.Do(detector.Done) }
	t.Cleanup(finishDetector)
	result := source.TaskManager().Spawn(task.TaskSpec{Kind: "subagent", WaitFor: task.WaitExit, Declarative: &task.Declarative{Kind: "subagent", AgentName: hidden, MessageBody: "ORIGINAL_OUTBOX_GOAL", Params: map[string]string{"child_invocation_id": childInv, "local_continuation": "true"}}}, detector)
	require.NotNil(t, result.Task)
	putStartupContinuation(t, store, hidden, childInv, "ORIGINAL_OUTBOX_GOAL", agent.ContinuationContext{ParentAgent: parent, ParentTaskID: result.Task.ID, ParentTaskRound: result.Task.RoundID()}, "completed", "SAVED_OUTBOX_RESULT", nil)
	detector.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "SAVED_OUTBOX_RESULT", ObservedAt: time.Now().Add(-time.Hour)})
	finishDetector()
	source.TaskManager().ResumePendingDeliveries()
	parentPartition := memory.PartitionIDFromName(parent)
	require.Eventually(t, func() bool {
		refs, queryErr := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{parentPartition}, EventTypes: []string{tagentevent.TypeTaskSignalReceipt}})
		return queryErr == nil && len(refs) > 0
	}, time.Second, time.Millisecond, "the real writer commits its short receipt before failing the absent child's acknowledgement")
	targets, err := persistedOutboxTargets(source)
	require.NoError(t, err)
	require.Equal(t, []string{hidden}, targets, "the real task_signal wire payload identifies its hidden child owner")
	require.NoError(t, source.Close())
	require.NoError(t, release())

	parentModel, childModel := &startupRecoveryModel{}, &startupRecoveryModel{}
	cfg := Config{Entry: parent, Reliability: ReliabilityConfig{BusSpillDir: filepath.Join(dir, "bus")}, Agents: map[string]AgentConfig{
		parent: {SystemPrompt: PromptConfig{Inline: "parent"}, Memory: mc},
		hidden: {SystemPrompt: PromptConfig{Inline: "hidden"}, Memory: mc},
	}}
	entry, err := New(cfg, WithModel(parentModel), WithModelOverrides(map[string]model.Model{hidden: childModel}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, entry.Close()) })
	_, retained := entry.TaskManager().Get(result.Task.ID)
	require.False(t, retained, "the accepted terminal result is older than the board's grace period")
	require.NotNil(t, entry.ResidentTable()[hidden], "raw outbox ownership survives terminal task pruning")
	_, err = entry.StartLoop("user", "outbox-recovery")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(parentModel.snapshot()) == 1 }, 3*time.Second, time.Millisecond)
	require.Contains(t, parentModel.snapshot()[0], "SAVED_OUTBOX_RESULT")
	require.Empty(t, childModel.snapshot())
	refs, err := entry.ResidentTable()[hidden].MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName(hidden)}, EventTypes: []string{tagentevent.TypeContinuationState}})
	require.NoError(t, err)
	acked := false
	for _, ref := range refs {
		fact, readErr := entry.ResidentTable()[hidden].MemStore().GetEvent(ref.EventKey)
		require.NoError(t, readErr)
		if fact != nil && strings.Contains(fact.Content, `"result_acked":true`) {
			acked = true
		}
	}
	require.True(t, acked, "the exact original child receives its pending parent acknowledgement")
}

func TestStartupHotTTLCommitAndRollbackRescheduleDeadlineWithoutAnotherTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "org.yaml")
	tick := time.Now()
	content := func(ttl string) string {
		return fmt.Sprintf("entry: deadline-owner\nagents:\n  deadline-owner:\n    system_prompt:\n      inline: test\n    task_default_ttl: %s\n", ttl)
	}
	tick = writeGateConfig(t, path, content("300ms"), tick)
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(path))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, entry.Close()) })
	d := task.NewManualDetector()
	d.FireDetach()
	t.Cleanup(d.Done)
	res := entry.TaskManager().Spawn(task.TaskSpec{Kind: "command"}, d)
	tick = writeGateConfig(t, path, content("1h"), tick)
	entry.CheckOrgReload()
	require.Equal(t, time.Hour, entry.TaskManager().DefaultTTL())
	time.Sleep(400 * time.Millisecond)
	require.True(t, res.Task.Status().Live(), "a committed longer TTL disarms the old deadline")
	entry.Rollback()
	require.Equal(t, 300*time.Millisecond, entry.TaskManager().DefaultTTL())
	require.Eventually(t, func() bool { return !res.Task.Status().Live() }, time.Second, 5*time.Millisecond, "rollback must wake the deadline worker without a model/board read")
}

func TestStartupCommandRecoveryKeepsRoundAndDefersDeliveryUntilLoopReady(t *testing.T) {
	for _, unboundIntent := range []bool{false, true} {
		t.Run(fmt.Sprint(unboundIntent), func(t *testing.T) {
			m := &startupRecoveryModel{called: make(chan struct{}, 2)}
			owner, err := agent.NewTagentAgent(&agent.TagentConfig{Name: "command-recovery", Model: m, SystemPrompt: "test"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, owner.Close()) })
			d := task.NewManualDetector()
			d.FireDetach()
			t.Cleanup(d.Done)
			const round = uint64(9)
			producer := round
			if unboundIntent {
				producer = 0
			}
			decl := &task.Declarative{Kind: "command", Desc: "original command", RoundID: round, ProducerRound: producer, WaitFor: task.WaitExit}
			spec := task.TaskSpec{Kind: "command", WaitFor: task.WaitExit, Declarative: decl}
			starts, recovers := 0, 0
			if unboundIntent {
				spec.Start = func(tk *task.Task) (task.TaskSpec, task.SettleDetector, error) {
					starts++
					require.Equal(t, round, tk.RoundID())
					return spec, d, nil
				}
			}
			tk := owner.TaskManager().RestoreTask("original-command", spec, time.Now(), task.TaskSuspect)
			recoverDetector := func(actual *task.Task) task.SettleDetector {
				recovers++
				require.Same(t, tk, actual)
				return d
			}
			require.NoError(t, recoverCommandTasks(owner.Info().Name, owner, recoverDetector))
			require.NoError(t, recoverCommandTasks(owner.Info().Name, owner, recoverDetector), "repeated assembly must not attach or start twice")
			if unboundIntent {
				require.Equal(t, 1, starts)
				require.Zero(t, recovers)
			} else {
				require.Zero(t, starts)
				require.Equal(t, 1, recovers)
			}
			require.True(t, tk.HasProducer())
			require.Equal(t, round, tk.ProducerRound())
			d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "ORIGINAL_COMMAND_RESULT"})
			require.Eventually(t, func() bool { return len(tk.PendingSignals()) == 1 }, time.Second, time.Millisecond)
			require.Empty(t, m.snapshot(), "recovery receivers are still being assembled")
			refs, err := owner.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{memory.PartitionIDFromName(owner.Info().Name)}, EventTypes: []string{tagentevent.TypeTaskSignalReceipt}})
			require.NoError(t, err)
			require.Empty(t, refs, "the recovered producer cannot claim acceptance before the final startup phase")
			_, err = owner.StartLoop("user", "command-recovery")
			require.NoError(t, err)
			select {
			case <-m.called:
			case <-time.After(3 * time.Second):
				t.Fatal("the original command result was not consumed")
			}
			require.Equal(t, round, tk.RoundID())
			require.Equal(t, round, tk.ProducerRound())
			require.Contains(t, strings.Join(m.snapshot(), "\n"), "ORIGINAL_COMMAND_RESULT")
		})
	}
}

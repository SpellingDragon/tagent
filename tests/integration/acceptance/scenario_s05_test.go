package acceptance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"
)

// scenarioDeliveries records the notifications one deliverer was offered.
type scenarioDeliveries struct {
	mu      sync.Mutex
	offered []task.NoticeEnvelope
	accept  task.DeliveryAcceptance
}

// deliver offers one envelope to the delivery of the drive.
func (d *scenarioDeliveries) deliver(ctx context.Context, envelope task.NoticeEnvelope) (task.DeliveryAcceptance, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.offered = append(d.offered, envelope)
	return d.accept, nil
}

// snapshot reports the envelopes offered so far.
func (d *scenarioDeliveries) snapshot() []task.NoticeEnvelope {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]task.NoticeEnvelope, len(d.offered))
	copy(out, d.offered)
	return out
}

// reset drops the recorded offers so the next round reports its own.
func (d *scenarioDeliveries) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.offered = nil
}

// scenarioTaskFace joins the task manager and the registry of one drive so the
// close face of an agent settles through both.
type scenarioTaskFace struct {
	*task.MemoryManager
	*task.MemoryRegistry
}

// newScenarioTaskFace builds the task face of one close drive.
func newScenarioTaskFace(manager *task.MemoryManager, registry *task.MemoryRegistry) *scenarioTaskFace {
	return &scenarioTaskFace{MemoryManager: manager, MemoryRegistry: registry}
}

// scenarioAdmitRunning admits one task and moves it to running.
func scenarioAdmitRunning(t *testing.T, face *scenarioTaskFace, name string, instance event.InstanceID, target event.Address) event.TaskID {
	t.Helper()
	ctx := context.Background()
	receipt, err := face.Admit(ctx, task.Spec{
		CallID:       "call-" + name,
		DedupKey:     task.DedupKey("dedup-" + name),
		InvocationID: event.InvocationID("inv-" + name),
		InstanceID:   instance,
		Description:  "hand the report of " + name + " over",
		Source:       event.Address{AgentID: "agent-src", SessionID: "session-src"},
		Target:       target,
		Execution:    "serial",
	})
	if err != nil || !receipt.Accepted {
		t.Fatalf("admit %s = (%+v, %v), want one accepted admission", name, receipt, err)
	}
	if result, err := face.Advance(ctx, receipt.TaskID, task.StatusRunning, task.MutationID("run-"+name), nil); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance %s = (%v, %v), want the task running", name, result, err)
	}
	return receipt.TaskID
}

// scenarioIssueProgress commits one progress notification beside its frozen
// routing and reports the envelope it issued.
func scenarioIssueProgress(t *testing.T, face *scenarioTaskFace, taskID event.TaskID, fact event.EventID, mutation task.MutationID) task.NoticeEnvelope {
	t.Helper()
	ctx := context.Background()
	envelope, err := face.IssueNotice(ctx, taskID, task.NoticeProgress, fact)
	if err != nil {
		t.Fatalf("issue the notification of %s: %v", taskID, err)
	}
	if result, err := face.Advance(ctx, taskID, task.StatusProgress, mutation, []task.NoticeEnvelope{envelope}); result != task.CommitCommitted || err != nil {
		t.Fatalf("commit the notification of %s = (%v, %v), want the notification committed beside its frozen routing", taskID, result, err)
	}
	return envelope
}

// TestScenario05CloseHandoffReincarnation drives one task across a handoff and closes one agent over its producer.
// - the notification issued before the handoff keeps the target it froze
// - the notification issued after the handoff freezes the new target and instance, and no old notification reaches the new instance
// - the drain reports no termination while a producer still runs and reports it once every producer stopped
// - every notification carries its task, round, category and sequence identity
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestScenario05CloseHandoffReincarnation(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S05")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	oldTarget := event.Address{AgentID: "agent-recv", SessionID: "session-old"}
	newTarget := event.Address{AgentID: "agent-recv", SessionID: "session-new"}
	const sourceInstance event.InstanceID = "instance-source"
	const targetInstance event.InstanceID = "instance-target"

	deliveries := &scenarioDeliveries{accept: task.DeliveryPending}
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
	manager := task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(deliveries.deliver))
	face := newScenarioTaskFace(manager, registry)

	taskID := scenarioAdmitRunning(t, face, "handoff", sourceInstance, oldTarget)
	started := scenarioIssueProgress(t, face, taskID, "evt-started", "progress-started")
	if err := manager.Handoff(ctx, taskID, task.HandoffTarget{Target: newTarget, TargetInstance: targetInstance}); err != nil {
		t.Fatalf("hand the task over: %v", err)
	}
	future := scenarioIssueProgress(t, face, taskID, "evt-future", "progress-future")
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush the owed notifications: %v", err)
	}
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("repeat the flush: %v", err)
	}
	offered := deliveries.snapshot()
	startedFixed := started.Target == oldTarget && started.TargetInstance == sourceInstance
	crossed := false
	for _, attempt := range offered {
		switch attempt.Notice {
		case started.Notice:
			if attempt != started {
				startedFixed = false
				crossed = true
			}
		case future.Notice:
			if attempt != future {
				crossed = true
			}
		}
	}
	observations["started_notice_target_fixed"] = startedFixed
	observations["future_notice_target_new_instance"] = future.Target == newTarget && future.TargetInstance == targetInstance
	observations["old_notice_delivered_to_new_instance"] = crossed
	observations["notice_identity_fields"] = map[string]any{
		"task":     started.Notice.TaskID == taskID && future.Notice.TaskID == taskID,
		"round":    started.Notice.Round >= 1 && future.Notice.Round == started.Notice.Round,
		"category": started.Notice.Kind == task.NoticeProgress && future.Notice.Kind == task.NoticeProgress,
		"sequence": future.Notice.Seq == started.Notice.Seq+1,
	}
	refs = append(refs, string(taskID), string(started.Notice.Event), string(future.Notice.Event))

	openDeliveries := &scenarioDeliveries{accept: task.DeliveryAccepted}
	openRegistry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
	openManager := task.NewMemoryManager(openRegistry, nil, task.WithEnvelopeDeliverer(openDeliveries.deliver))
	openFace := newScenarioTaskFace(openManager, openRegistry)
	open := agent.NewShutdown(openFace)
	receipt, err := open.Admit(ctx, task.Spec{
		CallID:       "call-drain",
		DedupKey:     task.DedupKey("dedup-drain"),
		InvocationID: event.InvocationID("inv-drain"),
		InstanceID:   sourceInstance,
		Target:       oldTarget,
		Execution:    "serial",
	})
	if err != nil || !receipt.Accepted {
		t.Fatalf("admit the drained task = (%+v, %v), want one accepted admission", receipt, err)
	}
	if result, err := open.Advance(ctx, receipt.TaskID, task.StatusRunning, "run-drain", nil); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance the drained task = (%v, %v), want the task running", result, err)
	}
	running := true
	if err := open.Watch(receipt.TaskID, func() bool { return !running }); err != nil {
		t.Fatalf("watch the producer: %v", err)
	}
	beforeReport, beforeErr := open.Close(ctx, agent.CloseRequest{
		Address:  event.Address{AgentID: "agent-src", SessionID: "session-src"},
		Deadline: time.Now().Add(100 * time.Millisecond),
	})
	refs = append(refs, string(receipt.TaskID))

	closedDeliveries := &scenarioDeliveries{accept: task.DeliveryAccepted}
	closedRegistry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), task.NewMemoryAdmission())
	closedManager := task.NewMemoryManager(closedRegistry, nil, task.WithEnvelopeDeliverer(closedDeliveries.deliver))
	closedFace := newScenarioTaskFace(closedManager, closedRegistry)
	closed := agent.NewShutdown(closedFace)
	closedReceipt, err := closed.Admit(ctx, task.Spec{
		CallID:       "call-stopped",
		DedupKey:     task.DedupKey("dedup-stopped"),
		InvocationID: event.InvocationID("inv-stopped"),
		InstanceID:   sourceInstance,
		Target:       oldTarget,
		Execution:    "serial",
	})
	if err != nil || !closedReceipt.Accepted {
		t.Fatalf("admit the stopped task = (%+v, %v), want one accepted admission", closedReceipt, err)
	}
	if result, err := closed.Advance(ctx, closedReceipt.TaskID, task.StatusRunning, "run-stopped", nil); result != task.CommitCommitted || err != nil {
		t.Fatalf("advance the stopped task = (%v, %v), want the task running", result, err)
	}
	if result, err := closed.Advance(ctx, closedReceipt.TaskID, task.StatusSettled, "settle-stopped", nil); result != task.CommitCommitted || err != nil {
		t.Fatalf("settle the stopped task = (%v, %v), want the settle committed", result, err)
	}
	if err := closed.Watch(closedReceipt.TaskID, func() bool { return true }); err != nil {
		t.Fatalf("watch the stopped producer: %v", err)
	}
	afterReport, afterErr := closed.Close(ctx, agent.CloseRequest{
		Address:  event.Address{AgentID: "agent-src", SessionID: "session-src"},
		Deadline: time.Now().Add(10 * time.Second),
	})
	refs = append(refs, string(closedReceipt.TaskID))

	observations["drain_phases"] = map[string]any{
		"before_all_stopped": beforeReport.Drained,
		"after_all_stopped":  afterReport.Drained,
	}

	EmitScenarioRecord(t, scenario, refs, observations)

	if !errors.Is(beforeErr, agent.ErrDrainUnsettled) {
		t.Errorf("close beside a running producer = %v, want the named unsettled state %v", beforeErr, agent.ErrDrainUnsettled)
	}
	if afterErr != nil {
		t.Errorf("close beside stopped producers = %v, want a clean drain", afterErr)
	}
}

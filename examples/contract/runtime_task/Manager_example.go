package taskexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// taskManager is the canonical admission and settlement face: it admits a task, reports the
// obligations it still holds, hands future notifications to another target and flushes the
// notifications whose obligations are still open.
type taskManager struct {
	receipts    map[event.TaskID]task.AdmissionReceipt
	obligations map[event.TaskID]task.Obligations
	handedOff   map[event.TaskID]task.HandoffTarget
	flushed     map[event.TaskID]int
}

// Admit accepts one task declaration and returns the admission receipt, with the obligation
// it opened still open.
func (m *taskManager) Admit(_ context.Context, spec task.Spec) (task.AdmissionReceipt, error) {
	if m.receipts == nil {
		m.receipts = map[event.TaskID]task.AdmissionReceipt{}
		m.obligations = map[event.TaskID]task.Obligations{}
	}
	taskID := event.TaskID("task-" + spec.CallID)
	m.receipts[taskID] = task.AdmissionReceipt{CallID: spec.CallID, TaskID: taskID, Accepted: true, Kind: task.AdmissionFresh}
	m.obligations[taskID] = task.Obligations{
		Count: 1,
		Owner: spec.InvocationID,
		Open:  []task.Obligation{{Kind: task.ObligationAwaitingDelivery, TaskID: taskID}},
	}
	return m.receipts[taskID], nil
}

// Obligations reports the manager's outstanding obligations for a task, by kind.
func (m *taskManager) Obligations(_ context.Context, taskID event.TaskID) (task.Obligations, error) {
	obligations, ok := m.obligations[taskID]
	if !ok {
		return task.Obligations{}, errUnknownTask
	}
	return obligations, nil
}

// Handoff moves a task's future notifications to the target address and target instance.
func (m *taskManager) Handoff(_ context.Context, taskID event.TaskID, target task.HandoffTarget) error {
	if _, ok := m.receipts[taskID]; !ok {
		return errUnknownTask
	}
	if m.handedOff == nil {
		m.handedOff = map[event.TaskID]task.HandoffTarget{}
	}
	m.handedOff[taskID] = target
	return nil
}

// Flush delivers the notifications of a task whose obligations are still open.
func (m *taskManager) Flush(_ context.Context, taskID event.TaskID) error {
	obligations, ok := m.obligations[taskID]
	if !ok {
		return errUnknownTask
	}
	if obligations.Count == 0 {
		return nil
	}
	if m.flushed == nil {
		m.flushed = map[event.TaskID]int{}
	}
	m.flushed[taskID]++
	return nil
}

var _ task.Manager = (*taskManager)(nil)

// ExampleManager admits one task, reports the obligation the admission opened, hands the
// task's future notifications to another instance and flushes them while the obligation is
// still open.
//
// 契约: docs/wiki/agent/task-lifecycle.md#detach-suppression
func ExampleManager() {
	ctx := context.Background()
	manager := &taskManager{}

	receipt, err := manager.Admit(ctx, task.Spec{CallID: "call-1", InvocationID: "invocation-1", Target: event.Address{AgentID: "assistant", SessionID: "session-1"}})
	if err != nil || !receipt.Accepted {
		return
	}
	obligations, err := manager.Obligations(ctx, receipt.TaskID)
	if err != nil || obligations.Count != 1 || len(obligations.Open) != 1 {
		return
	}
	target := task.HandoffTarget{
		Target:         event.Address{AgentID: "assistant", SessionID: "session-2"},
		TargetInstance: "instance-2",
	}
	if err := manager.Handoff(ctx, receipt.TaskID, target); err != nil {
		return
	}
	if err := manager.Flush(ctx, receipt.TaskID); err != nil || manager.flushed[receipt.TaskID] != 1 {
		return
	}
}

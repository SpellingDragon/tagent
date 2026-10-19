package agentexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// delegationLedger is the canonical task face of the delegation ledger: it admits one
// delegated call, advances it beside the notifications that transition owes, reports the
// obligations still open and delivers them.
type delegationLedger struct {
	status map[event.TaskID]task.Status
	owed   map[event.TaskID][]task.NoticeEnvelope
}

func newDelegationLedger() *delegationLedger {
	return &delegationLedger{
		status: map[event.TaskID]task.Status{},
		owed:   map[event.TaskID][]task.NoticeEnvelope{},
	}
}

// Admit admits one delegated call as a task and reports the receipt of that admission.
func (l *delegationLedger) Admit(_ context.Context, spec task.Spec) (task.AdmissionReceipt, error) {
	id := event.TaskID("task-" + spec.CallID)
	l.status[id] = task.StatusAccepted
	return task.AdmissionReceipt{
		CallID:   spec.CallID,
		TaskID:   id,
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}, nil
}

// Advance moves one task to the next status and books the notifications it still owes.
func (l *delegationLedger) Advance(_ context.Context, taskID event.TaskID, to task.Status, _ task.MutationID, outbox []task.NoticeEnvelope) (task.CommitResult, error) {
	if _, ok := l.status[taskID]; !ok {
		return task.CommitNotCommitted, task.ErrTaskNotFound
	}
	l.status[taskID] = to
	l.owed[taskID] = append(l.owed[taskID], outbox...)
	return task.CommitCommitted, nil
}

// Obligations reports the obligations the task still owes, each beside the wait it is in.
func (l *delegationLedger) Obligations(_ context.Context, taskID event.TaskID) (task.Obligations, error) {
	open := make([]task.Obligation, 0, len(l.owed[taskID]))
	for _, envelope := range l.owed[taskID] {
		open = append(open, task.Obligation{
			Kind:   task.ObligationAwaitingDelivery,
			TaskID: taskID,
			Notice: envelope.Notice,
		})
	}
	return task.Obligations{Count: len(open), Open: open}, nil
}

// Flush delivers the notifications of one task; only an acceptance clears one.
func (l *delegationLedger) Flush(_ context.Context, taskID event.TaskID) error {
	delete(l.owed, taskID)
	return nil
}

var _ agent.DelegationTasks = newDelegationLedger()

// ExampleDelegationTasks admits one delegated call, settles it beside its notification
// and reports that the obligation cleared only once it was delivered.
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func ExampleDelegationTasks() {
	ctx := context.Background()
	target := event.Address{AgentID: "worker", SessionID: "session-2"}
	ledger := newDelegationLedger()
	receipt, err := ledger.Admit(ctx, task.Spec{
		CallID:       "call-1",
		DedupKey:     "instance-1/inv-1/decision-1/call-1",
		InvocationID: "inv-1",
		Target:       target,
	})
	if err != nil {
		return
	}
	notice := task.Notice{TaskID: receipt.TaskID, Round: 1, Kind: task.NoticeSettled}
	if _, err := ledger.Advance(ctx, receipt.TaskID, task.StatusSettled, "mutation-1", []task.NoticeEnvelope{
		{Notice: notice, Target: target},
	}); err != nil {
		return
	}
	owed, err := ledger.Obligations(ctx, receipt.TaskID)
	if err != nil {
		return
	}
	if err := ledger.Flush(ctx, receipt.TaskID); err != nil {
		return
	}
	_ = owed.Count
}

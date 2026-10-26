package subagentexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// delegationLog is the canonical delegation face: it admits one sub-agent call as a task,
// answers the caller with that admission's receipt and reports the terminal of the same
// task as the notice the issuing session receives.
type delegationLog struct {
	receipts map[event.TaskID]task.AdmissionReceipt
}

// errTaskNotAdmitted is the named refusal a delegation reports instead of answering a
// terminal for a task it never admitted.
var errTaskNotAdmitted = errors.New("subagentexample: the task was never admitted")

func newDelegationLog() *delegationLog {
	return &delegationLog{receipts: map[event.TaskID]task.AdmissionReceipt{}}
}

// Delegate admits one sub-agent call as a task and reports its admission receipt.
func (d *delegationLog) Delegate(_ context.Context, req agent.Delegation) (task.AdmissionReceipt, error) {
	receipt := task.AdmissionReceipt{
		CallID:   req.CallID,
		TaskID:   req.TaskID,
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}
	d.receipts[req.TaskID] = receipt
	return receipt, nil
}

// Complete reports the terminal of one admitted call, so its notification carries the
// outcome to the session that issued the call.
func (d *delegationLog) Complete(_ context.Context, taskID event.TaskID, status task.Status, fact event.EventID) (task.Notice, error) {
	if _, ok := d.receipts[taskID]; !ok {
		return task.Notice{}, errTaskNotAdmitted
	}
	return task.Notice{
		TaskID: taskID,
		Round:  1,
		Kind:   task.NoticeSettled,
		Seq:    1,
		Event:  fact,
	}, nil
}

var _ subagent.Delegation = (*delegationLog)(nil)

// ExampleDelegation admits one sub-agent call, reports the receipt that answers its
// caller, reports the terminal notice of that same task and reports that a task never
// admitted is refused rather than answered with a terminal.
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func ExampleDelegation() {
	ctx := context.Background()
	delegation := newDelegationLog()
	req := agent.Delegation{
		InvocationID: "invocation-1",
		DecisionID:   "decision-1",
		CallID:       "call-1",
		TaskID:       "task-1",
		Target:       event.Address{AgentID: "researcher", SessionID: "session-2"},
		Ref:          state.BindingRef{AgentID: "assistant", InstanceID: "instance-1", Generation: 3},
		Status:       task.StatusAccepted,
	}

	receipt, err := delegation.Delegate(ctx, req)
	if err != nil {
		return
	}
	if !receipt.Accepted || receipt.TaskID != req.TaskID || receipt.CallID != req.CallID {
		return
	}
	notice, err := delegation.Complete(ctx, req.TaskID, task.StatusSettled, "evt-1")
	if err != nil {
		return
	}
	if notice.TaskID != req.TaskID || notice.Kind != task.NoticeSettled || notice.Event != "evt-1" {
		return
	}
	if _, err := delegation.Complete(ctx, "task-other", task.StatusSettled, "evt-2"); !errors.Is(err, errTaskNotAdmitted) {
		return
	}
}

package taskexample

import (
	"context"
	"encoding/json"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// settledAccount is the canonical settle account: it answers the receipts one
// task's durable record holds, keyed by task identity, so a checkpoint states the
// account of every task it names with one read per task instead of scanning the
// durable prefixes of the history those tasks already closed. The families are
// held in the notice order the durable read hands them back in.
type settledAccount struct {
	held map[event.TaskID]task.Receipts
}

// ReceiptsOf answers the receipts one task holds, ordered by notice identity.
func (a settledAccount) ReceiptsOf(taskID event.TaskID) task.Receipts {
	return a.held[taskID]
}

var _ task.SettleAccount = settledAccount{}

// ExampleSettleAccount states the settle account of the tasks one checkpoint
// names: the task that consumed a notification and refused another is stated with
// both families beside its record, and the task beside no receipt is stated with
// the three families as empty lists rather than left out.
//
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func ExampleSettleAccount() {
	ctx := context.Background()
	payload, err := json.Marshal(task.RegistryCheckpoint{Tasks: []task.StatedTask{
		{TaskID: "task-settled"},
		{TaskID: "task-unsettled"},
	}})
	if err != nil {
		return
	}
	account := settledAccount{held: map[event.TaskID]task.Receipts{
		"task-settled": {
			Consumed: []task.StatedConsumption{{
				Notice: task.Notice{TaskID: "task-settled", Round: 1, Kind: task.NoticeAccepted, Seq: 1, Event: "fact-1"},
				Event:  "fact-1",
			}},
			Attempts: []task.StatedAttempt{{
				Notice:   task.Notice{TaskID: "task-settled", Round: 1, Kind: task.NoticeProgress, Seq: 1},
				Terminal: "completed",
			}},
			Rejected: []task.StatedRefusal{{
				Notice:   task.Notice{TaskID: "task-settled", Round: 1, Kind: task.NoticeSettled, Seq: 1},
				Target:   event.Address{AgentID: "agent-1", SessionID: "session-1"},
				Instance: "instance-1",
				Reason:   "the target is gone",
			}},
		},
	}}
	stated, err := task.StateSettleAccount(ctx, payload, account)
	if err != nil {
		return
	}
	var wire task.RegistryCheckpoint
	if err := json.Unmarshal(stated, &wire); err != nil {
		return
	}
	if len(wire.Tasks[0].Consumed) != 1 || len(wire.Tasks[0].Attempts) != 1 || len(wire.Tasks[0].Rejected) != 1 {
		return
	}
	if wire.Tasks[1].Consumed == nil || wire.Tasks[1].Attempts == nil || wire.Tasks[1].Rejected == nil {
		return
	}
	if len(wire.Tasks[1].Consumed) != 0 || len(wire.Tasks[1].Attempts) != 0 || len(wire.Tasks[1].Rejected) != 0 {
		return
	}
}

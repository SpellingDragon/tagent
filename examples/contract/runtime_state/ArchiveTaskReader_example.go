package stateexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// errUnknownTask is reported for a task the chain holds no record for.
var errUnknownTask = errors.New("stateexample: the chain holds no record for the task")

// migratedTaskChain is the canonical read face of the migrated task chain: the tasks it
// holds facts for and the fact chain of one of them.
type migratedTaskChain struct {
	order   []event.TaskID
	records map[event.TaskID][]task.TaskRecord
}

// TaskIDs returns every task the chain holds facts for.
func (c *migratedTaskChain) TaskIDs() []event.TaskID {
	return append([]event.TaskID(nil), c.order...)
}

// Records returns the fact chain of one task, in its recorded order.
func (c *migratedTaskChain) Records(_ context.Context, taskID event.TaskID) ([]task.TaskRecord, error) {
	records, ok := c.records[taskID]
	if !ok {
		return nil, errUnknownTask
	}
	return append([]task.TaskRecord(nil), records...), nil
}

var _ state.ArchiveTaskReader = (*migratedTaskChain)(nil)

// ExampleArchiveTaskReader enumerates the tasks the chain holds facts for, reads one chain
// in recorded order and reports a task it holds nothing for by name.
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func ExampleArchiveTaskReader() {
	ctx := context.Background()
	chain := &migratedTaskChain{
		order: []event.TaskID{"task-1"},
		records: map[event.TaskID][]task.TaskRecord{
			"task-1": {
				{Spec: task.Spec{CallID: "call-1"}},
				{Fact: task.CommitRecord{MutationID: "mutation-1", TaskID: "task-1", Status: task.StatusRunning}},
			},
		},
	}

	ids := chain.TaskIDs()
	if len(ids) != 1 {
		return
	}
	records, err := chain.Records(ctx, ids[0])
	if err != nil || len(records) != 2 || records[0].Spec.CallID != "call-1" {
		return
	}
	if _, err := chain.Records(ctx, "task-9"); !errors.Is(err, errUnknownTask) {
		return
	}
}

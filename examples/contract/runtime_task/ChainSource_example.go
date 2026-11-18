package taskexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// chainLinks is the canonical chain source: it answers the durable fact chain of one task by
// its identity, in commit order and with the admission material on the admission link alone.
type chainLinks struct {
	chains map[event.TaskID][]task.TaskRecord
	reads  int
}

// Records answers the fact chain of one task in commit order, and no chain at all for an
// identity the source holds no link for.
func (c *chainLinks) Records(_ context.Context, taskID event.TaskID) ([]task.TaskRecord, error) {
	c.reads++
	return c.chains[taskID], nil
}

var _ task.ChainSource = (*chainLinks)(nil)

// ExampleChainSource names one task a restored checkpoint stated no entry for: the registry
// reads its chain once, rebuilds the declaration and the status the links carry, and answers
// the next read of the same identity from the record it now holds. An identity the source
// holds no link for reads as unknown.
//
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func ExampleChainSource() {
	ctx := context.Background()
	taskID := event.TaskID("task-1")
	chains := &chainLinks{chains: map[event.TaskID][]task.TaskRecord{
		taskID: {
			{
				Spec: task.Spec{CallID: "call-1", DedupKey: "dedup-1", InvocationID: "invocation-1"},
				Fact: task.CommitRecord{MutationID: "mutation-1", TaskID: taskID, Status: task.StatusAccepted},
			},
			{
				Fact: task.CommitRecord{
					MutationID:       "mutation-2",
					TaskID:           taskID,
					Status:           task.StatusRunning,
					ExpectedRevision: 1,
				},
			},
		},
	}}
	registry := task.NewMemoryRegistry(&commitLog{}, task.NewMemoryAdmission())
	registry.RestoreChainSource(chains)
	if err := registry.RestoreCheckpoint(ctx, []byte("{\"tasks\":[]}")); err != nil {
		return
	}

	spec, status, err := registry.Lookup(ctx, taskID)
	if err != nil || spec.CallID != "call-1" || status != task.StatusRunning {
		return
	}
	if chains.reads != 1 {
		return
	}
	if _, _, err := registry.Lookup(ctx, taskID); err != nil || chains.reads != 1 {
		return
	}
	if _, _, err := registry.Lookup(ctx, event.TaskID("task-2")); err != task.ErrTaskNotFound {
		return
	}
}

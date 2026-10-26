package stateexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// errNoMutation is reported for a commit record that names no mutation identity.
var errNoMutation = errors.New("stateexample: the commit record names no mutation")

// migratedTaskWriter is the canonical write face of the migrated task chain: the admission
// material one migrated task declares beside the fact it commits.
type migratedTaskWriter struct {
	specs map[event.TaskID]task.Spec
	facts map[event.TaskID][]task.CommitRecord
}

// Declare stores the admission material of one migrated task.
func (w *migratedTaskWriter) Declare(_ context.Context, taskID event.TaskID, spec task.Spec) error {
	if w.specs == nil {
		w.specs = map[event.TaskID]task.Spec{}
	}
	if _, ok := w.specs[taskID]; !ok {
		w.specs[taskID] = spec
	}
	return nil
}

// Commit stores the deterministic fact of one migrated task, and recognises a repeated fact
// as the same fact.
func (w *migratedTaskWriter) Commit(_ context.Context, record task.CommitRecord) (task.CommitResult, error) {
	if record.MutationID == "" {
		return task.CommitUncertain, errNoMutation
	}
	if w.facts == nil {
		w.facts = map[event.TaskID][]task.CommitRecord{}
	}
	for _, fact := range w.facts[record.TaskID] {
		if fact.MutationID == record.MutationID {
			return task.CommitCommitted, nil
		}
	}
	w.facts[record.TaskID] = append(w.facts[record.TaskID], record)
	return task.CommitCommitted, nil
}

var _ state.ArchiveTaskWriter = (*migratedTaskWriter)(nil)

// ExampleArchiveTaskWriter declares one migrated task, commits its fact and reports that a
// replayed commit records no second fact.
//
// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
func ExampleArchiveTaskWriter() {
	ctx := context.Background()
	writer := &migratedTaskWriter{}
	var taskID event.TaskID = "task-1"

	if err := writer.Declare(ctx, taskID, task.Spec{CallID: "call-1", InvocationID: "invocation-1"}); err != nil {
		return
	}
	fact := task.CommitRecord{MutationID: "mutation-1", TaskID: taskID, Status: task.StatusRunning}
	if result, err := writer.Commit(ctx, fact); err != nil || result != task.CommitCommitted {
		return
	}
	if result, err := writer.Commit(ctx, fact); err != nil || result != task.CommitCommitted {
		return
	}
	if len(writer.facts[taskID]) != 1 {
		return
	}
	if _, err := writer.Commit(ctx, task.CommitRecord{TaskID: taskID}); !errors.Is(err, errNoMutation) {
		return
	}
}

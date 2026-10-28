package taskexample

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// errUnknownTask is reported for a task the table does not hold.
var errUnknownTask = errors.New("taskexample: the table holds no such task")

// taskTable is the canonical task table: it registers, looks up, cancels and resumes a task,
// and stores every transition on the commit port before it becomes visible.
type taskTable struct {
	commit    task.Commit
	specs     map[event.TaskID]task.Spec
	statuses  map[event.TaskID]task.Status
	mutations map[task.MutationID]task.CommitRecord
	seq       int
}

func (t *taskTable) transition(ctx context.Context, taskID event.TaskID, status task.Status) error {
	t.seq++
	record := task.CommitRecord{
		MutationID: task.MutationID(fmt.Sprintf("mutation-%d", t.seq)),
		TaskID:     taskID,
		Status:     status,
	}
	result, err := t.commit.Commit(ctx, record)
	if err != nil {
		return err
	}
	if result != task.CommitCommitted {
		return errUnknownTask
	}
	if t.mutations == nil {
		t.mutations = map[task.MutationID]task.CommitRecord{}
	}
	t.mutations[record.MutationID] = record
	t.statuses[taskID] = status
	return nil
}

// Register admits one task declaration and returns the admission receipt.
func (t *taskTable) Register(ctx context.Context, spec task.Spec) (task.AdmissionReceipt, error) {
	for taskID, existing := range t.specs {
		if spec.DedupKey != "" && existing.DedupKey == spec.DedupKey {
			return task.AdmissionReceipt{CallID: spec.CallID, TaskID: taskID, Accepted: true, Kind: task.AdmissionDedup}, nil
		}
	}
	var buf [16]byte
	rand.Read(buf[:])
	taskID := event.TaskID("task-" + hex.EncodeToString(buf[:]))
	if t.specs == nil {
		t.specs = map[event.TaskID]task.Spec{}
		t.statuses = map[event.TaskID]task.Status{}
	}
	t.specs[taskID] = spec
	if err := t.transition(ctx, taskID, task.StatusAccepted); err != nil {
		return task.AdmissionReceipt{}, err
	}
	return task.AdmissionReceipt{CallID: spec.CallID, TaskID: taskID, Accepted: true, Kind: task.AdmissionFresh}, nil
}

// Lookup returns the declaration and status of the task with the given identity.
func (t *taskTable) Lookup(_ context.Context, taskID event.TaskID) (task.Spec, task.Status, error) {
	spec, ok := t.specs[taskID]
	if !ok {
		return task.Spec{}, task.StatusUnknown, errUnknownTask
	}
	return spec, t.statuses[taskID], nil
}

// Cancel requests cancellation of the named task only.
func (t *taskTable) Cancel(ctx context.Context, taskID event.TaskID) error {
	if _, ok := t.specs[taskID]; !ok {
		return errUnknownTask
	}
	return t.transition(ctx, taskID, task.StatusCancelled)
}

// Resume feeds new input into an existing task and re-enters its lifecycle under the same
// identity.
func (t *taskTable) Resume(ctx context.Context, taskID event.TaskID, input event.Event) (task.AdmissionReceipt, error) {
	spec, ok := t.specs[taskID]
	if !ok {
		return task.AdmissionReceipt{}, errUnknownTask
	}
	if err := t.transition(ctx, taskID, task.StatusRunning); err != nil {
		return task.AdmissionReceipt{}, err
	}
	return task.AdmissionReceipt{CallID: spec.CallID, TaskID: taskID, Accepted: true, Kind: task.AdmissionDedup}, nil
}

var _ task.Registry = (*taskTable)(nil)

// ExampleRegistry registers one task, reports a repeated admission as a dedup of the same
// task, resumes it under the same identity and cancels it without touching any other task.
//
// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
func ExampleRegistry() {
	ctx := context.Background()
	table := &taskTable{commit: &commitLog{}}
	spec := task.Spec{CallID: "call-1", DedupKey: "dedup-1", InvocationID: "invocation-1"}

	first, err := table.Register(ctx, spec)
	if err != nil || first.Kind != task.AdmissionFresh {
		return
	}
	repeat, err := table.Register(ctx, spec)
	if err != nil || repeat.TaskID != first.TaskID || repeat.Kind != task.AdmissionDedup {
		return
	}
	if _, status, err := table.Lookup(ctx, first.TaskID); err != nil || status != task.StatusAccepted {
		return
	}
	if _, err := table.Resume(ctx, first.TaskID, event.Event{}); err != nil {
		return
	}
	if err := table.Cancel(ctx, first.TaskID); err != nil {
		return
	}
	if _, status, err := table.Lookup(ctx, first.TaskID); err != nil || status != task.StatusCancelled {
		return
	}
}

package taskexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/runtime/task"
)

// errNoMutation is reported for a record that names no mutation identity.
var errNoMutation = errors.New("taskexample: the commit record names no mutation")

// commitLog is the canonical commit port: it stores the status change and its pending
// notifications as one record and reports the three outcomes separately.
type commitLog struct {
	committed map[task.MutationID]task.CommitRecord
}

// Commit stores one record and reports the commit outcome.
func (l *commitLog) Commit(_ context.Context, record task.CommitRecord) (task.CommitResult, error) {
	if record.MutationID == "" {
		return task.CommitUncertain, errNoMutation
	}
	if l.committed == nil {
		l.committed = map[task.MutationID]task.CommitRecord{}
	}
	existing, ok := l.committed[record.MutationID]
	if ok && existing.Status != record.Status {
		return task.CommitNotCommitted, nil
	}
	l.committed[record.MutationID] = record
	return task.CommitCommitted, nil
}

var _ task.Commit = (*commitLog)(nil)

// ExampleCommit stores one transition with its notification, reports a repeated mutation
// identity with the same content as committed and refuses one that carries other content.
//
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func ExampleCommit() {
	ctx := context.Background()
	log := &commitLog{}
	record := task.CommitRecord{
		MutationID: "mutation-1",
		TaskID:     "task-1",
		Status:     task.StatusRunning,
		Outbox:     []task.Envelope{{}},
	}

	if result, err := log.Commit(ctx, record); err != nil || result != task.CommitCommitted {
		return
	}
	if result, err := log.Commit(ctx, record); err != nil || result != task.CommitCommitted {
		return
	}
	conflicting := record
	conflicting.Status = task.StatusSettled
	if result, err := log.Commit(ctx, conflicting); err != nil || result != task.CommitNotCommitted {
		return
	}
	if result, err := log.Commit(ctx, task.CommitRecord{TaskID: "task-1"}); err == nil || result != task.CommitUncertain {
		return
	}
}

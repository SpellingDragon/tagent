package task

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
//
// CancelReceipt is the control receipt of one cancellation request. It reports the
// request and nothing more: the task ends when the executor reports an actual stop
// through StopEvidence, so the receipt is never read as termination.
type CancelReceipt struct {
	// TaskID is the task the request targets.
	TaskID event.TaskID
	// Requested reports whether the cancellation request is recorded.
	Requested bool
}

// StopEvidence is the executor's report about actually stopping one task. The manager
// records the report and infers no stop of its own.
type StopEvidence struct {
	// TaskID is the task the report is about.
	TaskID event.TaskID
	// Stopped reports whether the execution actually stopped. A report that does not
	// confirm a stop moves no status.
	Stopped bool
	// Reason is the readable reason the executor observed.
	Reason string
}

// ErrCancelNotRequested reports a confirmed stop with no cancellation request open for
// that task, so the terminal kind is not the one to record here.
var ErrCancelNotRequested = errors.New("task: no cancellation request is open for this task")

// RequestCancel records one cancellation request and returns its control receipt. A
// repeated request returns the same receipt and records no second one; a task that
// already holds its terminal fact refuses the request.
func (r *MemoryRegistry) RequestCancel(ctx context.Context, taskID event.TaskID) (CancelReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return CancelReceipt{TaskID: taskID}, ErrTaskNotFound
	}
	if isTerminal(entry.status) {
		return CancelReceipt{TaskID: taskID}, ErrTerminalState
	}
	entry.cancelRequested = true
	return CancelReceipt{TaskID: taskID, Requested: true}, nil
}

// Cancel requests cancellation of the named task only; it does not cancel other
// invocations of the same session.
func (r *MemoryRegistry) Cancel(ctx context.Context, taskID event.TaskID) error {
	_, err := r.RequestCancel(ctx, taskID)
	return err
}

// RecordStop records the executor's stop evidence beside one task. A report that
// confirms no stop is recorded and moves no status; a confirmed stop of a task under a
// cancellation request stores the cancelled terminal fact beside its pending
// notification and closes the task's execution obligation with the reported reason.
func (m *MemoryManager) RecordStop(ctx context.Context, evidence StopEvidence) error {
	registry := m.registry
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry, ok := registry.tasks[evidence.TaskID]
	if !ok {
		return ErrTaskNotFound
	}
	entry.stops = append(entry.stops, evidence)
	if !evidence.Stopped {
		return nil
	}
	if !entry.cancelRequested {
		return ErrCancelNotRequested
	}
	notice := Notice{
		TaskID: evidence.TaskID,
		Round:  entry.round,
		Kind:   NoticeCancelled,
		Seq:    0,
	}
	record := CommitRecord{
		MutationID:       MutationID("stop:" + string(evidence.TaskID)),
		TaskID:           evidence.TaskID,
		ExpectedRevision: entry.revision,
		Status:           StatusCancelled,
		Outbox:           []Envelope{entry.envelope(notice)},
	}
	if result, known, err := entry.repeatOf(record); known {
		if result != CommitCommitted {
			return err
		}
		return nil
	}
	result, applied, err := registry.applyCommitLocked(ctx, entry, record)
	if result != CommitCommitted {
		if err == nil {
			err = ErrCommitUncertain
		}
		return err
	}
	if applied {
		entry.closeExecutionObligation(evidence.Reason)
	}
	return nil
}

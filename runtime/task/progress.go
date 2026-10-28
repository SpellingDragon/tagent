// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
//
// This file holds the progress read of a task. Progress is a fact a running task
// reports, not a state it is moved to: the report travels as a NoticeProgress in
// the outbox of the fact that carries it, so the committed chain already answers
// whether progress was reported. ProgressReported derives that answer from the
// chain, which is what makes it the same answer for a registry that watched the
// task and for one that rebuilt it from the chain — a stored status could not
// make that promise, because the write had to be kept in step with the fact and
// a rebuild could only see one of the two.
//
// 红线: 派生读只读已提交事实：不写 status、revision、mutation 或 obligation，反复读与只读一次同效。
package task

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
)

// replayedStatus reports the status a stored fact stands for when the chain is
// folded back. A fact written while progress was still a status stores the
// reserved ordinal, so the fold reads it as the running phase it describes; the
// progress notification that fact carried stays on the chain, so the task still
// answers progress through ProgressReported. Every other ordinal replays as
// itself.
//
// The fold is where a stored status is read back, so the mapping lives there
// rather than in the decoder: DecodeTaskRecord keeps the bytes as they were
// stored, and a chain is only ever read as a task through the fold.
func replayedStatus(status Status) Status {
	if status == statusProgressRetired {
		return StatusRunning
	}
	return status
}

// ProgressReported reports whether the task stands in the running phase with
// progress reported beside it: the task holds no terminal fact, its status is
// StatusRunning, and a fact on its chain carries a NoticeProgress.
//
// The read covers the whole committed chain and never only the current round.
// The round a task is at is not readable from the chain alone — folding a chain
// advances the round past the notices it carries — so a round-scoped read would
// answer differently for a live task and for the task restored from its own
// chain. The chain-wide read is the one both faces agree on.
func (r *MemoryRegistry) ProgressReported(ctx context.Context, taskID event.TaskID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return false, ErrTaskNotFound
	}
	return entry.progressReported(), nil
}

// progressReported derives the progress read from the facts the task stores. The
// caller holds the registry lock. Only the running phase reports progress: a
// settled, failed or cancelled task closed its round, and a task parked in the
// approval wait executes nothing, so the notifications their chains still carry
// are history a reader walks rather than a phase the task stands in.
func (t *memoryTask) progressReported() bool {
	if t.status != StatusRunning {
		return false
	}
	for _, record := range t.mutations {
		for _, envelope := range record.Outbox {
			if envelope.Notice.Kind == NoticeProgress {
				return true
			}
		}
	}
	return false
}

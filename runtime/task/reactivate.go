package task

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
//
// ErrReactivationFailed reports a reactivation refused for a failed task. A
// failed task is re-entered by relaunching it under a fresh identity: its
// terminal fact is kept, and re-entering the same task in place would overwrite
// an outcome the chain already holds.
var ErrReactivationFailed = errors.New("task: a failed task is re-entered by relaunch under a fresh identity")

// ErrReactivationCancelled reports a reactivation refused for a cancelled task.
// A cancelled task is re-entered by re-spawning it under a fresh identity: the
// cancellation is the caller's own decision, and re-entering the same task in
// place would answer it with a round nobody asked for.
var ErrReactivationCancelled = errors.New("task: a cancelled task is re-entered by re-spawn under a fresh identity")

// ErrReactivationRollback reports a rollback naming no round of that task in
// flight, so there is nothing of that re-entry left to withdraw. The rollback is
// refused by name and moves nothing.
var ErrReactivationRollback = errors.New("task: the task holds no round of that re-entry in flight")

// reactivatePrefix is the mutation identity prefix of one reactivation fact. The
// identity names the mutation the fact records — a task re-entered from its
// settled outcome — so the chain carries the settled-to-reactivated transition
// without a report field of its own: the link before it holds the settled status
// and this link holds the active one.
const reactivatePrefix = "reactivate:"

// rollbackPrefix is the mutation identity prefix of the compensating fact one
// aborted reactivation records. The identity names the mutation the fact records —
// a round put back to the settled outcome it was re-entered from because nothing
// served it — so the chain carries the reactivated-to-settled transition and a
// reader replays the task at the outcome it really holds.
const rollbackPrefix = "reactivate-rollback:"

// Reactivate re-enters a settled task as a new execution round and reports the
// round it opened. The task keeps its identity: the round that opens is the next
// one of the same task, so the notification its settlement carries is a new
// identity rather than a repeat of the round before it.
//
// Only a settled task is re-entered. A failed or a cancelled task holds a
// terminal outcome that re-entering in place would overwrite, so each is refused
// by name beside the relaunch or re-spawn that answers it; a task whose round is
// still in flight is refused by name beside the round already running. The
// transition is committed on the commit port with the revision it applies to, so
// two reactivations racing on one task commit one round: the first moves the
// task out of its settled status and the second is refused by name.
func (r *MemoryRegistry) Reactivate(ctx context.Context, taskID event.TaskID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return 0, ErrTaskNotFound
	}
	if err := entry.reentrant(); err != nil {
		return 0, err
	}
	record := CommitRecord{
		MutationID:       MutationID(reactivatePrefix + string(taskID) + ":" + strconv.FormatInt(reactivationSeq(entry)+1, 10)),
		TaskID:           taskID,
		ExpectedRevision: entry.revision,
		Status:           StatusRunning,
	}
	result, applied, err := r.applyCommitLocked(ctx, entry, record)
	if result != CommitCommitted {
		if err == nil {
			err = ErrCommitUncertain
		}
		return 0, err
	}
	if applied {
		entry.round++
		entry.ttlAnchor = entry.changedAt
		entry.reported = LogicalActive
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: ObligationAwaitingTool, TaskID: taskID},
		})
	}
	return entry.round, nil
}

// Reentrant reports whether the task may be re-entered, without moving it: the
// settled status design D7 admits, and the named refusal of every status it does
// not. A caller that settles more than one precondition before it opens a round
// reads this first, so the round it opens is the only thing it has to put back
// when a later precondition turns out to be gone.
func (r *MemoryRegistry) Reentrant(ctx context.Context, taskID event.TaskID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	return entry.reentrant()
}

// reentrant reports the refusal one status answers a re-entry with, and nil for
// the settled outcome a re-entry starts from.
func (t *memoryTask) reentrant() error {
	switch t.status {
	case StatusSettled:
		return nil
	case StatusFailed:
		return ErrReactivationFailed
	case StatusCancelled:
		return ErrReactivationCancelled
	case StatusUnknown:
		return ErrStatusUnknown
	default:
		return ErrRoundInFlight
	}
}

// RollbackReactivation puts one task back to the settled outcome the reactivation
// of that round re-entered it from, because nothing served the round it opened.
//
// The rollback exists for the re-entry that opened a round and then found a later
// precondition gone: without it the task keeps a round in flight that no turn will
// ever settle, so every later re-entry is refused by name until the task expires.
// The compensating fact is committed on the commit port before it becomes visible,
// like every other transition, so the chain replays the task at the outcome it
// holds rather than at the round nobody served. The obligation the reactivation
// opened is withdrawn with it, so a task put back is read as settled and owes
// nothing.
//
// - Only a round reactivation really opened is put back: a task holding another round, or no round in flight at all, is refused by name and moves nothing.
// - The round number stays consumed — a round identity is never reused, so the notification identities of the attempts that followed keep their own — and the re-entry deadline keeps counting from the reactivation, which reads no differently for a task that holds its terminal fact.
func (r *MemoryRegistry) RollbackReactivation(ctx context.Context, taskID event.TaskID, round int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if entry.status != StatusRunning || entry.round != round {
		return fmt.Errorf("%w: %s round %d", ErrReactivationRollback, taskID, round)
	}
	record := CommitRecord{
		MutationID:       MutationID(rollbackPrefix + string(taskID) + ":" + strconv.FormatInt(round, 10)),
		TaskID:           taskID,
		ExpectedRevision: entry.revision,
		Status:           StatusSettled,
	}
	result, applied, err := r.applyCommitLocked(ctx, entry, record)
	if result != CommitCommitted {
		if err == nil {
			err = ErrCommitUncertain
		}
		return err
	}
	if applied {
		entry.closeExecutionObligation("")
	}
	return nil
}

// reactivationSeq reports how many reactivation facts one task already stores. The
// count is the sequence the next reactivation identity carries, so one task may be
// re-entered after each settlement of it and every round keeps its own fact.
func reactivationSeq(entry *memoryTask) int64 {
	count := int64(0)
	for mutation := range entry.mutations {
		if len(mutation) >= len(reactivatePrefix) && string(mutation[:len(reactivatePrefix)]) == reactivatePrefix {
			count++
		}
	}
	return count
}

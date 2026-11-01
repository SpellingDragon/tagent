package task

import (
	"context"
	"errors"
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

// reactivatePrefix is the mutation identity prefix of one reactivation fact. The
// identity names the mutation the fact records — a task re-entered from its
// settled outcome — so the chain carries the settled-to-reactivated transition
// without a report field of its own: the link before it holds the settled status
// and this link holds the active one.
const reactivatePrefix = "reactivate:"

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
	switch entry.status {
	case StatusSettled:
	case StatusFailed:
		return 0, ErrReactivationFailed
	case StatusCancelled:
		return 0, ErrReactivationCancelled
	case StatusUnknown:
		return 0, ErrStatusUnknown
	default:
		return 0, ErrRoundInFlight
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

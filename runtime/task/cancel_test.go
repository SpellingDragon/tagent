package task

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestRestartRecordStopServesASettledTaskFromItsChain pins the stop report against the account a restart keeps.
// - positive: RecordStop records the report beside a finished task the checkpoint never stated, and moves no status
// - a confirmed stop with no cancellation request open answers ErrCancelNotRequested rather than an unknown task
// - negative: an identity no chain holds answers ErrTaskNotFound
// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
func TestRestartRecordStopServesASettledTaskFromItsChain(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	taskID := restartSettled(t, source, "call-stop-restart", "dedup-stop-restart")
	restored := restartAccount(t, source, commit, taskID)
	restartUnheld(t, restored, taskID)

	manager := NewMemoryManager(restored, nil)
	if err := manager.RecordStop(ctx, StopEvidence{TaskID: taskID, Stopped: false, Reason: "the executor observed no stop"}); err != nil {
		t.Fatalf("the restart read the settled task as %v, want the report recorded beside its chain", err)
	}
	if err := manager.RecordStop(ctx, StopEvidence{TaskID: taskID, Stopped: true, Reason: "the executor stopped"}); !errors.Is(err, ErrCancelNotRequested) {
		t.Fatalf("a confirmed stop of the finished task = %v, want %v", err, ErrCancelNotRequested)
	}
	if _, status, err := restored.Lookup(ctx, taskID); err != nil || status != StatusSettled {
		t.Fatalf("status after the reports = (%v, %v), want the settled outcome the record holds", status, err)
	}
	restored.mu.Lock()
	stops := len(restored.tasks[taskID].stops)
	restored.mu.Unlock()
	if stops != 2 {
		t.Fatalf("the record holds %d stop report(s), want the two the executor filed beside it", stops)
	}

	stranger := event.TaskID("task-no-chain-holds")
	if err := manager.RecordStop(ctx, StopEvidence{TaskID: stranger, Stopped: true}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("RecordStop of an identity no chain holds = %v, want %v", err, ErrTaskNotFound)
	}
}

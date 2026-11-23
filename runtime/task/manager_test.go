package task

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// restartAccount restores the newest checkpoint of one source registry into a
// fresh registry over the same commit, with the chains of the named tasks
// readable by identity, the way a boot reads the account a run left behind.
func restartAccount(t *testing.T, source *MemoryRegistry, commit *MemoryCommit, ids ...event.TaskID) *MemoryRegistry {
	t.Helper()
	ctx := context.Background()
	payload, err := source.SnapshotCheckpoint(ctx)
	if err != nil {
		t.Fatalf("state the account: %v", err)
	}
	specs := make(map[event.TaskID]Spec, len(ids))
	for _, taskID := range ids {
		specs[taskID] = source.tasks[taskID].spec
	}
	restored := NewMemoryRegistry(commit, NewMemoryAdmission())
	restored.RestoreChainSource(chainOf{commit: commit, specs: specs})
	if err := restored.RestoreCheckpoint(ctx, payload); err != nil {
		t.Fatalf("put the account back: %v", err)
	}
	return restored
}

// restartSettled admits one task and settles it with no notification owed, so the
// finished task states no checkpoint entry and its record stays on the chain alone.
func restartSettled(t *testing.T, registry *MemoryRegistry, callID, dedup string) event.TaskID {
	t.Helper()
	ctx := context.Background()
	taskID := invariantRegister(t, registry, callID, dedup)
	if result, err := registry.Advance(ctx, taskID, StatusRunning, MutationID("run-"+callID), nil); err != nil || result != CommitCommitted {
		t.Fatalf("move the task of %s under way = (%v, %v), want a committed change", callID, result, err)
	}
	if result, err := registry.Advance(ctx, taskID, StatusSettled, MutationID("end-"+callID), nil); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task of %s = (%v, %v), want a committed change", callID, result, err)
	}
	return taskID
}

// restartUnheld pins that the restored registry holds no record of one task yet,
// so the face under test answers it from the chain rather than from a resident
// record the restore already materialized.
func restartUnheld(t *testing.T, restored *MemoryRegistry, taskID event.TaskID) {
	t.Helper()
	restored.mu.Lock()
	_, held := restored.tasks[taskID]
	restored.mu.Unlock()
	if held {
		t.Fatalf("the restored registry holds the finished task %s before any reader names it, want its record left on the chain", taskID)
	}
}

// TestRestartObligationsReadASettledTaskFromItsChain pins the obligation report against the account a restart keeps.
// - positive: Obligations answers a finished task by identity alone, with no lookup materializing the record first
// - the report equals the one the live registry gave before the cut
// - negative: an identity no chain holds answers ErrTaskNotFound
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRestartObligationsReadASettledTaskFromItsChain(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	taskID := restartSettled(t, source, "call-obligations-restart", "dedup-obligations-restart")
	want, err := NewMemoryManager(source, nil).Obligations(ctx, taskID)
	if err != nil {
		t.Fatalf("read the obligations of the live settled task: %v", err)
	}

	restored := restartAccount(t, source, commit, taskID)
	restartUnheld(t, restored, taskID)
	got, err := NewMemoryManager(restored, nil).Obligations(ctx, taskID)
	if err != nil {
		t.Fatalf("the restart read the settled task as %v, want the report its chain holds", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the restart reported %+v, want the %+v the live registry gave", got, want)
	}

	stranger := event.TaskID("task-no-chain-holds")
	if _, err := NewMemoryManager(restored, nil).Obligations(ctx, stranger); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Obligations of an identity no chain holds = %v, want %v", err, ErrTaskNotFound)
	}
}

// TestRestartHandoffMovesASettledTaskFromItsChain pins the routing move against the account a restart keeps.
// - positive: Handoff records the move beside a finished task the checkpoint never stated, and the chain stores the link
// - a repeat of the handoff already recorded applies nothing a second time
// - negative: an identity no chain holds answers ErrTaskNotFound
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRestartHandoffMovesASettledTaskFromItsChain(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	taskID := restartSettled(t, source, "call-handoff-move-restart", "dedup-handoff-move-restart")
	restored := restartAccount(t, source, commit, taskID)
	restartUnheld(t, restored, taskID)

	manager := NewMemoryManager(restored, nil)
	target := HandoffTarget{Target: event.Address{AgentID: "agent-b", SessionID: "session-b"}, TargetInstance: "instance-b"}
	if err := manager.Handoff(ctx, taskID, target); err != nil {
		t.Fatalf("the restart moved the settled task beside %v, want the handoff its chain admits", err)
	}
	facts := commit.Facts(taskID)
	if len(facts) == 0 || !strings.HasPrefix(string(facts[len(facts)-1].MutationID), handoffPrefix) {
		t.Fatalf("chain of %s carries %+v, want the handoff link the move recorded", taskID, facts)
	}
	stored := len(commit.Facts(taskID))
	if err := manager.Handoff(ctx, taskID, target); err != nil {
		t.Fatalf("repeat the recorded handoff: %v", err)
	}
	if got := len(commit.Facts(taskID)); got != stored {
		t.Fatalf("facts after the repeated handoff = %d, want the stored ones alone", got)
	}

	stranger := event.TaskID("task-no-chain-holds")
	if err := manager.Handoff(ctx, stranger, target); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Handoff of an identity no chain holds = %v, want %v", err, ErrTaskNotFound)
	}
}

// TestRestartFlushServesASettledTaskFromItsChain pins the flush against the account a restart keeps.
// - positive: Flush answers a finished task the checkpoint never stated, and offers nothing: the record owes no delivery
// - negative: an identity no chain holds answers ErrTaskNotFound
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestRestartFlushServesASettledTaskFromItsChain(t *testing.T) {
	ctx := context.Background()
	source, commit := invariantRegistry(t)
	taskID := restartSettled(t, source, "call-flush-restart", "dedup-flush-restart")
	restored := restartAccount(t, source, commit, taskID)
	restartUnheld(t, restored, taskID)

	offered := 0
	manager := NewMemoryManager(restored, func(ctx context.Context, notice Notice) (DeliveryAcceptance, error) {
		offered++
		return DeliveryAccepted, nil
	})
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("the restart flushed the settled task beside %v, want the flush its chain admits", err)
	}
	if offered != 0 {
		t.Fatalf("the flush offered %d notification(s), want none: the finished task owes no delivery", offered)
	}
	restored.mu.Lock()
	_, held := restored.tasks[taskID]
	restored.mu.Unlock()
	if !held {
		t.Fatalf("the flush left the finished task unheld, want the record it read back kept beside the registry")
	}

	stranger := event.TaskID("task-no-chain-holds")
	if err := manager.Flush(ctx, stranger); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Flush of an identity no chain holds = %v, want %v", err, ErrTaskNotFound)
	}
}

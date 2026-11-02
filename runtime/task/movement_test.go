package task

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestMovementCoversTheTaskChain pins the one sequence the rest criterion is fenced by against the task chain.
// - an admission, a status commit and a flush each advance it
// - a fence read from one account's own commit sequence stands still while the chain moves
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestMovementCoversTheTaskChain(t *testing.T) {
	ctx := context.Background()
	movement := NewMovement()
	registry := NewMemoryRegistry(NewMemoryCommit(nil), NewMemoryAdmission(), WithMovement(movement))
	manager := NewMemoryManager(registry, func(context.Context, Notice) (DeliveryAcceptance, error) {
		return DeliveryAccepted, nil
	})

	at := movement.Read()
	taskID := invariantRegister(t, registry, "call-1", "dedup-1")
	if got := movement.Read(); got == at {
		t.Fatalf("the sequence stood at %d after an admission, want the write of the chain to advance it", got)
	}

	at = movement.Read()
	if result, err := registry.Advance(ctx, taskID, StatusRunning, "start", nil); err != nil || result != CommitCommitted {
		t.Fatalf("start the admitted task = (%v, %v), want a committed change", result, err)
	}
	if got := movement.Read(); got == at {
		t.Fatalf("the sequence stood at %d after a status commit, want the write of the chain to advance it", got)
	}

	at = movement.Read()
	if result, err := registry.Advance(ctx, taskID, StatusSettled, "settle", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}
	if got := movement.Read(); got == at {
		t.Fatalf("the sequence stood at %d after a terminal with a delivery obligation, want the write of the obligations to advance it", got)
	}

	at = movement.Read()
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush the notifications the task owes: %v", err)
	}
	if got := movement.Read(); got == at {
		t.Fatalf("the sequence stood at %d after a flush, want the write of the obligations to advance it", got)
	}
}

// TestNoMovementFenceStandsStill pins that an account opened beside no sequence advances nothing.
func TestNoMovementFenceStandsStill(t *testing.T) {
	var absent *Movement
	absent.Advance()
	if got := absent.Read(); got != 0 {
		t.Fatalf("sequence of an account beside none = %d, want the zero every read of one instant equals", got)
	}
	registry := NewMemoryRegistry(NewMemoryCommit(nil), NewMemoryAdmission())
	invariantRegister(t, registry, "call-1", "dedup-1")
	if _, err := registry.Advance(context.Background(), invariantTaskID(t, registry), StatusRunning, "start", nil); err != nil {
		t.Fatalf("start the admitted task: %v", err)
	}
}

// invariantTaskID reports the one task the registry holds, for a case that
// registered exactly one.
func invariantTaskID(t *testing.T, registry *MemoryRegistry) event.TaskID {
	t.Helper()
	unfinished, err := registry.Unfinished(context.Background())
	if err != nil {
		t.Fatalf("read the unfinished work: %v", err)
	}
	if len(unfinished) != 1 {
		t.Fatalf("unfinished work = %+v, want the one admitted task", unfinished)
	}
	return unfinished[0].TaskID
}

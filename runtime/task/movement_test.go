package task

import (
	"context"
	"testing"
)

// TestRegistryOwnsTheMovementSequence pins the task account's own sequence beside the account it fences.
// - the registry builds its own sequence: no option wires one in from outside
// - an admission, a status commit and a flush each advance that account's own sequence
// - a judgment reads the account's sequence instead of a mechanism kept beside it
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestRegistryOwnsTheMovementSequence(t *testing.T) {
	ctx := context.Background()
	registry := NewMemoryRegistry(NewMemoryCommit(nil), NewMemoryAdmission())
	manager := NewMemoryManager(registry, func(context.Context, Notice) (DeliveryAcceptance, error) {
		return DeliveryAccepted, nil
	})

	if got := registry.Load(); got != 0 {
		t.Fatalf("the sequence of a fresh registry = %d, want the first position the account builds", got)
	}
	at := registry.Load()
	taskID := invariantRegister(t, registry, "call-1", "dedup-1")
	if got := registry.Load(); got == at {
		t.Fatalf("the sequence stood at %d after an admission, want the write of the account to advance it", got)
	}

	at = registry.Load()
	if result, err := registry.Advance(ctx, taskID, StatusRunning, "start", nil); err != nil || result != CommitCommitted {
		t.Fatalf("start the admitted task = (%v, %v), want a committed change", result, err)
	}
	if got := registry.Load(); got == at {
		t.Fatalf("the sequence stood at %d after a status commit, want the write of the account to advance it", got)
	}

	at = registry.Load()
	if result, err := registry.Advance(ctx, taskID, StatusSettled, "settle", []Envelope{invariantEnvelope(taskID, NoticeSettled, 0)}); err != nil || result != CommitCommitted {
		t.Fatalf("settle the task = (%v, %v), want a committed change", result, err)
	}
	if got := registry.Load(); got == at {
		t.Fatalf("the sequence stood at %d after a terminal with a delivery obligation, want the write of the obligations to advance it", got)
	}

	at = registry.Load()
	if err := manager.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush the notifications the task owes: %v", err)
	}
	if got := registry.Load(); got == at {
		t.Fatalf("the sequence stood at %d after a flush, want the write of the obligations to advance it", got)
	}
}

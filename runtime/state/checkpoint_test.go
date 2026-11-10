// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
package state

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// recordingHolder is a state holder that records every payload a restore hands it.
type recordingHolder struct {
	name     string
	restored []string
}

func (h *recordingHolder) StateName() string { return h.name }

func (h *recordingHolder) SnapshotState(context.Context) ([]byte, error) {
	return []byte(emptyStateJSON), nil
}

func (h *recordingHolder) RestoreState(_ context.Context, payload []byte) error {
	h.restored = append(h.restored, string(payload))
	return nil
}

// TestPendingRestoreAccumulatesEveryCarrierOfAName pins the per-session carrier restore.
// - two carriers state a slice of the same name before the holder mounts; the holder restores both in order, not only the last
// - revert pending to one payload per name and the first session's slice is lost (the kept payload never restored)
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestPendingRestoreAccumulatesEveryCarrierOfAName(t *testing.T) {
	ctx := context.Background()
	registry := NewStateRegistry()
	for _, payload := range []string{`{"session":"kept"}`, `{"session":"other"}`} {
		if err := registry.RestoreState(ctx, []event.CheckpointSlice{{Name: "delivery", Payload: []byte(payload)}}); err != nil {
			t.Fatalf("restore a carrier slice: %v", err)
		}
	}
	if pending := registry.PendingRestore(); len(pending) != 1 || pending[0] != "delivery" {
		t.Fatalf("pending = %v, want the one delivery name awaiting its holder", pending)
	}
	holder := &recordingHolder{name: "delivery"}
	dispose, err := registry.Register(holder)
	if err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	defer dispose()
	if len(holder.restored) != 2 || holder.restored[0] != `{"session":"kept"}` || holder.restored[1] != `{"session":"other"}` {
		t.Fatalf("the holder restored %v, want both carrier slices in order (kept then other)", holder.restored)
	}
	if pending := registry.PendingRestore(); len(pending) != 0 {
		t.Fatalf("pending = %v after the holder mounted, want every slice restored", pending)
	}
}

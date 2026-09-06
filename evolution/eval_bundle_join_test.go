package evolution

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	tagentevent "github.com/SpellingDragon/tagent/event"
)

// TestEvidence_BundleJoin is the D1-B regression (design-report-closeout):
// Evidence collection joins events to the target bundle via the bundle_id
// Metadata stamp first; untagged events fall back to the time-window
// attribution. Events stamped for a DIFFERENT bundle must not leak into this
// bundle's evidence.
func TestEvidence_BundleJoin(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := 1
	now := time.Now().UnixMilli()
	// Keys MUST be real Snowflake ids: GetEvents derives the partition from
	// the key itself (PartitionIDFromEventKey), so bare ints would miss.
	put := func(seq int64, bundleID, subtype string) {
		key := memory.NewSnowflakeEventKey(pid, now+seq)
		md := map[string]string{tagentevent.MetaKeySubtype: subtype}
		if bundleID != "" {
			md[tagentevent.MetaKeyBundleID] = bundleID
		}
		if err := store.StoreEvent(key, memory.FullEvent{
			EventKey: key, PartitionID: pid, EventType: tagentevent.TypeGovernance,
			Timestamp: now + seq, Metadata: md,
		}); err != nil {
			t.Fatal(err)
		}
	}
	put(1, "b1", tagentevent.SubtypeDenial) // b1's denial
	put(2, "b2", tagentevent.SubtypeDenial) // b2's denial — must not leak into b1
	put(3, "", tagentevent.SubtypeDenial)   // untagged → window fallback, counted

	src := NewStoreEvidenceSource(store, pid, time.Hour)

	ev1, err := src.Collect(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	if ev1.DenialCount != 2 { // 101 (exact) + 103 (fallback)
		t.Fatalf("b1 DenialCount = %d, want 2 (exact join + window fallback)", ev1.DenialCount)
	}

	ev2, err := src.Collect(context.Background(), "b2")
	if err != nil {
		t.Fatal(err)
	}
	if ev2.DenialCount != 2 { // 102 (exact) + 103 (fallback)
		t.Fatalf("b2 DenialCount = %d, want 2", ev2.DenialCount)
	}
}

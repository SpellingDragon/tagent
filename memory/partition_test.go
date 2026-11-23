package memory

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestPartitionIdentityIsOneStableMapping pins one name-to-partition mapping whose cache is never the source of truth.
// - a derived partition always lands inside the ten-bit range
// - dropping a name's cache entry never changes the value that name derives
// - a batch of names re-derives to exactly the values the cached path served
// 契约: docs/wiki/memory/memory-architecture.md#partitionidfromname
func TestPartitionIdentityIsOneStableMapping(t *testing.T) {
	const name = "partition:invariant:one"
	first := PartitionIDFromName(name)
	if first < 0 || first >= 1<<PartitionBits {
		t.Fatalf("partition of %q = %d, want a value inside the ten-bit range", name, first)
	}
	partitionNames.Delete(name)
	if rederived := PartitionIDFromName(name); rederived != first {
		t.Fatalf("partition of %q after dropping its cache entry = %d, want the %d derived first", name, rederived, first)
	}

	names := make([]string, 0, 32)
	for i := 0; i < 32; i++ {
		names = append(names, fmt.Sprintf("partition:invariant:%02d", i))
	}
	derived := make(map[string]int, len(names))
	for _, one := range names {
		derived[one] = PartitionIDFromName(one)
		if derived[one] < 0 || derived[one] >= 1<<PartitionBits {
			t.Fatalf("partition of %q = %d, want a value inside the ten-bit range", one, derived[one])
		}
	}
	for _, one := range names {
		partitionNames.Delete(one)
	}
	for _, one := range names {
		if again := PartitionIDFromName(one); again != derived[one] {
			t.Fatalf("partition of %q re-derived = %d, want the %d the cached path served", one, again, derived[one])
		}
	}
}

// TestOneKeyFilesUnderOnePartition pins the key rule: one committed key names one position, and a replay elsewhere is refused instead of filed twice.
// - a repeat of the committed record is idempotent and adds no second event
// - the same key under another partition is refused by name
// - the same key under another window or sequence slot is refused by name
// - the same key with other bytes is refused by name
// - the published position stays the one the first commit took
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestOneKeyFilesUnderOnePartition(t *testing.T) {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the in-memory tier: %v", err)
	}
	committed := ledger.SegmentRecord{
		PartitionID: 0, WindowTS: 10, Seq: 1, EventKey: "ea01", EventTime: 10, Body: []byte(`{"probe":"one"}`),
	}
	if err := tier.Segments.StoreEvent(ctx, committed); err != nil {
		t.Fatalf("commit the first record: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, committed); err != nil {
		t.Fatalf("repeat the committed record: %v", err)
	}
	if count := tier.Segments.EventCount(); count != 1 {
		t.Fatalf("event count after a repeat = %d, want the one committed event", count)
	}

	refused := map[string]ledger.SegmentRecord{
		"another partition": {PartitionID: 1, WindowTS: 10, Seq: 1, EventKey: "ea01", EventTime: 10, Body: committed.Body},
		"another window":    {PartitionID: 0, WindowTS: 11, Seq: 1, EventKey: "ea01", EventTime: 11, Body: committed.Body},
		"another slot":      {PartitionID: 0, WindowTS: 10, Seq: 2, EventKey: "ea01", EventTime: 10, Body: committed.Body},
		"other bytes":       {PartitionID: 0, WindowTS: 10, Seq: 1, EventKey: "ea01", EventTime: 10, Body: []byte(`{"probe":"two"}`)},
	}
	for name, record := range refused {
		if err := tier.Segments.StoreEvent(ctx, record); !errors.Is(err, ledger.ErrContentConflict) {
			t.Fatalf("committing the same key under %s = %v, want ledger.ErrContentConflict", name, err)
		}
	}
	if count := tier.Segments.EventCount(); count != 1 {
		t.Fatalf("event count after refused commits = %d, want the one committed event", count)
	}
	stored, held, err := tier.Segments.GetEvent(ctx, "ea01")
	if err != nil || !held {
		t.Fatalf("read the committed key = (held %v, %v), want it published", held, err)
	}
	if stored.PartitionID != 0 || stored.WindowTS != 10 || stored.Seq != 1 || string(stored.Body) != `{"probe":"one"}` {
		t.Fatalf("published position = %+v, want the partition, window, slot and bytes of the first commit", stored)
	}
}

// TestPartitionsOfLeadsWithTheAgentPartition pins the partition set one agent may read across.
// - the agent's own namespace leads the set
// - declared reads follow in declaration order
// - repeated reads, the own namespace repeated and blank reads are dropped
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestPartitionsOfLeadsWithTheAgentPartition(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    string
		extra []string
		want  []string
	}{
		{name: "own partition only", id: "main", want: []string{"main"}},
		{name: "declared reads follow in order", id: "main", extra: []string{"audit", "ops"}, want: []string{"main", "audit", "ops"}},
		{name: "the own partition is not repeated", id: "main", extra: []string{"main", "audit"}, want: []string{"main", "audit"}},
		{name: "repeated reads collapse", id: "main", extra: []string{"audit", "audit"}, want: []string{"main", "audit"}},
		{name: "blank reads are dropped", id: "main", extra: []string{" ", "audit"}, want: []string{"main", "audit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PartitionsOf(tc.id, tc.extra); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("PartitionsOf(%q, %v) = %v, want %v", tc.id, tc.extra, got, tc.want)
			}
		})
	}
}

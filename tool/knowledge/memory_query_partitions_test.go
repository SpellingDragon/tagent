package knowledge

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// TestQueryHistoricalKnowledge_PartitionScoped locks the memory_query
// partition wiring (existing-defect cleanup, 2026-08-26): on partition-isolated
// stores (FileSegmentStore) a query without PartitionIDs scans nothing, so
// queryHistoricalKnowledge must scope to the injected readable partitions —
// matches the recall fix (own namespace first + read_namespaces).
func TestQueryHistoricalKnowledge_PartitionScoped(t *testing.T) {
	dir := t.TempDir()
	kv, err := memory.NewLocalFileKV(dir)
	if err != nil {
		t.Fatalf("local kv: %v", err)
	}
	rel, err := memory.NewInMemRelationStore(dir)
	if err != nil {
		t.Fatalf("relation store: %v", err)
	}
	store, err := memory.NewFileSegmentStore(kv, rel, dir, 100)
	if err != nil {
		t.Fatalf("segment store: %v", err)
	}

	pid := memory.PartitionIDFromName("knowledge")
	other := memory.PartitionIDFromName("unrelated")
	key := memory.NewSnowflakeEventKey(pid, 0)
	if err := store.StoreEvent(int64(pid), memory.FullEvent{
		EventKey:     key,
		PartitionID:  pid,
		EventType:    "agent_output",
		EventSummary: "Go 并发模式知识沉淀",
		Content:      "goroutine + channel",
		Timestamp:    1710000000000,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Readable partition includes the event's partition → hit.
	got := queryHistoricalKnowledge(store, []int{pid}, "Go 并发")
	if len(got) != 1 {
		t.Fatalf("expected 1 hit with own partition, got %d", len(got))
	}

	// Unrelated partition only → no hit (isolation holds).
	if got := queryHistoricalKnowledge(store, []int{other}, "Go 并发"); len(got) != 0 {
		t.Fatalf("expected 0 hits with unrelated partition, got %d", len(got))
	}
}

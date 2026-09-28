package knowledge

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
)

// TestQueryHistoricalKnowledge_PartitionScoped 钉住 memory_query 的分区接线：在按分区隔离的
//
// 契约: docs/wiki/tool/tool-architecture.md#memory-query-hard
func TestQueryHistoricalKnowledge_PartitionScoped(t *testing.T) {
	dir := t.TempDir()
	kv, err := kv.NewLocalFileKV(dir)
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

	got := queryHistoricalKnowledge(store, []int{pid}, "Go 并发")
	if len(got) != 1 {
		t.Fatalf("expected 1 hit with own partition, got %d", len(got))
	}

	if got := queryHistoricalKnowledge(store, []int{other}, "Go 并发"); len(got) != 0 {
		t.Fatalf("expected 0 hits with unrelated partition, got %d", len(got))
	}
}

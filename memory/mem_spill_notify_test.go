package memory_test

import (
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// TestMemSpillReplay_AppendsProjection 钉住
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestMemSpillReplay_AppendsProjection(t *testing.T) {
	dir := t.TempDir()
	spill := memory.NewMemSpill(filepath.Join(dir, "spill.jsonl"))
	inner := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")

	k1 := memory.NewSnowflakeEventKey(pid, 1750000000000)
	k2 := memory.NewSnowflakeEventKey(pid, 1750000001000)
	if err := spill.Append(k1, memory.FullEvent{EventKey: k1, PartitionID: pid, EventType: "external_input"}); err != nil {
		t.Fatal(err)
	}
	if err := spill.Append(k2, memory.FullEvent{EventKey: k2, PartitionID: pid, EventType: "agent_output"}); err != nil {
		t.Fatal(err)
	}
	_ = inner.StoreEvent(k2, memory.FullEvent{EventKey: k2, PartitionID: pid, EventType: "agent_output"})

	notified := map[int64]bool{}
	n, err := spill.ReplayWithNotify(inner, func(ev memory.FullEvent) { notified[ev.EventKey] = true })
	if err != nil || n != 2 {
		t.Fatalf("replay n=%d err=%v", n, err)
	}
	if !notified[k1] || !notified[k2] {
		t.Fatalf("notify must fire for both fresh replay and idempotent hit: %v", notified)
	}
	if n, err := spill.ReplayWithNotify(inner, nil); err != nil || n != 0 {
		t.Fatalf("empty replay n=%d err=%v", n, err)
	}
}

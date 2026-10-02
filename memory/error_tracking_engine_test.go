package memory_test

import (
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/engine"
)

// TestErrorTrackingStore_OptionalInterfacePassthrough 本文件是记忆存储行为的测试执行体。
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestErrorTrackingStore_OptionalInterfacePassthrough(t *testing.T) {
	inner := memory.NewInMemoryStore()
	eng := engine.NewInMemoryEngine(inner, membed.NewMockEmbedder(16), engine.EngineConfig{})
	defer eng.Close()
	bridge := engine.NewEngineBridge(inner, eng)
	// 下游拿到的是 MemoryStore 接口（memStore），故赋给接口再断言可选能力穿透。
	var ms memory.MemoryStore = memory.NewErrorTrackingStore(bridge, nil)

	ep, ok := ms.(memory.MemoryEngineProvider)
	if !ok || ep.MemoryEngine() == nil {
		t.Fatal("ErrorTrackingStore 应透传 MemoryEngineProvider（否则 recall hybrid 失效）")
	}
	c, ok := ms.(interface{ Close() error })
	if !ok {
		t.Fatal("ErrorTrackingStore 应透传 Close（agent.Closer 引擎回收）")
	}
	_ = c.Close()
}

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

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/kv"
)

func waitForKVKeys(t *testing.T, kv memory.KVStore, prefix string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pairs, err := kv.KVScan(prefix, 0)
		if err == nil && len(pairs) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	pairs, _ := kv.KVScan(prefix, 0)
	t.Fatalf("等待 KV 向量持久化超时: got %d want >=%d", len(pairs), want)
}

// TestInMemoryEngine_KVPersistenceRebuild 钉住持久化闭环：一个引擎索引并落 KV，另一个引擎
//
// 契约: docs/wiki/memory/memory-architecture.md#vector-persist
func TestInMemoryEngine_KVPersistenceRebuild(t *testing.T) {
	kv := kv.NewMockRustVikingClient()
	emb := membed.NewMockEmbedder(64)
	cfg := EngineConfig{EmbedFlushInterval: 10 * time.Millisecond, KV: kv, VecKeyPrefix: "test:vec:"}
	ctx := context.Background()

	k1 := memory.NewSnowflakeEventKey(1, testBaseMs)
	k2 := memory.NewSnowflakeEventKey(1, testBaseMs+1000)

	e1 := NewInMemoryEngine(nil, emb, cfg)
	_ = e1.Index(ctx, memory.IndexableEvent{EventKey: k1, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "database connection error 数据库报错", Timestamp: testBaseMs})
	_ = e1.Index(ctx, memory.IndexableEvent{EventKey: k2, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "deploy service success 部署成功", Timestamp: testBaseMs + 1000})
	waitForKVKeys(t, kv, "test:vec:", 2, 2*time.Second)
	if err := e1.Close(); err != nil {
		t.Fatalf("e1.Close: %v", err)
	}

	e2 := NewInMemoryEngine(nil, emb, cfg)
	defer e2.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !e2.RebuildDone() {
		time.Sleep(5 * time.Millisecond)
	}
	if !e2.RebuildDone() {
		t.Fatal("engine2 应完成 KV 重建")
	}
	if vc := e2.Stats().VectorCount; vc < 2 {
		t.Fatalf("重建后向量数应 >=2, got %d", vc)
	}

	hits, err := e2.Retrieve(ctx, memory.RetrievalQuery{Query: "database error 报错", PartitionIDs: []int{1}, Mode: memory.ModeVector, Limit: 5})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.EventKey == k1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("重建后应语义召回 k1, got %v", hits)
	}
}

// TestInMemoryEngine_RemoveDeletesPersisted 验证 Remove 同步删 KV 持久向量，
// 重建后不复活已删事件。
func TestInMemoryEngine_RemoveDeletesPersisted(t *testing.T) {
	kv := kv.NewMockRustVikingClient()
	emb := membed.NewMockEmbedder(64)
	cfg := EngineConfig{EmbedFlushInterval: 10 * time.Millisecond, KV: kv, VecKeyPrefix: "test:vec:"}
	ctx := context.Background()

	e := NewInMemoryEngine(nil, emb, cfg)
	defer e.Close()
	k1 := memory.NewSnowflakeEventKey(1, testBaseMs)
	_ = e.Index(ctx, memory.IndexableEvent{EventKey: k1, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "alpha token", Timestamp: testBaseMs})
	waitForKVKeys(t, kv, "test:vec:", 1, 2*time.Second)

	if err := e.Remove(ctx, k1); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	pairs, _ := kv.KVScan("test:vec:", 0)
	if len(pairs) != 0 {
		t.Fatalf("Remove 应删 KV 持久向量, 残留 %d", len(pairs))
	}
}

// TestInMemoryEngine_NoKVPureInMemory 验证 kv==nil 时纯内存（现状行为，不持久）。
func TestInMemoryEngine_NoKVPureInMemory(t *testing.T) {
	emb := membed.NewMockEmbedder(64)
	e := NewInMemoryEngine(nil, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer e.Close()
	if !e.RebuildDone() {
		t.Fatal("无 KV 时 RebuildDone 应为真")
	}
}

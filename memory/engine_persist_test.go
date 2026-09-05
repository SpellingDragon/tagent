package memory

import (
	"context"
	"testing"
	"time"
)

func waitForKVKeys(t *testing.T, kv KVStore, prefix string, want int, timeout time.Duration) {
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

// TestInMemoryEngine_KVPersistenceRebuild 验证 rustviking-backed 持久化闭环：
// engine1 索引事件 → 向量序列化入 KV → 关闭；engine2 用同一 KV 启动 → 从 KV 重建
// 内存索引 → 向量检索命中 engine1 索引的事件（跨"重启"语义召回恢复）。
func TestInMemoryEngine_KVPersistenceRebuild(t *testing.T) {
	kv := NewMockRustVikingClient() // 实现 KVStore，模拟持久后端
	emb := NewMockEmbedder(64)
	cfg := EngineConfig{EmbedFlushInterval: 10 * time.Millisecond, KV: kv, VecKeyPrefix: "test:vec:"}
	ctx := context.Background()

	k1 := NewSnowflakeEventKey(1, testBaseMs)
	k2 := NewSnowflakeEventKey(1, testBaseMs+1000)

	// engine1：索引 → 持久化到 KV。
	e1 := NewInMemoryEngine(nil, emb, cfg)
	_ = e1.Index(ctx, IndexableEvent{EventKey: k1, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "database connection error 数据库报错", Timestamp: testBaseMs})
	_ = e1.Index(ctx, IndexableEvent{EventKey: k2, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "deploy service success 部署成功", Timestamp: testBaseMs + 1000})
	waitForKVKeys(t, kv, "test:vec:", 2, 2*time.Second)
	if err := e1.Close(); err != nil {
		t.Fatalf("e1.Close: %v", err)
	}

	// engine2：同一 KV 启动 → 异步重建。
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

	// 向量检索命中 engine1 索引的事件（跨"重启"恢复）。
	hits, err := e2.Retrieve(ctx, RetrievalQuery{Query: "database error 报错", PartitionIDs: []int{1}, Mode: ModeVector, Limit: 5})
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
	kv := NewMockRustVikingClient()
	emb := NewMockEmbedder(64)
	cfg := EngineConfig{EmbedFlushInterval: 10 * time.Millisecond, KV: kv, VecKeyPrefix: "test:vec:"}
	ctx := context.Background()

	e := NewInMemoryEngine(nil, emb, cfg)
	defer e.Close()
	k1 := NewSnowflakeEventKey(1, testBaseMs)
	_ = e.Index(ctx, IndexableEvent{EventKey: k1, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "alpha token", Timestamp: testBaseMs})
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
	emb := NewMockEmbedder(64)
	e := NewInMemoryEngine(nil, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer e.Close()
	// 无 KV → rebuildDone 立即为真（无重建）。
	if !e.RebuildDone() {
		t.Fatal("无 KV 时 RebuildDone 应为真")
	}
}

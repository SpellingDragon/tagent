package engine

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
)

// TestEngineBridge_StoreEventIndexesAndProvider TestEngineBridge 覆盖接线装饰器：写入旁路索引、能力透传、删除同步、未接线行为逐字节不变、
//
// 契约: docs/wiki/memory/memory-architecture.md#engine-bridge
func TestEngineBridge_StoreEventIndexesAndProvider(t *testing.T) {
	store := memory.NewInMemoryStore()
	emb := membed.NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	key := memory.NewSnowflakeEventKey(1, testBaseMs)
	if err := bridge.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe,
		Content: "database connection error 数据库报错", EventSummary: "db error", Timestamp: testBaseMs,
	}); err != nil {
		t.Fatalf("StoreEvent: %v", err)
	}

	refs, err := bridge.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	if err != nil || len(refs) != 1 {
		t.Fatalf("inner 应有 1 事件, got %d err=%v", len(refs), err)
	}

	ep, ok := bridge.(memory.MemoryEngineProvider)
	if !ok {
		t.Fatal("bridge 应实现 memory.MemoryEngineProvider")
	}
	if ep.MemoryEngine() == nil {
		t.Fatal("memory.MemoryEngine() 不应为 nil")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !bridge.SupportsVectorSearch() {
		time.Sleep(5 * time.Millisecond)
	}
	if !bridge.SupportsVectorSearch() {
		t.Fatal("索引就绪后 SupportsVectorSearch 应为真")
	}

	qv, _ := emb.Embed(context.Background(), []string{"database connection error 数据库报错"})
	got, err := bridge.SearchByEmbedding(qv[0], 5)
	if err != nil {
		t.Fatalf("SearchByEmbedding: %v", err)
	}
	if len(got) == 0 || got[0].EventKey != key {
		t.Fatalf("向量检索应命中该事件, got %+v", got)
	}
}

func TestEngineBridge_DeleteRemovesFromEngine(t *testing.T) {
	store := memory.NewInMemoryStore()
	emb := membed.NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	key := memory.NewSnowflakeEventKey(1, testBaseMs)
	_ = bridge.StoreEvent(key, memory.FullEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "alpha token", Timestamp: testBaseMs})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if vc := eng.Stats().VectorCount; vc >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := bridge.DeleteEvent(key); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
	if vc := eng.Stats().VectorCount; vc != 0 {
		t.Fatalf("DeleteEvent 应从引擎移除向量, got %d", vc)
	}
}

func TestEngineBridge_RelationStorePassthrough(t *testing.T) {
	store := memory.NewInMemoryStore()
	eng := NewInMemoryEngine(store, nil, testEngineConfig())
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	rsp, ok := bridge.(memory.RelationStoreProvider)
	if !ok {
		t.Fatal("bridge 应透传 memory.RelationStoreProvider")
	}
	if rsp.RelationStore() == nil {
		t.Fatal("memory.RelationStore 透传不应为 nil")
	}
}

func TestEngineBridge_NoEngineUnchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	bridge := NewEngineBridge(store, nil)

	key := memory.NewSnowflakeEventKey(1, testBaseMs)
	if err := bridge.StoreEvent(key, memory.FullEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "x", Timestamp: testBaseMs}); err != nil {
		t.Fatalf("StoreEvent: %v", err)
	}
	if bridge.SupportsVectorSearch() {
		t.Fatal("无引擎时 SupportsVectorSearch 应为 false")
	}
	if _, err := bridge.SearchByEmbedding([]float32{0.1, 0.2}, 5); err == nil {
		t.Fatal("无引擎时应退回 inner stub（memory.ErrVectorSearchNotSupported）")
	}
}

// TestEngineIndex_IdempotentByKey 钉住索引按 EventKey 幂等：重复投递同一 key 只覆盖那条向量，
//
// 契约: docs/wiki/memory/memory-architecture.md#bridge-write-replay
func TestEngineIndex_IdempotentByKey(t *testing.T) {
	store := memory.NewInMemoryStore()
	emb := membed.NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 5 * time.Millisecond})
	defer eng.Close()

	k := memory.NewSnowflakeEventKey(1, testBaseMs)
	evt := memory.IndexableEvent{
		EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe,
		Text: "same text content", Timestamp: testBaseMs,
	}
	require.NoError(t, eng.Index(context.Background(), evt))
	require.NoError(t, eng.Index(context.Background(), evt))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && eng.Stats().VectorCount < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	require.EqualValues(t, 1, eng.Stats().VectorCount,
		"重复索引同一 EventKey 不得产生第二个逻辑向量条目")
}

// TestEngineBridge_AlreadyReplayDoesNotDoubleIncrement 钉住 已提交的回放不得再次触发容量计数：新写入恰好触发一次，重复回放增量为零。
func TestEngineBridge_AlreadyReplayDoesNotDoubleIncrement(t *testing.T) {
	store := memory.NewInMemoryStore()
	bridge := NewEngineBridge(store, nil)
	provider, ok := bridge.(memory.CapacityHookProvider)
	require.True(t, ok, "bridge must expose CapacityHookProvider")
	hookCount := 0
	provider.SetCapacityHook(func(int64, int, string) { hookCount++ })

	key := memory.NewSnowflakeEventKey(1, testBaseMs)
	ev := memory.FullEvent{
		EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe,
		Content: "x", Timestamp: testBaseMs,
	}
	require.NoError(t, bridge.StoreEvent(key, ev))
	require.Equal(t, 1, hookCount, "a new write fires the capacity hook exactly once")

	res, _, err := bridge.(memory.EventReplayer).ReplayEvent(key, ev)
	require.NoError(t, err)
	require.Equal(t, memory.ReplayAlreadyCommitted, res)
	require.Equal(t, 1, hookCount,
		"已提交的回放不得再次增加容量计数")
}

// ctxStrictEmbedder 是「合规」嵌入器：ctx 取消即失败（模拟 zhipu 尊重 ctx 取消/超时）。
// 用于验证审查 M1：Close 排空必须用不取消的 ctx，否则在途向量必然嵌入失败而丢失。
type ctxStrictEmbedder struct{ dim int }

func (c *ctxStrictEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, c.dim)
		v[0] = 1.0
		out[i] = v
	}
	return out, nil
}
func (c *ctxStrictEmbedder) Dimension() int  { return c.dim }
func (c *ctxStrictEmbedder) ModelID() string { return "ctx-strict" }

// TestInMemoryEngine_CloseDrainPersistsInFlight 验证审查 M1：Close 排空在途批用独立
// 不取消的 ctx（context.WithoutCancel + DrainTimeout），合规嵌入器仍成功嵌入 + 持久化，
// 不丢在途向量。修复前排空用已取消的 ctx → 合规嵌入器必失败 → 向量丢失且不持久化。
func TestInMemoryEngine_CloseDrainPersistsInFlight(t *testing.T) {
	kv := kv.NewMockRustVikingClient()
	emb := &ctxStrictEmbedder{dim: 8}
	e := NewInMemoryEngine(nil, emb, EngineConfig{
		EmbedFlushInterval: time.Hour,
		EmbedBatch:         100,
		KV:                 kv,
		VecKeyPrefix:       "drain:vec:",
		DrainTimeout:       3 * time.Second,
	})
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = e.Index(ctx, memory.IndexableEvent{
			EventKey: memory.NewSnowflakeEventKey(1, testBaseMs+int64(i)*1000), PartitionID: 1,
			EventType: TypeExternalInputProbe, Text: "payload", Timestamp: testBaseMs,
		})
	}
	_ = e.Close()
	pairs, _ := kv.KVScan("drain:vec:", 0)
	if len(pairs) == 0 {
		t.Fatal("Close 排空应以不继承取消的 ctx 持久化在途向量，实际落 KV 0 条")
	}
}

// TestInMemoryEngine_DimensionMismatchSkipped 验证审查 M3：查询向量维度与索引向量
// 不一致时跳过（不收 0 分候选），避免返回不确定顺序的垃圾票据。
func TestInMemoryEngine_DimensionMismatchSkipped(t *testing.T) {
	emb := membed.NewMockEmbedder(8)
	e := NewInMemoryEngine(nil, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer e.Close()
	key := memory.NewSnowflakeEventKey(1, testBaseMs)
	_ = e.Index(context.Background(), memory.IndexableEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "alpha", Timestamp: testBaseMs})
	waitForVectors(t, e, 1, 2*time.Second)

	hits, err := e.SearchByVector(context.Background(), []float32{0.1, 0.2, 0.3, 0.4}, 5, nil)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("维度不匹配的查询向量应跳过全部候选, got %d hits", len(hits))
	}
}

// TestInMemoryEngine_RebuildSkipsStaleModel 验证审查 M3：换嵌入模型后重启，
// 重建跳过旧模型指纹的向量（防跨模型语义混用）。
func TestInMemoryEngine_RebuildSkipsStaleModel(t *testing.T) {
	kv := kv.NewMockRustVikingClient()
	cfg := EngineConfig{EmbedFlushInterval: 10 * time.Millisecond, KV: kv, VecKeyPrefix: "model:vec:"}
	ctx := context.Background()

	e1 := NewInMemoryEngine(nil, membed.NewMockEmbedder(8), cfg)
	_ = e1.Index(ctx, memory.IndexableEvent{EventKey: memory.NewSnowflakeEventKey(1, testBaseMs), PartitionID: 1, EventType: TypeExternalInputProbe, Text: "alpha", Timestamp: testBaseMs})
	waitForKVKeys(t, kv, "model:vec:", 1, 2*time.Second)
	_ = e1.Close()

	e2 := NewInMemoryEngine(nil, membed.NewMockEmbedder(16), cfg)
	defer e2.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !e2.RebuildDone() {
		time.Sleep(5 * time.Millisecond)
	}
	if vc := e2.Stats().VectorCount; vc != 0 {
		t.Fatalf("换嵌入模型后重建应跳过旧模型指纹的向量, got vectorCount=%d", vc)
	}
}

// TestEngineBridge_RemoveVectorForwards 验证审查 M2：engineBridge 作为 memory.VectorRemover，
// RemoveVector 转发引擎 Remove（遗忘物理删除时同步移除向量，消除 Remove 死代码）。
func TestEngineBridge_RemoveVectorForwards(t *testing.T) {
	store := memory.NewInMemoryStore()
	emb := membed.NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	key := memory.NewSnowflakeEventKey(1, testBaseMs)
	_ = bridge.StoreEvent(key, memory.FullEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "alpha token", Timestamp: testBaseMs})
	waitForVectors(t, eng, 1, 2*time.Second)

	vr, ok := bridge.(memory.VectorRemover)
	if !ok {
		t.Fatal("bridge 应实现 memory.VectorRemover")
	}
	vr.RemoveVector(key)
	if vc := eng.Stats().VectorCount; vc != 0 {
		t.Fatalf("RemoveVector 应移除引擎中的向量, got %d", vc)
	}
}

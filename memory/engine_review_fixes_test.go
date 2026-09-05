package memory

import (
	"context"
	"testing"
	"time"
)

// ctxStrictEmbedder 是「合规」嵌入器：ctx 取消即失败（模拟 zhipu 尊重 ctx 取消/超时）。
// 用于验证审查 M1：Close 排空必须用不取消的 ctx，否则在途向量必然嵌入失败而丢失。
type ctxStrictEmbedder struct{ dim int }

func (c *ctxStrictEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err() // 合规：尊重取消
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
	kv := NewMockRustVikingClient()
	emb := &ctxStrictEmbedder{dim: 8}
	// 长 flush 间隔 + 大批：确保 Index 后事件停留在队列/批中，仅靠 Close 排空触发嵌入。
	e := NewInMemoryEngine(nil, emb, EngineConfig{
		EmbedFlushInterval: time.Hour,
		EmbedBatch:         100,
		KV:                 kv,
		VecKeyPrefix:       "drain:vec:",
		DrainTimeout:       3 * time.Second,
	})
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = e.Index(ctx, IndexableEvent{
			EventKey: NewSnowflakeEventKey(1, testBaseMs+int64(i)*1000), PartitionID: 1,
			EventType: TypeExternalInputProbe, Text: "payload", Timestamp: testBaseMs,
		})
	}
	_ = e.Close() // 触发排空：合规嵌入器在 drainCtx（不取消）下应成功 → 向量入 KV
	pairs, _ := kv.KVScan("drain:vec:", 0)
	if len(pairs) == 0 {
		t.Fatal("M1: Close 排空应持久化在途向量（用不取消的 ctx），got 0（修复前会丢）")
	}
}

// TestInMemoryEngine_DimensionMismatchSkipped 验证审查 M3：查询向量维度与索引向量
// 不一致时跳过（不收 0 分候选），避免返回不确定顺序的垃圾票据。
func TestInMemoryEngine_DimensionMismatchSkipped(t *testing.T) {
	emb := NewMockEmbedder(8) // 索引向量 dim=8
	e := NewInMemoryEngine(nil, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer e.Close()
	key := NewSnowflakeEventKey(1, testBaseMs)
	_ = e.Index(context.Background(), IndexableEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "alpha", Timestamp: testBaseMs})
	waitForVectors(t, e, 1, 2*time.Second)

	// 用 dim=4 查询向量（与索引 dim=8 不匹配）→ 应全部跳过，零命中。
	hits, err := e.SearchByVector(context.Background(), []float32{0.1, 0.2, 0.3, 0.4}, 5, nil)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("M3: 维度不匹配应跳过所有候选, got %d hits", len(hits))
	}
}

// TestInMemoryEngine_RebuildSkipsStaleModel 验证审查 M3：换嵌入模型后重启，
// 重建跳过旧模型指纹的向量（防跨模型语义混用）。
func TestInMemoryEngine_RebuildSkipsStaleModel(t *testing.T) {
	kv := NewMockRustVikingClient()
	cfg := EngineConfig{EmbedFlushInterval: 10 * time.Millisecond, KV: kv, VecKeyPrefix: "model:vec:"}
	ctx := context.Background()

	// engine1：模型 A（mock dim=8 → ModelID "mock-embed-8"）索引并持久化。
	e1 := NewInMemoryEngine(nil, NewMockEmbedder(8), cfg)
	_ = e1.Index(ctx, IndexableEvent{EventKey: NewSnowflakeEventKey(1, testBaseMs), PartitionID: 1, EventType: TypeExternalInputProbe, Text: "alpha", Timestamp: testBaseMs})
	waitForKVKeys(t, kv, "model:vec:", 1, 2*time.Second)
	_ = e1.Close()

	// engine2：模型 B（mock dim=16 → ModelID "mock-embed-16"）重建 → 应跳过模型 A 的向量。
	e2 := NewInMemoryEngine(nil, NewMockEmbedder(16), cfg)
	defer e2.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !e2.RebuildDone() {
		time.Sleep(5 * time.Millisecond)
	}
	if vc := e2.Stats().VectorCount; vc != 0 {
		t.Fatalf("M3: 换模型后重建应跳过旧模型向量, got vectorCount=%d", vc)
	}
}

// TestEngineBridge_RemoveVectorForwards 验证审查 M2：engineBridge 作为 VectorRemover，
// RemoveVector 转发引擎 Remove（遗忘物理删除时同步移除向量，消除 Remove 死代码）。
func TestEngineBridge_RemoveVectorForwards(t *testing.T) {
	store := NewInMemoryStore()
	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	key := NewSnowflakeEventKey(1, testBaseMs)
	_ = bridge.StoreEvent(key, FullEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "alpha token", Timestamp: testBaseMs})
	waitForVectors(t, eng, 1, 2*time.Second)

	vr, ok := bridge.(VectorRemover)
	if !ok {
		t.Fatal("bridge 应实现 VectorRemover")
	}
	vr.RemoveVector(key) // 模拟遗忘物理删除回调
	if vc := eng.Stats().VectorCount; vc != 0 {
		t.Fatalf("M2: RemoveVector 应移除引擎向量, got %d", vc)
	}
}

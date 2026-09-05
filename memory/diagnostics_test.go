package memory

import (
	"context"
	"testing"
	"time"
)

func TestMemoryDiagnostics_Snapshot(t *testing.T) {
	store := NewInMemoryStore()
	// 存两个事件（store 维度）。
	for i, c := range []string{"事件A", "事件B"} {
		k := NewSnowflakeEventKey(1, testBaseMs+int64(i)*1000)
		_ = store.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Content: c, Timestamp: testBaseMs})
	}
	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()

	// 索引一个事件（向量维度）。
	k := NewSnowflakeEventKey(1, testBaseMs+2000)
	_ = store.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "语义内容", Timestamp: testBaseMs})
	_ = eng.Index(context.Background(), IndexableEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "语义内容", Timestamp: testBaseMs})
	waitForVectors(t, eng, 1, 2*time.Second)

	diag := NewMemoryDiagnostics(eng, store)
	snap := diag.Snapshot()

	if !snap.CapKeyword || !snap.CapVector || !snap.CapHybrid {
		t.Fatalf("能力应齐全, got %+v", snap)
	}
	if !snap.EngineReady {
		t.Fatal("引擎应就绪")
	}
	if snap.VectorIndexed < 1 || snap.VectorCount < 1 {
		t.Fatalf("向量维度应反映索引, got indexed=%d count=%d", snap.VectorIndexed, snap.VectorCount)
	}
	if snap.TotalEvents < 3 {
		t.Fatalf("存储维度应反映事件数, got %d", snap.TotalEvents)
	}
	if snap.IndexHealth <= 0 || snap.IndexHealth > 1 {
		t.Fatalf("索引健康率应在 (0,1], got %f", snap.IndexHealth)
	}
}

func TestMemoryDiagnostics_NilSafe(t *testing.T) {
	// 无引擎无 store → 空快照，不 panic。
	diag := NewMemoryDiagnostics(nil, nil)
	snap := diag.Snapshot()
	if snap.CapVector || snap.TotalEvents != 0 {
		t.Fatalf("无源应空快照, got %+v", snap)
	}
	// nil 接收者也不 panic。
	var nilDiag *MemoryDiagnostics
	if s := nilDiag.Snapshot(); s.TotalEvents != 0 {
		t.Fatal("nil 诊断应返回空快照")
	}
}

func TestMemoryDiagnostics_DimMismatchSurfaced(t *testing.T) {
	emb := NewMockEmbedder(8)
	eng := NewInMemoryEngine(nil, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	k := NewSnowflakeEventKey(1, testBaseMs)
	_ = eng.Index(context.Background(), IndexableEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "x", Timestamp: testBaseMs})
	waitForVectors(t, eng, 1, 2*time.Second)
	// 维度不匹配查询 → dimMismatch 计数上升，诊断快照可见。
	_, _ = eng.SearchByVector(context.Background(), []float32{0.1, 0.2}, 5, nil)
	diag := NewMemoryDiagnostics(eng, nil)
	if diag.Snapshot().VectorDimMismatch < 1 {
		t.Fatalf("维度不匹配应被诊断捕获, got %d", diag.Snapshot().VectorDimMismatch)
	}
}

func TestIndexHealth(t *testing.T) {
	if h := indexHealth(0, 0, 0); h != 1.0 {
		t.Errorf("无数据健康率应 1.0, got %f", h)
	}
	if h := indexHealth(8, 2, 0); h != 0.8 {
		t.Errorf("8/10 应 0.8, got %f", h)
	}
}

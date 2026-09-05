package memory

import (
	"context"
	"testing"
	"time"
)

func TestEngineBridge_StoreEventIndexesAndProvider(t *testing.T) {
	store := NewInMemoryStore()
	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	key := NewSnowflakeEventKey(1, testBaseMs)
	if err := bridge.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe,
		Content: "database connection error 数据库报错", EventSummary: "db error", Timestamp: testBaseMs,
	}); err != nil {
		t.Fatalf("StoreEvent: %v", err)
	}

	// inner 收到事件（QueryEvents 经 bridge 委托可见）。
	refs, err := bridge.QueryEvents(QueryOptions{PartitionIDs: []int{1}})
	if err != nil || len(refs) != 1 {
		t.Fatalf("inner 应有 1 事件, got %d err=%v", len(refs), err)
	}

	// bridge 暴露引擎（MemoryEngineProvider）——recall 据此走 hybrid。
	ep, ok := bridge.(MemoryEngineProvider)
	if !ok {
		t.Fatal("bridge 应实现 MemoryEngineProvider")
	}
	if ep.MemoryEngine() == nil {
		t.Fatal("MemoryEngine() 不应为 nil")
	}

	// 引擎异步索引完成 → 向量就绪 → SupportsVectorSearch 转真。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !bridge.SupportsVectorSearch() {
		time.Sleep(5 * time.Millisecond)
	}
	if !bridge.SupportsVectorSearch() {
		t.Fatal("索引就绪后 SupportsVectorSearch 应为真")
	}

	// SearchByEmbedding 经引擎向量路（消灭 stub）：用事件自身向量查询应命中自己。
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
	store := NewInMemoryStore()
	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	key := NewSnowflakeEventKey(1, testBaseMs)
	_ = bridge.StoreEvent(key, FullEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "alpha token", Timestamp: testBaseMs})
	// 等向量就绪。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, _, vc := eng.Stats(); vc >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := bridge.DeleteEvent(key); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
	if _, _, _, vc := eng.Stats(); vc != 0 {
		t.Fatalf("DeleteEvent 应从引擎移除向量, got %d", vc)
	}
}

func TestEngineBridge_RelationStorePassthrough(t *testing.T) {
	store := NewInMemoryStore() // 实现 RelationStoreProvider
	eng := NewInMemoryEngine(store, nil, testEngineConfig())
	defer eng.Close()
	bridge := NewEngineBridge(store, eng)

	rsp, ok := bridge.(RelationStoreProvider)
	if !ok {
		t.Fatal("bridge 应透传 RelationStoreProvider")
	}
	if rsp.RelationStore() == nil {
		t.Fatal("RelationStore 透传不应为 nil")
	}
}

func TestEngineBridge_NoEngineUnchanged(t *testing.T) {
	// engine=nil 的 bridge：StoreEvent 仅委托 inner，SupportsVectorSearch=false，
	// SearchByEmbedding 退回 inner（stub）——保证「未接线行为逐字节不变」。
	store := NewInMemoryStore()
	bridge := NewEngineBridge(store, nil)

	key := NewSnowflakeEventKey(1, testBaseMs)
	if err := bridge.StoreEvent(key, FullEvent{EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "x", Timestamp: testBaseMs}); err != nil {
		t.Fatalf("StoreEvent: %v", err)
	}
	if bridge.SupportsVectorSearch() {
		t.Fatal("无引擎时 SupportsVectorSearch 应为 false")
	}
	if _, err := bridge.SearchByEmbedding([]float32{0.1, 0.2}, 5); err == nil {
		t.Fatal("无引擎时应退回 inner stub（ErrVectorSearchNotSupported）")
	}
}

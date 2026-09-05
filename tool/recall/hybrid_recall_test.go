package recall

import (
	"context"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

const hybridTestBaseMs = int64(1750000000000)

// seedAndIndex 存事件到 store 并投递引擎索引。
func seedAndIndex(t *testing.T, store *memory.InMemoryStore, eng memory.MemoryEngine, pid int, content string, ts int64) int64 {
	t.Helper()
	key := memory.NewSnowflakeEventKey(pid, ts)
	if err := store.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		Content: content, EventSummary: content, Timestamp: ts,
	}); err != nil {
		t.Fatalf("StoreEvent: %v", err)
	}
	if err := eng.Index(context.Background(), memory.IndexableEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input", Text: content, Timestamp: ts,
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return key
}

// TestRecallByQuery_HybridViaEngine 验证 recall query 路径经记忆引擎做 hybrid：
// accessor 暴露 MemoryEngineProvider 且引擎向量就绪时，recallByQuery 走引擎融合，
// 语义相近（共享词元）的事件被召回——协议输出不变（key/type/summary/time）。
func TestRecallByQuery_HybridViaEngine(t *testing.T) {
	store := memory.NewInMemoryStore()
	emb := memory.NewMockEmbedder(128)
	eng := memory.NewInMemoryEngine(store, emb, memory.EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	accessor := memory.NewEngineBridge(store, eng) // MemoryStore + MemoryEngineProvider

	kDB := seedAndIndex(t, store, eng, 1, "database connection error 数据库连接报错", hybridTestBaseMs)
	seedAndIndex(t, store, eng, 1, "deploy service success 部署服务成功", hybridTestBaseMs+1000)
	seedAndIndex(t, store, eng, 1, "weather sunny today 今天天气晴朗", hybridTestBaseMs+2000)

	// 等引擎向量就绪（3 条）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !accessor.SupportsVectorSearch() {
		time.Sleep(5 * time.Millisecond)
	}
	if !accessor.SupportsVectorSearch() {
		t.Fatal("引擎向量应就绪")
	}

	res, err := recallByQuery(context.Background(), accessor, []int{1}, memoryRecallArgs{Query: "database error 报错", Limit: 3})
	if err != nil {
		t.Fatalf("recallByQuery: %v", err)
	}
	if res.Count == 0 {
		t.Fatal("hybrid 应有命中")
	}
	// 命中条目的 key 应含 kDB（语义 + 关键词双命中，融合居首）。
	hexDB := tagentevent.FormatEventKey(kDB)
	found := false
	for _, e := range res.Entries {
		if e.Key == hexDB {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("database 事件应被 hybrid 召回, got %+v", res.Entries)
	}
}

// TestRecallByQuery_NoEngineKeywordOnly 验证未接线引擎时 recallByQuery 走纯关键词
// （现状行为），不因 T-A 引入而改变。
func TestRecallByQuery_NoEngineKeywordOnly(t *testing.T) {
	store := memory.NewInMemoryStore()
	key := memory.NewSnowflakeEventKey(1, hybridTestBaseMs)
	_ = store.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: 1, EventType: "external_input",
		Content: "deploy failure", EventSummary: "deploy failure", Timestamp: hybridTestBaseMs,
	})
	// 裸 store（非 bridge）作为 accessor：无 MemoryEngineProvider → 纯关键词。
	res, err := recallByQuery(context.Background(), store, []int{1}, memoryRecallArgs{Query: "deploy", Limit: 5})
	if err != nil {
		t.Fatalf("recallByQuery: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("纯关键词应召回 1 条, got %d", res.Count)
	}
}

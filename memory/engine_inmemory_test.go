package memory

import (
	"context"
	"sync"
	"testing"
	"time"
)

// testBaseMs 是测试用基准时间戳（毫秒），须晚于 snowflakeEpoch(2024-01-01) 以产生
// 正 key；各事件用 +1000ms 递增保证秒级唯一 → Snowflake key 唯一且分区编码一致。
const testBaseMs = int64(1750000000000)

// ---------------------------------------------------------------------------
// 测试替身与助手
// ---------------------------------------------------------------------------

// blockingEmbedder 首次 Embed 时关闭 started 并阻塞至 ctx 取消——确定性地制造
// 「worker 卡在嵌入、队列积压」场景。
type blockingEmbedder struct {
	dim     int
	started chan struct{}
	once    sync.Once
}

func (b *blockingEmbedder) Embed(ctx context.Context, _ []string) ([][]float32, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}
func (b *blockingEmbedder) Dimension() int  { return b.dim }
func (b *blockingEmbedder) ModelID() string { return "blocking-embed" }

func testEngineConfig() EngineConfig {
	return EngineConfig{EmbedFlushInterval: 10 * time.Millisecond, QueueCap: 64, EmbedBatch: 8}
}

// seedEvent 用 Snowflake 一致键（分区编码 == pid）存事件，返回 key。
func seedEvent(t *testing.T, s *InMemoryStore, pid int, etype, content string, ts int64) int64 {
	t.Helper()
	key := NewSnowflakeEventKey(pid, ts)
	if err := s.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: pid, EventType: etype,
		EventSummary: content, Content: content, Timestamp: ts,
	}); err != nil {
		t.Fatalf("StoreEvent: %v", err)
	}
	return key
}

// indexEvent 向引擎投递索引（内容直接传入，不依赖 store.GetEvent）。
func indexEvent(t *testing.T, eng *InMemoryEngine, key int64, pid int, etype, content string, ts int64) {
	t.Helper()
	if err := eng.Index(context.Background(), IndexableEvent{
		EventKey: key, PartitionID: pid, EventType: etype, Text: content, Timestamp: ts,
	}); err != nil {
		t.Fatalf("Index(%d): %v", key, err)
	}
}

func waitForVectors(t *testing.T, eng *InMemoryEngine, want int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, _, _, vc := eng.Stats(); vc >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _, _, vc := eng.Stats()
	t.Fatalf("等待向量索引超时: got %d want >=%d", vc, want)
}

func hitKeys(hits []RetrievalHit) map[int64]bool {
	m := make(map[int64]bool, len(hits))
	for _, h := range hits {
		m[h.EventKey] = true
	}
	return m
}

// ---------------------------------------------------------------------------
// 单元测试：融合与相似度
// ---------------------------------------------------------------------------

func TestRRFFuse_BothListsRankTop(t *testing.T) {
	listA := []int64{10, 20, 30}
	listB := []int64{10, 40, 50}
	fused := rrfFuse([][]int64{listA, listB}, 60)
	if len(fused) == 0 || fused[0] != 10 {
		t.Fatalf("两表共同命中应居首, got %v", fused)
	}
	seen := map[int64]bool{}
	for _, k := range fused {
		if seen[k] {
			t.Fatalf("融合结果重复 key %d", k)
		}
		seen[k] = true
	}
	for _, want := range []int64{10, 20, 30, 40, 50} {
		if !seen[want] {
			t.Fatalf("融合丢失 key %d: %v", want, fused)
		}
	}
}

func TestCosine_IdenticalAndOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	if c := cosine(a, []float32{1, 0, 0}); c < 0.999 {
		t.Errorf("相同向量余弦应≈1, got %f", c)
	}
	if c := cosine(a, []float32{0, 1, 0}); c > 0.001 {
		t.Errorf("正交向量余弦应≈0, got %f", c)
	}
	if c := cosine(a, []float32{0, 0, 0}); c != 0 {
		t.Errorf("零向量余弦应=0, got %f", c)
	}
	if c := cosine([]float32{1, 2}, []float32{1, 2, 3}); c != 0 {
		t.Errorf("维度不等余弦应=0, got %f", c)
	}
}

func TestMockEmbedder_DeterministicNormalized(t *testing.T) {
	emb := NewMockEmbedder(64)
	v1, _ := emb.Embed(context.Background(), []string{"部署 服务 deploy"})
	v2, _ := emb.Embed(context.Background(), []string{"部署 服务 deploy"})
	if len(v1) != 1 || len(v1[0]) != 64 {
		t.Fatalf("维度错: %d", len(v1[0]))
	}
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatal("相同文本应产生相同向量（确定性）")
		}
	}
	var sum float32
	for _, x := range v1[0] {
		sum += x * x
	}
	if sum < 0.98 || sum > 1.02 {
		t.Errorf("向量应 L2 归一化, |v|^2=%f", sum)
	}
}

// ---------------------------------------------------------------------------
// 引擎行为测试
// ---------------------------------------------------------------------------

func TestInMemoryEngine_KeywordOnlyDegradation(t *testing.T) {
	store := NewInMemoryStore()
	k1 := seedEvent(t, store, 1, TypeExternalInputProbe, "部署服务失败 deploy error", testBaseMs)
	seedEvent(t, store, 1, TypeExternalInputProbe, "天气晴朗 weather sunny", testBaseMs+1000)

	eng := NewInMemoryEngine(store, nil, testEngineConfig())
	defer eng.Close()

	if eng.Capabilities().Vector || eng.Capabilities().Hybrid {
		t.Fatal("无 embedder 时不应声明 Vector/Hybrid 能力")
	}
	if !eng.Capabilities().Keyword {
		t.Fatal("有 store 时应声明 Keyword 能力")
	}
	hits, err := eng.Retrieve(context.Background(), RetrievalQuery{Query: "deploy", PartitionIDs: []int{1}, Limit: 5})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !hitKeys(hits)[k1] {
		t.Fatalf("关键词应召回 deploy 事件, got %v", hits)
	}
}

func TestInMemoryEngine_HybridRecall(t *testing.T) {
	store := NewInMemoryStore()
	kDB := seedEvent(t, store, 1, TypeExternalInputProbe, "database connection error 数据库连接报错", testBaseMs)
	seedEvent(t, store, 1, TypeExternalInputProbe, "deploy service success 部署成功", testBaseMs+1000)
	seedEvent(t, store, 1, TypeExternalInputProbe, "weather sunny today 今天天气晴朗", testBaseMs+2000)

	emb := NewMockEmbedder(128)
	eng := NewInMemoryEngine(store, emb, testEngineConfig())
	defer eng.Close()

	indexEvent(t, eng, kDB, 1, TypeExternalInputProbe, "database connection error 数据库连接报错", testBaseMs)
	kDeploy := NewSnowflakeEventKey(1, testBaseMs+1000)
	indexEvent(t, eng, kDeploy, 1, TypeExternalInputProbe, "deploy service success 部署成功", testBaseMs+1000)
	kWeather := NewSnowflakeEventKey(1, testBaseMs+2000)
	indexEvent(t, eng, kWeather, 1, TypeExternalInputProbe, "weather sunny today 今天天气晴朗", testBaseMs+2000)
	waitForVectors(t, eng, 3, 2*time.Second)

	if !eng.Capabilities().Hybrid {
		t.Fatal("store+emb 就绪后应声明 Hybrid")
	}
	// 查询与 kDB 共享词元（database/error/报错）→ 关键词路与向量路都应命中，融合居首。
	hits, err := eng.Retrieve(context.Background(), RetrievalQuery{Query: "database error 报错", PartitionIDs: []int{1}, Mode: ModeHybrid, Limit: 3})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid 应有命中")
	}
	if hits[0].EventKey != kDB {
		t.Fatalf("database 事件应融合居首, got %v want %d", hits, kDB)
	}
}

func TestInMemoryEngine_PartitionFilterNoLeak(t *testing.T) {
	store := NewInMemoryStore()
	k1 := seedEvent(t, store, 1, TypeExternalInputProbe, "alpha shared token 共享词元", testBaseMs)
	k2 := seedEvent(t, store, 2, TypeExternalInputProbe, "alpha shared token 共享词元", testBaseMs+1000)

	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, testEngineConfig())
	defer eng.Close()

	indexEvent(t, eng, k1, 1, TypeExternalInputProbe, "alpha shared token 共享词元", testBaseMs)
	indexEvent(t, eng, k2, 2, TypeExternalInputProbe, "alpha shared token 共享词元", testBaseMs+1000)
	waitForVectors(t, eng, 2, 2*time.Second)

	// 仅查分区 1：k2（分区 2）MUST NOT 泄漏——跨分区泄漏防线（向量路 + 关键词路双重过滤）。
	hits, _ := eng.Retrieve(context.Background(), RetrievalQuery{Query: "alpha shared", PartitionIDs: []int{1}, Mode: ModeHybrid, Limit: 10})
	keys := hitKeys(hits)
	if keys[k2] {
		t.Fatalf("分区过滤失效: 分区2事件泄漏到分区1查询: %v", hits)
	}
	if !keys[k1] {
		t.Fatalf("分区1事件应命中, got %v", hits)
	}
}

func TestInMemoryEngine_SelectiveIndexSkipsNonEmbeddable(t *testing.T) {
	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(nil, emb, testEngineConfig())
	defer eng.Close()

	ctx := context.Background()
	// action_command 非 Embeddable（注册表），Index 应跳过——不产生向量。
	_ = eng.Index(ctx, IndexableEvent{EventKey: 100, PartitionID: 1, EventType: "action_command", Text: "some tool call", Timestamp: 100})
	time.Sleep(50 * time.Millisecond)
	if _, _, _, vc := eng.Stats(); vc != 0 {
		t.Fatalf("非 Embeddable 类型不应产生向量, got vectorCount=%d", vc)
	}
	// 负 key（合成投影引用）也不索引。
	_ = eng.Index(ctx, IndexableEvent{EventKey: -5, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "x", Timestamp: 1})
	time.Sleep(20 * time.Millisecond)
	if _, _, _, vc := eng.Stats(); vc != 0 {
		t.Fatalf("负 key 不应索引, got vectorCount=%d", vc)
	}
}

func TestInMemoryEngine_IndexNonBlocking_QueueFullDrop(t *testing.T) {
	be := &blockingEmbedder{dim: 32, started: make(chan struct{})}
	eng := NewInMemoryEngine(nil, be, EngineConfig{QueueCap: 1, EmbedBatch: 1, EmbedFlushInterval: time.Hour})
	defer eng.Close()

	ctx := context.Background()
	_ = eng.Index(ctx, IndexableEvent{EventKey: 1, PartitionID: 1, EventType: TypeExternalInputProbe, Text: "a", Timestamp: 1})
	<-be.started // 确定 worker 已进入 Embed 阻塞
	start := time.Now()
	for i := 2; i <= 20; i++ {
		_ = eng.Index(ctx, IndexableEvent{EventKey: int64(i), PartitionID: 1, EventType: TypeExternalInputProbe, Text: "x", Timestamp: int64(i)})
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Index 应非阻塞, 19 次耗时 %v", elapsed)
	}
	if _, dropped, _, _ := eng.Stats(); dropped == 0 {
		t.Fatal("队列满应产生丢弃计数")
	}
}

func TestInMemoryEngine_RemoveDeletesVector(t *testing.T) {
	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(nil, emb, testEngineConfig())
	defer eng.Close()

	key := NewSnowflakeEventKey(1, testBaseMs)
	indexEvent(t, eng, key, 1, TypeExternalInputProbe, "alpha token", testBaseMs)
	waitForVectors(t, eng, 1, 2*time.Second)

	if err := eng.Remove(context.Background(), key); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, _, _, vc := eng.Stats(); vc != 0 {
		t.Fatalf("Remove 后向量应清空, got %d", vc)
	}
}

func TestInMemoryEngine_EmptyQueryDegradesToKeyword(t *testing.T) {
	store := NewInMemoryStore()
	k1 := seedEvent(t, store, 1, TypeExternalInputProbe, "some content", testBaseMs)
	emb := NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, testEngineConfig())
	defer eng.Close()

	// 空查询 = 纯浏览/过滤，走关键词路（QueryEvents），不触发嵌入。
	hits, err := eng.Retrieve(context.Background(), RetrievalQuery{Query: "", PartitionIDs: []int{1}, Limit: 5})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !hitKeys(hits)[k1] {
		t.Fatalf("空查询应经 store 返回事件, got %v", hits)
	}
}

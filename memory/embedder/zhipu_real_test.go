package embedder

import (
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	meng "github.com/SpellingDragon/tagent/memory/engine"

	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// TestZhipuEmbedder_RealEmbed 是 hybrid 5.1/5.3 前置实测：真实 zhipu embedding-3 调用（有
// ZAI_API_KEY 才跑，无则 Skip——tests/ 惯例）。验证 GLM Coding Plan key 对 /embeddings 端点
// 可用 + 返回维度符合请求（512/1024）+ ModelID/Dimension 探测正确。
func TestZhipuEmbedder_RealEmbed(t *testing.T) {
	if os.Getenv("ZAI_API_KEY") == "" {
		t.Skip("ZAI_API_KEY 未设置，跳过真实 embedder 实测")
	}
	for _, dim := range []int{512, 1024} {
		emb, err := NewZhipuEmbedder(ZhipuEmbedderConfig{Dimensions: dim})
		if err != nil {
			t.Fatalf("NewZhipuEmbedder(dim=%d): %v", dim, err)
		}
		vecs, err := emb.Embed(context.Background(), []string{
			"部署失败告警：服务 X 健康检查连续 3 次超时",
			"今天午饭吃了火锅，味道不错",
		})
		if err != nil {
			t.Fatalf("真实 Embed(dim=%d) 失败(GLM Coding Plan key 对 /embeddings 端点不可用?): %v", dim, err)
		}
		if len(vecs) != 2 {
			t.Fatalf("应返回 2 向量, got %d", len(vecs))
		}
		if len(vecs[0]) != dim {
			t.Errorf("请求 %d 维, got %d", dim, len(vecs[0]))
		}
		t.Logf("真实 embedding-3(dim=%d) 成功: vec[0] len=%d, ModelID=%q, Dimension()=%d",
			dim, len(vecs[0]), emb.ModelID(), emb.Dimension())
	}
}

// TestSemanticRecall_RealEmbedderClosedLoop 是 hybrid 5.3 实测：真实 embedder 下「入库→语义
// 召回→票据取回全文」端到端闭环（有 key 才跑）。验证真实 embedding-3 向量下，语义查询 top1
// 命中语义最近事件（而非关键词匹配），票据可水合回全文。
func TestSemanticRecall_RealEmbedderClosedLoop(t *testing.T) {
	if os.Getenv("ZAI_API_KEY") == "" {
		t.Skip("ZAI_API_KEY 未设置，跳过真实 embedder 闭环实测")
	}
	emb, err := NewZhipuEmbedder(ZhipuEmbedderConfig{Dimensions: 1024})
	if err != nil {
		t.Fatalf("embedder: %v", err)
	}
	store := memory.NewInMemoryStore()
	eng := meng.NewInMemoryEngine(store, emb, meng.EngineConfig{VectorTopK: 5, KeywordTopK: 5})
	defer eng.Close()
	bridged := meng.NewEngineBridge(store, eng)

	pid := 1
	docs := []string{
		"生产环境数据库连接池耗尽，服务返回 503 错误",
		"用户预订了明天飞往上海的机票并支付了定金",
		"Kubernetes pod 因内存超限被 OOMKilled 频繁重启",
	}
	for i, text := range docs {
		k := memory.NewSnowflakeEventKey(pid, 1_700_000_000_000+int64(i))
		if err := bridged.StoreEvent(k, memory.FullEvent{
			EventKey: k, PartitionID: pid, EventType: tagentevent.TypeExternalInput,
			Content: text, Timestamp: 1_700_000_000_000 + int64(i),
		}); err != nil {
			t.Fatalf("StoreEvent: %v", err)
		}
	}
	// 等异步嵌入 worker 完成真实 API 调用 + 索引（InMemoryEngine 异步；3 事件一批 ≤16）。
	time.Sleep(4 * time.Second)

	// 语义召回：查询"服务器内存不足崩溃重启"与 OOMKilled 语义最近（无共同关键词"内存超限"
	// vs"内存不足"、"崩溃重启"vs"OOMKilled 重启"），考验真实向量语义而非字面匹配。
	hits, err := eng.Retrieve(context.Background(), memory.RetrievalQuery{
		Query: "服务器内存不足导致进程崩溃并不断重启", PartitionIDs: []int{pid}, Limit: 3,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("真实语义召回应返回结果")
	}
	// 票据取回全文（两段式：hits[0].EventKey → store.GetEvent 水合）。
	top, err := store.GetEvent(hits[0].EventKey)
	if err != nil || top == nil {
		t.Fatalf("5.3 票据取回全文失败: %v", err)
	}
	t.Logf("真实语义召回 top1=%q (score=%.4f, hits=%d)", top.Content, hits[0].Score, len(hits))
	if !strings.Contains(top.Content, "OOMKilled") {
		t.Errorf("5.3: 语义召回 top1 应是 OOMKilled 事件(语义最近), got %q", top.Content)
	}
}

// TestDimensionComparison_Real 是 hybrid 5.1 实测（X2 决议）：512 vs 1024 维召回质量对比。
// 用同批「语义相关对」与「语义不相关对」，对比两维度下 cosine 分离度（相关均值 - 不相关均值），
// 为默认维度选择提供真实数据（有 key 才跑）。分离度越大 = 该维度判别力越强。
func TestDimensionComparison_Real(t *testing.T) {
	if os.Getenv("ZAI_API_KEY") == "" {
		t.Skip("ZAI_API_KEY 未设置，跳过维度对比实测")
	}
	related := [][2]string{
		{"服务健康检查超时告警", "监控探测连续失败，实例不可用"},
		{"数据库连接池耗尽", "MySQL 连接数打满，新请求排队等待"},
		{"磁盘空间不足写入失败", "存储卷容量告急，IO 报错"},
	}
	unrelated := [][2]string{
		{"服务健康检查超时告警", "周末和朋友去爬山看日出"},
		{"数据库连接池耗尽", "猫喜欢钻进纸箱里睡觉"},
		{"磁盘空间不足写入失败", "这家咖啡馆的手冲不错"},
	}
	for _, dim := range []int{512, 1024} {
		emb, err := NewZhipuEmbedder(ZhipuEmbedderConfig{Dimensions: dim})
		if err != nil {
			t.Fatalf("embedder dim=%d: %v", dim, err)
		}
		relAvg := avgPairSim(t, emb, related)
		unrelAvg := avgPairSim(t, emb, unrelated)
		sep := relAvg - unrelAvg
		t.Logf("5.1 dim=%d: 相关对均相似=%.4f, 不相关对均相似=%.4f, 分离度=%.4f", dim, relAvg, unrelAvg, sep)
		if sep <= 0 {
			t.Errorf("dim=%d: 相关对应比不相关对更相似(分离度>0), got sep=%.4f", dim, sep)
		}
	}
}

func avgPairSim(t *testing.T, emb memory.Embedder, pairs [][2]string) float64 {
	t.Helper()
	var sum float64
	for _, p := range pairs {
		vecs, err := emb.Embed(context.Background(), []string{p[0], p[1]})
		if err != nil {
			t.Fatalf("Embed 对比对失败: %v", err)
		}
		sum += cosineSim(vecs[0], vecs[1])
	}
	return sum / float64(len(pairs))
}

func cosineSim(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

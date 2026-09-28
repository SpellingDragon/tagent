package recall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestUnifiedRecall_Routing: the unified entry
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestUnifiedRecall_Routing(t *testing.T) {
	tl := NewRecallTool(seedUnifiedStore(t), []int{memory.PartitionIDFromEventKey(kIn)}).(tool.CallableTool)
	call := func(args string) memoryRecallResult {
		out, err := tl.Call(context.Background(), []byte(args))
		require.NoError(t, err)
		return out.(memoryRecallResult)
	}

	t.Run("items takes precedence", func(t *testing.T) {
		res := call(`{"items":[{"key":"` + tagentevent.FormatEventKey(kA) + `"}]}`)
		assert.Equal(t, "items", res.Mode)
		require.Len(t, res.Entries, 1)
		assert.Contains(t, res.Entries[0].Content, "alpha content")
	})

	t.Run("turn_key walks the causal chain", func(t *testing.T) {
		res := call(`{"turn_key":"` + tagentevent.FormatEventKey(kOut) + `"}`)
		assert.Equal(t, "turn", res.Mode)
		require.NotEmpty(t, res.Entries)
		last := res.Entries[len(res.Entries)-1]
		assert.Equal(t, tagentevent.FormatEventKey(kOut), last.Key)
	})

	t.Run("query uses the retrieval layer", func(t *testing.T) {
		res := call(`{"query":"alpha"}`)
		assert.Equal(t, "query", res.Mode)
	})

	t.Run("pure time-range recall without keyword", func(t *testing.T) {
		res := call(`{"since":` + fmt.Sprintf("%d", kIn) + `,"until":` + fmt.Sprintf("%d", kAc) + `}`)
		assert.Equal(t, "query", res.Mode)
		require.Len(t, res.Entries, 3)
		assert.Equal(t, tagentevent.FormatEventKey(kAc), res.Entries[0].Key)
		assert.Equal(t, tagentevent.FormatEventKey(kIn), res.Entries[2].Key)
	})

	t.Run("since-only recalls everything after", func(t *testing.T) {
		res := call(`{"since":` + fmt.Sprintf("%d", kOut) + `}`)
		require.Len(t, res.Entries, 2, "kOut, kA")
	})

	t.Run("orchestrate returns explicit guidance, never silent fallback", func(t *testing.T) {
		res := call(`{"orchestrate":true}`)
		assert.Equal(t, "orchestrate", res.Mode)
		assert.Contains(t, res.Message, "未接线")
		assert.Empty(t, res.Entries, "orchestrate must not silently degrade to a deterministic shape")
	})

	t.Run("no shape is an error", func(t *testing.T) {
		_, err := tl.Call(context.Background(), []byte(`{}`))
		assert.Error(t, err)
	})
}

// kIn Seed keys: input → thinking_plan → action_command → agent_output chain.
var (
	kIn  = int64(0x1201aa00000001)
	kTp  = int64(0x1201aa00000002)
	kAc  = int64(0x1201aa00000003)
	kOut = int64(0x1201aa00000004)
	kA   = int64(0x1201aa00000005)
)

func seedUnifiedStore(t *testing.T) *memory.InMemoryStore {
	t.Helper()
	store := memory.NewInMemoryStore()
	put := func(key int64, typ, summary, content string) {
		require.NoError(t, store.StoreEvent(key, memory.FullEvent{
			EventKey: key, EventType: typ, EventSummary: summary, Content: content, Timestamp: key,
		}))
	}
	put(kIn, tagentevent.TypeExternalInput, "alpha input", "alpha content")
	put(kTp, tagentevent.TypeThinkingPlan, "plan", "plan prose")
	put(kAc, tagentevent.TypeActionCommand, "exec", "tool result")
	put(kOut, tagentevent.TypeAgentOutput, "done", "final answer")
	put(kA, tagentevent.TypeExternalInput, "alpha", "alpha content")
	rs := store.RelationStore()
	require.NoError(t, rs.SetParent(kTp, kIn))
	require.NoError(t, rs.SetParent(kAc, kTp))
	require.NoError(t, rs.SetParent(kOut, kAc))
	return store
}

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
	emb := membed.NewMockEmbedder(128)
	eng := engine.NewInMemoryEngine(store, emb, engine.EngineConfig{EmbedFlushInterval: 10 * time.Millisecond})
	defer eng.Close()
	accessor := engine.NewEngineBridge(store, eng)

	kDB := seedAndIndex(t, store, eng, 1, "database connection error 数据库连接报错", hybridTestBaseMs)
	seedAndIndex(t, store, eng, 1, "deploy service success 部署服务成功", hybridTestBaseMs+1000)
	seedAndIndex(t, store, eng, 1, "weather sunny today 今天天气晴朗", hybridTestBaseMs+2000)

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
	res, err := recallByQuery(context.Background(), store, []int{1}, memoryRecallArgs{Query: "deploy", Limit: 5})
	if err != nil {
		t.Fatalf("recallByQuery: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("纯关键词应召回 1 条, got %d", res.Count)
	}
}

// spyAccessor records GetEvent calls — the cap must bound hydration cost,
// not just the result length.
type spyAccessor struct {
	memory.MemoryStore
	gets int
}

func (s *spyAccessor) GetEvent(key int64) (*memory.FullEvent, error) {
	s.gets++
	return &memory.FullEvent{EventKey: key, EventType: "external_input", Content: "x"}, nil
}

// TestRecallByItems_BoundedHydration: over the
// cap only maxRecallItems tickets are hydrated; the truncation is reported in
// Message with the dropped count — never silent.
func TestRecallByItems_BoundedHydration(t *testing.T) {
	acc := &spyAccessor{}
	items := make([]recallItem, 0, 60)
	for i := 0; i < 60; i++ {
		items = append(items, recallItem{Key: fmt.Sprintf("%x", i+1)})
	}
	res := recallByItems(acc, items)

	if acc.gets != maxRecallItems {
		t.Fatalf("GetEvent calls = %d, want %d (hydration must be bounded)", acc.gets, maxRecallItems)
	}
	if len(res.Entries) != maxRecallItems {
		t.Fatalf("entries = %d, want %d", len(res.Entries), maxRecallItems)
	}
	if !strings.Contains(res.Message, "10 of 60 tickets dropped") {
		t.Fatalf("Message must report the truncation, got: %q", res.Message)
	}
}

// TestRecallByItems_UnderCap_NoTruncationNote: honesty cuts both ways — no
// truncation, no message.
func TestRecallByItems_UnderCap_NoTruncationNote(t *testing.T) {
	acc := &spyAccessor{}
	items := make([]recallItem, 0, 3)
	for i := 0; i < 3; i++ {
		items = append(items, recallItem{Key: fmt.Sprintf("%x", i+1)})
	}
	res := recallByItems(acc, items)
	if res.Message != "" {
		t.Fatalf("under-cap Message = %q, want empty", res.Message)
	}
	if acc.gets != 3 {
		t.Fatalf("GetEvent calls = %d, want 3", acc.gets)
	}
}

// seedManyPartitions 同 memory_recall_test.seedPartitions：隔离契约下查询
// 必须显式授权种子事件所在分区（2.7）。
func seedManyPartitions() []int {
	return []int{memory.PartitionIDFromEventKey(1000)}
}

func seedManyStore(t *testing.T, n int) memory.MemoryStore {
	t.Helper()
	store := memory.NewInMemoryStore()
	for i := 0; i < n; i++ {
		key := int64(1000 + i)
		if err := store.StoreEvent(key, memory.FullEvent{
			EventKey:     key,
			EventType:    "agent_output",
			EventSummary: "部署记录",
			Content:      "部署记录内容",
			Timestamp:    int64(1710000000000 + i*1000),
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return store
}

// TestTruncationHint_AtLimit: results hitting the limit carry the notice.
func TestTruncationHint_AtLimit(t *testing.T) {
	tl := NewMemoryRecallTool(seedManyStore(t, 20), seedManyPartitions())
	out := callMemoryRecall(t, tl, `{"query":"部署","limit":5}`)

	if !strings.Contains(out, "已达 limit") {
		t.Errorf("hitting limit must warn about truncation, got: %s", out)
	}
}

// TestTruncationHint_BelowLimit: full result sets carry no notice (no false
// "maybe more" when everything was returned).
func TestTruncationHint_BelowLimit(t *testing.T) {
	tl := NewMemoryRecallTool(seedManyStore(t, 3), seedManyPartitions())
	out := callMemoryRecall(t, tl, `{"query":"部署","limit":10}`)

	if strings.Contains(out, "已达 limit") {
		t.Errorf("complete result set must not warn about truncation, got: %s", out)
	}
}

// TestTruncationHint_Unit covers the shared helper directly, including the
// limit<=0 guard.
func TestTruncationHint_Unit(t *testing.T) {
	if got := truncationHint(10, 10); got == "" {
		t.Error("count == limit must produce a hint")
	}
	if got := truncationHint(9, 10); got != "" {
		t.Errorf("count < limit must produce no hint, got %q", got)
	}
	if got := truncationHint(0, 0); got != "" {
		t.Errorf("limit<=0 must produce no hint, got %q", got)
	}
}

// TestRecallTools_DeclarationDeterministic 验证 tasks 4.2：recall 工具 Declaration 确定性——
// 两次独立构造（模拟「配置开启向量前/后」）逐字节一致。结构性保证：recall 工具签名无向量
// 参数，向量能力经 accessor.SupportsVectorSearch() 在**运行时召回路径**判定，绝不进声明区
// → prefix-cache 稳定性不变量（声明区恒定，向量配置零触碰工具 Declaration）。
func TestRecallTools_DeclarationDeterministic(t *testing.T) {
	parts := []int{1}
	snapshot := func() map[string]string {
		accessor := memory.NewInMemoryStore()
		tools := map[string]tool.Tool{
			"recall_query":  NewRecallQueryTool(accessor, parts),
			"recall_get":    NewRecallGetTool(accessor),
			"recall_recent": NewRecallRecentTool(accessor, parts),
			"memory_recall": NewMemoryRecallTool(accessor, parts),
		}
		out := make(map[string]string, len(tools))
		for name, tl := range tools {
			raw, err := json.Marshal(tl.Declaration())
			if err != nil {
				t.Fatalf("%s Declaration marshal: %v", name, err)
			}
			out[name] = string(raw)
		}
		return out
	}

	a, b := snapshot(), snapshot()
	for name := range a {
		if a[name] != b[name] {
			t.Errorf("%s Declaration 非确定性（配置前后应逐字节一致）:\nA=%s\nB=%s", name, a[name], b[name])
		}
	}
	if len(a) != 4 {
		t.Fatalf("应覆盖 4 个 recall 工具, got %d", len(a))
	}
}

// TestRecallTools_DeclarationVectorFree 验证声明区不泄漏向量/embedding 配置字样——向量是
// 内部召回路径，工具对 LLM 呈现的声明与之无关（守 prefix-cache + 组8.3 声明区守卫）。
func TestRecallTools_DeclarationVectorFree(t *testing.T) {
	accessor := memory.NewInMemoryStore()
	tl := NewRecallQueryTool(accessor, []int{1})
	raw, _ := json.Marshal(tl.Declaration())
	lower := toLower(string(raw))
	for _, leak := range []string{"embedding", "embed_", "vectorstore", "hnsw", "rrf"} {
		if contains(lower, leak) {
			t.Errorf("recall_query Declaration 泄漏向量实现字样 %q（声明区应与向量配置无关）: %s", leak, raw)
		}
	}
}

// toLower 本地小工具（避免为测试引入 strings，保持包内自足）。
func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

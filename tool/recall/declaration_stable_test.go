package recall

import (
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestRecallTools_DeclarationDeterministic 验证 tasks 4.2：recall 工具 Declaration 确定性——
// 两次独立构造（模拟「配置开启向量前/后」）逐字节一致。结构性保证：recall 工具签名无向量
// 参数，向量能力经 accessor.SupportsVectorSearch() 在**运行时召回路径**判定，绝不进声明区
// → prefix-cache 稳定性不变量（声明区恒定，向量配置零触碰工具 Declaration）。
func TestRecallTools_DeclarationDeterministic(t *testing.T) {
	parts := []int{1}
	// snapshot 构造一组 recall 工具的 Declaration JSON。
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

// 本地小工具（避免为测试引入 strings，保持包内自足）。
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

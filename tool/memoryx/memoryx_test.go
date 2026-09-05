package memoryx

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// TestToolsConstruct 验证两个策展工具可构造（薄封装；核心逻辑 BuildConsolidationEvent/
// VerifyConsolidation/Diagnostics 已在 memory 包充分测试）。
func TestToolsConstruct(t *testing.T) {
	store := memory.NewInMemoryStore()
	if NewConsolidateTool(store, 1) == nil {
		t.Fatal("memory_consolidate 工具构造失败")
	}
	emb := memory.NewMockEmbedder(32)
	eng := memory.NewInMemoryEngine(store, emb, memory.EngineConfig{})
	defer eng.Close()
	if NewHealthTool(eng, store) == nil {
		t.Fatal("memory_health 工具构造失败")
	}
	// nil 源也不 panic（诊断工具容忍无引擎）。
	if NewHealthTool(nil, nil) == nil {
		t.Fatal("memory_health 应容忍 nil 源")
	}
}

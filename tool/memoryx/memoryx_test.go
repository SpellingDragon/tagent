package memoryx

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/engine"
)

// TestToolsConstruct pins that both curation tools are constructible as thin wrappers.
// - The core logic BuildConsolidationEvent, VerifyConsolidation and Diagnostics is exercised in the memory package.
//
// 契约: docs/wiki/memory/memory-architecture.md#curation
func TestToolsConstruct(t *testing.T) {
	store := memory.NewInMemoryStore()
	if NewConsolidateTool(store, 1) == nil {
		t.Fatal("memory_consolidate 工具构造失败")
	}
	emb := membed.NewMockEmbedder(32)
	eng := engine.NewInMemoryEngine(store, emb, engine.EngineConfig{})
	defer eng.Close()
	if NewHealthTool(eng, store) == nil {
		t.Fatal("memory_health 工具构造失败")
	}
	if NewHealthTool(nil, nil) == nil {
		t.Fatal("memory_health 应容忍 nil 源")
	}
}

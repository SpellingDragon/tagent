package memory_test

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/engine"
)

// TestErrorTrackingStore_OptionalInterfacePassthrough 本文件是记忆存储行为的测试执行体。
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestErrorTrackingStore_OptionalInterfacePassthrough(t *testing.T) {
	inner := memory.NewInMemoryStore()
	eng := engine.NewInMemoryEngine(inner, membed.NewMockEmbedder(16), engine.EngineConfig{})
	defer eng.Close()
	bridge := engine.NewEngineBridge(inner, eng)
	// 下游拿到的是 MemoryStore 接口（memStore），故赋给接口再断言可选能力穿透。
	var ms memory.MemoryStore = memory.NewErrorTrackingStore(bridge, nil)

	ep, ok := ms.(memory.MemoryEngineProvider)
	if !ok || ep.MemoryEngine() == nil {
		t.Fatal("ErrorTrackingStore 应透传 MemoryEngineProvider（否则 recall hybrid 失效）")
	}
	c, ok := ms.(interface{ Close() error })
	if !ok {
		t.Fatal("ErrorTrackingStore 应透传 Close（agent.Closer 引擎回收）")
	}
	_ = c.Close()
}

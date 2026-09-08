package memory_test

// ErrorTrackingStore 的可选能力透传测试（MemoryEngineProvider/Close）需构造
// memory/engine 的 engineBridge——以黑盒形式引用（白盒文件 import 子包会构成
// 测试 import 环：memory[test] → engine → memory）。

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/engine"
)

func TestErrorTrackingStore_OptionalInterfacePassthrough(t *testing.T) {
	// 包裹 engineBridge（有 MemoryEngine）→ ErrorTrackingStore 必须透传 MemoryEngineProvider，
	// 否则 recall hybrid 断言 memStore.(MemoryEngineProvider) 失效（能力丢失）。
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
	// Close 透传（agent 引擎回收依赖）。
	c, ok := ms.(interface{ Close() error })
	if !ok {
		t.Fatal("ErrorTrackingStore 应透传 Close（agent.Closer 引擎回收）")
	}
	_ = c.Close()
}

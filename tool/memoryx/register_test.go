package memoryx

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
)

// TestRegisterSubTools_RegistersFactories pins factory-style registration of the curation tools.
// - memory_consolidate and memory_health are held in the global factory table, which is how they reach the agent tool assembly.
// - Each factory produces a CallableTool from PlainToolFactoryConfig.MemStore at assembly time.
// - Constructor-style registration could not satisfy the agent context at global RegisterBuiltinTools time, which left the tools unreachable to the agent.
//
// 契约: docs/wiki/memory/memory-architecture.md#curation
func TestRegisterSubTools_RegistersFactories(t *testing.T) {
	RegisterSubTools()
	for _, id := range []string{"memory_consolidate", "memory_health"} {
		f, ok := agent.GetPlainToolFactory(id)
		if !ok || f == nil {
			t.Fatalf("%s 应注册到全局工厂表（A1 回归：此前从未注册）", id)
		}
		ct, err := f(agent.PlainToolFactoryConfig{MemStore: memory.NewInMemoryStore(), ReadPartitionIDs: []int{1}})
		if err != nil {
			t.Fatalf("%s factory 产出失败: %v", id, err)
		}
		if ct == nil || ct.Declaration() == nil {
			t.Fatalf("%s 应产出带 Declaration 的 CallableTool", id)
		}
	}
}

// TestConsolidateFactory_RequiresMemStore 验证工厂对缺 MemStore 显式失败（不静默产出坏工具）。
func TestConsolidateFactory_RequiresMemStore(t *testing.T) {
	if _, err := consolidateFactory(agent.PlainToolFactoryConfig{}); err == nil {
		t.Fatal("缺 MemStore 应显式失败")
	}
}

// TestHealthFactory_NoEngineDegrades 验证 MemStore 无引擎时 memory_health 仍产出工具：诊断省略向量维度并报告存储规模。
func TestHealthFactory_NoEngineDegrades(t *testing.T) {
	ct, err := healthFactory(agent.PlainToolFactoryConfig{MemStore: memory.NewInMemoryStore()})
	if err != nil || ct == nil {
		t.Fatalf("无引擎时 memory_health 仍应产出（降级诊断）: ct=%v err=%v", ct, err)
	}
}

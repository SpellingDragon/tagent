package memoryx

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
)

// TestRegisterSubTools_RegistersFactories 守卫 A1 回归：memory_consolidate/memory_health 经
// 工厂式注册接入 agent 工具装配链，工厂从 PlainToolFactoryConfig.MemStore 产出 CallableTool。
// 此前构造注入式（NewConsolidateTool(store,pid)）在全局 RegisterBuiltinTools 时刻无法满足
// agent 上下文 → 工具从未注册、agent 不可达（T-D 交付链断裂）。
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

// TestHealthFactory_NoEngineDegrades 验证 memory_health 在 MemStore 无引擎时仍产出工具
// （诊断省略向量维度、报告存储规模）——不因未配置语义检索而失败。
func TestHealthFactory_NoEngineDegrades(t *testing.T) {
	ct, err := healthFactory(agent.PlainToolFactoryConfig{MemStore: memory.NewInMemoryStore()})
	if err != nil || ct == nil {
		t.Fatalf("无引擎时 memory_health 仍应产出（降级诊断）: ct=%v err=%v", ct, err)
	}
}

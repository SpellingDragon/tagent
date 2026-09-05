package memoryx

import (
	"fmt"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ==================== 工厂式注册（A1 修复：接入 agent 工具装配链）====================
//
// 遵循仓库既定的工厂式注册习语（对照 recall.RegisterSubTools）：工厂从
// agent.PlainToolFactoryConfig 取 per-agent 的 MemStore/ReadPartitionIDs，而非构造注入。
// 此前 NewConsolidateTool/NewHealthTool 是构造注入式（store/pid 参数），在
// RegisterBuiltinTools()（全局、静态、无 agent 上下文）时刻无法满足 → 工具从未被注册、
// agent 不可达。本文件补上工厂适配 + RegisterSubTools，由 registry.go 的 registerOnce 调用。

// RegisterSubTools 注册记忆策展工具到全局注册表（memory_consolidate/memory_health）。
func RegisterSubTools() {
	agent.RegisterPlainTool("memory_consolidate", consolidateFactory)
	agent.RegisterPlainTool("memory_health", healthFactory)
}

func consolidateFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("memory_consolidate requires MemStore")
	}
	pid := 0
	if len(cfg.ReadPartitionIDs) > 0 {
		pid = cfg.ReadPartitionIDs[0] // 首个 = agent 自身写分区（巩固事件写入处，见 tagent.go resolveReadPartitions）
	}
	ct, ok := NewConsolidateTool(cfg.MemStore, pid).(tool.CallableTool)
	if !ok {
		return nil, fmt.Errorf("memory_consolidate: inner tool is not CallableTool")
	}
	return ct, nil
}

func healthFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	// memory_health 需引擎统计：从 MemStore 提取引擎（engineBridge 实现 MemoryEngineProvider）。
	// MemStore 无引擎（未配置语义检索）则 eng=nil，诊断省略向量维度、仍报告存储规模。
	var eng memory.MemoryEngine
	if ep, ok := cfg.MemStore.(memory.MemoryEngineProvider); ok {
		eng = ep.MemoryEngine()
	}
	ht, ok := NewHealthTool(eng, cfg.MemStore).(tool.CallableTool)
	if !ok {
		return nil, fmt.Errorf("memory_health: inner tool is not CallableTool")
	}
	return ht, nil
}

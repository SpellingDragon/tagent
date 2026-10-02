package memoryx

import (
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
)

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
		pid = cfg.ReadPartitionIDs[0]
	}
	ct, ok := NewConsolidateToolWithGate(cfg.MemStore, pid, cfg.ConsolidationMinSources).(tool.CallableTool)
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

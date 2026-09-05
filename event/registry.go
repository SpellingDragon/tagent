package event

import (
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ==================== 事件类型注册表（REG · 契约 C1）====================
//
// 目的：把「新增一个事件类型要改约 10 处」收敛为「注册一条 EventTypeSpec」。
// 注册表是事件类型声明式元数据的**唯一权威源**；既有函数（IsSpecialEventType /
// GenerateEventSummary / EventTypeToRole / IsSkeletonMessage）与既有变量
// （memory.LowValueEventTypes / lifecycle TypeTTL 默认）全部**委托/派生**自本注册表。
//
// 等价验收线：注册表复现全部 9 个内置类型的现有行为，既有测试零修改通过。
//
// 新增类型（consolidation/governance/feedback/...）只需在 init() 或调用方
// RegisterEventType 一条 spec，全链路（摘要/骨架/TTL/低价值/角色/可嵌入/可召回）
// 自动生效——这是 T-D/T-G/T-EVO 引入新类型的共同前置。
//
// 冻结纪律：EventTypeSpec 字段集即契约 C1。变更须走 execution-dag.md §4.2 ESCALATE。

// EventTypeSpec 声明一个事件类型的全链路元数据。
// 零值语义见各字段注释；未注册类型经 specOrDefault 回退到 defaultSpec，
// 精确复现既有函数对未知类型的 fallback 行为。
type EventTypeSpec struct {
	// Name 是类型常量值（如 "external_input"），注册表主键。
	Name string

	// Role 是时间线渲染角色（EventTypeToRole）。未知类型回退 RoleUser。
	Role model.Role

	// Special 标记「原文优先」类型（IsSpecialEventType）：external_input /
	// agent_output / thinking_plan。决定 GenerateEventSummary 走原文分支的优先级。
	Special bool

	// ToolLineSummary 标记摘要用「工具调用行」而非原文（仅 action_command）。
	ToolLineSummary bool

	// Skeleton 标记压缩骨架节点（IsSkeletonMessage）。false 仅 action_command /
	// thinking_plan（可丢弃中间事件）；其余（含未知类型）保守为 true，永不段内丢弃。
	Skeleton bool

	// LowValue 标记 L3 可丢弃 Content/ToolCalls 的类型（memory.LowValueEventTypes）：
	// thinking_plan / context_compress。
	LowValue bool

	// TTLDays 是类型级 TTL（天），派生 memory.lifecycle 的 TypeTTL 默认：
	//   0  = 继承全局默认（不进 TypeTTL map）
	//   -1 = 豁免遗忘（长期记忆，如 context_compress_summary）
	//   >0 = 显式天数
	TTLDays int

	// Synthetic 标记「合成投影引用」类型（负 EventKey，非落库真实事件）：
	// context_compress（滚动摘要 ref）/ tool_chain（工具链折叠 ref）。
	// 供存储/召回/渲染区分正负 key 语义（不变量：正负 key）。
	Synthetic bool

	// Embeddable 标记是否纳入向量索引（T-A 选择性生成）。默认仅
	// external_input / agent_output / thinking_knowledge / context_compress_summary。
	Embeddable bool

	// Recallable 标记是否可被 recall 取回原文（票据有效）。内置类型均可召回。
	Recallable bool
}

var (
	registryMu sync.RWMutex
	// eventTypeRegistry 是类型名 → spec 的注册表。
	eventTypeRegistry = make(map[string]EventTypeSpec)
)

// defaultSpec 复现既有函数对**未知类型**的 fallback：
// Role=user、非 special、非 tool-line、骨架保守 true、非低价值、TTL 继承、非合成。
var defaultSpec = EventTypeSpec{
	Role:         model.RoleUser,
	Special:      false,
	ToolLineSummary: false,
	Skeleton:     true,
	LowValue:     false,
	TTLDays:      0,
	Synthetic:    false,
	Embeddable:   false,
	Recallable:   true,
}

// RegisterEventType 注册（或覆盖）一个事件类型 spec。
// 内置类型在 init() 注册；新类型（consolidation/governance/feedback）可由
// 各自子系统在 init() 注册，实现「一处注册、全链路生效」。
// 幂等：同名覆盖（允许子系统显式重声明）。
func RegisterEventType(spec EventTypeSpec) {
	if spec.Name == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	eventTypeRegistry[spec.Name] = spec
}

// LookupEventType 返回类型 spec 与是否存在。
func LookupEventType(name string) (EventTypeSpec, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	spec, ok := eventTypeRegistry[name]
	return spec, ok
}

// specOrDefault 返回类型 spec；未注册则回退 defaultSpec（复现未知类型 fallback）。
func specOrDefault(name string) EventTypeSpec {
	if spec, ok := LookupEventType(name); ok {
		return spec
	}
	return defaultSpec
}

// ==================== 派生访问器（既有函数/变量委托这些）====================

// EventTypeRole 返回类型的渲染角色（委托注册表；未知类型回退 RoleUser）。
// 供 agent/compress.EventTypeToRole 委托。
func EventTypeRole(name string) model.Role {
	return specOrDefault(name).Role
}

// IsLowValueType 报告类型是否 L3 可丢弃内容（委托注册表）。
func IsLowValueType(name string) bool {
	return specOrDefault(name).LowValue
}

// LowValueTypes 返回全部低价值类型集合，供 memory.LowValueEventTypes 派生。
// 返回新 map（调用方持有，不与注册表共享可变状态）。
func LowValueTypes() map[string]bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]bool)
	for name, spec := range eventTypeRegistry {
		if spec.LowValue {
			out[name] = true
		}
	}
	return out
}

// DefaultTypeTTL 返回全部显式 TTLDays（非 0）的类型 → 天数映射，
// 供 memory.lifecycle DefaultLifecycleConfig 的 TypeTTL 派生。
// 含 -1（豁免）。返回新 map。
func DefaultTypeTTL() map[string]int {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]int)
	for name, spec := range eventTypeRegistry {
		if spec.TTLDays != 0 {
			out[name] = spec.TTLDays
		}
	}
	return out
}

// IsSyntheticEventType 报告类型是否为合成投影引用（负 key）。
func IsSyntheticEventType(name string) bool {
	return specOrDefault(name).Synthetic
}

// IsEmbeddableType 报告类型是否纳入向量索引（T-A 选择性生成）。
func IsEmbeddableType(name string) bool {
	return specOrDefault(name).Embeddable
}

// IsRecallableType 报告类型是否可被 recall 取回。
func IsRecallableType(name string) bool {
	return specOrDefault(name).Recallable
}

// IsSkeletonEventType 报告类型是否压缩骨架节点（委托注册表）。
// 供 agent/compress.IsSkeletonMessage 委托（未知类型保守 true）。
func IsSkeletonEventType(name string) bool {
	return specOrDefault(name).Skeleton
}

// RegisteredEventTypes 返回全部已注册类型名（诊断/测试用）。
func RegisteredEventTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(eventTypeRegistry))
	for name := range eventTypeRegistry {
		out = append(out, name)
	}
	return out
}

// ==================== 内置 9 类型注册（复现现有行为，等价验收线）====================

func init() {
	builtin := []EventTypeSpec{
		{Name: TypeExternalInput, Role: model.RoleUser, Special: true, Skeleton: true, TTLDays: 30, Embeddable: true, Recallable: true},
		{Name: TypeAgentOutput, Role: model.RoleAssistant, Special: true, Skeleton: true, TTLDays: 14, Embeddable: true, Recallable: true},
		{Name: TypeActionCommand, Role: model.RoleUser, ToolLineSummary: true, Skeleton: false, TTLDays: 14, Recallable: true},
		{Name: TypeThinkingPlan, Role: model.RoleAssistant, Special: true, Skeleton: false, LowValue: true, TTLDays: 3, Recallable: true},
		{Name: TypeThinkingRecall, Role: model.RoleUser, Skeleton: true, Recallable: true},
		{Name: TypeThinkingKnowledge, Role: model.RoleUser, Skeleton: true, Embeddable: true, Recallable: true},
		// 策展固化物：长期记忆，TTL 豁免（-1）；正 key 真实存储事件。
		{Name: TypeContextCompressSummary, Role: model.RoleUser, Skeleton: true, TTLDays: -1, Embeddable: true, Recallable: true},
		// 滚动摘要 ref：合成负 key；低价值；TTL 3 天。
		{Name: TypeContextCompress, Role: model.RoleUser, Skeleton: true, LowValue: true, TTLDays: 3, Synthetic: true, Recallable: true},
		// 工具链折叠 ref：合成负 key。
		{Name: TypeToolChain, Role: model.RoleUser, Skeleton: true, Synthetic: true, Recallable: true},
	}
	for _, spec := range builtin {
		RegisterEventType(spec)
	}
}

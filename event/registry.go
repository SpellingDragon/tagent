package event

import (
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// EventTypeSpec 声明一个事件类型的全链路静态属性：角色、是否原文优先、摘要形态、
// 是否压缩骨架、是否低价值、类型级 TTL、是否合成投影引用、是否可嵌入、是否可召回、
// 是否永不进投影。未注册类型回退 defaultSpec，与引入注册表前对未知类型的处理一致。
//
// 契约: docs/wiki/event/event-architecture.md#registry-authority
type EventTypeSpec struct {
	Name string

	Role model.Role

	Special bool

	ToolLineSummary bool

	Skeleton bool

	LowValue bool

	TTLDays int

	Synthetic bool

	Embeddable bool

	Recallable bool

	NonProjection bool
}

// registryMu defaultSpec 是未注册类型的回退：角色 user、非 special、非工具行、骨架保守 true、
// 非低价值、TTL 继承全局、非合成、不可嵌入、可召回、进投影。
var (
	registryMu        sync.RWMutex
	eventTypeRegistry = make(map[string]EventTypeSpec)
)

var defaultSpec = EventTypeSpec{
	Role:            model.RoleUser,
	Special:         false,
	ToolLineSummary: false,
	Skeleton:        true,
	LowValue:        false,
	TTLDays:         0,
	Synthetic:       false,
	Embeddable:      false,
	Recallable:      true,
}

// RegisterEventType 注册或覆盖一个类型 spec（同名覆盖，供子系统显式重声明）。
// 空 Name 被忽略。全链路属性由这一处声明派生。
func RegisterEventType(spec EventTypeSpec) {
	if spec.Name == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	eventTypeRegistry[spec.Name] = spec
}

// LookupEventType 返回类型 spec 及是否已注册。
func LookupEventType(name string) (EventTypeSpec, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	spec, ok := eventTypeRegistry[name]
	return spec, ok
}

// specOrDefault 返回类型 spec，未注册时回退 defaultSpec。
func specOrDefault(name string) EventTypeSpec {
	if spec, ok := LookupEventType(name); ok {
		return spec
	}
	return defaultSpec
}

// EventTypeRole 返回类型的渲染角色。
func EventTypeRole(name string) model.Role {
	return specOrDefault(name).Role
}

// IsLowValueType 报告类型内容是否可在深层压缩中丢弃。
func IsLowValueType(name string) bool {
	return specOrDefault(name).LowValue
}

// LowValueTypes 返回全部低价值类型名（新 map，调用方可自行持有）。
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

// DefaultTypeTTL 返回全部显式声明 TTLDays（非 0，含 -1 豁免）的类型→天数映射（新 map）。
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

// IsSyntheticEventType 报告类型是否为使用负 EventKey 的合成投影引用。
func IsSyntheticEventType(name string) bool {
	return specOrDefault(name).Synthetic
}

// IsEmbeddableType 报告类型是否纳入向量索引。
func IsEmbeddableType(name string) bool {
	return specOrDefault(name).Embeddable
}

// IsRecallableType 报告类型的原文票据能否被取回。
func IsRecallableType(name string) bool {
	return specOrDefault(name).Recallable
}

// IsSkeletonEventType 报告类型是否为压缩骨架节点（未知类型保守为 true）。
func IsSkeletonEventType(name string) bool {
	return specOrDefault(name).Skeleton
}

// IsNonProjectionEventType 报告类型是否事实链内部记录（永不进投影）。
func IsNonProjectionEventType(name string) bool {
	return specOrDefault(name).NonProjection
}

// IsNonProjectionRecord 是「可否进投影」的唯一判定源：类型维度取注册表，外加携带
// task_inline_record 标记的事件（终态 settle 已在回合内返回，再进投影即双呈现）。
// 所有 append 路径必须共用本谓词。
func IsNonProjectionRecord(eventType string, metadata map[string]string) bool {
	if IsNonProjectionEventType(eventType) {
		return true
	}
	return metadata[MetaKeyTaskInlineRecord] != ""
}

// RegisteredEventTypes 返回全部已注册类型名，供诊断与守卫断言使用。
func RegisteredEventTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(eventTypeRegistry))
	for name := range eventTypeRegistry {
		out = append(out, name)
	}
	return out
}

// init 内置类型在包初始化时注册；跨重启与恢复期的行为差异见文档的类型理由表。
func init() {
	builtin := []EventTypeSpec{
		{Name: TypeExternalInput, Role: model.RoleUser, Special: true, Skeleton: true, TTLDays: 30, Embeddable: true, Recallable: true},
		{Name: TypeAgentOutput, Role: model.RoleAssistant, Special: true, Skeleton: true, TTLDays: 14, Embeddable: true, Recallable: true},
		{Name: TypeActionCommand, Role: model.RoleUser, ToolLineSummary: true, Skeleton: false, TTLDays: 14, Recallable: true},
		{Name: TypeThinkingPlan, Role: model.RoleAssistant, Special: true, Skeleton: false, LowValue: true, TTLDays: 3, Recallable: true},
		{Name: TypeThinkingRecall, Role: model.RoleUser, Skeleton: true, Recallable: true},
		{Name: TypeThinkingKnowledge, Role: model.RoleUser, Skeleton: true, Embeddable: true, Recallable: true},
		{Name: TypeContextCompressSummary, Role: model.RoleUser, Skeleton: true, TTLDays: -1, Embeddable: true, Recallable: true, NonProjection: true},
		{Name: TypeContextCompress, Role: model.RoleUser, Skeleton: true, LowValue: true, TTLDays: 3, Synthetic: true, Recallable: true},
		{Name: TypeToolChain, Role: model.RoleUser, Skeleton: true, Synthetic: true, Recallable: true},
		{Name: TypeSettleFold, Role: model.RoleUser, Skeleton: true, Synthetic: true, Recallable: true},
		{Name: TypeTaskSpawned, Role: model.RoleUser, Skeleton: true, TTLDays: 30, Recallable: true, NonProjection: true},
		{Name: TypeResidentSession, Role: model.RoleUser, Skeleton: true, TTLDays: 30, Recallable: true, NonProjection: true},
		{Name: TypeConsolidation, Role: model.RoleSystem, Skeleton: true, TTLDays: -1, Embeddable: true, Recallable: true},
		{Name: TypeGovernance, Role: model.RoleSystem, Skeleton: false, TTLDays: -1, Embeddable: false, Recallable: true},
		{Name: TypeFeedback, Role: model.RoleSystem, Skeleton: true, TTLDays: 30, Embeddable: false, Recallable: true},
	}
	for _, spec := range builtin {
		RegisterEventType(spec)
	}
}

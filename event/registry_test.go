package event

import (
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestRegistryReproducesBuiltinBehavior 是重构等价验收线的核心：
// 注册表对 9 个内置类型的声明式元数据必须精确复现委托前各函数的行为。
func TestRegistryReproducesBuiltinBehavior(t *testing.T) {
	cases := []struct {
		name      string
		special   bool // IsSpecialEventType
		toolLine  bool // GenerateEventSummary 走工具行
		skeleton  bool // IsSkeletonMessage
		lowValue  bool // LowValueEventTypes
		ttl       int  // TypeTTL（0=继承全局）
		role      model.Role
		synthetic bool
	}{
		{TypeExternalInput, true, false, true, false, 30, model.RoleUser, false},
		{TypeAgentOutput, true, false, true, false, 14, model.RoleAssistant, false},
		{TypeActionCommand, false, true, false, false, 14, model.RoleUser, false},
		{TypeThinkingPlan, true, false, false, true, 3, model.RoleAssistant, false},
		{TypeThinkingRecall, false, false, true, false, 0, model.RoleUser, false},
		{TypeThinkingKnowledge, false, false, true, false, 0, model.RoleUser, false},
		{TypeContextCompressSummary, false, false, true, false, -1, model.RoleUser, false},
		{TypeContextCompress, false, false, true, true, 3, model.RoleUser, true},
		{TypeToolChain, false, false, true, false, 0, model.RoleUser, true},
	}
	for _, c := range cases {
		spec, ok := LookupEventType(c.name)
		if !ok {
			t.Fatalf("%s 未注册", c.name)
		}
		if got := IsSpecialEventType(c.name); got != c.special {
			t.Errorf("%s IsSpecial=%v 期望 %v", c.name, got, c.special)
		}
		if spec.ToolLineSummary != c.toolLine {
			t.Errorf("%s ToolLineSummary=%v 期望 %v", c.name, spec.ToolLineSummary, c.toolLine)
		}
		if got := IsSkeletonEventType(c.name); got != c.skeleton {
			t.Errorf("%s Skeleton=%v 期望 %v", c.name, got, c.skeleton)
		}
		if got := IsLowValueType(c.name); got != c.lowValue {
			t.Errorf("%s LowValue=%v 期望 %v", c.name, got, c.lowValue)
		}
		if spec.TTLDays != c.ttl {
			t.Errorf("%s TTLDays=%d 期望 %d", c.name, spec.TTLDays, c.ttl)
		}
		if got := EventTypeRole(c.name); got != c.role {
			t.Errorf("%s Role=%v 期望 %v", c.name, got, c.role)
		}
		if got := IsSyntheticEventType(c.name); got != c.synthetic {
			t.Errorf("%s Synthetic=%v 期望 %v", c.name, got, c.synthetic)
		}
	}
}

// TestRegistryUnknownTypeFallback 验证未知类型回退，精确复现委托前的 default 分支。
func TestRegistryUnknownTypeFallback(t *testing.T) {
	const unknown = "some_future_type_xyz"
	if IsSpecialEventType(unknown) {
		t.Error("未知类型 IsSpecial 应为 false")
	}
	if !IsSkeletonEventType(unknown) {
		t.Error("未知类型 Skeleton 应保守为 true")
	}
	if IsLowValueType(unknown) {
		t.Error("未知类型 LowValue 应为 false")
	}
	if got := EventTypeRole(unknown); got != model.RoleUser {
		t.Errorf("未知类型 Role=%v 应回退 RoleUser", got)
	}
	if IsSyntheticEventType(unknown) {
		t.Error("未知类型 Synthetic 应为 false")
	}
	if _, ok := LookupEventType(unknown); ok {
		t.Error("未知类型不应在注册表中")
	}
}

// TestRegistryDerivedSetsMatchLegacy 验证派生集合与委托前的字面量精确一致。
func TestRegistryDerivedSetsMatchLegacy(t *testing.T) {
	lowValue := LowValueTypes()
	wantLow := map[string]bool{TypeThinkingPlan: true, TypeContextCompress: true}
	if len(lowValue) != len(wantLow) {
		t.Fatalf("LowValueTypes 数量=%d 期望 %d: %v", len(lowValue), len(wantLow), lowValue)
	}
	for k := range wantLow {
		if !lowValue[k] {
			t.Errorf("LowValueTypes 缺 %s", k)
		}
	}

	ttl := DefaultTypeTTL()
	wantTTL := map[string]int{
		TypeContextCompress:        3,
		TypeThinkingPlan:           3,
		TypeExternalInput:          30,
		TypeAgentOutput:            14,
		TypeActionCommand:          14,
		TypeContextCompressSummary: -1,
		TypeConsolidation:          -1, // T-D 追加：巩固产物 TTL 豁免（长期记忆）
	}
	if len(ttl) != len(wantTTL) {
		t.Fatalf("DefaultTypeTTL 数量=%d 期望 %d: %v", len(ttl), len(wantTTL), ttl)
	}
	for k, v := range wantTTL {
		if ttl[k] != v {
			t.Errorf("DefaultTypeTTL[%s]=%d 期望 %d", k, ttl[k], v)
		}
	}
}

// TestRegistryOneRegistrationWholeChain 验证核心价值：注册一条新 spec，
// 全链路访问器（摘要/骨架/TTL/低价值/角色/可嵌入/可召回）自动生效——
// 收敛「改 10 处」为一处注册。
func TestRegistryOneRegistrationWholeChain(t *testing.T) {
	const newType = "consolidation_probe"
	RegisterEventType(EventTypeSpec{
		Name:       newType,
		Role:       model.RoleSystem,
		Special:    false,
		Skeleton:   true,
		LowValue:   false,
		TTLDays:    -1, // 豁免遗忘（长期记忆）
		Synthetic:  false,
		Embeddable: true,
		Recallable: true,
	})
	// 一处注册后，全链路自动生效：
	if got := EventTypeRole(newType); got != model.RoleSystem {
		t.Errorf("新类型 Role=%v 期望 RoleSystem", got)
	}
	if !IsSkeletonEventType(newType) {
		t.Error("新类型应骨架保留")
	}
	if ttl := DefaultTypeTTL()[newType]; ttl != -1 {
		t.Errorf("新类型 TTL=%d 期望 -1（豁免）", ttl)
	}
	if !IsEmbeddableType(newType) {
		t.Error("新类型应可嵌入")
	}
	if !IsRecallableType(newType) {
		t.Error("新类型应可召回")
	}
	if IsLowValueType(newType) {
		t.Error("新类型不应低价值")
	}
}

package event

import (
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestRegistryReproducesBuiltinBehavior 钉住内置类型的注册表声明：每个类型的原文优先、
//
// 契约: docs/wiki/event/event-architecture.md#registry-authority
func TestRegistryReproducesBuiltinBehavior(t *testing.T) {
	cases := []struct {
		name      string
		special   bool
		toolLine  bool
		skeleton  bool
		lowValue  bool
		ttl       int
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

// TestRegistryUnknownTypeFallback 钉住 未知类型的回退值：IsSpecial 为 false、Skeleton 保守为 true、LowValue 为 false。
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

// TestRegistryDerivedSetsMatchDeclaredTable 钉住 由注册表派生的跨包集合与声明表逐项相符：LowValueTypes 与 DefaultTypeTTL 的数量和取值都不得偏离。
func TestRegistryDerivedSetsMatchDeclaredTable(t *testing.T) {
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
		TypeConsolidation:          -1,
		TypeGovernance:             -1,
		TypeFeedback:               30,
		TypeTaskSpawned:            30,
		TypeResidentSession:        30,
		TypeInboxReceipt:           30,
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

// TestRegistryOneRegistrationWholeChain 钉住 注册表的单一声明性：注册一条新 spec 后，角色、TTL、可嵌入性等各面取值同时生效，无需再改第二处。
func TestRegistryOneRegistrationWholeChain(t *testing.T) {
	const newType = "consolidation_probe"
	RegisterEventType(EventTypeSpec{
		Name:       newType,
		Role:       model.RoleSystem,
		Special:    false,
		Skeleton:   true,
		LowValue:   false,
		TTLDays:    -1,
		Synthetic:  false,
		Embeddable: true,
		Recallable: true,
	})
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

// TestFeedbackEventRegistered 钉住 feedback 事件已注册：spec 取值正确、TTL 为 30 天，且出现在 RegisteredEventTypes 中。
func TestFeedbackEventRegistered(t *testing.T) {
	spec, ok := LookupEventType(TypeFeedback)
	if !ok {
		t.Fatal("feedback not registered")
	}
	if !spec.Recallable || spec.LowValue {
		t.Fatalf("feedback spec wrong: %+v", spec)
	}
	if spec.TTLDays != 30 {
		t.Fatalf("feedback TTL = %d, want 30", spec.TTLDays)
	}
	if found := false; !found {
		for _, n := range RegisteredEventTypes() {
			if n == TypeFeedback {
				found = true
			}
		}
		if !found {
			t.Fatal("feedback missing from RegisteredEventTypes")
		}
	}
}

// TestGovernanceNotSkeletonized 钉住 governance 事件不得被骨架化——审计与目标重建需要全文。
func TestGovernanceNotSkeletonized(t *testing.T) {
	if IsSkeletonEventType(TypeGovernance) {
		t.Fatal("governance must not be skeletonized (audit/goal-rebuild needs full text)")
	}
}

// TestIsNonProjectionEventType_Declarations 钉住 非投影声明集由注册表唯一提供：inbox receipt、task_spawned、resident_session 属内部或审计记录，compaction 事件正文由载荷重建。
func TestIsNonProjectionEventType_Declarations(t *testing.T) {
	require.True(t, IsNonProjectionEventType(TypeInboxReceipt), "inbox receipt is the current internal record class")
	require.True(t, IsNonProjectionEventType(TypeTaskSpawned), "task_spawned is registry data")
	require.True(t, IsNonProjectionEventType(TypeResidentSession), "resident_session is an audit record")
	require.True(t, IsNonProjectionEventType(TypeContextCompressSummary), "the compaction event body is re-folded from its payload")

	require.False(t, IsNonProjectionEventType(TypeExternalInput))
	require.False(t, IsNonProjectionEventType(TypeAgentOutput))
	require.False(t, IsNonProjectionEventType(TypeActionCommand))

	require.False(t, IsNonProjectionEventType("some_future_business_type"))
}

func TestIsNonProjectionRecord_MetadataClause(t *testing.T) {
	require.True(t, IsNonProjectionRecord(TypeAgentOutput, map[string]string{MetaKeyTaskInlineRecord: "true"}))
	require.False(t, IsNonProjectionRecord(TypeAgentOutput, map[string]string{MetaKeyTaskInlineRecord: ""}))
	require.False(t, IsNonProjectionRecord(TypeAgentOutput, nil))
	require.False(t, IsNonProjectionRecord(TypeExternalInput, map[string]string{"unrelated": "x"}))
}

// TestWFFacts_NeverProjectionRecord 钉住 wf.* 内部事实在唯一判定源下永不被投影，而 external_input 保持可投影。
func TestWFFacts_NeverProjectionRecord(t *testing.T) {
	for _, name := range WFExcludedTypes() {
		if IsNonProjectionRecord(name, map[string]string{}) {
			continue
		}
		t.Errorf("IsNonProjectionRecord(%s) = false; wf.* internal facts must never project", name)
	}
	if IsNonProjectionRecord(TypeExternalInput, nil) {
		t.Error("external_input must stay projectable")
	}
}

// TestWFPrefixFamilyIsClosed 钉住 家族封闭性：任何 wf. 前缀类型都必须在排除清单内。
func TestWFPrefixFamilyIsClosed(t *testing.T) {
	inList := map[string]bool{}
	for _, name := range WFExcludedTypes() {
		inList[name] = true
	}
	for _, name := range RegisteredEventTypes() {
		if len(name) > 3 && name[:3] == "wf." && !inList[name] {
			t.Errorf("type %q uses the wf. prefix but is not in WFExcludedTypes()", name)
		}
	}
}

// TestWFFacts_PassiveExclusionIntroducesNoTTL 钉住 被动排除的边界：wf.* 仍须保持注册以供排除判定，但不得因此获得 TTL。
func TestWFFacts_PassiveExclusionIntroducesNoTTL(t *testing.T) {
	typeTTL := DefaultTypeTTL()
	for _, name := range WFExcludedTypes() {
		spec, ok := LookupEventType(name)
		if !ok {
			t.Fatalf("wf type %q must stay registered for passive exclusion", name)
		}
		if spec.TTLDays != 0 {
			t.Errorf("wf type %q TTLDays = %d, want 0: passive exclusion must inherit the global TTL, never shorten history retention", name, spec.TTLDays)
		}
		if _, present := typeTTL[name]; present {
			t.Errorf("wf type %q leaked into DefaultTypeTTL()=%d; a passive-exclusion registration must add no type TTL", name, typeTTL[name])
		}
		if !spec.NonProjection {
			t.Errorf("wf type %q NonProjection = false; passive exclusion must be preserved", name)
		}
		if spec.Embeddable {
			t.Errorf("wf type %q Embeddable = true; internal facts must never be embedded", name)
		}
		if spec.Recallable {
			t.Errorf("wf type %q Recallable = true; internal facts must never be recalled", name)
		}
	}
}

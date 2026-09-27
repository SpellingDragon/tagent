package event

import "testing"

// §5.5/1.1 守卫：wf.* 内部事实在唯一判定源下被排除。三条 append 路径（正常
// 提交 persistBusEvent、在线 spill 回补、冷启动重建）共用 IsNonProjectionRecord，
// 本断言即覆盖三路径的类型维度——任何路径私设枚举都会被注册表漂移打爆。
func TestWFFacts_NeverProjectionRecord(t *testing.T) {
	for _, name := range WFExcludedTypes() {
		if IsNonProjectionRecord(name, map[string]string{}) {
			continue
		}
		t.Errorf("IsNonProjectionRecord(%s) = false; wf.* internal facts must never project", name)
	}
	// 业务类型不受影响（对照位）：external_input 必须仍可投影。
	if IsNonProjectionRecord(TypeExternalInput, nil) {
		t.Error("external_input must stay projectable")
	}
}

// 家族封闭性：任何新注册的 wf. 前缀类型都必须同时进入排除清单，
// 防止新增事实类型漏声明排除（fail-before：加类型不加清单为红）。
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

// R05/6.2 契约：wf.* 只是历史试验内部记录的**被动排除**，注册它的唯一目的是让
// 三条 append 路径把它挡在投影/召回/嵌入之外。这 MUST NOT 顺带引入类型级 TTL——
// 否则 30 天专用策略会覆盖既有全局/显式保留，把一条本应活到全局期限的历史记录
// 提前淘汰（本轮之前正是如此：init 里 TTLDays:30）。排除语义（NonProjection/
// 不可嵌入/不可召回）必须保持；TTL 必须为 0（继承全局，不进 DefaultTypeTTL map）。
// fail-before：修复前 TTLDays==30，本测为红。
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
		// 排除语义不得随 TTL 一起被削弱（三路投影/召回/嵌入判定的类型维度）。
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

package memory

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/event"
)

func seedSourceEvent(t *testing.T, s *InMemoryStore, pid int, content string, ts int64) int64 {
	t.Helper()
	key := NewSnowflakeEventKey(pid, ts)
	if err := s.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: pid, EventType: event.TypeExternalInput,
		Content: content, EventSummary: content, Timestamp: ts,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return key
}

// TestComputeReceiptFingerprint_DeterministicOrderIndependent 验证指纹确定性且与输入顺序无关
// （内部按 key 排序），且覆盖 content（改内容 → 指纹变，防漂移）。
func TestComputeReceiptFingerprint_DeterministicOrderIndependent(t *testing.T) {
	a := FullEvent{EventKey: 100, EventType: "external_input", Content: "alpha"}
	b := FullEvent{EventKey: 200, EventType: "external_input", Content: "beta"}
	fp1 := ComputeReceiptFingerprint([]FullEvent{a, b})
	fp2 := ComputeReceiptFingerprint([]FullEvent{b, a}) // 乱序
	if fp1 != fp2 {
		t.Fatalf("指纹应与顺序无关: %s vs %s", fp1, fp2)
	}
	if !strings.HasPrefix(fp1, "sha1:") {
		t.Fatalf("指纹应 sha1: 前缀, got %s", fp1)
	}
	// 改 content → 指纹变（防篡改/漂移）。
	b2 := FullEvent{EventKey: 200, EventType: "external_input", Content: "beta-TAMPERED"}
	if ComputeReceiptFingerprint([]FullEvent{a, b2}) == fp1 {
		t.Fatal("content 改变应改变指纹（防漂移）")
	}
}

// TestBuildAndVerifyConsolidation 验证服务端构造 + 回放验证闭环：LLM 无法伪造指纹。
func TestBuildAndVerifyConsolidation(t *testing.T) {
	store := NewInMemoryStore()
	k1 := seedSourceEvent(t, store, 1, "部署失败：数据库连接超时", 1750000000000)
	k2 := seedSourceEvent(t, store, 1, "重试后部署成功", 1750000001000)

	evt, verdict, err := BuildConsolidationEvent(store, 1,
		"经验：部署遇数据库超时应重试", "experience_distill", "meditation", []int64{k1, k2})
	if err != nil {
		t.Fatalf("BuildConsolidationEvent: %v", err)
	}
	if evt.EventType != event.TypeConsolidation {
		t.Fatalf("事件类型应 consolidation, got %s", evt.EventType)
	}
	if verdict.Resolved != 2 || verdict.Tombstoned != 0 {
		t.Fatalf("应取回 2 源事件, got %+v", verdict)
	}
	// 服务端指纹已写入 Metadata。
	fp := evt.Metadata[MetaReceiptFingerprint]
	if !strings.HasPrefix(fp, "sha1:") {
		t.Fatalf("应有服务端指纹, got %q", fp)
	}
	// 存巩固事件后回放验证：指纹匹配。
	if err := store.StoreEvent(evt.EventKey, evt); err != nil {
		t.Fatalf("store consolidation: %v", err)
	}
	got := VerifyConsolidation(store, evt)
	if !got.FingerprintMatch || got.Resolved != 2 {
		t.Fatalf("回放验证应指纹匹配, got %+v", got)
	}

	// 防伪造：LLM 篡改 content 但保留原指纹 → 验证失败（指纹不匹配）。
	forged := evt
	forged.Content = "伪造的经验"
	forged.Metadata = map[string]string{MetaReceiptKeys: evt.Metadata[MetaReceiptKeys], MetaReceiptFingerprint: "sha1:forged000"}
	if v := VerifyConsolidation(store, forged); v.FingerprintMatch {
		t.Fatal("伪造指纹不应匹配（服务端指纹防伪造）")
	}
}

// TestVerifyConsolidation_TombstonedIsHonestDecay 验证源事件被删后收据进入 Tombstoned
// （诚实衰减信号，非错误），指纹不可判。
func TestVerifyConsolidation_TombstonedIsHonestDecay(t *testing.T) {
	store := NewInMemoryStore()
	k1 := seedSourceEvent(t, store, 1, "源事件A", 1750000000000)
	k2 := seedSourceEvent(t, store, 1, "源事件B", 1750000001000)
	evt, _, err := BuildConsolidationEvent(store, 1, "巩固", "manual", "manual", []int64{k1, k2})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// 删除一个源事件（模拟 TTL 遗忘）。
	if err := store.DeleteEvent(k1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	v := VerifyConsolidation(store, evt)
	if v.Tombstoned != 1 || v.Resolved != 1 {
		t.Fatalf("应 1 墓碑 1 取回, got %+v", v)
	}
	if v.FingerprintMatch {
		t.Fatal("有墓碑时指纹不可判（应 false）")
	}
}

// TestConsolidationRegisteredTTLExempt 验证 consolidation 经注册表一处注册即 TTL 豁免
// （REG 收敛「改 10 处」的兑现：长期记忆不被遗忘）。
func TestConsolidationRegisteredTTLExempt(t *testing.T) {
	if ttl := event.DefaultTypeTTL()[event.TypeConsolidation]; ttl != -1 {
		t.Fatalf("consolidation 应 TTL 豁免(-1), got %d", ttl)
	}
	if !event.IsEmbeddableType(event.TypeConsolidation) {
		t.Error("consolidation 应可嵌入")
	}
	if !event.IsRecallableType(event.TypeConsolidation) {
		t.Error("consolidation 应可召回")
	}
	if !event.IsSkeletonEventType(event.TypeConsolidation) {
		t.Error("consolidation 应骨架保留（压缩不丢）")
	}
	if got := event.EventTypeRole(event.TypeConsolidation); string(got) != "system" {
		t.Errorf("consolidation 角色应 system, got %v", got)
	}
}

// TestBuildConsolidationEvent_EmptyContentRejected 验证空内容被拒（不产生空巩固）。
func TestBuildConsolidationEvent_EmptyContentRejected(t *testing.T) {
	store := NewInMemoryStore()
	if _, _, err := BuildConsolidationEvent(store, 1, "   ", "manual", "manual", nil); err == nil {
		t.Fatal("空内容应被拒")
	}
}

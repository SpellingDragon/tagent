package evolution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// mockEvidenceSource 返回固定证据（隔离 MetricGuardrail 逻辑）。
type mockEvidenceSource struct {
	ev  Evidence
	err error
}

func (m mockEvidenceSource) Collect(context.Context, string) (Evidence, error) { return m.ev, m.err }

// TestEvidence_RatesAndGuards TestEvidence/TestStoreEvidenceSource/TestMetricGuardrail 系列覆盖后验评估的证据口径、激活时刻窗口
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
func TestEvidence_RatesAndGuards(t *testing.T) {
	ev := Evidence{TurnCount: 10, DenialCount: 4, CriticalCount: 3}
	if ev.DenialRate() != 0.4 {
		t.Errorf("DenialRate=%f 期望 0.4", ev.DenialRate())
	}
	if ev.CriticalRate() != 0.3 {
		t.Errorf("CriticalRate=%f 期望 0.3", ev.CriticalRate())
	}
	if !ev.Sufficient(5) || ev.Sufficient(11) {
		t.Error("Sufficient 阈值判定错")
	}
	if (Evidence{}).DenialRate() != 0 || (Evidence{}).CriticalRate() != 0 {
		t.Error("空证据率应 0（除零守卫）")
	}
}

func TestMetricGuardrail_BreachOnHighDenial(t *testing.T) {
	g := NewMetricGuardrail(
		mockEvidenceSource{ev: Evidence{TurnCount: 10, DenialCount: 5}},
		GuardrailConfig{MaxDenialRate: 0.3, MinSamples: 5},
	)
	breach, reason := g.Breach("b1")
	if !breach {
		t.Fatal("denial 率 0.5 > 0.3 应 breach")
	}
	if reason == "" {
		t.Fatal("breach 应带理由")
	}
}

func TestMetricGuardrail_BreachOnHighCritical(t *testing.T) {
	g := NewMetricGuardrail(
		mockEvidenceSource{ev: Evidence{TurnCount: 10, CriticalCount: 5}},
		GuardrailConfig{MaxCriticalRate: 0.2, MinSamples: 5},
	)
	if breach, _ := g.Breach("b1"); !breach {
		t.Fatal("critical 率 0.5 > 0.2 应 breach")
	}
}

func TestMetricGuardrail_ConservativeNoBreach(t *testing.T) {
	gInsufficient := NewMetricGuardrail(
		mockEvidenceSource{ev: Evidence{TurnCount: 2, DenialCount: 2}},
		GuardrailConfig{MinSamples: 5},
	)
	if breach, _ := gInsufficient.Breach("b1"); breach {
		t.Fatal("样本不足应保守不 breach")
	}
	gErr := NewMetricGuardrail(mockEvidenceSource{err: fmt.Errorf("boom")}, GuardrailConfig{})
	if breach, _ := gErr.Breach("b1"); breach {
		t.Fatal("收集失败应保守不 breach")
	}
	gHealthy := NewMetricGuardrail(
		mockEvidenceSource{ev: Evidence{TurnCount: 20, DenialCount: 1}},
		GuardrailConfig{},
	)
	if breach, _ := gHealthy.Breach("b1"); breach {
		t.Fatal("健康表现不应 breach")
	}
	if breach, _ := (&MetricGuardrail{}).Breach("b1"); breach {
		t.Fatal("nil src 应不 breach")
	}
}

func TestStoreEvidenceSource_Collect(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("evo-eval")
	now := time.Now().UnixMilli()
	for i := 0; i < 3; i++ {
		k := memory.NewSnowflakeEventKey(pid, now+int64(i))
		_ = store.StoreEvent(k, memory.FullEvent{
			EventKey: k, PartitionID: pid, EventType: tagentevent.TypeGovernance, Timestamp: now,
			Metadata: map[string]string{"subtype": "denial"},
		})
	}
	for i := 0; i < 5; i++ {
		k := memory.NewSnowflakeEventKey(pid, now+int64(10+i))
		_ = store.StoreEvent(k, memory.FullEvent{
			EventKey: k, PartitionID: pid, EventType: tagentevent.TypeExternalInput, Timestamp: now,
		})
	}
	src := NewStoreEvidenceSource(store, pid, time.Hour)
	ev, err := src.Collect(context.Background(), "b1")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ev.TurnCount != 5 {
		t.Fatalf("新口径应统计 5 turn, got %d", ev.TurnCount)
	}
	if ev.DenialCount != 3 {
		t.Fatalf("应 3 denial, got %d", ev.DenialCount)
	}
	if ev.DenialRate() <= 0 {
		t.Fatal("denial 率应 > 0")
	}
}

func TestStoreEvidenceSource_WindowFiltersOld(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("evo-eval-win")
	now := time.Now().UnixMilli()
	oldK := memory.NewSnowflakeEventKey(pid, now-7200_000)
	_ = store.StoreEvent(oldK, memory.FullEvent{
		EventKey: oldK, PartitionID: pid, EventType: tagentevent.TypeExternalInput, Timestamp: now - 7200_000,
	})
	newK := memory.NewSnowflakeEventKey(pid, now)
	_ = store.StoreEvent(newK, memory.FullEvent{
		EventKey: newK, PartitionID: pid, EventType: tagentevent.TypeExternalInput, Timestamp: now,
	})
	src := NewStoreEvidenceSource(store, pid, 10*time.Minute)
	ev, _ := src.Collect(context.Background(), "b1")
	if ev.TurnCount != 1 {
		t.Fatalf("窗口过滤后应仅 1 事件, got %d", ev.TurnCount)
	}
}

func TestStoreEvidenceSource_NilStore(t *testing.T) {
	src := NewStoreEvidenceSource(nil, 0, time.Minute)
	ev, err := src.Collect(context.Background(), "b1")
	if err != nil || ev.TurnCount != 0 {
		t.Fatalf("nil store 应空证据无错, got %+v err=%v", ev, err)
	}
}

// TestStoreEvidenceSource_ActivationWindowStart 是 W4（§8.3）回归：Collect 以 bundle 激活时刻
// 为证据窗口起点——激活前的事件被 cutoff 排除。否则 CanaryHold=0「激活即评估」时固定回看窗
// （默认 10m）全是旧 bundle 数据，judge 对新激活 bundle 无判别力，"劣化即回滚"形同虚设。
func TestStoreEvidenceSource_ActivationWindowStart(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := 1
	src := NewStoreEvidenceSource(store, pid, 10*time.Minute)
	actLog := NewActivationLog()
	src.SetActivationLog(actLog)

	now := time.Now().UnixMilli()
	bundleID := "bundle-w4"
	actLog.Record(bundleID, now)

	oldKey := memory.NewSnowflakeEventKey(pid, now-60000)
	if err := store.StoreEvent(oldKey, memory.FullEvent{
		EventKey: oldKey, PartitionID: pid, EventType: tagentevent.TypeExternalInput, Timestamp: now - 60000,
	}); err != nil {
		t.Fatalf("store old event: %v", err)
	}

	ev, err := src.Collect(context.Background(), bundleID)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ev.TurnCount != 0 {
		t.Fatalf("W4: 激活前事件不应计入窗口(cutoff=激活时刻), got TurnCount=%d", ev.TurnCount)
	}
	if ev.WindowMs > 5000 {
		t.Fatalf("W4: 窗口应为激活时至今(≈0), got WindowMs=%d", ev.WindowMs)
	}

	ev2, err := src.Collect(context.Background(), "bundle-unknown")
	if err != nil {
		t.Fatalf("Collect unknown: %v", err)
	}
	if ev2.TurnCount != 1 {
		t.Fatalf("W4: 无激活记录应回退固定窗(含 60s 前事件), got TurnCount=%d", ev2.TurnCount)
	}
}

// TestEvidence_BundleJoin is the D1-B regression:
// Evidence collection joins events to the target bundle via the bundle_id
// Metadata stamp first; untagged events fall back to the time-window
// attribution. Events stamped for a DIFFERENT bundle must not leak into this
// bundle's evidence.
func TestEvidence_BundleJoin(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := 1
	now := time.Now().UnixMilli()
	put := func(seq int64, bundleID, subtype string) {
		key := memory.NewSnowflakeEventKey(pid, now+seq)
		md := map[string]string{tagentevent.MetaKeySubtype: subtype}
		if bundleID != "" {
			md[tagentevent.MetaKeyBundleID] = bundleID
		}
		if err := store.StoreEvent(key, memory.FullEvent{
			EventKey: key, PartitionID: pid, EventType: tagentevent.TypeGovernance,
			Timestamp: now + seq, Metadata: md,
		}); err != nil {
			t.Fatal(err)
		}
	}
	put(1, "b1", tagentevent.SubtypeDenial)
	put(2, "b2", tagentevent.SubtypeDenial)
	put(3, "", tagentevent.SubtypeDenial)

	src := NewStoreEvidenceSource(store, pid, time.Hour)

	ev1, err := src.Collect(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	if ev1.DenialCount != 2 {
		t.Fatalf("b1 DenialCount = %d, want 2 (exact join + window fallback)", ev1.DenialCount)
	}

	ev2, err := src.Collect(context.Background(), "b2")
	if err != nil {
		t.Fatal(err)
	}
	if ev2.DenialCount != 2 {
		t.Fatalf("b2 DenialCount = %d, want 2", ev2.DenialCount)
	}
}

// TestGuardrail_NegativeFeedbackRollback (2.5, design-report-closeout): the
// negative-feedback rate criterion must breach the guardrail when the share
// of negative feedback attributed to the canary bundle exceeds the
// threshold. Fail-before: no such criterion existed.
func TestGuardrail_NegativeFeedbackRollback(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := 1
	now := time.Now().UnixMilli()
	put := func(seq int64, etype, subtype, content string) {
		key := memory.NewSnowflakeEventKey(pid, now+seq)
		md := map[string]string{}
		if subtype != "" {
			md[tagentevent.MetaKeySubtype] = subtype
		}
		_ = store.StoreEvent(key, memory.FullEvent{
			EventKey: key, PartitionID: pid, EventType: etype,
			Timestamp: now + seq, Content: content, Metadata: md,
		})
	}
	for i := 0; i < 5; i++ {
		put(int64(i), tagentevent.TypeExternalInput, "", "turn")
	}
	for i := 5; i < 8; i++ {
		put(int64(i), tagentevent.TypeFeedback, "task_settle", `{"verdict":"negative","source":"task_settle"}`)
	}
	src := NewStoreEvidenceSource(store, pid, time.Hour)
	g := NewMetricGuardrail(src, GuardrailConfig{MinSamples: 5, MaxNegFbRate: 0.3})
	breach, reason := g.Breach("b1")
	if !breach || !strings.Contains(reason, "负反馈率") {
		t.Fatalf("breach=%v reason=%q", breach, reason)
	}
}

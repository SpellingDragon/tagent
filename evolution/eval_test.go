package evolution

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// mockEvidenceSource 返回固定证据（隔离 MetricGuardrail 逻辑）。
type mockEvidenceSource struct {
	ev  Evidence
	err error
}

func (m mockEvidenceSource) Collect(context.Context, string) (Evidence, error) { return m.ev, m.err }

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
	// 除零守卫：空证据率应 0（非 NaN/panic）。
	if (Evidence{}).DenialRate() != 0 || (Evidence{}).CriticalRate() != 0 {
		t.Error("空证据率应 0（除零守卫）")
	}
}

func TestMetricGuardrail_BreachOnHighDenial(t *testing.T) {
	g := NewMetricGuardrail(
		mockEvidenceSource{ev: Evidence{TurnCount: 10, DenialCount: 5}}, // 0.5 > 0.3
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
		mockEvidenceSource{ev: Evidence{TurnCount: 10, CriticalCount: 5}}, // 0.5 > 0.2
		GuardrailConfig{MaxCriticalRate: 0.2, MinSamples: 5},
	)
	if breach, _ := g.Breach("b1"); !breach {
		t.Fatal("critical 率 0.5 > 0.2 应 breach")
	}
}

func TestMetricGuardrail_ConservativeNoBreach(t *testing.T) {
	// 样本不足 → 保守不 breach（防抖动错杀）。
	gInsufficient := NewMetricGuardrail(
		mockEvidenceSource{ev: Evidence{TurnCount: 2, DenialCount: 2}},
		GuardrailConfig{MinSamples: 5},
	)
	if breach, _ := gInsufficient.Breach("b1"); breach {
		t.Fatal("样本不足应保守不 breach")
	}
	// 收集失败 → 保守不 breach。
	gErr := NewMetricGuardrail(mockEvidenceSource{err: fmt.Errorf("boom")}, GuardrailConfig{})
	if breach, _ := gErr.Breach("b1"); breach {
		t.Fatal("收集失败应保守不 breach")
	}
	// 健康表现 → 不 breach。
	gHealthy := NewMetricGuardrail(
		mockEvidenceSource{ev: Evidence{TurnCount: 20, DenialCount: 1}},
		GuardrailConfig{},
	)
	if breach, _ := gHealthy.Breach("b1"); breach {
		t.Fatal("健康表现不应 breach")
	}
	// nil src → 不 breach。
	if breach, _ := (&MetricGuardrail{}).Breach("b1"); breach {
		t.Fatal("nil src 应不 breach")
	}
}

func TestStoreEvidenceSource_Collect(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("evo-eval")
	now := time.Now().UnixMilli()
	// 3 个 governance denial 事件。
	for i := 0; i < 3; i++ {
		k := memory.NewSnowflakeEventKey(pid, now+int64(i))
		_ = store.StoreEvent(k, memory.FullEvent{
			EventKey: k, PartitionID: pid, EventType: event.TypeGovernance, Timestamp: now,
			Metadata: map[string]string{"subtype": "denial"},
		})
	}
	// 5 个普通事件。
	for i := 0; i < 5; i++ {
		k := memory.NewSnowflakeEventKey(pid, now+int64(10+i))
		_ = store.StoreEvent(k, memory.FullEvent{
			EventKey: k, PartitionID: pid, EventType: event.TypeExternalInput, Timestamp: now,
		})
	}
	src := NewStoreEvidenceSource(store, pid, time.Hour)
	ev, err := src.Collect(context.Background(), "b1")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if ev.TurnCount != 8 {
		t.Fatalf("应统计 8 事件, got %d", ev.TurnCount)
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
	// 窗口外旧事件（2 小时前）。
	oldK := memory.NewSnowflakeEventKey(pid, now-7200_000)
	_ = store.StoreEvent(oldK, memory.FullEvent{
		EventKey: oldK, PartitionID: pid, EventType: event.TypeExternalInput, Timestamp: now - 7200_000,
	})
	// 窗口内新事件。
	newK := memory.NewSnowflakeEventKey(pid, now)
	_ = store.StoreEvent(newK, memory.FullEvent{
		EventKey: newK, PartitionID: pid, EventType: event.TypeExternalInput, Timestamp: now,
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

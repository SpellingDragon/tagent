// 本文件负责依赖退化阶梯的可证伪性：进入降级需连续失败达阈值、degraded 下失败只加倍退避、
// recovering 期间任一次失败立即退回——恢复不能"看着像好了"就放回去。
// 契约: docs/wiki/reliability/durable-delivery.md#degradation-ladder
package reliability

import (
	"os"
	"sync"
	"testing"
	"time"
)

func newTestManager(onChange func(Dependency, DepState, DepState)) *DegradationManager {
	d := NewDegradationManager(onChange)
	d.now = func() time.Time { return time.Unix(1750000000, 0) }
	d.Configure(DepMemory, DepConfig{FailThreshold: 3, RecoverSuccesses: 2})
	return d
}

func TestDegradation_NormalToDegradedAfterThreshold(t *testing.T) {
	d := newTestManager(nil)
	if d.State(DepMemory) != StateNormal {
		t.Fatal("初始应 normal")
	}
	d.ReportFailure(DepMemory, nil)
	d.ReportFailure(DepMemory, nil)
	if d.IsDegraded(DepMemory) {
		t.Fatal("未达阈值(3)不应 degraded")
	}
	d.ReportFailure(DepMemory, nil)
	if d.State(DepMemory) != StateDegraded {
		t.Fatalf("达阈值应 degraded, got %s", d.State(DepMemory))
	}
}

func TestDegradation_RecoverToNormal(t *testing.T) {
	d := newTestManager(nil)
	for i := 0; i < 3; i++ {
		d.ReportFailure(DepMemory, nil)
	}
	if d.State(DepMemory) != StateDegraded {
		t.Fatal("前置：应 degraded")
	}
	d.ReportSuccess(DepMemory)
	if d.State(DepMemory) != StateRecovering {
		t.Fatalf("首次成功应 recovering, got %s", d.State(DepMemory))
	}
	d.ReportSuccess(DepMemory)
	if d.State(DepMemory) != StateNormal {
		t.Fatalf("连续成功达阈值应 normal, got %s", d.State(DepMemory))
	}
}

func TestDegradation_RecoveringFailureFallsBack(t *testing.T) {
	d := newTestManager(nil)
	for i := 0; i < 3; i++ {
		d.ReportFailure(DepMemory, nil)
	}
	d.ReportSuccess(DepMemory)
	if d.State(DepMemory) != StateRecovering {
		t.Fatal("前置：应 recovering")
	}
	d.ReportFailure(DepMemory, nil)
	if d.State(DepMemory) != StateDegraded {
		t.Fatalf("恢复中失败应退回 degraded, got %s", d.State(DepMemory))
	}
}

func TestDegradation_OnChangeFiresOnTransitions(t *testing.T) {
	var transitions []string
	d := newTestManager(func(dep Dependency, from, to DepState) {
		transitions = append(transitions, string(dep)+":"+string(from)+"->"+string(to))
	})
	d.ReportFailure(DepMemory, nil)
	d.ReportFailure(DepMemory, nil)
	if len(transitions) != 0 {
		t.Fatalf("未达阈值不应触发 onChange, got %v", transitions)
	}
	d.ReportFailure(DepMemory, nil)
	if len(transitions) != 1 || transitions[0] != "memory:normal->degraded" {
		t.Fatalf("应触发 normal->degraded, got %v", transitions)
	}
	d.ReportSuccess(DepMemory)
	d.ReportSuccess(DepMemory)
	if len(transitions) != 3 {
		t.Fatalf("应有 3 次迁移, got %v", transitions)
	}
}

func TestDegradation_UnmonitoredIsNormal(t *testing.T) {
	d := newTestManager(nil)
	if d.State(DepDisk) != StateNormal || d.IsDegraded(DepDisk) {
		t.Fatal("未监控依赖应视为 normal")
	}
}

func TestDegradation_NormalSuccessResetsFailCount(t *testing.T) {
	d := newTestManager(nil)
	d.ReportFailure(DepMemory, nil)
	d.ReportFailure(DepMemory, nil)
	d.ReportSuccess(DepMemory)
	d.ReportFailure(DepMemory, nil)
	d.ReportFailure(DepMemory, nil)
	if d.IsDegraded(DepMemory) {
		t.Fatal("成功应重置失败计数，2 次失败不应 degraded")
	}
	d.ReportFailure(DepMemory, nil)
	if d.State(DepMemory) != StateDegraded {
		t.Fatal("重置后重新累计达阈值应 degraded")
	}
}

func TestDegradation_Snapshot(t *testing.T) {
	d := newTestManager(nil)
	for i := 0; i < 3; i++ {
		d.ReportFailure(DepMemory, nil)
	}
	snap := d.Snapshot()
	if snap[DepMemory] != StateDegraded {
		t.Fatalf("快照应含 degraded, got %v", snap)
	}
}

// TestFaultInjection_MultiDependencyIndependentDegrade 钉住 注入多依赖故障，验证各自独立退化 + 独立恢复（一个依赖退化不污染其他，恢复也不联动）。
func TestFaultInjection_MultiDependencyIndependentDegrade(t *testing.T) {
	d := NewDegradationManager(nil)
	deps := []Dependency{DepMemory, DepRustViking, DepMCP, DepModel, DepDisk}
	for _, dep := range deps {
		d.Configure(dep, DepConfig{FailThreshold: 2, RecoverSuccesses: 1})
	}
	d.ReportFailure(DepMemory, nil)
	d.ReportFailure(DepMemory, nil)
	d.ReportFailure(DepDisk, nil)
	d.ReportFailure(DepDisk, nil)

	if !d.IsDegraded(DepMemory) || !d.IsDegraded(DepDisk) {
		t.Fatal("memory/disk 达阈值应退化")
	}
	for _, healthy := range []Dependency{DepRustViking, DepMCP, DepModel} {
		if d.IsDegraded(healthy) {
			t.Fatalf("%s 未注入故障不应退化（依赖独立性）", healthy)
		}
	}
	d.ReportSuccess(DepMemory)
	if d.IsDegraded(DepMemory) {
		t.Fatal("memory 单次成功（RecoverSuccesses=1）应恢复")
	}
	if !d.IsDegraded(DepDisk) {
		t.Fatal("disk 未恢复应仍退化（独立恢复）")
	}
}

// TestFaultInjection_ClockSkewNoPanic 钉住 注入时钟回拨（ShouldProbe 依赖 now），验证不 panic、 不因回拨累积错误状态。
func TestFaultInjection_ClockSkewNoPanic(t *testing.T) {
	d := NewDegradationManager(nil)
	now := time.Unix(1750000000, 0)
	d.now = func() time.Time { return now }
	d.Configure(DepMCP, DepConfig{FailThreshold: 1, ProbeBackoff: time.Minute})

	d.ReportFailure(DepMCP, nil)
	if d.State(DepMCP) != StateDegraded {
		t.Fatal("应退化")
	}
	now = now.Add(-time.Hour)
	_ = d.ShouldProbe(DepMCP)
	if d.State(DepMCP) != StateDegraded {
		t.Fatal("时钟回拨不应改变退化状态")
	}
	now = now.Add(2 * time.Hour)
	if !d.ShouldProbe(DepMCP) {
		t.Fatal("退避窗口过后应允许探测")
	}
}

// TestFaultInjection_DegradationConcurrentNoRace 钉住 并发注入故障/成功/查询，验证无数据竞争 （-race）、无 panic（DegradationManager 的 mu 保护 + onChange 锁外调用）。
func TestFaultInjection_DegradationConcurrentNoRace(t *testing.T) {
	var mu sync.Mutex
	transitions := 0
	d := NewDegradationManager(func(Dependency, DepState, DepState) {
		mu.Lock()
		transitions++
		mu.Unlock()
	})
	d.Configure(DepModel, DepConfig{FailThreshold: 2, RecoverSuccesses: 1})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); d.ReportFailure(DepModel, nil) }()
		go func() { defer wg.Done(); d.ReportSuccess(DepModel) }()
		go func() { defer wg.Done(); _ = d.State(DepModel); _ = d.Snapshot() }()
	}
	wg.Wait()
	if s := d.State(DepModel); s != StateNormal && s != StateDegraded && s != StateRecovering {
		t.Fatalf("状态应是合法枚举, got %s", s)
	}
}

// TestFaultInjection_AnchorCorruptNoPanic 钉住 注入坏锚点文件，验证 Load 返回 error（调用方 SetAnchorStore 保守用当前值），不 panic、不阻断。
func TestFaultInjection_AnchorCorruptNoPanic(t *testing.T) {
	s, _ := NewAnchorStore(t.TempDir() + "/anchors.json")
	_ = s.Save(MeditationAnchors{LastTurnEnd: 1})
	if err := os.WriteFile(s.Path(), []byte("{corrupt json"), 0o644); err != nil {
		t.Skipf("无法注入坏文件: %v", err)
	}
	if _, err := s.Load(); err == nil {
		t.Fatal("坏锚点文件 Load 应 error（调用方保守处理）")
	}
	if err := s.Save(MeditationAnchors{LastTurnEnd: 2}); err != nil {
		t.Fatalf("Save 应能覆盖坏文件: %v", err)
	}
	if a, err := s.Load(); err != nil || a.LastTurnEnd != 2 {
		t.Fatalf("自愈后应可读, got %+v err=%v", a, err)
	}
}

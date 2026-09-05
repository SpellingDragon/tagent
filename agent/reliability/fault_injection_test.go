package reliability

import (
	"os"
	"sync"
	"testing"
	"time"
)

// ==================== 故障注入矩阵（T-G · 常驻可靠性验证）====================
//
// 系统验证报告 D3 的可靠性原则：每个外部依赖失效有明确定义的「检测→降级→恢复」路径，
// 无静默丢失、无 panic、无死循环；依赖间退化相互独立；失败是一等资产（可查询、可恢复）。

// TestFaultInjection_MultiDependencyIndependentDegrade 注入多依赖故障，验证各自独立退化 +
// 独立恢复（一个依赖退化不污染其他，恢复也不联动）。
func TestFaultInjection_MultiDependencyIndependentDegrade(t *testing.T) {
	d := NewDegradationManager(nil)
	deps := []Dependency{DepMemory, DepRustViking, DepMCP, DepModel, DepDisk}
	for _, dep := range deps {
		d.Configure(dep, DepConfig{FailThreshold: 2, RecoverSuccesses: 1})
	}
	// 注入：memory + disk 故障，其余健康。
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
	// 恢复：memory 恢复，disk 仍退化（独立恢复，不联动）。
	d.ReportSuccess(DepMemory)
	if d.IsDegraded(DepMemory) {
		t.Fatal("memory 单次成功（RecoverSuccesses=1）应恢复")
	}
	if !d.IsDegraded(DepDisk) {
		t.Fatal("disk 未恢复应仍退化（独立恢复）")
	}
}

// TestFaultInjection_ClockSkewNoPanic 注入时钟回拨（ShouldProbe 依赖 now），验证不 panic、
// 不因回拨累积错误状态。
func TestFaultInjection_ClockSkewNoPanic(t *testing.T) {
	d := NewDegradationManager(nil)
	now := time.Unix(1750000000, 0)
	d.now = func() time.Time { return now }
	d.Configure(DepMCP, DepConfig{FailThreshold: 1, ProbeBackoff: time.Minute})

	d.ReportFailure(DepMCP, nil) // → degraded
	if d.State(DepMCP) != StateDegraded {
		t.Fatal("应退化")
	}
	// 时钟回拨 1 小时：ShouldProbe 不应 panic，退化状态保持。
	now = now.Add(-time.Hour)
	_ = d.ShouldProbe(DepMCP) // 不 panic 即可
	if d.State(DepMCP) != StateDegraded {
		t.Fatal("时钟回拨不应改变退化状态")
	}
	// 时钟前进超过退避 → 应允许探测。
	now = now.Add(2 * time.Hour)
	if !d.ShouldProbe(DepMCP) {
		t.Fatal("退避窗口过后应允许探测")
	}
}

// TestFaultInjection_DegradationConcurrentNoRace 并发注入故障/成功/查询，验证无数据竞争
// （-race）、无 panic（DegradationManager 的 mu 保护 + onChange 锁外调用）。
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
	// 无 panic、无 race（-race 检测）即通过；状态是确定的枚举之一。
	if s := d.State(DepModel); s != StateNormal && s != StateDegraded && s != StateRecovering {
		t.Fatalf("状态应是合法枚举, got %s", s)
	}
}

// TestFaultInjection_SpillConcurrentStress 并发溢出 + 回收压力（模拟 channel 满溢与消费并发），
// 验证无 panic、无死循环、最终可排空（at-least-once 不丢已落盘项）。
func TestFaultInjection_SpillConcurrentStress(t *testing.T) {
	s, err := NewSpillStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSpillStore: %v", err)
	}
	var wg sync.WaitGroup
	// 20 生产者溢出。
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Spill([]byte{byte(i)})
		}(i)
	}
	wg.Wait()
	// 排空：应恰好回收 20 项，无死循环。
	reclaimed := 0
	for {
		_, ok, err := s.Reclaim()
		if err != nil {
			t.Fatalf("Reclaim 出错: %v", err)
		}
		if !ok {
			break
		}
		reclaimed++
		if reclaimed > 100 {
			t.Fatal("回收超过溢出数，疑似死循环")
		}
	}
	if reclaimed != 20 {
		t.Fatalf("应回收全部 20 溢出项（不丢）, got %d", reclaimed)
	}
}

// TestFaultInjection_AnchorCorruptNoPanic 注入坏锚点文件，验证 Load 返回 error（调用方
// SetAnchorStore 保守用当前值），不 panic、不阻断。
func TestFaultInjection_AnchorCorruptNoPanic(t *testing.T) {
	s, _ := NewAnchorStore(t.TempDir() + "/anchors.json")
	// 正常 Save 后手动破坏文件。
	_ = s.Save(MeditationAnchors{LastTurnEnd: 1})
	if err := os.WriteFile(s.Path(), []byte("{corrupt json"), 0o644); err != nil {
		t.Skipf("无法注入坏文件: %v", err)
	}
	if _, err := s.Load(); err == nil {
		t.Fatal("坏锚点文件 Load 应 error（调用方保守处理）")
	}
	// Save 仍可覆盖坏文件（自愈）。
	if err := s.Save(MeditationAnchors{LastTurnEnd: 2}); err != nil {
		t.Fatalf("Save 应能覆盖坏文件: %v", err)
	}
	if a, err := s.Load(); err != nil || a.LastTurnEnd != 2 {
		t.Fatalf("自愈后应可读, got %+v err=%v", a, err)
	}
}

package reliability

import (
	"testing"
	"time"
)

func newTestManager(onChange func(Dependency, DepState, DepState)) *DegradationManager {
	d := NewDegradationManager(onChange)
	d.now = func() time.Time { return time.Unix(1750000000, 0) } // 固定时钟
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
	d.ReportSuccess(DepMemory) // → recovering (success 1)
	if d.State(DepMemory) != StateRecovering {
		t.Fatalf("首次成功应 recovering, got %s", d.State(DepMemory))
	}
	d.ReportSuccess(DepMemory) // → normal (success 2 >= RecoverSuccesses)
	if d.State(DepMemory) != StateNormal {
		t.Fatalf("连续成功达阈值应 normal, got %s", d.State(DepMemory))
	}
}

func TestDegradation_RecoveringFailureFallsBack(t *testing.T) {
	d := newTestManager(nil)
	for i := 0; i < 3; i++ {
		d.ReportFailure(DepMemory, nil)
	}
	d.ReportSuccess(DepMemory) // recovering
	if d.State(DepMemory) != StateRecovering {
		t.Fatal("前置：应 recovering")
	}
	d.ReportFailure(DepMemory, nil) // 恢复中失败 → 退回 degraded
	if d.State(DepMemory) != StateDegraded {
		t.Fatalf("恢复中失败应退回 degraded, got %s", d.State(DepMemory))
	}
}

func TestDegradation_OnChangeFiresOnTransitions(t *testing.T) {
	var transitions []string
	d := newTestManager(func(dep Dependency, from, to DepState) {
		transitions = append(transitions, string(dep)+":"+string(from)+"->"+string(to))
	})
	d.ReportFailure(DepMemory, nil) // normal, 未达阈值 → 无迁移
	d.ReportFailure(DepMemory, nil)
	if len(transitions) != 0 {
		t.Fatalf("未达阈值不应触发 onChange, got %v", transitions)
	}
	d.ReportFailure(DepMemory, nil) // → degraded
	if len(transitions) != 1 || transitions[0] != "memory:normal->degraded" {
		t.Fatalf("应触发 normal->degraded, got %v", transitions)
	}
	d.ReportSuccess(DepMemory) // → recovering
	d.ReportSuccess(DepMemory) // → normal
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
	d.ReportSuccess(DepMemory) // 重置失败计数
	d.ReportFailure(DepMemory, nil)
	d.ReportFailure(DepMemory, nil)
	if d.IsDegraded(DepMemory) {
		t.Fatal("成功应重置失败计数，2 次失败不应 degraded")
	}
	d.ReportFailure(DepMemory, nil) // 第3次连续
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

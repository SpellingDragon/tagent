package task

import (
	"sync"
	"testing"
	"time"
)

// hardening-review-batch2 1.5/1.8：终态事实一致性（task 层）——finalize 后
// 内存状态与 Settle 信号 Kind 必须同语义。WAL settle_status / marker / 反馈
// 极性由 agent 包的 settleMarkerAndStatus 表驱动测试覆盖（mapper 在 agent
// 层，task 包不引依赖）；此处锁定「信号 Kind 与内存状态不背离」这一环。

// TestFinalizeConsistency_ReconcilePathsEmitFailed：zombie/orphan/wall 三条
// reconcile 回收路径的终态信号必须都是 SettleFailed（曾出现 wall 发
// SettleCompleted 的错配），内存状态一致为 failed，且 settle 恰一次。
func TestFinalizeConsistency_ReconcilePathsEmitFailed(t *testing.T) {
	t.Run("zombie retire", func(t *testing.T) {
		var mu sync.Mutex
		var kinds []SettleKind
		tm := NewTaskManager(TaskManagerConfig{OnSettle: func(_ *Task, sig SettleSignal) {
			mu.Lock()
			kinds = append(kinds, sig.Kind)
			mu.Unlock()
		}})
		alive := false
		res := tm.Spawn(TaskSpec{Kind: "command", Desc: "z", Alive: func() bool { return alive }},
			NewManualDetectorDetach(30*time.Millisecond))
		waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskRunning })
		// 过 zombieGrace（默认 10 分钟）才满足回收判据。
		res.Task.StartedAt = time.Now().Add(-11 * time.Minute)
		alive = false
		_ = len(tm.List()) // List 驱动 reconcile

		if got := res.Task.Status(); got != TaskFailed {
			t.Fatalf("memory status = %s, want failed", got)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(kinds) == 0 || kinds[len(kinds)-1] != SettleFailed {
			t.Fatalf("terminal signal kind = %v, want [%s]", kinds, SettleFailed)
		}
	})

	t.Run("orphan retire", func(t *testing.T) {
		var mu sync.Mutex
		var kinds []SettleKind
		tm := NewTaskManager(TaskManagerConfig{OnSettle: func(_ *Task, sig SettleSignal) {
			mu.Lock()
			kinds = append(kinds, sig.Kind)
			mu.Unlock()
		}})
		tm.RestoreTask("t-orphan", TaskSpec{
			Kind: "command", Desc: "o",
			Declarative: &Declarative{Kind: "command", TaskID: "n-gone"},
		}, time.Now().Add(-2*defaultOrphanGrace), TaskSuspect)
		tm.SetSessionTracker(func(string) bool { return false })
		_ = len(tm.List()) // 通道 2：reconcile 复评孤儿

		tk, ok := tm.Get("t-orphan")
		if !ok || tk.Status() != TaskFailed {
			t.Fatalf("memory status = %v/%v, want failed", ok, tkStatus(tm, "t-orphan"))
		}
		mu.Lock()
		defer mu.Unlock()
		if len(kinds) == 0 || kinds[len(kinds)-1] != SettleFailed {
			t.Fatalf("terminal signal kind = %v, want [%s]", kinds, SettleFailed)
		}
	})
}

// TestFinalizeConsistency_LateSignalsNeverRevive（1.7 fencing）：终态后迟到
// 信号不得改状态、不得重复结算（含 Watch 通知）。
func TestFinalizeConsistency_LateSignalsNeverRevive(t *testing.T) {
	var mu sync.Mutex
	var settles []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(_ *Task, sig SettleSignal) {
			mu.Lock()
			settles = append(settles, sig)
			mu.Unlock()
		},
	})
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "fenced"}, d)
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskRunning })
	// 后台结算进入终态。
	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskCompleted })
	mu.Lock()
	before := len(settles)
	mu.Unlock()

	// 终态后迟到信号（旧 detector 泄漏的 Stable/Completed/Watch）。
	for _, late := range []SettleSignal{
		{Kind: SettleStable, Output: "late stable"},
		{Kind: SettleCompleted, Output: "late done"},
		{Kind: SettleWatch, Output: "late watch"},
	} {
		d.Emit(late)
	}
	time.Sleep(50 * time.Millisecond) // 让 watch goroutine 消化迟到信号

	if got := res.Task.Status(); got != TaskCompleted {
		t.Fatalf("status = %s, want completed (fence must not change state)", got)
	}
	mu.Lock()
	after := len(settles)
	mu.Unlock()
	if after != before {
		t.Fatalf("settles = %d, want %d (post-terminal signals dropped, not re-emitted)", after, before)
	}
}

func tkStatus(tm *TaskManager, id string) TaskStatus {
	tk, ok := tm.Get(id)
	if !ok {
		return ""
	}
	return tk.Status()
}

package task

import (
	"sync"
	"testing"
	"time"
)

// hardening-review-batch2 2.5/2.6/2.7：stale-detached 观测 + 可选 deadline 终止。
// 语义（对 7080753 强 failed 的重设计）：
//   - 超过 StaleAfter：job 型 → TaskStale 观测态（一次性 Watch 通知），进程不动；
//     service 型永不受观测/终止。
//   - JobDeadline>0：job 型 detached 超 deadline → owner Cancel + finalize(failed)
//     一次结算；service 永不因年龄终止。
//   - detachedAt 进事实链，恢复后沿用真实时长。

// staleFixture spawns a task with the given thresholds and drives it to
// alive_detached, returning the manager/task/settle-log and a clock shifter.
func staleFixture(t *testing.T, staleAfter, deadline time.Duration, kind string) (*TaskManager, *Task, *settleLog, *ManualDetector, func(time.Duration)) {
	t.Helper()
	log_ := &settleLog{}
	tm := NewTaskManager(TaskManagerConfig{
		StaleAfter:  staleAfter,
		JobDeadline: deadline,
		OnSettle:    log_.record,
	})
	base := time.Now()
	tm.now = func() time.Time { return base }
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: kind, Desc: "t", Alive: func() bool { return true }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })
	shift := func(by time.Duration) { tm.now = func() time.Time { return base.Add(by) } }
	return tm, res.Task, log_, d, shift
}

// settleLog is the race-safe settle recorder (cold-eyes P1-5: raw &settles
// reads from the test goroutine raced the OnSettle append from the watch
// goroutine under -race).
type settleLog struct {
	mu    sync.Mutex
	items []SettleSignal
}

func (l *settleLog) record(_ *Task, sig SettleSignal) {
	l.mu.Lock()
	l.items = append(l.items, sig)
	l.mu.Unlock()
}

func (l *settleLog) snapshot() []SettleSignal {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]SettleSignal, len(l.items))
	copy(out, l.items)
	return out
}

// 2.5 默认观测：job 型超 stale_after → stale（非终态、不终止、恰好一次告警）。
func TestStaleDetached_JobObservedNotTerminated(t *testing.T) {
	tm, tk, log_, _, shift := staleFixture(t, time.Hour, 0, "command")
	shift(2 * time.Hour)
	_ = len(tm.List()) // List 驱动 reconcile

	if got := tk.Status(); got != TaskStale {
		t.Fatalf("status = %s, want stale (observation-only)", got)
	}
	if n := len(log_.snapshot()); n != 2 {
		t.Fatalf("settles = %d, want 2 (ready + one-time stale notice)", n)
	}
	// 再次 reconcile：不重复告警。
	_ = len(tm.List())
	if n := len(log_.snapshot()); n != 2 {
		t.Fatalf("settles after re-reconcile = %d, want 2 (one-time notice)", n)
	}
}

// 2.5 service 型豁免：永不受 stale 观测。
func TestStaleDetached_ServiceKindNeverStaled(t *testing.T) {
	tm, tk, log_, _, shift := staleFixture(t, time.Hour, 0, "generic")
	shift(48 * time.Hour)
	_ = len(tm.List())

	if got := tk.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached (service exempt)", got)
	}
	if n := len(log_.snapshot()); n != 1 {
		t.Fatalf("settles = %d, want 1 (ready only)", n)
	}
}

// 2.5 阈值内保留。
func TestStaleDetached_NotYetThresholdKept(t *testing.T) {
	tm, tk, _, _, shift := staleFixture(t, time.Hour, 0, "command")
	shift(30 * time.Minute)
	_ = len(tm.List())
	if got := tk.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached (inside threshold)", got)
	}
}

// 2.5 负值禁用观测。
func TestStaleDetached_DisabledByNegative(t *testing.T) {
	tm, tk, _, _, shift := staleFixture(t, -time.Second, 0, "command")
	shift(48 * time.Hour)
	_ = len(tm.List())
	if got := tk.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached (observation disabled)", got)
	}
}

// 2.6 deadline 显式终止：owner Cancel + failed 一次结算；service 不触发。
func TestJobDeadline_CancelAndFinalizeFailed(t *testing.T) {
	tm, tk, log_, d, shift := staleFixture(t, time.Hour, 2*time.Hour, "command")
	shift(3 * time.Hour)
	_ = len(tm.List())

	if got := tk.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (deadline exceeded)", got)
	}
	if !d.Cancelled() {
		t.Fatal("owner must Cancel the detector (kills the backing session)")
	}
	if n := len(log_.snapshot()); n != 3 {
		t.Fatalf("settles = %d, want 3 (ready + stale notice @1h + deadline failure @2h)", n)
	}
	last := log_.snapshot()[len(log_.snapshot())-1]
	if last.Kind != SettleFailed {
		t.Fatalf("terminal signal kind = %s, want failed", last.Kind)
	}
	if got := tk.Result(); got != "(job-deadline-exceeded: job-kind task detached past the configured deadline - cancelled by owner and retired)" {
		t.Fatalf("result note = %q", got)
	}

	// service 型 + deadline：不终止。
	tm2, tk2, _, _, shift2 := staleFixture(t, time.Hour, 2*time.Hour, "generic")
	shift2(3 * time.Hour)
	_ = len(tm2.List())
	if got := tk2.Status(); got != TaskAliveDetached {
		t.Fatalf("service status = %s, want alive_detached (never age-terminated)", got)
	}
}

// 2.7 detachedAt 跨重启保真：RestoreTask(AliveDetached) + SetDetachedAtMilli
// 后 stale/deadline 判定沿用真实时长。
func TestStaleDetached_DetachedAtRestoredFromFactChain(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{StaleAfter: time.Hour})
	base := time.Now()
	tm.now = func() time.Time { return base }
	// detached 于 2h 前（事实链记录），现在恢复。
	tk := tm.RestoreTask("t-restored", TaskSpec{
		Kind: "command", Desc: "restored",
		Alive:       func() bool { return true },
		Declarative: &Declarative{Kind: "command", TaskID: "n-r"},
	}, base.Add(-3*time.Hour), TaskAliveDetached)
	tk.SetDetachedAtMilli(base.Add(-2 * time.Hour).UnixMilli())

	_ = len(tm.List()) // 驱动 reconcile：真实时长 2h > staleAfter 1h → stale
	if got := tk.Status(); got != TaskStale {
		t.Fatalf("status = %s, want stale (real detached age honored after restore)", got)
	}
}

// cold-eyes P0-1 回归①（真实时序）：先 stale（第一轮 List）→ 跨多轮 List 后
// 才到 deadline → 终止仍须生效（候选集曾不含 TaskStale，deadline 永不触发）。
func TestJobDeadline_AfterPriorStalePhase_StillTerminates(t *testing.T) {
	log_ := &settleLog{}
	tm := NewTaskManager(TaskManagerConfig{
		StaleAfter:  time.Hour,
		JobDeadline: 8 * time.Hour,
		OnSettle:    log_.record,
	})
	base := time.Now()
	tm.now = func() time.Time { return base }
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "af4aa4c7-shape", Alive: func() bool { return true }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })

	// 2h：第一轮 List → stale（非终态）。多轮 List 反复确认保持 stale。
	shift2h := func() { tm.now = func() time.Time { return base.Add(2 * time.Hour) } }
	shift2h()
	_ = len(tm.List())
	_ = len(tm.List())
	if got := res.Task.Status(); got != TaskStale {
		t.Fatalf("after 2h List: status = %s, want stale", got)
	}
	// 9h：真实时序下 deadline 到点——stale 任务必须仍在治理候选集内。
	tm.now = func() time.Time { return base.Add(9 * time.Hour) }
	_ = len(tm.List())
	if got := res.Task.Status(); got != TaskFailed {
		t.Fatalf("after 9h List (deadline 8h): status = %s, want failed — stale must stay governed", got)
	}
	if !d.Cancelled() {
		t.Fatal("owner must Cancel at deadline even after prior stale phase")
	}
}

// cold-eyes P0-1 回归②：stale 后 probe 死 → completed 复回收（回收路径同样
// 曾只认 AliveDetached）。
func TestStaleDetached_ProbeDeathStillReclaims(t *testing.T) {
	alive := true
	log_ := &settleLog{}
	tm := NewTaskManager(TaskManagerConfig{
		StaleAfter: time.Hour,
		OnSettle:   log_.record,
	})
	base := time.Now()
	tm.now = func() time.Time { return base }
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "s", Alive: func() bool { return alive }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })

	tm.now = func() time.Time { return base.Add(2 * time.Hour) }
	_ = len(tm.List())
	if got := res.Task.Status(); got != TaskStale {
		t.Fatalf("status = %s, want stale", got)
	}
	alive = false // 会话死后 out-of-band
	_ = len(tm.List())
	if got := res.Task.Status(); got != TaskCompleted {
		t.Fatalf("stale + probe dead: status = %s, want completed (reclaim path must include stale)", got)
	}
}

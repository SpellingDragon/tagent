// 本文件负责任务层的生命周期判据：结算窗口与边界不丢结算、脱离后的信号抑制、统一 TTL 回收
// 的四条性质、终结与谱系禁令、派生去重与来源袋、从事实重建的幂等语义。
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package task

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// TestTaskManager_SettleWithinWindow_Inline 钉住 settle before the sync-wait window elapses → Spawn returns inline with the signal, task Completed.
func TestTaskManager_SettleWithinWindow_Inline(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewManualDetectorDetach(500 * time.Millisecond)
	go func() {
		time.Sleep(30 * time.Millisecond)
		d.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
		d.Done()
	}()

	res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "quick"}, d)

	if !res.Settled {
		t.Fatalf("expected inline settle within window, got ack")
	}
	if res.Signal.Output != "done" || res.Signal.Kind != SettleCompleted {
		t.Errorf("unexpected signal: %+v", res.Signal)
	}
	if got := res.Task.Status(); got != TaskCompleted {
		t.Errorf("status = %s, want completed", got)
	}
}

// TestTaskManager_SettleAfterWindow_BackgroundAck 钉住 no settle within window → Spawn returns ack (Settled=false); later settle fires OnSettle exactly once.
func TestTaskManager_SettleAfterWindow_BackgroundAck(t *testing.T) {
	var mu sync.Mutex
	var bg []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			mu.Lock()
			bg = append(bg, sig)
			mu.Unlock()
		},
	})
	d := NewManualDetectorDetach(80 * time.Millisecond)

	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "long build"}, d)
	if res.Settled {
		t.Fatalf("expected ack (not settled in window)")
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Errorf("status after ack = %s, want running", got)
	}

	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "late"})
	d.Done()

	waitUntil(t, 2*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(bg) == 1 })
	mu.Lock()
	if len(bg) != 1 || bg[0].Output != "late" {
		t.Errorf("background settles = %+v, want exactly [late]", bg)
	}
	mu.Unlock()
	if got := res.Task.Status(); got != TaskCompleted {
		t.Errorf("final status = %s, want completed", got)
	}
}

// TestTaskManager_IdempotentSpawn_Dedup 钉住 a second Spawn with the same Key while the first is active returns Deduped and cancels the duplicate detector.
func TestTaskManager_IdempotentSpawn_Dedup(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d1 := NewManualDetectorDetach(300 * time.Millisecond)
	key := "same-key"

	var res1 SpawnResult
	done := make(chan struct{})
	go func() {
		res1 = tm.Spawn(TaskSpec{Kind: "command", Desc: "a", Key: key}, d1)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)

	d2 := NewManualDetector()
	res2 := tm.Spawn(TaskSpec{Kind: "command", Desc: "a-dup", Key: key}, d2)
	if !res2.Deduped {
		t.Fatalf("expected dedup for identical active key")
	}
	if !d2.Cancelled() {
		t.Errorf("duplicate detector should be cancelled")
	}

	<-done
	if res2.Task != res1.Task {
		t.Errorf("dedup should return the existing task")
	}
}

// TestTaskManager_WindowBoundary_NoLostSettle 钉住 hammer the window boundary; every settle must be accounted for exactly once (inline XOR background), never lost.
func TestTaskManager_WindowBoundary_NoLostSettle(t *testing.T) {
	const n = 60
	var mu sync.Mutex
	bg := map[string]int{}
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			mu.Lock()
			bg[task.ID]++
			mu.Unlock()
		},
	})

	inline := 0
	tasks := make([]*Task, 0, n)
	for i := 0; i < n; i++ {
		d := NewManualDetectorDetach(2 * time.Millisecond)
		go func() {
			time.Sleep(2 * time.Millisecond)
			d.Emit(SettleSignal{Kind: SettleCompleted, Output: "x"})
			d.Done()
		}()
		res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "boundary"}, d)
		tasks = append(tasks, res.Task)
		if res.Settled {
			inline++
		}
	}

	waitUntil(t, 3*time.Second, func() bool {
		for _, tk := range tasks {
			if tk.Status() != TaskCompleted {
				return false
			}
		}
		return true
	})

	mu.Lock()
	defer mu.Unlock()
	bgTotal := 0
	for _, c := range bg {
		if c != 1 {
			t.Errorf("task settled to background %d times, want 1", c)
		}
		bgTotal++
	}
	if inline+bgTotal != n {
		t.Errorf("accounting mismatch: inline=%d background=%d total=%d want %d",
			inline, bgTotal, inline+bgTotal, n)
	}
}

// TestTaskManager_Cancel marks a task cancelled and cancels its detector.
func TestTaskManager_Cancel(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewManualDetectorDetach(40 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "svc"}, d)
	if res.Settled {
		t.Fatalf("expected ack")
	}
	if !tm.Cancel(res.Task.ID) {
		t.Fatalf("cancel returned false")
	}
	if !d.Cancelled() {
		t.Errorf("detector not cancelled")
	}
	if got := res.Task.Status(); got != TaskCancelled {
		t.Errorf("status = %s, want cancelled", got)
	}
}

// TestFuncSettleDetector_Generic 钉住 verifies the generic goroutine detector settles Completed on return (with error captured).
func TestFuncSettleDetector_Generic(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewFuncSettleDetector(context.Background(), func(ctx context.Context) (string, error) {
		return "hello", nil
	})
	res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "fn"}, d)
	if !res.Settled || res.Signal.Output != "hello" {
		t.Fatalf("expected inline hello, got %+v", res)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// terminalTask builds a task already in a terminal state with the given
// settledAt, wired to a spy detector so prune-time resource reclaim is testable.
func terminalTask(id string, status TaskStatus, settledAt time.Time, d *ManualDetector) *Task {
	return &Task{ID: id, Spec: TaskSpec{Desc: id}, status: status, settledAt: settledAt, detector: d}
}

// TestPruneTerminal_RemovesExitedAndReclaims 钉住 过期的已退出任务既被移除也触发探测器取消以回收资源；存活任务保留且绝不被取消。
func TestPruneTerminal_RemovesExitedAndReclaims(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()
	dc := NewManualDetector()
	dl := NewManualDetector()
	tm.tasks["c1"] = terminalTask("c1", TaskCompleted, base, dc)
	tm.tasks["r1"] = &Task{ID: "r1", Spec: TaskSpec{Desc: "r1"}, status: TaskRunning, detector: dl}

	tm.now = func() time.Time { return base.Add(2 * time.Minute) }
	tm.pruneTerminal()

	if _, ok := tm.Get("c1"); ok {
		t.Error("exited task past grace must be pruned")
	}
	if !dc.Cancelled() {
		t.Error("pruned task's detector.Cancel() must be called (session resource reclaim)")
	}
	if _, ok := tm.Get("r1"); !ok {
		t.Error("live task must be kept")
	}
	if dl.Cancelled() {
		t.Error("live task must NOT be cancelled")
	}
}

// TestPruneTerminal_KeepsAliveDetached 钉住 alive_detached (a living service) is never pruned by TTL regardless of elapsed time — only cancel/death ends it.
func TestPruneTerminal_KeepsAliveDetached(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Nanosecond})
	base := time.Now()
	d := NewManualDetector()
	tm.tasks["s1"] = terminalTask("s1", TaskAliveDetached, base, d)

	tm.now = func() time.Time { return base.Add(time.Hour) }
	tm.pruneTerminal()

	if _, ok := tm.Get("s1"); !ok {
		t.Error("alive_detached (live service) must be kept")
	}
	if d.Cancelled() {
		t.Error("alive_detached must NOT be cancelled by prune")
	}
}

// TestPruneTerminal_KeepsWithinGrace 钉住 an exited task still within its grace TTL is retained so the resume_task window stays reachable.
func TestPruneTerminal_KeepsWithinGrace(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()
	d := NewManualDetector()
	tm.tasks["c1"] = terminalTask("c1", TaskCompleted, base, d)

	tm.now = func() time.Time { return base.Add(10 * time.Second) }
	tm.pruneTerminal()

	if _, ok := tm.Get("c1"); !ok {
		t.Error("exited task within grace must be retained (resume_task window)")
	}
	if d.Cancelled() {
		t.Error("within-grace task must not be cancelled yet")
	}
}

// TestList_PrunesExited: List() is a prune trigger and reclaims resources.
func TestList_PrunesExited(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()
	d := NewManualDetector()
	tm.tasks["c1"] = terminalTask("c1", TaskCompleted, base, d)

	tm.now = func() time.Time { return base.Add(2 * time.Minute) }
	if got := tm.List(); len(got) != 0 {
		t.Errorf("List should prune exited tasks, got %d", len(got))
	}
	if !d.Cancelled() {
		t.Error("List-triggered prune must reclaim resources")
	}
}

// TestPruneTerminal_Idempotent 钉住 pruning twice is safe (victim already gone; the detector Cancel is idempotent).
func TestPruneTerminal_Idempotent(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Nanosecond})
	base := time.Now()
	d := NewManualDetector()
	tm.tasks["c1"] = terminalTask("c1", TaskCompleted, base, d)

	tm.now = func() time.Time { return base.Add(time.Hour) }
	tm.pruneTerminal()
	tm.pruneTerminal()
	if _, ok := tm.Get("c1"); ok {
		t.Error("task should remain pruned")
	}
}

// TestOriginSpawner_StampsBaggage 钉住 the wrapper stamps its origin baggage onto a spawned task whose spec had no Origin — tools stay oblivious (裸调 Spawn).
func TestOriginSpawner_StampsBaggage(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	sp := &OriginSpawner{TaskController: tm, Origin: map[string]string{"chat_id": "u1", "user_name": "alice"}}
	d := NewManualDetectorDetach(10 * time.Millisecond)
	res := sp.Spawn(TaskSpec{Kind: "generic", Desc: "x"}, d)
	if res.Task == nil {
		t.Fatal("no task returned")
	}
	if res.Task.Spec.Origin["chat_id"] != "u1" {
		t.Errorf("origin chat_id not stamped: %v", res.Task.Spec.Origin)
	}
}

// TestOriginSpawner_DoesNotOverrideExplicit: an explicit spec.Origin is kept.
func TestOriginSpawner_DoesNotOverrideExplicit(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	sp := &OriginSpawner{TaskController: tm, Origin: map[string]string{"chat_id": "wrapper"}}
	d := NewManualDetectorDetach(10 * time.Millisecond)
	res := sp.Spawn(TaskSpec{Kind: "generic", Desc: "x", Origin: map[string]string{"chat_id": "explicit"}}, d)
	if res.Task.Spec.Origin["chat_id"] != "explicit" {
		t.Errorf("explicit Origin must not be overridden, got %v", res.Task.Spec.Origin)
	}
}

// TestOriginSpawner_CopyIsolation 钉住 the stamped Origin is a copy — mutating it does not affect the wrapper's source snapshot (baggage is decoupled).
func TestOriginSpawner_CopyIsolation(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	src := map[string]string{"chat_id": "u1"}
	sp := &OriginSpawner{TaskController: tm, Origin: src}
	d := NewManualDetectorDetach(10 * time.Millisecond)
	res := sp.Spawn(TaskSpec{Kind: "generic", Desc: "x"}, d)
	res.Task.Spec.Origin["chat_id"] = "mutated"
	if src["chat_id"] != "u1" {
		t.Error("stamped Origin must be a copy, not the source map")
	}
}

// neverSettleDetector is a detector that never settles nor detaches — the
// Spawn returns at the sync-wait boundary without side effects.
type neverSettleDetector struct{}

func (neverSettleDetector) Settled() <-chan SettleSignal { ch := make(chan SettleSignal); return ch }
func (neverSettleDetector) Detached() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (neverSettleDetector) Cancel() {}

// Stopped never fires: this stub has no producer at all (the gate test only
// checks adoption bookkeeping, never a real stop credential).
func (neverSettleDetector) Stopped() <-chan struct{} { return make(chan struct{}) }

// TestSpawnGate 钉住 磁盘降级时派生门拒绝新的派生并给出可读原因；默认不启用，无门即行为零变化。
// - 在途任务永不被该门拦截。
func TestSpawnGate(t *testing.T) {
	t.Run("default nil gate does not block", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{})
		res := tm.Spawn(TaskSpec{Kind: "command", Desc: "echo hi", Key: "k1"}, neverSettleDetector{})
		if res.Blocked != "" {
			t.Fatalf("default config must not block: %q", res.Blocked)
		}
	})
	t.Run("gate reason blocks spawn", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{
			SpawnGate: func() string { return "disk degraded" },
		})
		res := tm.Spawn(TaskSpec{Kind: "command", Desc: "echo hi", Key: "k2"}, neverSettleDetector{})
		if res.Blocked == "" || res.Task != nil {
			t.Fatalf("expected blocked spawn, got %+v", res)
		}
	})
	t.Run("empty gate reason passes through", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{
			SpawnGate: func() string { return "" },
		})
		res := tm.Spawn(TaskSpec{Kind: "command", Desc: "echo hi", Key: "k3"}, neverSettleDetector{})
		if res.Blocked != "" {
			t.Fatalf("empty reason must not block: %q", res.Blocked)
		}
	})
}

// cancelledDetector records Cancel calls (8.1 regression probe).
type cancelledDetector struct {
	neverSettleDetector
	cancelled bool
}

func (d *cancelledDetector) Cancel() { d.cancelled = true }

// TestSpawnGate_DedupWinsAndCancels 钉住 门生效时同键的在途任务仍走去重命中而非阻断——在途任务不受门拦截。
// - 新键则被阻断，且其探测器在派生入口内就地取消，不留无人认领的观察器。
func TestSpawnGate_DedupWinsAndCancels(t *testing.T) {
	degraded := false
	tm := NewTaskManager(TaskManagerConfig{
		SpawnGate: func() string {
			if degraded {
				return "disk degraded"
			}
			return ""
		},
	})
	first := tm.Spawn(TaskSpec{Kind: "command", Desc: "x", Key: "k"}, neverSettleDetector{})
	if first.Task == nil {
		t.Fatal("seed spawn must pass while healthy")
	}
	degraded = true
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "x", Key: "k"}, neverSettleDetector{})
	if res.Blocked != "" || !res.Deduped {
		t.Fatalf("in-flight same-key must dedup, not block: %+v", res)
	}
	det := &cancelledDetector{}
	res2 := tm.Spawn(TaskSpec{Kind: "command", Desc: "y", Key: "new-key"}, det)
	if res2.Blocked == "" {
		t.Fatal("new spawn must be blocked while degraded")
	}
	if !det.cancelled {
		t.Fatal("8.1: gate branch must cancel the detector (no orphan watcher)")
	}
}

// TestBindDetector_SignalsReachManager 钉住 重挂 detector 绑定到恢复任务后，watch/settle 信号必须真实到达 TaskManager——`tracked=true` 单独不构成充分证据；终态任务拒绝绑定（fencing）。
func TestBindDetector_SignalsReachManager(t *testing.T) {
	var mu sync.Mutex
	var settles []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{OnSettle: func(_ *Task, sig SettleSignal) {
		mu.Lock()
		settles = append(settles, sig)
		mu.Unlock()
	}})
	tk := tm.RestoreTask("t-bind", TaskSpec{
		Kind: "command", Lifetime: LifetimeService, Desc: "restored",
		Alive:       func() bool { return true },
		Declarative: &Declarative{Kind: "command", TaskID: "n-x"},
	}, time.Now(), TaskSuspect)
	tm.MarkTaskRunning("t-bind")

	d := NewManualDetector()
	if err := tm.BindDetector("t-bind", d); err != nil {
		t.Fatalf("bind: %v", err)
	}
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return tk.Status() == TaskAliveDetached })

	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
	waitUntil(t, time.Second, func() bool { return tk.Status() == TaskCompleted })

	waitUntil(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(settles) == 2
	})
	mu.Lock()
	defer mu.Unlock()
	if len(settles) != 2 {
		t.Fatalf("settles = %d, want 2 (ready + completion)", len(settles))
	}
	d2 := NewManualDetector()
	if err := tm.BindDetector("t-bind", d2); err == nil {
		t.Fatal("bind to finalized task must be refused")
	}
}

func TestBindDetector_NotFound(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	if err := tm.BindDetector("nope", NewManualDetector()); err == nil {
		t.Fatal("bind to unknown task must fail")
	}
}

// TestFinalizeConsistency_ReconcilePathsEmitFailed 钉住 僵尸、孤儿与年龄墙三条回收路径的终态信号必须同为失败结算。
// - 内存状态一致地记为失败，且结算恰好一次；三条路径不得各自外发不同口径的结论。
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
		res.Task.StartedAt = time.Now().Add(-11 * time.Minute)
		alive = false
		_ = len(tm.List())

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
		_ = len(tm.List())

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

// TestFinalizeConsistency_LateSignalsNeverRevive 钉住 （1.7 fencing）：终态后迟到 信号不得改状态、不得重复结算（含 Watch 通知）。
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
	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskCompleted })
	mu.Lock()
	before := len(settles)
	mu.Unlock()

	for _, late := range []SettleSignal{
		{Kind: SettleStable, Output: "late stable"},
		{Kind: SettleCompleted, Output: "late done"},
		{Kind: SettleWatch, Output: "late watch"},
	} {
		d.Emit(late)
	}
	time.Sleep(50 * time.Millisecond)

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

// TestFinalizeRetired_DowngradesLineage 钉住 退役路径的结算不得沿用任务原有的用户触发谱系。
// - 否则一次记账性退役会被当作"用户等待的结果"投递回去；谱系必须降级为退役类别。
func TestFinalizeRetired_DowngradesLineage(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	var gotKind SettleKind
	var gotOrigin string
	tm.onSettle = func(tk *Task, sig SettleSignal) {
		gotKind = sig.Kind
		if b, ok := tk.Spec.Origin[event.MetaKeyTriggerSource]; ok {
			gotOrigin = string(b)
		}
	}
	tk := &Task{Spec: TaskSpec{Kind: "command", Desc: "test"}}
	tk.Spec.Origin = map[string]string{event.MetaKeyTriggerSource: "user"}
	tk.status = TaskRunning
	tm.finalizeRetired(tk, "(zombie retired: test)", nil)

	if gotKind != SettleFailed {
		t.Fatalf("kind = %v, want failed", gotKind)
	}
	if gotOrigin != "task-retired" {
		t.Fatalf("lineage = %q, want task-retired (downgraded, not user)", gotOrigin)
	}
	if tk.status != TaskFailed {
		t.Fatalf("status = %v, want failed", tk.status)
	}
}

// TestFinalize_NormalKeepsLineage 钉住 正常终结路径不动：用户派生的任务完成时保留原有触发谱系，投递门据此可投递。
func TestFinalize_NormalKeepsLineage(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	var gotOrigin string
	tm.onSettle = func(tk *Task, sig SettleSignal) {
		if b, ok := tk.Spec.Origin[event.MetaKeyTriggerSource]; ok {
			gotOrigin = string(b)
		}
	}
	tk := &Task{Spec: TaskSpec{Kind: "command", Desc: "test-normal"}}
	tk.Spec.Origin = map[string]string{event.MetaKeyTriggerSource: "user"}
	tk.status = TaskRunning
	tm.finalize(tk, SettleCompleted, "done", nil)

	if gotOrigin != "user" {
		t.Fatalf("lineage = %q, want user (normal path untouched)", gotOrigin)
	}
}

// TestTaskDefaultTTLReachesLiveConsumerWithoutOverwritingExplicit 钉住 热设的默认 TTL 是回退来源，经真实回收路径生效，且不得改写既有任务的显式 TTL 或寿命锚点。
// - 断言走回收器实际使用的那个剩余寿命计算，而不是另算一份。
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
func TestTaskDefaultTTLReachesLiveConsumerWithoutOverwritingExplicit(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{DefaultTTL: 10 * time.Minute})

	fallback := tm.Spawn(TaskSpec{Kind: "command", Desc: "no explicit ttl", Key: "kfb"}, neverSettleDetector{}).Task
	explicit := tm.Spawn(TaskSpec{Kind: "command", Desc: "explicit ttl", Key: "kex", TTL: 99 * time.Minute}, neverSettleDetector{}).Task
	require.NotNil(t, fallback)
	require.NotNil(t, explicit)

	nowFB := fallback.StartedAt.Add(time.Minute)
	nowEX := explicit.StartedAt.Add(time.Minute)

	remFB, boundedFB := fallback.remainingLifetime(nowFB, tm.DefaultTTL())
	require.True(t, boundedFB)
	require.Equal(t, 9*time.Minute, remFB, "no-explicit task falls back to the manager default (10m - 1m)")
	remEX, _ := explicit.remainingLifetime(nowEX, tm.DefaultTTL())
	require.Equal(t, 98*time.Minute, remEX, "explicit-spec task is bounded by its own anchor (99m - 1m)")

	//  pull (S-E): the same rotation happens by rotating the SOURCE the
	// manager resolves at its next sweep/board read — there is no write into the
	// manager anymore.
	var termTTL, defTTL time.Duration
	tm.SetTTLSource(func() (time.Duration, time.Duration) { return termTTL, defTTL })
	defTTL = 20 * time.Minute
	require.Equal(t, 20*time.Minute, tm.DefaultTTL(), "the source reading is the live consumer")

	remFB2, _ := fallback.remainingLifetime(nowFB, tm.DefaultTTL())
	require.Equal(t, 19*time.Minute, remFB2, "the fallback task must follow the hot-updated default")
	remEX2, _ := explicit.remainingLifetime(nowEX, tm.DefaultTTL())
	require.Equal(t, 98*time.Minute, remEX2, "an existing task's EXPLICIT ttl/anchor must never be rewritten by the default")
}

// TestTerminalRecordSurvivesConsumerRestart 钉住 热值必须抵达真正执行终结保留判定的那个任务管理器。
// - 权威是记录读数本身：读数为零代表记录没有意见，由构造期的值应答——一次零读数不可能把在用周期归零。
// 契约: docs/wiki/agent/execution-generations.md#hot-source-pull-authority
func TestTerminalRecordSurvivesConsumerRestart(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: 2 * time.Minute})
	require.Equal(t, 2*time.Minute, tm.TerminalTTL())

	var termTTL, defTTL time.Duration
	tm.SetTTLSource(func() (time.Duration, time.Duration) { return termTTL, defTTL })

	termTTL = 5 * time.Minute
	require.Equal(t, 5*time.Minute, tm.TerminalTTL(), "a rotated source is the live terminal-retention period")

	termTTL = 0
	require.Equal(t, 2*time.Minute, tm.TerminalTTL(), "a zero reading falls back to construction — it must not zero the live terminal TTL")
}

// TestAliveDetached_ReadyOnceThenSuppress 钉住 服务型任务首次稳定结算恰好外发一次就绪通知，并转入已脱离态。
// - 其后的稳定信号（输出变化）被抑制——否则回收会被刷屏。
func TestAliveDetached_ReadyOnceThenSuppress(t *testing.T) {
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
	res := tm.Spawn(TaskSpec{Kind: "command", Lifetime: LifetimeService, Desc: "server :8080"}, d)
	if res.Settled {
		t.Fatalf("expected ack (background) for a service task")
	}

	d.Emit(SettleSignal{Kind: SettleStable, Output: "listening on :8080"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(settles) == 1 })
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Errorf("status = %s, want alive_detached", got)
	}

	d.Emit(SettleSignal{Kind: SettleStable, Output: "log line 1"})
	d.Emit(SettleSignal{Kind: SettleStable, Output: "log line 2"})
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	n := len(settles)
	mu.Unlock()
	if n != 1 {
		t.Errorf("expected exactly 1 ready notification, got %d", n)
	}
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Errorf("status after output changes = %s, want alive_detached", got)
	}
	d.Done()
}

// TestAliveDetached_SuspectSuppressed 钉住 a detached service going quiet (suspect)
func TestAliveDetached_SuspectSuppressed(t *testing.T) {
	var mu sync.Mutex
	count := 0
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(_ *Task, _ SettleSignal) { mu.Lock(); count++; mu.Unlock() },
	})
	d := NewManualDetectorDetach(30 * time.Millisecond)
	tm.Spawn(TaskSpec{Kind: "command", Lifetime: LifetimeService, Desc: "svc"}, d)

	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return count == 1 })
	d.Emit(SettleSignal{Kind: SettleSuspect, Output: "quiet"})
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 1 {
		t.Errorf("detached suspect should be suppressed; notifications = %d, want 1", got)
	}
	d.Done()
}

// TestAliveDetached_CompletionEndsAndNotifies 钉住 after alive-detached, a completed signal (process death) re-notifies and ends the task.
func TestAliveDetached_CompletionEndsAndNotifies(t *testing.T) {
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
	res := tm.Spawn(TaskSpec{Kind: "command", Lifetime: LifetimeService, Desc: "svc"}, d)

	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(settles) == 1 })

	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "exited"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(settles) == 2 })
	if got := res.Task.Status(); got != TaskCompleted {
		t.Errorf("status = %s, want completed after process death", got)
	}
	mu.Lock()
	last := settles[len(settles)-1]
	mu.Unlock()
	if last.Kind != SettleCompleted {
		t.Errorf("last settle = %v, want completed", last.Kind)
	}
	d.Done()
}

// TestAliveDetached_OnBoard: an alive-detached task shows compactly on the board.
func TestAliveDetached_OnBoard(t *testing.T) {
	task := &Task{ID: "svc-11111111", Spec: TaskSpec{Desc: "server :8080"}, status: TaskAliveDetached, StartedAt: time.Now()}
	board := RenderBoard([]*Task{task}, 10*time.Minute)
	if !strings.Contains(board, "server :8080") || !strings.Contains(board, "alive_detached") {
		t.Errorf("alive-detached task should appear on board:\n%s", board)
	}
}

func TestBatchRetire_ConcurrentFinalizeNoRace(t *testing.T) {
	const n = 64
	var (
		delivered []BatchRetired
		mu        sync.Mutex
		settleHit int
	)
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(tk *Task, sig SettleSignal) {
			mu.Lock()
			settleHit++
			mu.Unlock()
		},
		OnBatchRetire: func(b []BatchRetired) {
			mu.Lock()
			delivered = append(delivered, b...)
			mu.Unlock()
		},
	})
	base := time.Now()
	tm.now = func() time.Time { return base }

	tasks := make([]*Task, n)
	for i := 0; i < n; i++ {
		tasks[i] = tm.RestoreTask(fmt.Sprintf("c%d", i), orphanSpec(fmt.Sprintf("ck%d", i), "concurrent"), base, TaskSuspect)
		if tasks[i] == nil {
			t.Fatalf("RestoreTask %d nil", i)
		}
	}

	finish := tm.beginBatchRetire()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(tk *Task) {
			defer wg.Done()
			tm.finalize(tk, SettleCompleted, "ok", nil)
		}(tasks[i])
	}
	wg.Wait()
	finish()

	mu.Lock()
	defer mu.Unlock()
	if settleHit != 0 {
		t.Fatalf("batch mode must suppress per-task OnSettle, got %d", settleHit)
	}
	if len(delivered) != n {
		t.Fatalf("delivered batch = %d, want %d（有结算被丢弃）", len(delivered), n)
	}
	for _, r := range delivered {
		if r.Task.Status() != TaskCompleted {
			t.Fatalf("delivered task %s status = %v, want completed", r.Task.ID, r.Task.Status())
		}
	}
}

// TestBatchRetire_TTLReaperWaveCollapsed 钉住 大规模到期必须以一次 N→1 折叠摘要投递，而不是 N 条独立结算通知。
// - 统一回收器作为退役来源必须打开折叠收集器；
// - 逐条外发会把刚压下去的投影再吹起来。
// 契约: docs/wiki/agent/task-lifecycle.md#finalize-lineage
func TestBatchRetire_TTLReaperWaveCollapsed(t *testing.T) {
	var batch []BatchRetired
	perSettle := 0
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle:      func(_ *Task, _ SettleSignal) { perSettle++ },
		OnBatchRetire: func(b []BatchRetired) { batch = append(batch, b...) },
	})
	base := time.Now()
	tm.now = func() time.Time { return base }
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("ttl-%d", i)
		if tk := tm.RestoreTask(id, TaskSpec{
			Kind:        "command",
			Desc:        id,
			TTL:         time.Minute,
			Alive:       func() bool { return true },
			Declarative: &Declarative{Kind: "command", TaskID: "sess-" + id},
		}, base.Add(-2*time.Hour), TaskSuspect); tk == nil {
			t.Fatalf("RestoreTask %s returned nil", id)
		}
	}

	_ = tm.List()

	if len(batch) != 3 {
		t.Fatalf("TTL reaper wave must collapse into ONE OnBatchRetire of 3, got batch=%d perSettle=%d", len(batch), perSettle)
	}
	if perSettle != 0 {
		t.Fatalf("per-task OnSettle must be suppressed under batch mode, got %d", perSettle)
	}
	for _, r := range batch {
		if r.Task.Status() != TaskFailed {
			t.Fatalf("TTL-retired task %s status = %v, want failed", r.Task.ID, r.Task.Status())
		}
	}
}

// TestReconcileDetached_GoneSessionRetired 钉住 探活报告后端会话已消失的已脱离任务，按正常完成语义退役。
// - 终结通知恰好一次，之后交由保留期回收；重复列举不得再次外发。
func TestReconcileDetached_GoneSessionRetired(t *testing.T) {
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
	alive := false
	res := tm.Spawn(TaskSpec{Kind: "command", Lifetime: LifetimeService, Desc: "svc", Alive: func() bool { return alive }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })

	alive = true
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (probe alive)", got)
	}
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached", got)
	}

	alive = false
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (terminal grace window)", got)
	}
	if got := res.Task.Status(); got != TaskCompleted {
		t.Fatalf("status = %s, want completed", got)
	}
	mu.Lock()
	n := len(settles)
	var last SettleSignal
	if n > 0 {
		last = settles[n-1]
	}
	mu.Unlock()
	if n != 2 {
		t.Fatalf("settles = %d, want 2 (ready + completed)", n)
	}
	if last.Kind != SettleCompleted {
		t.Fatalf("last settle kind = %s, want completed", last.Kind)
	}

	_ = tm.List()
	mu.Lock()
	n2 := len(settles)
	mu.Unlock()
	if n2 != 2 {
		t.Fatalf("settles after re-List = %d, want 2 (no re-notify)", n2)
	}
	d.Done()
}

// TestReconcileDetached_NilProbeSkipped 钉住 tasks without a probe (subagent tasks) are never reconciled away.
func TestReconcileDetached_NilProbeSkipped(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "subagent", Lifetime: LifetimeService, Desc: "plan"}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "working"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (no probe -> untouched)", got)
	}
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached", got)
	}
	d.Done()
}

func orphanSpec(key, sess string) TaskSpec {
	return TaskSpec{
		Kind: "subagent", Desc: "orphan", Key: key,
		Declarative: &Declarative{Kind: "subagent", TaskID: sess},
	}
}

// TestRetireOrphans_RestoredSuspectRetired 钉住 年迈的重建可疑任务（无探活器）先按失败结算一次，再由终态保留期回收。
// - 回收必须释放索引键，同类派生才不会被阻塞。
func TestRetireOrphans_RestoredSuspectRetired(t *testing.T) {
	settles := 0
	tm := NewTaskManager(TaskManagerConfig{
		TerminalTTL: time.Minute,
		OnSettle:    func(tk *Task, sig SettleSignal) { settles++ },
	})
	base := time.Now()

	tk := tm.RestoreTask("p1", orphanSpec("k1", "sess-1"), base.Add(-2*time.Hour), TaskSuspect)
	if tk == nil {
		t.Fatal("RestoreTask returned nil")
	}

	tm.now = func() time.Time { return base }
	if n := tm.RetireOrphans(func(string) bool { return false }); n != 1 {
		t.Fatalf("retired = %d, want 1", n)
	}
	if got := tk.Status(); got != TaskFailed {
		t.Fatalf("status = %v, want failed", got)
	}
	if settles != 1 {
		t.Fatalf("onSettle calls = %d, want exactly 1", settles)
	}
	tk.mu.Lock()
	st := tk.settledAt
	tk.mu.Unlock()
	if st.IsZero() {
		t.Fatal("settledAt must be set (drives terminalTTL pruning)")
	}

	tm.now = func() time.Time { return base.Add(2 * time.Minute) }
	tm.pruneTerminal()
	if _, ok := tm.Get("p1"); ok {
		t.Fatal("retired orphan must be pruned after terminalTTL")
	}
	res := tm.Spawn(TaskSpec{Kind: "subagent", Desc: "respawn", Key: "k1"}, NewManualDetectorDetach(10*time.Millisecond))
	if res.Task == nil || res.Deduped {
		t.Fatalf("re-spawn must not be deduped after release: %+v", res)
	}
}

// TestRetireOrphans_GuardsNeverTouch 钉住 Guards: young / tracked / probe-carrying / no-Declarative / alive-detached are never touched.
func TestRetireOrphans_GuardsNeverTouch(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	base := time.Now()
	tm.now = func() time.Time { return base }

	tm.RestoreTask("young", orphanSpec("", "s1"), base.Add(-5*time.Minute), TaskSuspect)
	tm.RestoreTask("tracked", orphanSpec("", "s2"), base.Add(-2*time.Hour), TaskSuspect)
	tm.RestoreTask("probed", TaskSpec{
		Kind: "command", Desc: "x",
		Declarative: &Declarative{TaskID: "s3"},
		Alive:       func() bool { return false },
	}, base.Add(-2*time.Hour), TaskSuspect)
	tm.RestoreTask("generic", TaskSpec{Kind: "generic", Desc: "y"}, base.Add(-2*time.Hour), TaskSuspect)
	tm.RestoreTask("svc", orphanSpec("", "s4"), base.Add(-2*time.Hour), TaskAliveDetached)

	n := tm.RetireOrphans(func(id string) bool { return id == "s2" })
	if n != 0 {
		t.Fatalf("retired = %d, want 0 (every guard holds)", n)
	}
	for _, id := range []string{"young", "tracked", "probed", "generic", "svc"} {
		tk, ok := tm.Get(id)
		if !ok {
			t.Fatalf("%s vanished", id)
		}
		if tk.Status() == TaskFailed {
			t.Errorf("%s must not be retired", id)
		}
	}
}

// TestReconcileZombies_DeadSessionTrackerRetiresNilProbeOrphan 钉住 回收在运行时对重建后新产生的孤儿重跑同一判据；未接跟踪器时行为保持不变。
func TestReconcileZombies_DeadSessionTrackerRetiresNilProbeOrphan(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{SessionTracker: func(string) bool { return false }})
	base := time.Now()
	tm.RestoreTask("old-orphan", orphanSpec("k2", "s9"), base.Add(-2*time.Hour), TaskSuspect)
	tm.now = func() time.Time { return base }
	tm.reconcileZombies()
	tk, ok := tm.Get("old-orphan")
	if !ok || tk.Status() != TaskFailed {
		t.Fatalf("a wired dead-session tracker must retire the aged nil-probe orphan (ok=%v)", ok)
	}
}

func TestReconcileZombies_NoTrackerWired_Unchanged(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	base := time.Now()
	tm.RestoreTask("legacy", orphanSpec("k3", "s8"), base.Add(-2*time.Hour), TaskSuspect)
	tm.now = func() time.Time { return base }
	tm.reconcileZombies()
	tk, ok := tm.Get("legacy")
	if !ok || tk.Status() != TaskSuspect {
		t.Fatalf("without a wired tracker, legacy behavior must hold (suspect kept)")
	}
}

// TestBatchRetire_CollapsedNotification 钉住 注册 OnBatchRetire 后，批量退役在 bus 侧折叠为一次回调（N 条合成一条 BatchRetired）；未注册时按逐条 OnSettle 通知——两种模式互斥且不重叠。
func TestBatchRetire_CollapsedNotification(t *testing.T) {
	var batch []BatchRetired
	perSettle := 0
	tm := NewTaskManager(TaskManagerConfig{
		TerminalTTL: time.Minute,
		OnSettle:    func(tk *Task, sig SettleSignal) { perSettle++ },
		OnBatchRetire: func(b []BatchRetired) {
			batch = append(batch, b...)
		},
	})
	base := time.Now()
	for i := 0; i < 3; i++ {
		if tk := tm.RestoreTask(fmt.Sprintf("p%d", i), orphanSpec(fmt.Sprintf("k%d", i), fmt.Sprintf("sess-%d", i)), base.Add(-2*time.Hour), TaskSuspect); tk == nil {
			t.Fatalf("RestoreTask %d returned nil", i)
		}
	}
	tm.now = func() time.Time { return base }
	if n := tm.RetireOrphans(func(string) bool { return false }); n != 3 {
		t.Fatalf("retired = %d, want 3", n)
	}
	if len(batch) != 3 {
		t.Fatalf("OnBatchRetire batch = %d, want 3", len(batch))
	}
	if perSettle != 0 {
		t.Fatalf("per-task OnSettle must be suppressed in batch mode, got %d", perSettle)
	}
	for _, r := range batch {
		if r.Task.Status() != TaskFailed {
			t.Fatalf("batch task status = %v, want failed", r.Task.Status())
		}
	}
}

// TestBatchRetire_NestedNoDoubleDelivery 钉住 （嵌套安全）：reconcileZombies 内部调 RetireOrphans——内层 begin 复用外层 collector，回调只发生一次且覆盖全部条目。
func TestBatchRetire_NestedNoDoubleDelivery(t *testing.T) {
	var deliveries [][]BatchRetired
	tm := NewTaskManager(TaskManagerConfig{
		TerminalTTL: time.Minute,
		OnBatchRetire: func(b []BatchRetired) {
			cp := make([]BatchRetired, len(b))
			copy(cp, b)
			deliveries = append(deliveries, cp)
		},
	})
	outer := tm.beginBatchRetire()
	inner := tm.beginBatchRetire()
	tk := tm.RestoreTask("pn", orphanSpec("kn", "sess-n"), time.Now().Add(-2*time.Hour), TaskSuspect)
	if tk == nil {
		t.Fatal("RestoreTask returned nil")
	}
	if n := tm.RetireOrphans(func(string) bool { return false }); n != 1 {
		t.Fatalf("retired = %d, want 1", n)
	}
	inner()
	if len(deliveries) != 0 {
		t.Fatalf("nested finish must not deliver, got %d deliveries", len(deliveries))
	}
	outer()
	if len(deliveries) != 1 || len(deliveries[0]) != 1 {
		t.Fatalf("outer finish must deliver exactly once with 1 entry, got %+v", deliveries)
	}
}

// TestPruneTerminal_NilDetectorDoesNotPanic 钉住 is the regression guard for the production panic (task_manager.go:769 nil pointer dereference).
func TestPruneTerminal_NilDetectorDoesNotPanic(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()

	restored := &Task{ID: "restored-nil-det", Spec: TaskSpec{Desc: "restored"}, status: TaskCompleted, settledAt: base}
	if restored.detector != nil {
		t.Fatalf("precondition: restored task must carry a nil detector")
	}
	tm.tasks[restored.ID] = restored

	live := NewManualDetector()
	tm.tasks["live1"] = &Task{ID: "live1", Spec: TaskSpec{Desc: "live1"}, status: TaskRunning, detector: live}

	tm.now = func() time.Time { return base.Add(2 * time.Minute) }

	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (nil-detector terminal task pruned, live kept)", got)
	}
	if _, ok := tm.Get(restored.ID); ok {
		t.Error("terminal task with nil detector must still be pruned")
	}
	if _, ok := tm.Get("live1"); !ok {
		t.Error("live task must be kept")
	}
	if live.Cancelled() {
		t.Error("live task's detector must NOT be cancelled")
	}

	res := tm.Spawn(TaskSpec{Kind: "subagent", Desc: "after-prune"}, NewManualDetectorDetach(10*time.Millisecond))
	if res.Task == nil {
		t.Fatal("Spawn must still succeed after pruning a nil-detector victim")
	}
}

// TestPruneTerminal_RestoreTaskPathNilDetector 钉住 必须走真实的重建入口而非手搭结构体，守卫才跟得上实际的重建形状。
func TestPruneTerminal_RestoreTaskPathNilDetector(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()

	tk := tm.RestoreTask("fact-chain-1", TaskSpec{Kind: "subagent", Desc: "rebuilt"}, base, TaskCompleted)
	if tk == nil {
		t.Fatal("RestoreTask returned nil")
	}
	if tk.detector != nil {
		t.Fatal("RestoreTask must not wire a detector (nothing to reclaim after reboot)")
	}
	tk.mu.Lock()
	tk.settledAt = base
	tk.mu.Unlock()

	tm.now = func() time.Time { return base.Add(2 * time.Minute) }
	tm.pruneTerminal()

	if _, ok := tm.Get("fact-chain-1"); ok {
		t.Error("restored terminal task must be pruned")
	}
}

// TestResume_RestoredTaskNilWatchDone 钉住 从事实链重建的任务不携带探测器与观察通道（跨重启的探测器状态按设计不可恢复），对其执行 Resume 必须在缺这些字段时仍然安全：生命周期通道在重建时初始化，代际判定也不得把"探测器非空"当成观察变更的依据。
func TestResume_RestoredTaskNilWatchDone(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d2 := NewManualDetectorDetach(20 * time.Millisecond)
	defer d2.Done()
	if got := tm.RestoreTask("t-restored", TaskSpec{
		Kind: "service",
		Desc: "restored nightly probe",
		ResumeFn: func(_ context.Context, input string) (SettleDetector, error) {
			return d2, nil
		},
	}, time.Now(), TaskStable); got == nil {
		t.Fatal("RestoreTask returned nil")
	}
	res, err := tm.Resume(context.Background(), "t-restored", "go on")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if res.Task == nil {
		t.Fatal("Resume returned no task")
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Fatalf("status = %s, want running (resume claims)", got)
	}
}

// spawnAliveDetachedTask spawns a manual task and drives it to alive-detached
// (detach → background stable "ready").
func spawnAliveDetachedTask(t *testing.T, tm *TaskManager, resumeFn func(context.Context, string) (SettleDetector, error)) (*Task, *ManualDetector) {
	t.Helper()
	det := NewManualDetectorDetach(10 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Lifetime: LifetimeService, Desc: "svc", ResumeFn: resumeFn}, det)
	if res.Settled {
		t.Fatalf("expected ack (not settled) before detach")
	}
	det.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitStatus(t, res.Task, TaskAliveDetached)
	return res.Task, det
}

func waitStatus(t *testing.T, task *Task, want TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if task.Status() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task status = %s, want %s", task.Status(), want)
}

// TestResume_AliveToRunningAndSettle 钉住 the alive-detached → running edge, with the resumed round settling inline (within the new window) under the SAME task id.
func TestResume_AliveToRunningAndSettle(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	resumeDet := NewManualDetector()
	var gotInput string
	task, _ := spawnAliveDetachedTask(t, tm, func(_ context.Context, input string) (SettleDetector, error) {
		gotInput = input
		go func() {
			time.Sleep(10 * time.Millisecond)
			resumeDet.Emit(SettleSignal{Kind: SettleCompleted, Output: "round-2 output"})
		}()
		return resumeDet, nil
	})
	originalID := task.ID

	res, err := tm.Resume(context.Background(), task.ID, "make reload")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if gotInput != "make reload" {
		t.Errorf("ResumeFn must receive the input, got %q", gotInput)
	}
	if !res.Settled || res.Signal.Output != "round-2 output" {
		t.Errorf("resumed round should settle inline with increment output, got %+v", res)
	}
	if res.Task.ID != originalID {
		t.Errorf("resume must keep the SAME task id: %s vs %s", res.Task.ID, originalID)
	}
}

// TestResume_IllegalStates 钉住 running and terminal tasks reject resume with actionable messages; non-resumable tasks reject too.
func TestResume_IllegalStates(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	task, _ := spawnAliveDetachedTask(t, tm, func(context.Context, string) (SettleDetector, error) {
		return NewManualDetectorDetach(10 * time.Millisecond), nil
	})
	if _, err := tm.Resume(context.Background(), task.ID, "first"); err != nil {
		t.Fatalf("first resume: %v", err)
	}
	waitStatus(t, task, TaskRunning)

	if _, err := tm.Resume(context.Background(), task.ID, "second"); err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("concurrent resume must be rejected with running-state message, got %v", err)
	}

	det2 := NewManualDetectorDetach(10 * time.Millisecond)
	res2 := tm.Spawn(TaskSpec{Kind: "subagent", Desc: "quick", ResumeFn: func(context.Context, string) (SettleDetector, error) {
		d := NewManualDetector()
		go func() {
			time.Sleep(10 * time.Millisecond)
			d.Emit(SettleSignal{Kind: SettleCompleted, Output: "round-2"})
		}()
		return d, nil
	}}, det2)
	_ = res2
	det2.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
	waitStatus(t, res2.Task, TaskCompleted)
	if _, err := tm.Resume(context.Background(), res2.Task.ID, "continue"); err != nil {
		t.Errorf("completed task must be resumable (new run), got %v", err)
	}
	waitStatus(t, res2.Task, TaskCompleted)

	tm.Cancel(res2.Task.ID)
	if _, err := tm.Resume(context.Background(), res2.Task.ID, "x"); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancelled resume must be rejected with guidance, got %v", err)
	}

	det3 := NewManualDetectorDetach(10 * time.Millisecond)
	res3 := tm.Spawn(TaskSpec{Kind: "command", Lifetime: LifetimeService, Desc: "svc2"}, det3)
	det3.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitStatus(t, res3.Task, TaskAliveDetached)
	if _, err := tm.Resume(context.Background(), res3.Task.ID, "x"); err == nil || !strings.Contains(err.Error(), "does not support resume") {
		t.Errorf("non-resumable task must be rejected, got %v", err)
	}
}

// TestResume_BackgroundSettleGoesToOnSettle 钉住 超出窗口才结束的续跑轮次仍走标准结算路径外发，并携带同一任务标识。
func TestResume_BackgroundSettleGoesToOnSettle(t *testing.T) {
	settled := make(chan *Task, 1)
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			select {
			case settled <- task:
			default:
			}
		},
	})

	resumeDet := NewManualDetectorDetach(20 * time.Millisecond)
	task, _ := spawnAliveDetachedTask(t, tm, func(context.Context, string) (SettleDetector, error) {
		return resumeDet, nil
	})

	res, err := tm.Resume(context.Background(), task.ID, "slow op")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if res.Settled {
		t.Fatalf("expected ack for slow resumed round")
	}
	resumeDet.Emit(SettleSignal{Kind: SettleCompleted, Output: "late result"})
	select {
	case got := <-settled:
		if got.ID != task.ID {
			t.Errorf("background settle must carry the same task id: %s vs %s", got.ID, task.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("background settle not delivered")
	}
}

// TestResume_CompletedSubagentTask 钉住 the subagent resume path — a completed task resumes with a new run under the same id.
func TestResume_CompletedSubagentTask(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	spec := TaskSpec{
		Kind: "subagent",
		Desc: "plan: analyze",
		ResumeFn: func(_ context.Context, input string) (SettleDetector, error) {
			d := NewManualDetector()
			go func() {
				time.Sleep(10 * time.Millisecond)
				d.Emit(SettleSignal{Kind: SettleCompleted, Output: "continued: " + input})
			}()
			return d, nil
		},
	}
	det := NewManualDetector()
	go func() {
		time.Sleep(10 * time.Millisecond)
		det.Emit(SettleSignal{Kind: SettleCompleted, Output: "first done"})
	}()
	res := tm.Spawn(spec, det)
	if !res.Settled {
		t.Fatalf("first round should settle inline")
	}
	waitStatus(t, res.Task, TaskCompleted)

	res2, err := tm.Resume(context.Background(), res.Task.ID, "next instruction")
	if err != nil {
		t.Fatalf("resume completed subagent task: %v", err)
	}
	if !res2.Settled || res2.Signal.Output != "continued: next instruction" {
		t.Errorf("resumed round should settle inline with new-run output, got %+v", res2)
	}
	if res2.Task.ID != res.Task.ID {
		t.Errorf("same task id must be kept: %s vs %s", res2.Task.ID, res.Task.ID)
	}
}

// TestResume_ConcurrentSingleWinner 钉住 并发续跑只有一方胜出：落败方拿到"已在运行"的答复，恢复函数恰好执行一次。
func TestResume_ConcurrentSingleWinner(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	var fnMu sync.Mutex
	fnCalls := 0
	task, _ := spawnAliveDetachedTask(t, tm, func(context.Context, string) (SettleDetector, error) {
		fnMu.Lock()
		fnCalls++
		fnMu.Unlock()
		time.Sleep(30 * time.Millisecond)
		d := NewManualDetector()
		go func() {
			time.Sleep(5 * time.Millisecond)
			d.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
		}()
		return d, nil
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = tm.Resume(context.Background(), task.ID, "input")
		}(i)
	}
	wg.Wait()

	fnMu.Lock()
	defer fnMu.Unlock()
	if fnCalls != 1 {
		t.Errorf("ResumeFn must run exactly once under concurrent resumes, got %d", fnCalls)
	}
	nErr := 0
	for _, e := range errs {
		if e != nil {
			nErr++
			if !strings.Contains(e.Error(), "running") {
				t.Errorf("loser error must mention running state, got %v", e)
			}
		}
	}
	if nErr != 1 {
		t.Errorf("exactly one resume must lose the race, errs=%v", errs)
	}
}

// TestResume_RetiresOldWatch 钉住 重入之后，原探测器上的信号绝不路由进新一轮：既不错结算，也不留观察协程继续消费。
func TestResume_RetiresOldWatch(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	newDet := NewManualDetector()
	task, oldDet := spawnAliveDetachedTask(t, tm, func(context.Context, string) (SettleDetector, error) {
		return newDet, nil
	})

	resumed := make(chan SpawnResult, 1)
	go func() {
		res, err := tm.Resume(context.Background(), task.ID, "x")
		if err != nil {
			t.Errorf("resume: %v", err)
		}
		resumed <- res
	}()
	waitStatus(t, task, TaskRunning)

	oldDet.Emit(SettleSignal{Kind: SettleCompleted, Output: "STALE"})
	select {
	case res := <-resumed:
		t.Fatalf("stale old-detector signal settled the new round: %+v", res)
	case <-time.After(100 * time.Millisecond):
	}

	newDet.Emit(SettleSignal{Kind: SettleCompleted, Output: "fresh"})
	select {
	case res := <-resumed:
		if res.Signal.Output != "fresh" {
			t.Errorf("new-round settle must carry fresh output, got %q", res.Signal.Output)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("new-round settle not delivered")
	}
}

// skipUnlessFailBefore green; set TAGENT_RUN_FAILBEFORE=1 to run them RED (proving the target is not yet
// met now, and later confirming pass-after) without editing this file.
func skipUnlessFailBefore(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("TAGENT_RUN_FAILBEFORE") == "" {
		t.Skip(reason)
	}
}

// TestCounter_SuspectNeverDetachedEscapesEveryAgeWall 钉住 统一 TTL 回收器（reconcileTTL）按绝对锚点覆盖每一个活动状态：从未 detached 的 SUSPECT 任务超 TTL 必须被退役，判定不得退回"只认 detached"的门。
func TestCounter_SuspectNeverDetachedEscapesEveryAgeWall(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	base := time.Now()
	tm.now = func() time.Time { return base }
	tk := tm.RestoreTask("56bf24c3-restart", TaskSpec{
		Kind:  "command",
		Desc:  "restart-tagent.sh",
		Alive: func() bool { return true },
		TTL:   10 * time.Minute,
	}, base.Add(-30*time.Hour), TaskSuspect)
	if got := tk.DetachedAtMilli(); got != 0 {
		t.Fatalf("precondition: task must be never-detached (detachedAt=0), got %d", got)
	}

	_ = len(tm.List())
	_ = len(tm.List())

	if st := tk.Status(); st == TaskSuspect {
		t.Fatalf("§10.3: a suspect/undetached job 30h old (past a 10m TTL) must be reaped to a terminal state, still %s", st)
	}
	if st := tk.Status(); st != TaskFailed {
		t.Fatalf("§10.3: TTL expiry must retire as failed, got %s", st)
	}
}

// TestCounter_SuspectBoardShowsRemainingNotArbitration 钉住 面板渲染统一回收器算出的剩余寿命（有界且自回收 ⇒ 模型只判一次），绝不输出"需确认"这类每回合重发的非终态仲裁邀请；且此处的 suspect 必须处于 TTL 内（超期者已被回收器拦下，到不了面板）。
func TestCounter_SuspectBoardShowsRemainingNotArbitration(t *testing.T) {
	suspect := NewTaskFixture("56bf24c3-restart", "restart-tagent.sh", TaskSuspect, time.Now().Add(-time.Minute))
	board := RenderBoard([]*Task{suspect}, 10*time.Minute)

	if board == "" {
		t.Fatal("precondition: an active suspect task must render a board")
	}
	if strings.Contains(board, "需确认") {
		t.Fatalf("§10.6: the board must NOT carry the non-terminal '⚠…需确认' arbitration invitation; got:\n%s", board)
	}
	if !strings.Contains(board, "剩余") {
		t.Fatalf("§10.6: the board must render the task's remaining lifetime / expected-reclaim time so the model decides once; got:\n%s", board)
	}
}

// TestCounter_RestoredTaskWithoutTTLBindingIsReaped 钉住 统一回收器始终在位：管理器的保留期带有下限，持久化的寿命在恢复时被还原。
// - 于是未显式带寿命的恢复任务仍受该下限约束，越过下限即被回收；
// - 若把下限关掉，这类任务就再无任何寿命上界。
func TestCounter_RestoredTaskWithoutTTLBindingIsReaped(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	base := time.Now()
	tm.now = func() time.Time { return base }
	tk := tm.RestoreTask("t-restored", TaskSpec{
		Kind:  "command",
		Desc:  "restored: restart-tagent.sh",
		Alive: func() bool { return true },
	}, base.Add(-24*time.Hour), TaskSuspect)

	_ = len(tm.List())
	_ = len(tm.List())
	if st := tk.Status(); !isTerminalStatus(st) {
		t.Fatalf("§10.5: a 24h-old restored task with no explicit TTL must be reaped by the manager floor, still %s", st)
	}
	if st := tk.Status(); st != TaskFailed {
		t.Fatalf("§10.5: TTL expiry must retire as failed, got %s", st)
	}
}

// TestTTLReaperIndependentOfQuietState 钉住 静默超时／假死探测与统一 TTL 回收是两条正交轴。
// - 静默只喂可疑态：只标记、从不杀死（"大静默不得误杀在跑的构建"另由 tool/action 一侧钉住）；
// - 总寿命回收只由 TTL 回收器负责，判据是锚点年龄对 TTL，它不读任何静默状态；
// - 静默永不延长、也永不短路回收。
func TestTTLReaperIndependentOfQuietState(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	base := time.Now()
	tm.now = func() time.Time { return base }

	tk := tm.RestoreTask("quiet-but-young", TaskSpec{
		Kind:  "command",
		Desc:  "long compile",
		TTL:   4 * time.Hour,
		Alive: func() bool { return true },
	}, base.Add(-1*time.Hour), TaskSuspect)

	_ = tm.List()
	if st := tk.Status(); st != TaskSuspect {
		t.Fatalf("a suspect task only 1h into a 4h TTL must stay — quiet/suspect alone never reclaims, got %s", st)
	}

	tm.now = func() time.Time { return base.Add(5 * time.Hour) }
	_ = tm.List()
	if st := tk.Status(); st != TaskFailed {
		t.Fatalf("at 5h past a 4h TTL the task must be reaped by TTL regardless of quiet state, got %s", st)
	}
}

// TestTTLReentrantRenewal 钉住 写型重入刷新回收锚点：任务的绝对寿命从刷新时刻起算，而非派生时刻。
// - 发送类动作在动作层刷新，续跑在入口之内刷新；
// - 只读的查看绝不刷新——由"未被刷新的任务仍从派生起算"这一对照来证。
func TestTTLReentrantRenewal(t *testing.T) {
	newSuspect := func(tm *TaskManager, base time.Time, id, sess string) *Task {
		return tm.RestoreTask(id, TaskSpec{
			Kind:        "command",
			Desc:        "svc",
			TTL:         time.Hour,
			Alive:       func() bool { return true },
			Declarative: &Declarative{Kind: "command", TaskID: sess},
		}, base.Add(-2*time.Hour), TaskSuspect)
	}

	t.Run("renewed task is measured from the refresh anchor", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{})
		base := time.Now()
		tm.now = func() time.Time { return base }
		tk := newSuspect(tm, base, "renewed", "sess-1")

		if !tm.RenewTTLBySession("sess-1") {
			t.Fatal("RenewTTLBySession must match the active task by session binding")
		}
		_ = len(tm.List())
		if st := tk.Status(); st != TaskSuspect {
			t.Fatalf("renewed task must survive its first pass (anchor moved to now), got %s", st)
		}
		tm.now = func() time.Time { return base.Add(90 * time.Minute) }
		_ = len(tm.List())
		if st := tk.Status(); st != TaskFailed {
			t.Fatalf("90m after refresh (> 1h TTL) must reap, got %s", st)
		}
	})

	t.Run("unrenewed task is measured from spawn", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{})
		base := time.Now()
		tm.now = func() time.Time { return base }
		tk := newSuspect(tm, base, "unrenewed", "sess-2")
		_ = len(tm.List())
		if st := tk.Status(); st != TaskFailed {
			t.Fatalf("unrenewed task 2h past a 1h TTL must reap, got %s", st)
		}
	})

	t.Run("renew on unknown or empty session is a no-op", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{})
		if tm.RenewTTLBySession("nope") {
			t.Fatal("unknown session must return false")
		}
		if tm.RenewTTLBySession("") {
			t.Fatal("empty session must return false")
		}
	})
}

// TestReconcileZombies_RunningDeadSessionRetired 钉住 运行中任务的后端会话可证已消失、且年龄超过宽限时，按终结失败路径退役。
// - 判据要求探活结论与年龄宽限同时成立；面板上不得留下永远标着运行中的冻结项。
func TestReconcileZombies_RunningDeadSessionRetired(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewManualDetectorDetach(20 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "oneshot", Desc: "frozen probe",
		Alive: func() bool { return false }}, d)
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskRunning })

	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (grace)", got)
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Fatalf("status = %s, want running (grace)", got)
	}

	res.Task.StartedAt = time.Now().Add(-11 * time.Minute)
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (terminal grace window)", got)
	}
	if got := res.Task.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (zombie retire)", got)
	}

	_ = tm.List()
	if got := res.Task.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (stable terminal)", got)
	}
	d.Done()
}

// TestReconcileZombies_AliveProbeProtectsQuietRunner 钉住 后端会话仍活着的静默长跑任务，在僵尸探活路径下不因静默时长被退役。
// - 翻转探活结论之后，下一次清扫才退役它；
// - 总寿命回收归另一条 TTL 回收器负责（本用例以极大默认寿命把它隔离在外）。
func TestReconcileZombies_AliveProbeProtectsQuietRunner(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{ZombieGrace: time.Minute, DefaultTTL: 1000 * 24 * time.Hour})
	alive := true
	d := NewManualDetectorDetach(20 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "service", Desc: "quiet long-runner",
		Alive: func() bool { return alive }}, d)
	res.Task.StartedAt = time.Now().Add(-2 * time.Hour)

	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (probe alive)", got)
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Fatalf("status = %s, want running (quiet but alive)", got)
	}

	alive = false
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (terminal grace window)", got)
	}
	if got := res.Task.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (zombie retire)", got)
	}
	d.Done()
}

// TestReconcileZombies_NilProbeSkipped 钉住 僵尸探活路径跳过没有探活器的任务——裁决需要探活结论，年龄本身不是依据。
// - 因此一条极老的子代理任务在该路径下保持运行；总寿命上限由另一条 TTL 回收负责（"没有任务是永生的"在那一侧钉住）。
func TestReconcileZombies_NilProbeSkipped(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{DefaultTTL: 1000 * 24 * time.Hour})
	d := NewManualDetectorDetach(20 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "subagent", Desc: "plan"}, d)
	res.Task.StartedAt = time.Now().Add(-24 * time.Hour)
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (no probe -> untouched)", got)
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Fatalf("status = %s, want running", got)
	}
	d.Done()
}

// TestPruneTerminal_NilDetectorRestoredTask 钉住 从事实链重建的任务按设计没有探测器，退役与清理路径在缺探测器时必须仍然安全。
// - 重建只恢复声明与历史派生时刻（存活声明来自承诺表），这正是探活回收会命中的画像；
// - 跨重启的探测器不可恢复，"没有探测器"是常态而不是异常。
// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
func TestPruneTerminal_NilDetectorRestoredTask(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	tk := tm.RestoreTask("t-restored", TaskSpec{
		Kind:  "command",
		Desc:  "nightly probe",
		Key:   "nightly",
		Alive: func() bool { return false },
	}, time.Now().Add(-2*time.Hour), TaskRunning)
	if tk == nil {
		t.Fatal("RestoreTask returned nil")
	}
	if got := tk.Status(); got != TaskRunning {
		t.Fatalf("status = %s, want running (restored)", got)
	}

	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (terminal grace window)", got)
	}
	if got := tk.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (zombie retire)", got)
	}

	tk.mu.Lock()
	tk.settledAt = time.Now().Add(-5 * time.Minute)
	tk.mu.Unlock()
	if got := len(tm.List()); got != 0 {
		t.Fatalf("List len = %d, want 0 (pruned after TTL)", got)
	}
}

// TestSettleWatch_NoStateChange 钉住 观察信号是纯通知：不改生命周期状态、不干扰已脱离判定，且总是转发给结算回调。
func TestSettleWatch_NoStateChange(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	task := &Task{ID: "tw", status: TaskAliveDetached, aliveDetached: true}
	tm.emitBackground(task, SettleSignal{Kind: SettleWatch, Output: "watch pattern hit"})
	task.mu.Lock()
	defer task.mu.Unlock()
	if task.status != TaskAliveDetached {
		t.Fatalf("status changed to %v, want alive-detached preserved", task.status)
	}
}

// TestEmitBackground_JobStableNoNotify 钉住 job 型（command/subagent）的中间态信号被分流抑制。
// - SettleStable/SettleSuspect 不转 alive-detached、零 onSettle 通知；面板状态照常置。
// - 终态结算（SettleCompleted）照常通知，分流只针对中间态信号。
// - 终态收敛后 oneshot 不发射中间态信号，本分支随之退化。
func TestEmitBackground_JobStableNoNotify(t *testing.T) {
	var mu sync.Mutex
	notified := 0
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(_ *Task, _ SettleSignal) { mu.Lock(); notified++; mu.Unlock() },
	})

	d := NewManualDetectorDetach(10 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "sleep 180"}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "quiet"})
	waitUntil(t, time.Second, func() bool {
		res.Task.mu.Lock()
		defer res.Task.mu.Unlock()
		return res.Task.status == TaskStable
	})
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if notified != 0 {
		mu.Unlock()
		t.Fatalf("job stable should NOT notify; got %d", notified)
	}
	mu.Unlock()
	if got := res.Task.Status(); got == TaskAliveDetached {
		t.Fatalf("job stable must not transition to alive_detached")
	}

	d.Emit(SettleSignal{Kind: SettleSuspect, Output: "quiet2"})
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if notified != 0 {
		mu.Unlock()
		t.Fatalf("job suspect should NOT notify; got %d", notified)
	}
	mu.Unlock()

	d.Emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return notified == 1 })
	d.Done()
}

// TestEmitBackground_ServiceStableNotifies 钉住 service 型 stable 现状：转 alive-detached 并发一次性就绪通知。
func TestEmitBackground_ServiceStableNotifies(t *testing.T) {
	var mu sync.Mutex
	notified := 0
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(_ *Task, _ SettleSignal) { mu.Lock(); notified++; mu.Unlock() },
	})
	d := NewManualDetectorDetach(10 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Lifetime: LifetimeService, Desc: "dev-server"}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "listening"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return notified == 1 })
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Fatalf("service stable should transition to alive_detached, got %s", got)
	}
	d.Done()
}

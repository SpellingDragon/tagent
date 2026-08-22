package task

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestTaskManager_SettleWithinWindow_Inline: settle before the sync-wait window
// elapses → Spawn returns inline with the signal, task Completed.
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

// TestTaskManager_SettleAfterWindow_BackgroundAck: no settle within window →
// Spawn returns ack (Settled=false); later settle fires OnSettle exactly once.
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

	// Background settle arrives after the window.
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

// TestTaskManager_IdempotentSpawn_Dedup: a second Spawn with the same Key while
// the first is active returns Deduped and cancels the duplicate detector.
func TestTaskManager_IdempotentSpawn_Dedup(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d1 := NewManualDetectorDetach(300 * time.Millisecond) // detaches (acks) after the window
	key := "same-key"

	var res1 SpawnResult
	done := make(chan struct{})
	go func() {
		res1 = tm.Spawn(TaskSpec{Kind: "command", Desc: "a", Key: key}, d1)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // ensure task1 registered

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

// TestTaskManager_WindowBoundary_NoLostSettle: hammer the window boundary; every
// settle must be accounted for exactly once (inline XOR background), never lost.
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
			time.Sleep(2 * time.Millisecond) // right around the window boundary
			d.Emit(SettleSignal{Kind: SettleCompleted, Output: "x"})
			d.Done()
		}()
		res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "boundary"}, d)
		tasks = append(tasks, res.Task)
		if res.Settled {
			inline++
		}
	}

	// Every task must eventually reach Completed (applyStatus ran).
	waitUntil(t, 3*time.Second, func() bool {
		for _, tk := range tasks {
			if tk.Status() != TaskCompleted {
				return false
			}
		}
		return true
	})

	// Accounting: inline + background == n, and no background double-count.
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
	d := NewManualDetectorDetach(40 * time.Millisecond) // detaches (acks) after the window
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

// TestFuncSettleDetector_Generic verifies the generic goroutine detector settles
// Completed on return (with error captured).
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

// ============================================================================
// Exited-task pruning + resource reclaim (async-result-delivery)
// ============================================================================

// terminalTask builds a task already in a terminal state with the given
// settledAt, wired to a spy detector so prune-time resource reclaim is testable.
func terminalTask(id string, status TaskStatus, settledAt time.Time, d *ManualDetector) *Task {
	return &Task{ID: id, Spec: TaskSpec{Desc: id}, status: status, settledAt: settledAt, detector: d}
}

// TestPruneTerminal_RemovesExitedAndReclaims: an exited task past its grace TTL
// is removed AND its detector.Cancel() is invoked (resource reclaim), while a
// live task is kept and never cancelled.
func TestPruneTerminal_RemovesExitedAndReclaims(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()
	dc := NewManualDetector() // completed victim
	dl := NewManualDetector() // live, keep
	tm.tasks["c1"] = terminalTask("c1", TaskCompleted, base, dc)
	tm.tasks["r1"] = &Task{ID: "r1", Spec: TaskSpec{Desc: "r1"}, status: TaskRunning, detector: dl}

	tm.now = func() time.Time { return base.Add(2 * time.Minute) } // past grace
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

// TestPruneTerminal_KeepsAliveDetached: alive_detached (a living service) is
// never pruned by TTL regardless of elapsed time — only cancel/death ends it.
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

// TestPruneTerminal_KeepsWithinGrace: an exited task still within its grace TTL
// is retained so the resume_task window stays reachable.
func TestPruneTerminal_KeepsWithinGrace(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()
	d := NewManualDetector()
	tm.tasks["c1"] = terminalTask("c1", TaskCompleted, base, d)

	tm.now = func() time.Time { return base.Add(10 * time.Second) } // within grace
	tm.pruneTerminal()

	if _, ok := tm.Get("c1"); !ok {
		t.Error("exited task within grace must be retained (get_task_result window)")
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

// TestPruneTerminal_Idempotent: pruning twice is safe (victim already gone; the
// detector Cancel is idempotent).
func TestPruneTerminal_Idempotent(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Nanosecond})
	base := time.Now()
	d := NewManualDetector()
	tm.tasks["c1"] = terminalTask("c1", TaskCompleted, base, d)

	tm.now = func() time.Time { return base.Add(time.Hour) }
	tm.pruneTerminal()
	tm.pruneTerminal() // must not panic
	if _, ok := tm.Get("c1"); ok {
		t.Error("task should remain pruned")
	}
}

// ============================================================================
// spawn-time origin baggage capture (async-result-delivery)
// ============================================================================

// TestOriginSpawner_StampsBaggage: the wrapper stamps its origin baggage onto a
// spawned task whose spec had no Origin — tools stay oblivious (裸调 Spawn).
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

// TestOriginSpawner_CopyIsolation: the stamped Origin is a copy — mutating it
// does not affect the wrapper's source snapshot (baggage is decoupled).
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

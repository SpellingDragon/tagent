package task

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAliveDetached_ReadyOnceThenSuppress: a service task's first stable settle
// fires exactly one "ready" notification and transitions to alive-detached;
// subsequent stable signals (output changes) are suppressed (no reclaim spam).
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
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "server :8080"}, d)
	if res.Settled {
		t.Fatalf("expected ack (background) for a service task")
	}

	// First stable → one ready notice + alive-detached.
	d.Emit(SettleSignal{Kind: SettleStable, Output: "listening on :8080"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(settles) == 1 })
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Errorf("status = %s, want alive_detached", got)
	}

	// Subsequent stable signals (output changes) must be suppressed.
	d.Emit(SettleSignal{Kind: SettleStable, Output: "log line 1"})
	d.Emit(SettleSignal{Kind: SettleStable, Output: "log line 2"})
	time.Sleep(100 * time.Millisecond) // let watch process
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

// TestAliveDetached_SuspectSuppressed: a detached service going quiet (suspect)
// does not re-notify.
func TestAliveDetached_SuspectSuppressed(t *testing.T) {
	var mu sync.Mutex
	count := 0
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(_ *Task, _ SettleSignal) { mu.Lock(); count++; mu.Unlock() },
	})
	d := NewManualDetectorDetach(30 * time.Millisecond)
	tm.Spawn(TaskSpec{Kind: "command", Desc: "svc"}, d)

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

// TestAliveDetached_CompletionEndsAndNotifies: after alive-detached, a completed
// signal (process death) re-notifies and ends the task.
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
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "svc"}, d)

	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(settles) == 1 })

	// Process dies → completed settle must re-notify and end the task.
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
	board := RenderBoard([]*Task{task})
	if !strings.Contains(board, "server :8080") || !strings.Contains(board, "alive_detached") {
		t.Errorf("alive-detached task should appear on board:\n%s", board)
	}
}

package task

import (
	"sync"
	"testing"
	"time"
)

// Stale-detached wall tests (2026-09-16 af4aa4c7): a job-kind alive_detached
// task hanging forever on a dead stdout pipe keeps probe() true with no
// reclaim path — the wall retires it as failed once detached beyond
// MaxDetachedAge. generic-kind tasks are exempt; a negative configured value
// disables the wall.

// wallFixture spawns a command-kind task with the given wall and manual
// detector, settles it stable (→ alive_detached), and returns the manager
// (with the injectable clock), the task, and a settle recorder.
func wallFixture(t *testing.T, wall time.Duration) (*TaskManager, *Task, *[]SettleSignal, *ManualDetector, func(time.Duration)) {
	t.Helper()
	var mu sync.Mutex
	var settles []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		MaxDetachedAge: wall,
		OnSettle: func(_ *Task, sig SettleSignal) {
			mu.Lock()
			settles = append(settles, sig)
			mu.Unlock()
		},
	})
	base := time.Now()
	tm.now = func() time.Time { return base }
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "job", Alive: func() bool { return true }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })
	shift := func(by time.Duration) {
		tm.now = func() time.Time { return base.Add(by) }
	}
	return tm, res.Task, &settles, d, shift
}

func TestDetachedWall_CommandKindExpiredRetired(t *testing.T) {
	tm, tk, settles, _, shift := wallFixture(t, time.Hour)
	shift(2 * time.Hour) // detached 2h ago, wall 1h

	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (terminal grace window)", got)
	}
	if got := tk.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (stale-detached wall)", got)
	}
	if got := tk.Result(); got != "(stale-detached: job-kind task detached beyond the wall - auto-retired; likely dead-pipe zombie)" {
		t.Fatalf("result = %q, want wall retirement note", got)
	}
	if got := len(*settles); got != 2 {
		t.Fatalf("settle notifications = %d, want 2 (ready notice + wall retirement)", got)
	}
}

func TestDetachedWall_NotYetExpiredKept(t *testing.T) {
	tm, tk, _, _, shift := wallFixture(t, time.Hour)
	shift(30 * time.Minute) // detached 30m ago, wall 1h — inside the wall

	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1", got)
	}
	if got := tk.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached (inside wall)", got)
	}
}

func TestDetachedWall_GenericKindExempt(t *testing.T) {
	var mu sync.Mutex
	var settles []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		MaxDetachedAge: time.Hour,
		OnSettle: func(_ *Task, sig SettleSignal) {
			mu.Lock()
			settles = append(settles, sig)
			mu.Unlock()
		},
	})
	base := time.Now()
	tm.now = func() time.Time { return base }
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "generic", Desc: "display", Alive: func() bool { return true }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })

	tm.now = func() time.Time { return base.Add(24 * time.Hour) } // way past the wall
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1", got)
	}
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached (generic exempt)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := len(settles); got != 1 { // only the original ready notice
		t.Fatalf("settle notifications = %d, want 1", got)
	}
}

func TestDetachedWall_DisabledByNegativeConfig(t *testing.T) {
	tm, tk, _, _, shift := wallFixture(t, -time.Second) // negative disables
	shift(48 * time.Hour)

	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1", got)
	}
	if got := tk.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached (wall disabled)", got)
	}
}

func TestDetachedWall_ProbeDeadStillCompletes(t *testing.T) {
	// The wall must not disturb the existing probe-dead completion path.
	alive := true
	var mu sync.Mutex
	var settles []SettleSignal
	tm := NewTaskManager(TaskManagerConfig{
		MaxDetachedAge: time.Hour,
		OnSettle: func(_ *Task, sig SettleSignal) {
			mu.Lock()
			settles = append(settles, sig)
			mu.Unlock()
		},
	})
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "svc", Alive: func() bool { return alive }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })

	alive = false // probe dead, well inside the wall
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (terminal grace window)", got)
	}
	if got := res.Task.Status(); got != TaskCompleted {
		t.Fatalf("status = %s, want completed (probe-dead path unchanged)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := len(settles); got != 2 { // ready + completion
		t.Fatalf("settle notifications = %d, want 2", got)
	}
}

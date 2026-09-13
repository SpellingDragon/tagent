package task

import (
	"sync"
	"testing"
	"time"
)

// TestReconcileDetached_GoneSessionRetired: an alive_detached task whose
// liveness probe reports the backing session gone is retired through normal
// completion semantics - exactly one final onSettle notification, then TTL
// pruning applies. Repeated List() must not re-notify.
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
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "svc", Alive: func() bool { return alive }}, d)
	d.Emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskAliveDetached })

	// Probe says alive -> reconcile keeps the entry untouched.
	alive = true
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (probe alive)", got)
	}
	if got := res.Task.Status(); got != TaskAliveDetached {
		t.Fatalf("status = %s, want alive_detached", got)
	}

	// Session dies out-of-band -> next List() retires it via completion path.
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
	if n != 2 { // ready + final completed
		t.Fatalf("settles = %d, want 2 (ready + completed)", n)
	}
	if last.Kind != SettleCompleted {
		t.Fatalf("last settle kind = %s, want completed", last.Kind)
	}

	// Idempotent: repeated List() does not re-notify.
	_ = tm.List()
	mu.Lock()
	n2 := len(settles)
	mu.Unlock()
	if n2 != 2 {
		t.Fatalf("settles after re-List = %d, want 2 (no re-notify)", n2)
	}
	d.Done()
}

// TestReconcileDetached_NilProbeSkipped: tasks without a probe (subagent
// tasks) are never reconciled away.
func TestReconcileDetached_NilProbeSkipped(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewManualDetectorDetach(30 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "subagent", Desc: "plan"}, d)
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

package task

import (
	"testing"
	"time"
)

// TestReconcileZombies_RunningDeadSessionRetired: a running task whose
// backing session is provably gone and whose age exceeds the grace period is
// retired through the terminal failed path — the blind spot that left frozen
// probes on the board as [running] for hours.
func TestReconcileZombies_RunningDeadSessionRetired(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d := NewManualDetectorDetach(20 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "oneshot", Desc: "frozen probe",
		Alive: func() bool { return false }}, d)
	waitUntil(t, time.Second, func() bool { return res.Task.Status() == TaskRunning })

	// Young task: grace protects the spawn window even with a dead probe.
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (grace)", got)
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Fatalf("status = %s, want running (grace)", got)
	}

	// Past the grace: next List() retires it as failed.
	res.Task.StartedAt = time.Now().Add(-11 * time.Minute)
	if got := len(tm.List()); got != 1 {
		t.Fatalf("List len = %d, want 1 (terminal grace window)", got)
	}
	if got := res.Task.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (zombie retire)", got)
	}

	// Idempotent: repeated List() keeps it terminal.
	_ = tm.List()
	if got := res.Task.Status(); got != TaskFailed {
		t.Fatalf("status = %s, want failed (stable terminal)", got)
	}
	d.Done()
}

// TestReconcileZombies_AliveProbeProtectsQuietRunner: a quiet long-runner
// with a live backing session is never retired, regardless of age; flip the
// probe and it retires on the next sweep.
func TestReconcileZombies_AliveProbeProtectsQuietRunner(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{ZombieGrace: time.Minute})
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

// TestReconcileZombies_NilProbeSkipped: subagent tasks (no Alive probe) are
// never reconciled away, no matter how old.
func TestReconcileZombies_NilProbeSkipped(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
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

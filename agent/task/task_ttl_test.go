package task

import (
	"testing"
	"time"
)

// TestTTLReentrantRenewal covers async-task-lifetime 10.4: a write-type reentry
// resets the reaper anchor so the task's absolute lifetime is measured from the
// refresh, not the original spawn. op=send drives RenewTTLBySession at the action
// layer; resume drives it inside Resume. op=peek (read-only) never renews —
// exercised here by the "unrenewed task is measured from spawn" contrast.
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
		// Without a renewal this 2h-old task (1h TTL) would already be reaped;
		// the refresh moved the anchor to now, so it survives this pass.
		_ = len(tm.List())
		if st := tk.Status(); st != TaskSuspect {
			t.Fatalf("renewed task must survive its first pass (anchor moved to now), got %s", st)
		}
		// 90m after the refresh → now past the 1h TTL → reaped from the new anchor.
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
		_ = len(tm.List()) // no reentry → 2h old >= 1h TTL → reaped
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

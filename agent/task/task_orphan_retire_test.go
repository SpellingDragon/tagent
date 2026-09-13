package task

import (
	"testing"
	"time"
)

func orphanSpec(key, sess string) TaskSpec {
	return TaskSpec{
		Kind: "subagent", Desc: "orphan", Key: key,
		Declarative: &Declarative{Kind: "subagent", TaskID: sess},
	}
}

// Channel 1: aged restored nil-probe suspect → failed (settle-once), then
// terminalTTL pruning reclaims it and releases byKey (re-spawn unblocked).
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

	tm.now = func() time.Time { return base } // age = 2h >= defaultOrphanGrace(30m)
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

	// TTL past → prune reclaims; byKey released → re-spawn NOT deduped.
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

// Guards: young / tracked / probe-carrying / no-Declarative / alive-detached
// are never touched.
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

// Channel 2: reconcileZombies re-runs the same criterion at runtime for
// orphans created after rebuild. No tracker wired → behavior unchanged.
func TestReconcileZombies_Channel2NilProbeOrphan(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{SessionTracker: func(string) bool { return false }})
	base := time.Now()
	tm.RestoreTask("old-orphan", orphanSpec("k2", "s9"), base.Add(-2*time.Hour), TaskSuspect)
	tm.now = func() time.Time { return base }
	tm.reconcileZombies()
	tk, ok := tm.Get("old-orphan")
	if !ok || tk.Status() != TaskFailed {
		t.Fatalf("channel 2 must retire the aged nil-probe orphan (ok=%v)", ok)
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

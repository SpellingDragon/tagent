package task

import (
	"fmt"
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

// TestBatchRetire_CollapsedNotification（resident-remaining-hardening 1.1）：
// 注册 OnBatchRetire 后，批量退役的 bus 侧通知折叠为一次回调（N 条 BatchRetired），
// 逐条 OnSettle 不再触发；未注册回调时保持逐条旧行为（既有测试锁定）。
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

// TestBatchRetire_NestedNoDoubleDelivery（嵌套安全）：reconcileZombies 内部调
// RetireOrphans——内层 begin 复用外层 collector，回调只发生一次且覆盖全部条目。
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
	outer := tm.beginBatchRetire() // simulate outer reconcileZombies scope
	inner := tm.beginBatchRetire() // nested RetireOrphans scope
	tk := tm.RestoreTask("pn", orphanSpec("kn", "sess-n"), time.Now().Add(-2*time.Hour), TaskSuspect)
	if tk == nil {
		t.Fatal("RestoreTask returned nil")
	}
	if n := tm.RetireOrphans(func(string) bool { return false }); n != 1 {
		t.Fatalf("retired = %d, want 1", n)
	}
	inner() // nested finish: must NOT deliver
	if len(deliveries) != 0 {
		t.Fatalf("nested finish must not deliver, got %d deliveries", len(deliveries))
	}
	outer() // outer finish: delivers once with the collected entry
	if len(deliveries) != 1 || len(deliveries[0]) != 1 {
		t.Fatalf("outer finish must deliver exactly once with 1 entry, got %+v", deliveries)
	}
}

package task

import (
	"testing"
	"time"
)

// TestPruneTerminal_NilDetectorDoesNotPanic is the regression guard for the
// 2026-09-13 production panic (task_manager.go:769 nil pointer dereference).
//
// Root cause: RestoreTask rebuilds tasks from the fact chain and NEVER wires a
// live detector (nothing to reclaim). Such a task reaches a terminal state via
// reconcileDetached / reconcileZombies, then ages past terminalTTL and lands in
// pruneTerminal's victim list — where the historical code called
// t.detector.Cancel() unguarded, panicking every List()/Spawn() (i.e. every
// model turn via injectLiveTaskBoard, and every tool call via ActionTool).
//
// The contract: a nil detector is legal (fact-chain-restored tasks have none);
// pruning must skip Cancel but still reclaim the entry.
func TestPruneTerminal_NilDetectorDoesNotPanic(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: time.Minute})
	base := time.Now()

	// A task as RestoreTask would build it: terminal + nil detector.
	restored := &Task{ID: "restored-nil-det", Spec: TaskSpec{Desc: "restored"}, status: TaskCompleted, settledAt: base}
	if restored.detector != nil {
		t.Fatalf("precondition: restored task must carry a nil detector")
	}
	tm.tasks[restored.ID] = restored

	// Also cover the spawn path (Spawn -> pruneTerminal) which panicked in prod.
	live := NewManualDetector()
	tm.tasks["live1"] = &Task{ID: "live1", Spec: TaskSpec{Desc: "live1"}, status: TaskRunning, detector: live}

	tm.now = func() time.Time { return base.Add(2 * time.Minute) } // past grace

	// Must not panic; the nil-detector victim is still reclaimed.
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

	// Spawn also invokes pruneTerminal on the front path — exercise it too.
	res := tm.Spawn(TaskSpec{Kind: "subagent", Desc: "after-prune"}, NewManualDetectorDetach(10*time.Millisecond))
	if res.Task == nil {
		t.Fatal("Spawn must still succeed after pruning a nil-detector victim")
	}
}

// TestPruneTerminal_RestoreTaskPathNilDetector drives the real RestoreTask
// constructor (the production entry point) rather than a hand-built struct, so
// the guard tracks the actual rebuild shape.
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
	tm.pruneTerminal() // would panic pre-fix

	if _, ok := tm.Get("fact-chain-1"); ok {
		t.Error("restored terminal task must be pruned")
	}
}

package action

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
)

// These are REAL-tmux tests for the async-result-delivery resource-reclaim path.
// They exercise actual tmux sessions (not the spy detector used by the agent
// unit tests), verifying that a task's underlying tmux session is genuinely
// reaped. They skip when tmux cannot create a session (e.g. a sandbox without a
// PTY: "fork failed: Device not configured"), so they run only where tmux works.

// mustTmuxSession creates a real tmux session or skips the test when the
// environment cannot fork one. Returns the executor and session id.
func mustTmuxSession(t *testing.T, command string) (*TmuxExecutor, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("real tmux test; skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available")
	}
	exec := NewTmuxExecutor()
	sess, err := exec.CreateSession(context.Background(), TmuxCreateOptions{Command: command})
	if err != nil {
		t.Skipf("cannot create tmux session (sandboxed / no PTY?): %v", err)
	}
	return exec, sess.ID
}

// TestTmuxSettleDetector_CancelReapsRealSession: Cancel() reaps the underlying
// tmux session, and is idempotent (a second Cancel does not error/panic). This
// is the reap primitive that prune relies on for session-resource reclaim.
func TestTmuxSettleDetector_CancelReapsRealSession(t *testing.T) {
	exec, id := mustTmuxSession(t, "sleep 30")
	defer func() { _ = exec.KillSession(id) }()

	detector := NewTmuxSettleDetector(id, func() { _ = exec.KillSession(id) })
	if !exec.SessionExists(id) {
		t.Fatal("session should exist before Cancel")
	}

	detector.Cancel()
	if exec.SessionExists(id) {
		t.Error("Cancel() must reap (kill) the tmux session")
	}
	detector.Cancel() // idempotent — reapOnce/closeOnce guard against double-kill
}

// TestTmuxSettleDetector_CompletedReapsRealSession: a completed session is
// auto-reaped on the terminal state transition (output already captured), so
// finished command sessions do not accumulate.
func TestTmuxSettleDetector_CompletedReapsRealSession(t *testing.T) {
	exec, id := mustTmuxSession(t, "sleep 30")
	defer func() { _ = exec.KillSession(id) }()

	detector := NewTmuxSettleDetector(id, func() { _ = exec.KillSession(id) })
	detector.OnStateChange(SessionCompleted, "done")

	if exec.SessionExists(id) {
		t.Error("a completed tmux session must be auto-reaped")
	}
}

// TestTaskManager_PruneReclaimsTmuxTask: end-to-end with a real tmux-backed task
// in the TaskManager — after the task completes and its grace elapses, prune
// removes the registry entry (memory reclaim) and the tmux session is gone. The
// detector.Cancel() invoked by prune is an idempotent safety net (the session
// was already reaped at completion).
func TestTaskManager_PruneReclaimsTmuxTask(t *testing.T) {
	exec, id := mustTmuxSession(t, "sleep 30")
	defer func() { _ = exec.KillSession(id) }()

	// Short dense phase so Spawn detaches (ack) quickly instead of blocking.
	detector := NewTmuxSettleDetector(id, func() { _ = exec.KillSession(id) }, 150*time.Millisecond)
	tm := agent.NewTaskManager(agent.TaskManagerConfig{TerminalTTL: 100 * time.Millisecond})
	res := tm.Spawn(agent.TaskSpec{Kind: "command", Desc: "sleep 30", Key: "reclaim-k1"}, detector)
	if res.Task == nil {
		t.Fatal("no task returned from Spawn")
	}
	taskID := res.Task.ID

	// Drive completion → settle (status=completed) + natural session reap.
	detector.OnStateChange(SessionCompleted, "done")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if tk, ok := tm.Get(taskID); ok && tk.Status() == agent.TaskCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tk, ok := tm.Get(taskID); !ok || tk.Status() != agent.TaskCompleted {
		t.Fatalf("task did not reach completed state")
	}
	if exec.SessionExists(id) {
		t.Error("completed tmux session should already be reaped")
	}

	// Past the grace TTL → List() triggers prune, freeing the registry entry.
	time.Sleep(150 * time.Millisecond)
	if got := tm.List(); len(got) != 0 {
		t.Errorf("prune should have removed the exited task, got %d", len(got))
	}
	if _, ok := tm.Get(taskID); ok {
		t.Error("prune should remove the exited task entry (memory reclaim)")
	}
	if exec.SessionExists(id) {
		t.Error("tmux session must not exist after completion + prune")
	}
}

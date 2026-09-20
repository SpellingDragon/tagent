package task

import (
	"testing"
	"time"
)

// TestTTLReaperIndependentOfQuietState locks async-task-lifetime 10.8: the
// quiet_timeout / fake-dead detector and the unified TTL reaper are ORTHOGONAL
// axes. quiet only feeds the SUSPECT status (settle.go: timed_out → SettleSuspect —
// flagged, never killed; see tool/action.TestQuietTimeout_SessionOverridePreventsKill
// for the "large quiet does not falsely reclaim a quiet build" leg). Total-lifetime
// reclaim is the TTL reaper's SOLE job, keyed purely on anchor age vs TTL — it does
// not read any quiet/silence state, and quiet never extends or short-circuits it.
func TestTTLReaperIndependentOfQuietState(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	base := time.Now()
	tm.now = func() time.Time { return base }

	// A fake-dead-flagged (suspect) task 1h into a 4h TTL, backing session still
	// alive. Silence raised no alarm here — it is simply within its lifetime.
	tk := tm.RestoreTask("quiet-but-young", TaskSpec{
		Kind:  "command",
		Desc:  "long compile",
		TTL:   4 * time.Hour,
		Alive: func() bool { return true }, // still running (quiet, not dead)
	}, base.Add(-1*time.Hour), TaskSuspect)

	_ = tm.List()
	if st := tk.Status(); st != TaskSuspect {
		t.Fatalf("a suspect task only 1h into a 4h TTL must stay — quiet/suspect alone never reclaims, got %s", st)
	}

	// Advance past the 4h TTL. The task is STILL probe-alive and never went
	// "more quiet" — only its absolute age crossed the line. The reaper fires
	// because of age, entirely independent of the quiet/silence axis.
	tm.now = func() time.Time { return base.Add(5 * time.Hour) }
	_ = tm.List()
	if st := tk.Status(); st != TaskFailed {
		t.Fatalf("at 5h past a 4h TTL the task must be reaped by TTL regardless of quiet state, got %s", st)
	}
}

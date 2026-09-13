package task

import (
	"testing"
	"time"
)

// TestResume_RestoredTaskNilWatchDone: a task rebuilt from the fact chain
// (RestoreTask) carries no detector/watchDone/firstSettle — cross-restart
// detector state is unrecoverable by design. Resume on such a task used to
// hit close(nil) on watchDone (task_manager.go, generation check treats
// "detector != nil" as a watch change). Fail-before: MUST panic before the
// fix and pass after RestoreTask initializes lifecycle channels and the
// generation check becomes nil-aware.
func TestResume_RestoredTaskNilWatchDone(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	d2 := NewManualDetectorDetach(20 * time.Millisecond)
	defer d2.Done()
	if got := tm.RestoreTask("t-restored", TaskSpec{
		Kind: "service",
		Desc: "restored nightly probe",
		ResumeFn: func(input string) (SettleDetector, error) {
			return d2, nil
		},
	}, time.Now(), TaskStable); got == nil {
		t.Fatal("RestoreTask returned nil")
	}
	res, err := tm.Resume("t-restored", "go on")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if res.Task == nil {
		t.Fatal("Resume returned no task")
	}
	if got := res.Task.Status(); got != TaskRunning {
		t.Fatalf("status = %s, want running (resume claims)", got)
	}
}

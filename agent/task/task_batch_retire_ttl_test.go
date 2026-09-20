package task

import (
	"fmt"
	"testing"
	"time"
)

// TestBatchRetire_TTLReaperWaveCollapsed locks the async-task-lifetime 10.7 fix:
// the unified TTL reaper is the NEWEST retirement source, and a mass expiry (e.g.
// many restored tasks past TTL right after a restart) MUST be delivered as ONE
// N→1 batch-retire summary (→ newBatchRetiredSummaryEvent → a single fold-eligible
// "[task settled]" external_input), NOT N individual settle notices that re-inflate
// the projection. Before the fix, reconcileDetached did not open the collector, so
// reconcileTTL fell through to per-task OnSettle (batch=0, perSettle=N); this
// asserts the reaper wave now collapses (batch=N, perSettle=0).
func TestBatchRetire_TTLReaperWaveCollapsed(t *testing.T) {
	var batch []BatchRetired
	perSettle := 0
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle:      func(_ *Task, _ SettleSignal) { perSettle++ },
		OnBatchRetire: func(b []BatchRetired) { batch = append(batch, b...) },
	})
	base := time.Now()
	tm.now = func() time.Time { return base }
	// Three tasks well past a 1m TTL, still probe-alive (so ONLY the TTL reaper
	// retires them — not the zombie/orphan/probe-gone paths).
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("ttl-%d", i)
		if tk := tm.RestoreTask(id, TaskSpec{
			Kind:        "command",
			Desc:        id,
			TTL:         time.Minute,
			Alive:       func() bool { return true },
			Declarative: &Declarative{Kind: "command", TaskID: "sess-" + id},
		}, base.Add(-2*time.Hour), TaskSuspect); tk == nil {
			t.Fatalf("RestoreTask %s returned nil", id)
		}
	}

	_ = tm.List() // drives reconcileDetached → reconcileTTL over the whole wave

	if len(batch) != 3 {
		t.Fatalf("TTL reaper wave must collapse into ONE OnBatchRetire of 3, got batch=%d perSettle=%d", len(batch), perSettle)
	}
	if perSettle != 0 {
		t.Fatalf("per-task OnSettle must be suppressed under batch mode, got %d", perSettle)
	}
	for _, r := range batch {
		if r.Task.Status() != TaskFailed {
			t.Fatalf("TTL-retired task %s status = %v, want failed", r.Task.ID, r.Task.Status())
		}
	}
}

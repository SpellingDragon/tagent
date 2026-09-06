package task

import (
	"testing"
)

// neverSettleDetector is a detector that never settles nor detaches — the
// Spawn returns at the sync-wait boundary without side effects.
type neverSettleDetector struct{}

func (neverSettleDetector) Settled() <-chan SettleSignal { ch := make(chan SettleSignal); return ch }
func (neverSettleDetector) Detached() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}                                   // detach immediately: spawn returns ack
func (neverSettleDetector) Cancel() {}

// TestSpawnGate (5.4, design-report-closeout): the disk-degradation spawn gate
// rejects NEW spawns with a readable reason while disabled by default (nil
// gate = zero behavior change). In-flight tasks are never gated.
func TestSpawnGate(t *testing.T) {
	t.Run("default nil gate does not block", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{})
		res := tm.Spawn(TaskSpec{Kind: "command", Desc: "echo hi", Key: "k1"}, neverSettleDetector{})
		if res.Blocked != "" {
			t.Fatalf("default config must not block: %q", res.Blocked)
		}
	})
	t.Run("gate reason blocks spawn", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{
			SpawnGate: func() string { return "disk degraded" },
		})
		res := tm.Spawn(TaskSpec{Kind: "command", Desc: "echo hi", Key: "k2"}, neverSettleDetector{})
		if res.Blocked == "" || res.Task != nil {
			t.Fatalf("expected blocked spawn, got %+v", res)
		}
	})
	t.Run("empty gate reason passes through", func(t *testing.T) {
		tm := NewTaskManager(TaskManagerConfig{
			SpawnGate: func() string { return "" },
		})
		res := tm.Spawn(TaskSpec{Kind: "command", Desc: "echo hi", Key: "k3"}, neverSettleDetector{})
		if res.Blocked != "" {
			t.Fatalf("empty reason must not block: %q", res.Blocked)
		}
	})
}

// cancelledDetector records Cancel calls (8.1 regression probe).
type cancelledDetector struct {
	neverSettleDetector
	cancelled bool
}

func (d *cancelledDetector) Cancel() { d.cancelled = true }

// TestSpawnGate_DedupWinsAndCancels (8.1/8.6, review §8): with the disk gate
// ACTIVE, (a) an in-flight task with the same Key still hits dedup (Deduped,
// NOT Blocked — "in-flight tasks are never gated" holds), and (b) a NEW key
// is Blocked AND its detector is cancelled inside Spawn (no orphan watcher).
// Fail-before: gate preceded dedup (same-key in-flight got Blocked) and no
// Cancel happened.
func TestSpawnGate_DedupWinsAndCancels(t *testing.T) {
	degraded := false
	tm := NewTaskManager(TaskManagerConfig{
		SpawnGate: func() string {
			if degraded {
				return "disk degraded"
			}
			return ""
		},
	})
	// Phase 1 (healthy): seed an in-flight task with Key "k".
	first := tm.Spawn(TaskSpec{Kind: "command", Desc: "x", Key: "k"}, neverSettleDetector{})
	if first.Task == nil {
		t.Fatal("seed spawn must pass while healthy")
	}
	// Phase 2 (degraded): same Key → dedup wins over the gate.
	degraded = true
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "x", Key: "k"}, neverSettleDetector{})
	if res.Blocked != "" || !res.Deduped {
		t.Fatalf("in-flight same-key must dedup, not block: %+v", res)
	}
	// New key while degraded → Blocked + detector cancelled.
	det := &cancelledDetector{}
	res2 := tm.Spawn(TaskSpec{Kind: "command", Desc: "y", Key: "new-key"}, det)
	if res2.Blocked == "" {
		t.Fatal("new spawn must be blocked while degraded")
	}
	if !det.cancelled {
		t.Fatal("8.1: gate branch must cancel the detector (no orphan watcher)")
	}
}

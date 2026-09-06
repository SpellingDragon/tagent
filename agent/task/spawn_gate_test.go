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

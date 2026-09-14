package agent

import (
	"context"
	"testing"

	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// countingRunner is a minimal runner.Runner that counts Close calls (the
// upstream interface documents Close as idempotent).
type countingRunner struct{ closed int }

func (f *countingRunner) Run(context.Context, string, string, model.Message, ...upagent.RunOption) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	close(ch)
	return ch, nil
}
func (f *countingRunner) Close() error { f.closed++; return nil }

// TestRetireRunner_ClosedOnlyAfterInFlightDrops (implementation-hardening
// 5.1): a swapped-out runner is NOT closed while a turn is in flight
// (drain-free semantics), is closed exactly once when the counter drops, and
// repeated sweeps are idempotent. The current runner is never touched.
func TestRetireRunner_ClosedOnlyAfterInFlightDrops(t *testing.T) {
	cm := &ContextManager{name: "recycle-test"}
	current := &countingRunner{}
	cm.runner = current
	old1, old2 := &countingRunner{}, &countingRunner{}

	// Simulate an in-flight turn, then two hot-swaps.
	cm.runnerInFlight.Store(1)
	if got := cm.SwapExecutor(&countingRunner{}); got != current {
		t.Fatal("SwapExecutor must return the previous runner")
	}
	cm.RetireRunner(old1)
	if old1.closed != 0 {
		t.Fatalf("old1 closed while in-flight (drain-free violated): %d", old1.closed)
	}

	// Turn ends → sweep closes retired runners exactly once.
	cm.runnerInFlight.Store(0)
	cm.sweepRetiredRunners()
	if old1.closed != 1 {
		t.Fatalf("old1 closed = %d, want 1 after quiescence", old1.closed)
	}
	cm.sweepRetiredRunners()
	if old1.closed != 1 {
		t.Fatalf("repeated sweep must be idempotent, closed = %d", old1.closed)
	}

	// Second generation retires independently; current runner untouched.
	cm.RetireRunner(old2)
	cm.sweepRetiredRunners()
	if old2.closed != 1 {
		t.Fatalf("old2 closed = %d, want 1", old2.closed)
	}
	if current.closed != 0 {
		t.Fatalf("current runner must never be swept, closed = %d", current.closed)
	}
}

// TestContextManager_Close_DrainsRetiredUnconditionally: Close is terminal —
// retired runners drain even if the in-flight counter is stuck above zero
// (no new turns will run after Close).
func TestContextManager_Close_DrainsRetiredUnconditionally(t *testing.T) {
	cm := &ContextManager{name: "recycle-close"}
	cm.runner = &countingRunner{}
	old := &countingRunner{}
	cm.runnerInFlight.Store(3) // stuck counter must not block terminal drain
	cm.RetireRunner(old)
	if err := cm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if old.closed != 1 {
		t.Fatalf("retired runner closed = %d, want 1 (unconditional terminal drain)", old.closed)
	}
}

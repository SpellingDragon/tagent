package agent

import (
	"context"
	"testing"
	"time"
)

// TestFuncSettleDetector_DetachAfterDenseDuration: when fn runs longer than the
// dense duration, the detector signals detach (→ async ack).
func TestFuncSettleDetector_DetachAfterDenseDuration(t *testing.T) {
	d := NewFuncSettleDetector(context.Background(), func(ctx context.Context) (string, error) {
		<-ctx.Done() // never returns on its own
		return "", ctx.Err()
	}, 30*time.Millisecond)

	select {
	case <-d.Detached():
		// detached at the dense→sparse boundary
	case <-time.After(2 * time.Second):
		t.Fatal("expected detach after dense duration")
	}
	d.Cancel()
}

// TestFuncSettleDetector_NoDetachWhenSettledFirst: when fn returns within the
// dense duration, settle wins and detach never fires (timer cancelled).
func TestFuncSettleDetector_NoDetachWhenSettledFirst(t *testing.T) {
	d := NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		return "quick", nil
	}, 300*time.Millisecond)

	select {
	case sig := <-d.Settled():
		if sig.Output != "quick" {
			t.Errorf("settle output = %q, want quick", sig.Output)
		}
	case <-time.After(time.Second):
		t.Fatal("expected settle")
	}

	select {
	case <-d.Detached():
		t.Error("detach must not fire when the task settled first")
	case <-time.After(200 * time.Millisecond):
		// good — no detach
	}
}

// TestDetachAfter_StopPreventsFire: DetachAfter does not close when stop fires
// before the duration elapses.
func TestDetachAfter_StopPreventsFire(t *testing.T) {
	stop := make(chan struct{})
	ch := DetachAfter(500*time.Millisecond, stop)
	close(stop) // stop immediately
	select {
	case <-ch:
		t.Error("DetachAfter should not fire after stop")
	case <-time.After(200 * time.Millisecond):
		// good
	}
}

// TestDetachAfter_FiresAfterDuration: DetachAfter closes after the duration when
// not stopped.
func TestDetachAfter_FiresAfterDuration(t *testing.T) {
	ch := DetachAfter(20*time.Millisecond, make(chan struct{}))
	select {
	case <-ch:
		// good
	case <-time.After(time.Second):
		t.Fatal("DetachAfter should fire after the duration")
	}
}

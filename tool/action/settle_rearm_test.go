package action

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
)

// TestTmuxSettleDetector_Rearm: the detector is session-bound; Rearm resets
// round state — a fresh detach window and a new output baseline — without
// replacing the detector (no rebinding, no stale-signal risk).
func TestTmuxSettleDetector_Rearm(t *testing.T) {
	d := NewTmuxSettleDetector("s1", nil, 30*time.Millisecond)

	// Round 1: detach fires after the dense window.
	select {
	case <-d.Detached():
	case <-time.After(2 * time.Second):
		t.Fatalf("round-1 detach must fire")
	}

	// Rearm: fresh detach window, baseline set.
	d.Rearm(2)
	select {
	case <-d.Detached():
		t.Fatalf("round-2 detach must NOT be closed immediately after Rearm")
	case <-time.After(5 * time.Millisecond):
	}

	// Round-2 output is trimmed to the baseline (increment view).
	d.OnStateChange(SessionStable, "old1\nold2\nnew1\nnew2")
	select {
	case sig := <-d.Settled():
		if sig.Kind != agent.SettleStable || sig.Output != "new1\nnew2" {
			t.Errorf("round-2 settle must carry the post-baseline increment, got %+v", sig)
		}
	case <-time.After(time.Second):
		t.Fatalf("round-2 settle not delivered")
	}

	// Round-2 detach eventually fires on the NEW window.
	select {
	case <-d.Detached():
	case <-time.After(2 * time.Second):
		t.Fatalf("round-2 detach must fire after the re-armed dense window")
	}
}

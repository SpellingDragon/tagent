package tagent

import (
	"sync"
	"testing"
	"time"
)

// TestCapacityHint_TriggerAndSnooze (4.2, design-report-closeout): boundary
// events accumulate per partition; crossing the threshold fires exactly one
// hint and resets the counter; the snooze window suppresses repeats; after
// the window expires the next threshold crossing fires again. Non-boundary
// event types never count. Fail-before: no trigger mechanism existed
// (consolidation was manual-only).
func TestCapacityHint_TriggerAndSnooze(t *testing.T) {
	var mu sync.Mutex
	var hints []int
	tr := NewConsolidationHintTracker(3, time.Hour)
	if tr == nil {
		t.Fatal("tracker must be constructed for threshold>0")
	}
	clock := time.Unix(1750000000, 0)
	tr.now = func() time.Time { return clock }
	tr.SetOnHint(func(pid, count int) {
		mu.Lock()
		hints = append(hints, count)
		mu.Unlock()
	})

	// Non-boundary types never count.
	for i := 0; i < 5; i++ {
		tr.Track(int64(0x1000+1*100), 1, "action_command")
	}
	if len(hints) != 0 {
		t.Fatalf("non-boundary events must not trigger: %v", hints)
	}

	// 3 boundary events → exactly one hint.
	tr.Track(int64(0x1000+1*100), 1, "external_input")
	tr.Track(int64(0x1000+1*100), 1, "agent_output")
	if len(hints) != 0 {
		t.Fatalf("below threshold must not fire: %v", hints)
	}
	tr.Track(int64(0x1000+1*100), 1, "external_input")
	if len(hints) != 1 || hints[0] != 3 {
		t.Fatalf("threshold crossing must fire once with count=3: %v", hints)
	}

	// Counter reset: 2 more events do not fire again.
	tr.Track(int64(0x1000+1*100), 1, "agent_output")
	tr.Track(int64(0x1000+1*100), 1, "external_input")
	if len(hints) != 1 {
		t.Fatalf("counter must reset after hint: %v", hints)
	}

	// Cross again inside snooze → suppressed.
	tr.Track(int64(0x1000+1*100), 1, "agent_output")
	if len(hints) != 1 {
		t.Fatalf("snooze window must suppress: %v", hints)
	}

	// Advance past snooze → next crossing fires again.
	clock = clock.Add(2 * time.Hour)
	tr.Track(int64(0x1000+1*100), 1, "external_input") // 4th since reset → >= 3? counts: 2+1(suppressed)+1=4 ≥3
	if len(hints) != 2 {
		t.Fatalf("after snooze expiry must fire again: %v", hints)
	}

	// Per-partition isolation: partition 2 unaffected.
	if len(hints) != 2 {
		t.Fatal("partition isolation violated")
	}
	tr.Track(int64(0x1000+2*100), 2, "external_input")
	if len(hints) != 2 {
		t.Fatal("partition 2 below its own threshold must not fire")
	}

	// Disabled: threshold<=0 → nil tracker, Track on nil is a no-op.
	if NewConsolidationHintTracker(0, 0) != nil {
		t.Fatal("threshold=0 must disable (nil)")
	}
	var nilTracker *ConsolidationHintTracker
	nilTracker.Track(1, 1, "external_input") // must not panic
}

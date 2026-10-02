package tagent

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCapacityHint_TriggerAndSnooze pins that boundary events accumulate per partition and crossing the threshold fires exactly one hint, resetting the counter.
// - The snooze window suppresses repeats while the count is kept; after the window expires the next threshold crossing fires again.
// - Non-boundary event types never count; threshold<=0 disables (nil tracker) and Track on a nil receiver must not panic.
// - No trigger mechanism existed (consolidation was manual-only).
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

	for i := 0; i < 5; i++ {
		tr.Track(int64(0x1000+1*100), 1, "action_command")
	}
	if len(hints) != 0 {
		t.Fatalf("non-boundary events must not trigger: %v", hints)
	}

	tr.Track(int64(0x1000+1*100), 1, "external_input")
	tr.Track(int64(0x1000+1*100), 1, "agent_output")
	if len(hints) != 0 {
		t.Fatalf("below threshold must not fire: %v", hints)
	}
	tr.Track(int64(0x1000+1*100), 1, "external_input")
	if len(hints) != 1 || hints[0] != 3 {
		t.Fatalf("threshold crossing must fire once with count=3: %v", hints)
	}

	tr.Track(int64(0x1000+1*100), 1, "agent_output")
	tr.Track(int64(0x1000+1*100), 1, "external_input")
	if len(hints) != 1 {
		t.Fatalf("counter must reset after hint: %v", hints)
	}

	tr.Track(int64(0x1000+1*100), 1, "agent_output")
	if len(hints) != 1 {
		t.Fatalf("snooze window must suppress: %v", hints)
	}

	clock = clock.Add(2 * time.Hour)
	tr.Track(int64(0x1000+1*100), 1, "external_input")
	if len(hints) != 2 {
		t.Fatalf("after snooze expiry must fire again: %v", hints)
	}

	if len(hints) != 2 {
		t.Fatal("partition isolation violated")
	}
	tr.Track(int64(0x1000+2*100), 2, "external_input")
	if len(hints) != 2 {
		t.Fatal("partition 2 below its own threshold must not fire")
	}

	if NewConsolidationHintTracker(0, 0) != nil {
		t.Fatal("threshold=0 must disable (nil)")
	}
	var nilTracker *ConsolidationHintTracker
	nilTracker.Track(1, 1, "external_input")
}

// TestMeditationDigest_IncludesCandidates pins that the tracker renders a consolidation-candidates section (count + recent hex keys) for the digest injection.
// - Empty when nothing accumulated; a nil tracker renders empty — zero behavior change.
// 契约: docs/wiki/memory/memory-architecture.md#curation
func TestMeditationDigest_IncludesCandidates(t *testing.T) {
	var nilTracker *ConsolidationHintTracker
	if nilTracker.CandidatesText(1) != "" {
		t.Fatal("nil tracker must render empty")
	}

	tr := NewConsolidationHintTracker(3, time.Hour)
	if got := tr.CandidatesText(1); got != "" {
		t.Fatalf("no accumulation must render empty, got %q", got)
	}
	k1 := int64(0x1201aa10000001)
	k2 := int64(0x1201aa10000002)
	tr.Track(k1, 1, "external_input")
	tr.Track(k2, 1, "agent_output")
	got := tr.CandidatesText(1)
	if !strings.Contains(got, "巩固候选") || !strings.Contains(got, "2 个边界事件") {
		t.Fatalf("candidates text missing count: %q", got)
	}
	if !strings.Contains(got, "1201aa10000001") || !strings.Contains(got, "1201aa10000002") {
		t.Fatalf("candidates text missing hex keys: %q", got)
	}
	if tr.CandidatesText(2) != "" {
		t.Fatal("partition isolation violated in candidates")
	}
	tr.Track(int64(0x1201aa10000003), 2, "action_command")
	if tr.CandidatesText(2) != "" {
		t.Fatal("non-boundary event must not enter candidates")
	}
}

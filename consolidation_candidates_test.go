package tagent

import (
	"strings"
	"testing"
	"time"
)

// TestMeditationDigest_IncludesCandidates (4.3, design-report-closeout): the
// tracker renders a consolidation-candidates section (count + recent hex keys)
// for the meditation digest injection; empty when nothing accumulated; nil
// tracker renders empty (zero behavior change).
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
	// Other partition: empty (isolation).
	if tr.CandidatesText(2) != "" {
		t.Fatal("partition isolation violated in candidates")
	}
	// Non-boundary events do not enter candidates.
	tr.Track(int64(0x1201aa10000003), 2, "action_command")
	if tr.CandidatesText(2) != "" {
		t.Fatal("non-boundary event must not enter candidates")
	}
}

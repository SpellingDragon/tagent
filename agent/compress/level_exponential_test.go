package compress

import "testing"

// TestDeterministicLevel_Exponential (rolling-summary-anchor D2): aging
// boundaries are exponential {k, 2k} (base 2), not linear {k, 2k, 3k}.
// With keepRecent=2: L0 age<2, L1 age<4, L2 age>=4 — and the ladder CAPS AT
// L2: L3 is budget-escalation-only, never age-reachable (single-dimension
// trigger: segment count must not archive segments).
func TestDeterministicLevel_Exponential(t *testing.T) {
	seg := &TaskSegment{IsComplete: true}
	lvl := func(age, keepRecent int) int {
		// age = totalSegs-1-segIdx; set segIdx=0, totalSegs=age+1.
		return deterministicLevel(seg, 0, age+1, keepRecent)
	}
	cases := []struct {
		age  int
		want int
	}{
		{0, 0}, {1, 0}, // L0: age < 2
		{2, 1}, {3, 1}, // L1: age < 4
		{4, 2}, {5, 2}, {6, 2}, {7, 2}, // L2: age >= 4 (linear would give L3 at 6,7)
		{8, 2}, {9, 2}, {100, 2}, // still L2: the base ladder never reaches L3
	}
	for _, c := range cases {
		if got := lvl(c.age, 2); got != c.want {
			t.Errorf("age=%d keepRecent=2: got L%d, want L%d", c.age, got, c.want)
		}
	}
	// In-progress segment is never compressed.
	if got := deterministicLevel(&TaskSegment{IsComplete: false}, 0, 100, 2); got != 0 {
		t.Errorf("in-progress segment must be L0, got L%d", got)
	}
}

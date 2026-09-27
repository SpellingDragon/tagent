package compress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHotSourcePullRotatesWithoutPush pins the §6.4 consumption-boundary pull
// contract (introduce-durable-workflow-engine S-E): while a hot source is
// installed, the compressor resolves the FULL numeric group at every
// consumption boundary (BudgetLine/Threshold/Compress), and rotating the source
// alone — no push call of any kind — takes effect at the next read. The old
// push model made the atomics the authority reachable only through
// ApplyHotParams; here the source is the authority and the construction values
// are the fallback.
func TestHotSourcePullRotatesWithoutPush(t *testing.T) {
	cur := HotNumbers{ThresholdPct: 0.8, MaxTokens: 10000, KeepRecent: 2}
	sc := NewSmartCompressor(WithMaxTokens(5000), WithTriggerBudget(4000), WithKeepRecentTasks(1))
	cc := NewContextCompressor(sc, nil, NewDefaultTokenCounter(), 5000, 0.8, 1,
		WithHotSource(func() HotNumbers { return cur }))

	// Source wins over the construction pair (5000×0.8 would be 4000).
	require.Equal(t, 8000, cc.BudgetLine())
	require.InDelta(t, 0.8, cc.Threshold(), 1e-9)
	require.Equal(t, 2, cc.KeepRecentValue())

	// Rotate the SOURCE only — no push. One read yields the whole group, so the
	// outer trigger line and the inner compression target can never disagree
	// (the torn window hardening 5.3 patched by re-pushing both sides dies here).
	cur = HotNumbers{ThresholdPct: 0.5, MaxTokens: 20000, KeepRecent: 7}
	require.Equal(t, 10000, cc.BudgetLine())
	require.InDelta(t, 0.5, cc.Threshold(), 1e-9)
	require.Equal(t, 7, cc.KeepRecentValue())
}

// TestHotSourcePartialFallsBackToConstruction guards the standalone/bare edge
// (S-C lesson: enumerate the no-source boundary): zero fields from the source
// fall back to the construction values instead of zeroing the budget line.
func TestHotSourcePartialFallsBackToConstruction(t *testing.T) {
	cc := NewContextCompressor(NewSmartCompressor(), nil, NewDefaultTokenCounter(), 6000, 0.5, 3,
		WithHotSource(func() HotNumbers { return HotNumbers{} }))
	require.Equal(t, 3000, cc.BudgetLine()) // 6000×0.5 from construction
	require.Equal(t, 3, cc.KeepRecentValue())
}

// TestHotSourceAbsentKeepsLegacyFallback is the no-source boundary: a directly
// built compressor (tests, standalone wiring) keeps reading its construction
// atomics — the pull contract must not require a source to exist.
func TestHotSourceAbsentKeepsLegacyFallback(t *testing.T) {
	cc := NewContextCompressor(NewSmartCompressor(), nil, NewDefaultTokenCounter(), 6000, 0.5, 3)
	require.Equal(t, 3000, cc.BudgetLine())
	require.InDelta(t, 0.5, cc.Threshold(), 1e-9)
}

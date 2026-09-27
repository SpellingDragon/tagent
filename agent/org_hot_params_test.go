package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// Tests for the full-hot-config numeric bundle (implementation-hardening →
// full-hot-config Phase 1, 2026-09-16): threshold/maxTokens/keepRecent on
// the resident compressor — applied WITHOUT any structural rebuild. The
// maxTokens leg is the C-defect fix (yaml window change reaching the
// resident cm).

// rotateHot installs a mutable §6.4 pull source on cm's compressor — the test
// stand-in for what production does by rotating the committed record
// (cm.ApplyOrgHotParams / the compressor push face are deleted). Nil-safe, so a
// compressor-less manager stays a guarded no-op.
func rotateHot(cm *ContextManager, cur *OrgHotParams) {
	if cm == nil || cm.contextCompressor == nil {
		return
	}
	cm.contextCompressor.SetHotSource(func() compress.HotNumbers {
		return compress.HotNumbers{
			ThresholdPct: cur.ThresholdPct,
			MaxTokens:    cur.MaxTokens,
			KeepRecent:   cur.KeepRecentTasks,
		}
	})
}

func TestApplyOrgHotParams_RoutesAndGuards(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, cc := rbFoldCM(store, proj, 2)

	cur := &OrgHotParams{ThresholdPct: 0.9}
	rotateHot(cm, cur)
	require.Equal(t, 0.9, cc.Threshold(), "threshold must resolve from the hot source")

	// A zero group means the source has no opinion → the CONSTRUCTION value
	// answers; the live threshold can never be clobbered to zero. (The retired
	// push kept the last written value instead — stickiness is gone on purpose,
	// because the record is now the single authority.)
	*cur = OrgHotParams{}
	require.Equal(t, 0.8, cc.Threshold(), "a zero reading falls back to construction, it must not clobber to zero")
}

func TestApplyOrgHotParams_BudgetLineMoves(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cmA := driveRealFold(t, store, proj, 2)
	ccA := cmA.contextCompressor
	cm := &ContextManager{partitionID: rbPid, memStore: store, projection: proj, contextCompressor: ccA}

	// Budget line = maxTokens × threshold, re-resolved at every Compress (the
	// C-defect site). Old line: 60 × 0.8 = 48.
	line := func() int { return ccA.BudgetLine() }
	require.Equal(t, 48, line(), "rbFoldCM construction line")

	rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: 16000, KeepRecentTasks: 5})

	require.Equal(t, 12800, line(), "source rotation must move the budget line to 16000×0.8")
}

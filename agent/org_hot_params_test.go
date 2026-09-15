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

func TestApplyOrgHotParams_RoutesAndGuards(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, cc := rbFoldCM(store, proj, 2)

	cm.ApplyOrgHotParams(OrgHotParams{ThresholdPct: 0.9})
	require.Equal(t, 0.9, cm.thresholdPct, "threshold must hot-apply")

	// Zero bundle keeps current values (guards against accidental resets).
	cm.ApplyOrgHotParams(OrgHotParams{})
	require.Equal(t, 0.9, cm.thresholdPct)
	_ = cc
}

func TestApplyOrgHotParams_BudgetLineMoves(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cmA := driveRealFold(t, store, proj, 2)
	ccA := cmA.contextCompressor
	cm := &ContextManager{partitionID: rbPid, memStore: store, projection: proj, contextCompressor: ccA}

	// Budget line = maxTokens × threshold, re-read at every Compress (the
	// C-defect site). Old line: 60 × 0.8 = 48.
	line := func() int { return ccA.BudgetLine() }
	require.Equal(t, 48, line(), "rbFoldCM construction line")

	cm.ApplyOrgHotParams(OrgHotParams{ThresholdPct: 0.8, MaxTokens: 16000, KeepRecentTasks: 5})

	require.Equal(t, 12800, line(), "hot apply must move the budget line to 16000×0.8")
}

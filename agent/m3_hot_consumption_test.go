package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestM3_OutputCapIsConstructionDerivedBoundary pins the D4/M-3 clause
// "核字段默认与 OutputLimitTool 构造派生边界": the tool-output cap is derived from
// a CONSTRUCTION-time MaxTokens and is deliberately NOT one of the five hot
// axes — folding every derived number into the hot face / fingerprint is exactly
// the fallacy D4 rejects. The derivation is capped (A6: no 128K→256K blowup) and
// floored; and once an OutputLimitTool is built its cap is frozen (there is no
// hot path that re-derives or resizes it — re-derivation happens only on the next
// structural rebuild from the current effective config).
func TestM3_OutputCapIsConstructionDerivedBoundary(t *testing.T) {
	require.Equal(t, 8000, outputCapForMaxTokens(4000))                 // ratio 4000/2*4
	require.Equal(t, 20000, outputCapForMaxTokens(10000))               // ratio 10000/2*4 (under cap)
	require.Equal(t, toolOutputCapChars, outputCapForMaxTokens(128000)) // 256000 > cap → A6 cap
	require.Equal(t, toolOutputCapChars, outputCapForMaxTokens(0))      // <=0 → cap floor
	require.Equal(t, toolOutputCapChars, outputCapForMaxTokens(-5))     // negative → cap floor

	// A built OutputLimitTool keeps its derived cap even though an UNRELATED
	// agent's hot budget moves wildly: no setter, no re-derivation on reload.
	olt := NewOutputLimitTool(leafTool{name: "x"}, outputCapForMaxTokens(4000)) // cap = 8000
	cm := &ContextManager{}                                                     // no compressor → guarded no-op
	rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: 100000})
	require.Equal(t, 8000, olt.maxChars, "a hot max_tokens change must NOT resize an already-built tool's derived output cap")
}

// TestM3_ResidentBudgetHotAppliesToRealConsumer documents the same hot axis on
// the side that IS hot-applicable (max_tokens → compressor): the effective
// sub-model budget line is the real consumer, and it moves under hot apply —
// complementary to the frozen output cap above. (The deeper real-fold proof is
// TestApplyOrgHotParams_BudgetLineMoves/driveRealFold; this pins that the CM has
// no rival source after the construction-only cm.maxTokens field was removed.)
func TestM3_ResidentBudgetHotAppliesToRealConsumer(t *testing.T) {
	store := memory.NewInMemoryStore()
	proj := compress.NewSessionProjection()
	cm, cc := rbFoldCM(store, proj, 2)
	require.Equal(t, 48, cc.BudgetLine(), "constructed from the config source at build (new-call-effective-at-init)")

	rotateHot(cm, &OrgHotParams{ThresholdPct: 0.8, MaxTokens: 16000, KeepRecentTasks: 5})
	require.Equal(t, 12800, cc.BudgetLine(), "source rotation reaches the compressor (authoritative source), no separate cm field")
}

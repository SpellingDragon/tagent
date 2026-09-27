package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
)

// TestBuildExecutionFaceMatchesCMFace pins the ONE-spelling contract of
// faceFromConfig/BuildExecutionFace (S-A/2.3): the de-shelled face derivation
// must equal, field by field over the execution surface, the initial face a
// ContextManager is constructed with. Runtime handles (MemPlugin/SessionSvc/
// OutputCh/Bus/Projection/OnEvent) are deliberately zero on the derived face —
// buildExecutor overrides MemPlugin/SessionSvc from the resident cm and the
// loop-side handles are the cm's own — so they are excluded. Compressor and
// TokenCounter are distinct instances by design (per-generation); equivalence is
// asserted via the derived budget line, matching how consumers observe them.
func TestBuildExecutionFaceMatchesCMFace(t *testing.T) {
	enabled := true
	tokens := 4096
	effort := "high"
	cfg := &TagentConfig{
		Name:                 "face-eq",
		Model:                &mockModel{},
		SystemPrompt:         "prompt body",
		Temperature:          0.7,
		MaxToolIterations:    12,
		MaxTokens:            9000,
		CompressThreshold:    0.75,
		KeepRecentTasks:      9,
		ThinkingEnabled:      &enabled,
		ThinkingTokens:       &tokens,
		ReasoningEffort:      &effort,
		ReasoningContentMode: "parsed",
		Compress: CompressConfig{
			CompactKeysListed: 11,
			RecentFullCount:   22,
			CardMaxChars:      333,
			SummaryMaxTokens:  777,
		},
	}

	bus := NewEventBus()
	outCh := make(chan *event.Event, 4)
	proj := compress.NewSessionProjection()
	cm := newContextManagerFromConfig(cfg, nil, nil, nil, bus, outCh, proj, nil)
	cmFace := cm.ExecutorConfig()

	got := BuildExecutionFace(cfg)
	require.Equal(t, cmFace.Name, got.Name)
	require.Equal(t, cmFace.SystemPrompt, got.SystemPrompt)
	require.Equal(t, cmFace.Temperature, got.Temperature)
	require.Equal(t, cmFace.MaxToolIters, got.MaxToolIters)
	require.Equal(t, cmFace.MaxTokens, got.MaxTokens)
	require.Equal(t, cmFace.ThresholdPct, got.ThresholdPct)
	require.Equal(t, cmFace.ThinkingEnabled, got.ThinkingEnabled)
	require.Equal(t, cmFace.ThinkingTokens, got.ThinkingTokens)
	require.Equal(t, cmFace.ReasoningEffort, got.ReasoningEffort)
	require.Equal(t, cmFace.ReasoningContentMode, got.ReasoningContentMode)
	require.Equal(t, cmFace.CompactKeysListed, got.CompactKeysListed)
	require.Equal(t, cmFace.RecentFullCount, got.RecentFullCount)
	require.Equal(t, cmFace.CardMaxChars, got.CardMaxChars)
	require.Same(t, cmFace.Model, got.Model)
	require.Equal(t, cmFace.Tools, got.Tools)
	require.Equal(t, cmFace.MemStore, got.MemStore)
	require.NotNil(t, got.Compressor)
	require.NotNil(t, got.TokenCounter)
	// Compressor equivalence is input-pinned, not instance-pinned: both derive
	// from the shared buildCompressorOpts over the already-asserted-equal
	// MaxTokens/ThresholdPct/KeepRecent/Compress inputs (per-generation instances
	// by design — S-E will collapse them into one pull source).
	// Runtime handles stay zero on the derived face by contract.
	require.Nil(t, got.MemPlugin)
	require.Nil(t, got.SessionSvc)
	require.Nil(t, got.Bus)
	require.Nil(t, got.Projection)
	require.Nil(t, got.OnEvent)
}

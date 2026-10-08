// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
package agent

import (
	"github.com/SpellingDragon/tagent/agent/compress"
)

// faceFromConfig maps a TagentConfig onto the publishable execution face
// (S-A/2.3). It replicates the face literal newContextManagerFromConfig builds
// a cm's initial execCfg from — the ONE spelling is pinned by
// TestBuildExecutionFaceMatchesCMFace (face_test.go), which compares this
// derivation against a real cm's ExecutorConfig field-by-field; keep the two in
// sync or fold them when newContextManagerFromConfig is next touched.
//
// Runtime handles (MemPlugin/SessionSvc/OutputCh/Bus/Projection/OnEvent) are
// left zero on purpose: buildExecutor ALWAYS overrides MemPlugin/SessionSvc
// from the resident cm, and the loop-side handles belong to the cm — the face
// carries only the execution surface (model/tools/prompt/thinking/compress
// numerics + MemStore).
func faceFromConfig(cfg *TagentConfig, compressor *compress.SmartCompressor) ContextManagerConfig {
	return ContextManagerConfig{
		Name:                  cfg.Name,
		Model:                 cfg.Model,
		Tools:                 cfg.Tools,
		SystemPrompt:          cfg.SystemPrompt,
		SystemPromptSource:    cfg.SystemPromptSource,
		Temperature:           cfg.Temperature,
		MaxToolIters:          cfg.MaxToolIterations,
		ThinkingEnabled:       cfg.ThinkingEnabled,
		ThinkingTokens:        cfg.ThinkingTokens,
		ReasoningEffort:       cfg.ReasoningEffort,
		ReasoningContentMode:  cfg.ReasoningContentMode,
		Compressor:            compressor,
		TokenCounter:          compress.NewDefaultTokenCounter(),
		MaxTokens:             cfg.MaxTokens,
		ThresholdPct:          cfg.CompressThreshold,
		CompactKeysListed:     cfg.Compress.CompactKeysListed,
		RecentFullCount:       cfg.Compress.RecentFullCount,
		CardMaxChars:          cfg.Compress.CardMaxChars,
		MemStore:              cfg.MemoryStore,
		SummaryTimeoutSeconds: cfg.SummaryTimeoutSeconds,
		CaptureEnabled:        cfg.CaptureEnabled,
	}
}

// BuildExecutionFace derives the publishable execution face from an assembled
// TagentConfig WITHOUT constructing a ContextManager or TagentAgent (S-A/2.3:
// the reload path de-shells existing-agent regeneration — D1「热更换代不复制
// agent 状态」). The compressor is built with exactly the construction-time
// options newContextManagerFromConfig uses (buildCompressorOpts + token
// counter), so a face assembled here is byte-equivalent to the face a discarded
// shell's ExecutorConfig() would have returned.
func BuildExecutionFace(cfg *TagentConfig) ContextManagerConfig {
	copts := buildCompressorOpts(cfg)
	copts = append(copts, compress.WithTokenCounter(compress.NewDefaultTokenCounter()))
	return faceFromConfig(cfg, compress.NewSmartCompressor(copts...))
}

package compress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// SmartCompressor performs deterministic context compression.
//
// Pipeline (skeleton model):
//  1. Segment messages into task turns bounded by agent_output.
//  2. Deterministic level per segment age (pure function).
//  3. Per-segment drop: L0 (keep) / L1 (drop tool) / L2 (skeleton only) /
//     L3 (multi-segment compaction — whole segment leaves the timeline).
//  4. Assemble chronologically; kept messages keep their event key prefixes.
//
// This is a "view transformation" — it modifies the messages sent to the LLM,
// but does NOT modify the Session or Projection.
type SmartCompressor struct {
	summaryModel    model.Model  // Optional: used for index-card condensation (condenseCardLines)
	KeepRecentTasks int          // Number of recent complete tasks to keep (default: 2)
	maxTokens       int          // Token budget for calculating batch size (default: DefaultMaxTokens)
	tokenCounter    TokenCounter // Token estimator (injected, not NewDefaultTokenCounter)

	// Summary parameters
	summaryMaxTokens int // output-token budget floor for summary calls (0 → DefaultSummaryMaxTokens)
}

// NewSmartCompressor creates a new SmartCompressor.
func NewSmartCompressor(opts ...SmartCompressorOption) *SmartCompressor {
	sc := &SmartCompressor{
		KeepRecentTasks: 2,
		tokenCounter:    NewDefaultTokenCounter(),
		maxTokens:       DefaultMaxTokens,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

// SmartCompressorOption configures SmartCompressor.
type SmartCompressorOption func(*SmartCompressor)

// WithSummaryModel sets the LLM model for index-card condensation.
func WithSummaryModel(m model.Model) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.summaryModel = m
	}
}

// WithKeepRecentTasks sets how many recent tasks to keep.
func WithKeepRecentTasks(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.KeepRecentTasks = n
	}
}

// WithMaxTokens sets the token budget used for batch size calculation.
func WithMaxTokens(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxTokens = n }
}

// WithSummaryMaxTokens sets the output-token budget floor for summary calls
// (0 → DefaultSummaryMaxTokens). Reasoning models spend part of max_tokens on
// their thinking chain; reserving enough output tokens keeps Content from
// coming back empty (which would degrade every segment).
func WithSummaryMaxTokens(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		if n > 0 {
			sc.summaryMaxTokens = n
		}
	}
}

func WithTokenCounter(tc TokenCounter) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.tokenCounter = tc }
}

// Compress implements budget-aware compression via the skeleton pipeline
// (task-skeleton-compression): agent_output-bounded segments, age-driven
// deterministic levels, tool>assistant drop order, L3 multi-segment
// compaction. Pure engineering — no per-segment LLM summarization.
func (sc *SmartCompressor) Compress(
	ctx context.Context,
	messages []model.Message,
) []model.Message {
	return sc.compressSkeleton(ctx, messages)
}

// deterministicLevel assigns a base compression level to a task segment by
// age (deterministic-compress-level spec). Pure function: no side effects, no
// LLM/store reads; age = totalSegs - 1 - segIdx (0 = newest). The old
// HasUserInput criterion is retired — segments are agent_output-bounded, so
// archival (L3) is genuinely reachable.
//
// The base ladder CAPS AT L2 (single-dimension-trigger alignment): L3 is
// budget-escalation-only. Segment age governs the cheap, low-loss aging
// bands (drop tool results, then thinking) — it must NOT archive segments on
// its own, otherwise "enough segments" becomes an implicit second trigger
// and lossy L3 folds (plus their LLM narrative calls) fire with budget to
// spare. keepRecent remains a post-compaction STATE constraint: the most
// recent k segments stay L0 on every path, including escalation.
func deterministicLevel(seg *TaskSegment, segIdx, totalSegs, keepRecent int) int {
	if seg == nil || !seg.IsComplete {
		return 0 // in-progress segment: pending input, never compressed
	}
	if keepRecent < 1 {
		keepRecent = 1
	}
	// Exponential age boundaries (rolling-summary-anchor D2): aging level L
	// covers age in [keepRecent·2^(L-1), keepRecent·2^L). Compared to the old
	// linear {k,2k,3k}, each level's span doubles, so segments dwell longer at
	// each level — aged renders change less frequently, which improves LLM
	// prefix-cache reuse. Base is fixed at 2. Above 2k everything is L2; L3
	// exists only on the budget-escalation path in compressSkeleton.
	age := totalSegs - 1 - segIdx
	switch {
	case age < keepRecent:
		return 0 // age < k·2^0
	case age < keepRecent*2:
		return 1 // age < k·2^1
	default:
		return 2 // age >= k·2^1: skeleton; NOT L3 — aging never archives
	}
}

// applySegmentLevel renders the messages a segment keeps at a level. Kept
// messages retain their original content — and thus the [evt_KEY|type]
// prefix — so buildRetainedRefs can track surviving refs. Drop order is
// tool > assistant: L1 drops action_command only, L2 drops thinking_plan
// too, L3 drops the whole segment (multi-segment compaction).
func applySegmentLevel(seg *TaskSegment, level int) []model.Message {
	switch level {
	case 0:
		return seg.Messages
	case 1:
		out := make([]model.Message, 0, len(seg.Messages))
		for i := range seg.Messages {
			msg := seg.Messages[i]
			if MessageEventType(&msg) == tagentevent.TypeActionCommand {
				continue
			}
			// Results are dropped, so declared calls must go too — a
			// dangling tool_call is illegal in native protocol form. Keep
			// the prose (with its event prefix); drop pure-call messages.
			if len(msg.ToolCalls) > 0 {
				if msg.Content == "" {
					continue
				}
				msg.ToolCalls = nil
			}
			out = append(out, msg)
		}
		return out
	case 2:
		out := make([]model.Message, 0, len(seg.Messages))
		for i := range seg.Messages {
			msg := seg.Messages[i]
			if !IsSkeletonMessage(&msg) {
				continue
			}
			msg.ToolCalls = nil
			out = append(out, msg)
		}
		return out
	default:
		// L3 multi-segment compaction: the whole segment leaves the timeline.
		// Its event keys never appear in the output, so buildRetainedRefs
		// folds the skeleton (external_input/agent_output cards) into the
		// rolling summary — the archival exit, zero LLM required.
		return nil
	}
}

func (sc *SmartCompressor) compressSkeleton(_ context.Context, messages []model.Message) []model.Message {
	startTime := time.Now()
	systemMsg, rest := SplitSystemMessage(messages)
	// Extract the rolling summary so it never rides inside segment 0 (which is
	// the first to be L3-dropped). It is re-prepended after compression, right
	// after the system message — a permanent, always-visible prefix (D1).
	rollingMsg, rest := splitRollingSummaryMessage(rest)
	segments := SegmentMessages(rest)

	keepRecent := sc.KeepRecentTasks
	if keepRecent < 1 {
		keepRecent = 1
	}
	beforeTokens := sc.tokenCounter.Estimate(messages)
	// Single-dimension trigger alignment: budget is the ONLY reason to modify
	// the view here. Segment count / age alone SHALL NOT trigger aging or
	// archival (the old `completeCount <= keepRecent` guard let many-segment
	// histories lossy-compress with budget to spare — an implicit second
	// trigger violating the single-dimension-trigger spec).
	if beforeTokens <= sc.maxTokens {
		return messages
	}

	// Base levels by segment age (pure function).
	levels := make([]int, len(segments))
	for i, seg := range segments {
		levels[i] = deterministicLevel(seg, i, len(segments), keepRecent)
	}

	// Precompute per-segment cost at every level (4n Estimate calls), so
	// budget escalation below is O(1) incremental per step instead of
	// re-estimating the whole timeline (O(n²) — code review M1).
	systemCost := 0
	if systemMsg != nil {
		systemCost = sc.tokenCounter.Estimate([]model.Message{*systemMsg})
	}
	cost := make([][4]int, len(segments))
	for i, seg := range segments {
		for lvl := 0; lvl <= 3; lvl++ {
			cost[i][lvl] = sc.tokenCounter.Estimate(applySegmentLevel(seg, lvl))
		}
	}
	total := systemCost
	// The protected rolling summary is always present in the output, so its
	// tokens count toward the budget-escalation target — otherwise an
	// escalated result could overshoot maxTokens by up to one rolling summary
	// (code-review Minor).
	if rollingMsg != nil {
		total += sc.tokenCounter.Estimate([]model.Message{*rollingMsg})
	}
	for i := range segments {
		total += cost[i][levels[i]]
	}

	// Budget escalation (D3 "预算仍不足"): press all old complete segments to
	// skeleton first, then compact whole segments oldest-first. Segments
	// within keepRecent and in-progress segments are never escalated.
	escalate := func(i, lvl int) {
		total -= cost[i][levels[i]] - cost[i][lvl]
		levels[i] = lvl
	}
	if total > sc.maxTokens {
		for i, seg := range segments {
			if age := len(segments) - 1 - i; seg.IsComplete && age >= keepRecent && levels[i] < 2 {
				escalate(i, 2)
			}
		}
	}
	if total > sc.maxTokens {
		for i, seg := range segments {
			if age := len(segments) - 1 - i; seg.IsComplete && age >= keepRecent && levels[i] < 3 {
				escalate(i, 3)
				if total <= sc.maxTokens {
					break
				}
			}
		}
	}

	// Chronological assembly: kept messages carry their original event key
	// prefixes so surviving refs stay trackable downstream.
	result := make([]model.Message, 0, len(messages))
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}
	if rollingMsg != nil {
		result = append(result, *rollingMsg)
	}
	changed := false
	levelCounts := map[int]int{0: 0, 1: 0, 2: 0, 3: 0}
	for i, seg := range segments {
		kept := applySegmentLevel(seg, levels[i])
		if len(kept) != len(seg.Messages) {
			changed = true
		}
		levelCounts[levels[i]]++
		result = append(result, kept...)
	}
	if !changed {
		return messages
	}

	afterTokens := sc.tokenCounter.Estimate(result)
	metrics := map[string]interface{}{
		"event":         "smart_compress",
		"mode":          "skeleton",
		"before_tokens": beforeTokens,
		"after_tokens":  afterTokens,
		"segments":      len(segments),
		"l0_keep":       levelCounts[0],
		"l1_drop_tool":  levelCounts[1],
		"l2_skeleton":   levelCounts[2],
		"l3_compact":    levelCounts[3],
		"duration_ms":   time.Since(startTime).Milliseconds(),
	}
	if metricsJSON, err := json.Marshal(metrics); err == nil {
		log.Infof("[SmartCompress] %s tokens=%d->%d (%+d) levels=L0:%d L1:%d L2:%d L3:%d",
			string(metricsJSON), beforeTokens, afterTokens, afterTokens-beforeTokens,
			levelCounts[0], levelCounts[1], levelCounts[2], levelCounts[3])
	}
	return result
}

// generatePlainSummary runs a single plain-prompt completion on the summary
// model (used by index-card condensation). Returns an error when no model is
// configured or the call fails — callers degrade engineering-side.
func (sc *SmartCompressor) generatePlainSummary(ctx context.Context, prompt string) (string, error) {
	if sc.summaryModel == nil {
		return "", fmt.Errorf("no summary model configured")
	}
	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个历史记录浓缩助手。严格遵循用户的硬性要求。"),
			model.NewUserMessage(prompt),
		},
	}
	// Same reasoning-model guard as the retired batch summarizer: reserve ample
	// output tokens so reasoning doesn't squeeze Content to empty.
	plainMaxOut := sc.effectiveSummaryMaxTokens()
	req.MaxTokens = &plainMaxOut
	respCh, err := sc.summaryModel.GenerateContent(ctx, req)
	if err != nil {
		return "", err
	}
	var result string
	var reasoning string
	for resp := range respCh {
		if resp.Error != nil {
			return "", fmt.Errorf("summary model error: %s", resp.Error.Message)
		}
		if len(resp.Choices) > 0 {
			result += resp.Choices[0].Message.Content
			reasoning += resp.Choices[0].Message.ReasoningContent
		}
	}
	// Reasoning-model fallback: use reasoning content when the model left
	// Content empty.
	if strings.TrimSpace(result) == "" && strings.TrimSpace(reasoning) != "" {
		result = reasoning
	}
	return strings.TrimSpace(result), nil
}

// splitSystemMessage separates the system message from the rest.
func SplitSystemMessage(messages []model.Message) (*model.Message, []model.Message) {
	if len(messages) == 0 {
		return nil, nil
	}
	if messages[0].Role == model.RoleSystem {
		return &messages[0], messages[1:]
	}
	return nil, messages
}

// splitRollingSummaryMessage extracts a LEADING context_compress (rolling
// summary) message so it is never compacted away by L3 — analogous to
// SplitSystemMessage. The rolling summary is the compressed-history anchor
// (cards + recall tickets); if it rode inside segment 0 it would be the
// FIRST thing L3-dropped (segment 0 is oldest → highest age → L3), making the
// model lose all awareness of older history exactly when the conversation is
// long enough to need it (rolling-summary-anchor D1). Splitting it out keeps
// it a permanent, always-visible prefix (right after the system message).
// Forgery is harmless: a real rolling summary ref carries a negative key in
// the projection; imitated text parses into nothing downstream.
func splitRollingSummaryMessage(messages []model.Message) (*model.Message, []model.Message) {
	if len(messages) == 0 {
		return nil, nil
	}
	if MessageEventType(&messages[0]) == tagentevent.TypeContextCompress {
		return &messages[0], messages[1:]
	}
	return nil, messages
}

// effectiveSummaryMaxTokens returns the output-token budget floor for summary
// calls (config override or package default).
func (sc *SmartCompressor) effectiveSummaryMaxTokens() int {
	if sc.summaryMaxTokens > 0 {
		return sc.summaryMaxTokens
	}
	return DefaultSummaryMaxTokens
}

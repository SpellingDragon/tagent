// 契约: docs/wiki/agent/compression-and-telemetry.md
package compress

import "time"

// DefaultMaxTokens Compression defaults (single source; the agent package re-exports aliases).
const (
	DefaultMaxTokens         = 8000
	DefaultCompressThreshold = 0.8

	// DefaultSummaryMaxTokens is the floor for the output-token budget reserved
	// on each summary LLM call. A reasoning model spends part of max_tokens on
	// its thinking chain; too small a budget leaves Content empty (mass
	// degradation). The per-call budget scales up with the summary size
	// (targetChars*2) but never below this floor.
	DefaultSummaryMaxTokens = 8192

	// DefaultCompactKeysListed caps the keys listed in the rolling
	// compaction summary; older events stay retrievable via recall.
	DefaultCompactKeysListed = 32
	// DefaultRefsPerTurn is the assumed refs per complete task turn
	// (external_input + thinking_plan + action_command + agent_output) used
	// to derive the recentFullCount default: keepRecent × DefaultRefsPerTurn,
	// so the most recent keepRecent turns resolve with full content as a
	// whole. An explicit WithRecentFullCount /
	// recent_full_count setting overrides the derived value.
	DefaultRefsPerTurn = 4
	// DefaultCardMaxChars caps the index-card section of the rolling summary;
	// beyond it old card lines are LLM-condensed (or sink, without a model).
	DefaultCardMaxChars = 6000

	// DefaultSummaryTimeout is the shared deadline for ONE real fold's
	// synchronous summary calls (index-card condensation + rolling narrative).
	// They share a single sub-context, so a stalled summary can never cost the
	// round twice its budget. config compress.summary_timeout_seconds overrides
	// it (0 → this default, negative → rejected by config validation).
	DefaultSummaryTimeout = 5 * time.Second
)

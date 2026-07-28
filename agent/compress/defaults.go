package compress

// Compression defaults (single source; the agent package re-exports aliases).
const (
	DefaultMaxTokens         = 8000
	DefaultCompressThreshold = 0.8

	// DefaultMaxSummaryInputChars is the splitting threshold for a single
	// summary call's input. A giant segment (e.g. a long sub-agent run with
	// many messages) whose content exceeds this is split into smaller
	// message-groups, each summarized separately then joined — keeping each
	// summary call's input within a reasonable range. This is a SPLITTING
	// threshold, not a truncation cap: a single oversized message is sent as-is
	// (splitting one message's content is meaningless and loses information).
	DefaultMaxSummaryInputChars = 40000

	// DefaultSummaryMaxTokens is the floor for the output-token budget reserved
	// on each summary LLM call. A reasoning model spends part of max_tokens on
	// its thinking chain; too small a budget leaves Content empty (mass
	// degradation). The per-call budget scales up with the summary size
	// (targetChars*2) but never below this floor.
	DefaultSummaryMaxTokens = 8192

	// DefaultArchiveCacheCap bounds the per-process L3 archive cache (the
	// archives themselves persist in MemoryStore).
	DefaultArchiveCacheCap = 256
	// DefaultCompactKeysListed caps the keys listed in the rolling
	// compaction summary; older events stay retrievable via recall.
	DefaultCompactKeysListed = 32
	// DefaultRefsPerTurn is the assumed refs per complete task turn
	// (external_input + thinking_plan + action_command + agent_output) used
	// to derive the recentFullCount default: keepRecent × DefaultRefsPerTurn,
	// so the most recent keepRecent turns resolve with full content as a
	// whole (task-skeleton-compression D6). An explicit WithRecentFullCount /
	// recent_full_count setting overrides the derived value.
	DefaultRefsPerTurn = 4
	// DefaultMaxNoticeChars caps the compress-notice text length.
	DefaultMaxNoticeChars = 800
	// DefaultCardMaxChars caps the index-card section of the rolling summary;
	// beyond it old card lines are LLM-condensed (or sink, without a model).
	DefaultCardMaxChars = 6000
	// Default compress parameters
	DefaultMaxExecStateChars  = 2000
	DefaultMaxToolResultChars = 500
	DefaultMaxToolArgsChars   = 80
	DefaultChunkSize          = 1000
	DefaultChunkSummaryLen    = 150
)

package compress

// Compression defaults (single source; the agent package re-exports aliases).
const (
	DefaultMaxTokens         = 8000
	DefaultCompressThreshold = 0.8
	// DefaultMaxSummaryInputChars caps a single summary LLM call's input.
	// Oversized task segments (e.g. a long sub-agent run with large file
	// round-trips) are split recursively before summarization — one giant
	// call is slow, failure-prone, and can exceed the summary model window.
	DefaultMaxSummaryInputChars = 40000

	// DefaultArchiveCacheCap bounds the per-process L3 archive cache (the
	// archives themselves persist in MemoryStore).
	DefaultArchiveCacheCap = 256
	// DefaultCompactKeysListed caps the keys listed in the rolling
	// compaction summary; older events stay retrievable via recall.
	DefaultCompactKeysListed = 32
	// DefaultRecentFullCount is how many most-recent refs resolve with full
	// content from MemoryStore.
	DefaultRecentFullCount = 4
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

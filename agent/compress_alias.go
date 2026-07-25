package agent

// Compression symbols moved to the agent/compress sub-package (the pure
// view-transform domain: SmartCompressor L0-L3 levels, the rolling
// card-sequence compactor, the session projection they operate on, and the
// token estimator). All in-repo consumers now import the sub-package
// directly; these aliases remain ONLY as an external-facing compatibility
// facade and for intra-package use.
// ContextManager stays in this package — it is the engine-side glue.

import (
	"github.com/SpellingDragon/tagent/agent/compress"
)

// Types.
type (
	SmartCompressor         = compress.SmartCompressor
	SmartCompressorOption   = compress.SmartCompressorOption
	ContextCompressor       = compress.ContextCompressor
	ContextCompressorOption = compress.ContextCompressorOption
	CompressResult          = compress.CompressResult
	SessionProjection       = compress.SessionProjection
	TaskSegment             = compress.TaskSegment
	TokenCounter            = compress.TokenCounter
	DefaultTokenCounter     = compress.DefaultTokenCounter
	EventInfo               = compress.EventInfo
)

// Constructors.
var (
	NewSmartCompressor     = compress.NewSmartCompressor
	NewContextCompressor   = compress.NewContextCompressor
	NewSessionProjection   = compress.NewSessionProjection
	NewDefaultTokenCounter = compress.NewDefaultTokenCounter
)

// SmartCompressor options.
var (
	WithSummaryModel       = compress.WithSummaryModel
	WithKeepRecentTasks    = compress.WithKeepRecentTasks
	WithMaxTokens          = compress.WithMaxTokens
	WithTokenCounter       = compress.WithTokenCounter
	WithMemStore           = compress.WithMemStore
	WithProjection         = compress.WithProjection
	WithMaxExecStateChars  = compress.WithMaxExecStateChars
	WithMaxToolResultChars = compress.WithMaxToolResultChars
	WithMaxNoticeChars     = compress.WithMaxNoticeChars
	WithArchiveCacheCap    = compress.WithArchiveCacheCap
	WithChunkSummaryLen    = compress.WithChunkSummaryLen
)

// ContextCompressor options.
var (
	WithCompactKeysListed = compress.WithCompactKeysListed
	WithRecentFullCount   = compress.WithRecentFullCount
	WithCardMaxChars      = compress.WithCardMaxChars
)

// Compression defaults (single source lives in agent/compress).
const (
	DefaultMaxTokens          = compress.DefaultMaxTokens
	DefaultCompressThreshold  = compress.DefaultCompressThreshold
	DefaultArchiveCacheCap    = compress.DefaultArchiveCacheCap
	DefaultCompactKeysListed  = compress.DefaultCompactKeysListed
	DefaultRecentFullCount    = compress.DefaultRecentFullCount
	DefaultMaxNoticeChars     = compress.DefaultMaxNoticeChars
	DefaultCardMaxChars       = compress.DefaultCardMaxChars
	DefaultMaxExecStateChars  = compress.DefaultMaxExecStateChars
	DefaultMaxToolResultChars = compress.DefaultMaxToolResultChars
	DefaultMaxToolArgsChars   = compress.DefaultMaxToolArgsChars
	DefaultChunkSize          = compress.DefaultChunkSize
	DefaultChunkSummaryLen    = compress.DefaultChunkSummaryLen
)

package memory

import (
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// EventReference is a lightweight reference to an event stored in MemoryStore.
// Session only keeps EventReference list, not full event details.
type EventReference struct {
	EventKey     int64  `json:"event_key"`              // Snowflake int64 EventKey
	PartitionID  int    `json:"partition_id,omitempty"` // Storage partition key
	EventType    string `json:"event_type"`             // Event type
	EventSummary string `json:"event_summary"`          // Brief summary of event result
	Timestamp    int64  `json:"timestamp"`              // Unix timestamp in milliseconds
}

// FullEvent represents a complete event with all details stored in MemoryStore.
// This is the single source of truth for event data.
type FullEvent struct {
	EventKey     int64                  `json:"event_key"`            // Snowflake int64 unique identifier
	PartitionID  int                    `json:"partition_id"`         // Storage partition key
	ParentKey    int64                  `json:"parent_key,omitempty"` // Causal chain: preceding event key
	EventType    string                 `json:"event_type"`           // Event type
	EventSummary string                 `json:"event_summary"`        // Brief summary (for LLM context)
	Timestamp    int64                  `json:"timestamp"`            // Unix timestamp (ms)
	Content      string                 `json:"content"`              // Event content/text
	ToolCalls    []model.ToolCall       `json:"tool_calls"`           // Tool calls (if any)
	ToolResults  map[string]interface{} `json:"tool_results"`         // Tool execution results
	Metadata     map[string]string      `json:"metadata"`             // Additional metadata

	// Response field for compatibility with Phase 2
	Response *model.Response `json:"response,omitempty"`
}

// MemoryStore is the interface for event storage and retrieval.
// It serves as the single source of truth for all event data.
//
// Storage isolation: MemoryStore uses PartitionID as the storage partition key.
// Memory does not know about agents — PartitionID is a pure storage concept.
// The mapping from agent identity → PartitionID happens outside MemoryStore
// (in MemoryPlugin), keeping Memory's storage semantics clean.
type MemoryStore interface {
	// === Write Operations ===

	// StoreEvent stores a single event with its full details.
	StoreEvent(key int64, event FullEvent) error

	// StoreEvents stores multiple events in batch.
	StoreEvents(events map[int64]FullEvent) error

	// === Read Operations ===

	// GetEvent retrieves a single event by its EventKey.
	GetEvent(key int64) (*FullEvent, error)

	// GetEvents retrieves multiple events by their EventKeys.
	// Returns events in the same order as keys. Skips missing keys.
	GetEvents(keys []int64) ([]FullEvent, error)

	// QueryEvents queries events based on filters.
	// Returns EventReference list (lightweight).
	QueryEvents(query QueryOptions) ([]EventReference, error)

	// === RAG Vector Search (Optional) ===

	// SearchByEmbedding performs semantic search using a query embedding.
	SearchByEmbedding(query []float32, topK int) ([]EventReference, error)

	// StoreEventWithEmbedding stores an event with its vector embedding.
	StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error

	// SupportsVectorSearch returns true if this store supports vector operations.
	SupportsVectorSearch() bool

	// === Management Operations ===

	// DeleteEvent permanently deletes an event from storage.
	DeleteEvent(key int64) error

	// GetStats returns storage statistics.
	GetStats() StoreStats
}

// Vector search errors.
var (
	ErrVectorSearchNotSupported = fmt.Errorf("vector search not supported")
)

// QueryOptions specifies filters for querying events.
type QueryOptions struct {
	// PartitionID filters events by storage partition.
	// 0 = no partition filter (query across all partitions).
	PartitionID int `json:"partition_id"`
	// PartitionIDs filters events across multiple partitions.
	// Takes precedence over PartitionID if non-empty.
	PartitionIDs []int    `json:"partition_ids"`
	EventTypes   []string `json:"event_types"`
	StartTime    int64    `json:"start_time"`
	EndTime      int64    `json:"end_time"`
	Limit        int      `json:"limit"`
	Offset       int      `json:"offset"`
	OrderBy      string   `json:"order_by"`
	// Keyword filters events whose EventSummary or Content contains the keyword (case-insensitive).
	// Empty string = no keyword filter.
	Keyword string `json:"keyword,omitempty"`
}

// StoreStats contains storage statistics.
type StoreStats struct {
	TotalEvents int    `json:"total_events"`
	StorageSize int64  `json:"storage_size"`
	DataDir     string `json:"data_dir"`
}

// Event type constants for event classification.
const (
	EventTypeExternalInput   = "external_input"
	EventTypeAgentOutput     = "agent_output"
	EventTypeActionCommand   = "action_command"
	EventTypeThinkingPlan    = "thinking_plan"
	EventTypeContextCompress = "context_compress"
)

// ==================== Snowflake EventKey ====================
//
// EventKey is a 64-bit integer following a Snowflake-like layout:
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ 63       53 │ 52            22 │ 21       12 │ 11             0 │
//	│  PartitionID│   Timestamp      │  Sequence   │   Reserved     │
//	│  (11 bits)  │   (31 bits)      │  (10 bits)  │   (12 bits)    │
//	└──────────────────────────────────────────────────────────────────┘
//
// PartitionID: storage partition (0-2047). Caller-derived, Memory does not interpret.
// Timestamp: seconds since snowflakeEpoch (~68 year range).
// Sequence: per-second counter (0-1023), sub-second uniqueness.
// Reserved: for future use (e.g., distributed worker ID).

const (
	partitionIDShift = 53
	timestampShift   = 22
	sequenceShift    = 12

	partitionIDMask = 0x7FF      // 11 bits
	timestampMask   = 0x7FFFFFFF // 31 bits
	sequenceMask    = 0x3FF      // 10 bits

	// snowflakeEpoch: 2024-01-01 00:00:00 UTC
	snowflakeEpoch = 1704067200
)

// snowflakeSeqMu protects per-partition sequence counters.
var snowflakeSeqMu sync.Mutex

// snowflakeSeqLast maps PartitionID → last timestamp (seconds).
var snowflakeSeqLast = make(map[int]int64)

// snowflakeSeqCnt maps PartitionID → sequence counter within current second.
var snowflakeSeqCnt = make(map[int]int)

// NewSnowflakeEventKey generates a Snowflake-style int64 EventKey.
// partitionID: storage partition (0-2047), provided by caller.
// nowMs: current time in milliseconds (0 = use time.Now).
func NewSnowflakeEventKey(partitionID int, nowMs int64) int64 {
	if nowMs <= 0 {
		nowMs = time.Now().UnixMilli()
	}
	ts := nowMs/1000 - snowflakeEpoch

	snowflakeSeqMu.Lock()
	if ts == snowflakeSeqLast[partitionID] {
		snowflakeSeqCnt[partitionID]++
	} else {
		snowflakeSeqCnt[partitionID] = 0
		snowflakeSeqLast[partitionID] = ts
	}
	seq := snowflakeSeqCnt[partitionID]
	snowflakeSeqMu.Unlock()

	return (int64(partitionID&partitionIDMask) << partitionIDShift) |
		((ts & timestampMask) << timestampShift) |
		(int64(seq&sequenceMask) << sequenceShift)
}

// PartitionIDFromEventKey extracts the PartitionID from a Snowflake EventKey.
func PartitionIDFromEventKey(key int64) int {
	return int((key >> partitionIDShift) & partitionIDMask)
}

// TimestampFromEventKey extracts the Unix timestamp (seconds) from a Snowflake EventKey.
func TimestampFromEventKey(key int64) int64 {
	return ((key >> timestampShift) & timestampMask) + snowflakeEpoch
}

// SequenceFromEventKey extracts the sequence number from a Snowflake EventKey.
func SequenceFromEventKey(key int64) int {
	return int((key >> sequenceShift) & sequenceMask)
}

// ==================== PartitionID from Name ====================
//
// PartitionIDFromName computes a stable PartitionID (0-2047) from a name string
// using FNV-1a hash. Deterministic: same name always yields same PartitionID.
// Collision is acceptable — partitioning is for causal chain isolation, not uniqueness.

var partitionIDCache sync.Map

// PartitionIDFromName computes a stable PartitionID from a name string.
func PartitionIDFromName(name string) int {
	if v, ok := partitionIDCache.Load(name); ok {
		return v.(int)
	}
	h := fnv.New32a()
	h.Write([]byte(name))
	id := int(h.Sum32() & uint32(partitionIDMask))
	partitionIDCache.Store(name, id)
	return id
}

// ==================== Global Atomic Counter ====================
//
// When no stable name is available, NewPartitionID generates a unique
// PartitionID using an atomic counter, ensuring process-level uniqueness.

var globalPartitionCounter atomic.Int64

// NewPartitionID generates a unique PartitionID using an atomic counter.
// Use when no stable name is available for PartitionIDFromName.
func NewPartitionID() int {
	seq := globalPartitionCounter.Add(1)
	return int((seq * 1337) & partitionIDMask)
}

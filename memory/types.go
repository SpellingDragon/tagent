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
	Role         string `json:"role,omitempty"`         // Original message role (user/assistant/tool/system)
}

// FullEvent represents a complete event with all details stored in MemoryStore.
// This is the single source of truth for event data.
//
// TIME CONTRACT (segment-query-recency D8) — two timestamps exist, with
// strictly separated roles:
//
//   - `Timestamp` (this struct) is the SINGLE semantic time axis: ordering,
//     time-range filtering, TTL age and card timelines read ONLY this. It is
//     the moment the event happened.
//   - The time embedded in `EventKey` (Snowflake, see NewSnowflakeEventKey) is
//     the moment the event was WRITTEN. It is used only to place the event in
//     a segment window and to break ties between same-millisecond events in
//     the total order — never for semantic time decisions.
//
// The two may diverge for asynchronous events (task_settled write-back,
// batched delivery). That divergence is harmless precisely because no decision
// depends on both: it only affects WHICH SEGMENT holds the event, and segment
// placement carries no semantics (see the segment-vs-time-unit note in
// docs/wiki/memory/memory-architecture.md §16.3).
//
// Note: ParentKey has been removed from this struct.
// Event causal relationships are now maintained by RelationStore,
// accessible via MemoryStore.RelationStore() method (if the store implements RelationStoreProvider).
// This separates immutable event content from mutable relationships.
type FullEvent struct {
	EventKey     int64  `json:"event_key"`     // Snowflake int64 unique identifier
	PartitionID  int    `json:"partition_id"`  // Storage partition key
	EventType    string `json:"event_type"`    // Event type
	EventSummary string `json:"event_summary"` // Brief summary (for LLM context)
	Timestamp    int64  `json:"timestamp"`     // Unix timestamp (ms)
	Content      string `json:"content"`       // Event content/text
	// ContentParts carries multimodal parts (image/file/audio) for an input whose
	// textual Content is empty. §4.3: a valid non-text input must survive into BOTH
	// the fact chain and the actual request. Additive + omitempty so events stored
	// before this field decode unchanged (backward compatible).
	ContentParts []model.ContentPart    `json:"content_parts,omitempty"`
	ToolCalls    []model.ToolCall       `json:"tool_calls"`        // Tool calls (if any)
	ToolID       string                 `json:"tool_id,omitempty"` // For a tool-result event: the tool_call id it answers (preserves pairing across store→resolve)
	ToolResults  map[string]interface{} `json:"tool_results"`      // Tool execution results
	Metadata     map[string]string      `json:"metadata"`          // Additional metadata

	// Response field stores the LLM response snapshot (optional)
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

	// === Read Operations ===

	// GetEvent retrieves a single event by its EventKey.
	GetEvent(key int64) (*FullEvent, error)

	// GetEvents retrieves multiple events by their EventKeys.
	// Returns events in the same order as keys. Skips missing keys.
	GetEvents(keys []int64) ([]FullEvent, error)

	// QueryEvents queries events based on filters.
	// Returns EventReference list (lightweight).
	QueryEvents(query QueryOptions) ([]EventReference, error)

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

// RelationStoreProvider is an optional interface for MemoryStore implementations
// that support causal relationship management via RelationStore.
// Callers should type-assert MemoryStore to RelationStoreProvider before accessing
// parent/child relationships, as not all implementations expose relation operations.
type RelationStoreProvider interface {
	RelationStore() RelationStore
}

// Compile-time interface satisfaction checks.
var (
	_ RelationStoreProvider = (*InMemoryStore)(nil)
	_ RelationStoreProvider = (*FileSegmentStore)(nil)
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
	// StartTime/EndTime filter events by timestamp, in Unix MILLISECONDS
	// (same unit as FullEvent.Timestamp and the recall tools' since/until).
	// Zero = no bound.
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
	// MinEventKey filters events whose EventKey is STRICTLY GREATER than this
	// value — the write-order axis, never semantic time (see the TIME CONTRACT
	// on FullEvent: async write-back events carry an EventKey assigned at
	// write time while their Timestamp is the bus-arrival moment, so a
	// StartTime approximation mis-cuts such stragglers). 0 = no bound.
	MinEventKey int64  `json:"min_event_key,omitempty"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
	OrderBy     string `json:"order_by"`
	// Keyword filters events whose EventSummary or Content contains the keyword (case-insensitive).
	// Empty string = no keyword filter.
	Keyword string `json:"keyword,omitempty"`
}

// StoreStats contains storage statistics.
type StoreStats struct {
	TotalEvents int    `json:"total_events"`
	StorageSize int64  `json:"storage_size"`
	DataDir     string `json:"data_dir"`
	// CountsKnown (resident-readiness-plan 2.8): false when the live counts
	// could not be rebuilt from the fact chain (backend without partition
	// enumeration, or a scan failure). Capacity eviction pauses on unknown —
	// unknown counts must never be reported as a precise 0.
	CountsKnown bool `json:"counts_known"`
}

// ReplayResult classifies the outcome of an EventReplayer.ReplayEvent call (D4 design).
type ReplayResult int

const (
	// ReplayNew: the event was not previously present; it has been committed fresh.
	// The live-count was incremented.
	ReplayNew ReplayResult = iota
	// ReplayRepaired: a half-written orphan was completed (evt slot was missing,
	// or meta absent, but idx existed). The commit barrier was re-run; the event
	// is now fully durable. The live-count was incremented exactly once.
	ReplayRepaired
	// ReplayAlreadyCommitted: the event was found fully committed (canonical fact
	// byte-identical and the key is in the publication cache). The call is
	// idempotent — no live-count increment.
	ReplayAlreadyCommitted
)

// EventReplayer is an optional capability interface for stores that support
// canonical content-checked replay (D4 design, fix F3/F8).
//
// Unlike the public StoreEvent, which rejects any already-present EventKey as
// a duplicate (including byte-identical content), ReplayEvent:
//  1. Verifies the stored fact is byte-identical before completing a partial
//     write (missing evt slot, missing meta) and re-running the commit barrier.
//  2. Returns a ReplayResult that distinguishes new-commit, orphan-repair, and
//     already-committed, so callers can decide whether to increment counters
//     or trigger side-effects exactly once.
//  3. Refuses different content under the same EventKey as a collision (D15),
//     never overwriting an existing fact.
//
// The reliable inbox (inbox-v2) and mem_spill recovery paths MUST use this
// interface rather than the public StoreEvent to make the commit vs. replay
// distinction explicit. Callers that hold no typed claim should continue to
// use StoreEvent.
//
// Implementations: FileSegmentStore, InMemoryStore, and decorator chain
// (engineBridge, ErrorTrackingStore) pass through transparently.
type EventReplayer interface {
	// ReplayEvent commits canonicalFact under key using the internal replay
	// path. It returns the classification of what happened and the canonical
	// fact as actually stored (which may differ from the input in non-content
	// fields if it was already committed). Returns a non-nil error when the
	// commit barrier fails or a content conflict is detected.
	ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error)
}

// RetentionGuard is the optional "材料保留" recovery capability (§2.8, spec L89:
// 显式重放 / 材料保留 / 底层屏障 三能力之一). The recovery owner (reliable inbox /
// spill) registers the durable originals of unacked material so the store's TTL
// expiry, capacity eviction and tombstone final-cleanup do not destroy them until
// they are safely released (ack dir-synced or spill removed). FileSegmentStore owns
// the lease; the engine/error-tracking wrapper chain recurses the capability through
// to it. A backend without durable recovery simply does not implement it.
type RetentionGuard interface {
	// ProtectKey registers a holder for a key (ref-counted; idempotent).
	ProtectKey(key int64)
	// ReleaseKey drops one holder; the key resumes normal age-based handling once
	// the last holder is gone (its ORIGINAL timestamp is never re-stamped).
	ReleaseKey(key int64)
	// ArmRetention signals that the recovery owner has finished rebuilding the lease
	// from existing unacked material, releasing the store's first destructive scan
	// (the restart-race gate). Idempotent; a store with no lease ignores it.
	ArmRetention()
	// BeginHold/EndHold raise and release the §5.8 registration barrier: while any
	// owner holds it, the lifecycle scanner pauses forgetting passes (unconditionally,
	// no grace escape) so a composition-root aggregate inventory or a late-attaching
	// shared recovery dir can protect its material before any destructive pass
	// lands. Pair every Begin with an End; nested/concurrent owners are ref-counted.
	BeginHold()
	EndHold()
}

// RetentionHoldable is the §5.8 registration-barrier surface alone (raised by a
// composition-root build gate or a late-attaching recovery owner to pause
// forgetting while its inventories land). *FileSegmentStore satisfies it through
// its lease; stores without a lease (no destructive scanner) do not, and callers
// treat the missing capability as "nothing to pause".
type RetentionHoldable interface {
	BeginHold()
	EndHold()
}

// Event type constants are defined in the event package (event.Type*).
// This is the single source of truth for event classification.
// See: github.com/SpellingDragon/tagent/event/types.go

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
// PartitionID: storage partition (0-1023), bits 53-62. Caller-derived, Memory
// does not interpret. Bit 63 (sign) is ALWAYS 0: positive keys are real
// events; NEGATIVE keys are reserved for synthetic summary references
// (compaction). An 11-bit partition field would reach the sign bit and flip
// keys negative for partitions ≥ 1024 (e.g. FNV("plan")=1810), silently
// breaking every `EventKey > 0` guard — store resolution, projection
// idempotency and retained-ref tracking would all treat those agents' events
// as unresolvable, leaving the model only summaries/placeholders.
// Timestamp: seconds since snowflakeEpoch (~68 year range).
// Sequence: per-second counter (0-1023), sub-second uniqueness.
// Reserved: for future use (e.g., distributed worker ID).

const (
	partitionIDShift = 53
	timestampShift   = 22
	sequenceShift    = 12

	partitionIDMask = 0x3FF      // 10 bits — keeps bit 63 (sign) clear
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
	// explicit=true when the caller drives the clock (tests); the real-clock
	// path (nowMs=0) alone gets the regression guard.
	explicit := nowMs > 0
	if !explicit {
		nowMs = time.Now().UnixMilli()
	}
	ts := nowMs/1000 - snowflakeEpoch

	snowflakeSeqMu.Lock()
	if !explicit && ts < snowflakeSeqLast[partitionID] {
		// Real-clock regression (NTP backward step): pin to the last issued
		// timestamp instead of resetting, so keys never regress below
		// already-issued ones — the compression render-freeze full-window
		// anchor (ref.EventKey >= boundary) is a hard dependency on key
		// monotonicity (stable-context-compaction). Caller-controlled
		// timestamps honor the given time (test semantics).
		ts = snowflakeSeqLast[partitionID]
	}
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

// RaiseSnowflakeFloor seeds the per-partition monotonicity guard from a
// DURABLE observation: the highest event key already issued on disk. A new
// process generation inherits the fact chain, not the in-memory counters —
// without this, a restart inside the same second re-issues keys that collide
// with committed facts (the §8.5 30-restart cadence proved the surface real:
// every colliding commit CONFLICTs and, with the frozen-key replay rule,
// conflicts FOREVER). The floor is one-way: only a strictly higher observed
// key raises it; a lower or equal one is a no-op.
func RaiseSnowflakeFloor(partitionID int, highestIssuedKey int64) {
	if highestIssuedKey <= 0 {
		return
	}
	ts := (highestIssuedKey >> timestampShift) & timestampMask
	seq := (highestIssuedKey >> sequenceShift) & sequenceMask
	snowflakeSeqMu.Lock()
	defer snowflakeSeqMu.Unlock()
	if ts > snowflakeSeqLast[partitionID] ||
		(ts == snowflakeSeqLast[partitionID] && int64(seq) >= int64(snowflakeSeqCnt[partitionID])) {
		seq++
		if seq > sequenceMask { // same-second floor exhausted: pin one second ahead
			ts++
			seq = 0
		}
		snowflakeSeqLast[partitionID] = ts
		snowflakeSeqCnt[partitionID] = int(seq)
	}
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
// PartitionIDFromName computes a stable PartitionID (0-1023) from a name string
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

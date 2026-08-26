package memory

import (
	"container/list"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== Simple LRU Cache ====================

// lruEntry holds a key-value pair in the LRU cache.
type lruEntry struct {
	key   int64
	value *FullEvent
}

// simpleLRU is a simple LRU cache for FullEvent objects.
type simpleLRU struct {
	mu      sync.Mutex
	items   map[int64]*list.Element
	order   *list.List
	maxSize int
}

func newSimpleLRU(maxSize int) *simpleLRU {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &simpleLRU{
		items:   make(map[int64]*list.Element),
		order:   list.New(),
		maxSize: maxSize,
	}
}

func (c *simpleLRU) Get(key int64) (*FullEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return elem.Value.(*lruEntry).value, true
	}
	return nil, false
}

func (c *simpleLRU) Add(key int64, value *FullEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*lruEntry).value = value
		return
	}
	elem := c.order.PushFront(&lruEntry{key: key, value: value})
	c.items[key] = elem
	if c.order.Len() > c.maxSize {
		c.removeOldest()
	}
}

func (c *simpleLRU) Remove(key int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.Remove(elem)
		delete(c.items, key)
	}
}

func (c *simpleLRU) removeOldest() {
	elem := c.order.Back()
	if elem != nil {
		entry := elem.Value.(*lruEntry)
		delete(c.items, entry.key)
		c.order.Remove(elem)
	}
}

func (c *simpleLRU) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// ==================== FileSegmentStore ====================
//
// FileSegmentStore implements MemoryStore using RustViking KV as the backing store.
// Events are organized by time-windowed segments following the KV key schema:
//
//	{pid}:evt:{window_ts}:{seq}  → JSON FullEvent content
//	{pid}:idx:{event_key}        → {window_ts}:{seq}  (offset index)
//	{pid}:meta:{window_ts}       → JSON segment metadata
//
// L0 (hot): The current time window's events - written directly to KV.
// L1 (warm): Sealed segments from previous time windows.
//
// The "segment" is a logical grouping by window_ts, not a physical file.
// Segment metadata tracks event count, min/max timestamp, and layer.

// PartitionState holds per-partition state for FileSegmentStore.
type PartitionState struct {
	mu            sync.Mutex
	currentWindow int64 // Current active window timestamp
	seqCounter    int   // Sequence counter within current window
	eventCount    int64 // Total events in this partition
}

// SegmentMeta holds metadata for a segment.
type SegmentMeta struct {
	PartitionID int   `json:"pid"`
	WindowTS    int64 `json:"window_ts"`
	Layer       int   `json:"layer"` // 1=L1 (sealed), 2=L2, 3=L3
	EventCount  int   `json:"event_count"`
	MinTime     int64 `json:"min_time"`
	MaxTime     int64 `json:"max_time"`
	Sealed      bool  `json:"sealed"`
}

// EventCache is an LRU cache for frequently accessed FullEvents.
const defaultCacheSize = 1000

// FileSegmentStore implements MemoryStore using RustViking KV + segment model.
type FileSegmentStore struct {
	kv         KVStore       // RustViking KV client (or mock)
	rel        RelationStore // Causal relationship graph
	tombstones *TombstoneSet // Optional tombstone set for dead event filtering
	cache      *simpleLRU    // EventCache LRU
	dataDir    string
	partitions sync.Map // map[int]*PartitionState

	// Lifecycle components (optional, set via Set* methods)
	lifecycle *LifecycleManager
	compactor *Compactor
	closeOnce sync.Once
}

// NewFileSegmentStore creates a FileSegmentStore.
func NewFileSegmentStore(kv KVStore, rel RelationStore, dataDir string, cacheSize int) (*FileSegmentStore, error) {
	if cacheSize <= 0 {
		cacheSize = defaultCacheSize
	}
	if rel == nil {
		rel = newSimpleInMemRelationStore()
	}
	return &FileSegmentStore{
		kv:      kv,
		rel:     rel,
		cache:   newSimpleLRU(cacheSize),
		dataDir: dataDir,
	}, nil
}

// Init initializes the FileSegmentStore by scanning existing KV data
// and recovering partition states. Called on startup after crash recovery.
func (s *FileSegmentStore) Init() error {
	// Scan for existing segment metadata to discover partitions
	// We need to scan all possible partition prefixes.
	// In practice, partitions are discovered from the meta keys.
	// For simplicity, we don't pre-scan all partitions here;
	// they are lazily initialized on first access via getPartitionState.
	// The RustViking RocksDB handles its own crash recovery via WAL,
	// so no local file truncation is needed.
	return nil
}

// getPartitionState returns (or creates) the PartitionState for a given partition ID.
func (s *FileSegmentStore) getPartitionState(pid int) *PartitionState {
	actual, _ := s.partitions.LoadOrStore(pid, &PartitionState{})
	return actual.(*PartitionState)
}

// ==================== Write Operations ====================

// StoreEvent stores a single event via RustViking KV.
func (s *FileSegmentStore) StoreEvent(key int64, event FullEvent) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}

	pid := event.PartitionID
	if pid == 0 {
		pid = PartitionIDFromEventKey(key)
	}
	event.EventKey = key
	event.PartitionID = pid

	// Derive window timestamp from event key
	tsSec := TimestampFromEventKey(key)
	windowTS := WindowTimestamp(tsSec, DefaultWindowSize)

	state := s.getPartitionState(pid)
	state.mu.Lock()

	// Check if we've moved to a new time window
	if state.currentWindow != windowTS {
		state.currentWindow = windowTS
		state.seqCounter = 0
	}
	if state.seqCounter == 0 {
		// Recover the highest seq already used in this window (D12): an in-
		// memory zero seqCounter cannot distinguish "fresh window" from
		// "process restarted / window revisited", and reusing seq 0 would
		// overwrite the existing slot — silently swallowing the event that
		// lived there (production: one dangling idx already proved this).
		// The scan is scoped to this single window and happens at most once
		// per window switch. Recovery FAILS LOUD (M3): falling back to 0
		// would re-introduce the overwrite it exists to prevent.
		recovered, recErr := s.recoverWindowSeqLocked(pid, windowTS)
		if recErr != nil {
			state.mu.Unlock()
			return fmt.Errorf("seq recovery failed for window %d: %w", windowTS, recErr)
		}
		state.seqCounter = recovered

		// Revisit of a SEALED window (m5): its recorded MinTime/MaxTime no
		// longer covers what we are about to add, so demote it back to
		// memtable semantics (Sealed=false → always scanned, never pruned).
		if rawMeta, metaErr := s.kv.KVGet(MetaKeyStr(pid, windowTS)); metaErr == nil {
			var oldMeta SegmentMeta
			if jsonErr := json.Unmarshal([]byte(rawMeta), &oldMeta); jsonErr == nil && oldMeta.Sealed {
				oldMeta.Sealed = false
				if patched, mErr := json.Marshal(oldMeta); mErr == nil {
					if pErr := s.kv.KVPut(MetaKeyStr(pid, windowTS), string(patched)); pErr != nil {
						log.Warnf("[SegmentStore] failed to demote sealed window pid=%d window=%d: %v", pid, windowTS, pErr)
					}
				}
			}
		}
	}
	seq := state.seqCounter
	state.seqCounter++
	state.eventCount++
	state.mu.Unlock()

	// Serialize event
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event %d: %w", key, err)
	}

	// Collision guard (D15, before any write per code-review M2): an EventKey
	// IS the event's identity — a second write under the same key can only
	// be a snowflake collision (the in-memory seq counter restarts across
	// processes; two writers in the same second collide). Silently
	// overwriting the idx pointer would break event immutability, so reject
	// instead. All producers mint a fresh key per event; no caller relies on
	// rewriting an existing key. Must precede the evt write: segment scans
	// read events directly, so a rejected write must not leave a ghost.
	idxKVKey := IndexKeyStr(pid, key)
	if _, err := s.kv.KVGet(idxKVKey); err == nil {
		return fmt.Errorf("event key %d already exists (snowflake collision?): refusing to overwrite", key)
	}

	// KV key for event content
	evtKVKey := EventKeyStr(pid, windowTS, seq)
	if err := s.kv.KVPut(evtKVKey, string(eventJSON)); err != nil {
		return fmt.Errorf("failed to store event key %d: %w", key, err)
	}

	// KV key for index (EventKey → segment position)
	idxValue := fmt.Sprintf("%d:%d", windowTS, seq)
	if err := s.kv.KVPut(idxKVKey, idxValue); err != nil {
		return fmt.Errorf("failed to store index for event %d: %w", key, err)
	}

	// Write/update segment metadata (if first event in window)
	if seq == 0 {
		meta := SegmentMeta{
			PartitionID: pid,
			WindowTS:    windowTS,
			Layer:       1,
			Sealed:      false,
		}
		metaJSON, _ := json.Marshal(meta)
		metaKVKey := MetaKeyStr(pid, windowTS)
		if err := s.kv.KVPut(metaKVKey, string(metaJSON)); err != nil {
			log.Errorf("[SegmentStore] KVPut meta failed pid=%d window=%d: %v", pid, windowTS, err)
		}
	}

	// Update LRU cache
	s.cache.Add(key, &event)

	return nil
}

// recoverWindowSeqLocked returns the next free seq for a window by scanning
// its existing event keys. It returns 0 for an empty window and max(seq)+1
// otherwise. seq keys are zero-padded-free strings (0,1,10,2…) so the max
// must be computed numerically, not taken from scan order.
//
// A scan failure is an ERROR, not a silent 0: falling back to 0 would
// overwrite existing slots (the very bug D12 exists to prevent), so the
// caller fails the StoreEvent instead (code-review M3).
//
// Caller holds the partition state lock.
func (s *FileSegmentStore) recoverWindowSeqLocked(pid int, windowTS int64) (int, error) {
	pairs, err := s.kv.KVScan(SegmentEventPrefix(pid, windowTS), 0)
	if err != nil {
		return 0, fmt.Errorf("scan window events: %w", err)
	}
	maxSeq := -1
	for _, pair := range pairs {
		pk, err := ParseKey(pair.Key)
		if err != nil || pk.KeyType != "evt" {
			continue
		}
		if pk.Seq > maxSeq {
			maxSeq = pk.Seq
		}
	}
	return maxSeq + 1, nil
}

// ==================== Read Operations ====================

// GetEvent retrieves a single event by its EventKey.
func (s *FileSegmentStore) GetEvent(key int64) (*FullEvent, error) {
	if key == 0 {
		return nil, fmt.Errorf("event key cannot be zero")
	}

	// Check tombstone
	if s.tombstones != nil && s.tombstones.IsTombstone(key) {
		return nil, fmt.Errorf("event %d not found: tombstoned", key)
	}

	// 1. Check LRU cache
	if cached, ok := s.cache.Get(key); ok {
		return cached, nil
	}

	// 2. Look up index to find segment position
	pid := PartitionIDFromEventKey(key)
	idxKVKey := IndexKeyStr(pid, key)
	idxValue, err := s.kv.KVGet(idxKVKey)
	if err != nil {
		return nil, fmt.Errorf("event %d not found (index lookup failed): %w", key, err)
	}

	// Parse index value: "windowTS:seq"
	parts := strings.SplitN(idxValue, ":", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid index value for event %d: %s", key, idxValue)
	}

	var windowTS, seq int64
	if _, err := fmt.Sscanf(idxValue, "%d:%d", &windowTS, &seq); err != nil {
		return nil, fmt.Errorf("failed to parse index value for event %d: %s", key, idxValue)
	}

	// 3. Read event from KV store
	evtKVKey := EventKeyStr(pid, windowTS, int(seq))
	eventJSON, err := s.kv.KVGet(evtKVKey)
	if err != nil {
		return nil, fmt.Errorf("event %d not found at %s: %w", key, evtKVKey, err)
	}

	var event FullEvent
	if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event %d: %w", key, err)
	}

	// Add to cache
	s.cache.Add(key, &event)
	return &event, nil
}

// GetEvents retrieves multiple events by their EventKeys.
func (s *FileSegmentStore) GetEvents(keys []int64) ([]FullEvent, error) {
	results := make([]FullEvent, 0, len(keys))
	for _, key := range keys {
		evt, err := s.GetEvent(key)
		if err != nil {
			continue // Skip missing keys
		}
		results = append(results, *evt)
	}
	return results, nil
}

// QueryEvents queries events based on filters.
//
// Behavioral contract (segment-query-recency): the result is semantically
// equivalent to "filter all events → total-order sort → offset/limit".
// Segmentation, window pruning and early-stop below are optimizations only
// and must not change the observable result. Total order: (Timestamp,
// EventKey) — same-millisecond events are tie-broken by EventKey so any two
// runs (and any store implementation) return identical sequences.
func (s *FileSegmentStore) QueryEvents(query QueryOptions) ([]EventReference, error) {
	// Determine which partitions to search
	partitions := s.resolvePartitions(query)

	limit := query.Limit
	if limit <= 0 {
		limit = 100 // Default limit
	}
	// Per-partition collection budget: the global top-(offset+limit) can only
	// draw from each partition's own top-(offset+limit), so collecting that
	// many per partition before the global sort is lossless.
	budget := limit
	if query.Offset > 0 {
		budget += query.Offset
	}

	var matched []EventReference
	for _, pid := range partitions {
		// Scan this partition's segments (no cross-partition truncation —
		// truncating before the global sort would drop newer partitions).
		refs, err := s.scanPartition(pid, query, budget)
		if err != nil {
			continue
		}
		matched = append(matched, refs...)
	}

	sortRefsByTotalOrder(matched, query.OrderBy)

	// Apply offset
	if query.Offset > 0 {
		if query.Offset >= len(matched) {
			return nil, nil
		}
		matched = matched[query.Offset:]
	}
	// Apply limit after sorting
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return matched, nil
}

// sortRefsByTotalOrder sorts references by the total order (Timestamp,
// EventKey); orderBy "timestamp_desc" reverses both keys together.
func sortRefsByTotalOrder(refs []EventReference, orderBy string) {
	desc := orderBy == "timestamp_desc"
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Timestamp != refs[j].Timestamp {
			if desc {
				return refs[i].Timestamp > refs[j].Timestamp
			}
			return refs[i].Timestamp < refs[j].Timestamp
		}
		if desc {
			return refs[i].EventKey > refs[j].EventKey
		}
		return refs[i].EventKey < refs[j].EventKey
	})
}

// noUpperBoundMs marks a segment whose time upper bound cannot be proven.
const noUpperBoundMs int64 = math.MaxInt64

// segmentBounds derives the segment's TRUTHFUL event-time envelope for query
// pruning and early-stop (segment-query-recency D14 — LSM key-range
// metadata):
//
//   - Sealed with MinTime/MaxTime → those bounds (event time, stable once
//     the segment is immutable). WindowTS remains a valid lower bound when
//     MinTime is absent.
//   - Unsealed (active memtable) OR sealed-but-boundless (legacy segment) →
//     unprovable: provable=false, meaning NEVER prune, NEVER skip.
//
// The nominal window name/layer is NOT consulted for time reasoning — it
// encodes write recency and compaction generation, not event-time coverage.
func segmentBounds(windowTS int64, meta SegmentMeta) (lowerMs, upperMs int64, provable bool) {
	if !meta.Sealed || meta.MaxTime <= 0 {
		return 0, noUpperBoundMs, false
	}
	// MinTime is the REAL event-time minimum recorded at seal/compaction —
	// trust it directly. Do NOT max() it against the nominal window start:
	// placement follows WRITE time while MinTime is EVENT time, and the two
	// diverge for asynchronous write-back (an event older than its window).
	// The window name is only the fallback when MinTime is absent.
	lowerMs = windowTS * 1000
	if meta.MinTime > 0 {
		lowerMs = meta.MinTime
	}
	return lowerMs, meta.MaxTime, true
}

// scanPartition scans a single partition's segments matching the query.
//
// Windows are traversed in the query's time direction (desc: newest first) so
// that early-stop sacrifices the oldest matches, never the newest — the
// recall arrow must point the same way as compression (drop old, keep new).
// Collection is whole-window: seq keys are lexicographic (0,1,10,2…), not
// time-ordered, so a window must be fully scanned before its matches count.
func (s *FileSegmentStore) scanPartition(pid int, query QueryOptions, budget int) ([]EventReference, error) {
	// Phase 1: window discovery — scan meta prefix, parse (windowTS, layer,
	// MinTime/MaxTime) from key + value (all ride in the meta JSON; no extra
	// KVGet).
	metaPrefix := MetaPrefix(pid)
	metaPairs, err := s.kv.KVScan(metaPrefix, 0)
	if err != nil {
		return nil, err
	}

	type windowInfo struct {
		ts       int64
		layer    int
		provable bool  // whether lower/upper are truthful (D14)
		lowerMs  int64 // truthful lower bound when provable
		upperMs  int64 // truthful upper bound when provable
	}
	var windows []windowInfo
	for _, pair := range metaPairs {
		pk, err := ParseKey(pair.Key)
		if err != nil || pk.KeyType != "meta" {
			continue
		}
		var meta SegmentMeta
		if err := json.Unmarshal([]byte(pair.Value), &meta); err != nil {
			meta = SegmentMeta{}
		}
		lowerMs, upperMs, provable := segmentBounds(pk.WindowTS, meta)

		// Time range pruning (Unix ms contract): a segment is pruned only when
		// its PROVABLE bounds put it entirely outside the range. Active
		// (unsealed) and boundless segments are memtables — never pruned.
		if provable {
			if query.EndTime > 0 && lowerMs > query.EndTime {
				continue
			}
			if query.StartTime > 0 && upperMs < query.StartTime {
				continue
			}
		}
		windows = append(windows, windowInfo{ts: pk.WindowTS, layer: meta.Layer, provable: provable, lowerMs: lowerMs, upperMs: upperMs})
	}

	desc := query.OrderBy == "timestamp_desc"
	sort.Slice(windows, func(i, j int) bool {
		if desc {
			return windows[i].ts > windows[j].ts
		}
		return windows[i].ts < windows[j].ts
	})

	// Phase 2: per-window collection with cross-window dedup and
	// budget-based skipping.
	var matched []EventReference
	// Dedup: EventKey → (index in matched, layer of chosen version). On
	// duplicates (compaction crash window: same event alive in source and
	// target layers) keep the higher-layer version — direction-independent,
	// so desc and asc return the same content version (D3).
	seenIdx := make(map[int64]int)
	seenLayer := make(map[int64]int)
	// Collected-side bounds use ACTUAL event timestamps, never nominal window
	// bounds (D10): nominal bounds are untruthful for compacted segments, and
	// the real timestamps are already in hand and tighter.
	var minCollectedTs, maxCollectedTs int64
	collectedAny := false

	for _, w := range windows {
		// Early-stop as a per-window skip (layers have non-monotonic bounds, so
		// a hard break could skip an overlapping daily/weekly window). Once the
		// budget is met, a window can be skipped only when it provably cannot
		// hold an event that outranks the collected set. Comparison is STRICT:
		// on equality the same-millisecond EventKey tie-break could still let
		// the candidate win.
		if budget > 0 && len(matched) >= budget && collectedAny {
			if desc && w.provable && w.upperMs < minCollectedTs {
				// Every event in w is older than every collected match.
				continue
			}
			if !desc && w.provable && w.lowerMs > maxCollectedTs {
				// Every event in w is newer than every collected match.
				continue
			}
		}

		wLayer := w.layer

		// Scan events in this segment (whole window, no intra-window stop).
		eventPrefix := SegmentEventPrefix(pid, w.ts)
		eventPairs, err := s.kv.KVScan(eventPrefix, 0)
		if err != nil {
			continue
		}

		for _, ep := range eventPairs {
			evtPK, err := ParseKey(ep.Key)
			if err != nil || evtPK.KeyType != "evt" {
				continue
			}

			// Parse the value (JSON FullEvent) to extract reference fields
			var event FullEvent
			if err := json.Unmarshal([]byte(ep.Value), &event); err != nil {
				continue
			}

			// Skip tombstoned events
			if s.tombstones != nil && s.tombstones.IsTombstone(event.EventKey) {
				continue
			}

			// Apply filters
			if !matchesQueryFilters(event, query) {
				continue
			}

			ref := EventReference{
				EventKey:     event.EventKey,
				PartitionID:  event.PartitionID,
				EventType:    event.EventType,
				EventSummary: event.EventSummary,
				Timestamp:    event.Timestamp,
			}
			if idx, dup := seenIdx[event.EventKey]; dup {
				if wLayer > seenLayer[event.EventKey] {
					matched[idx] = ref
					seenLayer[event.EventKey] = wLayer
				}
				continue
			}
			seenIdx[event.EventKey] = len(matched)
			seenLayer[event.EventKey] = wLayer
			matched = append(matched, ref)

			// Track the collected set's ACTUAL timestamp extremes — the
			// early-stop basis (D10).
			if !collectedAny || event.Timestamp < minCollectedTs {
				minCollectedTs = event.Timestamp
			}
			if !collectedAny || event.Timestamp > maxCollectedTs {
				maxCollectedTs = event.Timestamp
			}
			collectedAny = true
		}
	}

	return matched, nil
}

// resolvePartitions determines which partitions to search.
func (s *FileSegmentStore) resolvePartitions(query QueryOptions) []int {
	if len(query.PartitionIDs) > 0 {
		return query.PartitionIDs
	}
	if query.PartitionID > 0 {
		return []int{query.PartitionID}
	}
	// No partition filter - cannot determine partitions from KV
	// Return empty to skip (caller must specify at least one partition)
	return nil
}

// matchesQueryFilters checks if an event matches the query filters.
func matchesQueryFilters(event FullEvent, query QueryOptions) bool {
	// Filter by event types
	if len(query.EventTypes) > 0 {
		found := false
		for _, et := range query.EventTypes {
			if event.EventType == et {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	// Filter by time range
	if query.StartTime > 0 && event.Timestamp < query.StartTime {
		return false
	}
	if query.EndTime > 0 && event.Timestamp > query.EndTime {
		return false
	}
	// Filter by keyword: term-split ANY-match (see matchesKeyword) — a
	// literal whole-string match silently returns zero for the space-
	// separated keyword lists and sentences models actually send.
	if query.Keyword != "" {
		if !matchesKeyword(event.EventSummary, query.Keyword) &&
			!matchesKeyword(event.Content, query.Keyword) {
			return false
		}
	}
	return true
}

// RelationStore returns the underlying RelationStore for relationship operations.
func (s *FileSegmentStore) RelationStore() RelationStore {
	return s.rel
}

// ==================== Management Operations ====================

// DeleteEvent permanently deletes an event from storage.
func (s *FileSegmentStore) DeleteEvent(key int64) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}

	pid := PartitionIDFromEventKey(key)
	idxKVKey := IndexKeyStr(pid, key)

	// Get the event key to determine the full KV key to delete
	idxValue, err := s.kv.KVGet(idxKVKey)
	if err != nil {
		return fmt.Errorf("event %d not found", key)
	}

	var windowTS, seq int64
	fmt.Sscanf(idxValue, "%d:%d", &windowTS, &seq)
	evtKVKey := EventKeyStr(pid, windowTS, int(seq))

	// Batch delete
	ops := []KVOp{
		{Type: "delete", Key: evtKVKey},
		{Type: "delete", Key: idxKVKey},
	}
	if err := s.kv.KVBatch(ops); err != nil {
		return fmt.Errorf("failed to delete event %d: %w", key, err)
	}

	s.cache.Remove(key)

	// Keep the process-lifetime count accurate (D11): capacity eviction
	// reads this counter.
	s.decrementEventCount(pid, 1)
	return nil
}

// decrementEventCount lowers the process-lifetime event counter after
// physical removal (DeleteEvent, compaction cleanup). Floored at zero.
func (s *FileSegmentStore) decrementEventCount(pid int, n int64) {
	state := s.getPartitionState(pid)
	state.mu.Lock()
	state.eventCount -= n
	if state.eventCount < 0 {
		state.eventCount = 0
	}
	state.mu.Unlock()
}

// GetStats returns storage statistics.
func (s *FileSegmentStore) GetStats() StoreStats {
	total := int64(0)
	s.partitions.Range(func(_, v interface{}) bool {
		state := v.(*PartitionState)
		state.mu.Lock()
		total += state.eventCount
		state.mu.Unlock()
		return true
	})
	return StoreStats{
		TotalEvents: int(total),
		DataDir:     s.dataDir,
	}
}

// SearchByEmbedding performs semantic search (stub — not supported).
func (s *FileSegmentStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
	return nil, ErrVectorSearchNotSupported
}

// StoreEventWithEmbedding stores event with embedding (stub — ignores embedding).
func (s *FileSegmentStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
	return s.StoreEvent(key, event)
}

// SupportsVectorSearch returns false.
func (s *FileSegmentStore) SupportsVectorSearch() bool {
	return false
}

// ==================== Segment Management ====================

// SealCurrent seals the current active segment for a partition.
// Updates segment metadata in KV to mark it as L1 (sealed).
func (s *FileSegmentStore) SealCurrent(pid int) error {
	state := s.getPartitionState(pid)
	state.mu.Lock()
	windowTS := state.currentWindow
	seqCount := state.seqCounter
	state.mu.Unlock()

	if seqCount == 0 {
		return nil // Nothing to seal
	}

	// Sealing is the LSM flush point: the segment becomes immutable, so its
	// truthful event-time envelope (MinTime/MaxTime) is recorded here and
	// stays stable forever after. One scan of the window being sealed
	// (hourly, per partition) — cheap compared to the queries it enables.
	minTime, maxTime := int64(0), int64(0)
	hasMin := false // 0 is a legal Timestamp; don't use it as the sentinel (n1)
	eventPrefix := SegmentEventPrefix(pid, windowTS)
	eventPairs, err := s.kv.KVScan(eventPrefix, 0)
	if err == nil {
		for _, ep := range eventPairs {
			var evt struct {
				Timestamp int64 `json:"timestamp"`
			}
			if jsonErr := json.Unmarshal([]byte(ep.Value), &evt); jsonErr != nil {
				continue
			}
			if !hasMin || evt.Timestamp < minTime {
				minTime = evt.Timestamp
				hasMin = true
			}
			if evt.Timestamp > maxTime {
				maxTime = evt.Timestamp
			}
		}
	}

	// Write/update segment metadata
	meta := SegmentMeta{
		PartitionID: pid,
		WindowTS:    windowTS,
		Layer:       1,
		EventCount:  seqCount,
		MinTime:     minTime,
		MaxTime:     maxTime,
		Sealed:      true,
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal segment meta: %w", err)
	}

	metaKVKey := MetaKeyStr(pid, windowTS)
	return s.kv.KVPut(metaKVKey, string(metaJSON))
}

// GetSegmentMeta retrieves segment metadata from KV.
func (s *FileSegmentStore) GetSegmentMeta(pid int, windowTS int64) (*SegmentMeta, error) {
	metaKVKey := MetaKeyStr(pid, windowTS)
	value, err := s.kv.KVGet(metaKVKey)
	if err != nil {
		return nil, err
	}
	var meta SegmentMeta
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ListSegments returns all segment window timestamps for a partition.
func (s *FileSegmentStore) ListSegments(pid int) ([]int64, error) {
	metaPrefix := MetaPrefix(pid)
	pairs, err := s.kv.KVScan(metaPrefix, 0)
	if err != nil {
		return nil, err
	}
	var windows []int64
	for _, pair := range pairs {
		pk, err := ParseKey(pair.Key)
		if err != nil {
			continue
		}
		windows = append(windows, pk.WindowTS)
	}
	sort.Slice(windows, func(i, j int) bool {
		return windows[i] < windows[j]
	})
	return windows, nil
}

// ==================== Lifecycle Wiring ====================

// SetTombstoneSet injects a TombstoneSet into the store after construction.
// This allows tombstone filtering to be enabled without modifying NewFileSegmentStore's signature.
// Once set, GetEvent and QueryEvents will skip tombstoned events.
func (s *FileSegmentStore) SetTombstoneSet(ts *TombstoneSet) {
	s.tombstones = ts
}

// SetLifecycleManager injects a LifecycleManager for graceful shutdown.
// The manager is stopped when Close() is called.
func (s *FileSegmentStore) SetLifecycleManager(lm *LifecycleManager) {
	s.lifecycle = lm
}

// SetCompactor injects a Compactor for graceful shutdown.
// The compactor is stopped when Close() is called.
func (s *FileSegmentStore) SetCompactor(c *Compactor) {
	s.compactor = c
}

// closer is an internal interface for resources that need cleanup.
type closer interface {
	Close() error
}

// Close stops all background components (Compactor, LifecycleManager) and
// closes the RelationStore if it supports closing. Idempotent via sync.Once.
func (s *FileSegmentStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		// Stop Compactor first (it may be writing segments)
		if s.compactor != nil {
			s.compactor.Stop()
		}
		// Stop LifecycleManager (it may be marking tombstones)
		if s.lifecycle != nil {
			s.lifecycle.Stop()
		}
		// Close RelationStore if it supports closing (e.g., InMemRelationStore flushes WAL)
		if c, ok := s.rel.(closer); ok {
			if e := c.Close(); e != nil {
				err = e
			}
		}
		// Close the KV last — it performs the final durability flush.
		// Without this, LocalFileKV's deferred-flush window (up to
		// flushInterval / flushThreshold-1 writes) is lost on graceful exit.
		if c, ok := s.kv.(closer); ok {
			if e := c.Close(); e != nil {
				err = e
			}
		}
	})
	return err
}

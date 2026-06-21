package memory

import (
	"container/list"
	"encoding/json"
	"fmt"
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
	seq := state.seqCounter
	state.seqCounter++
	state.eventCount++
	state.mu.Unlock()

	// Serialize event
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event %d: %w", key, err)
	}

	// KV key for event content
	evtKVKey := EventKeyStr(pid, windowTS, seq)
	if err := s.kv.KVPut(evtKVKey, string(eventJSON)); err != nil {
		return fmt.Errorf("failed to store event key %d: %w", key, err)
	}

	// KV key for index (EventKey → segment position)
	idxKVKey := IndexKeyStr(pid, key)
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

// StoreEvents stores multiple events in batch.
func (s *FileSegmentStore) StoreEvents(events map[int64]FullEvent) error {
	// Build batch operations
	ops := make([]KVOp, 0, len(events)*2)
	for key, event := range events {
		if key == 0 {
			continue
		}
		pid := event.PartitionID
		if pid == 0 {
			pid = PartitionIDFromEventKey(key)
		}
		event.EventKey = key
		event.PartitionID = pid

		tsSec := TimestampFromEventKey(key)
		windowTS := WindowTimestamp(tsSec, DefaultWindowSize)

		state := s.getPartitionState(pid)
		state.mu.Lock()
		if state.currentWindow != windowTS {
			state.currentWindow = windowTS
			state.seqCounter = 0
		}
		seq := state.seqCounter
		state.seqCounter++
		state.eventCount++
		state.mu.Unlock()

		eventJSON, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("failed to marshal event %d: %w", key, err)
		}

		evtKVKey := EventKeyStr(pid, windowTS, seq)
		ops = append(ops, KVOp{Type: "put", Key: evtKVKey, Value: string(eventJSON)})

		idxKVKey := IndexKeyStr(pid, key)
		idxValue := fmt.Sprintf("%d:%d", windowTS, seq)
		ops = append(ops, KVOp{Type: "put", Key: idxKVKey, Value: idxValue})

		s.cache.Add(key, &event)
	}

	if len(ops) > 0 {
		return s.kv.KVBatch(ops)
	}
	return nil
}

// ensureSegmentMeta writes segment metadata if it doesn't exist.
func (s *FileSegmentStore) ensureSegmentMeta(pid int, windowTS int64) error {
	metaKVKey := MetaKeyStr(pid, windowTS)
	_, err := s.kv.KVGet(metaKVKey)
	if err == nil {
		return nil // Already exists
	}
	// Create new segment meta
	meta := SegmentMeta{
		PartitionID: pid,
		WindowTS:    windowTS,
		Layer:       1,
		Sealed:      false,
	}
	metaJSON, _ := json.Marshal(meta)
	return s.kv.KVPut(metaKVKey, string(metaJSON))
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
func (s *FileSegmentStore) QueryEvents(query QueryOptions) ([]EventReference, error) {
	// Determine which partitions to search
	partitions := s.resolvePartitions(query)

	var matched []EventReference
	limit := query.Limit
	if limit <= 0 {
		limit = 100 // Default limit
	}

	for _, pid := range partitions {
		if limit > 0 && len(matched) >= limit {
			break
		}
		// Scan this partition's segments
		refs, err := s.scanPartition(pid, query, limit-len(matched))
		if err != nil {
			continue
		}
		matched = append(matched, refs...)
	}

	// Sort by timestamp
	sort.Slice(matched, func(i, j int) bool {
		if query.OrderBy == "timestamp_desc" {
			return matched[i].Timestamp > matched[j].Timestamp
		}
		return matched[i].Timestamp < matched[j].Timestamp
	})

	// Apply offset
	if query.Offset > 0 && query.Offset < len(matched) {
		matched = matched[query.Offset:]
	}
	// Apply limit after sorting
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return matched, nil
}

// scanPartition scans a single partition's segments matching the query.
func (s *FileSegmentStore) scanPartition(pid int, query QueryOptions, maxResults int) ([]EventReference, error) {
	// Scan meta prefix to discover segments
	metaPrefix := MetaPrefix(pid)
	metaPairs, err := s.kv.KVScan(metaPrefix, 0)
	if err != nil {
		return nil, err
	}

	var matched []EventReference
	for _, pair := range metaPairs {
		if maxResults > 0 && len(matched) >= maxResults {
			break
		}

		// Parse meta key to extract window_ts
		pk, err := ParseKey(pair.Key)
		if err != nil || pk.KeyType != "meta" {
			continue
		}

		// Time range pruning: skip segments outside query range
		if query.StartTime > 0 || query.EndTime > 0 {
			windowStart := pk.WindowTS
			windowEnd := pk.WindowTS + DefaultWindowSize
			if query.EndTime > 0 && windowStart > query.EndTime {
				continue
			}
			if query.StartTime > 0 && windowEnd < query.StartTime {
				continue
			}
		}

		// Scan events in this segment
		eventPrefix := SegmentEventPrefix(pid, pk.WindowTS)
		eventPairs, err := s.kv.KVScan(eventPrefix, 0)
		if err != nil {
			continue
		}

		for _, ep := range eventPairs {
			if maxResults > 0 && len(matched) >= maxResults {
				break
			}

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

			matched = append(matched, EventReference{
				EventKey:     event.EventKey,
				PartitionID:  event.PartitionID,
				EventType:    event.EventType,
				EventSummary: event.EventSummary,
				Timestamp:    event.Timestamp,
			})
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
	// Filter by keyword
	if query.Keyword != "" {
		if !containsIgnoreCase(event.EventSummary, query.Keyword) &&
			!containsIgnoreCase(event.Content, query.Keyword) {
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
	return nil
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

	// Write/update segment metadata
	meta := SegmentMeta{
		PartitionID: pid,
		WindowTS:    windowTS,
		Layer:       1,
		EventCount:  seqCount,
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
	})
	return err
}

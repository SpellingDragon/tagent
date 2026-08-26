package memory

import (
	"fmt"
	"sort"
	"sync"
)

// InMemoryStore implements MemoryStore using an in-memory map.
// Events are partitioned by PartitionID for storage isolation.
// Suitable for testing and prototyping.
//
// Note: InMemoryStore embeds RelationStore to provide O(1)
// parent/child relationship queries via GetParent/GetChildren.
type InMemoryStore struct {
	mu     sync.RWMutex
	events map[int]map[int64]FullEvent // PartitionID → EventKey → FullEvent
	rel    RelationStore               // Causal relationship graph
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return NewInMemoryStoreWithRelation(nil)
}

// NewInMemoryStoreWithRelation creates a new InMemoryStore with a RelationStore.
// If rel is nil, creates a default simpleInMemRelationStore that stores relationships in memory.
func NewInMemoryStoreWithRelation(rel RelationStore) *InMemoryStore {
	if rel == nil {
		rel = newSimpleInMemRelationStore()
	}
	return &InMemoryStore{
		events: make(map[int]map[int64]FullEvent),
		rel:    rel,
	}
}

// StoreEvent stores a single event.
func (s *InMemoryStore) StoreEvent(key int64, event FullEvent) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	pid := event.PartitionID
	if pid == 0 {
		// Fallback: extract from key
		pid = PartitionIDFromEventKey(key)
		event.PartitionID = pid
	}
	event.EventKey = key

	if s.events[pid] == nil {
		s.events[pid] = make(map[int64]FullEvent)
	}
	s.events[pid][key] = event
	return nil
}

// GetEvent retrieves a single event by its EventKey.
func (s *InMemoryStore) GetEvent(key int64) (*FullEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pid := PartitionIDFromEventKey(key)
	partition, ok := s.events[pid]
	if !ok {
		return nil, fmt.Errorf("event not found: %d", key)
	}
	event, ok := partition[key]
	if !ok {
		return nil, fmt.Errorf("event not found: %d", key)
	}
	return &event, nil
}

// GetEvents retrieves multiple events by their EventKeys.
func (s *InMemoryStore) GetEvents(keys []int64) ([]FullEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]FullEvent, 0, len(keys))
	for _, key := range keys {
		pid := PartitionIDFromEventKey(key)
		if partition, ok := s.events[pid]; ok {
			if event, ok := partition[key]; ok {
				results = append(results, event)
			}
		}
	}
	return results, nil
}

// QueryEvents queries events based on filters.
func (s *InMemoryStore) QueryEvents(query QueryOptions) ([]EventReference, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Determine which partitions to search
	partitions := s.resolvePartitions(query)

	var matched []FullEvent
	for _, pid := range partitions {
		partition, ok := s.events[pid]
		if !ok {
			continue
		}
		for _, event := range partition {
			if !s.matchesQuery(event, query) {
				continue
			}
			matched = append(matched, event)
		}
	}

	// Sort by the total order (Timestamp, EventKey) — tie-break keeps
	// same-millisecond events deterministic and behaviorally aligned with
	// FileSegmentStore (segment-query-recency contract).
	desc := query.OrderBy == "timestamp_desc"
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Timestamp != matched[j].Timestamp {
			if desc {
				return matched[i].Timestamp > matched[j].Timestamp
			}
			return matched[i].Timestamp < matched[j].Timestamp
		}
		if desc {
			return matched[i].EventKey > matched[j].EventKey
		}
		return matched[i].EventKey < matched[j].EventKey
	})

	// Apply offset and limit. Limit <= 0 defaults to 100 — the same as
	// FileSegmentStore, so the two implementations stay behaviorally
	// identical under the parity contract (code-review m3).
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	start := 0
	if query.Offset > 0 {
		start = query.Offset
	}
	if start > len(matched) {
		return nil, nil
	}
	matched = matched[start:]
	if len(matched) > limit {
		matched = matched[:limit]
	}

	// Convert to EventReference
	results := make([]EventReference, 0, len(matched))
	for _, event := range matched {
		results = append(results, EventReference{
			EventKey:     event.EventKey,
			PartitionID:  event.PartitionID,
			EventType:    event.EventType,
			EventSummary: event.EventSummary,
			Timestamp:    event.Timestamp,
		})
	}
	return results, nil
}

// resolvePartitions determines which partitions to search based on query.
func (s *InMemoryStore) resolvePartitions(query QueryOptions) []int {
	if len(query.PartitionIDs) > 0 {
		return query.PartitionIDs
	}
	if query.PartitionID > 0 {
		return []int{query.PartitionID}
	}
	// All partitions
	pids := make([]int, 0, len(s.events))
	for pid := range s.events {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return pids
}

// matchesQuery checks if an event matches the query filters.
func (s *InMemoryStore) matchesQuery(event FullEvent, query QueryOptions) bool {
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

// DeleteEvent permanently deletes an event.
func (s *InMemoryStore) DeleteEvent(key int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pid := PartitionIDFromEventKey(key)
	if partition, ok := s.events[pid]; ok {
		delete(partition, key)
		if len(partition) == 0 {
			delete(s.events, pid)
		}
	}
	return nil
}

// GetStats returns storage statistics.
func (s *InMemoryStore) GetStats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, partition := range s.events {
		total += len(partition)
	}
	return StoreStats{
		TotalEvents: total,
		DataDir:     ":memory:",
	}
}

// GetParent returns the parent EventKey for the given event key.
func (s *InMemoryStore) GetParent(key int64) (int64, error) {
	return s.rel.GetParent(key)
}

// GetChildren returns all direct child EventKeys for the given event key.
func (s *InMemoryStore) GetChildren(key int64) ([]int64, error) {
	return s.rel.GetChildren(key)
}

// SetParent sets the parent EventKey for the given event key.
func (s *InMemoryStore) SetParent(key int64, parentKey int64) error {
	return s.rel.SetParent(key, parentKey)
}

// RelationStore returns the underlying RelationStore for relationship operations.
// This is used by higher-level components (e.g., plugin) to manage
// parent-child relationships independently of CRUD operations.
func (s *InMemoryStore) RelationStore() RelationStore {
	return s.rel
}

// AllEvents returns all stored events (for testing/debugging).
func (s *InMemoryStore) AllEvents() []FullEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []FullEvent
	for _, partition := range s.events {
		for _, event := range partition {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})
	return result
}

// AllEventsByPartition returns events for a specific partition (for testing/debugging).
func (s *InMemoryStore) AllEventsByPartition(partitionID int) []FullEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	partition, ok := s.events[partitionID]
	if !ok {
		return nil
	}
	result := make([]FullEvent, 0, len(partition))
	for _, event := range partition {
		result = append(result, event)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})
	return result
}

// SearchByEmbedding performs semantic search (stub — not supported).
func (s *InMemoryStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
	return nil, ErrVectorSearchNotSupported
}

// StoreEventWithEmbedding stores event with embedding (stub — ignores embedding).
func (s *InMemoryStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
	return s.StoreEvent(key, event)
}

// SupportsVectorSearch returns false for InMemoryStore.
func (s *InMemoryStore) SupportsVectorSearch() bool {
	return false
}

func containsIgnoreCase(s, substr string) bool {
	return contains(toLower(s), toLower(substr))
}

func toLower(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result = append(result, c)
	}
	return string(result)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ==================== simpleInMemRelationStore（内存关系存储，无持久化） ====================

// simpleInMemRelationStore 是一个内存关系存储实现，用于 InMemoryStore 默认场景。
// 相比 InMemRelationStore，它不提供 WAL journal、快照和崩溃恢复，仅用于测试和原型开发。
type simpleInMemRelationStore struct {
	mu               sync.RWMutex
	childToParent    map[int64]int64
	parentToChildren map[int64][]int64
}

func newSimpleInMemRelationStore() *simpleInMemRelationStore {
	return &simpleInMemRelationStore{
		childToParent:    make(map[int64]int64),
		parentToChildren: make(map[int64][]int64),
	}
}

func (s *simpleInMemRelationStore) SetParent(childKey, parentKey int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldParent, hadOld := s.childToParent[childKey]
	if hadOld && oldParent == parentKey {
		return nil
	}
	if hadOld {
		s.removeFromChildren(oldParent, childKey)
	}
	s.childToParent[childKey] = parentKey
	s.parentToChildren[parentKey] = append(s.parentToChildren[parentKey], childKey)
	return nil
}

func (s *simpleInMemRelationStore) GetParent(childKey int64) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	parentKey, ok := s.childToParent[childKey]
	if !ok {
		return 0, nil
	}
	return parentKey, nil
}

func (s *simpleInMemRelationStore) GetChildren(parentKey int64) ([]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	children, ok := s.parentToChildren[parentKey]
	if !ok || len(children) == 0 {
		return []int64{}, nil
	}
	result := make([]int64, len(children))
	copy(result, children)
	return result, nil
}

func (s *simpleInMemRelationStore) GetParents(keys []int64) (map[int64]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[int64]int64, len(keys))
	for _, key := range keys {
		if parentKey, ok := s.childToParent[key]; ok {
			result[key] = parentKey
		} else {
			result[key] = 0
		}
	}
	return result, nil
}

func (s *simpleInMemRelationStore) RemoveRelations(key int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if parentKey, ok := s.childToParent[key]; ok {
		s.removeFromChildren(parentKey, key)
	}
	delete(s.childToParent, key)
	delete(s.parentToChildren, key)
	return nil
}

func (s *simpleInMemRelationStore) Snapshot() (map[int64]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make(map[int64]int64, len(s.childToParent))
	for k, v := range s.childToParent {
		snapshot[k] = v
	}
	return snapshot, nil
}

func (s *simpleInMemRelationStore) LoadSnapshot(data map[int64]int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.childToParent = make(map[int64]int64, len(data))
	s.parentToChildren = make(map[int64][]int64)
	for child, parent := range data {
		s.childToParent[child] = parent
		s.parentToChildren[parent] = append(s.parentToChildren[parent], child)
	}
	return nil
}

func (s *simpleInMemRelationStore) ReplayJournal(entries []JournalEntry) error {
	return nil
}

func (s *simpleInMemRelationStore) EventsCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.childToParent)
}

func (s *simpleInMemRelationStore) removeFromChildren(parentKey, childKey int64) {
	children, ok := s.parentToChildren[parentKey]
	if !ok {
		return
	}
	for i, c := range children {
		if c == childKey {
			s.parentToChildren[parentKey] = append(children[:i], children[i+1:]...)
			break
		}
	}
	if len(s.parentToChildren[parentKey]) == 0 {
		delete(s.parentToChildren, parentKey)
	}
}

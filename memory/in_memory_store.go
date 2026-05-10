package memory

import (
	"fmt"
	"sort"
	"sync"
)

// InMemoryStore implements MemoryStore using an in-memory map.
// Events are partitioned by PartitionID for storage isolation.
// Suitable for testing and prototyping.
type InMemoryStore struct {
	mu     sync.RWMutex
	events map[int]map[int64]FullEvent // PartitionID → EventKey → FullEvent
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		events: make(map[int]map[int64]FullEvent),
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

// StoreEvents stores multiple events in batch.
func (s *InMemoryStore) StoreEvents(events map[int64]FullEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, event := range events {
		event.EventKey = key
		pid := event.PartitionID
		if pid == 0 {
			pid = PartitionIDFromEventKey(key)
			event.PartitionID = pid
		}
		if s.events[pid] == nil {
			s.events[pid] = make(map[int64]FullEvent)
		}
		s.events[pid][key] = event
	}
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

	// Sort by timestamp
	sort.Slice(matched, func(i, j int) bool {
		if query.OrderBy == "timestamp_desc" {
			return matched[i].Timestamp > matched[j].Timestamp
		}
		return matched[i].Timestamp < matched[j].Timestamp
	})

	// Apply offset and limit
	start := 0
	if query.Offset > 0 {
		start = query.Offset
	}
	if start > len(matched) {
		return nil, nil
	}
	matched = matched[start:]
	if query.Limit > 0 && len(matched) > query.Limit {
		matched = matched[:query.Limit]
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
	// Filter by keyword (case-insensitive match against EventSummary or Content)
	if query.Keyword != "" {
		if !containsIgnoreCase(event.EventSummary, query.Keyword) &&
			!containsIgnoreCase(event.Content, query.Keyword) {
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

// SearchBySummary searches events by matching against EventSummary.
// Returns events whose summary contains the query string (case-insensitive).
// Deprecated: Use QueryEvents with QueryOptions.Keyword instead.
func (s *InMemoryStore) SearchBySummary(query string) []EventReference {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []EventReference
	for _, partition := range s.events {
		for _, event := range partition {
			if containsIgnoreCase(event.EventSummary, query) || containsIgnoreCase(event.Content, query) {
				results = append(results, EventReference{
					EventKey:     event.EventKey,
					PartitionID:  event.PartitionID,
					EventType:    event.EventType,
					EventSummary: event.EventSummary,
					Timestamp:    event.Timestamp,
				})
			}
		}
	}
	return results
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

package memory

import (
	"fmt"
	"sort"
	"sync"
)

// InMemoryStore implements MemoryStore using an in-memory map.
// Suitable for testing and prototyping.
type InMemoryStore struct {
	mu     sync.RWMutex
	events map[string]FullEvent
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		events: make(map[string]FullEvent),
	}
}

// StoreEvent stores a single event.
func (s *InMemoryStore) StoreEvent(key string, event FullEvent) error {
	if key == "" {
		return fmt.Errorf("event key cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event.EventKey = key
	s.events[key] = event
	return nil
}

// StoreEvents stores multiple events in batch.
func (s *InMemoryStore) StoreEvents(events map[string]FullEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, event := range events {
		event.EventKey = key
		s.events[key] = event
	}
	return nil
}

// GetEvent retrieves a single event by its EventKey.
func (s *InMemoryStore) GetEvent(key string) (*FullEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	event, ok := s.events[key]
	if !ok {
		return nil, fmt.Errorf("event not found: %s", key)
	}
	return &event, nil
}

// GetEvents retrieves multiple events by their EventKeys.
func (s *InMemoryStore) GetEvents(keys []string) ([]FullEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []FullEvent
	for _, key := range keys {
		if event, ok := s.events[key]; ok {
			results = append(results, event)
		}
	}
	return results, nil
}

// QueryEvents queries events based on filters.
func (s *InMemoryStore) QueryEvents(query QueryOptions) ([]EventReference, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect matching events into a slice for sorting
	var matched []FullEvent
	for _, event := range s.events {
		if len(query.EventTypes) > 0 {
			found := false
			for _, et := range query.EventTypes {
				if event.EventType == et {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if query.StartTime > 0 && event.Timestamp < query.StartTime {
			continue
		}
		if query.EndTime > 0 && event.Timestamp > query.EndTime {
			continue
		}
		matched = append(matched, event)
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
			EventType:    event.EventType,
			EventSummary: event.EventSummary,
			Timestamp:    event.Timestamp,
		})
	}
	return results, nil
}

// DeleteEvent permanently deletes an event.
func (s *InMemoryStore) DeleteEvent(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.events, key)
	return nil
}

// GetStats returns storage statistics.
func (s *InMemoryStore) GetStats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return StoreStats{
		TotalEvents: len(s.events),
		DataDir:     ":memory:",
	}
}

// SearchBySummary searches events by matching against EventSummary.
// Returns events whose summary contains the query string (case-insensitive).
func (s *InMemoryStore) SearchBySummary(query string) []EventReference {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []EventReference
	for _, event := range s.events {
		if containsIgnoreCase(event.EventSummary, query) || containsIgnoreCase(event.Content, query) {
			results = append(results, EventReference{
				EventKey:     event.EventKey,
				EventType:    event.EventType,
				EventSummary: event.EventSummary,
				Timestamp:    event.Timestamp,
			})
		}
	}
	return results
}

// AllEvents returns all stored events (for testing/debugging).
func (s *InMemoryStore) AllEvents() []FullEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []FullEvent
	for _, event := range s.events {
		result = append(result, event)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp < result[j].Timestamp
	})
	return result
}

func containsIgnoreCase(s, substr string) bool {
	// Simple case-insensitive substring search
	sLower := toLower(s)
	substrLower := toLower(substr)
	return contains(sLower, substrLower)
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

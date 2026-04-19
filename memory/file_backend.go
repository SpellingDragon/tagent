package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileBackend implements MemoryStore using local file system.
// Each event is stored as a separate JSON file.
// This is suitable for prototyping and small-scale usage.
type FileBackend struct {
	dataDir string
	mu      sync.RWMutex
}

// NewFileBackend creates a new FileBackend with the specified data directory.
func NewFileBackend(dataDir string) (*FileBackend, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	return &FileBackend{
		dataDir: dataDir,
	}, nil
}

// StoreEvent stores a single event as a JSON file.
func (b *FileBackend) StoreEvent(key string, event FullEvent) error {
	if key == "" {
		return fmt.Errorf("event key cannot be empty")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Ensure EventKey matches the file key
	event.EventKey = key

	// Marshal event to JSON
	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal event %s: %w", key, err)
	}

	// Write to file: {dataDir}/{key}.json
	path := filepath.Join(b.dataDir, key+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write event file %s: %w", path, err)
	}

	return nil
}

// StoreEvents stores multiple events in batch.
func (b *FileBackend) StoreEvents(events map[string]FullEvent) error {
	if len(events) == 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	var firstErr error
	for key, event := range events {
		// Ensure EventKey matches
		event.EventKey = key

		data, err := json.MarshalIndent(event, "", "  ")
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to marshal event %s: %w", key, err)
			}
			continue
		}

		path := filepath.Join(b.dataDir, key+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to write event file %s: %w", path, err)
			}
		}
	}

	return firstErr
}

// GetEvent retrieves a single event by its EventKey.
func (b *FileBackend) GetEvent(key string) (*FullEvent, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	path := filepath.Join(b.dataDir, key+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("event %s not found", key)
		}
		return nil, fmt.Errorf("failed to read event file %s: %w", path, err)
	}

	var event FullEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event %s: %w", key, err)
	}

	return &event, nil
}

// GetEvents retrieves multiple events by their EventKeys.
// Returns events in the same order as keys.
// Skips keys that don't exist (no error).
func (b *FileBackend) GetEvents(keys []string) ([]FullEvent, error) {
	if len(keys) == 0 {
		return []FullEvent{}, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	events := make([]FullEvent, 0, len(keys))

	for _, key := range keys {
		path := filepath.Join(b.dataDir, key+".json")

		data, err := os.ReadFile(path)
		if err != nil {
			// Skip non-existent events (no error)
			continue
		}

		var event FullEvent
		if err := json.Unmarshal(data, &event); err != nil {
			// Skip malformed events
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

// QueryEvents queries events based on filters.
// Returns EventReference list (lightweight).
func (b *FileBackend) QueryEvents(query QueryOptions) ([]EventReference, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Read all event files
	entries, err := os.ReadDir(b.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	var refs []EventReference

	for _, entry := range entries {
		// Only process .json files
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Extract EventKey from filename
		eventKey := strings.TrimSuffix(entry.Name(), ".json")

		// Read just enough to get metadata
		path := filepath.Join(b.dataDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Unmarshal only the fields we need for filtering
		var meta struct {
			EventType    string `json:"event_type"`
			EventSummary string `json:"event_summary"`
			Timestamp    int64  `json:"timestamp"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		// Apply filters
		if !b.matchesQuery(meta, query) {
			continue
		}

		refs = append(refs, EventReference{
			EventKey:     eventKey,
			EventType:    meta.EventType,
			EventSummary: meta.EventSummary,
			Timestamp:    meta.Timestamp,
		})
	}

	// Apply sorting
	b.sortReferences(refs, query)

	// Apply pagination
	return b.paginateReferences(refs, query), nil
}

// DeleteEvent permanently deletes an event from storage.
func (b *FileBackend) DeleteEvent(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	path := filepath.Join(b.dataDir, key+".json")

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("event %s not found", key)
		}
		return fmt.Errorf("failed to delete event file %s: %w", path, err)
	}

	return nil
}

// GetStats returns storage statistics.
func (b *FileBackend) GetStats() StoreStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var totalEvents int
	var totalSize int64

	entries, err := os.ReadDir(b.dataDir)
	if err != nil {
		return StoreStats{
			TotalEvents: 0,
			StorageSize: 0,
			DataDir:     b.dataDir,
		}
	}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			totalEvents++

			info, err := entry.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
	}

	return StoreStats{
		TotalEvents: totalEvents,
		StorageSize: totalSize,
		DataDir:     b.dataDir,
	}
}

// matchesQuery checks if an event matches the query filters.
func (b *FileBackend) matchesQuery(meta struct {
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
	Timestamp    int64  `json:"timestamp"`
}, query QueryOptions) bool {
	// Filter by event types
	if len(query.EventTypes) > 0 {
		found := false
		for _, t := range query.EventTypes {
			if meta.EventType == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by time range
	if query.StartTime > 0 && meta.Timestamp < query.StartTime {
		return false
	}
	if query.EndTime > 0 && meta.Timestamp > query.EndTime {
		return false
	}

	return true
}

// sortReferences sorts the event references based on query.OrderBy.
func (b *FileBackend) sortReferences(refs []EventReference, query QueryOptions) {
	switch query.OrderBy {
	case "timestamp_desc":
		sort.Slice(refs, func(i, j int) bool {
			return refs[i].Timestamp > refs[j].Timestamp
		})
	case "timestamp_asc", "":
		sort.Slice(refs, func(i, j int) bool {
			return refs[i].Timestamp < refs[j].Timestamp
		})
	}
}

// paginateReferences applies pagination to the event references.
func (b *FileBackend) paginateReferences(refs []EventReference, query QueryOptions) []EventReference {
	// Apply offset
	if query.Offset > 0 && query.Offset < len(refs) {
		refs = refs[query.Offset:]
	}

	// Apply limit
	if query.Limit > 0 && query.Limit < len(refs) {
		refs = refs[:query.Limit]
	}

	return refs
}

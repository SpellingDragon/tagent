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
// Events are stored in per-partition directories for storage isolation.
//
// Directory layout:
//
//	dataDir/
//	├── 0/              ← PartitionID=0 (e.g., top-level agent)
//	│   ├── 1234567890123456789.json
//	│   └── 1234567890123456790.json
//	├── 1/              ← PartitionID=1 (e.g., knowledge agent)
//	│   └── ...
//	└── 2/              ← PartitionID=2 (e.g., recall agent)
//	    └── ...
type FileBackend struct {
	dataDir string
	mu      sync.RWMutex
}

// NewFileBackend creates a new FileBackend with the specified data directory.
func NewFileBackend(dataDir string) (*FileBackend, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}
	return &FileBackend{dataDir: dataDir}, nil
}

// partitionDir returns the directory path for a partition.
func (b *FileBackend) partitionDir(partitionID int) string {
	return filepath.Join(b.dataDir, fmt.Sprintf("%d", partitionID))
}

// eventFilePath returns the file path for an event.
func (b *FileBackend) eventFilePath(partitionID int, key int64) string {
	return filepath.Join(b.partitionDir(partitionID), fmt.Sprintf("%d.json", key))
}

// StoreEvent stores a single event as a JSON file.
func (b *FileBackend) StoreEvent(key int64, event FullEvent) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	event.EventKey = key
	pid := event.PartitionID
	if pid == 0 {
		pid = PartitionIDFromEventKey(key)
		event.PartitionID = pid
	}

	// Ensure partition directory exists
	dir := b.partitionDir(pid)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create partition dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal event %d: %w", key, err)
	}

	path := b.eventFilePath(pid, key)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write event file %s: %w", path, err)
	}
	return nil
}

// StoreEvents stores multiple events in batch.
func (b *FileBackend) StoreEvents(events map[int64]FullEvent) error {
	if len(events) == 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	var firstErr error
	for key, event := range events {
		event.EventKey = key
		pid := event.PartitionID
		if pid == 0 {
			pid = PartitionIDFromEventKey(key)
			event.PartitionID = pid
		}

		dir := b.partitionDir(pid)
		if err := os.MkdirAll(dir, 0755); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to create partition dir %s: %w", dir, err)
			}
			continue
		}

		data, err := json.MarshalIndent(event, "", "  ")
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to marshal event %d: %w", key, err)
			}
			continue
		}

		path := b.eventFilePath(pid, key)
		if err := os.WriteFile(path, data, 0644); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to write event file %s: %w", path, err)
			}
		}
	}
	return firstErr
}

// GetEvent retrieves a single event by its EventKey.
func (b *FileBackend) GetEvent(key int64) (*FullEvent, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	pid := PartitionIDFromEventKey(key)
	path := b.eventFilePath(pid, key)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("event %d not found", key)
		}
		return nil, fmt.Errorf("failed to read event file %s: %w", path, err)
	}

	var event FullEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event %d: %w", key, err)
	}
	return &event, nil
}

// GetEvents retrieves multiple events by their EventKeys.
func (b *FileBackend) GetEvents(keys []int64) ([]FullEvent, error) {
	if len(keys) == 0 {
		return []FullEvent{}, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	events := make([]FullEvent, 0, len(keys))
	for _, key := range keys {
		pid := PartitionIDFromEventKey(key)
		path := b.eventFilePath(pid, key)

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var event FullEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

// QueryEvents queries events based on filters.
func (b *FileBackend) QueryEvents(query QueryOptions) ([]EventReference, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	partitions := b.resolvePartitions(query)

	var refs []EventReference
	for _, pid := range partitions {
		dir := b.partitionDir(pid)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read partition dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var meta struct {
				EventKey     int64  `json:"event_key"`
				PartitionID  int    `json:"partition_id"`
				EventType    string `json:"event_type"`
				EventSummary string `json:"event_summary"`
				Timestamp    int64  `json:"timestamp"`
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				continue
			}

			if !b.matchesQuery(meta, query) {
				continue
			}

			refs = append(refs, EventReference{
				EventKey:     meta.EventKey,
				PartitionID:  meta.PartitionID,
				EventType:    meta.EventType,
				EventSummary: meta.EventSummary,
				Timestamp:    meta.Timestamp,
			})
		}
	}

	b.sortReferences(refs, query)
	return b.paginateReferences(refs, query), nil
}

// resolvePartitions determines which partition directories to search.
func (b *FileBackend) resolvePartitions(query QueryOptions) []int {
	if len(query.PartitionIDs) > 0 {
		return query.PartitionIDs
	}
	if query.PartitionID > 0 {
		return []int{query.PartitionID}
	}
	// All partitions: list subdirectories
	entries, err := os.ReadDir(b.dataDir)
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(entry.Name(), "%d", &pid); err == nil {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids
}

// matchesQuery checks if an event matches the query filters.
func (b *FileBackend) matchesQuery(meta struct {
	EventKey     int64  `json:"event_key"`
	PartitionID  int    `json:"partition_id"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
	Timestamp    int64  `json:"timestamp"`
}, query QueryOptions) bool {
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
	if query.StartTime > 0 && meta.Timestamp < query.StartTime {
		return false
	}
	if query.EndTime > 0 && meta.Timestamp > query.EndTime {
		return false
	}
	return true
}

// DeleteEvent permanently deletes an event from storage.
func (b *FileBackend) DeleteEvent(key int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	pid := PartitionIDFromEventKey(key)
	path := b.eventFilePath(pid, key)

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("event %d not found", key)
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
		return StoreStats{DataDir: b.dataDir}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		partitionDir := filepath.Join(b.dataDir, entry.Name())
		files, err := os.ReadDir(partitionDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".json") {
				totalEvents++
				if info, err := f.Info(); err == nil {
					totalSize += info.Size()
				}
			}
		}
	}

	return StoreStats{
		TotalEvents: totalEvents,
		StorageSize: totalSize,
		DataDir:     b.dataDir,
	}
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
	if query.Offset > 0 && query.Offset < len(refs) {
		refs = refs[query.Offset:]
	}
	if query.Limit > 0 && query.Limit < len(refs) {
		refs = refs[:query.Limit]
	}
	return refs
}

// SearchByEmbedding performs semantic search (stub — not supported).
func (b *FileBackend) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
	return nil, ErrVectorSearchNotSupported
}

// StoreEventWithEmbedding stores event with embedding (stub — ignores embedding).
func (b *FileBackend) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
	return b.StoreEvent(key, event)
}

// SupportsVectorSearch returns false for FileBackend.
func (b *FileBackend) SupportsVectorSearch() bool {
	return false
}

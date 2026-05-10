package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFileBackend_StoreAndGetEvent(t *testing.T) {
	tempDir := t.TempDir()

	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	// Create test event with PartitionID
	partitionID := PartitionIDFromName("test-agent")
	eventKey := NewSnowflakeEventKey(partitionID, 1710678000000)

	evt := FullEvent{
		PartitionID: partitionID,
		EventType:   EventTypeExternalInput,
		Timestamp:   1710678000000,
		Content:     "Test message",
	}

	// Store event
	err = backend.StoreEvent(eventKey, evt)
	if err != nil {
		t.Fatalf("Failed to store event: %v", err)
	}

	// Verify partition directory was created
	partDir := filepath.Join(tempDir, fmt.Sprintf("%d", partitionID))
	if _, err := os.Stat(partDir); os.IsNotExist(err) {
		t.Fatalf("Partition directory was not created: %s", partDir)
	}

	// Get event
	retrieved, err := backend.GetEvent(eventKey)
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}

	// Verify event content
	if retrieved.EventType != evt.EventType {
		t.Errorf("Expected event type %s, got %s", evt.EventType, retrieved.EventType)
	}
	if retrieved.Content != evt.Content {
		t.Errorf("Expected content %s, got %s", evt.Content, retrieved.Content)
	}
	if retrieved.PartitionID != partitionID {
		t.Errorf("Expected partitionID %d, got %d", partitionID, retrieved.PartitionID)
	}
}

func TestFileBackend_StoreEvents(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("batch-test")

	// Create multiple events
	events := map[int64]FullEvent{
		NewSnowflakeEventKey(partitionID, 1710678000000): {
			PartitionID: partitionID,
			EventType:   EventTypeExternalInput,
			Content:     "Message 1",
			Timestamp:   1710678000000,
		},
		NewSnowflakeEventKey(partitionID, 1710678001000): {
			PartitionID: partitionID,
			EventType:   EventTypeAgentOutput,
			Content:     "Message 2",
			Timestamp:   1710678001000,
		},
		NewSnowflakeEventKey(partitionID, 1710678002000): {
			PartitionID: partitionID,
			EventType:   EventTypeActionCommand,
			Content:     "Message 3",
			Timestamp:   1710678002000,
		},
	}

	// Store events
	err = backend.StoreEvents(events)
	if err != nil {
		t.Fatalf("Failed to store events: %v", err)
	}

	// Get all events
	keys := make([]int64, 0, len(events))
	for k := range events {
		keys = append(keys, k)
	}
	retrieved, err := backend.GetEvents(keys)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("Expected 3 events, got %d", len(retrieved))
	}
}

func TestFileBackend_GetEvents_SkipMissing(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("skip-test")
	key1 := NewSnowflakeEventKey(partitionID, 1710678000000)

	// Store one event
	backend.StoreEvent(key1, FullEvent{
		PartitionID: partitionID,
		Content:     "Test",
		Timestamp:   1710678000000,
	})

	// Try to get multiple events (some missing)
	keys := []int64{key1, 9999999999, 8888888888}
	retrieved, err := backend.GetEvents(keys)
	if err != nil {
		t.Fatalf("GetEvents should not fail for missing keys: %v", err)
	}

	// Should only return the existing event
	if len(retrieved) != 1 {
		t.Errorf("Expected 1 event, got %d", len(retrieved))
	}
}

func TestFileBackend_QueryEvents(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("query-test")

	// Store events with different types and timestamps
	events := map[int64]FullEvent{
		NewSnowflakeEventKey(partitionID, 1710678000000): {
			PartitionID: partitionID,
			EventType:   EventTypeExternalInput,
			Content:     "User message 1",
			Timestamp:   1710678000000,
		},
		NewSnowflakeEventKey(partitionID, 1710678001000): {
			PartitionID: partitionID,
			EventType:   EventTypeAgentOutput,
			Content:     "Agent response 1",
			Timestamp:   1710678001000,
		},
		NewSnowflakeEventKey(partitionID, 1710678002000): {
			PartitionID: partitionID,
			EventType:   EventTypeExternalInput,
			Content:     "User message 2",
			Timestamp:   1710678002000,
		},
	}

	backend.StoreEvents(events)

	// Query all events
	refs, err := backend.QueryEvents(QueryOptions{
		OrderBy: "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events: %v", err)
	}

	if len(refs) != 3 {
		t.Errorf("Expected 3 events, got %d", len(refs))
	}

	// Query by type
	refs, err = backend.QueryEvents(QueryOptions{
		EventTypes: []string{EventTypeExternalInput},
		OrderBy:    "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events by type: %v", err)
	}

	if len(refs) != 2 {
		t.Errorf("Expected 2 external_input events, got %d", len(refs))
	}

	// Query by partition
	refs, err = backend.QueryEvents(QueryOptions{
		PartitionID: partitionID,
		OrderBy:     "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events by partition: %v", err)
	}

	if len(refs) != 3 {
		t.Errorf("Expected 3 events for partition %d, got %d", partitionID, len(refs))
	}
}

func TestFileBackend_QueryEvents_TimeFilter(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("time-test")

	events := map[int64]FullEvent{
		NewSnowflakeEventKey(partitionID, 1710678000000): {PartitionID: partitionID, Timestamp: 1710678000000},
		NewSnowflakeEventKey(partitionID, 1710678001000): {PartitionID: partitionID, Timestamp: 1710678001000},
		NewSnowflakeEventKey(partitionID, 1710678002000): {PartitionID: partitionID, Timestamp: 1710678002000},
	}

	backend.StoreEvents(events)

	// Query with time range
	refs, err := backend.QueryEvents(QueryOptions{
		StartTime: 1710678001000,
		EndTime:   1710678002000,
		OrderBy:   "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events with time filter: %v", err)
	}

	if len(refs) != 2 {
		t.Errorf("Expected 2 events in time range, got %d", len(refs))
	}
}

func TestFileBackend_QueryEvents_Pagination(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("page-test")

	// Store 5 events
	for i := 1; i <= 5; i++ {
		ts := int64(1710678000000 + i*1000)
		key := NewSnowflakeEventKey(partitionID, ts)
		backend.StoreEvent(key, FullEvent{
			PartitionID: partitionID,
			Timestamp:   ts,
		})
	}

	// Query with limit
	refs, err := backend.QueryEvents(QueryOptions{
		Limit:   2,
		OrderBy: "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events with limit: %v", err)
	}

	if len(refs) != 2 {
		t.Errorf("Expected 2 events with limit, got %d", len(refs))
	}

	// Query with offset
	refs, err = backend.QueryEvents(QueryOptions{
		Offset:  2,
		Limit:   2,
		OrderBy: "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events with offset: %v", err)
	}

	if len(refs) != 2 {
		t.Errorf("Expected 2 events with offset, got %d", len(refs))
	}
}

func TestFileBackend_DeleteEvent(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("delete-test")
	key := NewSnowflakeEventKey(partitionID, 1710678000000)

	// Store event
	backend.StoreEvent(key, FullEvent{
		PartitionID: partitionID,
		Content:     "Test",
		Timestamp:   1710678000000,
	})

	// Delete event
	err = backend.DeleteEvent(key)
	if err != nil {
		t.Fatalf("Failed to delete event: %v", err)
	}

	// Verify event is deleted
	_, err = backend.GetEvent(key)
	if err == nil {
		t.Error("Expected error after deleting event, got nil")
	}
}

func TestFileBackend_GetStats(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("stats-test")

	// Store some events
	for i := 1; i <= 3; i++ {
		ts := int64(1710678000000 + i*1000)
		key := NewSnowflakeEventKey(partitionID, ts)
		backend.StoreEvent(key, FullEvent{
			PartitionID: partitionID,
			Content:     "Test",
			Timestamp:   ts,
		})
	}

	// Get stats
	stats := backend.GetStats()

	if stats.TotalEvents != 3 {
		t.Errorf("Expected 3 events, got %d", stats.TotalEvents)
	}
	if stats.DataDir != tempDir {
		t.Errorf("Expected data dir %s, got %s", tempDir, stats.DataDir)
	}
	if stats.StorageSize == 0 {
		t.Error("Expected non-zero storage size")
	}
}

func TestSnowflakeEventKey(t *testing.T) {
	partitionID := PartitionIDFromName("snowflake-test")

	key := NewSnowflakeEventKey(partitionID, 1710678000000)

	// Extract PartitionID from key
	extractedPID := PartitionIDFromEventKey(key)
	if extractedPID != partitionID {
		t.Errorf("Expected partitionID %d, got %d", partitionID, extractedPID)
	}

	// Extract timestamp
	ts := TimestampFromEventKey(key)
	if ts == 0 {
		t.Error("Expected non-zero timestamp")
	}

	// Keys with different timestamps should be different
	key2 := NewSnowflakeEventKey(partitionID, 1710678001000)
	if key == key2 {
		t.Error("Expected different keys for different timestamps")
	}
}

func TestPartitionIDFromName(t *testing.T) {
	// Same name → same PartitionID (deterministic)
	pid1 := PartitionIDFromName("tagent")
	pid2 := PartitionIDFromName("tagent")
	if pid1 != pid2 {
		t.Errorf("Expected same PartitionID for same name, got %d and %d", pid1, pid2)
	}

	// Different names → (likely) different PartitionIDs
	pid3 := PartitionIDFromName("knowledge")
	if pid1 == pid3 {
		t.Log("Warning: FNV-1a collision for 'tagent' and 'knowledge' — acceptable but unlikely")
	}

	// PartitionID should be in valid range [0, 2047]
	if pid1 < 0 || pid1 > 2047 {
		t.Errorf("PartitionID %d out of valid range [0, 2047]", pid1)
	}
}

func TestNewPartitionID(t *testing.T) {
	pid1 := NewPartitionID()
	pid2 := NewPartitionID()

	// Should be unique
	if pid1 == pid2 {
		t.Errorf("Expected unique PartitionIDs, got same value %d", pid1)
	}

	// Should be in valid range
	if pid1 < 0 || pid1 > 2047 {
		t.Errorf("PartitionID %d out of valid range [0, 2047]", pid1)
	}
}

func TestFileBackend_QueryEvents_KeywordFilter(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	partitionID := PartitionIDFromName("keyword-test")

	events := map[int64]FullEvent{
		NewSnowflakeEventKey(partitionID, 1710678000000): {
			PartitionID:  partitionID,
			EventType:    EventTypeExternalInput,
			EventSummary: "部署 nginx 到生产环境",
			Content:      "用户请求部署 nginx 到生产环境",
			Timestamp:    1710678000000,
		},
		NewSnowflakeEventKey(partitionID, 1710678001000): {
			PartitionID:  partitionID,
			EventType:    EventTypeAgentOutput,
			EventSummary: "数据库迁移完成",
			Content:      "数据库从 MySQL 5.7 迁移到 8.0 已完成",
			Timestamp:    1710678001000,
		},
		NewSnowflakeEventKey(partitionID, 1710678002000): {
			PartitionID:  partitionID,
			EventType:    EventTypeExternalInput,
			EventSummary: "重新部署 nginx",
			Content:      "nginx 配置需要修改后重新部署",
			Timestamp:    1710678002000,
		},
	}

	backend.StoreEvents(events)

	// Query with keyword matching "部署" in EventSummary or Content
	refs, err := backend.QueryEvents(QueryOptions{
		Keyword: "部署",
		OrderBy: "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events by keyword: %v", err)
	}

	if len(refs) != 2 {
		t.Errorf("Expected 2 events matching '部署', got %d", len(refs))
	}

	// Query with keyword matching only content
	refs, err = backend.QueryEvents(QueryOptions{
		Keyword: "MySQL",
		OrderBy: "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events by keyword: %v", err)
	}

	if len(refs) != 1 {
		t.Errorf("Expected 1 event matching 'MySQL', got %d", len(refs))
	}

	// Query with keyword that matches nothing
	refs, err = backend.QueryEvents(QueryOptions{
		Keyword: "Kubernetes",
		OrderBy: "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events by keyword: %v", err)
	}

	if len(refs) != 0 {
		t.Errorf("Expected 0 events matching 'Kubernetes', got %d", len(refs))
	}

	// Case-insensitive test
	refs, err = backend.QueryEvents(QueryOptions{
		Keyword: "mysql",
		OrderBy: "timestamp_asc",
	})
	if err != nil {
		t.Fatalf("Failed to query events by keyword: %v", err)
	}

	if len(refs) != 1 {
		t.Errorf("Expected 1 event matching 'mysql' (case-insensitive), got %d", len(refs))
	}
}

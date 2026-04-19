package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestFileBackend_StoreAndGetEvent(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	// Create test event
	event := FullEvent{
		EventType: EventTypeExternalInput,
		Timestamp: 1710678000000,
		Content:   "Test message",
		Response: &model.Response{
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleUser,
						Content: "Test message",
					},
				},
			},
		},
	}

	// Store event
	eventKey := "evt_1710678000000_001"
	err = backend.StoreEvent(eventKey, event)
	if err != nil {
		t.Fatalf("Failed to store event: %v", err)
	}

	// Verify file was created
	path := filepath.Join(tempDir, eventKey+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Event file was not created: %s", path)
	}

	// Get event
	retrieved, err := backend.GetEvent(eventKey)
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}

	// Verify event content
	if retrieved.EventType != event.EventType {
		t.Errorf("Expected event type %s, got %s", event.EventType, retrieved.EventType)
	}
	if retrieved.Content != event.Content {
		t.Errorf("Expected content %s, got %s", event.Content, retrieved.Content)
	}
	if retrieved.Timestamp != event.Timestamp {
		t.Errorf("Expected timestamp %d, got %d", event.Timestamp, retrieved.Timestamp)
	}
}

func TestFileBackend_StoreEvents(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	// Create multiple events
	events := map[string]FullEvent{
		"evt_001": {
			EventType: EventTypeExternalInput,
			Content:   "Message 1",
			Timestamp: 1710678000000,
		},
		"evt_002": {
			EventType: EventTypeAgentOutput,
			Content:   "Message 2",
			Timestamp: 1710678001000,
		},
		"evt_003": {
			EventType: EventTypeActionCommand,
			Content:   "Message 3",
			Timestamp: 1710678002000,
		},
	}

	// Store events
	err = backend.StoreEvents(events)
	if err != nil {
		t.Fatalf("Failed to store events: %v", err)
	}

	// Get all events
	keys := []string{"evt_001", "evt_002", "evt_003"}
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

	// Store one event
	backend.StoreEvent("evt_001", FullEvent{Content: "Test", Timestamp: 1710678000000})

	// Try to get multiple events (some missing)
	keys := []string{"evt_001", "evt_002", "evt_003"}
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

	// Store events with different types and timestamps
	events := map[string]FullEvent{
		"evt_001": {
			EventType: EventTypeExternalInput,
			Content:   "User message 1",
			Timestamp: 1710678000000,
		},
		"evt_002": {
			EventType: EventTypeAgentOutput,
			Content:   "Agent response 1",
			Timestamp: 1710678001000,
		},
		"evt_003": {
			EventType: EventTypeExternalInput,
			Content:   "User message 2",
			Timestamp: 1710678002000,
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
}

func TestFileBackend_QueryEvents_TimeFilter(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create FileBackend: %v", err)
	}

	// Store events with different timestamps
	events := map[string]FullEvent{
		"evt_001": {Timestamp: 1710678000000},
		"evt_002": {Timestamp: 1710678001000},
		"evt_003": {Timestamp: 1710678002000},
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

	// Store 5 events
	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("evt_%03d", i)
		backend.StoreEvent(key, FullEvent{
			Timestamp: int64(1710678000000 + i*1000),
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

	// Store event
	backend.StoreEvent("evt_001", FullEvent{Content: "Test"})

	// Delete event
	err = backend.DeleteEvent("evt_001")
	if err != nil {
		t.Fatalf("Failed to delete event: %v", err)
	}

	// Verify event is deleted
	_, err = backend.GetEvent("evt_001")
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

	// Store some events
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("evt_%03d", i)
		backend.StoreEvent(key, FullEvent{Content: "Test"})
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

func TestNewEventKey(t *testing.T) {
	// Test key format
	key1 := NewEventKey(1710678000000, 1)
	if key1 != "evt_1710678000000_001" {
		t.Errorf("Expected evt_1710678000000_001, got %s", key1)
	}

	key2 := NewEventKey(1710678001000, 42)
	if key2 != "evt_1710678001000_042" {
		t.Errorf("Expected evt_1710678001000_042, got %s", key2)
	}

	// Test that keys with different timestamps are different
	key3 := NewEventKey(1710678002000, 1)
	if key1 == key3 {
		t.Error("Expected different keys for different timestamps")
	}
}

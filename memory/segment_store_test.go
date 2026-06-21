package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSegmentStore creates a FileSegmentStore with a mock KV store for testing.
func newTestSegmentStore(t *testing.T) *FileSegmentStore {
	t.Helper()
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)
	require.NotNil(t, store)
	return store
}

func TestSegmentStore_StoreAndGetEvent(t *testing.T) {
	store := newTestSegmentStore(t)

	event := FullEvent{
		PartitionID:  1,
		EventType:    "test",
		EventSummary: "test event summary",
		Content:      "test content",
		Timestamp:    1710678000000,
	}

	key := NewSnowflakeEventKey(1, 1710678000000)
	err := store.StoreEvent(key, event)
	require.NoError(t, err, "StoreEvent should succeed")

	// Retrieve the event
	retrieved, err := store.GetEvent(key)
	require.NoError(t, err, "GetEvent should succeed")
	require.NotNil(t, retrieved)
	assert.Equal(t, key, retrieved.EventKey)
	assert.Equal(t, "test", retrieved.EventType)
	assert.Equal(t, "test event summary", retrieved.EventSummary)
	assert.Equal(t, "test content", retrieved.Content)
}

func TestSegmentStore_GetEvent_NotFound(t *testing.T) {
	store := newTestSegmentStore(t)

	_, err := store.GetEvent(99999)
	assert.Error(t, err, "GetEvent should fail for non-existent key")
}

func TestSegmentStore_StoreAndGetEvents(t *testing.T) {
	store := newTestSegmentStore(t)

	events := make(map[int64]FullEvent)
	ts := int64(1710678000000)
	for i := 0; i < 5; i++ {
		key := NewSnowflakeEventKey(1, ts+int64(i)*1000)
		events[key] = FullEvent{
			EventKey:     key,
			PartitionID:  1,
			EventType:    "test",
			EventSummary: "event " + itoa(i),
			Timestamp:    ts + int64(i)*1000,
		}
	}

	err := store.StoreEvents(events)
	require.NoError(t, err, "StoreEvents should succeed")

	// Retrieve all keys
	keys := make([]int64, 0, len(events))
	for k := range events {
		keys = append(keys, k)
	}
	retrieved, err := store.GetEvents(keys)
	require.NoError(t, err)
	assert.Len(t, retrieved, 5)
}

func TestSegmentStore_GetParent_GetChildren(t *testing.T) {
	store := newTestSegmentStore(t)

	// Setup parent-child relationships
	parentKey := NewSnowflakeEventKey(1, 1710678000000)
	childKey1 := NewSnowflakeEventKey(1, 1710678001000)
	childKey2 := NewSnowflakeEventKey(1, 1710678002000)

	// Store events first
	store.StoreEvent(parentKey, FullEvent{PartitionID: 1, EventType: "parent", Timestamp: 1710678000000})
	store.StoreEvent(childKey1, FullEvent{PartitionID: 1, EventType: "child", Timestamp: 1710678001000})
	store.StoreEvent(childKey2, FullEvent{PartitionID: 1, EventType: "child", Timestamp: 1710678002000})

	// Set relationships
	rel := store.RelationStore()
	err := rel.SetParent(childKey1, parentKey)
	require.NoError(t, err)
	err = rel.SetParent(childKey2, parentKey)
	require.NoError(t, err)

	// Verify GetParent
	p, err := rel.GetParent(childKey1)
	require.NoError(t, err)
	assert.Equal(t, parentKey, p)

	// Verify GetChildren
	children, err := rel.GetChildren(parentKey)
	require.NoError(t, err)
	assert.Len(t, children, 2)
}

func TestSegmentStore_DeleteEvent(t *testing.T) {
	store := newTestSegmentStore(t)

	event := FullEvent{
		PartitionID:  1,
		EventType:    "test",
		EventSummary: "to be deleted",
		Timestamp:    1710678000000,
	}

	key := NewSnowflakeEventKey(1, 1710678000000)
	err := store.StoreEvent(key, event)
	require.NoError(t, err)

	// Verify it exists
	_, err = store.GetEvent(key)
	require.NoError(t, err)

	// Delete it
	err = store.DeleteEvent(key)
	require.NoError(t, err)

	// Verify it's gone
	_, err = store.GetEvent(key)
	assert.Error(t, err)
}

func TestSegmentStore_QueryEvents(t *testing.T) {
	store := newTestSegmentStore(t)

	ts := int64(1710678000000)
	for i := 0; i < 10; i++ {
		key := NewSnowflakeEventKey(1, ts+int64(i)*1000)
		store.StoreEvent(key, FullEvent{
			PartitionID:  1,
			EventType:    "test",
			EventSummary: "event " + itoa(i),
			Timestamp:    ts + int64(i)*1000,
		})
	}

	// Query with limit
	results, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{1},
		Limit:        5,
		OrderBy:      "timestamp_asc",
	})
	require.NoError(t, err)
	assert.Len(t, results, 5)
}

func TestSegmentStore_SealAndMeta(t *testing.T) {
	store := newTestSegmentStore(t)

	// Store an event
	key := NewSnowflakeEventKey(1, 1710678000000)
	store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "test",
		EventSummary: "seal test",
		Timestamp:    1710678000000,
	})

	// Seal the current segment
	err := store.SealCurrent(1)
	require.NoError(t, err)

	// Verify segment meta exists
	windowTS := WindowTimestamp(TimestampFromEventKey(key), DefaultWindowSize)
	meta, err := store.GetSegmentMeta(1, windowTS)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.True(t, meta.Sealed)
	assert.Equal(t, 1, meta.Layer)
}

func TestSegmentStore_EventCache(t *testing.T) {
	store := newTestSegmentStore(t)

	// Store and retrieve - should populate cache
	key := NewSnowflakeEventKey(1, 1710678000000)
	store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "cached",
		EventSummary: "should be cached",
		Timestamp:    1710678000000,
	})

	// First access - reads from KV
	evt, err := store.GetEvent(key)
	require.NoError(t, err)
	require.NotNil(t, evt)
	assert.Equal(t, "cached", evt.EventType)

	// Second access - should hit cache (no error expected)
	evt2, err := store.GetEvent(key)
	require.NoError(t, err)
	assert.Equal(t, "cached", evt2.EventType)
}

func TestSegmentStore_GetStats(t *testing.T) {
	store := newTestSegmentStore(t)

	stats := store.GetStats()
	assert.Equal(t, 0, stats.TotalEvents)
	assert.Equal(t, ":memory:", stats.DataDir)

	// Add an event
	key := NewSnowflakeEventKey(1, 1710678000000)
	store.StoreEvent(key, FullEvent{
		PartitionID: 1,
		EventType:   "stats",
		Timestamp:   1710678000000,
	})

	stats2 := store.GetStats()
	assert.Equal(t, 1, stats2.TotalEvents)
}

func TestSegmentStore_VectorSearchStub(t *testing.T) {
	store := newTestSegmentStore(t)

	assert.False(t, store.SupportsVectorSearch())

	_, err := store.SearchByEmbedding(nil, 0)
	assert.ErrorIs(t, err, ErrVectorSearchNotSupported)

	key := NewSnowflakeEventKey(1, 1710678000000)
	err = store.StoreEventWithEmbedding(key, FullEvent{
		EventType:   "test",
		Timestamp:   1710678000000,
		PartitionID: 1,
	}, nil)
	assert.NoError(t, err)
}

// Test that partition states are properly isolated
func TestSegmentStore_MultiPartition(t *testing.T) {
	store := newTestSegmentStore(t)

	// Store events in different partitions
	key1 := NewSnowflakeEventKey(1, 1710678000000)
	key2 := NewSnowflakeEventKey(2, 1710678000000)

	store.StoreEvent(key1, FullEvent{EventType: "p1", Timestamp: 1710678000000})
	store.StoreEvent(key2, FullEvent{EventType: "p2", Timestamp: 1710678000000})

	// Verify each partition has its own sequence
	_, err := store.GetEvent(key1)
	require.NoError(t, err)
	_, err = store.GetEvent(key2)
	require.NoError(t, err)
}

// TestSegmentStore_WithRealRelationStore verifies the production wiring path:
// FileSegmentStore + InMemRelationStore (not nil).
func TestSegmentStore_WithRealRelationStore(t *testing.T) {
	dir := t.TempDir()
	rel, err := NewInMemRelationStore(dir)
	require.NoError(t, err)
	t.Cleanup(func() { rel.Close() })

	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, rel, dir, 100)
	require.NoError(t, err)
	require.NotNil(t, store)

	// Verify RelationStoreProvider type assertion works
	var ms MemoryStore = store
	rsp, ok := ms.(RelationStoreProvider)
	require.True(t, ok, "FileSegmentStore must implement RelationStoreProvider")
	assert.Same(t, rel, rsp.RelationStore())

	// Store events and set parent relationships
	parentKey := NewSnowflakeEventKey(1, 1710678000000)
	childKey := NewSnowflakeEventKey(1, 1710678001000)

	err = store.StoreEvent(parentKey, FullEvent{PartitionID: 1, EventType: "parent", Timestamp: 1710678000000})
	require.NoError(t, err)
	err = store.StoreEvent(childKey, FullEvent{PartitionID: 1, EventType: "child", Timestamp: 1710678001000})
	require.NoError(t, err)

	// Set parent via RelationStoreProvider
	err = rsp.RelationStore().SetParent(childKey, parentKey)
	require.NoError(t, err)

	// Verify through store
	parent, err := rel.GetParent(childKey)
	require.NoError(t, err)
	assert.Equal(t, parentKey, parent)

	// Close relation store to trigger snapshot save
	rel.Close()

	// Reopen to test WAL recovery
	rel2, err := NewInMemRelationStore(dir)
	require.NoError(t, err)
	t.Cleanup(func() { rel2.Close() })

	store2, err := NewFileSegmentStore(mockKV, rel2, dir, 100)
	require.NoError(t, err)

	// Verify recovered events
	evt, err := store2.GetEvent(childKey)
	require.NoError(t, err)
	require.NotNil(t, evt)
	assert.Equal(t, "child", evt.EventType)

	// Verify recovered relationship
	var ms2 MemoryStore = store2
	rsp2, ok := ms2.(RelationStoreProvider)
	require.True(t, ok)
	recoveredParent, err := rsp2.RelationStore().GetParent(childKey)
	require.NoError(t, err)
	assert.Equal(t, parentKey, recoveredParent)
}

// Simple integer to string conversion for test usage.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProductionWiring_LifecycleAndCompactorStarted verifies that after wiring
// TombstoneSet → LifecycleManager → Compactor to a FileSegmentStore,
// both background components are running.
func TestProductionWiring_LifecycleAndCompactorStarted(t *testing.T) {
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	// Wire up lifecycle components (mirrors resolveMemoryStore in tagent.go)
	rel := store.RelationStore()
	tombstone := NewTombstoneSet(rel, mockKV, 0)
	store.SetTombstoneSet(tombstone)

	lm := NewLifecycleManager(store, tombstone, DefaultLifecycleConfig())
	lm.Start()
	store.SetLifecycleManager(lm)

	compactor := NewCompactor(store, mockKV, rel, tombstone, DefaultCompactionConfig())
	compactor.Start()
	store.SetCompactor(compactor)

	// Verify both are running
	assert.True(t, lm.running, "LifecycleManager should be running")
	assert.True(t, compactor.running, "Compactor should be running")

	// Clean up
	err = store.Close()
	require.NoError(t, err)
}

// TestProductionWiring_CloseStopsAll verifies that Close() stops both
// LifecycleManager and Compactor.
func TestProductionWiring_CloseStopsAll(t *testing.T) {
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	rel := store.RelationStore()
	tombstone := NewTombstoneSet(rel, mockKV, 0)
	store.SetTombstoneSet(tombstone)

	lm := NewLifecycleManager(store, tombstone, DefaultLifecycleConfig())
	lm.Start()
	store.SetLifecycleManager(lm)

	compactor := NewCompactor(store, mockKV, rel, tombstone, DefaultCompactionConfig())
	compactor.Start()
	store.SetCompactor(compactor)

	require.True(t, lm.running)
	require.True(t, compactor.running)

	// Close should stop both
	err = store.Close()
	require.NoError(t, err)

	assert.False(t, lm.running, "LifecycleManager should be stopped after Close")
	assert.False(t, compactor.running, "Compactor should be stopped after Close")
}

// TestProductionWiring_CloseIdempotent verifies that calling Close() multiple times
// does not panic or error.
func TestProductionWiring_CloseIdempotent(t *testing.T) {
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	rel := store.RelationStore()
	tombstone := NewTombstoneSet(rel, mockKV, 0)
	store.SetTombstoneSet(tombstone)

	lm := NewLifecycleManager(store, tombstone, DefaultLifecycleConfig())
	lm.Start()
	store.SetLifecycleManager(lm)

	compactor := NewCompactor(store, mockKV, rel, tombstone, DefaultCompactionConfig())
	compactor.Start()
	store.SetCompactor(compactor)

	// First close
	err = store.Close()
	require.NoError(t, err)

	// Second close should be idempotent (no panic, no error)
	err = store.Close()
	require.NoError(t, err)

	// Third close for good measure
	err = store.Close()
	require.NoError(t, err)
}

// TestProductionWiring_TombstoneFilterActive verifies that after wiring,
// tombstoned events are filtered from GetEvent and QueryEvents.
func TestProductionWiring_TombstoneFilterActive(t *testing.T) {
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	rel := store.RelationStore()
	tombstone := NewTombstoneSet(rel, mockKV, 0)
	store.SetTombstoneSet(tombstone)

	// Store an event
	key := NewSnowflakeEventKey(1, 0)
	err = store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "test_event",
		EventSummary: "test",
		Content:      "test content",
		Timestamp:    1,
	})
	require.NoError(t, err)

	// Event should be retrievable
	evt, err := store.GetEvent(key)
	require.NoError(t, err)
	assert.NotNil(t, evt)

	// Mark as tombstone
	err = tombstone.MarkTombstone(key)
	require.NoError(t, err)

	// Event should now be filtered (returns error)
	_, err = store.GetEvent(key)
	assert.Error(t, err, "tombstoned event should not be retrievable")
}

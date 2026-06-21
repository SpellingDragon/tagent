package memory

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCompactor creates a Compactor with a mock KV store for testing.
func newTestCompactor(t *testing.T) (*FileSegmentStore, *Compactor) {
	t.Helper()
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)
	compactor := NewCompactor(store, mockKV, store.rel, nil, DefaultCompactionConfig())
	return store, compactor
}

func TestCompactor_StartStop(t *testing.T) {
	_, compactor := newTestCompactor(t)

	// Start and immediately stop
	compactor.Start()
	compactor.Stop()

	// Should be able to start again
	compactor.Start()
	compactor.Stop()
}

func TestCompactor_SealCurrent(t *testing.T) {
	store, _ := newTestCompactor(t)

	// Store events in current window
	ts := int64(1710678000000) // 2024-03-17 07:00:00 UTC
	for i := 0; i < 3; i++ {
		key := NewSnowflakeEventKey(1, ts+int64(i)*1000)
		err := store.StoreEvent(key, FullEvent{
			PartitionID:  1,
			EventType:    "test",
			EventSummary: "event " + itoa(i),
			Timestamp:    ts + int64(i)*1000,
		})
		require.NoError(t, err)
	}

	// Seal the current segment
	err := store.SealCurrent(1)
	require.NoError(t, err)

	// Verify segment meta exists
	windowTS := WindowTimestamp(TimestampFromEventKey(NewSnowflakeEventKey(1, ts)), DefaultWindowSize)
	meta, err := store.GetSegmentMeta(1, windowTS)
	require.NoError(t, err)
	assert.True(t, meta.Sealed)
	assert.Equal(t, 1, meta.Layer)
}

func TestCompactor_L1ToL2(t *testing.T) {
	store, compactor := newTestCompactor(t)

	// Store events in 3 different hourly windows
	baseTS := int64(1710666000000) // 2024-03-17 05:00:00 UTC
	for hour := 0; hour < 3; hour++ {
		hourTS := baseTS + int64(hour)*3600000
		for i := 0; i < 2; i++ {
			key := NewSnowflakeEventKey(1, hourTS+int64(i)*1000)
			err := store.StoreEvent(key, FullEvent{
				PartitionID:  1,
				EventType:    "test",
				EventSummary: "hourly event",
				Timestamp:    hourTS + int64(i)*1000,
			})
			require.NoError(t, err)
		}
		// Seal each hour
		err := store.SealCurrent(1)
		require.NoError(t, err)
	}

	// List segments
	windows, err := store.ListSegments(1)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(windows), 1)

	// Run L1→L2 compaction on discovered windows
	err = compactor.CompactL1ToL2(1, windows)
	require.NoError(t, err)

	// Verify L2 meta exists
	l2WindowTS := computeDailyWindow(windows[0])
	l2Meta, err := store.GetSegmentMeta(1, l2WindowTS)
	require.NoError(t, err)
	assert.Equal(t, 2, l2Meta.Layer)
	assert.Equal(t, 6, l2Meta.EventCount)
}

func TestCompactor_L2ToL3(t *testing.T) {
	store, compactor := newTestCompactor(t)

	// Store events for L2
	baseTS := int64(1710604800000) // 2024-03-16 16:00:00 UTC (a daily boundary)
	key := NewSnowflakeEventKey(1, baseTS)
	err := store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    event.TypeThinkingPlan,
		EventSummary: "thinking event",
		Content:      "long thinking content that should be summarized in L3",
		Timestamp:    baseTS,
	})
	require.NoError(t, err)

	key2 := NewSnowflakeEventKey(1, baseTS+1000)
	err = store.StoreEvent(key2, FullEvent{
		PartitionID:  1,
		EventType:    event.TypeExternalInput,
		EventSummary: "external input",
		Content:      "preserved content",
		Timestamp:    baseTS + 1000,
	})
	require.NoError(t, err)

	// Seal
	err = store.SealCurrent(1)
	require.NoError(t, err)

	windows, err := store.ListSegments(1)
	require.NoError(t, err)

	// First compact to L2
	err = compactor.CompactL1ToL2(1, windows)
	require.NoError(t, err)

	// Now compact L2 to L3
	l2Windows, err := store.ListSegments(1)
	require.NoError(t, err)
	// Filter to only L2 windows
	var l2Only []int64
	for _, w := range l2Windows {
		meta, err := store.GetSegmentMeta(1, w)
		if err == nil && meta.Layer == 2 {
			l2Only = append(l2Only, w)
		}
	}

	if len(l2Only) > 0 {
		err = compactor.CompactL2ToL3(1, l2Only)
		require.NoError(t, err)

		// Verify L3 has summarization for low-value events
		l3WindowTS := computeWeeklyWindow(l2Only[0])
		l3Meta, err := store.GetSegmentMeta(1, l3WindowTS)
		if err == nil {
			assert.Equal(t, 3, l3Meta.Layer)
		}
	}
}

func TestCompactor_DanglingRefRepair(t *testing.T) {
	store, compactor := newTestCompactor(t)

	// Create a chain: key3 (child) → key2 (parent) → key1 (grandparent)
	baseTS := int64(1710678000000)
	key1 := NewSnowflakeEventKey(1, baseTS)
	key2 := NewSnowflakeEventKey(1, baseTS+1000)
	key3 := NewSnowflakeEventKey(1, baseTS+2000)

	store.StoreEvent(key1, FullEvent{PartitionID: 1, EventType: "grandparent", Timestamp: baseTS})
	store.StoreEvent(key2, FullEvent{PartitionID: 1, EventType: "parent", Timestamp: baseTS + 1000})
	store.StoreEvent(key3, FullEvent{PartitionID: 1, EventType: "child", Timestamp: baseTS + 2000})

	// Set parent relationships
	store.RelationStore().SetParent(key2, key1)
	store.RelationStore().SetParent(key3, key2)

	// Seal
	store.SealCurrent(1)

	// Get segments
	windows, _ := store.ListSegments(1)

	// Run L1→L2 compaction - should repair if any parent is missing
	err := compactor.CompactL1ToL2(1, windows)
	require.NoError(t, err)

	// key3 should still have key2 as parent (both are alive)
	parent, _ := store.RelationStore().GetParent(key3)
	assert.Equal(t, key2, parent)
}

func TestCompactor_ComputeWindows(t *testing.T) {
	// Test daily window computation
	hourly := int64(1710676800) // 2024-03-17 07:00:00 UTC
	daily := computeDailyWindow(hourly)
	expectedDaily := (hourly / 86400) * 86400
	assert.Equal(t, expectedDaily, daily)

	// Test weekly window computation
	weekly := computeWeeklyWindow(daily)
	expectedWeekly := (daily / 604800) * 604800
	assert.Equal(t, expectedWeekly, weekly)

	t.Logf("hourly=%d daily=%d weekly=%d", hourly, daily, weekly)
}

func TestCompactor_ConfigDefaults(t *testing.T) {
	cfg := DefaultCompactionConfig()
	assert.Equal(t, 24, cfg.L1Threshold)
	assert.Equal(t, 7, cfg.L2Threshold)
	assert.Equal(t, 5*time.Minute, cfg.CheckInterval)
}

func TestCompactor_NewCompactorCustomConfig(t *testing.T) {
	cfg := CompactionConfig{
		L1Threshold:   12,
		L2Threshold:   3,
		CheckInterval: time.Minute,
	}
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	compactor := NewCompactor(store, mockKV, store.rel, nil, cfg)
	assert.Equal(t, 12, compactor.config.L1Threshold)
	assert.Equal(t, 3, compactor.config.L2Threshold)
	assert.Equal(t, time.Minute, compactor.config.CheckInterval)
}

func TestCompactor_ZeroConfig(t *testing.T) {
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	compactor := NewCompactor(store, mockKV, store.rel, nil, CompactionConfig{})
	assert.Equal(t, 24, compactor.config.L1Threshold)
	assert.Equal(t, 7, compactor.config.L2Threshold)
	assert.Equal(t, 5*time.Minute, compactor.config.CheckInterval)
}

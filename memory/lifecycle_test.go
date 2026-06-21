package memory

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTombstoneSet_MarkAndCheck(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, nil, 1)

	// Mark tombstone
	err := ts.MarkTombstone(100)
	require.NoError(t, err)

	// Verify
	assert.True(t, ts.IsTombstone(100))
	assert.False(t, ts.IsTombstone(101))
}

func TestTombstoneSet_ZeroKey(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, nil, 1)
	err := ts.MarkTombstone(0)
	assert.Error(t, err)
}

func TestTombstoneSet_RemoveTombstones(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := NewMockRustVikingClient()
	ts := NewTombstoneSet(rel, mockKV, 1)

	ts.MarkTombstone(100)
	ts.MarkTombstone(200)

	require.Equal(t, 2, ts.Count())

	err := ts.RemoveTombstones([]int64{100})
	require.NoError(t, err)
	assert.Equal(t, 1, ts.Count())
	assert.False(t, ts.IsTombstone(100))
	assert.True(t, ts.IsTombstone(200))
}

func TestTombstoneSet_AllTombstones(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, nil, 1)

	ts.MarkTombstone(100)
	ts.MarkTombstone(200)
	ts.MarkTombstone(300)

	all := ts.AllTombstones()
	assert.Equal(t, 3, len(all))
	assert.Contains(t, all, int64(100))
	assert.Contains(t, all, int64(200))
	assert.Contains(t, all, int64(300))
}

func TestTombstoneSet_SnapshotRestore(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, nil, 1)

	ts.MarkTombstone(100)
	ts.MarkTombstone(200)

	// Snapshot
	snap, err := ts.Snapshot()
	require.NoError(t, err)

	// New tombstone set, load snapshot
	ts2 := NewTombstoneSet(rel, nil, 1)
	err = ts2.LoadSnapshot(snap)
	require.NoError(t, err)

	assert.True(t, ts2.IsTombstone(100))
	assert.True(t, ts2.IsTombstone(200))
	assert.False(t, ts2.IsTombstone(300))
}

func TestTombstoneSet_JSONRoundTrip(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, nil, 1)

	ts.MarkTombstone(100)
	ts.MarkTombstone(200)

	// Marshal
	data, err := ts.MarshalJSON()
	require.NoError(t, err)

	// Unmarshal into new set
	ts2 := NewTombstoneSet(rel, nil, 1)
	err = ts2.UnmarshalJSON(data)
	require.NoError(t, err)

	assert.True(t, ts2.IsTombstone(100))
	assert.True(t, ts2.IsTombstone(200))
	assert.Equal(t, 2, ts2.Count())
}

func TestTombstoneSet_CascadingParentRepair(t *testing.T) {
	rel := newSimpleInMemRelationStore()

	// Create chain: 3 → 2 → 1
	rel.SetParent(3, 2)
	rel.SetParent(2, 1)

	// Mark 2 as tombstone - this should repair 3's parent to 1
	ts := NewTombstoneSet(rel, nil, 1)
	err := ts.MarkTombstone(2)
	require.NoError(t, err)

	// Verify 3's parent is now 1 (skipped over 2)
	parent, err := rel.GetParent(3)
	require.NoError(t, err)
	assert.Equal(t, int64(1), parent)
}

func TestTombstoneSet_ChildrenRepairedOnTombstone(t *testing.T) {
	rel := newSimpleInMemRelationStore()

	// Chain: 4 → 3 → 2 → 1
	rel.SetParent(2, 1)
	rel.SetParent(3, 2)
	rel.SetParent(4, 3)

	ts := NewTombstoneSet(rel, nil, 1)

	// Tombstone middle of chain
	err := ts.MarkTombstone(2)
	require.NoError(t, err)

	// 3's parent should skip 2 and go to 1
	p3, _ := rel.GetParent(3)
	assert.Equal(t, int64(1), p3)

	// 4's parent should still be 3
	p4, _ := rel.GetParent(4)
	assert.Equal(t, int64(3), p4)
}

func TestLifecycleConfig_Defaults(t *testing.T) {
	cfg := DefaultLifecycleConfig()
	assert.Equal(t, 7, cfg.GlobalTTLDays)
	assert.Equal(t, time.Hour, cfg.CheckInterval)
	assert.Equal(t, 3, cfg.TypeTTL[event.TypeContextCompress])
	assert.Equal(t, 30, cfg.TypeTTL[event.TypeExternalInput])
}

func TestLifecycleManager_StartStop(t *testing.T) {
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, mockKV, 1)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())

	// Start and stop
	lm.Start()
	lm.Stop()

	// Can start again
	lm.Start()
	lm.Stop()
}

func TestExtractEventTypeFromJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected string
	}{
		{"simple", `{"event_type":"test","ts":123}`, "test"},
		{"with prefix", `{"event_type":"thinking_plan","content":"hello"}`, "thinking_plan"},
		{"empty", `{}`, ""},
		{"no match", `{"type":"other"}`, ""},
		{"nested", `{"event_type":"external_input","data":{"event_type":"inner"}}`, "external_input"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractEventTypeFromJSON(tt.json)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLifecycleTombstoneIntegration(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)

	// Create tombstone set and wire to store
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts

	// Store an event
	key := NewSnowflakeEventKey(1, 1710678000000)
	err = store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "test",
		EventSummary: "alive event",
		Timestamp:    1710678000000,
	})
	require.NoError(t, err)

	// GetEvent should succeed
	evt, err := store.GetEvent(key)
	require.NoError(t, err)
	require.NotNil(t, evt)

	// Mark as tombstone
	err = ts.MarkTombstone(key)
	require.NoError(t, err)

	// GetEvent should return "not found"
	_, err = store.GetEvent(key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tombstoned")
}

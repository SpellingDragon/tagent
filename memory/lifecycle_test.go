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
	// Curated artifacts are exempt from TTL (raw events may be forgotten,
	// artifacts persist — index cards point at these keys).
	assert.Equal(t, -1, cfg.TypeTTL["context_compress_summary"])
}

// TestGetEffectiveTTL_ArtifactExemption: a negative type TTL means exempt —
// getEffectiveTTL returns 0 and the expiry scan skips the type entirely
// (must NOT fall back to the global TTL).
func TestGetEffectiveTTL_ArtifactExemption(t *testing.T) {
	lm := &LifecycleManager{config: DefaultLifecycleConfig()}
	ttl, err := lm.getEffectiveTTL("context_compress_summary")
	assert.NoError(t, err)
	assert.Equal(t, 0, ttl, "negative TypeTTL must yield 0 (exempt), not global fallback")

	// Unknown types still fall back to the global TTL.
	ttl, err = lm.getEffectiveTTL("some_unknown_type")
	assert.NoError(t, err)
	assert.Equal(t, 7, ttl)
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

// TestCheckTTL_MarksExpiredEvents (I3): the whole point of TTL. Writing an
// event older than its type TTL must yield a tombstone after checkTTL — for
// months this silently did nothing because EventKey was read from the KV key
// (always zero), so `EventKey == 0 → continue` swallowed every event
// (production: 0 tombstones, 1034 overdue thinking_plan events still live).
func TestCheckTTL_MarksExpiredEvents(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())

	now := time.Now().UnixMilli()
	overdue := NewSnowflakeEventKey(1, now-10*24*3600*1000) // 10 days old
	require.NoError(t, store.StoreEvent(overdue, FullEvent{
		EventKey: overdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "old thinking", Timestamp: now - 10*24*3600*1000,
	}))
	fresh := NewSnowflakeEventKey(1, now-1000) // 1s old
	require.NoError(t, store.StoreEvent(fresh, FullEvent{
		EventKey: fresh, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "fresh thinking", Timestamp: now - 1000,
	}))
	artifact := NewSnowflakeEventKey(1, now-40*24*3600*1000) // 40 days old but exempt
	require.NoError(t, store.StoreEvent(artifact, FullEvent{
		EventKey: artifact, PartitionID: 1, EventType: "context_compress_summary",
		EventSummary: "curated artifact", Timestamp: now - 40*24*3600*1000,
	}))

	lm.checkTTL()

	assert.True(t, ts.IsTombstone(overdue),
		"thinking_plan (TTL=3d) at 10 days must be tombstoned")
	assert.False(t, ts.IsTombstone(fresh),
		"fresh event must not be tombstoned")
	assert.False(t, ts.IsTombstone(artifact),
		"curated artifacts (context_compress_summary) are exempt")

	// Tombstoned events are invisible to precise recall (the honest-miss path).
	_, err = store.GetEvent(overdue)
	assert.Error(t, err, "GetEvent must report tombstoned events as missing")
	_, err = store.GetEvent(fresh)
	assert.NoError(t, err)
}

// TestNegativeGlobalTTLDisablesTTL (B1): a NEGATIVE GlobalTTLDays means
// "disable TTL forgetting entirely" — it must survive NewLifecycleManager
// (the zero value alone falls back to the default 7).
func TestNegativeGlobalTTLDisablesTTL(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts

	cfg := DefaultLifecycleConfig()
	cfg.GlobalTTLDays = -1 // explicit off
	lm := NewLifecycleManager(store, ts, cfg)

	now := time.Now().UnixMilli()
	overdue := NewSnowflakeEventKey(1, now-30*24*3600*1000)
	require.NoError(t, store.StoreEvent(overdue, FullEvent{
		EventKey: overdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "ancient", Timestamp: now - 30*24*3600*1000,
	}))

	lm.checkTTL()
	assert.False(t, ts.IsTombstone(overdue),
		"GlobalTTLDays=-1 must disable TTL entirely (B1: must not be clamped to 7)")
}

// TestEvictionDecrementsLiveCount (M4): tombstoning must decrement the
// logically-live counter. Without it, an over-capacity partition would evict
// another excess+10 LIVE events every cycle — a ratchet that never stops
// until compaction physically removes the tombstones.
func TestEvictionDecrementsLiveCount(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := NewMockRustVikingClient()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts

	cfg := DefaultLifecycleConfig()
	cfg.MaxEventsPerPartition = 3
	lm := NewLifecycleManager(store, ts, cfg)

	now := time.Now().UnixMilli()
	for i := 0; i < 6; i++ {
		k := NewSnowflakeEventKey(1, now-int64(100+i))
		require.NoError(t, store.StoreEvent(k, FullEvent{
			EventKey: k, PartitionID: 1, EventType: "agent_output",
			EventSummary: "event", Timestamp: now - int64(100+i),
		}))
	}
	before := store.GetStats().TotalEvents
	require.Equal(t, 6, before)

	lm.checkCapacity() // evicts (6-3)+10 → capped by available events
	after := store.GetStats().TotalEvents
	assert.Less(t, after, before,
		"eviction must decrement the live counter, or the next cycle re-evicts live events")

	// A second cycle must NOT evict more than needed: counter already ≤ max.
	before2 := store.GetStats().TotalEvents
	lm.checkCapacity()
	assert.Equal(t, before2, store.GetStats().TotalEvents,
		"once within capacity, subsequent cycles must be no-ops")
}

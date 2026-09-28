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

	err := ts.MarkTombstone(100)
	require.NoError(t, err)

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
	mockKV := newMockKV()
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

	snap, err := ts.Snapshot()
	require.NoError(t, err)

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

	data, err := ts.MarshalJSON()
	require.NoError(t, err)

	ts2 := NewTombstoneSet(rel, nil, 1)
	err = ts2.UnmarshalJSON(data)
	require.NoError(t, err)

	assert.True(t, ts2.IsTombstone(100))
	assert.True(t, ts2.IsTombstone(200))
	assert.Equal(t, 2, ts2.Count())
}

func TestTombstoneSet_CascadingParentRepair(t *testing.T) {
	rel := newSimpleInMemRelationStore()

	rel.SetParent(3, 2)
	rel.SetParent(2, 1)

	ts := NewTombstoneSet(rel, nil, 1)
	err := ts.MarkTombstone(2)
	require.NoError(t, err)

	parent, err := rel.GetParent(3)
	require.NoError(t, err)
	assert.Equal(t, int64(1), parent)
}

func TestTombstoneSet_ChildrenRepairedOnTombstone(t *testing.T) {
	rel := newSimpleInMemRelationStore()

	rel.SetParent(2, 1)
	rel.SetParent(3, 2)
	rel.SetParent(4, 3)

	ts := NewTombstoneSet(rel, nil, 1)

	err := ts.MarkTombstone(2)
	require.NoError(t, err)

	p3, _ := rel.GetParent(3)
	assert.Equal(t, int64(1), p3)

	p4, _ := rel.GetParent(4)
	assert.Equal(t, int64(3), p4)
}

func TestLifecycleConfig_Defaults(t *testing.T) {
	cfg := DefaultLifecycleConfig()
	assert.Equal(t, 7, cfg.GlobalTTLDays)
	assert.Equal(t, time.Hour, cfg.CheckInterval)
	assert.Equal(t, 3, cfg.TypeTTL[event.TypeContextCompress])
	assert.Equal(t, 30, cfg.TypeTTL[event.TypeExternalInput])
	assert.Equal(t, -1, cfg.TypeTTL["context_compress_summary"])
}

// TestGetEffectiveTTL_ArtifactExemption 钉住 a negative type TTL means exempt
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestGetEffectiveTTL_ArtifactExemption(t *testing.T) {
	lm := &LifecycleManager{config: DefaultLifecycleConfig()}
	ttl, err := lm.getEffectiveTTL("context_compress_summary")
	assert.NoError(t, err)
	assert.Equal(t, 0, ttl, "negative TypeTTL must yield 0 (exempt), not global fallback")

	ttl, err = lm.getEffectiveTTL("some_unknown_type")
	assert.NoError(t, err)
	assert.Equal(t, 7, ttl)
}

// TestWFPassiveExclusionDoesNotShortenHistoryTTL 钉住 registering wf.*
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestWFPassiveExclusionDoesNotShortenHistoryTTL(t *testing.T) {
	cfg := DefaultLifecycleConfig()
	cfg.GlobalTTLDays = 90

	if _, present := cfg.TypeTTL[event.TypeWFReceived]; present {
		t.Fatalf("wf.received leaked a type TTL (%d); passive exclusion must add none", cfg.TypeTTL[event.TypeWFReceived])
	}
	lmOnly := &LifecycleManager{config: cfg}
	ttl, err := lmOnly.getEffectiveTTL(event.TypeWFReceived)
	require.NoError(t, err)
	assert.Equal(t, 90, ttl, "wf.received must inherit the 90-day global TTL")

	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lm := NewLifecycleManager(store, ts, cfg)

	now := time.Now().UnixMilli()
	age31 := int64(31 * 24 * 3600 * 1000)
	wfKey := NewSnowflakeEventKey(1, now-age31)
	require.NoError(t, store.StoreEvent(wfKey, FullEvent{
		EventKey: wfKey, PartitionID: 1, EventType: event.TypeWFReceived,
		EventSummary: "historical wf fact", Timestamp: now - age31,
	}))
	ctrlKey := NewSnowflakeEventKey(1, now-age31+1)
	require.NoError(t, store.StoreEvent(ctrlKey, FullEvent{
		EventKey: ctrlKey, PartitionID: 1, EventType: event.TypeExternalInput,
		EventSummary: "same-age external_input", Timestamp: now - age31,
	}))

	lm.checkTTL()

	assert.False(t, ts.IsTombstone(wfKey),
		"a 31-day wf record must NOT be evicted under the 90-day global policy (no added wf TTL)")
	assert.True(t, ts.IsTombstone(ctrlKey),
		"control: a 31-day external_input (explicit 30-day type TTL) must still be evicted — the scan is active")
}

func TestLifecycleManager_StartStop(t *testing.T) {
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, mockKV, 1)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())

	lm.Start()
	lm.Stop()

	lm.Start()
	lm.Stop()
}

func TestLifecycleTombstoneIntegration(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)

	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts

	key := NewSnowflakeEventKey(1, 1710678000000)
	err = store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "test",
		EventSummary: "alive event",
		Timestamp:    1710678000000,
	})
	require.NoError(t, err)

	evt, err := store.GetEvent(key)
	require.NoError(t, err)
	require.NotNil(t, evt)

	err = ts.MarkTombstone(key)
	require.NoError(t, err)

	_, err = store.GetEvent(key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tombstoned")
}

// TestCheckTTL_MarksExpiredEvents 钉住 whole point of TTL. Writing an
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestCheckTTL_MarksExpiredEvents(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())

	now := time.Now().UnixMilli()
	overdue := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	require.NoError(t, store.StoreEvent(overdue, FullEvent{
		EventKey: overdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "old thinking", Timestamp: now - 10*24*3600*1000,
	}))
	fresh := NewSnowflakeEventKey(1, now-1000)
	require.NoError(t, store.StoreEvent(fresh, FullEvent{
		EventKey: fresh, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "fresh thinking", Timestamp: now - 1000,
	}))
	artifact := NewSnowflakeEventKey(1, now-40*24*3600*1000)
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

	_, err = store.GetEvent(overdue)
	assert.Error(t, err, "GetEvent must report tombstoned events as missing")
	_, err = store.GetEvent(fresh)
	assert.NoError(t, err)
}

// TestNegativeGlobalTTLDisablesTTL 钉住 a NEGATIVE GlobalTTLDays means
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestNegativeGlobalTTLDisablesTTL(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts

	cfg := DefaultLifecycleConfig()
	cfg.GlobalTTLDays = -1
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

// TestEvictionDecrementsLiveCount 钉住 tombstoning must decrement the
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestEvictionDecrementsLiveCount(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
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

	lm.checkCapacity()
	require.Equal(t, before, store.GetStats().TotalEvents,
		"unknown counts must pause capacity eviction")
	require.False(t, store.LivesCountKnown())

	lm.evictOldest(1, 3)
	after := store.GetStats().TotalEvents
	assert.Less(t, after, before,
		"eviction must decrement the live counter, or the next cycle re-evicts live events")

	lm.evictOldest(1, 3)
	assert.Equal(t, 0, store.GetStats().TotalEvents,
		"second pass must skip already-tombstoned events (no double decrement)")
}

// TestProductionWiring_LifecycleAndCompactorStarted 钉住 verifies that after wiring
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestProductionWiring_LifecycleAndCompactorStarted(t *testing.T) {
	mockKV := newMockKV()
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

	assert.True(t, lm.running, "LifecycleManager should be running")
	assert.True(t, compactor.running, "Compactor should be running")

	err = store.Close()
	require.NoError(t, err)
}

// TestProductionWiring_CloseStopsAll 钉住 verifies that Close stops both
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestProductionWiring_CloseStopsAll(t *testing.T) {
	mockKV := newMockKV()
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

	err = store.Close()
	require.NoError(t, err)

	assert.False(t, lm.running, "LifecycleManager should be stopped after Close")
	assert.False(t, compactor.running, "Compactor should be stopped after Close")
}

// TestProductionWiring_CloseIdempotent 钉住 verifies that calling Close multiple times
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestProductionWiring_CloseIdempotent(t *testing.T) {
	mockKV := newMockKV()
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

	err = store.Close()
	require.NoError(t, err)

	err = store.Close()
	require.NoError(t, err)

	err = store.Close()
	require.NoError(t, err)
}

// TestProductionWiring_TombstoneFilterActive 钉住 verifies that after wiring,
//
// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
func TestProductionWiring_TombstoneFilterActive(t *testing.T) {
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	rel := store.RelationStore()
	tombstone := NewTombstoneSet(rel, mockKV, 0)
	store.SetTombstoneSet(tombstone)

	key := NewSnowflakeEventKey(1, 0)
	err = store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "test_event",
		EventSummary: "test",
		Content:      "test content",
		Timestamp:    1,
	})
	require.NoError(t, err)

	evt, err := store.GetEvent(key)
	require.NoError(t, err)
	assert.NotNil(t, evt)

	err = tombstone.MarkTombstone(key)
	require.NoError(t, err)

	_, err = store.GetEvent(key)
	assert.Error(t, err, "tombstoned event should not be retrievable")
}

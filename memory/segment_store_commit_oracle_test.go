package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSegmentStore_ReplayTombstonedEventRefused locks async-task-lifetime 2.4: a
// legally tombstoned (deliberately deleted) fact MUST NOT be resurrected by a replay.
// The internal replay path refuses it with the typed ErrEventForgotten before any
// write, and the tombstone survives — the guard neither overwrites nor removes it.
func TestSegmentStore_ReplayTombstonedEventRefused(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	rel := newSimpleInMemRelationStore()
	store, err := NewFileSegmentStore(kv, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, kv, 7)
	store.SetTombstoneSet(ts)

	const pid = 7
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	evt := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Timestamp: 1704067200000}
	require.NoError(t, store.StoreEvent(key, evt))
	require.NoError(t, ts.MarkTombstone(key))

	_, _, rerr := store.ReplayEvent(key, evt)
	require.True(t, IsEventForgotten(rerr), "a replay of a tombstoned fact must be refused as forgotten, got: %v", rerr)
	require.True(t, ts.IsTombstone(key), "the refusal must not remove the tombstone")
}

// TestSegmentStore_PartitionUnknownUntilRecompute locks async-task-lifetime 2.5: a
// partition's live count is UNTRUSTED (capacity eviction pauses for it) until a full-
// record scan establishes it — a brand-new process that only received +1 deltas has not
// verified the partition against the fact chain. A successful recompute makes it known
// and authoritative.
func TestSegmentStore_PartitionUnknownUntilRecompute(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	const pid = 3
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	evt := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Timestamp: 1704067200000}
	require.NoError(t, store.StoreEvent(key, evt))

	// Never rebuilt/scanned → the count is only this-process deltas, not authoritative.
	require.False(t, store.PartitionCountKnown(pid), "a partition is unknown until a full-record scan (2.5)")

	require.NoError(t, store.recomputePartition(pid))
	require.True(t, store.PartitionCountKnown(pid), "a successful recompute makes the count authoritative")
	require.EqualValues(t, 1, store.GetStats().TotalEvents)
}

// TestSegmentStore_ReplayWrongPartitionRefused locks async-task-lifetime 2.4 "不重盖
// 身份": the EventKey authoritatively encodes its partition, so a canonical fact that
// declares a CONTRADICTORY non-zero partition is an identity conflict — refused, never
// silently re-stamped onto the key's partition (which would let a mis-attributed write
// land in the wrong partition's chain).
func TestSegmentStore_ReplayWrongPartitionRefused(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	const pid = 5
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	mismatched := FullEvent{EventKey: key, PartitionID: 999, EventType: "external_input", Timestamp: 1704067200000}
	_, _, rerr := store.ReplayEvent(key, mismatched)
	require.True(t, IsDuplicateEventKey(rerr), "a fact contradicting the key's partition must be refused, got: %v", rerr)
	require.Equal(t, 0, store.GetStats().TotalEvents, "a refused identity conflict writes nothing")
}

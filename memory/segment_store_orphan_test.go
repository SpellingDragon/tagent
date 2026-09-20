package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// orphanWindow returns the placement window for a key in partition pid.
func orphanWindow(key int64) int64 {
	return WindowTimestamp(TimestampFromEventKey(key), DefaultWindowSize)
}

// TestSegmentStore_ReplayReusesOrphanEvtNoSecondCopy locks async-task-lifetime 2.3
// class "evt 有/idx 缺": when a crash leaves the evt slot written but its identity
// index missing, a replay MUST locate the orphaned original slot and REUSE it —
// publishing only the idx pointer — never commit a SECOND copy of the same fact.
// Before this fix ReplayEvent only probed the index; on a miss it allocated a fresh
// seq and wrote a duplicate evt, orphaning the first slot.
func TestSegmentStore_ReplayReusesOrphanEvtNoSecondCopy(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	const pid = 9
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	window := orphanWindow(key)
	evt := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Timestamp: 1704067200000}
	raw, _ := json.Marshal(evt)
	// Crash after the evt write, before the idx write: evt at seq 0, NO idx present.
	require.NoError(t, kv.KVPut(EventKeyStr(pid, window, 0), string(raw)))

	res, _, err := store.ReplayEvent(key, evt)
	require.NoError(t, err)
	require.Equal(t, ReplayRepaired, res)

	idxv, err := kv.KVGet(IndexKeyStr(pid, key))
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%d:%d", window, 0), idxv, "idx must reuse the ORIGINAL slot (w:0), not a new seq")

	pairs, err := kv.KVScan(SegmentEventPrefix(pid, window), 0)
	require.NoError(t, err)
	require.Len(t, pairs, 1, "reuse must NOT create a second evt slot for the same fact")
	require.Equal(t, 1, store.GetStats().TotalEvents, "the reused slot was never counted before (idx was missing)")
}

// TestSegmentStore_ReplayOrphanEvtDifferentContentRefused: an orphaned evt slot under
// the same identity whose CONTENT differs is a snowflake collision, not a replay
// target — it MUST be refused (ErrDuplicateEventKey), leaving neither a new copy nor
// a published index (identity is immutable).
func TestSegmentStore_ReplayOrphanEvtDifferentContentRefused(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	const pid = 9
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	window := orphanWindow(key)
	stored := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Content: "ORIGINAL", Timestamp: 1704067200000}
	raw, _ := json.Marshal(stored)
	require.NoError(t, kv.KVPut(EventKeyStr(pid, window, 0), string(raw)))

	different := stored
	different.Content = "DIFFERENT"
	res, _, err := store.ReplayEvent(key, different)
	require.ErrorIs(t, err, ErrDuplicateEventKey)
	require.Equal(t, ReplayNew, res)

	// Refusal must not publish an index nor add a copy — the original stays intact.
	_, idxErr := kv.KVGet(IndexKeyStr(pid, key))
	require.True(t, errors.Is(idxErr, ErrKeyNotFound), "a refused collision must not publish an index")
	pairs, _ := kv.KVScan(SegmentEventPrefix(pid, window), 0)
	require.Len(t, pairs, 1)
}

// TestSegmentStore_ReplayRepairsMissingMetaAtSeqGT0 locks async-task-lifetime 2.3
// class "meta 缺": a window whose segment meta was lost is invisible to discovery,
// so replay MUST repair the meta REGARDLESS of the orphan's seq. The earlier guard
// only wrote meta at seq==0, permanently stranding a seq>0 orphan in an undiscoverable
// window.
func TestSegmentStore_ReplayRepairsMissingMetaAtSeqGT0(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	const pid = 11
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	window := orphanWindow(key)
	evt := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Timestamp: 1704067200000}
	raw, _ := json.Marshal(evt)
	// An orphan whose idx+evt are durable but the window meta is missing, at seq 3.
	require.NoError(t, kv.KVPut(EventKeyStr(pid, window, 3), string(raw)))
	require.NoError(t, kv.KVPut(IndexKeyStr(pid, key), fmt.Sprintf("%d:%d", window, 3)))
	_, metaErr := kv.KVGet(MetaKeyStr(pid, window))
	require.True(t, errors.Is(metaErr, ErrKeyNotFound), "precondition: window meta absent")

	res, _, err := store.ReplayEvent(key, evt)
	require.NoError(t, err)
	require.Equal(t, ReplayRepaired, res)

	meta, err := store.GetSegmentMeta(pid, window)
	require.NoError(t, err, "replay must repair the missing meta even though the orphan is at seq 3")
	require.NotNil(t, meta)
	require.False(t, meta.Sealed, "a repaired meta is conservative (unsealed → always discoverable)")
}

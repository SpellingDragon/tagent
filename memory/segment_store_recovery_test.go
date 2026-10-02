package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSegmentStore_ReplayTombstonedEventRefused 钉住 locks async-task-lifetime 2.4: a
//
// 契约: docs/wiki/memory/memory-architecture.md#tombstone
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

// TestSegmentStore_PartitionUnknownUntilRecompute 钉住 分区在完整扫描前为未知；一次成功的重算才让计数成为权威值。
func TestSegmentStore_PartitionUnknownUntilRecompute(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	const pid = 3
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	evt := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Timestamp: 1704067200000}
	require.NoError(t, store.StoreEvent(key, evt))

	require.False(t, store.PartitionCountKnown(pid), "a partition is unknown until a full-record scan (2.5)")

	require.NoError(t, store.recomputePartition(pid))
	require.True(t, store.PartitionCountKnown(pid), "a successful recompute makes the count authoritative")
	require.EqualValues(t, 1, store.GetStats().TotalEvents)
}

// TestSegmentStore_ReplayWrongPartitionRefused 钉住 与键所属分区矛盾的事实必须被拒绝，且被拒的身份冲突不得写入任何内容。
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

// TestSegmentStore_ConcurrentSameKeyAtomicCommit 钉住 同一键的并发提交必须原子：恰好一个胜出，不得出现重复写入。
func TestSegmentStore_ConcurrentSameKeyAtomicCommit(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	if err != nil {
		t.Fatalf("NewFileSegmentStore: %v", err)
	}

	const pid = 7
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	evt := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Timestamp: 1704067200000}

	const n = 16
	var (
		wg       sync.WaitGroup
		gate     = make(chan struct{})
		okCount  atomic.Int64
		dupCount atomic.Int64
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			switch err := store.StoreEvent(key, evt); {
			case err == nil:
				okCount.Add(1)
			case errors.Is(err, ErrDuplicateEventKey):
				dupCount.Add(1)
			default:
				t.Errorf("unexpected StoreEvent error: %v", err)
			}
		}()
	}
	close(gate)
	wg.Wait()

	if got := okCount.Load(); got != 1 {
		t.Fatalf("concurrent same-key commit must yield exactly 1 winner, got %d (dups=%d)", got, dupCount.Load())
	}
	if got := dupCount.Load(); got != n-1 {
		t.Fatalf("expected %d duplicate rejections, got %d", n-1, got)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("live-count must increment exactly once (no double-counted ghost), got TotalEvents=%d", got)
	}
	if _, err := store.GetEvent(key); err != nil {
		t.Fatalf("the committed event must stay recallable: %v", err)
	}
}

// orphanWindow returns the placement window for a key in partition pid.
func orphanWindow(key int64) int64 {
	return WindowTimestamp(TimestampFromEventKey(key), DefaultWindowSize)
}

// TestSegmentStore_ReplayReusesOrphanEvtNoSecondCopy 钉住 回放复用孤立事件槽时不得为同一事实再建第二份槽；被复用的槽此前未被计数。
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

// TestSegmentStore_ReplayOrphanEvtDifferentContentRefused 钉住 孤立槽内容不一致时必须拒绝，且拒绝后不得发布索引。
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

	_, idxErr := kv.KVGet(IndexKeyStr(pid, key))
	require.True(t, errors.Is(idxErr, ErrKeyNotFound), "a refused collision must not publish an index")
	pairs, _ := kv.KVScan(SegmentEventPrefix(pid, window), 0)
	require.Len(t, pairs, 1)
}

// TestSegmentStore_ReplayRepairsMissingMetaAtSeqGT0 钉住 窗口元数据缺失时回放必须修复它（即便孤立项在 seq>0），修复后的元数据取保守值以保证可发现。
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

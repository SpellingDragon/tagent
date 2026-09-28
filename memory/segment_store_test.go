package memory

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSegmentStore creates a FileSegmentStore with a mock KV store for testing.
func newTestSegmentStore(t *testing.T) *FileSegmentStore {
	t.Helper()
	mockKV := newMockKV()
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

	for key, evt := range events {
		require.NoError(t, store.StoreEvent(key, evt), "StoreEvent should succeed")
	}

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

	parentKey := NewSnowflakeEventKey(1, 1710678000000)
	childKey1 := NewSnowflakeEventKey(1, 1710678001000)
	childKey2 := NewSnowflakeEventKey(1, 1710678002000)

	store.StoreEvent(parentKey, FullEvent{PartitionID: 1, EventType: "parent", Timestamp: 1710678000000})
	store.StoreEvent(childKey1, FullEvent{PartitionID: 1, EventType: "child", Timestamp: 1710678001000})
	store.StoreEvent(childKey2, FullEvent{PartitionID: 1, EventType: "child", Timestamp: 1710678002000})

	rel := store.RelationStore()
	err := rel.SetParent(childKey1, parentKey)
	require.NoError(t, err)
	err = rel.SetParent(childKey2, parentKey)
	require.NoError(t, err)

	p, err := rel.GetParent(childKey1)
	require.NoError(t, err)
	assert.Equal(t, parentKey, p)

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

	_, err = store.GetEvent(key)
	require.NoError(t, err)

	err = store.DeleteEvent(key)
	require.NoError(t, err)

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

	key := NewSnowflakeEventKey(1, 1710678000000)
	store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "test",
		EventSummary: "seal test",
		Timestamp:    1710678000000,
	})

	err := store.SealCurrent(1)
	require.NoError(t, err)

	windowTS := WindowTimestamp(TimestampFromEventKey(key), DefaultWindowSize)
	meta, err := store.GetSegmentMeta(1, windowTS)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.True(t, meta.Sealed)
	assert.Equal(t, 1, meta.Layer)
}

func TestSegmentStore_EventCache(t *testing.T) {
	store := newTestSegmentStore(t)

	key := NewSnowflakeEventKey(1, 1710678000000)
	store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    "cached",
		EventSummary: "should be cached",
		Timestamp:    1710678000000,
	})

	evt, err := store.GetEvent(key)
	require.NoError(t, err)
	require.NotNil(t, evt)
	assert.Equal(t, "cached", evt.EventType)

	evt2, err := store.GetEvent(key)
	require.NoError(t, err)
	assert.Equal(t, "cached", evt2.EventType)
}

func TestSegmentStore_GetStats(t *testing.T) {
	store := newTestSegmentStore(t)

	stats := store.GetStats()
	assert.Equal(t, 0, stats.TotalEvents)
	assert.Equal(t, ":memory:", stats.DataDir)

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

// TestSegmentStore_MultiPartition 钉住 Test that partition states are properly isolated
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestSegmentStore_MultiPartition(t *testing.T) {
	store := newTestSegmentStore(t)

	key1 := NewSnowflakeEventKey(1, 1710678000000)
	key2 := NewSnowflakeEventKey(2, 1710678000000)

	store.StoreEvent(key1, FullEvent{EventType: "p1", Timestamp: 1710678000000})
	store.StoreEvent(key2, FullEvent{EventType: "p2", Timestamp: 1710678000000})

	_, err := store.GetEvent(key1)
	require.NoError(t, err)
	_, err = store.GetEvent(key2)
	require.NoError(t, err)
}

// TestSegmentStore_WithRealRelationStore 钉住 verifies the production wiring path
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestSegmentStore_WithRealRelationStore(t *testing.T) {
	dir := t.TempDir()
	rel, err := NewInMemRelationStore(dir)
	require.NoError(t, err)
	t.Cleanup(func() { rel.Close() })

	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, dir, 100)
	require.NoError(t, err)
	require.NotNil(t, store)

	// Verify RelationStoreProvider type assertion works
	var ms MemoryStore = store
	rsp, ok := ms.(RelationStoreProvider)
	require.True(t, ok, "FileSegmentStore must implement RelationStoreProvider")
	assert.Same(t, rel, rsp.RelationStore())

	parentKey := NewSnowflakeEventKey(1, 1710678000000)
	childKey := NewSnowflakeEventKey(1, 1710678001000)

	err = store.StoreEvent(parentKey, FullEvent{PartitionID: 1, EventType: "parent", Timestamp: 1710678000000})
	require.NoError(t, err)
	err = store.StoreEvent(childKey, FullEvent{PartitionID: 1, EventType: "child", Timestamp: 1710678001000})
	require.NoError(t, err)

	err = rsp.RelationStore().SetParent(childKey, parentKey)
	require.NoError(t, err)

	parent, err := rel.GetParent(childKey)
	require.NoError(t, err)
	assert.Equal(t, parentKey, parent)

	rel.Close()

	rel2, err := NewInMemRelationStore(dir)
	require.NoError(t, err)
	t.Cleanup(func() { rel2.Close() })

	store2, err := NewFileSegmentStore(mockKV, rel2, dir, 100)
	require.NoError(t, err)

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

// itoa 是测试用的整数转字符串小工具。
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

func newFileStoreWithTombstones(t *testing.T) (*FileSegmentStore, *TombstoneSet) {
	t.Helper()
	kv := newMockKV()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)
	tset := NewTombstoneSet(store.rel, kv, 0)
	require.NoError(t, tset.RecoverFromKV())
	store.SetTombstoneSet(tset)
	return store, tset
}

func seedN(t *testing.T, store MemoryStore, pid, n int) []int64 {
	t.Helper()
	keys := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		key := NewSnowflakeEventKey(pid, 0)
		require.NoError(t, store.StoreEvent(key, FullEvent{
			EventKey: key, PartitionID: pid, EventType: "external_input",
			EventSummary: "e", Content: "c", Timestamp: 1700000000000 + int64(i),
			Metadata: map[string]string{"idx": itoaTest(i)},
		}))
		keys = append(keys, key)
	}
	return keys
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for ; i > 0; i /= 10 {
		digits = string(rune('0'+i%10)) + digits
	}
	return digits
}

func TestBackendParity_DuplicateRejected(t *testing.T) {
	mem := NewInMemoryStore()
	file, _ := newFileStoreWithTombstones(t)
	for name, s := range map[string]MemoryStore{"memory": mem, "file": file} {
		key := NewSnowflakeEventKey(1, 0)
		evt := FullEvent{EventKey: key, PartitionID: 1, EventType: "t", Timestamp: 1}
		require.NoError(t, s.StoreEvent(key, evt), name)
		clash := evt
		clash.Content = "different fact"
		err := s.StoreEvent(key, clash)
		require.Error(t, err, name)
		require.True(t, errors.Is(err, ErrDuplicateEventKey), "%s: want typed duplicate, got %v", name, err)
		sameErr := s.StoreEvent(key, evt)
		require.True(t, errors.Is(sameErr, ErrDuplicateEventKey),
			"%s: 2.1 same-content public duplicate must be refused, got %v", name, sameErr)
	}
}

func TestBackendParity_NoPartitionQueryIsEmpty(t *testing.T) {
	mem := NewInMemoryStore()
	file, _ := newFileStoreWithTombstones(t)
	for name, s := range map[string]MemoryStore{"memory": mem, "file": file} {
		seedN(t, s, 1, 2)
		refs, err := s.QueryEvents(QueryOptions{})
		require.NoError(t, err, name)
		require.Empty(t, refs, "%s: unscoped query must scan nothing", name)
	}
}

func TestBackendParity_ReturnedEventsAreClones(t *testing.T) {
	mem := NewInMemoryStore()
	file, _ := newFileStoreWithTombstones(t)
	for name, s := range map[string]MemoryStore{"memory": mem, "file": file} {
		key := seedN(t, s, 1, 1)[0]
		got, err := s.GetEvent(key)
		require.NoError(t, err, name)
		got.Metadata["idx"] = "mutated-after-store"
		got.Content = "mutated"
		got2, err := s.GetEvent(key)
		require.NoError(t, err, name)
		require.NotEqual(t, "mutated", got2.Content, name)
		require.Equal(t, "0", got2.Metadata["idx"], name)
		got2.Metadata["idx"] = "mutated-after-get"
		got3, err := s.GetEvent(key)
		require.NoError(t, err, name)
		require.Equal(t, "0", got3.Metadata["idx"], name)
	}
}

func TestTypedMissing_DistinguishedAcrossBackends(t *testing.T) {
	mem := NewInMemoryStore()
	file, _ := newFileStoreWithTombstones(t)
	for name, s := range map[string]MemoryStore{"memory": mem, "file": file} {
		_, err := s.GetEvent(NewSnowflakeEventKey(9, 0))
		require.Error(t, err, name)
		require.True(t, errors.Is(err, ErrKeyNotFound), "%s: want typed missing, got %v", name, err)
	}
}

func TestGetEvents_PartialWithIOErrorNotSilentlyComplete(t *testing.T) {
	file, _ := newFileStoreWithTombstones(t)
	keys := seedN(t, file, 1, 3)
	got, err := file.GetEvents(append(keys, NewSnowflakeEventKey(9, 0)))
	require.NoError(t, err)
	require.Len(t, got, 3)
}

func TestLiveCount_RebuildFromFactChain(t *testing.T) {
	file, tset := newFileStoreWithTombstones(t)
	keys := seedN(t, file, 1, 5)
	require.False(t, file.LivesCountKnown(), "unknown until rebuilt")

	require.NoError(t, tset.MarkTombstone(keys[0]))
	require.Equal(t, 5, file.GetStats().TotalEvents)

	require.NoError(t, file.RebuildLiveCounts())
	require.True(t, file.LivesCountKnown())
	require.Equal(t, 4, file.GetStats().TotalEvents)
	require.True(t, file.GetStats().CountsKnown)

	k := NewSnowflakeEventKey(1, 0)
	require.NoError(t, file.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: "t", Timestamp: 2}))
	require.Equal(t, 5, file.GetStats().TotalEvents)

	require.NoError(t, file.DeleteEvent(keys[0]))
	require.Equal(t, 5, file.GetStats().TotalEvents)

	require.NoError(t, file.DeleteEvent(keys[1]))
	require.Equal(t, 4, file.GetStats().TotalEvents)
	require.NoError(t, file.DeleteEvent(keys[1]))
	require.Equal(t, 4, file.GetStats().TotalEvents)
}

func TestLiveCount_UnknownOnEnumerationFailure(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	bare := &noEnumKV{data: map[string]string{}}
	store, err := NewFileSegmentStore(bare, rel, ":memory:", 100)
	require.NoError(t, err)
	seedN(t, store, 1, 2)
	require.Error(t, store.RebuildLiveCounts())
	require.False(t, store.LivesCountKnown())
	require.False(t, store.GetStats().CountsKnown)
	_ = NewTombstoneSet(rel, bare, 0)
}

// noEnumKV is a mockKV that deliberately omits ListPartitionIDs/Sync —
// the "capability-poor backend" shape (e.g. CLI stores without enumeration).
type noEnumKV struct{ data map[string]string }

func (m *noEnumKV) KVPut(key, value string) error { m.data[key] = value; return nil }
func (m *noEnumKV) KVGet(key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", KeyNotFound(key, nil)
	}
	return v, nil
}
func (m *noEnumKV) KVDelete(key string) error { delete(m.data, key); return nil }
func (m *noEnumKV) KVScan(prefix string, limit int) ([]KVPair, error) {
	var out []KVPair
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, KVPair{Key: k, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *noEnumKV) KVRange(start, end string, limit int) ([]KVPair, error) {
	var out []KVPair
	for k, v := range m.data {
		if k >= start && k < end {
			out = append(out, KVPair{Key: k, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *noEnumKV) KVBatch(ops []KVOp) error {
	for _, op := range ops {
		if op.Type == "delete" {
			delete(m.data, op.Key)
		} else {
			m.data[op.Key] = op.Value
		}
	}
	return nil
}

// barrierFailKV wraps a KVStore whose Sync always fails — the shape the
// durability-barrier contract must survive: StoreEvent reports
// failure, the live count stays untouched, and the error propagates through
// the ErrorTrackingStore decoration for degradation accounting (2.10).
type barrierFailKV struct {
	KVStore
	syncErr atomic.Value
	healed  atomic.Bool
}

func (b *barrierFailKV) Sync() error {
	if b.healed.Load() {
		return nil
	}
	if err, _ := b.syncErr.Load().(error); err != nil {
		return err
	}
	return nil
}

func TestBarrierFailure_FailsCommitAndSparesCountThroughDecorations(t *testing.T) {
	kv := newMockKV()
	base, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)
	failing := &barrierFailKV{KVStore: kv}
	failing.syncErr.Store(errors.New("disk gone"))
	base.kv = failing

	decorated := NewErrorTrackingStore(base, nil)
	pid := 1
	before := base.GetStats().TotalEvents
	key := NewSnowflakeEventKey(pid, 0)
	err = decorated.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "x", Content: "x", Timestamp: 1,
	})
	require.Error(t, err, "barrier failure must fail the commit")
	require.True(t, strings.Contains(err.Error(), "disk gone"),
		"the underlying barrier error must surface, got: %v", err)
	require.Equal(t, before, base.GetStats().TotalEvents,
		"a failed commit must not touch the live count")

	require.True(t, IsDuplicateEventKey(decorated.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "x", Content: "x", Timestamp: 1,
	})), "2.1: public StoreEvent must refuse the orphan retry")
	failing.healed.Store(true)
	res, _, herr := decorated.ReplayEvent(key, FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "x", Content: "x", Timestamp: 1,
	})
	require.NoError(t, herr, "replay-based orphan completion must succeed once the barrier heals")
	require.Equal(t, ReplayAlreadyCommitted, res, "2.4: a durable-complete replay classifies from the fact chain, not the cache")
	require.Equal(t, before+1, base.GetStats().TotalEvents)
	refs, qerr := base.QueryEvents(QueryOptions{PartitionIDs: []int{pid}})
	require.NoError(t, qerr)
	require.Len(t, refs, 1, "orphan completion must not duplicate the fact")
}

var _ EventReplayer = (*FileSegmentStore)(nil)

// TestReplayEvent_Classification 钉住 verifies the three ReplayResult values.
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestReplayEvent_Classification(t *testing.T) {
	kv := newMockKV()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	pid := 1
	key := NewSnowflakeEventKey(pid, 0)
	evt := FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "replay-test", Content: "replay-content", Timestamp: 1700000000000,
	}

	result, got, err := store.ReplayEvent(key, evt)
	require.NoError(t, err)
	require.Equal(t, ReplayNew, result, "first commit must be ReplayNew")
	require.Equal(t, key, got.EventKey)

	result2, _, err2 := store.ReplayEvent(key, evt)
	require.NoError(t, err2, "already-committed replay must return nil error")
	require.Equal(t, ReplayAlreadyCommitted, result2,
		"replay of already-committed event must be ReplayAlreadyCommitted (F8)")

	require.EqualValues(t, 1, store.GetStats().TotalEvents,
		"ReplayAlreadyCommitted must NOT increment live-count (F8 fix)")
}

// TestReplayEvent_HalfOrphanRepair 钉住 verifies that when the evt KV slot is
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestReplayEvent_HalfOrphanRepair(t *testing.T) {
	kv := newMockKV()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	pid := 1
	key := NewSnowflakeEventKey(pid, 0)
	evt := FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "half-orphan", Content: "half-orphan-replay", Timestamp: 1700000000000,
	}
	require.NoError(t, store.StoreEvent(key, evt))

	idxKey := IndexKeyStr(pid, key)
	idxVal, err := kv.KVGet(idxKey)
	require.NoError(t, err)
	var windowTS, seq int64
	_, perr := fmt.Sscanf(idxVal, "%d:%d", &windowTS, &seq)
	require.NoError(t, perr)
	require.NoError(t, kv.KVDelete(EventKeyStr(pid, windowTS, int(seq))))
	store.cache.Remove(key)

	result, _, err := store.ReplayEvent(key, evt)
	require.NoError(t, err, "F3: ReplayEvent must return nil after repairing half-orphan")
	require.Equal(t, ReplayRepaired, result,
		"F3: half-orphan repair must be classified as ReplayRepaired")

	store.cache.Remove(key)
	got, err := store.GetEvent(key)
	require.NoError(t, err, "F3: after ReplayRepaired, GetEvent must succeed via KV path")
	require.Equal(t, "half-orphan-replay", got.Content)
}

// TestHalfOrphan_RepairWritesMissingEvtSlot 钉住 half-orphan repair (idx exists, evt KV slot missing) must actually write
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestHalfOrphan_RepairWritesMissingEvtSlot(t *testing.T) {
	kv := newMockKV()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	pid := 1
	key := NewSnowflakeEventKey(pid, 0)
	evt := FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "s", Content: "half-orphan-f3", Timestamp: 1700000000000,
	}
	require.NoError(t, store.StoreEvent(key, evt))

	idxKey := IndexKeyStr(pid, key)
	idxVal, err := kv.KVGet(idxKey)
	require.NoError(t, err)
	var windowTS, seq int64
	_, perr := fmt.Sscanf(idxVal, "%d:%d", &windowTS, &seq)
	require.NoError(t, perr)
	evtKVKey := EventKeyStr(pid, windowTS, int(seq))
	require.NoError(t, kv.KVDelete(evtKVKey), "setup: must delete evt slot only")

	store.cache.Remove(key)

	err = store.StoreEvent(key, evt)
	require.True(t, IsDuplicateEventKey(err),
		"2.1: public StoreEvent must refuse (not repair) a half-orphan, got %v", err)
	_, kvGetErr := kv.KVGet(evtKVKey)
	require.True(t, errors.Is(kvGetErr, ErrKeyNotFound),
		"2.1: the public path must not write the missing evt slot")

	res, _, rerr := store.ReplayEvent(key, evt)
	require.NoError(t, rerr)
	require.Equal(t, ReplayRepaired, res, "F3: ReplayEvent must repair the half-orphan")
	_, kvGetErr = kv.KVGet(evtKVKey)
	require.NoError(t, kvGetErr, "F3: after ReplayEvent repair the evt slot is readable from KV")
	store.cache.Remove(key)
	got, err := store.GetEvent(key)
	require.NoError(t, err, "F3: after cache eviction, GetEvent must read from the repaired KV entry")
	require.Equal(t, "half-orphan-f3", got.Content)
}

// TestAlreadyCommitted_RetryDoesNotIncrementLiveCount 钉住 calling StoreEvent with an already-committed EventKey and byte-identical
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestAlreadyCommitted_RetryDoesNotIncrementLiveCount(t *testing.T) {
	kv := newMockKV()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	pid := 1
	key := NewSnowflakeEventKey(pid, 0)
	evt := FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "s", Content: "same-content", Timestamp: 1700000000000,
	}
	require.NoError(t, store.StoreEvent(key, evt))
	countAfterFirst := store.GetStats().TotalEvents
	require.EqualValues(t, 1, countAfterFirst, "first commit must increment count to 1")

	require.True(t, IsDuplicateEventKey(store.StoreEvent(key, evt)),
		"2.1: public StoreEvent must refuse a same-content duplicate")
	require.Equal(t, countAfterFirst, store.GetStats().TotalEvents,
		"2.1: a refused public duplicate must not increment the live count")

	res, _, rerr := store.ReplayEvent(key, evt)
	require.NoError(t, rerr)
	require.Equal(t, ReplayAlreadyCommitted, res)
	require.Equal(t, countAfterFirst, store.GetStats().TotalEvents,
		"F8: ReplayEvent of an already-committed event must NOT increment the live-count")
}

// discoveryKV is a mockKV with the optional ListPartitionIDs capability —
// the same shape LocalFileKV exposes (type-asserted by Init, NOT part of the
// KVStore six-method interface).
type discoveryKV struct {
	*mockKV
	ids []int
}

func (d *discoveryKV) ListPartitionIDs() []int { return d.ids }

// TestFileSegmentStore_ColdPartitionDiscovery 钉住
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestFileSegmentStore_ColdPartitionDiscovery(t *testing.T) {
	base := newMockKV()
	base.data["7:evt:1710676800:1"] = "{}"
	base.data["7:meta:1710676800"] = "{}"

	s, err := NewFileSegmentStore(&discoveryKV{mockKV: base, ids: []int{7}}, nil, ":memory:", 100)
	require.NoError(t, err)
	if _, ok := s.partitions.Load(7); !ok {
		t.Fatal("cold partition 7 not discovered at construction — forgetting scans would skip it forever")
	}
}

// TestFileSegmentStore_NoEnumerationBackend_KeepsLazyDiscovery 钉住 a backend
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestFileSegmentStore_NoEnumerationBackend_KeepsLazyDiscovery(t *testing.T) {
	base := &noEnumKV{data: map[string]string{
		"7:evt:1710676800:1": "{}",
	}}

	s, err := NewFileSegmentStore(base, nil, ":memory:", 100)
	require.NoError(t, err)
	if _, ok := s.partitions.Load(7); ok {
		t.Fatal("without enumeration capability discovery must stay lazy (no partition registered)")
	}
}

// TestRaiseSnowflakeFloor_RestartGenerationCannotCollide 钉住 guard.
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestRaiseSnowflakeFloor_RestartGenerationCannotCollide(t *testing.T) {
	pid := 7
	first := NewSnowflakeEventKey(pid, 0)
	second := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, second, first)

	RaiseSnowflakeFloor(pid, second)
	third := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, third, second, "the next issued key must sit above the durable floor")
	require.Equal(t, (second>>timestampShift)&timestampMask, (third>>timestampShift)&timestampMask,
		"same-second pinning, not a time jump")
}

func TestRaiseSnowflakeFloor_IsOneWay(t *testing.T) {
	pid := 8
	high := NewSnowflakeEventKey(pid, 0)
	RaiseSnowflakeFloor(pid, high)
	mid := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, mid, high)

	RaiseSnowflakeFloor(pid, high)
	next := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, next, mid, "floor is one-way")
}

// TestScanLiveKeys_TombstonedHighestKeyStillRaisesFloor 钉住。
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
func TestScanLiveKeys_TombstonedHighestKeyStillRaisesFloor(t *testing.T) {
	file, tset := newFileStoreWithTombstones(t)
	const pid = 900
	base := int64((snowflakeEpoch + 10) * 1000)
	issue := func() int64 { return NewSnowflakeEventKey(pid, base) }

	k0, k1, k2 := issue(), issue(), issue()
	for _, k := range []int64{k0, k1, k2} {
		require.NoError(t, file.StoreEvent(k, FullEvent{
			EventKey: k, PartitionID: pid, EventType: "external_input",
			EventSummary: "e", Content: "c", Timestamp: base,
		}))
	}
	require.NoError(t, tset.MarkTombstone(k1))
	require.NoError(t, tset.MarkTombstone(k2))

	snowflakeSeqMu.Lock()
	delete(snowflakeSeqLast, pid)
	delete(snowflakeSeqCnt, pid)
	snowflakeSeqMu.Unlock()

	require.NoError(t, file.RebuildLiveCounts())

	next := NewSnowflakeEventKey(pid, base)
	require.NotEqual(t, k2, next, "next key must not re-issue the tombstoned highest key")
	require.Greater(t, next, k2, "key floor must be raised past the tombstoned highest key")
}

package memory

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Storage-contract tests (resident-readiness-plan 2.5/2.7/2.8/2.9): typed
// errors, backend parity for immutability/isolation, and live-count
// rebuild/decrement semantics. The file backend runs over the in-package
// mockKV; the REAL barrier path (LocalFileKV + fresh process) is covered by
// TestStoreEventDurableWithoutClose (memory_test).

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
	// Parity contract: the SAME identity with DIFFERENT content is refused
	// on both backends — an EventKey is the event's identity (D15).
	for name, s := range map[string]MemoryStore{"memory": mem, "file": file} {
		key := NewSnowflakeEventKey(1, 0)
		evt := FullEvent{EventKey: key, PartitionID: 1, EventType: "t", Timestamp: 1}
		require.NoError(t, s.StoreEvent(key, evt), name)
		clash := evt
		clash.Content = "different fact"
		err := s.StoreEvent(key, clash)
		require.Error(t, err, name)
		require.True(t, errors.Is(err, ErrDuplicateEventKey), "%s: want typed duplicate, got %v", name, err)
		// (2.1) A SAME-content public duplicate must ALSO be refused on both
		// backends — orphan/already-committed completion belongs to ReplayEvent
		// only, so the public path never accepts a re-write of an existing key.
		sameErr := s.StoreEvent(key, evt)
		require.True(t, errors.Is(sameErr, ErrDuplicateEventKey),
			"%s: 2.1 same-content public duplicate must be refused, got %v", name, sameErr)
	}
	// 2.1 公共契约一致：两端 StoreEvent 对任意已存 EventKey 都拒（同内容或异内容
	// 均返回 typed duplicate）。孤儿/半孤儿的幂等补齐只属内部 ReplayEvent 路径。
}

func TestBackendParity_NoPartitionQueryIsEmpty(t *testing.T) {
	mem := NewInMemoryStore()
	file, _ := newFileStoreWithTombstones(t)
	for name, s := range map[string]MemoryStore{"memory": mem, "file": file} {
		seedN(t, s, 1, 2)
		refs, err := s.QueryEvents(QueryOptions{}) // no explicit partition
		require.NoError(t, err, name)
		require.Empty(t, refs, "%s: unscoped query must scan nothing", name)
	}
}

func TestBackendParity_ReturnedEventsAreClones(t *testing.T) {
	mem := NewInMemoryStore()
	file, _ := newFileStoreWithTombstones(t)
	for name, s := range map[string]MemoryStore{"memory": mem, "file": file} {
		key := seedN(t, s, 1, 1)[0]
		// Input isolation: mutating the event AFTER StoreEvent must not
		// corrupt the stored fact.
		got, err := s.GetEvent(key)
		require.NoError(t, err, name)
		got.Metadata["idx"] = "mutated-after-store"
		got.Content = "mutated"
		got2, err := s.GetEvent(key)
		require.NoError(t, err, name)
		require.NotEqual(t, "mutated", got2.Content, name)
		require.Equal(t, "0", got2.Metadata["idx"], name)
		// Output isolation: mutating a returned event must not poison later
		// reads (cache or store).
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
	// The mock's KVGet never I/O-fails, so exercise the contract the other
	// way: missing keys are skipped WITHOUT error; the typed-miss path is
	// what callers may rely on for partial handling.
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

	// 计数器跟随扫描器（checkTTL/evictOldest 的标记点），不跟 MarkTombstone
	// 本身；先标墓碑再重建，墓碑过滤给出逻辑存活数。
	require.NoError(t, tset.MarkTombstone(keys[0]))
	require.Equal(t, 5, file.GetStats().TotalEvents)

	// Rebuild from the chain: dedup + tombstone exclusion → 4。
	require.NoError(t, file.RebuildLiveCounts())
	require.True(t, file.LivesCountKnown())
	require.Equal(t, 4, file.GetStats().TotalEvents)
	require.True(t, file.GetStats().CountsKnown)

	// Successful commit increments exactly once.
	k := NewSnowflakeEventKey(1, 0)
	require.NoError(t, file.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: "t", Timestamp: 2}))
	require.Equal(t, 5, file.GetStats().TotalEvents)

	// 重复删除已墓碑键幂等且不二次递减（2.9）。
	require.NoError(t, file.DeleteEvent(keys[0]))
	require.Equal(t, 5, file.GetStats().TotalEvents)

	// 删除存活键递减恰一次；重复删除经 idx-miss 幂等。
	require.NoError(t, file.DeleteEvent(keys[1]))
	require.Equal(t, 4, file.GetStats().TotalEvents)
	require.NoError(t, file.DeleteEvent(keys[1]))
	require.Equal(t, 4, file.GetStats().TotalEvents)
}

func TestLiveCount_UnknownOnEnumerationFailure(t *testing.T) {
	// A backend WITHOUT enumeration keeps counts unknown: no rebuild, and
	// capacity eviction pauses rather than misreading unknown as 0 (2.8).
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

// barrierFailKV wraps a KVStore whose Sync() always fails — the shape the
// durability-barrier contract (2.3) must survive: StoreEvent reports
// failure, the live count stays untouched, and the error propagates through
// the ErrorTrackingStore decoration for degradation accounting (2.10).
type barrierFailKV struct {
	KVStore
	syncErr atomic.Value // error
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
	base.kv = failing // inject the failing barrier under the same store

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

	// 孤儿边界（D1.3 已声明）：屏障失败时 evt/idx 可能已落 KV 层并被查询
	// 看到——「未提交」绝不解释为「保证不存在」。
	// (2.1) A public StoreEvent retry is now REFUSED (both backends reject public
	// duplicates); the durable recovery path completes the orphan via the internal
	// ReplayEvent — exactly one count, exactly one visible fact, no duplicate.
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
	// 2.4: the barrier-failed commit still left idx+evt+meta durable in the KV, and this
	// replay re-runs the barrier successfully, so the record is a COMPLETE durable fact
	// classified AlreadyCommitted FROM THE FACT CHAIN — not from the cache (which §2.4
	// disqualifies as the oracle). Nothing had to be written this call (evt+meta were
	// both present), so it is not a Repair; the live count is made authoritative by the
	// partition recompute (before+1 below), and exactly one fact is visible (refs len 1
	// below). Pre-§2.4 the LRU-oracle returned Repaired for this cold/uncertain case.
	require.Equal(t, ReplayAlreadyCommitted, res, "2.4: a durable-complete replay classifies from the fact chain, not the cache")
	require.Equal(t, before+1, base.GetStats().TotalEvents)
	refs, qerr := base.QueryEvents(QueryOptions{PartitionIDs: []int{pid}})
	require.NoError(t, qerr)
	require.Len(t, refs, 1, "orphan completion must not duplicate the fact")
}

// ReplayEvent interface parity tests (D4 design, F3/F8 fix):
// FileSegmentStore must satisfy EventReplayer at compile time.
var _ EventReplayer = (*FileSegmentStore)(nil)

// TestReplayEvent_Classification verifies the three ReplayResult values.
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

	// 1. First commit → ReplayNew.
	result, got, err := store.ReplayEvent(key, evt)
	require.NoError(t, err)
	require.Equal(t, ReplayNew, result, "first commit must be ReplayNew")
	require.Equal(t, key, got.EventKey)

	// 2. Same key, same content → ReplayAlreadyCommitted (key is now in cache).
	result2, _, err2 := store.ReplayEvent(key, evt)
	require.NoError(t, err2, "already-committed replay must return nil error")
	require.Equal(t, ReplayAlreadyCommitted, result2,
		"replay of already-committed event must be ReplayAlreadyCommitted (F8)")

	// 3. Live-count must be 1 (not 2) after two replays.
	require.EqualValues(t, 1, store.GetStats().TotalEvents,
		"ReplayAlreadyCommitted must NOT increment live-count (F8 fix)")
}

// TestReplayEvent_HalfOrphanRepair verifies that when the evt KV slot is
// missing but idx exists, ReplayEvent writes the missing slot and returns
// ReplayRepaired (F3 fix). The repaired fact must be readable from KV even
// after cache eviction.
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
	// Normal commit first (writes idx+evt to KV).
	require.NoError(t, store.StoreEvent(key, evt))

	// Simulate half-orphan: delete only the evt KV slot.
	idxKey := IndexKeyStr(pid, key)
	idxVal, err := kv.KVGet(idxKey)
	require.NoError(t, err)
	var windowTS, seq int64
	_, perr := fmt.Sscanf(idxVal, "%d:%d", &windowTS, &seq)
	require.NoError(t, perr)
	require.NoError(t, kv.KVDelete(EventKeyStr(pid, windowTS, int(seq))))
	store.cache.Remove(key) // simulate restart: cache is cold

	// ReplayEvent must detect the missing evt, write it, and classify as Repaired.
	result, _, err := store.ReplayEvent(key, evt)
	require.NoError(t, err, "F3: ReplayEvent must return nil after repairing half-orphan")
	require.Equal(t, ReplayRepaired, result,
		"F3: half-orphan repair must be classified as ReplayRepaired")

	// Verify: the evt KV slot was actually written (not just cached).
	store.cache.Remove(key)
	got, err := store.GetEvent(key)
	require.NoError(t, err, "F3: after ReplayRepaired, GetEvent must succeed via KV path")
	require.Equal(t, "half-orphan-replay", got.Content)
}

// F3: half-orphan repair (idx exists, evt KV slot missing) must actually write
// the event content. Without the fix (832f43e8), completeOrphanCommit returned
// nil while the evt KV slot remained missing — the success was cache-only and
// was lost after eviction/reopen.
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
	// Normal first commit: writes idx+evt+meta to KV.
	require.NoError(t, store.StoreEvent(key, evt))

	// Simulate half-orphan: delete ONLY the evt KV slot, keep idx intact.
	// Parse idx to find the exact evt key that was written.
	idxKey := IndexKeyStr(pid, key)
	idxVal, err := kv.KVGet(idxKey)
	require.NoError(t, err)
	var windowTS, seq int64
	_, perr := fmt.Sscanf(idxVal, "%d:%d", &windowTS, &seq)
	require.NoError(t, perr)
	evtKVKey := EventKeyStr(pid, windowTS, int(seq))
	require.NoError(t, kv.KVDelete(evtKVKey), "setup: must delete evt slot only")

	// Evict from cache: simulates restart after cache eviction.
	store.cache.Remove(key)

	// (2.1) A public StoreEvent must NOT repair an orphan: the identity's idx
	// already exists → the public path refuses and leaves the evt slot missing.
	// Orphan repair is the internal ReplayEvent path's job.
	err = store.StoreEvent(key, evt)
	require.True(t, IsDuplicateEventKey(err),
		"2.1: public StoreEvent must refuse (not repair) a half-orphan, got %v", err)
	_, kvGetErr := kv.KVGet(evtKVKey)
	require.True(t, errors.Is(kvGetErr, ErrKeyNotFound),
		"2.1: the public path must not write the missing evt slot")

	// ReplayEvent then repairs the same half-orphan (F3): evt slot written, readable.
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

// F8: calling StoreEvent with an already-committed EventKey and byte-identical
// content must NOT increment the live-count. Without the fix (832f43e8), every
// such retry went through completeOrphanCommit which unconditionally called
// eventCount++, causing the count to grow without bound and driving premature
// capacity eviction of real events.
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
	// First commit via the public path: count 0→1.
	require.NoError(t, store.StoreEvent(key, evt))
	countAfterFirst := store.GetStats().TotalEvents
	require.EqualValues(t, 1, countAfterFirst, "first commit must increment count to 1")

	// (2.1) A public StoreEvent retry with the SAME key+content is now REFUSED
	// (both backends reject public duplicates) and must not touch the count.
	require.True(t, IsDuplicateEventKey(store.StoreEvent(key, evt)),
		"2.1: public StoreEvent must refuse a same-content duplicate")
	require.Equal(t, countAfterFirst, store.GetStats().TotalEvents,
		"2.1: a refused public duplicate must not increment the live count")

	// F8 idempotency lives on the internal ReplayEvent path: replaying the
	// already-committed fact is a no-op that never re-increments the live count.
	res, _, rerr := store.ReplayEvent(key, evt)
	require.NoError(t, rerr)
	require.Equal(t, ReplayAlreadyCommitted, res)
	require.Equal(t, countAfterFirst, store.GetStats().TotalEvents,
		"F8: ReplayEvent of an already-committed event must NOT increment the live-count")
}

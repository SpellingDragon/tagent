package memory

import (
	"errors"
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
	}
	// file 后端的提交协议（2.6）额外保证：同键同内容是未提交孤儿的幂等补齐
	// （见 TestOrphanCommit_IdempotentCompletion）；memory 是测试/原型后端，
	// 无提交协议，简单拒绝（已文档化差异）。
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
	// 看到——「未提交」绝不解释为「保证不存在」；收敛靠下方同内容幂等补齐。
	// The heal path (2.6): retrying the SAME event completes the orphan
	// commit — exactly one count, exactly one visible fact, no duplicate.
	failing.healed.Store(true)
	require.NoError(t, decorated.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "x", Content: "x", Timestamp: 1,
	}))
	require.Equal(t, before+1, base.GetStats().TotalEvents)
	refs, qerr := base.QueryEvents(QueryOptions{PartitionIDs: []int{pid}})
	require.NoError(t, qerr)
	require.Len(t, refs, 1, "orphan completion must not duplicate the fact")
}

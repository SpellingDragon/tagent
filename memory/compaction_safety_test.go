package memory

import (
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// scanFailOnceKV fails the FIRST KVScan on failPrefix, then behaves normally —
// the shape of a transient backend error (merge scan fails, delete scan works).
type scanFailOnceKV struct {
	*mockKV
	failPrefix string
	failed     atomic.Bool
}

func (s *scanFailOnceKV) KVScan(prefix string, limit int) ([]KVPair, error) {
	if s.failPrefix != "" && !s.failed.Load() && strings.HasPrefix(prefix, s.failPrefix) {
		s.failed.Store(true)
		return nil, errors.New("transient backend scan failure (review repro)")
	}
	return s.mockKV.KVScan(prefix, limit)
}

func TestMergeScanErrorMustAbortCompaction(t *testing.T) {
	kv := &scanFailOnceKV{mockKV: newMockKV()}
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	baseTS := int64(1710666000000)
	keysByHour := map[int]int64{}
	for hour := 0; hour < 2; hour++ {
		hourTS := baseTS + int64(hour)*3600000
		key := NewSnowflakeEventKey(1, hourTS)
		keysByHour[hour] = key
		require.NoError(t, store.StoreEvent(key, FullEvent{
			PartitionID:  1,
			EventType:    "test",
			EventSummary: "hourly event",
			Timestamp:    hourTS,
		}))
		require.NoError(t, store.SealCurrent(1))
	}

	w0 := WindowTimestamp(TimestampFromEventKey(keysByHour[0]), DefaultWindowSize)

	kv.failPrefix = SegmentEventPrefix(1, w0)

	compactor := NewCompactor(store, kv, store.rel, nil, DefaultCompactionConfig())

	err = compactor.CompactL1ToL2(1, []int64{w0, w0 + DefaultWindowSize})
	require.Error(t, err, "compaction must abort when a source window cannot be read")
	require.ErrorContains(t, err, "merge scan failed")

	_, evtErr := kv.KVGet(EventKeyStr(1, w0, 0))
	require.NoError(t, evtErr, "source evt slot must survive the aborted compaction")

	cold, cerr := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, cerr)
	_, gerr := cold.GetEvent(keysByHour[0])
	require.NoError(t, gerr, "window-0 fact must survive the aborted compaction (P1 regression gate)")
	_, gerr1 := cold.GetEvent(keysByHour[1])
	require.NoError(t, gerr1, "window-1 fact must remain readable")
}

// TestMergeScanErrorAbortsAndRetries 钉住 scheduler-retry closure promised
//
// 契约: docs/wiki/memory/memory-architecture.md#compaction-integrity
func TestMergeScanErrorAbortsAndRetries(t *testing.T) {
	kv := &scanFailOnceKV{mockKV: newMockKV()}
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	baseTS := int64(1710666000000)
	keys := make([]int64, 2)
	for hour := 0; hour < 2; hour++ {
		hourTS := baseTS + int64(hour)*3600000
		keys[hour] = NewSnowflakeEventKey(1, hourTS)
		require.NoError(t, store.StoreEvent(keys[hour], FullEvent{
			PartitionID: 1, EventType: "test", EventSummary: "hourly event", Timestamp: hourTS,
		}))
		require.NoError(t, store.SealCurrent(1))
	}
	w0 := WindowTimestamp(TimestampFromEventKey(keys[0]), DefaultWindowSize)
	windows := []int64{w0, w0 + DefaultWindowSize}

	kv.failPrefix = SegmentEventPrefix(1, w0)
	compactor := NewCompactor(store, kv, store.rel, nil, DefaultCompactionConfig())

	require.Error(t, compactor.CompactL1ToL2(1, windows), "aborted round must report the failure")
	_, evtErr := kv.KVGet(EventKeyStr(1, w0, 0))
	require.NoError(t, evtErr, "aborted round must not destroy the unreadable source")

	require.NoError(t, compactor.CompactL1ToL2(1, windows), "retry after recovery must succeed")
	cold, cerr := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, cerr)
	for i, k := range keys {
		_, gerr := cold.GetEvent(k)
		require.NoError(t, gerr, "window-%d fact must survive abort+retry compaction", i)
	}
}

func TestReplayEventSealedWindowDemotion(t *testing.T) {
	kv := newMockKV()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)

	ts := int64(1710678000000)
	t1, t2 := ts, ts+60_000
	for _, tt := range []int64{t1, t2} {
		require.NoError(t, store.StoreEvent(NewSnowflakeEventKey(1, tt), FullEvent{
			PartitionID: 1, EventType: "test", EventSummary: "envelope fact", Timestamp: tt,
		}))
	}
	w := WindowTimestamp(TimestampFromEventKey(NewSnowflakeEventKey(1, ts)), DefaultWindowSize)

	meta, merr := store.GetSegmentMeta(1, w)
	require.NoError(t, merr)
	meta.Sealed = true
	meta.MinTime, meta.MaxTime = t1, t2
	mj, _ := json.Marshal(meta)
	require.NoError(t, kv.KVPut(MetaKeyStr(1, w), string(mj)))

	t3 := t2 + 1800_000
	key3 := NewSnowflakeEventKey(1, t3)
	cold, cerr := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, cerr)
	res, _, rerr := cold.ReplayEvent(key3, FullEvent{
		PartitionID: 1, EventType: "test", EventSummary: "late replay fact", Timestamp: t3,
	})
	require.NoError(t, rerr)
	require.Equal(t, ReplayNew, res)

	_, gerr := cold.GetEvent(key3)
	require.NoError(t, gerr, "GetEvent must reach the replayed fact")

	refs, qerr := cold.QueryEvents(QueryOptions{
		PartitionIDs: []int{1},
		StartTime:    t2 + 1,
		EndTime:      t3 + 1,
	})
	require.NoError(t, qerr)
	require.NotEmpty(t, refs, "review P3 evidence: replayed fact invisible to time-range query — ReplayEvent lacks StoreEvent's sealed-window demotion")
}

package memory

// Review-evidence reproductions (2026-09-27 deep review). These tests FAIL by
// design while the defects below are unfixed; they are the executable evidence
// for the findings and double as the regression tests for their fixes:
//
//   - TestMergeScanErrorMustAbortCompaction: compaction.go mergeEvents
//     `continue`s on a KVScan error, so a transient scan failure on a source
//     window silently drops that window's events from the merge while
//     deleteSegments (whose scan recovered) still deletes the source — an
//     unrecoverable, unlogged loss of facts. Reproduces with a fail-once KV.
//
//   - TestReplayEventSealedWindowDemotion: segment_store.go ReplayEvent
//     lacks StoreEvent's sealed-window demotion, so a fresh replay committed
//     into a sealed window whose (MinTime,MaxTime) envelope predates the fact
//     is pruned from time-range queries until the next compaction rebuilds the
//     bounds. GetEvent stays reachable — only QueryEvents misses.

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

	// Two hourly windows, one event each.
	baseTS := int64(1710666000000) // 2024-03-17 05:00:00 UTC
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

	// Arm AFTER the writes: the next scan of window 0's event prefix fails —
	// which is the merge read inside CompactL1ToL2 (the delete-side scan that
	// follows has recovered, the transient-error shape under review).
	kv.failPrefix = SegmentEventPrefix(1, w0)

	// NewCompactor does not start its scheduler (Start does), so no background
	// compaction can interfere with this deterministic repro.
	compactor := NewCompactor(store, kv, store.rel, nil, DefaultCompactionConfig())

	// Correct behavior (spec: compaction 源窗口读失败必须中止本轮): the failed
	// source read aborts this compaction LOUD instead of silently migrating a
	// partial set. (Evidence-phase note: this assertion was originally written
	// to document the silent-success defect; corrected to the spec'd behavior
	// at fix time — it is the regression gate now.)
	err = compactor.CompactL1ToL2(1, []int64{w0, w0 + DefaultWindowSize})
	require.Error(t, err, "compaction must abort when a source window cannot be read")
	require.ErrorContains(t, err, "merge scan failed")

	// The aborted compaction destroyed nothing: window 0's evt slot is intact.
	_, evtErr := kv.KVGet(EventKeyStr(1, w0, 0))
	require.NoError(t, evtErr, "source evt slot must survive the aborted compaction")

	// Cold-cache read (a fresh store instance over the same KV, i.e. what a
	// restart sees): both windows' facts are retrievable — the P1 defect lost
	// window 0 silently behind a warm LRU.
	cold, cerr := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, cerr)
	_, gerr := cold.GetEvent(keysByHour[0])
	require.NoError(t, gerr, "window-0 fact must survive the aborted compaction (P1 regression gate)")
	_, gerr1 := cold.GetEvent(keysByHour[1])
	require.NoError(t, gerr1, "window-1 fact must remain readable")
}

// TestMergeScanErrorAbortsAndRetries is the scheduler-retry closure promised
// by spec scenario "后续 scheduler 轮次重试同一 compaction": after an aborted
// round the source segments stay intact, and the NEXT round (backend recovered)
// completes the migration with every fact preserved.
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

	// Round 1: transient read failure → abort loud, source intact.
	require.Error(t, compactor.CompactL1ToL2(1, windows), "aborted round must report the failure")
	_, evtErr := kv.KVGet(EventKeyStr(1, w0, 0))
	require.NoError(t, evtErr, "aborted round must not destroy the unreadable source")

	// Round 2 (scheduler retry, backend recovered): migration completes and
	// BOTH windows' facts survive on the cold path.
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

	// Two facts in window W with event times [T1, T2]...
	ts := int64(1710678000000)
	t1, t2 := ts, ts+60_000
	for _, tt := range []int64{t1, t2} {
		require.NoError(t, store.StoreEvent(NewSnowflakeEventKey(1, tt), FullEvent{
			PartitionID: 1, EventType: "test", EventSummary: "envelope fact", Timestamp: tt,
		}))
	}
	w := WindowTimestamp(TimestampFromEventKey(NewSnowflakeEventKey(1, ts)), DefaultWindowSize)

	// ...then the segment gets sealed with that truthful envelope (this is the
	// state an hourly seal or an L1→L2 promotion leaves behind).
	meta, merr := store.GetSegmentMeta(1, w)
	require.NoError(t, merr)
	meta.Sealed = true
	meta.MinTime, meta.MaxTime = t1, t2
	mj, _ := json.Marshal(meta)
	require.NoError(t, kv.KVPut(MetaKeyStr(1, w), string(mj)))

	// A recovery replay lands a FRESH fact in the same window whose event time
	// (t3) postdates the sealed envelope — the shape StoreEvent defends against
	// by demoting Sealed→false, and ReplayEvent currently does not. The replay
	// runs on a COLD store instance, the real recovery shape (durable replay
	// happens after a restart: the sealed window is not the fresh process's
	// current window, so the seqCounter==0 revisit path is taken).
	t3 := t2 + 1800_000 // +30min: same hour window, past the sealed envelope
	key3 := NewSnowflakeEventKey(1, t3)
	cold, cerr := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, cerr)
	res, _, rerr := cold.ReplayEvent(key3, FullEvent{
		PartitionID: 1, EventType: "test", EventSummary: "late replay fact", Timestamp: t3,
	})
	require.NoError(t, rerr)
	require.Equal(t, ReplayNew, res)

	// GetEvent stays reachable (idx → slot direct)...
	_, gerr := cold.GetEvent(key3)
	require.NoError(t, gerr, "GetEvent must reach the replayed fact")

	// ...but the time-range query [t2+1, t3+1] must ALSO see it. UNFIXED the
	// sealed envelope (MaxTime=t2 < StartTime) prunes window w wholesale and
	// the fact is invisible to recall until the next compaction rebuilds bounds.
	refs, qerr := cold.QueryEvents(QueryOptions{
		PartitionIDs: []int{1},
		StartTime:    t2 + 1,
		EndTime:      t3 + 1,
	})
	require.NoError(t, qerr)
	require.NotEmpty(t, refs, "review P3 evidence: replayed fact invisible to time-range query — ReplayEvent lacks StoreEvent's sealed-window demotion")
}

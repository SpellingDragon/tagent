package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const qrPID = 7

// qrSeed writes one event into the pid=qrPID partition at the given
// millisecond timestamp, returning its EventKey.
func qrSeed(t *testing.T, store *FileSegmentStore, tsMs int64, summary string) int64 {
	t.Helper()
	key := NewSnowflakeEventKey(qrPID, tsMs)
	require.NoError(t, store.StoreEvent(key, FullEvent{
		EventKey:     key,
		PartitionID:  qrPID,
		EventType:    "agent_output",
		EventSummary: summary,
		Content:      summary,
		Timestamp:    tsMs,
	}))
	return key
}

// qrHourly returns a millisecond timestamp `hoursAgo` hours before a fixed
// hour-aligned base, offset by `idx` seconds inside that hour.
func qrHourly(hoursAgo, idx int) int64 {
	base := int64(1785000000)
	return (base - int64(hoursAgo)*3600 + int64(idx)) * 1000
}

func qrKeys(refs []EventReference) []int64 {
	out := make([]int64, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.EventKey)
	}
	return out
}

// TestQueryEvents_RecencyFirstWhenOverLimit 钉住 three time
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_RecencyFirstWhenOverLimit(t *testing.T) {
	store := newTestSegmentStore(t)

	newestKeys := map[int64]bool{}
	for _, hoursAgo := range []int{48, 24, 0} {
		for i := 0; i < 10; i++ {
			k := qrSeed(t, store, qrHourly(hoursAgo, i), "彭伟业 简历评估")
			if hoursAgo == 0 {
				newestKeys[k] = true
			}
		}
	}

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		Keyword:      "彭伟业",
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 10)
	for _, r := range refs {
		assert.True(t, newestKeys[r.EventKey],
			"limit must sacrifice the oldest, never the newest (got event at %d)", r.Timestamp)
	}
}

// TestQueryEvents_RecentSemantics 钉住 no
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_RecentSemantics(t *testing.T) {
	store := newTestSegmentStore(t)

	var all []int64
	for _, hoursAgo := range []int{72, 48, 24, 0} {
		for i := 0; i < 8; i++ {
			all = append(all, qrSeed(t, store, qrHourly(hoursAgo, i), "event"))
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i] > all[j] })
	want := all[:20]

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		Limit:        20,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	assert.Equal(t, want, qrKeys(refs), "memory_recent must return the newest events")
}

// TestQueryEvents_MillisecondSinceFilter 钉住 StartTime/EndTime are Unix
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_MillisecondSinceFilter(t *testing.T) {
	store := newTestSegmentStore(t)

	qrSeed(t, store, qrHourly(48, 0), "old")
	recent := qrSeed(t, store, qrHourly(0, 5), "recent")

	since := qrHourly(0, 0)
	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    since,
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 1, "millisecond since must not prune every segment")
	assert.Equal(t, recent, refs[0].EventKey)
}

// TestQueryEvents_PruningMatchesEventFilter 钉住 window pruning must never
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_PruningMatchesEventFilter(t *testing.T) {
	store := newTestSegmentStore(t)

	for _, hoursAgo := range []int{5, 3, 1} {
		qrSeed(t, store, qrHourly(hoursAgo, 0), "ranged")
	}

	since, until := qrHourly(4, 0), qrHourly(2, 0)
	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    since,
		EndTime:      until,
		Limit:        50,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 1, "exactly the in-range event survives both filter levels")
	assert.GreaterOrEqual(t, refs[0].Timestamp, since)
	assert.LessOrEqual(t, refs[0].Timestamp, until)
}

// qrWriteL2Segment writes a daily-aligned L2 segment directly into the KV
// store (mimicking the compactor's output) and returns the event key.
func qrWriteL2Segment(t *testing.T, store *FileSegmentStore, dayWindowSec, tsMs int64, summary string) int64 {
	t.Helper()
	key := NewSnowflakeEventKey(qrPID, tsMs)
	evt := FullEvent{
		EventKey:     key,
		PartitionID:  qrPID,
		EventType:    "agent_output",
		EventSummary: summary,
		Content:      summary,
		Timestamp:    tsMs,
	}
	evtJSON, err := json.Marshal(evt)
	require.NoError(t, err)
	metaJSON, err := json.Marshal(SegmentMeta{
		PartitionID: qrPID,
		WindowTS:    dayWindowSec,
		Layer:       2,
		EventCount:  1,
		Sealed:      true,
	})
	require.NoError(t, err)
	require.NoError(t, store.kv.KVBatch([]KVOp{
		{Type: "put", Key: EventKeyStr(qrPID, dayWindowSec, 0), Value: string(evtJSON)},
		{Type: "put", Key: IndexKeyStr(qrPID, key), Value: fmt.Sprintf("%d:%d", dayWindowSec, 0)},
		{Type: "put", Key: MetaKeyStr(qrPID, dayWindowSec), Value: string(metaJSON)},
	}))
	return key
}

// TestQueryEvents_L2SegmentNotPrunedByHourlySpan 钉住 an L2 segment's window
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_L2SegmentNotPrunedByHourlySpan(t *testing.T) {
	store := newTestSegmentStore(t)

	dayWindow := (int64(1785000000) / 86400) * 86400
	afternoonMs := (dayWindow + 15*3600) * 1000
	key := qrWriteL2Segment(t, store, dayWindow, afternoonMs, "L2 event")

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    afternoonMs - 3600*1000,
		EndTime:      afternoonMs + 3600*1000,
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 1, "daily L2 segment must be scanned for an afternoon range")
	assert.Equal(t, key, refs[0].EventKey)
}

// TestQueryEvents_DedupKeepsHigherLayerBothDirections 钉住 in
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_DedupKeepsHigherLayerBothDirections(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 10)
	key := qrSeed(t, store, tsMs, "L1 原文版本")

	dayWindow := (tsMs / 1000 / 86400) * 86400
	evt := FullEvent{
		EventKey:     key,
		PartitionID:  qrPID,
		EventType:    "agent_output",
		EventSummary: "L2 摘要版本",
		Timestamp:    tsMs,
	}
	evtJSON, err := json.Marshal(evt)
	require.NoError(t, err)
	metaJSON, err := json.Marshal(SegmentMeta{
		PartitionID: qrPID, WindowTS: dayWindow, Layer: 2, EventCount: 1, Sealed: true,
	})
	require.NoError(t, err)
	require.NoError(t, store.kv.KVBatch([]KVOp{
		{Type: "put", Key: EventKeyStr(qrPID, dayWindow, 0), Value: string(evtJSON)},
		{Type: "put", Key: MetaKeyStr(qrPID, dayWindow), Value: string(metaJSON)},
	}))

	for _, orderBy := range []string{"timestamp_desc", "timestamp_asc"} {
		t.Run(orderBy, func(t *testing.T) {
			refs, err := store.QueryEvents(QueryOptions{
				PartitionIDs: []int{qrPID},
				Limit:        10,
				OrderBy:      orderBy,
			})
			require.NoError(t, err)
			require.Len(t, refs, 1, "a duplicated event must be returned once")
			assert.Equal(t, "L2 摘要版本", refs[0].EventSummary,
				"dedup must keep the higher-layer version independent of direction")
		})
	}
}

// TestQueryEvents_OffsetWithEarlyStop 钉住 (contract 1): offset+limit paging must
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_OffsetWithEarlyStop(t *testing.T) {
	store := newTestSegmentStore(t)

	var all []int64
	for _, hoursAgo := range []int{48, 24, 0} {
		for i := 0; i < 10; i++ {
			all = append(all, qrSeed(t, store, qrHourly(hoursAgo, i), "paged"))
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i] > all[j] })
	want := all[10:20]

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		Keyword:      "paged",
		Offset:       10,
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	assert.Equal(t, want, qrKeys(refs), "offset paging must match the declarative reference")
}

// TestQueryEvents_SameMillisecondStable 钉住 (contract 2): events sharing one
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_SameMillisecondStable(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 0)
	for i := 0; i < 8; i++ {
		qrSeed(t, store, tsMs, "tie")
	}

	var first []int64
	for run := 0; run < 5; run++ {
		refs, err := store.QueryEvents(QueryOptions{
			PartitionIDs: []int{qrPID},
			Limit:        8,
			OrderBy:      "timestamp_desc",
		})
		require.NoError(t, err)
		keys := qrKeys(refs)
		if run == 0 {
			first = keys
			assert.True(t, sort.SliceIsSorted(keys, func(i, j int) bool { return keys[i] > keys[j] }),
				"same-millisecond events must be ordered by descending EventKey")
			continue
		}
		assert.Equal(t, first, keys, "repeated queries must be positionally identical")
	}
}

// TestQueryEvents_StoreImplementationParity 钉住 two
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_StoreImplementationParity(t *testing.T) {
	fileStore := newTestSegmentStore(t)
	memStore := NewInMemoryStore()

	// Same event set in both stores, spread over multiple hour windows and
	// including same-millisecond ties.
	type seed struct {
		tsMs    int64
		summary string
	}
	var seeds []seed
	for _, hoursAgo := range []int{48, 24, 1, 0} {
		for i := 0; i < 6; i++ {
			s := seed{tsMs: qrHourly(hoursAgo, i), summary: "parity 彭伟业"}
			if i%2 == 0 {
				s.summary = "parity other"
			}
			seeds = append(seeds, s)
		}
		seeds = append(seeds,
			seed{tsMs: qrHourly(hoursAgo, 30), summary: "parity tie"},
			seed{tsMs: qrHourly(hoursAgo, 30), summary: "parity tie"},
		)
	}
	for _, s := range seeds {
		key := NewSnowflakeEventKey(qrPID, s.tsMs)
		evt := FullEvent{
			EventKey:     key,
			PartitionID:  qrPID,
			EventType:    "agent_output",
			EventSummary: s.summary,
			Content:      s.summary,
			Timestamp:    s.tsMs,
		}
		require.NoError(t, fileStore.StoreEvent(key, evt))
		require.NoError(t, memStore.StoreEvent(key, evt))
	}

	base := QueryOptions{PartitionIDs: []int{qrPID}}
	var matrix []QueryOptions
	for _, orderBy := range []string{"timestamp_desc", "timestamp_asc"} {
		for _, limit := range []int{1, 5, 20, 1000} {
			for _, offset := range []int{0, 3} {
				for _, keyword := range []string{"", "彭伟业", "tie"} {
					for _, rng := range [][2]int64{
						{0, 0},
						{qrHourly(24, 0), 0},
						{0, qrHourly(24, 0)},
						{qrHourly(48, 0), qrHourly(1, 0)},
					} {
						q := base
						q.OrderBy = orderBy
						q.Limit = limit
						q.Offset = offset
						q.Keyword = keyword
						q.StartTime, q.EndTime = rng[0], rng[1]
						matrix = append(matrix, q)
					}
				}
			}
		}
	}

	for i, q := range matrix {
		fileRefs, err := fileStore.QueryEvents(q)
		require.NoError(t, err)
		memRefs, err := memStore.QueryEvents(q)
		require.NoError(t, err)
		assert.Equal(t, qrKeys(memRefs), qrKeys(fileRefs),
			"case %d: implementations must agree (orderBy=%s limit=%d offset=%d keyword=%q since=%d until=%d)",
			i, q.OrderBy, q.Limit, q.Offset, q.Keyword, q.StartTime, q.EndTime)
	}
}

// TestKVScanLexicographicOrder 钉住 window-discovery phase relies on KVScan
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestKVScanLexicographicOrder(t *testing.T) {
	backends := map[string]KVStore{
		"mockKV": newMockKV(),
	}

	windows := []int64{1785010000, 1785000000, 1785030000, 1785020000}
	for name, kv := range backends {
		for _, w := range windows {
			require.NoError(t, kv.KVPut(MetaKeyStr(qrPID, w), "{}"), name)
		}
		pairs, err := kv.KVScan(MetaPrefix(qrPID), 0)
		require.NoError(t, err, name)
		require.Len(t, pairs, len(windows), name)

		keys := make([]string, 0, len(pairs))
		for _, p := range pairs {
			keys = append(keys, p.Key)
		}
		assert.True(t, sort.StringsAreSorted(keys),
			"%s: KVScan must return keys in lexicographic order, got %v", name, keys)
	}
}

const (
	// qrWithBounds 记录真实的 MinTime/MaxTime 包络，供查询剪枝使用。
	qrWithBounds = true
	// qrWithoutBounds 模拟未写包络的段：无包络即不可剪枝，必须恒扫描。
	qrWithoutBounds = false
)

// qrWriteWideSegment writes a compaction-style segment: nominal window is
// day-aligned (named after the earliest source window) while the events span
// `days` days beyond it — exactly the production shape (a "daily" L2 segment
// holding 57h of events). withBounds controls whether the truthful
// MinTime/MaxTime envelope is recorded（未写包络的段缺少它）。
func qrWriteWideSegment(t *testing.T, store *FileSegmentStore, dayWindowSec int64, tsList []int64, withBounds bool, summary string) []int64 {
	t.Helper()
	var keys []int64
	ops := make([]KVOp, 0, len(tsList)*2+1)
	for seq, tsMs := range tsList {
		key := NewSnowflakeEventKey(qrPID, tsMs)
		keys = append(keys, key)
		evt := FullEvent{
			EventKey:     key,
			PartitionID:  qrPID,
			EventType:    "agent_output",
			EventSummary: summary,
			Content:      summary,
			Timestamp:    tsMs,
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		ops = append(ops,
			KVOp{Type: "put", Key: EventKeyStr(qrPID, dayWindowSec, seq), Value: string(data)},
			KVOp{Type: "put", Key: IndexKeyStr(qrPID, key), Value: fmt.Sprintf("%d:%d", dayWindowSec, seq)},
		)
	}
	meta := SegmentMeta{
		PartitionID: qrPID, WindowTS: dayWindowSec, Layer: 2,
		EventCount: len(tsList), Sealed: true,
	}
	if withBounds {
		meta.MinTime = tsList[0]
		meta.MaxTime = tsList[len(tsList)-1]
	}
	metaJSON, err := json.Marshal(meta)
	require.NoError(t, err)
	ops = append(ops, KVOp{Type: "put", Key: MetaKeyStr(qrPID, dayWindowSec), Value: string(metaJSON)})
	require.NoError(t, store.kv.KVBatch(ops))
	return keys
}

// wideSegmentTs builds timestamps spanning day0 .. day0+2 inside a segment
// nominally covering only day0.
func wideSegmentTs(dayWindowSec int64) []int64 {
	return []int64{
		(dayWindowSec + 3600) * 1000,
		(dayWindowSec + 30*3600) * 1000,
		(dayWindowSec + 54*3600) * 1000,
	}
}

// TestQueryEvents_WideSegmentNotPrunedBySince 钉住 a compacted segment whose
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_WideSegmentNotPrunedBySince(t *testing.T) {
	store := newTestSegmentStore(t)
	dayWindow := (int64(1785000000) / 86400) * 86400
	tsList := wideSegmentTs(dayWindow)
	qrWriteWideSegment(t, store, dayWindow, tsList, qrWithBounds, "wide segment")

	since := (dayWindow + 30*3600) * 1000
	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    since,
		Limit:        50,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 2, "events beyond the nominal window end must remain visible")
	assert.Equal(t, tsList[2], refs[0].Timestamp)
	assert.Equal(t, tsList[1], refs[1].Timestamp)
}

// TestQueryEvents_WideSegmentNotSkippedByEarlyStop 钉住 desc traversal
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_WideSegmentNotSkippedByEarlyStop(t *testing.T) {
	store := newTestSegmentStore(t)
	dayWindow := (int64(1785000000) / 86400) * 86400
	tsList := wideSegmentTs(dayWindow)
	wideKeys := qrWriteWideSegment(t, store, dayWindow, tsList, qrWithBounds, "wide segment")

	hourlyTs := (dayWindow + 48*3600) * 1000
	hourlyKey := qrSeed(t, store, hourlyTs, "hourly newer window")

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		Limit:        1,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, wideKeys[2], refs[0].EventKey,
		"the wide segment holds the globally newest event; early-stop must not skip it")
	assert.NotEqual(t, hourlyKey, refs[0].EventKey)
}

// TestQueryEvents_LegacyWideSegmentNeverPruned 钉住 压实后的
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_LegacyWideSegmentNeverPruned(t *testing.T) {
	store := newTestSegmentStore(t)
	dayWindow := (int64(1785000000) / 86400) * 86400
	tsList := wideSegmentTs(dayWindow)
	qrWriteWideSegment(t, store, dayWindow, tsList, qrWithoutBounds, "legacy wide")

	since := (dayWindow + 30*3600) * 1000
	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    since,
		Limit:        50,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	assert.Len(t, refs, 2, "legacy segment without MaxTime must not be pruned")
}

// TestQueryEvents_StrictInequalityAtBoundary 钉住 when a candidate window's
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_StrictInequalityAtBoundary(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 0)
	newer := qrSeed(t, store, tsMs, "collected first")

	dayWindow := (tsMs / 1000 / 86400) * 86400
	tie := qrWriteWideSegment(t, store, dayWindow, []int64{tsMs}, qrWithBounds, "tie in wide segment")

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		Limit:        1,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	want := newer
	if tie[0] > newer {
		want = tie[0]
	}
	assert.Equal(t, want, refs[0].EventKey,
		"boundary equality must not skip the candidate window (strict inequality)")
}

// TestQueryEvents_DivergentWriteAndEventTime 钉住 EventKey embeds the WRITE
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_DivergentWriteAndEventTime(t *testing.T) {
	store := newTestSegmentStore(t)

	writeNow := qrHourly(0, 0)
	eventTimes := []int64{qrHourly(48, 0), qrHourly(5, 0), qrHourly(20, 0)}

	type seeded struct {
		key  int64
		tsMs int64
	}
	var all []seeded
	for _, evTs := range eventTimes {
		key := NewSnowflakeEventKey(qrPID, writeNow)
		require.NoError(t, store.StoreEvent(key, FullEvent{
			EventKey:     key,
			PartitionID:  qrPID,
			EventType:    "agent_output",
			EventSummary: "async write-back",
			Timestamp:    evTs,
		}))
		all = append(all, seeded{key: key, tsMs: evTs})
	}

	want := append([]seeded(nil), all...)
	sort.Slice(want, func(i, j int) bool {
		if want[i].tsMs != want[j].tsMs {
			return want[i].tsMs > want[j].tsMs
		}
		return want[i].key > want[j].key
	})
	wantKeys := make([]int64, 0, len(want))
	for _, w := range want {
		wantKeys = append(wantKeys, w.key)
	}

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	assert.Equal(t, wantKeys, qrKeys(refs),
		"ordering must follow Timestamp only, not the key-embedded write time")

	since, until := qrHourly(30, 0), qrHourly(10, 0)
	ranged, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    since,
		EndTime:      until,
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, ranged, 1, "only the event whose Timestamp falls in range qualifies")
	assert.Equal(t, qrHourly(20, 0), ranged[0].Timestamp)
}

// TestSealWritesTruthfulBounds 钉住 sealing is the LSM flush point — the
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestSealWritesTruthfulBounds(t *testing.T) {
	store := newTestSegmentStore(t)

	ts1, ts2 := qrHourly(0, 0), qrHourly(0, 30)
	qrSeed(t, store, ts1, "first")
	qrSeed(t, store, ts2, "second")

	require.NoError(t, store.SealCurrent(qrPID))

	w := (ts1 / 1000 / DefaultWindowSize) * DefaultWindowSize
	meta, err := store.GetSegmentMeta(qrPID, w)
	require.NoError(t, err)
	assert.True(t, meta.Sealed)
	assert.Equal(t, ts1, meta.MinTime)
	assert.Equal(t, ts2, meta.MaxTime)
}

// TestUnsealedSegmentIsMemtable 钉住 an active (unsealed) segment must
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestUnsealedSegmentIsMemtable(t *testing.T) {
	store := newTestSegmentStore(t)

	active := qrSeed(t, store, qrHourly(0, 5), "active memtable event")

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    qrHourly(72, 0),
		EndTime:      qrHourly(48, 0),
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	assert.Empty(t, refs, "range filter still applies at the event level")

	refs2, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    qrHourly(1, 0),
		EndTime:      qrHourly(0, 10),
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs2, 1)
	assert.Equal(t, active, refs2[0].EventKey)
}

// TestStoreEvent_SeqRecoveredAfterRestart 钉住 in-memory seqCounter
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestStoreEvent_SeqRecoveredAfterRestart(t *testing.T) {
	mockKV := newMockKV()
	store1, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	tsMs := qrHourly(0, 0)
	var firstKeys []int64
	for i := 0; i < 5; i++ {
		k := NewSnowflakeEventKey(qrPID, tsMs+int64(i))
		require.NoError(t, store1.StoreEvent(k, FullEvent{
			EventKey: k, PartitionID: qrPID, EventType: "agent_output",
			EventSummary: "before restart", Timestamp: tsMs + int64(i),
		}))
		firstKeys = append(firstKeys, k)
	}

	store2, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	newKey := NewSnowflakeEventKey(qrPID, tsMs+100)
	require.NoError(t, store2.StoreEvent(newKey, FullEvent{
		EventKey: newKey, PartitionID: qrPID, EventType: "agent_output",
		EventSummary: "after restart", Timestamp: tsMs + 100,
	}))

	evt, err := store2.GetEvent(newKey)
	require.NoError(t, err)
	assert.Equal(t, "after restart", evt.EventSummary)

	for i, k := range firstKeys {
		old, err := store2.GetEvent(k)
		require.NoError(t, err, "pre-restart event %d must survive", i)
		assert.Equal(t, "before restart", old.EventSummary)
	}
}

// TestStoreEvent_SeqRecoveredOnWindowRevisit 钉住 seq recovery must also
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestStoreEvent_SeqRecoveredOnWindowRevisit(t *testing.T) {
	store := newTestSegmentStore(t)

	w := (qrHourly(0, 0) / 1000 / DefaultWindowSize) * DefaultWindowSize
	var preKeys []int64
	for seq := 0; seq < 2; seq++ {
		k := NewSnowflakeEventKey(qrPID, qrHourly(0, seq))
		preKeys = append(preKeys, k)
		evt := FullEvent{EventKey: k, PartitionID: qrPID,
			EventType: "x", EventSummary: "pre-existing", Timestamp: qrHourly(0, seq)}
		data, _ := json.Marshal(evt)
		require.NoError(t, store.kv.KVPut(EventKeyStr(qrPID, w, seq), string(data)))
		require.NoError(t, store.kv.KVPut(IndexKeyStr(qrPID, k), fmt.Sprintf("%d:%d", w, seq)))
	}

	other := qrSeed(t, store, qrHourly(5, 0), "other window")
	_ = other
	k := NewSnowflakeEventKey(qrPID, qrHourly(0, 100))
	require.NoError(t, store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "revisit write", Timestamp: qrHourly(0, 100),
	}))

	evt, err := store.GetEvent(k)
	require.NoError(t, err)
	assert.Equal(t, "revisit write", evt.EventSummary)
	for _, pk := range preKeys {
		old, err := store.GetEvent(pk)
		require.NoError(t, err)
		assert.Equal(t, "pre-existing", old.EventSummary)
	}
}

// TestStoreEvent_SnowflakeCollisionRejected 钉住 an EventKey is the event's
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestStoreEvent_SnowflakeCollisionRejected(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 0)
	k := NewSnowflakeEventKey(qrPID, tsMs)
	require.NoError(t, store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "original", Timestamp: tsMs,
	}))

	err := store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "impostor", Timestamp: tsMs + 1,
	})
	require.Error(t, err, "rewriting an existing EventKey must be rejected")

	evt, getErr := store.GetEvent(k)
	require.NoError(t, getErr)
	assert.Equal(t, "original", evt.EventSummary)
}

// TestQueryEvents_MinTimeBelowWindowStart 钉住 a sealed segment containing an
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_MinTimeBelowWindowStart(t *testing.T) {
	store := newTestSegmentStore(t)

	dayWindow := (int64(1785000000) / 86400) * 86400
	oldTs := (dayWindow - 2*3600) * 1000
	midTs := (dayWindow + 3600) * 1000
	qrWriteWideSegment(t, store, dayWindow, []int64{oldTs, midTs}, qrWithBounds, "async write-back")

	until := (dayWindow - 3600) * 1000
	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		EndTime:      until,
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 1, "MinTime is the truthful lower bound — max() with window start drops it")
	assert.Equal(t, oldTs, refs[0].Timestamp)
}

// TestStoreEvent_RejectedCollisionLeavesNoGhost 钉住 a rejected collision
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestStoreEvent_RejectedCollisionLeavesNoGhost(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 0)
	k := NewSnowflakeEventKey(qrPID, tsMs)
	require.NoError(t, store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "original", Timestamp: tsMs,
	}))
	err := store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "impostor", Timestamp: tsMs + 1,
	})
	require.Error(t, err)

	refs, qErr := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID}, Limit: 10, OrderBy: "timestamp_desc",
	})
	require.NoError(t, qErr)
	require.Len(t, refs, 1, "a rejected write must not appear in segment scans")
	assert.Equal(t, "original", refs[0].EventSummary)
}

// TestStoreEvent_WriteIntoSealedWindowDemotesToMemtable 钉住 (m5): writing into a
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestStoreEvent_WriteIntoSealedWindowDemotesToMemtable(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 0)
	qrSeed(t, store, tsMs, "before seal")
	require.NoError(t, store.SealCurrent(qrPID))

	w := (tsMs / 1000 / DefaultWindowSize) * DefaultWindowSize
	meta, err := store.GetSegmentMeta(qrPID, w)
	require.NoError(t, err)
	require.True(t, meta.Sealed)

	qrSeed(t, store, qrHourly(5, 0), "other window")
	qrSeed(t, store, tsMs+60, "after seal")

	meta2, err := store.GetSegmentMeta(qrPID, w)
	require.NoError(t, err)
	assert.False(t, meta2.Sealed,
		"a window written after sealing must be demoted to memtable semantics")
}

// TestQueryEvents_ParityWithSealedAndWideSegments 钉住 (m2): the parity matrix
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestQueryEvents_ParityWithSealedAndWideSegments(t *testing.T) {
	fileStore := newTestSegmentStore(t)
	memStore := NewInMemoryStore()

	seedBoth := func(tsMs int64, summary string) {
		k := NewSnowflakeEventKey(qrPID, tsMs)
		evt := FullEvent{EventKey: k, PartitionID: qrPID, EventType: "agent_output",
			EventSummary: summary, Content: summary, Timestamp: tsMs}
		require.NoError(t, fileStore.StoreEvent(k, evt))
		require.NoError(t, memStore.StoreEvent(k, evt))
	}

	for _, hoursAgo := range []int{48, 24, 0} {
		for i := 0; i < 4; i++ {
			seedBoth(qrHourly(hoursAgo, i), "parity 彭伟业")
		}
		if hoursAgo > 0 {
			require.NoError(t, fileStore.SealCurrent(qrPID))
		}
	}
	dayWindow := (qrHourly(96, 0) / 1000 / 86400) * 86400
	for _, tsMs := range wideSegmentTs(dayWindow) {
		seedBoth(tsMs, "wide parity")
	}

	for _, orderBy := range []string{"timestamp_desc", "timestamp_asc"} {
		for _, limit := range []int{1, 5, 100} {
			for _, offset := range []int{0, 3} {
				q := QueryOptions{PartitionIDs: []int{qrPID}, OrderBy: orderBy, Limit: limit, Offset: offset}
				fr, err := fileStore.QueryEvents(q)
				require.NoError(t, err)
				mr, err := memStore.QueryEvents(q)
				require.NoError(t, err)
				assert.Equal(t, qrKeys(mr), qrKeys(fr),
					"parity must hold with sealed windows (orderBy=%s limit=%d offset=%d)", orderBy, limit, offset)
			}
		}
	}
}

// TestStoreEvent_NormalWriteDoesNotScanHistory 钉住 locks the hot-path
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestStoreEvent_NormalWriteDoesNotScanHistory(t *testing.T) {
	s := newFaultStore(t)

	pidOther := PartitionIDFromName("other")
	oldHour := int64(1_600_000_000)
	for i := 0; i < 3; i++ {
		k := NewSnowflakeEventKey(pidOther, oldHour*1000)
		require.NoError(t, s.StoreEvent(k, commitEvent(k, pidOther, fmt.Sprintf("old-%d", i))))
	}

	pid := PartitionIDFromName("hot")
	base := int64(1_700_000_000)
	kWarm := NewSnowflakeEventKey(pid, base*1000)
	require.NoError(t, s.StoreEvent(kWarm, commitEvent(kWarm, pid, "warm")))

	s.kv.clearSpy()
	k := NewSnowflakeEventKey(pid, base*1000)
	require.NoError(t, s.StoreEvent(k, commitEvent(k, pid, "measured")))

	spy := s.kv.spyLines()
	require.GreaterOrEqual(t, countSpy(spy, "store:sync"), 1,
		"measured write must reach the durability barrier")
	require.GreaterOrEqual(t, countSpy(spy, "store:write"), 2,
		"measured write must KVPut the evt and idx slots")
	require.GreaterOrEqual(t, countSpy(spy, "store:read:"), 1,
		"measured write must probe the idx key (point lookup)")

	require.Equal(t, 0, countSpy(spy, "store:read:scan:"),
		"ordinary write must not scan history; got spy=%v", spy)
}

// TestStoreEvent_WindowSwitchScanIsWindowBounded 钉住 complements the above by
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestStoreEvent_WindowSwitchScanIsWindowBounded(t *testing.T) {
	s := newFaultStore(t)

	pid := PartitionIDFromName("bounded")
	for h := 0; h < 4; h++ {
		for j := 0; j < 2; j++ {
			k := NewSnowflakeEventKey(pid, int64(1_600_000_000+h*3600)*1000)
			require.NoError(t, s.StoreEvent(k, commitEvent(k, pid, fmt.Sprintf("h%d-j%d", h, j))))
		}
	}

	s.kv.clearSpy()
	k := NewSnowflakeEventKey(pid, int64(1_600_000_000+9*3600)*1000)
	require.NoError(t, s.StoreEvent(k, commitEvent(k, pid, "new-window")))

	scanOps := countSpy(s.kv.spyLines(), "store:read:scan:")
	require.LessOrEqual(t, scanOps, 1,
		"a fresh-window write must scan at most its own single window, got %d scan ops", scanOps)
}

// TestSegmentQuery_HeaderParity 钉住 QueryEvents 窗口扫描的轻量 header 解码与原全解码过滤逐位一致。
//   - header 能决定的绝不因少解码而误纳或误拒
//   - 只能靠正文判定的 keyword（摘要未命中）回退整事件解码
//   - 坏值、空值、null 与原路径同进退
//   - 端到端：仅正文命中的召回仍返回，引用字段由 header 构造
func TestSegmentQuery_HeaderParity(t *testing.T) {
	mk := func(e FullEvent) string { b, _ := json.Marshal(e); return string(b) }
	corpus := []struct {
		name string
		js   string
	}{
		{"valid", mk(FullEvent{EventKey: 11, PartitionID: qrPID, EventType: "agent_output", EventSummary: "hello world", Content: "body text", Timestamp: 1000})},
		{"content-keyword", mk(FullEvent{EventKey: 12, PartitionID: qrPID, EventType: "tool_result", EventSummary: "no match here", Content: "needle in body", Timestamp: 2000})},
		{"summary-keyword", mk(FullEvent{EventKey: 13, PartitionID: qrPID, EventType: "agent_output", EventSummary: "needle summary", Content: "", Timestamp: 1500})},
		{"other-type", mk(FullEvent{EventKey: 14, PartitionID: qrPID, EventType: "user_input", EventSummary: "needle again", Content: "x", Timestamp: 3000})},
		{"bad-json", `{not valid json`},
		{"empty", ``},
		{"null", `null`},
	}
	queries := []QueryOptions{
		{},
		{Keyword: "needle"},
		{Keyword: "hello"},
		{EventTypes: []string{"agent_output"}},
		{EventTypes: []string{"tool_result"}, Keyword: "needle"},
		{StartTime: 1500},
		{EndTime: 1500},
		{StartTime: 1500, EndTime: 2500},
		{MinEventKey: 12},
		{Keyword: "zzz-miss"},
		{EventTypes: []string{"nope"}},
		{Keyword: "needle body"},
	}

	for _, v := range corpus {
		for _, q := range queries {
			// OLD oracle: full decode then the authoritative predicate.
			var ev FullEvent
			oldAccept := json.Unmarshal([]byte(v.js), &ev) == nil && matchesQueryFilters(ev, q)

			// NEW: exactly the header split scanPartition now performs.
			var newAccept bool
			if hdr, ok := decodeEventHeader(v.js); ok {
				switch filterEventHeader(hdr, q) {
				case filterReject:
					newAccept = false
				case filterAccept:
					newAccept = true
				case filterNeedBody:
					var full FullEvent
					if json.Unmarshal([]byte(v.js), &full) == nil {
						newAccept = matchesQueryFilters(full, q)
					}
				}
			}
			assert.Equal(t, oldAccept, newAccept, "corpus=%s query=%+v", v.name, q)
		}
	}

	store := newTestSegmentStore(t)
	contentKey := NewSnowflakeEventKey(qrPID, 1785000000000)
	require.NoError(t, store.StoreEvent(contentKey, FullEvent{
		EventKey:     contentKey,
		PartitionID:  qrPID,
		EventType:    "agent_output",
		EventSummary: "unrelated summary",
		Content:      "MAGIC_TOKEN inside body",
		Timestamp:    1785000000000,
	}))
	refs, err := store.QueryEvents(QueryOptions{PartitionIDs: []int{qrPID}, Keyword: "MAGIC_TOKEN"})
	require.NoError(t, err)
	require.Len(t, refs, 1, "content-only keyword must be recalled through the split path")
	assert.Equal(t, contentKey, refs[0].EventKey)
	assert.Equal(t, "unrelated summary", refs[0].EventSummary)
	assert.Equal(t, "agent_output", refs[0].EventType)

	none, err := store.QueryEvents(QueryOptions{PartitionIDs: []int{qrPID}, Keyword: "does-not-exist-anywhere"})
	require.NoError(t, err)
	assert.Empty(t, none)
}

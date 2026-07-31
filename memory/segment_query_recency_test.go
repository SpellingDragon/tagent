package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression suite for the segment-query-recency contract:
//
//	1. declarative semantics — result ≡ filter-all → total-order sort → offset/limit
//	2. total-order determinism — (Timestamp, EventKey)
//	3. recency first — limit sacrifices the oldest, never the newest
//	5. identity uniqueness — dedup keeps the higher-layer version
//
// (contract 4, honest truncation, lives in the recall tool layer)

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
	base := int64(1785000000) // hour-aligned epoch seconds
	return (base - int64(hoursAgo)*3600 + int64(idx)) * 1000
}

func qrKeys(refs []EventReference) []int64 {
	out := make([]int64, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.EventKey)
	}
	return out
}

// TestQueryEvents_RecencyFirstWhenOverLimit (contract 3 / B1): three time
// windows each holding 10 keyword matches, limit=10 desc — every returned
// event must come from the newest window. Before the fix the meta scan ran
// oldest→newest and truncated during scanning, so only the OLDEST window's
// events were returned (the 2026-07-31 meditation recall failure).
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

// TestQueryEvents_RecentSemantics (contract 3 / B1, memory_recent path): no
// keyword, desc, limit below the total count — must return the globally
// newest events. Before the fix "get the most recent N" returned the OLDEST
// N: full semantic inversion.
func TestQueryEvents_RecentSemantics(t *testing.T) {
	store := newTestSegmentStore(t)

	var all []int64
	for _, hoursAgo := range []int{72, 48, 24, 0} {
		for i := 0; i < 8; i++ {
			all = append(all, qrSeed(t, store, qrHourly(hoursAgo, i), "event"))
		}
	}
	// Expected: the 20 newest by (timestamp, key) — keys embed the timestamp,
	// so a descending key sort matches the total order here.
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

// TestQueryEvents_MillisecondSinceFilter (B2): StartTime/EndTime are Unix
// milliseconds. Window pruning compared second-scale window bounds against
// millisecond query bounds, so every segment was pruned and any `since`
// query returned 0 events.
func TestQueryEvents_MillisecondSinceFilter(t *testing.T) {
	store := newTestSegmentStore(t)

	qrSeed(t, store, qrHourly(48, 0), "old")
	recent := qrSeed(t, store, qrHourly(0, 5), "recent")

	// since = start of the newest hour window
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

// TestQueryEvents_PruningMatchesEventFilter (B2): window pruning must never
// drop a segment that holds an event passing the event-level filter.
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

// TestQueryEvents_L2SegmentNotPrunedByHourlySpan (B3): an L2 segment's window
// is day-aligned and spans 24h, but pruning assumed a fixed 1h span — a query
// window in the segment's afternoon pruned the whole day away.
func TestQueryEvents_L2SegmentNotPrunedByHourlySpan(t *testing.T) {
	store := newTestSegmentStore(t)

	dayWindow := (int64(1785000000) / 86400) * 86400 // day-aligned
	afternoonMs := (dayWindow + 15*3600) * 1000      // 15:00 inside that day
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

// TestQueryEvents_DedupKeepsHigherLayerBothDirections (contract 5 / D3): in
// the compaction crash window the same event lives in both the L1 source and
// the L2 target. It must be returned once, and the kept version must be the
// higher layer regardless of traversal direction — otherwise desc and asc
// would return different content for the same event.
func TestQueryEvents_DedupKeepsHigherLayerBothDirections(t *testing.T) {
	store := newTestSegmentStore(t)

	// L1 hourly segment (via the normal write path).
	tsMs := qrHourly(0, 10)
	key := qrSeed(t, store, tsMs, "L1 原文版本")

	// Same event also present in a day-aligned L2 segment, summarized.
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

// TestQueryEvents_OffsetWithEarlyStop (contract 1): offset+limit paging must
// match the declarative reference (filter → sort → offset/limit) even though
// the implementation stops scanning early.
func TestQueryEvents_OffsetWithEarlyStop(t *testing.T) {
	store := newTestSegmentStore(t)

	var all []int64
	for _, hoursAgo := range []int{48, 24, 0} {
		for i := 0; i < 10; i++ {
			all = append(all, qrSeed(t, store, qrHourly(hoursAgo, i), "paged"))
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i] > all[j] }) // desc reference
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

// TestQueryEvents_SameMillisecondStable (contract 2): events sharing one
// millisecond (parallel tool calls) must sort deterministically via the
// EventKey tie-break — repeated queries return the identical sequence.
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

// TestQueryEvents_StoreImplementationParity (contract 1+2 / D5): the two
// store implementations must agree on every QueryOptions combination —
// segmentation and pruning are optimizations, not semantics.
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
		// two events on the exact same millisecond
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

// TestKVScanLexicographicOrder: the window-discovery phase relies on KVScan
// returning keys in lexicographic order (window timestamps are fixed-width
// epoch seconds, so lexicographic == numeric). All KVStore implementations
// must honor that contract — RocksDB does natively, the Go-side backends sort
// explicitly. Guarded here because a violation would silently break recency
// ordering rather than fail loudly.
func TestKVScanLexicographicOrder(t *testing.T) {
	backends := map[string]KVStore{
		"MockRustVikingClient": NewMockRustVikingClient(),
	}
	localKV, err := NewLocalFileKV(t.TempDir())
	require.NoError(t, err)
	backends["LocalFileKV"] = localKV

	// Insert in deliberately shuffled order.
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

// ---------------------------------------------------------------------------
// D9/D10: segment bound truthfulness and actual-timestamp early-stop
// ---------------------------------------------------------------------------

// qrWriteWideSegment writes a compaction-style segment: nominal window is
// day-aligned (named after the earliest source window) while the events span
// `days` days beyond it — exactly the production shape (a "daily" L2 segment
// holding 57h of events). withBounds controls whether the truthful
// MinTime/MaxTime envelope is recorded (legacy segments lack it).
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
		(dayWindowSec + 3600) * 1000,    // day0 +1h  (inside nominal span)
		(dayWindowSec + 30*3600) * 1000, // day1 +6h  (beyond nominal end)
		(dayWindowSec + 54*3600) * 1000, // day2 +6h  (far beyond)
	}
}

// TestQueryEvents_WideSegmentNotPrunedBySince (D9): a compacted segment whose
// nominal span (1 day) understates its real coverage must not be pruned when
// the query's `since` falls beyond the nominal end. Before the truthful-bound
// fix this silently lost the segment's only copy of those events (production:
// 62% of in-range events).
func TestQueryEvents_WideSegmentNotPrunedBySince(t *testing.T) {
	store := newTestSegmentStore(t)
	dayWindow := (int64(1785000000) / 86400) * 86400
	tsList := wideSegmentTs(dayWindow)
	qrWriteWideSegment(t, store, dayWindow, tsList, true, "wide segment")

	// since = day1 +6h — STRICTLY beyond the nominal window end (day1 00:00),
	// so a nominal-bound implementation prunes the whole segment away.
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

// TestQueryEvents_WideSegmentNotSkippedByEarlyStop (D10): desc traversal
// visits windows by nominal ts, so a wide segment named very early is visited
// last — after the budget is already met. It must still be scanned because its
// TRUTHFUL upper bound reaches into (or past) the collected range.
func TestQueryEvents_WideSegmentNotSkippedByEarlyStop(t *testing.T) {
	store := newTestSegmentStore(t)
	dayWindow := (int64(1785000000) / 86400) * 86400
	tsList := wideSegmentTs(dayWindow)
	wideKeys := qrWriteWideSegment(t, store, dayWindow, tsList, true, "wide segment")

	// An hourly L1 window that sorts BEFORE the wide segment in desc order
	// (higher windowTS) but holds events OLDER than the wide segment's newest.
	hourlyTs := (dayWindow + 48*3600) * 1000 // day2 00:00 — older than tsList[2]
	hourlyKey := qrSeed(t, store, hourlyTs, "hourly newer window")

	// limit=1 → budget met by the hourly window, then the wide segment is
	// evaluated for skipping.
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

// TestQueryEvents_LegacyWideSegmentNeverPruned (D9 tier 3): a legacy compacted
// segment without MinTime/MaxTime has no provable upper bound — it must be
// neither pruned nor skipped, so its events stay reachable until the next
// compaction records real bounds.
func TestQueryEvents_LegacyWideSegmentNeverPruned(t *testing.T) {
	store := newTestSegmentStore(t)
	dayWindow := (int64(1785000000) / 86400) * 86400
	tsList := wideSegmentTs(dayWindow)
	qrWriteWideSegment(t, store, dayWindow, tsList, false /* legacy: no bounds */, "legacy wide")

	// Strictly beyond the nominal end, so only the tier-3 "unprovable upper
	// bound → never prune" rule keeps these events reachable.
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

// TestQueryEvents_StrictInequalityAtBoundary (D10): when a candidate window's
// truthful upper bound EQUALS the smallest collected timestamp, the window
// must still be scanned — the same-millisecond EventKey tie-break can rank its
// event ahead of the collected one.
func TestQueryEvents_StrictInequalityAtBoundary(t *testing.T) {
	store := newTestSegmentStore(t)

	// Two events on the exact same millisecond in two different hour windows
	// is impossible; instead put the boundary case in one wide segment whose
	// MaxTime equals an event timestamp in a later-sorting window.
	tsMs := qrHourly(0, 0)
	newer := qrSeed(t, store, tsMs, "collected first")

	dayWindow := (tsMs / 1000 / 86400) * 86400
	tie := qrWriteWideSegment(t, store, dayWindow, []int64{tsMs}, true, "tie in wide segment")

	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		Limit:        1,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	// Both share the timestamp; the larger EventKey wins the total order.
	want := newer
	if tie[0] > newer {
		want = tie[0]
	}
	assert.Equal(t, want, refs[0].EventKey,
		"boundary equality must not skip the candidate window (strict inequality)")
}

// ---------------------------------------------------------------------------
// D8: single semantic time axis
// ---------------------------------------------------------------------------

// TestQueryEvents_DivergentWriteAndEventTime (D8): EventKey embeds the WRITE
// time while FullEvent.Timestamp is the EVENT time; asynchronous write-back
// makes them diverge. Queries must behave exactly as if only Timestamp
// existed — the key's embedded time may decide segment placement, never
// ordering or filtering.
func TestQueryEvents_DivergentWriteAndEventTime(t *testing.T) {
	store := newTestSegmentStore(t)

	writeNow := qrHourly(0, 0) // all three written "now"
	// Event times deliberately out of order relative to the write order, and
	// in windows far from the write window (async write-back of old events).
	eventTimes := []int64{qrHourly(48, 0), qrHourly(5, 0), qrHourly(20, 0)}

	type seeded struct {
		key  int64
		tsMs int64
	}
	var all []seeded
	for _, evTs := range eventTimes {
		// Key embeds the WRITE time; the event carries its own (older) time.
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

	// Reference: pure "sort by Timestamp desc, tie-break EventKey desc".
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

	// Time filtering likewise: a range around the event times must select by
	// Timestamp even though every key embeds a much newer write time.
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

// TestSealWritesTruthfulBounds (D14): sealing is the LSM flush point — the
// segment becomes immutable and its meta records the truthful event-time
// envelope. Sealed segments with bounds participate in pruning; unsealed
// ones are memtables.
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

// TestUnsealedSegmentIsMemtable (D14): an active (unsealed) segment must
// never be pruned or skipped even when the query range is far from its
// nominal window — its key range is still moving.
func TestUnsealedSegmentIsMemtable(t *testing.T) {
	store := newTestSegmentStore(t)

	// Seed one event into an ACTIVE window (StoreEvent marks Sealed:false).
	active := qrSeed(t, store, qrHourly(0, 5), "active memtable event")

	// Query a range that would exclude the window if its nominal bounds were
	// trusted (range sits 48h before it).
	refs, err := store.QueryEvents(QueryOptions{
		PartitionIDs: []int{qrPID},
		StartTime:    qrHourly(72, 0),
		EndTime:      qrHourly(48, 0),
		Limit:        10,
		OrderBy:      "timestamp_desc",
	})
	require.NoError(t, err)
	assert.Empty(t, refs, "range filter still applies at the event level")

	// But a range overlapping the event's Timestamp must find it even though
	// the nominal window bounds would prune it (window is unsealed → scanned).
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

// ---------------------------------------------------------------------------
// D12: seq slot vs identity (restart safety)
// ---------------------------------------------------------------------------

// TestStoreEvent_SeqRecoveredAfterRestart (D12 / I4): the in-memory seqCounter
// cannot tell a fresh window from a revisited one. After a "restart" (a new
// store instance over the same KV), writing into a window that already holds
// seq 0..4 must resume at seq 5 — never overwrite the slots (production: one
// event was silently swallowed this way, leaving a dangling idx).
func TestStoreEvent_SeqRecoveredAfterRestart(t *testing.T) {
	mockKV := NewMockRustVikingClient()
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

	// "Restart": brand-new store (empty PartitionState) over the same KV.
	store2, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	newKey := NewSnowflakeEventKey(qrPID, tsMs+100)
	require.NoError(t, store2.StoreEvent(newKey, FullEvent{
		EventKey: newKey, PartitionID: qrPID, EventType: "agent_output",
		EventSummary: "after restart", Timestamp: tsMs + 100,
	}))

	// The new event must occupy seq 5, and all pre-restart events must still
	// resolve through their EventKeys (no slot was overwritten).
	evt, err := store2.GetEvent(newKey)
	require.NoError(t, err)
	assert.Equal(t, "after restart", evt.EventSummary)

	for i, k := range firstKeys {
		old, err := store2.GetEvent(k)
		require.NoError(t, err, "pre-restart event %d must survive", i)
		assert.Equal(t, "before restart", old.EventSummary)
	}
}

// TestStoreEvent_SeqRecoveredOnWindowRevisit (D12): seq recovery must also
// work when a window is revisited within one process (write A, write B, write
// A again) — simulated by pre-occupying the window via direct KV writes, so
// the test does not depend on snowflake monotonicity (a revisit with an old
// timestamp is exactly the snowflake-collision case guarded by D15).
func TestStoreEvent_SeqRecoveredOnWindowRevisit(t *testing.T) {
	store := newTestSegmentStore(t)

	// Pre-occupy window A with seq 0 and seq 1 via direct KV writes (real
	// snowflake keys so GetEvent can derive the partition back).
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

	// Write into a different window first (forces a window switch), then
	// write into the pre-occupied window — seq must resume at 2.
	other := qrSeed(t, store, qrHourly(5, 0), "other window")
	_ = other
	k := NewSnowflakeEventKey(qrPID, qrHourly(0, 100))
	require.NoError(t, store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "revisit write", Timestamp: qrHourly(0, 100),
	}))

	// The revisit write must land in seq 2; the pre-existing slots survive.
	evt, err := store.GetEvent(k)
	require.NoError(t, err)
	assert.Equal(t, "revisit write", evt.EventSummary)
	for _, pk := range preKeys {
		old, err := store.GetEvent(pk)
		require.NoError(t, err)
		assert.Equal(t, "pre-existing", old.EventSummary)
	}
}

// TestStoreEvent_SnowflakeCollisionRejected (D15): an EventKey is the event's
// identity. A second StoreEvent under an existing key (the only way two
// different events can share one) must be REJECTED, never silently overwrite
// the idx pointer — event immutability is the zero-th contract.
func TestStoreEvent_SnowflakeCollisionRejected(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 0)
	k := NewSnowflakeEventKey(qrPID, tsMs)
	require.NoError(t, store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "original", Timestamp: tsMs,
	}))

	// Same key, different event → must fail.
	err := store.StoreEvent(k, FullEvent{
		EventKey: k, PartitionID: qrPID, EventType: "x",
		EventSummary: "impostor", Timestamp: tsMs + 1,
	})
	require.Error(t, err, "rewriting an existing EventKey must be rejected")

	// The original event is untouched.
	evt, getErr := store.GetEvent(k)
	require.NoError(t, getErr)
	assert.Equal(t, "original", evt.EventSummary)
}

// ---------------------------------------------------------------------------
// Code-review regressions (M1/M2, parity hardening)
// ---------------------------------------------------------------------------

// TestQueryEvents_MinTimeBelowWindowStart (M1): a sealed segment containing an
// asynchronously written-back event whose Timestamp is OLDER than the window
// start must not be pruned by an `until` that falls between MinTime and the
// window start. Trusting max(windowStart, MinTime) would silently drop it.
func TestQueryEvents_MinTimeBelowWindowStart(t *testing.T) {
	store := newTestSegmentStore(t)

	dayWindow := (int64(1785000000) / 86400) * 86400
	oldTs := (dayWindow - 2*3600) * 1000 // 2h BEFORE the window start
	midTs := (dayWindow + 3600) * 1000
	qrWriteWideSegment(t, store, dayWindow, []int64{oldTs, midTs}, true, "async write-back")

	// until = window start - 1h: only oldTs (window start - 2h) qualifies.
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

// TestStoreEvent_RejectedCollisionLeavesNoGhost (M2): a rejected collision
// must leave NO trace — not in precise recall (GetEvent), and not in segment
// scans (QueryEvents) either, since scans don't go through the idx.
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

// TestStoreEvent_WriteIntoSealedWindowDemotesToMemtable (m5): writing into a
// sealed window makes its recorded bounds stale; the window must be demoted
// back to Sealed=false (memtable: always scanned, never pruned).
func TestStoreEvent_WriteIntoSealedWindowDemotesToMemtable(t *testing.T) {
	store := newTestSegmentStore(t)

	tsMs := qrHourly(0, 0)
	qrSeed(t, store, tsMs, "before seal")
	require.NoError(t, store.SealCurrent(qrPID))

	w := (tsMs / 1000 / DefaultWindowSize) * DefaultWindowSize
	meta, err := store.GetSegmentMeta(qrPID, w)
	require.NoError(t, err)
	require.True(t, meta.Sealed)

	// Switch away, then write into the sealed window again.
	qrSeed(t, store, qrHourly(5, 0), "other window")
	qrSeed(t, store, tsMs+60, "after seal")

	meta2, err := store.GetSegmentMeta(qrPID, w)
	require.NoError(t, err)
	assert.False(t, meta2.Sealed,
		"a window written after sealing must be demoted to memtable semantics")
}

// TestQueryEvents_ParityWithSealedAndWideSegments (m2): the parity matrix
// previously covered only unsealed windows (provable path never compared).
// Mix sealed hourly windows (via SealCurrent) and a wide compacted segment,
// and require bit-identical results vs the reference implementation.
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

	// Three hourly windows; seal the two older ones.
	for _, hoursAgo := range []int{48, 24, 0} {
		for i := 0; i < 4; i++ {
			seedBoth(qrHourly(hoursAgo, i), "parity 彭伟业")
		}
		if hoursAgo > 0 {
			require.NoError(t, fileStore.SealCurrent(qrPID))
		}
	}
	// A compaction-shaped wide segment (sealed, truthful bounds) holding
	// events that also exist in the hourly windows above — exercises both the
	// provable path and cross-layer dedup on the file side; the in-memory
	// store holds the same logical event set.
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

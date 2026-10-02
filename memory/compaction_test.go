package memory

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCompactor creates a Compactor with a mock KV store for testing.
func newTestCompactor(t *testing.T) (*FileSegmentStore, *Compactor) {
	t.Helper()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)
	compactor := NewCompactor(store, mockKV, store.rel, nil, DefaultCompactionConfig())
	return store, compactor
}

func TestCompactor_StartStop(t *testing.T) {
	_, compactor := newTestCompactor(t)

	compactor.Start()
	compactor.Stop()

	compactor.Start()
	compactor.Stop()
}

func TestCompactor_SealCurrent(t *testing.T) {
	store, _ := newTestCompactor(t)

	ts := int64(1710678000000)
	for i := 0; i < 3; i++ {
		key := NewSnowflakeEventKey(1, ts+int64(i)*1000)
		err := store.StoreEvent(key, FullEvent{
			PartitionID:  1,
			EventType:    "test",
			EventSummary: "event " + itoa(i),
			Timestamp:    ts + int64(i)*1000,
		})
		require.NoError(t, err)
	}

	err := store.SealCurrent(1)
	require.NoError(t, err)

	windowTS := WindowTimestamp(TimestampFromEventKey(NewSnowflakeEventKey(1, ts)), DefaultWindowSize)
	meta, err := store.GetSegmentMeta(1, windowTS)
	require.NoError(t, err)
	assert.True(t, meta.Sealed)
	assert.Equal(t, 1, meta.Layer)
}

func TestCompactor_L1ToL2(t *testing.T) {
	store, compactor := newTestCompactor(t)

	baseTS := int64(1710666000000)
	for hour := 0; hour < 3; hour++ {
		hourTS := baseTS + int64(hour)*3600000
		for i := 0; i < 2; i++ {
			key := NewSnowflakeEventKey(1, hourTS+int64(i)*1000)
			err := store.StoreEvent(key, FullEvent{
				PartitionID:  1,
				EventType:    "test",
				EventSummary: "hourly event",
				Timestamp:    hourTS + int64(i)*1000,
			})
			require.NoError(t, err)
		}
		err := store.SealCurrent(1)
		require.NoError(t, err)
	}

	windows, err := store.ListSegments(1)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(windows), 1)

	err = compactor.CompactL1ToL2(1, windows)
	require.NoError(t, err)

	l2WindowTS := computeDailyWindow(windows[0])
	l2Meta, err := store.GetSegmentMeta(1, l2WindowTS)
	require.NoError(t, err)
	assert.Equal(t, 2, l2Meta.Layer)
	assert.Equal(t, 6, l2Meta.EventCount)
}

// TestCompactor_L1ToL2_SecondFoldMergesExistingDailyWindow 钉住跨折叠目标窗口不丢历史。
// - 同一天两批 hourly 窗口先后各折一次：第二批压实必须并入 daily 目标窗既存事件（同键 seq 重排自覆盖会静默丢历史）。
// - 两批事件压实后均可召回，meta.EventCount 等于合并后总数。
func TestCompactor_L1ToL2_SecondFoldMergesExistingDailyWindow(t *testing.T) {
	store, compactor := newTestCompactor(t)

	baseTS := int64(1710666000000)
	foldHours := func(fold int) []int64 {
		before, err := store.ListSegments(1)
		require.NoError(t, err)
		for i := 0; i < 2; i++ {
			hourTS := baseTS + int64(fold*2+i)*3600000
			key := NewSnowflakeEventKey(1, hourTS)
			require.NoError(t, store.StoreEvent(key, FullEvent{
				PartitionID:  1,
				EventType:    "test",
				EventSummary: "marker-fold-" + string(rune('0'+fold)) + "-hour-" + string(rune('0'+i)),
				Timestamp:    hourTS,
			}))
			require.NoError(t, store.SealCurrent(1))
		}
		after, err := store.ListSegments(1)
		require.NoError(t, err)
		var fresh []int64
		for _, w := range after {
			seen := false
			for _, b := range before {
				if b == w {
					seen = true
					break
				}
			}
			if !seen {
				fresh = append(fresh, w)
			}
		}
		return fresh
	}

	fold0 := foldHours(0)
	require.NoError(t, compactor.CompactL1ToL2(1, fold0))
	require.NoError(t, compactor.CompactL1ToL2(1, foldHours(1)))

	for _, fold := range []int{0, 1} {
		marker := "marker-fold-" + string(rune('0'+fold))
		refs, err := store.QueryEvents(QueryOptions{PartitionIDs: []int{1}, Keyword: marker, Limit: 10})
		require.NoError(t, err)
		require.NotEmpty(t, refs, "fold %d events must survive the second fold into the same daily window", fold)
	}

	daily := computeDailyWindow(fold0[0])
	meta, err := store.GetSegmentMeta(1, daily)
	require.NoError(t, err)
	require.Equal(t, 4, meta.EventCount, "the merged daily segment must hold both folds' events")
}

// TestCompactor_SecondFoldTombstoneDoesNotResurrect 钉住目标窗墓碑收缩不复活已遗忘事件。
// - 同日二次折叠并入既存目标窗后，墓碑事件被剔除收缩 seq，旧尾段副本必须随发布同批删除。
// - 否则 finalizeTombstones 移除墓碑守卫后，后续折叠扫入孤儿尾段，已遗忘事件带全文复活（ErrEventForgotten 契约破）。
func TestCompactor_SecondFoldTombstoneDoesNotResurrect(t *testing.T) {
	kv := newMockKV()
	rel := newSimpleInMemRelationStore()
	store, err := NewFileSegmentStore(kv, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, kv, 1)
	store.tombstones = ts
	compactor := NewCompactor(store, kv, rel, ts, CompactionConfig{})

	baseTS := int64(1710666000000)
	freshHours := func(fold, n int) []int64 {
		before, err := store.ListSegments(1)
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			hourTS := baseTS + int64(fold*10+i)*3600000
			key := NewSnowflakeEventKey(1, hourTS)
			require.NoError(t, store.StoreEvent(key, FullEvent{
				PartitionID:  1,
				EventType:    "test",
				EventSummary: "resurrect-fold-" + string(rune('0'+fold)) + "-" + string(rune('0'+i)),
				Timestamp:    hourTS,
			}))
			require.NoError(t, store.SealCurrent(1))
		}
		after, err := store.ListSegments(1)
		require.NoError(t, err)
		var fresh []int64
		for _, w := range after {
			known := false
			for _, b := range before {
				if b == w {
					known = true
					break
				}
			}
			if !known {
				fresh = append(fresh, w)
			}
		}
		return fresh
	}

	require.NoError(t, compactor.CompactL1ToL2(1, freshHours(0, 4)))

	refs, err := store.QueryEvents(QueryOptions{PartitionIDs: []int{1}, Keyword: "resurrect-fold-0", Limit: 10})
	require.NoError(t, err)
	require.Len(t, refs, 4, "fold-0 published all four events")
	var deadA, deadB int64
	for _, r := range refs {
		if r.EventSummary == "resurrect-fold-0-2" {
			deadA = r.EventKey
		}
		if r.EventSummary == "resurrect-fold-0-3" {
			deadB = r.EventKey
		}
	}
	require.NotZero(t, deadA, "the soon-to-be-forgotten event A must exist")
	require.NotZero(t, deadB, "the soon-to-be-forgotten event B must exist")
	require.NoError(t, ts.MarkTombstone(deadA))
	require.NoError(t, ts.MarkTombstone(deadB))

	require.NoError(t, compactor.CompactL1ToL2(1, freshHours(1, 1)))

	require.NoError(t, compactor.CompactL1ToL2(1, freshHours(2, 1)))

	for _, forgotten := range []string{"resurrect-fold-0-2", "resurrect-fold-0-3"} {
		refs, err = store.QueryEvents(QueryOptions{PartitionIDs: []int{1}, Keyword: forgotten, Limit: 10})
		require.NoError(t, err)
		require.Empty(t, refs, "a tombstoned-away event (%s) must not resurrect via the target-window orphan tail", forgotten)
	}
}

func TestCompactor_L2ToL3(t *testing.T) {
	store, compactor := newTestCompactor(t)

	baseTS := int64(1710604800000)
	key := NewSnowflakeEventKey(1, baseTS)
	err := store.StoreEvent(key, FullEvent{
		PartitionID:  1,
		EventType:    event.TypeThinkingPlan,
		EventSummary: "thinking event",
		Content:      "long thinking content that should be summarized in L3",
		Timestamp:    baseTS,
	})
	require.NoError(t, err)

	key2 := NewSnowflakeEventKey(1, baseTS+1000)
	err = store.StoreEvent(key2, FullEvent{
		PartitionID:  1,
		EventType:    event.TypeExternalInput,
		EventSummary: "external input",
		Content:      "preserved content",
		Timestamp:    baseTS + 1000,
	})
	require.NoError(t, err)

	err = store.SealCurrent(1)
	require.NoError(t, err)

	windows, err := store.ListSegments(1)
	require.NoError(t, err)

	err = compactor.CompactL1ToL2(1, windows)
	require.NoError(t, err)

	l2Windows, err := store.ListSegments(1)
	require.NoError(t, err)
	// Filter to only L2 windows
	var l2Only []int64
	for _, w := range l2Windows {
		meta, err := store.GetSegmentMeta(1, w)
		if err == nil && meta.Layer == 2 {
			l2Only = append(l2Only, w)
		}
	}

	if len(l2Only) > 0 {
		err = compactor.CompactL2ToL3(1, l2Only)
		require.NoError(t, err)

		l3WindowTS := computeWeeklyWindow(l2Only[0])
		l3Meta, err := store.GetSegmentMeta(1, l3WindowTS)
		if err == nil {
			assert.Equal(t, 3, l3Meta.Layer)
		}
	}
}

func TestCompactor_DanglingRefRepair(t *testing.T) {
	store, compactor := newTestCompactor(t)

	baseTS := int64(1710678000000)
	key1 := NewSnowflakeEventKey(1, baseTS)
	key2 := NewSnowflakeEventKey(1, baseTS+1000)
	key3 := NewSnowflakeEventKey(1, baseTS+2000)

	store.StoreEvent(key1, FullEvent{PartitionID: 1, EventType: "grandparent", Timestamp: baseTS})
	store.StoreEvent(key2, FullEvent{PartitionID: 1, EventType: "parent", Timestamp: baseTS + 1000})
	store.StoreEvent(key3, FullEvent{PartitionID: 1, EventType: "child", Timestamp: baseTS + 2000})

	store.RelationStore().SetParent(key2, key1)
	store.RelationStore().SetParent(key3, key2)

	store.SealCurrent(1)

	windows, _ := store.ListSegments(1)

	err := compactor.CompactL1ToL2(1, windows)
	require.NoError(t, err)

	parent, _ := store.RelationStore().GetParent(key3)
	assert.Equal(t, key2, parent)
}

func TestCompactor_ComputeWindows(t *testing.T) {
	hourly := int64(1710676800)
	daily := computeDailyWindow(hourly)
	expectedDaily := (hourly / 86400) * 86400
	assert.Equal(t, expectedDaily, daily)

	weekly := computeWeeklyWindow(daily)
	expectedWeekly := (daily / 604800) * 604800
	assert.Equal(t, expectedWeekly, weekly)

	t.Logf("hourly=%d daily=%d weekly=%d", hourly, daily, weekly)
}

func TestCompactor_ConfigDefaults(t *testing.T) {
	cfg := DefaultCompactionConfig()
	assert.Equal(t, 24, cfg.L1Threshold)
	assert.Equal(t, 7, cfg.L2Threshold)
	assert.Equal(t, 5*time.Minute, cfg.CheckInterval)
}

func TestCompactor_NewCompactorCustomConfig(t *testing.T) {
	cfg := CompactionConfig{
		L1Threshold:   12,
		L2Threshold:   3,
		CheckInterval: time.Minute,
	}
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	compactor := NewCompactor(store, mockKV, store.rel, nil, cfg)
	assert.Equal(t, 12, compactor.config.L1Threshold)
	assert.Equal(t, 3, compactor.config.L2Threshold)
	assert.Equal(t, time.Minute, compactor.config.CheckInterval)
}

func TestCompactor_ZeroConfig(t *testing.T) {
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, nil, ":memory:", 100)
	require.NoError(t, err)

	compactor := NewCompactor(store, mockKV, store.rel, nil, CompactionConfig{})
	assert.Equal(t, 24, compactor.config.L1Threshold)
	assert.Equal(t, 7, compactor.config.L2Threshold)
	assert.Equal(t, 5*time.Minute, compactor.config.CheckInterval)
}

// TestCompaction_FinalizesTombstones 钉住 after compaction physically removes a
//
// 契约: docs/wiki/memory/memory-architecture.md#compaction-integrity
func TestCompaction_FinalizesTombstones(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()

	rel := newSimpleInMemRelationStore()
	ts := NewTombstoneSet(rel, kv, 7)
	c := NewCompactor(nil, kv, rel, ts, DefaultCompactionConfig())

	window := WindowTimestamp(1704067200, DefaultWindowSize)
	keyAlive := NewSnowflakeEventKey(7, 1704067200*1000)
	keyDead := NewSnowflakeEventKey(7, 1704067201*1000)
	for seq, k := range []int64{keyAlive, keyDead} {
		evt := FullEvent{EventKey: k, PartitionID: 7, EventType: "external_input", Timestamp: 1704067200000}
		raw, _ := json.Marshal(evt)
		_ = kv.KVPut(EventKeyStr(7, window, seq), string(raw))
		_ = kv.KVPut(IndexKeyStr(7, k), "x")
	}
	meta := SegmentMeta{PartitionID: 7, WindowTS: window, Layer: 1, EventCount: 2, Sealed: true}
	mraw, _ := json.Marshal(meta)
	_ = kv.KVPut(MetaKeyStr(7, window), string(mraw))

	if err := ts.MarkTombstone(keyDead); err != nil {
		t.Fatal(err)
	}
	if err := c.CompactL1ToL2(7, []int64{window}); err != nil {
		t.Fatalf("CompactL1ToL2: %v", err)
	}

	if ts.IsTombstone(keyDead) {
		t.Errorf("tombstone must be finalized after compaction removed the event")
	}
	if _, err := kv.KVGet(TombstoneKeyStr(7, keyDead)); err == nil {
		t.Errorf("tombstone KV key must be deleted")
	}
	if _, err := kv.KVGet(IndexKeyStr(7, keyDead)); err == nil {
		t.Errorf("dangling idx key must be deleted")
	}
	if _, err := kv.KVGet(IndexKeyStr(7, keyAlive)); err != nil {
		t.Errorf("alive event's idx must survive: %v", err)
	}
}

// TestCompaction_DayAlignedSourceSurvives 钉住 when the earliest L1 source
//
// 契约: docs/wiki/memory/memory-architecture.md#compaction-integrity
func TestCompaction_DayAlignedSourceSurvives(t *testing.T) {
	store, compactor := newTestCompactor(t)

	dayAligned := (int64(1710666000) / 86400) * 86400
	var keys []int64
	for i := 0; i < 2; i++ {
		k := NewSnowflakeEventKey(1, (dayAligned+int64(i))*1000)
		require.NoError(t, store.StoreEvent(k, FullEvent{
			PartitionID: 1, EventType: "test", EventSummary: "day event",
			Timestamp: (dayAligned + int64(i)) * 1000,
		}))
		keys = append(keys, k)
	}
	require.NoError(t, store.SealCurrent(1))

	require.NoError(t, store.StoreEvent(NewSnowflakeEventKey(1, (dayAligned+86400+3600)*1000), FullEvent{
		PartitionID: 1, EventType: "test", EventSummary: "next day",
		Timestamp: (dayAligned + 86400 + 3600) * 1000,
	}))
	require.NoError(t, store.SealCurrent(1))

	windows, err := store.ListSegments(1)
	require.NoError(t, err)
	require.Equal(t, dayAligned, computeDailyWindow(windows[0]),
		"fixture must place the target ON a source window")
	require.NoError(t, compactor.CompactL1ToL2(1, windows))

	for _, k := range keys {
		_, err := store.GetEvent(k)
		assert.NoError(t, err, "compacted event must survive the name collision")
	}
}

// TestCompaction_SkipsUnsealedSegments 钉住 an active (unsealed) segment is
//
// 契约: docs/wiki/memory/memory-architecture.md#compaction-integrity
func TestCompaction_SkipsUnsealedSegments(t *testing.T) {
	store, compactor := newTestCompactor(t)

	require.NoError(t, store.StoreEvent(NewSnowflakeEventKey(1, 1710666000000), FullEvent{
		PartitionID: 1, EventType: "test", EventSummary: "active",
		Timestamp: 1710666000000,
	}))

	windows, err := store.ListSegments(1)
	require.NoError(t, err)
	require.NoError(t, compactor.CompactL1ToL2(1, windows))

	evt, err := store.GetEvent(NewSnowflakeEventKey(1, 1710666000000))
	_ = evt
	_ = err
	windows2, _ := store.ListSegments(1)
	assert.NotEmpty(t, windows2, "unsealed segment must not be compacted away")
}

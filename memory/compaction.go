package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/event"
)

// SegmentLayer 表示一个段所处的层。
type SegmentLayer int

const (
	// LayerL0 是热层：当前时间窗，事件仍在写入。
	LayerL0 SegmentLayer = iota
	// LayerL1 L1 是第一层冷化目标。
	LayerL1
	// LayerL2 L2 是第二冷化层。
	LayerL2
	// LayerL3 L3 是最冷层。
	LayerL3
)

// String 返回该层的可读名称。
func (l SegmentLayer) String() string {
	switch l {
	case LayerL0:
		return "L0"
	case LayerL1:
		return "L1"
	case LayerL2:
		return "L2"
	case LayerL3:
		return "L3"
	default:
		return "??"
	}
}

// LowValueEventTypes are event types whose Content/ToolCalls can be discarded in L3.
// Derived from the event registry (single source of truth): thinking_plan, context_compress.
var LowValueEventTypes = event.LowValueTypes()

// CompactionConfig configures the compactor behavior.
type CompactionConfig struct {
	L1Threshold   int
	L2Threshold   int
	CheckInterval time.Duration
}

// DefaultCompactionConfig returns the default compaction configuration.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		L1Threshold:   24,
		L2Threshold:   7,
		CheckInterval: 5 * time.Minute,
	}
}

// Compactor manages background compaction operations.
type Compactor struct {
	store     *FileSegmentStore
	kv        KVStore
	rel       RelationStore
	tombstone *TombstoneSet
	config    CompactionConfig

	// l1Threshold Live thresholds, seeded from config and retunable via SetThresholds
	// (atomic so a soak/E2E harness can retune while the scheduler runs —
	// the "至少一次 compaction" harness requirement).
	l1Threshold atomic.Int64
	l2Threshold atomic.Int64

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewCompactor creates a new Compactor.
func NewCompactor(store *FileSegmentStore, kv KVStore, rel RelationStore, tombstone *TombstoneSet, config CompactionConfig) *Compactor {
	if config.L1Threshold <= 0 {
		config.L1Threshold = 24
	}
	if config.L2Threshold <= 0 {
		config.L2Threshold = 7
	}
	if config.CheckInterval <= 0 {
		config.CheckInterval = 5 * time.Minute
	}
	c := &Compactor{
		store:     store,
		kv:        kv,
		rel:       rel,
		tombstone: tombstone,
		config:    config,
		stopCh:    make(chan struct{}),
	}
	c.l1Threshold.Store(int64(config.L1Threshold))
	c.l2Threshold.Store(int64(config.L2Threshold))
	return c
}

// SetThresholds retunes the compaction thresholds at runtime (harness hook —
// a soak subprocess must exercise the real
// compaction path without producing 24 sealed hourly segments). Values <= 0
// are ignored. Atomic against the background scheduler by construction.
func (c *Compactor) SetThresholds(l1, l2 int) {
	if l1 > 0 {
		c.l1Threshold.Store(int64(l1))
	}
	if l2 > 0 {
		c.l2Threshold.Store(int64(l2))
	}
}

// CompactOnce runs one synchronous compaction sweep (hourly seal + L1→L2 +
// L2→L3) with the CURRENT thresholds. The scheduler self-ticks every
// CheckInterval (default 5min) — harnesses that must not wait call this
// instead; it is the same sweep body, so no second semantic exists.
func (c *Compactor) CompactOnce() {
	c.checkAndCompact()
}

// Start starts the compaction scheduler in a background goroutine.
func (c *Compactor) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.wg.Add(1)
	go c.schedulerLoop()
}

// Stop stops the compaction scheduler gracefully.
func (c *Compactor) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return
	}
	c.running = false
	close(c.stopCh)
	c.wg.Wait()
}

// schedulerLoop runs periodically to check compaction conditions.
func (c *Compactor) schedulerLoop() {
	defer c.wg.Done()

	c.checkAndCompact()

	ticker := time.NewTicker(c.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.checkAndCompact()
		case <-c.stopCh:
			return
		}
	}
}

// checkAndCompact checks all partitions and triggers compactions as needed.
func (c *Compactor) checkAndCompact() {
	c.checkHourlySeal()
	c.checkL1ToL2Compaction()
	c.checkL2ToL3Compaction()
}

// checkHourlySeal checks all active partitions and seals segments
// that have crossed an hour boundary.
func (c *Compactor) checkHourlySeal() {
	now := time.Now().Unix()
	currentHour := WindowTimestamp(now, DefaultWindowSize)

	c.store.partitions.Range(func(key, value interface{}) bool {
		pid := key.(int)
		state := value.(*PartitionState)
		state.mu.Lock()
		lastWindow := state.currentWindow
		state.mu.Unlock()

		if lastWindow != 0 && lastWindow < currentHour {
			_ = c.store.SealCurrent(pid)
		}
		return true
	})
}

// checkL1ToL2Compaction checks all partitions for L1 segments that exceed
// the L1Threshold and triggers L1→L2 compaction.
func (c *Compactor) checkL1ToL2Compaction() {
	c.store.partitions.Range(func(key, value interface{}) bool {
		pid := key.(int)
		windows, _ := c.store.ListSegments(pid)

		var l1Windows []int64
		for _, w := range windows {
			meta, err := c.getSegmentMeta(pid, w)
			if err != nil || meta == nil {
				continue
			}
			if meta.Layer == 1 && meta.Sealed {
				l1Windows = append(l1Windows, w)
			}
		}

		if len(l1Windows) >= int(c.l1Threshold.Load()) {
			if err := c.CompactL1ToL2(pid, l1Windows); err != nil {
				log.Errorf("[Compactor] L1→L2 failed pid=%d: %v", pid, err)
			}
		}
		return true
	})
}

// checkL2ToL3Compaction checks all partitions for L2 segments that exceed
// the L2Threshold and triggers L2→L3 compaction.
func (c *Compactor) checkL2ToL3Compaction() {
	c.store.partitions.Range(func(key, value interface{}) bool {
		pid := key.(int)
		windows, _ := c.store.ListSegments(pid)

		var l2Windows []int64
		for _, w := range windows {
			meta, err := c.getSegmentMeta(pid, w)
			if err != nil || meta == nil {
				continue
			}
			if meta.Layer == 2 && meta.Sealed {
				l2Windows = append(l2Windows, w)
			}
		}

		if len(l2Windows) >= int(c.l2Threshold.Load()) {
			if err := c.CompactL2ToL3(pid, l2Windows); err != nil {
				log.Errorf("[Compactor] L2→L3 failed pid=%d: %v", pid, err)
			}
		}
		return true
	})
}

// getSegmentMeta reads segment metadata from the KV store.
func (c *Compactor) getSegmentMeta(pid int, windowTS int64) (*SegmentMeta, error) {
	metaKVKey := MetaKeyStr(pid, windowTS)
	val, err := c.kv.KVGet(metaKVKey)
	if err != nil {
		return nil, err
	}
	var meta SegmentMeta
	if err := json.Unmarshal([]byte(val), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// lockPartition CompactL1ToL2 compacts L1 hourly segments into a single L2 daily segment for a partition.
// lockPartition takes the owning store's per-partition mutation lock for the
// lockPartition 把压实的持久发布串行化在写入方提交与同分区删除封存之间。
// seals on the same partition . It returns the unlock closure. When the
// compactor has no owning store (standalone over a bare KV — e.g. a unit test)
// there is no shared mutation to coordinate against, so it is a no-op.
func (c *Compactor) lockPartition(pid int) func() {
	if c.store == nil {
		return func() {}
	}
	st := c.store.getPartitionState(pid)
	st.mutationMu.Lock()
	return func() { st.mutationMu.Unlock() }
}

// CompactL1ToL2 把给定窗口集合从 L1 压实到 L2；窗口由调用方按老化策略选出。
func (c *Compactor) CompactL1ToL2(pid int, windowTSs []int64) error {
	if len(windowTSs) == 0 {
		return nil
	}
	dead, err := func() ([]int64, error) {
		defer c.lockPartition(pid)()

		events, err := c.mergeEvents(pid, windowTSs)
		if err != nil {
			return nil, fmt.Errorf("merge failed: %w", err)
		}
		if len(events) == 0 {
			return nil, nil
		}

		events, dead := c.filterTombstoned(events)

		events, err = c.repairDanglingRefs(events)
		if err != nil {
			return nil, fmt.Errorf("repair failed: %w", err)
		}

		l2WindowTS := computeDailyWindow(windowTSs[0])
		meta := SegmentMeta{
			PartitionID: pid,
			WindowTS:    l2WindowTS,
			Layer:       2,
			EventCount:  len(events),
			MinTime:     events[0].Timestamp,
			MaxTime:     events[len(events)-1].Timestamp,
			Sealed:      true,
		}

		batchOps := make([]KVOp, 0, len(events)*2)
		for seq, evt := range events {
			evtKVKey := EventKeyStr(pid, l2WindowTS, seq)
			evtJSON, _ := json.Marshal(evt)
			batchOps = append(batchOps, KVOp{Type: "put", Key: evtKVKey, Value: string(evtJSON)})

			idxKVKey := IndexKeyStr(pid, evt.EventKey)
			idxValue := fmt.Sprintf("%d:%d", l2WindowTS, seq)
			batchOps = append(batchOps, KVOp{Type: "put", Key: idxKVKey, Value: idxValue})
		}
		metaJSON, _ := json.Marshal(meta)
		metaKVKey := MetaKeyStr(pid, l2WindowTS)
		batchOps = append(batchOps, KVOp{Type: "put", Key: metaKVKey, Value: string(metaJSON)})

		if err := c.kv.KVBatch(batchOps); err != nil {
			return nil, fmt.Errorf("failed to write L2 segment: %w", err)
		}

		// 6. Cleanup: delete source L1 segments (only after L2 is fully written =
		// crash-safe). Collision guard : a day-aligned earliest
		// source window equals l2WindowTS — deleting it would erase the freshly
		// compacted segment, so it must be excluded.
		var cleanupWindows []int64
		for _, w := range windowTSs {
			if w != l2WindowTS {
				cleanupWindows = append(cleanupWindows, w)
			}
		}
		if err := c.deleteSegments(pid, cleanupWindows); err != nil {
			return nil, fmt.Errorf("cleanup failed: %w", err)
		}
		return dead, nil
	}()
	if err != nil {
		return err
	}
	c.finalizeTombstones(pid, dead)
	return nil
}

// mergeEvents reads all events from source segments and returns them sorted by timestamp.
func (c *Compactor) mergeEvents(pid int, windowTSs []int64) ([]FullEvent, error) {
	var events []FullEvent

	for _, windowTS := range windowTSs {
		eventPrefix := SegmentEventPrefix(pid, windowTS)
		pairs, err := c.kv.KVScan(eventPrefix, 0)
		if err != nil {
			return nil, fmt.Errorf("merge scan failed pid=%d window=%d: %w", pid, windowTS, err)
		}
		for _, pair := range pairs {
			var evt FullEvent
			if err := json.Unmarshal([]byte(pair.Value), &evt); err != nil {
				continue
			}
			events = append(events, evt)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp < events[j].Timestamp
	})

	return events, nil
}

// filterTombstoned removes tombstoned events from the list, returning the
// surviving events and the keys of the dead ones. The dead keys let the
// compaction finalize the tombstones: once the rewritten segment (without
// the dead events) is durably in place and the source segments are deleted,
// the tombstone has nothing left to guard — keeping it would leak an entry
// in the in-memory set, a {pid}:tomb:{key} KV key, and a dangling
// {pid}:idx:{key} forever.
func (c *Compactor) filterTombstoned(events []FullEvent) ([]FullEvent, []int64) {
	if c.tombstone == nil {
		return events, nil
	}
	var result []FullEvent
	var dead []int64
	for _, evt := range events {
		if !c.tombstone.IsTombstone(evt.EventKey) {
			result = append(result, evt)
		} else {
			dead = append(dead, evt.EventKey)
		}
	}
	return result, dead
}

// finalizeTombstones removes fully-compacted-away events' remaining traces:
// their dangling index keys and their tombstone entries (memory + KV).
// Crash between segment cleanup and this step is benign — stale tombstones
// are harmless and this finalization is idempotent.
// When the idx removal batch fails, the tombstone stays: it is both the retry
// marker and the resurrection guard (ErrEventForgotten). Dropping it over a
// failed removal loses the evidence AND the guard; keeping it is safe because
// finalize is idempotent and the next compaction round retries the same keys.
func (c *Compactor) finalizeTombstones(pid int, dead []int64) {
	if len(dead) == 0 {
		return
	}
	if c.store != nil {
		kept := make([]int64, 0, len(dead))
		for _, key := range dead {
			if c.store.IsKeyProtected(key) {
				continue
			}
			kept = append(kept, key)
		}
		dead = kept
		if len(dead) == 0 {
			return
		}
	}
	batchOps := make([]KVOp, 0, len(dead))
	for _, key := range dead {
		batchOps = append(batchOps, KVOp{Type: "delete", Key: IndexKeyStr(pid, key)})
		if c.store != nil {
			c.store.removeVector(key)
		}
	}
	if err := c.kv.KVBatch(batchOps); err != nil {
		log.Errorf("[Compaction] delete dangling idx failed pid=%d: %v", pid, err)
		return
	}
	if c.tombstone != nil {
		if err := c.tombstone.RemoveTombstones(dead); err != nil {
			log.Errorf("[Compaction] remove tombstones failed pid=%d: %v", pid, err)
		}
	}
}

// repairDanglingRefs fixes parent references that point to tombstoned events.
// Walks the causal chain to find the nearest alive ancestor.
func (c *Compactor) repairDanglingRefs(events []FullEvent) ([]FullEvent, error) {
	alive := make(map[int64]bool, len(events))
	for _, evt := range events {
		alive[evt.EventKey] = true
	}

	repaired := make([]FullEvent, len(events))
	copy(repaired, events)

	for i, evt := range events {
		if evt.EventKey == 0 {
			continue
		}
		parentKey, err := c.rel.GetParent(evt.EventKey)
		if err != nil || parentKey == 0 {
			continue
		}

		if alive[parentKey] {
			continue
		}

		ancestor := c.findAliveAncestor(parentKey, alive)
		if ancestor != parentKey {
			repaired[i] = evt
			if err := c.rel.SetParent(evt.EventKey, ancestor); err != nil {
				log.Errorf("[Compactor] repair SetParent failed key=%d ancestor=%d: %v", evt.EventKey, ancestor, err)
			}
		}
	}

	return repaired, nil
}

// findAliveAncestor walks the parent chain from the given key
// until it finds an alive event (or reaches root).
func (c *Compactor) findAliveAncestor(key int64, alive map[int64]bool) int64 {
	visited := make(map[int64]bool)
	current := key
	for current != 0 && !visited[current] {
		visited[current] = true
		if alive[current] {
			return current
		}
		parent, err := c.rel.GetParent(current)
		if err != nil {
			return 0
		}
		current = parent
	}
	return 0
}

// deleteSegments deletes all KV keys for the given segments (crash-safe cleanup).
// A scan failure never silently skips a window: scan errors are collected and
// surfaced, so the delete act stays honest about what it actually removed.
func (c *Compactor) deleteSegments(pid int, windowTSs []int64) error {
	var batchOps []KVOp
	var scanErrs []error

	for _, windowTS := range windowTSs {
		eventPrefix := SegmentEventPrefix(pid, windowTS)
		pairs, err := c.kv.KVScan(eventPrefix, 0)
		if err != nil {
			scanErrs = append(scanErrs, fmt.Errorf("delete-segments scan pid=%d window=%d: %w", pid, windowTS, err))
			continue
		}
		for _, pair := range pairs {
			batchOps = append(batchOps, KVOp{Type: "delete", Key: pair.Key})
		}
		metaKVKey := MetaKeyStr(pid, windowTS)
		batchOps = append(batchOps, KVOp{Type: "delete", Key: metaKVKey})
	}

	if len(batchOps) > 0 {
		if err := c.kv.KVBatch(batchOps); err != nil {
			return errors.Join(append(scanErrs, err)...)
		}
	}

	return errors.Join(scanErrs...)
}

// CompactL2ToL3 compacts L2 daily segments into a single L3 weekly segment.
// In addition to L1→L2 steps, it summarizes low-value events.
func (c *Compactor) CompactL2ToL3(pid int, windowTSs []int64) error {
	if len(windowTSs) == 0 {
		return nil
	}
	dead, err := func() ([]int64, error) {
		defer c.lockPartition(pid)()

		events, err := c.mergeEvents(pid, windowTSs)
		if err != nil {
			return nil, fmt.Errorf("merge failed: %w", err)
		}
		if len(events) == 0 {
			return nil, nil
		}

		events, dead := c.filterTombstoned(events)
		events, err = c.repairDanglingRefs(events)
		if err != nil {
			return nil, fmt.Errorf("repair failed: %w", err)
		}

		for i, evt := range events {
			if LowValueEventTypes[evt.EventType] {
				events[i].Content = ""
				events[i].ToolCalls = nil
			}
		}

		l3WindowTS := computeWeeklyWindow(windowTSs[0])
		meta := SegmentMeta{
			PartitionID: pid,
			WindowTS:    l3WindowTS,
			Layer:       3,
			EventCount:  len(events),
			MinTime:     events[0].Timestamp,
			MaxTime:     events[len(events)-1].Timestamp,
			Sealed:      true,
		}

		batchOps := make([]KVOp, 0, len(events)*2)
		for seq, evt := range events {
			evtKVKey := EventKeyStr(pid, l3WindowTS, seq)
			evtJSON, _ := json.Marshal(evt)
			batchOps = append(batchOps, KVOp{Type: "put", Key: evtKVKey, Value: string(evtJSON)})

			idxKVKey := IndexKeyStr(pid, evt.EventKey)
			idxValue := fmt.Sprintf("%d:%d", l3WindowTS, seq)
			batchOps = append(batchOps, KVOp{Type: "put", Key: idxKVKey, Value: idxValue})
		}

		metaJSON, _ := json.Marshal(meta)
		metaKVKey := MetaKeyStr(pid, l3WindowTS)
		batchOps = append(batchOps, KVOp{Type: "put", Key: metaKVKey, Value: string(metaJSON)})

		if err := c.kv.KVBatch(batchOps); err != nil {
			return nil, fmt.Errorf("failed to write L3 segment: %w", err)
		}

		// Cleanup (with the same collision guard as CompactL1ToL2: a week-aligned
		// earliest source window equals l3WindowTS and must not be deleted).
		var l3CleanupWindows []int64
		for _, w := range windowTSs {
			if w != l3WindowTS {
				l3CleanupWindows = append(l3CleanupWindows, w)
			}
		}
		if err := c.deleteSegments(pid, l3CleanupWindows); err != nil {
			return nil, fmt.Errorf("cleanup failed: %w", err)
		}
		return dead, nil
	}()
	if err != nil {
		return err
	}
	c.finalizeTombstones(pid, dead)
	return nil
}

// computeDailyWindow computes the daily window timestamp from an hourly window.
func computeDailyWindow(hourlyWindowTS int64) int64 {
	return (hourlyWindowTS / 86400) * 86400
}

// computeWeeklyWindow computes the weekly window timestamp from a daily window.
func computeWeeklyWindow(dailyWindowTS int64) int64 {
	return (dailyWindowTS / 604800) * 604800
}

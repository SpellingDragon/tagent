package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== Compactor ====================
//
// Compactor manages the data lifecycle through L0→L1→L2→L3 transitions.
// In the KV store model:
//   - L0 (hot): Current time window - events being written
//   - L1 (warm): Sealed hourly segments (past 24 hours)
//   - L2 (cold): Daily segments (1-7 days) - gzip compressed
//   - L3 (archive): Weekly segments (7+ days) - gzip + summarization
//
// Compaction flow: Merge → Filter → Repair → Compress → Cleanup

// SegmentLayer represents the layer of a segment.
type SegmentLayer int

const (
	LayerL0 SegmentLayer = iota // Active (current window)
	LayerL1                     // Sealed hourly
	LayerL2                     // Compressed daily
	LayerL3                     // Archived weekly
)

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
	L1Threshold   int           // L1 segments before L1→L2 compaction (default: 24)
	L2Threshold   int           // L2 segments before L2→L3 compaction (default: 7)
	CheckInterval time.Duration // How often to check for compaction (default: 5min)
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
	tombstone *TombstoneSet // For filtering tombstoned events during compaction
	config    CompactionConfig

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
	return &Compactor{
		store:     store,
		kv:        kv,
		rel:       rel,
		tombstone: tombstone,
		config:    config,
		stopCh:    make(chan struct{}),
	}
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

	// Run an initial check immediately
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
			// Active segment crossed hour boundary, seal it
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
			// Only sealed segments are compaction sources (code-review P2):
			// an active (unsealed) segment is still taking writes — merging it
			// and deleting the source would make subsequent writes invisible
			// until the next seal, and its EventCount=0 meta would corrupt
			// accounting.
			if meta.Layer == 1 && meta.Sealed {
				l1Windows = append(l1Windows, w)
			}
		}

		if len(l1Windows) >= c.config.L1Threshold {
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

		if len(l2Windows) >= c.config.L2Threshold {
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

// ==================== L1→L2 Compaction ====================

// CompactL1ToL2 compacts L1 hourly segments into a single L2 daily segment for a partition.
func (c *Compactor) CompactL1ToL2(pid int, windowTSs []int64) error {
	if len(windowTSs) == 0 {
		return nil
	}

	// 1. Merge: read all events from source segments in timestamp order
	events, err := c.mergeEvents(pid, windowTSs)
	if err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	// 2. Filter: remove tombstoned events
	events, dead := c.filterTombstoned(events)

	// 3. Repair: fix dangling parent references
	events, err = c.repairDanglingRefs(events)
	if err != nil {
		return fmt.Errorf("repair failed: %w", err)
	}

	// 4. Build target L2 meta
	// MinTime/MaxTime are the segment's TRUTHFUL time envelope: the nominal
	// window (named after the earliest source window, day-aligned) understates
	// the real coverage whenever the sources span more than a day, and query
	// pruning/early-stop depend on a truthful upper bound. mergeEvents already
	// sorted ascending by Timestamp, so first/last suffice — no extra scan.
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

	// 5. Write L2 events to KV store
	batchOps := make([]KVOp, 0, len(events)*2)
	for seq, evt := range events {
		evtKVKey := EventKeyStr(pid, l2WindowTS, seq)
		evtJSON, _ := json.Marshal(evt)
		batchOps = append(batchOps, KVOp{Type: "put", Key: evtKVKey, Value: string(evtJSON)})

		idxKVKey := IndexKeyStr(pid, evt.EventKey)
		idxValue := fmt.Sprintf("%d:%d", l2WindowTS, seq)
		batchOps = append(batchOps, KVOp{Type: "put", Key: idxKVKey, Value: idxValue})
	}

	// Write L2 meta
	metaJSON, _ := json.Marshal(meta)
	metaKVKey := MetaKeyStr(pid, l2WindowTS)
	batchOps = append(batchOps, KVOp{Type: "put", Key: metaKVKey, Value: string(metaJSON)})

	if err := c.kv.KVBatch(batchOps); err != nil {
		return fmt.Errorf("failed to write L2 segment: %w", err)
	}

	// 6. Cleanup: delete source L1 segments (only after L2 is fully written = crash-safe).
	// Collision guard (code-review P1): when the earliest source window is
	// day-aligned, l2WindowTS EQUALS that source window — the target prefix
	// we just wrote IS a source prefix. Deleting it would erase the freshly
	// compacted segment (data loss), so it must be excluded.
	var cleanupWindows []int64
	for _, w := range windowTSs {
		if w != l2WindowTS {
			cleanupWindows = append(cleanupWindows, w)
		}
	}
	if err := c.deleteSegments(pid, cleanupWindows); err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// 7. Finalize tombstones: the dead events are physically gone now.
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
			continue
		}
		for _, pair := range pairs {
			var evt FullEvent
			if err := json.Unmarshal([]byte(pair.Value), &evt); err != nil {
				continue
			}
			events = append(events, evt)
		}
	}

	// Sort by timestamp
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
func (c *Compactor) finalizeTombstones(pid int, dead []int64) {
	if len(dead) == 0 {
		return
	}
	batchOps := make([]KVOp, 0, len(dead))
	for _, key := range dead {
		batchOps = append(batchOps, KVOp{Type: "delete", Key: IndexKeyStr(pid, key)})
	}
	if err := c.kv.KVBatch(batchOps); err != nil {
		log.Errorf("[Compaction] delete dangling idx failed pid=%d: %v", pid, err)
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
	// Build set of alive event keys
	alive := make(map[int64]bool, len(events))
	for _, evt := range events {
		alive[evt.EventKey] = true
	}

	// Check each event's parent - if parent is not in the alive set,
	// walk the chain via RelationStore to find the nearest alive ancestor.
	repaired := make([]FullEvent, len(events))
	copy(repaired, events)

	for i, evt := range events {
		if evt.EventKey == 0 {
			continue
		}
		// Get parent from RelationStore
		parentKey, err := c.rel.GetParent(evt.EventKey)
		if err != nil || parentKey == 0 {
			continue // No parent or root event
		}

		if alive[parentKey] {
			continue // Parent is alive, no repair needed
		}

		// Parent is dead (tombstoned), walk the chain to find alive ancestor
		ancestor := c.findAliveAncestor(parentKey, alive)
		if ancestor != parentKey {
			repaired[i] = evt
			// Update RelationStore
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
func (c *Compactor) deleteSegments(pid int, windowTSs []int64) error {
	var batchOps []KVOp

	// Collect per-segment event counts BEFORE deletion (their meta keys are
	// among the deleted) so the process-lifetime counter can be decremented.
	var removedTotal int64
	for _, windowTS := range windowTSs {
		if meta, err := c.getSegmentMeta(pid, windowTS); err == nil && meta != nil && meta.EventCount > 0 {
			removedTotal += int64(meta.EventCount)
		}
	}

	for _, windowTS := range windowTSs {
		// Delete event keys
		eventPrefix := SegmentEventPrefix(pid, windowTS)
		pairs, err := c.kv.KVScan(eventPrefix, 0)
		if err != nil {
			continue
		}
		for _, pair := range pairs {
			batchOps = append(batchOps, KVOp{Type: "delete", Key: pair.Key})
		}
		// Delete meta keys
		metaKVKey := MetaKeyStr(pid, windowTS)
		batchOps = append(batchOps, KVOp{Type: "delete", Key: metaKVKey})
	}

	if len(batchOps) > 0 {
		if err := c.kv.KVBatch(batchOps); err != nil {
			return err
		}
	}

	// Keep the process-lifetime count accurate (D11): the removed events are
	// physically gone. The store reference may be nil in bare compactor uses.
	if removedTotal > 0 && c.store != nil {
		c.store.decrementEventCount(pid, removedTotal)
	}
	return nil
}

// ==================== L2→L3 Deep Compaction ====================

// CompactL2ToL3 compacts L2 daily segments into a single L3 weekly segment.
// In addition to L1→L2 steps, it summarizes low-value events.
func (c *Compactor) CompactL2ToL3(pid int, windowTSs []int64) error {
	if len(windowTSs) == 0 {
		return nil
	}

	// Same flow as L1→L2
	events, err := c.mergeEvents(pid, windowTSs)
	if err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	events, dead := c.filterTombstoned(events)
	events, err = c.repairDanglingRefs(events)
	if err != nil {
		return fmt.Errorf("repair failed: %w", err)
	}

	// Summarize low-value events for L3
	for i, evt := range events {
		if LowValueEventTypes[evt.EventType] {
			events[i].Content = ""
			events[i].ToolCalls = nil
		}
	}

	l3WindowTS := computeWeeklyWindow(windowTSs[0])
	// Truthful time envelope (see CompactL1ToL2): events stay sorted ascending
	// through filter/repair/summarize, so first/last are the real bounds.
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
		return fmt.Errorf("failed to write L3 segment: %w", err)
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
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// Finalize tombstones: dead events are physically gone from L3 too.
	c.finalizeTombstones(pid, dead)

	return nil
}

// ==================== Utility ====================

// computeDailyWindow computes the daily window timestamp from an hourly window.
func computeDailyWindow(hourlyWindowTS int64) int64 {
	// Daily window = floor(timestamp / 86400) * 86400
	return (hourlyWindowTS / 86400) * 86400
}

// computeWeeklyWindow computes the weekly window timestamp from a daily window.
func computeWeeklyWindow(dailyWindowTS int64) int64 {
	// Weekly window = floor(timestamp / 604800) * 604800
	return (dailyWindowTS / 604800) * 604800
}

package memory

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== Simple LRU Cache ====================

// lruEntry holds a key-value pair in the LRU cache.
type lruEntry struct {
	key   int64
	value *FullEvent
}

// simpleLRU is a simple LRU cache for FullEvent objects.
type simpleLRU struct {
	mu      sync.Mutex
	items   map[int64]*list.Element
	order   *list.List
	maxSize int
}

func newSimpleLRU(maxSize int) *simpleLRU {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &simpleLRU{
		items:   make(map[int64]*list.Element),
		order:   list.New(),
		maxSize: maxSize,
	}
}

func (c *simpleLRU) Get(key int64) (*FullEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return elem.Value.(*lruEntry).value, true
	}
	return nil, false
}

func (c *simpleLRU) Add(key int64, value *FullEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*lruEntry).value = value
		return
	}
	elem := c.order.PushFront(&lruEntry{key: key, value: value})
	c.items[key] = elem
	if c.order.Len() > c.maxSize {
		c.removeOldest()
	}
}

func (c *simpleLRU) Remove(key int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.Remove(elem)
		delete(c.items, key)
	}
}

func (c *simpleLRU) removeOldest() {
	elem := c.order.Back()
	if elem != nil {
		entry := elem.Value.(*lruEntry)
		delete(c.items, entry.key)
		c.order.Remove(elem)
	}
}

func (c *simpleLRU) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// ==================== FileSegmentStore ====================
//
// FileSegmentStore implements MemoryStore using RustViking KV as the backing store.
// Events are organized by time-windowed segments following the KV key schema:
//
//	{pid}:evt:{window_ts}:{seq}  → JSON FullEvent content
//	{pid}:idx:{event_key}        → {window_ts}:{seq}  (offset index)
//	{pid}:meta:{window_ts}       → JSON segment metadata
//
// L0 (hot): The current time window's events - written directly to KV.
// L1 (warm): Sealed segments from previous time windows.
//
// The "segment" is a logical grouping by window_ts, not a physical file.
// Segment metadata tracks event count, min/max timestamp, and layer.

// PartitionState holds per-partition state for FileSegmentStore.
type PartitionState struct {
	// mutationMu serializes the ENTIRE mutation of this partition end to end —
	// seq/window alloc, identity/collision probe, evt+idx+meta writes, the
	// durability barrier, and cache/count publication — so a concurrent writer,
	// the async compactor, a delete, or a seal can never interleave into a torn
	// idx/evt or a double-counted live set (async-task-lifetime 2.2). It is the
	// OUTER lock; mu below stays the inner counter/window lock (M-order:
	// mutationMu → mu). Cross-partition work stays parallel (no global lock is
	// ever held across I/O), and store reverse-callbacks (vecRemover/Notify) run
	// outside it.
	mutationMu    sync.Mutex
	mu            sync.Mutex
	currentWindow int64 // Current active window timestamp
	seqCounter    int   // Sequence counter within current window
	eventCount    int64 // Total events in this partition
	countKnown    bool  // 2.5: eventCount is authoritative (post-scan); false pauses capacity eviction for THIS partition only
}

// SegmentMeta holds metadata for a segment.
type SegmentMeta struct {
	PartitionID int   `json:"pid"`
	WindowTS    int64 `json:"window_ts"`
	Layer       int   `json:"layer"` // 1=L1 (sealed), 2=L2, 3=L3
	EventCount  int   `json:"event_count"`
	MinTime     int64 `json:"min_time"`
	MaxTime     int64 `json:"max_time"`
	Sealed      bool  `json:"sealed"`
}

// EventCache is an LRU cache for frequently accessed FullEvents.
const defaultCacheSize = 1000

// FileSegmentStore implements MemoryStore using RustViking KV + segment model.
type FileSegmentStore struct {
	kv         KVStore       // RustViking KV client (or mock)
	rel        RelationStore // Causal relationship graph
	tombstones *TombstoneSet // Optional tombstone set for dead event filtering
	cache      *simpleLRU    // EventCache LRU
	dataDir    string
	partitions sync.Map // map[int]*PartitionState

	// Lifecycle components (optional, set via Set* methods)
	lifecycle *LifecycleManager
	compactor *Compactor
	closeOnce sync.Once

	// retention is the optional §2.8 unacked-recovery retention lease, owned by the
	// shared resource (set via SetRetentionLease after recovery rebuild). While a key
	// is protected, TTL/capacity/tombstone-final-cleanup must not destroy its original
	// and DeleteEvent returns ErrEventProtected. nil = no protection (inert).
	retention *RetentionLease

	// vecRemover 是可选向量移除回调（VectorRemover，atomic 存）：遗忘物理删除事件时
	// （Compactor.finalizeTombstones）回调，使记忆引擎同步移除向量（内存索引 + KV
	// 持久键），防死键堆积与重启复活（审查 M2）。compactor 先于本回调设置启动，故用
	// atomic 防竞态。由 wireMemoryEngine 经 SetVectorRemover 接线。
	vecRemover atomic.Value // VectorRemover

	// countsKnown (resident-readiness-plan 2.8): true only after a successful
	// RebuildLiveCounts — the eventCount fields then reflect the FULL logical
	// live set (deduped, tombstone-excluded), not just this process's writes.
	// Unknown counts pause capacity eviction (never treated as 0).
	countsKnown atomic.Bool
}

// NewFileSegmentStore creates a FileSegmentStore.
func NewFileSegmentStore(kv KVStore, rel RelationStore, dataDir string, cacheSize int) (*FileSegmentStore, error) {
	if cacheSize <= 0 {
		cacheSize = defaultCacheSize
	}
	if rel == nil {
		rel = newSimpleInMemRelationStore()
	}
	s := &FileSegmentStore{
		kv:      kv,
		rel:     rel,
		cache:   newSimpleLRU(cacheSize),
		dataDir: dataDir,
	}
	// Cold-partition discovery at construction (implementation-hardening 2.4):
	// forgetting scans Range over s.partitions — without startup discovery, a
	// partition this process never wrote to stayed invisible to TTL/capacity/
	// compaction forever. Non-fatal: a backend without enumeration capability
	// logs a known limitation and keeps the old lazy-discovery behavior.
	if err := s.Init(); err != nil {
		log.Warnf("[FileSegmentStore] cold-partition discovery failed (non-fatal, forgetting falls back to warm partitions only): %v", err)
	}
	return s, nil
}

// Init discovers persisted partitions and registers them in s.partitions so
// the forgetting scans (TTL / capacity / compaction, which Range over the
// map) cover the FULL store — not just partitions this process wrote to
// (cold-partition blind spot, implementation-hardening 2.4). Partition
// enumeration uses the optional ListPartitionIDs capability via type
// assertion; backends without it keep lazy discovery (known limitation,
// logged). seqCounter recovery stays in StoreEvent's window path (D12).
// Called from NewFileSegmentStore; safe to call again.
func (s *FileSegmentStore) Init() error {
	lister, ok := s.kv.(interface{ ListPartitionIDs() []int })
	if !ok {
		log.Infof("[FileSegmentStore] kv backend has no ListPartitionIDs capability — cold partitions stay undiscovered (known limitation)")
		return nil
	}
	discovered := 0
	for _, pid := range lister.ListPartitionIDs() {
		s.getPartitionState(pid) // registers; D12 seq recovery runs on first write
		discovered++
	}
	if discovered > 0 {
		log.Infof("[FileSegmentStore] discovered %d persisted partition(s) at startup — forgetting scans cover them", discovered)
	}
	return nil
}

// LivesCountKnown reports whether the per-partition live counts reflect the
// full logical set (2.8). Capacity eviction must pause when false.
func (s *FileSegmentStore) LivesCountKnown() bool { return s.countsKnown.Load() }

// RebuildLiveCounts rebuilds each discovered partition's logical live count
// from the single fact chain (resident-readiness-plan 2.8): dedup by
// EventKey (compaction crash windows keep both layers alive briefly) and
// tombstone exclusion. Must run AFTER tombstone recovery and BEFORE the
// lifecycle/compaction scanners start. Any scan failure leaves the counts
// unknown (capacity eviction pauses) instead of pretending an empty store.
func (s *FileSegmentStore) RebuildLiveCounts() error {
	lister, ok := s.kv.(interface{ ListPartitionIDs() []int })
	if !ok {
		return fmt.Errorf("kv backend has no ListPartitionIDs capability")
	}
	for _, pid := range lister.ListPartitionIDs() {
		live, maxKey, err := s.scanLiveKeys(pid)
		if err != nil {
			s.markPartitionUnknown(pid)
			return err
		}
		// Same full scan also raises the snowflake floor for this partition:
		// the new process generation may never re-issue a key already on the
		// chain (§8.5 restart-collision guard; see RaiseSnowflakeFloor).
		RaiseSnowflakeFloor(pid, maxKey)
		state := s.getPartitionState(pid)
		state.mu.Lock()
		state.eventCount = int64(live)
		state.countKnown = true
		state.mu.Unlock()
	}
	s.countsKnown.Store(true)
	return nil
}

// scanLiveKeys returns the deduped, tombstone-excluded count of COMPLETE live
// records in one partition, read straight from the fact chain. It is the ONLY
// authoritative count oracle (2.5): the cache, replay classification and engine
// callbacks are all explicitly disqualified as count sources (spec event-segment-store).
func (s *FileSegmentStore) scanLiveKeys(pid int) (liveCount int, maxKey int64, err error) {
	live := make(map[int64]struct{})
	windows, err := s.ListSegments(pid)
	if err != nil {
		return 0, 0, fmt.Errorf("list segments pid=%d: %w", pid, err)
	}
	for _, windowTS := range windows {
		pairs, err := s.kv.KVScan(SegmentEventPrefix(pid, windowTS), 0)
		if err != nil {
			return 0, 0, fmt.Errorf("scan window pid=%d window=%d: %w", pid, windowTS, err)
		}
		for _, pair := range pairs {
			var evt struct {
				EventKey int64 `json:"event_key"`
			}
			if json.Unmarshal([]byte(pair.Value), &evt) != nil || evt.EventKey == 0 {
				continue
			}
			if s.tombstones != nil && s.tombstones.IsTombstone(evt.EventKey) {
				continue // logically dead — not part of the live count
			}
			live[evt.EventKey] = struct{}{} // dedup across crash-window layers
			if evt.EventKey > maxKey {
				maxKey = evt.EventKey
			}
		}
	}
	return len(live), maxKey, nil
}

// recomputePartition makes pid's live count authoritative from a full-record scan
// and marks it known (2.5). Callers completing a repair or clearing an uncertain
// commit invoke it while holding pid's mutationMu. A scan failure leaves the
// partition unknown — never a guessed value.
func (s *FileSegmentStore) recomputePartition(pid int) error {
	live, maxKey, err := s.scanLiveKeys(pid)
	if err == nil {
		RaiseSnowflakeFloor(pid, maxKey) // same repair-time seed (§8.5)
	}
	if err != nil {
		s.markPartitionUnknown(pid)
		return err
	}
	state := s.getPartitionState(pid)
	state.mu.Lock()
	state.eventCount = int64(live)
	state.countKnown = true
	state.mu.Unlock()
	return nil
}

// markPartitionUnknown flags a partition whose live count can no longer be trusted
// (a durability/barrier failure that may have partially persisted, or a failed scan)
// so capacity eviction pauses for THIS partition only — other partitions keep
// evicting, and this one is never evicted on a guessed number (2.5).
func (s *FileSegmentStore) markPartitionUnknown(pid int) {
	state := s.getPartitionState(pid)
	state.mu.Lock()
	state.countKnown = false
	state.mu.Unlock()
}

// PartitionCountKnown reports whether pid's live count is authoritative; capacity
// eviction skips a partition when it is false (2.5).
func (s *FileSegmentStore) PartitionCountKnown(pid int) bool {
	state := s.getPartitionState(pid)
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.countKnown
}

// getPartitionState returns (or creates) the PartitionState for a given partition ID.
func (s *FileSegmentStore) getPartitionState(pid int) *PartitionState {
	actual, _ := s.partitions.LoadOrStore(pid, &PartitionState{})
	return actual.(*PartitionState)
}

// ==================== Write Operations ====================

// StoreEvent stores a single event via RustViking KV.
//
// Commit semantics (resident-readiness-plan 2.3): the event is committed
// serially — collision check → evt → idx → (meta) → DURABILITY BARRIER →
// cache/count publication. Success means the barrier completed: the event
// survives an unclean process termination even below the WAL flush
// threshold. Any failure leaves the cache and the live count untouched and
// the caller (stored-gate) must not project the event.
func (s *FileSegmentStore) StoreEvent(key int64, event FullEvent) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}

	pid := event.PartitionID
	if pid == 0 {
		pid = PartitionIDFromEventKey(key)
	}
	event.EventKey = key
	event.PartitionID = pid

	// Derive window timestamp from event key
	tsSec := TimestampFromEventKey(key)
	windowTS := WindowTimestamp(tsSec, DefaultWindowSize)

	state := s.getPartitionState(pid)
	// 2.2: hold the partition mutation lock across the whole commit (see the
	// PartitionState.mutationMu doc). All writes/probes/barrier/publish below run
	// serialized against every other mutation of this partition.
	state.mutationMu.Lock()
	defer state.mutationMu.Unlock()
	state.mu.Lock()

	// Check if we've moved to a new time window
	if state.currentWindow != windowTS {
		state.currentWindow = windowTS
		state.seqCounter = 0
	}
	if state.seqCounter == 0 {
		// Recover the highest seq already used in this window (D12): an in-
		// memory zero seqCounter cannot distinguish "fresh window" from
		// "process restarted / window revisited", and reusing seq 0 would
		// overwrite the existing slot — silently swallowing the event that
		// lived there (production: one dangling idx already proved this).
		// The scan is scoped to this single window and happens at most once
		// per window switch. Recovery FAILS LOUD (M3): falling back to 0
		// would re-introduce the overwrite it exists to prevent.
		recovered, recErr := s.recoverWindowSeqLocked(pid, windowTS)
		if recErr != nil {
			state.mu.Unlock()
			return fmt.Errorf("seq recovery failed for window %d: %w", windowTS, recErr)
		}
		state.seqCounter = recovered

		// Revisit of a SEALED window (m5): its recorded MinTime/MaxTime no
		// longer covers what we are about to add, so demote it back to
		// memtable semantics (Sealed=false → always scanned, never pruned).
		if rawMeta, metaErr := s.kv.KVGet(MetaKeyStr(pid, windowTS)); metaErr == nil {
			var oldMeta SegmentMeta
			if jsonErr := json.Unmarshal([]byte(rawMeta), &oldMeta); jsonErr == nil && oldMeta.Sealed {
				oldMeta.Sealed = false
				if patched, mErr := json.Marshal(oldMeta); mErr == nil {
					if pErr := s.kv.KVPut(MetaKeyStr(pid, windowTS), string(patched)); pErr != nil {
						log.Warnf("[SegmentStore] failed to demote sealed window pid=%d window=%d: %v", pid, windowTS, pErr)
					}
				}
			}
		}
	}
	seq := state.seqCounter
	state.seqCounter++
	state.mu.Unlock()

	// Serialize event
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event %d: %w", key, err)
	}

	// Collision guard (D15, before any write per code-review M2): an EventKey
	// IS the event's identity — a second write under the same key can only
	// be a snowflake collision (the in-memory seq counter restarts across
	// processes; two writers in the same second collide). Silently
	// overwriting the idx pointer would break event immutability, so reject
	// with the typed duplicate error. A storage I/O error here fails the
	// commit LOUD — it must never be misread as "the key is free" (2.5).
	// Must precede the evt write: segment scans read events directly, so a
	// rejected write must not leave a ghost.
	idxKVKey := IndexKeyStr(pid, key)
	if _, err := s.kv.KVGet(idxKVKey); err == nil {
		// Public-path duplicate policy (resident- reliability-protocol 2.1): an
		// EventKey IS the event's identity, so a public StoreEvent retry landing on
		// an existing slot is REFUSED — the public path NEVER silently completes an
		// orphan. Orphan / half-orphan completion belongs exclusively to the
		// internal ReplayEvent path (durable-inbox claim, mem_spill canonical
		// recovery). This makes FileSegmentStore's public contract identical to
		// InMemoryStore.StoreEvent, which already refuses any existing key (2.7).
		return fmt.Errorf("event key %d already exists (public StoreEvent refuses duplicates; orphan completion is ReplayEvent-only): %w", key, ErrDuplicateEventKey)
	} else if !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("collision probe for event %d failed: %w", key, err)
	}

	// KV key for event content
	evtKVKey := EventKeyStr(pid, windowTS, seq)
	if err := s.kv.KVPut(evtKVKey, string(eventJSON)); err != nil {
		return fmt.Errorf("failed to store event key %d: %w", key, err)
	}

	// KV key for index (EventKey → segment position)
	idxValue := fmt.Sprintf("%d:%d", windowTS, seq)
	return s.finishCommit(pid, key, idxKVKey, idxValue, windowTS, seq, event, state)
}

// ReplayEvent implements EventReplayer (D4 design): canonical content-checked
// replay that classifies the outcome (new / repaired / already-committed) rather
// than rejecting all duplicates like the public StoreEvent. This allows the
// reliable inbox and mem_spill paths to safely retry without double-incrementing
// live-count or treating a completed repair as a failure.
func (s *FileSegmentStore) ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error) {
	if key == 0 {
		return ReplayNew, canonicalFact, fmt.Errorf("event key cannot be zero")
	}
	keyPid := PartitionIDFromEventKey(key)
	// 2.4: the EventKey encodes its partition authoritatively. A fact declaring a
	// DIFFERENT non-zero partition is an identity conflict (错分区/错身份) — refused, never
	// silently re-stamped onto the key's partition. PartitionID 0 means "derive from key".
	if declared := canonicalFact.PartitionID; declared != 0 && declared != keyPid {
		return ReplayNew, canonicalFact, fmt.Errorf(
			"event key %d maps to partition %d but the fact declares partition %d (identity conflict): %w",
			key, keyPid, declared, ErrDuplicateEventKey)
	}
	pid := keyPid
	canonicalFact.EventKey = key
	canonicalFact.PartitionID = pid

	// 2.4: a legal tombstone means the fact was deliberately deleted — a replay MUST
	// NOT resurrect it. Refuse before any seq allocation or write; the caller holds the
	// recovery material (treats it like a conflict: not acked).
	if s.tombstones != nil && s.tombstones.IsTombstone(key) {
		return ReplayNew, canonicalFact, fmt.Errorf("replay of tombstoned event %d refused: %w", key, ErrEventForgotten)
	}

	tsSec := TimestampFromEventKey(key)
	windowTS := WindowTimestamp(tsSec, DefaultWindowSize)

	state := s.getPartitionState(pid)
	// 2.2: serialize the replay commit under the partition mutation lock, exactly
	// like StoreEvent (identity probe → evt write → finish/completeOrphan publish).
	state.mutationMu.Lock()
	defer state.mutationMu.Unlock()
	state.mu.Lock()
	if state.currentWindow != windowTS {
		state.currentWindow = windowTS
		state.seqCounter = 0
	}
	if state.seqCounter == 0 {
		recovered, recErr := s.recoverWindowSeqLocked(pid, windowTS)
		if recErr != nil {
			state.mu.Unlock()
			return ReplayNew, canonicalFact, fmt.Errorf("seq recovery failed for window %d: %w", windowTS, recErr)
		}
		state.seqCounter = recovered
	}
	seq := state.seqCounter
	state.seqCounter++
	state.mu.Unlock()

	eventJSON, err := json.Marshal(canonicalFact)
	if err != nil {
		return ReplayNew, canonicalFact, fmt.Errorf("failed to marshal event %d: %w", key, err)
	}

	idxKVKey := IndexKeyStr(pid, key)
	if _, probeErr := s.kv.KVGet(idxKVKey); probeErr == nil {
		// Identity slot exists: classify via the orphan/already-committed path.
		result, commitErr := s.completeOrphanCommit(pid, key, idxKVKey, canonicalFact, eventJSON)
		if commitErr != nil {
			return ReplayNew, canonicalFact, commitErr
		}
		return result, canonicalFact, nil
	} else if !errors.Is(probeErr, ErrKeyNotFound) {
		return ReplayNew, canonicalFact, fmt.Errorf("collision probe for event %d failed: %w", key, probeErr)
	}

	// Identity index absent. Before committing a FRESH copy, check for an orphaned evt
	// slot already holding this key — the half-write a crash leaves between the evt
	// write and the idx write (2.3 class "evt 有/idx 缺"). If found, REUSE the original
	// slot: publish only the idx pointer at it, never a second copy of the fact.
	if w, sq, ok, lerr := s.locateOrphanEvtSlot(pid, windowTS, key); lerr != nil {
		return ReplayNew, canonicalFact, lerr
	} else if ok {
		origKVKey := EventKeyStr(pid, w, sq)
		raw, rerr := s.kv.KVGet(origKVKey)
		if rerr != nil {
			return ReplayNew, canonicalFact, fmt.Errorf("orphan-evt reuse read for key %d: %w", key, rerr)
		}
		if raw != string(eventJSON) {
			// Same identity, different content → refused collision (D15), never reuse.
			return ReplayNew, canonicalFact, fmt.Errorf("event key %d exists (no index) with DIFFERENT content: %w", key, ErrDuplicateEventKey)
		}
		if _, merr := s.ensureWindowMeta(pid, w); merr != nil {
			return ReplayNew, canonicalFact, fmt.Errorf("orphan-evt reuse meta for key %d: %w", key, merr)
		}
		// The fact is already durable in that slot — publish idx→original, barrier, cache,
		// and a single live-count (the reused slot was never counted, its idx was missing).
		reuseIdxValue := fmt.Sprintf("%d:%d", w, sq)
		if err := s.kv.KVPut(idxKVKey, reuseIdxValue); err != nil {
			return ReplayNew, canonicalFact, fmt.Errorf("reuse index for event %d: %w", key, err)
		}
		if syncer, sok := s.kv.(interface{ Sync() error }); sok {
			if serr := syncer.Sync(); serr != nil {
				s.markPartitionUnknown(pid) // durability uncertain after a failed barrier (2.5)
				return ReplayNew, canonicalFact, fmt.Errorf("durability barrier for reused event %d: %w", key, serr)
			}
		}
		reused := cloneFullEvent(canonicalFact)
		s.cache.Add(key, &reused)
		// 2.4/2.5: the reused slot is a durable record that was never counted (its idx
		// was missing); make the live count authoritative from the fact chain rather than
		// a hand ++ that could diverge from the truth.
		if rerr := s.recomputePartition(pid); rerr != nil {
			return ReplayNew, canonicalFact, fmt.Errorf("recompute partition %d after reuse of %d: %w", pid, key, rerr)
		}
		return ReplayRepaired, canonicalFact, nil
	}

	// Genuinely new key: normal fresh-commit path.
	evtKVKey := EventKeyStr(pid, windowTS, seq)
	if err := s.kv.KVPut(evtKVKey, string(eventJSON)); err != nil {
		return ReplayNew, canonicalFact, fmt.Errorf("failed to store event key %d: %w", key, err)
	}
	idxValue := fmt.Sprintf("%d:%d", windowTS, seq)
	if err := s.finishCommit(pid, key, idxKVKey, idxValue, windowTS, seq, canonicalFact, state); err != nil {
		return ReplayNew, canonicalFact, err
	}
	return ReplayNew, canonicalFact, nil
}

// completeOrphanCommit finishes an UNCOMMITTED write found under the same
// EventKey (2.6). Two sub-cases are handled:
//
//  1. evt slot MISSING while idx exists (half-orphan): write the actual event
//     before running the commit barrier. Without this (F3), the retry reports
//     success but the fact remains unreadable after cache eviction.
//  2. evt slot EXISTS with byte-identical content: the record was already durable.
//     The live count is NOT decided from the cache (2.4: the cache is disqualified as a
//     commit oracle) — after re-running the barrier the partition's count is recomputed
//     from the fact chain, idempotent for an already-counted event (net +0) and picking
//     up a durable-but-never-counted C′. Returns already-committed when nothing had to
//     be written, repaired otherwise.
//
// Different content under the same identity remains a refused collision (D15).
func (s *FileSegmentStore) completeOrphanCommit(
	pid int, key int64, idxKVKey string, event FullEvent, eventJSON []byte,
) (ReplayResult, error) {
	idxValue, err := s.kv.KVGet(idxKVKey)
	if err != nil {
		return ReplayNew, fmt.Errorf("orphan probe for event %d failed: %w", key, err)
	}
	var windowTS, seq int64
	if _, err := fmt.Sscanf(idxValue, "%d:%d", &windowTS, &seq); err != nil {
		return ReplayNew, fmt.Errorf("orphan index for event %d unreadable (%q): %w", key, idxValue, err)
	}
	evtKVKey := EventKeyStr(pid, windowTS, int(seq))
	stored, err := s.kv.KVGet(evtKVKey)
	// Classify the evt slot state: missing vs. present-with-right/wrong content.
	evtWasMissing := errors.Is(err, ErrKeyNotFound)
	if err != nil && !evtWasMissing {
		return ReplayNew, fmt.Errorf("orphan slot read for event %d failed: %w", key, err)
	}
	if !evtWasMissing && stored != string(eventJSON) {
		return ReplayNew, fmt.Errorf("event key %d already exists with DIFFERENT content (snowflake collision?): %w",
			key, ErrDuplicateEventKey)
	}
	if evtWasMissing {
		// F3 half-orphan (idx exists, evt slot missing): write the event at its original
		// slot so the fact stays readable after cache eviction.
		if writeErr := s.kv.KVPut(evtKVKey, string(eventJSON)); writeErr != nil {
			return ReplayNew, fmt.Errorf("repair event slot for key %d: %w", key, writeErr)
		}
	}
	// 2.3 class "meta 缺": repair a missing segment meta REGARDLESS of seq (writes only
	// when absent; never resets an existing segment's layer/sealed).
	metaRepaired, mErr := s.ensureWindowMeta(pid, windowTS)
	if mErr != nil {
		return ReplayNew, fmt.Errorf("repair segment meta for event %d: %w", key, mErr)
	}
	// wasComplete: the durable record already existed before this call (evt byte-
	// identical AND meta present) → an already-committed fact on the chain; anything we
	// had to write (the evt or the meta) makes this a repair. Decided from the fact
	// chain, never the cache (2.4).
	wasComplete := !evtWasMissing && !metaRepaired
	// 2.4: an already-committed same-content replay STILL re-runs the real barrier.
	if syncer, ok := s.kv.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			s.markPartitionUnknown(pid) // durability uncertain after a failed barrier (2.5)
			return ReplayNew, fmt.Errorf("durability barrier for event %d failed: %w", key, err)
		}
	}
	// Re-warm the read cache (harmless if present); NOT a commit oracle.
	storedClone := cloneFullEvent(event)
	s.cache.Add(key, &storedClone)
	// Authoritative live count from the fact chain (2.4/2.5), never a per-event ++.
	if rerr := s.recomputePartition(pid); rerr != nil {
		return ReplayNew, fmt.Errorf("recompute partition %d after replay of %d: %w", pid, key, rerr)
	}
	if wasComplete {
		return ReplayAlreadyCommitted, nil
	}
	return ReplayRepaired, nil
}

// ensureWindowMeta repairs a MISSING segment meta for a window without ever
// resetting an existing one (2.3 class "meta 缺"): a window that lost its meta key
// is invisible to segment discovery (ListSegments scans meta), so an event committed
// there — at seq 0 OR seq>0 — would be unfindable. When meta is genuinely absent we
// write a conservative L1 / unsealed record (unsealed → always scanned, never pruned,
// so a conservative envelope can never hide the event) and report repaired=true. When
// meta already exists it is left byte-for-byte intact (repaired=false): a compaction-
// promoted (L2/L3) or sealed segment's layer / sealed flags MUST NOT be unconditionally
// demoted during a replay repair.
func (s *FileSegmentStore) ensureWindowMeta(pid int, windowTS int64) (bool, error) {
	_, err := s.kv.KVGet(MetaKeyStr(pid, windowTS))
	if err == nil {
		return false, nil // present: never reset an existing segment's layer/sealed state
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return false, fmt.Errorf("segment meta probe failed for pid=%d window=%d: %w", pid, windowTS, err)
	}
	meta := SegmentMeta{PartitionID: pid, WindowTS: windowTS, Layer: 1, Sealed: false}
	metaJSON, mErr := json.Marshal(meta)
	if mErr != nil {
		return false, fmt.Errorf("marshal repaired segment meta pid=%d window=%d: %w", pid, windowTS, mErr)
	}
	if pErr := s.kv.KVPut(MetaKeyStr(pid, windowTS), string(metaJSON)); pErr != nil {
		return false, fmt.Errorf("failed to store repaired segment meta for pid=%d window=%d: %w", pid, windowTS, pErr)
	}
	return true, nil
}

// locateOrphanEvtSlot finds an EXISTING event slot already holding `key` when the
// identity index is absent — the half-write a crash leaves between the evt write and
// the idx write (2.3 class "evt 有/idx 缺"). It scans the key's placement window plus
// every registered segment of THIS partition (bounded to one key's partition, not a
// full-history scan), and if the same key lives on more than one layer (a stale L1
// copy beside a compacted L2/L3 copy) it returns the HIGHEST-layer slot so the reused
// idx points at the segment that will survive compaction. A KVScan error fails loud:
// an incomplete scan cannot prove absence, and treating "unknown" as "not found"
// would commit a duplicate second copy of the fact.
func (s *FileSegmentStore) locateOrphanEvtSlot(pid int, hintWindow int64, key int64) (int64, int, bool, error) {
	candidates := map[int64]bool{hintWindow: true}
	if ws, err := s.ListSegments(pid); err == nil {
		for _, w := range ws {
			candidates[w] = true
		}
	}
	var (
		bestW     int64
		bestSeq   int
		bestLayer int64 = -1
		found     bool
	)
	for w := range candidates {
		pairs, err := s.kv.KVScan(SegmentEventPrefix(pid, w), 0)
		if err != nil {
			return 0, 0, false, fmt.Errorf("orphan-evt locate scan failed pid=%d window=%d: %w", pid, w, err)
		}
		layer := int64(1)
		if m, mErr := s.GetSegmentMeta(pid, w); mErr == nil && m != nil {
			layer = int64(m.Layer)
		}
		for _, p := range pairs {
			var e FullEvent
			if json.Unmarshal([]byte(p.Value), &e) != nil || e.EventKey != key {
				continue
			}
			if !found || layer > bestLayer {
				pk, pErr := ParseKey(p.Key)
				if pErr != nil {
					return 0, 0, false, fmt.Errorf("orphan-evt locate parse failed for %q: %w", p.Key, pErr)
				}
				found, bestW, bestSeq, bestLayer = true, w, pk.Seq, layer
			}
		}
	}
	return bestW, bestSeq, found, nil
}

// finishCommit performs the tail of the commit protocol shared by fresh
// writes and idempotent orphan completion (2.6): idx → (meta) → barrier →
// cache/count publication.
func (s *FileSegmentStore) finishCommit(
	pid int, key int64, idxKVKey, idxValue string, windowTS int64, seq int,
	event FullEvent, state *PartitionState,
) error {
	if err := s.kv.KVPut(idxKVKey, idxValue); err != nil {
		s.markPartitionUnknown(pid) // a partial write leaves the partition count untrusted (2.5)
		return fmt.Errorf("failed to store index for event %d: %w", key, err)
	}

	// Write/update segment metadata (if first event in window). Meta is the
	// segment-discovery entry point — a failed meta write must NOT be
	// reported as a committed event (2.5): fail the commit; the orphan
	// evt/idx pair is repaired by the deterministic replay path (2.6).
	if seq == 0 {
		meta := SegmentMeta{
			PartitionID: pid,
			WindowTS:    windowTS,
			Layer:       1,
			Sealed:      false,
		}
		metaJSON, _ := json.Marshal(meta)
		metaKVKey := MetaKeyStr(pid, windowTS)
		if err := s.kv.KVPut(metaKVKey, string(metaJSON)); err != nil {
			s.markPartitionUnknown(pid) // partial write: count untrusted until a re-scan (2.5)
			return fmt.Errorf("failed to store segment meta for event %d: %w", key, err)
		}
	}

	// Durability barrier (2.3): the event is committed only once its
	// evt+idx(+meta) writes are flushed/fsynced by the backend. Backends
	// without a Sync capability keep flush-level semantics (documented).
	if syncer, ok := s.kv.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			s.markPartitionUnknown(pid) // durability uncertain: pause eviction for this partition (2.5)
			return fmt.Errorf("durability barrier for event %d failed: %w", key, err)
		}
	}

	// Publication happens ONLY after the barrier: cache (defensive clone) and
	// the live count. A failed commit above leaves both untouched (2.9).
	stored := cloneFullEvent(event)
	s.cache.Add(key, &stored)
	state.mu.Lock()
	state.eventCount++
	state.mu.Unlock()

	return nil
}

// recoverWindowSeqLocked returns the next free seq for a window by scanning
// its existing event keys. It returns 0 for an empty window and max(seq)+1
// otherwise. seq keys are zero-padded-free strings (0,1,10,2…) so the max
// must be computed numerically, not taken from scan order.
//
// A scan failure is an ERROR, not a silent 0: falling back to 0 would
// overwrite existing slots (the very bug D12 exists to prevent), so the
// caller fails the StoreEvent instead (code-review M3).
//
// Caller holds the partition state lock.
func (s *FileSegmentStore) recoverWindowSeqLocked(pid int, windowTS int64) (int, error) {
	pairs, err := s.kv.KVScan(SegmentEventPrefix(pid, windowTS), 0)
	if err != nil {
		return 0, fmt.Errorf("scan window events: %w", err)
	}
	maxSeq := -1
	for _, pair := range pairs {
		pk, err := ParseKey(pair.Key)
		if err != nil || pk.KeyType != "evt" {
			continue
		}
		if pk.Seq > maxSeq {
			maxSeq = pk.Seq
		}
	}
	return maxSeq + 1, nil
}

// ==================== Read Operations ====================

// GetEvent retrieves a single event by its EventKey.
//
// Error taxonomy (2.5): a genuinely absent key returns an error wrapping
// ErrKeyNotFound; storage I/O failures are returned AS-IS (never dressed up
// as "not found") so upper layers can tell a miss from an outage. Returned
// events are defensive clones — callers may mutate them freely.
func (s *FileSegmentStore) GetEvent(key int64) (*FullEvent, error) {
	if key == 0 {
		return nil, fmt.Errorf("event key cannot be zero")
	}

	// Check tombstone
	if s.tombstones != nil && s.tombstones.IsTombstone(key) {
		return nil, fmt.Errorf("event %d not found: %w (tombstoned)", key, ErrKeyNotFound)
	}

	// 1. Check LRU cache (clone: the cache must not alias what we hand out)
	if cached, ok := s.cache.Get(key); ok {
		evt := cloneFullEvent(*cached)
		return &evt, nil
	}

	// 2. Look up index to find segment position
	pid := PartitionIDFromEventKey(key)
	idxKVKey := IndexKeyStr(pid, key)
	idxValue, err := s.kv.KVGet(idxKVKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("event %d not found: %w", key, ErrKeyNotFound)
		}
		return nil, fmt.Errorf("event %d index lookup failed (storage I/O): %w", key, err)
	}

	// Parse index value: "windowTS:seq"
	parts := strings.SplitN(idxValue, ":", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid index value for event %d: %s", key, idxValue)
	}

	var windowTS, seq int64
	if _, err := fmt.Sscanf(idxValue, "%d:%d", &windowTS, &seq); err != nil {
		return nil, fmt.Errorf("failed to parse index value for event %d: %s", key, idxValue)
	}

	// 3. Read event from KV store
	evtKVKey := EventKeyStr(pid, windowTS, int(seq))
	eventJSON, err := s.kv.KVGet(evtKVKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("event %d not found at %s: %w", key, evtKVKey, ErrKeyNotFound)
		}
		return nil, fmt.Errorf("event %d read failed at %s (storage I/O): %w", key, evtKVKey, err)
	}

	var event FullEvent
	if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event %d: %w", key, err)
	}

	// Add to cache (clone) and hand back an independent copy.
	stored := cloneFullEvent(event)
	s.cache.Add(key, &stored)
	evt := cloneFullEvent(event)
	return &evt, nil
}

// GetEvents retrieves multiple events by their EventKeys. A genuinely
// missing key (typed) is skipped; a storage I/O failure is NOT silently
// swallowed — the successfully read events are returned together with the
// error so callers can tell a partial result from a complete one (2.5).
func (s *FileSegmentStore) GetEvents(keys []int64) ([]FullEvent, error) {
	results := make([]FullEvent, 0, len(keys))
	var firstErr error
	for _, key := range keys {
		evt, err := s.GetEvent(key)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				continue // legal miss: tombstoned or never written
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("GetEvents key %d: %w", key, err)
			}
			continue
		}
		results = append(results, *evt)
	}
	return results, firstErr
}

// QueryEvents queries events based on filters.
//
// Behavioral contract (segment-query-recency): the result is semantically
// equivalent to "filter all events → total-order sort → offset/limit".
// Segmentation, window pruning and early-stop below are optimizations only
// and must not change the observable result. Total order: (Timestamp,
// EventKey) — same-millisecond events are tie-broken by EventKey so any two
// runs (and any store implementation) return identical sequences.
func (s *FileSegmentStore) QueryEvents(query QueryOptions) ([]EventReference, error) {
	// Determine which partitions to search
	partitions := s.resolvePartitions(query)

	limit := query.Limit
	if limit <= 0 {
		limit = 100 // Default limit
	}
	// Per-partition collection budget: the global top-(offset+limit) can only
	// draw from each partition's own top-(offset+limit), so collecting that
	// many per partition before the global sort is lossless.
	budget := limit
	if query.Offset > 0 {
		budget += query.Offset
	}

	var matched []EventReference
	var queryErr error
	for _, pid := range partitions {
		// Scan this partition's segments (no cross-partition truncation —
		// truncating before the global sort would drop newer partitions).
		// A partition scan failure is recorded and returned with the partial
		// result — it must not degrade into a silent empty answer (2.5).
		refs, err := s.scanPartition(pid, query, budget)
		if err != nil {
			if queryErr == nil {
				queryErr = fmt.Errorf("scan partition %d: %w", pid, err)
			}
			continue
		}
		matched = append(matched, refs...)
	}

	sortRefsByTotalOrder(matched, query.OrderBy)

	// Apply offset
	if query.Offset > 0 {
		if query.Offset >= len(matched) {
			return nil, queryErr
		}
		matched = matched[query.Offset:]
	}
	// Apply limit after sorting
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return matched, queryErr
}

// sortRefsByTotalOrder sorts references by the total order (Timestamp,
// EventKey); orderBy "timestamp_desc" reverses both keys together.
func sortRefsByTotalOrder(refs []EventReference, orderBy string) {
	desc := orderBy == "timestamp_desc"
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Timestamp != refs[j].Timestamp {
			if desc {
				return refs[i].Timestamp > refs[j].Timestamp
			}
			return refs[i].Timestamp < refs[j].Timestamp
		}
		if desc {
			return refs[i].EventKey > refs[j].EventKey
		}
		return refs[i].EventKey < refs[j].EventKey
	})
}

// noUpperBoundMs marks a segment whose time upper bound cannot be proven.
const noUpperBoundMs int64 = math.MaxInt64

// segmentBounds derives the segment's TRUTHFUL event-time envelope for query
// pruning and early-stop (segment-query-recency D14 — LSM key-range
// metadata):
//
//   - Sealed with MinTime/MaxTime → those bounds (event time, stable once
//     the segment is immutable). WindowTS remains a valid lower bound when
//     MinTime is absent.
//   - Unsealed (active memtable) OR sealed-but-boundless (legacy segment) →
//     unprovable: provable=false, meaning NEVER prune, NEVER skip.
//
// The nominal window name/layer is NOT consulted for time reasoning — it
// encodes write recency and compaction generation, not event-time coverage.
func segmentBounds(windowTS int64, meta SegmentMeta) (lowerMs, upperMs int64, provable bool) {
	if !meta.Sealed || meta.MaxTime <= 0 {
		return 0, noUpperBoundMs, false
	}
	// MinTime is the REAL event-time minimum recorded at seal/compaction —
	// trust it directly. Do NOT max() it against the nominal window start:
	// placement follows WRITE time while MinTime is EVENT time, and the two
	// diverge for asynchronous write-back (an event older than its window).
	// The window name is only the fallback when MinTime is absent.
	lowerMs = windowTS * 1000
	if meta.MinTime > 0 {
		lowerMs = meta.MinTime
	}
	return lowerMs, meta.MaxTime, true
}

// scanPartition scans a single partition's segments matching the query.
//
// Windows are traversed in the query's time direction (desc: newest first) so
// that early-stop sacrifices the oldest matches, never the newest — the
// recall arrow must point the same way as compression (drop old, keep new).
// Collection is whole-window: seq keys are lexicographic (0,1,10,2…), not
// time-ordered, so a window must be fully scanned before its matches count.
func (s *FileSegmentStore) scanPartition(pid int, query QueryOptions, budget int) ([]EventReference, error) {
	// Phase 1: window discovery — scan meta prefix, parse (windowTS, layer,
	// MinTime/MaxTime) from key + value (all ride in the meta JSON; no extra
	// KVGet).
	metaPrefix := MetaPrefix(pid)
	metaPairs, err := s.kv.KVScan(metaPrefix, 0)
	if err != nil {
		return nil, err
	}

	type windowInfo struct {
		ts       int64
		layer    int
		provable bool  // whether lower/upper are truthful (D14)
		lowerMs  int64 // truthful lower bound when provable
		upperMs  int64 // truthful upper bound when provable
	}
	var windows []windowInfo
	for _, pair := range metaPairs {
		pk, err := ParseKey(pair.Key)
		if err != nil || pk.KeyType != "meta" {
			continue
		}
		var meta SegmentMeta
		if err := json.Unmarshal([]byte(pair.Value), &meta); err != nil {
			meta = SegmentMeta{}
		}
		lowerMs, upperMs, provable := segmentBounds(pk.WindowTS, meta)

		// Time range pruning (Unix ms contract): a segment is pruned only when
		// its PROVABLE bounds put it entirely outside the range. Active
		// (unsealed) and boundless segments are memtables — never pruned.
		if provable {
			if query.EndTime > 0 && lowerMs > query.EndTime {
				continue
			}
			if query.StartTime > 0 && upperMs < query.StartTime {
				continue
			}
		}
		windows = append(windows, windowInfo{ts: pk.WindowTS, layer: meta.Layer, provable: provable, lowerMs: lowerMs, upperMs: upperMs})
	}

	desc := query.OrderBy == "timestamp_desc"
	sort.Slice(windows, func(i, j int) bool {
		if desc {
			return windows[i].ts > windows[j].ts
		}
		return windows[i].ts < windows[j].ts
	})

	// Phase 2: per-window collection with cross-window dedup and
	// budget-based skipping.
	var matched []EventReference
	var scanErr error
	// Dedup: EventKey → (index in matched, layer of chosen version). On
	// duplicates (compaction crash window: same event alive in source and
	// target layers) keep the higher-layer version — direction-independent,
	// so desc and asc return the same content version (D3).
	seenIdx := make(map[int64]int)
	seenLayer := make(map[int64]int)
	// Collected-side bounds use ACTUAL event timestamps, never nominal window
	// bounds (D10): nominal bounds are untruthful for compacted segments, and
	// the real timestamps are already in hand and tighter.
	var minCollectedTs, maxCollectedTs int64
	collectedAny := false

	for _, w := range windows {
		// Early-stop as a per-window skip (layers have non-monotonic bounds, so
		// a hard break could skip an overlapping daily/weekly window). Once the
		// budget is met, a window can be skipped only when it provably cannot
		// hold an event that outranks the collected set. Comparison is STRICT:
		// on equality the same-millisecond EventKey tie-break could still let
		// the candidate win.
		if budget > 0 && len(matched) >= budget && collectedAny {
			if desc && w.provable && w.upperMs < minCollectedTs {
				// Every event in w is older than every collected match.
				continue
			}
			if !desc && w.provable && w.lowerMs > maxCollectedTs {
				// Every event in w is newer than every collected match.
				continue
			}
		}

		wLayer := w.layer

		// Scan events in this segment (whole window, no intra-window stop).
		// A window scan failure is recorded (first error wins) and scanning
		// continues with the remaining windows — the result stays partial,
		// never a silent empty answer (2.5).
		eventPrefix := SegmentEventPrefix(pid, w.ts)
		eventPairs, err := s.kv.KVScan(eventPrefix, 0)
		if err != nil {
			if scanErr == nil {
				scanErr = fmt.Errorf("scan window %d: %w", w.ts, err)
			}
			continue
		}

		for _, ep := range eventPairs {
			evtPK, err := ParseKey(ep.Key)
			if err != nil || evtPK.KeyType != "evt" {
				continue
			}

			// Parse the value (JSON FullEvent) to extract reference fields
			var event FullEvent
			if err := json.Unmarshal([]byte(ep.Value), &event); err != nil {
				continue
			}

			// Skip tombstoned events
			if s.tombstones != nil && s.tombstones.IsTombstone(event.EventKey) {
				continue
			}

			// Apply filters
			if !matchesQueryFilters(event, query) {
				continue
			}

			ref := EventReference{
				EventKey:     event.EventKey,
				PartitionID:  event.PartitionID,
				EventType:    event.EventType,
				EventSummary: event.EventSummary,
				Timestamp:    event.Timestamp,
			}
			if idx, dup := seenIdx[event.EventKey]; dup {
				if wLayer > seenLayer[event.EventKey] {
					matched[idx] = ref
					seenLayer[event.EventKey] = wLayer
				}
				continue
			}
			seenIdx[event.EventKey] = len(matched)
			seenLayer[event.EventKey] = wLayer
			matched = append(matched, ref)

			// Track the collected set's ACTUAL timestamp extremes — the
			// early-stop basis (D10).
			if !collectedAny || event.Timestamp < minCollectedTs {
				minCollectedTs = event.Timestamp
			}
			if !collectedAny || event.Timestamp > maxCollectedTs {
				maxCollectedTs = event.Timestamp
			}
			collectedAny = true
		}
	}

	return matched, scanErr
}

// resolvePartitions determines which partitions to search.
func (s *FileSegmentStore) resolvePartitions(query QueryOptions) []int {
	if len(query.PartitionIDs) > 0 {
		return query.PartitionIDs
	}
	if query.PartitionID > 0 {
		return []int{query.PartitionID}
	}
	// No partition filter - cannot determine partitions from KV
	// Return empty to skip (caller must specify at least one partition)
	return nil
}

// matchesQueryFilters checks if an event matches the query filters.
func matchesQueryFilters(event FullEvent, query QueryOptions) bool {
	// Filter by event types
	if len(query.EventTypes) > 0 {
		found := false
		for _, et := range query.EventTypes {
			if event.EventType == et {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	// Filter by time range
	if query.StartTime > 0 && event.Timestamp < query.StartTime {
		return false
	}
	if query.EndTime > 0 && event.Timestamp > query.EndTime {
		return false
	}
	// Filter by write-order key bound (strictly greater; see QueryOptions.
	// MinEventKey — write axis, never a semantic-time approximation).
	if query.MinEventKey != 0 && event.EventKey <= query.MinEventKey {
		return false
	}
	// Filter by keyword: term-split ANY-match (see matchesKeyword) — a
	// literal whole-string match silently returns zero for the space-
	// separated keyword lists and sentences models actually send.
	if query.Keyword != "" {
		if !matchesKeyword(event.EventSummary, query.Keyword) &&
			!matchesKeyword(event.Content, query.Keyword) {
			return false
		}
	}
	return true
}

// RelationStore returns the underlying RelationStore for relationship operations.
func (s *FileSegmentStore) RelationStore() RelationStore {
	return s.rel
}

// ==================== Management Operations ====================

// DeleteEvent permanently deletes an event from storage.
//
// Idempotent against logical death (2.9): an already-tombstoned event had
// its live count decremented at marking time — a repeated (or compaction
// cleanup) delete must NOT decrement again.
func (s *FileSegmentStore) DeleteEvent(key int64) error {
	if key == 0 {
		return fmt.Errorf("event key cannot be zero")
	}
	if s.tombstones != nil && s.tombstones.IsTombstone(key) {
		return nil // logically dead already: deletion is idempotent, no re-decrement
	}
	// §2.8: an explicit delete of a still-retained (unacked recovery) original is refused
	// WITHOUT destroying the durable record — the shared-resource recovery owner holds a
	// live lease. Lossless relocation is unaffected (only this destruction path checks).
	if s.retention.IsProtected(key) {
		return fmt.Errorf("%w: key=%d", ErrEventProtected, key)
	}

	pid := PartitionIDFromEventKey(key)
	// 2.2: a delete is a partition mutation — serialize it under the same
	// mutationMu as commits/compaction/seal so it can never interleave with a
	// writer or the compactor republishing this pid (lost or resurrected event).
	// decrementEventCount below takes the inner state.mu (M-order).
	delState := s.getPartitionState(pid)
	delState.mutationMu.Lock()
	defer delState.mutationMu.Unlock()
	idxKVKey := IndexKeyStr(pid, key)

	// Get the event key to determine the full KV key to delete. A genuinely
	// absent key (typed missing) makes the deletion IDEMPOTENT (2.9): no
	// error, no double decrement — "delete what is already gone" succeeded.
	idxValue, err := s.kv.KVGet(idxKVKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil
		}
		return fmt.Errorf("event %d delete probe failed (storage I/O): %w", key, err)
	}

	var windowTS, seq int64
	fmt.Sscanf(idxValue, "%d:%d", &windowTS, &seq)
	evtKVKey := EventKeyStr(pid, windowTS, int(seq))

	// Batch delete
	ops := []KVOp{
		{Type: "delete", Key: evtKVKey},
		{Type: "delete", Key: idxKVKey},
	}
	if err := s.kv.KVBatch(ops); err != nil {
		return fmt.Errorf("failed to delete event %d: %w", key, err)
	}

	s.cache.Remove(key)

	// Keep the process-lifetime count accurate (D11): capacity eviction
	// reads this counter.
	s.decrementEventCount(pid, 1)
	return nil
}

// decrementEventCount lowers the process-lifetime event counter after
// physical removal (DeleteEvent, compaction cleanup). Floored at zero.
func (s *FileSegmentStore) decrementEventCount(pid int, n int64) {
	state := s.getPartitionState(pid)
	state.mu.Lock()
	state.eventCount -= n
	if state.eventCount < 0 {
		state.eventCount = 0
	}
	state.mu.Unlock()
}

// GetStats returns storage statistics.
func (s *FileSegmentStore) GetStats() StoreStats {
	total := int64(0)
	s.partitions.Range(func(_, v interface{}) bool {
		state := v.(*PartitionState)
		state.mu.Lock()
		total += state.eventCount
		state.mu.Unlock()
		return true
	})
	return StoreStats{
		TotalEvents: int(total),
		DataDir:     s.dataDir,
		CountsKnown: s.countsKnown.Load(),
	}
}

// KVBackend 暴露底层 KVStore（KVProvider 实现）——供记忆引擎做向量持久化
// （序列化向量入 KV + 启动重建，见 engine_persist.go）。
func (s *FileSegmentStore) KVBackend() KVStore { return s.kv }

// WalQuarantined 透传底层 KV 的 WAL 隔离计数（§8.11⑤ 第三跳——此前装饰链在
// FileSegmentStore 断裂，诊断恒采 0）。
func (s *FileSegmentStore) WalQuarantined() int64 {
	if q, ok := s.kv.(interface{ WalQuarantined() int64 }); ok {
		return q.WalQuarantined()
	}
	return 0
}

// SetVectorRemover 注册向量移除回调（wireMemoryEngine 包裹引擎后调用）。
// 用 atomic 存：compactor goroutine 可能已在运行，读写需无竞态。
func (s *FileSegmentStore) SetVectorRemover(vr VectorRemover) {
	if vr != nil {
		s.vecRemover.Store(vr)
	}
}

// removeVector 回调已注册的向量移除器（未注册则 no-op）。遗忘物理删除时调用（审查 M2）。
func (s *FileSegmentStore) removeVector(eventKey int64) {
	if v := s.vecRemover.Load(); v != nil {
		if vr, ok := v.(VectorRemover); ok {
			vr.RemoveVector(eventKey)
		}
	}
}

// SearchByEmbedding performs semantic search (stub — not supported).
func (s *FileSegmentStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
	return nil, ErrVectorSearchNotSupported
}

// StoreEventWithEmbedding stores event with embedding (stub — ignores embedding).
func (s *FileSegmentStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
	return s.StoreEvent(key, event)
}

// SupportsVectorSearch returns false.
func (s *FileSegmentStore) SupportsVectorSearch() bool {
	return false
}

// ==================== Segment Management ====================

// SealCurrent seals the current active segment for a partition.
// Updates segment metadata in KV to mark it as L1 (sealed).
func (s *FileSegmentStore) SealCurrent(pid int) error {
	state := s.getPartitionState(pid)
	// 2.2: seal publishes the current window's immutable meta and must serialize
	// with writer commits to that window — a concurrent append after the envelope
	// scan would make the recorded MinTime/MaxTime lie or re-open a sealing segment.
	state.mutationMu.Lock()
	defer state.mutationMu.Unlock()
	state.mu.Lock()
	windowTS := state.currentWindow
	seqCount := state.seqCounter
	state.mu.Unlock()

	if seqCount == 0 {
		return nil // Nothing to seal
	}

	// Sealing is the LSM flush point: the segment becomes immutable, so its
	// truthful event-time envelope (MinTime/MaxTime) is recorded here and
	// stays stable forever after. One scan of the window being sealed
	// (hourly, per partition) — cheap compared to the queries it enables.
	minTime, maxTime := int64(0), int64(0)
	hasMin := false // 0 is a legal Timestamp; don't use it as the sentinel (n1)
	eventPrefix := SegmentEventPrefix(pid, windowTS)
	eventPairs, err := s.kv.KVScan(eventPrefix, 0)
	if err == nil {
		for _, ep := range eventPairs {
			var evt struct {
				Timestamp int64 `json:"timestamp"`
			}
			if jsonErr := json.Unmarshal([]byte(ep.Value), &evt); jsonErr != nil {
				continue
			}
			if !hasMin || evt.Timestamp < minTime {
				minTime = evt.Timestamp
				hasMin = true
			}
			if evt.Timestamp > maxTime {
				maxTime = evt.Timestamp
			}
		}
	}

	// Write/update segment metadata
	meta := SegmentMeta{
		PartitionID: pid,
		WindowTS:    windowTS,
		Layer:       1,
		EventCount:  seqCount,
		MinTime:     minTime,
		MaxTime:     maxTime,
		Sealed:      true,
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal segment meta: %w", err)
	}

	metaKVKey := MetaKeyStr(pid, windowTS)
	return s.kv.KVPut(metaKVKey, string(metaJSON))
}

// GetSegmentMeta retrieves segment metadata from KV.
func (s *FileSegmentStore) GetSegmentMeta(pid int, windowTS int64) (*SegmentMeta, error) {
	metaKVKey := MetaKeyStr(pid, windowTS)
	value, err := s.kv.KVGet(metaKVKey)
	if err != nil {
		return nil, err
	}
	var meta SegmentMeta
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ListSegments returns all segment window timestamps for a partition.
func (s *FileSegmentStore) ListSegments(pid int) ([]int64, error) {
	metaPrefix := MetaPrefix(pid)
	pairs, err := s.kv.KVScan(metaPrefix, 0)
	if err != nil {
		return nil, err
	}
	var windows []int64
	for _, pair := range pairs {
		pk, err := ParseKey(pair.Key)
		if err != nil {
			continue
		}
		windows = append(windows, pk.WindowTS)
	}
	sort.Slice(windows, func(i, j int) bool {
		return windows[i] < windows[j]
	})
	return windows, nil
}

// ==================== Lifecycle Wiring ====================

// SetTombstoneSet injects a TombstoneSet into the store after construction.
// This allows tombstone filtering to be enabled without modifying NewFileSegmentStore's signature.
// Once set, GetEvent and QueryEvents will skip tombstoned events.
func (s *FileSegmentStore) SetTombstoneSet(ts *TombstoneSet) {
	s.tombstones = ts
}

// SetLifecycleManager injects a LifecycleManager for graceful shutdown.
// The manager is stopped when Close() is called.
func (s *FileSegmentStore) SetLifecycleManager(lm *LifecycleManager) {
	s.lifecycle = lm
}

// SetCompactor injects a Compactor for graceful shutdown.
// The compactor is stopped when Close() is called.
func (s *FileSegmentStore) SetCompactor(c *Compactor) {
	s.compactor = c
}

// SetRetentionLease injects the §2.8 unacked-recovery retention lease. It MUST be
// called during recovery rebuild, BEFORE the lifecycle/compaction producers start,
// so a scanner can never destroy a retained original before its lease is registered
// (spec restart-race scenario). Passing nil clears protection.
func (s *FileSegmentStore) SetRetentionLease(l *RetentionLease) {
	s.retention = l
}

// RetentionLease returns the shared-resource retention lease (nil when unwired).
func (s *FileSegmentStore) RetentionLease() *RetentionLease { return s.retention }

// IsKeyProtected reports whether a key is under a live §2.8 retention lease. The TTL
// scanner, capacity evictor and compactor final-cleanup consult this so a protected
// original survives until its recovery owner releases the lease. nil-safe.
func (s *FileSegmentStore) IsKeyProtected(key int64) bool { return s.retention.IsProtected(key) }

// ProtectKey registers a holder for a key under the shared retention lease (§2.8). It
// is used by the recovery owner (inbox envelope / spill) before the first durable fact
// commit so the material cannot be evicted out from under an in-flight prepare.
func (s *FileSegmentStore) ProtectKey(key int64) { s.retention.Protect(key) }

// ReleaseKey drops one holder for a key (§2.8); the original becomes eligible for
// normal age-based destruction once the last holder is gone. Called after ack dir-sync
// success or spill safe-removal. Restores the key's ORIGINAL TTL window (no re-stamping).
func (s *FileSegmentStore) ReleaseKey(key int64) { s.retention.Release(key) }

// ArmRetention releases the store's first destructive scan once the recovery owner has
// finished rebuilding the lease from existing unacked material (the §2.8 restart-race
// gate, paired with the scanner's Lease.Ready() wait). Idempotent; a store with no lease
// (nil retention) ignores it. Implements memory.RetentionGuard.
func (s *FileSegmentStore) ArmRetention() { s.retention.MarkReady() }

// BeginHold/EndHold expose the §5.8 registration barrier of the store's lease
// (composition-root aggregate inventory + late attach pause forgetting while
// any owner is mid-registration). nil-safe via the lease itself.
func (s *FileSegmentStore) BeginHold() { s.retention.BeginHold() }
func (s *FileSegmentStore) EndHold()   { s.retention.EndHold() }

// FileSegmentStore owns the shared retention lease (the durable "材料保留" capability).
var _ RetentionGuard = (*FileSegmentStore)(nil)

// Compactor returns the injected background compactor (nil when the store
// runs without one). Harness hook for synchronous compaction triggering
// (resident-remaining-hardening 3.1 soak subprocess).
func (s *FileSegmentStore) Compactor() *Compactor {
	return s.compactor
}

// Lifecycle returns the injected lifecycle manager (nil when the store runs
// without one). Harness hook for synchronous TTL/capacity sweeps
// (resident-remaining-hardening 3.2 E2E).
func (s *FileSegmentStore) Lifecycle() *LifecycleManager {
	return s.lifecycle
}

// closer is an internal interface for resources that need cleanup.
type closer interface {
	Close() error
}

// StopProducers halts the background forgetting producers (compactor +
// lifecycle scanner) WITHOUT touching the relation/KV backend. D5 close order
// requires the producers to stop BEFORE the engine worker, because they call
// the engine's vector remover while sweeping — yet the engine's own drain still
// writes the KV, so the KV must close AFTER the engine. Both Stop methods are
// idempotent, so a subsequent Close re-stops them as a no-op.
func (s *FileSegmentStore) StopProducers() {
	// Stop Compactor first (it may be writing segments)
	if s.compactor != nil {
		s.compactor.Stop()
	}
	// Stop LifecycleManager (it may be marking tombstones)
	if s.lifecycle != nil {
		s.lifecycle.Stop()
	}
}

// Close stops all background components (Compactor, LifecycleManager) and
// closes the RelationStore if it supports closing. Idempotent via sync.Once.
func (s *FileSegmentStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		// Producers first (idempotent: a no-op if closeResource already stopped
		// them before the engine worker).
		s.StopProducers()
		// Close RelationStore if it supports closing (e.g., InMemRelationStore flushes WAL)
		if c, ok := s.rel.(closer); ok {
			if e := c.Close(); e != nil {
				err = e
			}
		}
		// Close the KV last — it performs the final durability flush.
		// Without this, LocalFileKV's deferred-flush window (up to
		// flushInterval / flushThreshold-1 writes) is lost on graceful exit.
		if c, ok := s.kv.(closer); ok {
			if e := c.Close(); e != nil {
				err = e
			}
		}
	})
	return err
}

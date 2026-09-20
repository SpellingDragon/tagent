package memory

import (
	"encoding/json"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/event"
)

// ==================== Lifecycle Manager ====================
//
// LifecycleManager handles TTL expiration and capacity-based eviction.
// It works with TombstoneSet to mark expired events as tombstoned.

// LifecycleConfig configures the lifecycle manager.
type LifecycleConfig struct {
	// GlobalTTLDays is the default TTL for all events (default: 7).
	GlobalTTLDays int `json:"global_ttl_days"`
	// MaxEventsPerPartition is the maximum event count per partition (0 = no limit).
	MaxEventsPerPartition int `json:"max_events_per_partition"`
	// CheckInterval is how often to check for expired events (default: 1 hour).
	CheckInterval time.Duration `json:"check_interval"`
	// TypeTTL overrides global TTL for specific event types (in days).
	// Key = event type, Value = TTL in days.
	TypeTTL map[string]int `json:"type_ttl,omitempty"`
}

// DefaultLifecycleConfig returns the default lifecycle configuration.
func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		GlobalTTLDays:         7,
		MaxEventsPerPartition: 0, // No limit by default
		CheckInterval:         time.Hour,
		// TypeTTL 派生自事件类型注册表（唯一权威源，见 event/registry.go）：
		// external_input=30, agent_output=14, action_command=14, thinking_plan=3,
		// context_compress=3, context_compress_summary=-1（豁免遗忘，长期记忆：
		// 索引卡片指向这些 key，过期会留下悬空票据）。
		TypeTTL: event.DefaultTypeTTL(),
	}
}

// LifecycleManager manages event TTL expiration and capacity eviction.
type LifecycleManager struct {
	store     *FileSegmentStore
	tombstone *TombstoneSet
	config    LifecycleConfig

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewLifecycleManager creates a LifecycleManager.
func NewLifecycleManager(store *FileSegmentStore, tombstone *TombstoneSet, config LifecycleConfig) *LifecycleManager {
	// Only the ZERO value falls back to the default (bare programmatic
	// construction). A NEGATIVE GlobalTTLDays is meaningful: it disables
	// TTL-based forgetting entirely (getEffectiveTTL <= 0 → skip), so it must
	// NOT be clamped back to the default (code-review B1).
	if config.GlobalTTLDays == 0 {
		config.GlobalTTLDays = 7
	}
	if config.CheckInterval <= 0 {
		config.CheckInterval = time.Hour
	}
	if config.TypeTTL == nil {
		config.TypeTTL = make(map[string]int)
	}
	return &LifecycleManager{
		store:     store,
		tombstone: tombstone,
		config:    config,
		stopCh:    make(chan struct{}),
	}
}

// Start starts the lifecycle manager background goroutine.
func (lm *LifecycleManager) Start() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if lm.running {
		return
	}
	lm.running = true
	lm.stopCh = make(chan struct{})
	lm.wg.Add(1)
	go lm.scannerLoop()
}

// Stop stops the lifecycle manager gracefully.
func (lm *LifecycleManager) Stop() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if !lm.running {
		return
	}
	lm.running = false
	close(lm.stopCh)
	lm.wg.Wait()
}

// armGrace bounds how long the first destructive scan waits for the §2.8 retention
// lease to be armed before proceeding (anti-starvation backstop for a durable store
// opened without a durable recovery owner). Normal wiring arms in milliseconds.
const armGrace = 60 * time.Second

func (lm *LifecycleManager) armGrace() time.Duration { return armGrace }

// scannerLoop runs periodically to check for expired events and capacity.
func (lm *LifecycleManager) scannerLoop() {
	defer lm.wg.Done()

	// §2.8 B: never run the first destructive pass until the recovery owner has rebuilt
	// the retention lease from existing unacked material. A store with no lease (nil)
	// proceeds immediately (no durable recovery to guard, behavior unchanged); a leased
	// store blocks here until MarkReady, closing the restart race (spec L117-119) where an
	// overdue-but-retained original could otherwise be tombstoned before its lease lands.
	// A startup grace backstops the wait so a leased store whose recovery owner never arms
	// (e.g. a durable backend opened without a durable inbox) cannot starve TTL/capacity
	// forever — in normal wiring the arm lands in milliseconds, far inside the grace. Stop()
	// unblocks this via stopCh. SweepOnce (harness) bypasses the gate deliberately.
	select {
	case <-lm.store.RetentionLease().Ready():
	case <-time.After(lm.armGrace()):
		if lm.store.RetentionLease() != nil {
			log.Warnf("[Lifecycle] retention lease not armed within grace — proceeding with forgetting (no durable recovery owner registered; §2.8 backstop)")
		}
	case <-lm.stopCh:
		return
	}

	// §5.8: an active registration barrier pauses forgetting UNCONDITIONALLY — the
	// armGrace backstop above only covers "a recovery owner never registered";
	// while an owner is mid-inventory (composition-root build gate, a late
	// attach, or a BLOCKED inventory whose hold is deliberately kept), no timed
	// escape may release a destructive pass. Nil lease / no hold passes at once.
	select {
	case <-lm.store.RetentionLease().HoldClear():
	case <-lm.stopCh:
		return
	}

	// Run initial check
	lm.checkTTL()
	lm.checkCapacity()

	ticker := time.NewTicker(lm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// §5.8: a late-attaching owner (new shared recovery dir on an already
			// running store) raises the barrier at any time; passes issued while it
			// holds must pause — skip this tick, the next one lands shortly.
			if lm.forgettingPaused() {
				continue
			}
			lm.checkTTL()
			lm.checkCapacity()
		case <-lm.stopCh:
			return
		}
	}
}

// forgettingPaused reports an active §5.8 registration barrier without waiting
// (the pass for this tick is skipped; forgetting resumes on the next tick once
// every in-flight inventory has ended its hold).
func (lm *LifecycleManager) forgettingPaused() bool {
	select {
	case <-lm.store.RetentionLease().HoldClear():
		return false
	default:
		return true
	}
}

// SweepOnce runs one synchronous lifecycle pass (TTL scan + capacity check)
// — the scheduler body, exported so E2E/soak harnesses exercise real TTL
// expiry without waiting the scanner interval
// (resident-remaining-hardening 3.2). Same semantics, no second truth.
func (lm *LifecycleManager) SweepOnce() {
	lm.checkTTL()
	lm.checkCapacity()
}

// checkTTL scans events and marks expired ones as tombstoned.
func (lm *LifecycleManager) checkTTL() {
	now := time.Now().UnixMilli()

	// Iterate over partitions to check events
	lm.store.partitions.Range(func(key, value interface{}) bool {
		pid := key.(int)

		// List segments for this partition
		windows, err := lm.store.ListSegments(pid)
		if err != nil {
			return true
		}

		for _, windowTS := range windows {
			eventPrefix := SegmentEventPrefix(pid, windowTS)
			pairs, err := lm.store.kv.KVScan(eventPrefix, 0)
			if err != nil {
				continue
			}

			for _, pair := range pairs {
				// The EventKey lives in the event's JSON VALUE, not in the KV key
				// (whose format {pid}:evt:{window}:{seq} carries no event key —
				// ParseKey leaves EventKey zero for evt keys, which silently
				// disabled TTL for months: `EventKey == 0 → continue` swallowed
				// every event).
				var evt struct {
					EventKey  int64  `json:"event_key"`
					Timestamp int64  `json:"timestamp"`
					Type      string `json:"event_type"`
				}
				if err := json.Unmarshal([]byte(pair.Value), &evt); err != nil || evt.EventKey == 0 {
					continue
				}

				// Check if already tombstoned
				if lm.tombstone.IsTombstone(evt.EventKey) {
					continue
				}

				// §2.8: a still-retained unacked-recovery original must not be expired by
				// TTL — its lease is released only after ack/spill-removal, after which the
				// key resumes normal age-based expiry using its ORIGINAL timestamp.
				if lm.store.IsKeyProtected(evt.EventKey) {
					continue
				}

				// Determine TTL for this event type
				ttlDays, err := lm.getEffectiveTTL(evt.Type)
				if err != nil || ttlDays <= 0 {
					continue
				}

				ttlMs := int64(ttlDays) * 24 * 60 * 60 * 1000
				eventAge := now - evt.Timestamp

				if eventAge > ttlMs {
					if err := lm.tombstone.MarkTombstone(evt.EventKey); err != nil {
						log.Errorf("[Lifecycle] MarkTombstone failed key=%d: %v", evt.EventKey, err)
						continue
					}
					// eventCount tracks LOGICALLY LIVE events (capacity decisions);
					// a tombstoned event is dead to readers even before compaction
					// removes it physically — decrement on marking (code-review M4).
					lm.store.decrementEventCount(pid, 1)
				}
			}
		}
		return true
	})
}

// checkCapacity checks if partitions exceed capacity and evicts oldest events.
func (lm *LifecycleManager) checkCapacity() {
	if lm.config.MaxEventsPerPartition <= 0 {
		return
	}
	// Unknown counts must never drive eviction (2.8): a failed/unavailable
	// rebuild would otherwise be misread as "0 events" and skip, or a stale
	// count could over-evict. Pause until a successful RebuildLiveCounts.
	if !lm.store.LivesCountKnown() {
		return
	}

	lm.store.partitions.Range(func(key, value interface{}) bool {
		pid := key.(int)
		state := value.(*PartitionState)

		state.mu.Lock()
		count := state.eventCount
		known := state.countKnown
		state.mu.Unlock()

		// 2.5: never evict THIS partition on an untrusted count (a partition marked
		// unknown by an uncertain durability outcome pauses its own eviction, while
		// unrelated known partitions keep evicting — the store-level gate above is
		// belt-and-suspenders for a never-rebuilt store).
		if !known {
			return true
		}

		if count <= int64(lm.config.MaxEventsPerPartition) {
			return true
		}

		// Exceeds capacity - evict oldest events
		excess := int(count) - lm.config.MaxEventsPerPartition
		lm.evictOldest(pid, excess+10) // Evict a few extra to avoid churn

		return true
	})
}

// evictOldest marks the oldest events in a partition as tombstoned.
func (lm *LifecycleManager) evictOldest(pid int, count int) {
	windows, err := lm.store.ListSegments(pid)
	if err != nil {
		return
	}

	evicted := 0
	for _, windowTS := range windows {
		if evicted >= count {
			break
		}

		eventPrefix := SegmentEventPrefix(pid, windowTS)
		pairs, err := lm.store.kv.KVScan(eventPrefix, 0)
		if err != nil {
			continue
		}

		for _, pair := range pairs {
			if evicted >= count {
				break
			}
			// EventKey from the JSON value, not the KV key (same fix as
			// checkTTL — the KV key format carries no event key).
			var evt struct {
				EventKey int64  `json:"event_key"`
				Type     string `json:"event_type"`
			}
			if err := json.Unmarshal([]byte(pair.Value), &evt); err != nil || evt.EventKey == 0 {
				continue
			}
			if lm.tombstone.IsTombstone(evt.EventKey) {
				continue
			}
			// §2.8: never capacity-evict a still-retained unacked-recovery original.
			if lm.store.IsKeyProtected(evt.EventKey) {
				continue
			}
			// Curated artifacts are exempt from capacity eviction too (same rule
			// as TTL): raw events may be forgotten, artifacts persist — index
			// cards point at these keys.
			if evt.Type == event.TypeContextCompressSummary {
				continue
			}

			if err := lm.tombstone.MarkTombstone(evt.EventKey); err != nil {
				log.Errorf("[Lifecycle] evict MarkTombstone failed key=%d: %v", evt.EventKey, err)
				continue
			}
			// Decrement the logically-live counter alongside the tombstone —
			// otherwise every hourly cycle would evict another excess+10 LIVE
			// events (tombstones don't change the physical count until the next
			// compaction, which may never fire for quiet partitions).
			lm.store.decrementEventCount(pid, 1)
			evicted++
		}
	}
}

// getEffectiveTTL returns the effective TTL in days for a given event type.
// A NEGATIVE global TTL is the master switch: it disables TTL entirely and
// takes precedence over the per-type table. A NEGATIVE type-specific TTL
// exempts that type (curated artifacts — "raw events may be forgotten,
// artifacts persist"); the caller skips types whose effective TTL is <= 0.
func (lm *LifecycleManager) getEffectiveTTL(eventType string) (int, error) {
	if lm.config.GlobalTTLDays < 0 {
		return 0, nil // master off switch (B1): nothing expires by age
	}
	// Type-specific TTL takes precedence; negative = exempt.
	if ttl, ok := lm.config.TypeTTL[eventType]; ok {
		if ttl < 0 {
			return 0, nil
		}
		if ttl > 0 {
			return ttl, nil
		}
	}
	// Fall back to global TTL
	return lm.config.GlobalTTLDays, nil
}

// ==================== Compact integration ====================

// GetTombstoneFilterFunc returns a filter function for compaction that
// checks if an event key is tombstoned.
func (lm *LifecycleManager) GetTombstoneFilterFunc() func(int64) bool {
	return func(key int64) bool {
		return lm.tombstone.IsTombstone(key)
	}
}

// 契约: docs/wiki/memory/memory-architecture.md#ttl-authority
package memory

import (
	"encoding/json"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/event"
)

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
		MaxEventsPerPartition: 0,
		CheckInterval:         time.Hour,
		TypeTTL:               event.DefaultTypeTTL(),
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

// armGrace bounds how long the first destructive scan waits for the retention
// lease to be armed before proceeding (anti-starvation backstop for a durable store
// opened without a durable recovery owner). Normal wiring arms in milliseconds.
const armGrace = 60 * time.Second

func (lm *LifecycleManager) armGrace() time.Duration { return armGrace }

// scannerLoop runs periodically to check for expired events and capacity.
func (lm *LifecycleManager) scannerLoop() {
	defer lm.wg.Done()

	select {
	case <-lm.store.RetentionLease().Ready():
	case <-time.After(lm.armGrace()):
		if lm.store.RetentionLease() != nil {
			log.Warnf("[Lifecycle] retention lease not armed within grace — proceeding with forgetting (no durable recovery owner registered; §2.8 backstop)")
		}
	case <-lm.stopCh:
		return
	}

	select {
	case <-lm.store.RetentionLease().HoldClear():
	case <-lm.stopCh:
		return
	}

	lm.checkTTL()
	lm.checkCapacity()

	ticker := time.NewTicker(lm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
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

// forgettingPaused reports an active registration barrier without waiting
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
// . Same semantics, no second truth.
func (lm *LifecycleManager) SweepOnce() {
	lm.checkTTL()
	lm.checkCapacity()
}

// checkTTL scans events and marks expired ones as tombstoned.
func (lm *LifecycleManager) checkTTL() {
	now := time.Now().UnixMilli()

	lm.store.partitions.Range(func(key, value interface{}) bool {
		pid := key.(int)

		windows, err := lm.store.ListSegments(pid)
		if err != nil {
			return true
		}

		for _, windowTS := range windows {
			eventPrefix := SegmentEventPrefix(pid, windowTS)
			pairs, err := lm.store.kv.KVScan(eventPrefix, 0)
			if err != nil {
				log.Warnf("[Lifecycle] TTL scan failed pid=%d window=%d: %v (skipped this round, retried next sweep)", pid, windowTS, err)
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

				if lm.tombstone.IsTombstone(evt.EventKey) {
					continue
				}

				if lm.store.IsKeyProtected(evt.EventKey) {
					continue
				}

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

		if !known {
			return true
		}

		if count <= int64(lm.config.MaxEventsPerPartition) {
			return true
		}

		excess := int(count) - lm.config.MaxEventsPerPartition
		lm.evictOldest(pid, excess+10)

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
			if lm.store.IsKeyProtected(evt.EventKey) {
				continue
			}
			if evt.Type == event.TypeContextCompressSummary {
				continue
			}

			if err := lm.tombstone.MarkTombstone(evt.EventKey); err != nil {
				log.Errorf("[Lifecycle] evict MarkTombstone failed key=%d: %v", evt.EventKey, err)
				continue
			}
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
		return 0, nil
	}
	if ttl, ok := lm.config.TypeTTL[eventType]; ok {
		if ttl < 0 {
			return 0, nil
		}
		if ttl > 0 {
			return ttl, nil
		}
	}
	return lm.config.GlobalTTLDays, nil
}

// GetTombstoneFilterFunc returns a filter function for compaction that
// checks if an event key is tombstoned.
func (lm *LifecycleManager) GetTombstoneFilterFunc() func(int64) bool {
	return func(key int64) bool {
		return lm.tombstone.IsTombstone(key)
	}
}

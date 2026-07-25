package memory

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
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
		TypeTTL: map[string]int{
			event.TypeContextCompress: 3,  // Low-value: 3 days
			event.TypeThinkingPlan:    3,  // Low-value: 3 days
			event.TypeExternalInput:   30, // High-value: 30 days
			event.TypeAgentOutput:     14, // Standard: 14 days
			event.TypeActionCommand:   14, // Standard: 14 days
			// Curated artifacts (segment summaries) are LONG-TERM MEMORY:
			// raw events may be forgotten by TTL, artifacts persist. The index
			// cards in the rolling summary point at these keys — expiring them
			// would leave dangling tickets. Negative = exempt from TTL.
			event.TypeContextCompressSummary: -1,
		},
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
	if config.GlobalTTLDays <= 0 {
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

// scannerLoop runs periodically to check for expired events and capacity.
func (lm *LifecycleManager) scannerLoop() {
	defer lm.wg.Done()

	// Run initial check
	lm.checkTTL()
	lm.checkCapacity()

	ticker := time.NewTicker(lm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lm.checkTTL()
			lm.checkCapacity()
		case <-lm.stopCh:
			return
		}
	}
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
				// Parse event key from KV key
				eventPK, _ := ParseKey(pair.Key)
				if eventPK == nil || eventPK.EventKey == 0 {
					continue
				}

				// Check if already tombstoned
				if lm.tombstone.IsTombstone(eventPK.EventKey) {
					continue
				}

				// Determine TTL for this event type
				eventType := extractEventTypeFromJSON(pair.Value)

				ttlDays, err := lm.getEffectiveTTL(eventType)
				if err != nil || ttlDays <= 0 {
					continue
				}

				// Parse actual Timestamp from event JSON for accurate TTL calculation
				var evt struct {
					Timestamp int64 `json:"timestamp"`
				}
				if err := json.Unmarshal([]byte(pair.Value), &evt); err != nil {
					continue
				}

				ttlMs := int64(ttlDays) * 24 * 60 * 60 * 1000
				eventAge := now - evt.Timestamp

				if eventAge > ttlMs {
					if err := lm.tombstone.MarkTombstone(eventPK.EventKey); err != nil {
						log.Errorf("[Lifecycle] MarkTombstone failed key=%d: %v", eventPK.EventKey, err)
					}
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

	lm.store.partitions.Range(func(key, value interface{}) bool {
		pid := key.(int)
		state := value.(*PartitionState)

		state.mu.Lock()
		count := state.eventCount
		state.mu.Unlock()

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
			eventPK, _ := ParseKey(pair.Key)
			if eventPK == nil || eventPK.EventKey == 0 {
				continue
			}
			if lm.tombstone.IsTombstone(eventPK.EventKey) {
				continue
			}
			// Curated artifacts are exempt from capacity eviction too (same rule
			// as TTL): raw events may be forgotten, artifacts persist — index
			// cards point at these keys.
			if extractEventTypeFromJSON(pair.Value) == event.TypeContextCompressSummary {
				continue
			}

			if err := lm.tombstone.MarkTombstone(eventPK.EventKey); err != nil {
				log.Errorf("[Lifecycle] evict MarkTombstone failed key=%d: %v", eventPK.EventKey, err)
				continue
			}
			evicted++
		}
	}
}

// getEffectiveTTL returns the effective TTL in days for a given event type.
// A NEGATIVE type-specific TTL means the type is exempt from expiration
// (curated artifacts — "raw events may be forgotten, artifacts persist");
// the caller skips types whose effective TTL is <= 0.
func (lm *LifecycleManager) getEffectiveTTL(eventType string) (int, error) {
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

// ==================== Helper ====================

// extractEventTypeFromJSON does a minimal parse of JSON to extract event_type field.
// This avoids a full json.Unmarshal for the common case.
func extractEventTypeFromJSON(jsonStr string) string {
	// Simple string search for "event_type":"<value>"
	// This is faster than full unmarshal for scan operations
	const prefix = `"event_type":"`
	start := indexOf(jsonStr, prefix)
	if start < 0 {
		return ""
	}
	start += len(prefix)
	end := indexOfFrom(jsonStr, start, `"`)
	if end < 0 {
		return ""
	}
	return jsonStr[start:end]
}

// indexOf finds the first occurrence of substr in s.
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// indexOfFrom finds the first occurrence of substr in s starting from start.
func indexOfFrom(s string, start int, substr string) int {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ==================== Compact integration ====================

// GetTombstoneFilterFunc returns a filter function for compaction that
// checks if an event key is tombstoned.
func (lm *LifecycleManager) GetTombstoneFilterFunc() func(int64) bool {
	return func(key int64) bool {
		return lm.tombstone.IsTombstone(key)
	}
}

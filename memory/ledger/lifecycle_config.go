// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Declarative lifecycle policy.
//
// The lifecycle policy is injected declaratively through the agent's memory
// configuration: the age window, the per-type overrides, the scan cadence and
// the per-partition capacity bound. An unset field falls back to the default
// and an explicit value wins, so no composition root hardcodes a second copy of
// the default beside the config it was handed.
package ledger

import "time"

// LifecycleConfig is the declarative lifecycle policy: the age window every
// event type ages out under, the per-type overrides, the TTL scan cadence and
// the per-partition capacity bound. The fields are named for the memory config
// map they are injected from.
type LifecycleConfig struct {
	// GlobalTTLDays is the age window every event type ages out under, in days.
	// Zero means the default window; a negative value exempts every type from
	// TTL tombstoning.
	GlobalTTLDays int `json:"global_ttl_days" yaml:"global_ttl_days"`
	// TypeTTL overrides the age window per frozen event type, in days; a
	// negative value exempts that type from TTL tombstoning. A type absent from
	// the map ages out under the global window.
	TypeTTL map[string]int `json:"type_ttl" yaml:"type_ttl"`
	// CheckInterval is the cadence the periodic TTL scan runs at. Zero means
	// the default cadence.
	CheckInterval time.Duration `json:"check_interval" yaml:"check_interval"`
	// MaxEventsPerPartition bounds the event count of one partition; zero means
	// unbounded.
	MaxEventsPerPartition int `json:"max_events_per_partition" yaml:"max_events_per_partition"`
}

// deliverySettlementType is the frozen event type of the delivery settlement
// fact family (assembly/host_delivery.go, FACT-VOCABULARY 第十一族). It is
// spelled here as the frozen word it is, because this package must stay clear of
// the composition root and two spellings of one type would exempt nothing.
const deliverySettlementType = "delivery.settlement"

// deliverySettlementTTLDays is the registration of the settlement family in the
// lifecycle policy: a negative override is the forward-looking exemption from
// TTL tombstoning (a settlement fact stands as long as an obligation can be read
// back). No producer mounts a Lifecycle today — the assembly keeps the family
// exempt through its own unwritten window instead — so the registration is a
// declaration of the invariant rather than a live effect; the day a composition
// root mounts the Lifecycle, the exemption this entry states is what keeps a
// settlement fact readable. Registering it in the policy (and not only in
// Window below) keeps the invariant visible to any reader of the injected
// config.
const deliverySettlementTTLDays = -1

// DefaultLifecycleConfig returns the default lifecycle policy: the seven-day
// global window, the delivery settlement family's negative (exempt) override,
// the one-hour scan cadence and no capacity bound. It is the fallback every
// unset field resolves to — never a value a composition root hardcodes beside
// its config.
func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		GlobalTTLDays: 7,
		TypeTTL: map[string]int{
			deliverySettlementType: deliverySettlementTTLDays,
		},
		CheckInterval:         time.Hour,
		MaxEventsPerPartition: 0,
	}
}

// Normalized returns the policy with every unset field filled from the default
// and every explicit value kept as given, so the injected policy and the one
// the lifecycle runs under are the same object.
//
// - The settlement exemption is a property of the family, not a preference the injected map may drop: a config naming per-type windows of its own still carries the family's negative entry, and the map is copied before the entry is added.
func (c LifecycleConfig) Normalized() LifecycleConfig {
	out := c
	if out.GlobalTTLDays == 0 {
		out.GlobalTTLDays = DefaultLifecycleConfig().GlobalTTLDays
	}
	if out.TypeTTL == nil {
		out.TypeTTL = map[string]int{}
	}
	if _, declared := out.TypeTTL[deliverySettlementType]; !declared {
		merged := make(map[string]int, len(out.TypeTTL)+1)
		for eventType, days := range out.TypeTTL {
			merged[eventType] = days
		}
		merged[deliverySettlementType] = deliverySettlementTTLDays
		out.TypeTTL = merged
	}
	if out.CheckInterval == 0 {
		out.CheckInterval = DefaultLifecycleConfig().CheckInterval
	}
	return out
}

// Window returns the age window one frozen event type ages out under and
// whether that type is exempt from TTL tombstoning entirely: a negative
// override exempts the type, a negative global window exempts every type, and
// an absent override falls back to the global window.
//
// - The settlement family is exempt by construction: its facts are the read face of obligations that outlive any age window, so no injected override re-introduces a tombstone for it.
func (c LifecycleConfig) Window(eventType string) (time.Duration, bool) {
	cfg := c.Normalized()
	if eventType == deliverySettlementType {
		return 0, true
	}
	if override, overridden := cfg.TypeTTL[eventType]; overridden {
		if override < 0 {
			return 0, true
		}
		return time.Duration(override) * 24 * time.Hour, false
	}
	if cfg.GlobalTTLDays < 0 {
		return 0, true
	}
	return time.Duration(cfg.GlobalTTLDays) * 24 * time.Hour, false
}

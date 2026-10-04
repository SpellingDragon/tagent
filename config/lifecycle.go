// 本文件承载配置模型向 memory 生命周期参数的投影：字段语义与缺省在此一处定死。
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
package config

import (
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ResolveLifecycleConfig merges the optional YAML lifecycle declaration over
// the built-in defaults. Nil or partially-set fields keep defaults; a
// negative GlobalTTLDays disables TTL-based forgetting entirely.
func ResolveLifecycleConfig(c *LifecycleConfig) memory.LifecycleConfig {
	cfg := memory.DefaultLifecycleConfig()
	if c == nil {
		return cfg
	}
	if c.GlobalTTLDays != nil {
		cfg.GlobalTTLDays = *c.GlobalTTLDays
	}
	if len(c.TypeTTL) > 0 {
		if cfg.TypeTTL == nil {
			cfg.TypeTTL = make(map[string]int, len(c.TypeTTL))
		}
		for k, v := range c.TypeTTL {
			cfg.TypeTTL[k] = v
		}
	}
	if c.CheckInterval != "" {
		if d, err := time.ParseDuration(c.CheckInterval); err == nil && d > 0 {
			cfg.CheckInterval = d
		} else {
			log.Warnf("[tagent] invalid lifecycle check_interval %q, keeping default", c.CheckInterval)
		}
	}
	if c.MaxEventsPerPartition != nil {
		cfg.MaxEventsPerPartition = *c.MaxEventsPerPartition
	}
	return cfg
}

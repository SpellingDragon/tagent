// 本文件承载配置模型的深拷贝：热更候选事务需要整份快照，克隆即快照的实现。
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
package config

import (
	"encoding/json"
	"fmt"
)

// Clone returns a private deep copy of the configuration: a published generation
// and the rollback ring each own their snapshot, so no ring ever aliases the live
// map. It round-trips through JSON because Config is pure data with symmetric tags,
// so a future nested field is copied automatically. ConfigPath is excluded from the
// snapshot and re-attached by hand; an empty-but-non-nil slice or map with omitempty
// comes back nil.
func (c *Config) Clone() (*Config, error) {
	if c == nil {
		return nil, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("config clone: marshal: %w", err)
	}
	out := &Config{}
	if err := json.Unmarshal(b, out); err != nil {
		return nil, fmt.Errorf("config clone: unmarshal: %w", err)
	}
	out.ConfigPath = c.ConfigPath
	return out, nil
}

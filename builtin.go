// Package tagent provides the top-level composition root for tagent applications:
// it encapsulates agent instantiation and wires cross-boundary dependencies.
//
// - Tools are usable only when both registered and declared for the agent; this file holds the built-in plain tool factories.
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package tagent

import (
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/SpellingDragon/tagent/workspace"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// actionFactory creates an ActionTool (shell command executor via tmux).
// Uses the option pattern: WithActionWorkspace, WithActionRunAsUser, etc.
func actionFactory(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
	properties := map[string]interface{}{}
	if cfg.Properties != nil {
		properties = cfg.Properties
	}

	var opts []action.ActionToolOption

	if wd, ok := properties["workspace"].(string); ok && wd != "" {
		opts = append(opts, action.WithActionWorkspace(wd))
	} else if cfg.WorkingDir != "" {
		opts = append(opts, action.WithActionWorkspace(cfg.WorkingDir))
	}
	opts = append(opts, action.WithActionOutputDir(workspace.ToolOutputPath(cfg.WorkspaceRoot)))
	if ru, ok := properties["run_as_user"].(string); ok && ru != "" {
		opts = append(opts, action.WithActionRunAsUser(ru))
	}
	if rg, ok := properties["run_as_group"].(string); ok && rg != "" {
		opts = append(opts, action.WithActionRunAsGroup(rg))
	}

	if monRaw, ok := properties["monitor"]; ok && monRaw != nil {
		if monCfg := parseMonitorConfig(monRaw); monCfg != nil {
			opts = append(opts, action.WithActionMonitorConfig(*monCfg))
		}
	}

	t := action.NewActionTool(opts...)
	return t, nil
}

// parseMonitorConfig parses a monitor config from properties map.
// Supports duration strings (e.g., "10s", "30s") via time.ParseDuration.
// that fails to parse leaves its field unset. It returns nil when no field is set,
// so the caller keeps the action package's own defaults.
func parseMonitorConfig(raw any) *action.MonitorConfig {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	cfg := &action.MonitorConfig{}
	if v, ok := m["interval"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Interval = d
		}
	}
	if v, ok := m["stable_duration"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.StableDuration = d
		}
	}
	if v, ok := m["interactive_stable_duration"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.InteractiveStableDuration = d
		}
	}
	if v, ok := m["fake_dead_duration"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.FakeDeadDuration = d
		}
	}
	if v, ok := m["dense_interval"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.DenseInterval = d
		}
	}
	if v, ok := m["dense_duration"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.DenseDuration = d
		}
	}
	if v, ok := m["max_interval"].(string); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MaxInterval = d
		}
	}
	if v, ok := m["backoff_factor"].(float64); ok && v >= 1 {
		cfg.BackoffFactor = v
	}
	if cfg.Interval == 0 && cfg.StableDuration == 0 && cfg.FakeDeadDuration == 0 &&
		cfg.DenseInterval == 0 && cfg.DenseDuration == 0 && cfg.MaxInterval == 0 && cfg.BackoffFactor == 0 {
		return nil
	}
	return cfg
}

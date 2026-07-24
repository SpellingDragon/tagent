// Package tagent provides the top-level composition root for tagent applications.
//
// The root package encapsulates the agent instantiation process, assembling
// a TagentAgent with configured tools and wiring cross-boundary dependencies.
//
// Tool Registration:
//
// Built-in tools are registered via RegisterBuiltinTools() (see registry.go).
// External tools can be registered via RegisterPlainTool() and RegisterToolAgent().
// Only tools that are both registered AND configured for an agent can be used.
//
// This file contains factory functions for built-in plain tools.
package tagent

import (
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/tool/action"

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
	}
	if ru, ok := properties["run_as_user"].(string); ok && ru != "" {
		opts = append(opts, action.WithActionRunAsUser(ru))
	}
	if rg, ok := properties["run_as_group"].(string); ok && rg != "" {
		opts = append(opts, action.WithActionRunAsGroup(rg))
	}

	// Parse monitor config if provided
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
	// Adaptive poll schedule (optional).
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
	// If no fields set, return nil to use defaults
	if cfg.Interval == 0 && cfg.StableDuration == 0 && cfg.FakeDeadDuration == 0 &&
		cfg.DenseInterval == 0 && cfg.DenseDuration == 0 && cfg.MaxInterval == 0 && cfg.BackoffFactor == 0 {
		return nil
	}
	return cfg
}

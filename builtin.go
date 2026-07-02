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

	if wd, ok := properties["work_dir"].(string); ok && wd != "" {
		opts = append(opts, action.WithActionWorkspace(wd))
	}
	if ru, ok := properties["run_as_user"].(string); ok && ru != "" {
		opts = append(opts, action.WithActionRunAsUser(ru))
	}
	if rg, ok := properties["run_as_group"].(string); ok && rg != "" {
		opts = append(opts, action.WithActionRunAsGroup(rg))
	}

	t := action.NewActionTool(opts...)
	return t, nil
}

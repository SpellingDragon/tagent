// Package tagent — ToolRegistry wraps the global tool registration maps
// from agent/tool_agent.go and provides a unified interface for:
//   - Registering built-in tools (exec + knowledge/recall sub-tools)
//   - Querying factories by ID
//   - Validating that config-referenced tools are registered
package tagent

import (
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/tool/file"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/SpellingDragon/tagent/tool/recall"
	"github.com/SpellingDragon/tagent/tool/spec"
	"github.com/SpellingDragon/tagent/tool/task"
)

// ToolRegistry is a facade over the agent package's global tool registration maps.
// It provides a unified entry point for tool registration, lookup, and validation.
//
// The actual factory maps live in agent/tool_agent.go as package-level variables.
// ToolRegistry delegates to those maps so callers can register tools via either
// the ToolRegistry API or agent.RegisterPlainTool / agent.RegisterToolAgent directly.
type ToolRegistry struct{}

var globalRegistry = &ToolRegistry{}

// GetRegistry returns the global ToolRegistry singleton.
func GetRegistry() *ToolRegistry {
	return globalRegistry
}

// RegisterPlainTool registers a plain tool factory. Delegates to agent.RegisterPlainTool.
func (r *ToolRegistry) RegisterPlainTool(id string, factory agent.PlainToolFactory) {
	agent.RegisterPlainTool(id, factory)
}

// RegisterToolAgent registers a tool agent factory. Delegates to agent.RegisterToolAgent.
func (r *ToolRegistry) RegisterToolAgent(id string, factory agent.ToolAgentFactory) {
	agent.RegisterToolAgent(id, factory)
}

// GetPlainToolFactory returns the factory for the given plain tool ID.
func (r *ToolRegistry) GetPlainToolFactory(id string) (agent.PlainToolFactory, bool) {
	return agent.GetPlainToolFactory(id)
}

// GetToolAgentFactory returns the factory for the given tool agent ID.
func (r *ToolRegistry) GetToolAgentFactory(id string) (agent.ToolAgentFactory, bool) {
	return agent.GetToolAgentFactory(id)
}

// RegisterBuiltinTools registers all built-in tools into the ToolRegistry.
// Called once in tagent.New() before config validation.
// Uses sync.Once for idempotency — safe to call multiple times.
//
// Registered plain tools:
//   - exec: shell command executor (ActionTool via tmux)
//   - file sub-tools: read_file, save_file, list_file, search_file, search_content, read_multiple_files, replace_content
//   - knowledge sub-tools: skill_search, skill_load, mcp_discover, web_search, duckduckgo_search, memory_query
//   - recall sub-tools: recall_query, recall_get, recall_recent, recall_trace
//   - mcp_call: generic MCP execution gateway (mcp-discovery-execution-loop)
var registerOnce sync.Once

func RegisterBuiltinTools() error {
	registerOnce.Do(func() {
		// exec: shell command executor
		agent.RegisterPlainTool("exec", actionFactory)

		// file sub-tools (7 plain tools)
		file.RegisterTools()

		// knowledge sub-tools (6 plain tools)
		knowledge.RegisterSubTools()

		// recall sub-tools (4 plain tools)
		recall.RegisterSubTools()

		// task sub-tools: list_tasks, cancel_task,
		// relaunch_task, resume_task
		task.RegisterSubTools()

		// spec: typed spec/plan management (no shell; openspec backend)
		spec.RegisterTool()

		// mcp_call: generic MCP execution gateway — constant declaration,
		// resolves server/tool through the live MCP registry at call time
		toolmcp.RegisterTool()
	})
	return nil
}

// ValidateToolAccess checks that all config-referenced plain tools (kind: tool)
// are registered in the ToolRegistry. Returns an error on the first unregistered tool.
//
// Agent-kind tools (kind: agent) are not checked here — they reference other agents
// in the Config.Agents map, which is validated separately in Config.Validate().
func (r *ToolRegistry) ValidateToolAccess(cfg *Config) error {
	for name, ac := range cfg.Agents {
		for i, tr := range ac.Tools {
			if tr.Kind == ToolKindTool {
				if _, ok := agent.GetPlainToolFactory(tr.ID); !ok {
					return fmt.Errorf("agent %q tool[%d] %q is not registered (use RegisterPlainTool to register it)",
						name, i, tr.ID)
				}
			}
		}
	}
	return nil
}

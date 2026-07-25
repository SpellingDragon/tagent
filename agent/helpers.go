package agent

import (
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// MemStore returns the MemoryStore for direct access (e.g., by RecallTool).
func (ta *TagentAgent) MemStore() memory.MemoryStore {
	return ta.memStore
}

// Runner returns the underlying Runner from ContextManager.
func (ta *TagentAgent) Runner() runner.Runner {
	if ta.contextManager != nil {
		return ta.contextManager.runner
	}
	return nil
}

// SetToolParentProjection wires the agent's SessionProjection to all
// AgentToolWrapper instances in the tool list. This enables auto-inject
// of event_keys when LLM does not pass them.
// Must be called after NewTagentAgent (which creates the projection).
func (ta *TagentAgent) SetToolParentProjection() {
	if ta.projection == nil || ta.config == nil {
		return
	}
	for _, t := range ta.config.Tools {
		if wrapper, ok := t.(*AgentToolWrapper); ok {
			wrapper.SetParentProjection(ta.projection)
		}
	}
}

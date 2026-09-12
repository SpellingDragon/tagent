package agent

import (
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// MemStore returns the MemoryStore for direct access (e.g., by RecallTool).
func (ta *TagentAgent) MemStore() memory.MemoryStore {
	return ta.memStore
}

// Runner returns the underlying Runner from ContextManager.
func (ta *TagentAgent) Runner() runner.Runner {
	if ta.contextManager != nil {
		// R4（review 🟠5）：走读锁路径（currentRunner）——裸字段读与 SwapExecutor
		// 写构成 data race。
		return ta.contextManager.currentRunner()
	}
	return nil
}

// SessionSvc exposes the resident session service (R4 review 🔴1: the
// executorOnly rebuild shell reuses it so session records and the
// AppendEventHook→outputCh wiring stay on the resident instance).
func (ta *TagentAgent) SessionSvc() session.Service {
	if ta == nil {
		return nil
	}
	return ta.sessionSvc
}

// SetToolParentProjection wires the agent's compress.SessionProjection to all
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

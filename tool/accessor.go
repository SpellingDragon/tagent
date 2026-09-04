package tool

import (
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory"
)

// MemoryStoreAccessor defines the minimal interface for tools to access memory.
// This decouples tools from the concrete memory.MemoryStore implementation,
// following the "tool self-accesses memory" design principle.
//
// Note: memory.MemoryStore satisfies this interface without adaptation.
type MemoryStoreAccessor interface {
	// QueryEvents queries events matching the given options.
	// Returns lightweight EventReference list (not full details).
	QueryEvents(opts memory.QueryOptions) ([]memory.EventReference, error)

	// GetEvent retrieves a single full event by its EventKey (Snowflake int64).
	GetEvent(key int64) (*memory.FullEvent, error)
}

// ==================== Common Interfaces ====================

// ==================== Skill Repository Adapter ====================

// SkillRepository provides access to skill summaries and content.
// This abstracts the skill source from the concrete file system implementation.
type SkillRepository interface {
	Summaries() []skill.Summary
	Get(name string) (*skill.Skill, error)
}

// ==================== MCP Registry ====================

// MCPRegistry provides read access to the live MCP server registry
// (implemented by tool/mcp.Registry). Reads reflect the registry's CURRENT
// content at call time — runtime registration and config hot-sync are
// visible immediately, so tools holding an MCPRegistry must not snapshot
// its content at construction. Implementations are safe for concurrent use.
type MCPRegistry interface {
	// Get returns the toolset registered under name.
	Get(name string) (trpctool.ToolSet, bool)
	// List returns all registered toolsets, sorted by server name.
	List() []trpctool.ToolSet
	// Names returns all registered server names, sorted.
	Names() []string
}

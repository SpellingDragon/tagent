package tool

import (
	"trpc.group/trpc-go/trpc-agent-go/skill"

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

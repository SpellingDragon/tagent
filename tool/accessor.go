package tool

import (
	"strings"

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

	// GetEvent retrieves a single full event by its EventKey.
	GetEvent(key string) (*memory.FullEvent, error)
}

// ==================== Knowledge Types ====================

// KnowledgeResult represents a single piece of acquired knowledge.
// Used by sub-tools as return value type.
type KnowledgeResult struct {
	Type          string         `json:"type"`                     // "skill", "skill_content", "web", "mcp_tool", "historical_memory"
	Title         string         `json:"title"`                    // Human-readable title
	Content       string         `json:"content"`                  // Knowledge content
	Source        string         `json:"source,omitempty"`         // Source identifier
	ExecutionPlan *ExecutionPlan `json:"execution_plan,omitempty"` // Translated executable plan
}

// ExecutionPlan describes a physical execution plan that CommandTool can directly run.
// This is the core output of knowledge "translation": converting capability descriptions
// into concrete executable commands.
type ExecutionPlan struct {
	Function    string            `json:"function"`              // "exec", "tmux_exec", "mcp_call"
	Command     string            `json:"command,omitempty"`     // Command for exec/tmux_exec
	MCPTool     string            `json:"mcp_tool,omitempty"`    // MCP tool name for mcp_call
	MCPArgs     map[string]any    `json:"mcp_args,omitempty"`    // MCP tool arguments for mcp_call
	Env         map[string]string `json:"env,omitempty"`         // Environment variables
	Dir         string            `json:"dir,omitempty"`         // Working directory
	Timeout     int               `json:"timeout,omitempty"`     // Timeout in seconds
	Description string            `json:"description,omitempty"` // Human-readable description
}

// ==================== Recall Types ====================

// RecallQuery represents a recall request.
// Agent uses natural language to describe what historical information it needs.
type RecallQuery struct {
	Query    string `json:"query"`               // Natural language description of what to recall
	EventKey string `json:"event_key,omitempty"` // Get full details of a specific event by key
	Limit    int    `json:"limit,omitempty"`     // Maximum number of results
}

// RecallResponse represents the result of a recall operation.
type RecallResponse struct {
	Events  []RecallEvent `json:"events"`
	Message string        `json:"message"`
}

// RecallEvent represents a lightweight event reference from memory.
type RecallEvent struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
}

// RecallEventDetail represents a full event with all details.
type RecallEventDetail struct {
	Key       string `json:"key"`
	ParentKey string `json:"parent_key,omitempty"`
	Type      string `json:"type"`
	Summary   string `json:"summary"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// ==================== Skill Repository Adapter ====================

// SkillRepository provides access to skill summaries and content.
// This abstracts the skill source from the concrete file system implementation.
type SkillRepository interface {
	Summaries() []skill.Summary
	Get(name string) (*skill.Skill, error)
}

// ==================== Shared Utilities ====================

// extractKeywords extracts search keywords from a natural language query
// by removing stop words and short tokens.
func extractKeywords(query string) []string {
	var keywords []string
	for _, part := range strings.Fields(query) {
		if len(part) >= 2 && !stopWords[strings.ToLower(part)] {
			keywords = append(keywords, strings.ToLower(part))
		}
	}
	return keywords
}

// stopWords contains common Chinese and English stop words to filter out
// during keyword extraction.
var stopWords = map[string]bool{
	// Chinese stop words
	"我": true, "你": true, "他": true, "她": true, "它": true,
	"的": true, "了": true, "是": true, "在": true, "有": true,
	"和": true, "与": true, "或": true, "但": true, "而": true,
	"就": true, "也": true, "都": true, "要": true, "会": true,
	"让": true, "把": true, "被": true, "从": true, "到": true,
	"什么": true, "怎么": true, "如何": true,
	// English stop words
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"i": true, "you": true, "he": true, "she": true, "it": true,
	"we": true, "they": true, "me": true, "him": true, "her": true,
	"and": true, "or": true, "but": true, "not": true, "no": true,
	"of": true, "at": true, "by": true, "for": true, "with": true,
	"to": true, "from": true, "in": true, "on": true,
	"that": true, "this": true, "these": true, "those": true,
}

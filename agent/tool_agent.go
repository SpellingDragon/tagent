// Package agent provides tool agent registration for extensible agent composition.
//
// Tool agents are TagentAgent instances wrapped as CallableTool via agenttool.NewTool().
// This file provides the registration mechanism for built-in and custom tool agents.
//
// Registration flow:
//
//  1. Built-in factories are registered in tagent/builtin.go init()
//  2. Custom factories can be registered via RegisterToolAgent()
//  3. tagent.New() resolves ToolConfig entries by looking up registered factories
package agent

import (
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolAgentFactory creates a tool agent (TagentAgent) from the given config.
// The returned TagentAgent will be wrapped via agenttool.NewTool() by the caller
// to become a CallableTool for the parent agent.
//
// This is the extension point for adding custom tool agents:
//
//	agent.RegisterToolAgent("my-agent", func(cfg agent.ToolAgentFactoryConfig) (*agent.TagentAgent, error) {
//	    return agent.NewTagentAgent(&agent.TagentConfig{...})
//	})
type ToolAgentFactory func(cfg ToolAgentFactoryConfig) (*TagentAgent, error)

// ToolAgentFactoryConfig provides everything a factory needs to create a TagentAgent.
//
// Event context design: The top-level agent's context sent to LLM is a record stream
// composed of events (tracked by MemoryPlugin). Tool agents interact with the parent
// agent and need access to its memory to retrieve full context.
//
// When a tool agent is invoked by the parent agent's LLM, it receives the request text
// from the LLM. But to access structured context (causal chain, full event details),
// it needs the parent's MemStore and the triggering EventKey.
//
// Memory data isolation: Memory does NOT know about agents or FilterKey.
// Memory uses PartitionID as a pure storage partition key (integer, no agent semantics).
// The mapping from AgentName (framework concept) → PartitionID (storage concept) happens
// at the tagent layer (MemoryPlugin), transparent to Memory.
//
//	AgentName (framework)       MemoryPlugin              Memory (partition)
//	"tagent"               →   FNV-1a → 42         →   partition=42
//	"knowledge"            →   FNV-1a → 85         →   partition=85
//	"recall"               →   FNV-1a → 123        →   partition=123
//
// Key distinction: FilterKey is dynamic (contains UUID per invocation), AgentName is
// stable (framework-assigned per agent type), PartitionID is derived from AgentName
// via deterministic FNV-1a hash. No independent AgentID concept needed — we unify
// on the framework's existing AgentName.
//
// Runtime EventKey injection mechanism:
//
//  1. MemoryPlugin.OnEvent derives PartitionID from inv.AgentName via FNV-1a
//  2. MemoryPlugin generates Snowflake EventKey (int64, encoding PartitionID)
//     and writes it to StateDelta
//  3. Flow layer extracts EventKey from StateDelta before executing tool_call
//  4. Flow layer auto-injects EventKey into tool's JSON args (if declared in InputSchema)
//  5. Tool agent receives EventKey in Call() and queries MemStore for full context
//  6. Tool agent can extract PartitionID from EventKey (PartitionIDFromEventKey) for scoped queries
//
// Flow:
//
//	Parent Agent LLM → tool_calls: knowledge({request: "..."})
//	→ MemoryPlugin.OnEvent: AgentName → PartitionID, Snowflake EventKey → StateDelta["event_key"]
//	→ Flow injects EventKey → tool.Call(ctx, {"request": "...", "event_key": 9223372036854775807})
//	→ tool agent queries MemStore with EventKey for full context
//	→ tool agent extracts PartitionID for scoped event queries
//	→ tool agent's internal LLM makes informed decisions
//
// IMPORTANT: Tool agents' Declaration InputSchema MUST declare event_key as an optional
// parameter. The framework auto-injects the value; LLM does not need to provide it.
// This is critical because the LLM cannot perceive EventKey values (they are in StateDelta,
// not in the conversation messages).
//
// Example Declaration:
//
//	"event_key": {
//	    Type:        "integer",
//	    Description: "[auto-injected] Snowflake EventKey of the triggering event. Use this to retrieve full context from memory.",
//	}
type ToolAgentFactoryConfig struct {
	// ID is the tool agent identifier (e.g., "knowledge", "recall")
	ID string

	// Model is the LLM model for the tool agent (resolved from config)
	Model model.Model

	// SystemPrompt is the loaded system prompt (already resolved from PromptConfig)
	SystemPrompt string

	// Description is the tool description shown to the parent agent's LLM
	// (loaded from Description/DescriptionFile)
	Description string

	// SubTools are the pre-built sub-tools for this agent
	SubTools []trpctool.Tool

	// MemStore provides access to the parent agent's memory.
	// Tool agents use this to query the parent's event stream for full context,
	// rather than relying solely on the text passed by the LLM.
	//
	// Key usage: When a tool agent is invoked, it can:
	//  1. Query recent events to understand the conversation context
	//  2. Retrieve full event details by EventKey for structured data
	//  3. Follow the ParentKey causal chain to trace event lineage
	MemStore memory.MemoryStore

	// Agent parameters
	MaxToolIterations int
	MaxTokens         int
	Temperature       float64
}

var (
	toolAgentFactories   = map[string]ToolAgentFactory{}
	toolAgentFactoriesMu sync.RWMutex
)

// RegisterToolAgent registers a factory for creating tool agents by ID.
// This is the primary extension mechanism for adding custom tool agents.
//
// Must be called before tagent.New() (typically in init()).
// Panics if a factory with the same ID is already registered.
func RegisterToolAgent(id string, factory ToolAgentFactory) {
	toolAgentFactoriesMu.Lock()
	defer toolAgentFactoriesMu.Unlock()

	if _, exists := toolAgentFactories[id]; exists {
		panic(fmt.Sprintf("tool agent factory %q already registered", id))
	}
	toolAgentFactories[id] = factory
}

// GetToolAgentFactory returns the factory for the given ID.
// Returns false if no factory is registered for the ID.
func GetToolAgentFactory(id string) (ToolAgentFactory, bool) {
	toolAgentFactoriesMu.RLock()
	defer toolAgentFactoriesMu.RUnlock()

	f, ok := toolAgentFactories[id]
	return f, ok
}

// PlainToolFactory creates a plain tool (implements tool.CallableTool) from the given config.
// Plain tools have no internal React loop — they are pure execution tools.
//
// This is the extension point for adding custom plain tools:
//
//	agent.RegisterPlainTool("my-tool", func(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
//	    return mypackage.NewMyTool(cfg.Config)
//	})
type PlainToolFactory func(cfg PlainToolFactoryConfig) (trpctool.CallableTool, error)

// PlainToolFactoryConfig provides everything a factory needs to create a plain tool.
type PlainToolFactoryConfig struct {
	// ID is the tool identifier (e.g., "command")
	ID string

	// Description is the tool description (loaded from Description/DescriptionFile)
	Description string

	// Config holds tool-specific configuration from the ToolConfig.Config map
	Config map[string]any
}

var (
	plainToolFactories   = map[string]PlainToolFactory{}
	plainToolFactoriesMu sync.RWMutex
)

// RegisterPlainTool registers a factory for creating plain tools by ID.
// Must be called before tagent.New() (typically in init()).
// Panics if a factory with the same ID is already registered.
func RegisterPlainTool(id string, factory PlainToolFactory) {
	plainToolFactoriesMu.Lock()
	defer plainToolFactoriesMu.Unlock()

	if _, exists := plainToolFactories[id]; exists {
		panic(fmt.Sprintf("plain tool factory %q already registered", id))
	}
	plainToolFactories[id] = factory
}

// GetPlainToolFactory returns the factory for the given ID.
func GetPlainToolFactory(id string) (PlainToolFactory, bool) {
	plainToolFactoriesMu.RLock()
	defer plainToolFactoriesMu.RUnlock()

	f, ok := plainToolFactories[id]
	return f, ok
}

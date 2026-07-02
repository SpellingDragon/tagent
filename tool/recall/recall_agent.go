package recall

import (
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/model"
	tagenttool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
)

// PromptConfig describes how to load a system prompt (bootstrap style).
// Re-exported from prompt package for use by sub-packages.
type PromptConfig = prompt.CompositeConfig

// Config holds configuration for creating the Recall Agent.
//
// RecallAgent is a TagentAgent instance configured for intelligent memory recall.
// Unlike the simple RecallTool, RecallAgent uses an internal LLM React loop
// to understand user queries and synthesize memory into coherent responses.
//
// Architecture: RecallAgent → TagentAgent (agent.Agent) → agent.Tool (CallableTool)
type Config struct {
	Model model.Model // Required: LLM model for the internal React loop

	MemStore memory.MemoryStore // Required: agent's own MemoryStore (writes via MemoryPlugin, reads via sub-tools)

	// ReadPartitionIDs lists PartitionIDs this agent is allowed to read in addition
	// to its own namespace. Injected from ToolAgentFactoryConfig.ReadPartitionIDs.
	ReadPartitionIDs []int

	// Tools are the sub-tools available to this agent (e.g., recall_query, recall_get).
	// In the config-driven path, these are injected by buildAgent from the agent's
	// config tools list. If empty, buildRecallSubTools is called for backward compatibility.
	Tools []tagenttool.Tool

	PromptDir string // Optional: base directory for prompt files (default: "resources/prompts")

	// Prompt loading (bootstrap style)
	Prompt PromptConfig // Optional: overrides PromptDir + "recall_agent.md" if set

	// Tool description shown to the parent agent's LLM
	Description     string // Optional: inline description (overrides default)
	DescriptionFile string // Optional: description loaded from file (relative to PromptDir)

	// Optional overrides
	MaxToolIterations int // Default: 5
	MaxTokens         int // Default: 4096
}

// NewAgent creates a TagentAgent configured for intelligent memory recall.
// The returned *agent.TagentAgent implements agent.Agent and can be wrapped via
// agenttool.NewTool() to become a CallableTool for the main TagentAgent.
func NewAgent(cfg Config) (*agent.TagentAgent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("recall agent: model is required")
	}
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("recall agent: memStore is required")
	}

	// 1. Resolve prompt directory
	promptDir := cfg.PromptDir
	if promptDir == "" {
		promptDir = "resources/prompts"
	}
	loader := prompt.NewLoader(promptDir)

	// 2. Load system prompt
	var systemPrompt string
	var err error
	if !cfg.Prompt.IsEmpty() {
		// Use PromptConfig (bootstrap style) if configured
		systemPrompt, err = loader.LoadComposite(cfg.Prompt.Inline, cfg.Prompt.Files, cfg.Prompt.Dir)
		if err != nil {
			return nil, fmt.Errorf("recall agent: load prompt: %w", err)
		}
	} else {
		// Fallback: load single file
		systemPrompt, err = loader.LoadFromFile("recall_agent.md")
		if err != nil {
			// Fallback to embedded prompt if file not found
			systemPrompt = getDefaultRecallPrompt()
		}
	}

	// 3. Assemble sub-tools
	// Config-driven path: tools are injected by buildAgent.
	// Backward compat: if Tools is empty, build sub-tools internally.
	subTools := cfg.Tools
	if len(subTools) == 0 {
		subTools = buildRecallSubTools(cfg.MemStore, cfg.ReadPartitionIDs)
	}

	// 4. Apply defaults
	maxToolIter := cfg.MaxToolIterations
	if maxToolIter <= 0 {
		maxToolIter = 5
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	// 5. Create TagentAgent instance (inherits all tagent core mechanisms)
	agentCfg := &agent.TagentConfig{
		Name:              "recall",
		Description:       "Intelligent memory recall agent. Queries historical events and synthesizes memories into coherent responses.",
		Model:             cfg.Model,
		MemoryStore:       cfg.MemStore, // MUST be same store: MemoryPlugin writes here, sub-tools read here
		SystemPrompt:      systemPrompt,
		Tools:             subTools,
		MaxToolIterations: maxToolIter,
		MaxTokens:         maxTokens,
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("recall agent: create tagent agent: %w", err)
	}

	return ta, nil
}

// NewTool is a convenience function that creates a RecallAgent
// and wraps it as a CallableTool ready for registration.
//
// If cfg.Description is empty and cfg.DescriptionFile is set, the description
// is loaded from the file (relative to cfg.PromptDir).
// If both are empty, a hardcoded default is used for backward compatibility.
//
// Note: This wraps with a simple AgentToolWrapper without event_key resolution.
// For full event_key support, use tagent.New() which builds agents from Config.
func NewTool(cfg Config) (tagenttool.Tool, error) {
	recallAgent, err := NewAgent(cfg)
	if err != nil {
		return nil, err
	}

	// Resolve tool description
	desc, err := resolveDescription(cfg, "Intelligent memory recall tool. Queries historical events and synthesizes memories into coherent responses.")
	if err != nil {
		return nil, err
	}

	// Wrap as AgentToolWrapper (no event_key resolution in standalone mode)
	return agent.NewAgentToolWrapper(recallAgent, desc, nil, nil), nil
}

// resolveDescription resolves the tool description from inline text or file.
func resolveDescription(cfg Config, fallback string) (string, error) {
	if cfg.Description != "" {
		return cfg.Description, nil
	}
	if cfg.DescriptionFile != "" {
		promptDir := cfg.PromptDir
		if promptDir == "" {
			promptDir = "resources/prompts"
		}
		loader := prompt.NewLoader(promptDir)
		desc, err := loader.LoadFromFile(cfg.DescriptionFile)
		if err != nil {
			return "", fmt.Errorf("recall agent: load description file: %w", err)
		}
		return desc, nil
	}
	return fallback, nil
}

// getDefaultRecallPrompt returns a default prompt if the file-based prompt is not available.
func getDefaultRecallPrompt() string {
	return `You are an intelligent memory recall assistant.

Your role is to help users retrieve and synthesize relevant information from historical events.

## Your Tools

1. **memory_query** - Query historical events with time range filtering (since/until, Unix ms timestamps)
2. **memory_get** - Get full event details by key, optionally include parent event (include_parent=true)
3. **memory_recent** - Get the most recent events with optional time range filtering
4. **memory_trace** - Trace the causal chain backward from an event by following ParentKey links

## Response Format

When users ask about memories, format your response as:

## Related Memories (N items)

1. [timestamp] [event_type]
   Summary: [event_summary]
   Key: [event_key]
...

## Summary
[Provide a concise summary based on the memories]

## Guidelines

- If no relevant memories found, honestly tell the user
- Prioritize showing the most recent and relevant memories
- Keep responses concise but informative
- Ask follow-up questions if the user's query is unclear`
}

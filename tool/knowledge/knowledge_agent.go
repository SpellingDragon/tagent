package knowledge

import (
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/model"
	tagenttool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	tagentpkg "github.com/SpellingDragon/tagent/tool"
)

// PromptConfig describes how to load a system prompt (bootstrap style).
// Re-exported from tagent root package for use by sub-packages.
type PromptConfig = prompt.CompositeConfig

// Config holds configuration for creating the Knowledge Agent.
type Config struct {
	Model       model.Model               // Required: LLM model
	MemStore    memory.MemoryStore        // Optional: agent's own MemoryStore (if set, wired to MemoryPlugin + sub-tools)
	SkillRepo   tagentpkg.SkillRepository // Optional: skill source
	MCPToolSets []tagenttool.ToolSet      // Optional: MCP tool sources
	PromptDir   string                    // Optional: base directory for prompt files (default: "resources/prompts")
	// ReadPartitionIDs scopes partition-isolated queries (memory_query) to the
	// agent's readable partitions (own namespace first + read_namespaces).
	ReadPartitionIDs []int

	// Tools are the sub-tools available to this agent (e.g., skill_search, memory_query).
	// In the config-driven path, these are injected by buildAgent from the agent's
	// config tools list. If empty, BuildSubTools is called for backward compatibility.
	Tools []tagenttool.Tool

	// Prompt loading (bootstrap style)
	Prompt PromptConfig // Optional: overrides PromptDir + "knowledge_agent.md" if set

	// Tool description shown to the parent agent's LLM
	Description     string // Optional: inline description (overrides default)
	DescriptionFile string // Optional: description loaded from file (relative to PromptDir)

	// Optional overrides
	MaxToolIterations int     // Default: 5 (knowledge acquisition needs few iterations)
	MaxTokens         int     // Default: 4096
	Temperature       float64 // Default: 0.3 (precision over creativity)
}

// NewAgent creates a TagentAgent configured for knowledge acquisition & translation.
// The returned *agent.TagentAgent implements agent.Agent and can be wrapped via
// agenttool.NewTool() to become a CallableTool for the main TagentAgent.
func NewAgent(cfg Config) (*agent.TagentAgent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("knowledge agent: model is required")
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
			return nil, fmt.Errorf("knowledge agent: load prompt: %w", err)
		}
	} else {
		// Fallback: load single file
		systemPrompt, err = loader.LoadFromFile("knowledge_agent.md")
		if err != nil {
			return nil, fmt.Errorf("knowledge agent: load prompt: %w", err)
		}
	}

	// 3. Assemble sub-tools
	// Config-driven path: tools are injected by buildAgent.
	// Backward compat: if Tools is empty, build sub-tools internally.
	subTools := cfg.Tools
	if len(subTools) == 0 {
		subTools = BuildSubTools(cfg)
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
		Name:              "knowledge",
		Description:       "Knowledge acquisition and translation agent. Discovers skills, MCP tools, and web resources; translates them into executable plans.",
		Model:             cfg.Model,
		MemoryStore:       cfg.MemStore, // Wire store so MemoryPlugin and sub-tools share the same store
		SystemPrompt:      systemPrompt,
		Tools:             subTools,
		MaxToolIterations: maxToolIter,
		MaxTokens:         maxTokens,
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("knowledge agent: create tagent agent: %w", err)
	}

	return ta, nil
}

// NewTool is a convenience function that creates a KnowledgeAgent
// and wraps it as a CallableTool ready for registration.
//
// If cfg.Description is empty and cfg.DescriptionFile is set, the description
// is loaded from the file (relative to cfg.PromptDir).
// If both are empty, a hardcoded default is used for backward compatibility.
//
// Note: This wraps with a simple AgentToolWrapper without event_key resolution.
// For full event_key support, use tagent.New() which builds agents from Config.
func NewTool(cfg Config) (tagenttool.Tool, error) {
	knowledgeAgent, err := NewAgent(cfg)
	if err != nil {
		return nil, err
	}

	// Resolve tool description
	desc, err := resolveDescription(cfg, "Knowledge acquisition and translation tool. Acquires knowledge needed to complete tasks and translates it into executable plans.")
	if err != nil {
		return nil, err
	}

	// Wrap as AgentToolWrapper (no event_key resolution in standalone mode)
	return agent.NewAgentToolWrapper(knowledgeAgent, desc, nil, nil), nil
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
			return "", fmt.Errorf("knowledge agent: load description file: %w", err)
		}
		return desc, nil
	}
	return fallback, nil
}

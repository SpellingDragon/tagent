// Package tagent provides the top-level composition root for tagent applications.
//
// The root package encapsulates the agent instantiation process, assembling
// a TagentAgent with configured tools and wiring cross-boundary dependencies.
//
// Dependency direction (all one-way, no cycles):
//
//	tagent (root) → agent → plugin → memory
//	tagent (root) → tool/command → memory
//	tagent (root) → tool/recall → memory
//	tagent (root) → tool/knowledge → memory
//	tagent (root) → prompt
//
// Usage:
//
//	ta, err := tagent.New(tagent.DefaultConfig(),
//	    tagent.WithModel(modelInstance),
//	)
package tagent

import (
	"fmt"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool"
	"github.com/SpellingDragon/tagent/tool/command"

	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// Option injects runtime-only dependencies that cannot be serialized.
type Option func(*runtimeConfig)

// runtimeConfig holds runtime-only dependencies.
type runtimeConfig struct {
	model        model.Model
	memStore     memory.MemoryStore       // Full MemoryStore for tool agents to query event context
	memAccessor  tool.MemoryStoreAccessor // Narrow accessor for tool sub-packages
	skillRepo    tool.SkillRepository
	mcpToolSets  []trpctool.ToolSet
	summaryModel model.Model
}

// WithModel sets the resolved model instance (required).
func WithModel(m model.Model) Option {
	return func(rc *runtimeConfig) { rc.model = m }
}

// WithMemStore sets the shared memory store.
// The MemoryStore is passed to tool agents so they can query the parent
// agent's event stream for full context (causal chain, event details, etc.).
func WithMemStore(ms memory.MemoryStore) Option {
	return func(rc *runtimeConfig) {
		rc.memStore = ms
		rc.memAccessor = ms // MemoryStore satisfies MemoryStoreAccessor
	}
}

// WithSkillRepo sets the skill repository for knowledge agent.
func WithSkillRepo(sr tool.SkillRepository) Option {
	return func(rc *runtimeConfig) { rc.skillRepo = sr }
}

// WithMCPToolSets sets the MCP tool sources for knowledge agent.
func WithMCPToolSets(ts []trpctool.ToolSet) Option {
	return func(rc *runtimeConfig) { rc.mcpToolSets = ts }
}

// WithSummaryModel sets the model for Stage 2 LLM summary compression.
func WithSummaryModel(m model.Model) Option {
	return func(rc *runtimeConfig) { rc.summaryModel = m }
}

// New creates a fully-wired TagentAgent from declarative Config + runtime Options.
//
// Config is declarative and serializable (loadable from YAML/JSON via LoadConfig).
// Options inject runtime-only dependencies (model instances, memory stores, etc.).
//
// New handles all cross-boundary wiring internally:
//   - Resolves each ToolConfig by kind (agent vs tool) and id
//   - Loads prompts and descriptions from configured files
//   - Creates tool agents via registered factories (agent.GetToolAgentFactory)
//   - Creates plain tools via registered factories (agent.GetPlainToolFactory)
//   - Wires CommandTool's MessageInjector back to the agent
func New(cfg Config, opts ...Option) (*agent.TagentAgent, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	rc := &runtimeConfig{}
	for _, opt := range opts {
		opt(rc)
	}
	if rc.model == nil {
		return nil, fmt.Errorf("tagent: model is required (use WithModel)")
	}

	// Resolve main agent prompt
	loader := prompt.NewLoader(cfg.PromptDir)
	systemPrompt, err := loader.LoadComposite(cfg.SystemPrompt.Inline, cfg.SystemPrompt.Files, cfg.SystemPrompt.Dir)
	if err != nil {
		return nil, fmt.Errorf("tagent: load system prompt: %w", err)
	}

	// Build tools from declarative config
	var tools []trpctool.Tool
	var cmdTool *command.CommandTool

	for _, tc := range cfg.Tools {
		t, isCmd, err := buildTool(tc, rc, loader)
		if err != nil {
			return nil, fmt.Errorf("tagent: build tool %q: %w", tc.ID, err)
		}
		if isCmd {
			cmdTool = t.(*command.CommandTool)
		}
		tools = append(tools, t)
	}

	// Create main TagentAgent
	agentCfg := &agent.TagentConfig{
		Name:              cfg.Name,
		Model:             rc.model,
		SystemPrompt:      systemPrompt,
		Tools:             tools,
		MaxToolIterations: cfg.MaxToolIterations,
		MaxTokens:         cfg.MaxTokens,
		Temperature:       cfg.Temperature,
		CompressThreshold: cfg.CompressThreshold,
	}
	if rc.summaryModel != nil {
		agentCfg.SummaryModel = rc.summaryModel
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("tagent: create agent: %w", err)
	}

	// Wire CommandTool's tmux notifications back to the agent
	if cmdTool != nil {
		cmdTool.SetMessageInjector(ta)
	}

	return ta, nil
}

// buildTool creates a tool from a ToolConfig entry.
// Returns the tool, whether it's a CommandTool (for post-wiring), and any error.
func buildTool(tc ToolConfig, rc *runtimeConfig, loader *prompt.Loader) (trpctool.Tool, bool, error) {
	desc, err := resolveToolDescription(tc, loader)
	if err != nil {
		return nil, false, err
	}

	switch tc.Kind {
	case ToolKindAgent:
		return buildToolAgent(tc, rc, desc, loader)
	case ToolKindTool:
		return buildPlainTool(tc, desc)
	default:
		return nil, false, fmt.Errorf("unknown tool kind %q", tc.Kind)
	}
}

// buildToolAgent creates a tool agent via the factory registry.
func buildToolAgent(tc ToolConfig, rc *runtimeConfig, desc string, loader *prompt.Loader) (trpctool.Tool, bool, error) {
	toolModel := rc.model // Default to parent model

	systemPrompt, err := loader.LoadComposite(tc.Prompt.Inline, tc.Prompt.Files, tc.Prompt.Dir)
	if err != nil {
		return nil, false, fmt.Errorf("load prompt: %w", err)
	}

	factory, ok := agent.GetToolAgentFactory(tc.ID)
	if !ok {
		return nil, false, fmt.Errorf("no tool agent factory registered for id %q", tc.ID)
	}

	tagentAgent, err := factory(agent.ToolAgentFactoryConfig{
		ID:                tc.ID,
		Model:             toolModel,
		SystemPrompt:      systemPrompt,
		Description:       desc,
		MemStore:          rc.memStore,
		MaxToolIterations: tc.MaxToolIterations,
		MaxTokens:         tc.MaxTokens,
		Temperature:       tc.Temperature,
	})
	if err != nil {
		return nil, false, err
	}

	return wrapToolAgent(tagentAgent, desc), false, nil
}

// buildPlainTool creates a plain tool via the factory registry.
func buildPlainTool(tc ToolConfig, desc string) (trpctool.Tool, bool, error) {
	factory, ok := agent.GetPlainToolFactory(tc.ID)
	if !ok {
		return nil, false, fmt.Errorf("no plain tool factory registered for id %q", tc.ID)
	}

	callable, err := factory(agent.PlainToolFactoryConfig{
		ID:          tc.ID,
		Description: desc,
		Config:      tc.Config,
	})
	if err != nil {
		return nil, false, err
	}

	_, isCmd := callable.(*command.CommandTool)
	return callable, isCmd, nil
}

// resolveToolDescription resolves the tool description from inline text or file.
func resolveToolDescription(tc ToolConfig, loader *prompt.Loader) (string, error) {
	if tc.Description != "" {
		return tc.Description, nil
	}
	if tc.DescriptionFile != "" {
		desc, err := loader.LoadFromFile(tc.DescriptionFile)
		if err != nil {
			return "", fmt.Errorf("load description file %q: %w", tc.DescriptionFile, err)
		}
		return desc, nil
	}
	return "", fmt.Errorf("tool %q: description or description_file is required", tc.ID)
}

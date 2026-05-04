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
	"sync"

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
	model        model.Model // Default model (can be overridden per-agent)
	summaryModel model.Model // Optional: for Stage 2 LLM summary
	skillRepo    tool.SkillRepository
	mcpToolSets  []trpctool.ToolSet
}

// namedMemStores provides shared InMemoryStore instances by path.
// When two agents configure memory type: memory with the same path,
// they share the same store — so recall can read tagent's partition even in-memory.
// path empty = isolated store (default behavior).
var (
	namedMemMu     sync.Mutex
	namedMemStores = map[string]*memory.InMemoryStore{}
)

// WithModel sets the resolved model instance (required).
// This is the default model; individual agents can override via AgentConfig.Model.
func WithModel(m model.Model) Option {
	return func(rc *runtimeConfig) { rc.model = m }
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
// Options inject runtime-only dependencies (model instances, etc.).
//
// New handles all cross-boundary wiring internally:
//   - Resolves the entry agent from Config.Agents map
//   - Creates a MemoryStore per agent (isolated, from MemoryConfig)
//   - Builds tools by resolving ToolRef entries (agent refs → sub-agents)
//   - For agent-kind tools: creates the referenced agent and wraps it via AgentToolWrapper
//     which handles event_key → external context resolution
//   - For tool-kind tools: delegates to registered plain tool factories
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

	loader := prompt.NewLoader(cfg.PromptDir)

	// Pre-create all agents (topological order handled by agent refs)
	// We use a cache to avoid creating the same agent twice.
	agentCache := make(map[string]*agent.TagentAgent)

	// Build entry agent (the top-level agent returned by New)
	entryCfg := cfg.Agents[cfg.Entry]
	entryAgent, err := buildAgent(cfg.Entry, entryCfg, cfg, rc, loader, agentCache)
	if err != nil {
		return nil, fmt.Errorf("tagent: build entry agent %q: %w", cfg.Entry, err)
	}

	return entryAgent, nil
}

// buildAgent recursively creates a TagentAgent for the given agent name.
// It resolves tools by looking up referenced agents in the Config.Agents map.
func buildAgent(
	name string,
	acfg AgentConfig,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
) (*agent.TagentAgent, error) {
	// Check cache first
	if ta, ok := cache[name]; ok {
		return ta, nil
	}

	// 1. Create this agent's MemoryStore (isolated per-agent)
	memStore, err := resolveMemoryStore(acfg.Memory)
	if err != nil {
		return nil, fmt.Errorf("agent %q: create memory store: %w", name, err)
	}

	// 2. Resolve system prompt
	systemPrompt, err := loader.LoadComposite(
		acfg.SystemPrompt.Inline,
		acfg.SystemPrompt.Files,
		acfg.SystemPrompt.Dir,
	)
	if err != nil {
		return nil, fmt.Errorf("agent %q: load system prompt: %w", name, err)
	}

	// 3. Resolve model
	agentModel := rc.model // Default to parent model
	if acfg.Model != "" {
		agentModel = rc.model // TODO: support per-agent model resolution
	}

	// Resolve ReadNamespaces to PartitionIDs for cross-namespace read access
	var readPartitionIDs []int
	for _, ns := range acfg.Memory.ReadNamespaces {
		readPartitionIDs = append(readPartitionIDs, memory.PartitionIDFromName(ns))
	}

	// 3.5 Check for registered ToolAgentFactory — if the agent is well-known
	// (e.g., "knowledge", "recall"), delegate to its factory for proper sub-tool wiring
	// including skill repositories and MCP tool sets.
	if factory, ok := agent.GetToolAgentFactory(name); ok {
		factoryCfg := agent.ToolAgentFactoryConfig{
			ID:                name,
			Model:             agentModel,
			SystemPrompt:      systemPrompt,
			MemoryStore:       memStore,
			ReadPartitionIDs:  readPartitionIDs,
			MaxToolIterations: acfg.MaxToolIterations,
			MaxTokens:         acfg.MaxTokens,
			Temperature:       acfg.Temperature,
			SkillRepo:         rc.skillRepo,
			MCPToolSets:       rc.mcpToolSets,
		}

		ta, err := factory(factoryCfg)
		if err != nil {
			return nil, fmt.Errorf("agent %q: factory failed: %w", name, err)
		}

		cache[name] = ta
		return ta, nil
	}

	// 4. Build tools from ToolRef list
	var tools []trpctool.Tool
	var cmdTool *command.CommandTool

	for _, tr := range acfg.Tools {
		t, isCmd, err := buildToolFromRef(tr, cfg, rc, loader, cache, memStore)
		if err != nil {
			return nil, fmt.Errorf("agent %q: build tool %q: %w", name, tr.AgentID, err)
		}
		if isCmd {
			cmdTool = t.(*command.CommandTool)
		}
		tools = append(tools, t)
	}

	// 5. Create TagentAgent
	agentCfg := &agent.TagentConfig{
		Name:              name,
		Model:             agentModel,
		MemoryStore:       memStore,
		SystemPrompt:      systemPrompt,
		Tools:             tools,
		MaxToolIterations: acfg.MaxToolIterations,
		MaxTokens:         acfg.MaxTokens,
		Temperature:       acfg.Temperature,
		CompressThreshold: acfg.CompressThreshold,
	}
	if rc.summaryModel != nil {
		agentCfg.SummaryModel = rc.summaryModel
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("agent %q: create tagent agent: %w", name, err)
	}

	// Wire CommandTool's tmux notifications back to the agent
	if cmdTool != nil {
		cmdTool.SetMessageInjector(ta)
	}

	cache[name] = ta
	return ta, nil
}

// buildToolFromRef creates a tool from a ToolRef entry.
func buildToolFromRef(
	tr ToolRef,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	parentMemStore memory.MemoryStore,
) (trpctool.Tool, bool, error) {
	desc, err := resolveToolDescription(tr, loader)
	if err != nil {
		return nil, false, err
	}

	switch tr.Kind {
	case ToolKindAgent:
		return buildAgentToolRef(tr, cfg, rc, loader, cache, parentMemStore, desc)
	case ToolKindTool:
		return buildPlainToolRef(tr, desc)
	default:
		return nil, false, fmt.Errorf("unknown tool kind %q", tr.Kind)
	}
}

// buildAgentToolRef creates a tool agent and wraps it as a CallableTool.
// Unlike the previous agenttool.NewTool() approach, we use our own AgentToolWrapper
// which handles event_key → external context resolution.
func buildAgentToolRef(
	tr ToolRef,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	parentMemStore memory.MemoryStore,
	desc string,
) (trpctool.Tool, bool, error) {
	// Look up the referenced agent's config
	refCfg, ok := cfg.Agents[tr.AgentID]
	if !ok {
		return nil, false, fmt.Errorf("referenced agent %q not found in config", tr.AgentID)
	}

	// Recursively build the referenced agent
	subAgent, err := buildAgent(tr.AgentID, refCfg, cfg, rc, loader, cache)
	if err != nil {
		return nil, false, err
	}

	// Wrap with AgentToolWrapper — this replaces agenttool.NewTool().
	// The wrapper:
	//   - Declares event_key parameter in InputSchema (if tr.EventParams includes it)
	//   - On Call: resolves event_key → fetches full event from parentMemStore
	//   - Passes the event data as external context to the sub-agent
	wrapper := agent.NewAgentToolWrapper(subAgent, desc, tr.EventParams, parentMemStore)
	return wrapper, false, nil
}

// buildPlainToolRef creates a plain tool via the factory registry.
func buildPlainToolRef(tr ToolRef, desc string) (trpctool.Tool, bool, error) {
	factory, ok := agent.GetPlainToolFactory(tr.ID)
	if !ok {
		return nil, false, fmt.Errorf("no plain tool factory registered for id %q", tr.ID)
	}

	callable, err := factory(agent.PlainToolFactoryConfig{
		ID:          tr.ID,
		Description: desc,
		Properties:  tr.Properties,
	})
	if err != nil {
		return nil, false, err
	}

	_, isCmd := callable.(*command.CommandTool)
	return callable, isCmd, nil
}

// resolveMemoryStore creates a MemoryStore from MemoryConfig.
//
// For type: file, each call creates a new FileBackend instance.
// Multiple instances pointing to the same directory can safely read/write
// different partitions (different subdirectories) without shared locking.
//
// For type: memory, when a non-empty path is provided, the same path
// returns the same InMemoryStore instance (shared via registry).
// An empty path creates an isolated store — suitable for agents that
// don't need cross-agent memory access (e.g., knowledge agent).
func resolveMemoryStore(mc MemoryConfig) (memory.MemoryStore, error) {
	switch mc.Type {
	case "memory", "":
		if mc.Path == "" {
			// Isolated store — no sharing needed
			return memory.NewInMemoryStore(), nil
		}
		// Shared by path: same path → same InMemoryStore instance
		namedMemMu.Lock()
		defer namedMemMu.Unlock()
		if s, ok := namedMemStores[mc.Path]; ok {
			return s, nil
		}
		s := memory.NewInMemoryStore()
		namedMemStores[mc.Path] = s
		return s, nil
	case "file":
		if mc.Path == "" {
			return nil, fmt.Errorf("file memory store requires path")
		}
		return memory.NewFileBackend(mc.Path)
	default:
		return nil, fmt.Errorf("unknown memory store type %q", mc.Type)
	}
}

// resolveToolDescription resolves the tool description from inline text or file.
func resolveToolDescription(tr ToolRef, loader *prompt.Loader) (string, error) {
	if tr.Description != "" {
		return tr.Description, nil
	}
	if tr.DescriptionFile != "" {
		desc, err := loader.LoadFromFile(tr.DescriptionFile)
		if err != nil {
			return "", fmt.Errorf("load description file %q: %w", tr.DescriptionFile, err)
		}
		return desc, nil
	}
	return "", fmt.Errorf("tool %q: description or description_file is required", tr.AgentID)
}

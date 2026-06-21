// Package tagent provides the top-level composition root for tagent applications.
//
// The root package encapsulates the agent instantiation process, assembling
// a TagentAgent with configured tools and wiring cross-boundary dependencies.
//
// Dependency direction (all one-way, no cycles):
//
//	tagent (root) → agent → plugin → memory
//	tagent (root) → tool/action → memory
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
	"os"
	"path/filepath"
	"sync"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool"
	"github.com/SpellingDragon/tagent/tool/action"

	"trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
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

	// trajectoryRecorder is set when cfg.TrajectoryDump is true.
	// It wraps rc.model, and is registered as a Closer on the entry agent.
	trajectoryRecorder *agent.TrajectoryRecorder
}

// namedMemStores provides shared InMemoryStore instances by path.
// When two agents configure memory type: memory with the same path,
// they share the same store — so recall can read tagent's partition even in-memory.
// path empty = isolated store (default behavior).
var (
	namedMemMu     sync.Mutex
	namedMemStores = map[string]*memory.InMemoryStore{}

	// namedFileStores provides shared FileSegmentStore instances by path.
	// When two agents configure memory type: localfile with the same path,
	// they share the same FileSegmentStore — so recall can read tagent's partition.
	namedFileMu     sync.Mutex
	namedFileStores = map[string]*memory.FileSegmentStore{}
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

	// Wrap model with TrajectoryRecorder if enabled
	if cfg.TrajectoryDump {
		tr, err := agent.NewTrajectoryRecorder(rc.model, cfg.TrajectoryDir, cfg.APIEndpoint)
		if err != nil {
			return nil, fmt.Errorf("tagent: create trajectory recorder: %w", err)
		}
		rc.trajectoryRecorder = tr
		rc.model = tr
		// Also wrap summary model if present
		if rc.summaryModel != nil {
			// Summary model shares the same recorder (same JSONL files)
			rc.summaryModel = tr
		}
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

	// Register TrajectoryRecorder for graceful shutdown and session info
	if rc.trajectoryRecorder != nil {
		entryAgent.SetTrajectoryRecorder(rc.trajectoryRecorder)
		entryAgent.RegisterCloser(rc.trajectoryRecorder)
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

	// 3. Resolve model (currently uses parent model for all agents)
	agentModel := rc.model

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
	var actionTool *action.ActionTool

	for _, tr := range acfg.Tools {
		t, isAction, err := buildToolFromRef(tr, cfg, rc, loader, cache, memStore)
		if err != nil {
			return nil, fmt.Errorf("agent %q: build tool %q: %w", name, tr.AgentID, err)
		}
		if isAction {
			actionTool = t.(*action.ActionTool)
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

	// Wire ActionTool's tmux notifications back to the agent and register for cleanup
	if actionTool != nil {
		actionTool.SetMessageInjector(ta)
		ta.RegisterCloser(actionTool)
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
// When ToolRef.Remote is set, creates a remote A2AAgent instead of a local TagentAgent.
// Both paths produce an agent.Agent, which AgentToolWrapper wraps uniformly.
func buildAgentToolRef(
	tr ToolRef,
	cfg Config,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	parentMemStore memory.MemoryStore,
	desc string,
) (trpctool.Tool, bool, error) {
	// Remote path: create A2AAgent that communicates via trpc-a2a-go
	if tr.Remote != nil && tr.Remote.URL != "" {
		a2aAgent, err := a2aagent.New(
			a2aagent.WithName(tr.AgentID),
			a2aagent.WithDescription(desc),
			a2aagent.WithAgentCardURL(tr.Remote.URL),
			// TransferStateKey ensures RuntimeState["external_context"] is
			// auto-copied to A2A message metadata. The remote A2A server
			// auto-maps metadata back to RuntimeState (server.go:377).
			a2aagent.WithTransferStateKey(agent.ExternalContextKey),
		)
		if err != nil {
			return nil, false, fmt.Errorf("create remote A2A agent %q: %w", tr.AgentID, err)
		}
		wrapper := agent.NewAgentToolWrapper(a2aAgent, desc, tr.EventParams, parentMemStore)
		log.Infof("[tagent] created remote A2A agent tool: %s → %s", tr.AgentID, tr.Remote.URL)
		return wrapper, false, nil
	}

	// Local path: build the referenced agent recursively
	refCfg, ok := cfg.Agents[tr.AgentID]
	if !ok {
		return nil, false, fmt.Errorf("referenced agent %q not found in config", tr.AgentID)
	}

	subAgent, err := buildAgent(tr.AgentID, refCfg, cfg, rc, loader, cache)
	if err != nil {
		return nil, false, err
	}

	// Wrap with AgentToolWrapper — this replaces agenttool.NewTool().
	// The wrapper:
	//   - Declares event_key parameter in InputSchema (if tr.EventParams includes it)
	//   - On Call: resolves event_key → fetches full event from parentMemStore
	//   - Serializes events into RuntimeState["external_context"] and calls agent.Run
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

	_, isAction := callable.(*action.ActionTool)
	return callable, isAction, nil
}

// resolveMemoryStore creates a MemoryStore from MemoryConfig.
//
// For type: file, creates a FileSegmentStore backed by RustViking CLI
// and InMemRelationStore (WAL + snapshot persistence).
//
// For type: localfile, creates a FileSegmentStore backed by LocalFileKV
// (JSON file persistence, no external binary dependency) and InMemRelationStore.
// Same path → same FileSegmentStore instance (shared via namedFileStores registry).
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
		rel, err := memory.NewInMemRelationStore(mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create relation store: %w", err)
		}
		configPath, err := ensureRustVikingConfig(mc.RustVikingBinary, mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create rustviking config: %w", err)
		}
		kv := memory.NewRustVikingClient(mc.RustVikingBinary, configPath)
		store, err := memory.NewFileSegmentStore(kv, rel, mc.Path, 1000)
		if err != nil {
			return nil, fmt.Errorf("create file segment store: %w", err)
		}

		// Wire up lifecycle components: TombstoneSet → LifecycleManager → Compactor
		tombstone := memory.NewTombstoneSet(rel, kv, 0) // pid=0 for store-level tombstones
		if err := tombstone.RecoverFromKV(); err != nil {
			log.Warnf("[tagent] tombstone recovery failed (non-fatal): %v", err)
		}
		store.SetTombstoneSet(tombstone)

		lm := memory.NewLifecycleManager(store, tombstone, memory.DefaultLifecycleConfig())
		lm.Start()
		store.SetLifecycleManager(lm)

		compactor := memory.NewCompactor(store, kv, rel, tombstone, memory.DefaultCompactionConfig())
		compactor.Start()
		store.SetCompactor(compactor)

		return store, nil
	case "localfile":
		if mc.Path == "" {
			return nil, fmt.Errorf("localfile memory store requires path")
		}
		// Shared by path: same path → same FileSegmentStore instance
		// (so recall can read tagent's partition via read_namespaces)
		namedFileMu.Lock()
		defer namedFileMu.Unlock()
		if s, ok := namedFileStores[mc.Path]; ok {
			return s, nil
		}
		rel, err := memory.NewInMemRelationStore(mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create relation store: %w", err)
		}
		kv, err := memory.NewLocalFileKV(mc.Path)
		if err != nil {
			return nil, fmt.Errorf("create local file kv: %w", err)
		}
		store, err := memory.NewFileSegmentStore(kv, rel, mc.Path, 1000)
		if err != nil {
			return nil, fmt.Errorf("create file segment store: %w", err)
		}

		// Wire up lifecycle components: TombstoneSet → LifecycleManager → Compactor
		tombstone := memory.NewTombstoneSet(rel, kv, 0)
		if err := tombstone.RecoverFromKV(); err != nil {
			log.Warnf("[tagent] tombstone recovery failed (non-fatal): %v", err)
		}
		store.SetTombstoneSet(tombstone)

		lm := memory.NewLifecycleManager(store, tombstone, memory.DefaultLifecycleConfig())
		lm.Start()
		store.SetLifecycleManager(lm)

		compactor := memory.NewCompactor(store, kv, rel, tombstone, memory.DefaultCompactionConfig())
		compactor.Start()
		store.SetCompactor(compactor)

		namedFileStores[mc.Path] = store
		return store, nil
	default:
		return nil, fmt.Errorf("unknown memory store type %q", mc.Type)
	}
}

// ensureRustVikingConfig writes a rustviking config.toml to the data directory
// and returns the config file path. If the file already exists, it is reused.
func ensureRustVikingConfig(binary, dataDir string) (string, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dataDir, err)
	}
	configPath := filepath.Join(dataDir, "rustviking.toml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}

	// Write default config
	config := fmt.Sprintf(`[storage]
path = "%s"
create_if_missing = true
max_open_files = 10000

[vector_store]
plugin = "memory"

[embedding]
plugin = "mock"
`, filepath.Join(dataDir, "rocksdb"))
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return "", fmt.Errorf("write config %s: %w", configPath, err)
	}
	return configPath, nil
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

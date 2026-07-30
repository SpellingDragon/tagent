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
// Tool Registration:
//
// tagent uses a ToolRegistry to manage available tools. Built-in tools are
// registered via RegisterBuiltinTools(). External tools can be registered via
// RegisterPlainTool() and RegisterToolAgent(). Only tools that are both
// registered and configured for an agent can be used by that agent.
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
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/tool"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/SpellingDragon/tagent/tool/plan"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/provider"
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

	// resolvedModels caches model.Model instances keyed by "provider:model" string.
	// Agents sharing the same provider+model reuse the same instance.
	resolvedModels map[string]model.Model

	// modelOverrides injects pre-resolved model instances for specific agents.
	// This supports scenarios like SwappableModel for entry agent (AReaL proxy).
	modelOverrides map[string]model.Model

	// trajectoryRecorder is set when cfg.TrajectoryDump is true.
	// It wraps rc.model, and is registered as a Closer on the entry agent.
	trajectoryRecorder *rl.TrajectoryRecorder
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

// WithModelOverrides injects pre-resolved model instances for specific agents.
// This supports scenarios like SwappableModel for entry agent (AReaL proxy).
// The map key is the agent name, the value is the model instance to use.
func WithModelOverrides(overrides map[string]model.Model) Option {
	return func(rc *runtimeConfig) { rc.modelOverrides = overrides }
}

// New creates a fully-wired TagentAgent from declarative Config + runtime Options.
//
// Config is declarative and serializable (loadable from YAML/JSON via LoadConfig).
// Options inject runtime-only dependencies (model instances, etc.).
//
// New handles all cross-boundary wiring internally:
//   - Registers built-in tools (knowledge, recall, exec)
//   - Validates that all configured tools are registered
//   - Resolves the entry agent from Config.Agents map
//   - Creates a MemoryStore per agent (isolated, from MemoryConfig)
//   - Builds tools by resolving ToolRef entries (agent refs → sub-agents)
//   - For agent-kind tools: creates the referenced agent and wraps it via AgentToolWrapper
//     which handles event_key → external context resolution
//   - For tool-kind tools: delegates to registered plain tool factories
func New(cfg Config, opts ...Option) (*agent.TagentAgent, error) {
	// Register built-in tools
	if err := RegisterBuiltinTools(); err != nil {
		return nil, fmt.Errorf("tagent: register builtin tools: %w", err)
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Validate that all configured tools are registered
	registry := GetRegistry()
	if err := registry.ValidateToolAccess(&cfg); err != nil {
		return nil, fmt.Errorf("tagent: tool access validation: %w", err)
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
		tr, err := rl.NewTrajectoryRecorder(rc.model, cfg.TrajectoryDir, cfg.APIEndpoint)
		if err != nil {
			return nil, fmt.Errorf("tagent: create trajectory recorder: %w", err)
		}
		rc.trajectoryRecorder = tr
		rc.model = tr
		log.Infof("[tagent] TrajectoryRecorder wrapping model, dir=%s", cfg.TrajectoryDir)
		// Also wrap summary model if present
		if rc.summaryModel != nil {
			// Summary model shares the same recorder (same JSONL files)
			rc.summaryModel = tr
		}
	}

	// Loader reads prompts from cfg.PromptDir on disk, falling back to the
	// framework's embedded default prompts for anything not overridden there.
	loader := prompt.NewLoader(cfg.PromptDir, prompt.WithFallback(defaultPromptsFS, DefaultPromptsPrefix))

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

// builtinAgentNames are agent names that must always be built via the
// config-driven path. This protects knowledge/recall/action/speak/draw
// from being silently overridden by a registered ToolAgentFactory.
var builtinAgentNames = map[string]bool{
	"knowledge": true,
	"recall":    true,
	"action":    true,
	"speak":     true,
	"draw":      true,
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
	// Create hot-reloadable source for system prompt
	var systemPromptSource *prompt.Source
	if !acfg.SystemPrompt.IsEmpty() {
		systemPromptSource = prompt.NewSource(loader, acfg.SystemPrompt)
	}

	// 3. Resolve model — per-agent override supported
	agentModel := rc.resolveAgentModel(name, acfg, cfg)

	// Resolve ReadNamespaces to PartitionIDs for cross-namespace read access
	// (computed once, used by both factory path and config-driven path)
	var readPartitionIDs []int
	for _, ns := range acfg.Memory.ReadNamespaces {
		readPartitionIDs = append(readPartitionIDs, memory.PartitionIDFromName(ns))
	}

	// 3.5 Check for registered ToolAgentFactory — for custom agents only.
	// Built-in agent names (knowledge/recall/action/read/write/speak/draw) are
	// protected and always built via the config-driven path. This prevents a
	// registered factory from silently overriding the declared AgentConfig.Tools.
	registry := GetRegistry()
	if !builtinAgentNames[name] {
		if factory, ok := registry.GetToolAgentFactory(name); ok {
			factoryCfg := agent.ToolAgentFactoryConfig{
				ID:                   name,
				Model:                agentModel,
				SystemPrompt:         systemPrompt,
				MemoryStore:          memStore,
				ReadPartitionIDs:     readPartitionIDs,
				MaxToolIterations:    acfg.MaxToolIterations,
				MaxTokens:            acfg.MaxTokens,
				Temperature:          acfg.Temperature,
				SkillRepo:            rc.skillRepo,
				MCPToolSets:          rc.mcpToolSets,
				ThinkingEnabled:      acfg.ThinkingEnabled,
				ThinkingTokens:       acfg.ThinkingTokens,
				ReasoningEffort:      acfg.ReasoningEffort,
				ReasoningContentMode: acfg.ReasoningContentMode,
			}

			ta, err := factory(factoryCfg)
			if err != nil {
				return nil, fmt.Errorf("agent %q: factory failed: %w", name, err)
			}

			cache[name] = ta
			return ta, nil
		}
	}

	// 4. Build tools from ToolRef list
	var tools []trpctool.Tool
	var actionTool *action.ActionTool

	for _, tr := range acfg.Tools {
		t, isAction, err := buildToolFromRef(tr, cfg, acfg.WorkspaceRoot, rc, loader, cache, memStore, readPartitionIDs)
		if err != nil {
			return nil, fmt.Errorf("agent %q: build tool %q: %w", name, tr.AgentID, err)
		}
		// Agent-level task knobs flow to sub-agent wrappers at assembly time
		// (ToolRef stays a pure reference declaration).
		if w, ok := t.(*agent.AgentToolWrapper); ok && acfg.ResumeContextRounds > 0 {
			w.SetResumeContextRounds(acfg.ResumeContextRounds)
		}
		if isAction {
			actionTool = t.(*action.ActionTool)
		}
		tools = append(tools, t)
	}

	// 5. Create TagentAgent
	agentCfg := &agent.TagentConfig{
		Name:                 name,
		Model:                agentModel,
		MemoryStore:          memStore,
		SystemPrompt:         systemPrompt,
		SystemPromptSource:   systemPromptSource,
		Tools:                tools,
		MaxToolIterations:    acfg.MaxToolIterations,
		MaxTokens:            acfg.MaxTokens,
		Temperature:          acfg.Temperature,
		CompressThreshold:    acfg.CompressThreshold,
		KeepRecentTasks:      acfg.KeepRecentTasks,
		ThinkingEnabled:      acfg.ThinkingEnabled,
		ThinkingTokens:       acfg.ThinkingTokens,
		ReasoningEffort:      acfg.ReasoningEffort,
		ReasoningContentMode: acfg.ReasoningContentMode,
		Compress: agent.CompressConfig{
			MaxToolResultChars:   acfg.Compress.MaxToolResultChars,
			MaxExecStateChars:    acfg.Compress.MaxExecStateChars,
			ChunkSummaryLen:      acfg.Compress.ChunkSummaryLen,
			SkeletonSegmentation: acfg.Compress.SkeletonSegmentation,
			MaxNoticeChars:       acfg.Compress.MaxNoticeChars,
			CompactKeysListed:    acfg.Compress.CompactKeysListed,
			RecentFullCount:      acfg.Compress.RecentFullCount,
			CardMaxChars:         acfg.Compress.CardMaxChars,
			ArchiveCacheCap:      acfg.Compress.ArchiveCacheCap,
			MaxSummaryInputChars: acfg.Compress.MaxSummaryInputChars,
			SummaryMaxTokens:     acfg.Compress.SummaryMaxTokens,
		},
		TaskSettledMaxInline: acfg.TaskSettledMaxInline,
		WorkspaceRoot:        acfg.WorkspaceRoot,
	}
	// task_terminal_ttl: duration string → time.Duration; empty/invalid falls
	// back to the task package default (2m) via zero value.
	if acfg.TaskTerminalTTL != "" {
		if ttl, err := time.ParseDuration(acfg.TaskTerminalTTL); err == nil && ttl > 0 {
			agentCfg.TaskTerminalTTL = ttl
		} else {
			log.Warnf("[tagent] agent %q: invalid task_terminal_ttl %q, using default", name, acfg.TaskTerminalTTL)
		}
	}
	if summaryModel := rc.resolveSummaryModel(name, acfg, cfg); summaryModel != nil {
		agentCfg.SummaryModel = summaryModel
	}

	// Parse meditation config (string durations → time.Duration)
	if acfg.Meditation.Enabled {
		interval, _ := time.ParseDuration(acfg.Meditation.Interval)
		if interval <= 0 {
			interval = 30 * time.Minute
		}
		minGap, _ := time.ParseDuration(acfg.Meditation.MinGap)
		if minGap <= 0 {
			minGap = 2 * time.Hour
		}
		promptFile := acfg.Meditation.PromptFile
		if promptFile == "" {
			promptFile = "meditation.md"
		}
		promptText, err := loader.LoadFromFile(promptFile)
		if err != nil {
			return nil, fmt.Errorf("agent %q: load meditation prompt: %w", name, err)
		}
		// Create hot-reloadable source for meditation prompt
		meditationPromptSource := prompt.NewSource(loader, prompt.CompositeConfig{
			Files: []string{promptFile},
		})
		agentCfg.Meditation = agent.MeditationConfig{
			Enabled:      true,
			Interval:     interval,
			MinGap:       minGap,
			PromptText:   promptText,
			PromptSource: meditationPromptSource,
		}
	}

	ta, err := agent.NewTagentAgent(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("agent %q: create tagent agent: %w", name, err)
	}

	// Register ActionTool for cleanup on agent shutdown.
	if actionTool != nil {
		ta.RegisterCloser(actionTool)
	}

	// Register the memory store for graceful shutdown — file-backed stores
	// (FileSegmentStore over LocalFileKV/RustViking) perform their final
	// durability flush in Close. Same-path shared instances are safe: Close
	// is idempotent (closeOnce) and only invoked at process exit.
	if c, ok := memStore.(agent.Closer); ok {
		ta.RegisterCloser(c)
	}

	// Wire parentProjection to AgentToolWrapper instances for auto-inject fallback.
	// This must happen after TagentAgent creation (projection is created inside NewTagentAgent).
	ta.SetToolParentProjection()

	cache[name] = ta
	return ta, nil
}

// buildToolFromRef creates a tool from a ToolRef entry.
func buildToolFromRef(
	tr ToolRef,
	cfg Config,
	workspaceRoot string,
	rc *runtimeConfig,
	loader *prompt.Loader,
	cache map[string]*agent.TagentAgent,
	parentMemStore memory.MemoryStore,
	readPartitionIDs []int,
) (trpctool.Tool, bool, error) {
	desc, err := resolveToolDescription(tr, loader)
	if err != nil {
		return nil, false, err
	}

	switch tr.Kind {
	case ToolKindAgent:
		return buildAgentToolRef(tr, cfg, rc, loader, cache, parentMemStore, desc)
	case ToolKindTool:
		return buildPlainToolRef(tr, workspaceRoot, rc, parentMemStore, readPartitionIDs, desc)
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
		if tr.Async != nil && !*tr.Async {
			wrapper.SetAsyncDisabled(true)
		}
		if len(tr.ExtraParams) > 0 {
			wrapper.SetExtraParams(tr.ExtraParams)
		}
		// Enable hot-reload for tool description if loaded from a file
		if tr.DescriptionFile != "" {
			wrapper.SetDescriptionSource(prompt.NewSource(loader, prompt.CompositeConfig{
				Files: []string{tr.DescriptionFile},
			}))
		}
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

	// Wrap with PlanAgent if this is the plan agent — enables dual-mode Run
	// (progress queries bypass LLM via direct file I/O).
	var agentImpl trpcagent.Agent = subAgent
	if tr.AgentID == "plan" {
		agentImpl = plan.NewPlanAgent(subAgent, ".")
	}

	// Wrap with AgentToolWrapper — this replaces agenttool.NewTool().
	wrapper := agent.NewAgentToolWrapper(agentImpl, desc, tr.EventParams, parentMemStore)
	if tr.Async != nil && !*tr.Async {
		wrapper.SetAsyncDisabled(true)
	}
	if len(tr.ExtraParams) > 0 {
		wrapper.SetExtraParams(tr.ExtraParams)
	}
	// Enable hot-reload for tool description if loaded from a file
	if tr.DescriptionFile != "" {
		wrapper.SetDescriptionSource(prompt.NewSource(loader, prompt.CompositeConfig{
			Files: []string{tr.DescriptionFile},
		}))
	}
	return wrapper, false, nil
}

// buildPlainToolRef creates a plain tool via the factory registry.
// Runtime dependencies (rc, memStore, readPartitionIDs) are injected into
// PlainToolFactoryConfig so that sub-tools like skill_search, memory_query,
// recall_query etc. can access them during factory creation.
func buildPlainToolRef(
	tr ToolRef,
	workspaceRoot string,
	rc *runtimeConfig,
	memStore memory.MemoryStore,
	readPartitionIDs []int,
	desc string,
) (trpctool.Tool, bool, error) {
	registry := GetRegistry()
	factory, ok := registry.GetPlainToolFactory(tr.ID)
	if !ok {
		return nil, false, fmt.Errorf("no plain tool factory registered for id %q", tr.ID)
	}

	callable, err := factory(agent.PlainToolFactoryConfig{
		ID:               tr.ID,
		Description:      desc,
		Properties:       tr.Properties,
		WorkspaceRoot:    workspaceRoot,
		MemStore:         memStore,
		SkillRepo:        rc.skillRepo,
		MCPToolSets:      rc.mcpToolSets,
		ReadPartitionIDs: readPartitionIDs,
	})
	if err != nil {
		return nil, false, err
	}

	_, isAction := callable.(*action.ActionTool)
	return callable, isAction, nil
}

// resolveAgentModel resolves the model for a specific agent.
// Resolution order:
//  1. modelOverrides (pre-resolved instances, e.g., SwappableModel for entry agent)
//  2. If agent has no Model field → use parent model (rc.model)
//  3. Resolve via provider.Model() using the agent's provider+model from config
//  4. On error, fall back to parent model with a warning
func (rc *runtimeConfig) resolveAgentModel(name string, acfg AgentConfig, cfg Config) model.Model {
	// 1. Check overrides (SwappableModel for entry agent, etc.)
	if rc.modelOverrides != nil {
		if m, ok := rc.modelOverrides[name]; ok {
			return m
		}
	}

	// 2. If agent has no model override, use parent model
	if acfg.Model == "" {
		return rc.model
	}

	// 3. Resolve provider+model from config
	providerName := acfg.Provider
	if providerName == "" {
		providerName = cfg.Provider
	}
	cacheKey := providerName + ":" + acfg.Model
	if m, ok := rc.resolvedModels[cacheKey]; ok {
		return m
	}

	// 4. Look up provider connection info and determine protocol implementation
	var opts []provider.Option
	protocolName := providerName // default to registry key name
	if pcfg, ok := cfg.Providers[providerName]; ok {
		// If ProviderConfig specifies a protocol, use it (e.g., "zhipu" -> "openai")
		if pcfg.Provider != "" {
			protocolName = pcfg.Provider
		}
		if pcfg.APIEndpoint != "" {
			opts = append(opts, provider.WithBaseURL(pcfg.APIEndpoint))
		}
		if pcfg.APIKeyEnv != "" {
			if key := os.Getenv(pcfg.APIKeyEnv); key != "" {
				opts = append(opts, provider.WithAPIKey(key))
			}
		}
	}

	m, err := provider.Model(protocolName, acfg.Model, opts...)
	if err != nil {
		log.Warnf("agent %q: resolve model %q via provider %q (protocol %q) failed: %v, falling back to parent model",
			name, acfg.Model, providerName, protocolName, err)
		return rc.model
	}

	// Wrap with TrajectoryRecorder if enabled, so sub-agent LLM calls
	// are also recorded for RL training data.
	if rc.trajectoryRecorder != nil {
		m = rl.NewTrajectoryRecorderModelWrapper(m, rc.trajectoryRecorder)
		log.Debugf("[tagent] agent %q: wrapped model %q with TrajectoryRecorder", name, acfg.Model)
	}

	if rc.resolvedModels == nil {
		rc.resolvedModels = make(map[string]model.Model)
	}
	rc.resolvedModels[cacheKey] = m
	log.Infof("[tagent] agent %q: resolved model %q via provider %q", name, acfg.Model, providerName)
	return m
}

// resolveSummaryModel resolves the summary model for a specific agent.
// Resolution order:
//  1. If agent has SummaryModel field in YAML → resolve via provider (SummaryProvider or agent's Provider)
//  2. If rc.summaryModel is set via Go option → use that
//  3. Otherwise → nil (no summary model)
func (rc *runtimeConfig) resolveSummaryModel(name string, acfg AgentConfig, cfg Config) model.Model {
	// Resolve model and provider from compress config.
	// Falls back to agent's main model/provider if compress.summary_model is empty.
	summaryModel := acfg.Compress.SummaryModel
	summaryProvider := acfg.Compress.SummaryProvider

	// 1. If resolved summary_model, resolve it
	if summaryModel != "" {
		// Use summaryProvider if specified, otherwise fall back to agent's Provider or global Provider
		providerName := summaryProvider
		if providerName == "" {
			providerName = acfg.Provider
		}
		if providerName == "" {
			providerName = cfg.Provider
		}
		cacheKey := "summary:" + providerName + ":" + summaryModel
		if m, ok := rc.resolvedModels[cacheKey]; ok {
			return m
		}

		var opts []provider.Option
		protocolName := providerName // default to registry key name
		if pcfg, ok := cfg.Providers[providerName]; ok {
			// If ProviderConfig specifies a protocol, use it (e.g., "zhipu" -> "openai")
			if pcfg.Provider != "" {
				protocolName = pcfg.Provider
			}
			if pcfg.APIEndpoint != "" {
				opts = append(opts, provider.WithBaseURL(pcfg.APIEndpoint))
			}
			if pcfg.APIKeyEnv != "" {
				if key := os.Getenv(pcfg.APIKeyEnv); key != "" {
					opts = append(opts, provider.WithAPIKey(key))
				}
			}
		}

		m, err := provider.Model(protocolName, summaryModel, opts...)
		if err != nil {
			log.Warnf("agent %q: resolve summary model %q via provider %q (protocol %q) failed: %v, falling back to rc.summaryModel",
				name, summaryModel, providerName, protocolName, err)
			return rc.summaryModel
		}

		if rc.trajectoryRecorder != nil {
			m = rl.NewTrajectoryRecorderModelWrapper(m, rc.trajectoryRecorder)
			log.Debugf("[tagent] agent %q: wrapped summary model %q with TrajectoryRecorder", name, summaryModel)
		}

		if rc.resolvedModels == nil {
			rc.resolvedModels = make(map[string]model.Model)
		}
		rc.resolvedModels[cacheKey] = m
		log.Infof("[tagent] agent %q: resolved summary model %q via provider %q", name, summaryModel, providerName)
		return m
	}

	// 2. Fall back to Go option
	return rc.summaryModel
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
	// For tool-kind tools, description is optional — the tool's built-in
	// description from trpc-agent-go will be used if not provided.
	return "", nil
}

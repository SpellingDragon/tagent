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
//	tagent (root) → tool/mcp → tool (MCPRegistry interface)
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
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/tool"
	"github.com/SpellingDragon/tagent/tool/action"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
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

	// mcpRegistry is the process-level MCP server registry (config-declared
	// servers + WithMCPToolSets merged). Consumed by mcp_discover/mcp_call;
	// mutations never touch agent tool declarations.
	mcpRegistry *toolmcp.Registry

	// resolvedModels caches model.Model instances keyed by "provider:model" string.
	// Agents sharing the same provider+model reuse the same instance.
	resolvedModels map[string]model.Model

	// modelOverrides injects pre-resolved model instances for specific agents.
	// This supports scenarios like SwappableModel for entry agent (AReaL proxy).
	modelOverrides map[string]model.Model

	// trajectoryRecorder is set when cfg.TrajectoryDump is true.
	// It wraps rc.model, and is registered as a Closer on the entry agent.
	trajectoryRecorder *rl.TrajectoryRecorder

	// evolution (TC0/T-EVO)：热配置自进化运行时，cfg.Evolution.Enabled 时构造，跨 agent 共享。
	// evoStore 存不可变 bundle（内容寻址 + 原子 active 指针）；evoRelease 是风险分级发布状态机。
	evoStore   *evolution.BundleStore
	evoRelease *evolution.ReleaseManager

	// governance (T-G)：治理闸运行时，cfg.Governance.Enabled 时构造，跨 agent 共享。
	// govGate 对 entry agent 的 leaf 工具调用做风险分级 + 预算 + goal + critical 批准。
	govGate *governance.GovernanceGate
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

	// namedEngines 按 path 共享记忆引擎（与 namedMemStores/namedFileStores 同键），
	// 使共享 store 的引擎也共享——保跨 agent 语义召回一致（T-A）。空 path = 每 agent 独立引擎。
	namedEngineMu sync.Mutex
	namedEngines  = map[string]memory.MemoryEngine{}
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

// WithMCPToolSets injects pre-built MCP toolsets. They are merged into the
// process-level MCP registry under their Name() (alongside YAML-declared
// mcp_servers), becoming visible to mcp_discover/mcp_call immediately.
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

	// Build the process-level MCP registry: config-declared servers plus
	// WithMCPToolSets merged in, one live source for mcp_discover/mcp_call.
	// Registry mutations (runtime add/remove, config hot-sync) never touch
	// any agent's tool declarations, so the prompt prefix stays cache-stable.
	rc.mcpRegistry = toolmcp.NewRegistry(toolmcp.WithConfigPath(cfg.ConfigPath))
	rc.mcpRegistry.Seed(cfg.MCPServers)
	for _, ts := range rc.mcpToolSets {
		if ts != nil {
			rc.mcpRegistry.Add(ts.Name(), ts)
		}
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

	// T-EVO/TC0: 热配置自进化运行时（配置门控，默认关闭 → 现状零行为变化）。
	// 构造共享 BundleStore + 风险分级发布状态机；entry agent 的系统提示词经 VersionedSource
	// 从 active bundle 读，refine 工具让 agent 提案（经发布道裁决，无直接激活权）。
	if cfg.Evolution.Enabled {
		dir := cfg.Evolution.Dir
		if dir == "" {
			dir = "data/evolution"
		}
		store, eerr := evolution.NewBundleStore(dir)
		if eerr != nil {
			return nil, fmt.Errorf("tagent: init evolution bundle store: %w", eerr)
		}
		rm, eerr := evolution.NewReleaseManager(evolution.ReleaseDeps{
			Store:  store,
			Router: evolution.NewDiffRiskRouter(), // 按 diff 路由：模型/参数→慢道，仅提示词→快道
			// Gate 四槽留 nil（runGate：nil→通过；replay/shadow cassette 门待交付，见 review S2）。
			// Evaluator(LLM-judge)/Guardrail(指标闸) 于 buildAgent 阶段经 BindPosterior 绑定
			// （需 entry agent 的 model + memStore，故延迟到 New 之后——见下方 buildAgent 接线）。
			Config: evolution.ReleaseConfig{
				SkipApprovalGate: cfg.Evolution.SkipApproval, // 反义:config skip_approval 默认false→需批准门(保守)
				ProtectedPrompts: cfg.Evolution.ProtectedPrompts,
				CanaryHoldMs:     int64(cfg.Evolution.CanaryHoldSeconds) * 1000,
			},
		})
		if eerr != nil {
			return nil, fmt.Errorf("tagent: init evolution release manager: %w", eerr)
		}
		rc.evoStore = store
		rc.evoRelease = rm
		log.Infof("[tagent] evolution enabled: bundle store at %s (hot-config self-evolution)", dir)
	}

	// T-G: 治理闸运行时（配置门控，默认关闭 → 全放行，现状零行为变化）。构造 Budget/Approval/
	// Goal/Ledger + GovernanceGate；entry agent 的 leaf 工具经 GovernanceTool 装饰器过闸。
	if cfg.Governance.Enabled {
		rc.govGate = governance.NewGovernanceGate(governance.GateDeps{
			Budget: governance.NewBudgetManager(governance.BudgetConfig{
				Window:        time.Duration(cfg.Governance.BudgetWindowMinutes) * time.Minute,
				MaxHighRisk:   cfg.Governance.MaxHighRisk,
				MaxMediumRisk: cfg.Governance.MaxMediumRisk,
			}, cfg.Governance.Dir),
			Approval: governance.NewApprovalManager(cfg.Governance.Dir, 0),
			Goals:    governance.NewGoalRegistry(),
			Ledger:   governance.NewDenialLedger(nil, 0), // 内存账本；治理事件持久化待接 entry memStore
			Config: governance.GateConfig{
				Enabled:         true,
				Enforcement:     governance.Enforcement(cfg.Governance.Enforcement),
				GoalRequiredFor: cfg.Governance.GoalRequiredFor,
			},
		})
		log.Infof("[tagent] governance enabled: enforcement=%s (bounded autonomy gate)", cfg.Governance.Enforcement)
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

	// Register the MCP registry for graceful shutdown — closes all MCP
	// toolset connections once at process exit (registry is process-level,
	// shared across agents).
	if rc.mcpRegistry != nil {
		entryAgent.RegisterCloser(rc.mcpRegistry)
	}

	return entryAgent, nil
}

// builtinAgentNames are agent names that must always be built via the
// config-driven path. This protects knowledge/recall/action
// from being silently overridden by a registered ToolAgentFactory.
var builtinAgentNames = map[string]bool{
	"knowledge": true,
	"recall":    true,
	"action":    true,
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
	// 1.5 按配置包裹记忆引擎（T-A 解耦缝）：未配置则原样返回（行为逐字节不变）。
	memStore, err = wireMemoryEngine(memStore, acfg.Memory)
	if err != nil {
		return nil, fmt.Errorf("agent %q: wire memory engine: %w", name, err)
	}
	// 构建失败回收（审查 Nit5）：隔离 store（无共享）时，若后续步骤失败则关闭已启动的
	// 引擎（worker/重建 goroutine），防泄漏。共享 store 的引擎按 path 复用，不在此关闭。
	buildOK := false
	defer func() {
		if !buildOK && acfg.Memory.Path == "" {
			if c, ok := memStore.(agent.Closer); ok {
				_ = c.Close()
			}
		}
	}()

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
	var systemPromptSource prompt.Getter
	if !acfg.SystemPrompt.IsEmpty() {
		systemPromptSource = prompt.NewSource(loader, acfg.SystemPrompt)
	}
	// T-EVO/TC0: entry agent 系统提示词经 VersionedSource 从 active bundle 读（回合边界生效，
	// 热配置）。首次无 active bundle 时用静态提示词初始化基线（幂等）。配置门控（rc.evoStore
	// != nil 且 name == cfg.Entry）；非 entry agent 或未启用则原样（现状逐字节不变）。
	if rc.evoStore != nil && name == cfg.Entry {
		if rc.evoStore.Active() == nil && systemPrompt != "" {
			if _, ierr := rc.evoStore.InitBaseline(
				map[string]string{"system": systemPrompt}, evolution.BundleParams{}, evolution.ModelRef{},
			); ierr != nil {
				log.Warnf("[tagent] evolution init baseline failed: %v", ierr)
			}
		}
		if rc.evoStore.Active() != nil {
			systemPromptSource = evolution.NewVersionedSource(
				evolution.NewBundleProvider(rc.evoStore), "system", systemPromptSource,
			)
		}
		// T-EVO 后验评估闭环：绑定 LLM-judge（模型决策回滚）+ MetricGuardrail（确定性指标闸）
		// 到发布状态机。EvidenceSource 从 entry memStore 收集 canary 证据（治理拒绝率/critical
		// 率/事件量），故延迟到 memStore 就绪后绑定。judge 复用主 model。
		if rc.evoRelease != nil {
			evSrc := evolution.NewStoreEvidenceSource(memStore, memory.PartitionIDFromName(name), 0)
			rc.evoRelease.BindPosterior(
				evolution.NewLLMJudgeEvaluator(rc.model, evSrc, 5, 0.5, 0),
				evolution.NewMetricGuardrail(evSrc, evolution.GuardrailConfig{}),
			)
		}
	}

	// 3. Resolve model — per-agent override supported
	agentModel := rc.resolveAgentModel(name, acfg, cfg)

	// Resolve read partitions for recall/query tools (computed once, used by
	// both factory path and config-driven path):
	//   1. The agent's OWN namespace always comes first — events are written to
	//      PartitionIDFromName(agentName), so an agent must be able to query its
	//      own timeline (e.g. timeline recall via since/until) without any
	//      extra config. Without this, query-mode recall on FileSegmentStore
	//      silently returns 0 events (resolvePartitions treats "no partitions"
	//      as "scan nothing", per event-segment-store isolation).
	//   2. read_namespaces adds CROSS-namespace read access on top (deduped).
	ownPartition := memory.PartitionIDFromName(name)
	readPartitionIDs := []int{ownPartition}
	for _, ns := range acfg.Memory.ReadNamespaces {
		pid := memory.PartitionIDFromName(ns)
		if pid != ownPartition {
			readPartitionIDs = append(readPartitionIDs, pid)
		}
	}

	// 3.5 Check for registered ToolAgentFactory — for custom agents only.
	// Built-in agent names (knowledge/recall/action) are
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
			// Nil-guard: keep the interface field nil when no registry exists
			// (a typed nil *Registry would read as non-nil).
			if rc.mcpRegistry != nil {
				factoryCfg.MCPRegistry = rc.mcpRegistry
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

	// T-EVO: refine 工具（agent 自我修改通道 propose/diff/status/rollback，无 activate）——
	// 仅 entry agent 且 evolution 启用时注册。**先于治理包裹追加**（A3：refine 是最高权限通道，
	// rollback 直接切换 active bundle 绕过发布道评估，必须过治理闸；DefaultRules 有 refine 规则）。
	if rc.evoStore != nil && name == cfg.Entry {
		tools = append(tools, evolution.NewRefineTool(rc.evoStore, rc.evoRelease))
	}

	// T-G: 治理闸包裹 leaf 工具（配置门控，默认关闭则不包裹 → 现状逐字节不变）。跳过 sub-agent
	// 包装器（*agent.AgentToolWrapper）——下游需按具体类型断言接 parentProjection；治理聚焦
	// exec/file/mcp/refine 等 leaf 工具（主风险面）。actionTool 原始引用已在循环内提取，包裹
	// tools[] 不影响其 RegisterCloser；agent.go 随后包 OutputLimitTool，链式委托 GovernanceTool.Call。
	if rc.govGate != nil && rc.govGate.Enabled() && name == cfg.Entry {
		// 治理账本绑定 entry agent 的持久 memStore：治理记录写 governance 事件（可 recall
		// 审计、跨重启重建）。分区 = agent 自身写分区（PartitionIDFromName）。
		rc.govGate.BindLedger(memStore, memory.PartitionIDFromName(name))
		for i, t := range tools {
			if _, isWrapper := t.(*agent.AgentToolWrapper); isWrapper {
				continue
			}
			tools[i] = governance.NewGovernanceTool(t, rc.govGate)
		}
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
			CompactKeysListed: acfg.Compress.CompactKeysListed,
			RecentFullCount:   acfg.Compress.RecentFullCount,
			CardMaxChars:      acfg.Compress.CardMaxChars,
			SummaryMaxTokens:  acfg.Compress.SummaryMaxTokens,
		},
		WorkspaceRoot: acfg.WorkspaceRoot,
	}
	// T-G ReliableBus：per-agent 溢出子目录（<BusSpillDir>/<agentName> 隔离，防多 agent 事件串）。
	// 全局 BusSpillDir 空则保持空（NewReliableEventBus 回退纯 channel bus，现状零变化）。
	if cfg.Reliability.BusSpillDir != "" {
		agentCfg.BusSpillDir = filepath.Join(cfg.Reliability.BusSpillDir, name)
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
		// T-G AnchorStore：per-agent 冥想锚点持久化路径（<MeditationAnchorDir>/<name>.json）。
		// MeditationAnchorDir 空则 AnchorPath 空（冥想锚点纯内存，现状零变化）。
		meditationAnchorPath := ""
		if cfg.Reliability.MeditationAnchorDir != "" {
			meditationAnchorPath = filepath.Join(cfg.Reliability.MeditationAnchorDir, name+".json")
		}
		agentCfg.Meditation = agent.MeditationConfig{
			Enabled:      true,
			Interval:     interval,
			MinGap:       minGap,
			PromptText:   promptText,
			PromptSource: meditationPromptSource,
			AnchorPath:   meditationAnchorPath,
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
	buildOK = true // 构建成功：取消失败回收 defer（审查 Nit5）
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

	factoryCfg := agent.PlainToolFactoryConfig{
		ID:               tr.ID,
		Description:      desc,
		Properties:       tr.Properties,
		WorkspaceRoot:    workspaceRoot,
		MemStore:         memStore,
		SkillRepo:        rc.skillRepo,
		MCPToolSets:      rc.mcpToolSets,
		ReadPartitionIDs: readPartitionIDs,
	}
	// Nil-guard: assigning a typed nil *Registry to the interface field
	// would make cfg.MCPRegistry != nil inside factories.
	if rc.mcpRegistry != nil {
		factoryCfg.MCPRegistry = rc.mcpRegistry
	}
	callable, err := factory(factoryCfg)
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

// resolveLifecycleConfig merges the optional YAML lifecycle declaration over
// the built-in defaults. Nil or partially-set fields keep defaults; a
// negative GlobalTTLDays disables TTL-based forgetting entirely.
func resolveLifecycleConfig(c *LifecycleConfig) memory.LifecycleConfig {
	cfg := memory.DefaultLifecycleConfig()
	if c == nil {
		return cfg
	}
	if c.GlobalTTLDays != nil {
		cfg.GlobalTTLDays = *c.GlobalTTLDays
	}
	if len(c.TypeTTL) > 0 {
		if cfg.TypeTTL == nil {
			cfg.TypeTTL = make(map[string]int, len(c.TypeTTL))
		}
		for k, v := range c.TypeTTL {
			cfg.TypeTTL[k] = v
		}
	}
	if c.CheckInterval != "" {
		if d, err := time.ParseDuration(c.CheckInterval); err == nil && d > 0 {
			cfg.CheckInterval = d
		} else {
			log.Warnf("[tagent] invalid lifecycle check_interval %q, keeping default", c.CheckInterval)
		}
	}
	if c.MaxEventsPerPartition != nil {
		cfg.MaxEventsPerPartition = *c.MaxEventsPerPartition
	}
	return cfg
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

		lm := memory.NewLifecycleManager(store, tombstone, resolveLifecycleConfig(mc.Lifecycle))
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

		lm := memory.NewLifecycleManager(store, tombstone, resolveLifecycleConfig(mc.Lifecycle))
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

// wireMemoryEngine 按 MemoryConfig.Engine 为 store 包裹记忆引擎（T-A 解耦缝）。
// 未配置 Engine 或无 Embedding → 返回原 store（纯关键词，行为逐字节不变）。
// 共享 store（path 非空）的引擎按 path 共享（namedEngines），保跨 agent 语义召回一致。
// 嵌入器初始化失败（如无 API key）→ 优雅降级：记录并返回原 store（不阻断 agent 构建）。
func wireMemoryEngine(store memory.MemoryStore, mc MemoryConfig) (memory.MemoryStore, error) {
	if mc.Engine == nil || mc.Engine.Embedding == nil {
		return store, nil
	}
	if mc.Path != "" {
		// 引擎缓存键含 backend/model/dimensions（审查 Nit6）：同 path 但不同引擎配置
		// 不串用（否则会静默复用一个语义不同的引擎）。
		cacheKey := engineCacheKey(mc)
		namedEngineMu.Lock()
		defer namedEngineMu.Unlock()
		if eng, ok := namedEngines[cacheKey]; ok {
			return newEngineBridgeWithRemover(store, eng), nil
		}
		eng, err := buildMemoryEngine(store, *mc.Engine)
		if err != nil {
			log.Warnf("[tagent] memory engine disabled (build failed): %v", err)
			return store, nil
		}
		namedEngines[cacheKey] = eng
		return newEngineBridgeWithRemover(store, eng), nil
	}
	eng, err := buildMemoryEngine(store, *mc.Engine)
	if err != nil {
		log.Warnf("[tagent] memory engine disabled (build failed): %v", err)
		return store, nil
	}
	return newEngineBridgeWithRemover(store, eng), nil
}

// engineCacheKey 构造共享引擎缓存键：path + backend + embedding model/dimensions
// （审查 Nit6：同 path 不同引擎配置不串用）。
func engineCacheKey(mc MemoryConfig) string {
	backend, model, dims := "", "", 0
	if mc.Engine != nil {
		backend = mc.Engine.Backend
		if mc.Engine.Embedding != nil {
			model = mc.Engine.Embedding.Model
			dims = mc.Engine.Embedding.Dimensions
		}
	}
	return fmt.Sprintf("%s|%s|%s|%d", mc.Path, backend, model, dims)
}

// newEngineBridgeWithRemover 创建 engineBridge 并把向量移除回调接到 base store（若支持
// SetVectorRemover）——使 TTL/容量遗忘物理删除事件时同步移除向量（内存索引 + KV 持久键），
// 消除 engine.Remove 死代码、防死键堆积与重启复活（审查 M2）。
func newEngineBridgeWithRemover(store memory.MemoryStore, eng memory.MemoryEngine) memory.MemoryStore {
	bridge := memory.NewEngineBridge(store, eng)
	if setter, ok := store.(interface{ SetVectorRemover(memory.VectorRemover) }); ok {
		if vr, ok := bridge.(memory.VectorRemover); ok {
			setter.SetVectorRemover(vr)
		}
	}
	return bridge
}

// buildMemoryEngine 按配置构建嵌入器 + 引擎。嵌入器不可用（无 key）时返回 error（调用方降级）。
func buildMemoryEngine(store memory.MemoryStore, ec MemoryEngineConfig) (memory.MemoryEngine, error) {
	emb, err := buildEmbedder(*ec.Embedding)
	if err != nil {
		return nil, err
	}
	ecfg := memory.EngineConfig{
		VectorTopK:  ec.VectorTopK,
		KeywordTopK: ec.KeywordTopK,
		RRFK:        ec.RRFK,
	}
	// 持久化：store 若提供底层 KV（FileSegmentStore over rustviking/LocalFileKV），
	// 引擎向量序列化入 KV + 启动重建——跨重启恢复语义召回。纯内存 store 无 KV → 不持久。
	if kvp, ok := store.(memory.KVProvider); ok {
		ecfg.KV = kvp.KVBackend()
	}
	switch ec.Backend {
	case "", "memory", "rustviking":
		// MVP 内存向量索引 + 可选 KV 持久化（rustviking-backed）。依据 F1 报告「追加发现」：
		// rustviking 原生 index CLI 进程内易失，故用其 KV 持久化向量 + 启动重建；
		// 原生 HNSW/IVF 索引持久化（接入 ivf_persist）为 rustviking backlog。
		return memory.NewInMemoryEngine(store, emb, ecfg), nil
	default:
		return nil, fmt.Errorf("unknown memory engine backend %q", ec.Backend)
	}
}

// buildEmbedder 按配置构建嵌入器。zhipu 无 key 时返回 error（调用方优雅降级）。
func buildEmbedder(ec EmbeddingConfig) (memory.Embedder, error) {
	var inner memory.Embedder
	switch ec.Provider {
	case "mock":
		dim := ec.Dimensions
		if dim <= 0 {
			dim = 64
		}
		inner = memory.NewMockEmbedder(dim)
	case "", "zhipu":
		z, err := memory.NewZhipuEmbedder(memory.ZhipuEmbedderConfig{
			Endpoint:   ec.Endpoint,
			Model:      ec.Model,
			APIKeyEnv:  ec.APIKeyEnv,
			Dimensions: ec.Dimensions,
		})
		if err != nil {
			return nil, err
		}
		inner = z
	default:
		return nil, fmt.Errorf("unknown embedding provider %q", ec.Provider)
	}
	// 组8 向量链路可观测：TracedEmbedder 统一包裹（embedding span + GenAI 属性 + counter/
	// histogram）。noop 安全——未设 OTLP 时零开销、Embed 行为逐字节不变。
	return memory.NewTracedEmbedder(inner), nil
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

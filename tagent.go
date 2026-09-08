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
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/tool"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"

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

	// mcpRegistry is the process-level MCP server registry (config-declared
	// servers + WithMCPToolSets merged). Consumed by mcp_discover/mcp_call;
	// mutations never touch agent tool declarations.
	mcpRegistry *toolmcp.Registry

	// reliability 是根配置可靠性段副本（5.4 design-report-closeout）：
	// buildPlainToolRef 据此注入 MCPProbeEvery（降级行为配置，per-agent 工具共用）。
	reliability ReliabilityConfig

	// resolvedModels caches model.Model instances keyed by "provider:model" string.
	// Agents sharing the same provider+model reuse the same instance.
	resolvedModels map[string]model.Model

	// modelOverrides injects pre-resolved model instances for specific agents.
	// This supports scenarios like SwappableModel for entry agent (AReaL proxy).
	modelOverrides map[string]model.Model

	// trajectoryRecorder is set when cfg.TrajectoryDump is true.
	// It wraps rc.model, and is registered as a Closer on the entry agent.
	trajectoryRecorder *rl.TrajectoryRecorder

	// evolution (self-evolution-git-native)：git 原生自进化装配单元（配置门控，默认关）。
	// 文件即真源（热重载直生效）+ git 版本层（commit/revert/log）+ 建议式评估（judge/guardrail
	// 只产 evaluation 事件，P4 框架不动手）。judge/guard 在 buildAgent 经 BindRuntime 延迟绑定。
	evoGit *evolution.GitEvolution

	// approvalChannels（R5）：外部审批送达通道（WithApprovalChannel 注入，govGate 构造后注册）。
	approvalChannels []governance.ApprovalChannel

	// governance (T-G)：治理闸运行时，cfg.Governance.Enabled 时构造，跨 agent 共享。
	// govGate 对 entry agent 的 leaf 工具调用做风险分级 + 预算 + goal + critical 批准。
	govGate *governance.GovernanceGate
	// govLedger 是跨 agent 共享的治理账本（N2）：所有 agent gate 复用同一实例，entry buildAgent
	// 时延迟绑定 entry memStore（子 agent 先构造、entry memStore 后就绪），使子 agent 治理记录
	// 也持久化到 entry governance 分区（durable 审计，重启可 recall）。
	govLedger *governance.DenialLedger
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

	// namedRVStores provides shared rustviking-backed FileSegmentStore instances
	// by path（M-1，四审）：type: file 与 memory/localfile 同构——同 path 必须同实例，否则跨
	// agent read_namespaces 下 InMemRelationStore 内存图分歧（因果链断链）+ 双 Compactor
	// 基于独立视图并发覆盖同一 KV 键 + 双 LifecycleManager 重复扫描。
	namedRVMu     sync.Mutex
	namedRVStores = map[string]*memory.FileSegmentStore{}

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

// WithApprovalChannel 注入外部审批送达通道（R5 backlog-final-closeout）——审批请求经
// Deliver 渠道直投（如微信 SendTextToUser），不依赖 agent 转述。evolution/governance
// 未启用时为 no-op。可多次调用（多通道尽力投递）。
func WithApprovalChannel(ch governance.ApprovalChannel) Option {
	return func(rc *runtimeConfig) {
		rc.approvalChannels = append(rc.approvalChannels, ch)
	}
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

	rc := &runtimeConfig{reliability: cfg.Reliability}
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

	// evolution (self-evolution-git-native)：git 原生自进化（配置门控，默认关 → 零行为变化）。
	// 文件即真源+git 版本层；启动自检 git 仓（Warn 不阻断——非仓下改文件仍生效，无留痕/评估）。
	if cfg.Evolution.Enabled {
		rc.evoGit = evolution.NewGitEvolution(evolution.GitEvolutionConfig{
			WorkDir:        "", // 运行 cwd
			ProtectedPaths: cfg.Evolution.ProtectedPaths,
			JudgeDelay:     time.Duration(cfg.Evolution.JudgeDelaySeconds) * time.Second,
		})
		if !rc.evoGit.IsRepo() {
			log.Warnf("[tagent] evolution enabled but cwd is not a git repo — improvements will take effect (hot-reload) but have no git trail/evaluation")
		}
	}

	// T-G: 治理闸运行时（配置门控，默认关闭 → 全放行，现状零行为变化）。构造 Budget/Approval/
	// Goal/Ledger + GovernanceGate；entry agent 的 leaf 工具经 GovernanceTool 装饰器过闸。
	if cfg.Governance.Enabled {
		// N2：共享治理账本（所有 agent gate 复用），entry buildAgent 时延迟绑定 entry memStore。
		rc.govLedger = governance.NewDenialLedger(nil, 0)
		rc.govGate = governance.NewGovernanceGate(governance.GateDeps{
			Budget: governance.NewBudgetManager(governance.BudgetConfig{
				Window:        time.Duration(cfg.Governance.BudgetWindowMinutes) * time.Minute,
				MaxHighRisk:   cfg.Governance.MaxHighRisk,
				MaxMediumRisk: cfg.Governance.MaxMediumRisk,
			}, cfg.Governance.Dir),
			Approval: governance.NewApprovalManager(cfg.Governance.Dir, 0),
			Goals:    governance.NewGoalRegistry(),
			Ledger:   rc.govLedger, // N2：共享账本（替代 nil 兜底内存，供所有 agent gate 复用）
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

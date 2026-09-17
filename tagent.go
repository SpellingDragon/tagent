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
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/tool"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// Option injects runtime-only dependencies that cannot be serialized.
type Option func(*runtimeConfig)

// runtimeConfig holds runtime-only dependencies.
type runtimeConfig struct {
	model       model.Model // Default model (can be overridden per-agent)
	skillRepo   tool.SkillRepository
	mcpToolSets []trpctool.ToolSet

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

	// configPath (agent-config-hot-reload, incremental A): when set via
	// WithConfigPath, arms a lazy org-config watcher on the entry agent.
	// Empty = disabled (default).
	configPath string

	// trajectoryRecorder is set when cfg.TrajectoryDump is true.
	// It wraps rc.model, and is registered as a Closer on the entry agent.
	trajectoryRecorder *rl.TrajectoryRecorder

	// evolution (self-evolution-git-native)：git 原生自进化装配单元（配置门控，默认关）。
	// 文件即真源（热重载直生效）+ git 版本层（commit/revert/log）+ 建议式评估（judge/guardrail
	// 只产 evaluation 事件，P4 框架不动手）。judge/guard 在 buildAgent 经 BindRuntime 延迟绑定。
	evoGit *evolution.GitEvolution

	// approvalChannels（R5）：外部审批送达通道（WithApprovalChannel 注入，govGate 构造后注册）。
	approvalChannels []governance.ApprovalChannel

	// storeOwners（resident-readiness-plan 4.4）：底层 store 指针 → pid → 首个
	// 占名 agent——共享 store 内不同名同 pid 的冲突在构建期 fail-closed。
	storeOwners   map[string]map[int]string
	storeOwnersMu sync.Mutex

	// residentAgents（resident-readiness-plan 4.5）：常驻构建后的 name → agent
	// 绑定表——热更壳子树按 agent 身份借用其常驻资源（store 等），绝不全部
	// 复用 entryMemStore（防子 agent 存储漂移）。拓扑增减（新增/删除 agent）
	// 在 reloader 中 fail-closed 拒绝（须重启）。
	residentAgents map[string]*agent.TagentAgent

	// governance (T-G)：治理闸运行时，cfg.Governance.Enabled 时构造，跨 agent 共享。
	// govGate 对 entry agent 的 leaf 工具调用做风险分级 + 预算 + goal + critical 批准。
	govGate *governance.GovernanceGate
	// govLedger 是跨 agent 共享的治理账本（N2）：所有 agent gate 复用同一实例，entry buildAgent
	// 时延迟绑定 entry memStore（子 agent 先构造、entry memStore 后就绪），使子 agent 治理记录
	// 也持久化到 entry governance 分区（durable 审计，重启可 recall）。
	govLedger *governance.DenialLedger

	// entryMemStore/entrySessionSvc（R4，review 🔴1）：常驻 entry 的持久事实链 store
	// 与 session 服务（含 AppendEventHook→outputCh 接线）——executorOnly 热重建壳
	// **复用**它们（而非内存实例/新 sessionSvc），否则换代后事实链停止增长、
	// 用户消息出向投递断链。New() 构造 entry 后回填。
	entryMemStore   memory.MemoryStore
	entrySessionSvc session.Service
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

// WithConfigPath records the on-disk path the Config was loaded from
// (agent-config-hot-reload, incremental A). When set, the entry agent arms
// a lazy org-config watcher: before each LLM call it stats the file and on
// change re-parses + fingerprints the org whitelist subset — migratable
// numeric params (compress_threshold) are hot-applied; structural diffs
// are logged as restart-required until snapshot rebuild (incremental B).
func WithConfigPath(path string) Option {
	return func(rc *runtimeConfig) { rc.configPath = path }
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

	rc := &runtimeConfig{
		reliability: cfg.Reliability,
		storeOwners: make(map[string]map[int]string),
	}
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
	entryAgent, err := buildAgent(cfg.Entry, entryCfg, cfg, rc, loader, agentCache, buildModeResident)
	if err != nil {
		return nil, fmt.Errorf("tagent: build entry agent %q: %w", cfg.Entry, err)
	}
	// R4（review 🔴1）：回填常驻 entry 资源——懒检查 fp 变分支的 executorOnly 热重建
	// 从这里取真实事实链 store 与 session 服务。
	rc.entryMemStore = entryAgent.MemStore()
	rc.entrySessionSvc = entryAgent.SessionSvc()
	rc.residentAgents = agentCache // 4.5：常驻身份绑定表（含 entry 与全部子 agent）
	names := make(map[string]bool, len(agentCache))
	for n := range agentCache {
		names[n] = true
	}
	entryAgent.SetResidentNames(names)
	for _, a := range agentCache { // 4.5：全拓扑共享绑定表（含自身）
		a.SetResidentTable(agentCache)
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

	// Org-layer config hot reload (agent-config-hot-reload, incremental A).
	// When the config path is known, arm a lazy check on the entry agent:
	// before each LLM call, stat the file; on mtime churn re-parse and
	// fingerprint the org whitelist subset (D3). Behavior:
	//   - fingerprint UNCHANGED: hot-apply migratable numeric params
	//     (compress_threshold) onto the live agent via ApplyOrgParams.
	//   - fingerprint CHANGED: structural diff (tools/agents/model wiring).
	//     Snapshot rebuild lands in incremental B; until then log loudly
	//     that a restart is required and keep serving (fail-closed).
	if rc.configPath != "" {
		cfgPath := rc.configPath
		var ( // closure-captured reload state
			lastFP        string
			lastMemFP     string         // R4 3.1：memory 先序检测（🔴5——被 org 指纹排除，不先检则静默不生效）
			lastSeenMtime int64          // atomic; -nanos of last processed config mtime
			mu            sync.Mutex     // single-flight reload
			execGen       int            // R4 3.8：执行器代次（代际日志）
			prevKeep      *Config        // R4 3.8：ring 2 当前代配置（换代时转 prevSnapshot）
			prevSnapshot  reloadSnapshot // R4 3.8：ring 2 上一代（Rollback 数据源）
		)
		// hardening-review-batch2 5.1（逐 agent 热更）：hotParamsFor 提取 +
		// agentCache 全遍历——数值热更不得仅覆盖 entry（spec config-hot-reload：
		// 只改子 agent keep_recent_tasks 时该子 agent 压缩器 MUST 收到新值）。
		// 4.7（desired/effective 全量语义）：hotParamsFor 输出**全量 desired**——
		// 显式配置生效，字段删除回落解析默认（entry 8000 / 子 4096；阈值 0.8；
		// keepRecent 2；task 2m/1h/关）。ApplyOrgHotParams 的零值保护对正默认
		// 透明；「显式 0/负」仍可表达（StaleAfter/JobDeadline 支持负语义）。
		hotParamsFor := func(aname string, ac *AgentConfig) agent.OrgHotParams {
			isEntry := aname == cfg.Entry
			defMax := 4096
			if isEntry {
				defMax = 8000
			}
			p := agent.OrgHotParams{
				ThresholdPct:    compress.DefaultCompressThreshold,
				MaxTokens:       defMax,
				KeepRecentTasks: 2,               // agent-layer parsed default (agent/agent.go)
				TaskTerminalTTL: 2 * time.Minute, // task.defaultTerminalTTL (agent/task)
				TaskStaleAfter:  time.Hour,       // task.defaultStaleAfter (agent/task)
			}
			if ac == nil {
				return p
			}
			if ac.CompressThreshold > 0 {
				p.ThresholdPct = ac.CompressThreshold
			}
			if ac.MaxTokens > 0 {
				p.MaxTokens = ac.MaxTokens
			}
			if ac.KeepRecentTasks > 0 {
				p.KeepRecentTasks = ac.KeepRecentTasks
			}
			if ttl, perr := time.ParseDuration(ac.TaskTerminalTTL); perr == nil && ttl > 0 {
				p.TaskTerminalTTL = ttl
			}
			if sa, saerr := time.ParseDuration(ac.TaskStaleAfter); saerr == nil && ac.TaskStaleAfter != "" {
				p.TaskStaleAfter = sa // 显式负值 = 关闭观测（语义保留）
			}
			if jd, jderr := time.ParseDuration(ac.TaskJobDeadline); jderr == nil && ac.TaskJobDeadline != "" {
				p.TaskJobDeadline = jd
			}
			return p
		}
		applyHotAll := func(freshCfg *Config) {
			for aname, a := range agentCache {
				if a == nil {
					continue
				}
				src := freshCfg.Agents[aname] // 值类型：hotParamsFor 取址安全（map 内元素不可寻址，拷贝后取）
				p := hotParamsFor(aname, &src)
				a.ApplyOrgHotParams(p)
				log.Infof("[org-hotreload] agent %q hot params applied: threshold=%.2f maxTokens=%d keepRecent=%d terminalTTL=%s staleAfter=%s jobDeadline=%s",
					aname, p.ThresholdPct, p.MaxTokens, p.KeepRecentTasks, p.TaskTerminalTTL, p.TaskStaleAfter, p.TaskJobDeadline)
			}
		}
		if fp, err := computeOrgFingerprint(&cfg); err == nil {
			lastFP = fp
		}
		if mfp, err := computeMemoryFingerprint(&cfg); err == nil {
			lastMemFP = mfp
		}
		// hardening-review-batch2 5.5（首代快照）：启动代即 ring-2 的第零代——
		// 否则首次结构热更把 prevKeep(nil→启动后第一代) 存进 prevSnapshot，
		// Rollback 永远报「无上一代快照」。启动代快照让首次换代即可回滚。
		startupCfg := cfg
		prevKeep = &startupCfg
		curReach := reachableAgents(&cfg, cfg.Entry) // 4.5：启动代可达拓扑（增减检测基准）
		entryAgent.SetOrgReloader(func() {
			info, err := os.Stat(cfgPath)
			if err != nil {
				return // file gone/unreachable: keep serving, nothing to do
			}
			mt := info.ModTime().UnixNano()
			if mt == atomic.LoadInt64(&lastSeenMtime) {
				return // hot path: unchanged
			}
			mu.Lock()
			defer mu.Unlock()
			if info2, err2 := os.Stat(cfgPath); err2 == nil {
				if info2.ModTime().UnixNano() == atomic.LoadInt64(&lastSeenMtime) {
					return // re-check under lock
				}
				atomic.StoreInt64(&lastSeenMtime, info2.ModTime().UnixNano())
			}
			fresh, err := LoadConfig(cfgPath)
			if err != nil {
				log.Errorf("[org-hotreload] config parse FAILED — serving previous: %v", err)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 配置解析失败，沿用旧配置（未生效）: %v", err))
				return
			}
			fp, err := computeOrgFingerprint(fresh)
			if err != nil {
				log.Errorf("[org-hotreload] fingerprint FAILED — serving previous: %v", err)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 指纹计算失败，沿用旧配置: %v", err))
				return
			}
			// R4 3.1（🔴5）memory 先序：org 指纹比对**之前**独立 diff memory 段——
			// 命中即拒绝热更并明示须重启（fail-closed；不依赖 fingerprint 变化路径）。
			mfp, merr := computeMemoryFingerprint(fresh)
			if merr == nil && mfp != lastMemFP {
				log.Errorf("[org-hotreload] agents.*.memory.* CHANGED (mem-fp %s.. -> %s..) — runtime storage migration is not supported; RESTART required to apply", short(lastMemFP), short(mfp))
				entryAgent.EmitSystemAlert("org-hotreload: memory 段变更需重启迁移，本次未热更（须重启生效）")
				// 4.7：拒绝不推进 effective——同一未生效 memory 配置再次编辑
				// 其他字段时仍以 effective 为基准拒绝，绝不借第二次检查绕过。
				return
			}
			if fp == lastFP {
				// org structure unchanged: hot-apply the numeric bundle to
				// EVERY built agent (5.1 逐 agent) — per-agent effective 回执。
				applyHotAll(fresh)
				return
			}
			// 4.5 拓扑增减检测：新增/删除 agent 的热更被拒（须重启）——常驻身份
			// 绑定表只覆盖启动代拓扑；热更壳对既有 agent 按身份借用其常驻资源，
			// 对新增 agent 无常驻资源可借（其资源生命周期无法安全并入常驻层）。
			freshReach := reachableAgents(fresh, cfg.Entry)
			for aname := range freshReach {
				if _, resident := rc.residentAgents[aname]; !resident {
					log.Errorf("[org-hotreload] NEW agent %q cannot be hot-added — RESTART required (fail-closed)", aname)
					entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 新增 agent %q 需重启生效，本次未热更", aname))
					return
				}
			}
			for aname := range curReach {
				if _, ok := freshReach[aname]; !ok {
					log.Errorf("[org-hotreload] REMOVED agent %q cannot be hot-removed — RESTART required (fail-closed)", aname)
					entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 删除 agent %q 需重启生效，本次未热更", aname))
					return
				}
			}
			// R4（resident-continuity-r2-r4 3.5/3.8）：结构变化→cm.runner 级热重建
			//（executorOnly 模式：丢弃壳仅取 runner；按 ownership 表跳过对共享物
			// 副作用），build-validate-then-swap：构建失败 fail-closed（旧 runner
			// 原样服务），成功 SwapExecutor+代际日志（下一 turn 生效；in-flight
			// turn 用旧 runner 跑完）。ring 2 上一代配置供 Rollback。
			newTA, rerr := buildAgent(cfg.Entry, fresh.Agents[cfg.Entry], *fresh, rc, loader, map[string]*agent.TagentAgent{}, buildModeExecutorShell)
			if rerr != nil {
				log.Errorf("[org-hotreload] executor rebuild FAILED — serving previous (fail-closed): %v", rerr)
				return
			}
			// hotswap-fix 5.7：不再把新壳的 runner 整壳换入（新壳 fwAgent 的
			// BeforeModel 闭包捕获新壳自己的空 cm → 换装后请求装配接到空投影，
			// n=1 system-only → provider 400/1214，2026-09-13 21:5x 事故根因）。
			// 改为在常驻 cm 上只重建执行面（模型/工具/提示词），projection/bus/
			// sessionSvc/回调闭包全部同源保留。newTA 仅作构建验证（fail-closed 语义：构建不过不换）。
			newRunner := newTA.RebuildExecutorOn(entryAgent.ContextManager())
			if newRunner == nil {
				log.Errorf("[org-hotreload] executor rebuild produced no runner — serving previous (fail-closed)")
				entryAgent.EmitSystemAlert("org-hotreload: executor 重建失败，沿用旧配置（fail-closed）")
				return
			}
			oldFP := lastFP
			_ = newTA
			// hardening-review-batch2 5.1/5.4（热更统一应用）：结构变化分支
			// 同批对所有已构建 agent 应用数值参数（互斥分支拆除的完成形态）。
			applyHotAll(fresh)
			lastFP = fp
			execGen++
			log.Infof("[org-hotreload] executor generation %d swapped (fp %s.. -> %s.., effective next turn; prompt/model/tools rebuilt, cm/bus/projection/registry untouched) — numeric bundle applied to %d built agent(s)",
				execGen, short(oldFP), short(fp), len(agentCache))
			entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 热更新已生效（generation %d，fp %s.. → %s..，下回合起用新配置）", execGen, short(oldFP), short(fp)))
			// ring 2（3.8）：保留上一代配置摘要供 Rollback（覆盖更早代）。
			prevSnapshot = reloadSnapshot{fp: oldFP, cfg: prevKeep}
			prevKeep = fresh
			entryAgent.SetRollbackFn(func() {
				mu.Lock()
				defer mu.Unlock()
				if prevSnapshot.cfg == nil {
					log.Warnf("[org-hotreload] rollback: no previous generation snapshot")
					entryAgent.EmitSystemAlert("org-hotreload: 回滚失败——无上一代快照")
					return
				}
				rollbackC := *prevSnapshot.cfg
				rbp, rerr0 := computeOrgFingerprint(&rollbackC)
				if rerr0 != nil {
					log.Errorf("[org-hotreload] rollback fingerprint FAILED: %v", rerr0)
					entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 回滚指纹计算失败: %v", rerr0))
					return
				}
				if rbp == fp {
					log.Warnf("[org-hotreload] rollback: previous generation equals current (fp %s..)", short(fp))
					return
				}
				if ta2, rerr2 := buildAgent(cfg.Entry, rollbackC.Agents[cfg.Entry], rollbackC, rc, loader, map[string]*agent.TagentAgent{}, buildModeExecutorShell); rerr2 != nil {
					log.Errorf("[org-hotreload] rollback rebuild FAILED — serving current (fail-closed): %v", rerr2)
					entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 回滚重建失败，保留当前代（fail-closed）: %v", rerr2))
					return
				} else if r2 := ta2.RebuildExecutorOn(entryAgent.ContextManager()); r2 != nil {
					// 5.7×5.1 融合：RebuildExecutorOn→cm.RebuildExecutor 已完成 SwapExecutor，
					// 旧 runner 已在其内部交 RetireRunner 延迟 Close；不得二次换入
					// （新签名下 SwapExecutor(r2) 返回 r2 作“旧 runner”，误 retire 关闭在用 runner）。
					_ = r2
					execGen++
					lastFP = rbp
					log.Infof("[org-hotreload] executor generation %d rolled back to fp %s..", execGen, short(rbp))
				}
			})
		})
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

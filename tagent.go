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
	"errors"
	"fmt"
	"os"
	"sort"
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
	model       model.Model
	skillRepo   tool.SkillRepository
	mcpToolSets []trpctool.ToolSet

	// mcpRegistry is the process-level MCP server registry (config-declared
	// servers + WithMCPToolSets merged). Consumed by mcp_discover/mcp_call;
	// mutations never touch agent tool declarations.
	mcpRegistry *toolmcp.Registry

	// reliability 是根配置可靠性段副本：
	// buildPlainToolRef 据此注入 MCPProbeEvery（降级行为配置，per-agent 工具共用）。
	reliability ReliabilityConfig

	// resolvedModels caches model.Model instances keyed by "provider:model" string.
	// Agents sharing the same provider+model reuse the same instance.
	resolvedModels map[string]model.Model

	// modelOverrides injects pre-resolved model instances for specific agents.
	// This supports scenarios like SwappableModel for entry agent (AReaL proxy).
	modelOverrides map[string]model.Model

	// configPath names the org-config file armed via WithConfigPath: when set, a
	// lazy watcher runs on the entry agent. Empty = disabled (default).
	// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
	configPath string

	// retirementPoke carries the drain-forward check (the same lazy, single-flight
	// pending drain a business turn rides on) to every owner the assembly builds,
	// so a released usage right continues retirement without waiting for another
	// turn. Set once the reloader closures exist; owners built before that are
	// armed in the same step. Unset = nothing to notify.
	// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
	retirementPoke atomic.Pointer[func()]

	// trajectoryRecorder is set when cfg.TrajectoryDump is true.
	// It wraps rc.model, and is registered as a Closer on the entry agent.
	trajectoryRecorder *rl.TrajectoryRecorder

	// evoGit 是 git 原生自进化的装配单元（配置门控，默认关）：文件即真源（热重载直接
	// 生效），git 承载版本层（commit/revert/log），评估只建议不动手——judge/guardrail
	// 仅产出 evaluation 事件。judge 与 guard 于 buildAgent 期间经 BindRuntime 延迟绑定。
	// 契约: docs/wiki/platform/platform-subsystems.md#evolution-wiring
	evoGit *evolution.GitEvolution

	// approvalChannels 是外部审批的送达通道：由 WithApprovalChannel 注入，govGate 构造之后注册。
	approvalChannels []governance.ApprovalChannel

	// storeOwners：底层 store 指针 → pid → 首个
	// 占名 agent——共享 store 内不同名同 pid 的冲突在构建期 fail-closed。
	storeOwners   map[string]map[int]string
	storeOwnersMu sync.Mutex

	// resident 是常驻构建之后的 name → agent 绑定表：热更壳子树按 agent 身份借用其
	// 常驻资源（store 等），绝不整体复用 entryMemStore（防子 agent 存储漂移）。热更
	// 时新加入的成员同样**写入**此表，故它是共享的 copy-on-write 快照
	// （agent.ResidentTopology），不是可原地改的裸 map。
	// 契约: docs/wiki/platform/org-hot-reload.md#staged-add-visibility
	resident *agent.ResidentTopology
	// residentMemFP 是名字 → 其 memory 段指纹。被移除后重入的同名 agent 必须复用原
	// owner，memory 段变了就拒绝候选——此表是那个判定的依据，与 resident 表同生命周期
	// （移除不清，拒绝回退才清）。只在 reloader 的互斥量下读写。
	// 契约: docs/wiki/platform/org-hot-reload.md#staged-add-visibility
	residentMemFP map[string]string

	// govGate 是治理闸运行时（cfg.Governance.Enabled 时构造，跨 agent 共享）：对 entry
	// agent 的 leaf 工具调用做风险分级 + 预算 + goal + critical 批准。
	govGate *governance.GovernanceGate
	// govLedger 是跨 agent 共享的治理账本：所有 agent 的 gate 复用同一实例，entry
	// buildAgent 时延迟绑定 entry memStore（子 agent 后构造、entry memStore 那时才就绪），
	// 使子 agent 的治理记录也持久化到 entry 的 governance 分区（durable 审计，重启可 recall）。
	// 契约: docs/wiki/platform/platform-subsystems.md#governance-gate
	govLedger *governance.DenialLedger

	// entryMemStore/entrySessionSvc 是常驻 entry 的持久事实链 store 与 session 服务
	// （含 AppendEventHook→outputCh 接线）——executorOnly 热重建壳**复用**它们（而非
	// 内存实例/新 sessionSvc），否则换代后事实链停止增长、用户消息出向投递断链。
	// New() 构造 entry 之后回填。
	entryMemStore   memory.MemoryStore
	entrySessionSvc session.Service

	// storeBarriers 是本次 build 已登记的共享 store 屏障集：store 登记时按去重抬起，
	// buildAgent 顶层统一放下，覆盖「清点→Protect→核对」整段窗口——多个 owner 共用
	// 一条 store 时，登记间隙不会被遗忘扫描插缝（不靠注册顺序或宽限计时碰巧）。
	// 契约: docs/wiki/platform/resource-ownership.md#composition-barrier
	storeBarriers   map[memory.RetentionHoldable]struct{}
	storeBarriersMu sync.Mutex
}

// raiseStoreBarrier 对带保留租约的共享 store 抬起登记屏障：同一 build 内多次登记
// 同一 store 只抬一层。没有租约的 store（纯内存、无破坏性扫描）无屏障可抬，静默跳过。
// 契约: docs/wiki/platform/resource-ownership.md#composition-barrier
func (rc *runtimeConfig) raiseStoreBarrier(store memory.MemoryStore) {
	if rc == nil {
		return
	}
	h, ok := store.(memory.RetentionHoldable)
	if !ok {
		return
	}
	rc.storeBarriersMu.Lock()
	defer rc.storeBarriersMu.Unlock()
	if rc.storeBarriers == nil {
		rc.storeBarriers = map[memory.RetentionHoldable]struct{}{}
	}
	if _, held := rc.storeBarriers[h]; held {
		return
	}
	rc.storeBarriers[h] = struct{}{}
	h.BeginHold()
}

// releaseStoreBarriers 放下本次 build 持有的全部 store 登记屏障，遗忘随之恢复放行；
// lease 层对多余放下幂等，不会受损。持有集合摘清之后，屏障在锁外逐层放下（不与
// 扫描器唤醒互锁）。可重入：热更壳重建时重新抬起。
// 契约: docs/wiki/platform/resource-ownership.md#composition-barrier
func (rc *runtimeConfig) releaseStoreBarriers() {
	if rc == nil {
		return
	}
	rc.storeBarriersMu.Lock()
	held := make([]memory.RetentionHoldable, 0, len(rc.storeBarriers))
	for h := range rc.storeBarriers {
		held = append(held, h)
	}
	rc.storeBarriers = nil
	rc.storeBarriersMu.Unlock()
	for _, h := range held {
		h.EndHold()
	}
}

// WithModel sets the resolved model instance (required).
// This is the default model; individual agents can override via AgentConfig.Model.
func WithModel(m model.Model) Option {
	return func(rc *runtimeConfig) { rc.model = m }
}

// WithApprovalChannel 注入外部审批送达通道——审批请求经
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

// closeResourcesExited reports whether every deferred exit has run. The helper is
// separate so its payload cannot be read as reporting "close finished" from a
// non-nil error alone: the first report's error stays visible while the tail
// finishes independently.
func closeResourcesExited(ta *agent.TagentAgent) bool {
	done, _ := ta.DeferredCloseOutcome()
	return done
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
//
// 契约: docs/wiki/platform/org-hot-reload.md#overview
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
// 契约: docs/wiki/platform/org-hot-reload.md#memory-preflight
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
// 契约: docs/wiki/tool/tool-architecture.md#declaration-stability
// 契约: docs/wiki/platform/platform-subsystems.md#governance-gate
// 契约: docs/wiki/platform/platform-subsystems.md#evolution-wiring
func New(cfg Config, opts ...Option) (*agent.TagentAgent, error) {
	if err := RegisterBuiltinTools(); err != nil {
		return nil, fmt.Errorf("tagent: register builtin tools: %w", err)
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

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

	rc.mcpRegistry = toolmcp.NewRegistry(toolmcp.WithConfigPath(cfg.ConfigPath))
	rc.mcpRegistry.Seed(cfg.MCPServers)
	for _, ts := range rc.mcpToolSets {
		if ts != nil {
			rc.mcpRegistry.Add(ts.Name(), ts)
		}
	}

	if cfg.TrajectoryDump {
		tr, err := rl.NewTrajectoryRecorder(rc.model, cfg.TrajectoryDir, cfg.APIEndpoint)
		if err != nil {
			return nil, fmt.Errorf("tagent: create trajectory recorder: %w", err)
		}
		rc.trajectoryRecorder = tr
		rc.model = tr
		log.Infof("[tagent] TrajectoryRecorder wrapping model, dir=%s", cfg.TrajectoryDir)
	}

	if cfg.Evolution.Enabled {
		rc.evoGit = evolution.NewGitEvolution(evolution.GitEvolutionConfig{
			WorkDir:        "",
			ProtectedPaths: cfg.Evolution.ProtectedPaths,
			JudgeDelay:     time.Duration(cfg.Evolution.JudgeDelaySeconds) * time.Second,
		})
		if !rc.evoGit.IsRepo() {
			log.Warnf("[tagent] evolution enabled but cwd is not a git repo — improvements will take effect (hot-reload) but have no git trail/evaluation")
		}
	}

	if cfg.Governance.Enabled {
		rc.govLedger = governance.NewDenialLedger(nil, 0)
		rc.govGate = governance.NewGovernanceGate(governance.GateDeps{
			Budget: governance.NewBudgetManager(governance.BudgetConfig{
				Window:        time.Duration(cfg.Governance.BudgetWindowMinutes) * time.Minute,
				MaxHighRisk:   cfg.Governance.MaxHighRisk,
				MaxMediumRisk: cfg.Governance.MaxMediumRisk,
			}, cfg.Governance.Dir),
			Approval: governance.NewApprovalManager(cfg.Governance.Dir, 0),
			Goals:    governance.NewGoalRegistry(),
			Ledger:   rc.govLedger,
			Config: governance.GateConfig{
				Enabled:         true,
				Enforcement:     governance.Enforcement(cfg.Governance.Enforcement),
				GoalRequiredFor: cfg.Governance.GoalRequiredFor,
			},
		})
		log.Infof("[tagent] governance enabled: enforcement=%s (bounded autonomy gate)", cfg.Governance.Enforcement)
	}

	loader := prompt.NewLoader(cfg.PromptDir, prompt.WithFallback(defaultPromptsFS, DefaultPromptsPrefix))

	agentCache := make(map[string]*agent.TagentAgent)

	entryCfg := cfg.Agents[cfg.Entry]
	entryAgent, err := buildAgent(cfg.Entry, entryCfg, cfg, rc, loader, agentCache, buildModeResident)
	if err != nil {
		return nil, fmt.Errorf("tagent: build entry agent %q: %w", cfg.Entry, err)
	}
	rc.entryMemStore = entryAgent.MemStore()
	rc.entrySessionSvc = entryAgent.SessionSvc()
	rc.resident = agent.NewResidentTopology(agentCache)
	rc.residentMemFP = make(map[string]string, len(agentCache))
	for n := range agentCache {
		mc := cfg.Agents[n]
		rc.residentMemFP[n] = agentMemoryFingerprint(&mc)
	}
	for _, a := range agentCache {
		a.SetResidentTable(rc.resident)
	}
	entryAgent.SetStoreOwnerSnapshot(func() map[string]bool { return rc.ownedAgentNames() })

	if rc.trajectoryRecorder != nil {
		entryAgent.SetTrajectoryRecorder(rc.trajectoryRecorder)
	}

	if rc.configPath != "" {
		cfgPath := rc.configPath
		var (
			lastSeenMtime int64
			mu            sync.Mutex
			building      atomic.Bool
			stopped       atomic.Bool
			retiring      = newRetirementLedger(func(name string) int {
				roster := make([]*agent.TagentAgent, 0, len(rc.resident.Snapshot()))
				for _, a := range rc.resident.Snapshot() {
					if a != nil {
						roster = append(roster, a)
					}
				}
				return agent.BindingHolders(name, roster)
			})
			publishedReach map[string]bool
		)
		coord := newOrgCoordinator()
		publishedReach = reachableAgents(&cfg, cfg.Entry)
		entryAgent.SetOrgDiagnostics(func() map[string]any {
			st := coord.status()
			payload := map[string]any{
				"generation":  st.Generation,
				"revision":    st.Revision,
				"fingerprint": st.Fingerprint,
				"desired":     st.Desired,
				"agents":      st.Agents,
				"configPath":  cfgPath,
				"liveDebt": OrgLiveDebt{
					CapturedAt:         time.Now(),
					Executors:          entryAgent.ContextManager().ExecutorRefs(),
					PendingRetirements: retiring.diagnostics(),
				},
				"close": OrgCloseState{
					Initiated:       entryAgent.CloseStarted(),
					ResourcesExited: closeResourcesExited(entryAgent),
				},
			}
			if !st.LastApplied.IsZero() {
				payload["lastAppliedAt"] = st.LastApplied
			}
			if !st.LastPublished.IsZero() {
				payload["lastPublishedAt"] = st.LastPublished
			}
			if st.LastFailure != nil {
				payload["lastFailure"] = st.LastFailure
			}
			return payload
		})
		hotParamsFor := func(aname string, ac *AgentConfig) agent.OrgHotParams {
			isEntry := aname == cfg.Entry
			defMax := 4096
			if isEntry {
				defMax = 8000
			}
			p := agent.OrgHotParams{
				ThresholdPct:    compress.DefaultCompressThreshold,
				MaxTokens:       defMax,
				KeepRecentTasks: 2,
				TaskTerminalTTL: 2 * time.Minute,
				TaskDefaultTTL:  10 * time.Minute,
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
			if d, derr := time.ParseDuration(ac.TaskDefaultTTL); derr == nil && ac.TaskDefaultTTL != "" && d > 0 {
				p.TaskDefaultTTL = d
			}
			return p
		}
		retireUnrouted := func() {
			for name, owner := range rc.resident.Snapshot() {
				if name == cfg.Entry || owner == nil {
					continue
				}
				if publishedReach[name] {
					retiring.release(name)
					continue
				}
				retiring.track(name, owner)
			}
		}
		sweepRetirements := func() {
			for _, d := range retiring.sweep(publishedReach) {
				switch {
				case d.Retired:
					rc.resident.Unpublish([]string{d.Name})
					log.Infof("[org-hotreload] owner %q retired — %s", d.Name, d.Why)
				case d.Held:
					log.Warnf("[org-hotreload] owner %q retirement held — %s", d.Name, d.Why)
				default:
					log.Infof("[org-hotreload] owner %q still draining — %s", d.Name, d.Why)
				}
			}
		}
		var appliedFromLastApply []appliedAgent
		applyHotAll := func(freshCfg *Config) {
			routable := reachableAgents(freshCfg, cfg.Entry)
			receipts := make([]OrgAgentApply, 0, len(rc.resident.Names()))
			appliedRecord := make([]appliedAgent, 0, len(rc.resident.Names()))
			for aname, a := range rc.resident.Snapshot() {
				if a == nil {
					continue
				}
				if !routable[aname] {
					receipts = append(receipts, OrgAgentApply{Name: aname, Outcome: "draining"})
					if last, ok := a.HotSnapshot(); ok {
						appliedRecord = append(appliedRecord, appliedAgent{Name: aname, Hot: last, Draining: true})
					}
					log.Infof("[org-hotreload] agent %q not routable in this generation — params left untouched (draining owner)", aname)
					continue
				}
				src := freshCfg.Agents[aname]
				p := hotParamsFor(aname, &src)
				appliedRecord = append(appliedRecord, appliedAgent{Name: aname, Hot: p})
				hotSrc := coord.currentHotFor
				ownerName := aname
				a.SetHotSource(func() (agent.OrgHotParams, bool) { return hotSrc(ownerName) })
				receipts = append(receipts, OrgAgentApply{
					Name: aname, Outcome: "applied",
					ThresholdPct: p.ThresholdPct, MaxTokens: p.MaxTokens, KeepRecentTasks: p.KeepRecentTasks,
				})
				log.Infof("[org-hotreload] agent %q hot params applied: threshold=%.2f maxTokens=%d keepRecent=%d terminalTTL=%s defaultTTL=%s",
					aname, p.ThresholdPct, p.MaxTokens, p.KeepRecentTasks, p.TaskTerminalTTL, p.TaskDefaultTTL)
			}
			sort.Slice(receipts, func(i, j int) bool { return receipts[i].Name < receipts[j].Name })
			sort.Slice(appliedRecord, func(i, j int) bool { return appliedRecord[i].Name < appliedRecord[j].Name })
			coord.recordApply(receipts)
			appliedFromLastApply = appliedRecord
		}
		if fp, err := computeOrgFingerprint(&cfg); err == nil {
			if snap, cerr := cfg.Clone(); cerr == nil {
				coord.init(fp, snap)
			} else {
				log.Warnf("[org-hotreload] startup snapshot clone FAILED — Rollback 无首代快照: %v", cerr)
				coord.recordFailure(fmt.Errorf("startup snapshot clone: %w", cerr))
			}
		}
		doRollback := func() {
			mu.Lock()
			defer mu.Unlock()
			if stopped.Load() {
				log.Warnf("[org-hotreload] rollback: shutdown in progress, refused (no publish to a tearing-down cm)")
				return
			}
			prev := coord.rollbackSource()
			if prev == nil || prev.cfg == nil {
				log.Warnf("[org-hotreload] rollback: no previous generation snapshot")
				entryAgent.EmitSystemAlert("org-hotreload: 回滚失败——无上一代快照")
				return
			}
			rollbackC, rbErr := prev.cfg.Clone()
			if rbErr != nil {
				log.Errorf("[org-hotreload] rollback snapshot clone FAILED — serving current (fail-closed): %v", rbErr)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 回滚快照拷贝失败: %v", rbErr))
				coord.recordFailure(fmt.Errorf("rollback snapshot clone: %w", rbErr))
				return
			}
			rbp, rerr0 := computeOrgFingerprint(rollbackC)
			if rerr0 != nil {
				log.Errorf("[org-hotreload] rollback fingerprint FAILED: %v", rerr0)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 回滚指纹计算失败: %v", rerr0))
				coord.recordFailure(fmt.Errorf("rollback fingerprint: %w", rerr0))
				return
			}
			if coord.sameFullAsCurrent(rbp, rollbackC) {
				log.Warnf("[org-hotreload] rollback: previous generation is identical to current on both axes (fp %s.. + hot params) — nothing to restore", short(rbp))
				return
			}
			rbReach := reachableAgents(rollbackC, cfg.Entry)
			if blocked := retiring.closingIn(rbReach); len(blocked) > 0 {
				log.Errorf("[org-hotreload] rollback re-routes %v while their retiring owner is already closing — RESTART required (rejected before any candidate build)", blocked)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 回滚将重路由 %v，但其退役中 owner 已开始关闭，须重启生效（本次未回滚）", blocked))
				coord.recordFailure(fmt.Errorf("rollback re-routes closing owner(s) %v: retirement already began", blocked))
				return
			}
			failRB := func(site string, err error) {
				msg := site
				if err != nil {
					msg = fmt.Sprintf("%s: %v", site, err)
				}
				log.Errorf("[org-hotreload] rollback %s — serving current (fail-closed)", msg)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 回滚中%s，保留当前代（fail-closed）", msg))
				if err != nil {
					coord.recordFailure(fmt.Errorf("rollback %s: %w", site, err))
				} else {
					coord.recordFailure(fmt.Errorf("rollback %s", site))
				}
			}
			rbOv, rbOvOK := buildCandidateOwners(rc, loader, rollbackC, rbReach, failRB)
			if !rbOvOK {
				return
			}
			defer rbOv.abandon()
			rbNames, rbStaged, rbParts, rbOK := stageOrgGenerations(rc, loader, rollbackC, rbReach, rbOv.resolve(), failRB)
			if !rbOK {
				return
			}
			rbOv.commit()
			activateOwnerGenerations(rbOv.resolve(), rbNames, rbStaged, rbParts)
			applyHotAll(rollbackC)
			rgen := coord.recordRollback(rbp, rollbackC, appliedFromLastApply)
			publishedReach = rbReach
			retireUnrouted()
			sweepRetirements()
			log.Infof("[org-hotreload] executor generation %d rolled back to fp %s.. (structure + hot params restored via the same transaction)", rgen.seq, short(rbp))
		}
		entryAgent.SetRollbackFn(doRollback)
		reload := func() {
			info, err := os.Stat(cfgPath)
			if err != nil {
				return
			}
			mt := info.ModTime().UnixNano()
			if mt == atomic.LoadInt64(&lastSeenMtime) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if stopped.Load() {
				return
			}
			if h := orgBuildBarrier.Load(); h != nil {
				(*h)()
			}
			if info2, err2 := os.Stat(cfgPath); err2 == nil {
				if info2.ModTime().UnixNano() == atomic.LoadInt64(&lastSeenMtime) {
					return
				}
				atomic.StoreInt64(&lastSeenMtime, info2.ModTime().UnixNano())
			}
			coord.noteDesired("")
			fresh, err := LoadConfig(cfgPath)
			if err != nil {
				log.Errorf("[org-hotreload] config parse FAILED — serving previous: %v", err)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 配置解析失败，沿用旧配置（未生效）: %v", err))
				coord.recordFailure(fmt.Errorf("config parse: %w", err))
				return
			}
			fp, err := computeOrgFingerprint(fresh)
			if err != nil {
				log.Errorf("[org-hotreload] fingerprint FAILED — serving previous: %v", err)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 指纹计算失败，沿用旧配置: %v", err))
				coord.recordFailure(fmt.Errorf("fingerprint: %w", err))
				return
			}
			coord.noteDesired(fp)
			if fresh.Entry != cfg.Entry {
				log.Errorf("[org-hotreload] entry identity changed %q -> %q — the resident entry is not hot-migratable; RESTART required to apply (rejected before any candidate build)", cfg.Entry, fresh.Entry)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 入口 entry 由 %q 改为 %q，常驻入口不可热迁，须重启生效（本次未热更）", cfg.Entry, fresh.Entry))
				coord.recordFailure(fmt.Errorf("entry identity changed %q -> %q: resident entry is not hot-migratable, restart required", cfg.Entry, fresh.Entry))
				return
			}
			freshReach := reachableAgents(fresh, cfg.Entry)
			if changed := changedMemoryAgents(fresh, rc.residentMemFP, freshReach); len(changed) > 0 {
				log.Errorf("[org-hotreload] agents.*.memory CHANGED for owner-held agent(s) %v — runtime storage migration is not supported; RESTART required to apply", changed)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: agent %v 的 memory 段变更需重启迁移，本次未热更（须重启生效）", changed))
				coord.recordFailure(fmt.Errorf("memory section changed for %v: restart required", changed))
				return
			}
			if blocked := retiring.closingIn(freshReach); len(blocked) > 0 {
				log.Errorf("[org-hotreload] agent(s) %v re-enter while their retiring owner is already closing — RESTART required (rejected before any candidate build; no second writer opened)", blocked)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: agent %v 正在退役关闭中，同名重入须重启生效（本次未热更，未建第二 writer）", blocked))
				coord.recordFailure(fmt.Errorf("re-entry into closing owner %v: retirement already began, restart required", blocked))
				return
			}
			if coord.sameAsCurrent(fp) {
				hotSnap, hErr := fresh.Clone()
				if hErr != nil {
					log.Errorf("[org-hotreload] numeric-only snapshot clone FAILED — leaving both axes untouched (no half-apply): %v", hErr)
					entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 数值热更快照拷贝失败，保持当前配置（未半换）: %v", hErr))
					coord.recordFailure(fmt.Errorf("numeric-only snapshot clone: %w", hErr))
					return
				}
				applyHotAll(fresh)
				retireUnrouted()
				sweepRetirements()
				if h := orgCommitBarrier.Swap(nil); h != nil {
					(*h)()
				}
				if coord.recordHotApply(hotSnap, appliedFromLastApply) {
					log.Infof("[org-hotreload] numeric-only full apply recorded (revision %d, generation unchanged; rollback ring now restores pre-edit values)", coord.status().Revision)
				}
				return
			}
			failCand := func(site string, err error) {
				msg := site
				if err != nil {
					msg = fmt.Sprintf("%s: %v", site, err)
				}
				log.Errorf("[org-hotreload] %s — serving previous (fail-closed)", msg)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: %s，沿用旧配置（fail-closed）", msg))
				if err != nil {
					coord.recordFailure(fmt.Errorf("%s: %w", site, err))
				} else {
					coord.recordFailure(fmt.Errorf("%s", site))
				}
			}
			ov, ovOK := buildCandidateOwners(rc, loader, fresh, freshReach, failCand)
			if !ovOK {
				return
			}
			defer ov.abandon()
			if len(ov.pendingNames()) > 0 {
				log.Infof("[org-hotreload] hot-added %d agent(s) %v — staged in the private candidate overlay (published only at the single commit point)", len(ov.pendingNames()), ov.pendingNames())
			}
			candCache := ov.resolve()
			snapshot, snapErr := fresh.Clone()
			if snapErr != nil {
				log.Errorf("[org-hotreload] candidate snapshot clone FAILED — serving previous (fail-closed): %v", snapErr)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 候选配置拷贝失败，沿用旧配置（fail-closed）: %v", snapErr))
				coord.recordFailure(fmt.Errorf("candidate snapshot clone: %w", snapErr))
				return
			}
			resolve := candCache
			genNames, staged, genParts, ok := stageOrgGenerations(rc, loader, snapshot, freshReach, resolve, func(site string, err error) {
				msg := site
				if err != nil {
					msg = fmt.Sprintf("%s: %v", site, err)
				}
				log.Errorf("[org-hotreload] %s — serving previous (fail-closed)", msg)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: %s，沿用旧配置（fail-closed）", msg))
				if err != nil {
					coord.recordFailure(fmt.Errorf("%s: %w", site, err))
				} else {
					coord.recordFailure(fmt.Errorf("%s", site))
				}
			})
			if !ok {
				return
			}
			if h := orgCommitBarrier.Swap(nil); h != nil {
				(*h)()
			}
			ov.commit()
			applyHotAll(snapshot)
			activateOwnerGenerations(resolve, genNames, staged, genParts)
			oldFP, gen := coord.swap(fp, snapshot, appliedFromLastApply)
			publishedReach = freshReach
			retireUnrouted()
			sweepRetirements()
			log.Infof("[org-hotreload] executor generation %d swapped (fp %s.. -> %s.., effective next turn; prompt/model/tools rebuilt, cm/bus/projection/registry untouched) — numeric bundle applied to %d resident agent(s)",
				gen.seq, short(oldFP), short(fp), len(rc.resident.Names()))
			entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 热更新已生效（generation %d，fp %s.. → %s..，下回合起用新配置）", gen.seq, short(oldFP), short(fp)))
		}
		var requestCheck func()
		requestCheck = func() {
			if stopped.Load() {
				return
			}
			info, err := os.Stat(cfgPath)
			if err != nil {
				return
			}
			changed := info.ModTime().UnixNano() != atomic.LoadInt64(&lastSeenMtime)
			if !changed && !retiring.hasPending() {
				return
			}
			if !building.CompareAndSwap(false, true) {
				return
			}
			go func() {
				if changed {
					defer building.Store(false)
					reload()
					return
				}
				var recheck bool
				func() {
					defer building.Store(false)
					mu.Lock()
					defer mu.Unlock()
					if stopped.Load() {
						return
					}
					sweepRetirements()
					recheck = retiring.retireablePending(publishedReach)
				}()
				if recheck {
					requestCheck()
				}
			}()
		}
		entryAgent.SetOrgReloader(requestCheck)
		entryAgent.SetOrgReloadSyncCheck(reload)
		poke := requestCheck
		rc.retirementPoke.Store(&poke)
		for _, a := range rc.resident.Snapshot() {
			if a != nil {
				a.ContextManager().SetRetirementPoke(poke)
			}
		}
		entryAgent.ContextManager().SetRetirementPoke(poke)
		entryAgent.RegisterCloser(orgReloadStopper{stop: &stopped, drain: func() { mu.Lock(); mu.Unlock() }})
	}

	entryAgent.RegisterCloser(orgOwnerCloser{rc: rc, self: cfg.Entry})

	if rc.mcpRegistry != nil {
		entryAgent.RegisterCloser(rc.mcpRegistry)
	}

	return entryAgent, nil
}

// stageOrgGenerations is the 3.2-trunk publish core shared by reload and
// rollback: it assembles and STAGES one next generation for EVERY routable
// owner — unchanged parents included (D-a fix: instance and store identity
// never move; only the execution view advances) — and wires the
// generation-level declarations (agent.WireOrgGeneration: face wrappers are
// stamped with their declared child bindings, and each declarer records its
// hold) while NOTHING is activated yet. No transitional shell is constructed
// anywhere: buildAgentFace is pure assembly, wrappers resolve to the stable
// resident instances in `resolve`, and the new declarations reach them
// through the declared generation's own run config (session.go) instead of a
// duplicated agent. The caller owns the commit — barrier → resident merge →
// activateOwnerGenerations (swap in + tracker re-arm) → coordinator record — and
// gets back the deterministic activation order, the staged generations and the
// per-owner assemblies they need. False means the caller's own fail-closed path
// must run (staged generations are already discarded).
func stageOrgGenerations(
	rc *runtimeConfig,
	loader *prompt.Loader,
	next *Config,
	reach map[string]bool,
	resolve map[string]*agent.TagentAgent,
	fail func(site string, err error),
) ([]string, map[string]*agent.StagedGeneration, map[string]*assembledAgent, bool) {
	names := make([]string, 0, len(reach))
	for n := range reach {
		names = append(names, n)
	}
	sort.Strings(names)

	owned := names[:0]
	for _, n := range names {
		if !remoteDeclarationOnly(next, n) {
			owned = append(owned, n)
		}
	}
	names = owned

	staged := make(map[string]*agent.StagedGeneration, len(names))
	newParts := make(map[string]*assembledAgent, len(names))
	for _, name := range names {
		acfg, defined := next.Agents[name]
		if !defined {
			fail(fmt.Sprintf("agent %q is routed but not defined", name), nil)
			return nil, nil, nil, false
		}
		face, parts, err := buildAgentFace(name, acfg, *next, rc, loader, resolve)
		if err != nil {
			fail(fmt.Sprintf("agent %q generation rebuild", name), err)
			return nil, nil, nil, false
		}
		if parts == nil {
			fail(fmt.Sprintf("agent %q generation rebuild produced no assembly", name), nil)
			return nil, nil, nil, false
		}
		newParts[name] = parts
		if parts.actionTool != nil {
			if res := rc.resident.Get(name); res != nil {
				parts.actionTool.SetResidentRecordSink(res.RecordResidentSession)
				if next.ResidentMetaDir != "" {
					parts.actionTool.SetResidentMetaDir(next.ResidentMetaDir)
				}
			}
			if ta := resolve[name]; ta != nil {
				parts.actionTool.SetDefaultTTLSource(spawnerTTLSource(ta))
			}
		}
		ta := resolve[name]
		if ta == nil {
			fail(fmt.Sprintf("agent %q has no resident owner to publish on", name), nil)
			return nil, nil, nil, false
		}
		cm := ta.ContextManager()
		cand := cm.NewExecutorCandidate(face)
		if cand == nil {
			fail(fmt.Sprintf("agent %q candidate construction produced no runner", name), nil)
			for _, st := range staged {
				st.Discard()
			}
			return nil, nil, nil, false
		}
		staged[name] = cm.StageExecutor(cand, face, parts.cfg)
	}

	ownerCMs := make(map[string]*agent.ContextManager, len(resolve))
	for n, ta := range resolve {
		ownerCMs[n] = ta.ContextManager()
	}
	agent.WireOrgGeneration(ownerCMs, staged)
	return names, staged, newParts, true
}

// activateOwnerGenerations is the commit-time half of a publish, shared by
// reload and rollback: it swaps each staged generation in (which retires the
// superseded one) and, immediately after that swap, re-arms every owner's task
// board with the tracker of the ActionTool that generation actually assembled.
//
// The re-arm is not optional polish: `TaskManager`'s session tracker is the
// BOUND method value `actionTool.IsTrackedSession`, so it captures the tool
// instance it was wired from. A face publish assembles a new tool per owner, and
// the double-channel reclaim (orphan adjudication, suspect→running promotion)
// decides from that tracker — left on the superseded tool it answers `false` for
// sessions the live generation is still monitoring, which is how an already-managed
// task would be retired while running (已纳管任务不因工具换代失监视;
// the same rule build_agent's wireAgent documents for shells). It therefore runs
// AFTER activation — a candidate that never commits must not retarget an online
// board at a tool that is about to be discarded — and is idempotent, so an owner
// whose generation carries no exec tool keeps the tracker it already has.
func activateOwnerGenerations(
	resolve map[string]*agent.TagentAgent,
	names []string,
	staged map[string]*agent.StagedGeneration,
	parts map[string]*assembledAgent,
) {
	for _, name := range names {
		ta := resolve[name]
		if ta == nil {
			continue
		}
		ta.ContextManager().ActivateExecutor(staged[name])
		if at := parts[name]; at != nil && at.actionTool != nil {
			if tm := ta.TaskManager(); tm != nil {
				tm.SetSessionTracker(at.actionTool.IsTrackedSession)
			}
		}
	}
}

// orgOwnerCloser closes every resident owner except the entry itself.
// Each owner runs its own close sequence, so an owner whose execution never
// confirmed a stop is reported (and keeps its store-owner registration) instead
// of being force-closed — the sweep inherits the per-owner honesty guarantee
// rather than flattening it. Errors are joined so the host sees WHICH owner is
// still held, and shared state stays protected by the same lease accounting that
// protects it from any other early close.
type orgOwnerCloser struct {
	rc   *runtimeConfig
	self string
}

func (c orgOwnerCloser) Close() error {
	if c.rc == nil || c.rc.resident == nil {
		return nil
	}
	owners := c.rc.resident.Snapshot()
	names := make([]string, 0, len(owners))
	for name := range owners {
		if name != c.self {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var errs []error
	for _, name := range names {
		owner := owners[name]
		if owner == nil {
			continue
		}
		if err := owner.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close resident owner %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// orgReloadStopper halts the D3 org-reload scheduler on agent Close. It flips
// `stopped` (so requestCheck stops scheduling and any not-yet-started reload
// no-ops before touching the cm) and then drains any in-flight build: reload
// holds `mu` for its entire critical section, so acquiring `mu` here proves no
// build is mid-publish — after the drain the cm may be torn down safely. The
// drain is bounded by one build's duration (builds terminate).
type orgReloadStopper struct {
	stop  *atomic.Bool
	drain func()
}

func (s orgReloadStopper) Close() error {
	s.stop.Store(true)
	s.drain()
	return nil
}

// orgBuildBarrier is a TEST-only seam (nil in production). When set, reload()
// invokes it while holding `mu`, so a test can park a background build and prove
// the business acquire path (requestCheck, which never takes `mu`) is not blocked
// by it. Guarded by atomic.Pointer so the test's set/read and reload's read are
// race-free; production loads nil and the branch is a no-op.
var orgBuildBarrier atomic.Pointer[func()]

// orgCommitBarrier is the commit-point twin of orgBuildBarrier (see its use in the
// structural reload): armed by a test, fired at most once, nil in production.
var orgCommitBarrier atomic.Pointer[func()]

// builtinAgentNames are agent names that must always be built via the
// config-driven path. This protects knowledge/recall/action
// from being silently overridden by a registered ToolAgentFactory.
var builtinAgentNames = map[string]bool{
	"knowledge": true,
	"recall":    true,
	"action":    true,
}

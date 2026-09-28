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
	model       model.Model // Default model (can be overridden per-agent)
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

	// configPath (agent-config-hot-reload, incremental A): when set via
	// WithConfigPath, arms a lazy org-config watcher on the entry agent.
	// Empty = disabled (default).
	configPath string

	// retirementPoke carries the §4.3 drain-forward check (the same lazy,
	// single-flight pending drain a business turn rides on) to every owner the
	// assembly builds, so a released usage right continues retirement without
	// waiting for another turn. Set once the reloader closures exist; owners built
	// before that are armed in the same step. Unset = nothing to notify.
	retirementPoke atomic.Pointer[func()]

	// trajectoryRecorder is set when cfg.TrajectoryDump is true.
	// It wraps rc.model, and is registered as a Closer on the entry agent.
	trajectoryRecorder *rl.TrajectoryRecorder

	// evolution：git 原生自进化装配单元（配置门控，默认关）。
	// 文件即真源（热重载直生效）+ git 版本层（commit/revert/log）+ 建议式评估（judge/guardrail
	// 只产 evaluation 事件，P4 框架不动手）。judge/guard 在 buildAgent 经 BindRuntime 延迟绑定。
	evoGit *evolution.GitEvolution

	// approvalChannels（R5）：外部审批送达通道（WithApprovalChannel 注入，govGate 构造后注册）。
	approvalChannels []governance.ApprovalChannel

	// storeOwners：底层 store 指针 → pid → 首个
	// 占名 agent——共享 store 内不同名同 pid 的冲突在构建期 fail-closed。
	storeOwners   map[string]map[int]string
	storeOwnersMu sync.Mutex

	// resident：常驻构建后的 name → agent
	// 绑定表——热更壳子树按 agent 身份借用其常驻资源（store 等），绝不全部复用
	// entryMemStore（防子 agent 存储漂移）。§4.3 起热新增**写入**此表：故它是
	// 共享的 copy-on-write 快照（agent.ResidentTopology），不是可原地改的裸 map。
	resident *agent.ResidentTopology
	// residentMemFP（§4.3，D7）：名字 → 其 memory 段指纹。被移除后重入的同名
	// agent 必须复用原 owner，memory 段变了就拒绝候选——此表是那个判定的依据，
	// 与 resident 表同生命周期（移除不清，拒绝回退才清）。仅 reloader 的 mu 下读写。
	residentMemFP map[string]string

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

	// storeBarriers（§5.8 组合根汇总屏障）：本次 build 已登记的共享 store 屏障集。
	// store 登记时 dedup-Begin（buildAgentDFS），buildAgent 顶层统一 End——覆盖
	// 「清点→Protect→§5.7 核对」全窗口；多 owner 同 store 的登记间隙不再有遗忘
	// 扫描插缝（design L140-141/L234：不靠注册顺序或宽限计时碰巧）。
	storeBarriers   map[memory.RetentionHoldable]struct{}
	storeBarriersMu sync.Mutex
}

// raiseStoreBarrier §5.8：对带恢复租约的共享 store 升登记屏障（幂等 dedup，
// 同一 build 多次登记同一 store 只 Begin 一次）。无租约 store（纯内存，
// 无破坏性扫描）无屏障可升，静默跳过。
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

// releaseStoreBarriers §5.8：释放本 build 持有的全部 store 登记屏障（遗忘
// 恢复放行；lease 层对多余 End 幂等，不会受损）。先摘清集合再锁外 End，
// 避免与扫描器唤醒互锁。可重入：热更壳重建下一轮重新 raise。
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

// Shared persistent stores and their memory engines are owned by the
// RuntimeResources registry (resources.go): one entry per canonical path holds
// the backend store and its same-generation engine, with a lease per consumer
// and the last release closing both. Isolated stores (empty path) bypass the
// registry and are owned exclusively by their agent.

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
// closeResourcesExited reports whether every deferred exit has run. The helper is
// separate so the payload cannot be read as reporting "close finished" from a
// non-nil error alone: §4.1 keeps the FIRST report's error visible while the tail
// finishes independently.
func closeResourcesExited(ta *agent.TagentAgent) bool {
	done, _ := ta.DeferredCloseOutcome()
	return done
}

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

	// evolution：git 原生自进化（配置门控，默认关 → 零行为变化）。
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
	// 4.5：常驻身份绑定表（含 entry 与全部子 agent）。§4.3：此后新增走
	// ResidentTopology.Add 发布新快照，agentCache 本身不再被改动。
	rc.resident = agent.NewResidentTopology(agentCache)
	rc.residentMemFP = make(map[string]string, len(agentCache))
	for n := range agentCache {
		mc := cfg.Agents[n] // 值拷贝：map 元素不可寻址
		rc.residentMemFP[n] = agentMemoryFingerprint(&mc)
	}
	for _, a := range agentCache { // 4.5：全拓扑共享同一绑定表（含自身）
		a.SetResidentTable(rc.resident)
	}
	// §2.3/R01：候选事务回退须撤销本候选登记的每个 owner（含失败父）。把 owner 归属
	// 探针接到装配层的 registry 上，宿主/测试可确定观测在线拓扑之外的 owner 泄漏。
	entryAgent.SetStoreOwnerSnapshot(func() map[string]bool { return rc.ownedAgentNames() })

	// Set the TrajectoryRecorder for session wiring. §6.2: it is deliberately
	// NOT also a plain closer — closeOnce owns its flush-after-runner stop, so
	// registering it twice would double-own (and pre-runner-close) the writeLoop.
	if rc.trajectoryRecorder != nil {
		entryAgent.SetTrajectoryRecorder(rc.trajectoryRecorder)
	}

	// The MCP registry is registered as a closer at the END of New (§4.3/R02): it
	// is process-level and shared by every owner's toolsets, so it must close after
	// the owners that borrow it, not before them.

	// Org-layer config hot reload (agent-config-hot-reload, incremental A).
	// When the config path is known, arm the organization check on the entry
	// agent. Since §3.1 the check fires ONCE per business turn — at the loop's
	// turn boundary (ContextManager.BeginTurn, outside the transport-retry loop)
	// — not before every LLM iteration, so a multi-iteration turn can no longer
	// cross generations. The ops entry (CheckOrgReload/Rollback) calls this same
	// closure. Behavior:
	//   - fingerprint UNCHANGED: numeric-only apply — the committed record is
	//     rotated (hotParamsFor → appliedRecord) and every consumer pulls the new
	//     values at its own safe boundary (§6.4; no value is pushed onto agents).
	//   - fingerprint CHANGED: structural diff (tools/agents/model wiring).
	//     Snapshot rebuild lands in incremental B; until then log loudly
	//     that a restart is required and keep serving (fail-closed).
	if rc.configPath != "" {
		cfgPath := rc.configPath
		var ( // closure-captured reload state
			lastSeenMtime int64       // atomic; -nanos of last processed config mtime
			mu            sync.Mutex  // serializes the heavy reload critical section
			building      atomic.Bool // D3 single-flight: at most one background rebuild
			stopped       atomic.Bool // Close → stop scheduling + drain in-flight build
			// §3.2/D8 usage axis: WHO holds a retired name is derived from the live
			// execution generations' own faces (agent layer = single routing truth);
			// the roster of agents is the assembly's own fact. The ledger only reads.
			retiring = newRetirementLedger(func(name string) int {
				roster := make([]*agent.TagentAgent, 0, len(rc.resident.Snapshot()))
				for _, a := range rc.resident.Snapshot() {
					if a != nil {
						roster = append(roster, a)
					}
				}
				return agent.BindingHolders(name, roster)
			})
			// publishedReach is the reach set of the generation ACTUALLY in force; retirement
			// decisions read only this, never a file some candidate was rejected for.
			publishedReach map[string]bool
		)
		// 第二轮收敛：org 版本簿记
		// （effective 指纹/发布序号/ring-2 回滚快照/最近拒绝原因）集中到单一
		// 协调器，取代此前 lastFP/execGen/prevKeep/prevSnapshot 散点状态。
		coord := newOrgCoordinator()
		// §4.3/R02: retirement reads the reach set of the generation ACTUALLY in force.
		// Re-deriving it from a file a candidate was rejected for would let a refused
		// edit retire owners the online topology still routes.
		publishedReach = reachableAgents(&cfg, cfg.Entry)
		// §5.1（D9）：代际诊断接入现有诊断面——启动期注入提供者（运行期只读），
		// payload 形状由本包拥有。成功与失败同一形状；任何执行路径不读它（唯一
		// 读取点是宿主/运维的诊断接口），因此它不会成为第二真源。
		entryAgent.SetOrgDiagnostics(func() map[string]any {
			st := coord.status()
			payload := map[string]any{
				"generation":  st.Generation,
				"revision":    st.Revision,    // 完整应用计数（含 numeric-only），仅观测
				"fingerprint": st.Fingerprint, // 不透明诊断标签，非业务键
				"desired":     st.Desired,     // 最近一次检查看到的 desired 指纹
				"agents":      st.Agents,      // 逐 agent 应用回执（D9）
				"configPath":  cfgPath,
				// §5.1：实时债务与已提交记录**分组隔开**并自带采集时刻——
				// 上面几字段来自同一次 coord.status() 读取（原子），下面这组是
				// 此刻的引用账，两者不可混为一谈（「不把多次无锁 getter 拼成
				// 原子成功快照」的正向表达）。
				"liveDebt": OrgLiveDebt{
					CapturedAt:         time.Now(),
					Executors:          entryAgent.ContextManager().ExecutorRefs(),
					PendingRetirements: retiring.diagnostics(), // §4.3：仍被自己未完成工作持有的 owner
				},
				// §5.1：关闭「已发起」与「资源已退出」分开呈现（§4.1 的有界返回
				// 不等于收尾完成）。
				"close": OrgCloseState{
					Initiated:       entryAgent.CloseStarted(),
					ResourcesExited: closeResourcesExited(entryAgent),
				},
			}
			if !st.LastApplied.IsZero() {
				payload["lastAppliedAt"] = st.LastApplied // 完整配置最近成功应用时间（含 numeric-only）
			}
			if !st.LastPublished.IsZero() {
				payload["lastPublishedAt"] = st.LastPublished // 最近结构发布/回滚时间
			}
			if st.LastFailure != nil {
				payload["lastFailure"] = st.LastFailure
			}
			return payload
		})
		// hardening-review-batch2 5.1（逐 agent 热更）：hotParamsFor 提取 +
		// agentCache 全遍历——数值热更不得仅覆盖 entry（spec config-hot-reload：
		// 只改子 agent keep_recent_tasks 时该子 agent 压缩器 MUST 收到新值）。
		// 4.7（desired/effective 全量语义）：hotParamsFor 输出**全量 desired**——
		// 显式配置生效，字段删除回落解析默认（entry 8000 / 子 4096；阈值 0.8；
		// keepRecent 2；task terminal 2m / defaultTTL 10m）。消费者侧的「非正值＝无
		// 意见→回落构造值」对正默认透明；TTL reaper 恒开（无负值/禁用哨兵，缺省回落
		// 10m 地板）。提交点只写记录，不写任何消费者（§6.4 pull）。
		hotParamsFor := func(aname string, ac *AgentConfig) agent.OrgHotParams {
			isEntry := aname == cfg.Entry
			defMax := 4096
			if isEntry {
				defMax = 8000
			}
			p := agent.OrgHotParams{
				ThresholdPct:    compress.DefaultCompressThreshold,
				MaxTokens:       defMax,
				KeepRecentTasks: 2,                // agent-layer parsed default (agent/agent.go)
				TaskTerminalTTL: 2 * time.Minute,  // task.defaultTerminalTTL (agent/task)
				TaskDefaultTTL:  10 * time.Minute, // task.defaultManagerTTL (agent/task)
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
		// retireUnrouted records every resident owner the generation in force no longer
		// routes (§4.3/D7) and releases any name that came back before it drained. It does
		// NOT close anything: removal is atomic on the routing side, while the owner stays
		// as long as its own executions, background work or accepted inputs need it.
		retireUnrouted := func() {
			for name, owner := range rc.resident.Snapshot() {
				if name == cfg.Entry || owner == nil {
					continue
				}
				if publishedReach[name] {
					retiring.release(name) // re-routed before draining: the SAME owner is reused
					continue
				}
				retiring.track(name, owner)
			}
		}
		// sweepRetirements retires the owners whose obligations converged: close through the
		// owner's OWN bounded sequence (an unconverged execution keeps it listed instead of
		// being force-closed — §4.1's guarantee, inherited rather than flattened), then
		// revoke its resident registration. Always called under `mu`, the lock a publish
		// holds, so a name is never observable in a half-retired state.
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
		// S-C：applyHotAll 顺产 appliedRecord（routable 全量 desired＋draining 末值），
		// 三提交点（swap/recordHotApply/recordRollback）随各自的 commit 把它并入
		// orgGeneration——记录与版本在同一锁临界区内轮转，读侧 currentHotFor。
		var appliedFromLastApply []appliedAgent
		applyHotAll := func(freshCfg *Config) {
			// §4.3 后的边界收紧：只对**本代可路由**的 agent 下发数值。常驻表自 §4.3
			// 起会保留已移除的 owner（供旧代执行/后台任务/已接受输入收敛），若照旧遍历
			// 整表：被同时删除定义的移除 agent 会拿到零值 AgentConfig → 回落全默认，
			// 使它正在服务的旧代工作被静默重参数化（与 D7「旧 owner 保留到收敛」相悖）。
			routable := reachableAgents(freshCfg, cfg.Entry)
			receipts := make([]OrgAgentApply, 0, len(rc.resident.Names()))
			appliedRecord := make([]appliedAgent, 0, len(rc.resident.Names()))
			for aname, a := range rc.resident.Snapshot() { // §4.3：含热并入的新 agent
				if a == nil {
					continue
				}
				if !routable[aname] {
					receipts = append(receipts, OrgAgentApply{Name: aname, Outcome: "draining"})
					// S-C/J8：Draining 条目携**最后有效值**（owner 当前快照）——记录面读者
					// 可观察「在册但排空中」，且永不被重参数化为默认。
					if last, ok := a.HotSnapshot(); ok {
						appliedRecord = append(appliedRecord, appliedAgent{Name: aname, Hot: last, Draining: true})
					}
					log.Infof("[org-hotreload] agent %q not routable in this generation — params left untouched (draining owner)", aname)
					continue
				}
				src := freshCfg.Agents[aname] // 值类型：hotParamsFor 取址安全（map 内元素不可寻址，拷贝后取）
				p := hotParamsFor(aname, &src)
				// §6.4 pull 终态（S-E）：提交点对热参**不做任何写**——本函数只产出
				// 记录条目。常驻/私有压缩器与 taskManager 都经 owner 源现读记录，
				// 记录一在提交点轮转，就在各自的下一个消费边界生效；不存在「resident
				// setter 被调用而子调用陈旧」这种需要扇出弥补的形态。draining owner 走
				// 上方 skip 分支，其记录条目停在其最后有效值（J8），不被改成默认。
				appliedRecord = append(appliedRecord, appliedAgent{Name: aname, Hot: p})
				// S-C：注入记录源（幂等）——owner 的 HotSnapshot 自此经 currentHotFor
				// 读唯一已提交应用记录；后续任何提交点轮转记录即单写者生效。
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
			sort.Slice(receipts, func(i, j int) bool { return receipts[i].Name < receipts[j].Name }) // 确定性回执序
			sort.Slice(appliedRecord, func(i, j int) bool { return appliedRecord[i].Name < appliedRecord[j].Name })
			coord.recordApply(receipts)
			appliedFromLastApply = appliedRecord
		}
		// hardening-review-batch2 5.5（首代快照）：启动代即 ring-2 的第零代——
		// 否则首次结构热更把上一代存成 nil，Rollback 永远报「无上一代快照」。
		if fp, err := computeOrgFingerprint(&cfg); err == nil {
			// §2.1（私有快照）：启动代也存深拷贝——回滚环上的配置不得与任何
			// 可变容器共享（后续构建只拿到同一份拷贝的引用，防未来原地编辑）。
			if snap, cerr := cfg.Clone(); cerr == nil {
				coord.init(fp, snap)
			} else {
				log.Warnf("[org-hotreload] startup snapshot clone FAILED — Rollback 无首代快照: %v", cerr)
				coord.recordFailure(fmt.Errorf("startup snapshot clone: %w", cerr))
			}
		}
		// §2.4（轮二十五重开缺口，D9）：回滚钩子在**装配期一次性**安装，绝不依赖
		// 任何一次结构发布先行。旧实现把装钩放在结构发布分支内——org 的首次更新
		// 若是 numeric-only，recordHotApply 已把启动代轮进回滚环，但钩子从未装上，
		// Rollback() 静默 no-op（红基线：TestL3_RollbackHookSurvivesNumericOnlyFirstUpdate）。
		// 闭包本身只动态读 coord.rollbackSource()，与安装时机无关，故迁移无语义变化；
		// stopped 闸门（M-1）保留，摘钩语义（SetRollbackFn(nil)）不受影响。
		doRollback := func() {
			mu.Lock()
			defer mu.Unlock()
			// D3/M-1: same gate as reload() — Rollback is invoked through the
			// atomic hook independent of `stopped`, so without this a rollback
			// arriving after Close began draining would PublishExecutor (swap +
			// RetireRunner) onto a cm mid-teardown, retiring a runner/lease after
			// the lifecycle tail already closed it.
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
			// §2.4：回滚与普通热更走同一“取私有快照→候选构造→发布”路径；
			// 发布为新序号，仅影响之后开始的调用（在途 turn 持旧 runner 跑完）。
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
			// §2.4（轮九十二）：回滚**不再有专用重建分支**。§4.3/R02 要求的「已退役
			// owner 必须先重新获取，否则壳按身份借用会落到 entry store（F06 类）与
			// 同名双 writer」这里依然成立，但做法与正向热增**同一个**候选 overlay：
			// 构造期绝不并入常驻表，全部成功才在唯一提交点 commit()，任何后段失败
			// （face 装配／激活）由 abandon() 按获取逆序整体回退——原实现提前 Add 且
			// 不回退，后段失败会把未发布的 owner 留在在线清册里并占住其 store 租约。
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
				return // helper 已逆序回退本轮回滚取得的每个 owner
			}
			defer rbOv.abandon() // 后段任何失败都不半改（提交点后为 no-op）
			// S-D/3.2 主干（轮九十，回滚与正向同型）：发布推进每个可达 owner 的
			// 执行视图，过渡壳消亡；wrapper declared 盖章与声明沿代持有由共享的
			// stageOrgGenerations 在任何激活之前完成（构建→wiring→激活同序）。
			rbNames, rbStaged, rbParts, rbOK := stageOrgGenerations(rc, loader, rollbackC, rbReach, rbOv.resolve(), failRB)
			if !rbOK {
				return // defer → abandon：已建 staged 代与本轮 owner 一并逆序归还
			}
			// 唯一提交点：本轮新增 owner 自此刻对并发读者可见，随后激活
			// （ActivateExecutor 内部换入+retire）＋tracker 重挂——与正向同一个
			// 提交后时机，绝不在候选未提交时把在线看板指向将被丢弃的工具。
			rbOv.commit()
			activateOwnerGenerations(rbOv.resolve(), rbNames, rbStaged, rbParts)
			// §2.4（L-3）：回滚与普通热更同构——executor 重建恢复结构，
			// applyHotAll(rollbackC) 同时把五个热参下发到常驻 agent，二者
			// 缺一不可（只换 executor 会把已排空/存活的旧 owner 数值留在改后
			// 值上）。recordRollback 存回滚后完整 effective 配置，令回滚环的真
			// 源与序号一致。
			applyHotAll(rollbackC)
			rgen := coord.recordRollback(rbp, rollbackC, appliedFromLastApply)
			// 回滚丢掉的 owner 走同一条退役路：回滚环里的配置只是**数据**，不是让
			// 运行实例继续被持有的理由。
			publishedReach = rbReach
			retireUnrouted()
			sweepRetirements()
			log.Infof("[org-hotreload] executor generation %d rolled back to fp %s.. (structure + hot params restored via the same transaction)", rgen.seq, short(rbp))
		}
		entryAgent.SetRollbackFn(doRollback)
		// reload 是完整同步的检查→构建→发布，全程在 `mu` 下（唯一重活临界区）。
		// 它同时是后台构建线程（requestCheck 派生）与 ops 同步入口
		// （SetOrgReloadSyncCheck）执行的同一个 reload——单一生效路径，无第二真源。
		// D3（§2.3）：业务 turn 只经 requestCheck 触发，绝不在其线程跑这段重活。
		reload := func() {
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
			// D3：Close 已开始（stopped）则绝不把候选发布到正在拆除的 cm 上。
			if stopped.Load() {
				return
			}
			// 测试可控屏障：停在 mu 之下，用于证明业务获取路径（requestCheck 不碰
			// mu）不被长构建阻塞。生产恒为 nil，零成本。
			if h := orgBuildBarrier.Load(); h != nil {
				(*h)()
			}
			if info2, err2 := os.Stat(cfgPath); err2 == nil {
				if info2.ModTime().UnixNano() == atomic.LoadInt64(&lastSeenMtime) {
					return // re-check under lock
				}
				atomic.StoreInt64(&lastSeenMtime, info2.ModTime().UnixNano())
			}
			// 本轮检查开始：desired 尚未可知（解析失败时就应该报空，不能沿用上轮残留）。
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
			// §5.1（D9）：算出 desired 指纹后立即登记——后面的拒绝都能说出
			// “被拒的是哪一份”，与 effective 指纹不等就是“改了没生效”的直接证据。
			coord.noteDesired(fp)
			// R04／L-5（resident-continuity「入口身份变更在资源获取前拒绝」）：entry
			// 是常驻入口身份，热更不热迁它。改名（无论旧定义保留还是删除）都在任何
			// 候选资源构建**之前**拒绝——后续 reachableAgents / buildAgent 一律以启动
			// 时 cfg.Entry 为基准，若放任改名，fresh.Agents[cfg.Entry] 会是零配置或指向
			// 已不是当前入口的旧定义，导致按空配置发布丢掉委派工具、或把重命名当成一次
			// 生效的结构热更（二者都是审阅发现的真实行为）。序号、effective、owner 全不变。
			if fresh.Entry != cfg.Entry {
				log.Errorf("[org-hotreload] entry identity changed %q -> %q — the resident entry is not hot-migratable; RESTART required to apply (rejected before any candidate build)", cfg.Entry, fresh.Entry)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 入口 entry 由 %q 改为 %q，常驻入口不可热迁，须重启生效（本次未热更）", cfg.Entry, fresh.Entry))
				coord.recordFailure(fmt.Errorf("entry identity changed %q -> %q: resident entry is not hot-migratable, restart required", cfg.Entry, fresh.Entry))
				return
			}
			// §4.3（D7）子树热增删与 memory 先检共用的判定域：本代从 entry 实际可达的
			// agent 集合。必须在 memory 先检**之前**算出——不可达的名字不进入本代执行。
			freshReach := reachableAgents(fresh, cfg.Entry)
			// R4 3.1（🔴5）memory 先序：org 指纹比对**之前**逐 agent diff memory 段——
			// 命中即拒绝热更并明示须重启（fail-closed；不依赖 fingerprint 变化路径）。
			// §4.3 精化：只对**已有 owner 且本代可达**的名字判定（审阅 H-1：定义仍在但
			// 已被摘路由的名字不会在本代被构造，拒它无第二 writer 可防，却会永久冻结整条
			// 热更路）。新增 agent 自带 memory 段不构成“运行时存储迁移”——它是全新 owner，
			// 若路径与已有 agent 相同则由 acquire 共用同一实例并受 pid 冲突校验保护；因此
			// 不得因它误拒，否则热新增永不可达。粘性未削：同名重入时它回到判定域、旧 fp
			// 仍作基准。基准只在启动播种与真正新增时前进（已有 resident 名字不收敛），
			// 所以旧 fp 始终作为基准：同一未生效配置再编其他字段仍以 effective 拒绝。
			if changed := changedMemoryAgents(fresh, rc.residentMemFP, freshReach); len(changed) > 0 {
				log.Errorf("[org-hotreload] agents.*.memory CHANGED for owner-held agent(s) %v — runtime storage migration is not supported; RESTART required to apply", changed)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: agent %v 的 memory 段变更需重启迁移，本次未热更（须重启生效）", changed))
				coord.recordFailure(fmt.Errorf("memory section changed for %v: restart required", changed))
				return
			}
			// D7/§4.3: a name this generation wants may be on the retirement list with
			// its owner ALREADY closing. Reusing it is then impossible, and admitting a
			// fresh owner would open a second writer for the same storage identity, so the
			// candidate is refused BEFORE any resource is built and the online topology
			// stays intact. Ordering matters: this gate must run before the drain sweep,
			// or the owner this very reload is bringing back would be retired first and
			// the gate could never fire.
			if blocked := retiring.closingIn(freshReach); len(blocked) > 0 {
				log.Errorf("[org-hotreload] agent(s) %v re-enter while their retiring owner is already closing — RESTART required (rejected before any candidate build; no second writer opened)", blocked)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: agent %v 正在退役关闭中，同名重入须重启生效（本次未热更，未建第二 writer）", blocked))
				coord.recordFailure(fmt.Errorf("re-entry into closing owner %v: retirement already began, restart required", blocked))
				return
			}
			if coord.sameAsCurrent(fp) {
				// org structure unchanged: hot-apply the numeric bundle to
				// EVERY built agent (5.1 逐 agent) — per-agent effective 回执。
				// §2.4（L-3）：numeric-only 成功也是一次「完整应用」。先取私有快照
				// （失败按「失败不半换」——不下发不登记），再下发，再登记完整 effective
				// 配置：轮转回滚环使后续 Rollback 能恢复改前数值，推进 revision 与
				// lastAppliedAt，但不推进结构 generation/lastPublishedAt。语义完全相同
				// 的重存由 recordHotApply 的签名比较识别，不轮转。
				hotSnap, hErr := fresh.Clone()
				if hErr != nil {
					log.Errorf("[org-hotreload] numeric-only snapshot clone FAILED — leaving both axes untouched (no half-apply): %v", hErr)
					entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 数值热更快照拷贝失败，保持当前配置（未半换）: %v", hErr))
					coord.recordFailure(fmt.Errorf("numeric-only snapshot clone: %w", hErr))
					return
				}
				applyHotAll(fresh)
				// §4.3/R02: an owner whose work settled in an earlier generation is retired
				// at the NEXT ACTIVITY, not only at the next publish — draining must not
				// depend on this boundary also changing the structure.
				retireUnrouted()
				sweepRetirements()
				// S-C commit-point barrier (TEST-only, nil in production): the numeric-only
				// path is the same single-commit-gate story as the structural swap — the
				// applied record rotates only inside recordHotApply below, in this one `mu`
				// critical section. Park here so a test can observe the record still
				// serving the PREVIOUS commit: since S-E nothing is pushed to any
				// consumer at all, so every reader (owner hot view AND the compressor/
				// taskManager that pull through it) shows one whole generation, never a
				// half-commit.
				if h := orgCommitBarrier.Swap(nil); h != nil {
					(*h)()
				}
				if coord.recordHotApply(hotSnap, appliedFromLastApply) {
					log.Infof("[org-hotreload] numeric-only full apply recorded (revision %d, generation unchanged; rollback ring now restores pre-edit values)", coord.status().Revision)
				}
				return
			}
			// §4.3（D7）+ introduce-durable-workflow-engine 2.3（S-B 有序责任表）／
			// 2.4（轮九十二）：候选以**私有 overlay** 构造（`buildCandidateOwners`），
			// 循环内绝不并入常驻表，全部成功才在唯一提交点一次 Add；未发布即由
			// abandon() 按获取逆序整体回退。reload 与 rollback 走同一函数——回滚不
			// 再保留自己的重建分支，后段失败因此与正向同构地不半改。
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
				return // 已在 helper 内逆序回退，在线面未动
			}
			defer ov.abandon() // 后段任何失败都不遗留（提交点后为 no-op）
			if len(ov.pendingNames()) > 0 {
				log.Infof("[org-hotreload] hot-added %d agent(s) %v — staged in the private candidate overlay (published only at the single commit point)", len(ov.pendingNames()), ov.pendingNames())
			}
			candCache := ov.resolve() // 候选域＝在线快照∪本候选新增
			// R4（resident-continuity-r2-r4 3.5/3.8）+ introduce-durable-workflow-engine
			// §2.1（候选构造与发布分离）：结构变化→先拿私有快照，再构候选（不
			// Swap），任一步失败都可弃掉而有效 runner/在线工具表未动；全部成功后
			// 才在常驻 cm 上换入新代（下一 turn 生效；in-flight turn 用旧 runner 跑完）。
			snapshot, snapErr := fresh.Clone()
			if snapErr != nil {
				log.Errorf("[org-hotreload] candidate snapshot clone FAILED — serving previous (fail-closed): %v", snapErr)
				entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 候选配置拷贝失败，沿用旧配置（fail-closed）: %v", snapErr))
				coord.recordFailure(fmt.Errorf("candidate snapshot clone: %w", snapErr))
				return
			}
			// S-D/3.2 主干（轮九十）：发布推进**每个**可达 owner 的执行视图——
			// 未变父也换 face（D-a 修复：实例与 store 身份不动）；wrapper 由
			// agent.WireOrgGeneration 盖 declared（指向被声明子的同代 binding）、
			// 声明沿代持有（heldBy 不入义务轴）；委派调用期按发起代取该代装配
			// 配置（session.go）——过渡壳（为已变 agent 再造整实例承载新配置）自此消亡。
			// 构建域＝候选 overlay（在线快照∪本候选新增），零额外构造。
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
				return // defer → txn.discard() 逆序回退：成功依赖 + 失败父登记
			}
			// 唯一线性化点：候选私有 overlay 的新增 owner 并入常驻表，与各 owner
			// 的换代激活、版本层记录在同一 `mu` 临界区内一次完成（R01/D3
			// 「统一短提交」）；此前它们一直只在 candCache/added 里，对并发读不可见。
			// orgCommitBarrier (TEST-only, nil in production) parks exactly at the commit
			// point, so a test can observe the private overlay BEFORE the merge and prove
			// §4.3's first clause: a hot-added name is visible nowhere — not in the
			// resident table, not on any published face — until this one critical section.
			if h := orgCommitBarrier.Swap(nil); h != nil {
				(*h)()
			}
			ov.commit() // 唯一提交点：本候选新增的 owner 自此对并发读者可见
			// hardening-review-batch2 5.1/5.4（热更统一应用）＋§5.1 轮一百零三
			// 次序纠正：本调用原先跑在 commit **之前**，于是候选新增的 owner 还不在
			// 常驻表里——它既拿不到回执（诊断面漏掉的正是本轮刚装上的消费源），也要
			// 等到下一次 numeric-only 才被接上记录源。移到提交之后、激活与 swap 之前：
			// 回执/applied 记录覆盖本代全部可路由 owner，且记录随同一次 swap 轮转。
			applyHotAll(snapshot)
			// 激活＋tracker 重挂同在一个提交后时机（3.3「已纳管任务不因工具换代
			// 失监视」）：换代装配的是新 ActionTool，管理器持有的是它的绑定方法值。
			activateOwnerGenerations(resolve, genNames, staged, genParts)
			oldFP, gen := coord.swap(fp, snapshot, appliedFromLastApply)
			// §4.3/R02：新的有效可达集成立即「移除即摘路由」已达成；此刻把本代不再路由的
			// owner 记入退役账并 sweep——义务已消失者当场退役，仍被依赖者留待后续活动
			// 边界，绝不因「配置里还留着这个名字」而永久保留运行实例。
			publishedReach = freshReach
			retireUnrouted()
			sweepRetirements()
			log.Infof("[org-hotreload] executor generation %d swapped (fp %s.. -> %s.., effective next turn; prompt/model/tools rebuilt, cm/bus/projection/registry untouched) — numeric bundle applied to %d resident agent(s)",
				gen.seq, short(oldFP), short(fp), len(rc.resident.Names()))
			entryAgent.EmitSystemAlert(fmt.Sprintf("org-hotreload: 热更新已生效（generation %d，fp %s.. → %s..，下回合起用新配置）", gen.seq, short(oldFP), short(fp)))
			// §2.4（轮二十七）：回滚钩子不在此安装——旧实现只在结构发布分支装钩，
			// 「启动后仅 numeric-only 更新」的 org 环已轮转（recordHotApply）却无钩子，
			// Rollback() 静默 no-op，违反 D9。改由装配期一次性安装（见 doRollback）。
		}
		// requestCheck：业务 turn 的 D3 非阻塞懒触发。只做一次 stat 比较，绝不
		// 拿 `mu`（故永不被构建/回滚/Close 屏障阻塞）。发现变化时以 building CAS
		// 单飞，至多派生一个后台 reload，多余请求被合并（mtime 守卫令重复成无操作）。
		// 声明为具名变量而非字面量：排空尾巴需要「本趟结束后再自觉一次」这条自召回
		// （§4.3 级联收敛），字面量自引用不合法。
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
				return // hot path: nothing changed and nothing is still draining
			}
			if !building.CompareAndSwap(false, true) {
				return // single-flight: a rebuild is already in progress; this request merges into it
			}
			go func() {
				if changed {
					defer building.Store(false)
					reload()
					return
				}
				// Only pending retirements: drain under the same `mu` a publish holds. This
				// reuses the existing activity-driven boundary instead of adding a timer
				// goroutine that would outlive the work it watches.
				var recheck bool
				func() {
					// Release the single-flight gate before deciding whether to re-arm:
					// a release that landed WHILE this pass ran was merged away above,
					// so a cascade (owner A exits → the name A was holding becomes free)
					// would otherwise wait for traffic that may never come (§4.3 「不必须
					// 再来一个业务 turn」). Defers run LIFO: `mu` drops, then the gate.
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
		// 懒触发（业务）与同步入口（ops）共用同一个 reload——发布/回退只有一条生效路。
		entryAgent.SetOrgReloader(requestCheck)
		entryAgent.SetOrgReloadSyncCheck(reload)
		// §4.3「释放使用权…经原生命周期轻量通知继续退役，不必须再来一个业务 turn」：
		// 把这条懒检查交给装配袋（其后在 buildAgent 里成形的 owner，含热增者，自臂），
		// 并就地臂上此刻已存活的全部 owner。不新增计时 goroutine——复用的正是
		// 业务 turn 走的那条单飞懒检查路径。
		poke := requestCheck
		rc.retirementPoke.Store(&poke)
		for _, a := range rc.resident.Snapshot() {
			if a != nil {
				a.ContextManager().SetRetirementPoke(poke)
			}
		}
		entryAgent.ContextManager().SetRetirementPoke(poke)
		// Close：先停调度，再排空在途构建（reload 全程持 `mu`，取到锁即证明没有构建
		// 正处在发布中）。构建有限时长，排空有界。
		entryAgent.RegisterCloser(orgReloadStopper{stop: &stopped, drain: func() { mu.Lock(); mu.Unlock() }})
	}

	// §4.3/R02 (spec「组织关闭覆盖热新增 owner」): the organization built every
	// resident owner, so its Close must reach all of them — the entry's own sequence
	// never did. The set is read AT CLOSE time from the live topology, which is what
	// makes owners adopted by the hot path part of the sweep.
	//
	// Registration ORDER is the dependency order, and it is deliberate:
	//   - after the reload stopper, which has already refused new scheduling and
	//     drained any build in flight — otherwise a publish could Add an owner right
	//     after the snapshot and escape the sweep forever;
	//   - before the process-level shared services below, so every owner closes
	//     while what it borrows is still alive.
	entryAgent.RegisterCloser(orgOwnerCloser{rc: rc, self: cfg.Entry})

	// Register the MCP registry for graceful shutdown — closes all MCP
	// toolset connections once at process exit (registry is process-level,
	// shared across agents). Registered AFTER the owner sweep (§4.3/R02): the
	// registry is shared by the owners' toolsets, so it must not disappear under
	// an owner that is still draining its in-flight calls.
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
	sort.Strings(names) // deterministic build, wiring, and activation order

	// A remote-declared-only name is genuinely reachable (the entry really can
	// delegate to it) but has no owner and no generation of its own — publish the
	// owners, skip the declaration. Without this the same config that loads cold
	// would fail its FIRST hot reload closed forever (§5.44's single predicate was
	// honoured by validation and the builder but not by this publish loop).
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
		// 去壳后面上 ActionTool 的接线归各 owner 自己（entry 单点逻辑的推广；
		// J7：spawner TTL 源读所属 agent 自己的记录投影，不到祖先）。工厂分支
		// 产配置不产句柄，actionTool 恒 nil——同一条规则，无需特判（轮九十一合同）。
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
// §7's double-channel reclaim (orphan adjudication, suspect→running promotion)
// decides from that tracker — left on the superseded tool it answers `false` for
// sessions the live generation is still monitoring, which is how an already-managed
// task would be retired while running (3.3「已纳管任务不因工具换代失监视」;
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

// orgOwnerCloser closes every resident owner except the entry itself (§4.3/R02).
// Each owner runs its own close sequence, so an owner whose execution never
// confirmed a stop is reported (and keeps its store-owner registration) instead
// of being force-closed — the sweep inherits the per-owner honesty guarantee
// rather than flattening it. Errors are joined so the host sees WHICH owner is
// still held, and shared state stays protected by the same lease accounting that
// protects it from any other early close.
type orgOwnerCloser struct {
	rc   *runtimeConfig
	self string // the entry name: its Close is the caller's own sequence
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
	sort.Strings(names) // deterministic shutdown order and deterministic error text
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

// buildAgent recursively creates a TagentAgent for the given agent name.
// It resolves tools by looking up referenced agents in the Config.Agents map.

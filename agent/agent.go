// Package agent provides tagent's core agent mechanism coordination.
//
// TagentAgent wires together:
//   - EventBus + AgentLoop (event-driven execution engine)
//   - Runner (framework orchestration with plugins, retained for session/plugin lifecycle)
//   - MemoryPlugin (OnEvent: event persistence + causal chain)
//   - Preprocessor (event filtering, token budget, SmartCompress)
//
// Core principle: AgentLoop is a pure event-driven engine with no business semantics.
// All domain decisions (event filtering, shouldCallModel, compression) live in Preprocessor.
//
// TagentAgent implements agent.Agent, so it can be wrapped as agent.Tool
// for tool-agent composition.
//
// Top-level usage: StartLoop / InjectMessage / StopLoop (persistent event loop only).
// Sub-agent usage: agent.Run() via AgentToolWrapper.Call() (invoked by parent LLM).
//
// NOTE: This package does NOT depend on tagent/tool.
// Application-level wiring (KnowledgeAgent assembly, WireActionTool, etc.)
// lives in the root tagent package.
package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/workspace"
)

// Closer is implemented by components that hold resources requiring cleanup
// on agent shutdown (e.g., ActionTool stops its TmuxMonitor).
// Using an interface avoids a direct dependency on tagent/tool.
type Closer interface {
	Close() error
}

// Verify TagentAgent implements agent.Agent at compile time.
var _ agent.Agent = (*TagentAgent)(nil)

// TagentAgent is tagent's top-level Agent assembly.
// It implements agent.Agent so it can be used both as a standalone agent
// and as a tool-agent (wrapped via AgentToolWrapper).
//
// In the event-driven architecture, TagentAgent owns an EventBus and
// AgentLoop. External inputs (user messages, tmux callbacks, meditation)
// are published to the bus; the AgentLoop consumes them, calls the model
// via Preprocessor, and dispatches tool_use events asynchronously.
type TagentAgent struct {
	// droppedOutputEvents counts SessionHook deliveries skipped because the
	// outputCh was full at try-time (F2 observability, design-report-closeout).
	// Pointer: the SessionHook closure is registered before ta is built and
	// shares the counter.
	droppedOutputEvents *atomic.Int64

	// activeBus is the single event bus for this agent, regardless of
	// whether it is running in persistent loop mode (StartLoop) or
	// sub-agent invocation mode (Run). Tools (e.g., ActionTool via
	// TmuxMonitor callbacks) publish to this bus via InjectMessage.
	// StartLoop sets it to ta.persistentBus; Run() sets it to invBus.
	activeBus   *EventBus
	activeBusMu sync.Mutex

	// persistentBus is the bus created at construction time, used by
	// the persistent AgentLoop started via StartLoop.
	persistentBus  *EventBus
	contextManager *ContextManager

	// hotSource (§6.4/2.3)：owner 热参的**唯一**读源闭包。构造期由 NewTagentAgent
	// 恒装静态源（design §2「不存在无源状态」）；组合根在协调器就绪后换成记录绑定
	// 源（读唯一已提交应用记录），此后每次提交点轮转记录即单写者生效。
	// compressor / taskManager / 私有 CM 全部经它现读——不再有第二份可写缓存。
	hotSource atomic.Pointer[func() (OrgHotParams, bool)]

	// liveCMs (§6.4/D4)：随调用生命周期注册/注销的**存活调用私有 CM**集合
	// （绝非历史对象列表）。§6.4 pull 反转后提交点不再向它扇出任何值——私有 CM
	// 的压缩器绑本 agent 的热参源，在下一次压缩/预算读取边界现读记录。本集合目前
	// **无生产读取方**（只有注册/注销与 LiveCMCount／snapshotLiveCMs 读面，后者已按
	// §5.1 收为包内——向外暴露内部 CM 切片没有生产价值）：其存续理由
	// 待 S-D 按 D4「若保留只服务取消/完成等待，归唯一 owner」裁定。有界：调用结束即注销。
	liveCMsMu sync.Mutex
	liveCMs   map[*ContextManager]struct{}

	// taskManager owns async task lifecycle. Tools spawn via the injected
	// task.TaskSpawner; background settles are published back to persistentBus as
	// task_settled events by its OnSettle hook.
	taskManager *task.TaskManager

	// settleSinks (S3m-a, M2越窗 routing): per-invocation settle sinks keyed by the
	// delegation invocation_id (S2m Origin handle). The OnSettle hook routes a
	// background task_settled to the owning sub-invocation loop's sink when one is
	// registered; with no sink (entry owner + every pre-S3m-b path) it falls back
	// to persistentBus, so this is behavior-neutral until S3m-b registers a sink.
	settleSinks *settleSinkRegistry

	// orgRollback（R4，resident-continuity-r2-r4 3.8）：热更回滚钩子（tagent
	// 包懒检查闭包注入；Rollback() 触发）。它**每次成功发布都被重写**（运行期写），
	// 而宿主/运维可从另一个 goroutine 调 Rollback()——故用 atomic.Pointer，
	// 不得换成裸字段。
	orgRollback atomic.Pointer[func()]

	// orgDiags（D9，introduce-durable-workflow-engine 5.1）：装配层注入的编排代际
	// 诊断提供者。形状由装配层拥有（本包只见 map[string]any），**仅诊断面读取**——
	// 无任何执行路径据它选版（resident-continuity：指纹/序号不是应用可见 identity）。
	// 与 orgRollback **不同规**：它只在启动期注入一次（tagent.go 的 configPath 接线块），
	// 运行期只读，所以普通字段就够；不要拿 orgRollback 当参照。
	orgDiags func() map[string]any

	// storeOwnerSnapshot (§2.3/R01):装配层注入的「当前持有 store owner 登记的 agent
	// 名集」只读探针。候选事务回退须撤销本候选登记的每个 owner（含失败父），该探针
	// 让宿主/测试能确定性地观测在线拓扑之外的 owner 归属；纯内省，无执行路径读它。
	// 与 orgDiags 同规：启动期注入一次，运行期只读。
	storeOwnerSnapshot func() map[string]bool

	// storeOwnerRevoke (§4.3/R02): the assembly hook that drops THIS agent's
	// store-owner registration. Called by closeOnce only after the store exit was
	// really taken; an unconverged close keeps the registration (the holder may
	// still write). Set at build time, read-only afterwards.
	storeOwnerRevoke func()

	// Framework integration
	memStore memory.MemoryStore
	// memStoreRelease (resident-readiness-plan 4.2): lease release bound to
	// THIS agent's lifecycle — executed from Close; nil for borrowed/shell
	// agents and for isolated stores the agent fully owns via its own Close.
	// Returns the release's close error (last-lease close reaches Close, D5).
	memStoreRelease func() error
	// memStoreOwned (§6.3/review M-2): this agent is the SOLE close owner of
	// memStore (isolated build) — the direct-Close fallback may fire when no
	// lease exists. Borrowed shells and leased holders are false: shared state
	// exits only through the lease release, never through them.
	memStoreOwned bool

	// resident (4.5, §4.3): the SHARED immutable-snapshot binding table —
	// name → resident agent instance, which the hot-reload shells borrow
	// per-agent resources from and which hot ADD publishes into. Held by
	// pointer so one publish reaches every agent; readers never see a torn map.
	resident   *ResidentTopology
	memPlugin  *plugin.MemoryPlugin // registered on ContextManager's Runner
	config     *TagentConfig
	sessionSvc session.Service

	// Agent identity (for agent.Agent interface)
	name        string
	description string

	// Session context for event injection (set on first Run)
	sessionMu     sync.Mutex
	lastUserID    string
	lastSessionID string

	// External events pending ingestion (legacy direct-Ingest API single-handoff
	// slot): set via IngestExternalEvents, drained atomically by the next Run at
	// entry (§7.1 D2). It is NOT a history buffer and the primary delegation path
	// (RuntimeState) never writes it; the mutex keeps a concurrent Run's drain from
	// tearing against a set.
	externalEventsMu      sync.Mutex
	pendingExternalEvents []memory.FullEvent

	// Resource closers — components like ActionTool that need cleanup on shutdown.
	// Closed in Close() before the runner is stopped.
	closers []Closer

	// TrajectoryRecorder (optional) — records LLM calls to JSONL when enabled.
	// Set via SetTrajectoryRecorder. StartLoop calls SetSessionInfo on it.
	trajectoryRecorder *rl.TrajectoryRecorder

	// Persistent Event Loop — 持久事件循环（StartLoop 模式）。§6.1：Start/Stop/Close
	// 由同一显式状态机协调（loopIdle→loopRunning→loopStopping→loopClosed），
	// 首次关闭执行、其余等待同一完成结果（不因标志已翻跳过等待）；在途计数
	// （loopWg.Add）先于发布 running 登记；输出通道在循环 goroutine 退出（含
	// panic 路径）时恰好关闭一次，从未启动则由 Close 落定终态（V15 终结语义不变）。
	outputCh     chan *event.Event  // 持久输出 channel（经 settleOutput 恰关闭一次）
	loopCtx      context.Context    // Loop context（StopLoop 取消）
	loopCancel   context.CancelFunc // Loop cancel
	loopState    atomic.Int32       // §6.1 生命周期状态机（loopIdle/Running/Stopping/Closed）
	loopDone     chan struct{}      // 循环终态：由 settleOutput 与 outputCh 同批关闭；Stop/Close 等同一终态
	loopWg       sync.WaitGroup     // 等待 Loop goroutine 退出
	outputSettle sync.Once          // review C-1/M-1：outputCh+loopDone 的结算唯一入口（结构上恰一次）
	closeMu      sync.Mutex         // §6.1 关闭协调：首次 Close 执行，并发者等待同一结果
	closeStarted bool
	closeDone    chan struct{}
	closeErr     error
	// §4.1 terminal tail: a bounded Close that could not honestly finish carries
	// the remainder (still-used tool closers, recorder, store exit and its owner
	// registration) in exactly ONE continuation, armed on the manager's own
	// reclaim event and run when the executions actually stop. Guarded by closeMu;
	// closeTailOnce makes the exit itself exactly-once.
	closeTail     func()
	closeTailOnce sync.Once
	closeTailDone bool
	closeTailErr  error

	// Meditation manager — started/stopped with the persistent event loop.
	meditationMgr *MeditationManager

	// degradation 是五依赖退化状态机（T-G，报告 D3）。可选（nil=未启用）：event_loop 在
	// RunFlow 失败/成功时上报 DepModel；ErrorTrackingStore（存储栈最外层）上报 memory/disk/rustviking。
	degradation *reliability.DegradationManager

	// cleanupCancel stops the workspace cleaner goroutine (started in NewTagentAgent).
	cleanupCancel context.CancelFunc
	// cleanupDone closes when the cleaner goroutine has returned. Cancelling is
	// only a request; this is the confirmation the close sequence waits on
	// (§4.3/R02: a retired owner must not leave its maintenance producer running).
	cleanupDone chan struct{}

	// projection is the lightweight, bounded Session projection (EventReference[])
	// shared by onEvent and Preprocessor. It is created per TagentAgent and
	// passed to each invocation's AgentLoop.
	projection *compress.SessionProjection
}

// TagentConfig holds configuration for creating a TagentAgent.
type TagentConfig struct {
	Model       model.Model        // Required: LLM model
	MemoryStore memory.MemoryStore // Optional: external MemoryStore (default: InMemoryStore)
	// MemStoreRelease (resident-readiness-plan 4.2): lease release for the
	// provided MemoryStore; executed exactly once from Close. nil = the agent
	// does not own a registry lease.
	MemStoreRelease func() error
	// MemStoreBorrowed (§6.3/review M-2) marks an executor shell that BORROWS
	// the resident shared store without a lease: it holds no close right over
	// shared state at all — the direct-Close fallback must not fire. Default
	// false = the agent owns its (isolated) store exclusively and Close flushes
	// it via the fallback tail.
	MemStoreBorrowed   bool
	SessionSvc         session.Service // R4（review 🔴1）：外部 SessionSvc 注入（executorOnly 热重建壳复用常驻实例；nil=内部新建）
	SystemPrompt       string          // System prompt loaded from AGENTS.md/SOUL.md/USER.md/TOOLS.md
	SystemPromptSource prompt.Getter   // Hot-reloadable system prompt (optional, overrides SystemPrompt); Getter 接口（文件即真源，mtime 热重载）
	Tools              []tool.Tool     // CallableTools to register
	MaxToolIterations  int             // Default: DefaultMaxToolIterations (50)
	MaxTokens          int             // Token budget for context (default: 8000)
	CompressThreshold  float64         // Compression trigger threshold (default: 0.8)
	SummaryModel       model.Model     // Optional: for Stage 2 LLM summary
	SummaryEffort      string          // Optional: reasoning_effort for summary calls (tagent-unify-model-call-config)
	Temperature        float64         // Optional: LLM temperature (default: 0.7)
	KeepRecentTasks    int             // Min task segments to keep during compression (default: 2)
	Compress           CompressConfig  // compress.SmartCompressor parameters

	// TaskTerminalTTL is the grace period an exited task (completed/failed/
	// cancelled/dead) is retained before pruning. It bounds the resume_task
	// window for terminal subagent tasks (task-chain restorer). Non-positive
	// falls back to the task package default (2m).
	TaskTerminalTTL time.Duration

	// TaskDefaultTTL is the unified reaper's fallback absolute lifetime for
	// spawns whose model-side `ttl` is unset (async-task-lifetime 10.5). Zero →
	// task package default (10m). There is no disable path — no task is immortal.
	TaskDefaultTTL time.Duration

	// Thinking/reasoning controls (merged into model.GenerationConfig)
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string

	// Agent identity (for agent.Agent interface)
	Name        string // Default: "tagent"
	Description string // Default: "TagentAgent - AI assistant powered by tagent"

	// Meditation configures the meditation/heartbeat mechanism.
	Meditation MeditationConfig

	// Degradation 是五依赖退化状态机（T-G，可选）。非 nil 时 event_loop 上报 model 依赖、
	// ErrorTrackingStore（wireMemoryEngine 最外层）上报 memory/disk/rustviking。nil=不启用（现状）。
	Degradation *reliability.DegradationManager

	// DegradationBehaviors（5.4 design-report-closeout）是依赖退化的行为响应层（警告级，
	// 每项独立、默认零值=全部关闭，仅 Degradation 非 nil 时生效）。闸不是墙：行为只是
	// 免打已确认故障的依赖，上报恢复即回正常路径。
	DegradationBehaviors DegradationBehaviors

	// WorkspaceRoot is the unified on-disk scratch root (default: .tagent-workspace).
	// Oversized tool outputs go to <root>/tool-output; the tmux command working
	// directory is <root>/exec. A periodic cleaner bounds tool-output files.
	WorkspaceRoot string

	// Workspace cleanup (periodic, tool-output dir only). Non-positive values
	// fall back to the defaults below (there is no disable switch).
	WorkspaceCleanupInterval time.Duration // default: 1h
	WorkspaceCleanupMaxAge   time.Duration // default: 24h
	WorkspaceCleanupMaxFiles int           // default: 200

	// BusSpillDir 是事件总线磁盘溢出目录（T-G ReliableBus）。非空时 channel 满则事件溢出
	// 落盘而非丢弃（at-least-once，常驻不丢事件），重启后未消费项可回收；空 = 纯 channel（现状）。
	BusSpillDir string
}

// DegradationBehaviors (5.4, design-report-closeout) configures the
// behavior-level responses to dependency degradation. All fields are
// independent; zero values disable each behavior (zero behavior change).
// Only effective when Degradation is wired; “a gate, not a wall”.
type DegradationBehaviors struct {
	// ModelBackoff pauses the loop between turns while DepModel is degraded.
	ModelBackoff time.Duration
	// MCPProbeEvery circuit-breaks mcp_call while DepMCP is degraded and
	// lets 1 real probe through every N calls (half-open recovery).
	MCPProbeEvery int
	// DiskBlockSpawn rejects NEW task spawns while DepDisk is degraded
	// (in-flight tasks are unaffected).
	DiskBlockSpawn bool
}

// Default configuration values
const (
	DefaultMaxToolIterations         = 50
	DefaultSubAgentMaxToolIterations = 10
	DefaultAgentName                 = "tagent"
	DefaultAgentDescription          = "TagentAgent - AI assistant powered by tagent"

	// DefaultResumeContextRounds caps the rounds restored by the subagent
	// task-chain restorer on resume.
	DefaultResumeContextRounds = 3

	// Workspace cleanup defaults (periodic bounding of on-disk scratch files).
	DefaultWorkspaceCleanupInterval = time.Hour
	DefaultWorkspaceCleanupMaxAge   = 24 * time.Hour
	DefaultWorkspaceCleanupMaxFiles = 200
)

// CompressConfig holds compress.SmartCompressor parameters.
type CompressConfig struct {
	// CompactKeysListed caps the number of keys listed in the rolling
	// compaction summary (default 32); older events stay recallable.
	CompactKeysListed int
	// RecentFullCount is the number of most recent refs resolved with full
	// content from MemoryStore. Unset (0) derives keepRecent ×
	// compress.DefaultRefsPerTurn (D6); explicit values win.
	RecentFullCount int
	// CardMaxChars caps the index-card section of the rolling compaction
	// summary (default compress.DefaultCardMaxChars); beyond it old card lines are
	// LLM-condensed (or sink, without a summary model).
	CardMaxChars int
	// SummaryMaxTokens is the output-token budget floor for summary calls
	// (0 → pkg default). Guards reasoning models against empty Content.
	SummaryMaxTokens int
}

// constructedTagents counts every successful NewTagentAgent entry in this
// process (S-A/2.3 + 5.3): the de-shell contract asserts a hot reload that only
// MODIFIES existing agents constructs ZERO new TagentAgents (shells are the
// duplication D1 removes), and the complexity report counts constructions per
// cold/reload/rollback. Diagnostics/test introspection only — no execution path
// reads it.
var constructedTagents atomic.Int64

// TagentAgentsConstructed reports the process-wide count of TagentAgent
// constructions (see constructedTagents). Callers assert DELTAS around an
// operation, never absolute values (tests share the process).
func TagentAgentsConstructed() int64 { return constructedTagents.Load() }

// NewTagentAgent creates a new TagentAgent with the given configuration.
//
// In the event-driven architecture, NewTagentAgent:
//   - Creates MemoryStore + MemoryPlugin + compress.SmartCompressor
//   - Creates Preprocessor (replacing ContextIntervention.BeforeModel)
//   - Creates EventBus + AgentLoop
//   - Creates SessionService + Runner (as shell for session/plugin management)
//
// The Runner is retained for session management and plugin lifecycle
// (MemoryPlugin.OnEvent, SummaryPlugin). Actual execution is driven by
// AgentLoop, not the Runner.
func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if cfg.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	constructedTagents.Add(1)

	// Apply defaults
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = DefaultMaxToolIterations
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = compress.DefaultMaxTokens
	}
	if cfg.CompressThreshold <= 0 || cfg.CompressThreshold > 1 {
		cfg.CompressThreshold = compress.DefaultCompressThreshold
	}
	if cfg.KeepRecentTasks <= 0 {
		cfg.KeepRecentTasks = 2
	}
	cfg.WorkspaceRoot = workspace.Root(cfg.WorkspaceRoot)
	if cfg.WorkspaceCleanupInterval <= 0 {
		cfg.WorkspaceCleanupInterval = DefaultWorkspaceCleanupInterval
	}
	if cfg.WorkspaceCleanupMaxAge <= 0 {
		cfg.WorkspaceCleanupMaxAge = DefaultWorkspaceCleanupMaxAge
	}
	if cfg.WorkspaceCleanupMaxFiles <= 0 {
		cfg.WorkspaceCleanupMaxFiles = DefaultWorkspaceCleanupMaxFiles
	}

	// 1. Create MemoryStore (use provided or default to InMemoryStore)
	var memStore memory.MemoryStore
	if cfg.MemoryStore != nil {
		memStore = cfg.MemoryStore
	} else {
		memStore = memory.NewInMemoryStore()
	}

	// 3. Create MemoryPlugin
	memPlugin := plugin.NewMemoryPlugin(memStore)

	// Apply identity defaults
	if cfg.Name == "" {
		cfg.Name = DefaultAgentName
	}
	name := cfg.Name
	if cfg.Description == "" {
		cfg.Description = DefaultAgentDescription
	}
	description := cfg.Description

	// 4. Wrap all tools with OutputLimitTool
	// A6：封顶 MaxTokens/2*4 派生——保留小 budget 的比例语义，但封顶 toolOutputCapChars，
	// 防长上下文配置（128K budget → 256K 字符）下溢出保护形同不存在。
	maxOutputChars := outputCapForMaxTokens(cfg.MaxTokens)
	outputWorkspace := workspace.ToolOutputPath(cfg.WorkspaceRoot)
	if maxOutputChars > 0 && len(cfg.Tools) > 0 {
		wrapped := make([]tool.Tool, len(cfg.Tools))
		for i, t := range cfg.Tools {
			olt := NewOutputLimitTool(t, maxOutputChars)
			olt.SetWorkspace(outputWorkspace)
			wrapped[i] = olt
		}
		cfg.Tools = wrapped
	}

	// 5. Create outputCh + EventBus + projection EARLY so the
	// AppendEventHook (created next) can capture them.
	outputCh := make(chan *event.Event, 100)
	// T-G ReliableBus（resident-readiness-plan 3.2）：BusSpillDir 非空启用 durable
	// inbox（所有输入先持久化，durable receipt 后才被消费）。配置了可靠性却构建失败
	// （磁盘不可写 / 旧 .spill 未排空）必须 fail-loud —— 可靠性绝不静默降级。
	bus, busErr := NewReliableEventBus(cfg.BusSpillDir)
	if busErr != nil {
		return nil, fmt.Errorf("agent %q: durable inbox init failed: %w", name, busErr)
	}
	// T-3.5（fix-resident-reliability-boundaries D4）：可靠模式要求事实链支持显式
	// 幂等重放（memory.EventReplayer）——durable inbox 的 prepared_fact 重放依赖
	// ReplayEvent 的 commit-vs-replay 语义才能既不双写也不漂移地收敛。配置了 durable
	// bus 却接入无重放能力的 store 会让「至少一次投递」静默退化为可能的双写，必须
	// fail-loud，绝不降级为 volatile。
	if bus.Durable() {
		if _, ok := memStore.(memory.EventReplayer); !ok {
			return nil, fmt.Errorf("agent %q: durable inbox requires a replay-capable MemoryStore (%T does not implement memory.EventReplayer); refusing to degrade durability", name, memStore)
		}
		// §2.8: wire the store's retention guard and rebuild+arm the lease from existing
		// unacked envelopes BEFORE serving. The durable store's lifecycle scanner has been
		// gated on Lease.Ready() since open (restart race); arming here releases it only once
		// the durable originals of unacked material are protected. §5.8: a failed inventory
		// is NOT swallowed — the bus holds the registration barrier (forgetting blocked on
		// the incomplete view) and this build REFUSES to open ingest, reporting the block
		// instead of serving durable inputs a later pass could destroy unguarded.
		if g, ok := memStore.(memory.RetentionGuard); ok {
			bus.SetRetentionGuard(g)
			if aerr := bus.ArmRetentionFromInbox(); aerr != nil {
				return nil, fmt.Errorf("agent %q: recovery inventory unreadable — ingest refused, forgetting barrier held (explicit block, no silent proceed): %w", name, aerr)
			}
		}
	}
	projection := compress.NewSessionProjection()

	// Task layer: tools spawn long-running work via the injected task.TaskSpawner;
	// when a task settles in the background (after its sync-wait window), the
	// OnSettle hook publishes a task_settled event onto the bus, which the
	// persistent loop reclaims into a new turn (idle → wakes Pull; mid-turn →
	// buffered until the current turn finishes — single-consumer queueing).
	//
	// R2（resident-continuity-r2-r4 1.6）：OnSpawn/OnInlineSettle 经 late-bind sink
	// 写事实链记录（task_spawned 载 Declarative / inline settle 终态记录——registry
	// 重建数据源，记录-only 不发 bus 不进投影）。cm 在下方创建后才绑定。
	taskRecords := &taskRecordSink{}
	sinkReg := newSettleSinkRegistry()
	taskManager := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			evt := newTaskSettledEvent(tk, sig, settleInlineCapChars, outputWorkspace)
			// S3m-a: route to the owning sub-invocation loop's sink when one is
			// registered for this task's delegation id (S2m Origin); else fall back
			// to persistentBus — the entry owner and every pre-S3m-b path keep the
			// current single-consumer behavior exactly.
			deliverTaskSettled(sinkReg, bus, tk, evt)
		},
		OnSpawn:        taskRecords.onSpawn,
		OnInlineSettle: taskRecords.onInlineSettle,
		OnCancel:       taskRecords.onCancel,
		// 6.7①（resident-remaining-hardening）：批量退役汇总——per-task settle
		// 记录仍逐条 record-only 落链（registry 归并数据源不变），bus 只发一条
		// 汇总 external_input（终结结算风暴：孤儿/zombie 批量退役不再以
		// 2.2 万字符/条的消息刷满上下文）。
		OnBatchRetire: func(batch []task.BatchRetired) {
			for _, r := range batch {
				taskRecords.onInlineSettle(r.Task, r.Sig)
			}
			if evt := newBatchRetiredSummaryEvent(batch); evt != nil {
				bus.Publish(evt)
			}
		},
		// Zero → task package default (2m). Bounds the resume window for
		// terminal tasks; wired from YAML task_terminal_ttl.
		TerminalTTL: cfg.TaskTerminalTTL,
		// §10.5: unified reaper fallback lifetime for tasks whose spec has no
		// explicit TTL (restored/subagent). Wired from the operator task_default_ttl
		// slot; zero → the task package floors it to 10m. The reaper is always on —
		// the old StaleAfter/JobDeadline knobs are retired.
		DefaultTTL: cfg.TaskDefaultTTL,
		// 5.4（design-report-closeout）：disk degraded 时拒绝新 spawn（闸不是墙——
		// 进行中任务的 settle/轮询不受影响）。默认关（DiskBlockSpawn=false 或
		// Degradation 未接线 → gate 为 nil）。
		SpawnGate: buildSpawnGate(cfg),
	})

	// onEventRef is set after TagentAgent creation. The AppendEventHook
	// uses it to propagate meta_* onto user message events before forwarding
	// them to outputCh (delivery only — projection writes happen in the
	// event-plugin pipeline via ProjectionSink, unified-event-projection D1).
	var onEventRef func(evt *event.Event)

	// F2 observability: counter shared by the SessionHook closure (registered
	// below, before ta exists) and TagentAgent.
	droppedOutputCounter := &atomic.Int64{}

	// 6. Create SessionService
	// Limit session events to 2: only the current invocation's user message
	// and the latest tool result are needed for ContentRequestProcessor's
	// TimelineFilterCurrentRequest. Historical context is managed entirely
	// by compress.SessionProjection + compress.ContextCompressor, so the runner session does
	// not need to retain full event history.
	//
	// AppendEventHook forwards user message events to outputCh (the runner
	// appends user messages to session but does NOT emit them through the
	// agent event channel — without this hook the consumer would never see
	// them).
	var sessionSvc session.Service
	// R4（review 🔴1）：外部注入的 SessionSvc（executorOnly 热重建壳）优先——
	// 其 AppendEventHook 已绑定常驻 outputCh，session 记录续写同一 session。
	if cfg.SessionSvc != nil {
		sessionSvc = cfg.SessionSvc
	} else {
		sessionSvc = sessioninmemory.NewSessionService(
			sessioninmemory.WithSessionEventLimit(2),
			sessioninmemory.WithAppendEventHook(func(ctx *session.AppendEventContext, next func() error) error {
				original := ctx.Event
				var evtCopy event.Event
				if original.Response != nil {
					evtCopy = *original
					evtCopy.Response = original.Response.Clone()
					ctx.Event = &evtCopy
				}
				err := next()
				ctx.Event = original

				// Forward user message events to outputCh (delivery). LLM/tool
				// events are emitted via eventCh in RunFlow, not here. Projection
				// writes happen in the event-plugin pipeline (MemoryPlugin →
				// ProjectionSink), which has already run for this event. Deliver a
				// clone with its own StateDelta map: onEventRef writes meta_* and
				// must never mutate the framework's shared event object.
				if onEventRef != nil && original.IsUserMessage() {
					emitEvt := cloneEventForDelivery(original)
					onEventRef(emitEvt)
					select {
					case outputCh <- emitEvt:
					default:
						droppedOutputCounter.Add(1)
						log.Warnf("[SessionHook] outputCh full, user message event dropped (total dropped: %d)",
							droppedOutputCounter.Load())
					}
				}

				return err
			}),
		)
	}

	// 7. Create TagentAgent (without contextManager yet — wired after callback creation)
	ta := &TagentAgent{
		persistentBus:       bus,
		activeBus:           bus,
		settleSinks:         sinkReg,
		droppedOutputEvents: droppedOutputCounter,
		memStore:            memStore,
		memStoreRelease:     cfg.MemStoreRelease,
		memStoreOwned:       !cfg.MemStoreBorrowed,
		memPlugin:           memPlugin,
		config:              cfg,
		sessionSvc:          sessionSvc,
		name:                name,
		description:         description,
		outputCh:            outputCh,
		closers:             []Closer{},
		projection:          projection,
		degradation:         cfg.Degradation,
	}

	// 8. Create onEvent callback and ContextManager.
	onEvent := ta.makeOnEventCallback()
	onEventRef = onEvent // Wire the hook's callback.
	cm := newContextManagerFromConfig(cfg, nil, memPlugin, sessionSvc, bus, outputCh, projection, onEvent)
	taskRecords.cm = cm // R2: bind the record sink (late — hooks are best-effort nil-safe before this)
	ta.contextManager = cm
	ta.liveCMs = make(map[*ContextManager]struct{})
	// §6.4 恒装源（design §2「不存在无源状态」）：构造期即以解析后的完整 desired
	// 装上 owner 热参源；组合根在首个提交点把它换成记录绑定源，此后每次提交轮转
	// 记录即单写者生效——不再有需要与记录手动对齐的第二份快照通路。
	ta.SetHotSource(staticHotSource(initialHotParams(cfg)))
	// §6.4 pull: the resident compressor and the task registry resolve their
	// numeric group from the owner's live hot view at every consumption boundary
	// (compression boundary / TTL sweep & board read). The reloader owns the single
	// commit point; nothing is pushed into either consumer.
	cm.contextCompressor.SetHotSource(ta.liveHotNumbers)
	ta.taskManager = taskManager
	taskManager.SetTTLSource(ta.taskTTLs)
	cm.taskController = taskManager

	// Initialize meditation manager if enabled.
	if cfg.Meditation.Enabled {
		ta.meditationMgr = NewMeditationManager(cfg.Meditation, ta)
		// Feed the read-only task controller so meditation carries a self-state
		// digest (task-layer health). taskManager is always non-nil here.
		ta.meditationMgr.SetTaskController(taskManager)
		// T-G AnchorStore：锚点持久化路径非空则注入，跨重启保留冥想门控三锚点（重启后不
		// 立即误触发冥想、正确计算 novelty）。init 失败降级为内存锚点（可用性优先）。
		if cfg.Meditation.AnchorPath != "" {
			if as, aerr := reliability.NewAnchorStore(cfg.Meditation.AnchorPath); aerr == nil {
				ta.meditationMgr.SetAnchorStore(as)
			} else {
				log.Warnf("[Meditation] anchor store init failed (%v), anchors stay in-memory", aerr)
			}
		}
	}

	// Start the workspace cleaner. Scope: tool-output only. The exec/ dir is a
	// tmux working directory whose lifecycle belongs to the task layer (tasks
	// may run for days and write artifacts there) — cleaning it by file age or
	// count would delete live-task outputs.
	cleanCtx, cleanCancel := context.WithCancel(context.Background())
	ta.cleanupCancel = cleanCancel
	ta.cleanupDone = make(chan struct{})
	cleaner := workspace.NewCleaner(workspace.ToolOutputPath(cfg.WorkspaceRoot), cfg.WorkspaceCleanupInterval, cfg.WorkspaceCleanupMaxAge, cfg.WorkspaceCleanupMaxFiles)
	go func() {
		defer close(ta.cleanupDone)
		cleaner.Run(cleanCtx)
	}()

	return ta, nil
}

// buildCompressorOpts builds compress.SmartCompressor options from TagentConfig.
// Shared by NewTagentAgent and Run() to avoid duplicating option-building logic.
func buildCompressorOpts(cfg *TagentConfig) []compress.SmartCompressorOption {
	opts := []compress.SmartCompressorOption{
		compress.WithMaxTokens(cfg.MaxTokens),
	}
	// Post-compression aging target defaults to the trigger line
	// (threshold*MaxTokens) instead of MaxTokens. Kills the dead zone where
	// a sticky session baseline sits between trigger line and budget line,
	// which caused every-turn no-op compression invocations.
	pct := cfg.CompressThreshold
	if pct <= 0 || pct > 1 {
		pct = 0.8
	}
	opts = append(opts, compress.WithTriggerBudget(int(float64(pct)*float64(cfg.MaxTokens))))
	if cfg.KeepRecentTasks > 0 {
		opts = append(opts, compress.WithKeepRecentTasks(cfg.KeepRecentTasks))
	}
	if cfg.SummaryModel != nil {
		opts = append(opts, compress.WithSummaryModel(cfg.SummaryModel))
	}
	if cfg.SummaryEffort != "" {
		opts = append(opts, compress.WithSummaryEffort(cfg.SummaryEffort))
	}
	// Compress config
	if cfg.Compress.SummaryMaxTokens > 0 {
		opts = append(opts, compress.WithSummaryMaxTokens(cfg.Compress.SummaryMaxTokens))
	}
	return opts
}

// ownerHotNumbersSource returns the §6.4 pull source for an invocation-private
// compressor (the owner's live hot view), or nil for the resident CM — which
// installs its own source after the agent fields are wired (NewTagentAgent).
func ownerHotNumbersSource(owner *TagentAgent) func() compress.HotNumbers {
	if owner == nil {
		return nil
	}
	return owner.liveHotNumbers
}

// hotOverlayConfig is GONE (§6.4 pull, S-E): seeding an invocation-private CM
// from the owner's snapshot at construction existed to make fresh calls start
// effective. Its successor is weaker and stronger at once — the private CM binds
// the owner's LIVE source instead, so it is effective at its NEXT boundary (no
// stale-generation window) and there is no second copy of the numbers to keep in
// step. Construction config values remain only as the no-source fallback.

// initialHotParams derives the construction-time hot bundle from an already
// parsed config (the root package's parse layer fills unset TTLs with the task
// defaults). The threshold fallback mirrors buildCompressorOpts's 0.8, so the
// seeded snapshot and the seeded compressor always agree.
func initialHotParams(cfg *TagentConfig) OrgHotParams {
	th := cfg.CompressThreshold
	if th <= 0 || th > 1 {
		th = 0.8
	}
	return OrgHotParams{
		ThresholdPct:    th,
		MaxTokens:       cfg.MaxTokens,
		KeepRecentTasks: cfg.KeepRecentTasks,
		TaskTerminalTTL: cfg.TaskTerminalTTL,
		TaskDefaultTTL:  cfg.TaskDefaultTTL,
	}
}

// newContextManagerFromConfig creates a ContextManager from TagentConfig.
// Shared by NewTagentAgent and Run(). owner is non-nil only for
// invocation-private CMs built inside sub-agent Run(): the CM is seeded from
// the owner's effective hot snapshot (§6.4/D4 — fresh calls start effective,
// never from construction-frozen config); session.Run then registers it into
// the owner's live set so in-flight calls apply later hot updates at their
// next compression/budget boundary. The resident CM passes owner == nil and
// installs its own source in NewTagentAgent once the agent fields exist.
func newContextManagerFromConfig(cfg *TagentConfig, owner *TagentAgent, memPlugin *plugin.MemoryPlugin, sessionSvc session.Service, bus *EventBus, outputCh chan *event.Event, projection *compress.SessionProjection, onEvent func(evt *event.Event)) *ContextManager {
	// §6.4 pull (S-E): no construction-time seeding anymore — the private CM
	// below binds the owner's live hot source, so a later rotation still reaches
	// it at its next boundary. cfg's own values remain as the no-source fallback.
	eff := cfg
	copts := buildCompressorOpts(eff)
	copts = append(copts, compress.WithTokenCounter(compress.NewDefaultTokenCounter()))
	compressor := compress.NewSmartCompressor(copts...)
	// Use system prompt from config (framework details are in AGENTS.md)
	systemPrompt := cfg.SystemPrompt

	cm := NewContextManager(ContextManagerConfig{
		Name:                 eff.Name,
		Model:                eff.Model,
		Tools:                eff.Tools,
		SystemPrompt:         systemPrompt,
		SystemPromptSource:   eff.SystemPromptSource,
		Temperature:          eff.Temperature,
		MaxToolIters:         eff.MaxToolIterations,
		ThinkingEnabled:      eff.ThinkingEnabled,
		ThinkingTokens:       eff.ThinkingTokens,
		ReasoningEffort:      eff.ReasoningEffort,
		ReasoningContentMode: eff.ReasoningContentMode,
		Compressor:           compressor,
		TokenCounter:         compress.NewDefaultTokenCounter(),
		MaxTokens:            eff.MaxTokens,
		ThresholdPct:         eff.CompressThreshold,
		// §6.4 pull: an invocation-private compressor binds the OWNER's live hot
		// view, so a rotation made effective after this CM was constructed
		// reaches it at its NEXT boundary instead of requiring a per-CM push
		// fan-out; the overlay-seeded values above remain the no-source fallback
		// only (a nil owner leaves the source unset — the resident path wires
		// its own source once the agent exists).
		HotNumbersSource:  ownerHotNumbersSource(owner),
		CompactKeysListed: eff.Compress.CompactKeysListed,
		RecentFullCount:   eff.Compress.RecentFullCount,
		CardMaxChars:      eff.Compress.CardMaxChars,
		MemStore:          eff.MemoryStore,
		MemPlugin:         memPlugin,
		SessionSvc:        sessionSvc,
		OutputCh:          outputCh,
		Bus:               bus,
		Projection:        projection,
		OnEvent:           onEvent,
	})
	// F2 (design-report-closeout): stalled-consumer overflow persists under
	// <workspace>/tool-output/output-overflow (workspace.Root applies the
	// default workspace when cfg.WorkspaceRoot is empty).
	cm.overflowDir = filepath.Join(workspace.ToolOutputPath(cfg.WorkspaceRoot), "output-overflow")
	return cm
}

// buildSpawnGate (5.4, design-report-closeout) returns the disk-degradation
// spawn gate, or nil when disabled (zero behavior change). A gate, not a wall:
// in-flight tasks are never gated.
func buildSpawnGate(cfg *TagentConfig) func() string {
	if cfg == nil || !cfg.DegradationBehaviors.DiskBlockSpawn || cfg.Degradation == nil {
		return nil
	}
	degradation := cfg.Degradation
	return func() string {
		if degradation.IsDegraded(reliability.DepDisk) {
			return "disk dependency degraded: new background tasks are paused (in-flight tasks unaffected); retry after disk recovers"
		}
		return ""
	}
}

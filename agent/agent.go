// Package agent provides tagent's core agent mechanism coordination.
//
// TagentAgent wires together:
// - EventBus + AgentLoop (event-driven execution engine)
// - Runner (framework orchestration with plugins, retained for session/plugin lifecycle)
// - MemoryPlugin (OnEvent: event persistence + causal chain)
// - Preprocessor (event filtering, token budget, SmartCompress)
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
	// outputCh was full at try-time.
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

	// hotSource：owner 热参的**唯一**读源闭包。构造期由 NewTagentAgent
	// 恒装静态源；组合根在协调器就绪后换成记录绑定
	// 源（读唯一已提交应用记录），此后每次提交点轮转记录即单写者生效。
	// compressor / taskManager / 私有 CM 全部经它现读——它就是唯一读源，不存在第二份可写缓存。
	hotSource atomic.Pointer[func() (OrgHotParams, bool)]

	// liveCMsMu liveCMs：随调用生命周期注册/注销的**存活调用私有 CM**集合
	// （绝非历史对象列表）。提交点从不向它扇出任何值——私有 CM
	// 的压缩器绑本 agent 的热参源，在下一次压缩/预算读取边界现读记录。本集合目前
	// **无生产读取方**（只有注册/注销与 LiveCMCount／snapshotLiveCMs 读面，后者已按
	//  收为包内——向外暴露内部 CM 切片没有生产价值）：其存续理由
	// 待 S-D 按 D4「若保留只服务取消/完成等待，归唯一 owner」裁定。有界：调用结束即注销。
	liveCMsMu sync.Mutex
	liveCMs   map[*ContextManager]struct{}

	// taskManager owns async task lifecycle. Tools spawn via the injected
	// task.TaskSpawner; background settles are published back to persistentBus as
	// task_settled events by its OnSettle hook.
	taskManager *task.TaskManager
	// selfAudit is the behavior-audit dimension of the attention-budget
	// architecture (self-telemetry-audit): it observes settle/input traffic,
	// escalates the L1/L2/L3 ladder and feeds the task layer's AuditGate.
	selfAudit *SelfTelemetryAuditor

	// settleSinks (S3m-a, M2越窗 routing): per-invocation settle sinks keyed by the
	// delegation invocation_id (S2m Origin handle). The OnSettle hook routes a
	// background task_settled to the owning sub-invocation loop's sink when one is
	// registered; with no sink (entry owner + every pre-S3m-b path) it falls back
	// to persistentBus, so this is behavior-neutral until S3m-b registers a sink.
	settleSinks *settleSinkRegistry

	// orgRollback：热更回滚钩子（tagent
	// 包懒检查闭包注入；Rollback() 触发）。它**每次成功发布都被重写**（运行期写），
	// 而宿主/运维可从另一个 goroutine 调 Rollback()——故用 atomic.Pointer，
	// 不得换成裸字段。
	orgRollback atomic.Pointer[func()]

	// orgDiags：装配层注入的编排代际
	// 诊断提供者。形状由装配层拥有（本包只见 map[string]any），**仅诊断面读取**——
	// 无任何执行路径据它选版（resident-continuity：指纹/序号不是应用可见 identity）。
	// 与 orgRollback **不同规**：它只在启动期注入一次（tagent.go 的 configPath 接线块），
	// 运行期只读，所以普通字段就够；不要拿 orgRollback 当参照。
	orgDiags func() map[string]any

	// storeOwnerSnapshot:装配层注入的「当前持有 store owner 登记的 agent
	// 名集」只读探针。候选事务回退须撤销本候选登记的每个 owner（含失败父），该探针
	// 让宿主/测试能确定性地观测在线拓扑之外的 owner 归属；纯内省，无执行路径读它。
	// 与 orgDiags 同规：启动期注入一次，运行期只读。
	storeOwnerSnapshot func() map[string]bool

	// storeOwnerRevoke: the assembly hook that drops THIS agent's
	// store-owner registration. Called by closeOnce only after the store exit was
	// really taken; an unconverged close keeps the registration (the holder may
	// still write). Set at build time, read-only afterwards.
	storeOwnerRevoke func()

	// memStore Framework integration
	memStore memory.MemoryStore
	// memStoreRelease: lease release bound to
	// THIS agent's lifecycle — executed from Close; nil for borrowed/shell
	// agents and for isolated stores the agent fully owns via its own Close.
	// Returns the release's close error (last-lease close reaches Close).
	memStoreRelease func() error
	// memStoreOwned: this agent is the SOLE close owner of
	// memStore (isolated build) — the direct-Close fallback may fire when no
	// lease exists. Borrowed shells and leased holders are false: shared state
	// exits only through the lease release, never through them.
	memStoreOwned bool

	// resident: the SHARED immutable-snapshot binding table —
	// name → resident agent instance, which the hot-reload shells borrow
	// per-agent resources from and which hot ADD publishes into. Held by
	// pointer so one publish reaches every agent; readers never see a torn map.
	resident   *ResidentTopology
	memPlugin  *plugin.MemoryPlugin
	config     *TagentConfig
	sessionSvc session.Service

	// name Agent identity (for agent.Agent interface)
	name        string
	description string

	// sessionMu Session context for event injection (set on first Run)
	sessionMu     sync.Mutex
	lastUserID    string
	lastSessionID string

	// externalEventsMu 守护待摄入的外部事件（direct-Ingest API 的单槽交收，非主路径）：
	// 由 IngestExternalEvents 写入，下一次 Run 在进入时原子取走。它不是历史缓冲区，
	// 主委托路径（RuntimeState）从不写它；互斥锁保证并发 Run 的取走不会与写入互相撕裂。
	externalEventsMu      sync.Mutex
	pendingExternalEvents []memory.FullEvent

	// closers Resource closers — components like ActionTool that need cleanup on shutdown.
	// Closed in Close() before the runner is stopped.
	closers []Closer

	// trajectoryRecorder TrajectoryRecorder (optional) — records LLM calls to JSONL when enabled.
	// Set via SetTrajectoryRecorder. StartLoop calls SetSessionInfo on it.
	trajectoryRecorder *rl.TrajectoryRecorder

	// outputCh Persistent Event Loop — 持久事件循环（StartLoop 模式）。：Start/Stop/Close
	// 由同一显式状态机协调（loopIdle→loopRunning→loopStopping→loopClosed），
	// 首次关闭执行、其余等待同一完成结果（不因标志已翻跳过等待）；在途计数
	// （loopWg.Add）先于发布 running 登记；输出通道在循环 goroutine 退出（含
	// panic 路径）时恰好关闭一次，从未启动则由 Close 落定终态（V15 终结语义不变）。
	outputCh     chan *event.Event
	loopCtx      context.Context
	loopCancel   context.CancelFunc
	loopState    atomic.Int32
	loopDone     chan struct{}
	loopWg       sync.WaitGroup
	outputSettle sync.Once
	closeMu      sync.Mutex
	closeStarted bool
	closeDone    chan struct{}
	closeErr     error
	// closeTail terminal tail: a bounded Close that could not honestly finish carries
	// the remainder (still-used tool closers, recorder, store exit and its owner
	// registration) in exactly ONE continuation, armed on the manager's own
	// reclaim event and run when the executions actually stop. Guarded by closeMu;
	// closeTailOnce makes the exit itself exactly-once.
	closeTail     func()
	closeTailOnce sync.Once
	closeTailDone bool
	closeTailErr  error

	// meditationMgr Meditation manager — started/stopped with the persistent event loop.
	meditationMgr *MeditationManager

	// degradation 是五依赖退化状态机（T-G，报告 D3）。可选（nil=未启用）：event_loop 在
	// RunFlow 失败/成功时上报 DepModel；ErrorTrackingStore（存储栈最外层）上报 memory/disk/rustviking。
	degradation *reliability.DegradationManager

	// cleanupCancel stops the workspace cleaner goroutine (started in NewTagentAgent).
	cleanupCancel context.CancelFunc
	// cleanupDone closes when the cleaner goroutine has returned. Cancelling is
	// only a request; this is the confirmation the close sequence waits on
	//.
	cleanupDone chan struct{}

	// projection is the lightweight, bounded Session projection (EventReference[])
	// shared by onEvent and Preprocessor. It is created per TagentAgent and
	// passed to each invocation's AgentLoop.
	projection *compress.SessionProjection
}

// TagentConfig holds configuration for creating a TagentAgent.
type TagentConfig struct {
	Model       model.Model
	MemoryStore memory.MemoryStore
	// MemStoreRelease: lease release for the
	// provided MemoryStore; executed exactly once from Close. nil = the agent
	// does not own a registry lease.
	MemStoreRelease func() error
	// MemStoreBorrowed marks an executor shell that BORROWS
	// the resident shared store without a lease: it holds no close right over
	// shared state at all — the direct-Close fallback must not fire. Default
	// false = the agent owns its (isolated) store exclusively and Close flushes
	// it via the fallback tail.
	MemStoreBorrowed   bool
	SessionSvc         session.Service
	SystemPrompt       string
	SystemPromptSource prompt.Getter
	Tools              []tool.Tool
	MaxToolIterations  int
	MaxTokens          int
	CompressThreshold  float64
	SummaryModel       model.Model
	SummaryEffort      string
	Temperature        float64
	KeepRecentTasks    int
	Compress           CompressConfig

	// TaskTerminalTTL is the grace period an exited task (completed/failed/
	// cancelled/dead) is retained before pruning. It bounds the resume_task
	// window for terminal subagent tasks (task-chain restorer). Non-positive
	// falls back to the task package default (2m).
	TaskTerminalTTL time.Duration

	// TaskDefaultTTL is the unified reaper's fallback absolute lifetime for
	// spawns whose model-side `ttl` is unset (async-task-lifetime 10.5). Zero →
	// task package default (10m). There is no disable path — no task is immortal.
	TaskDefaultTTL time.Duration

	// ThinkingEnabled Thinking/reasoning controls (merged into model.GenerationConfig)
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string

	// Name Agent identity (for agent.Agent interface)
	Name        string
	Description string

	// Meditation configures the meditation/heartbeat mechanism.
	Meditation MeditationConfig

	// Degradation 是五依赖退化状态机（T-G，可选）。非 nil 时 event_loop 上报 model 依赖、
	// ErrorTrackingStore（wireMemoryEngine 最外层）上报 memory/disk/rustviking。nil=不启用（现状）。
	Degradation *reliability.DegradationManager

	// DegradationBehaviors是依赖退化的行为响应层（警告级，
	// 每项独立、默认零值=全部关闭，仅 Degradation 非 nil 时生效）。闸不是墙：行为只是
	// 免打已确认故障的依赖，上报恢复即回正常路径。
	DegradationBehaviors DegradationBehaviors

	// WorkspaceRoot is the unified on-disk scratch root (default: .tagent-workspace).
	// Oversized tool outputs go to <root>/tool-output; the tmux command working
	// directory is <root>/exec. A periodic cleaner bounds tool-output files.
	WorkspaceRoot string

	// WorkspaceCleanupInterval Workspace cleanup (periodic, tool-output dir only). Non-positive values
	// fall back to the defaults below (there is no disable switch).
	WorkspaceCleanupInterval time.Duration
	WorkspaceCleanupMaxAge   time.Duration
	WorkspaceCleanupMaxFiles int

	// BusSpillDir 是事件总线磁盘溢出目录（T-G ReliableBus）。非空时 channel 满则事件溢出
	// 落盘而非丢弃（at-least-once，常驻不丢事件），重启后未消费项可回收；空 = 纯 channel（现状）。
	BusSpillDir string
}

// DegradationBehaviors configures the
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

// DefaultMaxToolIterations Default configuration values
const (
	DefaultMaxToolIterations         = 50
	DefaultSubAgentMaxToolIterations = 10
	DefaultAgentName                 = "tagent"
	DefaultAgentDescription          = "TagentAgent - AI assistant powered by tagent"

	// DefaultResumeContextRounds caps the rounds restored by the subagent
	// task-chain restorer on resume.
	DefaultResumeContextRounds = 3

	// DefaultWorkspaceCleanupInterval Workspace cleanup defaults (periodic bounding of on-disk scratch files).
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
	// compress.DefaultRefsPerTurn ; explicit values win.
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
// - Creates MemoryStore + MemoryPlugin + compress.SmartCompressor
// - Creates Preprocessor (replacing ContextIntervention.BeforeModel)
// - Creates EventBus + AgentLoop
// - Creates SessionService + Runner (as shell for session/plugin management)
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

	memPlugin := plugin.NewMemoryPlugin(memStore)

	if cfg.Name == "" {
		cfg.Name = DefaultAgentName
	}
	name := cfg.Name
	if cfg.Description == "" {
		cfg.Description = DefaultAgentDescription
	}
	description := cfg.Description

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

	outputCh := make(chan *event.Event, 100)
	bus, busErr := NewReliableEventBus(cfg.BusSpillDir)
	if busErr != nil {
		return nil, fmt.Errorf("agent %q: durable inbox init failed: %w", name, busErr)
	}
	if bus.Durable() {
		if _, ok := memStore.(memory.EventReplayer); !ok {
			return nil, fmt.Errorf("agent %q: durable inbox requires a replay-capable MemoryStore (%T does not implement memory.EventReplayer); refusing to degrade durability", name, memStore)
		}
		if g, ok := memStore.(memory.RetentionGuard); ok {
			bus.SetRetentionGuard(g)
			if aerr := bus.ArmRetentionFromInbox(); aerr != nil {
				return nil, fmt.Errorf("agent %q: recovery inventory unreadable — ingest refused, forgetting barrier held (explicit block, no silent proceed): %w", name, aerr)
			}
		}
	}
	projection := compress.NewSessionProjection()

	taskRecords := &taskRecordSink{}
	sinkReg := newSettleSinkRegistry()
	// Behavior audit (self-telemetry-audit): the L1 alert enters the bus as an
	// ordinary external_input (unconsumed level → full delivery, and its
	// reclaim turn persists the fact-chain record via the normal store path —
	// no parallel recording machinery). Level transitions only, so the alert
	// cannot itself feed a new alert loop.
	var lastAlertLevel int32
	selfAudit := NewSelfTelemetryAuditor(func(level int, ratio float64, samples int, frozen bool) {
		if int(atomic.SwapInt32(&lastAlertLevel, int32(level))) == level {
			return
		}
		if level >= 1 {
			bus.Publish(NewExternalInputEvent("system", model.NewUserMessage(
				fmt.Sprintf("[self-telemetry-audit] 空转审计级别 L%d：自管遥测占比 %.0f%%（窗口样本 %d，冻结=%v）。"+
					"L2 起拒绝自管新任务纳管，L3 冻结非保护类 spawn（保护类豁免）。", level, ratio*100, samples, frozen))))
		}
	})
	taskManager := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			evt := newTaskSettledEvent(tk, sig, settleInlineCapChars, outputWorkspace)
			selfAudit.ObserveSettle(evt.Metadata)
			deliverTaskSettled(sinkReg, bus, tk, evt)
		},
		OnSpawn:        taskRecords.onSpawn,
		OnInlineSettle: taskRecords.onInlineSettle,
		OnCancel:       taskRecords.onCancel,
		OnBatchRetire: func(batch []task.BatchRetired) {
			for _, r := range batch {
				taskRecords.onInlineSettle(r.Task, r.Sig)
			}
			if evt := newBatchRetiredSummaryEvent(batch); evt != nil {
				bus.Publish(evt)
			}
		},
		TerminalTTL: cfg.TaskTerminalTTL,
		DefaultTTL:  cfg.TaskDefaultTTL,
		SpawnGate:   buildSpawnGate(cfg),
		AuditGate:   selfAudit.GateReason,
	})

	// onEventRef is set after TagentAgent creation. The AppendEventHook
	// uses it to propagate meta_* onto user message events before forwarding
	// them to outputCh — delivery only; projection writes happen in the
	// event-plugin pipeline via ProjectionSink.
	// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
	var onEventRef func(evt *event.Event)

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

	onEvent := ta.makeOnEventCallback()
	onEventRef = onEvent
	cm := newContextManagerFromConfig(cfg, nil, memPlugin, sessionSvc, bus, outputCh, projection, onEvent)
	taskRecords.cm = cm
	ta.contextManager = cm
	ta.liveCMs = make(map[*ContextManager]struct{})
	ta.SetHotSource(staticHotSource(initialHotParams(cfg)))
	cm.contextCompressor.SetHotSource(ta.liveHotNumbers)
	ta.taskManager = taskManager
	ta.selfAudit = selfAudit
	if ta.meditationMgr != nil {
		ta.meditationMgr.SetAuditLine(selfAudit.DigestLine)
	}
	taskManager.SetTTLSource(ta.taskTTLs)
	cm.taskController = taskManager

	if cfg.Meditation.Enabled {
		ta.meditationMgr = NewMeditationManager(cfg.Meditation, ta)
		ta.meditationMgr.SetTaskController(taskManager)
		if cfg.Meditation.AnchorPath != "" {
			if as, aerr := reliability.NewAnchorStore(cfg.Meditation.AnchorPath); aerr == nil {
				ta.meditationMgr.SetAnchorStore(as)
			} else {
				log.Warnf("[Meditation] anchor store init failed (%v), anchors stay in-memory", aerr)
			}
		}
	}

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
	if cfg.Compress.SummaryMaxTokens > 0 {
		opts = append(opts, compress.WithSummaryMaxTokens(cfg.Compress.SummaryMaxTokens))
	}
	return opts
}

// ownerHotNumbersSource returns the  pull source for an invocation-private
// compressor (the owner's live hot view), or nil for the resident CM — which
// installs its own source after the agent fields are wired (NewTagentAgent).
func ownerHotNumbersSource(owner *TagentAgent) func() compress.HotNumbers {
	if owner == nil {
		return nil
	}
	return owner.liveHotNumbers
}

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
// the owner's effective hot snapshot (/D4 — fresh calls start effective,
// never from construction-frozen config); session.Run then registers it into
// the owner's live set so in-flight calls apply later hot updates at their
// next compression/budget boundary. The resident CM passes owner == nil and
// installs its own source in NewTagentAgent once the agent fields exist.
func newContextManagerFromConfig(cfg *TagentConfig, owner *TagentAgent, memPlugin *plugin.MemoryPlugin, sessionSvc session.Service, bus *EventBus, outputCh chan *event.Event, projection *compress.SessionProjection, onEvent func(evt *event.Event)) *ContextManager {
	eff := cfg
	copts := buildCompressorOpts(eff)
	copts = append(copts, compress.WithTokenCounter(compress.NewDefaultTokenCounter()))
	compressor := compress.NewSmartCompressor(copts...)
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
		HotNumbersSource:     ownerHotNumbersSource(owner),
		CompactKeysListed:    eff.Compress.CompactKeysListed,
		RecentFullCount:      eff.Compress.RecentFullCount,
		CardMaxChars:         eff.Compress.CardMaxChars,
		MemStore:             eff.MemoryStore,
		MemPlugin:            memPlugin,
		SessionSvc:           sessionSvc,
		OutputCh:             outputCh,
		Bus:                  bus,
		Projection:           projection,
		OnEvent:              onEvent,
	})
	cm.overflowDir = filepath.Join(workspace.ToolOutputPath(cfg.WorkspaceRoot), "output-overflow")
	return cm
}

// buildSpawnGate returns the disk-degradation
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

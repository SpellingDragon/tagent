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

	// taskManager owns async task lifecycle. Tools spawn via the injected
	// task.TaskSpawner; background settles are published back to persistentBus as
	// task_settled events by its OnSettle hook.
	taskManager *task.TaskManager

	// orgRollback（R4，resident-continuity-r2-r4 3.8）：热更回滚钩子（tagent
	// 包懒检查闭包注入；Rollback() 触发）。
	orgRollback func()

	// Framework integration
	memStore memory.MemoryStore
	// memStoreRelease (resident-readiness-plan 4.2): lease release bound to
	// THIS agent's lifecycle — executed from Close; nil for borrowed/shell
	// agents and for isolated stores the agent fully owns via its own Close.
	memStoreRelease func()

	// residentNames (4.5/4.6 introspection): the resident topology binding
	// table this agent belongs to (entry + sub-agents), set at New().
	residentNames map[string]bool
	// residentTable (4.5): name → resident agent instance — the binding table
	// the hot-reload shells borrow per-agent resources from.
	residentTable map[string]*TagentAgent
	memPlugin     *plugin.MemoryPlugin // registered on ContextManager's Runner
	config        *TagentConfig
	sessionSvc    session.Service

	// Agent identity (for agent.Agent interface)
	name        string
	description string

	// Session context for event injection (set on first Run)
	sessionMu     sync.Mutex
	lastUserID    string
	lastSessionID string

	// External events pending ingestion (set before Run)
	// These are converted to internal context messages at the start of the next run.
	pendingExternalEvents []memory.FullEvent

	// Resource closers — components like ActionTool that need cleanup on shutdown.
	// Closed in Close() before the runner is stopped.
	closers []Closer

	// TrajectoryRecorder (optional) — records LLM calls to JSONL when enabled.
	// Set via SetTrajectoryRecorder. StartLoop calls SetSessionInfo on it.
	trajectoryRecorder *rl.TrajectoryRecorder

	// Persistent Event Loop — 持久事件循环（StartLoop 模式）。生命周期一次性：
	// StopLoop 后实例终结（loopTerminated），二次 StartLoop 显式报错——输出通道
	// 在循环 goroutine 退出时恰好关闭一次（消费者 range 语义的终态信号），
	// 复用已关通道即生产 panic（V15，2026-09-14 修复）。
	outputCh       chan *event.Event  // 持久输出 channel（循环 goroutine 退出时恰好关闭一次）
	loopCtx        context.Context    // Loop context（StopLoop 取消）
	loopCancel     context.CancelFunc // Loop cancel
	loopActive     atomic.Bool        // Loop 是否运行中
	loopTerminated atomic.Bool        // Loop 已终结（StopLoop 后不可再 Start）
	loopWg         sync.WaitGroup     // 等待 Loop goroutine 退出

	// residentReady closes once the cold-start rebuild sequence (R1
	// projection + R2 task registry + R3 orphan adjudication) completes —
	// the host-side replacement for fixed-sleep timing guesses (β-fix).
	residentReady chan struct{}

	// Meditation manager — started/stopped with the persistent event loop.
	meditationMgr *MeditationManager

	// degradation 是五依赖退化状态机（T-G，报告 D3）。可选（nil=未启用）：event_loop 在
	// RunFlow 失败/成功时上报 DepModel；ErrorTrackingStore（存储栈最外层）上报 memory/disk/rustviking。
	degradation *reliability.DegradationManager

	// cleanupCancel stops the workspace cleaner goroutine (started in NewTagentAgent).
	cleanupCancel context.CancelFunc

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
	// does not own a registry lease (borrowed/shell agents; isolated stores
	// closed via the normal Close path).
	MemStoreRelease    func()
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

	// TaskStaleAfter is the stale-observation threshold: job-kind tasks
	// alive_detached past it are marked stale (one-time notice) — observation
	// only, no termination (2.5). Zero → task default (1h); negative disables.
	TaskStaleAfter time.Duration
	// TaskJobDeadline is the OPTIONAL termination policy: job-kind tasks
	// detached past it are cancelled by owner and finalized failed (2.6).
	// Zero (default) disables termination; negative disables explicitly.
	TaskJobDeadline time.Duration

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
	maxOutputChars := cfg.MaxTokens / 2 * 4
	if maxOutputChars <= 0 || maxOutputChars > toolOutputCapChars {
		maxOutputChars = toolOutputCapChars
	}
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
	taskManager := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			bus.Publish(newTaskSettledEvent(tk, sig, settleInlineCapChars, outputWorkspace))
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
		// Zero → task package default (1h); negative disables observation.
		StaleAfter: cfg.TaskStaleAfter,
		// Optional job termination policy; zero (default) = disabled.
		JobDeadline: cfg.TaskJobDeadline,
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
		droppedOutputEvents: droppedOutputCounter,
		memStore:            memStore,
		memStoreRelease:     cfg.MemStoreRelease,
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
	cm := newContextManagerFromConfig(cfg, memPlugin, sessionSvc, bus, outputCh, projection, onEvent)
	taskRecords.cm = cm // R2: bind the record sink (late — hooks are best-effort nil-safe before this)
	ta.contextManager = cm
	ta.taskManager = taskManager
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
	workspace.NewCleaner(workspace.ToolOutputPath(cfg.WorkspaceRoot), cfg.WorkspaceCleanupInterval, cfg.WorkspaceCleanupMaxAge, cfg.WorkspaceCleanupMaxFiles).Start(cleanCtx)

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

// newContextManagerFromConfig creates a ContextManager from TagentConfig.
// Shared by NewTagentAgent and Run().
func newContextManagerFromConfig(cfg *TagentConfig, memPlugin *plugin.MemoryPlugin, sessionSvc session.Service, bus *EventBus, outputCh chan *event.Event, projection *compress.SessionProjection, onEvent func(evt *event.Event)) *ContextManager {
	copts := buildCompressorOpts(cfg)
	copts = append(copts, compress.WithTokenCounter(compress.NewDefaultTokenCounter()))
	compressor := compress.NewSmartCompressor(copts...)
	// Use system prompt from config (framework details are in AGENTS.md)
	systemPrompt := cfg.SystemPrompt

	cm := NewContextManager(ContextManagerConfig{
		Name:                 cfg.Name,
		Model:                cfg.Model,
		Tools:                cfg.Tools,
		SystemPrompt:         systemPrompt,
		SystemPromptSource:   cfg.SystemPromptSource,
		Temperature:          cfg.Temperature,
		MaxToolIters:         cfg.MaxToolIterations,
		ThinkingEnabled:      cfg.ThinkingEnabled,
		ThinkingTokens:       cfg.ThinkingTokens,
		ReasoningEffort:      cfg.ReasoningEffort,
		ReasoningContentMode: cfg.ReasoningContentMode,
		Compressor:           compressor,
		TokenCounter:         compress.NewDefaultTokenCounter(),
		MaxTokens:            cfg.MaxTokens,
		ThresholdPct:         cfg.CompressThreshold,
		CompactKeysListed:    cfg.Compress.CompactKeysListed,
		RecentFullCount:      cfg.Compress.RecentFullCount,
		CardMaxChars:         cfg.Compress.CardMaxChars,
		MemStore:             cfg.MemoryStore,
		MemPlugin:            memPlugin,
		SessionSvc:           sessionSvc,
		OutputCh:             outputCh,
		Bus:                  bus,
		Projection:           projection,
		OnEvent:              onEvent,
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

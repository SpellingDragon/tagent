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
	"sync"
	"sync/atomic"
	"time"

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
	// TaskSpawner; background settles are published back to persistentBus as
	// task_settled events by its OnSettle hook.
	taskManager *TaskManager

	// Framework integration
	memStore   memory.MemoryStore
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

	// External events pending ingestion (set before Run)
	// These are converted to internal context messages at the start of the next run.
	pendingExternalEvents []memory.FullEvent

	// Resource closers — components like ActionTool that need cleanup on shutdown.
	// Closed in Close() before the runner is stopped.
	closers []Closer

	// TrajectoryRecorder (optional) — records LLM calls to JSONL when enabled.
	// Set via SetTrajectoryRecorder. StartLoop calls SetSessionInfo on it.
	trajectoryRecorder *rl.TrajectoryRecorder

	// Persistent Event Loop — 持久事件循环（StartLoop 模式）
	outputCh   chan *event.Event  // 持久输出 channel（Loop 模式下不关闭）
	loopCtx    context.Context    // Loop context（StopLoop 取消）
	loopCancel context.CancelFunc // Loop cancel
	loopActive atomic.Bool        // Loop 是否运行中
	loopWg     sync.WaitGroup     // 等待 Loop goroutine 退出

	// Meditation manager — started/stopped with the persistent event loop.
	meditationMgr *MeditationManager

	// cleanupCancel stops the workspace cleaner goroutine (started in NewTagentAgent).
	cleanupCancel context.CancelFunc

	// projection is the lightweight, bounded Session projection (EventReference[])
	// shared by onEvent and Preprocessor. It is created per TagentAgent and
	// passed to each invocation's AgentLoop.
	projection *SessionProjection

	// asyncTaskCheckers are checked by Run() before returning.
	// If any checker reports pending async tasks, Run() continues waiting
	// instead of returning immediately (call stack semantics: don't pop
	// until all async tasks complete).
	asyncTaskCheckers []AsyncTaskChecker
}

// AsyncTaskChecker is implemented by tools that have pending async operations.
// Run() calls HasPendingAsyncTasks() before returning a final response;
// if true, it continues waiting for async results to arrive via InjectMessage.
type AsyncTaskChecker interface {
	HasPendingAsyncTasks() bool
}

// TagentConfig holds configuration for creating a TagentAgent.
type TagentConfig struct {
	Model              model.Model        // Required: LLM model
	MemoryStore        memory.MemoryStore // Optional: external MemoryStore (default: InMemoryStore)
	SystemPrompt       string             // System prompt loaded from AGENTS.md/SOUL.md/USER.md/TOOLS.md
	SystemPromptSource *prompt.Source     // Hot-reloadable system prompt (optional, overrides SystemPrompt)
	Tools              []tool.Tool        // CallableTools to register
	MaxToolIterations  int                // Default: DefaultMaxToolIterations (50)
	MaxTokens          int                // Token budget for context (default: 8000)
	CompressThreshold  float64            // Compression trigger threshold (default: 0.8)
	SummaryModel       model.Model        // Optional: for Stage 2 LLM summary
	Temperature        float64            // Optional: LLM temperature (default: 0.7)
	KeepRecentTasks    int                // Min task segments to keep during compression (default: 2)
	Compress           CompressConfig     // SmartCompressor parameters

	// TaskSettledMaxInline caps the inline result length in a task_settled
	// notification (default: DefaultTaskSettledMaxInline); the full result
	// stays retrievable via get_task_result.
	TaskSettledMaxInline int

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

	// WorkspaceRoot is the unified on-disk scratch root (default: .tagent-workspace).
	// Oversized tool outputs go to <root>/tool-output; the tmux command working
	// directory is <root>/exec. A periodic cleaner bounds tool-output files.
	WorkspaceRoot string

	// Workspace cleanup (periodic, tool-output dir only). Non-positive values
	// fall back to the defaults below (there is no disable switch).
	WorkspaceCleanupInterval time.Duration // default: 1h
	WorkspaceCleanupMaxAge   time.Duration // default: 24h
	WorkspaceCleanupMaxFiles int           // default: 200
}

// Default configuration values
const (
	DefaultMaxToolIterations         = 50
	DefaultSubAgentMaxToolIterations = 10
	DefaultAgentName                 = "tagent"
	DefaultAgentDescription          = "TagentAgent - AI assistant powered by tagent"

	// DefaultTaskSettledMaxInline caps the inline result in task_settled
	// notifications; the full result stays available via get_task_result.
	DefaultTaskSettledMaxInline = 2000
	// DefaultResumeContextRounds caps the rounds restored by the subagent
	// task-chain restorer on resume.
	DefaultResumeContextRounds = 3

	// Workspace cleanup defaults (periodic bounding of on-disk scratch files).
	DefaultWorkspaceCleanupInterval = time.Hour
	DefaultWorkspaceCleanupMaxAge   = 24 * time.Hour
	DefaultWorkspaceCleanupMaxFiles = 200
)

// CompressConfig holds SmartCompressor parameters.
type CompressConfig struct {
	MaxToolResultChars int
	MaxExecStateChars  int
	ChunkSummaryLen    int
	// MaxNoticeChars caps the compress-notice text length (default 800).
	MaxNoticeChars int
	// CompactKeysListed caps the number of keys listed in the rolling
	// compaction summary (default 32); older events stay recallable.
	CompactKeysListed int
	// RecentFullCount is the number of most recent refs resolved with full
	// content from MemoryStore (default 4).
	RecentFullCount int
	// CardMaxChars caps the index-card section of the rolling compaction
	// summary (default DefaultCardMaxChars); beyond it old card lines are
	// LLM-condensed (or sink, without a summary model).
	CardMaxChars int
	// ArchiveCacheCap bounds the per-process L3 archive cache entries
	// (default DefaultArchiveCacheCap).
	ArchiveCacheCap int
}

// NewTagentAgent creates a new TagentAgent with the given configuration.
//
// In the event-driven architecture, NewTagentAgent:
//   - Creates MemoryStore + MemoryPlugin + SmartCompressor
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
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.CompressThreshold <= 0 || cfg.CompressThreshold > 1 {
		cfg.CompressThreshold = DefaultCompressThreshold
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
	maxOutputChars := cfg.MaxTokens / 2 * 4
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
	bus := NewEventBus()
	projection := NewSessionProjection()

	// Task layer: tools spawn long-running work via the injected TaskSpawner;
	// when a task settles in the background (after its sync-wait window), the
	// OnSettle hook publishes a task_settled event onto the bus, which the
	// persistent loop reclaims into a new turn (idle → wakes Pull; mid-turn →
	// buffered until the current turn finishes — single-consumer queueing).
	taskManager := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			bus.Publish(newTaskSettledEvent(task, sig, cfg.TaskSettledMaxInline))
		},
	})

	// onEventRef is set after TagentAgent creation. The AppendEventHook
	// uses it to propagate meta_* onto user message events before forwarding
	// them to outputCh (delivery only — projection writes happen in the
	// event-plugin pipeline via ProjectionSink, unified-event-projection D1).
	var onEventRef func(evt *event.Event)

	// 6. Create SessionService
	// Limit session events to 2: only the current invocation's user message
	// and the latest tool result are needed for ContentRequestProcessor's
	// TimelineFilterCurrentRequest. Historical context is managed entirely
	// by SessionProjection + ContextCompressor, so the runner session does
	// not need to retain full event history.
	//
	// AppendEventHook forwards user message events to outputCh (the runner
	// appends user messages to session but does NOT emit them through the
	// agent event channel — without this hook the consumer would never see
	// them).
	sessionSvc := sessioninmemory.NewSessionService(
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
					log.Warnf("[SessionHook] outputCh full, user message event dropped")
				}
			}

			return err
		}),
	)

	// 7. Create TagentAgent (without contextManager yet — wired after callback creation)
	ta := &TagentAgent{
		persistentBus: bus,
		activeBus:     bus,
		memStore:      memStore,
		memPlugin:     memPlugin,
		config:        cfg,
		sessionSvc:    sessionSvc,
		name:          name,
		description:   description,
		outputCh:      outputCh,
		closers:       []Closer{},
		projection:    projection,
	}

	// 8. Create onEvent callback and ContextManager.
	onEvent := ta.makeOnEventCallback()
	onEventRef = onEvent // Wire the hook's callback.
	cm := newContextManagerFromConfig(cfg, memPlugin, sessionSvc, bus, outputCh, projection, onEvent)
	ta.contextManager = cm
	ta.taskManager = taskManager
	cm.taskController = taskManager

	// Initialize meditation manager if enabled.
	if cfg.Meditation.Enabled {
		ta.meditationMgr = NewMeditationManager(cfg.Meditation, ta)
		// Feed the read-only task controller so meditation carries a self-state
		// digest (task-layer health). taskManager is always non-nil here.
		ta.meditationMgr.SetTaskController(taskManager)
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

// buildCompressorOpts builds SmartCompressor options from TagentConfig.
// Shared by NewTagentAgent and Run() to avoid duplicating option-building logic.
func buildCompressorOpts(cfg *TagentConfig) []SmartCompressorOption {
	opts := []SmartCompressorOption{
		WithMaxTokens(cfg.MaxTokens),
	}
	if cfg.KeepRecentTasks > 0 {
		opts = append(opts, WithKeepRecentTasks(cfg.KeepRecentTasks))
	}
	if cfg.SummaryModel != nil {
		opts = append(opts, WithSummaryModel(cfg.SummaryModel))
	}
	// Compress config
	if cfg.Compress.MaxToolResultChars > 0 {
		opts = append(opts, WithMaxToolResultChars(cfg.Compress.MaxToolResultChars))
	}
	if cfg.Compress.MaxExecStateChars > 0 {
		opts = append(opts, WithMaxExecStateChars(cfg.Compress.MaxExecStateChars))
	}
	if cfg.Compress.ChunkSummaryLen > 0 {
		opts = append(opts, WithChunkSummaryLen(cfg.Compress.ChunkSummaryLen))
	}
	if cfg.Compress.MaxNoticeChars > 0 {
		opts = append(opts, WithMaxNoticeChars(cfg.Compress.MaxNoticeChars))
	}
	if cfg.Compress.ArchiveCacheCap > 0 {
		opts = append(opts, WithArchiveCacheCap(cfg.Compress.ArchiveCacheCap))
	}
	return opts
}

// newCompressorFromConfig creates a SmartCompressor from TagentConfig.
func newCompressorFromConfig(cfg *TagentConfig) *SmartCompressor {
	return NewSmartCompressor(buildCompressorOpts(cfg)...)
}

// newContextManagerFromConfig creates a ContextManager from TagentConfig.
// Shared by NewTagentAgent and Run().
func newContextManagerFromConfig(cfg *TagentConfig, memPlugin *plugin.MemoryPlugin, sessionSvc session.Service, bus *EventBus, outputCh chan *event.Event, projection *SessionProjection, onEvent func(evt *event.Event)) *ContextManager {
	copts := buildCompressorOpts(cfg)
	copts = append(copts, WithTokenCounter(NewDefaultTokenCounter()))
	// Inject MemStore and Projection into SmartCompressor for chunk persistence
	if cfg.MemoryStore != nil {
		copts = append(copts, WithMemStore(cfg.MemoryStore))
	}
	if projection != nil {
		copts = append(copts, WithProjection(projection))
	}
	compressor := NewSmartCompressor(copts...)
	// Use system prompt from config (framework details are in AGENTS.md)
	systemPrompt := cfg.SystemPrompt

	return NewContextManager(ContextManagerConfig{
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
		TokenCounter:         NewDefaultTokenCounter(),
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
}

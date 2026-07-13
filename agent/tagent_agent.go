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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	a2ago "trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	a2aserver "trpc.group/trpc-go/trpc-agent-go/server/a2a"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
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
	trajectoryRecorder *TrajectoryRecorder

	// Persistent Event Loop — 持久事件循环（StartLoop 模式）
	outputCh   chan *event.Event  // 持久输出 channel（Loop 模式下不关闭）
	loopCtx    context.Context    // Loop context（StopLoop 取消）
	loopCancel context.CancelFunc // Loop cancel
	loopActive atomic.Bool        // Loop 是否运行中
	loopWg     sync.WaitGroup     // 等待 Loop goroutine 退出

	// Meditation manager — started/stopped with the persistent event loop.
	meditationMgr *MeditationManager

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
	Model             model.Model        // Required: LLM model
	MemoryStore       memory.MemoryStore // Optional: external MemoryStore (default: InMemoryStore)
	SystemPrompt      string             // System prompt loaded from AGENTS.md/SOUL.md/USER.md/TOOLS.md
	Tools             []tool.Tool        // CallableTools to register
	MaxToolIterations int                // Default: 200
	MaxTokens         int                // Token budget for context (default: 8000)
	CompressThreshold float64            // Compression trigger threshold (default: 0.8)
	SummaryModel      model.Model        // Optional: for Stage 2 LLM summary
	Temperature       float64            // Optional: LLM temperature (default: 0.7)
	KeepRecentTasks   int                // Min task segments to keep during compression (default: 2)
	Compress          CompressConfig     // SmartCompressor parameters

	// Thinking/reasoning controls (merged into model.GenerationConfig)
	ThinkingEnabled     *bool
	ThinkingTokens      *int
	ReasoningEffort     *string
	ReasoningContentMode string

	// Agent identity (for agent.Agent interface)
	Name        string // Default: "tagent"
	Description string // Default: "TagentAgent - AI assistant powered by tagent"

	// Meditation configures the meditation/heartbeat mechanism.
	Meditation MeditationConfig
}

// Default configuration values
const (
	DefaultMaxToolIterations         = 50
	DefaultSubAgentMaxToolIterations = 10
	DefaultMaxTokens                 = 8000
	DefaultCompressThreshold         = 0.8
	DefaultAgentName                 = "tagent"
	DefaultAgentDescription          = "TagentAgent - AI assistant powered by tagent"

	// Default compress parameters
	DefaultMaxExecStateChars = 2000

	DefaultMaxToolResultChars = 500
	DefaultMaxToolArgsChars   = 80
	DefaultChunkSize          = 1000
	DefaultChunkSummaryLen    = 150
)

// CompressConfig holds SmartCompressor parameters.
type CompressConfig struct {
	MaxToolResultChars int
	MaxExecStateChars  int
	ChunkSize          int
	ChunkSummaryLen    int

	// Value-driven compression (ideal path — no compatibility toggle)
	//
	// ValueFloors maps event type strings to minimum value_score (0.0-1.0).
	// The LLM valuator's output is clamped to at least the floor for each type.
	ValueFloors map[string]float64
	// ValuationTimeoutMs caps the wall-clock time for the entire valuation phase.
	// 0 = no timeout.
	ValuationTimeoutMs int
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
	if cfg.Compress.ChunkSize > 0 {
		opts = append(opts, WithChunkSize(cfg.Compress.ChunkSize))
	}
	if cfg.Compress.ChunkSummaryLen > 0 {
		opts = append(opts, WithChunkSummaryLen(cfg.Compress.ChunkSummaryLen))
	}
	// Valuation config
	valCfg := ValuationConfig{
		ValueFloors: cfg.Compress.ValueFloors,
	}
	if valCfg.ValueFloors == nil {
		valCfg.ValueFloors = DefaultValuationFloors()
	}
	if cfg.Compress.ValuationTimeoutMs > 0 {
		valCfg.Timeout = time.Duration(cfg.Compress.ValuationTimeoutMs) * time.Millisecond
	}
	opts = append(opts, WithValuationConfig(valCfg))
	// Build EventValuator from summary model if available
	if cfg.SummaryModel != nil {
		opts = append(opts, WithEventValuator(NewLLMEventValuator(cfg.SummaryModel, valCfg)))
	} else {
		opts = append(opts, WithEventValuator(NewNoopValuator()))
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
	compressor := newCompressorFromConfig(cfg)
	compressor.tokenCounter = NewDefaultTokenCounter()
	// Inject MemStore and Projection into SmartCompressor for chunk persistence
	if cfg.MemoryStore != nil {
		compressor.memStore = cfg.MemoryStore
	}
	if projection != nil {
		compressor.projection = projection
	}
	// Use system prompt from config (framework details are in AGENTS.md)
	systemPrompt := cfg.SystemPrompt

	return NewContextManager(ContextManagerConfig{
		Name:                 cfg.Name,
		Model:                cfg.Model,
		Tools:                cfg.Tools,
		SystemPrompt:         systemPrompt,
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
		MemStore:             cfg.MemoryStore,
		MemPlugin:            memPlugin,
		SessionSvc:           sessionSvc,
		OutputCh:             outputCh,
		Bus:          bus,
		Projection:   projection,
		OnEvent:      onEvent,
	})
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
	outputWorkspace := ".tagent-output"
	if maxOutputChars > 0 && len(cfg.Tools) > 0 {
		wrapped := make([]tool.Tool, len(cfg.Tools))
		for i, t := range cfg.Tools {
			olt := NewOutputLimitTool(t, maxOutputChars)
			olt.SetWorkspace(outputWorkspace)
			wrapped[i] = olt
		}
		cfg.Tools = wrapped
	}

	// 5. Create SessionService
	// Limit session events to 2: only the current invocation's user message
	// and the latest tool result are needed for ContentRequestProcessor's
	// TimelineFilterCurrentRequest. Historical context is managed entirely
	// by SessionProjection + ContextCompressor, so the runner session does
	// not need to retain full event history.
	sessionSvc := sessioninmemory.NewSessionService(
		sessioninmemory.WithSessionEventLimit(2),
		sessioninmemory.WithAppendEventHook(func(ctx *session.AppendEventContext, next func() error) error {
			original := ctx.Event
			if original.Response != nil {
				evtCopy := *original
				evtCopy.Response = original.Response.Clone()
				ctx.Event = &evtCopy
			}
			err := next()
			ctx.Event = original
			return err
		}),
	)

	// 6. Create outputCh + EventBus + projection
	outputCh := make(chan *event.Event, 100)
	bus := NewEventBus()
	projection := NewSessionProjection()

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
	onEvent := ta.makeOnEventCallback("", projection)
	cm := newContextManagerFromConfig(cfg, memPlugin, sessionSvc, bus, outputCh, projection, onEvent)
	ta.contextManager = cm

	// Initialize meditation manager if enabled.
	if cfg.Meditation.Enabled {
		ta.meditationMgr = NewMeditationManager(cfg.Meditation, ta)
	}

	return ta, nil
}

// Run implements agent.Agent interface.
//
// In the event-driven architecture, Run is the sub-agent invocation path
// (used by AgentToolWrapper for local sub-agent calls and A2A for remote calls).
// Top-level usage must use StartLoop/InjectMessage/StopLoop instead.
//
// Run creates a fresh EventBus + AgentLoop for this invocation, publishes
// the initial message as external_input, and returns the AgentLoop's
// outputCh. The caller reads events until the channel closes (context
// cancelled or agent_output produced).
//
// Context can arrive via two paths:
//  1. RuntimeState path (remote/wrapper): inv.RunOptions.RuntimeState["external_context"]
//     contains serialized ExternalContextEntry JSON. This is the A2A-compatible path.
//  2. Struct field path (direct API): pendingExternalEvents set via IngestExternalEvents.
func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	// Extract or generate userID and sessionID from Invocation.
	userID := "tagent-user"
	sessionID := fmt.Sprintf("tagent-session-%s", inv.InvocationID)

	// Store session context for event injection.
	ta.setSessionContext(userID, sessionID)

	// Path 1: Read external context from RuntimeState (remote/wrapper path).
	if inv.RunOptions.RuntimeState != nil {
		if raw, ok := inv.RunOptions.RuntimeState[ExternalContextKey]; ok {
			var data []byte
			switch v := raw.(type) {
			case json.RawMessage:
				data = v
			case []byte:
				data = v
			case string:
				data = []byte(v)
			}
			if len(data) > 0 {
				events, err := deserializeExternalContext(data)
				if err != nil {
					log.Warnf("[Run] failed to deserialize external context: %v", err)
				} else if len(events) > 0 {
					ta.IngestExternalEvents(events)
				}
			}
		}
	}

	message := inv.Message
	if message.Content == "" {
		message = model.NewUserMessage("")
	}

	// Path 2: Prepend external event context if pending.
	if len(ta.pendingExternalEvents) > 0 {
		message = ta.injectExternalContext(message)
	}

	// Validate required fields before creating sub-agent AgentLoop.
	if ta.config == nil || ta.config.Model == nil {
		return nil, fmt.Errorf("agent %q: config or model is nil", ta.name)
	}

	// Create a fresh EventBus + AgentLoop for this invocation.
	// Each sub-agent invocation gets its own isolated bus and compressor
	// (SmartCompressor has mutable state and must not be shared across
	// concurrent goroutines).
	invBus := NewEventBus()
	ta.setActiveBus(invBus)

	invOutputCh := make(chan *event.Event, 100)
	invProjection := NewSessionProjection()
	invOnEvent := ta.makeOnEventCallback(sessionID, invProjection)
	maxToolIters := DefaultSubAgentMaxToolIterations
	if ta.config.MaxToolIterations > 0 && ta.config.MaxToolIterations < maxToolIters {
		maxToolIters = ta.config.MaxToolIterations
	}
	invCfg := *ta.config
	invCfg.MaxToolIterations = maxToolIters
	if invCfg.Name == "" {
		invCfg.Name = ta.name
	}
	invCM := newContextManagerFromConfig(&invCfg, ta.memPlugin, ta.sessionSvc, invBus, invOutputCh, invProjection, invOnEvent)
	invCM.SetUserIDSessionID(ta.lastUserID, sessionID)

	// Publish the initial message as external_input.
	invBus.Publish(NewExternalInputEvent("user", message))

	// Run the event loop in a goroutine. It will exit when ctx is cancelled.
	runCtx, runCancel := context.WithCancel(ctx)
	go func() {
		defer close(invOutputCh)
		defer invCM.Close() // Release temporary Runner resources after runEventLoop exits
		ta.runEventLoop(runCtx, invBus, invCM)
	}()

	// Wrap the outputCh: forward events and cancel the loop when the
	// caller stops reading OR when the first agent_output is emitted
	// (sub-agent single-turn semantics).
	// EXCEPTION: if asyncTaskCheckers report pending tasks, continue waiting
	// for async results instead of returning immediately (call stack semantics).
	wrappedCh := make(chan *event.Event, cap(invOutputCh))
	go func() {
		defer close(wrappedCh)
		defer runCancel()
		defer ta.restorePersistentBus() // restore activeBus to persistentBus
		for evt := range invOutputCh {
			wrappedCh <- evt
			// Sub-agent semantics: stop after the first agent_output
			// (final response without tool_calls).
			if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				if len(choice.Message.ToolCalls) == 0 {
					if choice.Message.Content == "" {
						log.Warnf("[Run] sub-agent %q returned empty final response, treating as complete", ta.name)
					}

					// Drain mode: forward remaining tail events (e.g., MemoryPlugin
					// persistence, RequiresCompletion) for up to 500ms before exiting.
					// This prevents context cancellation from dropping tail events.
					drainTimer := time.NewTimer(500 * time.Millisecond)
					defer drainTimer.Stop()
					for {
						select {
						case tailEvt, ok := <-invOutputCh:
							if !ok {
								return // invOutputCh closed
							}
							wrappedCh <- tailEvt
						case <-drainTimer.C:
							return // drain timeout
						}
					}
				}
			}
		}
	}()

	return wrappedCh, nil
}

// RunSimple is removed. Top-level usage must use StartLoop/InjectMessage/StopLoop.
// Sub-agent invocation goes through agent.Run() via AgentToolWrapper.Call().

// Tools implements agent.Agent interface.
func (ta *TagentAgent) Tools() []tool.Tool {
	if ta.config != nil {
		return ta.config.Tools
	}
	return nil
}

// Info implements agent.Agent interface.
func (ta *TagentAgent) Info() agent.Info {
	return agent.Info{
		Name:        ta.name,
		Description: ta.description,
	}
}

// SubAgents implements agent.Agent interface.
// In the event-driven architecture, sub-agents are managed via AgentToolWrapper,
// not via the framework's sub-agent mechanism.
func (ta *TagentAgent) SubAgents() []agent.Agent {
	return nil
}

// FindSubAgent implements agent.Agent interface.
func (ta *TagentAgent) FindSubAgent(name string) agent.Agent {
	return nil
}

// RegisterCloser registers a component to be closed on agent shutdown.
// Components are closed in registration order before the runner is stopped.
func (ta *TagentAgent) RegisterCloser(c Closer) {
	ta.closers = append(ta.closers, c)
}

// SetTrajectoryRecorder sets the trajectory recorder for this agent.
// When set, StartLoop will automatically call SetSessionInfo on it.
// The recorder should also be registered via RegisterCloser for graceful shutdown.
func (ta *TagentAgent) SetTrajectoryRecorder(tr *TrajectoryRecorder) {
	ta.trajectoryRecorder = tr
}

// TrajectoryRecorder returns the trajectory recorder if one is set, or nil.
func (ta *TagentAgent) TrajectoryRecorder() *TrajectoryRecorder {
	return ta.trajectoryRecorder
}

// Close closes all registered resources and the runner.
// Closers (e.g., ActionTool) are stopped first, then the MemoryStore
// (if it supports closing), and finally the runner.
func (ta *TagentAgent) Close() error {
	var errs []error

	// Stop Persistent Event Loop first if active
	if ta.loopActive.Load() {
		ta.StopLoop()
	}

	// Close registered closers first (e.g., ActionTool stops TmuxMonitor)
	for _, c := range ta.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close resource: %w", err))
		}
	}

	// Close memory store if it supports closing (e.g., FileSegmentStore stops lifecycle components)
	if c, ok := ta.memStore.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close memory store: %w", err))
		}
	}

	// Close ContextManager (closes unified Runner)
	if ta.contextManager != nil {
		if err := ta.contextManager.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close context manager: %w", err))
		}
	}

	// Close TrajectoryRecorder (flush writeLoop + close files)
	// Must be after contextManager.Close() so no new LLM calls are made.
	if ta.trajectoryRecorder != nil {
		if err := ta.trajectoryRecorder.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close trajectory recorder: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// MemStore returns the MemoryStore for direct access (e.g., by RecallTool).
func (ta *TagentAgent) MemStore() memory.MemoryStore {
	return ta.memStore
}

// Runner returns the underlying Runner from ContextManager.
func (ta *TagentAgent) Runner() runner.Runner {
	if ta.contextManager != nil {
		return ta.contextManager.runner
	}
	return nil
}

// SetToolParentProjection wires the agent's SessionProjection to all
// AgentToolWrapper instances in the tool list. This enables auto-inject
// of event_keys when LLM does not pass them.
// Must be called after NewTagentAgent (which creates the projection).
func (ta *TagentAgent) SetToolParentProjection() {
	if ta.projection == nil || ta.config == nil {
		return
	}
	for _, t := range ta.config.Tools {
		if wrapper, ok := t.(*AgentToolWrapper); ok {
			wrapper.SetParentProjection(ta.projection)
		}
	}
}

// InjectMessage injects a message into the agent's persistent EventBus.
//
// Messages ALWAYS go to persistentBus — never to invBus. This ensures that
// user messages sent during sub-agent execution are not lost when the
// sub-agent's invBus is discarded. The BeforeModel InjectBusInputs callback
// on the persistent ContextManager will TryPull these messages and inject them
// into the next ReAct iteration.
func (ta *TagentAgent) InjectMessage(msg model.Message) {
	if ta.meditationMgr != nil {
		ta.meditationMgr.UpdateLastEventTime(time.Now())
	}
	// Always use persistentBus, not activeBus.
	// activeBus may be invBus during sub-agent execution, but user messages
	// should go to the persistent bus so the main runEventLoop's BeforeModel
	// callback can pick them up.
	if ta.persistentBus != nil {
		ta.persistentBus.Publish(NewExternalInputEvent("inject", msg))
		return
	}
	// Fallback: if persistentBus is nil (shouldn't happen), use activeBus.
	ta.activeBusMu.Lock()
	bus := ta.activeBus
	ta.activeBusMu.Unlock()
	if bus != nil {
		bus.Publish(NewExternalInputEvent("inject", msg))
		return
	}
	log.Warnf("[InjectMessage] agent %q has no bus, message dropped", ta.name)
}

// setActiveBus sets the current active bus for event injection.
// Called by StartLoop (sets persistentBus) and Run() (sets invBus).
func (ta *TagentAgent) setActiveBus(bus *EventBus) {
	ta.activeBusMu.Lock()
	ta.activeBus = bus
	ta.activeBusMu.Unlock()
}

// restorePersistentBus switches the active bus back to the persistent bus.
// Called when a sub-agent Run() completes so InjectMessage resumes routing
// to the persistent AgentLoop (if active).
func (ta *TagentAgent) restorePersistentBus() {
	ta.setActiveBus(ta.persistentBus)
}

// setSessionContext sets the userID and sessionID (thread-safe).
func (ta *TagentAgent) setSessionContext(userID, sessionID string) {
	ta.sessionMu.Lock()
	defer ta.sessionMu.Unlock()
	ta.lastUserID = userID
	ta.lastSessionID = sessionID
}

// getOrCreateSession returns the session for the given sessionID (or the
// last-known sessionID if empty). Creates the session if it does not exist.
func (ta *TagentAgent) getOrCreateSession(sessionID ...string) *session.Session {
	if ta.sessionSvc == nil {
		return nil
	}
	sid := ta.lastSessionID
	if len(sessionID) > 0 && sessionID[0] != "" {
		sid = sessionID[0]
	}
	key := session.Key{
		AppName:   ta.name,
		UserID:    ta.lastUserID,
		SessionID: sid,
	}
	ctx := context.Background()
	sess, err := ta.sessionSvc.GetSession(ctx, key)
	if err != nil || sess == nil {
		sess, err = ta.sessionSvc.CreateSession(ctx, key, nil)
		if err != nil {
			log.Errorf("[getOrCreateSession] CreateSession failed: %v", err)
			return nil
		}
	}
	return sess
}

// makeOnEventCallback creates the onEvent callback for StartLoop and Run().
// It only performs projection.Append — sessionSvc.AppendEvent and
// MemoryPlugin.OnEvent are handled by the framework Runner internally.
func (ta *TagentAgent) makeOnEventCallback(sessionID string, projection *SessionProjection) func(evt *event.Event) {
	return func(evt *event.Event) {
		if evt == nil || projection == nil {
			return
		}
		if ref, ok := BuildEventReference(evt); ok {
			projection.Append(ref)
		}
	}
}

// IngestExternalEvents queues external events to be injected as context
// into the next Run call. This is the mechanism for passing
// context from a parent agent to a tool agent via AgentToolWrapper.
//
// The events are converted to a system message summarizing the external
// context and prepended to the user message. After injection, the pending
// events are cleared.
func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent) {
	ta.pendingExternalEvents = events
}

// injectExternalContext converts pending external events into a context message
// prepended to the user message. After injection, the pending events are cleared.
//
// Only EventSummary is injected — NOT the full Content. This keeps external context
// compact so sub-agents stay within their token budget. The sub-agent retrieves full
// event details via its own memory tools (memory_get, memory_query) if needed.
func (ta *TagentAgent) injectExternalContext(msg model.Message) model.Message {
	events := ta.pendingExternalEvents
	ta.pendingExternalEvents = nil // Clear after consumption

	if len(events) == 0 {
		return msg
	}

	// Build external context summary (EventSummary only — compact, no full Content)
	var contextBuilder string
	contextBuilder = "[External Context from Parent Agent]\n\n"
	for i, evt := range events {
		contextBuilder += fmt.Sprintf("Event %d: [%s] %s\n", i+1, evt.EventType, evt.EventSummary)
	}
	contextBuilder += "\n[End of External Context]\n\n"

	log.Infof("[InjectContext] injecting %d external events, context_len=%d", len(events), len(contextBuilder))

	// Prepend external context to the user message
	msg.Content = contextBuilder + msg.Content
	return msg
}

// ---------------------------------------------------------------------------
// Persistent Event Loop — 持久事件循环
//
// runEventLoop is the core event loop, mirroring the prototype's DefaultRun.
// It pulls events from EventBus, merges them via ContextManager.BuildInvocation,
// and executes the framework Flow via ContextManager.RunFlow.
// ---------------------------------------------------------------------------

// runEventLoop is the core event loop (prototype's DefaultRun equivalent).
// It blocks until ctx is cancelled.
// On RunFlow failure, uses exponential backoff retry (100ms→200ms→400ms, max 3).
// After exhausting retries, publishes an error event to EventBus and continues.
func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager) {
	const maxRetries = 3
	retryDelays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

	for {
		if err := ctx.Err(); err != nil {
			log.Infof("[runEventLoop:%s] ctx cancelled, exiting: %v", ta.name, err)
			return
		}

		events, err := bus.Pull(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Infof("[runEventLoop:%s] Pull returned: %v, exiting", ta.name, err)
				return
			}
			log.Errorf("[runEventLoop:%s] Pull error: %v", ta.name, err)
			return
		}
		if len(events) == 0 {
			continue
		}
		log.Infof("[runEventLoop:%s] iteration start: pulled %d events (%s)",
			ta.name, len(events), summarizeEvents(events))

		msg := cm.BuildInvocation(events)
		if msg.Content == "" {
			log.Debugf("[runEventLoop:%s] empty message after merge, skipping", ta.name)
			continue
		}

		// RunFlow with exponential backoff retry
		var lastErr error
		retried := false
		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				// Check ctx before retrying
				if err := ctx.Err(); err != nil {
					log.Infof("[runEventLoop:%s] ctx cancelled during retry, exiting: %v", ta.name, err)
					return
				}
				delay := retryDelays[attempt-1]
				log.Warnf("[runEventLoop:%s] RunFlow retry %d/%d after %v", ta.name, attempt, maxRetries, delay)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					log.Infof("[runEventLoop:%s] ctx cancelled during retry wait, exiting", ta.name)
					return
				}
			}

			if err := cm.RunFlow(ctx, msg); err != nil {
				lastErr = err
				log.Errorf("[runEventLoop:%s] RunFlow failed (attempt %d/%d): %v", ta.name, attempt+1, maxRetries+1, err)
				if attempt < maxRetries {
					retried = true
					continue
				}
				// Retries exhausted — publish error event to EventBus
				log.Errorf("[runEventLoop:%s] RunFlow exhausted %d retries, publishing error event", ta.name, maxRetries)
				ta.publishErrorEvent(bus, lastErr)
			} else {
				lastErr = nil
				break
			}
		}

		if lastErr != nil && !retried {
			// Single failure without retry (shouldn't happen with current logic, but defensive)
			log.Errorf("[runEventLoop:%s] RunFlow failed: %v", ta.name, lastErr)
		}
	}
}

// publishErrorEvent publishes an error event to EventBus so external
// listeners can be aware of RunFlow failures.
func (ta *TagentAgent) publishErrorEvent(bus *EventBus, runErr error) {
	if bus == nil || runErr == nil {
		return
	}
	errMsg := fmt.Sprintf("[error] RunFlow failed after retries: %v", runErr)
	busEvt := &AgentEvent{
		ID:        uuid.NewString(),
		Type:      tagentevent.TypeExternalInput,
		Source:    "error",
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleSystem, Content: errMsg},
		Metadata:  make(map[string]any),
	}
	bus.Publish(busEvt)
}

// summarizeEvents returns a compact summary of event types in a batch.
func summarizeEvents(events []*AgentEvent) string {
	counts := make(map[string]int)
	for _, evt := range events {
		if evt == nil {
			counts["nil"]++
			continue
		}
		counts[evt.Type]++
	}
	var parts []string
	for typ, n := range counts {
		parts = append(parts, fmt.Sprintf("%s:%d", typ, n))
	}
	return strings.Join(parts, ", ")
}

// StartLoop starts the persistent event loop.
// It creates an EventBus, launches an AgentLoop goroutine, and returns
// the outputCh for callers to receive agent_output events.
//
// Subsequent calls with the same agent return the existing outputCh.
// The outputCh is closed when StopLoop is called.
func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error) {
	// Use sessionMu to prevent concurrent StartLoop calls from racing
	// on the loopActive check + initialization sequence.
	ta.sessionMu.Lock()
	if ta.loopActive.Load() {
		ta.sessionMu.Unlock()
		return ta.outputCh, nil
	}

	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())
	ta.loopActive.Store(true)
	ta.sessionMu.Unlock()

	// Cache session context for event injection.
	ta.setSessionContext(userID, sessionID)

	// Create or attach session for the persistent loop.
	sess := ta.getOrCreateSession(sessionID)
	_ = sess // session managed by ContextManager's Runner

	// Update ContextManager with session context.
	ta.contextManager.SetUserIDSessionID(userID, sessionID)

	// Set TrajectoryRecorder session info (if enabled).
	if ta.trajectoryRecorder != nil {
		ta.trajectoryRecorder.SetSessionInfo(userID, sessionID)
	}

	// Launch runEventLoop in a dedicated goroutine.
	ta.loopWg.Add(1)
	go func() {
		defer ta.loopWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[StartLoop] runEventLoop panic recovered: %v", r)
			}
			close(ta.outputCh)
		}()
		ta.runEventLoop(ta.loopCtx, ta.persistentBus, ta.contextManager)
	}()

	// Start meditation manager (if configured).
	if ta.meditationMgr != nil {
		ta.meditationMgr.Start()
	}

	log.Infof("[StartLoop] persistent event loop started user=%s session=%s", userID, sessionID)
	return ta.outputCh, nil
}

// StopLoop stops the persistent event loop.
// Cancels the loop context, waits for the AgentLoop goroutine to exit.
func (ta *TagentAgent) StopLoop() {
	if !ta.loopActive.Load() {
		return
	}
	ta.loopActive.Store(false)

	// Stop meditation manager first (stop injecting new meditation events).
	if ta.meditationMgr != nil {
		ta.meditationMgr.Stop()
	}

	ta.loopCancel()
	ta.loopWg.Wait()
	log.Infof("[StopLoop] persistent event loop stopped")
}

// truncateString truncates s to at most n characters, appending "..." if truncated.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---------------------------------------------------------------------------
// SwappableModel — 可运行时替换的 model.Model 包装器
//
// 用于 HTTPAPI 接收 AReaL adapter 传入的 llm_base_url 时，
// 将 LLM 请求重定向到 AReaL proxy（端口动态分配）。
// 不改变事件机制（persistent loop / InjectMessage / outputCh 不变），
// 仅替换底层 model.Model 实例。
// ---------------------------------------------------------------------------

// SwappableModel wraps a model.Model, allowing the inner model to be
// swapped at runtime without recreating the LLMAgent or Runner.
// All GenerateContent/Info calls delegate to the current inner model.
type SwappableModel struct {
	mu    sync.RWMutex
	inner model.Model
}

// NewSwappableModel creates a SwappableModel wrapping the given model.
func NewSwappableModel(m model.Model) *SwappableModel {
	return &SwappableModel{inner: m}
}

// Swap replaces the inner model atomically.
// In-flight GenerateContent calls continue with the old model;
// subsequent calls use the new model.
func (m *SwappableModel) Swap(inner model.Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inner = inner
}

// GenerateContent delegates to the current inner model.
func (m *SwappableModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.GenerateContent(ctx, request)
}

// Info delegates to the current inner model.
func (m *SwappableModel) Info() model.Info {
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.Info()
}

// NewA2AServer creates an A2A server that exposes the given TagentAgent.
func NewA2AServer(ta *TagentAgent, host string) (*a2ago.A2AServer, error) {
	if ta == nil {
		return nil, fmt.Errorf("tagent agent is required")
	}
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	srv, err := a2aserver.New(
		a2aserver.WithAgent(ta, true),
		a2aserver.WithHost(host),
	)
	if err != nil {
		return nil, fmt.Errorf("create A2A server: %w", err)
	}
	return srv, nil
}

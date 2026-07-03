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
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"

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
	// Event-driven core (replaces llmAgent + runner for execution)
	bus          *EventBus
	agentLoop    *AgentLoop
	preprocessor *Preprocessor

	// Framework integration (retained for session/plugin/trace)
	memStore   memory.MemoryStore
	memPlugin  *plugin.MemoryPlugin // direct reference for onEvent callback
	config     *TagentConfig
	runner     runner.Runner // retained as shell for session+plugin management
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

	// Agent identity (for agent.Agent interface)
	Name        string // Default: "tagent"
	Description string // Default: "TagentAgent - AI assistant powered by tagent"

	// Meditation configures the meditation/heartbeat mechanism.
	// Only effective when the agent is started via StartLoop.
	Meditation MeditationConfig
}

// Default configuration values
const (
	DefaultMaxToolIterations = 200
	DefaultMaxTokens         = 8000
	DefaultCompressThreshold = 0.8
	DefaultAgentName         = "tagent"
	DefaultAgentDescription  = "TagentAgent - AI assistant powered by tagent"
)

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
	return opts
}

// newPreprocessorFromConfig creates a Preprocessor from TagentConfig.
// Shared by NewTagentAgent and Run() so that Run() does not need to access
// the parent Preprocessor's private fields (maxTokens, tokenCounter, thresholdPct).
func newPreprocessorFromConfig(cfg *TagentConfig) *Preprocessor {
	compressor := NewSmartCompressor(buildCompressorOpts(cfg)...)
	counter := NewDefaultTokenCounter()
	return NewPreprocessor(compressor, counter, cfg.MaxTokens, cfg.CompressThreshold)
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

	// 1. Create MemoryStore (use provided or default to InMemoryStore)
	var memStore memory.MemoryStore
	if cfg.MemoryStore != nil {
		memStore = cfg.MemoryStore
	} else {
		memStore = memory.NewInMemoryStore()
	}

	// 2. Create MemoryPlugin (OnEvent: event persistence + causal chain + StateDelta)
	memPlugin := plugin.NewMemoryPlugin(memStore)

	// 3. Create Preprocessor (replacing ContextIntervention)
	preprocessor := newPreprocessorFromConfig(cfg)

	// Apply identity defaults
	name := cfg.Name
	if name == "" {
		name = DefaultAgentName
	}
	description := cfg.Description
	if description == "" {
		description = DefaultAgentDescription
	}

	// 4. Wrap all tools with OutputLimitTool
	maxOutputChars := cfg.MaxTokens / 2 * 4
	if maxOutputChars > 0 && len(cfg.Tools) > 0 {
		wrapped := make([]tool.Tool, len(cfg.Tools))
		for i, t := range cfg.Tools {
			wrapped[i] = NewOutputLimitTool(t, maxOutputChars)
		}
		cfg.Tools = wrapped
	}

	// 5. Create SessionService
	sessionSvc := sessioninmemory.NewSessionService(
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

	// 6. Create outputCh
	outputCh := make(chan *event.Event, 100)

	// 7. Create EventBus + AgentLoop
	bus := NewEventBus()
	agentLoop := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preprocessor,
		Model:        cfg.Model,
		Tools:        cfg.Tools,
		OutputCh:     outputCh,
		Name:         name,
		MaxToolIters: cfg.MaxToolIterations,
		SystemPrompt: cfg.SystemPrompt,
		Temperature:  cfg.Temperature,
	})

	// 8. Create Runner (retained as shell for session/plugin management).
	// We pass a lightweight "identity agent" to the runner — it only uses
	// it for Info() (name). Actual execution goes through AgentLoop.
	identityAgent := &identityOnlyAgent{
		info: agent.Info{Name: name, Description: description},
	}
	r := runner.NewRunner(name, identityAgent, runner.WithPlugins(
		plugin.NewSummaryPlugin(),
		memPlugin,
	), runner.WithSessionService(sessionSvc))

	ta := &TagentAgent{
		bus:          bus,
		agentLoop:    agentLoop,
		preprocessor: preprocessor,
		memStore:     memStore,
		memPlugin:    memPlugin,
		config:       cfg,
		runner:       r,
		sessionSvc:   sessionSvc,
		name:         name,
		description:  description,
		outputCh:     outputCh,
		closers:      []Closer{sessionSvc},
	}

	// Wire onEvent callback after ta is created.
	// This connects AgentLoop events to MemoryPlugin (persistence + causal chain)
	// and SessionService (session.Events append).
	agentLoop.SetOnEvent(ta.makeOnEventCallback())

	// Initialize meditation manager if enabled.
	if cfg.Meditation.Enabled {
		ta.meditationMgr = NewMeditationManager(cfg.Meditation, ta)
	}

	return ta, nil
}

// identityOnlyAgent is a minimal agent.Agent implementation used only to
// satisfy runner.NewRunner's agent parameter. The runner uses it for
// Info() (name). Actual execution goes through TagentAgent.AgentLoop.
type identityOnlyAgent struct {
	info agent.Info
}

func (a *identityOnlyAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	return nil, fmt.Errorf("identityOnlyAgent: Run should not be called directly")
}
func (a *identityOnlyAgent) Tools() []tool.Tool              { return nil }
func (a *identityOnlyAgent) Info() agent.Info                { return a.info }
func (a *identityOnlyAgent) SubAgents() []agent.Agent        { return nil }
func (a *identityOnlyAgent) FindSubAgent(string) agent.Agent { return nil }

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
	invOutputCh := make(chan *event.Event, 100)
	invPreprocessor := newPreprocessorFromConfig(ta.config)
	invLoop := NewAgentLoop(AgentLoopConfig{
		Bus:          invBus,
		Preprocessor: invPreprocessor,
		Model:        ta.config.Model,
		Tools:        ta.config.Tools,
		OutputCh:     invOutputCh,
		Name:         ta.name,
		MaxToolIters: ta.config.MaxToolIterations,
		SystemPrompt: ta.config.SystemPrompt,
		Temperature:  ta.config.Temperature,
	})
	// Attach session so Preprocessor can build messages from session.Events.
	if sess := ta.getOrCreateSession(); sess != nil {
		invLoop.SetSession(sess)
	}
	invLoop.SetOnEvent(ta.makeSubAgentOnEventCallback(sessionID))

	// Publish the initial message as external_input.
	invBus.Publish(NewExternalInputEvent("user", message))

	// Run the AgentLoop in a goroutine. It will exit when ctx is cancelled.
	runCtx, runCancel := context.WithCancel(ctx)
	go func() {
		defer close(invOutputCh)
		invLoop.Run(runCtx)
	}()

	// Wrap the outputCh: forward events and cancel the loop when the
	// caller stops reading OR when the first agent_output is emitted
	// (sub-agent single-turn semantics).
	wrappedCh := make(chan *event.Event, cap(invOutputCh))
	go func() {
		defer close(wrappedCh)
		defer runCancel()
		for evt := range invOutputCh {
			wrappedCh <- evt
			// Sub-agent semantics: stop after the first agent_output
			// (final response without tool_calls).
			if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				if len(choice.Message.ToolCalls) == 0 && choice.Message.Content != "" {
					return
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

	// Close runner
	if err := ta.runner.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close runner: %w", err))
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

// Runner returns the underlying Runner (for TmuxMonitor event injection).
func (ta *TagentAgent) Runner() runner.Runner {
	return ta.runner
}

// InjectMessage injects a message into the agent's EventBus as an
// external_input event. This is used by tools (e.g., TmuxMonitor) and
// external callers (HTTPAPI, A2A) to inject asynchronous messages.
//
// Requires the agent to be running (loopActive=true). If not running,
// the message is dropped with a warning.
func (ta *TagentAgent) InjectMessage(msg model.Message) {
	if !ta.loopActive.Load() {
		log.Warnf("[InjectMessage] agent loop not started, message dropped")
		return
	}
	if ta.meditationMgr != nil {
		ta.meditationMgr.UpdateLastEventTime(time.Now())
	}
	ta.bus.Publish(NewExternalInputEvent("inject", msg))
}

// setSessionContext sets the userID and sessionID (thread-safe).
func (ta *TagentAgent) setSessionContext(userID, sessionID string) {
	ta.sessionMu.Lock()
	defer ta.sessionMu.Unlock()
	ta.lastUserID = userID
	ta.lastSessionID = sessionID
}

// getOrCreateSession returns the session for the current lastUserID/lastSessionID.
// Creates the session if it does not exist.
func (ta *TagentAgent) getOrCreateSession() *session.Session {
	if ta.sessionSvc == nil {
		return nil
	}
	key := session.Key{
		AppName:   ta.name,
		UserID:    ta.lastUserID,
		SessionID: ta.lastSessionID,
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

// makeOnEventCallback creates the callback that wires AgentLoop events to
// MemoryPlugin (event persistence + causal chain) and SessionService
// (session.Events append).
func (ta *TagentAgent) makeOnEventCallback() func(evt *event.Event) {
	return func(evt *event.Event) {
		if evt == nil {
			return
		}
		ctx := context.Background()

		// Build a lightweight invocation for the plugin. MemoryPlugin only
		// needs AgentName and Session for partition/causal chain.
		inv := &agent.Invocation{
			AgentName: ta.name,
		}
		if ta.sessionSvc != nil && ta.lastSessionID != "" {
			sess := ta.getOrCreateSession()
			if sess != nil {
				inv.Session = sess
				// 1. MemoryPlugin: persist + causal chain + StateDelta.
				if ta.memPlugin != nil {
					if _, err := ta.memPlugin.OnEvent(ctx, inv, evt); err != nil {
						log.Errorf("[onEvent] MemoryPlugin.OnEvent failed: %v", err)
					}
				}
				// 2. SessionService: append to session.Events.
				if err := ta.sessionSvc.AppendEvent(ctx, sess, evt); err != nil {
					log.Errorf("[onEvent] AppendEvent failed: %v", err)
				}
			}
		}
	}
}

// makeSubAgentOnEventCallback creates the onEvent callback for sub-agent
// invocations. Sub-agents have their own isolated session.
func (ta *TagentAgent) makeSubAgentOnEventCallback(sessionID string) func(evt *event.Event) {
	return func(evt *event.Event) {
		if evt == nil {
			return
		}
		if ta.sessionSvc == nil {
			return
		}
		ctx := context.Background()

		key := session.Key{
			AppName:   ta.name,
			UserID:    ta.lastUserID,
			SessionID: sessionID,
		}
		sess, err := ta.sessionSvc.GetSession(ctx, key)
		if err != nil || sess == nil {
			sess, err = ta.sessionSvc.CreateSession(ctx, key, nil)
			if err != nil {
				log.Errorf("[subAgentOnEvent] CreateSession failed: %v", err)
				return
			}
		}

		inv := &agent.Invocation{
			AgentName: ta.name,
			Session:   sess,
		}
		if ta.memPlugin != nil {
			if _, err := ta.memPlugin.OnEvent(ctx, inv, evt); err != nil {
				log.Errorf("[subAgentOnEvent] MemoryPlugin.OnEvent failed: %v", err)
			}
		}
		if err := ta.sessionSvc.AppendEvent(ctx, sess, evt); err != nil {
			log.Errorf("[subAgentOnEvent] AppendEvent failed: %v", err)
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
// In the event-driven architecture, StartLoop launches an AgentLoop goroutine
// that pulls events from the EventBus and drives the model via Preprocessor.
// External inputs (user, tmux, meditation) are published to the bus via
// InjectMessage. Tool results are also published to the bus by the
// ToolDispatch layer.
//
// The AgentLoop is the sole consumer of the bus; outputCh receives final
// agent_output events for callers.
// ---------------------------------------------------------------------------

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
	sess := ta.getOrCreateSession()
	ta.agentLoop.SetSession(sess)

	// Set TrajectoryRecorder session info (if enabled).
	if ta.trajectoryRecorder != nil {
		ta.trajectoryRecorder.SetSessionInfo(userID, sessionID)
	}

	// Launch AgentLoop in a dedicated goroutine.
	ta.loopWg.Add(1)
	go func() {
		defer ta.loopWg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("[StartLoop] AgentLoop panic recovered: %v", r)
			}
			close(ta.outputCh)
		}()
		ta.agentLoop.Run(ta.loopCtx)
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

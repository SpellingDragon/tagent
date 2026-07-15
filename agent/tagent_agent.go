package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	"trpc.group/trpc-go/trpc-agent-go/tool"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

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

// InjectMessage injects a user message into the agent's persistent EventBus.
// It is a convenience wrapper around InjectMessageWithSource with source="user".
func (ta *TagentAgent) InjectMessage(msg model.Message) {
	ta.InjectMessageWithSource("user", msg)
}

// InjectMessageWithSource injects a message with a source label that
// identifies the origin (e.g., "user", "meditation", "async_result").
// The source is propagated to outputCh events via StateDelta["trigger_source"]
// so consumers can deterministically dispatch responses without inferring.
//
// Messages ALWAYS go to persistentBus — never to invBus. This ensures that
// user messages sent during sub-agent execution are not lost when the
// sub-agent's invBus is discarded. The BeforeModel InjectBusInputs callback
// on the persistent ContextManager will TryPull these messages and inject them
// into the next ReAct iteration.
func (ta *TagentAgent) InjectMessageWithSource(source string, msg model.Message) {
	if ta.meditationMgr != nil {
		ta.meditationMgr.UpdateLastEventTime(time.Now())
	}
	// Always use persistentBus, not activeBus.
	// activeBus may be invBus during sub-agent execution, but user messages
	// should go to the persistent bus so the main runEventLoop's BeforeModel
	// callback can pick them up.
	if ta.persistentBus != nil {
		ta.persistentBus.Publish(NewExternalInputEvent(source, msg))
		return
	}
	// Fallback: if persistentBus is nil (shouldn't happen), use activeBus.
	ta.activeBusMu.Lock()
	bus := ta.activeBus
	ta.activeBusMu.Unlock()
	if bus != nil {
		bus.Publish(NewExternalInputEvent(source, msg))
		return
	}
	log.Warnf("[InjectMessageWithSource] agent %q has no bus, message dropped", ta.name)
}

// InjectMessageWithMetadata injects a message with a source label and
// arbitrary metadata. The metadata is propagated to all events derived
// from this message via event.StateDelta with "meta_" prefix.
//
// Common metadata keys:
//   - "chat_id": target user/session identifier for response routing
//   - "user_name": human-readable user identifier for logs
//   - "channel": communication channel (wechat, discord, etc.)
func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string) {
	if ta.meditationMgr != nil {
		ta.meditationMgr.UpdateLastEventTime(time.Now())
	}
	evt := NewExternalInputEvent(source, msg)
	// 将 metadata 复制到 AgentEvent.Metadata
	if evt.Metadata == nil {
		evt.Metadata = make(map[string]any)
	}
	for k, v := range metadata {
		if k == "" || v == "" {
			continue
		}
		evt.Metadata[k] = v
	}
	if ta.persistentBus != nil {
		ta.persistentBus.Publish(evt)
		return
	}
	ta.activeBusMu.Lock()
	bus := ta.activeBus
	ta.activeBusMu.Unlock()
	if bus != nil {
		bus.Publish(evt)
		return
	}
	log.Warnf("[InjectMessageWithMetadata] agent %q has no bus, message dropped", ta.name)
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
// It performs two tasks:
// 1. Append EventReference to the projection
// 2. Propagate currentMetadata from ContextManager to event.StateDelta with "meta_" prefix
func (ta *TagentAgent) makeOnEventCallback(sessionID string, projection *SessionProjection) func(evt *event.Event) {
	return func(evt *event.Event) {
		if evt == nil {
			return
		}
		// Propagate metadata from ContextManager to event.StateDelta
		if ta.contextManager != nil {
			md := ta.contextManager.GetInvocationMetadata()
			if len(md) > 0 {
				if evt.StateDelta == nil {
					evt.StateDelta = make(map[string][]byte)
				}
				for k, v := range md {
					key := k
					if !strings.HasPrefix(key, "meta_") {
						key = "meta_" + key
					}
					evt.StateDelta[key] = []byte(v)
				}
			}
		}
		// Append to projection
		if projection != nil {
			if ref, ok := BuildEventReference(evt); ok {
				projection.Append(ref)
			}
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

		// Determine trigger source from batch events for deterministic
		// consumer-side dispatch. The source is attached to outputCh
		// events via StateDelta["trigger_source"] in RunFlow.
		cm.SetTriggerSource(extractTriggerSource(events))

		// Extract and propagate metadata (chat_id, user_name, etc.) from
		// the source event to all derived events via StateDelta["meta_*"].
		cm.SetInvocationMetadata(extractRootMetadata(events))

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

// extractTriggerSource determines the trigger source from a batch of
// AgentEvents. Uses the first non-agent_output, non-error external_input
// event's Source field. This provides deterministic source identification
// for consumer dispatch (meditation vs async_result vs user) without
// content-based inference.
func extractTriggerSource(events []*AgentEvent) string {
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" {
			continue
		}
		if evt.Source != "" {
			return evt.Source
		}
	}
	return "user"
}

// extractRootMetadata extracts metadata from a batch of AgentEvents.
// Collects metadata from external_input events (non-agent_output, non-error)
// and merges them into a single map. Later events override earlier ones.
// Empty keys or values are ignored.
func extractRootMetadata(events []*AgentEvent) map[string]string {
	md := make(map[string]string)
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" {
			continue
		}
		for k, v := range evt.Metadata {
			if k == "" {
				continue
			}
			if s, ok := v.(string); ok && s != "" {
				md[k] = s
			}
		}
	}
	return md
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

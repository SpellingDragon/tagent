package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// AgentLoop is the core execution engine for a TagentAgent.
//
// It is a pure loop with NO business semantics — all domain decisions
// (event filtering, shouldCallModel, compression) live in the Preprocessor.
//
// Per iteration, the AgentLoop:
//  1. Pulls a batch of events from the EventBus (blocks until available).
//  2. Calls Preprocessor.Process to get messages + shouldCallModel.
//  3. If shouldCallModel is false → dispatches tool_use events asynchronously
//     and returns to step 1.
//  4. If shouldCallModel is true → calls model.GenerateContent.
//  5. Parses the response:
//     - tool_calls present → publishes tool_use events to bus + dispatches them.
//     - no tool_calls (final response) → emits agent_output to outputCh
//     (NOT to bus — avoids self-triggering).
//  6. Loops back to step 1.
//
// The AgentLoop is NOT safe to run concurrently from multiple goroutines —
// it is expected to run in a single dedicated goroutine (started by StartLoop).
type AgentLoop struct {
	bus          *EventBus
	preprocessor *Preprocessor
	m            model.Model
	tools        []trpctool.Tool
	toolMap      map[string]trpctool.Tool
	outputCh     chan *event.Event
	session      *session.Session
	sessionSvc   session.Service
	name         string
	maxToolIters int
	systemPrompt string
	temperature  float64

	// toolIterations tracks the number of tool-call iterations in the current
	// "conversation turn". Reset when the model returns a final response
	// (no tool_calls). Used to enforce maxToolIters limit.
	toolIterations int

	// history accumulates the conversation context across rounds within
	// a single turn (user → tool_calls → tool_results → ...). Reset when
	// the model produces a final response (no tool_calls).
	// Protected by maxHistoryMessages to prevent unbounded growth.
	history []model.Message

	// onEvent is an optional callback invoked for every event emitted
	// to outputCh. Used for plugin integration (e.g., MemoryPlugin).
	onEvent func(evt *event.Event)
}

// AgentLoopConfig holds everything needed to create an AgentLoop.
// Populated by TagentAgent when the loop is started.
type AgentLoopConfig struct {
	Bus          *EventBus
	Preprocessor *Preprocessor
	Model        model.Model
	Tools        []trpctool.Tool
	OutputCh     chan *event.Event
	Session      *session.Session
	SessionSvc   session.Service
	Name         string
	MaxToolIters int
	SystemPrompt string
	Temperature  float64

	// OnEvent is an optional callback invoked for every event emitted
	// to outputCh. Used for plugin integration (e.g., MemoryPlugin.OnEvent
	// for persisting events to MemoryStore).
	OnEvent func(evt *event.Event)
}

// NewAgentLoop creates an AgentLoop from the given config.
func NewAgentLoop(cfg AgentLoopConfig) *AgentLoop {
	toolMap := make(map[string]trpctool.Tool, len(cfg.Tools))
	for _, t := range cfg.Tools {
		if decl := t.Declaration(); decl != nil {
			toolMap[decl.Name] = t
		}
	}
	maxIters := cfg.MaxToolIters
	if maxIters <= 0 {
		maxIters = DefaultMaxToolIterations
	}
	return &AgentLoop{
		bus:          cfg.Bus,
		preprocessor: cfg.Preprocessor,
		m:            cfg.Model,
		tools:        cfg.Tools,
		toolMap:      toolMap,
		outputCh:     cfg.OutputCh,
		session:      cfg.Session,
		sessionSvc:   cfg.SessionSvc,
		name:         cfg.Name,
		maxToolIters: maxIters,
		systemPrompt: cfg.SystemPrompt,
		temperature:  cfg.Temperature,
		onEvent:      cfg.OnEvent,
	}
}

// Run executes the agent loop until ctx is cancelled.
// It recovers from panics to avoid crashing the host process.
func (al *AgentLoop) Run(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[AgentLoop:%s] panic recovered: %v", al.name, r)
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			log.Infof("[AgentLoop:%s] ctx cancelled, exiting: %v", al.name, err)
			return
		}

		events, err := al.bus.Pull(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Infof("[AgentLoop:%s] Pull returned: %v, exiting", al.name, err)
				return
			}
			log.Errorf("[AgentLoop:%s] Pull error: %v", al.name, err)
			return
		}
		if len(events) == 0 {
			continue
		}

		result := al.preprocessor.Process(ctx, events)

		if !result.ShouldCallModel {
			continue
		}

		// Build full message list: history + new messages from this round.
		fullMessages := make([]model.Message, 0, len(al.history)+len(result.Messages))
		fullMessages = append(fullMessages, al.history...)
		fullMessages = append(fullMessages, result.Messages...)

		// Call the model with full context.
		resp, err := al.callModel(ctx, fullMessages)
		if err != nil {
			log.Errorf("[AgentLoop:%s] callModel failed: %v", al.name, err)
			errEvt := event.NewErrorEvent("", al.name, "agent_error", err.Error())
			al.emitEvent(errEvt)
			continue
		}
		if resp == nil {
			log.Warnf("[AgentLoop:%s] callModel returned nil response", al.name)
			continue
		}

		// Update history ONLY after successful model call.
		// This prevents duplicate messages when callModel fails and the
		// loop retries with the same or new events.
		al.history = append(al.history, result.Messages...)
		if len(resp.Choices) > 0 {
			al.history = append(al.history, resp.Choices[0].Message)
		}
		al.history = trimHistory(al.history)

		// Parse and act on the response.
		hasToolCalls := al.handleResponse(ctx, resp)

		// If no tool calls (final response), reset history for next turn.
		if !hasToolCalls {
			al.history = nil
			al.toolIterations = 0
		}
	}
}

// callModel builds a model.Request from messages + tool declarations and
// calls the configured model. Returns the final non-partial response.
func (al *AgentLoop) callModel(ctx context.Context, messages []model.Message) (*model.Response, error) {
	// Build the tools map for the request. The framework's model adapter
	// converts tool.Tool declarations into provider-specific formats internally.
	toolsForReq := make(map[string]trpctool.Tool, len(al.tools))
	for _, t := range al.tools {
		if decl := t.Declaration(); decl != nil {
			toolsForReq[decl.Name] = t
		}
	}

	req := &model.Request{
		Messages: messages,
		Tools:    toolsForReq,
	}

	// Prepend system prompt as the first message (if configured).
	if al.systemPrompt != "" {
		sysMsg := model.Message{
			Role:    model.RoleSystem,
			Content: al.systemPrompt,
		}
		req.Messages = append([]model.Message{sysMsg}, req.Messages...)
	}

	// Set generation config (temperature).
	if al.temperature > 0 {
		temp := al.temperature
		req.Temperature = &temp
	}

	ch, err := al.m.GenerateContent(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("model.GenerateContent: %w", err)
	}

	// Consume the response channel: collect the last non-partial response.
	// Streaming models emit partial responses followed by a final non-partial one.
	var finalResp *model.Response
	for resp := range ch {
		if resp == nil {
			continue
		}
		// Check for response errors.
		if resp.Error != nil {
			return nil, fmt.Errorf("model response error: %s: %s", resp.Error.Type, resp.Error.Message)
		}
		if !resp.IsPartial {
			finalResp = resp
		}
	}
	if finalResp == nil {
		return nil, fmt.Errorf("model returned no final response")
	}
	return finalResp, nil
}

// handleResponse inspects the model's response and takes action:
//   - tool_calls → publish tool_use events to bus + dispatch, returns true
//   - no tool_calls → emit agent_output to outputCh (NOT to bus), returns false
func (al *AgentLoop) handleResponse(ctx context.Context, resp *model.Response) bool {
	if resp == nil || len(resp.Choices) == 0 {
		log.Warnf("[AgentLoop:%s] empty response", al.name)
		return false
	}

	choice := resp.Choices[0]
	toolCalls := choice.Message.ToolCalls

	if len(toolCalls) > 0 {
		// Model wants to call tools.
		al.toolIterations++
		if al.toolIterations > al.maxToolIters {
			log.Warnf("[AgentLoop:%s] max tool iterations (%d) exceeded, forcing final response",
				al.name, al.maxToolIters)
			al.emitAgentOutput(resp)
			al.toolIterations = 0
			return false
		}

		for _, tc := range toolCalls {
			log.Infof("[AgentLoop:%s] tool_use: %s(%s)",
				al.name, tc.Function.Name, truncateString(string(tc.Function.Arguments), 200))
			evt := NewToolUseEvent(tc)
			al.bus.Publish(evt)
		}

		// Emit assistant response with tool_calls to outputCh so callers
		// can observe the tool calling process.
		toolCallEvt := event.NewResponseEvent("", al.name, resp)
		al.emitEvent(toolCallEvt)

		for _, tc := range toolCalls {
			al.dispatchToolUse(ctx, tc)
		}
		return true
	}

	// No tool_calls → final response.
	al.toolIterations = 0
	al.emitAgentOutput(resp)
	return false
}

// emitAgentOutput emits the final response to outputCh (NOT to bus).
// This is the mechanism for delivering agent_output to callers.
func (al *AgentLoop) emitAgentOutput(resp *model.Response) {
	if resp == nil {
		return
	}
	evt := event.NewResponseEvent("", al.name, resp)
	al.emitEvent(evt)
}

// emitEvent sends an event to outputCh. Drops silently if outputCh is full
// or nil. This is a best-effort delivery — callers should size outputCh
// appropriately.
func (al *AgentLoop) emitEvent(evt *event.Event) {
	if evt == nil {
		return
	}
	// Invoke onEvent callback for plugin integration (e.g., MemoryPlugin).
	if al.onEvent != nil {
		al.onEvent(evt)
	}
	if al.outputCh == nil {
		return
	}
	select {
	case al.outputCh <- evt:
	default:
		log.Warnf("[AgentLoop:%s] outputCh full, dropping event", al.name)
	}
}

// ---------------------------------------------------------------------------
// Tool Dispatch — asynchronous
// ---------------------------------------------------------------------------
//
// AgentLoop publishes tool_use events to the bus AND dispatches them
// asynchronously via goroutines. The AgentLoop does NOT block on tool
// execution.
//
// Two dispatch paths:
//
//   1. Shell / normal tool (*CallableTool but NOT *AgentToolWrapper):
//      goroutine calls tool.Call(). If the result is a tmux-async marker
//      (implements tmuxAsyncResult), do NOT publish — TmuxMonitor will
//      publish the real result later. Otherwise publish the result as
//      external_input immediately.
//
//   2. Sub-agent (*AgentToolWrapper):
//      goroutine calls wrapper.Call() (which runs the sub-agent to
//      completion and returns the final output). Publishes the output
//      as external_input.
//
// Both paths publish to the same EventBus, so the AgentLoop sees the
// result in its next Pull batch.

// toolResultMarker is used by dispatchToolUse to distinguish tmux-async
// results (which return TmuxExecResponse-like structs) from synchronous
// results (which should be published immediately).
//
// This is a temporary heuristic. Group 5 (full tmux) will remove the
// synchronous code path entirely, making this distinction unnecessary.
type tmuxAsyncResult interface {
	IsTmuxAsync() bool
}

// dispatchToolUse starts a goroutine that executes the tool and publishes
// the result back to the bus as an external_input event.
//
// This method returns immediately — the AgentLoop is not blocked.
func (al *AgentLoop) dispatchToolUse(ctx context.Context, toolCall model.ToolCall) {
	name := toolCall.Function.Name
	t, ok := al.toolMap[name]
	if !ok {
		log.Errorf("[AgentLoop:%s] tool %q not found", al.name, name)
		return
	}

	// Detect sub-agent vs shell tool.
	if wrapper, isSubAgent := t.(*AgentToolWrapper); isSubAgent {
		al.dispatchSubAgent(ctx, wrapper, toolCall)
		return
	}

	// Shell / normal tool path.
	callable, ok := t.(trpctool.CallableTool)
	if !ok {
		log.Errorf("[AgentLoop:%s] tool %q is not CallableTool", al.name, name)
		return
	}

	go al.dispatchCallable(ctx, callable, toolCall)
}

// dispatchCallable executes a CallableTool in a goroutine and publishes
// the result to the bus. Handles tmux-async detection.
func (al *AgentLoop) dispatchCallable(
	ctx context.Context,
	callable trpctool.CallableTool,
	toolCall model.ToolCall,
) {
	name := toolCall.Function.Name
	startTime := time.Now()

	result, err := callable.Call(ctx, toolCall.Function.Arguments)
	elapsed := time.Since(startTime)

	var content string
	if err != nil {
		content = fmt.Sprintf("Error: %v", err)
	} else if result == nil {
		content = "(no output)"
	} else {
		// Check if this is a tmux-async result.
		if asyncMarker, ok := result.(tmuxAsyncResult); ok && asyncMarker.IsTmuxAsync() {
			log.Infof("[AgentLoop:%s] tool %s returned async marker, waiting for TmuxMonitor callback",
				al.name, name)
			return
		}
		// JSON-serialize the result for LLM-friendly format.
		// Falls back to %v for types that don't marshal cleanly.
		if b, marshalErr := json.Marshal(result); marshalErr == nil {
			content = string(b)
		} else {
			content = fmt.Sprintf("%v", result)
		}
	}

	log.Infof("[AgentLoop:%s] tool %s completed in %v, content_len=%d",
		al.name, name, elapsed, len(content))

	al.bus.Publish(NewExternalInputEvent("tool_result", model.Message{
		Role:    model.RoleTool,
		Content: content,
		ToolID:  toolCall.ID,
	}))
}

// dispatchSubAgent runs an AgentToolWrapper in a goroutine and publishes
// the result to the bus.
func (al *AgentLoop) dispatchSubAgent(
	ctx context.Context,
	wrapper *AgentToolWrapper,
	toolCall model.ToolCall,
) {
	go func() {
		name := wrapper.agent.Info().Name
		startTime := time.Now()

		result, err := wrapper.Call(ctx, toolCall.Function.Arguments)
		elapsed := time.Since(startTime)

		var content string
		if err != nil {
			content = fmt.Sprintf("[agent error] %s: %v", name, err)
		} else if result == nil {
			content = fmt.Sprintf("[agent %s] completed (no output)", name)
		} else {
			content = fmt.Sprintf("[agent %s] %v", name, result)
		}

		log.Infof("[AgentLoop:%s] sub-agent %s completed in %v, content_len=%d",
			al.name, name, elapsed, len(content))

		al.bus.Publish(NewExternalInputEvent("subagent_result", model.Message{
			Role:    model.RoleTool,
			Content: content,
			ToolID:  toolCall.ID,
		}))
	}()
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// trimHistory trims the history to at most max messages, keeping the most recent.
// This prevents unbounded growth during long tool-call chains.
const maxHistoryMessages = 100

func trimHistory(h []model.Message) []model.Message {
	if len(h) <= maxHistoryMessages {
		return h
	}
	// Keep the most recent messages. The earliest messages in a long
	// tool-call chain are least likely to be relevant.
	return h[len(h)-maxHistoryMessages:]
}

// SetSession updates the session reference. Called when a session is
// attached to the agent (e.g., by TagentAgent.Run or StartLoop).
func (al *AgentLoop) SetSession(sess *session.Session) {
	al.session = sess
	if al.preprocessor != nil {
		al.preprocessor.SetSession(sess)
	}
}

// truncateString truncates s to at most n characters, appending "..." if truncated.
// Reuses the helper already defined in tagent_agent.go.
// (This comment serves as a marker for future deduplication.)

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
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
//  2. Dispatches any tool_use events in the batch asynchronously (goroutines).
//     Tool dispatch happens on consumption, not production — handleResponse
//     only publishes tool_use to bus, the main loop dispatches on next Pull.
//  3. Persists any external_input events via onEvent callback (session + MemoryStore).
//  4. Calls Preprocessor.Process to get messages + shouldCallModel.
//  5. If shouldCallModel is false → returns to step 1.
//  6. If shouldCallModel is true → calls model.GenerateContent.
//  7. Parses the response:
//     - tool_calls present → publishes tool_use events to bus + onEvent
//     (NOT dispatched here — dispatched on next Pull iteration).
//     - no tool_calls (final response) → emits agent_output to outputCh
//     (NOT to bus — avoids self-triggering).
//  8. Loops back to step 1.
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
	name         string
	maxToolIters int
	systemPrompt string
	temperature  float64

	// toolIterations tracks the number of tool-call iterations in the current
	// "conversation turn". Reset when the model returns a final response
	// (no tool_calls). Used to enforce maxToolIters limit.
	toolIterations int

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
		name:         cfg.Name,
		maxToolIters: maxIters,
		systemPrompt: cfg.SystemPrompt,
		temperature:  cfg.Temperature,
		onEvent:      cfg.OnEvent,
	}
}

// wrapAsFrameworkEvent converts an AgentEvent (bus type) to a framework
// event.Event suitable for MemoryPlugin.OnEvent and sessionSvc.AppendEvent.
func (al *AgentLoop) wrapAsFrameworkEvent(evt *AgentEvent) *event.Event {
	if evt == nil {
		return nil
	}
	frameworkEvt := event.New("", al.name)
	frameworkEvt.Timestamp = evt.Timestamp
	if evt.Message != nil {
		frameworkEvt.Response = &model.Response{
			Choices: []model.Choice{{
				Message: *evt.Message,
			}},
		}
	}
	return frameworkEvt
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
		log.Infof("[AgentLoop:%s] iteration start: pulled %d events (%s)",
			al.name, len(events), summarizeEvents(events))

		// Step 1: Dispatch tool_use events (consumed from bus, not produced in handleResponse).
		// Tool dispatch happens here rather than in handleResponse so that tool_use
		// events are dispatched when the AgentLoop pulls them from the bus, not when
		// they are produced. This decouples production from execution.
		for _, evt := range events {
			if evt != nil && evt.Type == TypeToolUse && evt.ToolCall != nil {
				al.dispatchToolUse(ctx, *evt.ToolCall)
			}
		}

		// Step 2: Persist external_input events to session + MemoryStore
		// BEFORE Preprocessor.Process reads session.Events.
		for _, evt := range events {
			if evt == nil || evt.Type != tagentevent.TypeExternalInput || evt.Message == nil {
				continue
			}
			frameworkEvt := al.wrapAsFrameworkEvent(evt)
			if frameworkEvt == nil {
				continue
			}
			if al.onEvent != nil {
				al.onEvent(frameworkEvt)
			}
			// Also append to the AgentLoop's own session copy so Preprocessor
			// can read the conversation history. See emitEvent for details.
			if al.session != nil && frameworkEvt.Response != nil && !frameworkEvt.IsPartial && frameworkEvt.IsValidContent() {
				al.session.EventMu.Lock()
				al.session.Events = append(al.session.Events, *frameworkEvt)
				al.session.EventMu.Unlock()
			}
		}

		// Step 3: Build messages and decide whether to call model.
		result := al.preprocessor.Process(ctx, events, al.session)

		if !result.ShouldCallModel {
			log.Infof("[AgentLoop:%s] shouldCallModel=false, skip model call", al.name)
			continue
		}

		log.Infof("[AgentLoop:%s] shouldCallModel=true, calling model with %d messages", al.name, len(result.Messages))
		log.Debugf("[AgentLoop:%s] model request messages:\n%s", al.name, formatMessages(result.Messages))

		// Call the model with the complete messages from Preprocessor.
		// Preprocessor builds messages from session.Events (the single source
		// of conversation history), so AgentLoop does not maintain its own.
		resp, err := al.callModel(ctx, result.Messages)
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

		log.Debugf("[AgentLoop:%s] model response: %s", al.name, formatResponse(resp))

		// Parse and act on the response.
		hasToolCalls := al.handleResponse(ctx, resp)

		// If no tool calls (final response), reset tool iteration counter.
		if !hasToolCalls {
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

	log.Infof("[AgentLoop:%s] model.GenerateContent: msgs=%d tools=%d system_prompt=%v temp=%.2f",
		al.name, len(req.Messages), len(toolsForReq), al.systemPrompt != "", al.temperature)

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
//   - tool_calls → publish tool_use events to bus + onEvent, returns true
//   - no tool_calls → emit agent_output to outputCh (NOT to bus), returns false
//
// Tool dispatch is NOT done here; it happens in the main Run() loop when
// tool_use events are consumed from the bus on the next Pull iteration.
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

		return true
	}

	// No tool_calls → final response.
	al.toolIterations = 0
	content := ""
	finishReason := ""
	reasoningContent := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		if resp.Choices[0].FinishReason != nil {
			finishReason = *resp.Choices[0].FinishReason
		}
		reasoningContent = resp.Choices[0].Message.ReasoningContent
	}

	// Build a detailed log line for the final response.
	var usageStr string
	if resp.Usage != nil {
		usageStr = fmt.Sprintf(" prompt_tokens=%d completion_tokens=%d total=%d",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
	log.Infof("[AgentLoop:%s] final response: content_len=%d finish_reason=%q%s",
		al.name, len(content), finishReason, usageStr)
	if reasoningContent != "" {
		log.Debugf("[AgentLoop:%s] reasoning_content (len=%d): %s",
			al.name, len(reasoningContent), truncateString(reasoningContent, 500))
	}
	if len(content) == 0 {
		log.Warnf("[AgentLoop:%s] empty final response! finish_reason=%q, check model behavior or content filtering", al.name, finishReason)
	}
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
	log.Debugf("[AgentLoop:%s] emitEvent: tag=%s partial=%v valid=%v",
		al.name, evt.Tag, evt.IsPartial, evt.IsValidContent())
	// Invoke onEvent callback for plugin integration (e.g., MemoryPlugin).
	if al.onEvent != nil {
		al.onEvent(evt)
	}
	// Append to the AgentLoop's own session copy so Preprocessor can read
	// the full conversation history on the next iteration. The session
	// service returns clones, so the session held by AgentLoop would
	// otherwise diverge from the persisted one.
	if al.session != nil && evt.Response != nil && !evt.IsPartial && evt.IsValidContent() {
		al.session.EventMu.Lock()
		al.session.Events = append(al.session.Events, *evt)
		al.session.EventMu.Unlock()
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
// the result to the bus. A 10-minute timeout protects against sub-agent
// goroutine leaks if the sub-agent never terminates.
func (al *AgentLoop) dispatchSubAgent(
	ctx context.Context,
	wrapper *AgentToolWrapper,
	toolCall model.ToolCall,
) {
	go func() {
		name := wrapper.agent.Info().Name
		startTime := time.Now()

		subCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		result, err := wrapper.Call(subCtx, toolCall.Function.Arguments)
		elapsed := time.Since(startTime)

		var content string
		if err != nil {
			content = fmt.Sprintf("[agent error] %s: %v", name, err)
		} else if result == nil {
			content = fmt.Sprintf("[agent %s] completed (no output)", name)
		} else if b, marshalErr := json.Marshal(result); marshalErr == nil {
			content = string(b)
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

// SetSession updates the session reference. Called when a session is
// attached to the agent (e.g., by TagentAgent.Run or StartLoop).
func (al *AgentLoop) SetSession(sess *session.Session) {
	al.session = sess
}

// SetOnEvent sets the onEvent callback. Called after TagentAgent is created
// so the callback can close over the agent instance.
func (al *AgentLoop) SetOnEvent(cb func(evt *event.Event)) {
	al.onEvent = cb
}

// formatResponse returns a compact, debug-friendly summary of a model response.
func formatResponse(resp *model.Response) string {
	if resp == nil || len(resp.Choices) == 0 {
		return "(empty response)"
	}
	choice := resp.Choices[0]
	msg := choice.Message
	finishReason := ""
	if choice.FinishReason != nil {
		finishReason = *choice.FinishReason
	}
	content := msg.Content
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	reasoning := ""
	if msg.ReasoningContent != "" {
		r := msg.ReasoningContent
		if len(r) > 100 {
			r = r[:100] + "..."
		}
		reasoning = fmt.Sprintf(" reasoning=%q", r)
	}
	if len(msg.ToolCalls) > 0 {
		names := make([]string, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			names = append(names, tc.Function.Name)
		}
		return fmt.Sprintf("role=%s finish=%q tool_calls=%v%s", msg.Role, finishReason, names, reasoning)
	}
	return fmt.Sprintf("role=%s finish=%q content=%q%s", msg.Role, finishReason, content, reasoning)
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

// truncateString truncates s to at most n characters, appending "..." if truncated.
// Reuses the helper already defined in tagent_agent.go.
// (This comment serves as a marker for future deduplication.)

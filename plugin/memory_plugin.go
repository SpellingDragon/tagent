// Package plugin provides tagent-specific plugins for trpc-agent-go's Runner.
package plugin

import (
	"context"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/plugin"

	"github.com/SpellingDragon/tagent/memory"
)

// MemoryPlugin syncs events to MemoryStore via OnEvent hook.
// It implements plugin.Plugin and is registered on the Runner.
//
// Core responsibilities:
//  1. Infer event type from Event role
//  2. Generate EventKey
//  3. Build FullEvent with ParentKey causal chain
//  4. Persist to MemoryStore
//  5. Write back EventKey/EventType to Event.StateDelta
type MemoryPlugin struct {
	memStore     memory.MemoryStore
	mu           sync.Mutex // Protects lastEventKey for concurrent safety
	lastEventKey string     // Causal chain: key of the preceding event
}

// NewMemoryPlugin creates a new MemoryPlugin.
func NewMemoryPlugin(store memory.MemoryStore) *MemoryPlugin {
	return &MemoryPlugin{
		memStore: store,
	}
}

// Name implements plugin.Plugin.
func (p *MemoryPlugin) Name() string {
	return "memory"
}

// Register implements plugin.Plugin.
func (p *MemoryPlugin) Register(r *plugin.Registry) {
	r.OnEvent(p.onEvent)
}

// onEvent is the EventHook that syncs events to MemoryStore.
func (p *MemoryPlugin) onEvent(
	ctx context.Context,
	inv *agent.Invocation,
	evt *event.Event,
) (*event.Event, error) {
	if evt == nil {
		return nil, nil
	}

	// 1. Infer event type from Event role
	eventType := inferEventType(evt)

	// 2. Generate EventKey
	timestamp := time.Now().UnixMilli()
	eventKey := memory.NewEventKey(timestamp, 0)

	// 3. Extract summary from event content
	eventSummary := extractSummary(evt)

	// 4. Build FullEvent with ParentKey causal chain
	p.mu.Lock()
	parentKey := p.lastEventKey
	p.mu.Unlock()

	fullEvent := memory.FullEvent{
		EventKey:     eventKey,
		ParentKey:    parentKey, // causal chain
		EventType:    eventType,
		EventSummary: eventSummary,
		Timestamp:    timestamp,
	}

	if evt.Response != nil && len(evt.Response.Choices) > 0 {
		msg := evt.Response.Choices[0].Message
		fullEvent.Content = msg.Content
		fullEvent.ToolCalls = msg.ToolCalls
		fullEvent.Response = evt.Response
	}

	// 5. Persist to MemoryStore
	if p.memStore != nil {
		if err := p.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			log.Errorf("MemoryPlugin: failed to store event %s: %v", eventKey, err)
		} else {
			log.Debugf("MemoryPlugin: stored event %s (type=%s, parent=%s)",
				eventKey, eventType, p.lastEventKey)
		}
	}

	// 6. Write back EventKey and EventType to StateDelta
	if evt.StateDelta == nil {
		evt.StateDelta = make(map[string][]byte)
	}
	evt.StateDelta["event_key"] = []byte(eventKey)
	evt.StateDelta["event_type"] = []byte(eventType)

	// 7. Update causal chain (thread-safe)
	p.mu.Lock()
	p.lastEventKey = eventKey
	p.mu.Unlock()

	return evt, nil
}

// inferEventType infers the event type from an Event's role.
func inferEventType(evt *event.Event) string {
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return memory.EventTypeExternalInput
	}
	msg := evt.Response.Choices[0].Message
	return inferEventTypeFromMessage(msg)
}

// inferEventTypeFromMessage infers event type from a model.Message's role.
// Note: System prompt is NOT part of the event stream — it is injected by
// InstructionProcessor at initialization and preserved through compression.
// RoleSystem may appear in the event stream (e.g., TmuxMonitor state notifications)
// and is classified as external_input.
func inferEventTypeFromMessage(msg model.Message) string {
	switch msg.Role {
	case model.RoleUser:
		return memory.EventTypeExternalInput
	case model.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			return memory.EventTypeThinkingPlan
		}
		return memory.EventTypeAgentOutput
	case model.RoleTool:
		return memory.EventTypeActionCommand
	default:
		// Fallback: includes RoleSystem from TmuxMonitor injections (classified as external_input).
		return memory.EventTypeExternalInput
	}
}

// extractSummary extracts a summary from the event content.
// Special events (external_input, agent_output) use full original content.
// Other events (action_command, thinking_plan) use a brief summary.
// Core principle: no information loss outside of design intent.
func extractSummary(evt *event.Event) string {
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return ""
	}
	msg := evt.Response.Choices[0].Message

	// Determine event type to decide summary strategy
	eventType := inferEventTypeFromMessage(msg)

	// Special events: use full original content (no truncation)
	switch eventType {
	case memory.EventTypeExternalInput, memory.EventTypeAgentOutput:
		return msg.Content // Full content, no information loss
	}

	// Normal events: generate a descriptive summary
	// action_command: "调用工具: toolName(args)"
	// thinking_plan: "思考: content..." (brief)
	switch msg.Role {
	case model.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			return formatToolCallSummary(msg.ToolCalls)
		}
		return msg.Content
	case model.RoleTool:
		// Tool results can be long; use full content per design principle
		return msg.Content
	default:
		return msg.Content
	}
}

// formatToolCallSummary generates a summary for tool call events.
func formatToolCallSummary(toolCalls []model.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	var parts []string
	for _, tc := range toolCalls {
		toolName := tc.Function.Name
		args := string(tc.Function.Arguments)
		parts = append(parts, toolName+"("+args+")")
	}
	return "调用工具: " + joinStrings(parts, ", ")
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for i := 1; i < len(ss); i++ {
		result += sep + ss[i]
	}
	return result
}

package plugin

import (
	"context"
	"sync"

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
//  1. Derive PartitionID from Invocation.AgentName (using FNV-1a hash)
//  2. Generate Snowflake EventKey (int64, encoding PartitionID)
//  3. Build FullEvent with ParentKey (per-partition independent causal chain)
//  4. Persist to MemoryStore
//  5. Write back EventKey/PartitionID to Event.StateDelta
//
// Storage isolation: MemoryPlugin maps AgentName → PartitionID (storage concept).
// Memory itself only sees PartitionID as an integer — it has no agent awareness.
// The mapping is:
//
//	AgentName (framework) → PartitionIDFromName() → PartitionID (storage)
//	"tagent"       → hash → 42
//	"knowledge"    → hash → 85
//	"recall"       → hash → 123
//
// Causal chain isolation: each PartitionID maintains an independent causal chain
// (lastEventKeys map). This prevents sub-agent events from breaking the
// parent agent's causal chain.
type MemoryPlugin struct {
	memStore      memory.MemoryStore
	mu            sync.Mutex
	lastEventKeys map[int]int64 // PartitionID → lastEventKey (independent causal chain)
}

// NewMemoryPlugin creates a new MemoryPlugin.
func NewMemoryPlugin(store memory.MemoryStore) *MemoryPlugin {
	return &MemoryPlugin{
		memStore:      store,
		lastEventKeys: make(map[int]int64),
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

	// 1. Derive PartitionID from AgentName (framework concept → storage concept)
	agentName := p.extractAgentName(inv)
	partitionID := memory.PartitionIDFromName(agentName)

	// 2. Generate Snowflake EventKey
	eventKey := memory.NewSnowflakeEventKey(partitionID, 0)

	// 3. Infer event type from Event role
	eventType := inferEventType(evt)

	// 4. Extract summary from event content
	eventSummary := extractSummary(evt)

	// 5. Get parent key from independent causal chain
	p.mu.Lock()
	parentKey := p.lastEventKeys[partitionID]
	p.mu.Unlock()

	// 6. Extract timestamp
	timestamp := extractTimestamp(evt)

	// 7. Build FullEvent
	fullEvent := memory.FullEvent{
		EventKey:     eventKey,
		PartitionID:  partitionID,
		ParentKey:    parentKey,
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

	// 8. Persist to MemoryStore
	if p.memStore != nil {
		if err := p.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			log.Errorf("MemoryPlugin: failed to store event %d: %v", eventKey, err)
		} else {
			log.Debugf("MemoryPlugin: stored event %d (partition=%d, type=%s, parent=%d)",
				eventKey, partitionID, eventType, parentKey)
		}
	}

	// 9. Write back EventKey and PartitionID to StateDelta
	if evt.StateDelta == nil {
		evt.StateDelta = make(map[string][]byte)
	}
	evt.StateDelta["event_key"] = []byte(int64ToString(eventKey))
	evt.StateDelta["partition_id"] = []byte(intToString(partitionID))
	evt.StateDelta["event_type"] = []byte(eventType)

	// 10. Update independent causal chain (thread-safe)
	p.mu.Lock()
	p.lastEventKeys[partitionID] = eventKey
	p.mu.Unlock()

	return evt, nil
}

// extractAgentName extracts the agent name from Invocation.
// Falls back to "unknown" if not available, which maps to a default PartitionID.
func (p *MemoryPlugin) extractAgentName(inv *agent.Invocation) string {
	if inv == nil {
		return "unknown"
	}
	if inv.AgentName != "" {
		return inv.AgentName
	}
	return "unknown"
}

// extractTimestamp extracts the timestamp from an Event.
func extractTimestamp(evt *event.Event) int64 {
	if evt == nil {
		return 0
	}
	return evt.Timestamp.UnixMilli()
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
		return memory.EventTypeExternalInput
	}
}

// extractSummary extracts a summary from the event content.
func extractSummary(evt *event.Event) string {
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return ""
	}
	msg := evt.Response.Choices[0].Message

	eventType := inferEventTypeFromMessage(msg)

	// Special events: use full original content (no truncation)
	switch eventType {
	case memory.EventTypeExternalInput, memory.EventTypeAgentOutput:
		return msg.Content
	}

	// Normal events: generate a descriptive summary
	switch msg.Role {
	case model.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			return formatToolCallSummary(msg.ToolCalls)
		}
		return msg.Content
	case model.RoleTool:
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
	result := "调用工具: " + joinStrings(parts, ", ")
	return result
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

// int64ToString converts int64 to string using fmt.Sprintf.
func int64ToString(n int64) string {
	return formatInt64(n)
}

// intToString converts int to string.
func intToString(n int) string {
	return formatInt(n)
}

// formatInt64 and formatInt are overridable for testing.
var (
	formatInt64 = func(n int64) string { return defaultFormatInt64(n) }
	formatInt   = func(n int) string { return defaultFormatInt(n) }
)

func defaultFormatInt64(n int64) string {
	return simpleItoa64(n)
}

func defaultFormatInt(n int) string {
	return simpleItoa(int64(n))
}

// simpleItoa64 converts int64 to string without importing strconv.
func simpleItoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// simpleItoa converts int to string.
func simpleItoa(n int64) string {
	return simpleItoa64(n)
}

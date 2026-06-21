package plugin

import (
	"context"
	"fmt"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/plugin"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// MemoryPlugin syncs events to MemoryStore via OnEvent hook.
// It implements plugin.Plugin and is registered on the Runner.
//
// Core responsibilities:
//  1. Derive PartitionID from Invocation.AgentName (using FNV-1a hash)
//  2. Generate Snowflake EventKey (int64, encoding PartitionID)
//  3. Build FullEvent (parent relationship set via RelationStore.SetParent)
//  4. Persist to MemoryStore
//  5. Write back EventKey/PartitionID to Event.StateDelta
//
// Causal chain isolation: each (PartitionID, SessionID) pair maintains an independent
// causal chain (lastEventKeys map). This prevents sub-agent events and cross-session
// events from breaking each other's causal chains.
type MemoryPlugin struct {
	memStore      memory.MemoryStore
	mu            sync.Mutex
	lastEventKeys map[string]int64 // "partitionID:sessionID" → lastEventKey
}

// NewMemoryPlugin creates a new MemoryPlugin.
func NewMemoryPlugin(store memory.MemoryStore) *MemoryPlugin {
	return &MemoryPlugin{
		memStore:      store,
		lastEventKeys: make(map[string]int64),
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

	// 1. Derive PartitionID from AgentName
	agentName := p.extractAgentName(inv)
	partitionID := memory.PartitionIDFromName(agentName)

	// 2. Generate Snowflake EventKey
	eventKey := memory.NewSnowflakeEventKey(partitionID, 0)

	// 3. Infer event type and generate summary
	eventType, eventSummary := p.inferEventInfo(evt)

	// 4. Get parent key from independent causal chain (partitionID:sessionID)
	sessionID := ""
	if inv != nil && inv.Session != nil {
		sessionID = inv.Session.ID
	}
	causalKey := fmt.Sprintf("%d:%s", partitionID, sessionID)

	p.mu.Lock()
	parentKey := p.lastEventKeys[causalKey]
	p.mu.Unlock()

	// 5. Extract timestamp
	timestamp := extractTimestamp(evt)

	// 6. Build FullEvent (no ParentKey field — relationships via RelationStore)
	fullEvent := memory.FullEvent{
		EventKey:     eventKey,
		PartitionID:  partitionID,
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

	// 7. Persist to MemoryStore
	if p.memStore != nil {
		if err := p.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			log.Errorf("[Memory] store failed key=%d partition=%d: %v", eventKey, partitionID, err)
		} else {
			// Set parent relationship via RelationStoreProvider (content-relation separation)
			if parentKey != 0 {
				if rsp, ok := p.memStore.(memory.RelationStoreProvider); ok {
					if err := rsp.RelationStore().SetParent(eventKey, parentKey); err != nil {
						log.Errorf("[Memory] set parent failed key=%d parent=%d: %v", eventKey, parentKey, err)
					}
				}
			}
			log.Debugf("[Memory] stored key=%d partition=%d type=%s summary_len=%d",
				eventKey, partitionID, eventType, len(eventSummary))
		}
	}

	// 8. Write back EventKey and PartitionID to StateDelta
	if evt.StateDelta == nil {
		evt.StateDelta = make(map[string][]byte)
	}
	evt.StateDelta["event_key"] = []byte(int64ToString(eventKey))
	evt.StateDelta["partition_id"] = []byte(intToString(partitionID))
	evt.StateDelta["event_type"] = []byte(eventType)

	// 9. Update independent causal chain (thread-safe)
	p.mu.Lock()
	p.lastEventKeys[causalKey] = eventKey
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

// inferEventInfo extracts event type and summary from an event using tagent/event package.
// This uses the same unified classification as SummaryPlugin, ensuring consistency.
func (p *MemoryPlugin) inferEventInfo(evt *event.Event) (string, string) {
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return tagentevent.TypeExternalInput, ""
	}
	msg := evt.Response.Choices[0].Message
	eventType := tagentevent.ExtractEventType(msg)
	opts := tagentevent.DefaultOptionsForLLMContext()
	summary := tagentevent.GenerateEventSummary(msg, eventType, opts)
	return eventType, summary
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

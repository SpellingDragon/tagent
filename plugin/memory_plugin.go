package plugin

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
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

// OnEvent is the exported wrapper around the internal onEvent hook. Production
// invocation happens through the framework's plugin pipeline (Register →
// r.OnEvent); the exported form exists for direct use in tests and tools.
func (p *MemoryPlugin) OnEvent(
	ctx context.Context,
	inv *agent.Invocation,
	evt *event.Event,
) (*event.Event, error) {
	return p.onEvent(ctx, inv, evt)
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

	// Skip events that carry no actual message payload. The runner/flow may
	// emit synchronization events (start/wait/barrier) with a nil Response or
	// no Choices. Without this guard, MemoryPlugin infers them as
	// "external_input" with empty content, and they end up in the projection
	// as misleading user-role placeholders that crowd out real context.
	if evt.Response == nil || len(evt.Response.Choices) == 0 {
		return evt, nil
	}

	// Skip streaming PARTIAL (delta) events: only aggregated events are stored
	// and projected (unified-event-projection D8 invariant). A no-op for
	// non-streaming deployments; for streaming, this keeps intermediate deltas
	// (empty content, unaggregated tool_calls) out of the store and projection.
	if evt.Response.IsPartial {
		return evt, nil
	}

	// Skip degenerate empty final responses: an agent_output (assistant, no
	// tool_calls) with empty content carries no information. Persisting it would
	// pollute the projection/history with an empty assistant message and store a
	// summary_len=0 record. This mirrors RunFlow's echo suppression and the
	// consumer's drop, making "empty final = non-event" consistent at the storage
	// layer too (async-result-delivery H1). Tool-call turns (empty content WITH
	// tool_calls → thinking_plan) and non-empty finals are unaffected.
	if m := evt.Response.Choices[0].Message; m.Content == "" && tagentevent.ExtractEventType(m) == tagentevent.TypeAgentOutput {
		log.Debugf("[Memory] skip degenerate empty final (agent_output, no content/tool_calls)")
		return evt, nil
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
	// 归因盖章（TC0）：填充 FullEvent.Metadata——修复「Metadata 从未被填充」缺口
	// （报告 §4.3 F1）。基线盖 agent_name（provenance，立即可用）；ctx 归因载体
	// （WithAttribution）叠加 bundle_id/rollout_id（RunFlow 注入，T-EVO 版本归因）。
	fullEvent.Metadata = make(map[string]string, 2)
	if agentName != "" {
		fullEvent.Metadata[tagentevent.MetaKeyAgentName] = agentName
	}
	if attr, ok := AttributionFrom(ctx); ok {
		for k, v := range attr {
			fullEvent.Metadata[k] = v
		}
	}

	if evt.Response != nil && len(evt.Response.Choices) > 0 {
		msg := evt.Response.Choices[0].Message
		fullEvent.Content = sanitizeAssistantContent(msg)
		fullEvent.ToolCalls = msg.ToolCalls
		fullEvent.ToolID = msg.ToolID
		fullEvent.Response = evt.Response
	}

	// 7. Persist to MemoryStore, then project at the same synchronous point
	// (write unification, unified-event-projection D1): the pipeline is the
	// single place where a stored event also enters the invocation's
	// projection. The projection's own EventKey idempotency (L1) makes
	// re-delivery harmless.
	stored := false
	if p.memStore != nil {
		if err := p.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			log.Errorf("[Memory] store failed key=%d partition=%d: %v", eventKey, partitionID, err)
		} else {
			stored = true
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
	if stored {
		if sink, ok := ProjectionSinkFrom(ctx); ok {
			sink.Append(memory.EventReference{
				EventKey:     eventKey,
				PartitionID:  partitionID,
				EventType:    eventType,
				EventSummary: eventSummary,
				Timestamp:    timestamp,
				Role:         string(evt.Response.Choices[0].Message.Role),
			})
		}
	}

	// 8. Write back the storage identifiers to StateDelta (metadata contract:
	// keys defined once in tagentevent, unified-event-projection D4)
	if evt.StateDelta == nil {
		evt.StateDelta = make(map[string][]byte)
	}
	evt.StateDelta[tagentevent.MetaKeyEventKey] = []byte(tagentevent.FormatEventKey(eventKey))
	evt.StateDelta[tagentevent.MetaKeyPartitionID] = []byte(strconv.Itoa(partitionID))
	evt.StateDelta[tagentevent.MetaKeyEventType] = []byte(eventType)
	evt.StateDelta[tagentevent.MetaKeyEventSummary] = []byte(eventSummary)

	// 9. Update independent causal chain (thread-safe)
	p.mu.Lock()
	p.lastEventKeys[causalKey] = eventKey
	p.mu.Unlock()

	return evt, nil
}

// fakeEvtPrefixRe matches model-fabricated timeline prefixes at the start of
// assistant output. The [evt_KEY|type] prefix is a SYSTEM-generated rendering
// artifact; when a model imitates it, the fake key would poison prefixEventKey
// (which skips already-prefixed content) and buildRetainedRefs' retained-key
// scan. Strip it at the storage boundary.
var fakeEvtPrefixRe = regexp.MustCompile(`^(\[evt_-?(0[xX])?[0-9a-fA-F]+\|[a-z_]+\]\s*)+`)

// sanitizeAssistantContent strips fabricated [evt_...] prefixes from assistant
// output before storage. Non-assistant content is stored verbatim.
func sanitizeAssistantContent(msg model.Message) string {
	if msg.Role != model.RoleAssistant || msg.Content == "" {
		return msg.Content
	}
	cleaned := fakeEvtPrefixRe.ReplaceAllString(msg.Content, "")
	if cleaned != msg.Content {
		log.Warnf("[Memory] stripped model-fabricated [evt_...] prefix from assistant output")
	}
	return cleaned
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

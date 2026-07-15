package agent

import (
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// InjectMessage injects a user message into the agent's event bus.
// The message is published to the persistent bus (not the invocation bus)
// so it can be processed by the persistent event loop.
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

// IngestExternalEvents stores external events for later injection into
// the agent's context. These events are typically from a parent agent
// or external system.
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

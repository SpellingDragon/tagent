package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ErrLoopTerminated is returned by InjectMessageContext after StopLoop: the
// instance's output channel is closed (terminal lifecycle, V15) — a silent
// acceptance here would strand the input forever (3.1).
var ErrLoopTerminated = errors.New("agent: persistent loop already terminated — create a new agent for a fresh loop")

// InjectMessageContext is the decidable injection entry (resident-readiness-
// plan 3.1): it returns a receipt (volatile/durable accepted) or an error —
// terminated loop, full queue, timeout and durable-write failure are NEVER
// reported as accepted. Hosts and HTTP handlers MUST use this entry; the
// legacy void wrappers keep working for internal producers only.
func (ta *TagentAgent) InjectMessageContext(ctx context.Context, source string, msg model.Message) (PublishReceipt, error) {
	if ta == nil {
		return PublishReceipt{}, ErrNilEvent
	}
	// Terminal lifecycle check (V15): StopLoop is terminal on this instance.
	if ta.loopTerminated.Load() {
		return PublishReceipt{}, ErrLoopTerminated
	}
	// Meditation novelty gate (meditation-gate-split): armed HERE at the
	// input-side injection point — ground truth, unchanged.
	ta.armMeditationNoveltyGate(source)
	bus := ta.persistentBus
	if bus == nil {
		ta.activeBusMu.Lock()
		bus = ta.activeBus
		ta.activeBusMu.Unlock()
	}
	if bus == nil {
		return PublishReceipt{}, ErrBusClosed
	}
	evt := NewExternalInputEvent(source, msg)
	return bus.PublishContext(ctx, evt)
}

// InjectEnvelope accepts a WHOLE batch as one acceptance unit (5.2): durable
// mode persists a single multi-message envelope; the returned requestID is
// the batch's stable identity (202 semantics belong to the HTTP layer).
func (ta *TagentAgent) InjectEnvelope(ctx context.Context, source string, msgs []model.Message) (requestID string, durable bool, err error) {
	if ta == nil {
		return "", false, ErrNilEvent
	}
	if ta.loopTerminated.Load() {
		return "", false, ErrLoopTerminated
	}
	ta.armMeditationNoveltyGate(source)
	bus := ta.persistentBus
	if bus == nil {
		ta.activeBusMu.Lock()
		bus = ta.activeBus
		ta.activeBusMu.Unlock()
	}
	if bus == nil {
		return "", false, ErrBusClosed
	}
	rec, err := bus.PublishEnvelopeContext(ctx, source, msgs)
	return rec.RequestID, rec.Durable, err
}

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
	// Meditation novelty gate (meditation-gate-split): a source=="user"
	// injection arms the gate HERE, at the injection point — input-side source
	// is ground truth. Non-user sources (meditation/task/tmux) never arm it,
	// and no output-side event ever does.
	ta.armMeditationNoveltyGate(source)
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

// EmitSystemAlert publishes an environment-level alert (config errors, hot
// reload failures/rollbacks, degraded fallbacks) onto the persistent bus so
// the agent perceives infrastructure problems as events (external_input) on
// its next iteration, instead of them being silently confined to log files.
// 2026-09-14: motivated by the 03:52 incident -- the hot-reloader correctly
// rejected a bad config ("parse FAILED - serving previous") but the agent
// never saw it; the follow-up restart then cold-booted the same bad config.
func (ta *TagentAgent) EmitSystemAlert(alert string) {
	if ta == nil {
		return
	}
	msg := model.Message{
		Role:    model.RoleSystem,
		Content: "[system-alert] " + alert,
	}
	evt := NewExternalInputEvent("system_alert", msg)
	if evt.Metadata == nil {
		evt.Metadata = make(map[string]any)
	}
	evt.Metadata["alert"] = alert
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
	log.Warnf("[EmitSystemAlert] agent %q has no bus, alert dropped: %s", ta.name, alert)
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
	// Same novelty-gate arming as InjectMessageWithSource — both injection
	// entry points are the single source of truth for input lineage.
	ta.armMeditationNoveltyGate(source)
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

// armMeditationNoveltyGate updates the meditation novelty-gate anchor for
// source=="user" injections. No-op when meditation is disabled or the source
// is not user.
func (ta *TagentAgent) armMeditationNoveltyGate(source string) {
	if source == "user" && ta.meditationMgr != nil {
		ta.meditationMgr.UpdateLastUserInput(time.Now())
	}
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

// 契约: docs/wiki/agent/event-flow.md#event-stream-overview
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

// InjectMessageContext 是可判定的注入入口：成功返回凭据（易失或持久已受理），否则返回错误——
// 回路已终止、队列满、超时、持久写失败 绝不报成已受理。宿主与 HTTP handler 必须用它；
// void 包装入口只留给内部生产者使用。
func (ta *TagentAgent) InjectMessageContext(ctx context.Context, source string, msg model.Message) (PublishReceipt, error) {
	if ta == nil {
		return PublishReceipt{}, ErrNilEvent
	}
	if ta.loopTerminatedNow() {
		return PublishReceipt{}, ErrLoopTerminated
	}
	ta.armMeditationNoveltyGate(source)
	ta.selfAudit.ObserveInputFor(source)
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
	if ta.loopTerminatedNow() {
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
	ta.armMeditationNoveltyGate(source)
	if ta.persistentBus != nil {
		ta.persistentBus.Publish(NewExternalInputEvent(source, msg))
		return
	}
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
// : motivated by the 03:52 incident -- the hot-reloader correctly
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
// - "chat_id": target user/session identifier for response routing
// - "user_name": human-readable user identifier for logs
// - "channel": communication channel (wechat, discord, etc.)
func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string) {
	ta.armMeditationNoveltyGate(source)
	evt := NewExternalInputEvent(source, msg)
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

// IngestExternalEvents 把外部事件暂存，供本 agent 的下一次 Run 摄入（direct 兼容入口）。
// 它是单槽交收而非历史缓冲，并有守卫使并发 Run 的取走与本次写入互不撕裂。
// 主委托路径经调用的 RuntimeState 传递上下文，从不碰这个共享槽。
func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent) {
	ta.externalEventsMu.Lock()
	ta.pendingExternalEvents = events
	ta.externalEventsMu.Unlock()
}

// drainPendingExternalEvents 原子取走并清空单槽，交给即将开始的 Run——使 direct 兼容入口
// 调用者的事件折进那一次调用的本地上下文，不会带进并发的第二个 Run。
func (ta *TagentAgent) drainPendingExternalEvents() []memory.FullEvent {
	ta.externalEventsMu.Lock()
	defer ta.externalEventsMu.Unlock()
	events := ta.pendingExternalEvents
	ta.pendingExternalEvents = nil
	return events
}

// applyExternalContext folds external events into the driving message as a
// compact EventSummary prelude (never the full Content — external context stays
// compact so sub-agents stay within their token budget; the sub-agent retrieves
// full details via its own memory tools, memory_get/memory_query, if needed).
// It is a pure function of (msg, events) — no shared `ta` state.
func applyExternalContext(msg model.Message, events []memory.FullEvent) model.Message {
	if len(events) == 0 {
		return msg
	}

	var contextBuilder string
	contextBuilder = "[External Context from Parent Agent]\n\n"
	for i, evt := range events {
		contextBuilder += fmt.Sprintf("Event %d: [%s] %s\n", i+1, evt.EventType, evt.EventSummary)
	}
	contextBuilder += "\n[End of External Context]\n\n"

	log.Infof("[InjectContext] injecting %d external events, context_len=%d", len(events), len(contextBuilder))

	msg.Content = contextBuilder + msg.Content
	return msg
}

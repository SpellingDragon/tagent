package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// runEventLoop runs the persistent event loop for this agent.
// It pulls events from the EventBus, builds an invocation, and runs the flow.
// The loop runs in a dedicated goroutine and exits when ctx is cancelled.
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
		// Mixed-batch defense (meditation-gate-split D4): a meditation event
		// sharing a batch with anything else means the agent was NOT idle when
		// the batch was pulled — meditation's constitutional premise ("never
		// interrupt activity") is already broken, so it yields. This also keeps
		// extractTriggerSource from mislabeling the whole turn in either
		// direction (meditation content tagged "task" / a user task result
		// tagged "meditation" and dropped by consumers).
		events = dropMeditationFromMixedBatch(events, ta.name)
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

		// T-B: turn root span（一 turn 一 trace）。spanCtx 传给 RunFlow，框架自动 span 挂
		// 为子树；turn 末显式 End（循环内禁用 defer，否则累积到函数退出）。noop 当未配 OTLP。
		turnMeta := extractRootMetadata(events)
		spanCtx, turnSpan := startTurnSpan(ctx, turnSpanAttrs{
			AgentName:     ta.name,
			TriggerSource: extractTriggerSource(events),
			ChatID:        turnMeta["chat_id"],
			UserID:        turnMeta["user_id"],
			BatchSize:     len(events),
			EventSources:  eventSources(events),
		})

		// RunFlow with exponential backoff retry
		var lastErr error
		retried := false
		retriedDegenerate := false
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

			if err := cm.RunFlow(spanCtx, msg); err != nil {
				lastErr = err
				log.Errorf("[runEventLoop:%s] RunFlow failed (attempt %d/%d): %v", ta.name, attempt+1, maxRetries+1, err)
				if attempt < maxRetries {
					retried = true
					continue
				}
				// Retries exhausted. Note: RunFlow only returns transport-level
				// errors (model-API errors flow through outputCh as events), so
				// there is no meaningful error event to publish — just log.
				log.Errorf("[runEventLoop:%s] RunFlow exhausted %d retries: %v", ta.name, maxRetries, lastErr)
			} else {
				lastErr = nil
				// A degenerate turn (no tool call, empty final) is an occasional
				// model hiccup that would otherwise stall the conversation until
				// the next external event — retry it exactly once.
				if cm.LastTurnDegenerate() && !retriedDegenerate && attempt < maxRetries {
					retriedDegenerate = true
					log.Warnf("[runEventLoop:%s] degenerate turn (no tool call, empty final) — retrying once", ta.name)
					continue
				}
				break
			}
		}

		if lastErr != nil && !retried {
			// Single failure without retry (shouldn't happen with current logic, but defensive)
			log.Errorf("[runEventLoop:%s] RunFlow failed: %v", ta.name, lastErr)
		}

		// T-B: 关闭 turn span（退化重试标记为属性，同一 turn 不另开 root span）。
		endTurnSpan(turnSpan, retriedDegenerate)

		// Idle-gate anchor (meditation-gate-split): every turn end counts as
		// activity, regardless of trigger source or success — lineage-agnostic
		// by design. A meditation-derived turn merely DELAYS the next
		// meditation; only a user injection can re-arm the novelty gate.
		if ta.meditationMgr != nil {
			ta.meditationMgr.UpdateLastTurnEnd(time.Now())
		}
	}
}

// dropMeditationFromMixedBatch removes meditation events from a batch that
// also contains any non-meditation event (meditation-gate-split D4).
// The dropped meditation is NOT re-injected: lastMeditation already advanced
// at fire time, and the novelty gate re-evaluates naturally at the next real
// idle window. Pure-meditation batches pass through unchanged.
func dropMeditationFromMixedBatch(events []*AgentEvent, agentName string) []*AgentEvent {
	hasMeditation, hasOther := false, false
	for _, evt := range events {
		if evt == nil {
			continue
		}
		if evt.Type == tagentevent.TypeExternalInput && evt.Source == "meditation" {
			hasMeditation = true
		} else {
			hasOther = true
		}
	}
	if !hasMeditation || !hasOther {
		return events
	}
	filtered := make([]*AgentEvent, 0, len(events))
	for _, evt := range events {
		if evt != nil && evt.Type == tagentevent.TypeExternalInput && evt.Source == "meditation" {
			continue
		}
		filtered = append(filtered, evt)
	}
	log.Infof("[runEventLoop:%s] mixed batch: dropped %d meditation event(s) — agent not idle",
		agentName, len(events)-len(filtered))
	return filtered
}

// extractTriggerSource determines the trigger source from a batch of
// AgentEvents. Uses the first external_input event's Source field. This
// provides deterministic source identification for consumer dispatch
// (meditation vs task vs user) without content-based inference.
func extractTriggerSource(events []*AgentEvent) string {
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source != "" {
			return evt.Source
		}
	}
	return "user"
}

// extractRootMetadata extracts metadata from a batch of AgentEvents.
// Collects metadata from external_input events and merges them into a single
// map. Later events override earlier ones. Empty keys or values are ignored.
func extractRootMetadata(events []*AgentEvent) map[string]string {
	md := make(map[string]string)
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
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

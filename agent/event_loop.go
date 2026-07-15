package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
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

		// RunFlow with exponential backoff retry
		var lastErr error
		retried := false
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

			if err := cm.RunFlow(ctx, msg); err != nil {
				lastErr = err
				log.Errorf("[runEventLoop:%s] RunFlow failed (attempt %d/%d): %v", ta.name, attempt+1, maxRetries+1, err)
				if attempt < maxRetries {
					retried = true
					continue
				}
				// Retries exhausted — publish error event to EventBus
				log.Errorf("[runEventLoop:%s] RunFlow exhausted %d retries, publishing error event", ta.name, maxRetries)
				ta.publishErrorEvent(bus, lastErr)
			} else {
				lastErr = nil
				break
			}
		}

		if lastErr != nil && !retried {
			// Single failure without retry (shouldn't happen with current logic, but defensive)
			log.Errorf("[runEventLoop:%s] RunFlow failed: %v", ta.name, lastErr)
		}
	}
}

// extractTriggerSource determines the trigger source from a batch of
// AgentEvents. Uses the first non-agent_output, non-error external_input
// event's Source field. This provides deterministic source identification
// for consumer dispatch (meditation vs async_result vs user) without
// content-based inference.
func extractTriggerSource(events []*AgentEvent) string {
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" {
			continue
		}
		if evt.Source != "" {
			return evt.Source
		}
	}
	return "user"
}

// extractRootMetadata extracts metadata from a batch of AgentEvents.
// Collects metadata from external_input events (non-agent_output, non-error)
// and merges them into a single map. Later events override earlier ones.
// Empty keys or values are ignored.
func extractRootMetadata(events []*AgentEvent) map[string]string {
	md := make(map[string]string)
	for _, evt := range events {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" {
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

// publishErrorEvent publishes an error event to EventBus so external
// listeners can be aware of RunFlow failures.
func (ta *TagentAgent) publishErrorEvent(bus *EventBus, runErr error) {
	if bus == nil || runErr == nil {
		return
	}
	errMsg := fmt.Sprintf("[error] RunFlow failed after retries: %v", runErr)
	busEvt := &AgentEvent{
		ID:        uuid.NewString(),
		Type:      tagentevent.TypeExternalInput,
		Source:    "error",
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleSystem, Content: errMsg},
		Metadata:  make(map[string]any),
	}
	bus.Publish(busEvt)
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

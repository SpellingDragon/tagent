package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// AgentEvent is the unified event type for the agent's event bus.
// Every event flowing through the bus is represented as an AgentEvent,
// carrying a typed payload that the Preprocessor and AgentLoop inspect.
//
// Only two event types serve as bus triggers:
//   - TypeExternalInput: external input (user, tmux, meditation, sub-agent result)
//   - TypeToolUse: LLM decided to call a tool (dispatched asynchronously)
//
// agent_output does NOT enter the bus — it is emitted directly to outputCh.
type AgentEvent struct {
	// ID is a unique identifier for this event.
	ID string

	// Type is the event type (e.g., "external_input", "tool_use").
	// Reuses tagentevent.TypeExternalInput for external inputs.
	// Uses TypeToolUse for tool invocations.
	Type string

	// Source identifies the producer of this event.
	// Values: "user", "tmux", "meditation", "subagent", "agent_loop", "inject".
	Source string

	// Timestamp is when this event was created.
	Timestamp time.Time

	// Message carries the payload for external_input events.
	// Nil for non-external_input events.
	Message *model.Message

	// ToolCall carries the payload for tool_use events.
	// Nil for non-tool_use events.
	ToolCall *model.ToolCall

	// Metadata holds extension data (event_key, partition_id, source_session, etc.).
	Metadata map[string]any
}

// TypeToolUse identifies tool invocation events on the bus.
// Unlike tagentevent.TypeThinkingPlan (which is an event *type* for persistence),
// TypeToolUse is a bus *trigger*: it causes the AgentLoop to dispatch the tool
// asynchronously and continue without blocking.
//
// The LLM's tool_calls are converted to TypeToolUse events on the bus;
// the LLM itself never sees this type in its context.
const TypeToolUse = "tool_use"

// NewExternalInputEvent creates an external_input event with the given source and message payload.
// The message is stored by pointer — callers MUST NOT mutate it after publishing.
func NewExternalInputEvent(source string, msg model.Message) *AgentEvent {
	return &AgentEvent{
		ID:        uuid.NewString(),
		Type:      tagentevent.TypeExternalInput,
		Source:    source,
		Timestamp: time.Now(),
		Message:   &msg,
		Metadata:  make(map[string]any),
	}
}

// NewToolUseEvent creates a tool_use event from a model.ToolCall.
func NewToolUseEvent(toolCall model.ToolCall) *AgentEvent {
	return &AgentEvent{
		ID:        uuid.NewString(),
		Type:      TypeToolUse,
		Source:    "agent_loop",
		Timestamp: time.Now(),
		ToolCall:  &toolCall,
		Metadata:  make(map[string]any),
	}
}

// ---------------------------------------------------------------------------
// EventBus
// ---------------------------------------------------------------------------

// EventBus is a per-agent ordered event queue.
//
// Producers (InjectMessage, TmuxMonitor, MeditationManager, sub-agent callbacks,
// and the AgentLoop itself) call Publish to enqueue events.
//
// The AgentLoop is the sole consumer: it calls Pull to block until at least one
// event arrives, then non-blocking drains all remaining pending events.
//
// Design rationale: a single consumer (AgentLoop) means no fan-out races,
// no ordering guarantees across consumers, and simple backpressure (channel
// fills up → Publish blocks).
type EventBus struct {
	ch chan *AgentEvent
}

// NewEventBus creates an EventBus backed by a buffered channel (cap=256,
// matching the historical mailbox size).
func NewEventBus() *EventBus {
	return &EventBus{
		ch: make(chan *AgentEvent, 256),
	}
}

// publishTimeout is the maximum time Publish will wait before dropping
// an event when the channel is full. This prevents permanent blocking
// if the AgentLoop goroutine has exited unexpectedly.
const publishTimeout = 5 * time.Second

// Publish enqueues an event. If the channel is full, waits up to
// publishTimeout before dropping the event with a warning.
// Logs a warning on nil events.
func (b *EventBus) Publish(event *AgentEvent) {
	if event == nil {
		log.Warnf("[EventBus] Publish nil event, skipped")
		return
	}
	select {
	case b.ch <- event:
	case <-time.After(publishTimeout):
		log.Warnf("[EventBus] Publish timeout (channel full), event dropped: type=%s source=%s",
			event.Type, event.Source)
	}
}

// Pull blocks until at least one event arrives or ctx is cancelled.
// Then non-blocking drains all remaining pending events.
// Returns the batch and nil error on success.
// Returns nil and ctx.Err() when ctx is cancelled before any event arrives.
func (b *EventBus) Pull(ctx context.Context) ([]*AgentEvent, error) {
	// Block for the first event.
	select {
	case evt := <-b.ch:
		batch := []*AgentEvent{evt}
		// Non-blocking drain all remaining events.
		for {
			select {
			case evt := <-b.ch:
				batch = append(batch, evt)
			default:
				return batch, nil
			}
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

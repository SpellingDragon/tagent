package agent

import (
	"context"
	"fmt"
	"github.com/SpellingDragon/tagent/agent/task"
	"strings"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/google/uuid"
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

// SourceTask identifies task_settled events on the bus (a settled background
// task reclaimed into a new turn).
const SourceTask = "task"

// newTaskSettledEvent builds a self-contained external_input event describing a
// background task that has settled, so the persistent loop reclaims it into a
// new turn. It carries the task description, status, and a (truncated) result
// inline so the LLM needs no extra lookup for small results; large results are
// tail-truncated with a hint to use get_task_result.
func newTaskSettledEvent(tk *task.Task, sig task.SettleSignal, maxInline int) *AgentEvent {
	status := "completed"
	switch {
	case sig.Err != nil:
		status = "failed"
	case sig.Kind == task.SettleStable:
		status = "就绪/存活 (alive-detached：后续不再重复通知，除非结束或你主动查询)"
	case sig.Kind == task.SettleSuspect:
		status = "suspect (长时间无输出，可能假死，需确认)"
	}

	result := sig.Output
	if maxInline <= 0 {
		maxInline = DefaultTaskSettledMaxInline
	}
	if len(result) > maxInline {
		result = "...(已截断，完整结果用 get_task_result 拉取)\n" + result[len(result)-maxInline:]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[task settled] 后台任务已结算\n任务: %s\n状态: %s\n(task id: %s)",
		tk.Spec.Desc, status, tk.ID)
	if sig.Err != nil {
		fmt.Fprintf(&b, "\n错误: %v", sig.Err)
	}
	if result != "" {
		fmt.Fprintf(&b, "\n结果:\n%s", result)
	}
	evt := NewExternalInputEvent(SourceTask, model.Message{Role: model.RoleUser, Content: b.String()})
	// Carry the originating turn's opaque routing baggage (chat_id, ...) captured
	// at spawn time, so the reclaim turn's output can be delivered back to the
	// originating session. Reuses the existing extractRootMetadata → meta_*
	// pipeline. (async-result-delivery.)
	for k, v := range tk.Spec.Origin {
		evt.Metadata[k] = v
	}
	return evt
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

// TryPull non-blocking reads all pending events without waiting.
// Returns an empty (non-nil) slice if no events are pending.
// Unlike Pull, this does not block — it immediately returns if the channel is empty.
func (b *EventBus) TryPull() []*AgentEvent {
	batch := []*AgentEvent{}
	for {
		select {
		case evt := <-b.ch:
			if evt != nil {
				batch = append(batch, evt)
			}
		default:
			return batch
		}
	}
}

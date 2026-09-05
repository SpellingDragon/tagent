package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"

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

// settleInlineTail is the tail excerpt kept inline in a spilled task_settled
// notice (aligned with ActionTool's tail view).
const settleInlineTail = 2000

// settleInlineCapChars is the compile-time inline-result cap for task_settled
// notices (context-efficiency-and-trajectory D2/D3): results at/below this stay
// inline (newlines escaped to ␤ for the single-line trajectory form); larger
// results spill to the tool-output dir with a tail preview. It is a named
// constant, NOT a config knob — the derivation `MaxTokens/2*4` that previously
// fed this path (~256K chars at a 128K budget, an unowned formula accident) is
// removed.
const settleInlineCapChars = 600

// settleDescMaxChars caps the task desc rendered in the single-line form.
const settleDescMaxChars = 60

// settleErrMaxChars caps the error text rendered inline for a failed settle.
const settleErrMaxChars = 200

// settleMarkerAndStatus maps a settle signal to its single-line trajectory
// marker and English status word. Markers: ✓ completed / ✗ failed / ∞
// alive-detached / ⚠ suspect.
func settleMarkerAndStatus(sig task.SettleSignal) (marker, statusWord string) {
	switch {
	case sig.Err != nil:
		return "✗", "failed"
	case sig.Kind == task.SettleStable:
		return "∞", "alive-detached"
	case sig.Kind == task.SettleSuspect:
		return "⚠", "suspect"
	default:
		return "✓", "completed"
	}
}

// escapeNewlines flattens internal newlines to ␤ so a settle notice stays a
// single-line trajectory entry (dense, no blank-line padding).
func escapeNewlines(s string) string {
	return strings.NewReplacer("\r\n", "␤", "\n", "␤", "\r", "␤").Replace(s)
}

// newTaskSettledEvent builds a self-contained external_input event describing a
// background task that has settled, so the persistent loop reclaims it into a
// new turn. The event body is a COMPACT SINGLE-LINE trajectory form
// (context-efficiency-and-trajectory D2): `[task settled] <marker> <desc>
// (id=<short>) <status> → 结果: <inline|spill>` — dense, append-only friendly,
// and information-lossless (task_id / desc / status / error / result-or-spill
// ticket all present; only layout redundancy is dropped). Result bounding keeps
// the event body BOUNDED so recalling it can never re-inject an oversized
// result: results over maxChars spill to a file under outputDir
// (workspace.Cleaner bounds the directory) and the Content carries the path
// ticket + tail preview; consumption goes through read_file paging. Write
// failure degrades to inline full text (availability over bounding).
// maxChars<=0 or empty outputDir disables spillover (tests / small results).
func newTaskSettledEvent(tk *task.Task, sig task.SettleSignal, maxChars int, outputDir string) *AgentEvent {
	marker, statusWord := settleMarkerAndStatus(sig)

	result := sig.Output
	spillPath := ""
	if maxChars > 0 && outputDir != "" && len(result) > maxChars {
		shortID := tk.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		path := filepath.Join(outputDir, fmt.Sprintf("task-%s-%d.txt", shortID, time.Now().UnixMilli()))
		if err := os.MkdirAll(outputDir, 0o755); err == nil {
			if err := os.WriteFile(path, []byte(result), 0o644); err == nil {
				log.Infof("[task_settled] result %d chars > %d limit, spilled to %s", len(result), maxChars, path)
				tail := result
				if len(result) > settleInlineTail {
					tail = result[len(result)-settleInlineTail:]
				}
				spillPath = path
				result = fmt.Sprintf("output_spilled 结果 %d 字符已保存到: %s（可用 read_file 配合 start_line/num_lines 分段读取）；尾部: %s",
					len(sig.Output), path, escapeNewlines(tail))
			} else {
				log.Warnf("[task_settled] spill write to %s failed (%v), falling back to inline full text", path, err)
			}
		} else {
			log.Warnf("[task_settled] spill dir %s ensure failed, falling back to inline full text", outputDir)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[task settled] %s %s (id=%s) %s",
		marker, truncateRunes(tk.Spec.Desc, settleDescMaxChars), task.ShortID(tk.ID), statusWord)
	if sig.Err != nil {
		fmt.Fprintf(&b, " 错误: %s", truncateRunes(sig.Err.Error(), settleErrMaxChars))
	}
	if result != "" {
		if spillPath != "" {
			// result already carries the spill ticket + escaped tail
			fmt.Fprintf(&b, " → %s", result)
		} else {
			fmt.Fprintf(&b, " → 结果: %s", escapeNewlines(result))
		}
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

	// spill 是可选的磁盘溢出存储（T-G ReliableBus）。非 nil 时 Publish 在 channel 满时
	// 溢出落盘而非丢弃（at-least-once，常驻不丢事件），Pull 在 channel 空时优先回收溢出项。
	// nil = 纯 channel（现状，向后兼容：NewEventBus 不设，行为逐字节不变）。
	spill *reliability.SpillStore
}

// NewEventBus creates an EventBus backed by a buffered channel (cap=256,
// matching the historical mailbox size).
func NewEventBus() *EventBus {
	return &EventBus{
		ch: make(chan *AgentEvent, 256),
	}
}

// NewReliableEventBus 创建带磁盘溢出的 EventBus（T-G ReliableBus）：channel 满时事件溢出
// 落盘而非丢弃（at-least-once），重启后未消费溢出项可回收。spillDir 为空或 SpillStore 构建
// 失败 → 回退纯 channel bus（现状，可用性优先于持久性）。
func NewReliableEventBus(spillDir string) *EventBus {
	b := NewEventBus()
	if spillDir == "" {
		return b
	}
	store, err := reliability.NewSpillStore(spillDir)
	if err != nil {
		log.Errorf("[ReliableBus] spill store init failed (%v), falling back to in-memory bus", err)
		return b
	}
	b.spill = store
	log.Infof("[ReliableBus] disk spill enabled at %s (at-least-once, no event drop on full channel)", spillDir)
	return b
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
	// 纯 channel 模式（现状，向后兼容）：阻塞等 publishTimeout 后丢弃。
	if b.spill == nil {
		select {
		case b.ch <- event:
		case <-time.After(publishTimeout):
			log.Warnf("[EventBus] Publish timeout (channel full), event dropped: type=%s source=%s",
				event.Type, event.Source)
		}
		return
	}
	// 可靠模式：先非阻塞 try channel，满则溢出落盘（不丢，at-least-once）。
	select {
	case b.ch <- event:
		return
	default:
	}
	data, err := json.Marshal(event)
	if err == nil {
		err = b.spill.Spill(data)
	}
	if err != nil {
		// 序列化或溢出失败：回退阻塞入 channel（尽力不丢），超时才丢弃（可用性优先）。
		log.Errorf("[ReliableBus] spill failed (%v), falling back to blocking publish", err)
		select {
		case b.ch <- event:
		case <-time.After(publishTimeout):
			log.Warnf("[ReliableBus] Publish timeout (channel full + spill failed), event dropped: type=%s source=%s",
				event.Type, event.Source)
		}
		return
	}
	log.Warnf("[ReliableBus] channel full, event spilled to disk (at-least-once): type=%s source=%s",
		event.Type, event.Source)
}

// Pull blocks until at least one event arrives or ctx is cancelled.
// Then non-blocking drains all remaining pending events.
// Returns the batch and nil error on success.
// Returns nil and ctx.Err() when ctx is cancelled before any event arrives.
func (b *EventBus) Pull(ctx context.Context) ([]*AgentEvent, error) {
	// 优先回收磁盘溢出项（时序更早：溢出发生在 channel 满时，早于此后入 channel 的新事件）。
	if spilled := b.drainSpill(); len(spilled) > 0 {
		return append(spilled, b.drainChannel()...), nil
	}
	// 无溢出：阻塞等 channel 首个事件（现状逻辑）。
	select {
	case evt := <-b.ch:
		batch := []*AgentEvent{evt}
		batch = append(batch, b.drainChannel()...)
		// channel 消费腾出空间后，回收可能存在的溢出项（防滞留）。
		batch = append(batch, b.drainSpill()...)
		return batch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// drainChannel 非阻塞排空 channel 现有事件。
func (b *EventBus) drainChannel() []*AgentEvent {
	var batch []*AgentEvent
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

// drainSpill 回收全部磁盘溢出项（反序列化为 AgentEvent）。坏项跳过（Reclaim 已删）；回收
// 出错则停止本轮 drain（下次 Pull 再试，防死循环）。spill 为 nil 返回空（现状逐字节不变）。
func (b *EventBus) drainSpill() []*AgentEvent {
	if b.spill == nil {
		return nil
	}
	var batch []*AgentEvent
	for {
		data, ok, err := b.spill.Reclaim()
		if err != nil {
			log.Warnf("[ReliableBus] reclaim error, stopping drain: %v", err)
			return batch
		}
		if !ok {
			return batch
		}
		var evt AgentEvent
		if uerr := json.Unmarshal(data, &evt); uerr != nil {
			log.Warnf("[ReliableBus] reclaim unmarshal failed, dropping spilled event: %v", uerr)
			continue
		}
		batch = append(batch, &evt)
	}
}

// TryPull non-blocking reads all pending events without waiting.
// Returns an empty (non-nil) slice if no events are pending.
// Unlike Pull, this does not block — it immediately returns if the channel is empty.
func (b *EventBus) TryPull() []*AgentEvent {
	// 先回收溢出项，再排空 channel（与 Pull 一致的回收优先级）。
	batch := b.drainSpill()
	batch = append(batch, b.drainChannel()...)
	if batch == nil {
		batch = []*AgentEvent{}
	}
	return batch
}

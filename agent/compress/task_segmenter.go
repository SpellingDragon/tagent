package compress

import (
	"fmt"
	"strings"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// TaskSegment
// ---------------------------------------------------------------------------

// TaskSegment is a group of messages delimited by task boundaries.
// Under the skeleton model (task-skeleton-compression) a segment is one
// complete task turn `[external_input, (thinking_plan|action_command)*,
// agent_output]`, closed by the final reply. A trailing segment without an
// agent_output is in progress (IsComplete=false) and never compressed.
type TaskSegment struct {
	Messages   []model.Message
	IsComplete bool
}

// MessageEventType classifies a message by event type (task-skeleton D1).
// Primary: the [evt_KEY|type] prefix stamped by resolveRef. Fallback for
// unprefixed inputs (e.g. tests feeding bare messages): the role heuristic of
// ExtractEventType — assistant without tool_calls counts as agent_output.
func MessageEventType(msg *model.Message) string {
	if _, evtType, _ := tagentevent.ParseEventKeyAndType(msg.Content); evtType != "unknown" {
		return evtType
	}
	return tagentevent.ExtractEventType(*msg)
}

// isAgentOutputMessage reports whether a message closes a task turn.
func isAgentOutputMessage(msg *model.Message) bool {
	return MessageEventType(msg) == tagentevent.TypeAgentOutput
}

// IsSkeletonMessage reports whether a message is a task-skeleton node —
// a pure event-type function, never reading message content. Skeleton:
// external_input / agent_output. Droppable intermediates: action_command /
// thinking_plan. Any other type (e.g. the rolling context_compress summary)
// is conservatively treated as skeleton so it is never dropped in-segment.
func IsSkeletonMessage(msg *model.Message) bool {
	// 委托事件类型注册表：Skeleton=false 仅 action_command/thinking_plan，
	// 其余（含未知类型）保守为 true，永不段内丢弃。
	return tagentevent.IsSkeletonEventType(MessageEventType(msg))
}

// SegmentMessages splits messages into task-turn segments bounded by
// agent_output (task-skeleton D1): an agent_output closes the current
// segment; consecutive external_input (user re-sends, agent silent) stay in
// the same in-progress segment; a trailing run without agent_output is left
// IsComplete=false.
func SegmentMessages(messages []model.Message) []*TaskSegment {
	if len(messages) == 0 {
		return nil
	}
	var segments []*TaskSegment
	var current *TaskSegment
	for i := range messages {
		msg := &messages[i]
		if current == nil {
			current = &TaskSegment{}
		}
		current.Messages = append(current.Messages, *msg)
		if isAgentOutputMessage(msg) {
			current.IsComplete = true
			segments = append(segments, current)
			current = nil
		}
	}
	if current != nil {
		segments = append(segments, current)
	}
	return segments
}

// buildSummaryReference creates a compact summary reference for compressed events.
// Used by ContextCompressor.buildRetainedRefs.
func buildSummaryReference(keys []string, minTs int64) memory.EventReference {
	if minTs == 0 {
		minTs = 1
	}
	return memory.EventReference{
		EventKey:     -minTs,
		EventType:    tagentevent.TypeContextCompress,
		EventSummary: fmt.Sprintf("[Compacted %d historical events: keys=%s]", len(keys), strings.Join(keys, ",")),
		Timestamp:    minTs,
		Role:         "system",
	}
}

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
type TaskSegment struct {
	Messages   []model.Message
	IsComplete bool
}

// SegmentMessages splits messages into segments at task boundaries.
func SegmentMessages(messages []model.Message) []*TaskSegment {
	if len(messages) == 0 {
		return nil
	}
	var segments []*TaskSegment
	var current *TaskSegment
	for i := range messages {
		msg := &messages[i]
		if isMessageTaskBoundary(msg) && current != nil {
			current.IsComplete = true
			segments = append(segments, current)
			current = nil
		}
		if current == nil {
			current = &TaskSegment{}
		}
		current.Messages = append(current.Messages, *msg)
	}
	if current != nil {
		segments = append(segments, current)
	}
	return segments
}

func isMessageTaskBoundary(msg *model.Message) bool {
	return msg.Role == model.RoleUser
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

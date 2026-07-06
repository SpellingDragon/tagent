package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
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
		if current == nil {
			current = &TaskSegment{}
		}
		current.Messages = append(current.Messages, *msg)
		if isMessageTaskBoundary(msg) {
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

// SegmentReferences splits references into task groups at task boundaries.
func SegmentReferences(refs []memory.EventReference) [][]memory.EventReference {
	var tasks [][]memory.EventReference
	var current []memory.EventReference
	for _, ref := range refs {
		current = append(current, ref)
		if isReferenceTaskBoundary(ref) {
			tasks = append(tasks, current)
			current = nil
		}
	}
	if len(current) > 0 {
		tasks = append(tasks, current)
	}
	return tasks
}

func isMessageTaskBoundary(msg *model.Message) bool {
	return msg.Role == model.RoleAssistant && len(msg.ToolCalls) == 0
}

func isReferenceTaskBoundary(ref memory.EventReference) bool {
	if ref.EventType == tagentevent.TypeAgentOutput {
		return true
	}
	if ref.EventType == "" && ref.Role == string(model.RoleAssistant) {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Compactor
// ---------------------------------------------------------------------------

const DefaultCompactKeepRecentTasks = 2

// Compactor reduces SessionProjection by collapsing old tasks into a summary.
type Compactor struct {
	keepRecentTasks int
}

func NewCompactor(keepRecentTasks int) *Compactor {
	if keepRecentTasks <= 0 {
		keepRecentTasks = DefaultCompactKeepRecentTasks
	}
	return &Compactor{keepRecentTasks: keepRecentTasks}
}

func (c *Compactor) Compact(refs []memory.EventReference) []memory.EventReference {
	if len(refs) == 0 {
		return refs
	}
	startTime := time.Now()
	tasks := SegmentReferences(refs)
	if len(tasks) <= c.keepRecentTasks {
		return refs
	}
	recentStart := len(tasks) - c.keepRecentTasks
	oldTasks := tasks[:recentStart]
	recentTasks := tasks[recentStart:]

	var oldRefs []memory.EventReference
	for _, task := range oldTasks {
		oldRefs = append(oldRefs, task...)
	}

	summaryRef := buildSummaryReference(oldRefs)
	result := make([]memory.EventReference, 0, 1+len(recentTasks))
	result = append(result, summaryRef)
	for _, task := range recentTasks {
		result = append(result, task...)
	}

	// Structured JSON metrics
	metrics := map[string]interface{}{
		"event":           "compactor",
		"before_refs":     len(refs),
		"after_refs":      len(result),
		"compacted_tasks": len(oldTasks),
		"duration_ms":     time.Since(startTime).Milliseconds(),
	}
	if metricsJSON, err := json.Marshal(metrics); err == nil {
		log.Infof("[Compactor] %s", string(metricsJSON))
	}

	return result
}

func buildSummaryReference(oldRefs []memory.EventReference) memory.EventReference {
	var keys []string
	var minTs int64
	for i, ref := range oldRefs {
		keys = append(keys, fmt.Sprintf("%d", ref.EventKey))
		if i == 0 || ref.Timestamp < minTs {
			minTs = ref.Timestamp
		}
	}
	if minTs == 0 {
		minTs = time.Now().UnixMilli()
	}
	return memory.EventReference{
		EventType:    tagentevent.TypeContextCompress,
		EventSummary: fmt.Sprintf("[Compacted %d historical events: keys=%s]", len(oldRefs), strings.Join(keys, ",")),
		Timestamp:    minTs,
		Role:         "system",
	}
}

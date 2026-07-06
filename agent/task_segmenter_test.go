package agent

import (
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestSegmentMessages_BasicBoundary(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "task1"},
		{Role: model.RoleAssistant, Content: "resp1"}, // boundary (no tool calls)
		{Role: model.RoleUser, Content: "task2"},
		{Role: model.RoleAssistant, Content: "resp2"}, // boundary
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 2)
	assert.True(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 2)
}

func TestSegmentMessages_IncompleteTrailingSegment(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "task1"},
		{Role: model.RoleAssistant, Content: "resp1"}, // boundary
		{Role: model.RoleUser, Content: "task2"},      // incomplete (no assistant response yet)
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete)
}

func TestSegmentMessages_ToolCallNotBoundary(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "do something"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "echo"}},
		}}, // NOT a boundary (has tool calls)
		{Role: model.RoleTool, Content: "result"},
		{Role: model.RoleAssistant, Content: "final"}, // boundary
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 1)
	assert.True(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 4)
}

func TestSegmentMessages_Empty(t *testing.T) {
	assert.Nil(t, SegmentMessages(nil))
	assert.Nil(t, SegmentMessages([]model.Message{}))
}

func TestSegmentReferences_BasicBoundary(t *testing.T) {
	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, Role: "user"},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, Role: "assistant"},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput, Role: "user"},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput, Role: "assistant"},
	}

	tasks := SegmentReferences(refs)
	require.Len(t, tasks, 2)
	assert.Len(t, tasks[0], 2)
	assert.Len(t, tasks[1], 2)
}

func TestSegmentReferences_IncompleteTrailingTask(t *testing.T) {
	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, Role: "user"},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, Role: "assistant"},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput, Role: "user"},
	}

	tasks := SegmentReferences(refs)
	require.Len(t, tasks, 2)
	assert.Len(t, tasks[0], 2)
	assert.Len(t, tasks[1], 1)
}

func TestSegmentReferences_FallbackBoundary(t *testing.T) {
	refs := []memory.EventReference{
		{EventKey: 1, EventType: "", Role: "user"},
		{EventKey: 2, EventType: "", Role: "assistant"}, // fallback boundary
		{EventKey: 3, EventType: "", Role: "user"},
	}

	tasks := SegmentReferences(refs)
	require.Len(t, tasks, 2)
}

func TestSegmentReferences_Empty(t *testing.T) {
	assert.Nil(t, SegmentReferences(nil))
	assert.Nil(t, SegmentReferences([]memory.EventReference{}))
}

func TestSegmentMessagesAndReferences_ConsistentBoundaries(t *testing.T) {
	// A conversation with 3 tasks should produce 3 segments in both representations.
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "q1"},
		{Role: model.RoleAssistant, Content: "a1"},
		{Role: model.RoleUser, Content: "q2"},
		{Role: model.RoleAssistant, Content: "a2"},
		{Role: model.RoleUser, Content: "q3"},
		{Role: model.RoleAssistant, Content: "a3"},
	}

	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, Role: "user"},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, Role: "assistant"},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput, Role: "user"},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput, Role: "assistant"},
		{EventKey: 5, EventType: tagentevent.TypeExternalInput, Role: "user"},
		{EventKey: 6, EventType: tagentevent.TypeAgentOutput, Role: "assistant"},
	}

	msgSegments := SegmentMessages(msgs)
	refTasks := SegmentReferences(refs)

	assert.Len(t, msgSegments, 3)
	assert.Len(t, refTasks, 3)

	for i := 0; i < 3; i++ {
		assert.True(t, msgSegments[i].IsComplete, "segment %d should be complete", i)
		assert.Len(t, refTasks[i], 2, "task %d should have 2 refs", i)
	}
}

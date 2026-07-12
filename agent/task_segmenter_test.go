package agent

import (
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSegmentMessages_BasicBoundary(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "task1"},
		{Role: model.RoleAssistant, Content: "resp1"},
		{Role: model.RoleUser, Content: "task2"},
		{Role: model.RoleAssistant, Content: "resp2"},
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 2)
	assert.False(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 2)
}

func TestSegmentMessages_IncompleteTrailingSegment(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "task1"},
		{Role: model.RoleAssistant, Content: "resp1"},
		{Role: model.RoleUser, Content: "task2"},
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
		}},
		{Role: model.RoleTool, Content: "result"},
		{Role: model.RoleAssistant, Content: "final"},
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 1)
	assert.False(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 4)
}

func TestSegmentMessages_Empty(t *testing.T) {
	assert.Nil(t, SegmentMessages(nil))
	assert.Nil(t, SegmentMessages([]model.Message{}))
}

func TestSegmentMessages_ThreeTasks(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "q1"},
		{Role: model.RoleAssistant, Content: "a1"},
		{Role: model.RoleUser, Content: "q2"},
		{Role: model.RoleAssistant, Content: "a2"},
		{Role: model.RoleUser, Content: "q3"},
		{Role: model.RoleAssistant, Content: "a3"},
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 3)
	assert.True(t, segments[0].IsComplete, "segment 0 should be complete")
	assert.True(t, segments[1].IsComplete, "segment 1 should be complete")
	assert.False(t, segments[2].IsComplete, "segment 2 should be incomplete (trailing)")
}

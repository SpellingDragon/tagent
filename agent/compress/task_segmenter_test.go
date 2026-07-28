package compress

import (
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prefixedMsg builds a message carrying the [evt_KEY|type] prefix, the
// primary segmentation signal (task-skeleton D1).
func prefixedMsg(role model.Role, key int64, evtType, content string) model.Message {
	return model.Message{
		Role:    role,
		Content: prefixEventKey(content, memory.EventReference{EventKey: key, EventType: evtType}),
	}
}

// TestSegmentMessages_CompleteTurns: agent_output closes each segment
// (spec scenario "完整回合闭合成一个段").
func TestSegmentMessages_CompleteTurns(t *testing.T) {
	msgs := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "task A"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeThinkingPlan, "plan A"),
		prefixedMsg(model.RoleTool, 3, tagentevent.TypeActionCommand, "tool A"),
		prefixedMsg(model.RoleAssistant, 4, tagentevent.TypeAgentOutput, "reply A"),
		prefixedMsg(model.RoleUser, 5, tagentevent.TypeExternalInput, "task B"),
		prefixedMsg(model.RoleAssistant, 6, tagentevent.TypeAgentOutput, "reply B"),
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 4)
	assert.True(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 2)
}

// TestSegmentMessages_ConsecutiveExternalInputs: user re-sends without an
// agent reply stay in one in-progress segment (spec scenario "连续
// external_input 归入同一进行中段").
func TestSegmentMessages_ConsecutiveExternalInputs(t *testing.T) {
	msgs := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "task A"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeAgentOutput, "reply A"),
		prefixedMsg(model.RoleUser, 3, tagentevent.TypeExternalInput, "task B"),
		prefixedMsg(model.RoleUser, 4, tagentevent.TypeExternalInput, "task C"),
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 2, "consecutive external_input must merge into one in-progress segment")
}

// TestSegmentMessages_TrailingWithoutOutput: a tail without agent_output is
// an in-progress segment (spec scenario "无 agent_output 的尾部为进行中段").
func TestSegmentMessages_TrailingWithoutOutput(t *testing.T) {
	msgs := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "task A"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeAgentOutput, "reply A"),
		prefixedMsg(model.RoleUser, 3, tagentevent.TypeExternalInput, "task B"),
		prefixedMsg(model.RoleAssistant, 4, tagentevent.TypeThinkingPlan, "plan B"),
		prefixedMsg(model.RoleTool, 5, tagentevent.TypeActionCommand, "tool B"),
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete)
	assert.Len(t, segments[1].Messages, 3)
}

// TestSegmentMessages_HeuristicFallback: unprefixed messages fall back to the
// role heuristic — assistant without tool_calls closes the turn.
func TestSegmentMessages_HeuristicFallback(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "do something"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "echo"}},
		}},
		{Role: model.RoleTool, Content: "result"},
		{Role: model.RoleAssistant, Content: "final"},
		{Role: model.RoleUser, Content: "next task"},
	}

	segments := SegmentMessages(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete, "assistant without tool_calls closes the turn")
	assert.Len(t, segments[0].Messages, 4)
	assert.False(t, segments[1].IsComplete, "pending user input is an in-progress segment")
}

func TestSegmentMessages_Empty(t *testing.T) {
	assert.Nil(t, SegmentMessages(nil))
	assert.Nil(t, SegmentMessages([]model.Message{}))
}

// TestIsSkeletonMessage: skeleton vs intermediate is a pure event-type
// function; content is never read.
func TestIsSkeletonMessage(t *testing.T) {
	skeleton := []model.Message{
		prefixedMsg(model.RoleUser, 1, tagentevent.TypeExternalInput, "in"),
		prefixedMsg(model.RoleAssistant, 2, tagentevent.TypeAgentOutput, "out"),
		// Rolling summary and other types are conservatively kept.
		prefixedMsg(model.RoleUser, -3, tagentevent.TypeContextCompress, "compacted"),
	}
	for i := range skeleton {
		assert.True(t, IsSkeletonMessage(&skeleton[i]), "message %d should be skeleton", i)
	}

	intermediate := []model.Message{
		prefixedMsg(model.RoleTool, 4, tagentevent.TypeActionCommand, "tool result"),
		prefixedMsg(model.RoleAssistant, 5, tagentevent.TypeThinkingPlan, "planning"),
	}
	for i := range intermediate {
		assert.False(t, IsSkeletonMessage(&intermediate[i]), "message %d should be intermediate", i)
	}
}

// TestSegmentMessagesByUser_Legacy locks the legacy fallback path
// (WithSkeletonSegmentation(false)): user messages open segments.
func TestSegmentMessagesByUser_Legacy(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "task1"},
		{Role: model.RoleAssistant, Content: "resp1"},
		{Role: model.RoleUser, Content: "task2"},
		{Role: model.RoleAssistant, Content: "resp2"},
	}

	segments := segmentMessagesByUser(msgs)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete, "legacy trailing segment stays incomplete")
}

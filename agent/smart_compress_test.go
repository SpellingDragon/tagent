package agent

import (
	"context"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// splitSystemMessage tests
// ============================================================================

func TestSplitSystemMessage_WithSystem(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	sys, rest := splitSystemMessage(messages)
	require.NotNil(t, sys)
	assert.Equal(t, "system prompt", sys.Content)
	assert.Len(t, rest, 2)
}

func TestSplitSystemMessage_NoSystem(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	sys, rest := splitSystemMessage(messages)
	assert.Nil(t, sys)
	assert.Len(t, rest, 2)
}

func TestSplitSystemMessage_Empty(t *testing.T) {
	sys, rest := splitSystemMessage(nil)
	assert.Nil(t, sys)
	assert.Nil(t, rest)
}

// ============================================================================
// splitByTaskBoundary tests
// ============================================================================

func TestSplitByTaskBoundary_SingleTask(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	segments := splitByTaskBoundary(messages)
	require.Len(t, segments, 1)
	assert.True(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 2)
}

func TestSplitByTaskBoundary_MultipleTasks(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	segments := splitByTaskBoundary(messages)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.True(t, segments[1].IsComplete)
}

func TestSplitByTaskBoundary_IncompleteTask(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2 (incomplete)"},
	}

	segments := splitByTaskBoundary(messages)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete, "last segment without agent_output should be incomplete")
}

func TestSplitByTaskBoundary_ToolCallNotBoundary(t *testing.T) {
	// Assistant with tool calls should NOT be a task boundary
	messages := []model.Message{
		{Role: model.RoleUser, Content: "do something"},
		{Role: model.RoleAssistant, Content: "using tool", ToolCalls: []model.ToolCall{{ID: "1"}}},
		{Role: model.RoleTool, Content: "tool result"},
		{Role: model.RoleAssistant, Content: "final answer"},
	}

	segments := splitByTaskBoundary(messages)
	require.Len(t, segments, 1, "tool call cycle should be part of one task")
	assert.True(t, segments[0].IsComplete)
}

func TestSplitByTaskBoundary_Empty(t *testing.T) {
	segments := splitByTaskBoundary(nil)
	assert.Nil(t, segments)
}

// ============================================================================
// SmartCompressor Compress tests
// ============================================================================

func TestSmartCompress_Stage1Only(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	// Build messages with multiple task segments
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
		{Role: model.RoleUser, Content: "task 3"},
		{Role: model.RoleAssistant, Content: "result 3"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// Should keep: system + compress notice + 1 recent task
	assert.Less(t, len(result), len(messages), "compressed messages should be fewer")
	assert.Equal(t, model.RoleSystem, result[0].Role, "first message should be system")
	// The last messages should be from the most recent task
	assert.Equal(t, "task 3", result[len(result)-2].Content)
	assert.Equal(t, "result 3", result[len(result)-1].Content)
}

func TestSmartCompress_PreservesSystem(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	messages := []model.Message{
		{Role: model.RoleSystem, Content: "important system prompt"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	assert.Equal(t, model.RoleSystem, result[0].Role)
	assert.Equal(t, "important system prompt", result[0].Content, "system prompt should be preserved")
}

func TestSmartCompress_PreservesRecentTasks(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2))

	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
		{Role: model.RoleUser, Content: "task 3"},
		{Role: model.RoleAssistant, Content: "result 3"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// Should keep: system + compress notice + task2 + task3 (2 recent)
	// Find task2 and task3 in the result
	foundTask2 := false
	foundTask3 := false
	for _, msg := range result {
		if msg.Content == "task 2" {
			foundTask2 = true
		}
		if msg.Content == "task 3" {
			foundTask3 = true
		}
	}
	assert.True(t, foundTask2, "recent task 2 should be preserved")
	assert.True(t, foundTask3, "recent task 3 should be preserved")
}

func TestSmartCompress_UnderThreshold(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2))

	// Only 1 task segment, which is <= keepRecentTasks
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	result := sc.Compress(context.Background(), messages, nil)
	assert.Equal(t, messages, result, "should not compress when under threshold")
}

func TestSmartCompress_NoSystem(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)
	assert.Less(t, len(result), len(messages), "should compress even without system message")
	// First message should be the compress notice
	assert.Equal(t, model.RoleSystem, result[0].Role)
}

// ============================================================================
// Fallback Notice tests
// ============================================================================

func TestSmartCompress_Fallback_NoModel(t *testing.T) {
	// Without summaryModel, compression should include [Compressed: ...] notice
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// The compress notice should be the second message (after system)
	require.Len(t, result, 4) // system + compress + 1 recent task (2 msgs)
	compressMsg := result[1]
	assert.Equal(t, model.RoleSystem, compressMsg.Role)
	// Should contain the [Compressed: ...] format
	assert.Contains(t, compressMsg.Content, "[Compressed: 1 earlier tasks omitted.]")
	// Should NOT contain the recall agent hint (no model = no error)
	assert.NotContains(t, compressMsg.Content, "recall agent")
	// Should still contain the Chinese context_compress prefix
	assert.Contains(t, compressMsg.Content, "[context_compress]")
}

func TestSmartCompress_Fallback_CompressNoticeFormat(t *testing.T) {
	// Verify the [context_compress] prefix is present in compressed output
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// First message is the compress notice
	compressMsg := result[0]
	assert.Contains(t, compressMsg.Content, "[context_compress]", "compress notice should start with [context_compress] prefix")
	assert.Contains(t, compressMsg.Content, "[Compressed:", "compress notice should contain [Compressed: ...] format")
}

// ============================================================================
// parseEventKeyFromPrefix tests
// ============================================================================

func TestParseEventKeyFromPrefix_Valid(t *testing.T) {
	key := parseEventKeyFromPrefix("[evt_123456789|task] user request content")
	assert.Equal(t, int64(123456789), key)
}

func TestParseEventKeyFromPrefix_NoPrefix(t *testing.T) {
	key := parseEventKeyFromPrefix("user request content")
	assert.Equal(t, int64(0), key)
}

func TestParseEventKeyFromPrefix_Malformed(t *testing.T) {
	key := parseEventKeyFromPrefix("[evt_invalid_key|task] content")
	assert.Equal(t, int64(0), key)
}

func TestParseEventKeyFromPrefix_NoBar(t *testing.T) {
	key := parseEventKeyFromPrefix("[evt_12345task] content")
	assert.Equal(t, int64(0), key)
}

func TestParseEventKeyFromPrefix_LargeKey(t *testing.T) {
	key := parseEventKeyFromPrefix("[evt_9223372036854775807|memory] large snowflake key")
	assert.Equal(t, int64(9223372036854775807), key)
}

func TestParseEventKeyFromPrefix_EmptyContent(t *testing.T) {
	key := parseEventKeyFromPrefix("")
	assert.Equal(t, int64(0), key)
}

func TestParseEventKeyFromPrefix_OnlyPrefix(t *testing.T) {
	key := parseEventKeyFromPrefix("[evt_42|")
	assert.Equal(t, int64(42), key)
}

// ============================================================================
// collectCompressedKeys tests
// ============================================================================

func TestCollectCompressedKeys_SingleKey(t *testing.T) {
	sc := &SmartCompressor{}
	segments := []*TaskSegment{
		{Messages: []model.Message{
			{Role: model.RoleUser, Content: "[evt_100|user_input] hello"},
			{Role: model.RoleAssistant, Content: "[evt_101|task] world"},
		}},
	}

	keys := sc.collectCompressedKeys(segments)
	assert.Equal(t, []int64{100, 101}, keys)
}

func TestCollectCompressedKeys_DuplicateKeys(t *testing.T) {
	sc := &SmartCompressor{}
	segments := []*TaskSegment{
		{Messages: []model.Message{
			{Role: model.RoleUser, Content: "[evt_100|user_input] task 1"},
			{Role: model.RoleAssistant, Content: "[evt_100|task] result 1"},
		}},
		{Messages: []model.Message{
			{Role: model.RoleUser, Content: "[evt_100|user_input] task 2"},
		}},
	}

	keys := sc.collectCompressedKeys(segments)
	assert.Equal(t, []int64{100}, keys, "duplicate keys should be deduplicated")
}

func TestCollectCompressedKeys_MessagesWithoutPrefix(t *testing.T) {
	sc := &SmartCompressor{}
	segments := []*TaskSegment{
		{Messages: []model.Message{
			{Role: model.RoleSystem, Content: "system prompt"},
			{Role: model.RoleUser, Content: "[evt_200|user_input] hello"},
		}},
	}

	keys := sc.collectCompressedKeys(segments)
	assert.Equal(t, []int64{200}, keys, "system message without prefix should be skipped")
}

func TestCollectCompressedKeys_MixedValidInvalid(t *testing.T) {
	sc := &SmartCompressor{}
	segments := []*TaskSegment{
		{Messages: []model.Message{
			{Role: model.RoleUser, Content: "[evt_300|user_input] valid"},
			{Role: model.RoleAssistant, Content: "no prefix here"},
			{Role: model.RoleTool, Content: "[evt_invalid_key|tool] malformed"},
			{Role: model.RoleAssistant, Content: "[evt_301|task] final"},
		}},
	}

	keys := sc.collectCompressedKeys(segments)
	assert.Equal(t, []int64{300, 301}, keys, "only valid prefixed keys should be extracted")
}

func TestCollectCompressedKeys_EmptySegments(t *testing.T) {
	sc := &SmartCompressor{}

	keys := sc.collectCompressedKeys(nil)
	assert.Empty(t, keys)

	keys = sc.collectCompressedKeys([]*TaskSegment{})
	assert.Empty(t, keys)
}

func TestCollectCompressedKeys_DistinctEventKeys(t *testing.T) {
	sc := &SmartCompressor{}
	segments := []*TaskSegment{
		{Messages: []model.Message{
			{Role: model.RoleUser, Content: "[evt_111|user_input] task 1"},
		}},
		{Messages: []model.Message{
			{Role: model.RoleAssistant, Content: "[evt_222|task] result 1"},
		}},
		{Messages: []model.Message{
			{Role: model.RoleTool, Content: "[evt_333|tool_call] tool result"},
		}},
	}

	keys := sc.collectCompressedKeys(segments)
	assert.ElementsMatch(t, []int64{111, 222, 333}, keys, "all distinct keys across segments should be collected")
}

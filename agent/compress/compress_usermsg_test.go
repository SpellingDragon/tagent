package compress

import (
	"context"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== Task 6.6: 有 pending user 时保留原始 user message ====================

func TestCompress_PreservesPendingUserMessage(t *testing.T) {
	// Create enough segments to trigger compression (KeepRecentTasks=2 by default)
	// The last segment has a pending user message
	messages := []model.Message{
		// Old segment 1 (will be compressed)
		{Role: model.RoleUser, Content: "old task 1"},
		{Role: model.RoleAssistant, Content: "old result 1"},
		// Old segment 2 (will be compressed)
		{Role: model.RoleUser, Content: "old task 2"},
		{Role: model.RoleAssistant, Content: "old result 2"},
		// Recent segment 1 (complete)
		{Role: model.RoleUser, Content: "recent task 1"},
		{Role: model.RoleAssistant, Content: "recent result 1"},
		// Recent segment 2 (incomplete - has pending user)
		{Role: model.RoleUser, Content: "pending new task"},
	}

	sc := NewSmartCompressor() // KeepRecentTasks=2, no summaryModel
	result := sc.Compress(context.Background(), messages, nil)

	require.NotEmpty(t, result)

	// The last message should be the pending user message
	lastMsg := result[len(result)-1]
	assert.Equal(t, model.RoleUser, lastMsg.Role)
	assert.Equal(t, "pending new task", lastMsg.Content,
		"last message should be the pending user message")
}

// ==================== Task 6.7: 无 pending user 时添加引导消息 ====================

func TestCompress_AddsGuidanceMessageWhenNoPendingUser(t *testing.T) {
	// With "user input as anchor" compression:
	// - Old segments are fully compressed
	// - Recent segments: user input preserved, execution compressed (if long enough)
	// - Last incomplete segment: preserved fully (current context)
	messages := []model.Message{
		// Old segment 1 (will be compressed)
		{Role: model.RoleUser, Content: "old task 1"},
		{Role: model.RoleAssistant, Content: "old result 1"},
		// Old segment 2 (will be compressed)
		{Role: model.RoleUser, Content: "old task 2"},
		{Role: model.RoleAssistant, Content: "old result 2"},
		// Recent segment 1 (complete - closed by next user input)
		{Role: model.RoleUser, Content: "recent task 1"},
		{Role: model.RoleAssistant, Content: "recent result 1"},
		// Recent segment 2 (incomplete - no next user input to close it)
		{Role: model.RoleUser, Content: "recent task 2"},
		{Role: model.RoleAssistant, Content: "recent result 2"},
	}

	sc := NewSmartCompressor()
	result := sc.Compress(context.Background(), messages, nil)

	require.NotEmpty(t, result)

	// Last incomplete segment is preserved fully — last message is "recent result 2"
	lastMsg := result[len(result)-1]
	assert.Equal(t, "recent result 2", lastMsg.Content,
		"last message should be from the fully preserved last incomplete segment")

	// No guidance message ("以上是对话历史摘要") should be present
	for _, msg := range result {
		assert.NotContains(t, msg.Content, "以上是对话历史摘要",
			"guidance message should not be appended")
	}
}

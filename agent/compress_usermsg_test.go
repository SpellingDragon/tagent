package agent

import (
	"context"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== Task 6.6: 有 pending user 时保留原始 user message ====================

func TestFindPendingUserMessage_WithPendingUser(t *testing.T) {
	// Segments: [complete] [complete] [incomplete with user message]
	segments := []*TaskSegment{
		{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "task 1"},
				{Role: model.RoleAssistant, Content: "result 1"},
			},
			IsComplete: true,
		},
		{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "task 2"},
				{Role: model.RoleAssistant, Content: "result 2"},
			},
			IsComplete: true,
		},
		{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "new task pending"},
			},
			IsComplete: false,
		},
	}

	msg := findPendingUserMessage(segments)
	require.NotNil(t, msg)
	assert.Equal(t, "new task pending", msg.Content)
	assert.Equal(t, model.RoleUser, msg.Role)
}

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

func TestFindPendingUserMessage_NoPendingUser(t *testing.T) {
	// All segments are complete - no pending user message
	segments := []*TaskSegment{
		{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "task 1"},
				{Role: model.RoleAssistant, Content: "result 1"},
			},
			IsComplete: true,
		},
		{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "task 2"},
				{Role: model.RoleAssistant, Content: "result 2"},
			},
			IsComplete: true,
		},
	}

	msg := findPendingUserMessage(segments)
	assert.Nil(t, msg, "should return nil when no pending user message")
}

func TestCompress_AddsGuidanceMessageWhenNoPendingUser(t *testing.T) {
	// All segments are complete - no pending user message after compression
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
		// Recent segment 2 (complete)
		{Role: model.RoleUser, Content: "recent task 2"},
		{Role: model.RoleAssistant, Content: "recent result 2"},
	}

	sc := NewSmartCompressor()
	result := sc.Compress(context.Background(), messages, nil)

	require.NotEmpty(t, result)

	// The last message should be the guidance message
	lastMsg := result[len(result)-1]
	assert.Equal(t, model.RoleUser, lastMsg.Role)
	assert.Equal(t, "（以上是对话历史摘要。如果有新任务，请告诉我。）", lastMsg.Content,
		"last message should be the guidance message when no pending user")
}

func TestFindPendingUserMessage_EmptySegments(t *testing.T) {
	msg := findPendingUserMessage(nil)
	assert.Nil(t, msg)
}

func TestFindPendingUserMessage_AllIncomplete(t *testing.T) {
	// No complete segments - should return nil (no boundary to search after)
	segments := []*TaskSegment{
		{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "task 1"},
			},
			IsComplete: false,
		},
	}

	msg := findPendingUserMessage(segments)
	assert.Nil(t, msg, "should return nil when no complete segments exist")
}

// ==================== Task 6.8: ensureUserPrompt 不再添加 "继续" ====================

func TestEnsureUserPrompt_AddsGuidanceNotContinue(t *testing.T) {
	// Messages with no user role
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleAssistant, Content: "assistant response"},
	}

	result := ensureUserPrompt(messages)

	require.Len(t, result, 3) // original 2 + 1 appended
	assert.Equal(t, model.RoleUser, result[2].Role)
	assert.Equal(t, "（以上是对话历史摘要。如果有新任务，请告诉我。）", result[2].Content,
		"should append guidance message, not '继续'")
	assert.NotEqual(t, "继续", result[2].Content,
		"should NOT append '继续'")
}

func TestEnsureUserPrompt_WithExistingUser_NoChange(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "user message"},
		{Role: model.RoleAssistant, Content: "assistant response"},
	}

	result := ensureUserPrompt(messages)

	assert.Len(t, result, 3, "should not append when user message exists")
}

func TestEnsureUserPrompt_EmptyMessages(t *testing.T) {
	result := ensureUserPrompt([]model.Message{})

	require.Len(t, result, 1)
	assert.Equal(t, model.RoleUser, result[0].Role)
	assert.Equal(t, "（以上是对话历史摘要。如果有新任务，请告诉我。）", result[0].Content)
}

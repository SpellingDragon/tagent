package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

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
	assert.Equal(t, "请基于以上上下文继续处理。", result[2].Content,
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
	assert.Equal(t, "请基于以上上下文继续处理。", result[0].Content)
}

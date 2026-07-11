package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
)

func TestInjectEventKeyPrefixes_BasicInjection(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}
	refs := []memory.EventReference{
		{EventKey: 100, EventType: "external_input", Role: "user"},
		{EventKey: 101, EventType: "thinking_plan", Role: "assistant"},
	}

	injectEventKeyPrefixes(&messages, refs)

	assert.Equal(t, "[evt_100|external_input] hello", messages[0].Content)
	assert.Equal(t, "[evt_101|thinking_plan] hi", messages[1].Content)
}

func TestInjectEventKeyPrefixes_Idempotent(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "[evt_100|external_input] hello"},
		{Role: model.RoleAssistant, Content: "[evt_101|thinking_plan] hi"},
	}
	refs := []memory.EventReference{
		{EventKey: 200, EventType: "external_input", Role: "user"},
		{EventKey: 201, EventType: "thinking_plan", Role: "assistant"},
	}

	injectEventKeyPrefixes(&messages, refs)

	// Should remain unchanged — no duplicate prefix
	assert.Equal(t, "[evt_100|external_input] hello", messages[0].Content)
	assert.Equal(t, "[evt_101|thinking_plan] hi", messages[1].Content)
}

func TestInjectEventKeyPrefixes_MixedPrefixedAndNot(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "[evt_100|external_input] already prefixed"},
		{Role: model.RoleAssistant, Content: "not prefixed yet"},
	}
	refs := []memory.EventReference{
		{EventKey: 200, EventType: "external_input", Role: "user"},
		{EventKey: 201, EventType: "thinking_plan", Role: "assistant"},
	}

	injectEventKeyPrefixes(&messages, refs)

	// First message already prefixed — unchanged
	assert.Equal(t, "[evt_100|external_input] already prefixed", messages[0].Content)
	// Second message gets ref[0] (key=200) because the idempotent skip
	// happens before refIdx++, so refIdx stays at 0 for the next message.
	assert.Equal(t, "[evt_200|external_input] not prefixed yet", messages[1].Content)
}

func TestInjectEventKeyPrefixes_SkipSystemAndTool(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "user message"},
		{Role: model.RoleTool, Content: "tool result"},
	}
	refs := []memory.EventReference{
		{EventKey: 100, EventType: "external_input", Role: "user"},
	}

	injectEventKeyPrefixes(&messages, refs)

	assert.Equal(t, "system prompt", messages[0].Content)
	assert.Equal(t, "[evt_100|external_input] user message", messages[1].Content)
	assert.Equal(t, "tool result", messages[2].Content)
}

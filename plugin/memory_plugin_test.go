package plugin

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// inferEventType tests
// ============================================================================

func TestInferEventType_UserMessage(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleUser, Content: "hello"}},
			},
		},
	}
	assert.Equal(t, memory.EventTypeExternalInput, inferEventType(evt))
}

func TestInferEventType_AssistantWithToolCalls(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:      model.RoleAssistant,
						Content:   "I'll use a tool",
						ToolCalls: []model.ToolCall{{ID: "call-1"}},
					},
				},
			},
		},
	}
	assert.Equal(t, memory.EventTypeThinkingPlan, inferEventType(evt))
}

func TestInferEventType_AssistantPlain(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleAssistant, Content: "Here is the answer"}},
			},
		},
	}
	assert.Equal(t, memory.EventTypeAgentOutput, inferEventType(evt))
}

func TestInferEventType_ToolResult(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleTool, Content: "tool result"}},
			},
		},
	}
	assert.Equal(t, memory.EventTypeActionCommand, inferEventType(evt))
}

func TestInferEventType_NilResponse(t *testing.T) {
	evt := &event.Event{}
	assert.Equal(t, memory.EventTypeExternalInput, inferEventType(evt))
}

func TestInferEventType_EmptyChoices(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{Choices: []model.Choice{}},
	}
	assert.Equal(t, memory.EventTypeExternalInput, inferEventType(evt))
}

// ============================================================================
// inferEventTypeFromMessage tests
// ============================================================================

func TestInferEventTypeFromMessage_AllRoles(t *testing.T) {
	tests := []struct {
		name     string
		msg      model.Message
		expected string
	}{
		{"user", model.Message{Role: model.RoleUser}, memory.EventTypeExternalInput},
		{"assistant_no_tools", model.Message{Role: model.RoleAssistant}, memory.EventTypeAgentOutput},
		{"assistant_with_tools", model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1"}}}, memory.EventTypeThinkingPlan},
		{"tool", model.Message{Role: model.RoleTool}, memory.EventTypeActionCommand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, inferEventTypeFromMessage(tt.msg))
		})
	}
}

// ============================================================================
// MemoryPlugin OnEvent tests
// ============================================================================

func TestMemoryPlugin_OnEvent_StoresFullEvent(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	evt := &event.Event{
		InvocationID: "inv-1",
		Author:       "tagent",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleUser, Content: "test message"}},
			},
		},
	}

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify event was stored in MemoryStore
	stats := store.GetStats()
	assert.Equal(t, 1, stats.TotalEvents, "one event should be stored")

	// Verify the stored event
	allEvents := store.AllEvents()
	require.Len(t, allEvents, 1)
	assert.Equal(t, memory.EventTypeExternalInput, allEvents[0].EventType)
	assert.Equal(t, "test message", allEvents[0].EventSummary)
	assert.NotZero(t, allEvents[0].EventKey, "Snowflake EventKey should be non-zero")
	assert.NotZero(t, allEvents[0].PartitionID, "PartitionID should be non-zero")
}

func TestMemoryPlugin_OnEvent_WritesStateDelta(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	evt := &event.Event{
		InvocationID: "inv-1",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleAssistant, Content: "response"}},
			},
		},
	}

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
	require.NoError(t, err)

	// Verify StateDelta was written
	require.NotNil(t, result.StateDelta)
	assert.NotEmpty(t, result.StateDelta["event_key"], "event_key should be written to StateDelta")
	assert.Equal(t, memory.EventTypeAgentOutput, string(result.StateDelta["event_type"]))
}

func TestMemoryPlugin_OnEvent_ParentChain(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	// Insert 3 events and verify causal chain
	events := []*event.Event{
		{
			InvocationID: "inv-1",
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "first"}},
				},
			},
		},
		{
			InvocationID: "inv-1",
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleAssistant, Content: "second"}},
				},
			},
		},
		{
			InvocationID: "inv-1",
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "third"}},
				},
			},
		},
	}

	for i, evt := range events {
		_, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
		require.NoError(t, err)
		if i < len(events)-1 {
			time.Sleep(2 * time.Millisecond) // Ensure unique timestamps for EventKeys
		}
	}

	// Verify ParentKey chain
	allEvents := store.AllEvents()
	require.Len(t, allEvents, 3)

	// First event: no parent
	assert.Zero(t, allEvents[0].ParentKey, "first event should have zero ParentKey")

	// Second event: parent = first event's key
	assert.Equal(t, allEvents[0].EventKey, allEvents[1].ParentKey,
		"second event's ParentKey should point to first event")

	// Third event: parent = second event's key
	assert.Equal(t, allEvents[1].EventKey, allEvents[2].ParentKey,
		"third event's ParentKey should point to second event")
}

func TestMemoryPlugin_OnEvent_NilEvent(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "nil event should return nil")
}

// ============================================================================
// extractSummary tests
// ============================================================================

func TestExtractSummary(t *testing.T) {
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleUser, Content: "short message"}},
			},
		},
	}
	assert.Equal(t, "short message", extractSummary(evt))
}

func TestExtractSummary_SpecialEventFullContent(t *testing.T) {
	// external_input (RoleUser and RoleSystem) should use full content, no truncation
	longContent := string(make([]byte, 500)) // 500 zero bytes
	evt := &event.Event{
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleUser, Content: longContent}},
			},
		},
	}
	summary := extractSummary(evt)
	// external_input uses full content (no information loss)
	assert.Equal(t, longContent, summary, "external_input should preserve full content")
	assert.NotContains(t, summary, "...", "external_input should not be truncated")
}

func TestExtractSummary_NilResponse(t *testing.T) {
	evt := &event.Event{}
	assert.Equal(t, "", extractSummary(evt))
}

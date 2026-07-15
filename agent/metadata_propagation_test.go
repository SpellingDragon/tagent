package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestInjectMessageWithMetadata(t *testing.T) {
	// Create a minimal TagentAgent with a persistent bus
	bus := NewEventBus()
	ta := &TagentAgent{
		name:          "test-agent",
		persistentBus: bus,
	}

	// Inject message with metadata
	msg := model.Message{Role: model.RoleUser, Content: "test message"}
	metadata := map[string]string{
		"chat_id":   "user-123",
		"user_name": "Alice",
		"channel":   "wechat",
	}
	ta.InjectMessageWithMetadata("user", msg, metadata)

	// Pull the event from the bus
	events, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)

	// Verify metadata is stored in AgentEvent.Metadata
	evt := events[0]
	assert.Equal(t, "user", evt.Source)
	assert.Equal(t, "user-123", evt.Metadata["chat_id"])
	assert.Equal(t, "Alice", evt.Metadata["user_name"])
	assert.Equal(t, "wechat", evt.Metadata["channel"])
}

func TestInjectMessageWithMetadata_EmptyValues(t *testing.T) {
	bus := NewEventBus()
	ta := &TagentAgent{
		name:          "test-agent",
		persistentBus: bus,
	}

	msg := model.Message{Role: model.RoleUser, Content: "test"}
	metadata := map[string]string{
		"chat_id": "user-456",
		"empty":   "",                     // Should be ignored
		"":        "value-with-empty-key", // Should be ignored
	}
	ta.InjectMessageWithMetadata("user", msg, metadata)

	events, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)

	evt := events[0]
	assert.Equal(t, "user-456", evt.Metadata["chat_id"])
	assert.NotContains(t, evt.Metadata, "empty")
	assert.NotContains(t, evt.Metadata, "")
}

func TestExtractRootMetadata(t *testing.T) {
	tests := []struct {
		name     string
		events   []*AgentEvent
		expected map[string]string
	}{
		{
			name: "single external_input with metadata",
			events: []*AgentEvent{
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id":   "user-123",
						"user_name": "Bob",
					},
				},
			},
			expected: map[string]string{
				"chat_id":   "user-123",
				"user_name": "Bob",
			},
		},
		{
			name: "multiple events - later overrides earlier",
			events: []*AgentEvent{
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-1",
					},
				},
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-2",
					},
				},
			},
			expected: map[string]string{
				"chat_id": "user-2",
			},
		},
		{
			name: "skip agent_output and error sources",
			events: []*AgentEvent{
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-123",
					},
				},
				{
					Type:   "external_input",
					Source: "agent_output",
					Metadata: map[string]any{
						"chat_id": "should-be-ignored",
					},
				},
				{
					Type:   "external_input",
					Source: "error",
					Metadata: map[string]any{
						"chat_id": "also-ignored",
					},
				},
			},
			expected: map[string]string{
				"chat_id": "user-123",
			},
		},
		{
			name: "skip non-external_input events",
			events: []*AgentEvent{
				{
					Type:   "tool_use",
					Source: "agent_loop",
					Metadata: map[string]any{
						"chat_id": "should-be-ignored",
					},
				},
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-789",
					},
				},
			},
			expected: map[string]string{
				"chat_id": "user-789",
			},
		},
		{
			name: "ignore non-string values",
			events: []*AgentEvent{
				{
					Type:   "external_input",
					Source: "user",
					Metadata: map[string]any{
						"chat_id": "user-123",
						"count":   42,   // non-string, should be ignored
						"flag":    true, // non-string, should be ignored
					},
				},
			},
			expected: map[string]string{
				"chat_id": "user-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRootMetadata(tt.events)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOnEventCallback_PropagatesMetadata(t *testing.T) {
	// Create ContextManager with metadata
	cm := &ContextManager{
		currentMetadata: map[string]string{
			"chat_id":   "user-999",
			"user_name": "Charlie",
		},
	}

	// Create projection
	projection := NewSessionProjection()

	// Create a TagentAgent with the ContextManager
	ta := &TagentAgent{
		name:           "test-agent",
		contextManager: cm,
	}

	// Create the callback
	callback := ta.makeOnEventCallback("session-1", projection)

	// Create an event (simulating what the framework would produce)
	evt := &event.Event{
		Author: "test-agent",
		StateDelta: map[string][]byte{
			"event_key": []byte("12345"),
		},
	}

	// Call the callback
	callback(evt)

	// Verify metadata was propagated with "meta_" prefix
	assert.Equal(t, "user-999", string(evt.StateDelta["meta_chat_id"]))
	assert.Equal(t, "Charlie", string(evt.StateDelta["meta_user_name"]))
	// Original StateDelta should be preserved
	assert.Equal(t, "12345", string(evt.StateDelta["event_key"]))
}

func TestOnEventCallback_NoMetadata(t *testing.T) {
	// Create ContextManager without metadata
	cm := &ContextManager{
		currentMetadata: nil,
	}

	projection := NewSessionProjection()
	ta := &TagentAgent{
		name:           "test-agent",
		contextManager: cm,
	}

	callback := ta.makeOnEventCallback("session-1", projection)

	evt := &event.Event{
		Author:     "test-agent",
		StateDelta: map[string][]byte{},
	}

	callback(evt)

	// Should not add any meta_ keys
	for k := range evt.StateDelta {
		assert.NotContains(t, k, "meta_")
	}
}

func TestOnEventCallback_AlreadyPrefixedMetadata(t *testing.T) {
	cm := &ContextManager{
		currentMetadata: map[string]string{
			"meta_chat_id": "already-prefixed",
			"user_name":    "not-prefixed",
		},
	}

	projection := NewSessionProjection()
	ta := &TagentAgent{
		name:           "test-agent",
		contextManager: cm,
	}

	callback := ta.makeOnEventCallback("session-1", projection)

	evt := &event.Event{}
	callback(evt)

	// Should not double-prefix
	assert.Equal(t, "already-prefixed", string(evt.StateDelta["meta_chat_id"]))
	assert.NotContains(t, evt.StateDelta, "meta_meta_chat_id")
	// Should prefix non-prefixed keys
	assert.Equal(t, "not-prefixed", string(evt.StateDelta["meta_user_name"]))
}

package tagent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// asyncMockModel is a mock model that simulates async tool execution
type asyncMockModel struct {
	callCount int
	mu        sync.Mutex
}

func (m *asyncMockModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.callCount++
	count := m.callCount
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)

	// First call: invoke async tool
	if count == 1 {
		ch <- &model.Response{
			Done: true,
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "I'll run the command asynchronously",
						ToolCalls: []model.ToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: model.FunctionDefinitionParam{
									Name:      "action",
									Arguments: []byte(`{"command":"echo hello","async":true}`),
								},
							},
						},
					},
				},
			},
		}
	} else {
		// Second call: respond after async result
		ch <- &model.Response{
			Done: true,
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "Command completed successfully",
					},
				},
			},
		}
	}

	close(ch)
	return ch, nil
}

func (m *asyncMockModel) Info() model.Info {
	return model.Info{Name: "async-mock-model"}
}

// TestAsyncResultRouting verifies that async tool results are correctly
// routed to the original user who triggered the command.
func TestAsyncResultRouting(t *testing.T) {
	// Create a TagentAgent with mock model
	cfg := &agent.TagentConfig{
		Model:     &asyncMockModel{},
		MaxTokens: 4096,
	}
	ta, err := agent.NewTagentAgent(cfg)
	require.NoError(t, err)

	// Start the event loop
	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	// Collect output events
	var mu sync.Mutex
	receivedEvents := make(map[string][]*event.Event) // chat_id -> events

	go func() {
		for evt := range outputCh {
			if evt == nil {
				continue
			}
			chatID := ""
			if evt.StateDelta != nil {
				if cid, ok := evt.StateDelta["meta_chat_id"]; ok {
					chatID = string(cid)
				}
			}
			if chatID != "" {
				mu.Lock()
				receivedEvents[chatID] = append(receivedEvents[chatID], evt)
				mu.Unlock()
			}
		}
	}()

	// User A sends a message that triggers an async command
	userA := "user_A_789"
	ta.InjectMessageWithMetadata("user", model.Message{
		Role:    model.RoleUser,
		Content: "Please run: echo hello",
	}, map[string]string{
		"chat_id":   userA,
		"user_name": "Alice",
	})

	// Wait for the first response (tool invocation)
	time.Sleep(500 * time.Millisecond)

	// Simulate tmux command completion by injecting an action_tool_result event
	// This mimics what TmuxMonitor does when a command completes
	ta.InjectMessageWithMetadata("async_result", model.Message{
		Role:    model.RoleUser,
		Content: "[action_tool_result] Command completed: echo hello\nOutput: hello",
	}, map[string]string{
		"chat_id":   userA, // Same chat_id as the original request
		"user_name": "Alice",
	})

	// Wait for the final response
	time.Sleep(1 * time.Second)

	// Verify user A received all responses
	mu.Lock()
	defer mu.Unlock()

	eventsA := receivedEvents[userA]
	assert.NotEmpty(t, eventsA, "User A should receive at least one event")

	// Verify all events have the correct chat_id
	for _, evt := range eventsA {
		if evt.StateDelta != nil {
			if cid, ok := evt.StateDelta["meta_chat_id"]; ok {
				assert.Equal(t, userA, string(cid), "All events should have chat_id=A")
			}
		}
	}

	// Verify we received at least the final response
	hasFinalResponse := false
	for _, evt := range eventsA {
		if evt.StateDelta != nil {
			if trigger, ok := evt.StateDelta["trigger_source"]; ok {
				if string(trigger) == "async_result" {
					hasFinalResponse = true
					break
				}
			}
		}
	}
	assert.True(t, hasFinalResponse, "Should receive a response triggered by async_result")
}

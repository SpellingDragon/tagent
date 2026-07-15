package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestIsTmuxAsyncPlaceholder verifies that the isTmuxAsyncPlaceholder function
// correctly identifies tmux async placeholder events.
func TestIsTmuxAsyncPlaceholder(t *testing.T) {
	tests := []struct {
		name     string
		evt      *event.Event
		expected bool
	}{
		{
			name:     "nil event",
			evt:      nil,
			expected: false,
		},
		{
			name: "event with nil response",
			evt: &event.Event{
				Response: nil,
			},
			expected: false,
		},
		{
			name: "event with empty choices",
			evt: &event.Event{
				Response: &model.Response{
					Choices: []model.Choice{},
				},
			},
			expected: false,
		},
		{
			name: "tool result with placeholder status",
			evt: &event.Event{
				Response: &model.Response{
					Choices: []model.Choice{
						{
							Message: model.Message{
								Role:    model.RoleTool,
								Content: `{"status":"waiting_async_response","session_id":"test-123"}`,
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "tool result without placeholder status",
			evt: &event.Event{
				Response: &model.Response{
					Choices: []model.Choice{
						{
							Message: model.Message{
								Role:    model.RoleTool,
								Content: `{"status":"completed","output":"hello world"}`,
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "non-tool message with placeholder text",
			evt: &event.Event{
				Response: &model.Response{
					Choices: []model.Choice{
						{
							Message: model.Message{
								Role:    model.RoleAssistant,
								Content: "The status is waiting_async_response",
							},
						},
					},
				},
			},
			expected: false, // Only tool results are checked
		},
		{
			name: "multiple choices with one tool placeholder",
			evt: &event.Event{
				Response: &model.Response{
					Choices: []model.Choice{
						{
							Message: model.Message{
								Role:    model.RoleAssistant,
								Content: "I will run the command",
							},
						},
						{
							Message: model.Message{
								Role:    model.RoleTool,
								Content: `{"status":"waiting_async_response","session_id":"abc"}`,
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "tool result with partial match",
			evt: &event.Event{
				Response: &model.Response{
					Choices: []model.Choice{
						{
							Message: model.Message{
								Role:    model.RoleTool,
								Content: `status is waiting_async_response for session`,
							},
						},
					},
				},
			},
			expected: true, // strings.Contains matches
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTmuxAsyncPlaceholder(tt.evt)
			assert.Equal(t, tt.expected, result)
		})
	}
}

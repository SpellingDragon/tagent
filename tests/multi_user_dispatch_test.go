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

// multiUserMockModel is a simple mock model for testing
type multiUserMockModel struct{}

func (m *multiUserMockModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Done: true,
		Choices: []model.Choice{
			{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "Mock response",
				},
			},
		},
	}
	close(ch)
	return ch, nil
}

func (m *multiUserMockModel) Info() model.Info {
	return model.Info{Name: "mock-model"}
}

// TestMultiUserDispatch verifies that concurrent messages from different users
// are correctly routed to their respective chat_ids without cross-contamination.
func TestMultiUserDispatch(t *testing.T) {
	// Create a TagentAgent with mock dependencies
	cfg := &agent.TagentConfig{
		Model:     &multiUserMockModel{},
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

	// Simulate two users sending messages almost simultaneously (100ms apart)
	userA := "user_A_123"
	userB := "user_B_456"

	// User A sends message
	ta.InjectMessageWithMetadata("user", model.Message{
		Role:    model.RoleUser,
		Content: "Hello from User A",
	}, map[string]string{
		"chat_id":   userA,
		"user_name": "Alice",
	})

	time.Sleep(100 * time.Millisecond)

	// User B sends message
	ta.InjectMessageWithMetadata("user", model.Message{
		Role:    model.RoleUser,
		Content: "Hello from User B",
	}, map[string]string{
		"chat_id":   userB,
		"user_name": "Bob",
	})

	// Wait for both responses
	time.Sleep(2 * time.Second)

	// Verify both users received their responses
	mu.Lock()
	defer mu.Unlock()

	eventsA := receivedEvents[userA]
	eventsB := receivedEvents[userB]

	assert.NotEmpty(t, eventsA, "User A should receive at least one event")
	assert.NotEmpty(t, eventsB, "User B should receive at least one event")

	// Verify no cross-contamination: User A's events should not contain User B's chat_id
	for _, evt := range eventsA {
		if evt.StateDelta != nil {
			if cid, ok := evt.StateDelta["meta_chat_id"]; ok {
				assert.Equal(t, userA, string(cid), "User A should only receive events for chat_id=A")
			}
		}
	}

	for _, evt := range eventsB {
		if evt.StateDelta != nil {
			if cid, ok := evt.StateDelta["meta_chat_id"]; ok {
				assert.Equal(t, userB, string(cid), "User B should only receive events for chat_id=B")
			}
		}
	}
}

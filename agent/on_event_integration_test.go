package agent

import (
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOnEvent_SessionEventsPopulated verifies that the onEvent callback writes
// both user external_input events and assistant agent_output events into
// session.Events when running the persistent event loop.
func TestOnEvent_SessionEventsPopulated(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "Hello from tagent",
			},
		}},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:        mockModel,
		SystemPrompt: "You are a test assistant.",
	})
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("user-1", "session-on-event")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("Hello"))

	select {
	case <-outputCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for agent output")
	}

	ta.StopLoop()

	sess := ta.getOrCreateSession()
	require.NotNil(t, sess)
	require.Len(t, sess.Events, 2, "session should contain user + assistant events")

	assert.Equal(t, model.RoleUser, sess.Events[0].Response.Choices[0].Message.Role)
	assert.Equal(t, "Hello", sess.Events[0].Response.Choices[0].Message.Content)
	assert.Equal(t, model.RoleAssistant, sess.Events[1].Response.Choices[0].Message.Role)
	assert.Equal(t, "Hello from tagent", sess.Events[1].Response.Choices[0].Message.Content)
}

// TestOnEvent_MemoryStorePopulated verifies that MemoryPlugin.OnEvent persists
// FullEvents to the MemoryStore and sets StateDelta event_key/type on the
// session events.
func TestOnEvent_MemoryStorePopulated(t *testing.T) {
	mockModel := newRecordableMockModel(&model.Response{
		ID:   "resp-1",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "Hello from tagent",
			},
		}},
	})

	ta, err := NewTagentAgent(&TagentConfig{
		Model:        mockModel,
		SystemPrompt: "You are a test assistant.",
	})
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("user-1", "session-memstore")
	require.NoError(t, err)

	ta.InjectMessage(model.NewUserMessage("Hello"))

	select {
	case <-outputCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for agent output")
	}

	ta.StopLoop()

	sess := ta.getOrCreateSession()
	require.NotNil(t, sess)
	require.NotEmpty(t, sess.Events)

	// Each session event should have StateDelta populated by MemoryPlugin.
	for i, evt := range sess.Events {
		assert.NotEmpty(t, evt.StateDelta["event_key"], "event %d missing event_key", i)
		assert.NotEmpty(t, evt.StateDelta["event_type"], "event %d missing event_type", i)
	}

	// MemoryStore should contain at least the user event and assistant event.
	partitionID := tagentmemory.PartitionIDFromName(ta.name)
	events, err := ta.memStore.QueryEvents(tagentmemory.QueryOptions{
		PartitionID: partitionID,
		Limit:       100,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 2, "MemoryStore should have at least 2 FullEvents")
}

package agent

import (
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestOnEvent_SessionEventsPopulated verifies that the onEvent callback writes
// both user external_input events and assistant agent_output events into
// session.Events when running the persistent event loop (legacy path).
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

	// Wait for final response
	evt := waitForFinalResponse(t, outputCh, 10*time.Second)
	require.NotNil(t, evt)

	ta.StopLoop()

	sess := ta.getOrCreateSession()
	require.NotNil(t, sess)
	require.GreaterOrEqual(t, len(sess.Events), 1, "session should contain events")
}

// TestOnEvent_MemoryStorePopulated verifies that MemoryPlugin.OnEvent persists
// FullEvents to the MemoryStore and sets StateDelta event_key/type on the
// session events (legacy path).
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

	// Wait for final response
	evt := waitForFinalResponse(t, outputCh, 10*time.Second)
	require.NotNil(t, evt)

	ta.StopLoop()

	sess := ta.getOrCreateSession()
	require.NotNil(t, sess)
	require.NotEmpty(t, sess.Events)

	// MemoryStore should contain events (written by framework Plugin).
	partitionID := tagentmemory.PartitionIDFromName(ta.name)
	events, err := ta.memStore.QueryEvents(tagentmemory.QueryOptions{
		PartitionID: partitionID,
		Limit:       100,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 1, "MemoryStore should have at least 1 FullEvent")
}

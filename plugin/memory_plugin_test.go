package plugin

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/memory"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	stats := store.GetStats()
	assert.Equal(t, 1, stats.TotalEvents, "one event should be stored")

	allEvents := store.AllEvents()
	require.Len(t, allEvents, 1)
	assert.Equal(t, tagentevent.TypeExternalInput, allEvents[0].EventType)
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

	require.NotNil(t, result.StateDelta)
	assert.NotEmpty(t, result.StateDelta["event_key"], "event_key should be written to StateDelta")
	assert.Equal(t, tagentevent.TypeAgentOutput, string(result.StateDelta["event_type"]))
}

func TestMemoryPlugin_OnEvent_ParentChain(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	// Set distinct timestamps to ensure AllEvents() returns events in insertion order.
	// AllEvents() sorts by Timestamp; zero-value timestamps cause arbitrary ordering.
	now := time.Now()
	events := []*event.Event{
		{
			InvocationID: "inv-1",
			Timestamp:    now,
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "first"}},
				},
			},
		},
		{
			InvocationID: "inv-1",
			Timestamp:    now.Add(10 * time.Millisecond),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleAssistant, Content: "second"}},
				},
			},
		},
		{
			InvocationID: "inv-1",
			Timestamp:    now.Add(20 * time.Millisecond),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleUser, Content: "third"}},
				},
			},
		},
	}

	for _, evt := range events {
		_, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
		require.NoError(t, err)
	}

	allEvents := store.AllEvents()
	require.Len(t, allEvents, 3)

	// Use GetParent instead of direct field access (content-relation separation)
	parent0, _ := store.GetParent(allEvents[0].EventKey)
	assert.Zero(t, parent0, "first event should have zero ParentKey")

	parent1, _ := store.GetParent(allEvents[1].EventKey)
	assert.Equal(t, allEvents[0].EventKey, parent1)

	parent2, _ := store.GetParent(allEvents[2].EventKey)
	assert.Equal(t, allEvents[1].EventKey, parent2)
}

func TestMemoryPlugin_OnEvent_NilEvent(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	result, err := p.onEvent(context.Background(), &agent.Invocation{}, nil)
	require.NoError(t, err)
	assert.Nil(t, result, "nil event should return nil")
}

func TestMemoryPlugin_OnEvent_SkipsEventsWithoutChoices(t *testing.T) {
	store := memory.NewInMemoryStore()
	p := NewMemoryPlugin(store)

	cases := []*event.Event{
		{InvocationID: "inv-1", Response: nil},
		{InvocationID: "inv-1", Response: &model.Response{Done: true, Choices: nil}},
		{InvocationID: "inv-1", Response: &model.Response{Done: true, Choices: []model.Choice{}}},
	}

	for _, evt := range cases {
		result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Nil(t, result.StateDelta, "sync events must not get StateDelta")
	}

	stats := store.GetStats()
	assert.Zero(t, stats.TotalEvents, "sync events should not be persisted")
}

// minimalStore wraps InMemoryStore but does NOT implement RelationStoreProvider.
// Used to test graceful fallback in onEvent when store lacks relation support.
type minimalStore struct {
	memory.MemoryStore
}

func TestMemoryPlugin_OnEvent_NoRelationStoreProvider(t *testing.T) {
	inner := memory.NewInMemoryStore()
	store := &minimalStore{MemoryStore: inner}
	p := NewMemoryPlugin(store)

	evt := &event.Event{
		InvocationID: "inv-1",
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: model.RoleUser, Content: "test"}},
			},
		},
	}

	// Should succeed even though store doesn't implement RelationStoreProvider
	result, err := p.onEvent(context.Background(), &agent.Invocation{}, evt)
	require.NoError(t, err)
	require.NotNil(t, result)

	stats := inner.GetStats()
	assert.Equal(t, 1, stats.TotalEvents, "event should still be stored")
}

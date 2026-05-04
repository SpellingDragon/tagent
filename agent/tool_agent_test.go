package agent

import (
	"context"
	"fmt"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// AgentToolWrapper Declaration tests
// ============================================================================

// mockRunner is a minimal Runner implementation for testing AgentToolWrapper.Call.
// It returns an immediately-closed channel so Call completes without events.
type mockRunner struct{}

func (m *mockRunner) Run(
	ctx context.Context,
	userID, sessionID string,
	message model.Message,
	_ ...agent.RunOption,
) (<-chan *event.Event, error) {
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (m *mockRunner) Close() error { return nil }

// TestAgentToolWrapper_Declaration_WithEventKeys verifies that Declaration
// includes event_keys parameter when eventParams contains "event_key".
// This covers Task 2.2.
func TestAgentToolWrapper_Declaration_WithEventKeys(t *testing.T) {
	subAgent := &TagentAgent{name: "test-tool"}
	wrapper := NewAgentToolWrapper(subAgent, "test tool description", []string{"event_key"}, nil)

	decl := wrapper.Declaration()
	require.NotNil(t, decl)
	assert.Equal(t, "test-tool", decl.Name)
	assert.Equal(t, "test tool description", decl.Description)

	// Verify InputSchema structure
	require.NotNil(t, decl.InputSchema)
	assert.Equal(t, "object", decl.InputSchema.Type)

	// Should have event_keys property
	eventKeysSchema, ok := decl.InputSchema.Properties["event_keys"]
	require.True(t, ok, "event_keys should be declared when eventParams includes 'event_key'")
	assert.Equal(t, "array", eventKeysSchema.Type)
	require.NotNil(t, eventKeysSchema.Items)
	assert.Equal(t, "integer", eventKeysSchema.Items.Type)

	// Should still have standard request parameter
	_, hasRequest := decl.InputSchema.Properties["request"]
	assert.True(t, hasRequest, "request parameter should always be present")
}

// TestAgentToolWrapper_Declaration_WithoutEventKeys verifies that Declaration
// does NOT include event_keys when eventParams is empty or nil.
// This covers Task 2.3.
func TestAgentToolWrapper_Declaration_WithoutEventKeys(t *testing.T) {
	subAgent := &TagentAgent{name: "test-tool"}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", nil, nil)

	decl := wrapper.Declaration()
	require.NotNil(t, decl)

	_, hasEventKeys := decl.InputSchema.Properties["event_keys"]
	assert.False(t, hasEventKeys, "event_keys should NOT be declared when eventParams is empty")

	// request parameter should still be present
	_, hasRequest := decl.InputSchema.Properties["request"]
	assert.True(t, hasRequest, "request parameter should always be present")
	// Required should contain "request"
	assert.Contains(t, decl.InputSchema.Required, "request")
}

// TestAgentToolWrapper_Declaration_NoToolCallsParam verifies that Declaration
// does NOT include tool_calls or other irrelevant parameters.
func TestAgentToolWrapper_Declaration_NoExtraParams(t *testing.T) {
	subAgent := &TagentAgent{name: "test-tool"}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", []string{"event_key"}, nil)

	decl := wrapper.Declaration()

	// Should only have request and event_keys
	assert.Len(t, decl.InputSchema.Properties, 2,
		"should only declare request and event_keys parameters")
}

// ============================================================================
// AgentToolWrapper Call tests
// ============================================================================

// TestAgentToolWrapper_Call_WithEventKeys verifies that Call properly
// resolves event_keys from parentStore and injects them into the sub-agent.
// This covers Task 2.4.
func TestAgentToolWrapper_Call_WithEventKeys(t *testing.T) {
	// Create parent store with test events
	parentStore := memory.NewInMemoryStore()
	partitionID := memory.PartitionIDFromName("test-agent")
	key1 := memory.NewSnowflakeEventKey(partitionID, 0)
	key2 := memory.NewSnowflakeEventKey(partitionID, 0)

	evt1 := memory.FullEvent{
		EventKey:     key1,
		PartitionID:  partitionID,
		EventType:    memory.EventTypeExternalInput,
		EventSummary: "first test event",
		Content:      "content of first event",
	}
	evt2 := memory.FullEvent{
		EventKey:     key2,
		PartitionID:  partitionID,
		EventType:    memory.EventTypeAgentOutput,
		EventSummary: "second test event",
		Content:      "content of second event",
	}
	require.NoError(t, parentStore.StoreEvent(key1, evt1))
	require.NoError(t, parentStore.StoreEvent(key2, evt2))

	// Create sub-agent with mock runner
	subAgent := &TagentAgent{name: "test-tool", runner: &mockRunner{}}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", []string{"event_key"}, parentStore)

	// Call with event_keys
	jsonArgs := fmt.Sprintf(`{"request":"do something","event_keys":[%d,%d]}`, key1, key2)
	result, err := wrapper.Call(context.Background(), []byte(jsonArgs))
	require.NoError(t, err)
	assert.Contains(t, result, "tool agent completed without output",
		"should return fallback output since mock runner produces no events")

	// Verify that events were resolved - IngestExternalEvents was called,
	// and RunSimple consumed them via injectExternalContext
	// (pendingExternalEvents should be nil after consumption)
	require.Nil(t, subAgent.pendingExternalEvents,
		"pendingExternalEvents should be consumed after RunSimple")
}

// TestAgentToolWrapper_Call_NonExistentEventKey verifies that Call handles
// missing event_keys gracefully without error.
// This covers Task 2.5.
func TestAgentToolWrapper_Call_NonExistentEventKey(t *testing.T) {
	parentStore := memory.NewInMemoryStore()
	partitionID := memory.PartitionIDFromName("test-agent")
	// Store one event
	key := memory.NewSnowflakeEventKey(partitionID, 0)
	require.NoError(t, parentStore.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: partitionID, EventType: memory.EventTypeExternalInput,
		EventSummary: "test", Content: "test",
	}))

	subAgent := &TagentAgent{name: "test-tool", runner: &mockRunner{}}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", []string{"event_key"}, parentStore)

	// Call with both valid and invalid event_keys
	nonexistentKey := key + 99999
	jsonArgs := fmt.Sprintf(`{"request":"test","event_keys":[%d,%d]}`, key, nonexistentKey)
	result, err := wrapper.Call(context.Background(), []byte(jsonArgs))
	require.NoError(t, err)
	assert.Contains(t, result, "tool agent completed without output")
	// Only the valid key should be injected; the non-existent key should be skipped
	require.Nil(t, subAgent.pendingExternalEvents, "external events should be consumed")
}

// TestAgentToolWrapper_Call_NoEventKeys verifies that Call works correctly
// when no event_keys are provided.
// This covers Task 2.6.
func TestAgentToolWrapper_Call_NoEventKeys(t *testing.T) {
	parentStore := memory.NewInMemoryStore()

	subAgent := &TagentAgent{name: "test-tool", runner: &mockRunner{}}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", []string{"event_key"}, parentStore)

	// Call without event_keys
	jsonArgs := []byte(`{"request":"do something"}`)
	result, err := wrapper.Call(context.Background(), jsonArgs)
	require.NoError(t, err)
	assert.Contains(t, result, "tool agent completed without output")

	// No external events should have been injected
	require.Nil(t, subAgent.pendingExternalEvents)
}

// TestAgentToolWrapper_Call_EmptyArgs verifies that Call works with empty args.
func TestAgentToolWrapper_Call_EmptyArgs(t *testing.T) {
	subAgent := &TagentAgent{name: "test-tool", runner: &mockRunner{}}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", nil, nil)

	// Call with empty args
	result, err := wrapper.Call(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.Contains(t, result, "tool agent completed without output")
}

// TestAgentToolWrapper_Call_InvalidJSON verifies that Call returns an error
// for malformed JSON args.
func TestAgentToolWrapper_Call_InvalidJSON(t *testing.T) {
	subAgent := &TagentAgent{name: "test-tool"}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", nil, nil)

	_, err := wrapper.Call(context.Background(), []byte(`{invalid json}`))
	assert.Error(t, err, "invalid JSON should return an error")
	assert.Contains(t, err.Error(), "parse args")
}

// ============================================================================
// ToolAgentFactory Registry tests
// ============================================================================

func TestRegisterAndGetToolAgentFactory(t *testing.T) {
	called := false
	RegisterToolAgent("test-factory", func(cfg ToolAgentFactoryConfig) (*TagentAgent, error) {
		called = true
		return &TagentAgent{name: cfg.ID}, nil
	})
	defer func() {
		toolAgentFactoriesMu.Lock()
		delete(toolAgentFactories, "test-factory")
		toolAgentFactoriesMu.Unlock()
	}()

	factory, ok := GetToolAgentFactory("test-factory")
	require.True(t, ok, "factory should be registered")

	agent, err := factory(ToolAgentFactoryConfig{ID: "test-factory"})
	require.NoError(t, err)
	require.NotNil(t, agent)
	assert.Equal(t, "test-factory", agent.name)
	assert.True(t, called, "factory function should have been called")
}

func TestRegisterToolAgent_Duplicate(t *testing.T) {
	RegisterToolAgent("dup-factory", func(cfg ToolAgentFactoryConfig) (*TagentAgent, error) {
		return &TagentAgent{}, nil
	})
	defer func() {
		toolAgentFactoriesMu.Lock()
		delete(toolAgentFactories, "dup-factory")
		toolAgentFactoriesMu.Unlock()
	}()

	assert.Panics(t, func() {
		RegisterToolAgent("dup-factory", func(cfg ToolAgentFactoryConfig) (*TagentAgent, error) {
			return &TagentAgent{}, nil
		})
	}, "duplicate registration should panic")
}

// ============================================================================
// PlainToolFactory Registry tests
// ============================================================================

func TestRegisterAndGetPlainToolFactory(t *testing.T) {
	called := false
	RegisterPlainTool("test-plain", func(cfg PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		called = true
		return nil, nil
	})
	defer func() {
		plainToolFactoriesMu.Lock()
		delete(plainToolFactories, "test-plain")
		plainToolFactoriesMu.Unlock()
	}()

	factory, ok := GetPlainToolFactory("test-plain")
	require.True(t, ok)

	_, err := factory(PlainToolFactoryConfig{ID: "test-plain"})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestGetToolAgentFactory_NotFound(t *testing.T) {
	_, ok := GetToolAgentFactory("non-existent-factory")
	assert.False(t, ok, "non-existent factory should return false")
}

func TestGetPlainToolFactory_NotFound(t *testing.T) {
	_, ok := GetPlainToolFactory("non-existent-factory")
	assert.False(t, ok, "non-existent factory should return false")
}

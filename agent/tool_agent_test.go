package agent

import (
	"context"
	"fmt"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	tagentevent "github.com/SpellingDragon/tagent/event"
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
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "first test event",
		Content:      "content of first event",
	}
	evt2 := memory.FullEvent{
		EventKey:     key2,
		PartitionID:  partitionID,
		EventType:    tagentevent.TypeAgentOutput,
		EventSummary: "second test event",
		Content:      "content of second event",
	}
	require.NoError(t, parentStore.StoreEvent(key1, evt1))
	require.NoError(t, parentStore.StoreEvent(key2, evt2))

	// Create sub-agent with mock runner and required fields.
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := NewDefaultTokenCounter()
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)
	subAgent := &TagentAgent{
		name:         "test-tool",
		runner:       &mockRunner{},
		preprocessor: preproc,
		config:       &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000, Model: &mockModel{}},
	}
	wrapper := NewAgentToolWrapper(subAgent, "test tool", []string{"event_key"}, parentStore)

	// Call with event_keys
	jsonArgs := fmt.Sprintf(`{"request":"do something","event_keys":[%d,%d]}`, key1, key2)
	result, err := wrapper.Call(context.Background(), []byte(jsonArgs))
	require.NoError(t, err)
	// In the event-driven architecture, the sub-agent returns the mock
	// model's response via the EventBus.
	assert.Contains(t, result, "mock response",
		"should return the model's response from the event-driven loop")

	// Verify that events were resolved - IngestExternalEvents was called,
	// and Run consumed them via injectExternalContext
	// (pendingExternalEvents should be nil after consumption)
	require.Nil(t, subAgent.pendingExternalEvents,
		"pendingExternalEvents should be consumed after Run")
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
		EventKey: key, PartitionID: partitionID, EventType: tagentevent.TypeExternalInput,
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

// ============================================================================
// ExternalContextEntry serialization tests
// ============================================================================

// TestSerializeExternalContext verifies that serializeExternalContext produces
// compact JSON with only event_key, event_type, event_summary — no Content.
func TestSerializeExternalContext(t *testing.T) {
	events := []memory.FullEvent{
		{
			EventKey:     12345,
			EventType:    tagentevent.TypeExternalInput,
			EventSummary: "user asked about deployment",
			Content:      "this is the full content that should NOT be serialized",
		},
		{
			EventKey:     67890,
			EventType:    tagentevent.TypeAgentOutput,
			EventSummary: "agent responded with plan",
			Content:      "more full content that should NOT be serialized",
		},
	}

	data, err := serializeExternalContext(events)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify the JSON does not contain "content"
	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "content", "Content should not be serialized")
	assert.Contains(t, jsonStr, "event_key")
	assert.Contains(t, jsonStr, "event_type")
	assert.Contains(t, jsonStr, "event_summary")
}

// TestDeserializeExternalContext verifies that deserializeExternalContext
// correctly reconstructs FullEvents with empty Content.
func TestDeserializeExternalContext(t *testing.T) {
	original := []memory.FullEvent{
		{
			EventKey:     111,
			EventType:    tagentevent.TypeExternalInput,
			EventSummary: "first event",
			Content:      "original content",
		},
		{
			EventKey:     222,
			EventType:    tagentevent.TypeAgentOutput,
			EventSummary: "second event",
			Content:      "more content",
		},
	}

	// Serialize → deserialize round-trip
	data, err := serializeExternalContext(original)
	require.NoError(t, err)

	restored, err := deserializeExternalContext(data)
	require.NoError(t, err)
	require.Len(t, restored, 2)

	// Verify fields are preserved
	assert.Equal(t, int64(111), restored[0].EventKey)
	assert.Equal(t, tagentevent.TypeExternalInput, restored[0].EventType)
	assert.Equal(t, "first event", restored[0].EventSummary)
	// Content should be empty
	assert.Empty(t, restored[0].Content, "Content should be empty after deserialization")

	assert.Equal(t, int64(222), restored[1].EventKey)
	assert.Equal(t, tagentevent.TypeAgentOutput, restored[1].EventType)
	assert.Equal(t, "second event", restored[1].EventSummary)
	assert.Empty(t, restored[1].Content)
}

// TestSerializeDeserialize_Empty verifies that empty event lists serialize
// and deserialize correctly.
func TestSerializeDeserialize_Empty(t *testing.T) {
	data, err := serializeExternalContext(nil)
	require.NoError(t, err)
	// make([]ExternalContextEntry, 0) produces an empty JSON array, not null
	assert.Equal(t, "[]", string(data), "empty slice should serialize to []")

	restored, err := deserializeExternalContext(data)
	require.NoError(t, err)
	assert.Empty(t, restored)
}

// ============================================================================
// TagentAgent.Run RuntimeState tests
// ============================================================================

// TestTagentAgent_Run_RuntimeStateContext verifies that Run reads
// external_context from RuntimeState and injects it into the message.
func TestTagentAgent_Run_RuntimeStateContext(t *testing.T) {
	// Create a TagentAgent with mock runner
	ta := &TagentAgent{
		name:   "test-agent",
		runner: &mockRunner{},
	}

	// Serialize external context
	events := []memory.FullEvent{
		{
			EventKey:     999,
			EventType:    tagentevent.TypeExternalInput,
			EventSummary: "context from runtime state",
		},
	}
	serialized, err := serializeExternalContext(events)
	require.NoError(t, err)

	// Create Invocation with RuntimeState
	runOpts := agent.RunOptions{
		RuntimeState: map[string]any{
			ExternalContextKey: serialized,
		},
	}
	inv := agent.NewInvocation(
		agent.WithInvocationMessage(model.NewUserMessage("hello")),
		agent.WithInvocationRunOptions(runOpts),
	)

	// Run — should read RuntimeState, inject context, then run
	eventCh, err := ta.Run(context.Background(), inv)
	require.NoError(t, err)

	// Drain the channel (mock runner closes immediately)
	for range eventCh {
	}

	// Verify pendingExternalEvents were consumed
	assert.Nil(t, ta.pendingExternalEvents,
		"pendingExternalEvents should be consumed after Run")
}

// TestTagentAgent_Run_NoRuntimeState verifies that Run works correctly
// when RuntimeState has no external_context.
func TestTagentAgent_Run_NoRuntimeState(t *testing.T) {
	ta := &TagentAgent{
		name:   "test-agent",
		runner: &mockRunner{},
	}

	inv := agent.NewInvocation(
		agent.WithInvocationMessage(model.NewUserMessage("hello")),
	)

	eventCh, err := ta.Run(context.Background(), inv)
	require.NoError(t, err)
	for range eventCh {
	}

	// No external events should have been injected
	assert.Nil(t, ta.pendingExternalEvents)
}

// ============================================================================
// AgentToolWrapper with agent.Agent interface tests
// ============================================================================

// mockAgent is a minimal agent.Agent implementation for testing
// AgentToolWrapper with the unified interface.
type mockAgent struct {
	name    string
	lastInv *agent.Invocation
	runErr  error
}

func (m *mockAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	m.lastInv = inv
	if m.runErr != nil {
		return nil, m.runErr
	}
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (m *mockAgent) Tools() []trpctool.Tool { return nil }

func (m *mockAgent) Info() agent.Info {
	return agent.Info{Name: m.name, Description: "mock agent"}
}

func (m *mockAgent) SubAgents() []agent.Agent { return nil }

func (m *mockAgent) FindSubAgent(name string) agent.Agent { return nil }

// TestAgentToolWrapper_GenericAgentInterface verifies that AgentToolWrapper
// works with any agent.Agent implementation (not just *TagentAgent).
func TestAgentToolWrapper_GenericAgentInterface(t *testing.T) {
	mockAg := &mockAgent{name: "mock-remote"}
	wrapper := NewAgentToolWrapper(mockAg, "mock tool", nil, nil)

	// Declaration should use agent.Info().Name
	decl := wrapper.Declaration()
	assert.Equal(t, "mock-remote", decl.Name)

	// Call should invoke agent.Run
	result, err := wrapper.Call(context.Background(), []byte(`{"request":"test"}`))
	require.NoError(t, err)
	assert.Contains(t, result, "tool agent completed without output")

	// Verify Run was called with an Invocation
	require.NotNil(t, mockAg.lastInv, "agent.Run should have been called")
}

// TestAgentToolWrapper_RuntimeStatePassThrough verifies that AgentToolWrapper
// passes external context via RuntimeState to the wrapped agent.
func TestAgentToolWrapper_RuntimeStatePassThrough(t *testing.T) {
	// Create parent store with test events
	parentStore := memory.NewInMemoryStore()
	partitionID := memory.PartitionIDFromName("test-agent")
	key1 := memory.NewSnowflakeEventKey(partitionID, 0)

	evt1 := memory.FullEvent{
		EventKey:     key1,
		PartitionID:  partitionID,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "test context for runtime state",
		Content:      "full content here",
	}
	require.NoError(t, parentStore.StoreEvent(key1, evt1))

	// Use mockAgent to capture the Invocation
	mockAg := &mockAgent{name: "mock-sub"}
	wrapper := NewAgentToolWrapper(mockAg, "test tool", []string{"event_key"}, parentStore)

	// Call with event_keys
	jsonArgs := fmt.Sprintf(`{"request":"do something","event_keys":[%d]}`, key1)
	_, err := wrapper.Call(context.Background(), []byte(jsonArgs))
	require.NoError(t, err)

	// Verify the Invocation was created with RuntimeState containing external_context
	require.NotNil(t, mockAg.lastInv, "Run should have been called")
	require.NotNil(t, mockAg.lastInv.RunOptions.RuntimeState, "RuntimeState should be set")

	raw, ok := mockAg.lastInv.RunOptions.RuntimeState[ExternalContextKey]
	assert.True(t, ok, "external_context should be in RuntimeState")

	// Verify the content can be deserialized
	var data []byte
	switch v := raw.(type) {
	case []byte:
		data = v
	default:
		// json.RawMessage is []byte underneath
		data = []byte(fmt.Sprintf("%s", raw))
	}
	restored, err := deserializeExternalContext(data)
	require.NoError(t, err)
	require.Len(t, restored, 1)
	assert.Equal(t, key1, restored[0].EventKey)
	assert.Equal(t, "test context for runtime state", restored[0].EventSummary)
	assert.Empty(t, restored[0].Content, "Content should not be serialized")
}

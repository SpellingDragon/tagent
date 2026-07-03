package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLoopModel is a model that returns a scripted final response.
type mockLoopModel struct {
	mu        sync.Mutex
	callCount int
}

func (m *mockLoopModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: "mock response"},
		}},
	}
	close(ch)
	return ch, nil
}

func (m *mockLoopModel) Info() model.Info {
	return model.Info{Name: "mock-loop-model"}
}

func (m *mockLoopModel) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// newLoopTestAgent creates a TagentAgent configured for loop tests.
func newLoopTestAgent(t *testing.T) *TagentAgent {
	t.Helper()
	bus := NewEventBus()
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := NewDefaultTokenCounter()
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)
	outputCh := make(chan *event.Event, 100)
	agentLoop := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        &mockLoopModel{},
		OutputCh:     outputCh,
		Name:         "test-loop",
		MaxToolIters: 10,
	})
	return &TagentAgent{
		persistentBus: bus,
		activeBus:     bus,
		agentLoop:     agentLoop,
		preprocessor:  preproc,
		config:        &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000},
		outputCh:      outputCh,
		name:          "test-loop",
	}
}

// ============================================================================

func TestStartLoop_InjectMessage_ReceivesEvents(t *testing.T) {
	ta := newLoopTestAgent(t)

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)
	assert.True(t, ta.loopActive.Load())

	// Inject a message — it should trigger the AgentLoop.
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "hello"})

	// The AgentLoop should process the message and emit an agent_output to outputCh.
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
		require.NotNil(t, evt.Response)
		require.Len(t, evt.Response.Choices, 1)
		assert.Equal(t, "mock response", evt.Response.Choices[0].Message.Content)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for agent_output on outputCh")
	}

	ta.StopLoop()
	assert.False(t, ta.loopActive.Load())
}

func TestStartLoop_MultipleInjects(t *testing.T) {
	ta := newLoopTestAgent(t)

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)
	defer ta.StopLoop()

	// Inject multiple messages rapidly.
	for i := 0; i < 3; i++ {
		ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "msg"})
	}

	// Should receive at least one response (the AgentLoop may batch them).
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for agent_output")
	}
}

func TestStopLoop_Idempotent(t *testing.T) {
	ta := newLoopTestAgent(t)

	_, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	// StopLoop should be safe to call multiple times.
	ta.StopLoop()
	ta.StopLoop() // no-op
	assert.False(t, ta.loopActive.Load())
}

func TestStartLoop_DuplicateCall(t *testing.T) {
	ta := newLoopTestAgent(t)

	ch1, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	ch2, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	// Should return the same outputCh.
	assert.Equal(t, ch1, ch2)

	ta.StopLoop()
}

func TestInjectMessage_LoopNotStarted(t *testing.T) {
	ta := newLoopTestAgent(t)

	// InjectMessage without StartLoop should drop the message silently.
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "hello"})
	// No panic, no error — message is just dropped.
}

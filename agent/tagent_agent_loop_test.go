package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLoopRunner implements runner.Runner for Persistent Event Loop tests.
// Each Run() call returns a channel that emits a final response event then closes.
type mockLoopRunner struct {
	mu       sync.Mutex
	calls    int
	messages []model.Message
	closed   bool
}

func (m *mockLoopRunner) Run(ctx context.Context, userID string, sessionID string, message model.Message, opts ...agent.RunOption) (<-chan *event.Event, error) {
	m.mu.Lock()
	m.calls++
	m.messages = append(m.messages, message)
	m.mu.Unlock()

	ch := make(chan *event.Event, 1)
	// Emit a final response event so the Flow would break
	rsp := &model.Response{
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: fmt.Sprintf("response to: %s", message.Content)}}},
	}
	evt := event.NewResponseEvent("test-inv", "test-author", rsp)
	ch <- evt
	close(ch)
	return ch, nil
}

func (m *mockLoopRunner) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockLoopRunner) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockLoopRunner) getMessages() []model.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]model.Message, len(m.messages))
	copy(cp, m.messages)
	return cp
}

// ============================================================================

func TestStartLoop_InjectMessage_ReceivesEvents(t *testing.T) {
	mockR := &mockLoopRunner{}
	ta := &TagentAgent{
		runner: mockR,
	}

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)
	assert.True(t, ta.loopActive.Load())

	// First batch: inject one message
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "hello"})

	// Wait for first response
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
		assert.True(t, evt.IsFinalResponse(), "should receive final response event")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first batch event")
	}

	// Second batch: inject another message
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "world"})

	// Wait for second response
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
		assert.True(t, evt.IsFinalResponse(), "should receive final response event")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for second batch event")
	}

	// Stop loop
	ta.StopLoop()
	assert.False(t, ta.loopActive.Load())

	// outputCh should be closed
	select {
	case _, ok := <-outputCh:
		assert.False(t, ok, "outputCh should be closed after StopLoop")
	case <-time.After(2 * time.Second):
		t.Fatal("outputCh should be closed")
	}

	// Verify runner.Run was called twice (one per batch)
	assert.Equal(t, 2, mockR.getCalls(), "runner.Run should be called once per batch")

	// Verify messages received by runner
	msgs := mockR.getMessages()
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0].Content, "hello")
	assert.Contains(t, msgs[1].Content, "world")
}

func TestStartLoop_MultipleMessagesMergedInOneBatch(t *testing.T) {
	mockR := &mockLoopRunner{}
	ta := &TagentAgent{
		runner: mockR,
	}

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	// Inject two messages quickly — they should be merged into one batch
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "msg1"})
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "msg2"})

	// Wait for response
	select {
	case evt := <-outputCh:
		require.NotNil(t, evt)
		assert.True(t, evt.IsFinalResponse())
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	ta.StopLoop()

	// runner.Run should be called once (both messages merged)
	assert.Equal(t, 1, mockR.getCalls(), "both messages should be merged into one runner.Run call")

	// Merged message should contain both contents
	msgs := mockR.getMessages()
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Content, "msg1")
	assert.Contains(t, msgs[0].Content, "msg2")
	assert.Contains(t, msgs[0].Content, "---")
}

func TestStartLoop_Idempotent(t *testing.T) {
	mockR := &mockLoopRunner{}
	ta := &TagentAgent{
		runner: mockR,
	}

	ch1, err := ta.StartLoop("user", "session")
	require.NoError(t, err)

	// Second StartLoop should return same channel without error
	ch2, err := ta.StartLoop("user", "session")
	require.NoError(t, err)
	assert.Equal(t, ch1, ch2, "should return same outputCh")

	ta.StopLoop()
}

func TestStopLoop_NotActive_NoOp(t *testing.T) {
	mockR := &mockLoopRunner{}
	ta := &TagentAgent{
		runner: mockR,
	}

	// StopLoop when not active should be a no-op
	ta.StopLoop()
	assert.False(t, ta.loopActive.Load())
}

func TestClose_StopsLoopFirst(t *testing.T) {
	mockR := &mockLoopRunner{}
	ta := &TagentAgent{
		runner:  mockR,
		closers: []Closer{},
	}

	outputCh, err := ta.StartLoop("user", "session")
	require.NoError(t, err)

	// Inject a message to ensure Loop is running
	ta.InjectMessage(model.Message{Role: model.RoleUser, Content: "test"})

	// Drain events to unblock Loop
	go func() {
		for range outputCh {
		}
	}()

	// Close should stop Loop first, then close runner
	err = ta.Close()
	require.NoError(t, err)

	assert.False(t, ta.loopActive.Load(), "Loop should be stopped")
	assert.True(t, mockR.closed, "runner should be closed")
}

func TestMergeBatch_SingleMessage(t *testing.T) {
	original := model.Message{Role: model.RoleUser, Content: "hello"}
	result := mergeBatch([]model.Message{original})
	assert.Equal(t, original, result, "single message should be returned as-is")
}

func TestMergeBatch_MultipleMessages(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "msg1"},
		{Role: model.RoleSystem, Content: "msg2"},
		{Role: model.RoleUser, Content: ""},
	}
	result := mergeBatch(msgs)
	assert.Equal(t, model.RoleUser, result.Role)
	assert.Contains(t, result.Content, "msg1")
	assert.Contains(t, result.Content, "msg2")
	assert.Contains(t, result.Content, "---")
	// Empty content should be skipped
	assert.NotContains(t, result.Content, "\n\n---\n\n\n\n---")
}

func TestDrainMailbox_SingleMessage(t *testing.T) {
	ta := &TagentAgent{
		mailbox: make(chan model.Message, 256),
	}
	ta.mailbox <- model.Message{Content: "test"}

	// Use a context that won't be cancelled
	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())

	batch := ta.drainMailbox()
	require.Len(t, batch, 1)
	assert.Equal(t, "test", batch[0].Content)
}

func TestDrainMailbox_MultipleMessages(t *testing.T) {
	ta := &TagentAgent{
		mailbox: make(chan model.Message, 256),
	}
	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())

	// Pre-load multiple messages
	ta.mailbox <- model.Message{Content: "msg1"}
	ta.mailbox <- model.Message{Content: "msg2"}
	ta.mailbox <- model.Message{Content: "msg3"}

	batch := ta.drainMailbox()
	assert.Len(t, batch, 3)
	assert.Equal(t, "msg1", batch[0].Content)
	assert.Equal(t, "msg2", batch[1].Content)
	assert.Equal(t, "msg3", batch[2].Content)
}

func TestDrainMailbox_Cancelled(t *testing.T) {
	ta := &TagentAgent{
		mailbox: make(chan model.Message, 256),
	}
	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())

	// Cancel immediately
	ta.loopCancel()

	batch := ta.drainMailbox()
	assert.Nil(t, batch, "drainMailbox should return nil when context is cancelled")
}

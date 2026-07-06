package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestEventBus_PublishPull(t *testing.T) {
	bus := NewEventBus()

	msg := model.Message{Role: model.RoleUser, Content: "hello"}
	evt := NewExternalInputEvent("user", msg)
	bus.Publish(evt)

	ctx := context.Background()
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	assert.Equal(t, evt.ID, batch[0].ID)
	assert.Equal(t, "external_input", batch[0].Type)
	assert.Equal(t, "user", batch[0].Source)
	assert.NotNil(t, batch[0].Message)
	assert.Equal(t, "hello", batch[0].Message.Content)
}

func TestEventBus_PullBatch(t *testing.T) {
	bus := NewEventBus()

	// Publish 3 events quickly before pulling.
	e1 := NewExternalInputEvent("user", model.Message{Content: "a"})
	e2 := NewExternalInputEvent("tmux", model.Message{Content: "b"})
	e3 := NewToolUseEvent(model.ToolCall{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "action"}})

	bus.Publish(e1)
	bus.Publish(e2)
	bus.Publish(e3)

	ctx := context.Background()
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 3)
	assert.Equal(t, e1.ID, batch[0].ID)
	assert.Equal(t, e2.ID, batch[1].ID)
	assert.Equal(t, e3.ID, batch[2].ID)
	assert.Equal(t, "tool_use", batch[2].Type)
}

func TestEventBus_PullBlocks(t *testing.T) {
	bus := NewEventBus()

	ctx := context.Background()
	done := make(chan struct{})

	go func() {
		batch, err := bus.Pull(ctx)
		require.NoError(t, err)
		require.Len(t, batch, 1)
		close(done)
	}()

	// Pull should block — give it time and verify it hasn't returned yet.
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("Pull returned before any event was published")
	default:
		// expected: Pull is blocking
	}

	// Now publish — Pull should unblock.
	bus.Publish(NewExternalInputEvent("user", model.Message{Content: "wakeup"}))

	select {
	case <-done:
		// Pull unblocked as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Pull did not unblock within 2s after Publish")
	}
}

func TestEventBus_PullCtxCancel(t *testing.T) {
	bus := NewEventBus()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := bus.Pull(ctx)
		done <- err
	}()

	// Cancel while Pull is blocking.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Pull did not return within 2s after ctx cancellation")
	}
}

func TestEventBus_PublishNil(t *testing.T) {
	bus := NewEventBus()

	// Publishing nil should be a no-op (not panic).
	bus.Publish(nil)

	// Bus should still be empty — verify with a ctx-cancel Pull.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	batch, err := bus.Pull(ctx)
	assert.Nil(t, batch)
	assert.Equal(t, context.Canceled, err)
}

func TestNewExternalInputEvent(t *testing.T) {
	msg := model.Message{Role: model.RoleSystem, Content: "tmux done"}
	evt := NewExternalInputEvent("tmux", msg)

	assert.NotEmpty(t, evt.ID)
	assert.Equal(t, "external_input", evt.Type)
	assert.Equal(t, "tmux", evt.Source)
	assert.False(t, evt.Timestamp.IsZero())
	assert.NotNil(t, evt.Message)
	assert.Equal(t, "tmux done", evt.Message.Content)
	assert.Nil(t, evt.ToolCall)
	assert.NotNil(t, evt.Metadata)
}

func TestNewToolUseEvent(t *testing.T) {
	tc := model.ToolCall{
		ID: "call-1",
		Function: model.FunctionDefinitionParam{
			Name:      "action",
			Arguments: []byte(`{"command":"ls"}`),
		},
	}
	evt := NewToolUseEvent(tc)

	assert.NotEmpty(t, evt.ID)
	assert.Equal(t, "tool_use", evt.Type)
	assert.Equal(t, "agent_loop", evt.Source)
	assert.False(t, evt.Timestamp.IsZero())
	assert.Nil(t, evt.Message)
	assert.NotNil(t, evt.ToolCall)
	assert.Equal(t, "action", evt.ToolCall.Function.Name)
	assert.NotNil(t, evt.Metadata)
}

func TestEventBus_TryPull_Empty(t *testing.T) {
	bus := NewEventBus()
	events := bus.TryPull()
	assert.NotNil(t, events)
	assert.Empty(t, events)
}

func TestEventBus_TryPull_Batch(t *testing.T) {
	bus := NewEventBus()

	// Publish 3 events
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg1"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg2"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "msg3"}))

	events := bus.TryPull()
	assert.Len(t, events, 3)
	assert.Equal(t, "msg1", events[0].Message.Content)
	assert.Equal(t, "msg2", events[1].Message.Content)
	assert.Equal(t, "msg3", events[2].Message.Content)

	// Channel should be empty now
	events2 := bus.TryPull()
	assert.Empty(t, events2)
}

func TestEventBus_TryPull_NonBlocking(t *testing.T) {
	bus := NewEventBus()

	// TryPull should return immediately (not block) when empty
	done := make(chan struct{})
	go func() {
		bus.TryPull()
		close(done)
	}()

	select {
	case <-done:
		// Success — returned immediately
	case <-time.After(100 * time.Millisecond):
		t.Fatal("TryPull blocked on empty channel")
	}
}

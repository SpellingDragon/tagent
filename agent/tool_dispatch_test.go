package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestDispatch_CallableTool_PublishesResult(t *testing.T) {
	bus := NewEventBus()
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	tool := &loopMockTool{name: "greet", result: "hello world"}

	al := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        &loopMockModel{},
		Tools:        []trpctool.Tool{tool},
		OutputCh:     make(chan *event.Event, 10),
		Name:         "test-dispatch",
		MaxToolIters: 10,
	})

	ctx := context.Background()

	// Dispatch a tool use event.
	tc := model.ToolCall{
		ID:       "tc-1",
		Function: model.FunctionDefinitionParam{Name: "greet", Arguments: []byte(`{"name":"alice"}`)},
	}
	al.dispatchToolUse(ctx, tc)

	// The goroutine should publish the result to bus shortly.
	deadline := time.After(2 * time.Second)
	var resultEvt *AgentEvent
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for tool result on bus")
		default:
			batch, err := bus.Pull(context.Background())
			if err != nil {
				t.Fatalf("Pull error: %v", err)
			}
			for _, e := range batch {
				if e.Source == "tool_result" {
					resultEvt = e
					goto found
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
found:
	require.NotNil(t, resultEvt)
	assert.Equal(t, "external_input", resultEvt.Type)
	assert.Contains(t, resultEvt.Message.Content, "hello world")
	assert.Equal(t, 1, tool.getCallCount())
}

func TestDispatch_TmuxAsync_DoesNotPublish(t *testing.T) {
	bus := NewEventBus()
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	// A tool that returns a tmux-async marker.
	tool := &asyncMarkerTool{name: "action"}

	al := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        &loopMockModel{},
		Tools:        []trpctool.Tool{tool},
		OutputCh:     make(chan *event.Event, 10),
		Name:         "test-dispatch",
		MaxToolIters: 10,
	})

	ctx := context.Background()

	tc := model.ToolCall{
		ID:       "tc-async",
		Function: model.FunctionDefinitionParam{Name: "action", Arguments: []byte(`{}`)},
	}
	al.dispatchToolUse(ctx, tc)

	// Give goroutine time to run.
	time.Sleep(100 * time.Millisecond)

	// Bus should be empty — tmux-async results are NOT published by dispatch.
	ctxCancel, cancel := context.WithCancel(context.Background())
	cancel()
	batch, _ := bus.Pull(ctxCancel)
	assert.Empty(t, batch, "tmux-async result should not be published to bus")
}

func TestDispatch_UnknownTool_NoPublish(t *testing.T) {
	bus := NewEventBus()
	compressor := NewSmartCompressor(WithMaxTokens(8000))
	counter := &mockTokenCounter{tokens: 100}
	preproc := NewPreprocessor(compressor, counter, 8000, 0.8)

	al := NewAgentLoop(AgentLoopConfig{
		Bus:          bus,
		Preprocessor: preproc,
		Model:        &loopMockModel{},
		OutputCh:     make(chan *event.Event, 10),
		Name:         "test-dispatch",
		MaxToolIters: 10,
	})

	ctx := context.Background()

	// Tool not in toolMap.
	tc := model.ToolCall{
		ID:       "tc-unknown",
		Function: model.FunctionDefinitionParam{Name: "nonexistent"},
	}
	al.dispatchToolUse(ctx, tc)

	// No goroutine launched → bus stays empty.
	time.Sleep(100 * time.Millisecond)
	ctxCancel, cancel := context.WithCancel(context.Background())
	cancel()
	batch, _ := bus.Pull(ctxCancel)
	assert.Empty(t, batch)
}

// asyncMarkerTool is a tool whose Call result satisfies tmuxAsyncResult.
type asyncMarkerTool struct {
	name string
}

func (t *asyncMarkerTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name, Description: "async marker"}
}

type asyncMarkerResult struct{}

func (asyncMarkerResult) IsTmuxAsync() bool { return true }

func (t *asyncMarkerTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return asyncMarkerResult{}, nil
}

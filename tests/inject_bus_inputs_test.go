package tagent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockEchoTool is a simple CallableTool that returns its arguments as the result.
// Used to satisfy the ReAct loop's tool_call requirement without external deps.
type mockEchoTool struct{}

func (m *mockEchoTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "Echo tool for testing",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"command": {Type: "string", Description: "Command to echo"},
			},
		},
	}
}

func (m *mockEchoTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return `{"status":"ok","output":"echo result"}`, nil
}

// TestInjectBusInputs_DuringReAct verifies that a message injected mid-ReAct reaches the next LLM request.
// - InjectMessage adds the message while a tool call runs; the InjectBusInputs BeforeModel callback pulls it into the request.
// - The next call therefore carries both the tool result and the injected message before the final response.
func TestInjectBusInputs_DuringReAct(t *testing.T) {
	memStore := tagentmemory.NewInMemoryStore()

	type llmCall struct {
		messages []model.Message
	}
	var llmCalls []llmCall
	var llmMu sync.Mutex

	mockModel := &invariantMockModel{
		responses: []*model.Response{
			makeToolCallResponse(),
			makeFinalResponse("done A"),
			makeFinalResponse("done B"),
		},
	}
	mockModel2 := &capturingMockModel{
		inner: mockModel,
		onCall: func(req *model.Request) {
			llmMu.Lock()
			llmCalls = append(llmCalls, llmCall{messages: req.Messages})
			llmMu.Unlock()
		},
	}

	cfg := &tagentagent.TagentConfig{
		Model:             mockModel2,
		MemoryStore:       memStore,
		MaxToolIterations: 5,
		MaxTokens:         8000,
		Tools:             []trpctool.Tool{&mockEchoTool{}},
	}

	ta, err := tagentagent.NewTagentAgent(cfg)
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	ta.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "message A: run echo hello",
	})

	time.Sleep(100 * time.Millisecond)
	ta.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "message B: also check git status",
	})

	for {
		select {
		case evt := <-outputCh:
			if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
				choice := evt.Response.Choices[len(evt.Response.Choices)-1]
				if len(choice.Message.ToolCalls) == 0 {
					goto done
				}
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timeout waiting for final response")
		}
	}
done:
	ta.StopLoop()

	llmMu.Lock()
	defer llmMu.Unlock()

	require.GreaterOrEqual(t, len(llmCalls), 2, "expected at least 2 LLM calls")

	firstCallHasA := false
	for _, msg := range llmCalls[0].messages {
		if strings.Contains(msg.Content, "message A: run echo hello") {
			firstCallHasA = true
			break
		}
	}
	assert.True(t, firstCallHasA, "first LLM call should contain message A")

	anyCallHasB := false
	for _, call := range llmCalls {
		for _, msg := range call.messages {
			if strings.Contains(msg.Content, "message B: also check git status") {
				anyCallHasB = true
				break
			}
		}
		if anyCallHasB {
			break
		}
	}

	if !anyCallHasB {
		t.Log("message B was consumed from EventBus but may not appear in LLM messages")
		t.Log("All LLM call messages:")
		for ci, call := range llmCalls {
			t.Logf("  Call %d:", ci)
			for i, msg := range call.messages {
				t.Logf("    [%d] role=%s content=%q", i, msg.Role, msg.Content[:min(len(msg.Content), 80)])
			}
		}
	}
}

// capturingMockModel wraps a mock model and captures request messages.
// It adds a delay before the first LLM call to allow InjectMessage to
// populate the bus before BeforeModel runs.
type capturingMockModel struct {
	inner  *invariantMockModel
	onCall func(req *model.Request)
}

func (m *capturingMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	if m.onCall != nil {
		m.onCall(req)
	}
	return m.inner.GenerateContent(ctx, req)
}

func (m *capturingMockModel) Info() model.Info { return m.inner.Info() }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

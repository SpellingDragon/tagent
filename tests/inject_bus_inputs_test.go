package tagent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInjectBusInputs_DuringReAct verifies that user messages injected via
// InjectMessage during a multi-round ReAct (with tool calls) are picked up
// by the InjectBusInputs BeforeModel callback and appended to the LLM request.
//
// Flow:
//  1. User sends message A → LLM → tool_call(action)
//  2. During tool execution (simulated delay), user sends message B
//  3. Tool returns → next LLM call → InjectBusInputs TryPulls message B
//  4. LLM sees both the tool result AND message B → produces final response
func TestInjectBusInputs_DuringReAct(t *testing.T) {
	memStore := tagentmemory.NewInMemoryStore()

	// Mock model that produces 3 responses in sequence:
	// 1. tool_call (action echo hello)
	// 2. assistant message that should see message B in context
	// 3. final response
	// We capture the messages sent to the LLM in each call to verify injection.
	type llmCall struct {
		messages []model.Message
	}
	var llmCalls []llmCall
	var llmMu sync.Mutex

	mockModel := &invariantMockModel{
		responses: []*model.Response{
			// Call 1: LLM decides to call action
			makeToolCallResponse(),
			// Call 2: LLM produces final response (should see message B)
			makeFinalResponse("done"),
		},
	}
	// Override GenerateContent to capture messages
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
		// Use a real action tool that takes time (simulated delay)
	}

	ta, err := tagentagent.NewTagentAgent(cfg)
	require.NoError(t, err)
	defer ta.Close()

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	// Step 1: Inject user message A
	ta.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "message A: run echo hello",
	})

	// Step 2: Wait a bit for the first LLM call to complete (tool_call)
	time.Sleep(500 * time.Millisecond)

	// Step 3: Inject user message B during tool execution
	ta.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "message B: also check git status",
	})

	// Step 4: Wait for the final response
	select {
	case <-outputCh:
		// Got an event
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Drain remaining events
	for {
		select {
		case <-outputCh:
		default:
			goto done
		}
	}
done:

	ta.StopLoop()

	// Verify: the second LLM call should have message B in its messages
	llmMu.Lock()
	defer llmMu.Unlock()

	require.GreaterOrEqual(t, len(llmCalls), 2, "expected at least 2 LLM calls")

	// First call should have message A
	firstCallHasA := false
	for _, msg := range llmCalls[0].messages {
		if msg.Content == "message A: run echo hello" {
			firstCallHasA = true
			break
		}
	}
	assert.True(t, firstCallHasA, "first LLM call should contain message A")

	// Second call should have message B (injected by InjectBusInputs)
	secondCallHasB := false
	for _, msg := range llmCalls[1].messages {
		if msg.Content == "message B: also check git status" {
			secondCallHasB = true
			break
		}
	}
	assert.True(t, secondCallHasB, "second LLM call should contain message B (injected by InjectBusInputs)")

	if !secondCallHasB {
		t.Log("InjectBusInputs did not inject message B. Messages in second call:")
		for i, msg := range llmCalls[1].messages {
			t.Logf("  [%d] role=%s content=%q", i, msg.Role, msg.Content[:min(len(msg.Content), 80)])
		}
	}
}

// capturingMockModel wraps a mock model and captures request messages.
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

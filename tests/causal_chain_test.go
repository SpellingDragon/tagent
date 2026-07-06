package tagent_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock model for causal-chain tests
// ============================================================================

// causalMockModel returns a fixed response on each call and records requests.
type causalMockModel struct {
	mu          sync.Mutex
	response    *model.Response
	lastRequest *model.Request
}

func newCausalMockModel(response *model.Response) *causalMockModel {
	return &causalMockModel{response: response}
}

func (m *causalMockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.lastRequest = req
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if m.response != nil {
		ch <- m.response
	}
	close(ch)
	return ch, nil
}

func (m *causalMockModel) Info() model.Info { return model.Info{Name: "causal-mock"} }

func (m *causalMockModel) LastRequest() *model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// sequenceMockModel returns responses in order for multi-turn tests.
type sequenceModel struct {
	mu        sync.Mutex
	responses []*model.Response
	idx       int
}

func newSequenceModel(responses []*model.Response) *sequenceModel {
	return &sequenceModel{responses: responses}
}

func (m *sequenceModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.idx
	m.idx++
	var resp *model.Response
	if idx < len(m.responses) {
		resp = m.responses[idx]
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	if resp != nil {
		ch <- resp
	}
	close(ch)
	return ch, nil
}

func (m *sequenceModel) Info() model.Info { return model.Info{Name: "sequence-mock"} }

// ============================================================================
// Mock tool for causal-chain tests
// ============================================================================

// echoTool is a simple callable tool used to verify tool-call chains.
type echoTool struct{}

func (echoTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "echo",
		Description: "Echoes the input back",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"msg": {Type: "string", Description: "Message to echo"},
			},
			Required: []string{"msg"},
		},
	}
}

func (echoTool) Call(ctx context.Context, args []byte) (any, error) {
	return `{"echo":` + string(args) + `}`, nil
}

// ============================================================================
// Causal chain tests
// ============================================================================

// TestCausalChain_EndToEnd verifies that a simple user -> assistant turn
// produces two persisted FullEvents with a parent/child causal link.
func TestCausalChain_EndToEnd(t *testing.T) {
	mockModel := newCausalMockModel(&model.Response{
		ID:   "resp-final",
		Done: true,
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: "Hello from tagent",
			},
		}},
	})

	store := tagentmemory.NewInMemoryStore()
	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:        mockModel,
		MemoryStore:  store,
		SystemPrompt: "You are a test assistant.",
	})
	require.NoError(t, err)
	defer ag.Close()

	outputCh, err := ag.StartLoop("user-1", "session-causal")
	require.NoError(t, err)

	ag.InjectMessage(model.NewUserMessage("Hello"))

	var final *event.Event
loop:
	for {
		select {
		case evt, ok := <-outputCh:
			if !ok {
				break loop
			}
			final = evt
			if evt.IsFinalResponse() {
				break loop
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for final response")
		}
	}
	ag.StopLoop()

	require.NotNil(t, final)
	assert.Equal(t, "Hello from tagent", final.Response.Choices[0].Message.Content)

	partitionID := tagentmemory.PartitionIDFromName(ag.Info().Name)
	events, err := store.QueryEvents(tagentmemory.QueryOptions{
		PartitionID: partitionID,
		Limit:       100,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 2, "expected at least user + assistant FullEvents")

	// Find the user event (external_input with non-empty EventSummary)
	// and the assistant event (agent_output)
	var userKey, asstKey int64
	for _, e := range events {
		if e.EventType == "external_input" && e.EventSummary != "" && userKey == 0 {
			userKey = e.EventKey
		}
		if e.EventType == "agent_output" && asstKey == 0 {
			asstKey = e.EventKey
		}
	}
	require.NotZero(t, userKey, "user event not found")
	require.NotZero(t, asstKey, "assistant event not found")

	parent, err := store.GetParent(asstKey)
	require.NoError(t, err)
	assert.NotZero(t, parent, "assistant event should have a non-zero parent (causal chain)")
	assert.NotEqual(t, asstKey, parent, "parent should not be self")
}

// TestCausalChain_WithToolCall verifies a 4-event tool-call chain:
//
//	user -> assistant(tool_calls) -> tool_result -> assistant(final)
//
// and checks that each persisted event has the expected causal parent.
func TestCausalChain_WithToolCall(t *testing.T) {
	mockModel := newSequenceModel([]*model.Response{
		{
			ID:   "resp-tool",
			Done: true,
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "",
					ToolCalls: []model.ToolCall{{
						ID:   "call-1",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name:      "echo",
							Arguments: []byte(`{"msg":"hi"}`),
						},
					}},
				},
			}},
		},
		{
			ID:   "resp-final",
			Done: true,
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: "Echo says hi",
				},
			}},
		},
	})

	store := tagentmemory.NewInMemoryStore()
	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:        mockModel,
		MemoryStore:  store,
		SystemPrompt: "You are a test assistant.",
		Tools:        []tool.Tool{echoTool{}},
	})
	require.NoError(t, err)
	defer ag.Close()

	outputCh, err := ag.StartLoop("user-1", "session-tool-chain")
	require.NoError(t, err)

	ag.InjectMessage(model.NewUserMessage("Echo hi"))

	var final *event.Event
loop:
	for {
		select {
		case evt, ok := <-outputCh:
			if !ok {
				break loop
			}
			final = evt
			if evt.IsFinalResponse() {
				break loop
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for final response")
		}
	}
	ag.StopLoop()

	require.NotNil(t, final)
	assert.Equal(t, "Echo says hi", final.Response.Choices[0].Message.Content)

	partitionID := tagentmemory.PartitionIDFromName(ag.Info().Name)
	events, err := store.QueryEvents(tagentmemory.QueryOptions{
		PartitionID: partitionID,
		Limit:       100,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 4, "expected at least 4 FullEvents (user, assistant-tool, tool_result, assistant-final)")

	// Sort events by timestamp to obtain causal order.
	sort.Slice(events, func(i, j int) bool {
		return events[i].EventKey < events[j].EventKey
	})

	// Causal chain: each event's parent should be the previous event.
	for i := 1; i < len(events); i++ {
		parent, err := store.GetParent(events[i].EventKey)
		require.NoError(t, err)
		assert.Equal(t, events[i-1].EventKey, parent,
			"event %d parent mismatch", i)
	}
}

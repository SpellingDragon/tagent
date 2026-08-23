package compress

import (
	"context"
	"fmt"
	"sync"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBatchSummaryModel is a mock model that returns pre-configured summaries.
// Used by tests in both smart_compress_test.go and context_compressor_test.go.
type mockBatchSummaryModel struct {
	mu         sync.Mutex
	callCount  int
	responses  []string
	failOnCall map[int]bool
}

func (m *mockBatchSummaryModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	callIdx := m.callCount
	m.callCount++
	m.mu.Unlock()

	if m.failOnCall[callIdx] {
		return nil, fmt.Errorf("mock error on call %d", callIdx)
	}

	summary := ""
	if callIdx < len(m.responses) {
		summary = m.responses[callIdx]
	}

	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{
			{Message: model.Message{Role: model.RoleAssistant, Content: summary}},
		},
	}
	close(ch)
	return ch, nil
}

func (m *mockBatchSummaryModel) Info() model.Info {
	return model.Info{Name: "mock-batch-summary"}
}

// countingSummaryModel returns a fixed multi-line summary and counts calls.
// Used by curateCards tests to verify condenseCardLines scrubs LLM output to
// single-line cards (the card section is parsed by "- "-prefixed lines).
type countingSummaryModel struct {
	mu    sync.Mutex
	calls int
}

func (m *countingSummaryModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{
			Role:    model.RoleAssistant,
			Content: "- 浓缩卡片 A\n- 浓缩卡片 B",
		}}},
	}
	close(ch)
	return ch, nil
}

func (m *countingSummaryModel) Info() model.Info { return model.Info{Name: "counting-summary"} }

// ============================================================================
// SplitSystemMessage tests
// ============================================================================

func TestSplitSystemMessage_WithSystem(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	sys, rest := SplitSystemMessage(messages)
	require.NotNil(t, sys)
	assert.Equal(t, "system prompt", sys.Content)
	assert.Len(t, rest, 2)
}

func TestSplitSystemMessage_NoSystem(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	sys, rest := SplitSystemMessage(messages)
	assert.Nil(t, sys)
	assert.Len(t, rest, 2)
}

func TestSplitSystemMessage_Empty(t *testing.T) {
	sys, rest := SplitSystemMessage(nil)
	assert.Nil(t, sys)
	assert.Nil(t, rest)
}

// ============================================================================
// parseEventKeyAndType tests
// ============================================================================

func TestParseEventKeyAndType_Valid(t *testing.T) {
	key, evtType, remainder := tagentevent.ParseEventKeyAndType("[evt_75bcd15|task] user request content")
	assert.Equal(t, int64(123456789), key)
	assert.Equal(t, "task", evtType)
	assert.Equal(t, "user request content", remainder)
}

func TestParseEventKeyAndType_NoPrefix(t *testing.T) {
	key, evtType, _ := tagentevent.ParseEventKeyAndType("user request content")
	assert.Equal(t, int64(0), key)
	assert.Equal(t, "unknown", evtType)
}

func TestParseEventKeyAndType_Malformed(t *testing.T) {
	key, _, _ := tagentevent.ParseEventKeyAndType("[evt_invalid_key|task] content")
	assert.Equal(t, int64(0), key)
}

func TestParseEventKeyAndType_NoBar(t *testing.T) {
	key, _, _ := tagentevent.ParseEventKeyAndType("[evt_12345task] content")
	assert.Equal(t, int64(0), key)
}

func TestParseEventKeyAndType_LargeKey(t *testing.T) {
	key, evtType, _ := tagentevent.ParseEventKeyAndType("[evt_7fffffffffffffff|memory] large snowflake key")
	assert.Equal(t, int64(9223372036854775807), key)
	assert.Equal(t, "memory", evtType)
}

func TestParseEventKeyAndType_EmptyContent(t *testing.T) {
	key, _, _ := tagentevent.ParseEventKeyAndType("")
	assert.Equal(t, int64(0), key)
}

func TestParseEventKeyAndType_OnlyPrefix(t *testing.T) {
	key, evtType, remainder := tagentevent.ParseEventKeyAndType("[evt_2a|task]")
	assert.Equal(t, int64(42), key)
	assert.Equal(t, "task", evtType)
	assert.Equal(t, "", remainder)
}

// ============================================================================
// eventTypeToRole tests
// ============================================================================

func TestEventTypeToRole(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		want      model.Role
	}{
		{"external_input", "external_input", model.RoleUser},
		{"agent_output", "agent_output", model.RoleAssistant},
		{"action_command", "action_command", model.RoleUser},
		{"thinking_plan", "thinking_plan", model.RoleAssistant},
		{"empty", "", model.RoleUser},
		{"unknown_type", "unknown_type", model.RoleUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EventTypeToRole(tt.eventType)
			assert.Equal(t, tt.want, got)
		})
	}
}

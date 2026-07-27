package compress

import (
	"context"
	"fmt"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
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
// SegmentMessages tests
// ============================================================================

func TestSplitByTaskBoundary_SingleTask(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	segments := SegmentMessages(messages)
	require.Len(t, segments, 1)
	// Incomplete because no next user input to close it
	assert.False(t, segments[0].IsComplete)
	assert.Len(t, segments[0].Messages, 2)
}

func TestSplitByTaskBoundary_MultipleTasks(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	segments := SegmentMessages(messages)
	require.Len(t, segments, 2)
	// First segment is complete (closed by next user input)
	assert.True(t, segments[0].IsComplete)
	// Last segment is incomplete (trailing)
	assert.False(t, segments[1].IsComplete)
}

func TestSplitByTaskBoundary_IncompleteTask(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2 (incomplete)"},
	}

	segments := SegmentMessages(messages)
	require.Len(t, segments, 2)
	assert.True(t, segments[0].IsComplete)
	assert.False(t, segments[1].IsComplete, "last segment without next user input should be incomplete")
}

func TestSplitByTaskBoundary_ToolCallNotBoundary(t *testing.T) {
	// Assistant with tool calls should NOT be a task boundary
	messages := []model.Message{
		{Role: model.RoleUser, Content: "do something"},
		{Role: model.RoleAssistant, Content: "using tool", ToolCalls: []model.ToolCall{{ID: "1"}}},
		{Role: model.RoleTool, Content: "tool result"},
		{Role: model.RoleAssistant, Content: "final answer"},
	}

	segments := SegmentMessages(messages)
	require.Len(t, segments, 1, "tool call cycle should be part of one task")
	// Incomplete because no next user input to close it
	assert.False(t, segments[0].IsComplete)
}

func TestSplitByTaskBoundary_Empty(t *testing.T) {
	segments := SegmentMessages(nil)
	assert.Nil(t, segments)
}

// ============================================================================
// SmartCompressor Compress tests
// ============================================================================

func TestSmartCompress_Stage1Only(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1)) // Force compression

	// Build messages with multiple task segments
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
		{Role: model.RoleUser, Content: "task 3"},
		{Role: model.RoleAssistant, Content: "result 3"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// System message is always first
	assert.Equal(t, model.RoleSystem, result[0].Role, "first message should be system")
	// Recent segment should be preserved (user input + result, possibly with compress notice between)
	hasTask3 := false
	hasResult3 := false
	for _, msg := range result {
		if msg.Content == "task 3" {
			hasTask3 = true
		}
		if msg.Content == "result 3" {
			hasResult3 = true
		}
	}
	assert.True(t, hasTask3, "recent segment user input 'task 3' should be preserved")
	assert.True(t, hasResult3, "recent segment result 'result 3' should be preserved")
	// Without a summaryModel, L2 degrades to first-stage and injects an error notice.
	hasCompressNotice := false
	for _, msg := range result {
		if strings.Contains(msg.Content, "[context_compress") {
			hasCompressNotice = true
			break
		}
	}
	assert.True(t, hasCompressNotice, "should have at least one compress notice or error notice")
}

func TestSmartCompress_PreservesSystem(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	messages := []model.Message{
		{Role: model.RoleSystem, Content: "important system prompt"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	assert.Equal(t, model.RoleSystem, result[0].Role)
	assert.Equal(t, "important system prompt", result[0].Content, "system prompt should be preserved")
}

func TestSmartCompress_PreservesRecentTasks(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2))

	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: "result 1"},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
		{Role: model.RoleUser, Content: "task 3"},
		{Role: model.RoleAssistant, Content: "result 3"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// Should keep: system + compress notice + task2 + task3 (2 recent)
	// Find task2 and task3 in the result
	foundTask2 := false
	foundTask3 := false
	for _, msg := range result {
		if msg.Content == "task 2" {
			foundTask2 = true
		}
		if msg.Content == "task 3" {
			foundTask3 = true
		}
	}
	assert.True(t, foundTask2, "recent task 2 should be preserved")
	assert.True(t, foundTask3, "recent task 3 should be preserved")
}

func TestSmartCompress_UnderThreshold(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2))

	// Only 1 task segment, which is <= keepRecentTasks
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	}

	result := sc.Compress(context.Background(), messages, nil)
	assert.Equal(t, messages, result, "should not compress when under threshold")
}

func TestSmartCompress_NoSystem(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	// Long non-key assistant output gives the old segment a real compression
	// target (short messages are all "key" → empty target → nothing to compress).
	longOutput := strings.Repeat("detailed execution output padding ", 6)
	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: longOutput},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)
	// Without system message, compress still happens.
	// Without summaryModel, L2 degrades to first-stage and a [context_compress_error]
	// notice is injected at the top (before segments).
	// Find the user message by content (it may not be result[0] if an error
	// notice was injected first).
	hasUserTask1 := false
	for _, msg := range result {
		if msg.Role == model.RoleUser && msg.Content == "task 1" {
			hasUserTask1 = true
			break
		}
	}
	assert.True(t, hasUserTask1, "preserved user input 'task 1' should be in result")
	// Verify an error notice exists somewhere in the result
	hasErrorNotice := false
	for _, msg := range result {
		if strings.Contains(msg.Content, "[context_compress") {
			hasErrorNotice = true
			break
		}
	}
	assert.True(t, hasErrorNotice, "should have compress notice or error notice")
}

// ============================================================================
// Fallback Notice tests
// ============================================================================

func TestSmartCompress_Fallback_NoModel(t *testing.T) {
	// Without summaryModel, L2 degrades to first-stage (drop tool, keep text)
	// and injects a [context_compress_error] notice so the agent is aware.
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	// The old segment carries a LONG assistant message (>100 chars → non-key,
	// compressible) so there is a genuine compression target; without a summary
	// model that target degrades to first-stage with an error notice.
	longOutput := strings.Repeat("detailed execution output padding ", 6)
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system"},
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: longOutput},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// System message preserved
	assert.Equal(t, model.RoleSystem, result[0].Role)
	// A compress error notice should be present (no summaryModel available)
	hasErrorNotice := false
	for _, msg := range result {
		if strings.Contains(msg.Content, "[context_compress_error]") {
			hasErrorNotice = true
			break
		}
	}
	assert.True(t, hasErrorNotice, "should have [context_compress_error] notice when no summaryModel")
}

func TestSmartCompress_Fallback_CompressNoticeFormat(t *testing.T) {
	// Without summaryModel, verify the error notice format
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	longOutput := strings.Repeat("detailed execution output padding ", 6)
	messages := []model.Message{
		{Role: model.RoleUser, Content: "task 1"},
		{Role: model.RoleAssistant, Content: longOutput},
		{Role: model.RoleUser, Content: "task 2"},
		{Role: model.RoleAssistant, Content: "result 2"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// Find the error notice
	var errorNotice string
	for _, msg := range result {
		if strings.Contains(msg.Content, "[context_compress_error]") {
			errorNotice = msg.Content
			break
		}
	}
	assert.Contains(t, errorNotice, "[context_compress_error]", "should contain error notice prefix")
	assert.Contains(t, errorNotice, "降级", "should mention degradation strategy")
}

// TestSmartCompress_Fallback_WhenAllSegmentsRecent verifies that when all
// segments are within keep_recent_tasks but the total still exceeds the budget,
// the compressor falls back to:
//  1. summarizing tool/exec info from the oldest segment (L2)
//  2. if still over budget, fully summarizing the most recent execution event (L3)
func TestSmartCompress_Fallback_WhenAllSegmentsRecent(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	sc := NewSmartCompressor(
		WithKeepRecentTasks(2),
		WithMaxTokens(50), // Force over-budget with only 2 segments
		WithMemStore(memStore),
		WithSummaryModel(&mockBatchSummaryModel{responses: []string{"summary of task 1"}}),
	)

	// Two task segments (both "recent" because keep_recent=2), with large
	// tool outputs in segment 0.
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "task 1 user input"},
		{Role: model.RoleAssistant, Content: "task 1 assistant response"},
		{Role: model.RoleTool, Content: strings.Repeat("tool output for task 1 ", 20)},
		{Role: model.RoleUser, Content: "task 2 user input"},
		{Role: model.RoleAssistant, Content: "task 2 assistant response"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// System + segment 1 (task 2) should be preserved
	hasTask2 := false
	for _, msg := range result {
		if msg.Content == "task 2 user input" {
			hasTask2 = true
		}
	}
	assert.True(t, hasTask2, "current task 2 user input should be preserved")

	// Segment 0 should be compressed (either L1 dropped tool, L2/L3 summary,
	// or L3 archive reference).
	hasCompressOrArchive := false
	for _, msg := range result {
		if strings.Contains(msg.Content, "[context_compress") || strings.Contains(msg.Content, "[context_archive") {
			hasCompressOrArchive = true
			break
		}
	}
	assert.True(t, hasCompressOrArchive, "segment 0 should be compressed when over budget")
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
// generateSummary split-and-re-summarize tests
// ============================================================================

// oversizedMockModel returns a summary that's always way too long.
type oversizedMockModel struct {
	responses []string
	callCount int
	mu        sync.Mutex
}

func (m *oversizedMockModel) GenerateContent(
	ctx context.Context, request *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.callCount
	m.callCount++
	m.mu.Unlock()

	content := "这是一个摘要。"
	if idx < len(m.responses) {
		content = m.responses[idx]
	}

	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		Choices: []model.Choice{{Message: model.Message{Content: content}}},
	}
	close(ch)
	return ch, nil
}

func (m *oversizedMockModel) Info() model.Info {
	return model.Info{Name: "oversized-mock"}
}

func TestGenerateSummary_OversizedTriggersResplit(t *testing.T) {
	// Mock returns 5000 chars on first call (exceeds target of ~2000 * 1.5 = 3000),
	// then returns 200 chars on subsequent calls (within target).
	oversized := strings.Repeat("摘要内容。", 500) // ~2500 chars * 2 = 5000 chars
	short := "简短摘要。"
	mock := &oversizedMockModel{
		responses: []string{oversized, short, short, short, short},
	}

	sc := NewSmartCompressor(
		WithSummaryModel(mock),
		WithMaxTokens(8000),
	)

	segments := []*TaskSegment{
		{Messages: []model.Message{{Role: model.RoleUser, Content: "msg1"}}, IsComplete: true},
		{Messages: []model.Message{{Role: model.RoleAssistant, Content: "reply1"}}, IsComplete: true},
		{Messages: []model.Message{{Role: model.RoleUser, Content: "msg2"}}, IsComplete: true},
		{Messages: []model.Message{{Role: model.RoleAssistant, Content: "reply2"}}, IsComplete: true},
	}

	summary, hadError := sc.generateSummary(context.Background(), segments, 1, 1)
	require.False(t, hadError)
	assert.NotEmpty(t, summary)
	// After re-splitting, the result should be shorter than the oversized original
	assert.Less(t, len(summary), 5000)
}

func TestGenerateSummary_WithinTargetNotSplit(t *testing.T) {
	mock := &oversizedMockModel{
		responses: []string{"正常长度摘要。"},
	}

	sc := NewSmartCompressor(
		WithSummaryModel(mock),
		WithMaxTokens(8000),
	)

	segments := []*TaskSegment{
		{Messages: []model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 100)}}},
	}

	summary, hadError := sc.generateSummary(context.Background(), segments, 1, 1)
	require.False(t, hadError)
	assert.Equal(t, "正常长度摘要。", summary)
}

func TestGenerateSummary_HardTruncateAsFallback(t *testing.T) {
	// Mock always returns oversized, even after splitting (depth reaches 2)
	oversized := strings.Repeat("超长摘要。", 5000) // ~20000 chars
	mock := &oversizedMockModel{
		responses: []string{oversized, oversized, oversized, oversized, oversized},
	}

	sc := NewSmartCompressor(
		WithSummaryModel(mock),
		WithMaxTokens(8000),
	)

	// Single segment — can't be split, so should hard-truncate
	segments := []*TaskSegment{
		{Messages: []model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 100)}}},
	}

	summary, hadError := sc.generateSummary(context.Background(), segments, 1, 1)
	require.False(t, hadError)
	assert.NotEmpty(t, summary)
	// Should be hard-truncated (targetChars would be ~20 chars, 1.5x = ~30)
	assert.Less(t, len(summary), len(oversized))
}

// ============================================================================
// eventTypeToRole tests
// ============================================================================

// TestBuildBatchSummaryPrompt_KeepsCorrelationIdentifiers locks the D7
// compression constraint: the summarization prompt must instruct the model to
// preserve correlation identifiers (task id / tool_id / tool name) so
// notifications and results stay content-linkable after compression.
func TestBuildBatchSummaryPrompt_KeepsCorrelationIdentifiers(t *testing.T) {
	prompt := buildBatchSummaryPrompt(3, 1, 2, 500, 2000)
	for _, want := range []string{"关联标识", "task id", "tool_id", "工具名"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("summary prompt must require keeping %q, got:\n%s", want, prompt)
		}
	}
}

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

// TestGenerateSummary_SplitsOversizedInput: a single giant task segment must
// be split into sub-calls before hitting the summary model — one oversized
// call is slow/failure-prone/can blow the model window (observed live:
// 128K-char segment from a long sub-agent run). Each sub-call's input must
// stay within ~cap bounds.
func TestGenerateSummary_SplitsOversizedInput(t *testing.T) {
	m := &inputRecordingModel{}
	sc := NewSmartCompressor(WithSummaryModel(m), WithMaxTokens(102400), WithMaxSummaryInputChars(40000))

	// One segment, 32 messages × 4000 chars = 128K chars.
	seg := &TaskSegment{}
	for i := 0; i < 32; i++ {
		seg.Messages = append(seg.Messages, model.NewUserMessage(strings.Repeat("长", 4000)))
	}
	summary, hadErr := sc.generateSummary(context.Background(), []*TaskSegment{seg}, 1, 1)
	if hadErr || summary == "" {
		t.Fatalf("summary failed: hadErr=%v", hadErr)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inputs) < 2 {
		t.Fatalf("oversized input must be split into multiple calls, got %d call(s)", len(m.inputs))
	}
	// Prompt adds overhead; allow input cap ×2 (last-resort truncation bound).
	for i, n := range m.inputs {
		if n > 40000*2+4096 {
			t.Errorf("call %d input %d chars exceeds split bound", i, n)
		}
	}
}

// inputRecordingModel records the char size of each summary call's user
// message and returns a fixed short summary.
type inputRecordingModel struct {
	mu     sync.Mutex
	inputs []int
}

func (m *inputRecordingModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.inputs = append(m.inputs, len(req.Messages[len(req.Messages)-1].Content))
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "子摘要"}}}}
	close(ch)
	return ch, nil
}

func (m *inputRecordingModel) Info() model.Info { return model.Info{Name: "input-recorder"} }

// TestSmartCompress_EmptyTarget_NotFalseDegradation verifies that a segment
// whose compression target is empty (e.g. an L1 segment that is only a user
// message, with no non-key exec content to summarize) is NOT treated as an
// LLM failure. Previously the empty summary was mistaken for a failure →
// false degradation + a scary [context_compress_error] notice + firstStage
// that barely reduces tokens (observed live as dur≈0 degradations where
// after>=before). Regression guard for the empty-target skip.
func TestSmartCompress_EmptyTarget_NotFalseDegradation(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1))

	// Segment 0 = [user "a"] only → L1 with EMPTY nonKeyMsgs (nothing to
	// compress). Segment 1 = [user "b", assistant "result b"] is recent (L0).
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system"},
		{Role: model.RoleUser, Content: "a"},
		{Role: model.RoleUser, Content: "b"},
		{Role: model.RoleAssistant, Content: "result b"},
	}

	result := sc.Compress(context.Background(), messages, nil)

	// No false degradation: the empty-target segment must NOT produce an error
	// notice (there was no LLM failure — there was simply nothing to compress).
	for _, msg := range result {
		assert.NotContains(t, msg.Content, "[context_compress_error]",
			"empty compression target must not be reported as an LLM failure")
	}
	// The user message is preserved.
	found := false
	for _, msg := range result {
		if msg.Content == "a" {
			found = true
		}
	}
	assert.True(t, found, "user message of the empty-target segment must be preserved")
}

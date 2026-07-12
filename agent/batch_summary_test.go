package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// referenceValuator is a mock EventValuator that returns Reference processing
// for all segments. Used to trigger level 3 compression in tests.
type referenceValuator struct{}

func (referenceValuator) Evaluate(_ context.Context, segments []*TaskSegment) ([]EventValue, string, error) {
	values := make([]EventValue, len(segments))
	for i := range segments {
		values[i] = EventValue{
			ValueScore: 0.3,
			Processing: Reference,
		}
	}
	return values, "batch summary", nil
}

// mockBatchSummaryModel is a controllable mock model.Model for batch summary tests.
// It returns pre-configured responses per call index, and can simulate failures.
type mockBatchSummaryModel struct {
	mu         sync.Mutex
	callCount  int
	responses  []string     // summaries to return per call index
	failOnCall map[int]bool // call indices that should return an error
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

func (m *mockBatchSummaryModel) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// ==================== Task 7.8: 多事件分批（50 事件 → 3 批 → 3 条摘要） ====================

func TestBatchSegmentsByTokenBudget_50Segments_3Batches(t *testing.T) {
	// Create 50 segments, each with ~30 tokens (15 per message)
	// "用户任务请求编号 N" = 10 runes → 5 tokens + 10 overhead = 15
	// "助手处理结果编号 N" = 10 runes → 5 tokens + 10 overhead = 15
	// Segment total = 30 tokens
	segments := make([]*TaskSegment, 50)
	for i := 0; i < 50; i++ {
		segments[i] = &TaskSegment{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: fmt.Sprintf("用户任务请求编号 %d", i+1)},
				{Role: model.RoleAssistant, Content: fmt.Sprintf("助手处理结果编号 %d", i+1)},
			},
			IsComplete: true,
		}
	}

	sc := NewSmartCompressor()
	// maxTokens=1050 → maxInputTokens=525
	// Each segment = 30 tokens → 17 segments per batch (510 tokens), 18th would be 540 > 525
	// Expected: 3 batches [0-16], [17-33], [34-49]
	batches := sc.batchSegmentsByTokenBudget(segments, 1050)

	require.Len(t, batches, 3, "50 segments with maxTokens=1050 should produce 3 batches")
	assert.Len(t, batches[0], 17, "batch 1 should have 17 segments")
	assert.Len(t, batches[1], 17, "batch 2 should have 17 segments")
	assert.Len(t, batches[2], 16, "batch 3 should have 16 segments (remaining)")
}

func TestSummarizeBatches_3Batches_3Summaries(t *testing.T) {
	// Create 3 batches with 1 segment each
	batches := make([][]*TaskSegment, 3)
	for i := range batches {
		batches[i] = []*TaskSegment{
			{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: fmt.Sprintf("batch %d task", i+1)},
					{Role: model.RoleAssistant, Content: fmt.Sprintf("batch %d result", i+1)},
				},
				IsComplete: true,
			},
		}
	}

	mockModel := &mockBatchSummaryModel{
		responses: []string{"summary 1", "summary 2", "summary 3"},
	}

	sc := NewSmartCompressor(WithSummaryModel(mockModel))
	msgs, hadError := sc.summarizeBatches(context.Background(), batches)

	require.False(t, hadError, "should not have error when all batches succeed")
	require.Len(t, msgs, 3, "should return 3 summary messages")
	assert.Equal(t, model.RoleAssistant, msgs[0].Role)
	assert.Equal(t, model.RoleAssistant, msgs[1].Role)
	assert.Equal(t, model.RoleAssistant, msgs[2].Role)
	assert.Contains(t, msgs[0].Content, "[摘要批次 1/3]")
	assert.Contains(t, msgs[0].Content, "summary 1")
	assert.Contains(t, msgs[1].Content, "[摘要批次 2/3]")
	assert.Contains(t, msgs[1].Content, "summary 2")
	assert.Contains(t, msgs[2].Content, "[摘要批次 3/3]")
	assert.Contains(t, msgs[2].Content, "summary 3")
}

// ==================== Task 7.9: 单批不超预算 ====================

func TestBatchSegmentsByTokenBudget_SingleBatch(t *testing.T) {
	// 5 small segments with large maxTokens → all in one batch
	segments := make([]*TaskSegment, 5)
	for i := 0; i < 5; i++ {
		segments[i] = &TaskSegment{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "task"},
				{Role: model.RoleAssistant, Content: "result"},
			},
			IsComplete: true,
		}
	}

	sc := NewSmartCompressor()
	batches := sc.batchSegmentsByTokenBudget(segments, 8000)

	require.Len(t, batches, 1, "5 small segments with maxTokens=8000 should fit in 1 batch")
	assert.Len(t, batches[0], 5, "single batch should contain all 5 segments")
}

func TestBatchSegmentsByTokenBudget_EmptySegments(t *testing.T) {
	sc := NewSmartCompressor()
	batches := sc.batchSegmentsByTokenBudget(nil, 8000)
	assert.Nil(t, batches)
}

// ==================== Task 7.10: 单批失败容错（mock LLM 第 2 批失败，第 1、3 批成功） ====================

func TestSummarizeBatches_PartialFailure(t *testing.T) {
	// 3 batches, 2nd batch (call index 1) fails
	batches := make([][]*TaskSegment, 3)
	for i := range batches {
		batches[i] = []*TaskSegment{
			{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: fmt.Sprintf("batch %d task", i+1)},
					{Role: model.RoleAssistant, Content: fmt.Sprintf("batch %d result", i+1)},
				},
				IsComplete: true,
			},
		}
	}

	mockModel := &mockBatchSummaryModel{
		responses:  []string{"summary 1", "summary 2", "summary 3"},
		failOnCall: map[int]bool{1: true}, // 2nd call (index 1) fails
	}

	sc := NewSmartCompressor(WithSummaryModel(mockModel))
	msgs, hadError := sc.summarizeBatches(context.Background(), batches)

	require.False(t, hadError, "should not have error when some batches succeed")
	require.Len(t, msgs, 2, "should return 2 summary messages (batch 2 failed)")

	// Batch numbering should reflect original batch positions, not compacted
	assert.Contains(t, msgs[0].Content, "[摘要批次 1/3]", "first success should be batch 1/3")
	assert.Contains(t, msgs[1].Content, "[摘要批次 3/3]", "second success should be batch 3/3")
	assert.NotContains(t, msgs[1].Content, "[摘要批次 2/3]", "should NOT have batch 2 (failed)")
}

func TestSummarizeBatches_AllFail(t *testing.T) {
	batches := make([][]*TaskSegment, 3)
	for i := range batches {
		batches[i] = []*TaskSegment{
			{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "task"},
					{Role: model.RoleAssistant, Content: "result"},
				},
				IsComplete: true,
			},
		}
	}

	mockModel := &mockBatchSummaryModel{
		failOnCall: map[int]bool{0: true, 1: true, 2: true},
	}

	sc := NewSmartCompressor(WithSummaryModel(mockModel))
	msgs, hadError := sc.summarizeBatches(context.Background(), batches)

	require.True(t, hadError, "should have error when all batches fail")
	assert.Nil(t, msgs, "should return nil messages when all batches fail")
}

// ==================== Task 7.11: 无 summaryModel 时跳过分批（不调用 LLM） ====================

func TestCompress_NoSummaryModel_SkipsBatching(t *testing.T) {
	// Create enough segments to trigger compression (KeepRecentTasks=2)
	messages := []model.Message{}
	for i := 0; i < 6; i++ {
		messages = append(messages,
			model.Message{Role: model.RoleUser, Content: fmt.Sprintf("task %d", i+1)},
			model.Message{Role: model.RoleAssistant, Content: fmt.Sprintf("result %d", i+1)},
		)
	}

	sc := NewSmartCompressor() // No summaryModel
	result := sc.Compress(context.Background(), messages, nil)

	require.NotEmpty(t, result)

	// Verify no batch summary messages are present
	for _, msg := range result {
		if msg.Role == model.RoleAssistant {
			assert.NotContains(t, msg.Content, "[摘要批次",
				"should not have batch summary messages when no summaryModel")
		}
	}
}

func TestSummarizeBatches_NoSummaryModel_ReturnsNil(t *testing.T) {
	sc := NewSmartCompressor() // No summaryModel
	batches := [][]*TaskSegment{
		{&TaskSegment{Messages: []model.Message{{Role: model.RoleUser, Content: "task"}}}},
	}

	msgs, hadError := sc.summarizeBatches(context.Background(), batches)

	assert.Nil(t, msgs, "should return nil when no summaryModel")
	assert.False(t, hadError, "should not report error when summaryModel is simply absent")
}

// ==================== Task 7.12: 摘要消息使用 System role 且包含批次编号 ====================

func TestSummarizeBatches_SystemRoleWithBatchNumber(t *testing.T) {
	batches := make([][]*TaskSegment, 3)
	for i := range batches {
		batches[i] = []*TaskSegment{
			{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: fmt.Sprintf("task %d", i+1)},
					{Role: model.RoleAssistant, Content: fmt.Sprintf("result %d", i+1)},
				},
				IsComplete: true,
			},
		}
	}

	mockModel := &mockBatchSummaryModel{
		responses: []string{"first summary", "second summary", "third summary"},
	}

	sc := NewSmartCompressor(WithSummaryModel(mockModel))
	msgs, _ := sc.summarizeBatches(context.Background(), batches)

	require.Len(t, msgs, 3)

	// All messages should be System role with batch numbering
	for i, msg := range msgs {
		assert.Equal(t, model.RoleAssistant, msg.Role,
			"summary message %d should be System role", i+1)
		assert.Contains(t, msg.Content, fmt.Sprintf("[摘要批次 %d/3]", i+1),
			"summary message %d should contain batch number", i+1)
	}

	assert.Contains(t, msgs[0].Content, "first summary")
	assert.Contains(t, msgs[1].Content, "second summary")
	assert.Contains(t, msgs[2].Content, "third summary")
}

// ==================== Integration: Compress with batch summaries ====================

func TestCompress_WithBatchSummaries(t *testing.T) {
	// Create 8 segments (6 old + 2 recent with KeepRecentTasks=2)
	// Use small maxTokens to force multiple batches
	messages := []model.Message{}
	for i := 0; i < 8; i++ {
		messages = append(messages,
			model.Message{Role: model.RoleUser, Content: fmt.Sprintf("用户任务编号 %d 完整内容", i+1)},
			model.Message{Role: model.RoleAssistant, Content: fmt.Sprintf("助手结果编号 %d 完整内容", i+1)},
		)
	}

	mockModel := &mockBatchSummaryModel{
		responses: []string{"summary A", "summary B", "summary C"},
	}

	// Use a mock valuator that returns Reference processing to trigger level 3 compression
	sc := NewSmartCompressor(
		WithSummaryModel(mockModel),
		WithMaxTokens(200), // maxInputTokens=100 → small batches
		WithEventValuator(&referenceValuator{}),
	)

	result := sc.Compress(context.Background(), messages, nil)

	require.NotEmpty(t, result)

	// Count System messages that are batch summaries
	batchSummaryCount := 0
	for _, msg := range result {
		if msg.Role == model.RoleAssistant && strings.Contains(msg.Content, "[摘要批次") {
			batchSummaryCount++
		}
	}
	assert.GreaterOrEqual(t, batchSummaryCount, 1,
		"should have at least 1 batch summary message")

	// Verify the compress event mentions batch info
	// Note: level 2 segments also have compress notices, but only level 3 notices
	// include batch summary info. We need to find the one that mentions "批摘要".
	foundCompressEvent := false
	for _, msg := range result {
		if msg.Role == model.RoleSystem && strings.Contains(msg.Content, "[context_compress]") {
			if strings.Contains(msg.Content, "批摘要") {
				foundCompressEvent = true
				break
			}
		}
	}
	assert.True(t, foundCompressEvent, "should have a context_compress event with batch summary info")
}

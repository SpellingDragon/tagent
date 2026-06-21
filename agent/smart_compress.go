package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// SmartCompressor performs two-stage context compression in BeforeModel.
//
// Stage 1: Drop old task segments based on task boundaries (agent_output markers)
// Stage 2: Generate LLM summary of dropped segments (if summaryModel is available)
//
// This is a "view transformation" — it modifies the messages sent to the LLM,
// but does NOT modify the Session.
type SmartCompressor struct {
	summaryModel    model.Model // Optional: used for Stage 2 LLM summary
	KeepRecentTasks int         // Number of recent complete tasks to keep (default: 2)
	maxTokens       int         // Token budget for calculating batch size (default: DefaultMaxTokens)
}

// NewSmartCompressor creates a new SmartCompressor.
func NewSmartCompressor(opts ...SmartCompressorOption) *SmartCompressor {
	sc := &SmartCompressor{
		KeepRecentTasks: 2,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

// SmartCompressorOption configures SmartCompressor.
type SmartCompressorOption func(*SmartCompressor)

// WithSummaryModel sets the LLM model for Stage 2 summary generation.
func WithSummaryModel(m model.Model) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.summaryModel = m
	}
}

// WithKeepRecentTasks sets how many recent tasks to keep.
func WithKeepRecentTasks(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.KeepRecentTasks = n
	}
}

// WithMaxTokens sets the token budget used for batch size calculation.
func WithMaxTokens(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) {
		sc.maxTokens = n
	}
}

// Compress compresses the message list by dropping old task segments.
// inv provides Session.Events access for extracting event_keys of compressed segments.
func (sc *SmartCompressor) Compress(
	ctx context.Context,
	messages []model.Message,
	inv *agent.Invocation,
) []model.Message {
	// 1. Separate system message
	systemMsg, rest := splitSystemMessage(messages)

	// 2. Split by task boundary
	segments := splitByTaskBoundary(rest)

	// Log segment details for diagnostics
	completeCount := 0
	for _, seg := range segments {
		if seg.IsComplete {
			completeCount++
		}
	}
	log.Debugf("[SmartCompress] split: segments=%d (complete=%d incomplete=%d) keepRecent=%d msgs=%d",
		len(segments), completeCount, len(segments)-completeCount, sc.KeepRecentTasks, len(messages))

	// 3. If within limit, no compression needed
	if len(segments) <= sc.KeepRecentTasks {
		log.Debugf("[SmartCompress] skip: segments=%d <= keepRecentTasks=%d — not enough task boundaries to compress",
			len(segments), sc.KeepRecentTasks)
		return messages
	}

	// 4. Split into old and recent segments
	oldSegments := segments[:len(segments)-sc.KeepRecentTasks]
	recentSegments := segments[len(segments)-sc.KeepRecentTasks:]

	// 5. Stage 2: Generate batched LLM summaries of old segments (if model available)
	var summaryMsgs []model.Message
	var summaryHadError bool
	batchCount := 0
	if sc.summaryModel != nil {
		batches := sc.batchSegmentsByTokenBudget(oldSegments, sc.maxTokens)
		batchCount = len(batches)
		log.Debugf("[SmartCompress] stage2: old_segments=%d batches=%d maxInputTokens=%d",
			len(oldSegments), batchCount, sc.maxTokens/2)
		summaryMsgs, summaryHadError = sc.summarizeBatches(ctx, batches)
	} else {
		log.Debugf("[SmartCompress] stage2: skipped (no summaryModel configured)")
	}

	// 6. Collect compressed event_keys from message prefix
	compressedKeys := sc.collectCompressedKeys(oldSegments)

	// 7. Reconstruct message list with context_compress event
	var result []model.Message
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}

	// Build context_compress event as a system message
	compressEvent := sc.buildCompressEvent(len(oldSegments), compressedKeys, batchCount, len(summaryMsgs), summaryHadError)
	result = append(result, compressEvent)

	// Append batch summary messages (each is a System message with batch number)
	result = append(result, summaryMsgs...)

	for _, seg := range recentSegments {
		result = append(result, seg.Messages...)
	}

	// Check for pending user message in recent segments.
	// If found, append it to the end so the LLM addresses it.
	// If not found, append a guidance message so the LLM knows context was compressed.
	if pendingUser := findPendingUserMessage(recentSegments); pendingUser != nil {
		result = append(result, *pendingUser)
	} else {
		result = append(result, model.Message{
			Role:    model.RoleUser,
			Content: "（以上是对话历史摘要。如果有新任务，请告诉我。）",
		})
	}

	// Token reduction summary for diagnostics
	beforeTokens := NewDefaultTokenCounter().Estimate(messages)
	afterTokens := NewDefaultTokenCounter().Estimate(result)
	log.Infof("[SmartCompress] old_segments=%d recent_segments=%d compressed_keys=%d batches=%d summary_msgs=%d tokens=%d->%d (-%d)",
		len(oldSegments), len(recentSegments), len(compressedKeys), batchCount, len(summaryMsgs),
		beforeTokens, afterTokens, beforeTokens-afterTokens)

	return result
}

// findPendingUserMessage searches for a pending user message in the recent segments.
// It finds the last IsComplete=true segment, then looks for the first user message
// in any segment after it. Returns nil if no pending user message is found.
// This ensures that a user message that hasn't been responded to yet is preserved
// and surfaced after compression.
func findPendingUserMessage(segments []*TaskSegment) *model.Message {
	if len(segments) == 0 {
		return nil
	}

	// Find the last complete segment (closed by agent_output)
	lastCompleteIdx := -1
	for i := len(segments) - 1; i >= 0; i-- {
		if segments[i].IsComplete {
			lastCompleteIdx = i
			break
		}
	}

	// If no complete segment exists, there is no task boundary to search after
	if lastCompleteIdx == -1 {
		return nil
	}

	// Look for the first user message in segments after the last complete one
	for i := lastCompleteIdx + 1; i < len(segments); i++ {
		for j := range segments[i].Messages {
			if segments[i].Messages[j].Role == model.RoleUser {
				return &segments[i].Messages[j]
			}
		}
	}

	return nil
}

// buildCompressEvent creates a context_compress event message.
// When batch summaries are generated, they are appended as separate System messages
// after the compress event. This message contains metadata only.
func (sc *SmartCompressor) buildCompressEvent(
	segmentCount int,
	keys []int64,
	batchCount int,
	successCount int,
	summaryHadError bool,
) model.Message {
	var content strings.Builder

	if len(keys) > 0 {
		// Generate a pseudo event_key for the compress event itself
		// and list the compressed event keys so LLM can select them
		content.WriteString(fmt.Sprintf("[context_compress] 压缩了 %d 个对话片段，被压缩的事件 key 列表: [", segmentCount))
		for i, k := range keys {
			if i > 0 {
				content.WriteString(", ")
			}
			content.WriteString(fmt.Sprintf("%d", k))
		}
		content.WriteString("]")
	} else {
		content.WriteString(fmt.Sprintf("[context_compress] 压缩了 %d 个对话片段", segmentCount))
	}

	switch {
	case batchCount > 0 && successCount > 0:
		content.WriteString(fmt.Sprintf("\n\n已生成 %d/%d 批摘要（见下方摘要消息）。", successCount, batchCount))
		if successCount < batchCount {
			content.WriteString("部分批次摘要生成失败。")
		}
	case batchCount > 0 && summaryHadError:
		content.WriteString("\n\n摘要生成失败。完整上下文可通过 recall agent 获取。")
	default:
		content.WriteString(fmt.Sprintf("\n\n[Compressed: %d earlier tasks omitted.]", segmentCount))
	}

	return model.NewSystemMessage(content.String())
}

// parseEventKeyFromPrefix extracts a Snowflake EventKey from a message content prefix.
// Format: "[evt_123456789|task] original content..."
// Returns 0 if no valid key is found (caller filters zero values).
func parseEventKeyFromPrefix(content string) int64 {
	const prefix = "[evt_"
	if !strings.HasPrefix(content, prefix) {
		return 0
	}
	barPos := strings.IndexByte(content[5:], '|')
	if barPos < 0 {
		return 0
	}
	keyStr := content[5 : 5+barPos]
	key, err := strconv.ParseInt(keyStr, 10, 64)
	if err != nil {
		return 0
	}
	return key
}

// collectCompressedKeys extracts event_keys from compressed message segments.
// Parses the "[evt_<KEY>|<type>]" prefix added by injectEventKeyPrefixes in BeforeModel.
// This does NOT access Session.Events — it reads keys directly from message content.
func (sc *SmartCompressor) collectCompressedKeys(
	oldSegments []*TaskSegment,
) []int64 {
	seen := make(map[int64]bool)
	var keys []int64

	for _, seg := range oldSegments {
		for _, msg := range seg.Messages {
			key := parseEventKeyFromPrefix(msg.Content)
			if key > 0 && !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}

	return keys
}

// TaskSegment is a group of messages delimited by task boundaries.
type TaskSegment struct {
	Messages   []model.Message
	IsComplete bool // true if closed by an agent_output (assistant without tool calls)
}

// splitSystemMessage separates the system message from the rest.
func splitSystemMessage(messages []model.Message) (*model.Message, []model.Message) {
	if len(messages) == 0 {
		return nil, nil
	}
	if messages[0].Role == model.RoleSystem {
		return &messages[0], messages[1:]
	}
	return nil, messages
}

// splitByTaskBoundary splits messages into segments at task boundaries.
// A task boundary is defined by an assistant message without tool calls (agent_output).
func splitByTaskBoundary(messages []model.Message) []*TaskSegment {
	if len(messages) == 0 {
		return nil
	}

	var segments []*TaskSegment
	var current *TaskSegment

	for i := range messages {
		msg := &messages[i]
		if current == nil {
			current = &TaskSegment{}
		}
		current.Messages = append(current.Messages, *msg)

		// Check for task boundary: assistant message without tool calls
		if msg.Role == model.RoleAssistant && len(msg.ToolCalls) == 0 {
			current.IsComplete = true
			segments = append(segments, current)
			current = nil
		}
	}

	// If there's an incomplete segment, add it
	if current != nil {
		segments = append(segments, current)
	}

	return segments
}

// generateSummary generates an LLM summary of old task segments.
// Returns the summary string and whether an error occurred during LLM invocation.
// If summaryModel is nil, returns ("", false) meaning summary not attempted.
func (sc *SmartCompressor) generateSummary(
	ctx context.Context, segments []*TaskSegment,
) (summary string, hadError bool) {
	if sc.summaryModel == nil {
		return "", false
	}

	// Build summary prompt from old segments
	var sb strings.Builder
	sb.WriteString("请对以下历史对话片段生成简洁的摘要，保留关键语义、决策和结果。不要遗漏重要信息。\n\n")
	for i, seg := range segments {
		sb.WriteString(fmt.Sprintf("--- 片段 %d ---\n", i+1))
		for _, msg := range seg.Messages {
			roleLabel := "unknown"
			switch msg.Role {
			case model.RoleSystem:
				roleLabel = "system"
			case model.RoleUser:
				roleLabel = "user"
			case model.RoleAssistant:
				roleLabel = "assistant"
			case model.RoleTool:
				roleLabel = "tool"
			}
			content := msg.Content
			if len(msg.ToolCalls) > 0 {
				var calls []string
				for _, tc := range msg.ToolCalls {
					calls = append(calls, fmt.Sprintf("%s(%s)", tc.Function.Name, string(tc.Function.Arguments)))
				}
				content = fmt.Sprintf("[tool_calls: %s]", strings.Join(calls, ", "))
				if msg.Content != "" {
					content = msg.Content + "\n" + content
				}
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", roleLabel, content))
		}
	}
	sb.WriteString("\n--- 摘要 ---")

	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个对话摘要助手。请为历史对话生成简洁但完整的摘要。"),
			model.NewUserMessage(sb.String()),
		},
	}

	respCh, err := sc.summaryModel.GenerateContent(ctx, req)
	if err != nil {
		log.Errorf("[SmartCompress] stage2 LLM failed: %v", err)
		return "", true
	}

	var result string
	for resp := range respCh {
		if resp.Error != nil {
			log.Errorf("[SmartCompress] stage2 response error: %v", resp.Error)
			return "", true
		}
		if len(resp.Choices) > 0 {
			result += resp.Choices[0].Message.Content
		}
	}

	return result, false
}

// batchSegmentsByTokenBudget divides segments into batches where each batch's
// estimated token count stays within maxTokens/2 (leaving room for summary output).
// Segments that individually exceed the budget form their own batch.
func (sc *SmartCompressor) batchSegmentsByTokenBudget(
	segments []*TaskSegment, maxTokens int,
) [][]*TaskSegment {
	if len(segments) == 0 {
		return nil
	}

	// Use half the token budget for input; reserve half for summary output
	maxInputTokens := maxTokens / 2
	if maxInputTokens < 100 {
		maxInputTokens = 100
	}

	counter := NewDefaultTokenCounter()
	var batches [][]*TaskSegment
	var currentBatch []*TaskSegment
	currentTokens := 0

	for _, seg := range segments {
		segTokens := counter.Estimate(seg.Messages)

		// Start a new batch if adding this segment would exceed the budget
		// and the current batch is non-empty
		if currentTokens+segTokens > maxInputTokens && len(currentBatch) > 0 {
			batches = append(batches, currentBatch)
			currentBatch = nil
			currentTokens = 0
		}

		currentBatch = append(currentBatch, seg)
		currentTokens += segTokens
	}

	if len(currentBatch) > 0 {
		batches = append(batches, currentBatch)
	}

	return batches
}

// summarizeBatches generates LLM summaries for each batch of segments.
// Each successful summary is wrapped as a System message with batch numbering.
// Failed batches are skipped (log warning). Returns (nil, true) if all batches fail.
func (sc *SmartCompressor) summarizeBatches(
	ctx context.Context, batches [][]*TaskSegment,
) ([]model.Message, bool) {
	if sc.summaryModel == nil || len(batches) == 0 {
		return nil, false
	}

	var result []model.Message
	successCount := 0

	for i, batch := range batches {
		summary, hadError := sc.generateSummary(ctx, batch)
		if hadError || summary == "" {
			log.Warnf("[SmartCompress] batch %d/%d summary failed, skipping", i+1, len(batches))
			continue
		}
		successCount++
		content := fmt.Sprintf("[摘要批次 %d/%d]\n%s", i+1, len(batches), summary)
		result = append(result, model.NewSystemMessage(content))
	}

	if successCount == 0 {
		log.Warnf("[SmartCompress] all %d batch summaries failed", len(batches))
		return nil, true
	}

	return result, false
}

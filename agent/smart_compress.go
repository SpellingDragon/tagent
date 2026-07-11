package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// SmartCompressor performs two-stage context compression in Preprocessor.
//
// Stage 1: Drop old task segments based on task boundaries (agent_output markers)
// Stage 2: Generate LLM summary of dropped segments (if summaryModel is available)
//
// This is a "view transformation" — it modifies the messages sent to the LLM,
// but does NOT modify the Session.
type SmartCompressor struct {
	summaryModel    model.Model  // Optional: used for Stage 2 LLM summary
	KeepRecentTasks int          // Number of recent complete tasks to keep (default: 2)
	maxTokens       int          // Token budget for calculating batch size (default: DefaultMaxTokens)
	tokenCounter    TokenCounter // Token estimator (injected, not NewDefaultTokenCounter)

	// Configurable truncation parameters (Task Group 3: migrated from package-level constants)
	maxExecStateChars  int // Total execution state truncation (default: 2000)
	maxToolResultChars int // Per-tool-result truncation (default: 500)
	maxToolArgsChars   int // Per-tool-args truncation (default: 80)

	// Chunk splitting parameters (Task Group 2)
	chunkSize       int                // Max chars per chunk (default: 1000)
	chunkSummaryLen int                // Summary length per chunk (default: 150)
	memStore        memory.MemoryStore // Optional: for chunk persistence
	projection      *SessionProjection // Optional: for chunk EventReference append
	chunkSplitter   *ChunkSplitter     // Lazily initialized
}

// NewSmartCompressor creates a new SmartCompressor.
func NewSmartCompressor(opts ...SmartCompressorOption) *SmartCompressor {
	sc := &SmartCompressor{
		KeepRecentTasks:    2,
		tokenCounter:       NewDefaultTokenCounter(),
		maxExecStateChars:  2000,
		maxToolResultChars: 500,
		maxToolArgsChars:   80,
		chunkSize:          1000,
		chunkSummaryLen:    150,
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
	return func(sc *SmartCompressor) { sc.maxTokens = n }
}

// WithMaxExecStateChars sets the total execution state truncation limit.
func WithMaxExecStateChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxExecStateChars = n }
}

// WithMaxToolResultChars sets the per-tool-result truncation limit.
func WithMaxToolResultChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxToolResultChars = n }
}

// WithMaxToolArgsChars sets the per-tool-args truncation limit.
func WithMaxToolArgsChars(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.maxToolArgsChars = n }
}

// WithChunkSize sets the maximum chunk size for semantic chunking.
func WithChunkSize(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.chunkSize = n }
}

// WithChunkSummaryLen sets the summary length per chunk.
func WithChunkSummaryLen(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.chunkSummaryLen = n }
}

// WithMemStore injects a MemoryStore for chunk persistence.
func WithMemStore(ms memory.MemoryStore) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.memStore = ms }
}

// WithProjection injects a SessionProjection for chunk EventReference append.
func WithProjection(p *SessionProjection) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.projection = p }
}

func WithTokenCounter(tc TokenCounter) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.tokenCounter = tc }
}

// Compress compresses the message list by dropping old task segments.
// inv provides Session.Events access for extracting event_keys of compressed segments.
func (sc *SmartCompressor) Compress(
	ctx context.Context,
	messages []model.Message,
	inv *agent.Invocation,
) []model.Message {
	startTime := time.Now()
	// 1. Separate system message
	systemMsg, rest := splitSystemMessage(messages)

	// 2. Split by task boundary
	segments := SegmentMessages(rest)

	// Log segment details for diagnostics
	completeCount := 0
	for _, seg := range segments {
		if seg.IsComplete {
			completeCount++
		}
	}
	log.Debugf("[SmartCompress] split: segments=%d (complete=%d incomplete=%d) keepRecent=%d msgs=%d",
		len(segments), completeCount, len(segments)-completeCount, sc.KeepRecentTasks, len(messages))

	// 3. Determine how many segments to keep (must leave room for summarization)
	keepCount := sc.KeepRecentTasks
	if keepCount >= len(segments) {
		// Not enough segments to satisfy KeepRecentTasks while still having
		// old segments to summarize. Reduce keepCount to make room.
		keepCount = len(segments) - 1
		log.Debugf("[SmartCompress] segments=%d <= keepRecentTasks=%d, reducing keepCount=%d for summarization",
			len(segments), sc.KeepRecentTasks, keepCount)
	}
	if keepCount < 1 {
		// Only 0 or 1 segment. Truncation is STRICTLY PROHIBITED (see event/types.go).
		if sc.summaryModel != nil {
			// Have summary model: summarize everything (keepCount=0).
			keepCount = 0
			log.Debugf("[SmartCompress] only %d segment(s), summarizing all (keepCount=0)", len(segments))
		} else {
			// No summary model and cannot split — return original to avoid
			// destructive information loss.
			log.Debugf("[SmartCompress] only %d segment(s) and no summaryModel — returning original", len(segments))
			return messages
		}
	}

	// 4. Split into old and recent segments
	oldSegments := segments[:len(segments)-keepCount]
	recentSegments := segments[len(segments)-keepCount:]

	// 4a. If no old segments to compress, return original messages.
	// This prevents adding empty [context_compress] messages that waste tokens.
	if len(oldSegments) == 0 {
		log.Debugf("[SmartCompress] no old segments to compress, returning original")
		return messages
	}

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

	// 6. Collect compressed event info (key + type + summary) from message prefix
	compressedInfos := sc.collectCompressedEventInfo(oldSegments)

	// 7. Reconstruct message list with context_compress event
	var result []model.Message
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}

	// Build context_compress event as a system message
	compressEvent := sc.buildCompressEvent(len(oldSegments), compressedInfos, batchCount, len(summaryMsgs), summaryHadError)
	result = append(result, compressEvent)

	// Append batch summary messages (each is a System message with batch number)
	result = append(result, summaryMsgs...)

	// Append structured execution state (pure code extraction, no LLM call)
	execState := sc.extractExecutionState(oldSegments)
	if execState != "" {
		result = append(result, model.NewSystemMessage(execState))
	}

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
	beforeTokens := sc.tokenCounter.Estimate(messages)
	afterTokens := sc.tokenCounter.Estimate(result)

	// Structured JSON metrics
	metrics := map[string]interface{}{
		"event":              "smart_compress",
		"before_tokens":      beforeTokens,
		"after_tokens":       afterTokens,
		"discarded_segments": len(oldSegments),
		"kept_segments":      len(recentSegments),
		"summary_generated":  sc.summaryModel != nil && len(summaryMsgs) > 0,
		"duration_ms":        time.Since(startTime).Milliseconds(),
	}
	if metricsJSON, err := json.Marshal(metrics); err == nil {
		log.Infof("[SmartCompress] %s old=%d recent=%d keys=%d batches=%d summary_msgs=%d tokens=%d->%d (-%d)",
			string(metricsJSON), len(oldSegments), len(recentSegments), len(compressedInfos),
			batchCount, len(summaryMsgs), beforeTokens, afterTokens, beforeTokens-afterTokens)
	}

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

// EventInfo holds extracted metadata from a compressed message's [evt_KEY|type] prefix.
type EventInfo struct {
	Key     int64
	Type    string
	Summary string
}

// collectCompressedEventInfo extracts EventKey, EventType, and a content summary
// from each message in oldSegments. It parses the "[evt_<KEY>|<type>]" prefix
// injected by InjectEventKeys, then truncates the remaining content as summary.
// For messages without a prefix, it falls back to [unknown] type.
func (sc *SmartCompressor) collectCompressedEventInfo(
	oldSegments []*TaskSegment,
) []EventInfo {
	seen := make(map[int64]bool)
	var infos []EventInfo

	for _, seg := range oldSegments {
		for _, msg := range seg.Messages {
			key, evtType, remainder := parseEventKeyAndType(msg.Content)
			if key > 0 && !seen[key] {
				seen[key] = true
				summary := truncate(remainder, sc.chunkSummaryLen)
				if summary == "" {
					summary = truncate(msg.Content, sc.chunkSummaryLen)
				}
				infos = append(infos, EventInfo{
					Key:     key,
					Type:    evtType,
					Summary: summary,
				})
			}
		}
	}

	return infos
}

// parseEventKeyAndType extracts EventKey and EventType from a message content
// with "[evt_<KEY>|<type>] <remainder>" prefix.
// Returns (0, "unknown", content) if no valid prefix is found.
func parseEventKeyAndType(content string) (key int64, eventType string, remainder string) {
	const prefix = "[evt_"
	if !strings.HasPrefix(content, prefix) {
		return 0, "unknown", content
	}
	// Find the closing bracket
	closePos := strings.IndexByte(content, ']')
	if closePos < 0 {
		return 0, "unknown", content
	}
	// Content between "[evt_" and "]" is "KEY|type"
	inner := content[len(prefix):closePos]
	barPos := strings.IndexByte(inner, '|')
	if barPos < 0 {
		return 0, "unknown", content
	}
	keyStr := inner[:barPos]
	eventType = inner[barPos+1:]
	k, err := strconv.ParseInt(keyStr, 10, 64)
	if err != nil {
		return 0, "unknown", content
	}
	// Remainder is everything after "] "
	remainder = strings.TrimSpace(content[closePos+1:])
	return k, eventType, remainder
}

// buildCompressEvent creates a context_compress event message.
// Lists each compressed event with its key, type, and summary so the LLM
// can selectively recall specific events by key.
func (sc *SmartCompressor) buildCompressEvent(
	segmentCount int,
	infos []EventInfo,
	batchCount int,
	successCount int,
	summaryHadError bool,
) model.Message {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("[context_compress] 压缩了 %d 个对话片段:", segmentCount))

	if len(infos) > 0 {
		content.WriteString("\n")
		for _, info := range infos {
			content.WriteString(fmt.Sprintf("\n- evt_%d [%s]: %s", info.Key, info.Type, info.Summary))
		}
		content.WriteString("\n\n使用 recall 工具检索对应 key 获取完整内容。")
	} else {
		content.WriteString(fmt.Sprintf("\n\n[Compressed: %d earlier tasks omitted.]", segmentCount))
	}

	switch {
	case batchCount > 0 && successCount > 0:
		content.WriteString(fmt.Sprintf("\n\n已生成 %d/%d 批摘要（见下方摘要消息）。", successCount, batchCount))
		if successCount < batchCount {
			content.WriteString("部分批次摘要生成失败。")
		}
	case batchCount > 0 && summaryHadError:
		content.WriteString("\n\n摘要生成失败。完整上下文可通过 recall 工具获取。")
	}

	return model.NewSystemMessage(content.String())
}

// extractExecutionState extracts a structured summary of tool calls and their
// results from the given segments. This is pure code extraction (no LLM call),
// ensuring that critical execution state (success/failure, key return values)
// is never lost during compression.
//
// Output format:
//
//	[执行状态]
//	- 调用: search_file({"path":"/tmp",...})
//	  → 结果: Error: invalid path...
//	- 调用: action({"command":"curl ..."})
//	  → 结果: {"session_id":"...","status":"running"}
//
// Total length is capped at sc.maxExecStateChars (default: 2000). Each tool result is
// truncated to sc.maxToolResultChars (default: 500). If total exceeds the cap, the
// most recent entries are kept (earlier ones are dropped).
func (sc *SmartCompressor) extractExecutionState(segments []*TaskSegment) string {
	var lines []string

	for _, seg := range segments {
		for i := range seg.Messages {
			msg := &seg.Messages[i]
			// Tool calls from assistant
			if msg.Role == model.RoleAssistant && len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					args := truncate(string(tc.Function.Arguments), sc.maxToolArgsChars)
					lines = append(lines, fmt.Sprintf("- 调用: %s(%s)", tc.Function.Name, args))
				}
			}
			// Tool results
			if msg.Role == model.RoleTool && msg.Content != "" {
				result := truncate(msg.Content, sc.maxToolResultChars)
				lines = append(lines, fmt.Sprintf("  → 结果: %s", result))
			}
			// Async tool results injected as system messages (e.g., ActionTool tmux completion)
			// These contain "[action_tool_result]" prefix from handleStateChange.
			if msg.Role == model.RoleSystem && msg.Content != "" {
				if strings.Contains(msg.Content, "[action_tool_result]") {
					result := truncate(msg.Content, sc.maxToolResultChars)
					lines = append(lines, fmt.Sprintf("  → 异步结果: %s", result))
				}
			}
		}
	}

	if len(lines) == 0 {
		return ""
	}

	result := "[执行状态]\n" + strings.Join(lines, "\n")

	// Truncate to sc.maxExecStateChars, keeping the most recent entries
	if len(result) > sc.maxExecStateChars {
		// Find a clean cut point (start of a line) within the last sc.maxExecStateChars chars
		cutStart := len(result) - sc.maxExecStateChars
		// Find the first newline after cutStart to avoid cutting mid-line
		for cutStart < len(result) && result[cutStart] != '\n' {
			cutStart++
		}
		if cutStart < len(result) {
			result = "[执行状态]\n" + result[cutStart+1:]
		}
	}

	return result
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

// generateSummary generates an LLM summary of old task segments.
// batchIndex and totalBatches provide context for dynamic target sizing.
// Returns the summary string and whether an error occurred during LLM invocation.
// If summaryModel is nil, returns ("", false) meaning summary not attempted.
func (sc *SmartCompressor) generateSummary(
	ctx context.Context, segments []*TaskSegment, batchIndex, totalBatches int,
) (summary string, hadError bool) {
	if sc.summaryModel == nil {
		return "", false
	}

	// Build conversation content from old segments
	var contentBuilder strings.Builder
	for i, seg := range segments {
		contentBuilder.WriteString(fmt.Sprintf("--- 片段 %d ---\n", i+1))
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
			contentBuilder.WriteString(fmt.Sprintf("%s: %s\n", roleLabel, content))
		}
	}

	// Dynamic target calculation based on actual input and token budget
	inputChars := contentBuilder.Len()
	// Default compression ratio: 5x (target = 20% of input)
	targetChars := inputChars / 5
	// Cap: ensure all batch summaries fit within token budget.
	// Reserve space for system prompt + recent segments by dividing budget
	// across (totalBatches + 2) slots.
	maxCharsPerBatch := sc.maxTokens * 4 / (totalBatches + 2)
	if targetChars > maxCharsPerBatch {
		targetChars = maxCharsPerBatch
	}
	if targetChars < 200 {
		targetChars = 200 // minimum viable summary
	}

	// Build prompt with explicit engineering target
	var promptBuilder strings.Builder
	promptBuilder.WriteString(fmt.Sprintf(
		"请对以下 %d 个历史对话片段生成摘要。这是第 %d/%d 批。\n\n"+
			"工程要求：\n"+
			"1. 保留关键语义、用户意图、执行操作和最终结果\n"+
			"2. 保留工具调用的成功/失败状态和关键返回值（如文件路径、命令输出摘要）\n"+
			"3. 摘要目标长度：约 %d 字符（原始内容 %d 字符，压缩比 %.1fx）\n"+
			"4. 超出目标长度的部分必须省略，不可溢出\n"+
			"5. 使用简洁的要点式表达\n\n",
		len(segments), batchIndex, totalBatches,
		targetChars, inputChars, float64(inputChars)/float64(targetChars),
	))
	promptBuilder.WriteString(contentBuilder.String())
	promptBuilder.WriteString("\n--- 摘要 ---")

	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个对话摘要助手。严格按照用户指定的目标长度生成摘要。"),
			model.NewUserMessage(promptBuilder.String()),
		},
	}

	log.Debugf("[SmartCompress] batch %d/%d: inputChars=%d targetChars=%d ratio=%.1f",
		batchIndex, totalBatches, inputChars, targetChars, float64(inputChars)/float64(targetChars))

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

	log.Debugf("[SmartCompress] batch %d/%d: summary generated %d chars (target %d)",
		batchIndex, totalBatches, len(result), targetChars)

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

	counter := sc.tokenCounter
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
	totalBatches := len(batches)

	for i, batch := range batches {
		summary, hadError := sc.generateSummary(ctx, batch, i+1, totalBatches)
		if hadError || summary == "" {
			log.Warnf("[SmartCompress] batch %d/%d summary failed, skipping", i+1, totalBatches)
			continue
		}
		successCount++
		content := fmt.Sprintf("[摘要批次 %d/%d]\n%s", i+1, totalBatches, summary)
		result = append(result, model.NewSystemMessage(content))
	}

	if successCount == 0 {
		log.Warnf("[SmartCompress] all %d batch summaries failed", totalBatches)
		return nil, true
	}

	return result, false
}

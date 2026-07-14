package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// SmartCompressor performs deterministic context compression with optional
// LLM summary generation.
//
// Pipeline:
//  1. Segment messages by user-input boundaries.
//  2. Deterministic level assignment based on segment age and content type.
//  3. Per-segment compression: L0 (keep) / L1 (selective) / L2 (partial) / L3 (archive).
//  4. Assemble compressed context with inline notices.
//
// This is a "view transformation" — it modifies the messages sent to the LLM,
// but does NOT modify the Session or Projection.
type SmartCompressor struct {
	summaryModel    model.Model  // Optional: used for LLM summary generation
	KeepRecentTasks int          // Number of recent complete tasks to keep (default: 2)
	maxTokens       int          // Token budget for calculating batch size (default: DefaultMaxTokens)
	tokenCounter    TokenCounter // Token estimator (injected, not NewDefaultTokenCounter)

	// Configurable truncation parameters
	maxExecStateChars  int // Total execution state truncation (default: 2000)
	maxToolResultChars int // Per-tool-result truncation (default: 500)
	maxToolArgsChars   int // Per-tool-args truncation (default: 80)

	// Summary parameters
	chunkSummaryLen int                // Summary length per segment (default: 150)
	memStore        memory.MemoryStore // Optional: for summary archive
	projection      *SessionProjection // Optional: for archive EventReference append
}

// NewSmartCompressor creates a new SmartCompressor.
func NewSmartCompressor(opts ...SmartCompressorOption) *SmartCompressor {
	sc := &SmartCompressor{
		KeepRecentTasks:    2,
		tokenCounter:       NewDefaultTokenCounter(),
		maxTokens:          DefaultMaxTokens,
		maxExecStateChars:  2000,
		maxToolResultChars: 500,
		maxToolArgsChars:   80,
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

// WithChunkSummaryLen sets the summary length per segment.
func WithChunkSummaryLen(n int) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.chunkSummaryLen = n }
}

// WithMemStore injects a MemoryStore for summary archive persistence.
func WithMemStore(ms memory.MemoryStore) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.memStore = ms }
}

// WithProjection injects a SessionProjection for archive EventReference append.
func WithProjection(p *SessionProjection) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.projection = p }
}

func WithTokenCounter(tc TokenCounter) SmartCompressorOption {
	return func(sc *SmartCompressor) { sc.tokenCounter = tc }
}

// Compress implements budget-aware greedy compression with deterministic level assignment.
//
// Algorithm:
// 1. Segment messages by user input boundary
// 2. Compute excess = current_tokens - max_tokens (how much to cut)
// 3. Deterministic level assignment per segment (age + content type):
//   - Level 0: no compression (recent segments or last incomplete segment)
//   - Level 1: selective (preserve user input + key tools, compress non-key exec)
//   - Level 2: partial (preserve user input, compress all exec)
//   - Level 3: full (archive to MemoryStore + inline summary)
//
// 4. Per-segment LLM call for level 2 exec and level 1 non-key (degrade to first-stage on failure)
// 5. Assemble: chronological order with level-specific handling
func (sc *SmartCompressor) Compress(
	ctx context.Context,
	messages []model.Message,
	inv *agent.Invocation,
) []model.Message {
	startTime := time.Now()

	// 1. Separate system message
	systemMsg, rest := splitSystemMessage(messages)

	// 2. Segment by user input boundary
	segments := SegmentMessages(rest)

	// 3. Quick exit: not enough segments or under budget with few segments
	beforeTokens := sc.tokenCounter.Estimate(messages)
	minPreserve := sc.KeepRecentTasks
	if minPreserve < 1 {
		minPreserve = 1
	}
	// Only skip compression if: under token budget AND not too many segments
	if beforeTokens <= sc.maxTokens && len(segments) <= minPreserve {
		return messages
	}

	// excess: how much we need to cut (at least 1 if there are old segments)
	excess := beforeTokens - sc.maxTokens
	hasOldSegments := len(segments) > minPreserve
	if hasOldSegments && excess <= 0 {
		excess = 1 // Force compression of old segments beyond KeepRecentTasks
	}

	completeCount := 0
	for _, seg := range segments {
		if seg.IsComplete {
			completeCount++
		}
	}
	log.Debugf("[SmartCompress] split: segments=%d (complete=%d incomplete=%d) excess=%d msgs=%d",
		len(segments), completeCount, len(segments)-completeCount, excess, len(messages))

	// 4. Budget-aware greedy compression planning
	type segPlan struct {
		seg        *TaskSegment
		level      int // 0=none, 1=selective, 2=partial, 3=full
		userMsgs   []model.Message
		keyMsgs    []model.Message
		nonKeyMsgs []model.Message
		execMsgs   []model.Message
		origIndex  int // original index in segments slice (for chronological assembly)
	}

	plans := make([]segPlan, len(segments))
	for i, seg := range segments {
		userMsgs, execMsgs := splitUserAndExec(seg.Messages)
		keyMsgs, nonKeyMsgs := sc.selectiveSplit(execMsgs)
		plans[i] = segPlan{
			seg:        seg,
			level:      0,
			userMsgs:   userMsgs,
			keyMsgs:    keyMsgs,
			nonKeyMsgs: nonKeyMsgs,
			execMsgs:   execMsgs,
			origIndex:  i,
		}
	}

	// 5. Deterministic level assignment based on segment age and content features.
	// This replaces the LLM-based valuation with a pure, instant function.
	//
	// Level assignment rules:
	//   - Last segment or recent (within KeepRecentTasks): L0 (no compression)
	//   - Has user input AND age < keepRecent*2: L1 (selective: keep user + key tools)
	//   - Has user input OR age < keepRecent*3: L2 (partial: keep user, compress exec)
	//   - Old tool-only segments: L3 (full: archive to MemoryStore + inline summary)

	for i := range plans {
		isLast := i == len(plans)-1
		isLastIncomplete := isLast && !plans[i].seg.IsComplete
		isRecent := i >= len(plans)-minPreserve
		hasUserInput := len(plans[i].userMsgs) > 0

		if isRecent || isLastIncomplete {
			plans[i].level = 0 // L0: keep
			continue
		}

		age := len(plans) - 1 - i // 0=newest, larger=older

		if hasUserInput && age < minPreserve*2 {
			plans[i].level = 1 // L1: selective
		} else if hasUserInput || age < minPreserve*3 {
			plans[i].level = 2 // L2: partial
		} else {
			plans[i].level = 3 // L3: full archive
		}
	}

	// Fallback: if we're over budget but all segments are considered "recent"
	// (e.g., only 2 segments with keep_recent=2), force compression of the
	// oldest non-last segments until we can make progress.
	if excess > 0 && len(plans) > 1 {
		allRecent := true
		for i := range plans {
			if plans[i].level > 0 {
				allRecent = false
				break
			}
		}
		if allRecent {
			for i := 0; i < len(plans)-1; i++ {
				if len(plans[i].execMsgs) > 0 {
					plans[i].level = 2 // L2: summarize exec, keep user input
					log.Debugf("[SmartCompress] over budget but no candidates; force-compressing oldest segment")
					break
				}
			}
		}
	}

	// 6. Collect compressed segments for event info
	var compressedSegs []*TaskSegment
	var level3Segs []*TaskSegment
	for _, p := range plans {
		if p.level > 0 {
			compressedSegs = append(compressedSegs, p.seg)
		}
		if p.level == 3 {
			level3Segs = append(level3Segs, p.seg)
		}
	}

	// If no segments were compressed, return original messages.
	// Don't add a useless [context_compress] message that wastes tokens.
	if len(compressedSegs) == 0 {
		log.Debugf("[SmartCompress] no segments compressed — returning original")
		return messages
	}

	// 6. Execute LLM summarization (no silent timeout/skip — errors degrade to
	// first-stage and are reported via compress_error notices).
	//
	// Level 3: per-segment full summary. Older segments (beyond keep_recent_tasks)
	// are summarized inline so the conversation gist stays in context, instead of
	// being archived as opaque references that the LLM cannot see unless it calls
	// recall.
	level3Failed := make(map[int]bool)
	level3Summaries := make(map[int]string)
	for i, p := range plans {
		if p.level != 3 {
			continue
		}
		if sc.summaryModel != nil {
			summary, hadErr := sc.summarizeMsgs(ctx, p.seg.Messages, i, len(plans))
			if hadErr || summary == "" {
				level3Failed[i] = true
			} else {
				level3Summaries[i] = summary
			}
		} else {
			level3Failed[i] = true
		}
	}
	// L3 LLM failed → degrade to L1 first-stage (drop tool, keep text)
	for i := range level3Failed {
		plans[i].level = 1
	}

	// Level 2 & 1: per-segment LLM summarization.
	// On failure, degrade to firstStageCompress (drop tool, keep text).
	level2Failed := make(map[int]bool)
	level1Failed := make(map[int]bool)
	level2Summaries := make(map[int]string)
	level1Summaries := make(map[int]string)
	for i, p := range plans {
		switch p.level {
		case 2:
			summary, hadErr := sc.summarizeMsgs(ctx, p.execMsgs, i, len(plans))
			if hadErr || summary == "" {
				level2Failed[i] = true
			} else {
				level2Summaries[i] = summary
			}
		case 1:
			summary, hadErr := sc.summarizeMsgs(ctx, p.nonKeyMsgs, i, len(plans))
			if hadErr || summary == "" {
				level1Failed[i] = true
			} else {
				level1Summaries[i] = summary
			}
		}
	}

	// 7. Assemble result (maintain chronological order)
	var result []model.Message
	if systemMsg != nil {
		result = append(result, *systemMsg)
	}

	// Track degraded segments for a combined error notice, injected at top
	// (before segments) so the agent sees it immediately and the pending user
	// message remains the last message in the result.
	degradedCount := 0
	for i := range plans {
		if level2Failed[i] || level1Failed[i] {
			degradedCount++
		}
	}
	if degradedCount > 0 {
		result = append(result, buildCompressErrorNotice("LLM 摘要生成失败", degradedCount))
	}

	// Chronological assembly
	for i, p := range plans {
		switch p.level {
		case 0:
			result = append(result, p.seg.Messages...)
		case 1:
			result = append(result, p.userMsgs...)
			result = append(result, p.keyMsgs...)
			if summary, ok := level1Summaries[i]; ok && summary != "" {
				result = append(result, sc.buildSegmentCompressNotice(p.nonKeyMsgs, "selective"))
				result = append(result, model.Message{
					Role: model.RoleAssistant, Content: summary,
				})
			} else {
				// LLM failed → first-stage: drop tool, keep text
				result = append(result, sc.firstStageCompress(p.nonKeyMsgs)...)
			}
		case 2:
			result = append(result, p.userMsgs...)
			if summary, ok := level2Summaries[i]; ok && summary != "" {
				result = append(result, sc.buildSegmentCompressNotice(p.execMsgs, "partial"))
				result = append(result, model.Message{
					Role: model.RoleAssistant, Content: summary,
				})
			} else {
				// LLM failed → first-stage: drop tool, keep text
				result = append(result, sc.firstStageCompress(p.execMsgs)...)
			}
		case 3:
			// Level 3: replace the entire old segment with an inline summary.
			// The segment is also archived so recall can retrieve full details.
			summary := level3Summaries[i]
			// Extract event key from first message of the segment
			segEventKey := int64(0)
			if len(p.seg.Messages) > 0 {
				k, _, _ := parseEventKeyAndType(p.seg.Messages[0].Content)
				segEventKey = k
			}
			if sc.memStore != nil {
				summaryKey, archiveErr := sc.archiveSegment(p.seg, summary)
				if archiveErr == nil {
					result = append(result, model.NewSystemMessage(
						fmt.Sprintf("[context_archive] evt_%d 已摘要归档，摘要 key=%d", segEventKey, summaryKey),
					))
				} else {
					log.Warnf("[SmartCompress] archive failed for segment %d: %v", p.origIndex, archiveErr)
				}
			}
			if summary != "" {
				result = append(result, model.Message{
					Role:    model.RoleAssistant,
					Content: fmt.Sprintf("[历史摘要] %s", summary),
				})
			}
		}
	}

	// 8. Metrics
	afterTokens := sc.tokenCounter.Estimate(result)
	levelCounts := map[int]int{0: 0, 1: 0, 2: 0, 3: 0}
	for _, p := range plans {
		levelCounts[p.level]++
	}
	metrics := map[string]interface{}{
		"event":             "smart_compress",
		"before_tokens":     beforeTokens,
		"after_tokens":      afterTokens,
		"excess":            excess,
		"l0_preserved":      levelCounts[0],
		"l1_selective":      levelCounts[1],
		"l2_partial":        levelCounts[2],
		"l3_full":           levelCounts[3],
		"summary_generated": sc.summaryModel != nil && len(level3Summaries) > 0,
		"batch_count":       0,
		"degraded_count":    degradedCount,
		"duration_ms":       time.Since(startTime).Milliseconds(),
	}
	if metricsJSON, err := json.Marshal(metrics); err == nil {
		log.Infof("[SmartCompress] %s tokens=%d->%d (%+d) levels=L0:%d L1:%d L2:%d L3:%d",
			string(metricsJSON), beforeTokens, afterTokens, afterTokens-beforeTokens,
			levelCounts[0], levelCounts[1], levelCounts[2], levelCounts[3])
	}

	return result
}

// splitUserAndExec splits messages into user (RoleUser) and execution (everything else).
func splitUserAndExec(msgs []model.Message) (user, exec []model.Message) {
	for _, msg := range msgs {
		if msg.Role == model.RoleUser {
			user = append(user, msg)
		} else {
			exec = append(exec, msg)
		}
	}
	return
}

// selectiveSplit identifies key messages (worth preserving) and non-key messages
// (compressible) within an execution process.
//
// Key messages (preserved at compression level 1):
//   - Tool calls (assistant with ToolCalls): preserve function name + args
//   - Tool results: must be kept in sync with their tool calls
//     (assistant tool_call + tool result is an atomic pair)
//   - Error results (tool results containing "Error")
//   - Async tool results (system messages with [action_tool_result])
//   - Short messages (< 100 chars, not worth compressing)
//
// Important: tool_call and tool_result pairs are atomic — both are key or both are non-key.
func (sc *SmartCompressor) selectiveSplit(execMsgs []model.Message) (key, nonKey []model.Message) {
	// First pass: identify which messages are tool_call/tool_result pairs
	// and decide per-pair whether they are key or non-key.
	isKey := make([]bool, len(execMsgs))

	for i := 0; i < len(execMsgs); i++ {
		msg := &execMsgs[i]

		// Tool call (assistant with ToolCalls)
		if msg.Role == model.RoleAssistant && len(msg.ToolCalls) > 0 {
			// Find the corresponding tool result(s)
			resultStart := i + 1
			resultEnd := resultStart
			for resultEnd < len(execMsgs) && execMsgs[resultEnd].Role == model.RoleTool {
				resultEnd++
			}

			// Determine if this tool_call+result pair is key
			pairIsKey := false

			// Short tool calls (content < 100) are key
			if len(msg.Content) < 100 {
				pairIsKey = true
			}

			// Check results for errors or short content
			for j := resultStart; j < resultEnd; j++ {
				result := &execMsgs[j]
				if strings.Contains(result.Content, "Error") {
					pairIsKey = true
				}
				if len(result.Content) < 100 {
					pairIsKey = true
				}
			}

			// If pair is non-key, check if it's the last tool result in the segment
			// (last result is always key — it's the most relevant)
			if !pairIsKey && resultEnd > resultStart {
				lastResultIdx := -1
				for k := len(execMsgs) - 1; k >= 0; k-- {
					if execMsgs[k].Role == model.RoleTool {
						lastResultIdx = k
						break
					}
				}
				if lastResultIdx >= resultStart && lastResultIdx < resultEnd {
					pairIsKey = true
				}
			}

			// Mark the entire pair
			isKey[i] = pairIsKey
			for j := resultStart; j < resultEnd; j++ {
				isKey[j] = pairIsKey
			}

			// Skip past the results
			i = resultEnd - 1
			continue
		}

		// Non tool_call messages
		// Async tool results are key
		if msg.Role == model.RoleSystem && strings.Contains(msg.Content, "[action_tool_result]") {
			isKey[i] = true
			continue
		}
		// Short messages are key
		if len(msg.Content) < 100 {
			isKey[i] = true
			continue
		}
		// Default: non-key (compressible)
		isKey[i] = false
	}

	// Second pass: split
	for i, msg := range execMsgs {
		if isKey[i] {
			key = append(key, msg)
		} else {
			nonKey = append(nonKey, msg)
		}
	}
	return
}

// summarizeMsgs summarizes a list of messages using the summary model.
// Returns (summary, hadError). hadError=true means the LLM call failed;
// caller should degrade to firstStageCompress and inject an error notice.
func (sc *SmartCompressor) summarizeMsgs(ctx context.Context, msgs []model.Message, segIdx, totalSegs int) (string, bool) {
	if len(msgs) == 0 {
		return "", false
	}

	if sc.summaryModel == nil {
		return "", true // no model available → treat as error so caller degrades
	}

	seg := &TaskSegment{Messages: msgs}
	summary, hadError := sc.generateSummary(ctx, []*TaskSegment{seg}, segIdx+1, totalSegs)
	if hadError {
		return "", true
	}
	if summary == "" {
		return "", true // empty summary is treated as failure
	}
	return summary, false
}

// firstStageCompress drops all tool-related messages from execMsgs, keeping only
// the text content of assistant and user messages. This is the "Stage 1" fallback
// strategy defined in the original smart-compress design: when LLM summarization
// is unavailable or fails, we still reduce token usage by removing bulky tool
// calls and tool results, while preserving the conversational thread.
//
// Messages dropped:
//   - assistant messages whose ONLY content is tool_calls (no text)
//   - tool result messages (role=tool)
//   - system messages containing [action_tool_result]
//
// Messages preserved (with tool_calls stripped):
//   - assistant messages with non-empty text content
//   - user messages
func (sc *SmartCompressor) firstStageCompress(execMsgs []model.Message) []model.Message {
	var result []model.Message
	for _, msg := range execMsgs {
		switch msg.Role {
		case model.RoleAssistant:
			if len(msg.ToolCalls) > 0 && msg.Content == "" {
				// Pure tool-call message with no text — drop
				continue
			}
			// Keep text, strip tool_calls
			if msg.Content != "" {
				result = append(result, model.Message{
					Role:    model.RoleAssistant,
					Content: msg.Content,
				})
			}
		case model.RoleUser:
			result = append(result, msg)
		case model.RoleTool:
			// Drop tool results
		case model.RoleSystem:
			if strings.Contains(msg.Content, "[action_tool_result]") {
				// Drop async tool results
			} else {
				result = append(result, msg)
			}
		}
	}
	return result
}

// buildCompressErrorNotice creates a system message that notifies the agent
// that compression encountered errors and degraded to first-stage strategy.
// This ensures the agent is aware of the information loss and can react
// accordingly (e.g., ask the user for clarification, recall specific events).
func buildCompressErrorNotice(reason string, degradedCount int) model.Message {
	content := fmt.Sprintf(
		"[context_compress_error] 上下文压缩遇到错误，已降级为第一阶段策略。\n"+
			"原因: %s\n"+
			"受影响片段: %d\n"+
			"降级策略: 丢弃工具调用记录，仅保留对话文本。\n"+
			"如需历史工具调用结果，请使用 recall 工具按 event_key 检索。",
		reason, degradedCount,
	)
	return model.NewSystemMessage(content)
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

// buildSegmentCompressNotice creates an inline compress notice for a single segment.
// Only lists exec message keys (user input is preserved, not listed here).
// Placed AFTER user input in the assembled messages.
//
// The notice is capped in size: it lists at most maxNoticeInfos unique event keys
// and truncates the whole text to maxNoticeChars. This prevents the notice itself
// from becoming a source of token bloat when a segment contains many exec messages.
//
// Warns the agent not to repeat expensive operations that were compressed —
// regardless of whether the cause was file reads, model outputs, search results, etc.
func (sc *SmartCompressor) buildSegmentCompressNotice(execMsgs []model.Message, level string) model.Message {
	const maxNoticeInfos = 5
	const maxNoticeChars = 800

	levelDesc := map[string]string{
		"selective": "选择性压缩",
		"partial":   "部分压缩",
		"full":      "全压缩",
	}
	desc := levelDesc[level]
	if desc == "" {
		desc = level
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("[context_compress] %s: 压缩了 %d 条执行消息", desc, len(execMsgs)))

	// Extract event keys from exec messages only
	seen := make(map[int64]bool)
	var infos []EventInfo

	for _, msg := range execMsgs {
		key, evtType, remainder := parseEventKeyAndType(msg.Content)
		if key > 0 && !seen[key] {
			seen[key] = true
			summary := truncate(remainder, sc.chunkSummaryLen)
			if summary == "" {
				summary = truncate(msg.Content, sc.chunkSummaryLen)
			}
			infos = append(infos, EventInfo{
				Key: key, Type: evtType, Summary: summary,
			})
			if len(infos) >= maxNoticeInfos {
				break
			}
		}
	}

	if len(infos) > 0 {
		content.WriteString("\n")
		for _, info := range infos {
			content.WriteString(fmt.Sprintf("\n- evt_%d [%s]: %s", info.Key, info.Type, info.Summary))
		}
		if len(execMsgs) > len(infos) {
			content.WriteString(fmt.Sprintf("\n... 还有 %d 条执行消息被压缩", len(execMsgs)-len(infos)))
		}

		// General warning: don't repeat operations that produced the compressed content.
		// The overflow cause might be anything — large files, model outputs, search results, etc.
		// The agent should not blindly re-execute the same operations to recover this information.
		content.WriteString("\n\n**不要重复执行已被压缩的操作来获取相同内容**，否则将再次触发压缩。如需查找特定信息，请使用：")
		content.WriteString("\n- `recall({\"event_keys\": [KEY]})` — 检索完整事件（如果可用）")
		content.WriteString("\n- `search_content({\"path\": \"<path>\", \"query\": \"<关键词>\"})` — 搜索特定内容")
		content.WriteString("\n- `read_file` 配合 `start_line`/`end_line` 参数 — 只读取需要的部分")
	}

	notice := content.String()
	if len(notice) > maxNoticeChars {
		notice = notice[:maxNoticeChars] + "...(通知已截断)"
	}
	return model.NewSystemMessage(notice)
}

// ============================================================================
// Archive: Summary-Memory RAG Integration
// ============================================================================

// archiveSegment writes a summary of the given segment to MemoryStore and
// returns the summary EventKey. Each call creates a new archive (no dedup cache).
func (sc *SmartCompressor) archiveSegment(seg *TaskSegment, summary string) (int64, error) {
	if sc.memStore == nil {
		return 0, fmt.Errorf("archiveSegment: memStore is nil")
	}

	// Determine partition ID from segment's first event key
	partitionID := memory.NewPartitionID()
	if len(seg.Messages) > 0 {
		k, _, _ := parseEventKeyAndType(seg.Messages[0].Content)
		if k > 0 {
			partitionID = memory.PartitionIDFromEventKey(k)
		}
	}

	// Generate new Snowflake key for the summary event
	summaryKey := memory.NewSnowflakeEventKey(partitionID, 0)

	// Build summary event
	summaryEvent := memory.FullEvent{
		EventKey:     summaryKey,
		PartitionID:  partitionID,
		EventType:    "context_compress_summary",
		EventSummary: summary,
		Content:      summary,
		Timestamp:    time.Now().UnixMilli(),
		Metadata: map[string]string{
			"content_hash": sc.segmentContentHash(seg),
		},
	}

	if err := sc.memStore.StoreEvent(summaryKey, summaryEvent); err != nil {
		return 0, fmt.Errorf("archiveSegment: StoreEvent failed: %w", err)
	}

	log.Infof("[SmartCompress] archived segment to summaryKey=%d", summaryKey)
	return summaryKey, nil
}

// segmentContentHash computes a stable hash of the segment's message contents.
func (sc *SmartCompressor) segmentContentHash(seg *TaskSegment) string {
	h := fnv.New64a()
	for _, msg := range seg.Messages {
		h.Write([]byte(msg.Content))
		h.Write([]byte{0}) // separator
	}
	return fmt.Sprintf("%x", h.Sum64())
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
			// Plain assistant/user messages without tool calls: keep a short preview so
			// the summary is never empty and we don't fall back to the full message.
			if (msg.Role == model.RoleAssistant || msg.Role == model.RoleUser) && msg.Content != "" && len(msg.ToolCalls) == 0 {
				label := roleLabel(msg.Role)
				preview := truncate(msg.Content, sc.maxToolResultChars)
				lines = append(lines, fmt.Sprintf("- %s: %s", label, preview))
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

// roleLabel returns a human-readable label for a model.Role.
func roleLabel(role model.Role) string {
	switch role {
	case model.RoleSystem:
		return "system"
	case model.RoleUser:
		return "user"
	case model.RoleAssistant:
		return "assistant"
	case model.RoleTool:
		return "tool"
	default:
		return "unknown"
	}
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
//
// If the LLM returns a summary exceeding targetChars * 1.5, the segments are
// split into two sub-batches and each is summarized independently with halved
// targetChars. Recursion depth is limited to 2. If still oversized at the
// limit, the result is hard-truncated to targetChars.
func (sc *SmartCompressor) generateSummary(
	ctx context.Context, segments []*TaskSegment, batchIndex, totalBatches int,
) (summary string, hadError bool) {
	return sc.generateSummaryRecursive(ctx, segments, batchIndex, totalBatches, 0)
}

// generateSummaryRecursive is the recursive implementation with depth tracking.
func (sc *SmartCompressor) generateSummaryRecursive(
	ctx context.Context, segments []*TaskSegment, batchIndex, totalBatches, depth int,
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

	log.Debugf("[SmartCompress] batch %d/%d (depth=%d): inputChars=%d targetChars=%d ratio=%.1f",
		batchIndex, totalBatches, depth, inputChars, targetChars, float64(inputChars)/float64(targetChars))

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

	log.Debugf("[SmartCompress] batch %d/%d (depth=%d): summary generated %d chars (target %d)",
		batchIndex, totalBatches, depth, len(result), targetChars)

	// Check if summary exceeds target with 50% tolerance.
	// If so, split segments and re-summarize each half independently.
	if len(result) > targetChars*3/2 && depth < 2 && len(segments) > 1 {
		log.Warnf("[SmartCompress] batch %d/%d summary %d chars exceeds target %d*1.5=%d, splitting into sub-batches (depth=%d)",
			batchIndex, totalBatches, len(result), targetChars, targetChars*3/2, depth)

		mid := len(segments) / 2
		if mid < 1 {
			mid = 1
		}
		left := segments[:mid]
		right := segments[mid:]

		var leftSummary, rightSummary string
		var leftErr, rightErr bool

		leftSummary, leftErr = sc.generateSummaryRecursive(ctx, left, batchIndex, totalBatches, depth+1)
		if !leftErr && len(right) > 0 {
			rightSummary, rightErr = sc.generateSummaryRecursive(ctx, right, batchIndex, totalBatches, depth+1)
		}

		if leftErr && rightErr {
			// Both failed — fall through to hard truncation below
		} else if !leftErr && !rightErr {
			result = leftSummary + "\n\n" + rightSummary
		} else if !leftErr {
			result = leftSummary
		} else {
			result = rightSummary
		}

		log.Debugf("[SmartCompress] batch %d/%d (depth=%d): re-summarized result %d chars",
			batchIndex, totalBatches, depth, len(result))
	}

	// Final hard truncation if still over target
	if len(result) > targetChars*3/2 {
		log.Warnf("[SmartCompress] batch %d/%d summary still %d chars after re-summarization, hard truncating to %d",
			batchIndex, totalBatches, len(result), targetChars)
		result = result[:targetChars] + "...(摘要已截断)"
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
// Each successful summary is wrapped as an assistant message with batch numbering.
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
		result = append(result, model.Message{
			Role:    model.RoleAssistant,
			Content: content,
		})
	}

	if successCount == 0 {
		log.Warnf("[SmartCompress] all %d batch summaries failed", totalBatches)
		return nil, true
	}

	return result, false
}

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ProcessingStrategy represents the recommended handling strategy for a compressed event.
// It describes what operation should be performed on the event to balance information
// preservation and token savings.
type ProcessingStrategy string

const (
	// Keep: retain the event unchanged in context. Used for high-value events.
	Keep ProcessingStrategy = "keep"

	// Truncate: keep a truncated version of the event. Used for moderately valuable events.
	Truncate ProcessingStrategy = "truncate"

	// KeyFacts: extract structured key facts, discard verbose details.
	KeyFacts ProcessingStrategy = "keyfacts"

	// Summary: replace with an LLM-generated summary.
	Summary ProcessingStrategy = "summary"

	// Reference: replace with a lightweight reference key, archive full content in MemoryStore.
	Reference ProcessingStrategy = "reference"

	// Drop: discard the event entirely. Used for events with near-zero information value.
	Drop ProcessingStrategy = "drop"
)

// DefaultProcessing is the fallback strategy when the LLM does not provide one.
const DefaultProcessing = Summary

// EventValue holds the valuation result for a single event/segment.
// Produced by EventValuator and consumed by SmartCompressor's planning phase.
type EventValue struct {
	// EventKey is the Snowflake key identifying this event in MemoryStore.
	EventKey int64

	// ValueScore is the information value of this event for the current task context,
	// in the range [0.0, 1.0]. Higher = more valuable.
	// 0 = discardable, 1 = must preserve.
	ValueScore float64

	// Processing is the recommended strategy for compressing this event.
	Processing ProcessingStrategy

	// KeyFacts is a concise bullet-point summary of the most important facts
	// (max ~200 chars). Used as the inline preview when the event is compressed.
	KeyFacts string

	// Reason is an optional free-text explanation of why the valuator assigned
	// this score and strategy. Useful for debugging and audit.
	Reason string
}

// EventValuator evaluates a batch of task segments and returns per-segment
// valuation plus a batch-level summary.
//
// The interface is deliberately batch-oriented so implementations can make a
// single LLM call to evaluate all segments at once.
type EventValuator interface {
	// Evaluate takes a slice of segments and returns:
	//   - values: one EventValue per input segment (same length)
	//   - batchSummary: a free-text summary of the whole batch
	//   - error: non-nil if the valuation failed entirely (caller should fall back)
	Evaluate(ctx context.Context, segments []*TaskSegment) (values []EventValue, batchSummary string, err error)
}

// ValuationConfig holds configuration for the LLM-based event valuator.
type ValuationConfig struct {
	// ValueFloors maps event type strings to minimum value_score.
	// If the LLM assigns a score below the floor for a given type, the floor is applied.
	// Example: {"external_input": 0.5, "agent_output": 0.4}
	ValueFloors map[string]float64

	// Timeout is the maximum wall-clock time allowed for the entire valuation
	// phase (including all LLM calls). Zero means no timeout.
	Timeout time.Duration
}

// DefaultValuationFloors returns the default per-event-type value floors.
func DefaultValuationFloors() map[string]float64 {
	return map[string]float64{
		"external_input": 0.5,
		"agent_output":   0.4,
	}
}

// noopValuator is the default EventValuator when no LLM valuator is configured.
// It returns uniform mid-value with Summary processing for every segment.
type noopValuator struct{}

// Evaluate returns default valuations without calling any LLM.
func (noopValuator) Evaluate(_ context.Context, segments []*TaskSegment) ([]EventValue, string, error) {
	values := make([]EventValue, len(segments))
	for i, seg := range segments {
		key := int64(0)
		if len(seg.Messages) > 0 {
			k, _, _ := parseEventKeyAndType(seg.Messages[0].Content)
			key = k
		}
		values[i] = EventValue{
			EventKey:   key,
			ValueScore: 0.5,
			Processing: Summary,
		}
	}
	return values, "", nil
}

// NewNoopValuator creates a pass-through EventValuator that does not call an LLM.
// Used when no summary model is configured.
func NewNoopValuator() EventValuator {
	return noopValuator{}
}

// ============================================================================
// LLMEventValuator
// ============================================================================

// LLMEventValuator uses an LLM to evaluate segments and produce a batch summary
// in a single call. The LLM response is expected to contain:
//  1. A JSON array of per-segment valuations (event_key, value_score, processing, key_facts, reason).
//  2. A free-text batch summary after a `--- BATCH SUMMARY ---` separator.
type LLMEventValuator struct {
	model  model.Model
	config ValuationConfig
}

// NewLLMEventValuator creates an LLM-based EventValuator.
func NewLLMEventValuator(m model.Model, cfg ValuationConfig) EventValuator {
	return &LLMEventValuator{model: m, config: cfg}
}

// valuationPrompt is the system+user prompt sent to the LLM for batch valuation.
const valuationPrompt = `你是一个上下文压缩评估助手。请对以下对话片段进行评估，为每个片段输出 JSON 格式的评估结果。

要求：
1. 对每个片段输出：
   - event_key: int64, 该片段的标识 key（从片段内容中提取）
   - value_score: float64 (0.0~1.0), 该片段对当前任务的信息价值
   - processing: string, 推荐处理方式，可选值：keep/truncate/keyfacts/summary/reference/drop
   - key_facts: string, 关键事实摘要（不超过200字符）
   - reason: string, 评分理由（可选）

2. 所有片段的评估结果输出为一个 JSON 数组。

3. 在 JSON 数组之后，输出分隔符 "--- BATCH SUMMARY ---"，然后输出对整个批次的整体摘要（纯文本，中文）。

价值判断标准：
- 1.0: 用户原始需求、关键决策、必须保留的约束
- 0.7-0.9: 重要的工具调用结果、中间推理
- 0.3-0.6: 一般性回复、常规操作
- 0.1-0.2: 冗余信息、失败重试的中间步骤
- 0.0: 可完全丢弃的噪声

处理方式选择：
- keep: 必须原样保留（value_score ≥ 0.8 且对后续任务有直接引用价值）
- truncate: 可截断（中等价值，保留前200字符即可）
- keyfacts: 提取关键事实（有结构化信息，摘要即可）
- summary: LLM 生成摘要（需要理解后压缩）
- reference: 替换为引用 key，存入长期记忆（大文件内容、完整搜索结果）
- drop: 直接丢弃（value_score ≤ 0.1）`

// Evaluate calls the LLM to valuate all segments and produce a batch summary.
func (v *LLMEventValuator) Evaluate(ctx context.Context, segments []*TaskSegment) ([]EventValue, string, error) {
	if v.model == nil {
		return nil, "", fmt.Errorf("LLMEventValuator: model is nil")
	}

	// Build the content for the LLM request
	var contentBuilder strings.Builder
	for i, seg := range segments {
		contentBuilder.WriteString(fmt.Sprintf("\n--- 片段 %d ---\n", i+1))
		for _, msg := range seg.Messages {
			roleLabel := roleLabel(msg.Role)
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

	prompt := fmt.Sprintf("%s\n\n%s", valuationPrompt, contentBuilder.String())

	// Apply timeout: default 10s if not configured. The valuation LLM call
	// should be fast — if it takes more than 10s, the context is likely too
	// large and the caller will degrade to first-stage compression anyway.
	timeout := v.config.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	evalCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个对话压缩评估助手。严格按照要求的 JSON 格式输出评估结果。"),
			model.NewUserMessage(prompt),
		},
	}

	respCh, err := v.model.GenerateContent(evalCtx, req)
	if err != nil {
		return nil, "", fmt.Errorf("LLMEventValuator: GenerateContent failed: %w", err)
	}

	var result string
	for resp := range respCh {
		if resp.Error != nil {
			return nil, "", fmt.Errorf("LLMEventValuator: response error: %w", resp.Error)
		}
		if len(resp.Choices) > 0 {
			result += resp.Choices[0].Message.Content
		}
	}

	// Parse response: JSON array + optional batch summary
	values, batchSummary, err := parseValuationResponse(result)
	if err != nil {
		// Include the full LLM response in the error for debugging
		responsePreview := result
		if len(responsePreview) > 500 {
			responsePreview = responsePreview[:500] + "..."
		}
		return nil, "", fmt.Errorf("LLMEventValuator: parse response: %w (response: %q)", err, responsePreview)
	}

	// Ensure one value per segment
	if len(values) < len(segments) {
		log.Warnf("[EventValuator] LLM returned %d values for %d segments, padding with defaults", len(values), len(segments))
		for len(values) < len(segments) {
			values = append(values, EventValue{ValueScore: 0.5, Processing: DefaultProcessing})
		}
	} else if len(values) > len(segments) {
		values = values[:len(segments)]
	}

	// Fill in missing event keys from segment messages
	for i, seg := range segments {
		if values[i].EventKey == 0 && len(seg.Messages) > 0 {
			k, _, _ := parseEventKeyAndType(seg.Messages[0].Content)
			values[i].EventKey = k
		}
	}

	// Apply value floors based on event type
	v.applyValueFloors(segments, values)

	return values, batchSummary, nil
}

// applyValueFloors clamps value scores to the configured minimum for each event type.
func (v *LLMEventValuator) applyValueFloors(segments []*TaskSegment, values []EventValue) {
	for i, seg := range segments {
		if i >= len(values) {
			break
		}
		// Determine event type from the first message with a prefix
		evtType := "unknown"
		for _, msg := range seg.Messages {
			_, et, _ := parseEventKeyAndType(msg.Content)
			if et != "unknown" {
				evtType = et
				break
			}
		}
		if floor, ok := v.config.ValueFloors[evtType]; ok {
			if values[i].ValueScore < floor {
				values[i].ValueScore = floor
			}
		}
	}
}

// parseValuationResponse splits the LLM response into a JSON valuation array and
// a free-text batch summary separated by "--- BATCH SUMMARY ---".
func parseValuationResponse(response string) ([]EventValue, string, error) {
	const separator = "--- BATCH SUMMARY ---"

	// Split on separator
	var jsonPart, batchSummary string
	if idx := strings.Index(response, separator); idx >= 0 {
		jsonPart = strings.TrimSpace(response[:idx])
		batchSummary = strings.TrimSpace(response[idx+len(separator):])
	} else {
		jsonPart = strings.TrimSpace(response)
	}

	// Try to extract JSON array from the response
	jsonPart = extractJSONArray(jsonPart)
	if jsonPart == "" {
		return nil, batchSummary, fmt.Errorf("no JSON array found in response")
	}

	// Parse JSON
	type rawValue struct {
		EventKey   int64   `json:"event_key"`
		ValueScore float64 `json:"value_score"`
		Processing string  `json:"processing"`
		KeyFacts   string  `json:"key_facts"`
		Reason     string  `json:"reason"`
	}

	var raw []rawValue
	if err := json.Unmarshal([]byte(jsonPart), &raw); err != nil {
		return nil, batchSummary, fmt.Errorf("JSON unmarshal: %w", err)
	}

	values := make([]EventValue, len(raw))
	for i, r := range raw {
		proc := ProcessingStrategy(r.Processing)
		switch proc {
		case Keep, Truncate, KeyFacts, Summary, Reference, Drop:
			// valid
		default:
			proc = DefaultProcessing
		}
		values[i] = EventValue{
			EventKey:   r.EventKey,
			ValueScore: clamp(r.ValueScore, 0, 1),
			Processing: proc,
			KeyFacts:   r.KeyFacts,
			Reason:     r.Reason,
		}
	}

	return values, batchSummary, nil
}

// extractJSONArray attempts to find a JSON array in the given text.
// It looks for the first '[' and finds its matching ']'.
func extractJSONArray(text string) string {
	start := strings.IndexByte(text, '[')
	if start < 0 {
		return ""
	}
	// Find matching ']'
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

// clamp restricts v to the range [lo, hi].
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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

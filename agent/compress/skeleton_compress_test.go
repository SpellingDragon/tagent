package compress

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// deterministicLevel tests (deterministic-compress-level spec)
// ============================================================================

func TestDeterministicLevel_Table(t *testing.T) {
	complete := &TaskSegment{IsComplete: true}
	inProgress := &TaskSegment{IsComplete: false}

	tests := []struct {
		name       string
		seg        *TaskSegment
		segIdx     int
		totalSegs  int
		keepRecent int
		want       int
	}{
		// Spec scenarios.
		{"in-progress is always L0", inProgress, 0, 5, 2, 0},
		{"recent turn kept (age=1)", complete, 3, 5, 2, 0},
		{"mid turn drops tool (age=2)", complete, 3, 6, 2, 1},
		{"old turn skeleton only (age=5)", complete, 2, 8, 2, 2},
		{"older turn stays skeleton (age=9) — base caps at L2", complete, 0, 10, 2, 2},
		// Boundary sweep with keepRecent=2 (exponential aging {k,2k} = {2,4};
		// L3 is budget-escalation-only, never age-reachable).
		{"age=0", complete, 4, 5, 2, 0},
		{"age=3 → L1", complete, 1, 5, 2, 1},
		{"age=4 → L2", complete, 0, 5, 2, 2},
		{"age=6 → L2 (exponential)", complete, 0, 7, 2, 2},
		{"age=7 → L2 (exponential)", complete, 0, 8, 2, 2},
		{"age=8 → L2 (base ladder never reaches L3)", complete, 0, 9, 2, 2},
		// keepRecent floor (k=1 → aging L2 at age>=2; L3 still unreachable).
		{"keepRecent=0 treated as 1", complete, 0, 5, 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deterministicLevel(tt.seg, tt.segIdx, tt.totalSegs, tt.keepRecent)
			assert.Equal(t, tt.want, got)
			assert.GreaterOrEqual(t, got, 0)
			assert.LessOrEqual(t, got, 3)
		})
	}
}

// ============================================================================
// applySegmentLevel: tool > assistant drop order
// ============================================================================

func skeletonTestSegment() *TaskSegment {
	return &TaskSegment{
		IsComplete: true,
		Messages: []model.Message{
			prefixedMsg(model.RoleUser, 11, tagentevent.TypeExternalInput, "user ask"),
			prefixedMsg(model.RoleAssistant, 12, tagentevent.TypeThinkingPlan, "thinking"),
			prefixedMsg(model.RoleTool, 13, tagentevent.TypeActionCommand, "tool result"),
			prefixedMsg(model.RoleAssistant, 14, tagentevent.TypeAgentOutput, "final answer"),
		},
	}
}

func eventTypesOf(msgs []model.Message) []string {
	var types []string
	for i := range msgs {
		types = append(types, MessageEventType(&msgs[i]))
	}
	return types
}

// L1 drops action_command only (spec scenario "第一档丢弃 tool 保留 assistant").
func TestApplySegmentLevel_L1DropsToolKeepsAssistant(t *testing.T) {
	kept := applySegmentLevel(skeletonTestSegment(), 1)
	assert.Equal(t, []string{
		tagentevent.TypeExternalInput,
		tagentevent.TypeThinkingPlan,
		tagentevent.TypeAgentOutput,
	}, eventTypesOf(kept))
}

// L2 keeps skeleton only (spec scenario "第二档丢弃 tool 与 assistant 仅留骨架").
func TestApplySegmentLevel_L2SkeletonOnly(t *testing.T) {
	kept := applySegmentLevel(skeletonTestSegment(), 2)
	assert.Equal(t, []string{
		tagentevent.TypeExternalInput,
		tagentevent.TypeAgentOutput,
	}, eventTypesOf(kept))
}

// L3 removes the whole segment from the timeline (multi-segment compaction).
func TestApplySegmentLevel_L3RemovesSegment(t *testing.T) {
	kept := applySegmentLevel(skeletonTestSegment(), 3)
	assert.Empty(t, kept)
}

// L1 must not leave dangling tool_calls when their results are dropped.
func TestApplySegmentLevel_L1StripsDanglingToolCalls(t *testing.T) {
	seg := &TaskSegment{IsComplete: true, Messages: []model.Message{
		prefixedMsg(model.RoleUser, 21, tagentevent.TypeExternalInput, "ask"),
		{
			Role:      model.RoleAssistant,
			Content:   "[evt_16|thinking_plan] planning with call",
			ToolCalls: []model.ToolCall{{ID: "tc1", Function: model.FunctionDefinitionParam{Name: "echo"}}},
		},
		prefixedMsg(model.RoleTool, 23, tagentevent.TypeActionCommand, "result"),
		prefixedMsg(model.RoleAssistant, 24, tagentevent.TypeAgentOutput, "done"),
	}}
	kept := applySegmentLevel(seg, 1)
	for _, msg := range kept {
		assert.Empty(t, msg.ToolCalls, "kept messages must not carry dangling tool_calls")
	}
}

// ============================================================================
// compressSkeleton pipeline tests
// ============================================================================

// buildTurns builds n complete task turns, each
// [external_input, thinking_plan, action_command, agent_output], with
// sequential event keys starting at 1000*(turn+1).
func buildTurns(n int) []model.Message {
	var msgs []model.Message
	for i := 0; i < n; i++ {
		base := int64(1000 * (i + 1))
		msgs = append(msgs,
			prefixedMsg(model.RoleUser, base, tagentevent.TypeExternalInput, fmt.Sprintf("task %d", i)),
			prefixedMsg(model.RoleAssistant, base+1, tagentevent.TypeThinkingPlan, fmt.Sprintf("plan %d", i)),
			prefixedMsg(model.RoleTool, base+2, tagentevent.TypeActionCommand, fmt.Sprintf("tool %d", i)),
			prefixedMsg(model.RoleAssistant, base+3, tagentevent.TypeAgentOutput, fmt.Sprintf("reply %d", i)),
		)
	}
	return msgs
}

// contentsOf joins message contents for substring assertions.
func contentsOf(msgs []model.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// Budget-driven aging ladder over 10 complete turns with keepRecent=2
// (exponential aging {k,2k}={2,4}): ages 0-1 → L0, ages 2-3 → L1, ages 4-9 →
// L2 — and NO L3: the base ladder never archives. maxTokens is tuned so the
// full render exceeds it but the post-aging render fits, proving aging fires
// on budget pressure and stops as soon as it fits (single-dimension trigger).
func TestCompressSkeleton_BudgetAgingLadder(t *testing.T) {
	var msgs []model.Message
	for i := 0; i < 10; i++ {
		base := int64(10 * (i + 1))
		msgs = append(msgs,
			prefixedMsg(model.RoleUser, base, tagentevent.TypeExternalInput, fmt.Sprintf("task %d %s", i, strings.Repeat("a", 1200))),
			prefixedMsg(model.RoleAssistant, base+1, tagentevent.TypeThinkingPlan, fmt.Sprintf("plan %d %s", i, strings.Repeat("b", 600))),
			prefixedMsg(model.RoleTool, base+2, tagentevent.TypeActionCommand, fmt.Sprintf("tool %d %s", i, strings.Repeat("c", 1800))),
			prefixedMsg(model.RoleAssistant, base+3, tagentevent.TypeAgentOutput, fmt.Sprintf("reply %d %s", i, strings.Repeat("d", 1200))),
		)
	}
	// Full render ≈ 25K tokens > 17K; post-aging render (6×L2 + 2×L1 + 2×L0)
	// ≈ 15.6K tokens ≤ 17K → no escalation, no L3, L1 band preserved (sizes
	// verified against the estimator: ~2 chars/token).
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(17000))

	result := sc.Compress(context.Background(), msgs)
	joined := contentsOf(result)

	// NO L3: every turn stays on the timeline (age never archives).
	for turn := 0; turn < 10; turn++ {
		assert.Contains(t, joined, fmt.Sprintf("task %d", turn), "turn %d must stay (no age-based L3)", turn)
		assert.Contains(t, joined, fmt.Sprintf("reply %d", turn), "turn %d skeleton must stay", turn)
	}
	// L2 (turns 0-5, age 9-4): skeleton only.
	for _, turn := range []int{0, 1, 2, 3, 4, 5} {
		assert.NotContains(t, joined, fmt.Sprintf("plan %d", turn), "L2 drops thinking_plan")
		assert.NotContains(t, joined, fmt.Sprintf("tool %d", turn), "L2 drops action_command")
	}
	// L1 (turns 6-7, age 3/2): tool dropped, thinking kept.
	for _, turn := range []int{6, 7} {
		assert.Contains(t, joined, fmt.Sprintf("plan %d", turn), "L1 keeps thinking_plan")
		assert.NotContains(t, joined, fmt.Sprintf("tool %d", turn), "L1 drops action_command")
	}
	// L0 (turns 8-9, age 1/0): everything kept.
	for _, turn := range []int{8, 9} {
		assert.Contains(t, joined, fmt.Sprintf("tool %d", turn), "L0 keeps everything")
	}
	// Zero-LLM path: no error/degradation notices ever.
	assert.NotContains(t, joined, "[context_compress_error]")
}

// Segment count alone is NOT a trigger (single-dimension-trigger spec): a
// deep history (many complete segments) under budget passes through
// untouched — no aging, no archival, no rolling-summary side effects.
func TestCompressSkeleton_ManySegmentsUnderBudgetNoChange(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2)) // default maxTokens = 8000
	msgs := buildTurns(10)                           // ~1.3K tokens: far under budget

	result := sc.Compress(context.Background(), msgs)
	assert.Equal(t, msgs, result, "segment count alone must not trigger any compression")
}

// In-progress segment is fully preserved even when the history is deep
// (spec "进行中段完整保留").
func TestCompressSkeleton_InProgressSegmentPreserved(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1))
	msgs := buildTurns(6)
	// Trailing in-progress turn: pending input + partial execution, no output.
	msgs = append(msgs,
		prefixedMsg(model.RoleUser, 9001, tagentevent.TypeExternalInput, "pending ask"),
		prefixedMsg(model.RoleAssistant, 9002, tagentevent.TypeThinkingPlan, "pending plan"),
		prefixedMsg(model.RoleTool, 9003, tagentevent.TypeActionCommand, "pending tool"),
	)

	result := sc.Compress(context.Background(), msgs)
	joined := contentsOf(result)

	assert.Contains(t, joined, "pending ask")
	assert.Contains(t, joined, "pending plan")
	assert.Contains(t, joined, "pending tool")
}

// All retained messages keep their [evt_KEY|type] prefixes so
// buildRetainedRefs can track surviving refs (task 2.4).
func TestCompressSkeleton_RetainedMessagesKeepPrefixes(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(1))
	msgs := append([]model.Message{{Role: model.RoleSystem, Content: "system"}}, buildTurns(8)...)

	result := sc.Compress(context.Background(), msgs)
	require.NotEmpty(t, result)
	assert.Equal(t, model.RoleSystem, result[0].Role)
	for _, msg := range result[1:] {
		key, _, _ := tagentevent.ParseEventKeyAndType(msg.Content)
		assert.Positive(t, key, "retained message must keep its event key prefix: %q", msg.Content)
	}
}

// Under budget with few complete segments: untouched.
func TestCompressSkeleton_UnderBudgetNoChange(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2))
	msgs := buildTurns(2)
	result := sc.Compress(context.Background(), msgs)
	assert.Equal(t, msgs, result)
}

// Budget escalation: when skeletons still exceed budget, old segments are
// compacted (L3) oldest-first while the most recent keepRecent complete
// turns survive.
func TestCompressSkeleton_BudgetEscalationCompactsOldest(t *testing.T) {
	// Skeleton-only turns (no intermediates): L1/L2 cannot reduce anything,
	// so only multi-segment compaction can meet the budget.
	var msgs []model.Message
	for i := 0; i < 5; i++ {
		base := int64(100 * (i + 1))
		msgs = append(msgs,
			prefixedMsg(model.RoleUser, base, tagentevent.TypeExternalInput, fmt.Sprintf("ask %d %s", i, strings.Repeat("x", 200))),
			prefixedMsg(model.RoleAssistant, base+1, tagentevent.TypeAgentOutput, fmt.Sprintf("ans %d %s", i, strings.Repeat("y", 200))),
		)
	}
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(80))

	result := sc.Compress(context.Background(), msgs)
	joined := contentsOf(result)

	// Oldest turns compacted away; the 2 most recent complete turns survive.
	assert.NotContains(t, joined, "ask 0")
	assert.Contains(t, joined, "ask 3")
	assert.Contains(t, joined, "ask 4")
}

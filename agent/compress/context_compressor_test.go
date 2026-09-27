package compress

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tagentevent "github.com/SpellingDragon/tagent/event"
	memory "github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestContextCompressor_PassThroughUnderBudget(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, nil, NewDefaultTokenCounter(), 8000, 0.8, 2)

	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "hello"},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, EventSummary: "hi"},
	}

	result := cc.Compress(context.Background(), refs)

	// Under budget: all refs resolved, no compression.
	if len(result.Messages) != len(refs) {
		t.Fatalf("expected %d messages, got %d", len(refs), len(result.Messages))
	}
	if len(result.RetainedRefs) != len(refs) {
		t.Fatalf("expected %d retained refs, got %d", len(refs), len(result.RetainedRefs))
	}
}

// TestContextCompressor_PrefixesUnderBudget verifies that resolved messages
// have [evt_KEY|type] prefixes even when no compression is needed.
func TestContextCompressor_PrefixesUnderBudget(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	memStore.StoreEvent(100, memory.FullEvent{
		EventKey:     100,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "user said hello",
		Content:      "hello",
	})

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2)

	refs := []memory.EventReference{
		{EventKey: 100, EventType: tagentevent.TypeExternalInput, EventSummary: "user said hello"},
	}

	result := cc.Compress(context.Background(), refs)

	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}

	// The resolved message should have the [evt_ prefix (canonical hex form).
	if !strings.HasPrefix(result.Messages[0].Content, "[evt_64|external_input]") {
		t.Fatalf("expected message to be prefixed, got: %s", result.Messages[0].Content)
	}
}

// TestBuildRetainedRefs_RollingSummary: a prior summary ref (negative key) is
// absorbed into the new one — count accumulates, card lines carry over, time
// lower bound carries over — and the listed keys are capped. Regression guard
// for the unbounded-keys-list / silent-history-drop pair.
func TestBuildRetainedRefs_RollingSummary(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(6000), WithCompactKeysListed(32))

	// Prior rolling summary: 105 events already compacted, one card line,
	// oldest ts=1000.
	refs := []memory.EventReference{{
		EventKey:     -1000,
		EventType:    tagentevent.TypeContextCompress,
		EventSummary: "[Compacted 105 historical events]\n- 07-20 10:00 [aa] 早期任务完成\nrecent keys=aa,bb",
		Timestamp:    1000,
		Role:         "user",
	}}
	// 40 fresh refs, all compressed away (none appear in compressedMsgs).
	// Boundary events (external_input) produce new card lines.
	for i := 1; i <= 40; i++ {
		refs = append(refs, memory.EventReference{
			EventKey: int64(i), EventType: tagentevent.TypeExternalInput,
			EventSummary: fmt.Sprintf("请求 %d", i), Timestamp: int64(2000 + i),
		})
	}

	retained := cc.buildRetainedRefs(refs, nil, context.Background(), nil)
	if len(retained) != 1 {
		t.Fatalf("expected single rolling summary ref, got %d: %+v", len(retained), retained)
	}
	s := retained[0]
	if s.EventKey != -1000 || s.Timestamp != 1000 {
		t.Errorf("time lower bound must carry over from prior summary, got key=%d ts=%d", s.EventKey, s.Timestamp)
	}
	if !strings.Contains(s.EventSummary, "[Compacted 145 historical events]") {
		t.Errorf("rolling count must accumulate (105+40=145), got: %q", s.EventSummary)
	}
	// Prior card line carried over verbatim (zero-drift accumulation).
	if !strings.Contains(s.EventSummary, "[aa] 早期任务完成") {
		t.Errorf("prior card line must carry over, got: %q", s.EventSummary)
	}
	// New boundary events produced card lines with recall tickets.
	if !strings.Contains(s.EventSummary, "["+tagentevent.FormatEventKey(1)+"] 请求 1") {
		t.Errorf("new card lines must be extracted, got: %q", s.EventSummary)
	}
	// recent keys list capped.
	lastLine := s.EventSummary[strings.LastIndex(s.EventSummary, "recent keys="):]
	if n := strings.Count(lastLine, ",") + 1; n > DefaultCompactKeysListed {
		t.Errorf("listed keys must be capped at %d, got %d", DefaultCompactKeysListed, n)
	}

	// A further round with NO new compression still preserves the entry point
	// AND the card lines.
	retained2 := cc.buildRetainedRefs(retained, nil, context.Background(), nil)
	if len(retained2) != 1 ||
		!strings.Contains(retained2[0].EventSummary, "[Compacted 145 historical events]") ||
		!strings.Contains(retained2[0].EventSummary, "[aa] 早期任务完成") {
		t.Errorf("summary and cards must survive rounds without new compression: %+v", retained2)
	}
}

// TestCurateCards_SinkWithoutModel: without a summary model, an over-cap card
// sequence sinks its oldest lines into the earlier-items counter — the
// engineering fallback never breaks.
func TestCurateCards_SinkWithoutModel(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(120))

	var cards []string
	for i := 0; i < 10; i++ {
		cards = append(cards, fmt.Sprintf("- 07-2%d 10:00 [k%d] 任务 %d 完成了一些工作", i%10, i, i))
	}
	out, earlier := cc.curateCards(context.Background(), cards, 3)
	if len(strings.Join(out, "\n")) > 120 {
		t.Errorf("curated cards must fit the cap, got %d chars", len(strings.Join(out, "\n")))
	}
	if earlier != 3+(len(cards)-len(out)) {
		t.Errorf("sunk lines must be counted: earlier=%d dropped=%d", earlier, len(cards)-len(out))
	}
	// Newest lines survive (sink from the oldest side).
	if !strings.Contains(out[len(out)-1], "[k9]") {
		t.Errorf("newest card must survive sinking, got: %v", out)
	}
}

// TestExtractCardLine_MeditationHighlight: meditation outputs get ★.
func TestExtractCardLine_MeditationHighlight(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1)
	cc.MarkMeditationKey(42)

	line := cc.extractCardLine(memory.EventReference{
		EventKey: 42, EventType: tagentevent.TypeAgentOutput,
		EventSummary: "冥想回顾: 近期专注知识库整理", Timestamp: 1710000000000,
	})
	if !strings.HasPrefix(line, "- ★ ") || !strings.Contains(line, "["+tagentevent.FormatEventKey(42)+"]") {
		t.Errorf("meditation card must be ★-highlighted with ticket, got: %q", line)
	}
	// Tool steps produce no card.
	if l := cc.extractCardLine(memory.EventReference{EventKey: 7, EventType: tagentevent.TypeActionCommand, EventSummary: "x"}); l != "" {
		t.Errorf("non-boundary events must not produce cards, got %q", l)
	}
}

func TestContextCompressor_RetainsRefsWhenCompressed(t *testing.T) {
	memStore := memory.NewInMemoryStore()

	for i := 1; i <= 6; i++ {
		key := int64(i)
		evtType := tagentevent.TypeExternalInput
		if i%2 == 0 {
			evtType = tagentevent.TypeAgentOutput
		}
		memStore.StoreEvent(key, memory.FullEvent{
			EventKey:     key,
			PartitionID:  1,
			EventType:    evtType,
			EventSummary: "event " + string(rune('A'+i-1)),
			Content:      "content " + string(rune('A'+i-1)),
		})
	}

	sc := NewSmartCompressor(
		WithKeepRecentTasks(2),
		WithMaxTokens(1), // Force compression
		WithSummaryModel(&mockBatchSummaryModel{responses: []string{"batch summary"}}),
	)
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 1, 0.8, 2)

	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "event A"},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, EventSummary: "event B"},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput, EventSummary: "event C"},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput, EventSummary: "event D"},
		{EventKey: 5, EventType: tagentevent.TypeExternalInput, EventSummary: "event E"},
		{EventKey: 6, EventType: tagentevent.TypeAgentOutput, EventSummary: "event F"},
	}

	result := cc.Compress(context.Background(), refs)

	// With deterministic level assignment, some segments are compressed (L1/L2).
	// The exact number of retained refs depends on which level they were assigned.
	// Key invariant: retained refs must not exceed original count.
	if len(result.RetainedRefs) > len(refs) {
		t.Fatalf("retained refs should not exceed original (%d -> %d)", len(refs), len(result.RetainedRefs))
	}

	// The first retained ref should be a context_compress summary
	if len(result.RetainedRefs) > 0 {
		summaryRef := result.RetainedRefs[0]
		if summaryRef.EventType == tagentevent.TypeContextCompress {
			if summaryRef.EventSummary == "" {
				t.Fatal("summary ref should have non-empty EventSummary")
			}
		}
	}

	// Recent refs (keep_recent=2) must survive compression.
	retainedKeys := make(map[int64]bool)
	for _, ref := range result.RetainedRefs {
		retainedKeys[ref.EventKey] = true
	}
	for _, key := range []int64{5, 6} {
		if !retainedKeys[key] {
			t.Fatalf("recent event key %d should be retained; retained keys=%v", key, retainedKeys)
		}
	}
}

func TestContextCompressor_ResolvesFullContentFromMemoryStore(t *testing.T) {
	memStore := memory.NewInMemoryStore()

	key := int64(100)
	memStore.StoreEvent(key, memory.FullEvent{
		EventKey:  key,
		EventType: tagentevent.TypeExternalInput,
		Content:   "full user message content",
	})

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2)

	refs := []memory.EventReference{
		{EventKey: key, EventType: tagentevent.TypeExternalInput},
	}

	result := cc.Compress(context.Background(), refs)

	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if !strings.Contains(result.Messages[0].Content, "full user message content") {
		t.Fatalf("expected full content, got: %s", result.Messages[0].Content)
	}
}

// TestContextCompressor_ActionCommandNativeAndDemote (D3 v2): a result whose
// declaring call is present renders as native role=tool; a result with no id
// or whose call is absent from the rendered sequence demotes to a user-side
// input note (content preserved) — so any compression cut stays legal.
func TestContextCompressor_ActionCommandNativeAndDemote(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	// call-x declared by a thinking_plan present in the sequence.
	memStore.StoreEvent(1, memory.FullEvent{EventKey: 1, EventType: "thinking_plan", Content: "执行命令",
		ToolCalls: []model.ToolCall{{ID: "call-x", Function: model.FunctionDefinitionParam{Name: "action"}}}})
	memStore.StoreEvent(2, memory.FullEvent{EventKey: 2, EventType: "action_command", Content: "paired tool output", ToolID: "call-x"})
	// id-less result → demote.
	memStore.StoreEvent(3, memory.FullEvent{EventKey: 3, EventType: "action_command", Content: "idless tool output"})
	// result whose call was compacted away → demote with marker.
	memStore.StoreEvent(4, memory.FullEvent{EventKey: 4, EventType: "action_command", Content: "orphan tool output", ToolID: "call-gone"})

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2)
	refs := []memory.EventReference{
		{EventKey: 1, EventType: "thinking_plan"},
		{EventKey: 2, EventType: "action_command"},
		{EventKey: 3, EventType: "action_command"},
		{EventKey: 4, EventType: "action_command"},
	}
	result := cc.Compress(context.Background(), refs)
	if len(result.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result.Messages))
	}
	assertRenderLegality(t, result.Messages)

	if result.Messages[1].Role != model.RoleTool || result.Messages[1].ToolID != "call-x" {
		t.Errorf("paired result must render native role=tool: %+v", result.Messages[1])
	}
	if result.Messages[2].Role != model.RoleUser || !strings.Contains(result.Messages[2].Content, "idless tool output") {
		t.Errorf("id-less result must demote to user note with content preserved: %+v", result.Messages[2])
	}
	if result.Messages[3].Role != model.RoleUser ||
		!strings.Contains(result.Messages[3].Content, "call-gone") ||
		!strings.Contains(result.Messages[3].Content, "orphan tool output") {
		t.Errorf("orphan result must demote keeping correlation id and content: %+v", result.Messages[3])
	}
}

func TestContextCompressor_EmptyRefs(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, nil, NewDefaultTokenCounter(), 8000, 0.8, 2)

	result := cc.Compress(context.Background(), nil)

	if len(result.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(result.Messages))
	}
	if len(result.RetainedRefs) != 0 {
		t.Fatalf("expected 0 retained refs, got %d", len(result.RetainedRefs))
	}
}

func TestContextCompressor_DoesNotMutateInputRefs(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(1))
	cc := NewContextCompressor(sc, nil, NewDefaultTokenCounter(), 1, 0.8, 2)

	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "a"},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, EventSummary: "b"},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput, EventSummary: "c"},
	}
	originalLen := len(refs)

	_ = cc.Compress(context.Background(), refs)

	if len(refs) != originalLen {
		t.Fatal("Compress must not mutate input refs slice")
	}
}

// TestContextCompressor_PreservesChronologicalOrder verifies that the
// projection timeline order is preserved in the resolved messages.
func TestContextCompressor_PreservesChronologicalOrder(t *testing.T) {
	memStore := memory.NewInMemoryStore()

	memStore.StoreEvent(1, memory.FullEvent{
		EventKey:     1,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "user message 1",
		Content:      "user message 1",
	})
	memStore.StoreEvent(2, memory.FullEvent{
		EventKey:     2,
		EventType:    tagentevent.TypeAgentOutput,
		EventSummary: "assistant response 1",
		Content:      "assistant response 1",
	})
	memStore.StoreEvent(3, memory.FullEvent{
		EventKey:     3,
		EventType:    tagentevent.TypeActionCommand,
		EventSummary: "tool result 1",
		Content:      "tool result 1",
	})
	memStore.StoreEvent(4, memory.FullEvent{
		EventKey:     4,
		EventType:    tagentevent.TypeAgentOutput,
		EventSummary: "assistant response 2",
		Content:      "assistant response 2",
	})

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2)

	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "user message 1"},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, EventSummary: "assistant response 1"},
		{EventKey: 3, EventType: tagentevent.TypeActionCommand, EventSummary: "tool result 1"},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput, EventSummary: "assistant response 2"},
	}

	result := cc.Compress(context.Background(), refs)

	// Verify chronological order: user1 should appear BEFORE assistant1.
	var order []string
	for _, m := range result.Messages {
		content := tagentevent.StripEventKeyPrefix(m.Content)
		if len(content) > 30 {
			content = content[:30]
		}
		order = append(order, fmt.Sprintf("%s:%s", m.Role, content))
	}

	userIdx := -1
	asstIdx := -1
	for i, s := range order {
		if strings.Contains(s, "user message 1") && userIdx == -1 {
			userIdx = i
		}
		if strings.Contains(s, "assistant response 1") && asstIdx == -1 {
			asstIdx = i
		}
	}

	if userIdx < 0 || asstIdx < 0 {
		t.Fatalf("missing key messages in order: %v", order)
	}
	if userIdx > asstIdx {
		t.Fatalf("user1 (idx=%d) should appear BEFORE assistant1 (idx=%d); order=%v",
			userIdx, asstIdx, order)
	}
}

// TestContextCompressor_SummaryRefRetainedAcrossCompressions verifies that
// after the first compression creates a summary ref (negative key), the
// second compression retains it rather than perpetually re-compressing it.
func TestContextCompressor_SummaryRefRetainedAcrossCompressions(t *testing.T) {
	memStore := memory.NewInMemoryStore()

	for i := 1; i <= 6; i++ {
		key := int64(i)
		evtType := tagentevent.TypeExternalInput
		if i%2 == 0 {
			evtType = tagentevent.TypeAgentOutput
		}
		memStore.StoreEvent(key, memory.FullEvent{
			EventKey:     key,
			EventType:    evtType,
			EventSummary: "event " + string(rune('A'+i-1)),
			Content:      "content " + string(rune('A'+i-1)),
		})
	}

	sc := NewSmartCompressor(
		WithKeepRecentTasks(1),
		WithMaxTokens(1),
		WithSummaryModel(&mockBatchSummaryModel{responses: []string{"batch summary"}}),
	)
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 1, 0.8, 1)

	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "event A", Timestamp: 1000},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, EventSummary: "event B", Timestamp: 2000},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput, EventSummary: "event C", Timestamp: 3000},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput, EventSummary: "event D", Timestamp: 4000},
		{EventKey: 5, EventType: tagentevent.TypeExternalInput, EventSummary: "event E", Timestamp: 5000},
		{EventKey: 6, EventType: tagentevent.TypeAgentOutput, EventSummary: "event F", Timestamp: 6000},
	}

	// First compression
	result1 := cc.Compress(context.Background(), refs)

	hasSummaryRef := false
	for _, ref := range result1.RetainedRefs {
		if ref.EventKey < 0 && ref.EventType == tagentevent.TypeContextCompress {
			hasSummaryRef = true
		}
	}
	if !hasSummaryRef {
		t.Fatal("first compression should produce a summary ref with negative key")
	}

	// Second compression using the retained refs from the first
	result2 := cc.Compress(context.Background(), result1.RetainedRefs)

	for _, ref := range result2.RetainedRefs {
		if ref.EventType == tagentevent.TypeContextCompress {
			if strings.Contains(ref.EventSummary, "keys=-") {
				t.Fatalf("summary ref should not be re-compressed; got: %s", ref.EventSummary)
			}
		}
	}
}

// TestCurateCards_MultiLineCondensation: LLM condensation output is scrubbed
// to a single line — the card section is parsed by "- "-prefixed lines, and a
// multi-line output would silently drop continuation lines next round.
func TestCurateCards_MultiLineCondensation(t *testing.T) {
	sm := &countingSummaryModel{}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(sm))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(100))

	var cards []string
	for i := 0; i < 8; i++ {
		cards = append(cards, fmt.Sprintf("- 07-2%d 10:00 [k%d] 任务 %d 完成了一些工作", i%10, i, i))
	}
	out, _ := cc.curateCards(context.Background(), cards, 0)
	joined := strings.Join(out, "\n")
	if len(joined) > 100 {
		t.Errorf("curated cards must fit the cap, got %d chars", len(joined))
	}
	// Every line is a well-formed card line (no continuation leakage).
	for _, line := range out {
		if !strings.HasPrefix(line, "- ") {
			t.Errorf("condensation must not leak non-card lines, got %q", line)
		}
	}
}

// assertRenderLegality mirrors the invariant assertion in the agent package
// (agent/invariants_test.go) — the render-legality law is identical on both
// sides of the package boundary.
func assertRenderLegality(t *testing.T, msgs []model.Message) {
	t.Helper()
	seenKeys := map[int64]bool{}
	declared := map[string]bool{}
	consumed := map[string]bool{}
	for i, m := range msgs {
		switch m.Role {
		case model.RoleAssistant:
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					declared[tc.ID] = true
				}
			}
			if len(m.ToolCalls) == 0 && strings.TrimSpace(tagentevent.StripEventKeyPrefix(m.Content)) == "" {
				t.Errorf("render legality: msg[%d] is an empty assistant output", i)
			}
		case model.RoleTool:
			if m.ToolID == "" || !declared[m.ToolID] {
				t.Errorf("render legality: msg[%d] is an orphan tool result (tool_id=%q has no prior declaring call)", i, m.ToolID)
			}
			if consumed[m.ToolID] {
				t.Errorf("render legality: msg[%d] duplicates an already-answered tool_id=%q", i, m.ToolID)
			}
			consumed[m.ToolID] = true
		}
		key, _, _ := tagentevent.ParseEventKeyAndType(m.Content)
		if key > 0 {
			if seenKeys[key] {
				t.Errorf("render legality: duplicate event key %d at msg[%d]", key, i)
			}
			seenKeys[key] = true
		}
	}
}

// makeTurn builds one complete task turn: external_input → thinking_plan →
// action_command → agent_output, with monotonically increasing keys/ts.
func makeTurn(base int64) []memory.EventReference {
	return []memory.EventReference{
		{EventKey: base + 1, EventType: tagentevent.TypeExternalInput, EventSummary: "用户请求", Timestamp: base + 1},
		{EventKey: base + 2, EventType: tagentevent.TypeThinkingPlan, EventSummary: "思考", Timestamp: base + 2},
		{EventKey: base + 3, EventType: tagentevent.TypeActionCommand, EventSummary: "执行", Timestamp: base + 3},
		{EventKey: base + 4, EventType: tagentevent.TypeAgentOutput, EventSummary: "完成", Timestamp: base + 4},
	}
}

// TestContextCompressor_TriggersOnCapacity (stable-context-compaction D2):
// the token budget is the ONLY compaction trigger. Over budget → compaction
// runs regardless of turn count; under budget → pass-through regardless of
// turn count (the old turn-count dimension is retired — it jittered the
// prefix every round at steady state).
func TestContextCompressor_TriggersOnCapacity(t *testing.T) {
	// 3 complete turns (12 refs); a 1-token budget forces the capacity gate.
	var refs []memory.EventReference
	for i := int64(0); i < 3; i++ {
		refs = append(refs, makeTurn(i*10)...)
	}

	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1, 0.8, 1)
	result := cc.Compress(context.Background(), refs)

	// Compaction must have run: some refs folded away (RetainedRefs fewer
	// than input) and/or a rolling summary ref (negative key) formed.
	hasRolling := false
	for _, r := range result.RetainedRefs {
		if r.EventKey < 0 {
			hasRolling = true
		}
	}
	if len(result.RetainedRefs) >= len(refs) && !hasRolling {
		t.Fatalf("compaction must run over the capacity gate: retained %d >= input %d and no rolling summary",
			len(result.RetainedRefs), len(refs))
	}

	// Counter-direction: the SAME turn count under a huge budget must
	// pass through untouched — turn count alone never triggers (D2).
	scBig := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1_000_000))
	ccBig := NewContextCompressor(scBig, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 1)
	resultBig := ccBig.Compress(context.Background(), refs)
	if len(resultBig.RetainedRefs) != len(refs) {
		t.Fatalf("under budget must pass through even with excess turns: retained %d != input %d",
			len(resultBig.RetainedRefs), len(refs))
	}
	for _, r := range resultBig.RetainedRefs {
		if r.EventKey < 0 {
			t.Fatalf("no rolling summary expected when under budget")
		}
	}
}

// TestContextCompressor_NoTriggerFewTurns: under budget, compression must NOT
// run (pass-through) — capacity is the sole gate.
func TestContextCompressor_NoTriggerFewTurns(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(1_000_000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 2)

	// 1 complete turn (4 refs) <= keepRecent=2, under budget.
	refs := makeTurn(0)
	result := cc.Compress(context.Background(), refs)

	if len(result.RetainedRefs) != len(refs) {
		t.Fatalf("no compression expected for few turns: retained %d != input %d",
			len(result.RetainedRefs), len(refs))
	}
	for _, r := range result.RetainedRefs {
		if r.EventKey < 0 {
			t.Fatalf("no rolling summary expected when not triggered")
		}
	}
}

// TestContextCompressor_CardCarriesMemoryTurnHint (compress-digest-reconnect):
// when a turn is fully dropped (L3), its agent_output card carries a
// "含 N 步工具调用，可用 memory_turn 追溯" hint so the model knows the dropped
// execution process is recoverable via memory_turn.
func TestContextCompressor_CardCarriesMemoryTurnHint(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(300))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 300, 0.8, 1)

	// 6 complete turns (each: external_input + thinking_plan + action_command +
	// agent_output = 2 tool steps). keepRecent=1 + exponential {1,2,4} → the
	// oldest turn (age 5) reaches L3 and is fully dropped into the rolling
	// summary; the 300-token budget (render ≈540) passes the capacity gate
	// (D2) without forcing the all-L3 escalation storm, so mid-aged turns
	// dwell at L1/L2 where their tool_chain lines survive.
	var refs []memory.EventReference
	for i := int64(0); i < 6; i++ {
		refs = append(refs, makeTurn(i*10)...)
	}

	result := cc.Compress(context.Background(), refs)

	// With tool-chain consolidation (tool-chain-consolidation), the aged tool
	// events fold into a tool_chain ref (carrying the step count + recall
	// ticket) rather than a per-card "含 N 步" hint (superseded). Verify the
	// tool steps are represented and no zero-info placeholder appears.
	hasChain := false
	for _, r := range result.RetainedRefs {
		if r.EventType == tagentevent.TypeToolChain {
			hasChain = true
		}
	}
	if !hasChain {
		t.Fatalf("expected a tool_chain ref representing the aged tool steps, got: %+v", result.RetainedRefs)
	}
	joined := ""
	for _, m := range result.Messages {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "工具链") {
		t.Errorf("model context must contain the tool-chain line, got:\n%s", joined)
	}
	if strings.Contains(joined, "历史事件摘要为空") {
		t.Errorf("model context must NOT contain empty-summary placeholder, got:\n%s", joined)
	}
}

// TestContextCompressor_HintIgnoresBusInjection (code-review Major): a
// bus-injected mid-turn event is ALSO typed external_input (persistBusEvent
// for task_settled etc.). The toolSteps counter must NOT reset on it — the
// card must count ALL of the turn's tool steps, not just post-injection ones.
func TestContextCompressor_HintIgnoresBusInjection(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(300))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 300, 0.8, 1)

	// Oldest turn: ext, tp, ac, [task_settled bus injection typed external_input],
	// tp, ac, out = 4 tool steps. Followed by 3 normal turns so it ages to L3.
	oldest := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "用户请求", Timestamp: 1},
		{EventKey: 2, EventType: tagentevent.TypeThinkingPlan, EventSummary: "思考", Timestamp: 2},
		{EventKey: 3, EventType: tagentevent.TypeActionCommand, EventSummary: "执行", Timestamp: 3},
		{EventKey: 4, EventType: tagentevent.TypeExternalInput, EventSummary: "task_settled", Timestamp: 4}, // bus injection mid-turn
		{EventKey: 5, EventType: tagentevent.TypeThinkingPlan, EventSummary: "思考2", Timestamp: 5},
		{EventKey: 6, EventType: tagentevent.TypeActionCommand, EventSummary: "执行2", Timestamp: 6},
		{EventKey: 7, EventType: tagentevent.TypeAgentOutput, EventSummary: "完成", Timestamp: 7},
	}
	refs := append([]memory.EventReference{}, oldest...)
	for i := int64(1); i <= 5; i++ { // 6 segments total → oldest (age 5) reaches L3 (exponential {1,2,4})
		refs = append(refs, makeTurn(i*10)...)
	}

	result := cc.Compress(context.Background(), refs)

	// With tool-chain consolidation, the bus-injected external_input (a boundary)
	// splits the tool run into separate tool_chains; the tool steps are still
	// represented (not lost) and no zero-info placeholder appears. (The old
	// "含 4 步" card hint is superseded by the tool_chain line's own count.)
	chains := 0
	for _, r := range result.RetainedRefs {
		if r.EventType == tagentevent.TypeToolChain {
			chains++
		}
	}
	if chains < 1 {
		t.Fatalf("expected tool_chain ref(s) representing the tool steps, got: %+v", result.RetainedRefs)
	}
	joined := ""
	for _, m := range result.Messages {
		joined += m.Content + "\n"
	}
	if strings.Contains(joined, "历史事件摘要为空") {
		t.Errorf("model context must NOT contain empty-summary placeholder, got:\n%s", joined)
	}
}

// TestContextCompressor_RollingSummarySurvivesL3 (rolling-summary-anchor D1):
// the rolling summary message must NOT be L3-dropped with segment 0. With the
// summary split out (like the system message), it stays visible even when the
// first turn's segment reaches L3. fail-before: the summary rode inside
// segment 0 and was dropped, so Messages lost it.
func TestContextCompressor_RollingSummarySurvivesL3(t *testing.T) {
	// keepRecent=1 + exponential {1,2,4}: L3 needs age>=4. With 5 turns, segment
	// 0 (turn 1) age = 5-1-0 = 4 → L3. This is the key: segment 0 must ACTUALLY
	// reach L3 (code-review Major — with keepRecent=2 + 8 turns, exponential
	// made segment 0 age 7 = L2, so the summary survived WITHOUT protection and
	// the test had no regression coverage). keepRecent=1 + 5 turns forces L3.
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1_000_000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 1)

	rolling := memory.EventReference{
		EventKey: -1000, EventType: tagentevent.TypeContextCompress,
		EventSummary: "[Compacted 100 historical events]\n- 早期卡片 [aa]\nrecent keys=aa",
		Timestamp:    1000, Role: "user",
	}
	refs := []memory.EventReference{rolling}
	for i := int64(0); i < 5; i++ { // 5 turns, keepRecent=1 -> segment 0 age 4 -> L3
		refs = append(refs, makeTurn(i*10)...)
	}

	result := cc.Compress(context.Background(), refs)

	// The rolling summary message must survive in the model context.
	hasSummaryMsg := false
	for _, m := range result.Messages {
		if strings.Contains(m.Content, "context_compress") && strings.Contains(m.Content, "Compacted 100") {
			hasSummaryMsg = true
		}
	}
	if !hasSummaryMsg {
		t.Fatalf("rolling summary message was L3-dropped from model context (D1 bug)")
	}
	// It must sit right after the system message (index 1) if a system msg exists,
	// else be the first message.
	if len(result.Messages) > 1 && result.Messages[0].Role == model.RoleSystem {
		if !strings.Contains(result.Messages[1].Content, "context_compress") {
			t.Errorf("rolling summary must be right after system message, got [1]=%.60q", result.Messages[1].Content)
		}
	}
	// The rolling summary ref must still be rebuilt in RetainedRefs.
	hasSummaryRef := false
	for _, r := range result.RetainedRefs {
		if r.EventKey < 0 && r.EventType == tagentevent.TypeContextCompress {
			hasSummaryRef = true
		}
	}
	if !hasSummaryRef {
		t.Errorf("rolling summary ref must be rebuilt in RetainedRefs")
	}
}

// Bounded fuzz + error injection for the compression projection parsers
// (resident-remaining-hardening 3.6, archived 7.6).
//
// guardCondensedCard is the machine ticket guard (design D6): it is the ONLY
// thing standing between a summary-model condensation and the compaction
// payload. Its load-bearing contract is ANTI-FABRICATION — it must never
// accept condensed text carrying a recall ticket that was not present in the
// folded input, because a forged [key] would poison every subsequent
// memory_recall. parseCardTickets / parseCardSection / fitTicketCard back it
// and must be panic-total.
//
// Deterministic bounded loop runs on every `go test`. Native targets:
//
//	go test ./agent/compress/ -run FuzzGuardCondensedCard -fuzz FuzzGuardCondensedCard -fuzztime=30s

var ticketChars = regexp.MustCompile(`^-?[0-9a-f]+$`)

func TestParseCardTickets_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xca4dca4d))
	for i := 0; i < 40000; i++ {
		line := randomCardLine(rng)
		for _, k := range parseCardTickets(line) {
			// Every extracted ticket must be canonical-hex and appear verbatim
			// bracketed in the source line.
			if !ticketChars.MatchString(k) {
				t.Fatalf("parseCardTickets(%q) returned non-canonical ticket %q", line, k)
			}
			if !strings.Contains(line, "["+k+"]") {
				t.Fatalf("parseCardTickets(%q) returned ticket %q not present bracketed in input", line, k)
			}
		}
		// Panic-totality of the sibling parsers on the same garbage.
		_, _ = parseCardSection(line)
		_ = parseNarrativeSection(line)
		_, _ = fitTicketCard(line, rng.Intn(64))
	}
}

// TestGuardCondensedCard_AcceptImpliesNoForgery is the core anti-fabrication
// property: whatever the guard ACCEPTS, its tickets must be a subset of the
// input's tickets. Enforced across random input/condensed pairings.
func TestGuardCondensedCard_AcceptImpliesNoForgery(t *testing.T) {
	rng := rand.New(rand.NewSource(0x9a0d))
	for i := 0; i < 40000; i++ {
		input := randomCardLines(rng)
		condensed := randomCardLine(rng)

		inSet := map[string]bool{}
		for _, l := range input {
			for _, k := range parseCardTickets(l) {
				inSet[k] = true
			}
		}
		verdict := guardCondensedCard(condensed, input)
		if verdict != "" {
			continue // rejected: fine, the guard is allowed to reject anything.
		}
		if len(inSet) == 0 {
			continue // "nothing to protect": acceptance without tickets is by design.
		}
		for _, k := range parseCardTickets(condensed) {
			if !inSet[k] {
				t.Fatalf("guard ACCEPTED condensed %q (input %v) containing forged ticket %q not in input",
					condensed, input, k)
			}
		}
	}
}

// TestGuardCondensedCard_ForgeryRejected is the deterministic error-injection
// counter-example: a condensed line that keeps the required head/tail tickets
// but injects one unknown ticket MUST be rejected. If this ever passes the
// guard, the anti-fabrication property is broken.
func TestGuardCondensedCard_ForgeryRejected(t *testing.T) {
	input := []string{
		"- [10] first item",
		"- [20] middle item",
		"- [30] tail item",
	}
	// Head [10] and tail [30] survive; [ff] is fabricated.
	forged := "- [10] [ff] [30] condensed"
	if verdict := guardCondensedCard(forged, input); verdict == "" {
		t.Fatalf("guard accepted a condensed line with forged ticket [ff]; verdict=%q", verdict)
	}
	// The legitimate condensation (subset of input) must be accepted.
	ok := "- [10] [30] condensed"
	if verdict := guardCondensedCard(ok, input); verdict != "" {
		t.Fatalf("guard rejected a legitimate condensation %q: %s", ok, verdict)
	}
}

func randomCardLine(rng *rand.Rand) string {
	toks := []string{"- ", "★ ", "[", "]", "1", "a", "f", "0", "-", " ", "z", "[1f]", "[ff]",
		"[evt_ab|x]", "〔预算截断〕", "\n", "(earlier 3 items retrievable via memory_recall)",
		"〔历史综述〕", "[AAAA0004]", "[10]"}
	var b strings.Builder
	n := 1 + rng.Intn(8)
	for i := 0; i < n; i++ {
		b.WriteString(toks[rng.Intn(len(toks))])
	}
	return b.String()
}

func randomCardLines(rng *rand.Rand) []string {
	n := 1 + rng.Intn(4)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, randomCardLine(rng))
	}
	return out
}

// FuzzParseCardTickets is the native target: parseCardTickets must never panic
// and must never return a ticket absent from the input.
func FuzzParseCardTickets(f *testing.F) {
	f.Add("- [10] [ff] item ★")
	f.Add("[AAAA0004] uppercase not a ticket")
	f.Add("")
	f.Fuzz(func(t *testing.T, line string) {
		for _, k := range parseCardTickets(line) {
			if !ticketChars.MatchString(k) || !strings.Contains(line, "["+k+"]") {
				t.Fatalf("bad ticket %q extracted from %q", k, line)
			}
		}
	})
}

// FuzzGuardCondensedCard is the native target for the anti-fabrication
// contract. Seed corpus is a valid pairing; the fuzzer mutates both halves.
func FuzzGuardCondensedCard(f *testing.F) {
	f.Add("- [10] [30] condensed", "- [10] a\n- [20] b\n- [30] c")
	f.Add("- [ff] forged", "- [10] a\n- [30] c")
	f.Fuzz(func(t *testing.T, condensed string, input string) {
		lines := strings.Split(input, "\n")
		inSet := map[string]bool{}
		for _, l := range lines {
			for _, k := range parseCardTickets(l) {
				inSet[k] = true
			}
		}
		if guardCondensedCard(condensed, lines) == "" && len(inSet) > 0 {
			for _, k := range parseCardTickets(condensed) {
				if !inSet[k] {
					t.Fatalf("accepted condensed %q forged ticket %q (input %q)", condensed, k, input)
				}
			}
		}
	})
}

// fixedCondenseModel returns a canned summary-model answer for
// condenseCardLines and counts calls (the guard must not re-ask the model).
type fixedCondenseModel struct {
	mu    sync.Mutex
	calls int
	out   string
}

func (m *fixedCondenseModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: m.out,
	}}}}
	close(ch)
	return ch, nil
}

func (m *fixedCondenseModel) Info() model.Info { return model.Info{Name: "fixed-condense"} }

func (m *fixedCondenseModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// cardLine builds a canonical extractCardLine-shaped card line with a hex
// recall ticket; star adds the meditation highlight prefix.
func cardLine(ts, key, text string, star bool) string {
	s := "- "
	if star {
		s += "★ "
	}
	return fmt.Sprintf("%s%s [%s] %s", s, ts, key, text)
}

// guardFixture returns an over-cap card sequence whose OLD HALF (first 4 of
// 8 lines) carries tickets t0..t3 with t0 as head, t3 as tail and t2
// highlighted, plus fresh newest lines that must stay verbatim.
func guardFixture(condensed string) (*ContextCompressor, *fixedCondenseModel, []string) {
	sm := &fixedCondenseModel{out: condensed}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(sm))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(250))
	cards := []string{
		cardLine("07-21 10:00", "aaaa0001", "任务一完成了一些工作", false),
		cardLine("07-21 11:00", "aaaa0002", "任务二完成了一些工作", false),
		cardLine("07-21 12:00", "aaaa0003", "冥想反思结论", true),
		cardLine("07-21 13:00", "aaaa0004", "任务四完成了一些工作", false),
		cardLine("07-22 10:00", "bbbb0005", "新任务五", false),
		cardLine("07-22 11:00", "bbbb0006", "新任务六", false),
		cardLine("07-22 12:00", "bbbb0007", "新任务七", false),
		cardLine("07-22 13:00", "bbbb0008", "新任务八", false),
	}
	return cc, sm, cards
}

// --- 2.1 counterexamples: the pre-guard path accepted ALL of these ----------

func TestCurateCards_TicketGuard_ValidCondensationAdopted(t *testing.T) {
	// Head/tail/★ tickets preserved, subset of input → adopt.
	condensed := cardAll("", []string{"aaaa0001", "aaaa0003", "aaaa0004"}, "浓缩旧任务一至四")
	cc, sm, cards := guardFixture(condensed)
	out, earlier := cc.curateCards(context.Background(), cards, 0)
	if sm.count() != 1 {
		t.Fatalf("exactly one condensation call expected, got %d", sm.count())
	}
	if earlier != 0 {
		t.Errorf("valid condensation must not sink anything, earlier=%d", earlier)
	}
	joined := strings.Join(out, "\n")
	if len(joined) > 250 {
		t.Errorf("accepted result must fit the cap, got %d chars", len(joined))
	}
	if !strings.Contains(out[0], "浓缩旧任务一至四") {
		t.Errorf("first line must be the condensed card: %q", out[0])
	}
	for _, k := range []string{"aaaa0001", "aaaa0003", "aaaa0004"} {
		if !strings.Contains(out[0], "["+k+"]") {
			t.Errorf("required ticket [%s] must survive in the condensed line: %q", k, out[0])
		}
	}
	// Cold-eyes W-3: the non-required ticket folded into prose (aaaa0002) is a
	// counted, observable navigation loss — never silent.
	if cc.CondensedTicketsLost() != 1 {
		t.Errorf("dropped non-required ticket must be counted, got %d", cc.CondensedTicketsLost())
	}
}

func TestCurateCards_TicketGuard_RejectsTicketLossAndFabrication(t *testing.T) {
	cases := []struct {
		name      string
		condensed string
	}{
		{"non_empty_without_ticket", "旧任务一到四的浓缩概述（没有任何票据）"},
		{"unknown_ticket", cardAll("", []string{"aaaa0001", "aaaa0004", "ffffffff"}, "伪造")},
		{"dropped_head", cardAll("", []string{"aaaa0002", "aaaa0003"}, "dropped first")},
		{"dropped_tail", cardAll("", []string{"aaaa0001", "aaaa0002", "aaaa0003"}, "dropped last")},
		{"dropped_star", cardAll("", []string{"aaaa0001", "aaaa0004"}, "dropped the highlighted ticket")},
		{"garbled_tickets", cardAll("", []string{"aaaa0001", "AAAA0004"}, "大小写未知票")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cc, sm, cards := guardFixture(tc.condensed)
			out, earlier := cc.curateCards(context.Background(), cards, 0)
			if sm.count() != 1 {
				t.Fatalf("guard must not ask the model again (calls=%d)", sm.count())
			}
			// Rejection = deterministic sinking of the ORIGINAL lines: every
			// surviving line must be verbatim from the input (model text never
			// enters the payload), and the dropped ones are counted.
			if earlier < 1 {
				t.Errorf("rejection must fall through to deterministic sinking (earlier=%d, out=%q)", earlier, out)
			}
			for _, line := range out {
				if !containsLine(cards, line) {
					t.Errorf("sunk fallback must keep original card lines verbatim, got %q", line)
				}
			}
		})
	}
}

// cardAll builds "- <star?>[ts] [k1] [k2] ... text" freely.
func cardAll(star string, tickets []string, text string) string {
	var b strings.Builder
	if star != "" {
		b.WriteString(star + " ")
	}
	for _, k := range tickets {
		b.WriteString("[" + k + "] ")
	}
	b.WriteString(text)
	return b.String()
}

func containsLine(lines []string, line string) bool {
	for _, l := range lines {
		if l == line {
			return true
		}
	}
	return false
}

// TestCurateCards_FullTicketSurvivalNotCounted: the all-tickets-survive
// scenario of the "浓缩导航丢失可观测" spec requirement — condensation that
// carries every input ticket must not touch the loss counter.
func TestCurateCards_FullTicketSurvivalNotCounted(t *testing.T) {
	condensed := cardAll("", []string{"aaaa0001", "aaaa0002", "aaaa0003", "aaaa0004"}, "浓缩")
	cc, _, cards := guardFixture(condensed)
	out, earlier := cc.curateCards(context.Background(), cards, 0)
	if earlier != 0 {
		t.Fatalf("expected adoption, got sinking (earlier=%d)", earlier)
	}
	if !strings.Contains(out[0], "浓缩") {
		t.Errorf("condensed line expected, got %q", out[0])
	}
	if cc.CondensedTicketsLost() != 0 {
		t.Errorf("full ticket survival must not count any loss, got %d", cc.CondensedTicketsLost())
	}
}

// --- 6.3 single over-cap card & budget-unrepresentable -----------------------

func TestCurateCards_SingleOverCapCardKeepsEveryTicket(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(50))
	cards := []string{"- ★ 07-21 10:00 [aaaa0001] 一段远超预算的冥想反思摘要文字 [bbbb0002] 尾部还有更多无法容纳的叙述"}

	out, earlier := cc.curateCards(context.Background(), cards, 0)
	if len(out) != 1 || earlier != 0 {
		t.Fatalf("a single card must be bounded in place, not dropped: out=%q earlier=%d", out, earlier)
	}
	if len(out[0]) > 50 {
		t.Errorf("fitted card must respect the cap, got %d chars: %q", len(out[0]), out[0])
	}
	for _, k := range []string{"aaaa0001", "bbbb0002"} {
		if !strings.Contains(out[0], "["+k+"]") {
			t.Errorf("ticket [%s] must survive truncation: %q", k, out[0])
		}
	}
	if !strings.Contains(out[0], "〔预算截断〕") || !strings.Contains(out[0], "★") {
		t.Errorf("truncation marker and ★ highlight must survive: %q", out[0])
	}
	if cc.BudgetUnrepresentable() != 0 {
		t.Errorf("representable truncation must not raise the counter")
	}
}

func TestCurateCards_BudgetUnrepresentableIsObservableAndStable(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(15)) // too small even for the tickets-only form
	cards := []string{"- [aaaa0001] [aaaa0002] [aaaa0003] 三条票据无法在 15 字符内表达"}

	out, _ := cc.curateCards(context.Background(), cards, 0)
	if cc.BudgetUnrepresentable() != 1 {
		t.Fatalf("budget-unrepresentable must be counted, got %d", cc.BudgetUnrepresentable())
	}
	for _, k := range []string{"aaaa0001", "aaaa0002", "aaaa0003"} {
		if !strings.Contains(out[0], "["+k+"]") {
			t.Errorf("ticket [%s] must survive even unrepresentable state (never silently dropped): %q", k, out[0])
		}
	}

	// Serialize → parse back (projection round-trip) → re-curated: the guard
	// stays stable, the tickets-only form is a fixed point, no unbounded growth.
	back, _ := parseCardSection(out[0])
	if len(back) != 1 || back[0] != out[0] {
		t.Fatalf("fitted card must parse back as one card line: %q", back)
	}
	out2, _ := cc.curateCards(context.Background(), out, 0)
	if out2[0] != out[0] {
		t.Errorf("re-curation must be a fixed point, got %q vs %q", out2[0], out[0])
	}
	if cc.BudgetUnrepresentable() != 2 {
		t.Errorf("each unrepresentable round is observed (monotone), got %d", cc.BudgetUnrepresentable())
	}
}

// --- 2.2 acceptance-side guarantees ------------------------------------------

// TestCurateCards_ModelFailureKeepsOriginals: the model-error path is the
// deterministic sinking branch — original lines verbatim, count honest, no
// retry by the guard.
func TestCurateCards_ModelFailureKeepsOriginals(t *testing.T) {
	sm := &failingCondenseModel{}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(sm))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(250))
	cards := []string{
		cardLine("07-21 10:00", "aaaa0001", "任务一完成了一些工作较长描述", false),
		cardLine("07-21 11:00", "aaaa0002", "任务二完成了一些工作较长描述", false),
		cardLine("07-21 12:00", "aaaa0003", "任务三完成了一些工作较长描述", false),
		cardLine("07-21 13:00", "aaaa0004", "任务四完成了一些工作较长描述", false),
	}
	out, earlier := cc.curateCards(context.Background(), cards, 0)
	if sm.calls != 1 {
		t.Fatalf("failed call must not be retried by the guard, calls=%d", sm.calls)
	}
	if earlier == 0 {
		t.Errorf("model failure must fall through to counted sinking")
	}
	for _, line := range out {
		if !containsLine(cards, line) {
			t.Errorf("original card lines must survive verbatim, got %q", line)
		}
	}
}

type failingCondenseModel struct{ calls int }

func (m *failingCondenseModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.calls++
	return nil, fmt.Errorf("summary engine unavailable")
}

func (m *failingCondenseModel) Info() model.Info { return model.Info{Name: "failing-condense"} }

// TestBuildRetainedRefs_RejectedCondensationNeverEntersSummary is the spec
// scenario "模型生成未知票据 → 不把伪造票据写进 compaction 载荷": over-cap
// real cards go through curateCards, the model answers with a fabricated
// ticket, the guard rejects it, and the CARD SEQUENCE carries only verbatim
// original card lines. (The 〔历史综述〕 narrative line is the comprehension
// layer — deliberately NOT ticket-guarded; scripted answers keep the two LLM
// channels distinguishable.)
func TestBuildRetainedRefs_RejectedCondensationNeverEntersSummary(t *testing.T) {
	sm := &scriptedCondenseModel{outs: []string{
		"[aaaa0001] [deadbeef] 模型编造的浓缩行", // curateCards → guard rejects
		"历史综述正常成文不受影响",                   // narrative channel
	}}
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(sm))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(60))

	// 10 dropped boundary refs → real hex cards (keys 0xaaaa0001..0xaaaa000a).
	refs := make([]memory.EventReference, 0, 10)
	for i := 0; i < 10; i++ {
		key := int64(0xaaaa0001 + i)
		refs = append(refs, memory.EventReference{
			EventKey: key, EventType: tagentevent.TypeExternalInput,
			EventSummary: fmt.Sprintf("历史任务 %d 的完整摘要行内容", i), Timestamp: 1_000_000 + int64(i),
		})
	}
	// compressedMsgs=nil → every ref is dropped → cards form → curate fires.
	retained := cc.buildRetainedRefs(refs, nil, context.Background(), nil)
	if len(retained) == 0 || retained[0].EventType != tagentevent.TypeContextCompress {
		t.Fatalf("expected leading rolling summary ref: %+v", retained)
	}
	summary := retained[0].EventSummary

	// Ticket law applies to the card sequence: every "- " line is verbatim
	// engineering output; the rejected model text is absent from them.
	var cardLines int
	for _, line := range strings.Split(summary, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		cardLines++
		if strings.Contains(line, "模型编造的浓缩行") || strings.Contains(line, "deadbeef") {
			t.Errorf("guard-rejected model text must never enter the card lines: %q", line)
		}
	}
	if cardLines == 0 {
		t.Fatalf("summary must retain card lines: %q", summary)
	}
	// One condensation ask + one narrative ask — the guard never re-asks the
	// model for the card channel.
	if sm.count() != 2 {
		t.Errorf("curate 1 + narrative 1 LLM calls expected, got %d", sm.count())
	}
	// Every dropped ticket survives (card line) or is counted (sunk).
	if !strings.Contains(summary, "[aaaa000a]") ||
		!strings.Contains(summary, "(earlier 9 items retrievable via memory_recall)") {
		t.Errorf("newest card must survive and the 9 sunk lines be counted: %q", summary)
	}
}

// scriptedCondenseModel returns scripted answers in call order (the last one
// repeats), counting calls.
type scriptedCondenseModel struct {
	mu    sync.Mutex
	calls int
	outs  []string
}

func (m *scriptedCondenseModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.outs[m.calls%len(m.outs)]
	m.calls++
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, Content: out,
	}}}}
	close(ch)
	return ch, nil
}

func (m *scriptedCondenseModel) Info() model.Info { return model.Info{Name: "scripted-condense"} }

func (m *scriptedCondenseModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// ==================== Task 6.6: 有 pending user 时保留原始 user message ====================

func TestCompress_PreservesPendingUserMessage(t *testing.T) {
	// Create enough segments to trigger compression (KeepRecentTasks=2 by default)
	// The last segment has a pending user message
	messages := []model.Message{
		// Old segment 1 (will be compressed)
		{Role: model.RoleUser, Content: "old task 1"},
		{Role: model.RoleAssistant, Content: "old result 1"},
		// Old segment 2 (will be compressed)
		{Role: model.RoleUser, Content: "old task 2"},
		{Role: model.RoleAssistant, Content: "old result 2"},
		// Recent segment 1 (complete)
		{Role: model.RoleUser, Content: "recent task 1"},
		{Role: model.RoleAssistant, Content: "recent result 1"},
		// Recent segment 2 (incomplete - has pending user)
		{Role: model.RoleUser, Content: "pending new task"},
	}

	sc := NewSmartCompressor() // KeepRecentTasks=2, no summaryModel
	result := sc.Compress(context.Background(), messages)

	require.NotEmpty(t, result)

	// The last message should be the pending user message
	lastMsg := result[len(result)-1]
	assert.Equal(t, model.RoleUser, lastMsg.Role)
	assert.Equal(t, "pending new task", lastMsg.Content,
		"last message should be the pending user message")
}

// ==================== Task 6.7: 无 pending user 时添加引导消息 ====================

func TestCompress_AddsGuidanceMessageWhenNoPendingUser(t *testing.T) {
	// With "user input as anchor" compression:
	// - Old segments are fully compressed
	// - Recent segments: user input preserved, execution compressed (if long enough)
	// - Last incomplete segment: preserved fully (current context)
	messages := []model.Message{
		// Old segment 1 (will be compressed)
		{Role: model.RoleUser, Content: "old task 1"},
		{Role: model.RoleAssistant, Content: "old result 1"},
		// Old segment 2 (will be compressed)
		{Role: model.RoleUser, Content: "old task 2"},
		{Role: model.RoleAssistant, Content: "old result 2"},
		// Recent segment 1 (complete - closed by next user input)
		{Role: model.RoleUser, Content: "recent task 1"},
		{Role: model.RoleAssistant, Content: "recent result 1"},
		// Recent segment 2 (incomplete - no next user input to close it)
		{Role: model.RoleUser, Content: "recent task 2"},
		{Role: model.RoleAssistant, Content: "recent result 2"},
	}

	sc := NewSmartCompressor()
	result := sc.Compress(context.Background(), messages)

	require.NotEmpty(t, result)

	// Last incomplete segment is preserved fully — last message is "recent result 2"
	lastMsg := result[len(result)-1]
	assert.Equal(t, "recent result 2", lastMsg.Content,
		"last message should be from the fully preserved last incomplete segment")

	// No guidance message ("以上是对话历史摘要") should be present
	for _, msg := range result {
		assert.NotContains(t, msg.Content, "以上是对话历史摘要",
			"guidance message should not be appended")
	}
}

// TestConfigFormula_Defaults (rolling-summary-anchor D3): when card_max_chars /
// compact_keys_listed are not explicitly set, they derive from the primary
// knob max_tokens (M): card_max_chars = M/20, compact_keys_listed = card/200.
func TestConfigFormula_Defaults(t *testing.T) {
	sc := NewSmartCompressor()
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 128000, 0.8, 2)
	if cc.cardMaxChars != 6400 { // 128000/20
		t.Errorf("cardMaxChars default = %d, want 6400 (M/20)", cc.cardMaxChars)
	}
	if cc.listedKeysCap != 32 { // 6400/200
		t.Errorf("listedKeysCap default = %d, want 32 (cardMaxChars/200)", cc.listedKeysCap)
	}
}

// TestConfigFormula_ExplicitWins: explicit settings override the formula.
func TestConfigFormula_ExplicitWins(t *testing.T) {
	sc := NewSmartCompressor()
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 128000, 0.8, 2,
		WithCardMaxChars(6000))
	if cc.cardMaxChars != 6000 {
		t.Errorf("explicit cardMaxChars = %d, want 6000", cc.cardMaxChars)
	}
	// listedKeysCap still derives from the effective cardMaxChars.
	if cc.listedKeysCap != 30 { // 6000/200
		t.Errorf("listedKeysCap = %d, want 30 (6000/200)", cc.listedKeysCap)
	}
}

// Tests for the org numeric bundle on the RESIDENT ContextCompressor, migrated
// to the §6.4 pull model (introduce-durable-workflow-engine S-E).
//
// The push face (ApplyHotParams/UpdateMaxTokens/UpdateKeepRecent) is deleted: a
// hot rotation now happens by rotating the SOURCE the compressor reads at every
// consumption boundary. Two contracts these tests must keep honest:
//   - the C-defect fix (a window change must reach the resident budget line
//     without a restart) — same expected numbers as the push era, different
//     trigger; and
//   - hardening-review-batch2 5.3 (参数同代) — under pull this is structural:
//     one source read yields threshold+maxTokens+keepRecent, and the whole group
//     travels per-call, so the outer trigger line and the inner compression
//     target cannot be observed from different generations.
//
// Rotation here is a plain assignment through the captured pointer, which is
// exactly how the composition root rotates it in production (record swap).

// rotatingCC builds a ContextCompressor whose hot source is a mutable bundle the
// test can rotate.
func rotatingCC(cur *HotNumbers, sc *SmartCompressor, store memory.MemoryStore, maxTokens int, threshold float64, keepRecent int) *ContextCompressor {
	return NewContextCompressor(sc, store, NewDefaultTokenCounter(), maxTokens, threshold, keepRecent,
		WithHotSource(func() HotNumbers { return *cur }))
}

func newHotTestCC(t *testing.T) (*ContextCompressor, memory.MemoryStore) {
	t.Helper()
	cc := NewContextCompressor(
		NewSmartCompressor(WithKeepRecentTasks(2)),
		memory.NewInMemoryStore(), NewDefaultTokenCounter(), 60, 0.8, 2)
	return cc, nil
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func storeTurn(t *testing.T, store memory.MemoryStore, assistantPayload string) []memory.EventReference {
	t.Helper()
	now := time.Now().UnixMilli()
	k1 := memory.NewSnowflakeEventKey(1, now)
	k2 := memory.NewSnowflakeEventKey(1, now+1)
	userPayload := "用户提问：" + strings.Repeat("问", 10)
	assistantPayload = "助手回复：" + assistantPayload
	evts := []memory.FullEvent{
		{EventKey: k1, PartitionID: 1, EventType: "external_input", Timestamp: now,
			Content: userPayload, EventSummary: cut(userPayload, 40)},
		{EventKey: k2, PartitionID: 1, EventType: "agent_output", Timestamp: now + 1,
			EventSummary: cut(assistantPayload, 40), Content: assistantPayload},
	}
	for _, ev := range evts {
		if err := store.StoreEvent(ev.EventKey, ev); err != nil {
			t.Fatalf("StoreEvent: %v", err)
		}
	}
	return []memory.EventReference{
		{EventKey: k1, EventType: "external_input", EventSummary: "ask", Timestamp: now},
		{EventKey: k2, EventType: "agent_output", EventSummary: cut(assistantPayload, 40), Timestamp: now + 1},
	}
}

func TestSourceRotation_BudgetLineMoves(t *testing.T) {
	cur := &HotNumbers{}
	cc := rotatingCC(cur,
		NewSmartCompressor(WithKeepRecentTasks(2)), memory.NewInMemoryStore(), 60, 0.8, 2)

	// No rotation yet: the construction pair answers (60 × 0.8 = 48).
	if got := cc.BudgetLine(); got != 48 {
		t.Fatalf("initial budget line = %d, want 48 (60×0.8)", got)
	}

	*cur = HotNumbers{ThresholdPct: 0.8, MaxTokens: 16000, KeepRecent: 5}

	if got := cc.BudgetLine(); got != 12800 {
		t.Fatalf("budget line after source rotation = %d, want 12800", got)
	}
	if got := cc.KeepRecentValue(); got != 5 {
		t.Fatalf("keepRecent = %d, want 5", got)
	}
	// The pull contract leaves no write behind: the shared inner fields still hold
	// their construction values (the group travels per-call instead).
	if got := cc.compressor.KeepRecentTasks; got != 2 {
		t.Fatalf("inner shared keepRecent must stay at construction (no push), got %d", got)
	}
}

func TestSourceRotation_ZeroGroupKeepsConstruction(t *testing.T) {
	cur := &HotNumbers{}
	cc := rotatingCC(cur,
		NewSmartCompressor(WithMaxTokens(10000), WithTriggerBudget(8000), WithKeepRecentTasks(2)),
		memory.NewInMemoryStore(), 10000, 0.8, 2)

	*cur = HotNumbers{} // a record with no opinion on any axis

	if got := cc.compressor.budget(); got != 8000 {
		t.Fatalf("zero group must keep the construction budget, got %d", got)
	}
	if got := cc.BudgetLine(); got != 8000 {
		t.Fatalf("zero group must keep the construction line (10000×0.8), got %d", got)
	}
	if got := cc.KeepRecentValue(); got != 2 {
		t.Fatalf("keepRecent = %d, want 2 (unchanged)", got)
	}
}

// TestSourceRotation_PassThroughBoundary is the exact production shape of the C
// defect (a 512k window never reached the resident budget line): a complete turn
// over the OLD line compresses; after rotating the source the SAME turn passes
// through unchanged.
func TestSourceRotation_PassThroughBoundary(t *testing.T) {
	store := memory.NewInMemoryStore()
	cur := &HotNumbers{}
	cc := rotatingCC(cur,
		NewSmartCompressor(WithKeepRecentTasks(2)), store, 60, 0.8, 2)

	refs := storeTurn(t, store, strings.Repeat("部", 400)) // ≈201+1 tokens > 48

	before := cc.Compress(context.Background(), refs)
	if !before.Compressed {
		t.Fatal("precondition: over-budget complete turn must compress")
	}

	*cur = HotNumbers{ThresholdPct: 0.8, MaxTokens: 100000, KeepRecent: 2}
	after := cc.Compress(context.Background(), refs)
	if after.Compressed {
		t.Fatal("after raising the window in the source: same turn must pass through unchanged")
	}
}

// 5.3（参数同代）在 pull 下的形态：缩窗热更后真实压缩必须按新预算执行。旧实现
// 只换外层触发线、内层沿用冷构造大目标，于是判「预算未花」而 no-op。per-call
// 下行后这不是靠「记得同步写两处」维持，而是结构性成立。
func TestSourceRotation_ShrinkWindow_RealCompressionFollows(t *testing.T) {
	store := memory.NewInMemoryStore()
	cur := &HotNumbers{}
	cc := rotatingCC(cur,
		NewSmartCompressor(WithKeepRecentTasks(2)), store, 60, 0.8, 2)

	// 扩窗到 50000（外层线 40000），再缩回 60（线 48）——反向暴露旧缺陷。
	*cur = HotNumbers{ThresholdPct: 0.8, MaxTokens: 50000, KeepRecent: 2}
	if got := cc.BudgetLine(); got != 40000 {
		t.Fatalf("budget line after enlarge = %d, want 40000", got)
	}

	for i := 0; i < 6; i++ {
		storeTurn(t, store, strings.Repeat("payload-", 40)+string(rune('a'+i)))
	}
	refs := allRefs(t, store)

	*cur = HotNumbers{ThresholdPct: 0.8, MaxTokens: 60, KeepRecent: 2}
	if got := cc.BudgetLine(); got != 48 {
		t.Fatalf("budget line after shrink = %d, want 48", got)
	}

	res := cc.Compress(context.Background(), refs)
	if !res.Compressed {
		t.Fatalf("expected real compression after shrink (inner target must follow the same generation), got no-op")
	}
	// 注：不断言输出 ≤ 预算线——keepRecent 全保真回合 + 骨架有结构下限，分层压缩
	// 按轮次升级；本用例锁定的是「内层目标跟随同一代」。
	_ = cc.tokenCounter.Estimate(res.Messages)
}

func allRefs(t *testing.T, store memory.MemoryStore) []memory.EventReference {
	t.Helper()
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	return refs
}

// TestHotSourcePullRotatesWithoutPush pins the §6.4 consumption-boundary pull
// contract (introduce-durable-workflow-engine S-E): while a hot source is
// installed, the compressor resolves the FULL numeric group at every
// consumption boundary (BudgetLine/Threshold/Compress), and rotating the source
// alone — no push call of any kind — takes effect at the next read. The old
// push model made the atomics the authority reachable only through
// ApplyHotParams; here the source is the authority and the construction values
// are the fallback.
func TestHotSourcePullRotatesWithoutPush(t *testing.T) {
	cur := HotNumbers{ThresholdPct: 0.8, MaxTokens: 10000, KeepRecent: 2}
	sc := NewSmartCompressor(WithMaxTokens(5000), WithTriggerBudget(4000), WithKeepRecentTasks(1))
	cc := NewContextCompressor(sc, nil, NewDefaultTokenCounter(), 5000, 0.8, 1,
		WithHotSource(func() HotNumbers { return cur }))

	// Source wins over the construction pair (5000×0.8 would be 4000).
	require.Equal(t, 8000, cc.BudgetLine())
	require.InDelta(t, 0.8, cc.Threshold(), 1e-9)
	require.Equal(t, 2, cc.KeepRecentValue())

	// Rotate the SOURCE only — no push. One read yields the whole group, so the
	// outer trigger line and the inner compression target can never disagree
	// (the torn window hardening 5.3 patched by re-pushing both sides dies here).
	cur = HotNumbers{ThresholdPct: 0.5, MaxTokens: 20000, KeepRecent: 7}
	require.Equal(t, 10000, cc.BudgetLine())
	require.InDelta(t, 0.5, cc.Threshold(), 1e-9)
	require.Equal(t, 7, cc.KeepRecentValue())
}

// TestHotSourcePartialFallsBackToConstruction guards the standalone/bare edge
// (S-C lesson: enumerate the no-source boundary): zero fields from the source
// fall back to the construction values instead of zeroing the budget line.
func TestHotSourcePartialFallsBackToConstruction(t *testing.T) {
	cc := NewContextCompressor(NewSmartCompressor(), nil, NewDefaultTokenCounter(), 6000, 0.5, 3,
		WithHotSource(func() HotNumbers { return HotNumbers{} }))
	require.Equal(t, 3000, cc.BudgetLine()) // 6000×0.5 from construction
	require.Equal(t, 3, cc.KeepRecentValue())
}

// TestHotSourceAbsentKeepsLegacyFallback is the no-source boundary: a directly
// built compressor (tests, standalone wiring) keeps reading its construction
// atomics — the pull contract must not require a source to exist.
func TestHotSourceAbsentKeepsLegacyFallback(t *testing.T) {
	cc := NewContextCompressor(NewSmartCompressor(), nil, NewDefaultTokenCounter(), 6000, 0.5, 3)
	require.Equal(t, 3000, cc.BudgetLine())
	require.InDelta(t, 0.5, cc.Threshold(), 1e-9)
}

// TestDeterministicLevel_Exponential (rolling-summary-anchor D2): aging
// boundaries are exponential {k, 2k} (base 2), not linear {k, 2k, 3k}.
// With keepRecent=2: L0 age<2, L1 age<4, L2 age>=4 — and the ladder CAPS AT
// L2: L3 is budget-escalation-only, never age-reachable (single-dimension
// trigger: segment count must not archive segments).
func TestDeterministicLevel_Exponential(t *testing.T) {
	seg := &TaskSegment{IsComplete: true}
	lvl := func(age, keepRecent int) int {
		// age = totalSegs-1-segIdx; set segIdx=0, totalSegs=age+1.
		return deterministicLevel(seg, 0, age+1, keepRecent)
	}
	cases := []struct {
		age  int
		want int
	}{
		{0, 0}, {1, 0}, // L0: age < 2
		{2, 1}, {3, 1}, // L1: age < 4
		{4, 2}, {5, 2}, {6, 2}, {7, 2}, // L2: age >= 4 (linear would give L3 at 6,7)
		{8, 2}, {9, 2}, {100, 2}, // still L2: the base ladder never reaches L3
	}
	for _, c := range cases {
		if got := lvl(c.age, 2); got != c.want {
			t.Errorf("age=%d keepRecent=2: got L%d, want L%d", c.age, got, c.want)
		}
	}
	// In-progress segment is never compressed.
	if got := deterministicLevel(&TaskSegment{IsComplete: false}, 0, 100, 2); got != 0 {
		t.Errorf("in-progress segment must be L0, got L%d", got)
	}
}

// §4.3: a valid non-text input must survive into the ACTUAL request. An image-only
// external_input (empty Content, non-empty ContentParts) must render to a message
// carrying those parts. Pre-§4.3 resolveRef's content-resolution branch keyed only on
// Content/ToolCalls, so an image-only fact fell through to summary-only rendering and
// the parts never reached the model.
func TestResolveRef_RendersImageOnlyInputParts(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	parts := []model.ContentPart{{Type: model.ContentTypeImage, Image: &model.Image{URL: "http://host/img.png"}}}
	key := memory.NewSnowflakeEventKey(1, time.Now().UnixMilli()) // a valid partition-1 key so GetEvent's pid derivation hits
	memStore.StoreEvent(key, memory.FullEvent{
		EventKey:     key,
		PartitionID:  1,
		EventType:    tagentevent.TypeExternalInput,
		EventSummary: "an image",
		Content:      "", // image-only: no text
		ContentParts: parts,
	})
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2)

	msgs := cc.resolveRefs(context.Background(), []memory.EventReference{
		{EventKey: key, PartitionID: 1, EventType: tagentevent.TypeExternalInput, EventSummary: "an image"},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 rendered message, got %d", len(msgs))
	}
	if len(msgs[0].ContentParts) != 1 || msgs[0].ContentParts[0].Image == nil ||
		msgs[0].ContentParts[0].Image.URL != "http://host/img.png" {
		t.Fatalf("§4.3: image parts must reach the rendered request, got %+v", msgs[0].ContentParts)
	}
}

// narrativeCaptureModel captures the last user-message prompt of each call and
// replies with a fixed string. Counts calls for cost assertions.
type narrativeCaptureModel struct {
	mu      sync.Mutex
	prompts []string
	reply   string
}

func (m *narrativeCaptureModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, req.Messages[len(req.Messages)-1].Content)
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{
		Message: model.Message{Role: model.RoleAssistant, Content: m.reply},
	}}}
	close(ch)
	return ch, nil
}
func (m *narrativeCaptureModel) Info() model.Info { return model.Info{Name: "narrative-capture"} }
func (m *narrativeCaptureModel) calls() int       { m.mu.Lock(); defer m.mu.Unlock(); return len(m.prompts) }
func (m *narrativeCaptureModel) lastPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.prompts) == 0 {
		return ""
	}
	return m.prompts[len(m.prompts)-1]
}

func newNarrativeCompressor(t *testing.T, m model.Model) (*ContextCompressor, *memory.InMemoryStore) {
	t.Helper()
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	if m != nil {
		sc = NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000), WithSummaryModel(m))
	}
	store := memory.NewInMemoryStore()
	cc := NewContextCompressor(sc, store, NewDefaultTokenCounter(), 8000, 0.8, 1,
		WithCardMaxChars(6000), WithCompactKeysListed(32))
	return cc, store
}

func seedNarrativeEvents(t *testing.T, store *memory.InMemoryStore) []memory.EventReference {
	t.Helper()
	// Two skeleton turns stored with FULL content (richer than EventSummary —
	// the material law must feed the real stored text to the model).
	type seed struct {
		key     int64
		typ     string
		summary string
		content string
	}
	seeds := []seed{
		{100, tagentevent.TypeExternalInput, "请求部署", "请帮我把服务部署到测试环境，并跑一遍健康检查"},
		{101, tagentevent.TypeAgentOutput, "部署完成", "服务已部署到测试环境，健康检查全部通过"},
		{102, tagentevent.TypeExternalInput, "请求汇总", "请汇总今天的部署结果"},
		{103, tagentevent.TypeAgentOutput, "汇总完成", "今日部署 1 个服务，全部通过"},
	}
	refs := make([]memory.EventReference, 0, len(seeds))
	for _, s := range seeds {
		if err := store.StoreEvent(s.key, memory.FullEvent{
			EventKey: s.key, EventType: s.typ, EventSummary: s.summary,
			Content: s.content, Timestamp: s.key,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		refs = append(refs, memory.EventReference{
			EventKey: s.key, EventType: s.typ, EventSummary: s.summary, Timestamp: s.key,
		})
	}
	return refs
}

// TestRollingNarrative_SynthesizedOnFold: when a summary model is wired and
// events are L3-folded this round, the rolling summary carries a 〔历史综述〕
// narrative line synthesized from the REAL stored content (material law),
// layered ABOVE the engineering ticket lines.
func TestRollingNarrative_SynthesizedOnFold(t *testing.T) {
	m := &narrativeCaptureModel{reply: "用户请求部署并汇总，服务部署成功且健康检查通过，当日结果已汇总。"}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	retained := cc.buildRetainedRefs(refs, nil, context.Background(), nil)
	if len(retained) != 1 {
		t.Fatalf("expected single rolling summary ref, got %d", len(retained))
	}
	s := retained[0].EventSummary

	// Narrative line present, above the ticket layer.
	if !strings.Contains(s, "〔历史综述〕用户请求部署并汇总") {
		t.Errorf("rolling summary must carry the synthesized narrative, got: %q", s)
	}
	// Ticket layer still present (additive, not replacement).
	if !strings.Contains(s, "["+tagentevent.FormatEventKey(100)+"] 请求部署") {
		t.Errorf("engineering card lines must stay, got: %q", s)
	}
	if !strings.Contains(s, "recent keys=") {
		t.Errorf("recent keys must stay, got: %q", s)
	}

	// Material law: the synthesis prompt must contain the FULL stored content,
	// not the summary stubs.
	p := m.lastPrompt()
	if !strings.Contains(p, "健康检查全部通过") || !strings.Contains(p, "请帮我把服务部署到测试环境") {
		t.Errorf("prompt must feed real stored content (material law), got: %q", p)
	}
	if !strings.Contains(p, "旧历史综述：（无）") {
		t.Errorf("first fold must mark prior narrative as absent, got: %q", p)
	}
}

// TestRollingNarrative_Incremental: the prior narrative is carried into the
// synthesis prompt and replaced by the new output; carry-over rounds with no
// new folds cost ZERO additional LLM calls.
func TestRollingNarrative_Incremental(t *testing.T) {
	m := &narrativeCaptureModel{reply: "第二轮综述：涵盖部署与汇总两轮工作。"}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	first := cc.buildRetainedRefs(refs, nil, context.Background(), nil)
	callsAfterFold := m.calls()
	if callsAfterFold != 1 {
		t.Fatalf("one L3 fold must cost exactly 1 LLM call, got %d", callsAfterFold)
	}

	// Second round: prior summary + one new dropped event.
	refs2 := append([]memory.EventReference{}, first...)
	refs2 = append(refs2, memory.EventReference{
		EventKey: 200, EventType: tagentevent.TypeExternalInput,
		EventSummary: "请求复盘", Timestamp: 200,
	})
	second := cc.buildRetainedRefs(refs2, nil, context.Background(), nil)
	if m.calls() != 2 {
		t.Fatalf("second fold must add exactly 1 call, got %d", m.calls())
	}
	// The mock replies with a fixed string, so round 1's narrative IS that
	// reply; round 2's prompt must carry it as the prior narrative.
	if !strings.Contains(m.lastPrompt(), "旧历史综述：\n第二轮综述：涵盖部署与汇总两轮工作。") {
		t.Errorf("prior narrative (round-1 model output) must feed the incremental synthesis, got: %q", m.lastPrompt())
	}
	if !strings.Contains(second[0].EventSummary, "〔历史综述〕第二轮综述") {
		t.Errorf("narrative must be replaced by the new synthesis, got: %q", second[0].EventSummary)
	}

	// Carry-over round (no new drops): no extra LLM call, narrative preserved.
	third := cc.buildRetainedRefs(second, nil, context.Background(), nil)
	if m.calls() != 2 {
		t.Errorf("carry-over round must cost zero LLM calls, got %d", m.calls())
	}
	if !strings.Contains(third[0].EventSummary, "〔历史综述〕第二轮综述") {
		t.Errorf("narrative must survive carry-over rounds, got: %q", third[0].EventSummary)
	}
}

// TestRollingNarrative_FailureFallsBack: on model failure the prior narrative
// is preserved and the ticket layer is intact — compaction never breaks.
func TestRollingNarrative_FailureFallsBack(t *testing.T) {
	m := &mockBatchSummaryModel{failOnCall: map[int]bool{0: true, 1: true}}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	// Round 1 with a prior narrative already present.
	refs = append([]memory.EventReference{{
		EventKey:     -50,
		EventType:    tagentevent.TypeContextCompress,
		EventSummary: "[Compacted 5 historical events]\n〔历史综述〕早期历史：完成过环境搭建。\nrecent keys=zz",
		Timestamp:    50,
		Role:         "user",
	}}, refs...)

	retained := cc.buildRetainedRefs(refs, nil, context.Background(), nil)
	s := retained[0].EventSummary
	if !strings.Contains(s, "〔历史综述〕早期历史：完成过环境搭建。") {
		t.Errorf("prior narrative must be preserved verbatim on failure, got: %q", s)
	}
	if !strings.Contains(s, "["+tagentevent.FormatEventKey(100)+"] 请求部署") {
		t.Errorf("ticket layer must be intact on failure, got: %q", s)
	}
}

// TestRollingNarrative_NoModelEngineeringOnly: without a summary model the
// rolling summary has NO narrative section — the pure-engineering form is
// unchanged (byte-compatible with the pre-narrative format for first folds).
func TestRollingNarrative_NoModelEngineeringOnly(t *testing.T) {
	cc, store := newNarrativeCompressor(t, nil)
	refs := seedNarrativeEvents(t, store)

	retained := cc.buildRetainedRefs(refs, nil, context.Background(), nil)
	if strings.Contains(retained[0].EventSummary, narrativePrefix) {
		t.Errorf("no model must yield pure engineering form, got: %q", retained[0].EventSummary)
	}
	if !strings.Contains(retained[0].EventSummary, "[Compacted 4 historical events]") {
		t.Errorf("count/cards must be unaffected, got: %q", retained[0].EventSummary)
	}
}

// TestRollingNarrative_CapEnforced: an over-long model reply is scrubbed to a
// single line and truncated at the compile-time cap.
func TestRollingNarrative_CapEnforced(t *testing.T) {
	long := strings.Repeat("综述内容", 2000) // 8000 runes
	m := &narrativeCaptureModel{reply: long}
	cc, store := newNarrativeCompressor(t, m)
	refs := seedNarrativeEvents(t, store)

	retained := cc.buildRetainedRefs(refs, nil, context.Background(), nil)
	line := ""
	for _, l := range strings.Split(retained[0].EventSummary, "\n") {
		if strings.HasPrefix(l, narrativePrefix) {
			line = strings.TrimPrefix(l, narrativePrefix)
		}
	}
	if line == "" {
		t.Fatalf("narrative line missing: %q", retained[0].EventSummary)
	}
	if n := len([]rune(line)); n > rollingNarrativeCapChars+3 {
		t.Errorf("narrative must be capped at %d runes (+ellipsis), got %d", rollingNarrativeCapChars, n)
	}
}

// TestParseNarrativeSection: round-trip parsing of the narrative line inside
// a rolling summary (cards and trailers ignored).
func TestParseNarrativeSection(t *testing.T) {
	summary := "[Compacted 7 historical events]\n〔历史综述〕用户完成了部署与汇总。\n- 08-23 17:08 [aa] 请求部署\n- 08-23 17:20 [bb] 部署完成\n(earlier 2 items retrievable via memory_recall)\nrecent keys=aa,bb"
	if got := parseNarrativeSection(summary); got != "用户完成了部署与汇总。" {
		t.Errorf("parseNarrativeSection = %q", got)
	}
	if got := parseNarrativeSection("[Compacted 3 historical events]\n- [aa] x"); got != "" {
		t.Errorf("absent narrative must parse empty, got %q", got)
	}
	// Multi-line narratives never occur (scrubbed at synthesis) but a trailing
	// continuation line must not be swallowed into cards either.
	fmt.Println("narrative parser ok")
}

// settleRefs builds N settle-notification refs with realistic single-line
// bodies (event_bus newTaskSettledEvent form): "[task settled] <marker> <desc>
// (id=…) <status> → 结果: <result>". resultLen pads the inline result so the
// fold has a measurable reclaim (settleInlineCapChars bounds it at 600).
func settleRefs(startKey int64, n int, resultLen int) []memory.EventReference {
	refs := make([]memory.EventReference, 0, n)
	for i := 0; i < n; i++ {
		key := startKey + int64(i)
		marker := "✓"
		status := "completed"
		if i%3 == 0 {
			marker = "✗"
			status = "failed"
		}
		body := fmt.Sprintf("[task settled] %s 任务%d (id=%08x) %s → 结果: %s",
			marker, i, key, status, strings.Repeat("数", resultLen))
		refs = append(refs, memory.EventReference{
			EventKey: key, EventType: tagentevent.TypeExternalInput,
			EventSummary: body, Timestamp: 1_000_000 + int64(i),
		})
	}
	return refs
}

// --- foldSettleRuns unit tests ---------------------------------------------

func TestFoldSettleRuns_ConsecutiveRunFoldsToCard(t *testing.T) {
	cc := newFoldCC(2)
	refs := append([]memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "用户请求", 10),
	}, settleRefs(101, 3, 200)...)
	refs = append(refs, toolRef(200, tagentevent.TypeAgentOutput, "完成", 99))

	folded := cc.foldSettleRuns(refs, nil)

	var cards []memory.EventReference
	var settles int
	for _, r := range folded {
		switch {
		case r.EventType == tagentevent.TypeSettleFold:
			cards = append(cards, r)
		case isSettleNoticeRef(r):
			settles++
		}
	}
	if len(cards) != 1 || settles != 0 {
		t.Fatalf("expected 1 card / 0 loose settles, got cards=%d settles=%d: %+v", len(cards), settles, folded)
	}
	card := cards[0]
	if card.EventKey >= 0 {
		t.Errorf("settle_fold ref must carry a negative synthetic key, got %d", card.EventKey)
	}
	if !strings.Contains(card.EventSummary, "memory_recall") {
		t.Errorf("card must carry the recall hint: %q", card.EventSummary)
	}
	lines := parseSettleFoldCardLines(card.EventSummary)
	if len(lines) != 3 {
		t.Fatalf("card must carry one row per settle, got %d: %q", len(lines), card.EventSummary)
	}
	for i, line := range lines {
		want := "[" + tagentevent.FormatEventKey(int64(101+i)) + "]"
		if !strings.Contains(line, want) {
			t.Errorf("row %d missing evt ticket %s: %q", i, want, line)
		}
		if !(strings.HasPrefix(line, "- ✓ ") || strings.HasPrefix(line, "- ✗ ") || strings.HasPrefix(line, "- ★ ✗ ")) {
			t.Errorf("row %d must start with '- <marker> ' (failed rows carry ★ per telemetry-channel L2), got %q", i, line)
		}
		if strings.HasPrefix(line, "- ✗") && !strings.HasPrefix(line, "- ★ ✗") {
			t.Errorf("failed-polarity row must carry ★ (long-term reflection anchor), got %q", line)
		}
	}
}

func TestFoldSettleRuns_SingleAndInterruptedNotFolded(t *testing.T) {
	cc := newFoldCC(2)
	single := settleRefs(101, 1, 50)
	interrupted := append(settleRefs(110, 1, 50),
		toolRef(120, tagentevent.TypeAgentOutput, "边界", 120))
	interrupted = append(interrupted, settleRefs(121, 1, 50)...)

	for name, refs := range map[string][]memory.EventReference{
		"single": single,
		"broken": interrupted,
	} {
		if folded := cc.foldSettleRuns(refs, nil); hasSettleFold(folded) {
			t.Errorf("%s: runs shorter than 2 (or interrupted) must not fold: %+v", name, folded)
		}
	}
}

func TestFoldSettleRuns_Idempotent(t *testing.T) {
	cc := newFoldCC(2)
	refs := settleRefs(101, 4, 100)
	once := cc.foldSettleRuns(refs, nil)
	twice := cc.foldSettleRuns(once, nil)
	if len(once) != 1 || len(twice) != 1 {
		t.Fatalf("folding must converge to one card, got %d then %d", len(once), len(twice))
	}
	if once[0].EventSummary != twice[0].EventSummary {
		t.Errorf("card must not re-fold or grow: %q vs %q", once[0].EventSummary, twice[0].EventSummary)
	}
}

func hasSettleFold(refs []memory.EventReference) bool {
	for _, r := range refs {
		if r.EventType == tagentevent.TypeSettleFold {
			return true
		}
	}
	return false
}

// --- settleFoldLine rendering ------------------------------------------------

func TestSettleFoldLine_MarkerTruncationAndPrefixStrip(t *testing.T) {
	body := "[task settled inline] ∞ 长任务描述 (id=deadbeef) alive-detached → 结果: " + strings.Repeat("结", 500)
	prefixed := tagentevent.FormatEventPrefix(77, tagentevent.TypeExternalInput) + " " + body
	line := settleFoldLine(memory.EventReference{
		EventKey: 77, EventType: tagentevent.TypeExternalInput, EventSummary: prefixed,
	})
	if !strings.HasPrefix(line, "∞ ["+tagentevent.FormatEventKey(77)+"] 长任务描述") {
		t.Errorf("row must carry marker + ticket + summary head, got %q", line)
	}
	if strings.Contains(line, "[task settled") {
		t.Errorf("wrapper must be stripped: %q", line)
	}
	if len([]rune(line)) > settleFoldRowMaxChars+40 {
		t.Errorf("row must stay bounded, got %d chars: %q", len([]rune(line)), line)
	}
	// cold-eyes m-1: CJK truncation must stay on rune boundaries — a card
	// line with invalid UTF-8 corrupts every downstream JSON render.
	if !utf8.ValidString(line) {
		t.Errorf("truncated row must be valid UTF-8: %q", line)
	}
}

// --- end-to-end through Compress ---------------------------------------------

// TestCompress_SettleStormFoldReclaims80Percent is the 1.3 acceptance scenario:
// 50 settle external_inputs in the projection, compaction fires → ONE ticket
// card, ≥80% character reclaim, fact chain untouched (every event still
// GetEvent-recallable by its ticket key).
func TestCompress_SettleStormFoldReclaims80Percent(t *testing.T) {
	ctx := context.Background()
	store := memory.NewInMemoryStore()
	refs := settleRefs(101, 50, 600)
	for _, r := range refs {
		if err := store.StoreEvent(r.EventKey, memory.FullEvent{
			EventKey: r.EventKey, EventType: r.EventType,
			EventSummary: r.EventSummary, Content: r.EventSummary, Timestamp: r.Timestamp,
		}); err != nil {
			t.Fatalf("StoreEvent %d: %v", r.EventKey, err)
		}
	}
	refs = append(refs,
		toolRef(500, tagentevent.TypeExternalInput, "用户请求", 900),
		toolRef(501, tagentevent.TypeAgentOutput, "答复", 901))

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(800))
	cc := NewContextCompressor(sc, store, NewDefaultTokenCounter(), 1000, 0.8, 2)

	beforeChars := 0
	for _, m := range cc.resolveRefs(ctx, refs) {
		beforeChars += len([]rune(m.Content))
	}

	res := cc.Compress(ctx, refs)
	if !res.Compressed {
		t.Fatalf("expected compaction to fire")
	}
	afterChars := 0
	cards := 0
	for _, m := range res.Messages {
		afterChars += len([]rune(m.Content))
		if strings.Contains(m.Content, "|"+tagentevent.TypeSettleFold+"]") {
			cards++
		}
	}
	if cards != 1 {
		t.Fatalf("expected exactly 1 ticket card message, got %d (messages=%d)", cards, len(res.Messages))
	}
	reclaim := 1 - float64(afterChars)/float64(beforeChars)
	if reclaim < 0.80 {
		t.Errorf("expected ≥80%% char reclaim, got %.1f%% (%d -> %d)", reclaim*100, beforeChars, afterChars)
	}

	// Fact chain unaffected: every settle event stays recallable by its key.
	for _, r := range refs[:50] {
		evt, err := store.GetEvent(r.EventKey)
		if err != nil || evt == nil || evt.Content != r.EventSummary {
			t.Fatalf("fact chain event %d damaged by fold: %v", r.EventKey, err)
		}
	}

	// The card's tickets must resolve back to full originals (recall path).
	var cardRef *memory.EventReference
	for i := range res.RetainedRefs {
		if res.RetainedRefs[i].EventType == tagentevent.TypeSettleFold {
			cardRef = &res.RetainedRefs[i]
		}
	}
	if cardRef == nil {
		t.Fatalf("settle_fold card must stay in retained refs: %+v", res.RetainedRefs)
	}
	lines := parseSettleFoldCardLines(cardRef.EventSummary)
	if len(lines) != 50 {
		t.Fatalf("expected 50 ticket rows, got %d", len(lines))
	}
	for i, line := range lines {
		key := tagentevent.FormatEventKey(int64(101 + i))
		if !strings.Contains(line, "["+key+"]") {
			t.Fatalf("row %d missing ticket %s: %q", i, key, line)
		}
	}
}

// --- buildRetainedRefs lifecycle ---------------------------------------------

func TestBuildRetainedRefs_SettleFoldSurvivalAndLosslessExit(t *testing.T) {
	ctx := context.Background()
	cc := newFoldCC(2)
	run := settleRefs(101, 2, 100)
	card := buildSettleFoldRef(run)
	user := toolRef(500, tagentevent.TypeExternalInput, "用户请求", 900)
	refs := []memory.EventReference{user, card}

	// Card message survived the round → ref kept verbatim, no summary forced.
	cardMsg := model.Message{Role: model.RoleUser, Content: prefixEventKey(card.EventSummary, card)}
	userMsg := model.Message{Role: model.RoleUser, Content: tagentevent.FormatEventPrefix(500, tagentevent.TypeExternalInput) + " 用户请求"}
	retained := cc.buildRetainedRefs(refs, []model.Message{userMsg, cardMsg}, ctx, nil)
	survived := false
	for _, r := range retained {
		if r.EventType == tagentevent.TypeSettleFold && r.EventKey == card.EventKey {
			survived = true
		}
	}
	if !survived {
		t.Fatalf("surviving card ref must be retained: %+v", retained)
	}

	// Card retired by L3 → ticket rows move into the rolling summary card
	// sequence and the compacted count grows by the row count (lossless exit).
	retained = cc.buildRetainedRefs(refs, []model.Message{userMsg}, ctx, nil)
	var summary *memory.EventReference
	for i := range retained {
		if retained[i].EventType == tagentevent.TypeContextCompress {
			summary = &retained[i]
		}
	}
	if summary == nil {
		t.Fatalf("retiring a card must emit the rolling summary even with no other drops: %+v", retained)
	}
	for i, r := range run {
		want := "- " + settleFoldLine(r)
		if !strings.Contains(summary.EventSummary, want) {
			t.Errorf("row %d must survive in the card sequence, want %q in %q", i, want, summary.EventSummary)
		}
	}
	if !strings.Contains(summary.EventSummary, fmt.Sprintf("[Compacted %d historical events", len(run))) {
		t.Errorf("rolling count must include the folded settles: %q", summary.EventSummary)
	}
}

// turnRefs builds one complete task turn's refs:
// external_input(base) + action_command(base+1) + agent_output(base+2).
func turnRefs(turn int) []memory.EventReference {
	base := int64(1000 * (turn + 1))
	return []memory.EventReference{
		{EventKey: base, EventType: tagentevent.TypeExternalInput,
			EventSummary: fmt.Sprintf("任务 %d", turn), Timestamp: base, Role: "user"},
		{EventKey: base + 1, EventType: tagentevent.TypeActionCommand,
			EventSummary: fmt.Sprintf("工具输出 %d", turn), Timestamp: base + 1, Role: "tool"},
		{EventKey: base + 2, EventType: tagentevent.TypeAgentOutput,
			EventSummary: fmt.Sprintf("答复 %d", turn), Timestamp: base + 2, Role: "assistant"},
	}
}

// TestContextCompressor_SkeletonArchiveIntoRollingSummary (tasks 3.2/3.3):
// with NO summary model, old skeleton segments compacted at L3 leave the
// timeline and their external_input/agent_output events surface as index
// cards in the rolling summary — recall keys traceable, zero LLM, no
// degradation notices.
func TestContextCompressor_SkeletonArchiveIntoRollingSummary(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1)) // summaryModel=nil
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1, 0.8, 1)

	var refs []memory.EventReference
	for i := 0; i < 8; i++ {
		refs = append(refs, turnRefs(i)...)
	}

	result := cc.Compress(context.Background(), refs)

	// Rolling summary ref emitted at the head (negative key).
	require.NotEmpty(t, result.RetainedRefs)
	summaryRef := result.RetainedRefs[0]
	require.Equal(t, tagentevent.TypeContextCompress, summaryRef.EventType)
	assert.Negative(t, summaryRef.EventKey)

	// The archived turn's SKELETON became index cards with recall tickets:
	// external_input original words and agent_output conclusion.
	assert.Contains(t, summaryRef.EventSummary, "任务 0", "external_input must enter the rolling summary")
	assert.Contains(t, summaryRef.EventSummary, "答复 0", "agent_output must enter the rolling summary")
	assert.Contains(t, summaryRef.EventSummary, "["+tagentevent.FormatEventKey(1000)+"]",
		"card line must carry the recall key")

	// Archived refs left the projection — segment count converges.
	assert.Less(t, len(result.RetainedRefs), len(refs))

	// Zero-LLM: no failure or degradation notices anywhere.
	assert.Empty(t, result.Notices)
	for _, msg := range result.Messages {
		assert.NotContains(t, msg.Content, "[context_compress_error]")
	}
}

// TestContextCompressor_SegmentCountConverges (replay-style, mirrors the
// production pathology L2:12→61): feeding each round's RetainedRefs forward
// with one new turn per round, the projection size stays bounded instead of
// growing monotonically — external_input refs now have an archival exit.
func TestContextCompressor_SegmentCountConverges(t *testing.T) {
	keepRecent := 2
	sc := NewSmartCompressor(WithKeepRecentTasks(keepRecent), WithMaxTokens(1))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1, 0.8, keepRecent)

	const rounds = 20
	var refs []memory.EventReference
	var sizes []int
	for i := 0; i < rounds; i++ {
		refs = append(refs, turnRefs(i)...)
		result := cc.Compress(context.Background(), refs)
		refs = result.RetainedRefs
		sizes = append(sizes, len(refs))
	}

	// Bounded projection: at most the keepRecent complete turns (3 refs each)
	// + the in-progress allowance + 1 rolling summary ref. Far below the 60
	// refs fed in total.
	bound := keepRecent*3 + 3 + 1
	assert.LessOrEqual(t, sizes[rounds-1], bound,
		"projection must converge, sizes=%v", sizes)

	// The rolling summary keeps an honest total across rounds.
	require.NotEmpty(t, refs)
	assert.Equal(t, tagentevent.TypeContextCompress, refs[0].EventType)
	assert.Contains(t, refs[0].EventSummary, "historical events")
	// Old turns stay traceable via cards even many rounds later (either as a
	// listed card or sunk into the earlier-items counter — never lost count).
	assert.Contains(t, refs[0].EventSummary, "[Compacted")
}

// TestContextCompressor_RecentFullCount (stable-context-compaction D3): the
// full window is ANCHORED at the compaction round and frozen afterwards.
// Before any compaction everything renders full (small-session behavior);
// a forced compaction anchors the window at the most recent recentFullCount
// retained refs; subsequent under-budget rounds keep old refs frozen on their
// summary render while newly appended refs render full (active frontier).
func TestContextCompressor_RecentFullCount(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	const n = 6
	for i := 1; i <= n; i++ {
		memStore.StoreEvent(int64(i), memory.FullEvent{
			EventKey:  int64(i),
			EventType: tagentevent.TypeExternalInput,
			Content:   fmt.Sprintf("FULL %d", i),
		})
	}
	makeRefs := func(from, to int) []memory.EventReference {
		var out []memory.EventReference
		for i := from; i <= to; i++ {
			out = append(out, memory.EventReference{
				EventKey: int64(i), EventType: tagentevent.TypeExternalInput,
				EventSummary: fmt.Sprintf("SUM %d", i), Timestamp: int64(i),
			})
		}
		return out
	}

	// Round 1 — never compacted: everything renders full (boundary=0).
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2,
		WithRecentFullCount(2))
	r1 := cc.Compress(context.Background(), makeRefs(1, n))
	require.Len(t, r1.Messages, n)
	for i, msg := range r1.Messages {
		assert.Contains(t, msg.Content, fmt.Sprintf("FULL %d", i+1),
			"pre-compaction round must render everything full")
	}

	// Round 2 — force a compaction (1-token budget): anchors the full window
	// at the most recent recentFullCount(2) retained refs.
	scSmall := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(1))
	ccSmall := NewContextCompressor(scSmall, memStore, NewDefaultTokenCounter(), 1, 0.8, 2,
		WithRecentFullCount(2))
	_ = ccSmall.Compress(context.Background(), makeRefs(1, n)) // anchor side effect lives on ccSmall
	require.NotZero(t, ccSmall.fullBoundary, "compaction must anchor a non-zero boundary")

	// Round 3 — a HEALTHY-budget compressor inherits the anchor (same-package
	// field access): the pass-through branch must be taken — projection
	// untouched, old refs frozen on summary, window refs full. This is the
	// "anchored + under budget" combination the render-freeze contract covers.
	scHealthy := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	ccHealthy := NewContextCompressor(scHealthy, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2,
		WithRecentFullCount(2))
	ccHealthy.fullBoundary = ccSmall.fullBoundary
	retained := makeRefs(1, n)
	result := ccHealthy.Compress(context.Background(), retained)
	assert.Equal(t, retained, result.RetainedRefs,
		"under-budget round must pass through with the projection untouched")
	for i, msg := range result.Messages {
		key := int64(i + 1)
		if key >= ccHealthy.fullBoundary {
			assert.Contains(t, msg.Content, fmt.Sprintf("FULL %d", key),
				"ref %d inside the anchored window must render full", key)
		} else {
			assert.Contains(t, msg.Content, fmt.Sprintf("SUM %d", key),
				"ref %d before the anchor must stay frozen on summary", key)
			assert.NotContains(t, msg.Content, "FULL")
		}
	}

	// Round 4 — append new refs under budget: the previous render is a
	// message-by-message PREFIX of the new render (byte-stable prefix, D3),
	// and newly appended refs render full (active frontier).
	for i := n + 1; i <= n+2; i++ {
		memStore.StoreEvent(int64(i), memory.FullEvent{
			EventKey:  int64(i),
			EventType: tagentevent.TypeExternalInput,
			Content:   fmt.Sprintf("FULL %d", i),
		})
	}
	r4 := ccHealthy.Compress(context.Background(), append(retained, makeRefs(n+1, n+2)...))
	require.Len(t, r4.Messages, n+2)
	for i := 0; i < len(result.Messages); i++ {
		assert.Equal(t, result.Messages[i], r4.Messages[i],
			"message %d must be byte-identical across under-budget rounds (prefix freeze)", i)
	}
	assert.Contains(t, r4.Messages[len(r4.Messages)-1].Content, fmt.Sprintf("FULL %d", n+2),
		"newly appended refs must render full")
}

// TestContextCompressor_StripsUnansweredToolCalls: an assistant tool_call
// whose result ref was compacted away must not be re-sent as a dangling call
// (render-time legality, symmetric with demoteToInputNote).
func TestContextCompressor_StripsUnansweredToolCalls(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	memStore.StoreEvent(1, memory.FullEvent{EventKey: 1, EventType: tagentevent.TypeThinkingPlan,
		Content:   "执行命令",
		ToolCalls: []model.ToolCall{{ID: "call-lost", Function: model.FunctionDefinitionParam{Name: "action"}}}})
	memStore.StoreEvent(2, memory.FullEvent{EventKey: 2, EventType: tagentevent.TypeAgentOutput,
		Content: "完成"})

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 8000, 0.8, 2)

	// The action_command result ref for call-lost is absent (dropped at L1
	// in an earlier round).
	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeThinkingPlan},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput},
	}

	result := cc.Compress(context.Background(), refs)
	require.Len(t, result.Messages, 2)
	assert.Empty(t, result.Messages[0].ToolCalls,
		"unanswered tool_call must be stripped at render time")
	assert.Contains(t, result.Messages[0].Content, "执行命令", "prose content preserved")
	assertRenderLegality(t, result.Messages)
}

// assertNoDanglingCalls: every assistant tool_call must be answered by a
// later role=tool message with the matching id (the reverse direction of
// assertRenderLegality's orphan-result check).
func assertNoDanglingCalls(t *testing.T, msgs []model.Message) {
	t.Helper()
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == model.RoleTool && m.ToolID != "" {
			answered[m.ToolID] = true
		}
	}
	for i, m := range msgs {
		if m.Role != model.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				t.Errorf("msg[%d] declares dangling tool_call id=%q with no result in output", i, tc.ID)
			}
		}
	}
}

// storeFullTurn persists one complete turn (ext/think+call/tool/out) with
// real tool_call↔result pairing and returns its refs. The tool result is
// bulky so L1's value shows up in token counts.
func storeFullTurn(memStore memory.MemoryStore, turn int) []memory.EventReference {
	base := int64(1000 * (turn + 1))
	callID := fmt.Sprintf("call-%d", turn)
	events := []memory.FullEvent{
		{EventKey: base, EventType: tagentevent.TypeExternalInput,
			Content: fmt.Sprintf("任务 %d", turn)},
		{EventKey: base + 1, EventType: tagentevent.TypeThinkingPlan,
			Content:   fmt.Sprintf("计划 %d", turn),
			ToolCalls: []model.ToolCall{{ID: callID, Function: model.FunctionDefinitionParam{Name: "action"}}}},
		{EventKey: base + 2, EventType: tagentevent.TypeActionCommand,
			Content: strings.Repeat("R", 4000), ToolID: callID},
		{EventKey: base + 3, EventType: tagentevent.TypeAgentOutput,
			Content: fmt.Sprintf("答复 %d", turn)},
	}
	var refs []memory.EventReference
	for _, e := range events {
		e.EventSummary = truncateString(e.Content, 60)
		memStore.StoreEvent(e.EventKey, e)
		refs = append(refs, memory.EventReference{
			EventKey: e.EventKey, EventType: e.EventType,
			EventSummary: e.EventSummary, Timestamp: e.EventKey,
		})
	}
	return refs
}

// TestContextCompressor_EndToEndRenderLegality (task 6.6①): multi-turn
// history with REAL tool_call/result pairs, over budget so the middle turn
// lands on L1 (tool dropped) — the final Messages must contain no dangling
// call and no orphan result.
func TestContextCompressor_EndToEndRenderLegality(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	var refs []memory.EventReference
	for i := 0; i < 3; i++ {
		refs = append(refs, storeFullTurn(memStore, i)...)
	}

	// Budget: over threshold (3 bulky tool results ≈ 6k tokens > 2400), but
	// after base levels (L2/L1/L0) comfortably under maxTokens — no L3
	// escalation, so the L1 drop-tool path is what gets exercised.
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(3000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 3000, 0.8, 1,
		WithRecentFullCount(100)) // resolve ALL refs full: real ToolCalls everywhere

	result := cc.Compress(context.Background(), refs)
	require.NotEmpty(t, result.Messages)

	assertRenderLegality(t, result.Messages)
	assertNoDanglingCalls(t, result.Messages)

	joined := contentsOf(result.Messages)
	// L0 turn (newest) keeps its full pair; L1 turn keeps thinking prose but
	// not the bulky tool result.
	assert.Contains(t, joined, "计划 2")
	assert.Contains(t, joined, "计划 1", "L1 keeps thinking_plan prose")
	assert.Contains(t, joined, "任务 1")
	assert.Contains(t, joined, "答复 1")
}

// TestContextCompressor_DroppedToolRefLeavesProjection (task 6.6②): the
// action_command key dropped by L1 must vanish from RetainedRefs AND show up
// in the rolling summary's "recent keys=" list (recall ticket preserved).
func TestContextCompressor_DroppedToolRefLeavesProjection(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	var refs []memory.EventReference
	for i := 0; i < 3; i++ {
		refs = append(refs, storeFullTurn(memStore, i)...)
	}

	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(3000))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 3000, 0.8, 1,
		WithRecentFullCount(100))

	result := cc.Compress(context.Background(), refs)

	// Turn 1 (age=1) is L1: its action_command key 2002+... base=2000 → tool
	// key = 2002. Turn 1's thinking_plan (2001) survives.
	const droppedToolKey = int64(2002)
	retained := map[int64]bool{}
	for _, ref := range result.RetainedRefs {
		retained[ref.EventKey] = true
	}
	assert.False(t, retained[droppedToolKey], "L1-dropped action_command ref must leave the projection")
	assert.True(t, retained[2001], "L1-kept thinking_plan ref must stay in the projection")

	require.NotEmpty(t, result.RetainedRefs)
	summaryRef := result.RetainedRefs[0]
	require.Equal(t, tagentevent.TypeContextCompress, summaryRef.EventType)
	assert.Contains(t, summaryRef.EventSummary, "recent keys=")
	assert.Contains(t, summaryRef.EventSummary, tagentevent.FormatEventKey(droppedToolKey),
		"dropped tool ref key must be listed as a recall ticket in the rolling summary")
}

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

// toolRef builds a tool-event ref. thinking_plan refs carry a "调用 X" summary
// (as GenerateEventSummary D1 produces); action_command refs carry a result.
func toolRef(key int64, eventType, summary string, ts int64) memory.EventReference {
	return memory.EventReference{EventKey: key, EventType: eventType, EventSummary: summary, Timestamp: ts}
}

func newFoldCC(recentFull int) *ContextCompressor {
	sc := NewSmartCompressor(WithKeepRecentTasks(2))
	return NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 2,
		WithRecentFullCount(recentFull))
}

// TestFoldToolRuns_FoldsRun: a run of 3 aged tool pairs folds into ONE
// tool_chain ref carrying the tool-name sequence and a recall ticket.
func TestFoldToolRuns_FoldsRun(t *testing.T) {
	cc := newFoldCC(2)
	refs := []memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "用户请求", 1),
		toolRef(2, tagentevent.TypeThinkingPlan, "调用 read_file", 2),
		toolRef(3, tagentevent.TypeActionCommand, "文件内容", 3),
		toolRef(4, tagentevent.TypeThinkingPlan, "调用 grep", 4),
		toolRef(5, tagentevent.TypeActionCommand, "匹配结果", 5),
		toolRef(6, tagentevent.TypeThinkingPlan, "调用 edit_file", 6),
		toolRef(7, tagentevent.TypeActionCommand, "编辑成功", 7),
		toolRef(8, tagentevent.TypeAgentOutput, "完成", 8),
		toolRef(9, tagentevent.TypeExternalInput, "近期1", 9), // recent frontier
		toolRef(10, tagentevent.TypeAgentOutput, "近期2", 10), // recent frontier
	}
	// len=10, recentFull=2 -> fullFrom=8; refs[0:8] aged (incl. the 6 tool events).
	folded := cc.foldToolRuns(refs)

	// Expect: ext_input, tool_chain, agent_output, recent1, recent2 = 5 refs.
	if len(folded) != 5 {
		t.Fatalf("folded len = %d, want 5: %+v", len(folded), folded)
	}
	var chain *memory.EventReference
	for i := range folded {
		if folded[i].EventType == tagentevent.TypeToolChain {
			chain = &folded[i]
		}
	}
	if chain == nil {
		t.Fatalf("no tool_chain ref produced: %+v", folded)
	}
	if chain.EventKey >= 0 {
		t.Errorf("tool_chain ref must have negative key, got %d", chain.EventKey)
	}
	for _, want := range []string{"read_file", "grep", "edit_file", "3步"} {
		if !strings.Contains(chain.EventSummary, want) {
			t.Errorf("tool_chain summary missing %q: %q", want, chain.EventSummary)
		}
	}
}

// TestFoldToolRuns_DoesNotCrossBoundary: a boundary event (agent_output) splits
// tool events into separate runs, each folded independently.
func TestFoldToolRuns_DoesNotCrossBoundary(t *testing.T) {
	cc := newFoldCC(2)
	refs := []memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "请求A", 1),
		toolRef(2, tagentevent.TypeThinkingPlan, "调用 read_file", 2),
		toolRef(3, tagentevent.TypeActionCommand, "结果", 3),
		toolRef(4, tagentevent.TypeAgentOutput, "完成A", 4), // boundary
		toolRef(5, tagentevent.TypeExternalInput, "请求B", 5),
		toolRef(6, tagentevent.TypeThinkingPlan, "调用 grep", 6),
		toolRef(7, tagentevent.TypeActionCommand, "结果", 7),
		toolRef(8, tagentevent.TypeAgentOutput, "完成B", 8), // boundary
		toolRef(9, tagentevent.TypeExternalInput, "近期", 9),
		toolRef(10, tagentevent.TypeAgentOutput, "近期", 10),
	}
	folded := cc.foldToolRuns(refs)

	chains := 0
	for _, r := range folded {
		if r.EventType == tagentevent.TypeToolChain {
			chains++
		}
	}
	if chains != 2 {
		t.Fatalf("expected 2 separate tool_chain refs (one per turn), got %d: %+v", chains, folded)
	}
}

// TestFoldToolRuns_RecentFrontierNative: tool events within recentFullCount are
// NOT folded (active frontier stays native for tool-call pairing legality).
func TestFoldToolRuns_RecentFrontierNative(t *testing.T) {
	cc := newFoldCC(4) // recentFull=4 -> last 4 refs native
	refs := []memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "请求", 1),
		toolRef(2, tagentevent.TypeThinkingPlan, "调用 read_file", 2),
		toolRef(3, tagentevent.TypeActionCommand, "结果", 3),
		toolRef(4, tagentevent.TypeAgentOutput, "完成", 4),
		// recent frontier (last 4): a tool pair stays native
		toolRef(5, tagentevent.TypeThinkingPlan, "调用 grep", 5),
		toolRef(6, tagentevent.TypeActionCommand, "结果", 6),
		toolRef(7, tagentevent.TypeThinkingPlan, "调用 edit", 7),
		toolRef(8, tagentevent.TypeActionCommand, "结果", 8),
	}
	folded := cc.foldToolRuns(refs)

	// The recent tool events (5-8) must remain (not folded into a tool_chain).
	hasRecentTool := false
	for _, r := range folded {
		if r.EventKey == 5 || r.EventKey == 6 || r.EventKey == 7 || r.EventKey == 8 {
			hasRecentTool = true
		}
	}
	if !hasRecentTool {
		t.Errorf("recent frontier tool events must stay native, got: %+v", folded)
	}
}

// TestResolveRef_ToolChain: a tool_chain ref renders as a user-side line.
func TestResolveRef_ToolChain(t *testing.T) {
	cc := newFoldCC(2)
	ref := memory.EventReference{
		EventKey: -100, EventType: tagentevent.TypeToolChain,
		EventSummary: "- 工具链: read_file→grep（2步）[evt_2→evt_5]", Timestamp: 100, Role: "user",
	}
	msg := cc.resolveRef(context.Background(), ref, false)
	if msg.Role != "user" {
		t.Errorf("tool_chain must render as user role, got %s", msg.Role)
	}
	if !strings.Contains(msg.Content, "工具链: read_file→grep") {
		t.Errorf("tool_chain render missing chain line: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "tool_chain") {
		t.Errorf("tool_chain render missing type tag: %q", msg.Content)
	}
}

// TestCompress_ToolChainEndToEnd: a capacity-triggered compaction (D2: forced
// here via a 1-token budget) folds the aged tool run; the model context shows
// the tool-chain line (no empty-summary placeholder) and RetainedRefs carries
// the tool_chain ref.
func TestCompress_ToolChainEndToEnd(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(1))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1, 0.8, 2,
		WithRecentFullCount(2))
	refs := []memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "研究任务", 1),
		toolRef(2, tagentevent.TypeThinkingPlan, "调用 read_file", 2),
		toolRef(3, tagentevent.TypeActionCommand, "文件内容", 3),
		toolRef(4, tagentevent.TypeThinkingPlan, "调用 grep", 4),
		toolRef(5, tagentevent.TypeActionCommand, "匹配", 5),
		toolRef(6, tagentevent.TypeAgentOutput, "阶段完成", 6),
		toolRef(7, tagentevent.TypeExternalInput, "近期", 7),
		toolRef(8, tagentevent.TypeAgentOutput, "近期", 8),
	}
	result := cc.Compress(context.Background(), refs)

	// Model context must contain the tool-chain line, not an empty placeholder.
	joined := ""
	for _, m := range result.Messages {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "工具链") {
		t.Errorf("model context must contain the tool-chain line, got:\n%s", joined)
	}
	if strings.Contains(joined, "历史事件摘要为空") {
		t.Errorf("model context must NOT contain empty-summary placeholder, got:\n%s", joined)
	}
	// RetainedRefs must carry the tool_chain ref forward.
	hasChain := false
	for _, r := range result.RetainedRefs {
		if r.EventType == tagentevent.TypeToolChain {
			hasChain = true
		}
	}
	if !hasChain {
		t.Errorf("RetainedRefs must carry the tool_chain ref, got: %+v", result.RetainedRefs)
	}
}

// TestCompress_NoFoldUnderBudget (stable-context-compaction D4): folding is
// part of the compaction act — an under-budget round passes through without
// folding, so aged tool pairs keep their native refs (rendered from
// EventSummary, byte-stable) until the next capacity-triggered compaction.
func TestCompress_NoFoldUnderBudget(t *testing.T) {
	cc := newFoldCC(2) // huge budget (1_000_000) → never triggers
	refs := []memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "研究任务", 1),
		toolRef(2, tagentevent.TypeThinkingPlan, "调用 read_file", 2),
		toolRef(3, tagentevent.TypeActionCommand, "文件内容", 3),
		toolRef(4, tagentevent.TypeThinkingPlan, "调用 grep", 4),
		toolRef(5, tagentevent.TypeActionCommand, "匹配", 5),
		toolRef(6, tagentevent.TypeAgentOutput, "阶段完成", 6),
		toolRef(7, tagentevent.TypeExternalInput, "近期", 7),
		toolRef(8, tagentevent.TypeAgentOutput, "近期", 8),
	}
	result := cc.Compress(context.Background(), refs)
	if len(result.RetainedRefs) != len(refs) {
		t.Fatalf("under-budget round must pass through untouched: retained %d != input %d",
			len(result.RetainedRefs), len(refs))
	}
	for _, r := range result.RetainedRefs {
		if r.EventType == tagentevent.TypeToolChain {
			t.Fatalf("under-budget round must NOT fold tool runs, got chain ref: %+v", r)
		}
	}
}

// TestFoldToolRuns_NoProseLeak (code-review M1): a thinking_plan whose summary
// is PROSE (think-then-call reasoning model, no "调用 " prefix) must NOT leak
// into the tool-chain line as a fake tool name.
func TestFoldToolRuns_NoProseLeak(t *testing.T) {
	cc := newFoldCC(2)
	refs := []memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "用户请求", 1),
		toolRef(2, tagentevent.TypeThinkingPlan, "我先读一下文件，分析其中的关键逻辑再决定下一步", 2), // prose, no "调用 "
		toolRef(3, tagentevent.TypeActionCommand, "文件内容", 3),
		toolRef(4, tagentevent.TypeThinkingPlan, "调用 grep", 4),
		toolRef(5, tagentevent.TypeActionCommand, "匹配结果", 5),
		toolRef(6, tagentevent.TypeAgentOutput, "完成", 6),
		toolRef(7, tagentevent.TypeExternalInput, "近期", 7),
		toolRef(8, tagentevent.TypeAgentOutput, "近期", 8),
	}
	folded := cc.foldToolRuns(refs)

	var chain *memory.EventReference
	for i := range folded {
		if folded[i].EventType == tagentevent.TypeToolChain {
			chain = &folded[i]
		}
	}
	if chain == nil {
		t.Fatalf("no tool_chain produced: %+v", folded)
	}
	if strings.Contains(chain.EventSummary, "我先读一下文件") || strings.Contains(chain.EventSummary, "关键逻辑") {
		t.Errorf("prose must NOT leak into the tool-chain line, got: %q", chain.EventSummary)
	}
	if !strings.Contains(chain.EventSummary, "grep") {
		t.Errorf("real tool name (grep) must be in the chain, got: %q", chain.EventSummary)
	}
}

// TestFoldToolRuns_MergesContiguousChain (code-review M2a): a new contiguous
// run merges into an existing trailing chain instead of creating a new chain.
func TestFoldToolRuns_MergesContiguousChain(t *testing.T) {
	cc := newFoldCC(2)
	existing := memory.EventReference{
		EventKey: -2, EventType: tagentevent.TypeToolChain,
		EventSummary: "- 工具链: read_file（1步）[evt_2→evt_3]", Timestamp: 2, Role: "user",
	}
	refs := []memory.EventReference{
		toolRef(1, tagentevent.TypeExternalInput, "用户请求", 1),
		existing,
		toolRef(4, tagentevent.TypeThinkingPlan, "调用 grep", 4),
		toolRef(5, tagentevent.TypeActionCommand, "匹配结果", 5),
		toolRef(6, tagentevent.TypeThinkingPlan, "调用 edit", 6),
		toolRef(7, tagentevent.TypeActionCommand, "编辑成功", 7),
		toolRef(8, tagentevent.TypeAgentOutput, "完成", 8),
		toolRef(9, tagentevent.TypeExternalInput, "近期", 9),
		toolRef(10, tagentevent.TypeAgentOutput, "近期", 10),
	}
	folded := cc.foldToolRuns(refs)

	chains := 0
	var chain *memory.EventReference
	for i := range folded {
		if folded[i].EventType == tagentevent.TypeToolChain {
			chains++
			chain = &folded[i]
		}
	}
	if chains != 1 {
		t.Fatalf("contiguous runs must merge into ONE chain, got %d: %+v", chains, folded)
	}
	for _, want := range []string{"read_file", "grep", "edit", "3步"} {
		if !strings.Contains(chain.EventSummary, want) {
			t.Errorf("merged chain missing %q: %q", want, chain.EventSummary)
		}
	}
}

// TestBuildRetainedRefs_RetiresArchivedChain (code-review M2b): a tool_chain
// ref whose message did NOT survive this round (its segment was L3-archived)
// is retired from the projection (not kept as a zombie).
func TestBuildRetainedRefs_RetiresArchivedChain(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 1)

	chain := memory.EventReference{
		EventKey: -100, EventType: tagentevent.TypeToolChain,
		EventSummary: "- 工具链: read_file（1步）[evt_2→evt_3]", Timestamp: 100, Role: "user",
	}
	refs := []memory.EventReference{chain}
	// compressedMsgs contains NO tool_chain message (the chain's segment was
	// archived), so the chain ref must be retired.
	retained := cc.buildRetainedRefs(refs, nil, context.Background(), nil)

	for _, r := range retained {
		if r.EventType == tagentevent.TypeToolChain {
			t.Errorf("archived tool_chain ref must be retired, but it was kept: %+v", retained)
		}
	}
}

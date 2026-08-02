package compress

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
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
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1)

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

	retained := cc.buildRetainedRefs(refs, nil, context.Background())
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
	retained2 := cc.buildRetainedRefs(retained, nil, context.Background())
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
		WithMemStore(memStore),
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
		WithMemStore(memStore),
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

// TestContextCompressor_TriggersOnExcessTurns (compress-digest-reconnect P0):
// with complete turns exceeding keepRecent, compression MUST run even when
// under the token budget — this is what un-starves the skeleton pipeline
// (previously a token-only gate never fired because placeholder rendering
// keeps usedTokens low). fail-before: old token-only gate returned all refs
// unchanged here.
func TestContextCompressor_TriggersOnExcessTurns(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1_000_000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 1)

	// 3 complete turns (12 refs), well under the (huge) token budget.
	var refs []memory.EventReference
	for i := int64(0); i < 3; i++ {
		refs = append(refs, makeTurn(i*10)...)
	}

	result := cc.Compress(context.Background(), refs)

	// Compression must have run: some refs folded away (RetainedRefs fewer
	// than input) and/or a rolling summary ref (negative key) formed.
	hasRolling := false
	for _, r := range result.RetainedRefs {
		if r.EventKey < 0 {
			hasRolling = true
		}
	}
	if len(result.RetainedRefs) >= len(refs) && !hasRolling {
		t.Fatalf("compression must run on excess turns even under budget: retained %d >= input %d and no rolling summary",
			len(result.RetainedRefs), len(refs))
	}
}

// TestContextCompressor_NoTriggerFewTurns: with complete turns within
// keepRecent and under budget, compression must NOT run (pass-through).
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
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1_000_000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 1)

	// 4 complete turns (each: external_input + thinking_plan + action_command +
	// agent_output = 2 tool steps). keepRecent=1 → oldest turn ages to L3 and
	// is fully dropped into the rolling summary.
	var refs []memory.EventReference
	for i := int64(0); i < 4; i++ {
		refs = append(refs, makeTurn(i*10)...)
	}

	result := cc.Compress(context.Background(), refs)

	// Find the rolling summary ref (negative key) and check the hint.
	var summary string
	for _, r := range result.RetainedRefs {
		if r.EventKey < 0 {
			summary = r.EventSummary
		}
	}
	if summary == "" {
		t.Fatalf("expected a rolling summary ref, got retained: %+v", result.RetainedRefs)
	}
	if !strings.Contains(summary, "含 2 步工具调用，可用 memory_turn 追溯") {
		t.Errorf("agent_output card must carry the memory_turn hint with tool-step count, got:\n%s", summary)
	}
}

// TestContextCompressor_HintIgnoresBusInjection (code-review Major): a
// bus-injected mid-turn event is ALSO typed external_input (persistBusEvent
// for task_settled etc.). The toolSteps counter must NOT reset on it — the
// card must count ALL of the turn's tool steps, not just post-injection ones.
func TestContextCompressor_HintIgnoresBusInjection(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(1_000_000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 1_000_000, 0.8, 1)

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
	for i := int64(1); i <= 3; i++ {
		refs = append(refs, makeTurn(i*10)...)
	}

	result := cc.Compress(context.Background(), refs)

	var summary string
	for _, r := range result.RetainedRefs {
		if r.EventKey < 0 {
			summary = r.EventSummary
		}
	}
	if summary == "" {
		t.Fatalf("expected rolling summary, got %+v", result.RetainedRefs)
	}
	if !strings.Contains(summary, "含 4 步工具调用，可用 memory_turn 追溯") {
		t.Errorf("bus injection must not truncate the tool-step count: want 含 4 步, got:\n%s", summary)
	}
}

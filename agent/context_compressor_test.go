package agent

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
// absorbed into the new one — count accumulates, time lower bound carries
// over — and the listed keys are capped at DefaultCompactKeysListed. Regression
// guard for the unbounded-keys-list / silent-history-drop pair.
func TestBuildRetainedRefs_RollingSummary(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(1), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 8000, 0.8, 1)

	// Prior rolling summary: 105 events already compacted, oldest ts=1000.
	refs := []memory.EventReference{{
		EventKey:     -1000,
		EventType:    tagentevent.TypeContextCompress,
		EventSummary: "[Compacted 105 historical events; recent keys=aa,bb]",
		Timestamp:    1000,
		Role:         "user",
	}}
	// 40 fresh refs, all compressed away (none appear in compressedMsgs).
	for i := 1; i <= 40; i++ {
		refs = append(refs, memory.EventReference{
			EventKey: int64(i), EventType: tagentevent.TypeExternalInput, Timestamp: int64(2000 + i),
		})
	}

	retained := cc.buildRetainedRefs(refs, nil)
	if len(retained) != 1 {
		t.Fatalf("expected single rolling summary ref, got %d: %+v", len(retained), retained)
	}
	s := retained[0]
	if s.EventKey != -1000 || s.Timestamp != 1000 {
		t.Errorf("time lower bound must carry over from prior summary, got key=%d ts=%d", s.EventKey, s.Timestamp)
	}
	if !strings.Contains(s.EventSummary, "[Compacted 145 historical events") {
		t.Errorf("rolling count must accumulate (105+40=145), got: %q", s.EventSummary)
	}
	if n := strings.Count(s.EventSummary, ",") + 1; n > DefaultCompactKeysListed {
		t.Errorf("listed keys must be capped at %d, got %d: %q", DefaultCompactKeysListed, n, s.EventSummary)
	}
	if !strings.Contains(s.EventSummary, "retrievable via recall") {
		t.Errorf("summary must point to recall for unlisted events, got: %q", s.EventSummary)
	}

	// A further round with NO new compression still preserves the entry point.
	retained2 := cc.buildRetainedRefs(retained, nil)
	if len(retained2) != 1 || !strings.Contains(retained2[0].EventSummary, "[Compacted 145 historical events") {
		t.Errorf("summary must survive rounds without new compression: %+v", retained2)
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
		content := stripEventKeyPrefix(m.Content)
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

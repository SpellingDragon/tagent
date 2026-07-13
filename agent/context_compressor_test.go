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
	msgs := []model.Message{
		model.NewSystemMessage("system prompt"),
		model.NewUserMessage("user message"),
	}

	result := cc.Compress(context.Background(), refs, msgs)

	// Under budget: unresolved historical refs are injected before current messages.
	// 2 unresolved refs (not in currentMessages) + system + user = 4 messages.
	expectedMsgs := 1 + len(refs) + 1 // system + 2 historical + user
	if len(result.Messages) != expectedMsgs {
		t.Fatalf("expected %d messages (with injected history), got %d", expectedMsgs, len(result.Messages))
	}
	if len(result.RetainedRefs) != len(refs) {
		t.Fatalf("expected %d retained refs, got %d", len(refs), len(result.RetainedRefs))
	}
}

// TestContextCompressor_PrefixesUnderBudget verifies that even when no
// compression is needed, the compressor adds [evt_KEY|type] prefixes to
// messages by matching them against projection refs. This ensures the LLM
// can see event keys for sub-agent tool calls regardless of token budget.
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
	msgs := []model.Message{
		model.NewSystemMessage("system prompt"),
		model.NewUserMessage("hello"),
	}

	result := cc.Compress(context.Background(), refs, msgs)

	if len(result.Messages) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result.Messages))
	}

	// The user message should have been prefixed with the event key
	userMsg := result.Messages[1]
	if !strings.HasPrefix(userMsg.Content, "[evt_100|external_input]") {
		t.Fatalf("expected user message to be prefixed, got: %s", userMsg.Content)
	}
}

func TestContextCompressor_RetainsRefsWhenCompressed(t *testing.T) {
	memStore := memory.NewInMemoryStore()

	// Store events in MemoryStore so resolveFullRef can load them
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

	// Force L3 full compression for all compressible segments so that original
	// event keys are actually dropped and replaced by a summary reference.
	sc := NewSmartCompressor(
		WithKeepRecentTasks(2),
		WithMaxTokens(1), // Force compression
		WithMemStore(memStore),
		WithSummaryModel(&mockBatchSummaryModel{responses: []string{"batch summary"}}),
		WithEventValuator(referenceValuator{}),
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
	msgs := []model.Message{
		model.NewUserMessage("current user message"),
	}

	result := cc.Compress(context.Background(), refs, msgs)

	if len(result.RetainedRefs) >= len(refs) {
		t.Fatalf("expected retained refs to be fewer than original (%d -> %d)", len(refs), len(result.RetainedRefs))
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

	// Recent refs (keep_recent=2) must survive compression so the LLM can
	// still recall them by key. Without event-key prefixes on the resolved
	// messages, SmartCompressor/buildRetainedRefs cannot tell which refs were
	// retained and everything collapses into a single summary ref.
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

	// Store an event with Content (non-LLM event)
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
	msgs := []model.Message{
		model.NewUserMessage("current message"),
	}

	result := cc.Compress(context.Background(), refs, msgs)

	// Under budget: the unresolved historical ref (not in currentMessages) is
	// injected before the current message. So we get 1 historical + 1 current = 2.
	if len(result.Messages) != len(msgs)+len(refs) {
		t.Fatalf("expected %d messages (with injected history), got %d", len(msgs)+len(refs), len(result.Messages))
	}
}

func TestContextCompressor_EmptyRefs(t *testing.T) {
	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(8000))
	cc := NewContextCompressor(sc, nil, NewDefaultTokenCounter(), 8000, 0.8, 2)

	msgs := []model.Message{
		model.NewSystemMessage("system"),
		model.NewUserMessage("hello"),
	}

	result := cc.Compress(context.Background(), nil, msgs)

	if len(result.Messages) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result.Messages))
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
	msgs := []model.Message{model.NewUserMessage("test")}

	_ = cc.Compress(context.Background(), refs, msgs)

	if len(refs) != originalLen {
		t.Fatal("Compress must not mutate input refs slice")
	}
}

// TestContextCompressor_DeduplicatesToolMessages verifies that when
// ContentRequestProcessor builds messages from the session (which includes
// ALL events), the ContextCompressor correctly deduplicates against the
// projection refs. Without this fix, tool messages appeared twice — once
// from resolved refs (with prefix) and once from currentMessages (without
// prefix) — because InjectEventKeys skipped tool messages.
func TestContextCompressor_DeduplicatesToolMessages(t *testing.T) {
	memStore := memory.NewInMemoryStore()

	// Store a tool event in MemoryStore
	memStore.StoreEvent(100, memory.FullEvent{
		EventKey:  100,
		EventType: tagentevent.TypeActionCommand,
		Content:   `{"files":["a.txt","b.txt"]}`,
	})

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(1))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 1, 0.8, 2)

	// Projection has the tool ref
	refs := []memory.EventReference{
		{EventKey: 100, EventType: tagentevent.TypeActionCommand, EventSummary: `{"files":["a.txt","b.txt"]}`},
	}

	// currentMessages from ContentRequestProcessor include the SAME tool message
	// (from session) plus a new user message
	msgs := []model.Message{
		model.NewSystemMessage("system"),
		model.NewToolMessage("call_1", "list_file", `{"files":["a.txt","b.txt"]}`),
		model.NewUserMessage("what's next?"),
	}

	result := cc.Compress(context.Background(), refs, msgs)

	// Count how many times the tool content appears
	toolContent := `{"files":["a.txt","b.txt"]}`
	count := 0
	for _, m := range result.Messages {
		if strings.Contains(m.Content, toolContent) {
			count++
		}
	}
	if count > 1 {
		t.Fatalf("tool message should appear at most once, got %d times", count)
	}
}

// TestContextCompressor_PreservesChronologicalOrder verifies that when
// merging currentMessages with resolved refs, the original chronological
// order from currentMessages is preserved. Specifically, user messages
// that are NOT in the projection (e.g., compressed historical events)
// must appear in their correct position, not at the end.
func TestContextCompressor_PreservesChronologicalOrder(t *testing.T) {
	memStore := memory.NewInMemoryStore()

	// Store events: assistant1, tool1, assistant2 (user1 is NOT stored —
	// simulating a compressed historical user message)
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

	sc := NewSmartCompressor(WithKeepRecentTasks(2), WithMaxTokens(1))
	cc := NewContextCompressor(sc, memStore, NewDefaultTokenCounter(), 1, 0.8, 2)

	// Projection has assistant1, tool1, assistant2 (user1 was compressed)
	refs := []memory.EventReference{
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput, EventSummary: "assistant response 1"},
		{EventKey: 3, EventType: tagentevent.TypeActionCommand, EventSummary: "tool result 1"},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput, EventSummary: "assistant response 2"},
	}

	// currentMessages from ContentRequestProcessor are in session order:
	// [user1 (historical, not in projection), assistant1, tool1, user2 (current)]
	msgs := []model.Message{
		model.NewSystemMessage("system"),
		model.NewUserMessage("user message 1"), // historical, not in projection
		{Role: model.RoleAssistant, Content: "assistant response 1"},
		model.NewToolMessage("call_1", "tool", "tool result 1"),
		model.NewUserMessage("current user message"), // current, not in projection
		{Role: model.RoleAssistant, Content: "assistant response 2"},
	}

	result := cc.Compress(context.Background(), refs, msgs)

	// Verify chronological order: user1 should appear BEFORE assistant1
	// and assistant2, not at the end.
	var order []string
	for _, m := range result.Messages {
		if m.Role == model.RoleSystem {
			continue
		}
		content := stripEventKeyPrefix(m.Content)
		if len(content) > 30 {
			content = content[:30]
		}
		order = append(order, fmt.Sprintf("%s:%s", m.Role, content))
	}

	// Find positions of key messages
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
		WithEventValuator(referenceValuator{}),
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
	msgs := []model.Message{model.NewUserMessage("new message")}

	// First compression
	result1 := cc.Compress(context.Background(), refs, msgs)

	// Should have a summary ref with negative key
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
	result2 := cc.Compress(context.Background(), result1.RetainedRefs, msgs)

	// The summary ref should NOT appear in compressedKeys of the new summary
	for _, ref := range result2.RetainedRefs {
		if ref.EventType == tagentevent.TypeContextCompress {
			// The old summary ref key is negative, so if it appears in the
			// keys= list, it would be like "keys=-1000"
			if strings.Contains(ref.EventSummary, "keys=-") {
				t.Fatalf("summary ref should not be re-compressed; got: %s", ref.EventSummary)
			}
		}
	}
}

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

	// The resolved message should have the [evt_ prefix.
	if !strings.HasPrefix(result.Messages[0].Content, "[evt_100|external_input]") {
		t.Fatalf("expected message to be prefixed, got: %s", result.Messages[0].Content)
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

// TestExtractCurrentTurnMessages verifies that the function correctly
// identifies current-turn messages (no [evt_ prefix) vs historical
// messages (with [evt_ prefix).
func TestExtractCurrentTurnMessages(t *testing.T) {
	messages := []model.Message{
		model.NewSystemMessage("system"),
		{Role: model.RoleUser, Content: "[evt_1|external_input] historical user"},
		{Role: model.RoleAssistant, Content: "[evt_2|agent_output] historical assistant"},
		// Current turn (no prefix):
		{Role: model.RoleAssistant, Content: "I will call a tool", ToolCalls: []model.ToolCall{{ID: "1", Function: model.FunctionDefinitionParam{Name: "action"}}}},
		{Role: model.RoleTool, Content: "tool result", ToolID: "1"},
	}

	currentTurn := extractCurrentTurnMessages(messages, true)

	if len(currentTurn) != 2 {
		t.Fatalf("expected 2 current-turn messages, got %d", len(currentTurn))
	}
	if currentTurn[0].Role != model.RoleAssistant {
		t.Fatalf("expected first current-turn message to be assistant, got %s", currentTurn[0].Role)
	}
	if currentTurn[1].Role != model.RoleTool {
		t.Fatalf("expected second current-turn message to be tool, got %s", currentTurn[1].Role)
	}
}

func TestExtractCurrentTurnMessages_AllPrefixed(t *testing.T) {
	messages := []model.Message{
		model.NewSystemMessage("system"),
		{Role: model.RoleUser, Content: "[evt_1|external_input] historical user"},
		{Role: model.RoleAssistant, Content: "[evt_2|agent_output] historical assistant"},
	}

	currentTurn := extractCurrentTurnMessages(messages, true)

	if len(currentTurn) != 0 {
		t.Fatalf("expected 0 current-turn messages when all are prefixed, got %d", len(currentTurn))
	}
}

// TestExtractCurrentTurnMessages_NoPrefixes_FirstCall verifies that on the
// very first call of a sub-agent, the unprefixed user message seeded by
// the framework's insertInvocationMessage is preserved (not filtered as
// a session echo). Dropping it would leave the LLM without user input,
// causing API errors like zhipu's "messages 参数非法" (code 1214).
func TestExtractCurrentTurnMessages_NoPrefixes_FirstCall(t *testing.T) {
	messages := []model.Message{
		model.NewSystemMessage("system"),
		// Unprefixed user from ContentRequestProcessor.insertInvocationMessage.
		// Since projection has no [evt_-prefixed user, this is the current
		// invocation's input and MUST be kept.
		{Role: model.RoleUser, Content: "user message"},
	}

	currentTurn := extractCurrentTurnMessages(messages, false)

	if len(currentTurn) != 1 {
		t.Fatalf("expected 1 current-turn message (unprefixed user preserved on first call), got %d: %+v", len(currentTurn), currentTurn)
	}
	if currentTurn[0].Role != model.RoleUser || currentTurn[0].Content != "user message" {
		t.Fatalf("expected user message preserved, got %+v", currentTurn[0])
	}
}

// TestExtractCurrentTurnMessages_ProjectedUserFiltersSessionEcho verifies
// that on subsequent calls (projection already has [evt_-prefixed user),
// unprefixed user messages from SessionService's WithSessionEventLimit(2)
// echo are filtered out to avoid history duplication.
func TestExtractCurrentTurnMessages_ProjectedUserFiltersSessionEcho(t *testing.T) {
	messages := []model.Message{
		model.NewSystemMessage("system"),
		// Projection has a prior user event — proves we are not on first call.
		{Role: model.RoleUser, Content: "[evt_1|external_input] prior user"},
		// Session echo (unprefixed user from SessionService limit=2 retention).
		{Role: model.RoleUser, Content: "prior user (session echo)"},
		// Prior agent_output echoed by session.
		{Role: model.RoleAssistant, Content: "prior final response"},
	}

	currentTurn := extractCurrentTurnMessages(messages, true)

	// Both unprefixed messages are session echoes and must be filtered.
	if len(currentTurn) != 0 {
		t.Fatalf("expected 0 current-turn messages (session echoes filtered when projection has user), got %d: %+v", len(currentTurn), currentTurn)
	}
}

// TestExtractCurrentTurnMessages_ReActInternal verifies that ReAct-internal
// messages (assistant with tool_calls + subsequent tool result) are kept,
// even when they sit alongside a session-echoed prior agent_output.
func TestExtractCurrentTurnMessages_ReActInternal(t *testing.T) {
	messages := []model.Message{
		model.NewSystemMessage("system"),
		{Role: model.RoleUser, Content: "[evt_1|external_input] user asks"},
		// Prior agent_output echoed by ContentRequestProcessor (unprefixed).
		{Role: model.RoleAssistant, Content: "prior final response"},
		// Current ReAct iteration: LLM invokes a tool.
		{
			Role:      model.RoleAssistant,
			Content:   "let me call a tool",
			ToolCalls: []model.ToolCall{{ID: "1", Function: model.FunctionDefinitionParam{Name: "action"}}},
		},
		{Role: model.RoleTool, Content: "tool result", ToolID: "1"},
	}

	currentTurn := extractCurrentTurnMessages(messages, true)

	if len(currentTurn) != 2 {
		t.Fatalf("expected 2 current-turn messages (assistant+tool_calls and tool result), got %d", len(currentTurn))
	}
	if currentTurn[0].Role != model.RoleAssistant || len(currentTurn[0].ToolCalls) == 0 {
		t.Fatalf("expected first current-turn message to be assistant with tool_calls, got %+v", currentTurn[0])
	}
	if currentTurn[1].Role != model.RoleTool {
		t.Fatalf("expected second current-turn message to be tool, got %s", currentTurn[1].Role)
	}
}

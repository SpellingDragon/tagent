package compress

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

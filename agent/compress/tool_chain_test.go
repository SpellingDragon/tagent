package compress

import (
	"context"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

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

// TestCompress_ToolChainEndToEnd: a long in-progress-style turn folds its aged
// tool run; the model context shows the tool-chain line (no empty-summary
// placeholder) and RetainedRefs carries the tool_chain ref.
func TestCompress_ToolChainEndToEnd(t *testing.T) {
	cc := newFoldCC(2)
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
	retained := cc.buildRetainedRefs(refs, nil, context.Background())

	for _, r := range retained {
		if r.EventType == tagentevent.TypeToolChain {
			t.Errorf("archived tool_chain ref must be retired, but it was kept: %+v", retained)
		}
	}
}

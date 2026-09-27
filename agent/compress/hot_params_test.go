package compress

import (
	"context"
	"strings"
	"testing"
	"time"

	memory "github.com/SpellingDragon/tagent/memory"
)

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

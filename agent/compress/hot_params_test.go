package compress

import (
	"context"
	"strings"
	"testing"
	"time"

	memory "github.com/SpellingDragon/tagent/memory"
)

// Tests for the full-hot-config numeric bundle (implementation-hardening →
// full-hot-config Phase 1, 2026-09-16): threshold/maxTokens/keepRecent are
// hot-applicable on the RESIDENT ContextCompressor — the C-defect fix (a yaml
// window change used to never reach the construction-frozen budget line).

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
	evts := []memory.FullEvent{
		{EventKey: k1, PartitionID: 1, EventType: "external_input", Timestamp: now,
			EventSummary: "ask", Content: "ask"},
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

func TestApplyHotParams_BudgetLineMoves(t *testing.T) {
	cc, _ := newHotTestCC(t)

	// ContextCompressor budget line = maxTokens × currentThreshold, read at
	// every Compress — the C-defect site. Old line: 60 × 0.8 = 48.
	line := func() int { return int(float64(cc.maxTokens) * cc.currentThreshold()) }
	if got := line(); got != 48 {
		t.Fatalf("initial budget line = %d, want 48 (60×0.8)", got)
	}

	cc.ApplyHotParams(0.8, 16000, 5)

	if got := line(); got != 12800 {
		t.Fatalf("budget line after hot apply = %d, want 12800", got)
	}
	if cc.keepRecent != 5 {
		t.Fatalf("keepRecent = %d, want 5", cc.keepRecent)
	}
	if cc.compressor.KeepRecentTasks != 5 {
		t.Fatalf("grading ladder keepRecent = %d, want 5", cc.compressor.KeepRecentTasks)
	}
}

func TestApplyHotParams_ZeroValuesKeepCurrent(t *testing.T) {
	cc, _ := newHotTestCC(t)
	cc.ApplyHotParams(0, 0, 0)
	if got := cc.compressor.budget(); got != 8000 {
		t.Fatalf("zero-values must keep current budget, got %d", got)
	}
	if cc.keepRecent != 2 {
		t.Fatalf("keepRecent = %d, want 2 (unchanged)", cc.keepRecent)
	}
}

// TestUpdateMaxTokens_PassThroughBoundary: a complete turn over the OLD line
// compresses; after UpdateMaxTokens the SAME turn passes through unchanged —
// the exact production shape of the C defect (512k window never reached the
// resident budget line).
func TestUpdateMaxTokens_PassThroughBoundary(t *testing.T) {
	store := memory.NewInMemoryStore()
	cc := NewContextCompressor(
		NewSmartCompressor(WithKeepRecentTasks(2)),
		store, NewDefaultTokenCounter(), 60, 0.8, 2)

	refs := storeTurn(t, store, strings.Repeat("部", 400)) // ≈201+1 tokens > 48

	before := cc.Compress(context.Background(), refs)
	if !before.Compressed {
		t.Fatal("precondition: over-budget complete turn must compress")
	}

	cc.UpdateMaxTokens(100000)
	after := cc.Compress(context.Background(), refs)
	if after.Compressed {
		t.Fatal("after hot budget raise: same turn must pass through unchanged")
	}
}

// hardening-review-batch2 5.3：参数同代——缩窗热更后真实压缩必须按新预算
// 执行（内层 SmartCompressor 同步换装），而非沿用冷构造大目标产出 no-op。
func TestApplyHotParams_ShrinkWindow_RealCompressionFollows(t *testing.T) {
	// 自建 store（newHotTestCC 的第二返回值为 nil——其 fixture 不暴露 store）。
	store := memory.NewInMemoryStore()
	cc := NewContextCompressor(
		NewSmartCompressor(WithKeepRecentTasks(2)),
		store, NewDefaultTokenCounter(), 60, 0.8, 2)
	// 冷构造：maxTokens=60。先热更换到 50000（扩窗）再缩回 60——暴露「内层
	// 不跟随」的旧缺陷（扩窗后内层 60；本用例反向：外层 50000 → 缩回 60）。
	cc.ApplyHotParams(0.8, 50000, 2)
	if got := cc.BudgetLine(); got != 40000 {
		t.Fatalf("budget line after enlarge = %d, want 40000", got)
	}

	// 存入足量历史（真实 tokens 超过缩窗后的预算线 48）。
	for i := 0; i < 6; i++ {
		storeTurn(t, store, strings.Repeat("payload-", 40)+string(rune('a'+i)))
	}
	refs := allRefs(t, store)

	// 缩窗回 60×0.8=48：触发线远低于现有体量。
	cc.ApplyHotParams(0.8, 60, 2)
	if got := cc.BudgetLine(); got != 48 {
		t.Fatalf("budget line after shrink = %d, want 48", got)
	}

	res := cc.Compress(context.Background(), refs)
	if !res.Compressed {
		t.Fatalf("expected real compression after shrink (inner target must follow), got no-op")
	}
	// 注：不断言输出 ≤ 预算线——keepRecent 全保真回合 + 骨架有结构下限，
	// 分层压缩按轮次升级；本用例锁定的是「内层目标跟随同一代」：旧实现
	// （内层仍 50000）会判预算未花而 no-op（Compressed=false）。
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

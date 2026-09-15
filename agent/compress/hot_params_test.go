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

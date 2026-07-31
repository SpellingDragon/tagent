package recall

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// Honest truncation (segment-query-recency contract 4): when a query-mode
// recall returns exactly `limit` results the caller must be told more may
// exist. Without this the LLM reads "returned N" as "only N exist" and stops
// looking — the 2026-07-31 meditation recall failure mode.

func seedManyStore(t *testing.T, n int) memory.MemoryStore {
	t.Helper()
	store := memory.NewInMemoryStore()
	for i := 0; i < n; i++ {
		key := int64(1000 + i)
		if err := store.StoreEvent(key, memory.FullEvent{
			EventKey:     key,
			EventType:    "agent_output",
			EventSummary: "部署记录",
			Content:      "部署记录内容",
			Timestamp:    int64(1710000000000 + i*1000),
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return store
}

// TestTruncationHint_AtLimit: results hitting the limit carry the notice.
func TestTruncationHint_AtLimit(t *testing.T) {
	tl := NewMemoryRecallTool(seedManyStore(t, 20), nil)
	out := callMemoryRecall(t, tl, `{"query":"部署","limit":5}`)

	if !strings.Contains(out, "已达 limit") {
		t.Errorf("hitting limit must warn about truncation, got: %s", out)
	}
}

// TestTruncationHint_BelowLimit: full result sets carry no notice (no false
// "maybe more" when everything was returned).
func TestTruncationHint_BelowLimit(t *testing.T) {
	tl := NewMemoryRecallTool(seedManyStore(t, 3), nil)
	out := callMemoryRecall(t, tl, `{"query":"部署","limit":10}`)

	if strings.Contains(out, "已达 limit") {
		t.Errorf("complete result set must not warn about truncation, got: %s", out)
	}
}

// TestTruncationHint_Unit covers the shared helper directly, including the
// limit<=0 guard.
func TestTruncationHint_Unit(t *testing.T) {
	if got := truncationHint(10, 10); got == "" {
		t.Error("count == limit must produce a hint")
	}
	if got := truncationHint(9, 10); got != "" {
		t.Errorf("count < limit must produce no hint, got %q", got)
	}
	if got := truncationHint(0, 0); got != "" {
		t.Errorf("limit<=0 must produce no hint, got %q", got)
	}
}

package agent

import (
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

func TestCompactor_NoCompactionWhenUnderLimit(t *testing.T) {
	c := NewCompactor(2)
	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput},
	}
	out := c.Compact(refs)
	if len(out) != 2 {
		t.Fatalf("expected no compaction, got len=%d", len(out))
	}
}

func TestCompactor_KeepsRecentTasks(t *testing.T) {
	c := NewCompactor(2)
	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput},
		{EventKey: 5, EventType: tagentevent.TypeExternalInput},
		{EventKey: 6, EventType: tagentevent.TypeAgentOutput},
	}
	out := c.Compact(refs)
	if len(out) != 5 {
		t.Fatalf("expected 5 refs (1 summary + 4 recent), got %d", len(out))
	}
	if out[0].EventType != tagentevent.TypeContextCompress {
		t.Fatalf("expected first ref to be context_compress, got %s", out[0].EventType)
	}
	if out[1].EventKey != 3 {
		t.Fatalf("expected recent task to start at key 3, got %d", out[1].EventKey)
	}
}

func TestCompactor_SummaryContainsKeys(t *testing.T) {
	c := NewCompactor(1)
	refs := []memory.EventReference{
		{EventKey: 10, EventType: tagentevent.TypeExternalInput},
		{EventKey: 20, EventType: tagentevent.TypeAgentOutput},
		{EventKey: 30, EventType: tagentevent.TypeExternalInput},
		{EventKey: 40, EventType: tagentevent.TypeAgentOutput},
	}
	out := c.Compact(refs)
	if len(out) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(out))
	}
	summary := out[0].EventSummary
	if summary == "" {
		t.Fatal("summary should not be empty")
	}
	for _, key := range []string{"10", "20"} {
		if !contains(summary, key) {
			t.Fatalf("summary %q should contain key %s", summary, key)
		}
	}
}

func TestCompactor_EmptyInput(t *testing.T) {
	c := NewCompactor(2)
	out := c.Compact(nil)
	if len(out) != 0 {
		t.Fatalf("expected empty output, got %d", len(out))
	}
}

func TestCompactor_DoesNotMutateInput(t *testing.T) {
	c := NewCompactor(1)
	refs := []memory.EventReference{
		{EventKey: 1, EventType: tagentevent.TypeExternalInput},
		{EventKey: 2, EventType: tagentevent.TypeAgentOutput},
		{EventKey: 3, EventType: tagentevent.TypeExternalInput},
		{EventKey: 4, EventType: tagentevent.TypeAgentOutput},
	}
	originalLen := len(refs)
	_ = c.Compact(refs)
	if len(refs) != originalLen {
		t.Fatal("Compact must not mutate input slice")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

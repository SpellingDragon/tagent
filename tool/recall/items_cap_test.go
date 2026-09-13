package recall

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// spyAccessor records GetEvent calls — the cap must bound hydration cost,
// not just the result length.
type spyAccessor struct {
	memory.MemoryStore
	gets int
}

func (s *spyAccessor) GetEvent(key int64) (*memory.FullEvent, error) {
	s.gets++
	return &memory.FullEvent{EventKey: key, EventType: "external_input", Content: "x"}, nil
}

// TestRecallByItems_BoundedHydration (implementation-hardening 3.4): over the
// cap only maxRecallItems tickets are hydrated; the truncation is reported in
// Message with the dropped count — never silent.
func TestRecallByItems_BoundedHydration(t *testing.T) {
	acc := &spyAccessor{}
	items := make([]recallItem, 0, 60)
	for i := 0; i < 60; i++ {
		items = append(items, recallItem{Key: fmt.Sprintf("%x", i+1)}) // valid hex keys
	}
	res := recallByItems(acc, items)

	if acc.gets != maxRecallItems {
		t.Fatalf("GetEvent calls = %d, want %d (hydration must be bounded)", acc.gets, maxRecallItems)
	}
	if len(res.Entries) != maxRecallItems {
		t.Fatalf("entries = %d, want %d", len(res.Entries), maxRecallItems)
	}
	if !strings.Contains(res.Message, "10 of 60 tickets dropped") {
		t.Fatalf("Message must report the truncation, got: %q", res.Message)
	}
}

// TestRecallByItems_UnderCap_NoTruncationNote: honesty cuts both ways — no
// truncation, no message.
func TestRecallByItems_UnderCap_NoTruncationNote(t *testing.T) {
	acc := &spyAccessor{}
	items := make([]recallItem, 0, 3)
	for i := 0; i < 3; i++ {
		items = append(items, recallItem{Key: fmt.Sprintf("%x", i+1)})
	}
	res := recallByItems(acc, items)
	if res.Message != "" {
		t.Fatalf("under-cap Message = %q, want empty", res.Message)
	}
	if acc.gets != 3 {
		t.Fatalf("GetEvent calls = %d, want 3", acc.gets)
	}
}

package agent

import (
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// L1: same EventKey (>0) appended twice → only one kept.
func TestProjection_IdempotentAppend(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 100, Role: "tool"})
	p.Append(memory.EventReference{EventKey: 100, Role: "tool"}) // duplicate
	p.Append(memory.EventReference{EventKey: 101, Role: "user"})
	if got := p.Len(); got != 2 {
		t.Errorf("duplicate key should be skipped: len=%d, want 2", got)
	}
}

// L1: EventKey==0 (unkeyed) must NOT be deduped.
func TestProjection_ZeroKeyNotDeduped(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 0, Role: "user"})
	p.Append(memory.EventReference{EventKey: 0, Role: "user"})
	if got := p.Len(); got != 2 {
		t.Errorf("key==0 must not dedup: len=%d, want 2", got)
	}
}

// L1: Replace rebuilds the seen set consistently with the new refs.
func TestProjection_ReplaceRebuildsSeen(t *testing.T) {
	p := NewSessionProjection()
	p.Append(memory.EventReference{EventKey: 1})
	p.Replace([]memory.EventReference{{EventKey: 2}})
	p.Append(memory.EventReference{EventKey: 1}) // 1 no longer present → accepted
	p.Append(memory.EventReference{EventKey: 2}) // 2 present → skipped
	if got := p.Len(); got != 2 {
		t.Errorf("after replace: len=%d, want 2 ({2,1})", got)
	}
}

// L1: concurrent appends of the same key stay idempotent + race-free.
func TestProjection_ConcurrentAppendSafe(t *testing.T) {
	p := NewSessionProjection()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.Append(memory.EventReference{EventKey: 7}) }()
	}
	wg.Wait()
	if got := p.Len(); got != 1 {
		t.Errorf("concurrent dup appends: len=%d, want 1", got)
	}
}

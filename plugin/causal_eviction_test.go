package plugin

import (
	"fmt"
	"math/rand"
	"testing"
)

// Causal-chain semantics after lastEventKeys eviction
// (resident-remaining-hardening 3.6, archived 7.6).
//
// lastEventKeys maps "partition:session" → the last EventKey written for that
// chain; it is capped at maxLastEventKeys and evicts the entry holding the
// OLDEST (smallest) event key. The safety contract the whole causal store
// rests on:
//
//   - a lookup returns EITHER the exact last key written under that same
//     causal key, OR 0 (absent) — never a foreign session's key. Eviction may
//     only ever DEGRADE a chain to "no parent" (a fresh root), never silently
//     re-link it to an unrelated predecessor;
//   - so a resurrected (previously evicted) session must start a NEW chain
//     (parent 0), and must not inherit a wrong parent.
//
// update mirrors the production write path (memory_plugin.go step 9).

func update(p *MemoryPlugin, causalKey string, eventKey int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastEventKeys[causalKey] = eventKey
	if len(p.lastEventKeys) > maxLastEventKeys {
		p.evictOldestLastEventKeysLocked()
	}
}

// parentOf mirrors the production read path (step 4): absent → 0.
func parentOf(p *MemoryPlugin, causalKey string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastEventKeys[causalKey]
}

func TestLastEventKeys_EvictionCausalSemantics(t *testing.T) {
	p := &MemoryPlugin{lastEventKeys: make(map[string]int64)}

	const sessions = maxLastEventKeys + 500
	// Insert each session's first event with a strictly increasing key, so the
	// first `sessions - cap` sessions hold the oldest keys and get evicted.
	key := int64(1_000_000)
	lastWritten := make(map[string]int64, sessions)
	for i := 0; i < sessions; i++ {
		ck := fmt.Sprintf("1:s%d", i)
		update(p, ck, key)
		lastWritten[ck] = key
		key++
	}

	// Cap holds.
	if len(p.lastEventKeys) > maxLastEventKeys {
		t.Fatalf("map exceeded cap: %d > %d", len(p.lastEventKeys), maxLastEventKeys)
	}

	// SAFETY: every retained entry equals the last value written under its OWN
	// key — eviction never reassigns or cross-links. And lookups of retained
	// keys are exact.
	for k, v := range p.lastEventKeys {
		if lastWritten[k] != v {
			t.Fatalf("retained %q=%d but last written under it was %d (cross-link?!) ", k, v, lastWritten[k])
		}
	}

	// The evicted sessions (lowest keys) must now read parent 0, while the most
	// recent maxLastEventKeys sessions still read their exact key.
	evictedCount := 0
	retainedCount := 0
	for i := 0; i < sessions; i++ {
		ck := fmt.Sprintf("1:s%d", i)
		got := parentOf(p, ck)
		want := lastWritten[ck]
		switch {
		case got == 0:
			evictedCount++
			// A zero parent is only acceptable if this session is genuinely
			// gone from the map — never while it still holds a live entry.
			if _, ok := p.lastEventKeys[ck]; ok {
				t.Fatalf("session %q present in map but reads parent 0", ck)
			}
		case got == want:
			retainedCount++
		default:
			t.Fatalf("session %q read parent %d, want %d (its last key) or 0 (evicted)", ck, got, want)
		}
	}
	if evictedCount != sessions-maxLastEventKeys {
		t.Fatalf("evicted %d sessions, want %d", evictedCount, sessions-maxLastEventKeys)
	}
	if retainedCount != maxLastEventKeys {
		t.Fatalf("retained %d sessions, want %d", retainedCount, maxLastEventKeys)
	}

	// RESURRECTION: a previously evicted session, written again, must root a
	// new chain (parent 0 at the moment of reuse) and then hold the fresh key.
	dead := "1:s0"
	if parentOf(p, dead) != 0 {
		t.Fatalf("evicted session %q must read parent 0 before reuse, got %d", dead, parentOf(p, dead))
	}
	update(p, dead, key)
	if got := parentOf(p, dead); got != key {
		t.Fatalf("resurrected %q reads %d, want new key %d", dead, got, key)
	}
}

// TestLastEventKeys_BoundedFuzz drives a randomized interleaving of updates to
// a small session space with strictly increasing keys and asserts the core
// invariant every step: the map is capped, and each present key maps to the
// last value written under THAT key (never a foreign value).
func TestLastEventKeys_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xcafe5))
	p := &MemoryPlugin{lastEventKeys: make(map[string]int64)}
	const space = maxLastEventKeys * 3 / 2 // overshoot cap → force continuous eviction

	writtenUnder := make(map[string]map[int64]bool)
	var key int64
	for i := 0; i < 6000; i++ {
		ck := fmt.Sprintf("1:s%d", rng.Intn(space))
		key++
		if writtenUnder[ck] == nil {
			writtenUnder[ck] = map[int64]bool{}
		}
		writtenUnder[ck][key] = true
		update(p, ck, key)

		if len(p.lastEventKeys) > maxLastEventKeys {
			t.Fatalf("iter %d: cap violated: %d", i, len(p.lastEventKeys))
		}
		// SAFETY: no value present under a key it was never written to.
		for k, v := range p.lastEventKeys {
			if !writtenUnder[k][v] {
				t.Fatalf("iter %d: key %q holds foreign value %d never written under it", i, k, v)
			}
		}
	}
}

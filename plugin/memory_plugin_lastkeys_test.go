package plugin

import (
	"fmt"
	"testing"
)

// TestMemoryPlugin_LastEventKeysBounded (implementation-hardening 5.3): the
// causal-chain map evicts oldest-by-event-key entries once over the cap —
// newest chains survive, oldest chains go first (event keys are
// time-monotonic within a partition).
func TestMemoryPlugin_LastEventKeysBounded(t *testing.T) {
	p := &MemoryPlugin{lastEventKeys: make(map[string]int64)}
	total := maxLastEventKeys + 100
	for i := 0; i < total; i++ {
		p.lastEventKeys[fmt.Sprintf("p:s%d", i)] = int64(1000 + i)
	}
	p.evictOldestLastEventKeysLocked()

	if len(p.lastEventKeys) != maxLastEventKeys {
		t.Fatalf("map len = %d, want %d (bounded)", len(p.lastEventKeys), maxLastEventKeys)
	}
	if _, ok := p.lastEventKeys["p:s0"]; ok {
		t.Fatal("oldest causal chain survived eviction — must go first")
	}
	newest := fmt.Sprintf("p:s%d", total-1)
	if _, ok := p.lastEventKeys[newest]; !ok {
		t.Fatalf("newest causal chain %q must survive", newest)
	}
}

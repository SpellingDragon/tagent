package tagent

import (
	"fmt"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
)

// registerStoreOwner (resident-readiness-plan 4.4): within one shared
// MemoryStore instance, two DIFFERENT agent names mapping to the same 10-bit
// partition id would silently merge their memory namespaces (each writes the
// other's timeline; recall crosses without authorization). Registered at
// build time with the UNDERLYING store pointer (taken BEFORE decoration —
// engine bridges wrap the same instance per agent and would defeat identity),
// failing closed with the colliding names. NEVER rewrite EventKeys or
// migrate history (delta spec「agent 身份隔离」).
//
// Isolated stores (empty path) are their own instance — no cross-agent risk.
// Same-name re-registration (executor-shell rebuild of the entry) is safe.
func (rc *runtimeConfig) registerStoreOwner(name string, memStore memory.MemoryStore) error {
	if rc == nil {
		return nil // direct buildAgent callers in tests bypass New()
	}
	storeID := fmt.Sprintf("%p", memStore)
	pid := memory.PartitionIDFromName(name)
	rc.storeOwnersMu.Lock()
	defer rc.storeOwnersMu.Unlock()
	if rc.storeOwners == nil { // direct buildAgent callers in tests bypass New()
		rc.storeOwners = make(map[string]map[int]string)
	}
	if rc.storeOwners[storeID] == nil {
		rc.storeOwners[storeID] = make(map[int]string)
	}
	if prev, dup := rc.storeOwners[storeID][pid]; dup && prev != name {
		return fmt.Errorf(
			"partition id collision: agents %q and %q hash to pid %d on the SAME store — rename one agent (never auto-migrate)",
			prev, name, pid)
	}
	rc.storeOwners[storeID][pid] = name
	return nil
}

var _ = agent.TagentAgent{} // keep the agent import for MemStore-typed helpers

// reachableAgents (resident-readiness-plan 4.5): the set of agent names the
// entry actually pulls in via tools references (transitively) — the true
// built topology, not the whole Agents map (which may carry unreferenced
// definitions).
func reachableAgents(cfg *Config, entry string) map[string]bool {
	out := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if out[name] {
			return
		}
		out[name] = true
		ac, ok := cfg.Agents[name]
		if !ok {
			return
		}
		for _, tr := range ac.Tools {
			if tr.Kind == "agent" || (tr.Kind == "" && tr.AgentID != "") {
				if tr.AgentID != "" {
					walk(tr.AgentID)
				}
			}
		}
	}
	walk(entry)
	return out
}

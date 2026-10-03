// 契约: docs/wiki/platform/org-hot-reload.md#memory-preflight
package tagent

import (
	"fmt"

	"github.com/SpellingDragon/tagent/memory"
)

// registerStoreOwner: within one shared
// MemoryStore instance, two DIFFERENT agent names mapping to the same 10-bit
// partition id would silently merge their memory namespaces (each writes the
// other's timeline; recall crosses without authorization). Registered at
// build time with the UNDERLYING store pointer (taken BEFORE decoration —
// engine bridges wrap the same instance per agent and would defeat identity),
// failing closed with the colliding names. NEVER rewrite EventKeys or
// migrate history (delta spec「agent 身份隔离」).
//
// Isolated stores (empty path) are their own instance — no cross-agent risk.
// Same-name re-registration (executor-shell rebuild of the entry) is safe.  A nil receiver and a nil owner map both occur for callers that build an
// is initialised on first use.
// agent directly instead of through New(): the receiver records nothing, the map
func (rc *runtimeConfig) registerStoreOwner(name string, memStore memory.MemoryStore) error {
	if rc == nil {
		return nil
	}
	storeID := fmt.Sprintf("%p", memStore)
	pid := memory.PartitionIDFromName(name)
	rc.storeOwnersMu.Lock()
	defer rc.storeOwnersMu.Unlock()
	if rc.storeOwners == nil {
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

// unRegisterStoreOwner removes every partition-id entry held by `name` (used when a
// hot-add is rolled back: the refused agent's store is closed, so leaving its pid
// registered would let a LATER agent that happens to reuse the recycled heap address
// of that store be refused by a stale entry — a false-positive collision).
// Cold-path only (candidate refusal / owner retirement).
func (rc *runtimeConfig) unRegisterStoreOwner(name string) {
	if rc == nil {
		return
	}
	rc.storeOwnersMu.Lock()
	defer rc.storeOwnersMu.Unlock()
	for storeID, byPID := range rc.storeOwners {
		for pid, owner := range byPID {
			if owner == name {
				delete(byPID, pid)
			}
		}
		if len(byPID) == 0 {
			delete(rc.storeOwners, storeID)
		}
	}
}

// ownedAgentNames returns the set of agent names that currently hold a store-owner registration.
//
// - The candidate transaction snapshots this before building and diffs after, so a refused candidate rollback revokes every owner it registered, including a parent that failed late.
// - Diagnostic read-only; returns a fresh set.
func (rc *runtimeConfig) ownedAgentNames() map[string]bool {
	if rc == nil {
		return map[string]bool{}
	}
	rc.storeOwnersMu.Lock()
	defer rc.storeOwnersMu.Unlock()
	out := make(map[string]bool)
	for _, byPID := range rc.storeOwners {
		for _, owner := range byPID {
			out[owner] = true
		}
	}
	return out
}

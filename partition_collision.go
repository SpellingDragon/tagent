// 契约: docs/wiki/platform/org-hot-reload.md#memory-preflight
package tagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
)

// agentMemoryFingerprint hashes ONE agent's memory section (D7). The
// reloader compares it across generations to decide whether a re-added name
// keeps its original storage owner (same path/backend → reuse) or would open a
// second writer on the same partition (changed → refuse the candidate).
// Unmarshal-free by design: only the memory subtree participates. A marshal
// failure returns the sentinel "unmarshal-error", which cannot equal any real
// fingerprint, so a failure never silently matches another agent’s stored value.
func agentMemoryFingerprint(acfg *AgentConfig) string {
	if acfg == nil {
		return ""
	}
	b, err := json.Marshal(acfg.Memory)
	if err != nil {
		return "unmarshal-error"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

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

// The blank reference is this file's only use of the agent import: dropping it
// means dropping the import in the same step.
var _ = agent.TagentAgent{}

// changedMemoryAgents returns the sorted names whose memory section differs from the one
// their existing storage owner was built with.
//
// - Judgment domain: existing owner intersect what the new generation will actually route to; only those can migrate a live store.
// - Names absent from fresh.Agents are excluded: route and definition are gone together, no definition to compare.
// - Names defined but unreachable this generation are excluded: refusing the whole reload over them would freeze orchestration hot-reload for an object the new generation never constructs.
func changedMemoryAgents(fresh *Config, ownerFP map[string]string, routable map[string]bool) []string {
	var out []string
	for name, want := range ownerFP {
		ac, ok := fresh.Agents[name]
		if !ok {
			continue
		}
		if !routable[name] {
			continue
		}
		cfg := ac
		if got := agentMemoryFingerprint(&cfg); got != want {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// reachableAgents: the set of agent names the
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

// remoteDeclarationOnly reports whether `name` is pulled in by `next` SOLELY as a
// remote agent reference and has no local definition. Such a name's declaration IS
// its definition: config validation accepts it through ToolRef.isRemoteRef (the
// single shared predicate — validation and build domains read the same fact), and
// build_agent resolves its wrapper as a remote target and builds NO executor for
// it. It therefore has no resident owner to construct and no generation to publish,
// so the owner-building loops must skip it rather than fail the whole publication
// closed.
//
// Mixed reachability is deliberately refused: if any non-remote reference also
// points at the name, that reference needs a real local owner, and a name defined
// nowhere must still fail closed — the gate's original purpose stays intact.
func remoteDeclarationOnly(next *Config, name string) bool {
	if next == nil || name == "" {
		return false
	}
	if _, defined := next.Agents[name]; defined {
		return false
	}
	remote, local := false, false
	for _, ac := range next.Agents {
		for _, tr := range ac.Tools {
			if !(tr.Kind == ToolKindAgent || (tr.Kind == "" && tr.AgentID != "")) || tr.AgentID != name {
				continue
			}
			if tr.isRemoteRef() {
				remote = true
			} else {
				local = true
			}
		}
	}
	return remote && !local
}

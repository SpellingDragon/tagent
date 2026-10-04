// 本文件承载配置模型的纯查询：记忆段指纹、可改存储属主的选择、可达拓扑、仅远端声明判定。
// 契约: docs/wiki/platform/org-hot-reload.md#memory-preflight
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// AgentMemoryFingerprint hashes ONE agent's memory section (D7). The
// reloader compares it across generations to decide whether a re-added name
// keeps its original storage owner (same path/backend → reuse) or would open a
// second writer on the same partition (changed → refuse the candidate).
// Unmarshal-free by design: only the memory subtree participates. A marshal
// failure returns the sentinel "unmarshal-error", which cannot equal any real
// fingerprint, so a failure never silently matches another agent’s stored value.
func AgentMemoryFingerprint(acfg *AgentConfig) string {
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

// ChangedMemoryAgents returns the sorted names whose memory section differs from the one
// their existing storage owner was built with.
//
// - Judgment domain: existing owner intersect what the new generation will actually route to; only those can migrate a live store.
// - Names absent from fresh.Agents are excluded: route and definition are gone together, no definition to compare.
// - Names defined but unreachable this generation are excluded: refusing the whole reload over them would freeze orchestration hot-reload for an object the new generation never constructs.
func ChangedMemoryAgents(fresh *Config, ownerFP map[string]string, routable map[string]bool) []string {
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
		if got := AgentMemoryFingerprint(&cfg); got != want {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ReachableAgents: the set of agent names the
// entry actually pulls in via tools references (transitively) — the true
// built topology, not the whole Agents map (which may carry unreferenced
// definitions).
func ReachableAgents(cfg *Config, entry string) map[string]bool {
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

// RemoteDeclarationOnly reports whether `name` is pulled in by `next` SOLELY as a
// remote agent reference and has no local definition. Such a name's declaration IS
// its definition: config validation accepts it through ToolRef.IsRemoteRef (the
// single shared predicate — validation and build domains read the same fact), and
// build_agent resolves its wrapper as a remote target and builds NO executor for
// it. It therefore has no resident owner to construct and no generation to publish,
// so the owner-building loops must skip it rather than fail the whole publication
// closed.
//
// Mixed reachability is deliberately refused: if any non-remote reference also
// points at the name, that reference needs a real local owner, and a name defined
// nowhere must still fail closed — the gate's original purpose stays intact.
func RemoteDeclarationOnly(next *Config, name string) bool {
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
			if tr.IsRemoteRef() {
				remote = true
			} else {
				local = true
			}
		}
	}
	return remote && !local
}

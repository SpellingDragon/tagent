package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// recoveryNotice renders the model-facing one-shot notice. Empty status/full
// → no notice at all (zero tokens for the healthy path — the tail-notice is
// only for degraded recoveries).
func recoveryNotice(r *RecoveryResult) string {
	if r == nil {
		return ""
	}
	switch {
	case r.Status == "failed":
		return fmt.Sprintf("[recovery] 本次冷启动恢复失败（mode=%s）——历史上下文不可用；请如实告知用户并建议 recall 检索可回补内容，勿虚构历史。", r.Mode)
	case r.Status == "partial" && r.Truncated > 0:
		return fmt.Sprintf("[recovery] 本次冷启动恢复不完整（mode=%s，已保留最近 %d 条，较早 %d 条未加载）；更早内容可用 recall 检索。", r.Mode, r.Projected, r.Truncated)
	case r.Status == "partial":
		return fmt.Sprintf("[recovery] 本次冷启动恢复部分缺失（mode=%s，缺失 %d 条）；缺失片段可用 recall 检索，勿虚构。", r.Mode, len(r.MissingKeys)+r.Truncated)
	default:
		return ""
	}
}

// RecoveryResult returns the cold-start rebuild outcome (nil before the first
// rebuild). Read-only snapshot for diagnostics.
func (cm *ContextManager) RecoveryResult() *RecoveryResult {
	if cm == nil {
		return nil
	}
	cm.recoveryMu.Lock()
	defer cm.recoveryMu.Unlock()
	return cm.recovery
}

// TakeRecoveryNotice returns and clears the one-shot model-facing recovery
// notice (empty = healthy path, nothing injected into the request).
func (cm *ContextManager) TakeRecoveryNotice() string {
	if cm == nil {
		return ""
	}
	cm.recoveryMu.Lock()
	defer cm.recoveryMu.Unlock()
	n := cm.recoveryNotice
	cm.recoveryNotice = ""
	return n
}

// RecoveryResult returns the cold-start rebuild outcome for diagnostics.
func (ta *TagentAgent) RecoveryResult() *RecoveryResult {
	if ta == nil || ta.contextManager == nil {
		return nil
	}
	return ta.contextManager.RecoveryResult()
}

var _ = sync.Mutex{}

// ResidentTopology is the process-wide name → resident agent binding, shared by
// pointer with every built agent (4.5).
//
// Why the indirection: hot reload may ADD agents to the resident
// topology while other goroutines read the table (delegation identity checks,
// diagnostics, the next shell build). Publishing a NEW immutable map through an
// atomic pointer swap keeps those readers race-free; mutating the published map
// in place would be a data race on a live map.
type ResidentTopology struct {
	pub sync.Mutex
	cur atomic.Pointer[map[string]*TagentAgent]
}

// NewResidentTopology takes ownership of the initial (startup) binding table.
// The caller must not mutate the map afterwards — publish changes through Add.
func NewResidentTopology(initial map[string]*TagentAgent) *ResidentTopology {
	rt := &ResidentTopology{}
	if initial == nil {
		initial = map[string]*TagentAgent{}
	}
	rt.cur.Store(&initial)
	return rt
}

func (rt *ResidentTopology) load() map[string]*TagentAgent {
	if rt == nil {
		return nil
	}
	p := rt.cur.Load()
	if p == nil {
		return nil
	}
	return *p
}

// Get returns the resident instance for name, or nil when not resident (:
// “is this agent already built and owned?” is exactly the hot-add question).
func (rt *ResidentTopology) Get(name string) *TagentAgent { return rt.load()[name] }

// Names returns the resident topology names.
func (rt *ResidentTopology) Names() []string {
	m := rt.load()
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	return out
}

// Snapshot returns a copy of the binding table (iteration / build seeding).
func (rt *ResidentTopology) Snapshot() map[string]*TagentAgent {
	m := rt.load()
	out := make(map[string]*TagentAgent, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// publish copies the current snapshot, applies mutate, and swaps in the new map.
func (rt *ResidentTopology) publish(mutate func(map[string]*TagentAgent)) {
	if rt == nil {
		return
	}
	rt.pub.Lock()
	defer rt.pub.Unlock()
	next := rt.Snapshot()
	mutate(next)
	rt.cur.Store(&next)
}

// Add publishes newly resident agents. An existing name is never
// overwritten — the original owner keeps the binding (D7: 同名重入复用原存储
// owner，禁止第二 writer).
func (rt *ResidentTopology) Add(adds map[string]*TagentAgent) {
	rt.publish(func(m map[string]*TagentAgent) {
		for n, a := range adds {
			if _, seen := m[n]; !seen {
				m[n] = a
			}
		}
	})
}

// Unpublish drops names (rollback of a REFUSED candidate's adds: a merged
// identity whose generation never published must not be borrowable later).
func (rt *ResidentTopology) Unpublish(names []string) {
	rt.publish(func(m map[string]*TagentAgent) {
		for _, n := range names {
			delete(m, n)
		}
	})
}

// SetResidentTable installs the shared binding table (4.5); the hot-reload shell
// borrows per-agent resources through it.
func (ta *TagentAgent) SetResidentTable(rt *ResidentTopology) {
	if ta == nil {
		return
	}
	ta.resident = rt
}

// ResidentTable returns the binding table copy (introspection, 4.6).
func (ta *TagentAgent) ResidentTable() map[string]*TagentAgent {
	if ta == nil || ta.resident == nil {
		return nil
	}
	return ta.resident.Snapshot()
}

// ResidentAgentNames returns the resident topology names.
func (ta *TagentAgent) ResidentAgentNames() []string {
	if ta == nil || ta.resident == nil {
		return nil
	}
	return ta.resident.Names()
}

// SetStoreOwnerSnapshot installs the store-owner introspection probe.
// Startup-injected once (like SetOrgDiagnostics); runtime read-only.
func (ta *TagentAgent) SetStoreOwnerSnapshot(fn func() map[string]bool) {
	if ta == nil {
		return
	}
	ta.storeOwnerSnapshot = fn
}

// StoreOwnerSnapshot returns the set of agent names currently holding a store-owner
// registration, or nil when no probe is wired. Introspection only — candidate
// rollback observability ; no execution path reads it.
func (ta *TagentAgent) StoreOwnerSnapshot() map[string]bool {
	if ta == nil || ta.storeOwnerSnapshot == nil {
		return nil
	}
	return ta.storeOwnerSnapshot()
}

// SetStoreOwnerRevoker installs the assembly's store-owner deregistration hook
// . The registration lives in the assembly's collision registry, so
// the agent cannot revoke it alone: the hook is injected where the owner was
// registered and closeOnce calls it ONLY after this instance really took its
// store exit. An unconverged close keeps the registration — a holder whose stop
// was never confirmed may still write, and forgetting it would let a second
// owner be accepted for the same partition.
func (ta *TagentAgent) SetStoreOwnerRevoker(fn func()) {
	if ta == nil {
		return
	}
	ta.storeOwnerRevoke = fn
}

// revokeStoreOwner drops this agent's registration, if the assembly wired one.
func (ta *TagentAgent) revokeStoreOwner() {
	if ta.storeOwnerRevoke != nil {
		ta.storeOwnerRevoke()
	}
}

// IsResidentAgent reports whether name is in the resident topology table.
func (ta *TagentAgent) IsResidentAgent(name string) bool {
	return ta != nil && ta.resident != nil && ta.resident.Get(name) != nil
}

// OrgKeepRecent returns the live keepRecent value (introspection, 4.6).
func (ta *TagentAgent) OrgKeepRecent() int {
	if ta == nil || ta.contextManager == nil || ta.contextManager.contextCompressor == nil {
		return 0
	}
	return ta.contextManager.contextCompressor.KeepRecentValue()
}

// OrgBudgetLine returns the compressor effective compression trigger line
// (maxTokens times the current threshold) — the real sub-model budget consumer,
// not the resident config field, so hot-param and rollback assertions read what
// the compressor actually uses. 0 when no compressor is wired.
// 契约: docs/wiki/agent/compression-and-telemetry.md#hot-bundle-atomicity
func (ta *TagentAgent) OrgBudgetLine() int {
	if ta == nil || ta.contextManager == nil || ta.contextManager.contextCompressor == nil {
		return 0
	}
	return ta.contextManager.contextCompressor.BudgetLine()
}

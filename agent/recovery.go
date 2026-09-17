package agent

import (
	"fmt"
	"sync"
)

// Recovery observability (resident-readiness-plan 3.8–3.10): the rebuild
// result is stored once at cold start and served to BOTH consumers —
// diagnostics (RecoveryResult) and the model (a one-shot tail notice consumed
// by the first request). A log line alone never reached either of them.

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

var _ = sync.Mutex{} // recoveryMu lives on ContextManager (see struct)

// SetResidentNames installs the topology binding table (4.5/4.6) — called by
// the composition root after the resident build; introspection for tests and
// diagnostics.
func (ta *TagentAgent) SetResidentNames(names map[string]bool) {
	if ta == nil {
		return
	}
	ta.residentNames = names
}

// ResidentAgentNames returns a copy of the resident topology names.
func (ta *TagentAgent) ResidentAgentNames() []string {
	if ta == nil || ta.residentNames == nil {
		return nil
	}
	out := make([]string, 0, len(ta.residentNames))
	for n := range ta.residentNames {
		out = append(out, n)
	}
	return out
}

// IsResidentAgent reports whether name is in the resident topology table.
func (ta *TagentAgent) IsResidentAgent(name string) bool {
	return ta != nil && ta.residentNames != nil && ta.residentNames[name]
}

// OrgKeepRecent returns the live keepRecent value (introspection, 4.6).
func (ta *TagentAgent) OrgKeepRecent() int {
	if ta == nil || ta.contextManager == nil || ta.contextManager.contextCompressor == nil {
		return 0
	}
	return ta.contextManager.contextCompressor.KeepRecentValue()
}

// SetResidentTable installs the name → resident agent binding (4.5); the
// hot-reload shell borrows per-agent resources through it.
func (ta *TagentAgent) SetResidentTable(table map[string]*TagentAgent) {
	if ta == nil {
		return
	}
	ta.residentTable = table
}

// ResidentTable returns the binding table copy (introspection, 4.6).
func (ta *TagentAgent) ResidentTable() map[string]*TagentAgent {
	if ta == nil || ta.residentTable == nil {
		return nil
	}
	out := make(map[string]*TagentAgent, len(ta.residentTable))
	for k, v := range ta.residentTable {
		out[k] = v
	}
	return out
}

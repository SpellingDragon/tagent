package action

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// residentReattachOnce（R3 2.6，唯一挂载点）：多 agent org 中每个配置了 action
// 工具的 agent 都构造各自 ActionTool+monitor——若都跑重挂则同一批 n- 会话被
// N 份 detector/probe-loop/watch 回调重复跟踪（🟠9）。进程级一次：首个实例执行
// 重挂，后续实例跳过（cleanup 保持幂等：n- 已被 orphan 重定义排除）。
var residentReattachOnce atomic.Bool

// Resident-session recovery (2026-09-11 D1).
//
// PROBLEM: tmux sessions (and their pipe-pane loggers) live in the tmux
// SERVER, which survives agent restarts. The agent's in-memory tracking
// (TmuxMonitor entries + TmuxSettleDetector + resident metadata like
// watch/probe) does not. Without recovery, a restart orphans every resident
// session: it keeps running but nothing watches it, and its watch/probe
// declaration is lost.
//
// SOLUTION: named sessions (B2, "n-" prefix) persist a small metadata record
// at spawn (mode/watch/probe, metaDir/<id>.json). On ActionTool startup,
// ReattachResidentSessions reconciles: tmux list (ground truth for liveness)
// ∩ metadata (ground truth for parameters) → rebuild detector + monitor
// entry, reusing the still-growing pipe file. Sessions without metadata
// (legacy/oneshot leftovers) are left to the existing orphan cleanup.

// ResidentMeta is the persisted parameter set needed to rebuild tracking.
// R3（resident-continuity-r2-r4 2.5）补 Command/TaskID/Origin：Command 供人/LLM
// 审计与重建描述；TaskID=会话 id 桥键（与 task_spawned 事件的 Declarative.TaskID
// 同源——重挂时按此与重建 registry 的任务重关联）；Origin 为可选路由 baggage
// （真相源在 task_spawned 事件，meta 侧预留零值兼容）。旧记录缺新字段：零值容错。
type ResidentMeta struct {
	Name             string `json:"name"`
	Mode             string `json:"mode"`
	Watch            string `json:"watch,omitempty"`
	Probe            string `json:"probe,omitempty"`
	ProbeIntervalSec int    `json:"probe_interval,omitempty"`
	ProbeFailures    int    `json:"probe_failures,omitempty"`
	SpawnedAt        string `json:"spawned_at"`
	// LastAdoptedAt is the last time an agent instance adopted (reattached or
	// found already-tracked) this session — hardening-review-batch2 2.1/2.2.
	// Sweep freshness is measured from HERE, never from SpawnedAt: a
	// long-running session re-adopted across restarts is not an orphan no
	// matter how old it is. Empty → fall back to SpawnedAt (legacy meta).
	LastAdoptedAt string            `json:"last_adopted_at,omitempty"`
	Command       string            `json:"command,omitempty"` // R3：原命令行（审计/重建描述）
	TaskID        string            `json:"task_id,omitempty"` // R3：桥键（=session id）
	Origin        map[string]string `json:"origin,omitempty"`  // R3：预留（真相源=task_spawned 事件）
}

// metaPath returns the metadata file for a session id.
func (ct *ActionTool) metaPath(sessionID string) string {
	return filepath.Join(ct.metaDir(), "sess-"+sessionID+".json")
}

func (ct *ActionTool) metaDir() string {
	if ct.residentMetaDirOverride != "" {
		return ct.residentMetaDirOverride // R3 2.5：resident_meta_dir 可配（离 /tmp 的持久卷）
	}
	return filepath.Join(os.TempDir(), "tagent-resident-meta")
}

// saveResidentMeta persists spawn parameters for restart recovery (D1).
// Called from startSession for named resident/interactive sessions.
func (ct *ActionTool) saveResidentMeta(sessionID string, args ActionArgs) {
	if args.Name == "" || args.Mode == string(ModeOneshot) || args.Mode == "" {
		return // oneshot sessions die with the round; nothing to recover
	}
	m := ResidentMeta{
		Name:             args.Name,
		Mode:             args.Mode,
		Watch:            args.Watch,
		Probe:            args.Probe,
		ProbeIntervalSec: args.ProbeIntervalSec,
		ProbeFailures:    args.ProbeFailures,
		SpawnedAt:        time.Now().Format(time.RFC3339),
		Command:          args.Command, // R3：全参数入事实链/meta
		TaskID:           sessionID,    // R3：桥键=会话 id（与 Declarative.TaskID 同源）
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(ct.metaDir(), 0o755)
	_ = os.WriteFile(ct.metaPath(sessionID), b, 0o600)
	// R3 2.5：spawn 全参入事实链（旁路 best-effort；真相源链=meta 文件+tmux 存活）。
	ct.emitResidentSessionEvent(sessionID, m, false)
}

// touchAdopted stamps LastAdoptedAt on the session meta — the freshness
// anchor SweepStaleResidents measures orphanhood from (hardening-review-
// batch2 2.1/2.2). Called on every adoption path: fresh reattach AND the
// already-tracked shortcut. Best-effort: a failed write only risks an
// over-eager sweep after another TTL of no adoptions.
func (ct *ActionTool) touchAdopted(sessionID string) {
	b, err := os.ReadFile(ct.metaPath(sessionID))
	if err != nil {
		return
	}
	var m ResidentMeta
	if json.Unmarshal(b, &m) != nil {
		return
	}
	m.LastAdoptedAt = time.Now().Format(time.RFC3339)
	nb, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.WriteFile(ct.metaPath(sessionID), nb, 0o600)
}

// removeResidentMeta drops the record when the session ends for any reason.
func (ct *ActionTool) removeResidentMeta(sessionID string) {
	// R3 2.5：终态结局入事实链（读 meta 组事件体；读不到则最小记录）。
	if b, err := os.ReadFile(ct.metaPath(sessionID)); err == nil {
		var m ResidentMeta
		if json.Unmarshal(b, &m) == nil {
			ct.emitResidentSessionEvent(sessionID, m, true)
		}
	}
	_ = os.Remove(ct.metaPath(sessionID))
}

// emitResidentSessionEvent writes a resident_session lifecycle record to the
// fact chain via the optional sink (wired by the build path to cm's
// record-only persistence; nil sink = standalone use, skip). Best-effort.
func (ct *ActionTool) emitResidentSessionEvent(sessionID string, m ResidentMeta, terminal bool) {
	if ct.residentSink == nil {
		return
	}
	kind := "spawn"
	detail := fmt.Sprintf("命令=%q 模式=%s watch=%q probe=%q", m.Command, m.Mode, m.Watch, m.Probe)
	if terminal {
		kind = "end"
		detail = fmt.Sprintf("会话结束（曾运行命令=%q 模式=%s）", m.Command, m.Mode)
	}
	ct.residentSink(sessionID, kind, m.Name, detail)
}

// ReattachResidentSessions reconciles surviving tmux sessions against the
// metadata dir at startup and rebuilds tracking for resident/interactive
// sessions. Returns the number of sessions reattached. Best-effort: failures
// are logged, never fatal — a broken metadata file must not block startup.
func (ct *ActionTool) ReattachResidentSessions() int {
	if ct.tmuxExecutor == nil || ct.tmuxMonitor == nil {
		return 0
	}
	// hardening-review-batch2 2.1：ADOPT FIRST, sweep last. The old order
	// swept before adoption on raw SpawnedAt age, so a healthy long-running
	// session was killed at the first restart after its 24h birthday. Adoption
	// stamps LastAdoptedAt; the sweep that follows only reaps sessions nobody
	// adopted and whose last adoption is past the TTL.
	sessions, err := ct.tmuxExecutor.ListSessions()
	if err != nil {
		return 0
	}
	reattached := 0
	for _, s := range sessions {
		if !strings.HasPrefix(s.ID, "n-") {
			continue
		}
		b, err := os.ReadFile(ct.metaPath(s.ID))
		if err != nil {
			continue // no metadata: legacy or oneshot leftover — skip
		}
		var m ResidentMeta
		if err := json.Unmarshal(b, &m); err != nil {
			log.Warnf("[ActionTool] recovery: corrupt meta for %s: %v", s.ID, err)
			continue
		}
		if m.Mode != string(ModeResident) && m.Mode != string(ModeInteractive) {
			continue
		}
		if _, tracked := ct.tmuxMonitor.GetSession(s.ID); tracked {
			// Already tracked (multi-instance share guard): still adopted —
			// refresh the freshness anchor so the sweep never reaps it.
			ct.touchAdopted(s.ID)
			continue
		}
		ct.reattachOne(s.ID, m)
		ct.touchAdopted(s.ID)
		reattached++
	}
	swept := ct.SweepStaleResidents(time.Now())
	if swept > 0 {
		log.Infof("[ActionTool] recovery: swept %d un-adopted resident session(s) past TTL", swept)
	}
	if reattached > 0 {
		log.Infof("[ActionTool] recovery: reattached %d resident session(s) after restart", reattached)
	}
	return reattached
}

// reattachOne rebuilds detector + monitor wiring + probe loop for one
// recovered session. The pipe file needs NO re-attachment: pipe-pane runs
// inside the tmux server and kept appending through our restart; the new
// detector just reads the same path from offset 0.
func (ct *ActionTool) reattachOne(sessionID string, m ResidentMeta) {
	detector := NewTmuxSettleDetector(sessionID, func() {
		if err := ct.tmuxExecutor.KillSession(sessionID); err != nil {
			log.Warnf("[ActionTool] recovery kill %s: %v", sessionID, err)
		}
		ct.tmuxMonitor.RemoveSession(sessionID)
		ct.removeResidentMeta(sessionID)
	})
	if m.Watch != "" {
		if err := detector.SetWatch(m.Watch, 5*time.Second); err != nil {
			log.Warnf("[ActionTool] recovery watch %s: %v", sessionID, err)
		}
	}
	isTUI := m.Mode == string(ModeInteractive)
	ct.tmuxMonitor.AddSessionWithCallback(&TmuxSession{
		ID:        sessionID,
		Name:      m.Name,
		Command:   "(recovered)",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     isTUI,
		Mode:      SessionMode(m.Mode),
	}, func(_ string, _, newStatus SessionStatus, output string) {
		detector.OnWatchOutput(output)
		detector.OnStateChange(newStatus, output)
	})
	if m.Probe != "" {
		ct.startProbeLoop(sessionID, ActionArgs{
			Probe:            m.Probe,
			ProbeIntervalSec: m.ProbeIntervalSec,
			ProbeFailures:    m.ProbeFailures,
		}, detector)
	}
	// hardening-review-batch2 3.1：注册重挂 detector 供 build 侧绑定到
	// registry 恢复的任务（重挂发生在 RebuildTaskRegistry 之前——时序桥）。
	ct.rememberReattachedDetector(sessionID, detector)
	log.Infof("[ActionTool] recovery: %s alive (mode=%s watch=%q probe=%q) — tracking rebuilt; agent notified on next watch/probe hit or peek", sessionID, m.Mode, m.Watch, m.Probe)
}

// residentTTL is how long a resident session may go WITHOUT ADOPTION before
// the sweep reaps it (hardening-review-batch2 2.2 — was: raw age since spawn,
// which killed healthy long-running sessions at their first post-24h restart).
// Freshness is measured from LastAdoptedAt (fallback SpawnedAt for legacy
// meta), and sessions still tracked by a live monitor are never swept.
var residentTTL = 24 * time.Hour

// maxResidentSessions caps concurrent resident sessions per agent instance
// (spawn-time refusal; an unbounded swarm of long-lived ptys is the failure
// mode the TTL sweep exists to catch).
var maxResidentSessions = 16

// SweepStaleResidents kills resident sessions nobody has adopted for longer
// than the TTL. Adoption paths (ReattachResidentSessions) stamp
// LastAdoptedAt; a session still tracked by the live monitor is NEVER swept
// regardless of freshness. Called after adoption in ReattachResidentSessions;
// safe to expose for a maintenance cron.
func (ct *ActionTool) SweepStaleResidents(now time.Time) int {
	entries, err := os.ReadDir(ct.metaDir())
	if err != nil {
		return 0
	}
	killed := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "sess-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "sess-"), ".json")
		// Never reap a session the live monitor still tracks (multi-instance
		// guard: another agent instance may have adopted it). nil monitor →
		// no tracking info; proceed on freshness alone (zero-value tool).
		if ct.tmuxMonitor != nil {
			if _, tracked := ct.tmuxMonitor.GetSession(id); tracked {
				continue
			}
		}
		b, err := os.ReadFile(ct.metaPath(id))
		if err != nil {
			continue
		}
		var m ResidentMeta
		if err := json.Unmarshal(b, &m); err != nil {
			continue // corrupt meta — leave the session alone
		}
		freshness := m.LastAdoptedAt
		if freshness == "" {
			freshness = m.SpawnedAt // legacy meta: fall back to spawn time
		}
		stamped, err := time.Parse(time.RFC3339, freshness)
		if err != nil {
			continue
		}
		if now.Sub(stamped) > residentTTL {
			// Un-adopted past TTL: orphan. Kill the session (when an executor
			// is wired — tests may run record-only) and drop its record:
			// the metadata is the leak we track.
			if ct.tmuxExecutor != nil {
				if err := ct.tmuxExecutor.KillSession(id); err == nil {
					killed++
				}
			}
			ct.removeResidentMeta(id)
			log.Warnf("[ActionTool] sweep: killed un-adopted resident %s (last adoption %s, age %s > TTL %s)", id, freshness, now.Sub(stamped).Round(time.Minute), residentTTL)
		}
	}
	return killed
}

// residentCount returns how many resident sessions this instance tracks.
func (ct *ActionTool) residentCount() int {
	n := 0
	for _, s := range ct.tmuxMonitor.ListSessions() {
		if s.Mode == ModeResident || s.Mode == ModeInteractive {
			n++
		}
	}
	return n
}

// CanSpawnResident reports whether a new resident/interactive session is
// allowed under the concurrency cap.
func (ct *ActionTool) CanSpawnResident() bool {
	return ct.residentCount() < maxResidentSessions
}

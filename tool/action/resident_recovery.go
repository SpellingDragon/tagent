package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

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
type ResidentMeta struct {
	Name             string `json:"name"`
	Mode             string `json:"mode"`
	Watch            string `json:"watch,omitempty"`
	Probe            string `json:"probe,omitempty"`
	ProbeIntervalSec int    `json:"probe_interval,omitempty"`
	ProbeFailures    int    `json:"probe_failures,omitempty"`
	SpawnedAt        string `json:"spawned_at"`
}

// metaPath returns the metadata file for a session id.
func (ct *ActionTool) metaPath(sessionID string) string {
	return filepath.Join(ct.metaDir(), "sess-"+sessionID+".json")
}

func (ct *ActionTool) metaDir() string {
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
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(ct.metaDir(), 0o755)
	_ = os.WriteFile(ct.metaPath(sessionID), b, 0o600)
}

// removeResidentMeta drops the record when the session ends for any reason.
func (ct *ActionTool) removeResidentMeta(sessionID string) {
	_ = os.Remove(ct.metaPath(sessionID))
}

// ReattachResidentSessions reconciles surviving tmux sessions against the
// metadata dir at startup and rebuilds tracking for resident/interactive
// sessions. Returns the number of sessions reattached. Best-effort: failures
// are logged, never fatal — a broken metadata file must not block startup.
func (ct *ActionTool) ReattachResidentSessions() int {
	if ct.tmuxExecutor == nil || ct.tmuxMonitor == nil {
		return 0
	}
	swept := ct.SweepStaleResidents(time.Now())
	if swept > 0 {
		log.Infof("[ActionTool] recovery: swept %d stale resident session(s)", swept)
	}
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
			continue // already tracked (multi-instance share guard)
		}
		ct.reattachOne(s.ID, m)
		reattached++
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
	log.Infof("[ActionTool] recovery: %s alive (mode=%s watch=%q probe=%q) — tracking rebuilt; agent notified on next watch/probe hit or peek", sessionID, m.Mode, m.Watch, m.Probe)
}

// residentTTL is how long an unattended resident session may outlive the
// agent process that spawned it without being re-adopted (2026-09-11 D2).
// A restart re-stamps SpawnedAt via reattach, so this only sweeps sessions
// NOBODY adopted across restarts — true leaks.
var residentTTL = 24 * time.Hour

// maxResidentSessions caps concurrent resident sessions per agent instance
// (spawn-time refusal; an unbounded swarm of long-lived ptys is the failure
// mode the TTL sweep exists to catch).
var maxResidentSessions = 16

// SweepStaleResidents kills resident sessions that outlived their TTL without
// recovery (i.e. no agent adopted them at any startup since spawn). Called
// from ReattachResidentSessions; safe to expose for a maintenance cron.
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
		b, err := os.ReadFile(ct.metaPath(id))
		if err != nil {
			continue
		}
		var m ResidentMeta
		if err := json.Unmarshal(b, &m); err != nil {
			continue // corrupt meta — leave the session alone
		}
		spawned, err := time.Parse(time.RFC3339, m.SpawnedAt)
		if err != nil {
			continue
		}
		if now.Sub(spawned) > residentTTL {
			// Not re-adopted within TTL: orphan. Kill the session (when an
			// executor is wired — tests may run record-only) and drop its
			// record either way: the metadata is the leak we track.
			if ct.tmuxExecutor != nil {
				if err := ct.tmuxExecutor.KillSession(id); err == nil {
					killed++
				}
			}
			ct.removeResidentMeta(id)
			log.Warnf("[ActionTool] sweep: killed stale resident %s (age %s > TTL %s)", id, now.Sub(spawned).Round(time.Minute), residentTTL)
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

package action

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/SpellingDragon/tagent/agent/task"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// residentReattachOnce（R3 2.6，唯一挂载点）：多 agent org 中每个配置了 action
// 工具的 agent 都构造各自 ActionTool+monitor——若都跑重挂则同一批 n- 会话被
// N 份 detector/probe-loop/watch 回调重复跟踪（🟠9）。进程级一次：首个实例执行
// 重挂，后续实例跳过（cleanup 保持幂等：n- 已被 orphan 重定义排除）。
var residentReattachOnce atomic.Bool

// ResidentMeta is the persisted parameter set needed to rebuild tracking.
// R3补 Command/TaskID/Origin：Command 供人/LLM
// 审计与重建描述；TaskID=会话 id 桥键（与 task_spawned 事件的 Declarative.TaskID
// 同源——重挂时按此与重建 registry 的任务重关联）；Origin 为可选路由 baggage
// （真相源在 task_spawned 事件，meta 侧预留零值兼容）。旧记录缺新字段：零值容错。
type ResidentMeta struct {
	ResourceID       string            `json:"resource_id,omitempty"`
	Phase            string            `json:"phase,omitempty"`
	Placeholder      string            `json:"placeholder,omitempty"`
	ReadyPattern     string            `json:"ready_pattern,omitempty"`
	InputPattern     string            `json:"input_pattern,omitempty"`
	WorkDir          string            `json:"work_dir,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	IsTUI            bool              `json:"is_tui,omitempty"`
	Baseline         int               `json:"baseline,omitempty"`
	ProducerRound    uint64            `json:"producer_round,omitempty"`
	Name             string            `json:"name"`
	Mode             string            `json:"mode"`
	Watch            string            `json:"watch,omitempty"`
	Probe            string            `json:"probe,omitempty"`
	ProbeIntervalSec int               `json:"probe_interval,omitempty"`
	ProbeFailures    int               `json:"probe_failures,omitempty"`
	SpawnedAt        string            `json:"spawned_at"`
	// LastAdoptedAt is the last time an agent instance adopted (reattached or
	// found already-tracked) this session.
	// Sweep freshness is measured from HERE, never from SpawnedAt: a
	// long-running session re-adopted across restarts is not an orphan no
	// matter how old it is. Empty → fall back to SpawnedAt (metadata without an adoption timestamp).
	LastAdoptedAt string            `json:"last_adopted_at,omitempty"`
	Command       string            `json:"command,omitempty"`
	TaskID        string            `json:"task_id,omitempty"`
	Origin        map[string]string `json:"origin,omitempty"`
}

// metaPath returns the metadata file for a session id.
func (ct *ActionTool) metaPath(sessionID string) string {
	return filepath.Join(ct.metaDir(), "sess-"+sessionID+".json")
}

func (ct *ActionTool) metaDir() string {
	if ct.residentMetaDirOverride != "" {
		return ct.residentMetaDirOverride
	}
	return filepath.Join(os.TempDir(), "tagent-resident-meta")
}

// saveResidentMeta persists spawn parameters for restart recovery .
// Called from startSession for named resident/interactive sessions.
func (ct *ActionTool) saveResidentMeta(sessionID string, args ActionArgs) {
	if args.Name == "" || args.Mode == string(ModeOneshot) || args.Mode == "" {
		return
	}
	resource := ""
	if ct.tmuxExecutor != nil {
		resource, _ = ct.tmuxExecutor.SessionResourceID(sessionID)
	}
	ct.saveBoundMeta(&TmuxSession{ID: sessionID, ResourceID: resource, Phase: "active", CreatedAt: time.Now()}, args)
}

func (ct *ActionTool) saveBoundMeta(session *TmuxSession, args ActionArgs) {
	m := ResidentMeta{Name: args.Name, Mode: args.Mode, Watch: args.Watch, Probe: args.Probe, ProbeIntervalSec: args.ProbeIntervalSec, ProbeFailures: args.ProbeFailures, SpawnedAt: session.CreatedAt.Format(time.RFC3339), Command: args.Command, TaskID: session.ID, ResourceID: session.ResourceID, Phase: session.Phase, Placeholder: session.Placeholder, ReadyPattern: args.ReadyPattern, InputPattern: args.InputPattern, WorkDir: args.WorkDir, Env: args.Env, IsTUI: args.IsTUI, ProducerRound: session.ProducerRound}
	_ = ct.writeResidentMeta(session.ID, m)
	ct.emitResidentSessionEvent(session.ID, m, false)
}

func (ct *ActionTool) writeResidentMeta(sessionID string, m ResidentMeta) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ct.metaDir(), 0o755); err != nil {
		return err
	}
	tmp := ct.metaPath(sessionID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ct.metaPath(sessionID))
}

func (ct *ActionTool) removeBoundMeta(sessionID, resourceID string) {
	b, err := os.ReadFile(ct.metaPath(sessionID))
	if err != nil {
		return
	}
	var m ResidentMeta
	if json.Unmarshal(b, &m) != nil || m.ResourceID != resourceID {
		return
	}
	ct.removeResidentMeta(sessionID)
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
			continue
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
//
// 契约: docs/wiki/tool/tmux-action.md#restart-takeover
func (ct *ActionTool) reattachOne(sessionID string, m ResidentMeta) {
	resource := m.ResourceID
	if resource == "" {
		var err error
		resource, err = ct.tmuxExecutor.SessionResourceID(sessionID)
		if err != nil {
			return
		}
		m.ResourceID = resource
		if err := ct.writeResidentMeta(sessionID, m); err != nil {
			return
		}
	}
	args := ActionArgs{Command: m.Command, Mode: m.Mode, Name: m.Name, Watch: m.Watch, Probe: m.Probe, ProbeIntervalSec: m.ProbeIntervalSec, ProbeFailures: m.ProbeFailures, ReadyPattern: m.ReadyPattern, InputPattern: m.InputPattern, WorkDir: m.WorkDir, Env: m.Env, IsTUI: m.IsTUI}
	session := &TmuxSession{ID: sessionID, ResourceID: resource, Name: m.Name, Command: m.Command, Mode: SessionMode(m.Mode), IsTUI: m.IsTUI, CreatedAt: time.Now(), Phase: m.Phase, Placeholder: m.Placeholder}
	detector, err := ct.detectorForSession(session, args)
	if err != nil {
		return
	}
	detector.RestoreRound(m.ProducerRound, 0)
	detector.Rearm(m.Baseline)
	detector.startFn = func() error {
		if m.Phase == "prepared" && m.Placeholder != "" {
			if err := ct.tmuxExecutor.activateResource(sessionID, resource, m.Placeholder, m.Command); err != nil {
				return err
			}
		}
		ct.activateMonitoring(session, args, detector)
		return nil
	}
	ct.rememberReattachedDetector(sessionID, detector)
}

// RecoverTaskDetector reconstructs declared live and unacknowledged command producers.
func (ct *ActionTool) RecoverTaskDetector(tk *task.Task) task.SettleDetector {
	if tk == nil || ct.tmuxExecutor == nil || ct.tmuxMonitor == nil {
		return nil
	}
	spec := tk.SpecSnapshot()
	decl := spec.Declarative
	if decl == nil || decl.Kind != "command" || decl.TaskID == "" {
		return nil
	}
	args, err := argsFromDeclarative(*decl)
	if err != nil {
		return nil
	}
	if args.Mode == "" {
		args.Mode = string(ModeOneshot)
	}
	resource := decl.Params[pResourceID]
	if resource == "" {
		resource, err = ct.tmuxExecutor.SessionResourceID(decl.TaskID)
		if err != nil {
			return nil
		}
		if decl.Params == nil {
			decl.Params = make(map[string]string)
		}
		decl.Params[pResourceID] = resource
	}
	if cached := ct.TakeReattachedDetector(decl.TaskID); cached != nil {
		if detector, ok := cached.(*TmuxSettleDetector); ok {
			detector.close()
		}
	}
	session := &TmuxSession{ID: decl.TaskID, ResourceID: resource, Name: args.Name, Command: args.Command, Mode: SessionMode(args.Mode), WorkDir: args.WorkDir, IsTUI: args.IsTUI, CreatedAt: time.Now(), QuietTimeout: time.Duration(args.QuietTimeout) * time.Second, Phase: decl.Params[pPhase], Placeholder: decl.Params[pPlaceholder]}
	detector, err := ct.detectorForSession(session, args)
	if err != nil {
		return nil
	}
	detector.sourceTaskID = tk.ID
	baseline, _ := strconv.Atoi(decl.Params[pBaseline])
	round := tk.ProducerRound()
	if round == 0 {
		round = tk.RoundID()
	}
	detector.RestoreRound(round, tk.SignalSeq())
	detector.Rearm(baseline)
	for _, pending := range tk.PendingSignals() {
		if pending.ProducerRound == round && (pending.Kind == task.SettleCompleted || pending.Kind == task.SettleFailed) && pending.Observation != "round_aborted" {
			copy := pending
			detector.signalMu.Lock()
			detector.terminal = &copy
			detector.signalMu.Unlock()
			if ct.tmuxExecutor.resourceStopped(session.ID, resource) {
				detector.confirmStopped()
			}
			detector.close()
			ct.saveBoundMeta(session, args)
			spec.ResumeFn = ct.resumeResourceClosure(session.ID, resource, args.IsTUI, detector)
			spec.Alive = ct.resourceAliveClosure(session.ID, resource)
			return &recoveredDetector{TmuxSettleDetector: detector, spec: spec}
		}
	}
	detector.startFn = func() error {
		if session.Phase == "prepared" && session.Placeholder != "" {
			if err := ct.tmuxExecutor.activateResource(session.ID, resource, session.Placeholder, args.Command); err != nil {
				return err
			}
		}
		ct.activateMonitoring(session, args, detector)
		return nil
	}
	ct.saveBoundMeta(session, args)
	spec.ResumeFn = ct.resumeResourceClosure(session.ID, resource, args.IsTUI, detector)
	spec.Alive = ct.resourceAliveClosure(session.ID, resource)
	return &recoveredDetector{TmuxSettleDetector: detector, spec: spec}
}

type recoveredDetector struct {
	*TmuxSettleDetector
	spec task.TaskSpec
}

// RecoveredSpec returns the resource-pinned declaration for durable rebinding.
func (d *recoveredDetector) RecoveredSpec() task.TaskSpec { return d.spec }

// residentTTL is how long a resident session may go without a recent adoption before
// the sweep reaps it. Freshness is measured from LastAdoptedAt (fallback SpawnedAt for
// metadata without an adoption timestamp), and sessions still tracked by a live monitor
// are never swept.
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
			continue
		}
		freshness := m.LastAdoptedAt
		if freshness == "" {
			freshness = m.SpawnedAt
		}
		stamped, err := time.Parse(time.RFC3339, freshness)
		if err != nil {
			continue
		}
		if now.Sub(stamped) > residentTTL {
			if ct.tmuxExecutor != nil {
				if ct.tmuxExecutor.IsPaneDead(id) {
					continue
				}
				if m.ResourceID == "" {
					m.ResourceID, err = ct.tmuxExecutor.SessionResourceID(id)
					if err != nil || ct.writeResidentMeta(id, m) != nil {
						continue
					}
				}
				if err := ct.tmuxExecutor.killResource(id, m.ResourceID); err != nil {
					continue
				}
				killed++
			}
			ct.removeBoundMeta(id, m.ResourceID)
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

func (ct *ActionTool) terminalOutputPath(sessionID, resourceID string) string {
	identity := strings.NewReplacer(":", "-", "$", "").Replace(resourceID)
	return filepath.Join(ct.metaDir(), "output-"+sessionID+"-"+identity+".log")
}

func (ct *ActionTool) persistTerminalOutput(sessionID, resourceID, output string) {
	if err := os.MkdirAll(ct.metaDir(), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(ct.terminalOutputPath(sessionID, resourceID), []byte(output), 0o600); err != nil {
		log.Warnf("[ActionTool] save terminal output %s: %v", sessionID, err)
	}
}

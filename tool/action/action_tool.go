// Package action 提供 exec 类动作工具及其 tmux 会话承载：动作的发起与执行、会话存活
// 判定、完成裁决（settle）、状态跃迁通知、轮询调度、跨重启的常驻恢复，以及供跨重启
// 重建闭包的 Declarative 投影。长期行为判据不在此复述，见下列契约。
//
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
// 契约: docs/wiki/tool/tmux-action.md#liveness-first
package action

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent/task"
)

// Verify ActionTool implements tool.CallableTool at compile time.
var _ tool.CallableTool = (*ActionTool)(nil)

// ActionTool is a shell command execution tool: every command runs in a tmux session
// and Call blocks until the TmuxMonitor reports a settle state.
//
// - Tool name is action; the result carries command, session ID, final status and captured output.
type ActionTool struct {
	// reattachMu reattachedDetectors caches detectors built during startup reattach so
	// the build path can BindDetector them to restored tasks AFTER the
	// registry rebuild.
	reattachMu    sync.Mutex
	reattachedMap map[string]task.SettleDetector

	workspace   string
	outputDir   string
	runAsUser   string
	runAsGroup  string
	description string
	defaultTTL  time.Duration
	// ttlSource is the live read face for defaultTTL (see SetDefaultTTLSource).
	ttlSource     atomic.Pointer[func() time.Duration]
	tmuxExecutor  *TmuxExecutor
	tmuxMonitor   *TmuxMonitor
	monitorConfig *MonitorConfig
	// orphanCleanupDisabled skips the startup reaping of prefix-matched
	// leftover sessions (see WithOrphanCleanupDisabled).
	orphanCleanupDisabled bool

	// residentSink：常驻会话生命周期事件
	//（resident_session）的可选事实链写入槽——build 路径接线到 cm 的记录-only
	// 持久化；nil=standalone 使用，跳过（best-effort）。
	residentSink func(sessionID, kind, name, detail string)
	// residentMetaDirOverride（R3 2.5）：ResidentMeta 目录覆盖（默认 $TMPDIR）。
	residentMetaDirOverride string

	// peeks tracks incremental peek cursors per session.
	peeks peekCursors

	startMu            sync.Mutex
	closeOnce          sync.Once
	closed             atomic.Bool
	detectorMu         sync.Mutex
	detectors          map[string]*TmuxSettleDetector
	closeErr           error
	residentAdmissions int
}

// ActionToolOption configures ActionTool.
type ActionToolOption func(*ActionTool)

// WithActionWorkspace sets the command working directory. Empty (the
// default) inherits the process working directory — keeping exec's relative
// paths consistent with the file tools' base directory, so the model sees ONE
// coherent filesystem view. A mismatch here (e.g. defaulting exec into a
// scratch dir) makes `list_file` results unreachable from `exec` and induces
// path hallucinations.
func WithActionWorkspace(dir string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.workspace = dir
	}
}

// WithOrphanCleanupDisabled skips the startup orphan-session cleanup. Use it
// when multiple instances share one tmux server AND one session prefix (the
// cleanup would reap the other instance's live sessions); prefer distinct
// prefixes instead.
func WithOrphanCleanupDisabled() ActionToolOption {
	return func(ct *ActionTool) {
		ct.orphanCleanupDisabled = true
	}
}

// WithActionOutputDir sets the directory where oversized command outputs are
// saved (defaults to the process working directory when empty). Kept separate
// from the command working directory — scratch artifacts must not force the
// command cwd away from the agent's world.
func WithActionOutputDir(dir string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.outputDir = dir
	}
}

// WithActionRunAsUser sets the user to run commands as.
func WithActionRunAsUser(user string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.runAsUser = user
	}
}

// WithActionRunAsGroup sets the group to run commands as.
func WithActionRunAsGroup(group string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.runAsGroup = group
	}
}

// WithDescription sets the tool description.
func WithDescription(desc string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.description = desc
	}
}

// WithActionMonitorConfig sets a custom TmuxMonitor configuration.
func WithActionMonitorConfig(cfg MonitorConfig) ActionToolOption {
	return func(ct *ActionTool) {
		ct.monitorConfig = &cfg
	}
}

// WithResidentRecordSink：接线常驻会话
// 生命周期事件的事实链写入槽（build 路径→cm 记录-only 持久化）。best-effort。
func WithResidentRecordSink(sink func(sessionID, kind, name, detail string)) ActionToolOption {
	return func(ct *ActionTool) {
		ct.residentSink = sink
	}
}

// WithResidentMetaDir（R3 2.5）：ResidentMeta 目录覆盖（默认 $TMPDIR；可指向
// 持久卷以便机器重启后仍可审计/TTL sweep）。
func WithResidentMetaDir(dir string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.residentMetaDirOverride = dir
	}
}

// IsTrackedSession reports whether the monitor currently tracks the session
// （R3 2.6 TaskID 桥：重挂后按此与重建 registry 的任务重关联）。
func (ct *ActionTool) IsTrackedSession(sessionID string) bool {
	if ct == nil || ct.tmuxMonitor == nil || sessionID == "" {
		return false
	}
	_, ok := ct.tmuxMonitor.GetSession(sessionID)
	return ok
}

// SetResidentRecordSink wires the fact-chain sink post-construction (R3 2.5;
// the build path obtains the ActionTool after factory creation).
func (ct *ActionTool) SetResidentRecordSink(sink func(sessionID, kind, name, detail string)) {
	if ct != nil {
		ct.residentSink = sink
	}
}

// SetResidentMetaDir overrides the ResidentMeta directory post-construction.
func (ct *ActionTool) SetResidentMetaDir(dir string) {
	if ct != nil {
		ct.residentMetaDirOverride = dir
		if ct.tmuxExecutor != nil && len(ct.detectors) == 0 {
			ct.tmuxExecutor.pipeRoot = dir
		}
	}
}

// NewActionTool creates a new ActionTool.
func NewActionTool(opts ...ActionToolOption) *ActionTool {
	ct := &ActionTool{
		description: "Execute a shell command via tmux. Oneshot waits for real exit; resident waits for evidenced readiness; interactive waits for a fresh input prompt.",
		defaultTTL:  defaultTaskTTL,
	}

	for _, opt := range opts {
		opt(ct)
	}

	if IsTmuxAvailable() {
		ct.tmuxExecutor = NewTmuxExecutor(
			WithTmuxWorkspace(ct.workspace),
			WithTmuxRunAsUser(ct.runAsUser),
			WithTmuxRunAsGroup(ct.runAsGroup),
		)
		monCfg := DefaultMonitorConfig()
		if ct.monitorConfig != nil {
			monCfg = *ct.monitorConfig
		}
		ct.tmuxExecutor.pipeRoot = ct.metaDir()
		ct.tmuxMonitor = NewTmuxMonitor(
			WithMonitorExecutor(ct.tmuxExecutor),
			WithMonitorConfig(monCfg),
		)
		if !ct.orphanCleanupDisabled && ct.residentMetaDirOverride == "" {
			ct.tmuxExecutor.CleanupOrphanSessions()
		}
		if residentReattachOnce.CompareAndSwap(false, true) {
			ct.ReattachResidentSessions()
		} else {
			log.Infof("[ActionTool] resident reattach already done by another instance (single mount point)")
		}
	}

	return ct
}

// defaultTaskTTL is the absolute lifetime applied when neither an explicit `ttl`
// arg nor a configured default is present (async-task-lifetime 10.2: every task
// is bounded; there is no "unlimited" default).
const defaultTaskTTL = 10 * time.Minute

// SetDefaultTTLSource installs the  pull source for the spawn-time default
// lifetime (spawner axis): the
// composition root binds it to the owner's committed application record, so a
// numeric-only rotation reaches every subsequent spawn without anyone pushing a
// number into this tool. It replaces SetDefaultTaskTTL, which kept a second,
// mutable copy of the same knob (and was a plain field: written on the reload
// goroutine, read by business turns).
func (ct *ActionTool) SetDefaultTTLSource(src func() time.Duration) {
	if src == nil {
		return
	}
	ct.ttlSource.Store(&src)
}

// resolveTTL returns the effective absolute lifetime for a spawn: explicit ttl
// seconds when > 0, else the live source (a record reading that has no opinion —
// zero/negative — falls through), else the configured default, else the
// 10-minute floor. A negative ttl is rejected in Call before this runs; 0 means
// "omit → default".
func (ct *ActionTool) resolveTTL(args ActionArgs) time.Duration {
	if args.TTL > 0 {
		return time.Duration(args.TTL) * time.Second
	}
	if src := ct.ttlSource.Load(); src != nil {
		if d := (*src)(); d > 0 {
			return d
		}
	}
	if ct.defaultTTL > 0 {
		return ct.defaultTTL
	}
	return defaultTaskTTL
}

// Close stops live producers and preserves natural terminal evidence until acceptance.
func (ct *ActionTool) Close() error {
	ct.closeOnce.Do(func() {
		ct.closed.Store(true)
		ct.startMu.Lock()
		if ct.tmuxMonitor != nil {
			ct.tmuxMonitor.Stop()
		}
		ct.startMu.Unlock()
		ct.detectorMu.Lock()
		detectors := make([]*TmuxSettleDetector, 0, len(ct.detectors))
		for _, detector := range ct.detectors {
			detectors = append(detectors, detector)
		}
		ct.detectorMu.Unlock()
		for _, detector := range detectors {
			if ct.tmuxExecutor != nil && ct.tmuxExecutor.resourceStopped(detector.sessionID, detector.resourceID) {
				output, _ := ct.tmuxExecutor.resourceOutput(detector.sessionID, detector.resourceID)
				status := SessionError
				if _, err := ct.tmuxExecutor.resourceTarget(detector.sessionID, detector.resourceID); err == nil {
					status = ct.tmuxMonitor.deathPolarity(detector.sessionID)
				}
				ct.persistTerminalOutput(detector.sessionID, detector.resourceID, output)
				detector.OnStateChange(status, output)
				detector.confirmStopped()
				continue
			}
			select {
			case <-detector.Stopped():
				continue
			default:
			}
			detector.Cancel()
			select {
			case <-detector.Stopped():
			default:
				ct.closeErr = fmt.Errorf("action: producer %s termination is unconfirmed", detector.sessionID)
			}
		}
	})
	return ct.closeErr
}

// Declaration implements tool.CallableTool.
func (ct *ActionTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "action",
		Description: ct.description,
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"command": {
					Type:        "string",
					Description: "The action to execute, described as a shell command. Runs via sh -c so pipes, redirects, and chaining are supported.",
				},
				"work_dir": {
					Type:        "string",
					Description: "Working directory for command execution",
				},
				"env": {
					Type:                 "object",
					Description:          "Environment variables as key-value pairs",
					AdditionalProperties: true,
				},
				"is_tui": {
					Type:        "boolean",
					Description: "Set to true if the command is a TUI application (e.g., vim, htop, qodercli). TUI apps use a screen-based monitor strategy that skips output-stability detection.",
				},
				"quiet_timeout": {
					Type:        "integer",
					Description: "Silence observation threshold in seconds. Quiet sessions remain alive; silence never terminates or restarts the process. 0 = configured default. Must be >= the stability window. NOTE: this only detects SILENCE and never bounds total lifetime — that is `ttl`'s job. A legitimate build slower than your `ttl` (default 10m) is still reaped, so for long-running work raise `ttl` alongside `quiet_timeout`. NOTE: the framework captures the process exit code (including signal deaths) and reports failure polarity on settle — let your command fail directly; do NOT mask it with `; echo \"EXIT=$?\"` (that swallows the code).",
				},
				"ttl": {
					Type:        "integer",
					Description: "ABSOLUTE lifetime of this session in seconds. The reaper terminates the backing process and retires the task this long after its last reentrant refresh (op=send / resume reset it; op=peek does not). 0 or omitted = configured default (10 minutes if unset). There is NO way to disable the reaper and NO age exemption by mode: 'resident'/'interactive' services are NOT immortal — pass a large ttl for long-lived services, or re-enter to extend. Orthogonal to quiet_timeout (which only detects silence, never bounds total lifetime). NOTE: the framework captures the process exit code (including signal deaths) and reports failure polarity on settle — let your command fail directly; do NOT mask it with `; echo \"EXIT=$?\"` (that swallows the code).",
				},
				"mode": {
					Type:        "string",
					Description: "Session liveness semantics: 'oneshot' (default, command semantics — settles when the process exits), 'resident' (long-lived service: dev server / tunnel / training — readiness requires a successful probe or ready_pattern match; silence is an observation), 'interactive' (REPL-like session: waits for a fresh prompt; input_pattern may define it).",
					Enum:        []any{"oneshot", "resident", "interactive"},
				},
				"name": {
					Type:        "string",
					Description: "Optional deterministic logical name (a-z A-Z 0-9 '-', max 64) for the session. Named sessions are addressable across calls (restart/exists) and duplicate spawn under an existing name is refused. Recommended for mode=resident/interactive services.",
				},
				"op": {
					Type:        "string",
					Description: "Session operation on an EXISTING session (instead of spawning): 'peek' = read incremental output since last peek (streaming log; use tail=N to cap lines, ansi=true to keep escape codes), 'send' = inject keys into an interactive/resident session (refused for TUI), 'stop' = graceful SIGTERM then kill-session after grace seconds. Requires session_id or name. When op is set, command/mode/is_tui are ignored.",
					Enum:        []any{"peek", "send", "stop"},
				},
				"keys": {
					Type:        "string",
					Description: "Keys to inject when op=send. A trailing Enter is appended unless enter=false.",
				},
				"enter": {
					Type:        "boolean",
					Description: "op=send only: append Enter after keys (default true).",
				},
				"tail": {
					Type:        "integer",
					Description: "op=peek only: return at most the last N lines of the new output.",
				},
				"ansi": {
					Type:        "boolean",
					Description: "op=peek only: keep ANSI escape sequences (default false = stripped for readability).",
				},
				"grace": {
					Type:        "integer",
					Description: "op=stop only: seconds to wait after SIGTERM before kill-session (default 5).",
				},
				"resource_id": {Type: "string", Description: "Immutable resource credential returned by action. Supply it with session operations to reject a reused session name."},
				"session_id": {
					Type:        "string",
					Description: "Exact tmux session id (from a previous action result) when op is set. Takes precedence over name.",
				},
				"ready_pattern": {Type: "string", Description: "Resident readiness regex. Only a matching output or successful probe marks ready."},
				"input_pattern": {Type: "string", Description: "Interactive input prompt regex. Only a fresh matching prompt requests input."},
				"probe":         {Type: "string", Description: "External health command; success proves resident readiness."},
				"watch": {
					Type:        "string",
					Description: "Regex (Go syntax). While the session runs, output matching it wakes the agent with a 'watch' settle (hits merged in a 5s window, cumulative count reported). Intended for resident sessions: watch 'ERROR|panic|OOM'.",
				},
			},
			Required: []string{"command"},
		},
	}
}

// Call executes an admitted command and waits for its declared exit, readiness, or input condition.
func (ct *ActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args ActionArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("action: invalid args: %w", err)
	}

	if ct.tmuxExecutor == nil || ct.tmuxMonitor == nil {
		return nil, fmt.Errorf("action: tmux not available (install: brew install tmux)")
	}

	if args.Op != "" {
		return ct.callSessionOp(ctx, &args)
	}

	if args.Command == "" {
		return nil, fmt.Errorf("action: command is required")
	}

	if args.QuietTimeout < 0 {
		return nil, fmt.Errorf("action: quiet_timeout must be >= 0, got %d", args.QuietTimeout)
	}
	if args.QuietTimeout > 0 {
		if minQuiet := int(ct.tmuxMonitor.stableWindow(args.IsTUI) / time.Second); args.QuietTimeout < minQuiet {
			return nil, fmt.Errorf("action: quiet_timeout (%ds) must be >= stability window (%ds) or 0 (default); shorter values would kill the session before it can stabilize", args.QuietTimeout, minQuiet)
		}
	}

	if args.TTL < 0 {
		return nil, fmt.Errorf("action: ttl must be >= 0 (0 = configured default; there is no 'unlimited'), got %d", args.TTL)
	}

	switch args.Mode {
	case "", string(ModeOneshot):
		args.Mode = string(ModeOneshot)
	case string(ModeResident), string(ModeInteractive):
		if args.IsTUI {
			return nil, fmt.Errorf("action: mode=%s conflicts with is_tui=true (TUI sessions have their own strategy)", args.Mode)
		}
	default:
		return nil, fmt.Errorf("action: unknown mode %q (want oneshot | resident | interactive)", args.Mode)
	}

	if err := validSessionName(args.Name); err != nil {
		return nil, err
	}

	log.Infof("[ActionTool] executing cmd=%q", args.Command)

	smuggleHint := smuggleHintFor(args.Command)

	if err := validateIntentPatterns(args); err != nil {
		return nil, err
	}
	if spawner, ok := task.TaskSpawnerFromContext(ctx); ok {
		res := spawner.Spawn(ct.commandSpec(spawner, args), nil)
		if res.Blocked != "" {
			return ct.buildBlockedResult(res.Blocked), nil
		}
		sessionID := ""
		if res.Task != nil {
			if decl := res.Task.SpecSnapshot().Declarative; decl != nil {
				sessionID = decl.TaskID
			}
		}
		if res.Settled {
			r := ct.buildResultFromSignal(sessionID, args.Command, args.IsTUI, res.Signal)
			if res.Task != nil {
				if decl := res.Task.SpecSnapshot().Declarative; decl != nil {
					r.ResourceID = decl.Params[pResourceID]
				}
			}
			r.Note += smuggleHint
			return r, nil
		}
		r := ct.buildAckResult(sessionID, args.Command, res.Task)
		r.Note += smuggleHint
		return r, nil
	}
	sessionID, detector, err := ct.startSession(ctx, args)
	if err != nil {
		return nil, err
	}
	for {
		select {
		case sig, ok := <-detector.Settled():
			if !ok {
				return nil, fmt.Errorf("action: session %s ended without settling", sessionID)
			}
			if !signalSatisfiesAction(args, sig) {
				continue
			}
			r := ct.buildResultFromSignal(sessionID, args.Command, args.IsTUI, sig)
			r.Note += smuggleHint
			detector.AcknowledgeSignal(sig)
			return r, nil
		case <-detector.Detached():
			return ct.buildAckResult(sessionID, args.Command, nil), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func validateIntentPatterns(args ActionArgs) error {
	for name, pattern := range map[string]string{"watch": args.Watch, "ready_pattern": args.ReadyPattern, "input_pattern": args.InputPattern} {
		if pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("action: %s: %w", name, err)
			}
		}
	}
	return nil
}

func waitForMode(mode string) task.WaitCondition {
	switch SessionMode(mode) {
	case ModeResident:
		return task.WaitReady
	case ModeInteractive:
		return task.WaitInput
	default:
		return task.WaitExit
	}
}

func signalSatisfiesAction(args ActionArgs, sig task.SettleSignal) bool {
	if sig.Kind == task.SettleCompleted || sig.Kind == task.SettleFailed {
		return true
	}
	return (args.Mode == string(ModeResident) && sig.Kind == task.SettleReady) || (args.Mode == string(ModeInteractive) && sig.Kind == task.SettleInput)
}

func (ct *ActionTool) commandSpec(spawner task.TaskSpawner, args ActionArgs) task.TaskSpec {
	decl := DeclarativeFromArgs(args, "")
	decl.Params[pPhase] = "intent"
	spec := task.TaskSpec{Kind: "command", Desc: args.Command, Key: args.Command, Mode: args.Mode, WaitFor: waitForMode(args.Mode), Declarative: decl, TTL: ct.resolveTTL(args), Relaunch: ct.relaunchClosure(spawner, args)}
	spec.Start = func(tk *task.Task) (task.TaskSpec, task.SettleDetector, error) {
		session, detector, err := ct.prepareSession(context.Background(), args)
		if err != nil {
			return task.TaskSpec{}, nil, err
		}
		detector.sourceTaskID = tk.ID
		bound := tk.SpecSnapshot()
		bound.Start = nil
		bound.Declarative = DeclarativeFromArgs(args, session.ID)
		bound.Declarative.Params[pResourceID] = session.ResourceID
		bound.Declarative.Params[pPhase], bound.Declarative.Params[pPlaceholder] = session.Phase, session.Placeholder
		bound.ResumeFn = ct.resumeResourceClosure(session.ID, session.ResourceID, args.IsTUI, detector)
		bound.Alive = ct.resourceAliveClosure(session.ID, session.ResourceID)
		return bound, detector, nil
	}
	return spec
}

func (ct *ActionTool) prepareSession(ctx context.Context, args ActionArgs) (*TmuxSession, *TmuxSettleDetector, error) {
	if ct.closed.Load() {
		return nil, nil, fmt.Errorf("action: tool is closed")
	}
	if args.Mode == string(ModeResident) || args.Mode == string(ModeInteractive) {
		ct.detectorMu.Lock()
		count := ct.residentAdmissions
		for _, detector := range ct.detectors {
			if detector.mode != ModeResident && detector.mode != ModeInteractive {
				continue
			}
			select {
			case <-detector.Stopped():
			default:
				count++
			}
		}
		if count >= maxResidentSessions {
			ct.detectorMu.Unlock()
			return nil, nil, fmt.Errorf("action: resident session cap (%d) reached; stop an existing resident before spawning another", maxResidentSessions)
		}
		ct.residentAdmissions++
		ct.detectorMu.Unlock()
		defer func() { ct.detectorMu.Lock(); ct.residentAdmissions--; ct.detectorMu.Unlock() }()
	}
	session, err := ct.tmuxExecutor.PrepareSession(ctx, TmuxCreateOptions{Command: args.Command, WorkDir: args.WorkDir, Env: args.Env, Mode: SessionMode(args.Mode), Name: args.Name})
	if err != nil {
		return nil, nil, fmt.Errorf("action: %w", err)
	}
	session.IsTUI, session.Mode, session.QuietTimeout = args.IsTUI, SessionMode(args.Mode), time.Duration(args.QuietTimeout)*time.Second
	detector, err := ct.detectorForSession(session, args)
	if err != nil {
		_ = ct.tmuxExecutor.killResource(session.ID, session.ResourceID)
		return nil, nil, err
	}
	ct.saveBoundMeta(session, args)
	detector.startFn = func() error {
		if ct.closed.Load() {
			return fmt.Errorf("action: tool closed before activation")
		}
		if err := ct.tmuxExecutor.activateResource(session.ID, session.ResourceID, session.Placeholder, args.Command); err != nil {
			return fmt.Errorf("action: activate %s: %w", session.ID, err)
		}
		session.Phase = "active"
		ct.saveBoundMeta(session, args)
		return ct.activateMonitoring(session, args, detector)
	}
	return session, detector, nil
}

func (ct *ActionTool) detectorForSession(session *TmuxSession, args ActionArgs) (*TmuxSettleDetector, error) {
	id, resource := session.ID, session.ResourceID
	detector := NewTmuxSettleDetector(id, nil)
	detector.resourceID = resource
	if err := detector.ConfigureIntent(SessionMode(args.Mode), args.ReadyPattern, args.InputPattern); err != nil {
		return nil, err
	}
	if args.Watch != "" {
		if err := detector.SetWatch(args.Watch, 5*time.Second); err != nil {
			return nil, err
		}
	}
	detector.SetPaneStatusReader(func() (int, bool) {
		if _, err := ct.tmuxExecutor.resourceTarget(id, resource); err != nil {
			return 0, false
		}
		return ct.tmuxExecutor.PaneDeadStatus(id)
	})
	cleanup := func() error {
		if err := ct.tmuxExecutor.killResource(id, resource); err != nil {
			return err
		}
		ct.removeBoundMeta(id, resource)
		ct.tmuxMonitor.RemoveResource(id, resource)
		ct.detectorMu.Lock()
		if ct.detectors[resource] == detector {
			delete(ct.detectors, resource)
		}
		ct.detectorMu.Unlock()
		return nil
	}
	detector.stopFn = func() bool {
		if err := cleanup(); err != nil {
			log.Warnf("[ActionTool] stop %s: %v", id, err)
			return ct.tmuxExecutor.resourceStopped(id, resource)
		}
		return ct.tmuxExecutor.resourceStopped(id, resource)
	}
	detector.ackFn = func() error {
		if err := cleanup(); err != nil {
			log.Warnf("[ActionTool] release terminal evidence %s: %v", id, err)
			return err
		}
		_ = os.Remove(ct.terminalOutputPath(id, resource))
		_ = os.Remove(ct.tmuxExecutor.resourcePipePath(id, resource))
		return nil
	}
	detector.beginRoundFn = func(round uint64) { ct.tmuxMonitor.BindProducerRound(id, round) }
	ct.detectorMu.Lock()
	if ct.detectors == nil {
		ct.detectors = make(map[string]*TmuxSettleDetector)
	}
	ct.detectors[resource] = detector
	ct.detectorMu.Unlock()
	return detector, nil
}

func (ct *ActionTool) activateMonitoring(session *TmuxSession, args ActionArgs, detector *TmuxSettleDetector) error {
	ct.startMu.Lock()
	defer ct.startMu.Unlock()
	if ct.closed.Load() {
		return fmt.Errorf("action: tool closed before monitor activation")
	}
	if current, err := ct.tmuxExecutor.SessionResourceID(session.ID); err == nil && current != session.ResourceID {
		return fmt.Errorf("action: session %s resource was replaced", session.ID)
	}
	session.Status, session.ProducerRound = SessionRunning, detector.ProducerRound()
	if !ct.tmuxMonitor.AddSessionWithRoundCallback(session, func(round uint64, status SessionStatus, output string) {
		if current, err := ct.tmuxExecutor.SessionResourceID(session.ID); err == nil && current != session.ResourceID {
			detector.OnObservation(round, SessionError, session.LastOutput)
			return
		}
		if isTerminalStatus(status) {
			ct.persistTerminalOutput(session.ID, session.ResourceID, output)
		}
		detector.OnWatchOutputRound(round, output)
		detector.OnObservation(round, status, output)
	}) {
		return fmt.Errorf("action: session %s is tracked under a different resource", session.ID)
	}
	if args.Probe != "" {
		ct.startProbeLoop(session.ID, args, detector)
	}
	ct.tmuxMonitor.Start()
	return nil
}

// startSession prepares and activates an unmanaged command session.
func (ct *ActionTool) startSession(ctx context.Context, args ActionArgs) (string, *TmuxSettleDetector, error) {
	session, detector, err := ct.prepareSession(ctx, args)
	if err != nil {
		return "", nil, err
	}
	detector.Start()
	return session.ID, detector, nil
}

func (ct *ActionTool) relaunchClosure(spawner task.TaskSpawner, args ActionArgs) func(context.Context) (task.SpawnResult, error) {
	return func(ctx context.Context) (task.SpawnResult, error) {
		current := spawner
		if initiating, ok := task.TaskSpawnerFromContext(ctx); ok {
			current = initiating
		}
		if current == nil {
			return task.SpawnResult{}, fmt.Errorf("action: no current task spawner for relaunch")
		}
		return current.Spawn(ct.commandSpec(current, args), nil), nil
	}
}

func (ct *ActionTool) sessionAliveClosure(sessionID string) func() bool {
	return func() bool { return ct.tmuxExecutor.SessionExists(sessionID) }
}

func (ct *ActionTool) resourceAliveClosure(sessionID, resourceID string) func() bool {
	return func() bool {
		current, err := ct.tmuxExecutor.SessionResourceID(sessionID)
		if err != nil {
			alive, known := ct.tmuxExecutor.SessionAlive3(sessionID)
			return !known || alive
		}
		return current == resourceID && !ct.tmuxExecutor.IsPaneDead(sessionID)
	}
}

func (ct *ActionTool) resumeClosure(sessionID string, isTUI bool, detector *TmuxSettleDetector) func(context.Context, string) (task.SettleDetector, error) {
	return ct.resumeResourceClosure(sessionID, detector.resourceID, isTUI, detector)
}

func (ct *ActionTool) resumeResourceClosure(sessionID, resourceID string, isTUI bool, detector *TmuxSettleDetector) func(context.Context, string) (task.SettleDetector, error) {
	return func(_ context.Context, input string) (task.SettleDetector, error) {
		if isTUI {
			return nil, fmt.Errorf("session %s is a TUI; use cancel and relaunch", sessionID)
		}
		if _, err := ct.tmuxExecutor.resourceTarget(sessionID, resourceID); err != nil {
			return nil, err
		}
		if !ct.tmuxMonitor.TouchSession(sessionID) {
			return nil, fmt.Errorf("session %s is no longer monitored; use relaunch_task", sessionID)
		}
		output, err := ct.tmuxExecutor.resourceOutput(sessionID, resourceID)
		if err != nil {
			return nil, fmt.Errorf("action: read resume output boundary: %w", err)
		}
		detector.RearmOutput(output)
		return &preparedResumeDetector{TmuxSettleDetector: detector, monitor: ct.tmuxMonitor, activate: func() error { return ct.tmuxExecutor.sendResource(sessionID, resourceID, input+"\n") }}, nil
	}
}

type preparedResumeDetector struct {
	*TmuxSettleDetector
	once      sync.Once
	activate  func() error
	monitor   *TmuxMonitor
	requested uint64
}

func (d *preparedResumeDetector) BeginRound(round uint64) { d.requested = round }

func (d *preparedResumeDetector) Start() {
	d.once.Do(func() {
		d.lifecycleMu.Lock()
		defer d.lifecycleMu.Unlock()
		select {
		case <-d.stop:
			return
		default:
		}
		d.monitor.mu.Lock()
		select {
		case <-d.stop:
			d.monitor.mu.Unlock()
			return
		default:
		}
		oldRound := d.ProducerRound()
		err := d.activate()
		if err == nil {
			d.mu.Lock()
			d.round, d.ready, d.inputSnapshot = d.requested, false, ""
			d.mu.Unlock()
			if session := d.monitor.sessions[d.sessionID]; session != nil {
				session.ProducerRound = d.requested
			}
		}
		d.monitor.mu.Unlock()
		if err != nil {
			d.emit(task.SettleSignal{Kind: task.SettleFailed, Err: err, Observation: "round_aborted", WaitSatisfied: true, RoundID: d.requested, ProducerRound: oldRound})
		}
	})
}

// settleToStatus maps a task settle signal to the tmux-style status string
// surfaced to the LLM in ActionToolResult.
func settleToStatus(sig task.SettleSignal) string {
	if sig.Err != nil {
		return "error"
	}
	switch sig.Kind {
	case task.SettleCompleted:
		return "completed"
	case task.SettleReady:
		return "ready"
	case task.SettleInput:
		return "input"
	case task.SettleStable:
		return "stable"
	case task.SettleSuspect:
		return "timed_out"
	default:
		return string(sig.Kind)
	}
}

// buildAckResult composes the tool result for an asynchronously-tracked command
// that did not settle within the sync-wait window. The session keeps running;
// its settle is written back later through the task layer.
func (ct *ActionTool) buildAckResult(sessionID, command string, task *task.Task) *ActionToolResult {
	note := "命令已在后台运行，满足声明的等待条件或退出后会自动回写结果；你现在可以给出简短回复并结束本回合，无需等待或轮询。"
	if task != nil {
		note = fmt.Sprintf("命令已在后台运行 (task %s)，满足声明的等待条件或退出后会自动回写结果；你现在可以给出简短回复并结束本回合，无需等待或轮询。", task.ID)
	}
	resource := ""
	if task != nil {
		if decl := task.SpecSnapshot().Declarative; decl != nil {
			resource = decl.Params[pResourceID]
		}
	}
	return &ActionToolResult{
		SessionID:  sessionID,
		ResourceID: resource,
		Command:    command,
		Status:     "running",
		Note:       note,
	}
}

// buildBlockedResult reports that the command was refused before business execution.
func (ct *ActionTool) buildBlockedResult(reason string) *ActionToolResult {
	return &ActionToolResult{Status: "blocked", Note: "命令未执行：" + reason}
}

// buildResultFromSignal composes the tool result payload the framework will
// forward to the LLM as a role=tool message, from a task settle signal.
func (ct *ActionTool) buildResultFromSignal(sessionID, command string, isTUI bool, sig task.SettleSignal) *ActionToolResult {
	// Enrich with any per-session context still available in the monitor
	// (e.g. how long the session has been stable).
	var extraNote string
	if ct.tmuxMonitor != nil {
		if session, ok := ct.tmuxMonitor.GetSession(sessionID); ok {
			if isTUI {
				extraNote = "TUI 会话 (基于屏幕，无心跳检测)"
			}
			if !session.StableSince.IsZero() {
				stableDuration := time.Since(session.StableSince).Round(time.Second)
				if extraNote != "" {
					extraNote += "; "
				}
				extraNote += fmt.Sprintf("会话已稳定 %v", stableDuration)
			}
		}
	}

	output := sig.Output
	var outputFile string
	if output != "" {
		output = cleanTmuxOutput(output)
		if len(output) > 2000 {
			outputDir := ct.outputDir
			if outputDir == "" {
				outputDir = ct.workspace
			}
			if outputDir == "" {
				if wd, err := os.Getwd(); err == nil {
					outputDir = wd
				} else {
					outputDir = "."
				}
			}
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				log.Warnf("[ActionTool] output dir %q ensure failed: %v", outputDir, err)
			}
			path := filepath.Join(outputDir, fmt.Sprintf("output_%s.txt", sessionID))
			if err := os.WriteFile(path, []byte(output), 0644); err != nil {
				log.Warnf("[ActionTool] failed to save output to %s: %v", path, err)
			} else {
				log.Infof("[ActionTool] full output saved to %s (%d chars)", path, len(output))
				outputFile = path
				output = "..." + output[len(output)-2000:]
			}
		}
	}

	return &ActionToolResult{
		SessionID:  sessionID,
		ResourceID: ct.signalResource(sessionID, sig),
		Command:    command,
		Status:     settleToStatus(sig),
		Output:     output,
		OutputFile: outputFile,
		Note:       extraNote,
		ExitCode:   sig.ExitCode,
	}
}

func (ct *ActionTool) signalResource(sessionID string, sig task.SettleSignal) string {
	ct.detectorMu.Lock()
	defer ct.detectorMu.Unlock()
	for resource, detector := range ct.detectors {
		if detector.sessionID == sessionID && (detector.sourceTaskID == "" || strings.HasPrefix(sig.SourceID, "task-settle-"+detector.sourceTaskID+"-")) {
			return resource
		}
	}
	return ""
}

// ActionArgs represents a command execution request.
type ActionArgs struct {
	Command string            `json:"command"`
	Timeout int               `json:"timeout,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	IsTUI   bool              `json:"is_tui,omitempty"`
	// QuietTimeout sets the silence observation threshold in seconds; zero uses the configured default.
	QuietTimeout int `json:"quiet_timeout,omitempty"`
	// TTL is the requested ABSOLUTE lifetime (seconds) for a spawned session: the
	// unified reaper terminates its backing process and retires the task this long
	// after its last reentrant refresh (op=send/resume reset it; op=peek does not).
	// 0/omitted = the configured default TTL (empty config → 10 minutes). There is
	// NO disable sentinel and NO age exemption by mode: resident/interactive must
	// pass a large ttl or re-enter to stay alive. Orthogonal to QuietTimeout, which
	// only detects silence and never bounds total lifetime (async-task-lifetime).
	TTL int `json:"ttl,omitempty"`
	// Mode selects oneshot exit, resident readiness, or interactive input semantics.
	Mode string `json:"mode,omitempty"`
	// Name: deterministic logical name for the session
	// ([a-zA-Z0-9-]{1,64}). The session becomes addressable by this name in
	// later calls (restart/exists checks); duplicate spawn under the same
	// name is refused. Empty = an auto-generated unique name.
	Name string `json:"name,omitempty"`
	// Op Session-operations. When Op != "" the call addresses an
	// existing session instead of spawning a new one:
	// Op="peek" — read incremental output since the last peek cursor
	// (tail=N caps lines; ansi=true keeps escape sequences).
	// Op="send" — inject Keys into the session (append Enter unless
	// enter=false); the session must be non-TUI.
	// Op="stop" — graceful stop: SIGTERM the pane process (fallback
	// kill-session), grace seconds then SIGKILL.
	// Target: session_id (exact) or name (logical name → n-<name>).
	Op         string `json:"op,omitempty"`
	Keys       string `json:"keys,omitempty"`
	Enter      *bool  `json:"enter,omitempty"`
	Tail       int    `json:"tail,omitempty"`
	Ansi       bool   `json:"ansi,omitempty"`
	GraceSec   int    `json:"grace,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
	// Watch: pattern-triggered wakeups. While the session
	// runs, output matching this regex wakes the agent (merged within a
	// 5s window; cumulative hit count reported). Primary use: resident
	// sessions (watch "ERROR|panic|OOM" on a dev server / trainer log).
	Watch        string `json:"watch,omitempty"`
	ReadyPattern string `json:"ready_pattern,omitempty"`
	InputPattern string `json:"input_pattern,omitempty"`
	// Probe: liveness check for resident sessions. A shell
	// command run every ProbeIntervalSec (default 30); after ProbeFailures
	// (default 3) consecutive failures the agent is woken once. Example:
	// "curl -sf http://localhost:8080/healthz".
	Probe            string `json:"probe,omitempty"`
	ProbeIntervalSec int    `json:"probe_interval,omitempty"`
	ProbeFailures    int    `json:"probe_failures,omitempty"`
}

// validSessionName validates a caller-supplied logical session name.
// Empty is legal (auto-naming). Returns nil when valid.
func validSessionName(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > 64 {
		return fmt.Errorf("action: name %q too long (max 64 chars)", name)
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return fmt.Errorf("action: name %q contains invalid character %q (allowed: a-z A-Z 0-9 -)", name, r)
		}
	}
	return nil
}

// ActionToolResult represents the outcome of a tmux command execution after
// the session reaches a stable state. It is returned as the tool_call result
// and rendered by the framework as a role=tool message.
type ActionToolResult struct {
	SessionID  string `json:"session_id"`
	ResourceID string `json:"resource_id,omitempty"`
	Command    string `json:"command"`
	OldStatus  string `json:"old_status,omitempty"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	OutputFile string `json:"output_file,omitempty"`
	Note       string `json:"note,omitempty"`
	// ExitCode carries the process exit status on failure polarity
	// (failure-polarity passthrough D2): non-zero, or negative for a signal death.
	// Omitted when zero (clean exit / not applicable) to avoid noise.
	ExitCode int `json:"exit_code,omitempty"`
}

// IsTmuxAvailable reports whether tmux is actually usable on this system —
// a real PATH probe. The old implementation
// returned NewTmuxExecutor() != nil, which was always true (the constructor
// never returns nil), making the availability gate at the construction site
// inert: no-tmux environments sailed through to first-execution failures.
func IsTmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// cleanTmuxOutput strips trailing blank lines from tmux capture-pane output.
// The -S -1000 option captures full scrollback which often includes many blank
// lines after short commands, wasting token budget. We keep leading/trailing
// content (including "Pane is dead" messages) but collapse consecutive blank
// lines in the middle and strip trailing blanks.
func cleanTmuxOutput(output string) string {
	lines := strings.Split(output, "\n")

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	// Collapse consecutive blank lines in the middle (keep at most 1)
	var result []string
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank {
			continue
		}
		result = append(result, line)
		prevBlank = isBlank
	}

	return strings.Join(result, "\n")
}

// ansiEscape matches ANSI/VT escape sequences (CSI, OSC, simple two-byte).
var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[a-zA-Z]|\][^\x07]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

// stripANSI removes ANSI escape sequences for LLM-friendly output.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// peekCursors holds the byte offsets each session's incremental peek has
// consumed from its pipe log. Guarded by peekMu.
type peekCursors struct {
	mu      sync.Mutex
	offsets map[string]int64
}

func (c *peekCursors) get(id string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.offsets == nil {
		return 0
	}
	return c.offsets[id]
}

func (c *peekCursors) set(id string, off int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.offsets == nil {
		c.offsets = make(map[string]int64)
	}
	c.offsets[id] = off
}

// resolveTarget maps call args to a concrete tmux session id: explicit
// session_id wins; otherwise name must be present and maps to n-<name>.
func (ct *ActionTool) resolveTarget(args *ActionArgs) (string, error) {
	if args.SessionID != "" {
		return args.SessionID, nil
	}
	if args.Name != "" {
		if err := validSessionName(args.Name); err != nil {
			return "", err
		}
		return NamedSessionName(args.Name), nil
	}
	return "", fmt.Errorf("action: op=%s requires session_id or name", args.Op)
}

// pipeRotateBytes is the pipe-log rotation threshold. Var,
// not const, so tests can shrink it. Long-resident sessions pipe megabytes
// of log; without rotation both peek reads and inode size grow unbounded.
var pipeRotateBytes int64 = 10 << 20

// maybeRotatePipe opportunistically rotates an overgrown pipe log on peek.
// COPYTRUNCATE semantics: copy current content to pf+".1", then truncate the
// live file. pipe-pane's O_APPEND fd keeps appending from offset 0 after
// truncate — no re-attach, no lost writes (a rename would leave the fd
// writing into the rotated-away inode). peek's existing "len < cursor →
// reset" rule transparently adapts to the truncation.
func (ct *ActionTool) maybeRotatePipe(target string) {
	path := ct.tmuxExecutor.PipeFileFor(target)
	if resource, err := ct.tmuxExecutor.SessionResourceID(target); err == nil && path == ct.tmuxExecutor.resourcePipePath(target, resource) {
		return
	}
	rotatePipeFile(path)
}

// rotatePipeFile is the copytruncate core, path-addressed for testability.
func rotatePipeFile(pf string) {
	st, err := os.Stat(pf)
	if err != nil || st.Size() < pipeRotateBytes {
		return
	}
	b, err := os.ReadFile(pf)
	if err != nil {
		return
	}
	if err := os.WriteFile(pf+".1", b, 0o600); err != nil {
		return
	}
	_ = os.Truncate(pf, 0)
	log.Infof("[ActionTool] rotated pipe log %s (%d bytes → .1)", pf, len(b))
}

// callSessionOp executes peek/send/stop against an existing session.
func (ct *ActionTool) callSessionOp(ctx context.Context, args *ActionArgs) (any, error) {
	target, err := ct.resolveTarget(args)
	if err != nil {
		return nil, err
	}
	switch args.Op {
	case "peek":
		return ct.opPeek(args, target)
	case "send":
		res, err := ct.opSend(args, target)
		if err == nil {
			if tc, ok := task.TaskControllerFromContext(ctx); ok {
				tc.RenewTTLBySession(target)
			}
		}
		return res, err
	case "stop":
		return ct.opStop(args, target)
	default:
		return nil, fmt.Errorf("action: unknown op %q (want peek | send | stop)", args.Op)
	}
}

func (ct *ActionTool) operationResource(args *ActionArgs, target string) (string, error) {
	resource := args.ResourceID
	if resource == "" && ct.tmuxMonitor != nil {
		if session, ok := ct.tmuxMonitor.GetSession(target); ok {
			resource = session.ResourceID
		}
	}
	if resource == "" {
		if b, err := os.ReadFile(ct.metaPath(target)); err == nil {
			var meta ResidentMeta
			if json.Unmarshal(b, &meta) == nil {
				resource = meta.ResourceID
			}
		}
	}
	if resource == "" {
		var err error
		resource, err = ct.tmuxExecutor.SessionResourceID(target)
		if err != nil {
			return "", err
		}
	}
	if _, err := ct.tmuxExecutor.resourceTarget(target, resource); err != nil {
		return "", err
	}
	return resource, nil
}

// opPeek returns output appended since the last peek on this session (or the
// whole log on first peek). tail=N bounds the returned lines (last N);
// ansi=true keeps escape sequences, default strips them for LLM readability.
func (ct *ActionTool) opPeek(args *ActionArgs, target string) (any, error) {
	ct.maybeRotatePipe(target)
	pf := ct.tmuxExecutor.PipeFileFor(target)
	b, err := os.ReadFile(pf)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{
				"session_id": target, "status": "no_output_log",
				"note": "pipe log missing — session may have exited and been archived; use a fresh spawn or check archived pipes",
			}, nil
		}
		return nil, fmt.Errorf("action: peek %s: %w", target, err)
	}

	from := ct.peeks.get(target)
	if int64(len(b)) < from {
		from = 0
	}
	fresh := b[from:]
	ct.peeks.set(target, int64(len(b)))

	text := string(fresh)
	text = strings.TrimSuffix(text, "\n")
	var lines []string
	if text == "" {
		lines = nil
	} else {
		lines = strings.Split(text, "\n")
	}
	truncated := false
	if args.Tail > 0 && len(lines) > args.Tail {
		lines = lines[len(lines)-args.Tail:]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if !args.Ansi {
		out = stripANSI(out)
	}
	return map[string]any{
		"session_id": target,
		"status":     "ok",
		"bytes_new":  len(fresh),
		"truncated":  truncated,
		"output":     out,
	}, nil
}

// opSend injects keys into the session (interactive/resident only; TUI
// sessions are refused — send-keys corrupts their screen state).
func (ct *ActionTool) opSend(args *ActionArgs, target string) (any, error) {
	if args.Keys == "" {
		return nil, fmt.Errorf("action: op=send requires keys")
	}
	if !ct.tmuxExecutor.SessionExists(target) {
		return nil, fmt.Errorf("action: session %s not found (stopped or never spawned)", target)
	}
	if sess, ok := ct.tmuxMonitor.GetSession(target); ok && sess.IsTUI {
		return nil, fmt.Errorf("action: send refused: session %s is a TUI session (send-keys corrupts screen state); stop and respawn non-TUI instead", target)
	}
	enter := true
	if args.Enter != nil {
		enter = *args.Enter
	}
	keys := args.Keys
	if enter {
		keys += "\n"
	}
	resource, err := ct.operationResource(args, target)
	if err != nil {
		return nil, err
	}
	if err := ct.tmuxExecutor.sendResource(target, resource, keys); err != nil {
		return nil, fmt.Errorf("action: send to %s: %w", target, err)
	}
	return map[string]any{
		"session_id": target, "status": "ok",
		"note": "keys injected; peek to observe the response",
	}, nil
}

// opStop gracefully terminates a session: SIGTERM the pane process first,
// escalating to kill-session after a grace period.
func (ct *ActionTool) opStop(args *ActionArgs, target string) (any, error) {
	if !ct.tmuxExecutor.SessionExists(target) {
		return map[string]any{"session_id": target, "status": "already_gone"}, nil
	}
	resource, err := ct.operationResource(args, target)
	if err != nil {
		return nil, err
	}
	native, err := ct.tmuxExecutor.resourceTarget(target, resource)
	if err != nil {
		return nil, err
	}
	grace := 5
	if args.GraceSec > 0 {
		grace = args.GraceSec
	}
	if pid, err := ct.tmuxExecutor.GetSessionPIDPublic(native); err == nil && pid > 0 {
		if _, err := ct.tmuxExecutor.resourceTarget(target, resource); err != nil {
			return nil, err
		}
		if p, findErr := os.FindProcess(pid); findErr == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
		deadline := time.Now().Add(time.Duration(grace) * time.Second)
		for time.Now().Before(deadline) {
			if ct.tmuxExecutor.resourceStopped(target, resource) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	if err := ct.tmuxExecutor.killResource(target, resource); err != nil {
		return nil, fmt.Errorf("action: stop %s: %w", target, err)
	}
	ct.removeBoundMeta(target, resource)
	return map[string]any{"session_id": target, "status": "stopped", "how": "kill-session"}, nil
}

// startProbeLoop launches the background liveness prober for a resident
// session. Runs args.Probe via sh every interval; after
// `failures` consecutive failures it wakes the agent once via the detector
// (EmitProbeResult latches until a success resets). The loop exits when the
// session's detector is cancelled/reaped — the reaper closure stops it.
func (ct *ActionTool) startProbeLoop(sessionID string, args ActionArgs, detector *TmuxSettleDetector) {
	interval := time.Duration(args.ProbeIntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	failures := args.ProbeFailures
	if failures <= 0 {
		failures = 3
	}
	stop := detector.Done()
	go func() {
		ticker := time.NewTimer(0)
		defer ticker.Stop()
		probeParent, stopProbe := context.WithCancel(context.Background())
		defer stopProbe()
		go func() {
			select {
			case <-stop:
				stopProbe()
			case <-probeParent.Done():
			}
		}()
		consec := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				ticker.Reset(interval)
				producerRound := detector.ProducerRound()
				probeCtx, cancel := context.WithTimeout(probeParent, interval/2)
				out, err := ct.probeOnceWithArgs(probeCtx, args)
				cancel()
				if detector.ProducerRound() != producerRound {
					continue
				}
				if err == nil {
					consec = 0
					detector.EmitProbeResultRound(producerRound, true, "")
					continue
				}
				consec++
				log.Warnf("[ActionTool] probe %s failed (%d/%d): %v%s", sessionID, consec, failures, err, truncateForLog(out, 120))
				if consec >= failures {
					detector.EmitProbeResultRound(producerRound, false, fmt.Sprintf("%d consecutive failures, last: %v", consec, err))
				}
			}
		}
	}()
}

// probeOnce runs one probe command with its own timeout. Output is truncated
// to keep the settle signal small.
func (ct *ActionTool) probeOnce(ctx context.Context, probe string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", probe)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (ct *ActionTool) probeOnceWithArgs(ctx context.Context, args ActionArgs) (string, error) {
	name, argv := "sh", []string{"-c", args.Probe}
	if ct.runAsUser != "" {
		name, argv = "sudo", []string{"-n", "-u", ct.runAsUser}
		if ct.runAsGroup != "" {
			argv = append(argv, "-g", ct.runAsGroup)
		}
		argv = append(argv, "sh", "-c", args.Probe)
	}
	cmd := exec.CommandContext(ctx, name, argv...)
	cmd.Dir = args.WorkDir
	if cmd.Dir == "" {
		cmd.Dir = ct.workspace
	}
	cmd.Env = os.Environ()
	for key, value := range args.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func truncateForLog(s string, n int) string {
	if len(s) > n {
		return " out=" + s[:n] + "…"
	}
	if s != "" {
		return " out=" + s
	}
	return ""
}

// rememberReattachedDetector caches a reattach-built detector (3.1 bridge).
func (ct *ActionTool) rememberReattachedDetector(sessionID string, d task.SettleDetector) {
	ct.reattachMu.Lock()
	defer ct.reattachMu.Unlock()
	if ct.reattachedMap == nil {
		ct.reattachedMap = make(map[string]task.SettleDetector)
	}
	ct.reattachedMap[sessionID] = d
}

// TakeReattachedDetector pops the reattach-built detector for a session
// (build_agent binds it to the restored task after RebuildTaskRegistry).
func (ct *ActionTool) TakeReattachedDetector(sessionID string) task.SettleDetector {
	ct.reattachMu.Lock()
	defer ct.reattachMu.Unlock()
	d, ok := ct.reattachedMap[sessionID]
	if ok {
		delete(ct.reattachedMap, sessionID)
	}
	return d
}

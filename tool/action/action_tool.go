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
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent/task"
)

// Verify ActionTool implements tool.CallableTool at compile time.
var _ tool.CallableTool = (*ActionTool)(nil)

// ActionTool is a shell command execution tool.
//
// Every command runs in a tmux session and Call() blocks until the session
// reaches a stable state (Stable/Completed/Error/TimedOut) as detected by
// TmuxMonitor. The final tool result carries the command, session ID, final
// status and captured output — so the framework records it as a proper
// role=tool message.
//
// Tool name is "action" — it represents performing behavioral actions on
// real-world resources triggered by natural language descriptions.
type ActionTool struct {
	workspace     string
	outputDir     string // oversized-output save dir (scratch), separate from command cwd
	runAsUser     string
	runAsGroup    string
	description   string // Configurable tool description
	tmuxExecutor  *TmuxExecutor
	tmuxMonitor   *TmuxMonitor
	monitorConfig *MonitorConfig // Optional: override default monitor config
	// orphanCleanupDisabled skips the startup reaping of prefix-matched
	// leftover sessions (see WithOrphanCleanupDisabled).
	orphanCleanupDisabled bool

	// peeks tracks incremental peek cursors per session (B3 session ops).
	peeks peekCursors

	closeOnce sync.Once
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

// NewActionTool creates a new ActionTool.
func NewActionTool(opts ...ActionToolOption) *ActionTool {
	ct := &ActionTool{
		description: "Execute a shell command via tmux and wait for it to stabilize. Returns the final status and captured output.",
	}

	for _, opt := range opts {
		opt(ct)
	}

	// Set up TmuxExecutor and TmuxMonitor if tmux is available
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
		ct.tmuxMonitor = NewTmuxMonitor(
			WithMonitorExecutor(ct.tmuxExecutor),
			WithMonitorConfig(monCfg),
		)
		// Reap orphan sessions left by a previous instance (crash or stop
		// while commands were running): nobody monitors them, they would
		// never be killed, and each holds a pty. Disable via
		// WithOrphanCleanupDisabled when running multiple instances that
		// share a tmux server (use distinct prefixes instead).
		if !ct.orphanCleanupDisabled {
			ct.tmuxExecutor.CleanupOrphanSessions()
		}
		// D1: resident sessions survive agent restarts (tmux server keeps
		// them + their pipe-pane loggers). Rebuild tracking from the
		// persisted metadata so their watch/probe keep working.
		ct.ReattachResidentSessions()
	}

	return ct
}

// Close stops the TmuxMonitor and reaps all sessions this instance still
// tracks: on graceful shutdown nothing keeps monitoring them, so leaving them
// alive would leak orphan sessions (and their ptys) until the next startup's
// orphan cleanup. Uses sync.Once to ensure idempotent closure.
func (ct *ActionTool) Close() error {
	ct.closeOnce.Do(func() {
		if ct.tmuxMonitor != nil && ct.tmuxMonitor.IsRunning() {
			ct.tmuxMonitor.Stop()
		}
		if ct.tmuxMonitor != nil && ct.tmuxExecutor != nil {
			for _, id := range ct.tmuxMonitor.SessionIDs() {
				if err := ct.tmuxExecutor.KillSession(id); err != nil {
					log.Warnf("[ActionTool] close: kill session %s failed: %v", id, err)
				}
			}
		}
	})
	return nil
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
					Description: "Per-session fake-dead threshold override in seconds. Silent-but-legal tasks (long downloads, compiles, model inference) produce no output while working; the default 150s kills them. 0 or omitted = default (150s). Must be >= the stability window (60s; TUI 90s) - shorter values are rejected. Recommended 600+ for installs and builds.",
				},
				"mode": {
					Type:        "string",
					Description: "Session liveness semantics: 'oneshot' (default, command semantics — settles when the process exits), 'resident' (long-lived service: dev server / tunnel / training — silence is healthy, no auto-kill, only unexpected death reports back; pair with an output redirect to a log file you can read later), 'interactive' (REPL-like session you intend to send input to later via resume).",
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
				"session_id": {
					Type:        "string",
					Description: "Exact tmux session id (from a previous action result) when op is set. Takes precedence over name.",
				},
				"watch": {
					Type:        "string",
					Description: "Regex (Go syntax). While the session runs, output matching it wakes the agent with a 'watch' settle (hits merged in a 5s window, cumulative count reported). Intended for resident sessions: watch 'ERROR|panic|OOM'.",
				},
			},
			Required: []string{"command"},
		},
	}
}

// Call implements tool.CallableTool.
//
// Creates a tmux session for the given command and blocks until the session
// reaches a stable state (Stable/Completed/Error/TimedOut) as observed by
// TmuxMonitor, at which point the final output is returned as the tool
// result. If ctx is cancelled first, the session keeps running but Call()
// returns with the context error.
func (ct *ActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args ActionArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("action: invalid args: %w", err)
	}

	if ct.tmuxExecutor == nil || ct.tmuxMonitor == nil {
		return nil, fmt.Errorf("action: tmux not available (install: brew install tmux)")
	}

	// Session-operations dispatch (2026-09-11 B3): when "op" is present the
	// call addresses an EXISTING session (peek/send/stop) instead of spawning
	// a new one. Command semantics (spawn) remain the default path. Dispatch
	// precedes the command-required check: op calls carry no command.
	if args.Op != "" {
		return ct.callSessionOp(ctx, &args)
	}

	if args.Command == "" {
		return nil, fmt.Errorf("action: command is required")
	}

	// quiet_timeout validation: must be either 0 (default) or >= the stability
	// window, otherwise a session would be killed as fake-dead before it ever
	// gets a chance to fire its Stable event.
	if args.QuietTimeout < 0 {
		return nil, fmt.Errorf("action: quiet_timeout must be >= 0, got %d", args.QuietTimeout)
	}
	if args.QuietTimeout > 0 {
		if minQuiet := int(ct.tmuxMonitor.stableWindow(args.IsTUI) / time.Second); args.QuietTimeout < minQuiet {
			return nil, fmt.Errorf("action: quiet_timeout (%ds) must be >= stability window (%ds) or 0 (default); shorter values would kill the session before it can stabilize", args.QuietTimeout, minQuiet)
		}
	}

	// mode validation + normalization (B1). Empty = oneshot.
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

	// name validation (B2): DNS-label-safe, bounded, so the derived tmux
	// session name is always a valid tmux target.
	if err := validSessionName(args.Name); err != nil {
		return nil, err
	}

	log.Infof("[ActionTool] executing cmd=%q", args.Command)

	sessionID, detector, err := ct.startSession(ctx, args)
	if err != nil {
		// tmux-LEVEL exception: the session itself could not be created (e.g. no
		// PTY in this runtime — "fork failed: Device not configured"), as opposed
		// to a tmux-TASK error (a command that runs but exits non-zero, which is
		// captured via the settle signal as a normal tool result). A tmux-level
		// failure is a FRAMEWORK/environment exception — log it in full so it is
		// diagnosable in the bot log, not silently returned as a plain tool error.
		log.Errorf("[ActionTool] tmux-level exception (framework/environment), cmd=%q: %v", args.Command, err)
		return nil, err
	}

	// Async path: hand the detector to the injected task spawner, which applies
	// the sync-wait window — inline settle if it stabilizes within the window,
	// otherwise an ack while it is tracked in the background. Absent a spawner
	// (standalone use / no task layer) fall back to a synchronous wait that
	// preserves the original blocking semantics.
	if spawner, ok := task.TaskSpawnerFromContext(ctx); ok {
		res := spawner.Spawn(task.TaskSpec{
			Kind:     "command",
			Desc:     args.Command,
			Key:      args.Command,
			Relaunch: ct.relaunchClosure(spawner, args),
			ResumeFn: ct.resumeClosure(sessionID, args.IsTUI, detector),
		}, detector)
		if res.Blocked != "" {
			// 5.4（design-report-closeout）：disk degraded 禁新 spawn——拒绝以 result
			// 渗透（可读原因，模型可稍后重试或改同步小命令）。
			return ct.buildBlockedResult(res.Blocked), nil
		}
		if res.Settled {
			return ct.buildResultFromSignal(sessionID, args.Command, args.IsTUI, res.Signal), nil
		}
		return ct.buildAckResult(sessionID, args.Command, res.Task), nil
	}

	// Synchronous fallback: block until the first settle or ctx cancellation.
	// On cancellation the session keeps running under monitor control.
	select {
	case sig, ok := <-detector.Settled():
		if !ok {
			return nil, fmt.Errorf("action: session %s ended without settling", sessionID)
		}
		return ct.buildResultFromSignal(sessionID, args.Command, args.IsTUI, sig), nil
	case <-ctx.Done():
		log.Warnf("[ActionTool] ctx cancelled while waiting for session %s: %v", sessionID, ctx.Err())
		return nil, ctx.Err()
	}
}

// startSession creates a tmux session for args, wires a per-session settle
// detector (whose Cancel kills the session), registers it with the monitor via
// a per-session callback, and ensures the monitor is running. Shared by Call
// and the relaunch closure.
func (ct *ActionTool) startSession(ctx context.Context, args ActionArgs) (string, *TmuxSettleDetector, error) {
	if (args.Mode == string(ModeResident) || args.Mode == string(ModeInteractive)) && !ct.CanSpawnResident() {
		return "", nil, fmt.Errorf("action: resident session cap (%d) reached; stop an existing resident (op=stop) before spawning another", maxResidentSessions)
	}
	session, err := ct.tmuxExecutor.CreateSession(ctx, TmuxCreateOptions{
		Command: args.Command,
		WorkDir: args.WorkDir,
		Env:     args.Env,
		Mode:    SessionMode(args.Mode),
		Name:    args.Name,
	})
	if err != nil {
		// Do not re-wrap the "failed to create tmux session" prefix (CreateSession
		// already carries it plus the captured stderr); just scope it to action.
		return "", nil, fmt.Errorf("action: %w", err)
	}
	sessionID := session.ID
	detector := NewTmuxSettleDetector(sessionID, func() {
		if err := ct.tmuxExecutor.KillSession(sessionID); err != nil {
			log.Warnf("[ActionTool] kill session %s: %v", sessionID, err)
		}
		ct.tmuxMonitor.RemoveSession(sessionID)
		ct.removeResidentMeta(sessionID)
	}) // QuietTimeout >0 only: zero value must stay zero so the monitor falls
	// back to its global default. relaunch reuses args, so the override
	// semantics carry over to relaunched sessions automatically.
	if args.Watch != "" {
		if err := detector.SetWatch(args.Watch, 5*time.Second); err != nil {
			return "", nil, err
		}
	}
	if args.Probe != "" {
		ct.startProbeLoop(sessionID, args, detector)
	}
	ct.saveResidentMeta(sessionID, args)
	var quietTimeout time.Duration
	if args.QuietTimeout > 0 {
		quietTimeout = time.Duration(args.QuietTimeout) * time.Second
	}
	ct.tmuxMonitor.AddSessionWithCallback(&TmuxSession{
		ID:           sessionID,
		Name:         session.Name,
		Command:      args.Command,
		WorkDir:      args.WorkDir,
		Status:       SessionRunning,
		CreatedAt:    time.Now(),
		IsTUI:        args.IsTUI,
		Mode:         SessionMode(args.Mode),
		QuietTimeout: quietTimeout,
	}, func(_ string, _, newStatus SessionStatus, output string) {
		detector.OnWatchOutput(output) // C1: pattern watch on every refresh
		detector.OnStateChange(newStatus, output)
	})
	if !ct.tmuxMonitor.IsRunning() {
		ct.tmuxMonitor.Start()
	}
	return sessionID, detector, nil
}

// relaunchClosure returns a closure that re-runs args as a fresh command task
// (used by relaunch(id)). It starts a new session in a background context (the
// original turn ctx may be gone) and re-spawns via the same task spawner; the
// re-spawned task is itself relaunchable.
func (ct *ActionTool) relaunchClosure(spawner task.TaskSpawner, args ActionArgs) func() (task.SpawnResult, error) {
	return func() (task.SpawnResult, error) {
		sessionID, detector, err := ct.startSession(context.Background(), args)
		if err != nil {
			return task.SpawnResult{}, err
		}
		return spawner.Spawn(task.TaskSpec{
			Kind:     "command",
			Desc:     args.Command,
			Key:      args.Command,
			Relaunch: ct.relaunchClosure(spawner, args),
			ResumeFn: ct.resumeClosure(sessionID, args.IsTUI, detector),
		}, detector), nil
	}
}

// resumeClosure returns the tmux-specific resume implementation: feed input
// into the LIVE session via SendKeys. The detector is bound to the session
// (not the round) — resume just Rearms it (new output baseline + fresh dense
// window) and returns the SAME detector; the monitor callback and the task
// watch never change hands, so there is no rebinding, no ordering discipline,
// and no stale-signal risk. TUI sessions refuse resume (send-keys would
// corrupt the screen). Returned to the task layer as TaskSpec.ResumeFn.
func (ct *ActionTool) resumeClosure(sessionID string, isTUI bool, detector *TmuxSettleDetector) func(string) (task.SettleDetector, error) {
	return func(input string) (task.SettleDetector, error) {
		if isTUI {
			return nil, fmt.Errorf("session %s is a TUI — resume (send-keys) would corrupt the screen; use cancel + a fresh call instead", sessionID)
		}
		// Re-enter dense polling for the resumed round; also verifies the
		// session is still monitored (a dead session was reaped → relaunch).
		if !ct.tmuxMonitor.TouchSession(sessionID) {
			return nil, fmt.Errorf("session %s is no longer monitored — use relaunch_task instead", sessionID)
		}
		// Baseline before send: this round's settle output = capture minus
		// the baseline line count (a shifted scrollback degrades to the full
		// capture rather than losing output — see trimToLineOffset).
		baseline := 0
		if out, err := ct.tmuxExecutor.GetSessionOutput(sessionID); err == nil {
			baseline = strings.Count(out, "\n")
		}
		detector.Rearm(baseline)
		if err := ct.tmuxExecutor.SendKeys(sessionID, input+"\n"); err != nil {
			return nil, fmt.Errorf("send to session %s failed (session may be gone): %w", sessionID, err)
		}
		return detector, nil
	}
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
	note := "命令已在后台运行，稳定或完成后会自动回写结果；你现在可以给出简短回复并结束本回合，无需等待或轮询。"
	if task != nil {
		note = fmt.Sprintf("命令已在后台运行 (task %s)，稳定或完成后会自动回写结果；你现在可以给出简短回复并结束本回合，无需等待或轮询。", task.ID)
	}
	return &ActionToolResult{
		SessionID: sessionID,
		Command:   command,
		Status:    "running",
		Note:      note,
	}
}

// buildBlockedResult (5.4, design-report-closeout; §8.1 wording fix) renders a
// spawn rejection (disk degraded) as a readable tool result — failure permeates
// as result, never as error. NOTE: the command session was ALREADY started by
// startSession before Spawn — the honest wording says "executed but unmanaged"
// (the detector is cancelled by Spawn's gate branch, so there is no orphan
// watcher; the tmux session itself keeps running untracked until the
// session-reclaim sweep).
func (ct *ActionTool) buildBlockedResult(reason string) *ActionToolResult {
	return &ActionToolResult{
		Status: "blocked",
		Note: "命令已执行但未被任务层纳管（结果不会被自动跟踪/回写）：" + reason +
			"。如需结果请稍后用 exec 重新以同步方式确认，或等 disk 恢复后重发。",
	}
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
				// Truncate to last 2000 chars for the LLM view.
				output = "..." + output[len(output)-2000:]
			}
		}
	}

	return &ActionToolResult{
		SessionID:  sessionID,
		Command:    command,
		Status:     settleToStatus(sig),
		Output:     output,
		OutputFile: outputFile,
		Note:       extraNote,
	}
}

// ==================== Data Structures ====================

// ActionArgs represents a command execution request.
type ActionArgs struct {
	Command string            `json:"command"`
	Timeout int               `json:"timeout,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	IsTUI   bool              `json:"is_tui,omitempty"` // Hint that this is a TUI application (different monitor strategy)
	// QuietTimeout overrides the fake-dead detection threshold for this session
	// (seconds). Silent-but-legal tasks (long downloads, compiles, inference
	// waits) produce no output while working; the default 150s threshold kills
	// them. 0 = use global default. Values < StableDuration are rejected in Call.
	QuietTimeout int `json:"quiet_timeout,omitempty"`
	// Mode selects session liveness semantics (2026-09-11 B1):
	//   "oneshot" (default) — command; settles on real exit; 60s quiet reports
	//   stable (never kills).
	//   "resident" — long-lived service (dev server / tunnel / trainer);
	//   silence is healthy, no auto-kill, only unexpected death settles.
	//   "interactive" — long conversational session (REPL); stable settle +
	//   resume supported.
	Mode string `json:"mode,omitempty"`
	// Name (2026-09-11 B2): deterministic logical name for the session
	// ([a-zA-Z0-9-]{1,64}). The session becomes addressable by this name in
	// later calls (restart/exists checks); duplicate spawn under the same
	// name is refused. Empty = auto-generated unique name (legacy).
	Name string `json:"name,omitempty"`
	// Session-operations (2026-09-11 B3). When Op != "" the call addresses an
	// existing session instead of spawning a new one:
	//   Op="peek" — read incremental output since the last peek cursor
	//   (tail=N caps lines; ansi=true keeps escape sequences).
	//   Op="send" — inject Keys into the session (append Enter unless
	//   enter=false); the session must be non-TUI.
	//   Op="stop" — graceful stop: SIGTERM the pane process (fallback
	//   kill-session), grace seconds then SIGKILL.
	// Target: session_id (exact) or name (logical name → n-<name>).
	Op        string `json:"op,omitempty"`
	Keys      string `json:"keys,omitempty"`
	Enter     *bool  `json:"enter,omitempty"`
	Tail      int    `json:"tail,omitempty"`
	Ansi      bool   `json:"ansi,omitempty"`
	GraceSec  int    `json:"grace,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	// Watch (2026-09-11 C1): pattern-triggered wakeups. While the session
	// runs, output matching this regex wakes the agent (merged within a
	// 5s window; cumulative hit count reported). Primary use: resident
	// sessions (watch "ERROR|panic|OOM" on a dev server / trainer log).
	Watch string `json:"watch,omitempty"`
	// Probe (2026-09-11 C2): liveness check for resident sessions. A shell
	// command run every ProbeIntervalSec (default 30); after ProbeFailures
	// (default 3) consecutive failures the agent is woken once. Example:
	// "curl -sf http://localhost:8080/healthz".
	Probe            string `json:"probe,omitempty"`
	ProbeIntervalSec int    `json:"probe_interval,omitempty"`
	ProbeFailures    int    `json:"probe_failures,omitempty"`
}

// validSessionName validates a caller-supplied logical session name (B2).
// Empty is legal (legacy auto-naming). Returns nil when valid.
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
	Command    string `json:"command"`
	OldStatus  string `json:"old_status,omitempty"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	OutputFile string `json:"output_file,omitempty"`
	Note       string `json:"note,omitempty"`
}

// IsTmuxAvailable checks if tmux is available on the system.
func IsTmuxAvailable() bool {
	// Simple check: try to find tmux binary
	return NewTmuxExecutor() != nil
}

// cleanTmuxOutput strips trailing blank lines from tmux capture-pane output.
// The -S -1000 option captures full scrollback which often includes many blank
// lines after short commands, wasting token budget. We keep leading/trailing
// content (including "Pane is dead" messages) but collapse consecutive blank
// lines in the middle and strip trailing blanks.
func cleanTmuxOutput(output string) string {
	lines := strings.Split(output, "\n")

	// Strip trailing blank lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	// Collapse consecutive blank lines in the middle (keep at most 1)
	var result []string
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank {
			continue // skip consecutive blank lines beyond the first
		}
		result = append(result, line)
		prevBlank = isBlank
	}

	return strings.Join(result, "\n")
}

// ---- Session operations (2026-09-11 B3): peek / send / stop ----

// ansiEscape matches ANSI/VT escape sequences (CSI, OSC, simple two-byte).
var ansiEscape = regexp.MustCompile("\\x1b(?:\\[[0-9;?]*[a-zA-Z]|\\][^\\x07]*(?:\\x07|\\x1b\\\\)|[@-Z\\\\-_])")

// stripANSI removes ANSI escape sequences for LLM-friendly output.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// peekCursor tracks the byte offset each session's incremental peek has
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

// pipeRotateBytes is the pipe-log rotation threshold (2026-09-11 D3). Var,
// not const, so tests can shrink it. Long-resident sessions pipe megabytes
// of log; without rotation both peek reads and inode size grow unbounded.
var pipeRotateBytes int64 = 10 << 20 // 10MB

// maybeRotatePipe opportunistically rotates an overgrown pipe log on peek.
// COPYTRUNCATE semantics: copy current content to pf+".1", then truncate the
// live file. pipe-pane's O_APPEND fd keeps appending from offset 0 after
// truncate — no re-attach, no lost writes (a rename would leave the fd
// writing into the rotated-away inode). peek's existing "len < cursor →
// reset" rule transparently adapts to the truncation.
func (ct *ActionTool) maybeRotatePipe(target string) {
	rotatePipeFile(ct.tmuxExecutor.PipeFileFor(target))
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
		return // keep the live file; retry on next peek
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
		return ct.opSend(args, target)
	case "stop":
		return ct.opStop(args, target)
	default:
		return nil, fmt.Errorf("action: unknown op %q (want peek | send | stop)", args.Op)
	}
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
			// No pipe log: session predates pipe attach or died cleaned-up.
			return map[string]any{
				"session_id": target, "status": "no_output_log",
				"note": "pipe log missing — session may have exited and been archived; use a fresh spawn or check archived pipes",
			}, nil
		}
		return nil, fmt.Errorf("action: peek %s: %w", target, err)
	}

	from := ct.peeks.get(target)
	if int64(len(b)) < from {
		// Log was truncated/rotated underneath us — reset to whole file.
		from = 0
	}
	fresh := b[from:]
	ct.peeks.set(target, int64(len(b)))

	// Trim ONE trailing newline so split doesn't produce a phantom empty
	// last element (which would eat into the tail=N budget and surface as
	// a stray blank line in the output).
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
	if err := ct.tmuxExecutor.SendKeys(target, keys); err != nil {
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
	grace := 5
	if args.GraceSec > 0 {
		grace = args.GraceSec
	}
	// Graceful phase: SIGTERM the pane process (if resolvable).
	if pid, err := ct.tmuxExecutor.GetSessionPIDPublic(target); err == nil && pid > 0 {
		if p, findErr := os.FindProcess(pid); findErr == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
		deadline := time.Now().Add(time.Duration(grace) * time.Second)
		for time.Now().Before(deadline) {
			if !ct.tmuxExecutor.SessionExists(target) {
				return map[string]any{"session_id": target, "status": "stopped", "how": "sigterm"}, nil
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	// Hard phase: kill the whole session.
	if err := ct.tmuxExecutor.KillSession(target); err != nil {
		return nil, fmt.Errorf("action: stop %s: %w", target, err)
	}
	return map[string]any{"session_id": target, "status": "stopped", "how": "kill-session"}, nil
}

// startProbeLoop launches the background liveness prober for a resident
// session (2026-09-11 C2). Runs args.Probe via sh every interval; after
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
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		consec := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				probeCtx, cancel := context.WithTimeout(context.Background(), interval/2)
				out, err := ct.probeOnce(probeCtx, args.Probe)
				cancel()
				if err == nil {
					consec = 0
					detector.EmitProbeResult(true, "")
					continue
				}
				consec++
				log.Warnf("[ActionTool] probe %s failed (%d/%d): %v%s", sessionID, consec, failures, err, truncateForLog(out, 120))
				if consec >= failures {
					detector.EmitProbeResult(false, fmt.Sprintf("%d consecutive failures, last: %v", consec, err))
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

func truncateForLog(s string, n int) string {
	if len(s) > n {
		return " out=" + s[:n] + "…"
	}
	if s != "" {
		return " out=" + s
	}
	return ""
}

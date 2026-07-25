package action

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
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

	if args.Command == "" {
		return nil, fmt.Errorf("action: command is required")
	}

	if ct.tmuxExecutor == nil || ct.tmuxMonitor == nil {
		return nil, fmt.Errorf("action: tmux not available (install: brew install tmux)")
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
	session, err := ct.tmuxExecutor.CreateSession(ctx, TmuxCreateOptions{
		Command: args.Command,
		WorkDir: args.WorkDir,
		Env:     args.Env,
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
	})
	ct.tmuxMonitor.AddSessionWithCallback(&TmuxSession{
		ID:        sessionID,
		Name:      session.Name,
		Command:   args.Command,
		WorkDir:   args.WorkDir,
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     args.IsTUI,
	}, func(_ string, _, newStatus SessionStatus, output string) {
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
	note := "命令已在后台运行，稳定或完成后将回写结果；可用任务工具查询状态/结果。"
	if task != nil {
		note = fmt.Sprintf("命令已在后台运行 (task %s)，稳定或完成后将回写结果；可用任务工具查询状态/结果。", task.ID)
	}
	return &ActionToolResult{
		SessionID: sessionID,
		Command:   command,
		Status:    "running",
		Note:      note,
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

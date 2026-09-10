package action

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// TmuxExecutor manages tmux sessions for async command execution.
type TmuxExecutor struct {
	prefix     string
	workspace  string
	runAsUser  string
	runAsGroup string
}

// TmuxExecutorOption configures TmuxExecutor
type TmuxExecutorOption func(*TmuxExecutor)

// WithTmuxPrefix sets the session name prefix
func WithTmuxPrefix(prefix string) TmuxExecutorOption {
	return func(te *TmuxExecutor) {
		te.prefix = prefix
	}
}

// WithTmuxWorkspace sets the workspace directory
func WithTmuxWorkspace(dir string) TmuxExecutorOption {
	return func(te *TmuxExecutor) {
		te.workspace = dir
	}
}

// WithTmuxRunAsUser sets the user to run commands as
func WithTmuxRunAsUser(user string) TmuxExecutorOption {
	return func(te *TmuxExecutor) {
		te.runAsUser = user
	}
}

// WithTmuxRunAsGroup sets the group to run commands as
func WithTmuxRunAsGroup(group string) TmuxExecutorOption {
	return func(te *TmuxExecutor) {
		te.runAsGroup = group
	}
}

// NewTmuxExecutor creates a new tmux executor
func NewTmuxExecutor(opts ...TmuxExecutorOption) *TmuxExecutor {
	te := &TmuxExecutor{
		prefix: "tagent",
	}

	for _, opt := range opts {
		opt(te)
	}

	return te
}

// buildTmuxCommand constructs a tmux command with optional sudo wrapping.
// When runAsUser is set, all tmux commands are wrapped with:
//
//	sudo -n -u <user> [-g <group>] tmux <args...>
//
// This ensures the tmux server and all sessions run as the restricted user,
// providing OS-level user isolation instead of sandboxing.
func (te *TmuxExecutor) buildTmuxCommand(args []string) (string, []string) {
	if te.runAsUser != "" {
		sudoArgs := []string{"-n", "-u", te.runAsUser}
		if te.runAsGroup != "" {
			sudoArgs = append(sudoArgs, "-g", te.runAsGroup)
		}
		sudoArgs = append(sudoArgs, "tmux")
		sudoArgs = append(sudoArgs, args...)
		return "sudo", sudoArgs
	}
	return "tmux", args
}

// setSessionEnv sets environment variables on a tmux session via set-environment.
func (te *TmuxExecutor) setSessionEnv(ctx context.Context, sessionName string, env map[string]string) {
	for k, v := range env {
		envArgs := []string{"set-environment", "-t", sessionName, k, v}
		envCmdName, envCmdArgs := te.buildTmuxCommand(envArgs)
		var envCmd *exec.Cmd
		if ctx != nil {
			envCmd = exec.CommandContext(ctx, envCmdName, envCmdArgs...)
		} else {
			envCmd = exec.Command(envCmdName, envCmdArgs...)
		}
		if envErr := envCmd.Run(); envErr != nil {
			log.Warnf("[TmuxExecutor] failed to set env %s on session %s: %v", k, sessionName, envErr)
		}
	}
}

// TmuxSession represents a tmux session
type TmuxSession struct {
	ID            string
	Name          string
	Command       string
	WorkDir       string
	Status        SessionStatus
	CreatedAt     time.Time
	LastOutput    string
	LastOutputMD5 string
	StableSince   time.Time // When output first became unchanged (zero if output changed in last check).
	// Used as the sole stability indicator: elapsed duration determines
	// Stable / fakeDead thresholds, replacing count-based detection.
	IsInteractive bool
	IsTUI         bool // TUI apps skip heartbeat (send-keys injection) at fakeDead threshold
	// Mode selects the liveness interpretation (zero value = ModeOneshot).
	// See SessionMode docs. Derived: IsTUI → ModeInteractive-like handling
	// remains via IsTUI checks; Mode only extends, never overrides IsTUI.
	Mode SessionMode
	// QuietTimeout overrides the global fakeDeadDuration for THIS session
	// (silent-but-legal tasks: long downloads, compiles, inference waits).
	// Zero value = fall back to the monitor's global default (150s).
	QuietTimeout time.Duration
	PID          int
	// PipeFile is the streaming output log attached via tmux pipe-pane at
	// session creation. It records every pty byte as it arrives, so it stays
	// COMPLETE even after the pane dies (unlike capture-pane, which reads the
	// pane grid and can freeze a stale truncation frame on dead panes).
	PipeFile       string
	KillRetryCount int // Number of failed KillSession attempts (used by handleFakeDead retry logic)
}

// SessionStatus represents the state of a tmux session
type SessionStatus string

const (
	SessionRunning   SessionStatus = "running"
	SessionStable    SessionStatus = "stable"
	SessionCompleted SessionStatus = "completed"
	SessionError     SessionStatus = "error"
	SessionFakeDead  SessionStatus = "fake_dead"
	SessionFakeAlive SessionStatus = "fake_alive"
	SessionTimedOut  SessionStatus = "timed_out" // TUI session exceeded fakeDeadDuration without output change
)

// SessionMode classifies how a session's liveness should be interpreted by the
// monitor and the settle stream (2026-09-11 async-action overhaul, B1).
//
//	ModeOneshot     (default) command semantics: settle on exit; a
//	                60s-quiet alive session reports Stable (never Completed —
//	                see detectSessionState), and quiet_timeout (if set) is a
//	                hard kill deadline.
//	ModeResident    long-lived services (dev servers, tunnels, training):
//	                silence is HEALTHY — no stable settle, no fake-dead kill,
//	                no auto-reap. Only unexpected death (Completed/Error)
//	                settles. Pair with watch/probe to hear from it.
//	ModeInteractive long-running conversational sessions (REPL, coding
//	                agents): stable settle + resume/send-keys semantics,
//	                heartbeat-based fake-dead detection (unchanged legacy).
type SessionMode string

const (
	ModeOneshot     SessionMode = "oneshot"
	ModeResident    SessionMode = "resident"
	ModeInteractive SessionMode = "interactive"
)

// TmuxCreateOptions defines how to create a tmux session
type TmuxCreateOptions struct {
	Command       string
	WorkDir       string
	IsInteractive bool
	// Mode selects the liveness interpretation (zero value = ModeOneshot).
	Mode SessionMode
	Env  map[string]string
	// Name (2026-09-11 B2): request a deterministic session name instead of
	// the generated prefix-timestamp. Empty = auto-generate (legacy). Non-empty
	// names must be DNS-label-safe ([a-zA-Z0-9-]{1,64}, enforced in Call) and
	// are prefixed to avoid colliding with generated names. Use-case: named
	// resident/interactive services so later calls can address them
	// (exists/restart/send) without keeping a session-id ticket.
	Name string
}

// CreateSession creates a new tmux session with the command
func (te *TmuxExecutor) CreateSession(ctx context.Context, opts TmuxCreateOptions) (*TmuxSession, error) {
	// Session name: caller-requested deterministic name (B2) or generated.
	sessionName := fmt.Sprintf("%s-%d", te.prefix, time.Now().UnixNano())
	if opts.Name != "" {
		sessionName = NamedSessionName(opts.Name)
		// Duplicate protection: a named session must be explicitly restarted
		// or stopped, never silently double-spawned (two dev servers on one
		// port is the classic footgun this guard exists for).
		if te.SessionExists(sessionName) {
			return nil, fmt.Errorf("action: session %q already exists (stop it first or use resume); duplicate spawn refused", sessionName)
		}
	}

	// Build tmux command.
	// Use tmux's ';' separator to set remain-on-exit inline during session
	// creation. This is critical: if the command exits quickly (e.g., `exit 42`),
	// a separate `set-option` call after new-session would fail because the
	// session/pane is already gone. Setting it inline ensures the pane persists
	// regardless of how fast the command completes.
	args := []string{
		"new-session",
		"-d", // detached
		"-s", sessionName,
	}

	// Set working directory
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = te.workspace
	}
	if workDir != "" {
		// Ensure the working directory exists — tmux new-session with `-c` to a
		// non-existent directory fails with a bare "exit status 1". Creating it
		// (best-effort) removes a common, hard-to-diagnose failure mode.
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			log.Warnf("[tmux] workDir %q ensure failed (continuing): %v", workDir, err)
		}
		args = append(args, "-c", workDir)
	}

	// Add command
	args = append(args, opts.Command)

	// Inline remain-on-exit: set immediately after the command starts so the
	// pane persists after the command exits. Using ";" (tmux command separator)
	// ensures this runs atomically with session creation.
	args = append(args, ";", "set-option", "remain-on-exit", "on")

	// Inline pipe-pane attach: same rationale as remain-on-exit -- the three
	// tmux commands execute back-to-back inside the tmux server, shrinking the
	// mount race from a cross-process RTT to <1ms. The pipe log records raw
	// pty bytes as they arrive and stays complete after pane death (the pane
	// grid can freeze a stale truncation frame on tmux 3.4). Worst case if a
	// hyper-fast command still outruns the mount: the log is missing the
	// HEAD of the stream, never the tail -- and consumers assert on tails.
	pipeFile := te.pipeFilePath(sessionName)
	os.WriteFile(pipeFile, nil, 0o600) // pre-create so fallback checks are deterministic
	args = append(args, ";", "pipe-pane", "-o", "-t", sessionName, "cat >> "+pipeFile)

	cmdName, cmdArgs := te.buildTmuxCommand(args)
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// The combined "new-session ; set-option" invocation returns the exit
		// code of the LAST command, so a trailing set-option that fails on some
		// tmux versions (e.g. remain-on-exit option scope) surfaces as a generic
		// failure even though the session was created. Only treat it as fatal
		// when the session does not actually exist; otherwise proceed and log
		// the captured stderr so the real cause is never hidden as "exit status 1".
		detail := strings.TrimSpace(stderr.String())
		if !te.SessionExists(sessionName) {
			return nil, fmt.Errorf("failed to create tmux session: %w: %s", err, detail)
		}
		log.Warnf("[tmux] session %s created, but a post-create option failed (non-fatal): %v: %s",
			sessionName, err, detail)
	}

	// Set environment variables on the session
	te.setSessionEnv(ctx, sessionName, opts.Env)

	// Get session PID
	pid, err := te.getSessionPID(sessionName)
	if err != nil {
		pid = 0 // Non-fatal
	}

	session := &TmuxSession{
		ID:            sessionName,
		Name:          sessionName,
		Command:       opts.Command,
		WorkDir:       workDir,
		Status:        SessionRunning,
		CreatedAt:     time.Now(),
		IsInteractive: opts.IsInteractive,
		PID:           pid,
		PipeFile:      pipeFile,
	}

	return session, nil
}

// KillSession kills a tmux session
func (te *TmuxExecutor) KillSession(sessionID string) error {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"kill-session", "-t", sessionID})
	cmd := exec.Command(cmdName, cmdArgs...)
	err := cmd.Run()
	// (2026-09-11 async-action overhaul, A3) Preserve the forensic pipe log
	// instead of deleting it: it is the only complete record of what the
	// session actually printed (capture-pane freezes a stale truncation frame
	// on dead panes). Archive under the tool-output dir; the workspace cleaner
	// bounds growth. A missing pipe file (never attached) is fine.
	if pf := te.pipeFilePath(sessionID); true {
		if _, statErr := os.Stat(pf); statErr == nil {
			dest := filepath.Join(os.TempDir(), "tagent-archived-pipes")
			if mkErr := os.MkdirAll(dest, 0o755); mkErr == nil {
				archived := filepath.Join(dest, filepath.Base(pf))
				if renErr := os.Rename(pf, archived); renErr != nil {
					log.Warnf("[tmux] archive pipe log %s failed: %v", pf, renErr)
					os.Remove(pf) // fall back to the old delete semantics
				}
			} else {
				log.Warnf("[tmux] archive dir %s create failed: %v", dest, mkErr)
				os.Remove(pf)
			}
		}
	}
	return err
}

// SessionExists checks if a tmux session exists
func (te *TmuxExecutor) SessionExists(sessionID string) bool {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"has-session", "-t", sessionID})
	cmd := exec.Command(cmdName, cmdArgs...)
	return cmd.Run() == nil
}

// GetSessionOutput gets the current output of a tmux session
// pipeFilePath returns the conventional streaming-log path for a session.
func (te *TmuxExecutor) pipeFilePath(sessionID string) string {
	return filepath.Join(os.TempDir(), "tagent-pipe-"+sessionID+".log")
}

// PipeFileFor exposes the streaming-log path for a session (B3 peek).
func (te *TmuxExecutor) PipeFileFor(sessionID string) string {
	return te.pipeFilePath(sessionID)
}

// GetSessionPIDPublic exposes the pane process PID lookup (B3 stop).
func (te *TmuxExecutor) GetSessionPIDPublic(sessionID string) (int, error) {
	return te.getSessionPID(sessionID)
}

// NamedSessionName maps a caller-supplied logical name to the deterministic
// tmux session name (2026-09-11 B2). The "n-" prefix segment keeps named
// sessions visually and syntactically distinct from generated
// prefix-timestamp names. Callers validate the logical name first
// (validSessionName in action_tool.go); this function is the single place
// that knows the naming convention.
func NamedSessionName(logical string) string {
	return "n-" + logical
}

func (te *TmuxExecutor) GetSessionOutput(sessionID string) (string, error) {
	// Prefer the pipe-pane streaming log: it records raw pty bytes as they
	// arrive and remains complete after pane death. capture-pane reads the
	// pane grid, which on a dead pane can be frozen at a stale truncation
	// frame (the root cause of missing END_MARKER tails).
	pipeFile := te.pipeFilePath(sessionID)
	if b, err := os.ReadFile(pipeFile); err == nil && len(b) > 0 {
		return string(b), nil
	}
	// Fallback: capture-pane (sessions created before this change, or when
	// pipe-pane attach failed at creation).
	// Use -S -1000 to capture full scrollback history, not just visible pane.
	// This ensures we get output from commands that finished quickly and
	// whose output may have scrolled past the visible area (especially when
	// tmux appends "Pane is dead" messages after remain-on-exit).
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"capture-pane", "-p", "-S", "-1000", "-t", sessionID})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return stdout.String(), nil
}

// IsPaneDead checks if the tmux pane is dead
func (te *TmuxExecutor) IsPaneDead(sessionID string) bool {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"display-message", "-p", "-t", sessionID, "#{pane_dead}"})
	cmd := exec.Command(cmdName, cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return true // Assume dead if can't check
	}

	return strings.TrimSpace(stdout.String()) == "1"
}

// ProcessExists checks if the main process of a tmux session is still running
func (te *TmuxExecutor) ProcessExists(sessionID string) bool {
	// Get pane PID
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"display-message", "-p", "-t", sessionID, "#{pane_pid}"})
	cmd := exec.Command(cmdName, cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return false
	}

	pidStr := strings.TrimSpace(stdout.String())
	if pidStr == "" || pidStr == "0" {
		return false
	}

	// Check if process exists using kill -0
	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
		return false
	}

	// kill -0 checks if process exists without sending a signal
	killCmd := exec.Command("kill", "-0", fmt.Sprintf("%d", pid))
	return killCmd.Run() == nil
}

// SendKeys sends keys to a tmux session (for interactive commands)
func (te *TmuxExecutor) SendKeys(sessionID string, keys string) error {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"send-keys", "-t", sessionID, keys})
	cmd := exec.Command(cmdName, cmdArgs...)
	return cmd.Run()
}

// SendHeartbeat sends a heartbeat command to detect if session is alive
func (te *TmuxExecutor) SendHeartbeat(sessionID string) string {
	// Send echo command and check response
	err := te.SendKeys(sessionID, "echo tmux_heartbeat\n")
	if err != nil {
		return "error"
	}

	// Wait briefly for output
	time.Sleep(500 * time.Millisecond)

	output, err := te.GetSessionOutput(sessionID)
	if err != nil {
		return "error"
	}

	if strings.Contains(output, "tmux_heartbeat") {
		return "ok"
	}

	return "no_response"
}

// ListSessions lists all tmux sessions with our prefix
func (te *TmuxExecutor) ListSessions() ([]*TmuxSession, error) {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"list-sessions", "-F", "#{session_name}"})
	cmd := exec.Command(cmdName, cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	var sessions []*TmuxSession
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Only include sessions with our prefix
		if strings.HasPrefix(line, te.prefix) {
			sessions = append(sessions, &TmuxSession{
				ID:   line,
				Name: line,
			})
		}
	}

	return sessions, nil
}

// CleanupOrphanSessions kills all prefix-matched tmux sessions. Called at
// startup: sessions from a previous (crashed or stopped) instance have no
// monitor watching them — they would never be reaped and each holds a pty
// (system-wide pty exhaustion was observed in the field). Best effort: a
// missing tmux server means nothing to clean. Returns the number killed.
func (te *TmuxExecutor) CleanupOrphanSessions() int {
	sessions, err := te.ListSessions()
	if err != nil {
		return 0 // no server / no sessions — nothing to clean
	}
	killed := 0
	for _, s := range sessions {
		if err := te.KillSession(s.ID); err != nil {
			log.Warnf("[TmuxExecutor] orphan cleanup: kill %s failed: %v", s.ID, err)
			continue
		}
		killed++
	}
	if killed > 0 {
		log.Infof("[TmuxExecutor] orphan cleanup: killed %d leftover session(s) with prefix %q", killed, te.prefix)
	}
	return killed
}

// getSessionPID gets the PID of the tmux session's main process
func (te *TmuxExecutor) getSessionPID(sessionID string) (int, error) {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"display-message", "-p", "-t", sessionID, "#{pane_pid}"})
	cmd := exec.Command(cmdName, cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return 0, err
	}

	var pid int
	_, err = fmt.Sscanf(strings.TrimSpace(stdout.String()), "%d", &pid)
	if err != nil {
		return 0, err
	}

	return pid, nil
}

// RestartSession attempts to restart a tmux session under the SAME session name.
// This ensures the restarted session continues to be tracked by TmuxMonitor
// under its original ID — no state chain breakage.
func (te *TmuxExecutor) RestartSession(sessionID string, opts TmuxCreateOptions) error {
	// Kill existing session (best-effort: the session may already be dead)
	te.KillSession(sessionID)

	// Re-create under the SAME session name so the monitor keeps tracking it.
	args := []string{"new-session", "-d", "-s", sessionID}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir = te.workspace
	}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}

	args = append(args, opts.Command)

	// Inline remain-on-exit so a quickly exiting command does not destroy the
	// pane before TmuxMonitor can capture its output. This mirrors CreateSession.
	args = append(args, ";", "set-option", "remain-on-exit", "on")

	cmdName, cmdArgs := te.buildTmuxCommand(args)
	cmd := exec.Command(cmdName, cmdArgs...)
	if err := cmd.Run(); err != nil {
		return err
	}

	// Set environment variables on the restarted session
	te.setSessionEnv(nil, sessionID, opts.Env)

	return nil
}

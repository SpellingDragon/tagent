package action

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	IsInteractive  bool
	IsTUI          bool // TUI apps skip heartbeat (send-keys injection) at fakeDead threshold
	PID            int
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

// TmuxCreateOptions defines how to create a tmux session
type TmuxCreateOptions struct {
	Command       string
	WorkDir       string
	IsInteractive bool
	Env           map[string]string
}

// CreateSession creates a new tmux session with the command
func (te *TmuxExecutor) CreateSession(ctx context.Context, opts TmuxCreateOptions) (*TmuxSession, error) {
	// Generate unique session name
	sessionName := fmt.Sprintf("%s-%d", te.prefix, time.Now().UnixNano())

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
	}

	return session, nil
}

// KillSession kills a tmux session
func (te *TmuxExecutor) KillSession(sessionID string) error {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"kill-session", "-t", sessionID})
	cmd := exec.Command(cmdName, cmdArgs...)
	return cmd.Run()
}

// SessionExists checks if a tmux session exists
func (te *TmuxExecutor) SessionExists(sessionID string) bool {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"has-session", "-t", sessionID})
	cmd := exec.Command(cmdName, cmdArgs...)
	return cmd.Run() == nil
}

// GetSessionOutput gets the current output of a tmux session
func (te *TmuxExecutor) GetSessionOutput(sessionID string) (string, error) {
	// Use -S -1000 to capture full scrollback history, not just visible pane.
	// This ensures we get output from commands that finished quickly and
	// whose output may have scrolled past the visible area (especially when
	// tmux appends "Pane is dead" messages after remain-on-exit).
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"capture-pane", "-p", "-S", "-1000", "-t", sessionID})
	cmd := exec.Command(cmdName, cmdArgs...)
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

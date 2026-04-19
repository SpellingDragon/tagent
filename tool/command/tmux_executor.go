package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// TmuxExecutor manages tmux sessions for async command execution.
type TmuxExecutor struct {
	prefix    string
	workspace string
	runAsUser string
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
	StableCount   int
	IsInteractive bool
	PID           int
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

	// Build tmux command
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
		args = append(args, "-c", workDir)
	}

	// Add command
	args = append(args, opts.Command)

	cmd := exec.CommandContext(ctx, "tmux", args...)
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

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
	cmd := exec.Command("tmux", "kill-session", "-t", sessionID)
	return cmd.Run()
}

// SessionExists checks if a tmux session exists
func (te *TmuxExecutor) SessionExists(sessionID string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", sessionID)
	return cmd.Run() == nil
}

// GetSessionOutput gets the current output of a tmux session
func (te *TmuxExecutor) GetSessionOutput(sessionID string) (string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-p", "-t", sessionID)
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
	cmd := exec.Command("tmux", "display-message", "-p", "-t", sessionID, "#{pane_dead}")
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
	cmd := exec.Command("tmux", "display-message", "-p", "-t", sessionID, "#{pane_pid}")
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
	cmd := exec.Command("tmux", "send-keys", "-t", sessionID, keys)
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
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
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

// getSessionPID gets the PID of the tmux session's main process
func (te *TmuxExecutor) getSessionPID(sessionID string) (int, error) {
	cmd := exec.Command("tmux", "display-message", "-p", "-t", sessionID, "#{pane_pid}")
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

// RestartSession attempts to restart a tmux session
func (te *TmuxExecutor) RestartSession(sessionID string, opts TmuxCreateOptions) error {
	// Kill existing session
	te.KillSession(sessionID)

	// Create new session
	_, err := te.CreateSession(context.Background(), opts)
	return err
}

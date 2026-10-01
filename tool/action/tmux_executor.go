package action

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	StableSince   time.Time
	// IsInteractive Used as the sole stability indicator: elapsed duration determines
	// Stable / fakeDead thresholds, replacing count-based detection.
	IsInteractive bool
	IsTUI         bool
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
	KillRetryCount int
	// ProbeUnknownCount：连续不可辨探测
	//（list-sessions err）计数——加闸达到 ProbeUnknownLimit 才按 dead 处理；
	// 任一可辨探测（alive/dead）清零。会话级字段（非包级），重启清零可接受。
	ProbeUnknownCount int
}

// SessionStatus represents the state of a tmux session
type SessionStatus string

const (
	// SessionRunning 进程存活且输出未达稳定阈值的进行中态；探测不可辨且未达连续上限时也保持该态。
	SessionRunning SessionStatus = "running"
	// SessionStable 输出已稳定但进程存活、且未显式声明静默超时的判定；不视为死亡，按调度上限继续轮询。
	SessionStable SessionStatus = "stable"
	// SessionCompleted 会话已结束的终态：pane 死、命令收尾或探测彻底不可辨，随即移出监控。
	SessionCompleted SessionStatus = "completed"
	// SessionError 探测器未装配（executor 为 nil）时的终态：随即移出监控。
	SessionError SessionStatus = "error"
	// SessionFakeDead 静默越过阈值后的假死判定中间态：仅在显式声明静默超时、或心跳失败且 pane 未死时进入。
	// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
	SessionFakeDead SessionStatus = "fake_dead"
	// SessionFakeAlive 心跳仍有响应的假活判定中间态：以原会话 ID 重启以保持监控链条。
	// 契约: docs/wiki/tool/tmux-action.md#fake-alive-restart
	SessionFakeAlive SessionStatus = "fake_alive"
	// SessionTimedOut TUI 会话静默越过假死阈值后的终态（不做假死/假活探测）：随即移出监控。
	SessionTimedOut SessionStatus = "timed_out"
)

// SessionMode classifies how a session's liveness is interpreted by the monitor
// and the settle stream. Each mode's semantics are documented on its constant.
type SessionMode string

const (
	// ModeOneshot is the default command semantics: settle on exit; a 60s-quiet alive
	// session reports Stable (never Completed — see detectSessionState), and quiet_timeout
	// (if set) is a hard kill deadline.
	ModeOneshot SessionMode = "oneshot"
	// ModeResident is for long-lived services (dev servers, tunnels, training): silence
	// is HEALTHY — no stable settle, no fake-dead kill, no auto-reap. Only unexpected death
	// (Completed/Error) settles; pair with watch/probe to hear from it.
	ModeResident SessionMode = "resident"
	// ModeInteractive is for long-running conversational sessions (REPL, coding agents):
	// stable settle plus resume/send-keys semantics, with heartbeat-based fake-dead
	// detection.
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
	// Name: request a deterministic session name instead of
	// the generated prefix-timestamp. Empty = auto-generated. Non-empty
	// names must be DNS-label-safe ([a-zA-Z0-9-]{1,64}, enforced in Call) and
	// are prefixed to avoid colliding with generated names. Use-case: named
	// resident/interactive services so later calls can address them
	// (exists/restart/send) without keeping a session-id ticket.
	Name string
}

// tmuxCreateRetries bounds the retries applied to a transient tmux server failure
// when creating a session, recovering a cold start or a session-count-dropped-to-
// zero race with a sibling teardown.
const tmuxCreateRetries = 3

// tmuxCreateRetryDelay is the backoff between tmux session-creation retry attempts.
const tmuxCreateRetryDelay = 150 * time.Millisecond

// isTransientTmuxServerFailure reports whether a tmux create failure is a race the
// server recovers from on retry — it died or lost its connection as the session was
// being spawned — rather than a durable error such as a missing binary or a
// duplicate session name.
func isTransientTmuxServerFailure(err error, detail string) bool {
	msg := err.Error() + " " + detail
	return strings.Contains(msg, "server exited unexpectedly") ||
		strings.Contains(msg, "lost server connection") ||
		strings.Contains(msg, "server is not running")
}

// runTmuxCreate executes a tmux new-session invocation, re-issuing it a bounded
// number of times when the server fails transiently. It returns a nil error on
// success or when a retry reveals the session already present (a prior attempt
// created it despite reporting failure); otherwise it returns the last error with
// its stderr so the caller can classify fatal versus post-create-non-fatal. A
// durable failure (duplicate name, missing binary) returns immediately, never
// retried.
func (te *TmuxExecutor) runTmuxCreate(ctx context.Context, cmdName string, cmdArgs []string, sessionName string) (string, error) {
	var runErr error
	var detail string
	for attempt := 0; ; attempt++ {
		cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		runErr = cmd.Run()
		detail = strings.TrimSpace(stderr.String())
		if runErr == nil || !isTransientTmuxServerFailure(runErr, detail) {
			return detail, runErr
		}
		if te.SessionExists(sessionName) {
			return "", nil
		}
		if attempt >= tmuxCreateRetries {
			return detail, runErr
		}
		select {
		case <-ctx.Done():
			return detail, runErr
		case <-time.After(tmuxCreateRetryDelay):
		}
	}
}

// CreateSession creates a new tmux session with the command
//
// 契约: docs/wiki/tool/tmux-action.md#named-session-singleton
func (te *TmuxExecutor) CreateSession(ctx context.Context, opts TmuxCreateOptions) (*TmuxSession, error) {
	sessionName := fmt.Sprintf("%s-%d", te.prefix, time.Now().UnixNano())
	if opts.Name != "" {
		sessionName = NamedSessionName(opts.Name)
		if te.SessionExists(sessionName) {
			return nil, fmt.Errorf("action: session %q already exists (stop it first or use resume); duplicate spawn refused", sessionName)
		}
	}

	args := []string{
		"new-session",
		"-d",
		"-s", sessionName,
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir = te.workspace
	}
	if workDir != "" {
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			log.Warnf("[tmux] workDir %q ensure failed (continuing): %v", workDir, err)
		}
		args = append(args, "-c", workDir)
	}

	args = append(args, opts.Command)

	args = append(args, ";", "set-option", "remain-on-exit", "on")

	pipeFile := te.pipeFilePath(sessionName)
	os.WriteFile(pipeFile, nil, 0o600)
	args = append(args, ";", "pipe-pane", "-o", "-t", sessionName, "cat >> "+pipeFile)

	cmdName, cmdArgs := te.buildTmuxCommand(args)
	if detail, runErr := te.runTmuxCreate(ctx, cmdName, cmdArgs, sessionName); runErr != nil {
		if !te.SessionExists(sessionName) {
			return nil, fmt.Errorf("failed to create tmux session: %w: %s", runErr, detail)
		}
		log.Warnf("[tmux] session %s created, but a post-create option failed (non-fatal): %v: %s",
			sessionName, runErr, detail)
	}

	te.setSessionEnv(ctx, sessionName, opts.Env)

	pid, err := te.getSessionPID(sessionName)
	if err != nil {
		pid = 0
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
//
// 契约: docs/wiki/tool/tmux-action.md#pipe-log
func (te *TmuxExecutor) KillSession(sessionID string) error {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"kill-session", "-t", sessionID})
	cmd := exec.Command(cmdName, cmdArgs...)
	err := cmd.Run()
	if pf := te.pipeFilePath(sessionID); true {
		if _, statErr := os.Stat(pf); statErr == nil {
			dest := filepath.Join(os.TempDir(), "tagent-archived-pipes")
			if mkErr := os.MkdirAll(dest, 0o755); mkErr == nil {
				archived := filepath.Join(dest, filepath.Base(pf))
				if renErr := os.Rename(pf, archived); renErr != nil {
					log.Warnf("[tmux] archive pipe log %s failed: %v", pf, renErr)
					os.Remove(pf)
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

// pipeFilePath GetSessionOutput gets the current output of a tmux session
// pipeFilePath returns the conventional streaming-log path for a session.
func (te *TmuxExecutor) pipeFilePath(sessionID string) string {
	return filepath.Join(os.TempDir(), "tagent-pipe-"+sessionID+".log")
}

// PipeFileFor exposes the streaming-log path for a session.
func (te *TmuxExecutor) PipeFileFor(sessionID string) string {
	return te.pipeFilePath(sessionID)
}

// GetSessionPIDPublic exposes the pane process PID lookup.
func (te *TmuxExecutor) GetSessionPIDPublic(sessionID string) (int, error) {
	return te.getSessionPID(sessionID)
}

// NamedSessionName maps a caller-supplied logical name to the deterministic
// tmux session name. The "n-" prefix segment keeps named
// sessions visually and syntactically distinct from generated
// prefix-timestamp names. Callers validate the logical name first
// (validSessionName in action_tool.go); this function is the single place
// that knows the naming convention.
func NamedSessionName(logical string) string {
	return "n-" + logical
}

// GetSessionOutput 返回该会话当前可见的输出：优先读流式记录文件（pipe），文件缺失或
// 为空时回落到 capture-pane 的最近 1000 行，该回落调用带 3s 超时。
func (te *TmuxExecutor) GetSessionOutput(sessionID string) (string, error) {
	pipeFile := te.pipeFilePath(sessionID)
	if b, err := os.ReadFile(pipeFile); err == nil && len(b) > 0 {
		return string(b), nil
	}
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
		return false
	}

	return strings.TrimSpace(stdout.String()) == "1"
}

// PaneDeadStatus reads the exit status of a dead pane via tmux's
// `#{pane_dead_status}`. It returns (code, known):
//   - pane dead → (exit code, true). tmux reports signal-death as a negative value.
//   - pane still alive, session gone, or command failed → (0, false) — the exit
//     status is not resolvable, which the caller must treat as "unknown", never as
//     success. Framework-created sessions run with remain-on-exit, so a dead pane
//     is retained and its status remains readable at detection time.
//
// 契约: docs/wiki/tool/tmux-action.md#failure-polarity
func (te *TmuxExecutor) PaneDeadStatus(sessionID string) (code int, known bool) {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"display-message", "-p", "-t", sessionID, "#{pane_dead_status}"})
	cmd := exec.Command(cmdName, cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return 0, false
	}
	trimmed := strings.TrimSpace(stdout.String())
	if trimmed == "" {
		return 0, false
	}
	status, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, false
	}
	return status, true
}

// SessionAlive3：三态存活探测——list-sessions
// 单源（会话在列表=活；不在=确定性死；命令 err=不可辨）。known=false 时调用方
// （monitor）计入 ProbeUnknownCount 连续加闸，不立即判死。
func (te *TmuxExecutor) SessionAlive3(sessionID string) (alive, known bool) {
	cmdName, cmdArgs := te.buildTmuxCommand([]string{"list-sessions", "-F", "#{session_name}"})
	cmd := exec.Command(cmdName, cmdArgs...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false, false
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.TrimSpace(line) == sessionID {
			return true, true
		}
	}
	return false, true
}

// ProcessExists checks if the main process of a tmux session is still running
func (te *TmuxExecutor) ProcessExists(sessionID string) bool {
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
	err := te.SendKeys(sessionID, "echo tmux_heartbeat\n")
	if err != nil {
		return "error"
	}

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

		if strings.HasPrefix(line, te.prefix) || strings.HasPrefix(line, "n-") {
			sessions = append(sessions, &TmuxSession{
				ID:   line,
				Name: line,
			})
		}
	}

	return sessions, nil
}

// CleanupOrphanSessions kills prefix-matched generated-name tmux sessions. Called at
// startup: sessions from a previous (crashed or stopped) instance have no
// monitor watching them — they would never be reaped and each holds a pty
// (system-wide pty exhaustion was observed in the field). Best effort: a
// missing tmux server means nothing to clean. Returns the number killed.
//
// R3（orphan 语义重定义）：named 会话被排除——
// cleanup 在装配时先于 reattach 执行，若纳入 named 会话则会屠杀全部常驻会话
// （修复前语义冲突：枚举双条件修复会让 cleanup 杀光 n-）。orphan=仅无主生成名
// 会话；named 会话由 R3 重挂接管或由 ResidentMeta TTL sweep 兑现终局。
func (te *TmuxExecutor) CleanupOrphanSessions() int {
	sessions, err := te.ListSessions()
	if err != nil {
		return 0
	}
	killed := 0
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, "n-") {
			continue
		}
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
	te.KillSession(sessionID)

	args := []string{"new-session", "-d", "-s", sessionID}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir = te.workspace
	}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}

	args = append(args, opts.Command)

	args = append(args, ";", "set-option", "remain-on-exit", "on")

	cmdName, cmdArgs := te.buildTmuxCommand(args)
	cmd := exec.Command(cmdName, cmdArgs...)
	if err := cmd.Run(); err != nil {
		return err
	}

	te.setSessionEnv(context.TODO(), sessionID, opts.Env)

	return nil
}

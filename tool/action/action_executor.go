package action

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ActionExecutor handles secure command execution with isolation.
type ActionExecutor struct {
	workspace      string
	runAsUser      string
	runAsGroup     string
	defaultTimeout time.Duration
}

// ActionExecutorOption configures ActionExecutor
type ActionExecutorOption func(*ActionExecutor)

// WithExecutorWorkspace sets the workspace directory
func WithExecutorWorkspace(dir string) ActionExecutorOption {
	return func(ce *ActionExecutor) {
		ce.workspace = dir
	}
}

// WithExecutorRunAsUser sets the user to run commands as
func WithExecutorRunAsUser(user string) ActionExecutorOption {
	return func(ce *ActionExecutor) {
		ce.runAsUser = user
	}
}

// WithExecutorRunAsGroup sets the group to run commands as
func WithExecutorRunAsGroup(group string) ActionExecutorOption {
	return func(ce *ActionExecutor) {
		ce.runAsGroup = group
	}
}

// WithExecutorDefaultTimeout sets the default timeout
func WithExecutorDefaultTimeout(d time.Duration) ActionExecutorOption {
	return func(ce *ActionExecutor) {
		ce.defaultTimeout = d
	}
}

// NewActionExecutor creates a new action executor
func NewActionExecutor(opts ...ActionExecutorOption) *ActionExecutor {
	ce := &ActionExecutor{
		defaultTimeout: 300 * time.Second, // 5 minutes default
	}

	for _, opt := range opts {
		opt(ce)
	}

	return ce
}

// ActionSpec defines how to execute a command
type ActionSpec struct {
	Command    string
	Args       []string
	Env        map[string]string
	Dir        string
	Workspace  string
	Timeout    time.Duration
	RunAsUser  string
	RunAsGroup string
}

// ActionResult holds the execution result
type ActionResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// Execute runs a command with security isolation
func (ce *ActionExecutor) Execute(ctx context.Context, spec ActionSpec) (ActionResult, error) {
	startTime := time.Now()

	// Set timeout via context
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = ce.defaultTimeout
	}

	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build command
	cmd, err := ce.buildCommand(spec)
	if err != nil {
		return ActionResult{}, err
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start command
	if err := cmd.Start(); err != nil {
		return ActionResult{ExitCode: -1, Duration: time.Since(startTime)}, err
	}

	// Wait for command to finish or context to expire
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- cmd.Wait()
	}()

	select {
	case err = <-doneCh:
		// Command finished normally
	case <-ctx.Done():
		// Context expired, kill the process group
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		err = <-doneCh // Wait for the killed process to reap
	}

	duration := time.Since(startTime)

	// Build result
	result := ActionResult{
		Duration: duration,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	} else {
		result.ExitCode = 0
	}

	// Return context error if context was cancelled (timeout)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	return result, nil
}

// buildCommand constructs the command with security isolation
func (ce *ActionExecutor) buildCommand(spec ActionSpec) (*exec.Cmd, error) {
	return ce.buildCommandWithContext(context.Background(), spec, 0)
}

// buildCommandWithContext constructs the command with context and timeout
func (ce *ActionExecutor) buildCommandWithContext(ctx context.Context, spec ActionSpec, timeout time.Duration) (*exec.Cmd, error) {
	var cmd *exec.Cmd

	// Determine execution user
	runAsUser := spec.RunAsUser
	if runAsUser == "" {
		runAsUser = ce.runAsUser
	}

	runAsGroup := spec.RunAsGroup
	if runAsGroup == "" {
		runAsGroup = ce.runAsGroup
	}

	// Build command with isolation
	if runAsUser != "" {
		// Use sudo for user isolation
		// -n: non-interactive (no password prompt)
		// -u <user>: run as specified user
		// -g <group>: run as specified group
		args := []string{"-n", "-u", runAsUser}
		if runAsGroup != "" {
			args = append(args, "-g", runAsGroup)
		}
		args = append(args, spec.Command)
		args = append(args, spec.Args...)

		cmd = exec.Command("sudo", args...)
		log.Infof("[ActionExecutor] running as user=%s group=%s", runAsUser, runAsGroup)
	} else {
		// Direct execution (runs as current process user)
		cmd = exec.Command(spec.Command, spec.Args...)
		log.Debugf("[ActionExecutor] running as current user (no user isolation configured)")
	}

	// Set working directory
	workDir := spec.Dir
	if workDir == "" {
		workDir = spec.Workspace
	}
	if workDir == "" {
		workDir = ce.workspace
	}
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Set environment variables
	cmd.Env = ce.buildEnv(spec.Env)

	// Set process group for cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	return cmd, nil
}

// buildEnv constructs the environment variables.
// It inherits the current process environment and overlays custom vars.
func (ce *ActionExecutor) buildEnv(customEnv map[string]string) []string {
	// Start with current process environment
	env := make([]string, 0, len(os.Environ())+len(customEnv))
	env = append(env, os.Environ()...)

	// Overlay custom environment variables (override existing keys)
	for k, v := range customEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// KillProcessGroup kills a process group (for cleanup)
func KillProcessGroup(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid: %d", pid)
	}
	// Kill entire process group
	return syscall.Kill(-pid, syscall.SIGKILL)
}

package command

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

// CommandExecutor handles secure command execution with isolation.
type CommandExecutor struct {
	workspace      string
	runAsUser      string
	runAsGroup     string
	defaultTimeout time.Duration
}

// CommandExecutorOption configures CommandExecutor
type CommandExecutorOption func(*CommandExecutor)

// WithExecutorWorkspace sets the workspace directory
func WithExecutorWorkspace(dir string) CommandExecutorOption {
	return func(ce *CommandExecutor) {
		ce.workspace = dir
	}
}

// WithExecutorRunAsUser sets the user to run commands as
func WithExecutorRunAsUser(user string) CommandExecutorOption {
	return func(ce *CommandExecutor) {
		ce.runAsUser = user
	}
}

// WithExecutorRunAsGroup sets the group to run commands as
func WithExecutorRunAsGroup(group string) CommandExecutorOption {
	return func(ce *CommandExecutor) {
		ce.runAsGroup = group
	}
}

// WithExecutorDefaultTimeout sets the default timeout
func WithExecutorDefaultTimeout(d time.Duration) CommandExecutorOption {
	return func(ce *CommandExecutor) {
		ce.defaultTimeout = d
	}
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor(opts ...CommandExecutorOption) *CommandExecutor {
	ce := &CommandExecutor{
		defaultTimeout: 300 * time.Second, // 5 minutes default
	}

	for _, opt := range opts {
		opt(ce)
	}

	return ce
}

// CommandSpec defines how to execute a command
type CommandSpec struct {
	Command    string
	Args       []string
	Env        map[string]string
	Dir        string
	Workspace  string
	Timeout    time.Duration
	RunAsUser  string
	RunAsGroup string
}

// CommandResult holds the execution result
type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// Execute runs a command with security isolation
func (ce *CommandExecutor) Execute(ctx context.Context, spec CommandSpec) (CommandResult, error) {
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
		return CommandResult{}, err
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start command
	if err := cmd.Start(); err != nil {
		return CommandResult{ExitCode: -1, Duration: time.Since(startTime)}, err
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
	result := CommandResult{
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
func (ce *CommandExecutor) buildCommand(spec CommandSpec) (*exec.Cmd, error) {
	return ce.buildCommandWithContext(context.Background(), spec, 0)
}

// buildCommandWithContext constructs the command with context and timeout
func (ce *CommandExecutor) buildCommandWithContext(ctx context.Context, spec CommandSpec, timeout time.Duration) (*exec.Cmd, error) {
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
		log.Infof("[CommandExecutor] running as user=%s group=%s", runAsUser, runAsGroup)
	} else {
		// Direct execution (runs as current process user)
		cmd = exec.Command(spec.Command, spec.Args...)
		log.Debugf("[CommandExecutor] running as current user (no user isolation configured)")
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
func (ce *CommandExecutor) buildEnv(customEnv map[string]string) []string {
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

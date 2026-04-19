package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Verify CommandTool implements tool.CallableTool at compile time.
var _ tool.CallableTool = (*CommandTool)(nil)

// MessageInjector injects a system message to trigger agent re-evaluation.
// This abstracts the agent-level message injection mechanism,
// allowing CommandTool to remain decoupled from the agent package.
type MessageInjector interface {
	InjectMessage(msg model.Message)
}

// CommandTool is a pure execution tool for running commands.
// It supports two modes: exec (sync) and tmux_exec (async).
//
// Design principle: CommandTool only executes commands.
// KnowledgeAgent is responsible for "understanding" (translating skills/MCP to commands).
// CommandTool is responsible for "execution" only.
//
// When a tmux session state changes, CommandTool formats the state change
// event and injects it via MessageInjector to trigger a new agent iteration.
type CommandTool struct {
	workspace    string
	runAsUser    string
	runAsGroup   string
	description  string // Configurable tool description
	executor     *CommandExecutor
	tmuxExecutor *TmuxExecutor
	tmuxMonitor  *TmuxMonitor

	// injector is used to inject system messages when tmux state changes.
	injector MessageInjector
}

// CommandToolOption configures CommandTool.
type CommandToolOption func(*CommandTool)

// WithCommandWorkspace sets the workspace directory.
func WithCommandWorkspace(dir string) CommandToolOption {
	return func(ct *CommandTool) {
		ct.workspace = dir
	}
}

// WithCommandRunAsUser sets the user to run commands as.
func WithCommandRunAsUser(user string) CommandToolOption {
	return func(ct *CommandTool) {
		ct.runAsUser = user
	}
}

// WithCommandRunAsGroup sets the group to run commands as.
func WithCommandRunAsGroup(group string) CommandToolOption {
	return func(ct *CommandTool) {
		ct.runAsGroup = group
	}
}

// WithMessageInjector sets the MessageInjector for tmux state change notifications.
func WithMessageInjector(injector MessageInjector) CommandToolOption {
	return func(ct *CommandTool) {
		ct.injector = injector
	}
}

// WithDescription sets the tool description.
func WithDescription(desc string) CommandToolOption {
	return func(ct *CommandTool) {
		ct.description = desc
	}
}

// SetMessageInjector sets the MessageInjector for tmux state change notifications.
// Use this for post-creation wiring.
func (ct *CommandTool) SetMessageInjector(injector MessageInjector) {
	ct.injector = injector
}

// NewCommandTool creates a new CommandTool.
func NewCommandTool(opts ...CommandToolOption) *CommandTool {
	ct := &CommandTool{
		description: "Execute shell commands. Supports sync (exec) and async (tmux_exec) modes. Skills and MCP tools are translated to commands by the knowledge tool and executed here.",
		executor:    NewCommandExecutor(),
	}

	for _, opt := range opts {
		opt(ct)
	}

	// Set up TmuxExecutor and TmuxMonitor if tmux is available
	if IsTmuxAvailable() {
		ct.tmuxExecutor = NewTmuxExecutor()
		ct.tmuxMonitor = NewTmuxMonitor(
			WithMonitorExecutor(ct.tmuxExecutor),
			WithMonitorConfig(DefaultMonitorConfig()),
			WithMonitorStateChangeCallback(func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
				ct.handleStateChange(sessionID, string(oldStatus), string(newStatus), output)
			}),
		)
	}

	return ct
}

// Declaration implements tool.CallableTool.
func (ct *CommandTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "command",
		Description: ct.description,
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"command": {
					Type:        "string",
					Description: "Command to execute (shell command, skill script, or MCP RPC call)",
				},
				"mode": {
					Type:        "string",
					Description: "Execution mode: 'exec' (sync, wait for result) or 'tmux_exec' (async, for long-running/interactive commands)",
					Enum:        []any{"exec", "tmux_exec"},
				},
				"timeout": {
					Type:        "integer",
					Description: "Timeout in seconds (exec mode only, default: 60)",
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
			},
			Required: []string{"command"},
		},
	}
}

// Call implements tool.CallableTool.
func (ct *CommandTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args CommandArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("command: invalid args: %w", err)
	}

	if args.Command == "" {
		return nil, fmt.Errorf("command: command is required")
	}

	// Default mode is exec
	if args.Mode == "" {
		args.Mode = "exec"
	}

	switch args.Mode {
	case "exec":
		return ct.executeSync(ctx, args)
	case "tmux_exec":
		return ct.executeAsync(ctx, args)
	default:
		return nil, fmt.Errorf("command: unknown mode %q, must be 'exec' or 'tmux_exec'", args.Mode)
	}
}

// executeSync runs a command synchronously and waits for the result.
func (ct *CommandTool) executeSync(ctx context.Context, args CommandArgs) (any, error) {
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 60
	}

	spec := CommandSpec{
		Command:    "sh",
		Args:       []string{"-c", args.Command},
		Env:        args.Env,
		Dir:        args.WorkDir,
		Workspace:  ct.workspace,
		Timeout:    time.Duration(timeout) * time.Second,
		RunAsUser:  ct.runAsUser,
		RunAsGroup: ct.runAsGroup,
	}

	result, err := ct.executor.Execute(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("command: execution failed: %w", err)
	}

	return &CommandExecResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

// executeAsync runs a command in a tmux session and returns immediately.
// TmuxMonitor monitors the session and calls onStateChange when status changes.
func (ct *CommandTool) executeAsync(ctx context.Context, args CommandArgs) (any, error) {
	if ct.tmuxExecutor == nil {
		// Fallback to sync exec if tmux is not available
		log.Printf("[CommandTool] tmux not available, falling back to sync exec")
		return ct.executeSync(ctx, args)
	}

	session, err := ct.tmuxExecutor.CreateSession(ctx, TmuxCreateOptions{
		Command: args.Command,
		WorkDir: args.WorkDir,
		Env:     args.Env,
	})
	if err != nil {
		return nil, fmt.Errorf("command: failed to create tmux session: %w", err)
	}

	// Start monitoring
	if ct.tmuxMonitor != nil {
		ct.tmuxMonitor.AddSession(&TmuxSession{
			ID:        session.ID,
			Name:      session.Name,
			Command:   args.Command,
			WorkDir:   args.WorkDir,
			Status:    SessionRunning,
			CreatedAt: time.Now(),
		})
		if !ct.tmuxMonitor.running {
			ct.tmuxMonitor.Start()
		}
	}

	return &TmuxExecResponse{
		SessionID: session.ID,
		Status:    "running",
	}, nil
}

// handleStateChange processes tmux session state changes.
// It formats the state change event and injects a system message
// via MessageInjector to trigger agent re-evaluation.
func (ct *CommandTool) handleStateChange(sessionID, oldStatus, newStatus, output string) {
	log.Printf("[CommandTool] tmux session %s: %s -> %s", sessionID, oldStatus, newStatus)

	if ct.injector == nil {
		return
	}

	// Build system_input message describing the state change
	content := fmt.Sprintf("[system] tmux session %s state changed: %s -> %s", sessionID, oldStatus, newStatus)
	if output != "" {
		// Truncate long output - keep the tail (last 2000 chars)
		if len(output) > 2000 {
			output = "...(truncated)" + output[len(output)-2000:]
		}
		content += fmt.Sprintf("\nOutput:\n%s", output)
	}

	ct.injector.InjectMessage(model.Message{
		Role:    model.RoleSystem,
		Content: content,
	})
}

// ==================== Data Structures ====================

// CommandArgs represents a command execution request.
type CommandArgs struct {
	Command string            `json:"command"`
	Mode    string            `json:"mode,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// CommandExecResult represents the result of a sync command execution.
type CommandExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// TmuxExecResponse represents the response of an async tmux command execution.
type TmuxExecResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// IsTmuxAvailable checks if tmux is available on the system.
func IsTmuxAvailable() bool {
	// Simple check: try to find tmux binary
	return NewTmuxExecutor() != nil
}

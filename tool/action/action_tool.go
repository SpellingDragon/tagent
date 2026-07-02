package action

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Verify ActionTool implements tool.CallableTool at compile time.
var _ tool.CallableTool = (*ActionTool)(nil)

// MessageInjector injects a system message to trigger agent re-evaluation.
// This abstracts the agent-level message injection mechanism,
// allowing ActionTool to remain decoupled from the agent package.
type MessageInjector interface {
	InjectMessage(msg model.Message)
}

// ActionTool is a pure execution tool for running actions on real resources.
// It supports two modes: exec (sync) and tmux_exec (async).
//
// Design principle: ActionTool only executes actions.
// KnowledgeAgent is responsible for "understanding" (translating skills/MCP to actions).
// ActionTool is responsible for "execution" only.
//
// When a tmux session state changes, ActionTool formats the state change
// event and injects it via MessageInjector to trigger a new agent iteration.
//
// Tool name is "action" — it represents performing behavioral actions on
// real-world resources triggered by natural language descriptions.
type ActionTool struct {
	workspace    string
	runAsUser    string
	runAsGroup   string
	description  string // Configurable tool description
	executor     *ActionExecutor
	tmuxExecutor *TmuxExecutor
	tmuxMonitor  *TmuxMonitor

	// injector is used to inject system messages when tmux state changes.
	injector MessageInjector

	closeOnce sync.Once
}

// ActionToolOption configures ActionTool.
type ActionToolOption func(*ActionTool)

// WithActionWorkspace sets the workspace directory.
func WithActionWorkspace(dir string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.workspace = dir
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

// WithMessageInjector sets the MessageInjector for tmux state change notifications.
func WithMessageInjector(injector MessageInjector) ActionToolOption {
	return func(ct *ActionTool) {
		ct.injector = injector
	}
}

// WithDescription sets the tool description.
func WithDescription(desc string) ActionToolOption {
	return func(ct *ActionTool) {
		ct.description = desc
	}
}

// SetMessageInjector sets the MessageInjector for tmux state change notifications.
// Use this for post-creation wiring.
func (ct *ActionTool) SetMessageInjector(injector MessageInjector) {
	ct.injector = injector
}

// NewActionTool creates a new ActionTool.
func NewActionTool(opts ...ActionToolOption) *ActionTool {
	ct := &ActionTool{
		description: "Execute actions on real-world resources via tmux. Commands run asynchronously — you will receive a session_id immediately, and the execution result (stdout/stderr/exit_code) will arrive as a subsequent event. Describe the behavior you want in natural language or as a shell command.",
		executor:    NewActionExecutor(),
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

// Close stops the TmuxMonitor and releases resources.
// Uses sync.Once to ensure idempotent closure.
func (ct *ActionTool) Close() error {
	ct.closeOnce.Do(func() {
		if ct.tmuxMonitor != nil && ct.tmuxMonitor.IsRunning() {
			ct.tmuxMonitor.Stop()
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
					Description: "Set to true if the command is a TUI application (e.g., vim, htop, qodercli). TUI apps use a different monitoring strategy that skips output-stability detection.",
				},
			},
			Required: []string{"command"},
		},
	}
}

// Call implements tool.CallableTool.
//
// All invocations are routed through tmux async (executeAsync). If tmux is
// not available, falls back to synchronous execution (executeSync) which
// blocks until the command completes — this is a degraded mode.
//
// The async path returns a TmuxExecResponse (session_id + status:running).
// The actual command output will arrive later via TmuxMonitor callback,
// which publishes an external_input event to the EventBus.
func (ct *ActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args ActionArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("action: invalid args: %w", err)
	}

	if args.Command == "" {
		return nil, fmt.Errorf("action: command is required")
	}

	log.Infof("[ActionTool] executing cmd=%q", args.Command)

	// Primary path: tmux async.
	if ct.tmuxExecutor != nil {
		return ct.executeAsync(ctx, args)
	}

	// Fallback: synchronous exec (tmux not available).
	log.Warnf("[ActionTool] tmux not available, falling back to sync exec")
	return ct.executeSync(ctx, args)
}

// executeSync runs a command synchronously and waits for the result.
func (ct *ActionTool) executeSync(ctx context.Context, args ActionArgs) (any, error) {
	timeout := args.Timeout
	if timeout <= 0 {
		timeout = 60
	}

	spec := ActionSpec{
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
		return nil, fmt.Errorf("action: execution failed: %w", err)
	}

	return &ActionExecResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

// executeAsync runs a command in a tmux session and returns immediately.
// TmuxMonitor monitors the session and calls onStateChange when status changes.
func (ct *ActionTool) executeAsync(ctx context.Context, args ActionArgs) (any, error) {
	if ct.tmuxExecutor == nil {
		// Fallback to sync exec if tmux is not available
		log.Infof("[ActionTool] tmux not available, falling back to sync exec")
		return ct.executeSync(ctx, args)
	}

	session, err := ct.tmuxExecutor.CreateSession(ctx, TmuxCreateOptions{
		Command: args.Command,
		WorkDir: args.WorkDir,
		Env:     args.Env,
	})
	if err != nil {
		return nil, fmt.Errorf("action: failed to create tmux session: %w", err)
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
			IsTUI:     args.IsTUI,
		})
		if !ct.tmuxMonitor.IsRunning() {
			ct.tmuxMonitor.Start()
		}
	}

	return &TmuxExecResponse{
		SessionID: session.ID,
		Status:    "running",
	}, nil
}

// handleStateChange processes tmux session state changes.
// It formats the state change event with rich context (StableSince, IsTUI, etc.)
// and injects a system message via MessageInjector to trigger agent re-evaluation.
func (ct *ActionTool) handleStateChange(sessionID, oldStatus, newStatus, output string) {
	log.Infof("[ActionTool] tmux session %s: %s -> %s", sessionID, oldStatus, newStatus)

	if ct.injector == nil {
		return
	}

	// Build system_input message describing the state change
	content := fmt.Sprintf("[system] tmux session %s state changed: %s -> %s", sessionID, oldStatus, newStatus)

	// Enrich with session context if available
	if ct.tmuxMonitor != nil {
		if session, ok := ct.tmuxMonitor.GetSession(sessionID); ok {
			if session.IsTUI {
				content += "\n[note] This is a TUI session (screen-based, no heartbeat)"
			}
			if !session.StableSince.IsZero() {
				stableDuration := time.Since(session.StableSince).Round(time.Second)
				content += fmt.Sprintf("\n[note] Session has been stable for %v", stableDuration)
			}

			// Distinguish fakeDead timeout from output change for stable→running transitions.
			// - StableSince non-zero → fakeDead timeout: output hasn't changed, TUI was awakened.
			// - StableSince zero → output actually changed, session naturally became active again.
			if oldStatus == string(SessionStable) && newStatus == string(SessionRunning) {
				if !session.StableSince.IsZero() {
					content += "\n[note] This is a fakeDead timeout — output did NOT change, the session was already stable"
				} else {
					content += "\n[note] Output changed — session is actively producing new content"
				}
			}
		}
	}

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

// ActionProperties defines the tool-specific configuration for ActionTool.
// This is deserialized from the ToolRef.Properties map by the factory,
// keeping ToolRef generic — no command-specific fields pollute the shared struct.
//
// Example YAML:
//
//   - kind: tool
//     id: action
//     properties:
//     workspace: /tmp/tagent-workspace
//     run_as_user: tagent-runner
//     run_as_group: tagent-runner
type ActionProperties struct {
	Workspace  string `json:"workspace,omitempty"    yaml:"workspace,omitempty"`
	RunAsUser  string `json:"run_as_user,omitempty"  yaml:"run_as_user,omitempty"`
	RunAsGroup string `json:"run_as_group,omitempty" yaml:"run_as_group,omitempty"`
}

// ActionArgs represents a command execution request.
type ActionArgs struct {
	Command string            `json:"command"`
	Mode    string            `json:"mode,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
	WorkDir string            `json:"work_dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	IsTUI   bool              `json:"is_tui,omitempty"` // Hint that this is a TUI application (different monitor strategy)
}

// ActionExecResult represents the result of a sync command execution.
type ActionExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// TmuxExecResponse represents the response of an async tmux command execution.
//
// IsTmuxAsync() returns true, which signals to the AgentLoop's tool dispatch
// layer that the actual command result will arrive later via TmuxMonitor
// callback (as an external_input event). The dispatch layer should NOT publish
// this response back to the bus.
type TmuxExecResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// IsTmuxAsync marks this response as an async tmux result.
// The AgentLoop uses this to distinguish synchronous tool results (publish
// immediately) from tmux-async results (wait for TmuxMonitor callback).
func (TmuxExecResponse) IsTmuxAsync() bool { return true }

// IsTmuxAvailable checks if tmux is available on the system.
func IsTmuxAvailable() bool {
	// Simple check: try to find tmux binary
	return NewTmuxExecutor() != nil
}

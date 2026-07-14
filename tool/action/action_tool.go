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
	InjectMessageWithSource(source string, msg model.Message)
}

// ActionTool is a pure execution tool for running shell commands.
// It supports two modes controlled by the "async" parameter:
//   - Sync mode (default, async=false): runs via exec.Command, blocks until
//     completion (max 60s), returns ActionExecResult with stdout/stderr.
//   - Async mode (async=true): runs in a tmux session, returns immediately
//     with TmuxExecResponse. TmuxMonitor detects completion and injects
//     [action_tool_result] events via MessageInjector.
//
// Design principle: ActionTool only executes actions.
// KnowledgeAgent is responsible for "understanding" (translating skills/MCP to actions).
// ActionTool is responsible for "execution" only.
//
// Tool name is "action" — it represents performing behavioral actions on
// real-world resources triggered by natural language descriptions.
type ActionTool struct {
	workspace     string
	runAsUser     string
	runAsGroup    string
	description   string // Configurable tool description
	executor      *ActionExecutor
	tmuxExecutor  *TmuxExecutor
	tmuxMonitor   *TmuxMonitor
	monitorConfig *MonitorConfig // Optional: override default monitor config

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

// WithActionMonitorConfig sets a custom TmuxMonitor configuration.
func WithActionMonitorConfig(cfg MonitorConfig) ActionToolOption {
	return func(ct *ActionTool) {
		ct.monitorConfig = &cfg
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
		description: "Execute shell commands on the system. Commands run synchronously by default (async=false) and return stdout/stderr directly. Set async=true for long-running commands (builds, training, servers, watchers) — these return a session_id immediately and the output arrives later via [action_tool_result] event. When in doubt, use sync first; if it times out, retry with async=true.",
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
		monCfg := DefaultMonitorConfig()
		if ct.monitorConfig != nil {
			monCfg = *ct.monitorConfig
		}
		ct.tmuxMonitor = NewTmuxMonitor(
			WithMonitorExecutor(ct.tmuxExecutor),
			WithMonitorConfig(monCfg),
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
				"async": {
					Type:        "boolean",
					Description: "If true, run the command asynchronously via tmux. Use for long-running commands (builds, training, servers, watchers like tail -f) that may exceed 60 seconds. You will receive a session_id with status 'waiting_async_response' — DO NOT retry the same command. Wait for the [action_tool_result] event. If false or omitted (default), the command runs synchronously and blocks until completion (max 60s timeout). You receive stdout/stderr directly. When in doubt, use sync first; if it times out, retry with async=true.",
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
					Description: "Set to true if the command is a TUI application (e.g., vim, htop, qodercli). TUI apps require async=true and use a different monitoring strategy that skips output-stability detection.",
				},
			},
			Required: []string{"command"},
		},
	}
}

// Call implements tool.CallableTool.
//
// By default (async=false), commands run synchronously via exec.Command and
// block until completion (max 60s). The LLM receives stdout/stderr directly
// as the tool result and can proceed immediately.
//
// When async=true, commands run in a tmux session and return immediately with
// a TmuxExecResponse (session_id + status:"waiting_async_response"). The actual
// output arrives later via TmuxMonitor callback as an [action_tool_result] event.
//
// If tmux is not available, async requests fall back to synchronous execution.
func (ct *ActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args ActionArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("action: invalid args: %w", err)
	}

	if args.Command == "" {
		return nil, fmt.Errorf("action: command is required")
	}

	log.Infof("[ActionTool] executing cmd=%q async=%v", args.Command, args.Async)

	if args.Async {
		// Async path: tmux + TmuxMonitor.
		if ct.tmuxExecutor != nil {
			return ct.executeAsync(ctx, args)
		}
		log.Warnf("[ActionTool] async requested but tmux unavailable, falling back to sync exec")
		return ct.executeSync(ctx, args)
	}

	// Default path: synchronous exec.
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
		Status:    "waiting_async_response",
	}, nil
}

// handleStateChange processes tmux session state changes.
// It formats the state change as an external_input event with clear
// command and status context, then injects via MessageInjector to
// trigger the next runEventLoop iteration.
func (ct *ActionTool) handleStateChange(sessionID, oldStatus, newStatus, output string) {
	log.Infof("[ActionTool] tmux session %s: %s -> %s", sessionID, oldStatus, newStatus)

	if ct.injector == nil {
		return
	}

	// Look up session to get the original command
	cmd := ""
	isTUI := false
	if ct.tmuxMonitor != nil {
		if session, ok := ct.tmuxMonitor.GetSession(sessionID); ok {
			cmd = session.Command
			isTUI = session.IsTUI
		} else {
			log.Warnf("[ActionTool] tmux session %s not found in monitor, command info will be empty", sessionID)
		}
	} else {
		log.Warnf("[ActionTool] tmuxMonitor is nil, cannot look up session %s", sessionID)
	}

	// Build external_input message with clear command + status context.
	var content strings.Builder
	content.WriteString(fmt.Sprintf("[action_tool_result] 命令执行状态变更\n"))
	content.WriteString(fmt.Sprintf("命令: %s\n", cmd))
	content.WriteString(fmt.Sprintf("session: %s\n", sessionID))
	content.WriteString(fmt.Sprintf("状态: %s → %s", oldStatus, newStatus))

	// Enrich with session context
	if isTUI {
		content.WriteString("\n[note] TUI 会话 (基于屏幕，无心跳检测)")
	}
	if ct.tmuxMonitor != nil {
		if session, ok := ct.tmuxMonitor.GetSession(sessionID); ok {
			if !session.StableSince.IsZero() {
				stableDuration := time.Since(session.StableSince).Round(time.Second)
				content.WriteString(fmt.Sprintf("\n[note] 会话已稳定 %v", stableDuration))
			}
			if oldStatus == string(SessionStable) && newStatus == string(SessionRunning) {
				if !session.StableSince.IsZero() {
					content.WriteString("\n[note] 假死超时 — 输出未变化，会话已稳定")
				} else {
					content.WriteString("\n[note] 输出变化 — 会话正在产生新内容")
				}
			}
		}
	}

	if output != "" {
		// Clean up tmux output: strip trailing blank lines to reduce token waste.
		// The capture-pane -S -1000 option captures full scrollback which often
		// includes many blank lines after short commands.
		output = cleanTmuxOutput(output)
		if len(output) > 2000 {
			// Save full output to a file in the workspace
			outputDir := ct.workspace
			if outputDir == "" {
				// Default to current working directory instead of os.TempDir()
				// to keep outputs in the project directory
				if wd, err := os.Getwd(); err == nil {
					outputDir = wd
				} else {
					outputDir = "."
				}
			}
			outputFile := filepath.Join(outputDir, fmt.Sprintf("output_%s.txt", sessionID))
			if writeErr := os.WriteFile(outputFile, []byte(output), 0644); writeErr != nil {
				log.Warnf("[ActionTool] failed to save output to %s: %v", outputFile, writeErr)
			} else {
				content.WriteString(fmt.Sprintf("\n输出 (完整内容已保存到 %s，显示最后 2000 字符):\n...%s",
					outputFile, output[len(output)-2000:]))
				log.Infof("[ActionTool] full output saved to %s (%d chars)", outputFile, len(output))
				output = ""
			}
		}
		if output != "" {
			content.WriteString(fmt.Sprintf("\n输出:\n%s", output))
		}
	}

	ct.injector.InjectMessageWithSource("async_result", model.Message{
		Role:    model.RoleSystem,
		Content: content.String(),
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
//     monitor:
//     interval: 10s
//     stable_duration: 30s
type ActionProperties struct {
	Workspace  string         `json:"workspace,omitempty"    yaml:"workspace,omitempty"`
	RunAsUser  string         `json:"run_as_user,omitempty"  yaml:"run_as_user,omitempty"`
	RunAsGroup string         `json:"run_as_group,omitempty" yaml:"run_as_group,omitempty"`
	Monitor    *MonitorConfig `json:"monitor,omitempty"      yaml:"monitor,omitempty"`
}

// ActionArgs represents a command execution request.
type ActionArgs struct {
	Command string            `json:"command"`
	Async   bool              `json:"async,omitempty"` // If true, run asynchronously via tmux. If false (default), run synchronously and block until completion.
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

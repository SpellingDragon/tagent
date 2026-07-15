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
	runAsUser     string
	runAsGroup    string
	description   string // Configurable tool description
	tmuxExecutor  *TmuxExecutor
	tmuxMonitor   *TmuxMonitor
	monitorConfig *MonitorConfig // Optional: override default monitor config

	// waiters maps sessionID → result channel. Call() registers a waiter
	// when it starts a tmux session; handleStateChange delivers the first
	// stable state to the waiter. Access guarded by waitersMu.
	waitersMu sync.Mutex
	waiters   map[string]chan *stableStateResult

	closeOnce sync.Once
}

// stableStateResult carries the tmux session's final observation delivered
// to a blocked Call() when the session reaches a stable state.
type stableStateResult struct {
	oldStatus string
	newStatus string
	output    string
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
		waiters:     make(map[string]chan *stableStateResult),
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

	session, err := ct.tmuxExecutor.CreateSession(ctx, TmuxCreateOptions{
		Command: args.Command,
		WorkDir: args.WorkDir,
		Env:     args.Env,
	})
	if err != nil {
		return nil, fmt.Errorf("action: failed to create tmux session: %w", err)
	}

	// Register a waiter before adding the session to the monitor so we do
	// not miss the first meaningful state transition.
	ch := make(chan *stableStateResult, 1)
	ct.waitersMu.Lock()
	ct.waiters[session.ID] = ch
	ct.waitersMu.Unlock()
	defer func() {
		ct.waitersMu.Lock()
		delete(ct.waiters, session.ID)
		ct.waitersMu.Unlock()
	}()

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

	// Block until the session reaches a stable state or ctx is cancelled.
	select {
	case result := <-ch:
		return ct.buildActionResult(session.ID, args.Command, args.IsTUI, result), nil
	case <-ctx.Done():
		log.Warnf("[ActionTool] ctx cancelled while waiting for session %s: %v", session.ID, ctx.Err())
		return nil, ctx.Err()
	}
}

// handleStateChange delivers the first stable state observation to the
// waiting Call() for the given session. The first meaningful state
// transition (Stable/Completed/Error/TimedOut) unblocks Call().
func (ct *ActionTool) handleStateChange(sessionID, oldStatus, newStatus, output string) {
	log.Infof("[ActionTool] tmux session %s: %s -> %s", sessionID, oldStatus, newStatus)

	ct.waitersMu.Lock()
	ch, ok := ct.waiters[sessionID]
	ct.waitersMu.Unlock()
	if !ok {
		// No waiter — Call() already returned (ctx cancelled or duplicate
		// callback). The session continues running under monitor control.
		return
	}

	select {
	case ch <- &stableStateResult{
		oldStatus: oldStatus,
		newStatus: newStatus,
		output:    output,
	}:
	default:
		// Waiter already received a result; drop subsequent transitions.
	}
}

// buildActionResult composes the tool result payload the framework will
// forward to the LLM as a role=tool message.
func (ct *ActionTool) buildActionResult(sessionID, command string, isTUI bool, result *stableStateResult) *ActionToolResult {
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

	output := result.output
	var outputFile string
	if output != "" {
		output = cleanTmuxOutput(output)
		if len(output) > 2000 {
			outputDir := ct.workspace
			if outputDir == "" {
				if wd, err := os.Getwd(); err == nil {
					outputDir = wd
				} else {
					outputDir = "."
				}
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
		OldStatus:  result.oldStatus,
		Status:     result.newStatus,
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

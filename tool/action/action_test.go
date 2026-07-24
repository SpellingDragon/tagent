package action

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// mustMarshal marshals args to JSON bytes for CallableTool.Call().
func mustMarshal(t *testing.T, args map[string]interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}
	return data
}

// ==================== ActionTool Tests ====================

// TestActionTool_TmuxExec verifies that a simple tmux command runs to completion
// and returns a properly-shaped ActionToolResult with the captured output.
func TestActionTool_TmuxExec(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux (slow, blocks on monitor stability); skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux test")
	}

	tool := NewActionTool()
	defer tool.Close()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "echo async_test",
	}))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	resp, ok := result.(*ActionToolResult)
	if !ok {
		t.Fatalf("Expected *ActionToolResult, got %T", result)
	}

	if resp.SessionID == "" {
		t.Error("Expected non-empty session_id")
	}
	if resp.Status == "" {
		t.Error("Expected non-empty final status")
	}
	if !strings.Contains(resp.Output, "async_test") {
		t.Errorf("Expected output to contain 'async_test', got: %q", resp.Output)
	}
	t.Logf("Exec: session_id=%s, status=%q, output=%q", resp.SessionID, resp.Status, resp.Output)
}

// TestActionTool_TmuxUnavailable verifies that Call() returns a clear error
// when tmux is unavailable.
func TestActionTool_TmuxUnavailable(t *testing.T) {
	tool := NewActionTool()
	if tool.tmuxMonitor != nil {
		defer tool.tmuxMonitor.Stop()
	}
	tool.tmuxExecutor = nil // Simulate tmux not installed

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "echo test",
	}))
	if err == nil {
		t.Fatal("Expected error when tmux unavailable, got nil")
	}
	if !strings.Contains(err.Error(), "tmux not available") {
		t.Errorf("Expected 'tmux not available' error, got: %v", err)
	}
}

// TestActionTool_EmptyCommand verifies that an empty command is rejected
// before any tmux session is created.
func TestActionTool_EmptyCommand(t *testing.T) {
	tool := NewActionTool()
	defer tool.Close()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "",
	}))
	if err == nil {
		t.Error("Expected error for empty command")
	}
}

// TestCommandParsing exercises Call() with a range of command strings and
// verifies success/failure aligns with the input.
func TestCommandParsing(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux (slow, blocks on monitor stability); skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping command parsing test")
	}

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"simple", "echo hello", false},
		{"with_args", "ls -la /tmp", false},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewActionTool()
			defer tool.Close()

			ctx := context.Background()
			_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
				"command": tt.command,
			}))

			if tt.wantErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected success, got error: %v", err)
			}
		})
	}
}

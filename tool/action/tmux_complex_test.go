package action

import (
	"context"
	"strings"
	"testing"
)

// TestActionTool_TmuxComplexOutput verifies that tmux execution captures
// complex multi-line output correctly. Call() blocks until the tmux session
// stabilizes and returns the final output as the tool result.
func TestActionTool_TmuxComplexOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux (slow, blocks on monitor stability); skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux test")
	}

	tool := NewActionTool()
	defer tool.Close()

	// Execute a command that produces complex multi-line output
	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": `echo '{"name":"test","value":42,"items":["a","b","c"]}' && echo "---SEPARATOR---" && for i in 1 2 3; do echo "line $i"; done && echo "COMPLEX_END"`,
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
		t.Errorf("Expected non-empty final status, got %q", resp.Status)
	}
	t.Logf("Session=%s status=%q output_len=%d", resp.SessionID, resp.Status, len(resp.Output))

	// Verify the captured output contains the expected markers.
	// (Output may be truncated when > 2000 chars; short outputs come through in full.)
	captured := resp.Output
	if resp.OutputFile != "" {
		t.Logf("Output was saved to %s (truncated view returned)", resp.OutputFile)
	}
	if !strings.Contains(captured, "COMPLEX_END") {
		t.Errorf("Expected 'COMPLEX_END' in output, got: %q", captured)
	}
	if !strings.Contains(captured, "SEPARATOR") {
		t.Errorf("Expected 'SEPARATOR' in output")
	}
	if !strings.Contains(captured, "line 1") || !strings.Contains(captured, "line 3") {
		t.Errorf("Expected 'line 1'..'line 3' in output")
	}
}

// TestActionTool_TmuxLongOutput verifies that long output is truncated to
// approximately 2000 chars with the tail preserved (and the full output
// saved to a file whose path is reported via OutputFile).
func TestActionTool_TmuxLongOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux (slow, blocks on monitor stability); skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux test")
	}

	tool := NewActionTool(WithActionWorkspace(t.TempDir()))
	defer tool.Close()

	// Generate ~7500 chars of output
	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": `for i in $(seq 1 100); do echo "line $i: this is a long line of text to fill output buffer with meaningful content"; done && echo "END_MARKER"`,
	}))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	resp := result.(*ActionToolResult)
	t.Logf("Session=%s status=%q output_len=%d output_file=%q",
		resp.SessionID, resp.Status, len(resp.Output), resp.OutputFile)

	// The trailing "END_MARKER" must be preserved in the tail we hand to the LLM.
	if !strings.Contains(resp.Output, "END_MARKER") {
		t.Errorf("Expected END_MARKER in tail output, got: %q", resp.Output)
	}
	// If the raw output exceeded the 2000-char inline limit, the tool must
	// have persisted the full text to a file for later inspection.
	if resp.OutputFile == "" && len(resp.Output) > 2500 {
		t.Errorf("Long output should have been offloaded to OutputFile, got Output length %d without file", len(resp.Output))
	}
}

// TestActionTool_TmuxExitCode verifies that a non-zero exit code produces
// a proper stable-state result (Pane is dead) containing the pre-exit output.
func TestActionTool_TmuxExitCode(t *testing.T) {
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
		"command": "echo 'before_error' && exit 42 && echo 'after_error'",
	}))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	resp := result.(*ActionToolResult)
	t.Logf("Session=%s status=%q output=%q", resp.SessionID, resp.Status, resp.Output)

	if !strings.Contains(resp.Output, "before_error") {
		t.Errorf("Expected 'before_error' in output, got %q", resp.Output)
	}
	if strings.Contains(resp.Output, "after_error") {
		t.Errorf("Did not expect 'after_error' (exit 42 should prevent it)")
	}
}

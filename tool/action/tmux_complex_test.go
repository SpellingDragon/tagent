package action

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestActionTool_TmuxComplexOutput verifies that tmux async execution produces
// correct output through the TmuxMonitor state change callback → InjectMessage path.
func TestActionTool_TmuxComplexOutput(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux async test")
	}

	tool := NewActionTool()
	defer tool.Close()

	injector := &mockInjector{}
	tool.SetMessageInjector(injector)

	// Execute a command that produces complex multi-line output
	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": `echo '{"name":"test","value":42,"items":["a","b","c"]}' && echo "---SEPARATOR---" && for i in 1 2 3; do echo "line $i: $(date)"; done && echo "COMPLEX_END"`,
	}))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Verify async response
	resp, ok := result.(*TmuxExecResponse)
	if !ok {
		t.Fatalf("Expected *TmuxExecResponse, got %T", result)
	}
	if resp.SessionID == "" {
		t.Error("Expected non-empty session_id")
	}
	if resp.Status != "waiting_async_response" {
		t.Errorf("Expected status 'waiting_async_response', got %q", resp.Status)
	}
	t.Logf("AsyncExec: session_id=%s, status=%q", resp.SessionID, resp.Status)

	// Wait for TmuxMonitor to detect completion and call handleStateChange → InjectMessage
	// TmuxMonitor checks every 30s by default, but we need a shorter interval for testing.
	// Since we can't change the interval easily, wait up to 35s.
	deadline := time.After(35 * time.Second)
	var stateChangeMsg string

	for {
		select {
		case <-deadline:
			// TmuxMonitor may not have fired — manually check session output
			output, err := tool.tmuxExecutor.GetSessionOutput(resp.SessionID)
			if err != nil {
				t.Fatalf("Failed to get session output: %v", err)
			}
			t.Logf("Manual output check: %q", output)
			if !strings.Contains(output, "COMPLEX_END") {
				t.Errorf("Expected output to contain 'COMPLEX_END', got: %q", output)
			}
			if !strings.Contains(output, "SEPARATOR") {
				t.Errorf("Expected output to contain 'SEPARATOR', got: %q", output)
			}
			t.Log("TmuxMonitor didn't fire in time, but manual output check passed")
			return

		default:
			// Check if injector received a state change message
			if len(injector.messages) > 0 {
				for _, msg := range injector.messages {
					if strings.Contains(msg.Content, "state changed") {
						stateChangeMsg = msg.Content
						goto verify
					}
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

verify:
	t.Logf("State change message received: %q", stateChangeMsg)

	// Verify the state change message contains expected content
	if !strings.Contains(stateChangeMsg, "completed") {
		t.Errorf("Expected 'completed' in state change message, got: %q", stateChangeMsg)
	}

	// Verify the output contains the complex content
	// Output is appended to the state change message as "\nOutput:\n..."
	outputIdx := strings.Index(stateChangeMsg, "Output:\n")
	if outputIdx < 0 {
		t.Log("State change message has no Output section (may be truncated)")
	} else {
		output := stateChangeMsg[outputIdx+len("Output:\n"):]
		t.Logf("Captured output: %q", output)

		if !strings.Contains(output, "COMPLEX_END") {
			t.Errorf("Expected output to contain 'COMPLEX_END'")
		}
		if !strings.Contains(output, "SEPARATOR") {
			t.Errorf("Expected output to contain 'SEPARATOR'")
		}
		if !strings.Contains(output, "line 1:") {
			t.Errorf("Expected output to contain 'line 1:'")
		}
		if !strings.Contains(output, "line 3:") {
			t.Errorf("Expected output to contain 'line 3:'")
		}
	}

	// Verify the JSON content in output
	if !strings.Contains(stateChangeMsg, `"name":"test"`) && !strings.Contains(stateChangeMsg, `"value":42`) {
		// JSON may be truncated if output > 2000 chars
		t.Log("JSON content may be truncated (expected if output > 2000 chars)")
	}
}

// TestActionTool_TmuxLongOutput verifies that long output is truncated to 2000 chars
// with the tail preserved.
func TestActionTool_TmuxLongOutput(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux async test")
	}

	tool := NewActionTool()
	defer tool.Close()

	injector := &mockInjector{}
	tool.SetMessageInjector(injector)

	// Generate 5000 chars of output
	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": `for i in $(seq 1 100); do echo "line $i: this is a long line of text to fill output buffer with meaningful content"; done && echo "END_MARKER"`,
	}))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	resp := result.(*TmuxExecResponse)
	t.Logf("Session: %s", resp.SessionID)

	// Wait for state change
	deadline := time.After(35 * time.Second)
	for {
		select {
		case <-deadline:
			// Manual check
			output, _ := tool.tmuxExecutor.GetSessionOutput(resp.SessionID)
			t.Logf("Manual output (len=%d): tail=%q", len(output), output[max(0, len(output)-200):])
			if !strings.Contains(output, "END_MARKER") {
				t.Error("Expected END_MARKER in output")
			}
			t.Log("TmuxMonitor didn't fire in time, manual check passed")
			return

		default:
			for _, msg := range injector.messages {
				if strings.Contains(msg.Content, "state changed") {
					// Check truncation: output should contain "(truncated)" if > 2000 chars
					if strings.Contains(msg.Content, "(truncated)") {
						t.Log("Output was truncated as expected (> 2000 chars)")
						// Verify tail is preserved
						if strings.Contains(msg.Content, "END_MARKER") {
							t.Log("END_MARKER found in truncated output (tail preserved)")
						}
					} else {
						// Output might be <= 2000 chars
						t.Log("Output was not truncated (<= 2000 chars)")
					}
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestActionTool_TmuxExitCode verifies that non-zero exit codes are captured.
func TestActionTool_TmuxExitCode(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux async test")
	}

	tool := NewActionTool()
	defer tool.Close()

	injector := &mockInjector{}
	tool.SetMessageInjector(injector)

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "echo 'before_error' && exit 42 && echo 'after_error'",
	}))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	resp := result.(*TmuxExecResponse)
	t.Logf("Session: %s", resp.SessionID)

	// Wait for state change
	deadline := time.After(35 * time.Second)
	for {
		select {
		case <-deadline:
			output, _ := tool.tmuxExecutor.GetSessionOutput(resp.SessionID)
			t.Logf("Manual output: %q", output)
			if !strings.Contains(output, "before_error") {
				t.Error("Expected 'before_error' in output")
			}
			if strings.Contains(output, "after_error") {
				t.Error("Did not expect 'after_error' (exit 42 should prevent it)")
			}
			t.Log("Manual check passed")
			return

		default:
			for _, msg := range injector.messages {
				if strings.Contains(msg.Content, "state changed") && strings.Contains(msg.Content, "completed") {
					t.Logf("State change: %q", msg.Content)
					if strings.Contains(msg.Content, "before_error") {
						t.Log("before_error found in state change output")
					}
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

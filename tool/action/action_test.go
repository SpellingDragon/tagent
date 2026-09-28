package action

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
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

// TestActionTool_TmuxExec 钉住 verifies that a simple tmux command runs to completion and returns a properly-shaped ActionToolResult with the captured output.
func TestActionTool_TmuxExec(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux (slow, blocks on monitor stability); skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux test")
	}

	tool := NewActionTool(WithOrphanCleanupDisabled())
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

// TestActionTool_TmuxUnavailable 钉住 verifies that Call() returns a clear error when tmux is unavailable.
func TestActionTool_TmuxUnavailable(t *testing.T) {
	tool := NewActionTool(WithOrphanCleanupDisabled())
	if tool.tmuxMonitor != nil {
		defer tool.tmuxMonitor.Stop()
	}
	tool.tmuxExecutor = nil

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

// TestActionTool_EmptyCommand 钉住 verifies that an empty command is rejected before any tmux session is created.
func TestActionTool_EmptyCommand(t *testing.T) {
	tool := NewActionTool(WithOrphanCleanupDisabled())
	defer tool.Close()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "",
	}))
	if err == nil {
		t.Error("Expected error for empty command")
	}
}

// TestCommandParsing 钉住 exercises Call() with a range of command strings and verifies success/failure aligns with the input.
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
			tool := NewActionTool(WithOrphanCleanupDisabled())
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

// TestBuildAckResult_NoPollingNudge 钉住 后台确认不得怂恿模型去轮询状态——那会诱发睡眠式空等。
// - 必须保住"结果稍后写回"的诚实，并说明结束回合才是合法的等待方式。
func TestBuildAckResult_NoPollingNudge(t *testing.T) {
	ct := &ActionTool{}

	cases := []struct {
		name string
		task *task.Task
	}{
		{name: "with task id", task: &task.Task{ID: "abcd-1234"}},
		{name: "nil task", task: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := ct.buildAckResult("sess-1", "make build", tc.task)
			if res.Status != "running" {
				t.Errorf("ack status should remain running, got %q", res.Status)
			}
			for _, notWant := range []string{"查询状态", "查询结果", "状态/结果"} {
				if strings.Contains(res.Note, notWant) {
					t.Errorf("ack SHALL NOT nudge polling, found %q in %q", notWant, res.Note)
				}
			}
			for _, want := range []string{"回写", "结束本回合"} {
				if !strings.Contains(res.Note, want) {
					t.Errorf("ack missing %q in %q", want, res.Note)
				}
			}
		})
	}
}

func TestCleanTmuxOutput_StripTrailingBlankLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip trailing blanks",
			input:    "line1\nline2\n\n\n\n",
			expected: "line1\nline2",
		},
		{
			name:     "collapse consecutive blanks",
			input:    "line1\n\n\n\nline2\n\n\n",
			expected: "line1\n\nline2",
		},
		{
			name:     "preserve single blank lines",
			input:    "line1\n\nline2\n\nline3",
			expected: "line1\n\nline2\n\nline3",
		},
		{
			name:     "handle pane is dead",
			input:    "output\n\n\n\nPane is dead",
			expected: "output\n\nPane is dead",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "only blank lines",
			input:    "\n\n\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanTmuxOutput(tt.input)
			if result != tt.expected {
				t.Errorf("cleanTmuxOutput() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCleanTmuxOutput_RealWorldExample(t *testing.T) {
	input := "27\n" + strings.Repeat("\n", 50) + "Pane is dead (status 0)"
	result := cleanTmuxOutput(input)

	if strings.Count(result, "\n\n") > 1 {
		t.Errorf("cleanTmuxOutput() should collapse blank lines, got: %q", result)
	}

	if strings.HasSuffix(result, "\n\n") {
		t.Errorf("cleanTmuxOutput() should strip trailing blank lines, got: %q", result)
	}

	if !strings.Contains(result, "27") || !strings.Contains(result, "Pane is dead") {
		t.Errorf("cleanTmuxOutput() should preserve content, got: %q", result)
	}
}

// TestResolveTTL 钉住 派生的绝对寿命依次取：显式寿命（为正时）、配置默认、十分钟下限。
// - 不存在任何取值能把回收器关掉。
func TestResolveTTL(t *testing.T) {
	ct := NewActionTool(WithActionWorkspace(t.TempDir()), WithOrphanCleanupDisabled())

	if got := ct.resolveTTL(ActionArgs{}); got != 10*time.Minute {
		t.Fatalf("omitted ttl (no configured default) = %v, want 10m floor", got)
	}
	if got := ct.resolveTTL(ActionArgs{TTL: 45}); got != 45*time.Second {
		t.Fatalf("explicit ttl = %v, want 45s", got)
	}
	cur := 2 * time.Hour
	ct.SetDefaultTTLSource(func() time.Duration { return cur })
	if got := ct.resolveTTL(ActionArgs{}); got != 2*time.Hour {
		t.Fatalf("configured default (ttl omitted) = %v, want 2h", got)
	}
	if got := ct.resolveTTL(ActionArgs{TTL: 30}); got != 30*time.Second {
		t.Fatalf("explicit ttl over configured default = %v, want 30s", got)
	}
	cur = 0
	if got := ct.resolveTTL(ActionArgs{}); got != 10*time.Minute {
		t.Fatalf("zero source reading must fall back to the construction default, got %v", got)
	}
}

// TestDeclarationExposesTTL verifies the model-facing schema advertises ttl.
func TestDeclarationExposesTTL(t *testing.T) {
	ct := NewActionTool(WithActionWorkspace(t.TempDir()), WithOrphanCleanupDisabled())
	props := ct.Declaration().InputSchema.Properties
	if props == nil {
		t.Fatal("declaration must carry an input schema")
	}
	if _, ok := props["ttl"]; !ok {
		t.Fatal("Declaration must expose the ttl parameter")
	}
}

// TestEmitProbeResult_Latch 钉住 探测失败闩：首次失败发一次，重复失败保持静默；一次成功清闩，使下一次失败再次外发。
func TestEmitProbeResult_Latch(t *testing.T) {
	d := NewTmuxSettleDetector("p-test", nil, time.Hour)
	defer d.close()

	d.EmitProbeResult(false, "connection refused")
	select {
	case sig := <-d.Settled():
		if sig.Kind != task.SettleWatch {
			t.Fatalf("kind = %q, want watch", sig.Kind)
		}
		if !strings.Contains(sig.Output, "probe FAILED") {
			t.Fatalf("output %q should carry probe failure", sig.Output)
		}
	default:
		t.Fatal("expected failure signal")
	}

	d.EmitProbeResult(false, "still down")
	select {
	case sig := <-d.Settled():
		t.Fatalf("latched failure must not re-emit, got %+v", sig)
	default:
	}

	d.EmitProbeResult(true, "")
	d.EmitProbeResult(false, "down again")
	select {
	case sig := <-d.Settled():
		if !strings.Contains(sig.Output, "down again") {
			t.Fatalf("post-reset failure should emit with new detail, got %q", sig.Output)
		}
	default:
		t.Fatal("expected signal after success-reset then failure")
	}
}

// TestEmitProbeResult_SuccessSilent C2: success-only history never emits.
func TestEmitProbeResult_SuccessSilent(t *testing.T) {
	d := NewTmuxSettleDetector("p-ok", nil, time.Hour)
	defer d.close()
	for i := 0; i < 5; i++ {
		d.EmitProbeResult(true, "")
	}
	select {
	case sig := <-d.Settled():
		t.Fatalf("healthy probes must not emit, got %+v", sig)
	default:
	}
}

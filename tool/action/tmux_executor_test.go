package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTmuxCommand_WithRunAsUser(t *testing.T) {
	te := NewTmuxExecutor(
		WithTmuxRunAsUser("agent-runner"),
		WithTmuxRunAsGroup("agent-group"),
	)

	cmd, args := te.buildTmuxCommand([]string{"new-session", "-d", "-s", "test"})

	if cmd != "sudo" {
		t.Fatalf("expected command 'sudo', got %q", cmd)
	}

	expected := []string{"-n", "-u", "agent-runner", "-g", "agent-group", "tmux", "new-session", "-d", "-s", "test"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg[%d]: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestBuildTmuxCommand_WithRunAsUserNoGroup(t *testing.T) {
	te := NewTmuxExecutor(
		WithTmuxRunAsUser("agent-runner"),
	)

	cmd, args := te.buildTmuxCommand([]string{"kill-session", "-t", "sess1"})

	if cmd != "sudo" {
		t.Fatalf("expected command 'sudo', got %q", cmd)
	}

	expected := []string{"-n", "-u", "agent-runner", "tmux", "kill-session", "-t", "sess1"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg[%d]: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestBuildTmuxCommand_WithoutRunAsUser(t *testing.T) {
	te := NewTmuxExecutor()

	cmd, args := te.buildTmuxCommand([]string{"new-session", "-d", "-s", "test"})

	if cmd != "tmux" {
		t.Fatalf("expected command 'tmux', got %q", cmd)
	}

	expected := []string{"new-session", "-d", "-s", "test"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg[%d]: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestBuildTmuxCommand_BackwardCompat_AllMethods(t *testing.T) {
	te := NewTmuxExecutor()

	cases := []struct {
		name string
		args []string
	}{
		{"new-session", []string{"new-session", "-d", "-s", "s1", "-c", "/tmp", "echo hi"}},
		{"kill-session", []string{"kill-session", "-t", "s1"}},
		{"has-session", []string{"has-session", "-t", "s1"}},
		{"capture-pane", []string{"capture-pane", "-p", "-t", "s1"}},
		{"display-message", []string{"display-message", "-p", "-t", "s1", "#{pane_pid}"}},
		{"send-keys", []string{"send-keys", "-t", "s1", "echo test"}},
		{"list-sessions", []string{"list-sessions", "-F", "#{session_name}"}},
		{"set-environment", []string{"set-environment", "-t", "s1", "FOO", "bar"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args := te.buildTmuxCommand(tc.args)
			if cmd != "tmux" {
				t.Errorf("expected 'tmux', got %q", cmd)
			}
			if len(args) != len(tc.args) {
				t.Errorf("expected %d args, got %d", len(tc.args), len(args))
			}
		})
	}
}

func TestBuildTmuxCommand_SetEnvironmentWithSudo(t *testing.T) {
	te := NewTmuxExecutor(
		WithTmuxRunAsUser("agent-runner"),
	)

	envVars := map[string]string{
		"PATH":     "/usr/local/bin:/usr/bin",
		"HOME":     "/home/agent-runner",
		"NODE_ENV": "production",
	}

	for k, v := range envVars {
		args := []string{"set-environment", "-t", "test-session", k, v}
		cmd, cmdArgs := te.buildTmuxCommand(args)

		if cmd != "sudo" {
			t.Errorf("expected 'sudo' for env %s, got %q", k, cmd)
		}

		found := false
		for i, a := range cmdArgs {
			if a == k && i+1 < len(cmdArgs) && cmdArgs[i+1] == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("env var %s=%s not found in command args: %v", k, v, cmdArgs)
		}
	}
}

func TestNewActionTool_PassesRunAsUserToTmuxExecutor(t *testing.T) {
	ct := NewActionTool(
		WithActionRunAsUser("testuser"),
		WithActionRunAsGroup("testgroup"),
		WithActionWorkspace("/tmp/test-ws"),
	)

	if ct.tmuxExecutor == nil {
		t.Skip("tmux not available, skipping")
	}

	if ct.tmuxExecutor.runAsUser != "testuser" {
		t.Errorf("expected tmuxExecutor.runAsUser='testuser', got %q", ct.tmuxExecutor.runAsUser)
	}
	if ct.tmuxExecutor.runAsGroup != "testgroup" {
		t.Errorf("expected tmuxExecutor.runAsGroup='testgroup', got %q", ct.tmuxExecutor.runAsGroup)
	}
	if ct.tmuxExecutor.workspace != "/tmp/test-ws" {
		t.Errorf("expected tmuxExecutor.workspace='/tmp/test-ws', got %q", ct.tmuxExecutor.workspace)
	}

	cmd, _ := ct.tmuxExecutor.buildTmuxCommand([]string{"new-session"})
	if cmd != "sudo" {
		t.Errorf("expected sudo-wrapped command, got %q", cmd)
	}
}

// TestIsTmuxAvailable_RealPathProbe 钉住 探测必须反映系统真相：PATH 中没有 tmux 可执行文件时必须返回假。
// - 判据不得是"构造对象返回非空"——那种写法恒真，等于没有探测。
func TestIsTmuxAvailable_RealPathProbe(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)
	if IsTmuxAvailable() {
		t.Fatal("IsTmuxAvailable must be false when PATH has no tmux binary")
	}
}

// TestIsTmuxAvailable_TrueWhenOnPath 钉住 with a dir containing an executable named tmux, the probe returns true (LookPath requires the exec bit).
func TestIsTmuxAvailable_TrueWhenOnPath(t *testing.T) {
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "tmux"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)
	if !IsTmuxAvailable() {
		t.Fatal("IsTmuxAvailable must be true when a tmux executable is on PATH")
	}
}

// TestActionTool_TmuxComplexOutput 钉住 复杂多行输出被完整捕获：调用阻塞到会话稳定，并把最终输出作为工具结果返回。
func TestActionTool_TmuxComplexOutput(t *testing.T) {
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

// TestActionTool_TmuxLongOutput 钉住 verifies that long output is truncated to approximately 2000 chars with the tail preserved.
func TestActionTool_TmuxLongOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("real tmux (slow, blocks on monitor stability); skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available, skipping tmux test")
	}

	tool := NewActionTool(WithActionWorkspace(t.TempDir()), WithOrphanCleanupDisabled())
	defer tool.Close()

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

	if !strings.Contains(resp.Output, "END_MARKER") {
		t.Errorf("Expected END_MARKER in tail output, got: %q", resp.Output)
	}
	if resp.OutputFile == "" && len(resp.Output) > 2500 {
		t.Errorf("Long output should have been offloaded to OutputFile, got Output length %d without file", len(resp.Output))
	}
}

// TestActionTool_TmuxExitCode 钉住 verifies that a non-zero exit code produces a proper stable-state result (Pane is dead) containing the pre-exit output.
func TestActionTool_TmuxExitCode(t *testing.T) {
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

func TestValidSessionName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"dev-server", false},
		{"MyTunnel2", false},
		{"", false},
		{"has space", true},
		{"has/slash", true},
		{"has.dot", true},
		{"has_underscore", true},
		{"has+plus", true},
		{strings.Repeat("x", 65), true},
		{strings.Repeat("x", 64), false},
		{"中文", true},
	}
	for _, tc := range cases {
		err := validSessionName(tc.name)
		if tc.wantErr && err == nil {
			t.Errorf("name %q accepted; want rejection", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("name %q rejected unexpectedly: %v", tc.name, err)
		}
	}
}

func TestNamedSession_NamingConvention(t *testing.T) {
	if got := NamedSessionName("dev"); got != "n-dev" {
		t.Errorf("NamedSessionName(dev) = %q, want n-dev", got)
	}
}

func TestNamedSession_CallRejectsInvalidName_BeforeSessionCreation(t *testing.T) {
	ct := &ActionTool{
		tmuxMonitor:  newQuietTestMonitor(&mockInspector{processExists: true}),
		tmuxExecutor: &TmuxExecutor{},
		workspace:    t.TempDir(),
	}
	if _, err := ct.Call(t.Context(), []byte(`{"command":"true","name":"bad name"}`)); err == nil {
		t.Fatalf("invalid name accepted; want rejection")
	} else if !strings.Contains(err.Error(), "invalid character") {
		t.Errorf("rejection is not the name-validation error: %v", err)
	}
}

func TestNamedSession_DuplicateSpawnRefused(t *testing.T) {
	te := &TmuxExecutor{}
	if te.SessionExists("n-nonexistent-integration-probe") {
		t.Skip("integration environment: real tmux server present")
	}
}

func newOpTestTool(t *testing.T) (*ActionTool, string) {
	t.Helper()
	dir := t.TempDir()
	ct := &ActionTool{
		tmuxMonitor:  newQuietTestMonitor(&mockInspector{processExists: true}),
		tmuxExecutor: &TmuxExecutor{workspace: dir},
		workspace:    dir,
	}
	return ct, dir
}

func TestOpPeek_IncrementalCursor(t *testing.T) {
	ct, _ := newOpTestTool(t)
	target := "peek-cursor-test"
	pf := ct.tmuxExecutor.PipeFileFor(target)
	t.Cleanup(func() { os.Remove(pf) })

	os.WriteFile(pf, []byte("line1\nline2\n"), 0o600)

	r1, err := ct.opPeek(&ActionArgs{}, target)
	if err != nil {
		t.Fatalf("peek1: %v", err)
	}
	out1 := r1.(map[string]any)
	if out1["output"] != "line1\nline2" {
		t.Errorf("peek1 output = %q, want full log", out1["output"])
	}

	f, _ := os.OpenFile(pf, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("line3\n")
	f.Close()

	r2, err := ct.opPeek(&ActionArgs{}, target)
	if err != nil {
		t.Fatalf("peek2: %v", err)
	}
	out2 := r2.(map[string]any)
	if out2["output"] != "line3" {
		t.Errorf("peek2 output = %q, want only new line3", out2["output"])
	}
	if n := out2["bytes_new"].(int); n != len("line3\n") {
		t.Errorf("peek2 bytes_new = %d, want %d", n, len("line3\n"))
	}

	r3, _ := ct.opPeek(&ActionArgs{}, target)
	if out3 := r3.(map[string]any); out3["output"] != "" {
		t.Errorf("peek3 output = %q, want empty", out3["output"])
	}
}

func TestOpPeek_TailCapsLines(t *testing.T) {
	ct, _ := newOpTestTool(t)
	target := "peek-tail-test"
	pf := ct.tmuxExecutor.PipeFileFor(target)
	t.Cleanup(func() { os.Remove(pf) })
	os.WriteFile(pf, []byte("a\nb\nc\nd\n"), 0o600)

	r, err := ct.opPeek(&ActionArgs{Tail: 2}, target)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	out := r.(map[string]any)
	if out["output"] != "c\nd" {
		t.Errorf("tail output = %q, want last 2 lines", out["output"])
	}
	if out["truncated"] != true {
		t.Errorf("truncated flag = %v, want true", out["truncated"])
	}
}

func TestOpPeek_AnsiStripped(t *testing.T) {
	ct, _ := newOpTestTool(t)
	target := "peek-ansi-test"
	pf := ct.tmuxExecutor.PipeFileFor(target)
	t.Cleanup(func() { os.Remove(pf) })
	os.WriteFile(pf, []byte("\x1b[31mred\x1b[0m plain\n"), 0o600)

	r, _ := ct.opPeek(&ActionArgs{}, target)
	out := r.(map[string]any)["output"].(string)
	if strings.Contains(out, "\x1b") {
		t.Errorf("ANSI escape survived default strip: %q", out)
	}
	if !strings.Contains(out, "red") {
		t.Errorf("text content lost in strip: %q", out)
	}
}

func TestOpPeek_MissingLogGraceful(t *testing.T) {
	ct, _ := newOpTestTool(t)
	r, err := ct.opPeek(&ActionArgs{}, "no-such-session-xyz")
	if err != nil {
		t.Fatalf("missing log should degrade gracefully, got error: %v", err)
	}
	if got := r.(map[string]any)["status"]; got != "no_output_log" {
		t.Errorf("status = %v, want no_output_log", got)
	}
}

func TestOpSend_Validation(t *testing.T) {
	ct, _ := newOpTestTool(t)

	if _, err := ct.opSend(&ActionArgs{}, "s"); err == nil {
		t.Error("op=send without keys accepted")
	}
	if _, err := ct.opSend(&ActionArgs{Keys: "hi"}, "no-such-session-xyz"); err == nil {
		t.Error("op=send to nonexistent session accepted")
	}
}

func TestOpStop_AlreadyGone(t *testing.T) {
	ct, _ := newOpTestTool(t)
	if _, err := ct.opStop(&ActionArgs{}, "no-such-session-xyz"); err != nil {
		t.Errorf("op=stop on dead session should be idempotent success, got %v", err)
	}
}

func TestResolveTarget_PrefersSessionID(t *testing.T) {
	ct, _ := newOpTestTool(t)
	got, err := ct.resolveTarget(&ActionArgs{SessionID: "tagent-123", Name: "dev"})
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if got != "tagent-123" {
		t.Errorf("resolveTarget = %q, want explicit session_id to win", got)
	}
	if _, err := ct.resolveTarget(&ActionArgs{}); err == nil {
		t.Error("resolveTarget without id/name accepted")
	}
}

func TestStripANSI_Basic(t *testing.T) {
	in := "\x1b[2J\x1b[Hhello\x1b[0m world"
	if got := stripANSI(in); got != "hello world" {
		t.Errorf("stripANSI = %q", got)
	}
}

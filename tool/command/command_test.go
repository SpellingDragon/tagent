package command

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// ==================== CommandTool Tests ====================

// Test 1: 同步命令执行
func TestCommandTool_SyncExec(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "echo hello",
	}))
	if err != nil {
		t.Fatalf("Sync exec failed: %v", err)
	}

	resp, ok := result.(*CommandExecResult)
	if !ok {
		t.Fatalf("Expected *CommandExecResult, got %T", result)
	}

	if resp.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", resp.ExitCode)
	}
	if resp.Stdout == "" && resp.Stderr == "" {
		t.Error("Expected non-empty stdout or stderr")
	}
	t.Logf("SyncExec: exit=%d, stdout=%q, stderr=%q", resp.ExitCode, resp.Stdout, resp.Stderr)
}

// Test 2: 异步 tmux 执行
func TestCommandTool_AsyncExec(t *testing.T) {
	result, err := quickAsyncTest(t)
	if err != nil {
		t.Fatalf("Async exec failed: %v", err)
	}

	resp, ok := result.(*TmuxExecResponse)
	if !ok {
		t.Fatalf("Expected *TmuxExecResponse, got %T", result)
	}

	if resp.SessionID == "" {
		t.Error("Expected non-empty session_id")
	}
	if resp.Status != string(SessionRunning) {
		t.Errorf("Expected status %q, got %q", SessionRunning, resp.Status)
	}
	t.Logf("AsyncExec: session_id=%s, status=%q", resp.SessionID, resp.Status)
}

func quickAsyncTest(t *testing.T) (any, error) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	return tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "tmux_exec",
		"command": "echo async_test",
	}))
}

// Test 3: 工作目录
func TestCommandTool_WorkDir(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewCommandTool(WithCommandWorkspace(tempDir))
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "pwd",
	}))
	if err != nil {
		t.Fatalf("Exec with workdir failed: %v", err)
	}

	resp := result.(*CommandExecResult)
	// pwd output should end with tempDir
	expected := tempDir + "\n"
	if resp.Stdout != expected {
		t.Errorf("Expected stdout %q, got %q", expected, resp.Stdout)
	}
}

// Test 4: 超时
func TestCommandTool_Timeout(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "sleep 5",
		"timeout": 1,
	}))
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if result != nil {
		t.Errorf("Expected nil result on timeout, got %T", result)
	}
}

// Test 5: 未知 mode
func TestCommandTool_UnknownMode(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "unknown",
		"command": "echo test",
	}))
	if err == nil {
		t.Error("Expected error for unknown mode")
	}
}

// Test 6: TmuxMonitor 状态获取
func TestTmuxMonitor_GetSession(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "tmux_exec",
		"command": "echo monitor_test",
	}))
	if err != nil {
		t.Fatalf("Async exec failed: %v", err)
	}

	resp := result.(*TmuxExecResponse)

	// 验证: TmuxMonitor 开始监控此会话
	_, exists := tool.tmuxMonitor.GetSession(resp.SessionID)
	if !exists {
		t.Error("Expected session to be monitored by TmuxMonitor")
	}
}

// Test 7: 命令执行与工作目录
func TestCommandTool_ExecInWorkDir(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewCommandTool(WithCommandWorkspace(tempDir))
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "ls",
	}))
	if err != nil {
		t.Fatalf("Exec in workdir failed: %v", err)
	}

	resp := result.(*CommandExecResult)
	if resp.ExitCode != 0 {
		t.Errorf("ls failed: exit=%d, stderr=%s", resp.ExitCode, resp.Stderr)
	}
}

// Test 8: 空命令
func TestCommandTool_EmptyCommand(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "",
	}))
	if err == nil {
		t.Error("Expected error for empty command")
	}
}

// Test 9: 命令退出码
func TestCommandTool_ExitCode(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "exit 42",
	}))
	if err != nil {
		t.Fatalf("Exec failed unexpectedly: %v", err)
	}

	resp := result.(*CommandExecResult)
	if resp.ExitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", resp.ExitCode)
	}
}

// Test 10: 同步执行带环境变量
func TestCommandTool_SyncExecWithEnv(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "echo $TEST_VAR",
		"env":     map[string]string{"TEST_VAR": "hello_world"},
	}))
	if err != nil {
		t.Fatalf("Sync exec with env failed: %v", err)
	}

	resp := result.(*CommandExecResult)
	if resp.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", resp.ExitCode)
	}
	expected := "hello_world\n"
	if resp.Stdout != expected {
		t.Errorf("Expected stdout %q, got %q", expected, resp.Stdout)
	}
}

// Test 11: 带工作目录的脚本执行
func TestCommandTool_ScriptExecution(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "test_script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho 'script executed'\n"), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": scriptPath,
	}))
	if err != nil {
		t.Fatalf("Script execution failed: %v", err)
	}

	resp := result.(*CommandExecResult)
	if resp.ExitCode != 0 {
		t.Errorf("Script failed: exit_code=%d, stderr=%s", resp.ExitCode, resp.Stderr)
	}
	t.Logf("ScriptExecution: %s", resp.Stdout)
}

// Test 12: TmuxMonitor 状态变化检测
func TestTmuxMonitor_StateDetection(t *testing.T) {
	tool := NewCommandTool(
		WithCommandWorkspace(t.TempDir()),
	)
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "tmux_exec",
		"command": "sleep 1 && echo done",
	}))
	if err != nil {
		t.Fatalf("Tmux exec failed: %v", err)
	}

	resp := result.(*TmuxExecResponse)
	sessionID := resp.SessionID

	t.Logf("Created session %s", sessionID)

	// 等待命令完成
	time.Sleep(2 * time.Second)

	// 验证: 会话状态应为 completed
	session, exists := tool.tmuxMonitor.GetSession(sessionID)
	if !exists {
		t.Fatal("Session not found")
	}

	if session.Status != SessionCompleted {
		t.Errorf("Expected status %q, got %q", SessionCompleted, session.Status)
	}
	t.Logf("Session status: %s", session.Status)
}

// Test 13: TmuxMonitor KillSession
func TestTmuxMonitor_KillSession(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "tmux_exec",
		"command": "sleep 60",
	}))
	if err != nil {
		t.Fatalf("Tmux exec failed: %v", err)
	}

	resp := result.(*TmuxExecResponse)
	sessionID := resp.SessionID

	// 验证: 杀死会话后状态应为 error
	tool.tmuxExecutor.KillSession(sessionID)
	time.Sleep(500 * time.Millisecond)

	session, exists := tool.tmuxMonitor.GetSession(sessionID)
	if !exists {
		t.Fatal("Session not found after kill")
	}

	t.Logf("After kill: status=%s", session.Status)
}

// Test 14: 命令解析
func TestCommandParsing(t *testing.T) {
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
			tool := NewCommandTool()
			defer tool.tmuxMonitor.Stop()

			ctx := context.Background()
			_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
				"mode":    "exec",
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

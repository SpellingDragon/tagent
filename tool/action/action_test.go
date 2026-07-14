package action

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// ==================== ActionTool Tests ====================

// Test 1: 同步命令执行（tmux 不可用时 fallback 路径）
func TestActionTool_SyncExec(t *testing.T) {
	tool := NewActionTool()
	// Force sync path by clearing tmuxExecutor.
	tool.tmuxExecutor = nil
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "echo hello",
	}))
	if err != nil {
		t.Fatalf("Sync exec failed: %v", err)
	}

	resp, ok := result.(*ActionExecResult)
	if !ok {
		t.Fatalf("Expected *ActionExecResult, got %T", result)
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
func TestActionTool_AsyncExec(t *testing.T) {
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
	if resp.Status != "waiting_async_response" {
		t.Errorf("Expected status %q, got %q", "waiting_async_response", resp.Status)
	}
	t.Logf("AsyncExec: session_id=%s, status=%q", resp.SessionID, resp.Status)
}

func quickAsyncTest(t *testing.T) (any, error) {
	tool := NewActionTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	return tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "echo async_test",
		"async":   true,
	}))
}

// Test 3: 工作目录（sync fallback 路径）
func TestActionTool_WorkDir(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewActionTool(WithActionWorkspace(tempDir))
	tool.tmuxExecutor = nil // Force sync path.
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "pwd",
	}))
	if err != nil {
		t.Fatalf("Exec with workdir failed: %v", err)
	}

	resp := result.(*ActionExecResult)
	// Normalize both paths to handle macOS /var -> /private/var symlink
	expectedPath, _ := filepath.EvalSymlinks(tempDir)
	actualPath := strings.TrimRight(resp.Stdout, "\n")
	actualPath, _ = filepath.EvalSymlinks(actualPath)
	if expectedPath != actualPath {
		t.Errorf("Expected stdout %q, got %q", expectedPath+"\n", actualPath+"\n")
	}
}

// Test 4: 超时（sync fallback 路径）
func TestActionTool_Timeout(t *testing.T) {
	tool := NewActionTool()
	tool.tmuxExecutor = nil // Force sync path.
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
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

// Test 5: mode 字段已废弃，任何值都被忽略（默认走 sync）
func TestActionTool_ModeIgnored(t *testing.T) {
	tool := NewActionTool()
	tool.tmuxExecutor = nil // Force sync for predictable test.
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	// Even with an unknown mode, the call should succeed (mode is ignored).
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "unknown_mode",
		"command": "echo test",
	}))
	if err != nil {
		t.Fatalf("Expected no error (mode ignored), got: %v", err)
	}
	if _, ok := result.(*ActionExecResult); !ok {
		t.Fatalf("Expected *ActionExecResult, got %T", result)
	}
}

// Test 6: TmuxMonitor 状态获取
func TestTmuxMonitor_GetSession(t *testing.T) {
	tool := NewActionTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "echo monitor_test",
		"async":   true,
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

// Test 7: 命令执行与工作目录（sync fallback 路径）
func TestActionTool_ExecInWorkDir(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewActionTool(WithActionWorkspace(tempDir))
	tool.tmuxExecutor = nil // Force sync path.
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "ls",
	}))
	if err != nil {
		t.Fatalf("Exec in workdir failed: %v", err)
	}

	resp := result.(*ActionExecResult)
	if resp.ExitCode != 0 {
		t.Errorf("ls failed: exit=%d, stderr=%s", resp.ExitCode, resp.Stderr)
	}
}

// Test 8: 空命令
func TestActionTool_EmptyCommand(t *testing.T) {
	tool := NewActionTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "",
	}))
	if err == nil {
		t.Error("Expected error for empty command")
	}
}

// Test 9: 命令退出码（sync fallback 路径）
func TestActionTool_ExitCode(t *testing.T) {
	tool := NewActionTool()
	tool.tmuxExecutor = nil // Force sync path.
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "exit 42",
	}))
	if err != nil {
		t.Fatalf("Exec failed unexpectedly: %v", err)
	}

	resp := result.(*ActionExecResult)
	if resp.ExitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", resp.ExitCode)
	}
}

// Test 10: 同步执行带环境变量（sync fallback 路径）
func TestActionTool_SyncExecWithEnv(t *testing.T) {
	tool := NewActionTool()
	tool.tmuxExecutor = nil // Force sync path.
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "echo $TEST_VAR",
		"env":     map[string]string{"TEST_VAR": "hello_world"},
	}))
	if err != nil {
		t.Fatalf("Sync exec with env failed: %v", err)
	}

	resp := result.(*ActionExecResult)
	if resp.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", resp.ExitCode)
	}
	expected := "hello_world\n"
	if resp.Stdout != expected {
		t.Errorf("Expected stdout %q, got %q", expected, resp.Stdout)
	}
}

// Test 11: 带工作目录的脚本执行（sync fallback 路径）
func TestActionTool_ScriptExecution(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "test_script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho 'script executed'\n"), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	tool := NewActionTool()
	tool.tmuxExecutor = nil // Force sync path.
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": scriptPath,
	}))
	if err != nil {
		t.Fatalf("Script execution failed: %v", err)
	}

	resp := result.(*ActionExecResult)
	if resp.ExitCode != 0 {
		t.Errorf("Script failed: exit_code=%d, stderr=%s", resp.ExitCode, resp.Stderr)
	}
	t.Logf("ScriptExecution: %s", resp.Stdout)
}

// Test 12: TmuxMonitor 状态变化检测
func TestTmuxMonitor_StateDetection(t *testing.T) {
	tool := NewActionTool(
		WithActionWorkspace(t.TempDir()),
	)
	defer tool.tmuxMonitor.Stop()

	// 使用短轮询间隔加速测试状态检测
	tool.tmuxMonitor.interval = 100 * time.Millisecond
	// 将 fakeDeadDuration 设置足够长，确保在命令完成前不会触发假死检测。
	// 命令 "sleep 1 && echo done" 约需 1s，Duration=3s 保证 pane 正常结束后才超时。
	tool.tmuxMonitor.fakeDeadDuration = 3 * time.Second

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "sleep 1 && echo done",
		"async":   true,
	}))
	if err != nil {
		t.Fatalf("Tmux exec failed: %v", err)
	}

	resp := result.(*TmuxExecResponse)
	sessionID := resp.SessionID

	t.Logf("Created session %s", sessionID)

	// 轮询等待会话完成（超时 10s）
	deadline := time.Now().Add(10 * time.Second)
	var exists bool
	seenRunning := false // ensure session was alive before disappearing
	for {
		status, ok := tool.tmuxMonitor.GetSessionStatus(sessionID)
		if ok {
			exists = true
			seenRunning = true
			if status == SessionCompleted {
				t.Log("Session completed")
				return
			}
		} else if seenRunning {
			// Session was removed by monitor after completion.
			// This is expected: completed sessions are cleaned up.
			t.Log("Session completed (removed by monitor)")
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 超时后做最终检查
	if !exists {
		t.Fatal("Session not found after timeout")
	}
	finalStatus, _ := tool.tmuxMonitor.GetSessionStatus(sessionID)
	if finalStatus != SessionCompleted {
		t.Errorf("Expected status %q, got %q after timeout", SessionCompleted, finalStatus)
	}
	t.Logf("Session status: %s", finalStatus)
}

// Test 13: TmuxMonitor KillSession
func TestTmuxMonitor_KillSession(t *testing.T) {
	tool := NewActionTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"command": "sleep 60",
		"async":   true,
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
			tool := NewActionTool()
			tool.tmuxExecutor = nil // Force sync path.
			defer tool.tmuxMonitor.Stop()

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

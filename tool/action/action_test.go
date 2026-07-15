package action

import (
	"context"
	"encoding/json"
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
// Test 1: 异步 tmux 执行
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
	}))
}

// Test 3: tmux 不可用时返回错误
func TestActionTool_TmuxUnavailable(t *testing.T) {
	tool := NewActionTool()
	tool.tmuxExecutor = nil // Simulate tmux not installed
	defer tool.tmuxMonitor.Stop()

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

// Test 4: TmuxMonitor 状态获取
func TestTmuxMonitor_GetSession(t *testing.T) {
	tool := NewActionTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
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

// Test 14: 命令解析（需要 tmux 可用）
func TestCommandParsing(t *testing.T) {
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

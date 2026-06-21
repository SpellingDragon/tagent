package action

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// ==================== Task 4.7: ActionTool.Close() 停止 TmuxMonitor ====================

func TestActionTool_CloseStopsTmuxMonitor(t *testing.T) {
	ct := NewActionTool()
	if ct.tmuxMonitor == nil {
		t.Skip("tmux not available, skipping")
	}

	// Start the monitor
	ct.tmuxMonitor.Start()
	if !ct.tmuxMonitor.IsRunning() {
		t.Fatal("expected monitor to be running after Start()")
	}

	// Close should stop the monitor
	if err := ct.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if ct.tmuxMonitor.IsRunning() {
		t.Error("expected monitor to be stopped after Close()")
	}
}

func TestActionTool_CloseIdempotent(t *testing.T) {
	ct := NewActionTool()
	if ct.tmuxMonitor == nil {
		t.Skip("tmux not available, skipping")
	}

	ct.tmuxMonitor.Start()

	// Multiple Close() calls should not panic or error
	for i := 0; i < 3; i++ {
		if err := ct.Close(); err != nil {
			t.Fatalf("Close() call %d returned error: %v", i, err)
		}
	}

	if ct.tmuxMonitor.IsRunning() {
		t.Error("expected monitor to be stopped after multiple Close() calls")
	}
}

func TestActionTool_CloseWithoutMonitor(t *testing.T) {
	// ActionTool without tmux should still Close() without error
	ct := &ActionTool{}
	if err := ct.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

// ==================== Task 4.9: KillSession 失败后 session 保留、3 次后强制删除 ====================

func TestHandleFakeDead_KillRetry_SessionRetained(t *testing.T) {
	mock := &mockInspector{
		killErr:       errors.New("permission denied"),
		processExists: true,
		isPaneDead:    false,
		output:        "stuck output",
		heartbeatResp: "no_response",
	}

	tm := NewTmuxMonitor(
		WithMonitorExecutor(mock),
		WithMonitorConfig(MonitorConfig{
			Interval:         10 * time.Millisecond,
			StableDuration:   1 * time.Millisecond,
			FakeDeadDuration: 5 * time.Millisecond,
		}),
	)

	session := &TmuxSession{
		ID:        "test-retry",
		Name:      "test-retry",
		Command:   "sleep 9999",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
	}
	tm.AddSession(session)

	// Attempt 1: KillSession fails, should return false (retain session)
	result := tm.handleFakeDead(session)
	if result {
		t.Error("expected handleFakeDead to return false on first kill failure (session should be retained)")
	}
	if session.KillRetryCount != 1 {
		t.Errorf("expected KillRetryCount=1, got %d", session.KillRetryCount)
	}
	if session.Status != SessionStable {
		t.Errorf("expected status reverted to Stable, got %s", session.Status)
	}
	if mock.killSessionCalls != 1 {
		t.Errorf("expected 1 kill attempt, got %d", mock.killSessionCalls)
	}

	// Attempt 2: KillSession fails again, should return false (retain session)
	result = tm.handleFakeDead(session)
	if result {
		t.Error("expected handleFakeDead to return false on second kill failure")
	}
	if session.KillRetryCount != 2 {
		t.Errorf("expected KillRetryCount=2, got %d", session.KillRetryCount)
	}

	// Attempt 3: KillSession fails again, should return true (force remove)
	result = tm.handleFakeDead(session)
	if !result {
		t.Error("expected handleFakeDead to return true on third kill failure (force remove)")
	}
	if session.KillRetryCount != 3 {
		t.Errorf("expected KillRetryCount=3, got %d", session.KillRetryCount)
	}
	if session.Status != SessionCompleted {
		t.Errorf("expected status Completed after force remove, got %s", session.Status)
	}
	if mock.killSessionCalls != 3 {
		t.Errorf("expected 3 kill attempts, got %d", mock.killSessionCalls)
	}
}

func TestHandleFakeDead_KillSucceeds_FirstTry(t *testing.T) {
	mock := &mockInspector{
		killErr:       nil, // KillSession succeeds
		processExists: true,
		isPaneDead:    false,
		output:        "stuck output",
		heartbeatResp: "no_response",
	}

	tm := NewTmuxMonitor(
		WithMonitorExecutor(mock),
	)

	session := &TmuxSession{
		ID:        "test-success",
		Name:      "test-success",
		Command:   "sleep 9999",
		Status:    SessionFakeDead,
		CreatedAt: time.Now(),
	}

	result := tm.handleFakeDead(session)
	if !result {
		t.Error("expected handleFakeDead to return true when kill succeeds")
	}
	if session.KillRetryCount != 0 {
		t.Errorf("expected KillRetryCount=0 on success, got %d", session.KillRetryCount)
	}
	if session.Status != SessionCompleted {
		t.Errorf("expected status Completed, got %s", session.Status)
	}
}

func TestHandleFakeDead_Integration_RetryCycle(t *testing.T) {
	// Integration test: simulate the full checkSession cycle with retry.
	mock := &mockInspector{
		killErr:       fmt.Errorf("operation not permitted"),
		processExists: true,
		isPaneDead:    false,
		output:        "unchanged output",
		heartbeatResp: "no_response",
	}

	tm := NewTmuxMonitor(
		WithMonitorExecutor(mock),
		WithMonitorConfig(MonitorConfig{
			Interval:         5 * time.Millisecond,
			StableDuration:   1 * time.Millisecond,
			FakeDeadDuration: 2 * time.Millisecond,
		}),
	)

	session := &TmuxSession{
		ID:        "test-cycle",
		Name:      "test-cycle",
		Command:   "sleep 9999",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
	}
	tm.AddSession(session)

	// Pre-warm: call checkSession once to set LastOutputMD5 (first call always
	// sees new output and resets StableSince). After this, LastOutputMD5 is set.
	tm.checkSession(session)

	// Now set StableSince to the past so detectSessionState returns FakeDead
	session.StableSince = time.Now().Add(-10 * time.Second)

	// Cycle 1: Running → FakeDead → handleFakeDead fails → status reverted to Stable
	tm.checkSession(session)
	if _, exists := tm.GetSession("test-cycle"); !exists {
		t.Error("session should still be in monitoring map after first kill failure")
	}
	status, _ := tm.GetSessionStatus("test-cycle")
	if status != SessionStable {
		t.Errorf("expected status Stable after retry, got %s", status)
	}

	// Cycle 2: Stable → FakeDead again → handleFakeDead fails
	session.StableSince = time.Now().Add(-10 * time.Second)
	tm.checkSession(session)
	if _, exists := tm.GetSession("test-cycle"); !exists {
		t.Error("session should still be in monitoring map after second kill failure")
	}

	// Cycle 3: Force remove
	session.StableSince = time.Now().Add(-10 * time.Second)
	tm.checkSession(session)
	if _, exists := tm.GetSession("test-cycle"); exists {
		t.Error("session should be removed after 3rd kill failure (force remove)")
	}

	if mock.killSessionCalls != 3 {
		t.Errorf("expected 3 kill attempts total, got %d", mock.killSessionCalls)
	}
}

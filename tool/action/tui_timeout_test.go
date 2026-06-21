package action

import (
	"testing"
	"time"
)

// ==================== Task 5.4: TUI 会话在 fakeDead 阈值后返回 SessionTimedOut 并被移除 ====================

func TestTUI_SessionTimedOut_RemovedFromMonitoring(t *testing.T) {
	mock := &mockInspector{
		processExists: true,
		isPaneDead:    false,
		output:        "tui output unchanged",
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
		ID:        "tui-session",
		Name:      "tui-session",
		Command:   "vim",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	}
	tm.AddSession(session)

	// Pre-warm: set LastOutputMD5 so subsequent calls see unchanged output
	tm.checkSession(session)

	// Set StableSince to the past to trigger fakeDead threshold
	session.StableSince = time.Now().Add(-10 * time.Second)

	// Track state changes via callback
	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	// checkSession should detect TimedOut for TUI session and remove it
	tm.checkSession(session)

	// Session should be removed from monitoring map
	if _, exists := tm.GetSession("tui-session"); exists {
		t.Error("TUI session should be removed after SessionTimedOut")
	}

	// Callback should have received SessionTimedOut
	if callbackStatus != SessionTimedOut {
		t.Errorf("expected callback status SessionTimedOut, got %s", callbackStatus)
	}

	// Session status should be TimedOut
	if session.Status != SessionTimedOut {
		t.Errorf("expected session status SessionTimedOut, got %s", session.Status)
	}

	// Verify KillSession was NOT called (TUI sessions skip heartbeat/kill)
	if mock.killSessionCalls != 0 {
		t.Errorf("expected 0 kill calls for TUI session, got %d", mock.killSessionCalls)
	}
}

// ==================== Task 5.5: 非 TUI 会话不受影响（走 fakeDead 路径） ====================

func TestNonTUI_Session_GoesThroughFakeDead_NotTimedOut(t *testing.T) {
	mock := &mockInspector{
		processExists: true,
		isPaneDead:    false,
		output:        "non-tui output unchanged",
		heartbeatResp: "no_response",
		killErr:       nil, // KillSession succeeds
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
		ID:        "non-tui-session",
		Name:      "non-tui-session",
		Command:   "sleep 9999",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     false, // Non-TUI session
	}
	tm.AddSession(session)

	// Pre-warm
	tm.checkSession(session)

	// Set StableSince to the past
	session.StableSince = time.Now().Add(-10 * time.Second)

	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	// checkSession should detect FakeDead (not TimedOut) for non-TUI session
	tm.checkSession(session)

	// Callback should have received SessionFakeDead, NOT SessionTimedOut
	if callbackStatus != SessionFakeDead {
		t.Errorf("expected callback status SessionFakeDead for non-TUI, got %s", callbackStatus)
	}

	// KillSession should have been called (non-TUI goes through heartbeat→kill path)
	if mock.killSessionCalls != 1 {
		t.Errorf("expected 1 kill call for non-TUI session, got %d", mock.killSessionCalls)
	}

	// Session should be removed (kill succeeded → handleFakeDead returns true)
	if _, exists := tm.GetSession("non-tui-session"); exists {
		t.Error("non-TUI session should be removed after successful kill")
	}

	// Session status should be Completed (set by handleFakeDead on success)
	if session.Status != SessionCompleted {
		t.Errorf("expected session status Completed, got %s", session.Status)
	}
}

func TestTUI_SessionStableBeforeFakeDead_NotRemoved(t *testing.T) {
	// Verify TUI session reaches Stable status before fakeDead threshold
	// and is NOT removed at that point.
	mock := &mockInspector{
		processExists: true,
		isPaneDead:    false,
		output:        "tui stable output",
		heartbeatResp: "no_response",
	}

	tm := NewTmuxMonitor(
		WithMonitorExecutor(mock),
		WithMonitorConfig(MonitorConfig{
			Interval:         5 * time.Millisecond,
			StableDuration:   1 * time.Second,
			FakeDeadDuration: 5 * time.Second,
		}),
	)

	session := &TmuxSession{
		ID:        "tui-stable",
		Name:      "tui-stable",
		Command:   "htop",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	}
	tm.AddSession(session)

	// Pre-warm
	tm.checkSession(session)

	// Set StableSince to just past stableDuration but before fakeDeadDuration
	session.StableSince = time.Now().Add(-2 * time.Second)

	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	tm.checkSession(session)

	// Should be Stable, not TimedOut (2s > 1s stableDuration, but < 5s fakeDeadDuration)
	if callbackStatus != SessionStable {
		t.Errorf("expected SessionStable for TUI session before fakeDead, got %s", callbackStatus)
	}

	// Session should NOT be removed
	if _, exists := tm.GetSession("tui-stable"); !exists {
		t.Error("TUI session should not be removed at Stable status")
	}
}

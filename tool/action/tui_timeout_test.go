package action

import (
	"testing"
	"time"
)

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

	tm.checkSession(session)

	session.StableSince = time.Now().Add(-10 * time.Second)

	// Track state changes via callback
	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	tm.checkSession(session)

	if _, exists := tm.GetSession("tui-session"); exists {
		t.Error("TUI session should be removed after SessionTimedOut")
	}

	if callbackStatus != SessionTimedOut {
		t.Errorf("expected callback status SessionTimedOut, got %s", callbackStatus)
	}

	if session.Status != SessionTimedOut {
		t.Errorf("expected session status SessionTimedOut, got %s", session.Status)
	}

	if mock.killSessionCalls != 0 {
		t.Errorf("expected 0 kill calls for TUI session, got %d", mock.killSessionCalls)
	}
}

// TestNonTUI_QuietAlive_NoExplicitTimeout_StaysStable 钉住 未声明静默超时的静默存活会话判为 Stable、零击杀、且继续被监视——静默不等于假死。
//
// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
func TestNonTUI_QuietAlive_NoExplicitTimeout_StaysStable(t *testing.T) {
	mock := &mockInspector{
		processExists: true,
		isPaneDead:    false,
		output:        "non-tui output unchanged",
		heartbeatResp: "no_response",
		killErr:       nil,
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
		IsTUI:     false,
	}
	tm.AddSession(session)

	tm.checkSession(session)

	session.StableSince = time.Now().Add(-10 * time.Second)

	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	tm.checkSession(session)

	if session.Status != SessionStable {
		t.Errorf("quiet+alive session without explicit quiet_timeout should stay Stable, got %s", session.Status)
	}
	if mock.killSessionCalls != 0 {
		t.Errorf("expected 0 kill calls (no auto-kill for silence), got %d", mock.killSessionCalls)
	}
	if _, exists := tm.GetSession("non-tui-session"); !exists {
		t.Error("session must remain monitored (silence is not death)")
	}
	if callbackStatus == SessionCompleted || callbackStatus == SessionFakeDead {
		t.Errorf("no settle/kill event should fire for quiet-alive session, got %s", callbackStatus)
	}
}

func TestNonTUI_QuietAlive_ExplicitQuietTimeout_HardTimeoutKill(t *testing.T) {
	mock := &mockInspector{
		processExists: true,
		isPaneDead:    false,
		output:        "non-tui output unchanged",
		heartbeatResp: "no_response",
		killErr:       nil,
	}

	tm := NewTmuxMonitor(
		WithMonitorExecutor(mock),
		WithMonitorConfig(MonitorConfig{
			Interval:         5 * time.Millisecond,
			StableDuration:   1 * time.Millisecond,
			FakeDeadDuration: 2 * time.Millisecond,
		}),
	)

	explicitTimeout := 2 * time.Millisecond
	session := &TmuxSession{
		ID:           "non-tui-explicit",
		Name:         "non-tui-explicit",
		Command:      "sleep 9999",
		Status:       SessionRunning,
		CreatedAt:    time.Now(),
		IsTUI:        false,
		QuietTimeout: explicitTimeout,
	}
	tm.AddSession(session)

	tm.checkSession(session)

	session.StableSince = time.Now().Add(-10 * time.Second)

	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	tm.checkSession(session)

	if session.Status != SessionCompleted {
		t.Errorf("expected session status SessionCompleted after successful kill, got %s", session.Status)
	}

	if mock.killSessionCalls != 1 {
		t.Errorf("expected 1 kill call for explicit quiet_timeout session, got %d", mock.killSessionCalls)
	}

	if _, exists := tm.GetSession("non-tui-explicit"); exists {
		t.Error("session should be removed after successful kill")
	}

	if session.Status != SessionCompleted {
		t.Errorf("expected session status Completed, got %s", session.Status)
	}
	if callbackStatus == SessionFakeDead {
		t.Logf("FakeDead observed as intermediate state before kill (acceptable)")
	}
}

func TestTUI_SessionStableBeforeFakeDead_NotRemoved(t *testing.T) {
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

	tm.checkSession(session)

	session.StableSince = time.Now().Add(-2 * time.Second)

	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	tm.checkSession(session)

	if callbackStatus != SessionStable {
		t.Errorf("expected SessionStable for TUI session before fakeDead, got %s", callbackStatus)
	}

	if _, exists := tm.GetSession("tui-stable"); !exists {
		t.Error("TUI session should not be removed at Stable status")
	}
}

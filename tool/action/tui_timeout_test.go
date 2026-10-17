package action

import (
	"testing"
	"time"
)

func TestTUI_SessionTimedOut_RemovedFromMonitoring(t *testing.T) {
	assertQuietObservation(t, ModeInteractive, true)
}

// TestNonTUI_QuietAlive_NoExplicitTimeout_StaysStable keeps quiet producers alive without writes or restart.
func TestNonTUI_QuietAlive_NoExplicitTimeout_StaysStable(t *testing.T) {
	assertQuietObservation(t, ModeOneshot, false)
}

func TestNonTUI_QuietAlive_ExplicitQuietTimeout_HardTimeoutKill(t *testing.T) {
	assertQuietObservation(t, ModeOneshot, false)
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

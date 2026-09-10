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

// ==================== Task 5.5 (revised 2026-09-11): 非交互会话静默≠假死 ====================
// 契约翻转（A1）：非交互会话静默超时但进程存活时，默认保持 Stable 不再自动击杀。
// 依据：静默是长任务的常态（编译/训练/长 sleep），旧"静默→heartbeat→杀"链路会误杀健康任务
// 且污染 stdin。仅当调用方显式声明 quiet_timeout（硬超时意图）时才走假死击杀路径。

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
		// 注意：QuietTimeout 未设置 —— 无显式硬超时声明
	}
	tm.AddSession(session)

	// Pre-warm
	tm.checkSession(session)

	// Set StableSince to the past (quiet far beyond threshold)
	session.StableSince = time.Now().Add(-10 * time.Second)

	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	tm.checkSession(session)

	// 契约：静默+存活+无显式超时 → 保持 Stable，绝不击杀
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

	explicitTimeout := 2 * time.Millisecond
	session := &TmuxSession{
		ID:           "non-tui-explicit",
		Name:         "non-tui-explicit",
		Command:      "sleep 9999",
		Status:       SessionRunning,
		CreatedAt:    time.Now(),
		IsTUI:        false,
		QuietTimeout: explicitTimeout, // 显式声明：静默超过此阈值视为假死（Duration >0 = opt-in）
	}
	tm.AddSession(session)

	// Pre-warm
	tm.checkSession(session)

	// Set StableSince to the past — beyond the explicit quiet_timeout
	session.StableSince = time.Now().Add(-10 * time.Second)

	var callbackStatus SessionStatus
	tm.StateChangeCallback = func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
		callbackStatus = newStatus
	}

	// 显式 quiet_timeout 超限 + heartbeat 无响应 → 假死击杀链路
	tm.checkSession(session)

	// Verify state transition: FakeDead detected, then kill succeeded → status set to Completed
	if session.Status != SessionCompleted {
		t.Errorf("expected session status SessionCompleted after successful kill, got %s", session.Status)
	}

	// KillSession should have been called (explicit opt-in to hard timeout)
	if mock.killSessionCalls != 1 {
		t.Errorf("expected 1 kill call for explicit quiet_timeout session, got %d", mock.killSessionCalls)
	}

	// Session should be removed (kill succeeded)
	if _, exists := tm.GetSession("non-tui-explicit"); exists {
		t.Error("session should be removed after successful kill")
	}

	// Session status should be Completed (set by handleFakeDead on success)
	if session.Status != SessionCompleted {
		t.Errorf("expected session status Completed, got %s", session.Status)
	}
	if callbackStatus == SessionFakeDead {
		t.Logf("FakeDead observed as intermediate state before kill (acceptable)")
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

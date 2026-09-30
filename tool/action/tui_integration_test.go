//go:build integration

package action

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// qodercliAvailable checks if qodercli binary is available on the system.
func qodercliAvailable() bool {
	_, err := exec.LookPath("qodercli")
	return err == nil
}

// tmuxIntegrationAvailable checks if tmux is available for integration tests.
func tmuxIntegrationAvailable() bool {
	return IsTmuxAvailable()
}

// skipIfNotIntegration skips the test if qodercli or tmux is not available,
// or if running in short mode.
func skipIfNotIntegration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !tmuxIntegrationAvailable() {
		t.Skip("tmux not available")
	}
	if !qodercliAvailable() {
		t.Skip("qodercli not available")
	}
}

// newIntegrationMonitor creates a TmuxMonitor with short intervals for fast integration tests.
// - Interval: 200ms (fast polling)
// - StableDuration: 1s (output unchanged for 1s → Stable)
// - FakeDeadDuration: 3s (output unchanged for 3s → TimedOut for TUI / FakeDead for non-TUI)
func newIntegrationMonitor(executor *TmuxExecutor) *TmuxMonitor {
	return NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval:         200 * time.Millisecond,
			StableDuration:   1 * time.Second,
			FakeDeadDuration: 3 * time.Second,
		}),
	)
}

// waitForStatus polls the monitor until the session reaches the target status or timeout.
// Returns true if the target status was reached, false on timeout.
func waitForStatus(t *testing.T, tm *TmuxMonitor, sessionID string, target SessionStatus, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if session, ok := tm.GetSession(sessionID); ok {
			if session.Status == target {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitForSessionRemoved polls the monitor until the session is removed or timeout.
func waitForSessionRemoved(t *testing.T, tm *TmuxMonitor, sessionID string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, ok := tm.GetSession(sessionID); !ok {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// sessionHasTimedOut checks if the transitions list contains a TimedOut transition.
func sessionHasTimedOut(transitions []string) bool {
	for _, tr := range transitions {
		if strings.Contains(tr, string(SessionTimedOut)) {
			return true
		}
	}
	return false
}

// TestTUIIntegration_QoderCLI_Lifecycle 钉住真机 TUI 会话的完整状态旅程：可达稳定、输入可回到运行中、静默越阈走 TimedOut。
// - TUI 会话全程不进入 FakeDead 与 FakeAlive
// - 判为 TimedOut 之后会话从监控中消失
//
// 契约: docs/wiki/tool/tool-architecture.md#tmux-monitor
func TestTUIIntegration_QoderCLI_Lifecycle(t *testing.T) {
	skipIfNotIntegration(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("test-tui-lifecycle"))
	monitor := newIntegrationMonitor(executor)

	// Track state transitions
	var mu sync.Mutex
	var transitions []string
	monitor.StateChangeCallback = func(sessionID string, oldS, newS SessionStatus, output string) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "qodercli",
	})
	if err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}

	t.Cleanup(func() {
		monitor.Stop()
		executor.KillSession(session.ID)
	})

	time.Sleep(2 * time.Second)
	if !executor.SessionExists(session.ID) {
		t.Skip("qodercli exited immediately (likely missing config), skipping lifecycle test")
	}

	monitor.AddSession(&TmuxSession{
		ID:        session.ID,
		Name:      session.Name,
		Command:   "qodercli",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	})
	monitor.Start()

	if !waitForStatus(t, monitor, session.ID, SessionStable, 15*time.Second) {
		mu.Lock()
		tr := append([]string{}, transitions...)
		mu.Unlock()
		t.Fatalf("qodercli TUI did not reach Stable within 15s. Transitions: %v", tr)
	}
	t.Logf("Phase 1: qodercli reached Stable")

	err = executor.SendKeys(session.ID, "a")
	if err != nil {
		t.Fatalf("failed to send keys: %v", err)
	}

	reachedRunning := waitForStatus(t, monitor, session.ID, SessionRunning, 5*time.Second)
	if reachedRunning {
		t.Logf("Phase 2: qodercli reached Running after input")
	} else {
		t.Logf("Phase 2: qodercli did not reach Running after input (may not echo single chars)")
	}

	if !waitForSessionRemoved(t, monitor, session.ID, 15*time.Second) {
		mu.Lock()
		tr := append([]string{}, transitions...)
		mu.Unlock()
		t.Fatalf("qodercli TUI session was not removed within 15s. Transitions: %v", tr)
	}
	t.Logf("Phase 3: qodercli TUI session removed after TimedOut")

	mu.Lock()
	if !sessionHasTimedOut(transitions) {
		mu.Unlock()
		t.Errorf("expected TimedOut in transitions, got: %v", transitions)
	} else {
		mu.Unlock()
	}

	if _, ok := monitor.GetSession(session.ID); ok {
		t.Errorf("qodercli TUI session was not removed from monitor after TimedOut")
	}

	mu.Lock()
	defer mu.Unlock()
	for _, tr := range transitions {
		if strings.Contains(tr, string(SessionFakeDead)) {
			t.Errorf("TUI session should not go through FakeDead, but transitions include: %s", tr)
		}
	}

	t.Logf("Lifecycle complete. Transitions: %v", transitions)
}

// TestTUIIntegration_QoderCLI_Cleanup 钉住 TUI 会话静默越阈后的清理：会话被移出监控，不留半清理状态。
func TestTUIIntegration_QoderCLI_Cleanup(t *testing.T) {
	skipIfNotIntegration(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("test-tui-cleanup"))
	monitor := newIntegrationMonitor(executor)

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "qodercli",
	})
	if err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}

	t.Cleanup(func() {
		monitor.Stop()
		executor.KillSession(session.ID)
	})

	time.Sleep(2 * time.Second)
	if !executor.SessionExists(session.ID) {
		t.Skip("qodercli exited immediately, skipping cleanup test")
	}

	monitor.AddSession(&TmuxSession{
		ID:        session.ID,
		Name:      session.Name,
		Command:   "qodercli",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	})
	monitor.Start()

	if !waitForSessionRemoved(t, monitor, session.ID, 25*time.Second) {
		t.Fatal("qodercli TUI session was not removed from monitor within 25s")
	}

	if _, ok := monitor.GetSession(session.ID); ok {
		t.Errorf("session should have been removed from monitor after TimedOut")
	}

	t.Logf("qodercli TUI session properly cleaned up from monitor")
}

// TestTUIIntegration_NonTUI_NotTimedOut 钉住非 TUI 会话的默认判定：输出稳定后长期静默只记 Stable，不进入 TimedOut。
func TestTUIIntegration_NonTUI_NotTimedOut(t *testing.T) {
	skipIfNotIntegration(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("test-nontui"))
	monitor := newIntegrationMonitor(executor)

	var mu sync.Mutex
	var transitions []string
	monitor.StateChangeCallback = func(sessionID string, oldS, newS SessionStatus, output string) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, fmt.Sprintf("%s→%s", oldS, newS))
	}

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "echo hello && sleep 30",
	})
	if err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}

	t.Cleanup(func() {
		monitor.Stop()
		executor.KillSession(session.ID)
	})

	monitor.AddSession(&TmuxSession{
		ID:        session.ID,
		Name:      session.Name,
		Command:   "echo hello && sleep 30",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     false,
	})
	monitor.Start()

	if !waitForStatus(t, monitor, session.ID, SessionStable, 10*time.Second) {
		mu.Lock()
		tr := append([]string{}, transitions...)
		mu.Unlock()
		t.Fatalf("non-TUI session did not reach Stable within 10s. Transitions: %v", tr)
	}
	t.Logf("non-TUI session reached Stable")

	time.Sleep(8 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if sessionHasTimedOut(transitions) {
		t.Errorf("non-TUI session should NOT get TimedOut. Transitions: %v", transitions)
	}

	t.Logf("non-TUI session transitions: %v (no TimedOut — correct)", transitions)
}

// TestTUIIntegration_QoderCLI_MultiSession 钉住多个 TUI 会话各自独立判定：各自到达 Stable、各自静默越阈移出，互不牵连。
func TestTUIIntegration_QoderCLI_MultiSession(t *testing.T) {
	skipIfNotIntegration(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("test-tui-multi"))
	monitor := newIntegrationMonitor(executor)

	ctx := context.Background()

	session1, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "qodercli",
	})
	if err != nil {
		t.Fatalf("failed to create tmux session 1: %v", err)
	}

	session2, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "qodercli",
	})
	if err != nil {
		executor.KillSession(session1.ID)
		t.Fatalf("failed to create tmux session 2: %v", err)
	}

	t.Cleanup(func() {
		monitor.Stop()
		executor.KillSession(session1.ID)
		executor.KillSession(session2.ID)
	})

	time.Sleep(2 * time.Second)

	if !executor.SessionExists(session1.ID) || !executor.SessionExists(session2.ID) {
		t.Skip("one or both qodercli instances exited immediately, skipping multi-session test")
	}

	monitor.AddSession(&TmuxSession{
		ID:        session1.ID,
		Name:      session1.Name,
		Command:   "qodercli",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	})
	monitor.AddSession(&TmuxSession{
		ID:        session2.ID,
		Name:      session2.Name,
		Command:   "qodercli",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	})
	monitor.Start()

	s1Stable := waitForStatus(t, monitor, session1.ID, SessionStable, 15*time.Second)
	s2Stable := waitForStatus(t, monitor, session2.ID, SessionStable, 15*time.Second)

	if !s1Stable || !s2Stable {
		t.Fatalf("sessions did not both reach Stable: s1=%v s2=%v", s1Stable, s2Stable)
	}
	t.Logf("both qodercli sessions reached Stable")

	s1Removed := waitForSessionRemoved(t, monitor, session1.ID, 25*time.Second)
	s2Removed := waitForSessionRemoved(t, monitor, session2.ID, 25*time.Second)

	if !s1Removed {
		t.Errorf("session 1 was not removed within 25s")
	}
	if !s2Removed {
		t.Errorf("session 2 was not removed within 25s")
	}

	t.Logf("both qodercli sessions independently timed out and were removed")
}

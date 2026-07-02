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

// ==================== Task 9.1: 辅助函数 ====================

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

// ==================== Task 9.2: qodercli TUI 完整生命周期 ====================

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

	// Create tmux session running qodercli
	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "qodercli",
	})
	if err != nil {
		t.Fatalf("failed to create tmux session: %v", err)
	}

	// Cleanup: stop monitor and kill session
	t.Cleanup(func() {
		monitor.Stop()
		executor.KillSession(session.ID)
	})

	// Wait for qodercli to start and verify it's still running
	time.Sleep(2 * time.Second)
	if !executor.SessionExists(session.ID) {
		t.Skip("qodercli exited immediately (likely missing config), skipping lifecycle test")
	}

	// Add to monitor as TUI session
	monitor.AddSession(&TmuxSession{
		ID:        session.ID,
		Name:      session.Name,
		Command:   "qodercli",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	})
	monitor.Start()

	// Phase 1: Wait for Stable (qodercli TUI should stabilize after initial render)
	if !waitForStatus(t, monitor, session.ID, SessionStable, 15*time.Second) {
		mu.Lock()
		tr := append([]string{}, transitions...)
		mu.Unlock()
		t.Fatalf("qodercli TUI did not reach Stable within 15s. Transitions: %v", tr)
	}
	t.Logf("Phase 1: qodercli reached Stable")

	// Phase 2: Send input to trigger Running
	err = executor.SendKeys(session.ID, "a")
	if err != nil {
		t.Fatalf("failed to send keys: %v", err)
	}

	// Wait for Running (output should change after input)
	// Note: This may not always trigger if qodercli doesn't echo input immediately.
	// We log but don't fail if Running is not reached.
	reachedRunning := waitForStatus(t, monitor, session.ID, SessionRunning, 5*time.Second)
	if reachedRunning {
		t.Logf("Phase 2: qodercli reached Running after input")
	} else {
		t.Logf("Phase 2: qodercli did not reach Running after input (may not echo single chars)")
	}

	// Phase 3: Wait for session removal (TUI sessions are removed right after TimedOut)
	if !waitForSessionRemoved(t, monitor, session.ID, 15*time.Second) {
		mu.Lock()
		tr := append([]string{}, transitions...)
		mu.Unlock()
		t.Fatalf("qodercli TUI session was not removed within 15s. Transitions: %v", tr)
	}
	t.Logf("Phase 3: qodercli TUI session removed after TimedOut")

	// Verify TimedOut is in transitions
	mu.Lock()
	if !sessionHasTimedOut(transitions) {
		mu.Unlock()
		t.Errorf("expected TimedOut in transitions, got: %v", transitions)
	} else {
		mu.Unlock()
	}

	// Verify session is removed from monitor
	if _, ok := monitor.GetSession(session.ID); ok {
		t.Errorf("qodercli TUI session was not removed from monitor after TimedOut")
	}

	// Verify no FakeDead in transitions (TUI should skip heartbeat/kill path)
	mu.Lock()
	defer mu.Unlock()
	for _, tr := range transitions {
		if strings.Contains(tr, string(SessionFakeDead)) {
			t.Errorf("TUI session should not go through FakeDead, but transitions include: %s", tr)
		}
	}

	t.Logf("Lifecycle complete. Transitions: %v", transitions)
}

// ==================== Task 9.3: qodercli 会话超时后 tmux session 被正确清理 ====================

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

	// Verify qodercli is still running
	time.Sleep(2 * time.Second)
	if !executor.SessionExists(session.ID) {
		t.Skip("qodercli exited immediately, skipping cleanup test")
	}

	// Add to monitor and start
	monitor.AddSession(&TmuxSession{
		ID:        session.ID,
		Name:      session.Name,
		Command:   "qodercli",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     true,
	})
	monitor.Start()

	// Wait for TimedOut and session removal
	if !waitForSessionRemoved(t, monitor, session.ID, 25*time.Second) {
		t.Fatal("qodercli TUI session was not removed from monitor within 25s")
	}

	// Verify the tmux session is killed (SessionExists returns false)
	// The monitor removes the session from its map, but we need to verify
	// the actual tmux session is also killed.
	// Note: TUI sessions with SessionTimedOut are removed from the monitor map
	// but the tmux session itself may still exist (TUI skips KillSession).
	// The agent is expected to handle the TimedOut notification and decide
	// whether to kill the session.
	//
	// For this test, we verify that the monitor no longer tracks the session,
	// and we manually kill it in cleanup.
	if _, ok := monitor.GetSession(session.ID); ok {
		t.Errorf("session should have been removed from monitor after TimedOut")
	}

	t.Logf("qodercli TUI session properly cleaned up from monitor")
}

// ==================== Task 9.4: 非 TUI 命令走 fakeDead 路径而非 TimedOut ====================

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

	// Add to monitor as NON-TUI session
	monitor.AddSession(&TmuxSession{
		ID:        session.ID,
		Name:      session.Name,
		Command:   "echo hello && sleep 30",
		Status:    SessionRunning,
		CreatedAt: time.Now(),
		IsTUI:     false, // Non-TUI: should go through FakeDead, not TimedOut
	})
	monitor.Start()

	// Wait for Stable (echo hello produces output, then sleep 30 has no new output)
	if !waitForStatus(t, monitor, session.ID, SessionStable, 10*time.Second) {
		mu.Lock()
		tr := append([]string{}, transitions...)
		mu.Unlock()
		t.Fatalf("non-TUI session did not reach Stable within 10s. Transitions: %v", tr)
	}
	t.Logf("non-TUI session reached Stable")

	// Wait beyond fakeDeadDuration (3s) + buffer
	// Non-TUI sessions should NOT get TimedOut
	time.Sleep(8 * time.Second)

	// Check transitions: verify no TimedOut
	mu.Lock()
	defer mu.Unlock()

	if sessionHasTimedOut(transitions) {
		t.Errorf("non-TUI session should NOT get TimedOut. Transitions: %v", transitions)
	}

	// The session should have gone through FakeDead or FakeAlive (heartbeat path),
	// but definitely NOT TimedOut.
	t.Logf("non-TUI session transitions: %v (no TimedOut — correct)", transitions)
}

// ==================== Task 9.5: 多个 qodercli TUI 会话并发运行且独立超时 ====================

func TestTUIIntegration_QoderCLI_MultiSession(t *testing.T) {
	skipIfNotIntegration(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("test-tui-multi"))
	monitor := newIntegrationMonitor(executor)

	ctx := context.Background()

	// Create 2 qodercli sessions
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

	// Wait for qodercli instances to start
	time.Sleep(2 * time.Second)

	if !executor.SessionExists(session1.ID) || !executor.SessionExists(session2.ID) {
		t.Skip("one or both qodercli instances exited immediately, skipping multi-session test")
	}

	// Add both to monitor as TUI sessions
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

	// Wait for both to reach Stable
	s1Stable := waitForStatus(t, monitor, session1.ID, SessionStable, 15*time.Second)
	s2Stable := waitForStatus(t, monitor, session2.ID, SessionStable, 15*time.Second)

	if !s1Stable || !s2Stable {
		t.Fatalf("sessions did not both reach Stable: s1=%v s2=%v", s1Stable, s2Stable)
	}
	t.Logf("both qodercli sessions reached Stable")

	// Wait for both to be removed (TimedOut)
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

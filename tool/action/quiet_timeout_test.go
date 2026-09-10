package action

// Tests for the quiet_timeout parameter: per-session fake-dead threshold
// override that protects silent-but-legal tasks (long downloads, compiles,
// inference waits) from being killed by the global 150s fake-dead detection.

import (
	"testing"
	"time"
)

// newQuietTestMonitor: like newTestMonitor but with durations suited for
// quiet-threshold testing (stable window must elapse before fake check).
func newQuietTestMonitor(inspector *mockInspector) *TmuxMonitor {
	tm := newTestMonitor(inspector)
	tm.stableDuration = 10 * time.Millisecond
	tm.interactiveStableDuration = 10 * time.Millisecond
	tm.fakeDeadDuration = 150 * time.Millisecond // scaled-down default
	return tm
}

// quietSession builds a session that has been "silent" for the given duration.
// The inspector output is pinned to "start" so currentMD5 == LastOutputMD5 and
// the state machine keeps StableSince (instead of resetting it on output change).
func quietSession(tm *TmuxMonitor, id string, silentFor time.Duration, quietTimeout time.Duration, isTUI bool) *TmuxSession {
	insp := tm.executor.(*mockInspector)
	insp.setOutput("start", nil)
	s := newTestSession(id, SessionRunning, "start")
	s.StableSince = time.Now().Add(-silentFor)
	s.QuietTimeout = quietTimeout
	s.IsTUI = isTUI
	return s
}

// TestQuietTimeout_SessionOverridePreventsKill (4.1): with a session-level
// QuietTimeout of 600s, a silence duration that exceeds the global default
// (150s) must NOT be judged fake-dead.
func TestQuietTimeout_SessionOverridePreventsKill(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)

	s := quietSession(tm, "q1", 200*time.Millisecond, 10*time.Minute, false)
	status := tm.detectSessionState(s)
	if status == SessionFakeDead {
		t.Fatalf("session with QuietTimeout=10m judged fake-dead at 200ms silence (global default 150ms) — override not effective")
	}
	if status == SessionFakeAlive {
		t.Fatalf("unexpected fake-alive: heartbeat should not have been reached under override")
	}
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("heartbeat sent %d times under override; want 0 (fake check not entered)", inspector.sendHeartbeatCalls)
	}
}

// TestQuietTimeout_DefaultEquivalence (4.2): without the parameter the
// threshold must equal the global fakeDeadDuration exactly — at threshold-1
// no fake-dead, at threshold+1 fake path entered.
func TestQuietTimeout_DefaultEquivalence(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector) // global default = 150ms

	// Boundary: just below threshold -> not fake-dead yet.
	s := quietSession(tm, "d1", 149*time.Millisecond, 0, false)
	if st := tm.detectSessionState(s); st == SessionFakeDead {
		t.Fatalf("silence 149ms judged fake-dead with 150ms default; boundary violated")
	}

	// Boundary: past threshold -> heartbeat path (fake-dead when no response).
	s2 := quietSession(tm, "d2", 200*time.Millisecond, 0, false)
	s2.IsInteractive = true // A1: default fake-dead path (heartbeat) is interactive-only now
	if st := tm.detectSessionState(s2); st != SessionFakeDead && st != SessionFakeAlive {
		t.Fatalf("silence 200ms with default threshold: got %v, want fake path", st)
	}
	if inspector.sendHeartbeatCalls == 0 {
		t.Errorf("default path did not send heartbeat; fake check not entered")
	}
}

// TestQuietTimeout_NegativeRejectedByCall (4.3): Call must reject
// quiet_timeout below the stability window (and negatives) BEFORE creating a
// session. Uses a nil-instrumented ActionTool — validation must fire before
// any tmux interaction, so executor/monitor absence is acceptable for the
// negative-value branch.
func TestQuietTimeout_NegativeRejectedByCall(t *testing.T) {
	ct := &ActionTool{} // zero-value: executor/monitor nil
	// Negative: rejected unconditionally.
	if _, err := ct.Call(t.Context(), []byte(`{"command":"true","quiet_timeout":-5}`)); err == nil {
		t.Fatalf("negative quiet_timeout accepted; want rejection")
	}
}

// TestQuietTimeout_BelowStableWindowRejected (4.3b): a positive value below
// the stability window is rejected with a readable error, no session created.
func TestQuietTimeout_BelowStableWindowRejected(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)
	ct := &ActionTool{
		tmuxMonitor: tm,
		workspace:   t.TempDir(),
	}
	// Call 校验分支在 executor 实际使用前返回；空 executor 只为过 nil 检查。
	ct.tmuxExecutor = &TmuxExecutor{}

	// Shrink the stability window so quiet_timeout=1s falls below it.
	// (quiet_timeout=0 acceptance is covered by 4.2's default-path tests;
	// a real Call here would create a tmux session and start the monitor.)
	tm.stableDuration = 5 * time.Second
	if _, err := ct.Call(t.Context(), []byte(`{"command":"true","quiet_timeout":1}`)); err == nil {
		t.Fatalf("quiet_timeout=1s with 5s stability window accepted; want rejection")
	} else {
		t.Logf("rejection message: %v", err)
	}
}

// TestQuietTimeout_ConcurrentIsolation (4.4): two sessions in one monitor —
// one with an override, one default — must be judged with their own thresholds.
func TestQuietTimeout_ConcurrentIsolation(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)

	a := quietSession(tm, "a", 200*time.Millisecond, 10*time.Minute, false) // protected
	b := quietSession(tm, "b", 200*time.Millisecond, 0, false)              // default 150ms
	a.IsInteractive = true                                                  // A1: fake-path assertions apply to interactive sessions
	b.IsInteractive = true

	stA := tm.detectSessionState(a)
	stB := tm.detectSessionState(b)

	if stA == SessionFakeDead {
		t.Errorf("session A (override 10m) judged fake-dead; isolation broken")
	}
	if stB != SessionFakeDead && stB != SessionFakeAlive {
		t.Errorf("session B (default) not on fake path at 200ms: %v", stB)
	}
}

// TestQuietTimeout_TUIBranchFollowsOverride (4.5): a TUI session with an
// override must pass the threshold check and NOT return TimedOut while under
// the override; past the override it must return TimedOut (not heartbeat).
func TestQuietTimeout_TUIBranchFollowsOverride(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)

	// Under override: 200ms silence < 10m override -> stable-ish, not TimedOut.
	s := quietSession(tm, "t1", 200*time.Millisecond, 10*time.Minute, true)
	if st := tm.detectSessionState(s); st == SessionTimedOut {
		t.Fatalf("TUI session with override returned TimedOut at 200ms; override not applied in TUI branch")
	}

	// Past override: 11m silence > 10m override -> TimedOut (TUI skips heartbeat).
	s2 := quietSession(tm, "t2", 11*time.Minute, 10*time.Minute, true)
	if st := tm.detectSessionState(s2); st != SessionTimedOut {
		t.Fatalf("TUI session past override: got %v, want TimedOut", st)
	}
	if inspector.sendHeartbeatCalls != 0 {
		t.Errorf("TUI branch sent heartbeat; want 0 (send-keys skipped)")
	}
}

// TestQuietTimeout_ThresholdHelper (unit): fakeDeadThreshold mapping logic.
func TestQuietTimeout_ThresholdHelper(t *testing.T) {
	tm := newQuietTestMonitor(&mockInspector{})
	if got := tm.fakeDeadThreshold(nil); got != 150*time.Millisecond {
		t.Errorf("nil session threshold = %v, want global 150ms", got)
	}
	s := &TmuxSession{}
	if got := tm.fakeDeadThreshold(s); got != 150*time.Millisecond {
		t.Errorf("zero QuietTimeout threshold = %v, want global 150ms", got)
	}
	s.QuietTimeout = 7 * time.Minute
	if got := tm.fakeDeadThreshold(s); got != 7*time.Minute {
		t.Errorf("override threshold = %v, want 7m", got)
	}
}

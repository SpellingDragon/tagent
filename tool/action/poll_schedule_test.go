package action

import (
	"testing"
	"time"
)

func TestPollSchedule_IntervalForAge(t *testing.T) {
	s := DefaultPollSchedule()
	cases := []struct {
		age  time.Duration
		want time.Duration
	}{
		{0, time.Second},
		{5 * time.Second, time.Second},
		{9 * time.Second, time.Second},
		{11 * time.Second, 2 * time.Second},
		{15 * time.Second, 4 * time.Second},
		{1000 * time.Second, 60 * time.Second},
	}
	for _, c := range cases {
		if got := s.intervalForAge(c.age); got != c.want {
			t.Errorf("intervalForAge(%s) = %s, want %s", c.age, got, c.want)
		}
	}
}

// TestPollSchedule_MonotonicNonDecreasing 钉住 the interval never shrinks as age grows and never exceeds MaxInterval.
func TestPollSchedule_MonotonicNonDecreasing(t *testing.T) {
	s := DefaultPollSchedule()
	var prev time.Duration
	for age := time.Duration(0); age <= 300*time.Second; age += time.Second {
		got := s.intervalForAge(age)
		if got < prev {
			t.Errorf("interval decreased at age=%s: %s < %s", age, got, prev)
		}
		if got > s.MaxInterval {
			t.Errorf("interval %s at age=%s exceeds MaxInterval %s", got, age, s.MaxInterval)
		}
		prev = got
	}
	if prev != s.MaxInterval {
		t.Errorf("interval should reach MaxInterval by 300s, got %s", prev)
	}
}

// TestPollSchedule_DegenerateInterval 钉住 a non-positive dense interval is returned as-is (no divide/loop hazard).
func TestPollSchedule_DegenerateInterval(t *testing.T) {
	s := PollSchedule{DenseInterval: 0}
	if got := s.intervalForAge(5 * time.Second); got != 0 {
		t.Errorf("degenerate schedule should return 0, got %s", got)
	}
}

// newQuietTestMonitor: like newTestMonitor but with durations suited for
// quiet-threshold testing (stable window must elapse before fake check).
func newQuietTestMonitor(inspector *mockInspector) *TmuxMonitor {
	tm := newTestMonitor(inspector)
	tm.stableDuration = 10 * time.Millisecond
	tm.interactiveStableDuration = 10 * time.Millisecond
	tm.fakeDeadDuration = 150 * time.Millisecond
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

// TestQuietTimeout_SessionOverridePreventsKill 钉住 会话级静默超时为 600s 时，超过全局默认 150s 的静默不得判为假死。
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

// TestQuietTimeout_DefaultEquivalence 钉住 未给参数时阈值必须与全局假死时长完全相等：阈值前不判假死，阈值后进入假死路径。
func TestQuietTimeout_DefaultEquivalence(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)

	s := quietSession(tm, "d1", 149*time.Millisecond, 0, false)
	if st := tm.detectSessionState(s); st == SessionFakeDead {
		t.Fatalf("silence 149ms judged fake-dead with 150ms default; boundary violated")
	}

	s2 := quietSession(tm, "d2", 200*time.Millisecond, 0, false)
	s2.IsInteractive = true
	if st := tm.detectSessionState(s2); st != SessionFakeDead && st != SessionFakeAlive {
		t.Fatalf("silence 200ms with default threshold: got %v, want fake path", st)
	}
	if inspector.sendHeartbeatCalls == 0 {
		t.Errorf("default path did not send heartbeat; fake check not entered")
	}
}

// TestQuietTimeout_NegativeRejectedByCall 钉住 调用入口必须在选择窗口之下（含负值）拒绝该参数，且发生在创建会话之前。
// - 校验先于任何 tmux 交互，因此未注入执行器与监视器也能走这条负值分支。
func TestQuietTimeout_NegativeRejectedByCall(t *testing.T) {
	ct := &ActionTool{}
	if _, err := ct.Call(t.Context(), []byte(`{"command":"true","quiet_timeout":-5}`)); err == nil {
		t.Fatalf("negative quiet_timeout accepted; want rejection")
	}
}

// TestQuietTimeout_BelowStableWindowRejected 钉住 (4.3b): a positive value below the stability window is rejected with a readable error, no session created.。
func TestQuietTimeout_BelowStableWindowRejected(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)
	ct := &ActionTool{
		tmuxMonitor: tm,
		workspace:   t.TempDir(),
	}
	ct.tmuxExecutor = &TmuxExecutor{}

	tm.stableDuration = 5 * time.Second
	if _, err := ct.Call(t.Context(), []byte(`{"command":"true","quiet_timeout":1}`)); err == nil {
		t.Fatalf("quiet_timeout=1s with 5s stability window accepted; want rejection")
	} else {
		t.Logf("rejection message: %v", err)
	}
}

// TestQuietTimeout_ConcurrentIsolation 钉住 (4.4): two sessions in one monitor — one with an override, one default — must be judged with their own thresholds.。
func TestQuietTimeout_ConcurrentIsolation(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)

	a := quietSession(tm, "a", 200*time.Millisecond, 10*time.Minute, false)
	b := quietSession(tm, "b", 200*time.Millisecond, 0, false)
	a.IsInteractive = true
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

// TestQuietTimeout_TUIBranchFollowsOverride 钉住 带覆盖阈值的 TUI 会话在阈值内不得返回超时判定。
// - 超过覆盖阈值后必须返回超时，而不是改走心跳分支。
func TestQuietTimeout_TUIBranchFollowsOverride(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newQuietTestMonitor(inspector)

	s := quietSession(tm, "t1", 200*time.Millisecond, 10*time.Minute, true)
	if st := tm.detectSessionState(s); st == SessionTimedOut {
		t.Fatalf("TUI session with override returned TimedOut at 200ms; override not applied in TUI branch")
	}

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

// TestTrimToLineOffset 钉住 以重放基线做行偏移即得本轮增量；回看缓冲行数少于基线时退化为全量捕获，而不是丢输出。
func TestTrimToLineOffset(t *testing.T) {
	full := "line1\nline2\nline3\nline4"
	if got := trimToLineOffset(full, 2); got != "line3\nline4" {
		t.Errorf("increment view wrong: %q", got)
	}
	if got := trimToLineOffset(full, 0); got != full {
		t.Errorf("zero baseline must return input, got %q", got)
	}
	if got := trimToLineOffset("only\ntwo", 5); got != "only\ntwo" {
		t.Errorf("shifted scrollback must degrade to full capture, got %q", got)
	}
}

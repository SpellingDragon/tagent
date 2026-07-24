package action

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
)

// compile-time assertion: TmuxSettleDetector implements agent.SettleDetector.
var _ agent.SettleDetector = (*TmuxSettleDetector)(nil)

func TestStatusToSettle_Mapping(t *testing.T) {
	cases := []struct {
		status   SessionStatus
		wantKind agent.SettleKind
		wantOK   bool
	}{
		{SessionCompleted, agent.SettleCompleted, true},
		{SessionError, agent.SettleCompleted, true},
		{SessionStable, agent.SettleStable, true},
		{SessionTimedOut, agent.SettleSuspect, true},
		{SessionRunning, "", false},
		{SessionFakeDead, "", false},
		{SessionFakeAlive, "", false},
	}
	for _, c := range cases {
		gotKind, gotOK := StatusToSettle(c.status)
		if gotKind != c.wantKind || gotOK != c.wantOK {
			t.Errorf("StatusToSettle(%s) = (%q,%v), want (%q,%v)", c.status, gotKind, gotOK, c.wantKind, c.wantOK)
		}
	}
}

// TestTmuxSettleDetector_CompletedClosesStream: a completed transition emits one
// completed signal and closes the stream.
func TestTmuxSettleDetector_CompletedClosesStream(t *testing.T) {
	d := NewTmuxSettleDetector("s1", nil)
	d.OnStateChange(SessionRunning, "starting") // intermediate → no signal
	d.OnStateChange(SessionCompleted, "done output")

	sig, ok := <-d.Settled()
	if !ok {
		t.Fatalf("expected a signal")
	}
	if sig.Kind != agent.SettleCompleted || sig.Output != "done output" || sig.Err != nil {
		t.Errorf("unexpected signal: %+v", sig)
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed after terminal status")
	}
}

// TestTmuxSettleDetector_StableThenCompleted: stable is non-terminal (stream
// stays open); a later completed closes it. Two signals in order.
func TestTmuxSettleDetector_StableThenCompleted(t *testing.T) {
	d := NewTmuxSettleDetector("s2", nil)
	d.OnStateChange(SessionStable, "listening on :8080")
	d.OnStateChange(SessionCompleted, "exited")

	first := <-d.Settled()
	if first.Kind != agent.SettleStable || first.Output != "listening on :8080" {
		t.Errorf("first signal = %+v, want stable", first)
	}
	second := <-d.Settled()
	if second.Kind != agent.SettleCompleted {
		t.Errorf("second signal = %+v, want completed", second)
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed")
	}
}

// TestTmuxSettleDetector_ErrorCarriesErr: error → completed kind with Err set.
func TestTmuxSettleDetector_ErrorCarriesErr(t *testing.T) {
	d := NewTmuxSettleDetector("s3", nil)
	d.OnStateChange(SessionError, "boom")
	sig := <-d.Settled()
	if sig.Kind != agent.SettleCompleted || sig.Err == nil {
		t.Errorf("error signal = %+v, want completed+err", sig)
	}
}

// TestTmuxSettleDetector_TimedOutSuspect: timed_out → suspect + close.
func TestTmuxSettleDetector_TimedOutSuspect(t *testing.T) {
	d := NewTmuxSettleDetector("s4", nil)
	d.OnStateChange(SessionTimedOut, "no output")
	sig := <-d.Settled()
	if sig.Kind != agent.SettleSuspect {
		t.Errorf("signal = %+v, want suspect", sig)
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed after timed_out")
	}
}

// TestTmuxSettleDetector_CancelClosesAndKills: Cancel invokes cancelFn and closes.
func TestTmuxSettleDetector_CancelClosesAndKills(t *testing.T) {
	killed := false
	d := NewTmuxSettleDetector("s5", func() { killed = true })
	d.Cancel()
	if !killed {
		t.Errorf("cancelFn not invoked")
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed after cancel")
	}
}

// TestTmuxSettleDetector_DrivesTaskManager: the detector composes with the
// TaskManager sync-wait primitive (stable settle within window → inline).
func TestTmuxSettleDetector_DrivesTaskManager(t *testing.T) {
	tm := agent.NewTaskManager(agent.TaskManagerConfig{})
	d := NewTmuxSettleDetector("s6", nil, 500*time.Millisecond) // dense window 500ms
	go func() {
		d.OnStateChange(SessionStable, "ready")
	}()
	res := tm.Spawn(agent.TaskSpec{Kind: "command", Desc: "svc"}, d)
	if !res.Settled || res.Signal.Kind != agent.SettleStable {
		t.Errorf("expected inline stable settle, got %+v", res)
	}
	if res.Task.Status() != agent.TaskStable {
		t.Errorf("task status = %s, want stable", res.Task.Status())
	}
	// Stable is non-terminal (service-type); cancel to close the stream so the
	// TaskManager watch goroutine exits cleanly (no leak).
	d.Cancel()
}

// TestTmuxSettleDetector_ReapsOnCompletion: a completed (process-dead) session
// is reaped (kill closure invoked) so dead sessions don't accumulate.
func TestTmuxSettleDetector_ReapsOnCompletion(t *testing.T) {
	reaped := false
	d := NewTmuxSettleDetector("s-reap", func() { reaped = true })
	d.OnStateChange(SessionCompleted, "done")
	if !reaped {
		t.Errorf("completed session should be reaped")
	}
}

// TestTmuxSettleDetector_NoReapWhileAlive: stable (alive) and timed_out (maybe
// alive) sessions are NOT auto-reaped.
func TestTmuxSettleDetector_NoReapWhileAlive(t *testing.T) {
	for _, st := range []SessionStatus{SessionStable, SessionTimedOut} {
		reaped := false
		d := NewTmuxSettleDetector("s-alive", func() { reaped = true })
		d.OnStateChange(st, "out")
		if reaped {
			t.Errorf("%s session should NOT be auto-reaped", st)
		}
		d.Cancel()
	}
}

// TestTmuxSettleDetector_ReapOnce: reaping runs at most once (completion then cancel).
func TestTmuxSettleDetector_ReapOnce(t *testing.T) {
	count := 0
	d := NewTmuxSettleDetector("s-once", func() { count++ })
	d.OnStateChange(SessionCompleted, "done")
	d.Cancel()
	if count != 1 {
		t.Errorf("reap should run exactly once, got %d", count)
	}
}

// 契约: docs/wiki/tool/tmux-action.md#notify-gates
package action

import (
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// TestMonitorDeliversSettleAfterIntermediateState verifies recovery and terminal transitions reach both callbacks.
func TestMonitorDeliversSettleAfterIntermediateState(t *testing.T) {
	for _, old := range []SessionStatus{SessionFakeAlive, SessionFakeDead} {
		for _, next := range []SessionStatus{SessionStable, SessionCompleted, SessionError} {
			t.Run(fmt.Sprintf("%s_to_%s", old, next), func(t *testing.T) {
				inspector := &mockInspector{processExists: true, output: "last output"}
				if next != SessionStable {
					inspector.isPaneDead = true
					inspector.paneExitKnown = true
					if next == SessionError {
						inspector.paneExitCode = 2
					}
				}
				monitor := newTestMonitor(inspector)
				session := newTestSession("settle", old, inspector.output)
				session.StableSince = time.Now().Add(-20 * time.Millisecond)
				detector := NewTmuxSettleDetector(session.ID, nil)
				t.Cleanup(detector.Cancel)
				monitor.AddSessionWithCallback(session, func(_ string, _, status SessionStatus, output string) {
					detector.OnStateChange(status, output)
				})
				var notified SessionStatus
				monitor.StateChangeCallback = func(_ string, _, status SessionStatus, _ string) { notified = status }
				monitor.checkSession(session)
				require.Equal(t, next, notified)
				select {
				case sig := <-detector.Settled():
					kind, _ := StatusToSettle(next)
					require.Equal(t, kind, sig.Kind)
					require.Equal(t, inspector.output, sig.Output)
				default:
					t.Fatal("settle did not reach the detector")
				}
			})
		}
	}
}

// TestMonitorDeliversFinalStatusAfterFakeDeadCleanup verifies cleanup delivers its final verdict before removing the session.
func TestMonitorDeliversFinalStatusAfterFakeDeadCleanup(t *testing.T) {
	for _, killFails := range []bool{false, true} {
		t.Run(fmt.Sprint(killFails), func(t *testing.T) {
			inspector := &mockInspector{processExists: true, output: "hung output"}
			want := SessionCompleted
			if killFails {
				inspector.killErr = fmt.Errorf("kill failed")
				want = SessionError
			}
			monitor := newTestMonitor(inspector)
			session := newTestSession("hung", SessionStable, inspector.output)
			session.StableSince = time.Now().Add(-2 * time.Minute)
			session.QuietTimeout = time.Minute
			session.KillRetryCount = 2
			detector := NewTmuxSettleDetector(session.ID, nil)
			t.Cleanup(detector.Cancel)
			monitor.AddSessionWithCallback(session, func(_ string, _, status SessionStatus, output string) {
				require.Equal(t, want, status)
				detector.OnStateChange(status, output)
			})
			monitor.checkSession(session)
			select {
			case sig := <-detector.Settled():
				require.Equal(t, task.SettleCompleted, sig.Kind)
				require.Equal(t, killFails, sig.Err != nil)
			default:
				t.Fatal("cleanup removed the session without delivering its terminal settle")
			}
		})
	}
}

// TestMonitorResumeCanNotifyStableAgain verifies an identical prompt can settle a new resumed round once.
func TestMonitorResumeCanNotifyStableAgain(t *testing.T) {
	inspector := &mockInspector{processExists: true, output: "prompt"}
	monitor := newTestMonitor(inspector)
	session := newTestSession("interactive", SessionRunning, inspector.output)
	session.StableSince = time.Now().Add(-20 * time.Millisecond)
	var stableCount int
	monitor.AddSessionWithCallback(session, func(_ string, _, status SessionStatus, _ string) {
		if status == SessionStable {
			stableCount++
		}
	})
	monitor.checkSession(session)
	require.Equal(t, 1, stableCount)
	require.True(t, monitor.TouchSession(session.ID))
	require.True(t, session.StableSince.IsZero(), "a resumed round needs a fresh stability window")
	session.StableSince = time.Now().Add(-20 * time.Millisecond)
	monitor.checkSession(session)
	monitor.checkSession(session)
	require.Equal(t, 2, stableCount, "notify once per resumed round, even with the same prompt")
}

// TestDetectorTerminalSurvivesFullObservationQueue verifies backpressure preserves completion at queue capacity.
func TestDetectorTerminalSurvivesFullObservationQueue(t *testing.T) {
	detector := NewTmuxSettleDetector("full-queue", nil)
	t.Cleanup(detector.Cancel)
	for i := 0; i < cap(detector.ch); i++ {
		detector.OnStateChange(SessionStable, "observation")
	}
	done := make(chan struct{})
	go func() {
		detector.OnStateChange(SessionCompleted, "terminal output")
		close(done)
	}()
	sawTerminal := false
	deadline := time.After(time.Second)
	for {
		select {
		case sig, ok := <-detector.Settled():
			if !ok {
				require.True(t, sawTerminal, "a full observation queue must not discard completion")
				<-done
				return
			}
			if sig.Kind == task.SettleCompleted {
				sawTerminal = true
				require.Equal(t, "terminal output", sig.Output)
			}
		case <-deadline:
			t.Fatal("terminal delivery did not unblock when the consumer drained the queue")
		}
	}
}

// TestDetectorIgnoresSignalsAfterCancellation verifies late monitor/probe callbacks cannot send to a closed stream.
func TestDetectorIgnoresSignalsAfterCancellation(t *testing.T) {
	detector := NewTmuxSettleDetector("cancelled", nil)
	require.NoError(t, detector.SetWatch("error", time.Second))
	detector.Cancel()
	detector.OnStateChange(SessionCompleted, "late output")
	detector.OnWatchOutput("error")
	detector.EmitProbeResult(false, "late probe")
	_, open := <-detector.Settled()
	require.False(t, open)
}

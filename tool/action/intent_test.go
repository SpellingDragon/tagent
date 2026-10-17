package action

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

func assertQuietObservation(t *testing.T, mode SessionMode, tui bool) {
	t.Helper()
	inspector := &mockInspector{processExists: true, output: "unchanged"}
	monitor := newTestMonitor(inspector)
	session := newTestSession("quiet", SessionRunning, inspector.output)
	session.Mode, session.IsTUI = mode, tui
	session.QuietTimeout = time.Second
	session.StableSince = time.Now().Add(-time.Hour)
	detector := NewTmuxSettleDetector(session.ID, nil)
	t.Cleanup(detector.Cancel)
	monitor.AddSessionWithCallback(session, func(_ string, _, status SessionStatus, output string) { detector.OnStateChange(status, output) })
	monitor.checkSession(session)
	require.Equal(t, SessionTimedOut, session.Status)
	_, tracked := monitor.GetSession(session.ID)
	require.True(t, tracked)
	require.Zero(t, inspector.killSessionCalls)
	require.Zero(t, inspector.restartSessionCalls)
	require.Zero(t, inspector.sendHeartbeatCalls)
	sig := <-detector.Settled()
	require.Equal(t, task.SettleSuspect, sig.Kind)
	require.False(t, sig.Final)
	require.False(t, sig.Ready)
	require.False(t, sig.WaitSatisfied)
	select {
	case <-detector.Stopped():
		t.Fatal("a quiet process has not stopped")
	default:
	}
}

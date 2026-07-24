package action

import (
	"testing"
)

// TestAddSessionWithCallback_FiresPerSession: a per-session callback registered
// via AddSessionWithCallback fires on that session's meaningful transition,
// alongside the global StateChangeCallback.
func TestAddSessionWithCallback_FiresPerSession(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("sess-1", SessionRunning, "A")

	var globalOld, globalNew SessionStatus
	var globalFired bool
	tm.StateChangeCallback = func(sid string, oldS, newS SessionStatus, output string) {
		globalFired = true
		globalOld, globalNew = oldS, newS
	}

	var perOld, perNew SessionStatus
	var perFired bool
	var perSID string
	tm.AddSessionWithCallback(session, func(sid string, oldS, newS SessionStatus, output string) {
		perFired = true
		perSID = sid
		perOld, perNew = oldS, newS
	})

	// Trigger Running → Completed (process exits).
	inspector.setProcess(false, false)
	tm.checkSession(session)

	if !perFired {
		t.Fatalf("per-session callback did not fire")
	}
	if perSID != "sess-1" || perOld != SessionRunning || perNew != SessionCompleted {
		t.Errorf("per-session cb args = (%s,%s→%s), want (sess-1,running→completed)", perSID, perOld, perNew)
	}
	if !globalFired || globalOld != SessionRunning || globalNew != SessionCompleted {
		t.Errorf("global cb should also fire with running→completed; got fired=%v %s→%s", globalFired, globalOld, globalNew)
	}
}

// TestAddSessionWithCallback_FiresWithoutGlobal: the per-session callback fires
// even when NO global StateChangeCallback is registered (gate is decoupled from
// the global callback's presence).
func TestAddSessionWithCallback_FiresWithoutGlobal(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector) // no global StateChangeCallback

	session := newTestSession("sess-2", SessionRunning, "A")
	var fired bool
	tm.AddSessionWithCallback(session, func(sid string, oldS, newS SessionStatus, output string) {
		fired = true
	})

	inspector.setProcess(false, false)
	tm.checkSession(session)

	if !fired {
		t.Fatalf("per-session callback should fire even without a global callback")
	}
}

// TestAddSessionWithCallback_RemovedOnRemoveSession: RemoveSession drops the
// per-session callback so it no longer fires.
func TestAddSessionWithCallback_RemovedOnRemoveSession(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := newTestMonitor(inspector)

	session := newTestSession("sess-3", SessionRunning, "A")
	var fired bool
	tm.AddSessionWithCallback(session, func(sid string, oldS, newS SessionStatus, output string) {
		fired = true
	})
	tm.RemoveSession("sess-3")

	// Re-add plainly (no callback) and transition; the old per-session cb must not fire.
	tm.AddSession(session)
	inspector.setProcess(false, false)
	tm.checkSession(session)

	if fired {
		t.Errorf("per-session callback fired after RemoveSession")
	}
}

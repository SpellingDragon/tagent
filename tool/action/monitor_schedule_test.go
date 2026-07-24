package action

import (
	"testing"
	"time"
)

// TestWithMonitorConfig_ScheduleParams: MonitorConfig schedule params flow into
// the monitor's PollSchedule (4.2).
func TestWithMonitorConfig_ScheduleParams(t *testing.T) {
	tm := NewTmuxMonitor(WithMonitorConfig(MonitorConfig{
		Interval:      time.Second,
		DenseDuration: 5 * time.Second,
		BackoffFactor: 3,
		MaxInterval:   30 * time.Second,
	}))
	if tm.schedule.DenseInterval != time.Second {
		t.Errorf("DenseInterval = %v, want 1s (from Interval)", tm.schedule.DenseInterval)
	}
	if tm.schedule.DenseDuration != 5*time.Second {
		t.Errorf("DenseDuration = %v, want 5s", tm.schedule.DenseDuration)
	}
	if tm.schedule.BackoffFactor != 3 {
		t.Errorf("BackoffFactor = %v, want 3", tm.schedule.BackoffFactor)
	}
	if tm.schedule.MaxInterval != 30*time.Second {
		t.Errorf("MaxInterval = %v, want 30s", tm.schedule.MaxInterval)
	}
}

func adaptiveTestMonitor(inspector *mockInspector) *TmuxMonitor {
	tm := newTestMonitor(inspector)
	tm.schedule = DefaultPollSchedule() // dense 1s/10s, ×2 backoff, cap 60s
	tm.nextPoll = make(map[string]time.Time)
	return tm
}

// TestReschedule_DenseToBackoff: a young session reschedules at the dense
// interval; an old one backs off to a larger interval.
func TestReschedule_DenseToBackoff(t *testing.T) {
	tm := adaptiveTestMonitor(&mockInspector{processExists: true})
	now := time.Now()

	young := newTestSession("y", SessionRunning, "A")
	young.CreatedAt = now
	tm.sessions["y"] = young
	tm.rescheduleSession(young, now)
	if got := tm.nextPoll["y"].Sub(now); got != tm.schedule.DenseInterval {
		t.Errorf("young interval = %v, want dense %v", got, tm.schedule.DenseInterval)
	}

	old := newTestSession("o", SessionRunning, "A")
	old.CreatedAt = now.Add(-100 * time.Second)
	tm.sessions["o"] = old
	tm.rescheduleSession(old, now)
	if got := tm.nextPoll["o"].Sub(now); got < 30*time.Second {
		t.Errorf("old interval = %v, want backed off (≥30s)", got)
	}
}

// TestReschedule_StableSparse: a stable (service-ready) session polls at the
// sparsest cadence (MaxInterval), regardless of age.
func TestReschedule_StableSparse(t *testing.T) {
	tm := adaptiveTestMonitor(&mockInspector{processExists: true})
	now := time.Now()
	s := newTestSession("s", SessionStable, "A")
	s.CreatedAt = now // young, but stable
	tm.sessions["s"] = s
	tm.rescheduleSession(s, now)
	if got := tm.nextPoll["s"].Sub(now); got != tm.schedule.MaxInterval {
		t.Errorf("stable interval = %v, want MaxInterval %v", got, tm.schedule.MaxInterval)
	}
}

// TestCheckAllSessions_SkipsNotDue: a session whose nextPoll is in the future is
// not polled (the inspector is not touched).
func TestCheckAllSessions_SkipsNotDue(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := adaptiveTestMonitor(inspector)
	s := newTestSession("s", SessionRunning, "A")
	s.CreatedAt = time.Now()
	tm.sessions["s"] = s
	tm.nextPoll["s"] = time.Now().Add(time.Hour) // not due

	inspector.resetCallCounters()
	tm.checkAllSessions()
	if inspector.processExistsCalls != 0 {
		t.Errorf("not-due session should be skipped; processExistsCalls = %d", inspector.processExistsCalls)
	}
}

// TestCheckAllSessions_PollsDue: a due session is polled and rescheduled.
func TestCheckAllSessions_PollsDue(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	tm := adaptiveTestMonitor(inspector)
	s := newTestSession("s", SessionRunning, "A")
	s.CreatedAt = time.Now()
	tm.sessions["s"] = s
	tm.nextPoll["s"] = time.Now().Add(-time.Second) // due

	inspector.resetCallCounters()
	tm.checkAllSessions()
	if inspector.processExistsCalls == 0 {
		t.Errorf("due session should be polled")
	}
	if !tm.nextPoll["s"].After(time.Now()) {
		t.Errorf("due session should be rescheduled into the future")
	}
}

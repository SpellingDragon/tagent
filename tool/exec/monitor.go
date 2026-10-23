// 契约: docs/wiki/tool/tool-architecture.md#tmux-monitor
package exec

import (
	"bytes"
	"context"
	"sync"
	"time"
)

// MonitorStatus is the silence classification of one monitored execution: the
// watch a fresh frame is held under, the stable a quiet frame reaches at the
// stable threshold, and the suspect a live but silent execution reaches at its
// fake-dead threshold.
type MonitorStatus string

const (
	// MonitorWatch is the state of an execution whose observed frame has not
	// stood quiet for the stable threshold.
	MonitorWatch MonitorStatus = "watch"
	// MonitorStable is the state of an execution whose observed frame has stood
	// quiet for the stable threshold. A stopped execution's final frame is stable.
	MonitorStable MonitorStatus = "stable"
	// MonitorSuspect is the state of a live execution whose observed frame has
	// stood quiet for the fake-dead threshold.
	MonitorSuspect MonitorStatus = "suspect"
)

// Monitor reads the silence of monitored executions through the executor behind
// them: one poll captures the recorded frame, probes the execution and classifies
// the quiet it has stood in against the monitor rhythm. A caller polls each
// execution at the interval of its rhythm.
type Monitor struct {
	executor Executor
	rhythm   MonitorConfig
	now      func() time.Time
	mu       sync.Mutex
	watched  map[ResourceKey]*watchedExecution
}

// watchedExecution is the frame and silence one monitored execution was last
// observed under.
type watchedExecution struct {
	frame     []byte
	changedAt time.Time
	status    MonitorStatus
}

// NewMonitor returns the monitor of the executions behind one executor, judging
// silence under the given rhythm. A rhythm field the caller leaves at zero takes
// the default of that field.
func NewMonitor(executor Executor, rhythm MonitorConfig) *Monitor {
	return &Monitor{
		executor: executor,
		rhythm:   rhythmWithDefaults(rhythm),
		now:      time.Now,
		watched:  map[ResourceKey]*watchedExecution{},
	}
}

// Poll observes one execution once and reports the silence classification of
// that observation. The first poll anchors the observed frame; a frame that
// changed since the previous poll resets the silence. A stopped execution is
// never suspect.
func (m *Monitor) Poll(ctx context.Context, key ResourceKey) (MonitorStatus, error) {
	frame, err := m.executor.Capture(ctx, key)
	if err != nil {
		return "", err
	}
	observed, err := m.executor.Probe(ctx, key)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	watched, ok := m.watched[key]
	if !ok {
		watched = &watchedExecution{frame: frame, changedAt: now}
		m.watched[key] = watched
	}
	if !bytes.Equal(watched.frame, frame) {
		watched.frame = frame
		watched.changedAt = now
	}
	watched.status = m.classify(observed.State, now.Sub(watched.changedAt))
	return watched.status, nil
}

// Status reports the last classification of one execution and whether it was
// observed at all.
func (m *Monitor) Status(key ResourceKey) (MonitorStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	watched, ok := m.watched[key]
	if !ok {
		return "", false
	}
	return watched.status, true
}

// classify reports the silence classification of one observation: a live
// execution quiet for its fake-dead threshold is suspect, a quiet frame that
// reached the stable threshold is stable, and every other observation is watched.
func (m *Monitor) classify(state ProbeState, quiet time.Duration) MonitorStatus {
	switch {
	case state == ProbeAlive && m.rhythm.FakeDeadDuration > 0 && quiet >= m.rhythm.FakeDeadDuration:
		return MonitorSuspect
	case m.rhythm.StableDuration > 0 && quiet >= m.rhythm.StableDuration:
		return MonitorStable
	default:
		return MonitorWatch
	}
}

// rhythmWithDefaults returns the rhythm with every zero field taken from the
// default rhythm of its own field.
func rhythmWithDefaults(rhythm MonitorConfig) MonitorConfig {
	defaults := DefaultMonitorConfig
	if rhythm.Interval <= 0 {
		rhythm.Interval = defaults.Interval
	}
	if rhythm.StableDuration <= 0 {
		rhythm.StableDuration = defaults.StableDuration
	}
	if rhythm.InteractiveStableDuration <= 0 {
		rhythm.InteractiveStableDuration = defaults.InteractiveStableDuration
	}
	if rhythm.FakeDeadDuration <= 0 {
		rhythm.FakeDeadDuration = defaults.FakeDeadDuration
	}
	return rhythm
}

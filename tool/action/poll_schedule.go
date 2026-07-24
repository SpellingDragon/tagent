package action

import "time"

// PollSchedule defines an adaptive per-task poll cadence: a dense phase (poll
// frequently right after spawn, for fast detection of quick commands) followed
// by a geometric backoff phase (sparse polling for long-running / alive-detached
// tasks), capped at a maximum interval. It replaces a single fixed Interval.
type PollSchedule struct {
	DenseInterval time.Duration // poll cadence during the dense phase
	DenseDuration time.Duration // how long the dense phase lasts
	BackoffFactor float64       // interval growth per step after the dense phase (>= 1)
	MaxInterval   time.Duration // upper cap on the interval
}

// DefaultPollSchedule returns the default adaptive schedule. The dense phase
// duration approximates the retired sync_wait (so quick commands still settle
// inline), and backoff caps at a minute for long-lived sessions.
func DefaultPollSchedule() PollSchedule {
	return PollSchedule{
		DenseInterval: 1 * time.Second,
		DenseDuration: 10 * time.Second,
		BackoffFactor: 2,
		MaxInterval:   60 * time.Second,
	}
}

// intervalForAge returns the poll interval in effect at the given task age.
// During the dense phase it is DenseInterval; afterwards it grows geometrically
// by BackoffFactor, capped at MaxInterval. The result is derived by simulating
// the poll progression so it matches what the scheduler actually applies.
func (s PollSchedule) intervalForAge(age time.Duration) time.Duration {
	if s.DenseInterval <= 0 {
		return s.DenseInterval
	}
	if age < s.DenseDuration {
		return s.DenseInterval
	}

	factor := s.BackoffFactor
	if factor < 1 {
		factor = 2
	}

	interval := float64(s.DenseInterval)
	// Walk the poll timeline from the end of the dense phase, growing the
	// interval geometrically, until the cumulative time reaches age.
	t := float64(s.DenseDuration)
	for t < float64(age) {
		interval *= factor
		if s.MaxInterval > 0 && interval >= float64(s.MaxInterval) {
			interval = float64(s.MaxInterval)
			break
		}
		t += interval
	}
	if s.MaxInterval > 0 && interval > float64(s.MaxInterval) {
		interval = float64(s.MaxInterval)
	}
	return time.Duration(interval)
}

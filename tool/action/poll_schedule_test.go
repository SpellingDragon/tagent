package action

import (
	"testing"
	"time"
)

func TestPollSchedule_IntervalForAge(t *testing.T) {
	s := DefaultPollSchedule() // dense 1s for 10s, ×2 backoff, cap 60s
	cases := []struct {
		age  time.Duration
		want time.Duration
	}{
		{0, time.Second},                       // dense
		{5 * time.Second, time.Second},         // dense
		{9 * time.Second, time.Second},         // dense (just before end)
		{11 * time.Second, 2 * time.Second},    // first backoff step
		{15 * time.Second, 4 * time.Second},    // second backoff step
		{1000 * time.Second, 60 * time.Second}, // capped at MaxInterval
	}
	for _, c := range cases {
		if got := s.intervalForAge(c.age); got != c.want {
			t.Errorf("intervalForAge(%s) = %s, want %s", c.age, got, c.want)
		}
	}
}

// TestPollSchedule_MonotonicNonDecreasing: the interval never shrinks as age
// grows and never exceeds MaxInterval.
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

// TestPollSchedule_DegenerateInterval: a non-positive dense interval is returned
// as-is (no divide/loop hazard).
func TestPollSchedule_DegenerateInterval(t *testing.T) {
	s := PollSchedule{DenseInterval: 0}
	if got := s.intervalForAge(5 * time.Second); got != 0 {
		t.Errorf("degenerate schedule should return 0, got %s", got)
	}
}

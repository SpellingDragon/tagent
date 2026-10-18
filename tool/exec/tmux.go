package exec

import (
	"errors"
	"fmt"
	osexec "os/exec"
	"time"
)

// 契约: docs/wiki/tool/tool-architecture.md#tmux-monitor
//
// KindTmux names the tmux session family of execution resources: the one family
// whose availability rests on the real tmux binary being reachable on PATH.
const KindTmux = "tmux"

// ErrTmuxUnavailable reports an acquisition refused because the declared kind
// needs the tmux capability and the system does not provide it.
var ErrTmuxUnavailable = errors.New("exec: the tmux capability is not available on this system")

// IsTmuxAvailable reports whether a tmux binary is reachable on PATH. The
// verdict comes from a real PATH lookup at every call, so a deployment without
// tmux reads as unavailable and no construction can stand in for the probe.
func IsTmuxAvailable() bool {
	_, err := osexec.LookPath("tmux")
	return err == nil
}

// MonitorConfig is the monitor rhythm one execution generation runs under: the
// polling interval beside the quietness thresholds the monitor judges output
// against.
type MonitorConfig struct {
	// Interval is the base polling rhythm of the monitor.
	Interval time.Duration
	// StableDuration is the quietness threshold of a plain session.
	StableDuration time.Duration
	// InteractiveStableDuration is the quietness threshold of a TUI session.
	InteractiveStableDuration time.Duration
	// FakeDeadDuration is the threshold beyond which a quiet pane with a live
	// process is treated as unresponsive.
	FakeDeadDuration time.Duration
}

// DefaultMonitorConfig is the rhythm an acquisition runs under when its caller
// declared no monitor configuration.
var DefaultMonitorConfig = MonitorConfig{
	Interval:                  30 * time.Second,
	StableDuration:            60 * time.Second,
	InteractiveStableDuration: 90 * time.Second,
	FakeDeadDuration:          120 * time.Second,
}

// MonitorConfigFrom reads the monitor rhythm out of a configuration map: the
// four supported keys take duration strings, and a key outside them — the
// retired tool-level compress section among them — is ignored rather than
// consumed or refused. A supported key that carries no duration is reported by
// name beside the default rhythm, so a malformed setting is visible instead of
// silently replaced.
func MonitorConfigFrom(values map[string]any) (MonitorConfig, error) {
	rhythm := DefaultMonitorConfig
	for key, set := range map[string]*time.Duration{
		"interval":                    &rhythm.Interval,
		"stable_duration":             &rhythm.StableDuration,
		"interactive_stable_duration": &rhythm.InteractiveStableDuration,
		"fake_dead_duration":          &rhythm.FakeDeadDuration,
	} {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			parsed, err := time.ParseDuration(typed)
			if err != nil {
				return DefaultMonitorConfig, fmt.Errorf("exec: monitor %q is not a duration: %q", key, typed)
			}
			*set = parsed
		case time.Duration:
			*set = typed
		default:
			return DefaultMonitorConfig, fmt.Errorf("exec: monitor %q is not a duration: %v", key, typed)
		}
	}
	return rhythm, nil
}

// DegradeError is the named refusal of an acquisition whose declared kind needs
// a system capability the system does not provide. The monitor rhythm of the
// refused call rides the refusal, so a configuration reaches its reader even on
// the degrade path instead of disappearing with the call.
type DegradeError struct {
	// Key is the resource the acquisition was refused for.
	Key ResourceKey
	// Rhythm is the monitor rhythm the refused call carried.
	Rhythm MonitorConfig
	// Reason is the readable cause of the refusal.
	Reason string
}

// Error reports the refusal beside the resource it applies to.
func (e *DegradeError) Error() string {
	return fmt.Sprintf("exec: %s: %s", e.Key, e.Reason)
}

// Unwrap reports the capability absence behind the refusal, so a caller
// classifies it with errors.Is.
func (e *DegradeError) Unwrap() error { return ErrTmuxUnavailable }

package action

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// trimToLineOffset returns the lines of s after the first n lines (the
// current round's increment view); if s has fewer lines than n (scrollback
// shifted), s is returned unchanged rather than losing output.
func trimToLineOffset(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[n:], "\n")
}

// StatusToSettle maps a tmux SessionStatus to a task-layer settle kind.
//
// It returns (kind, true) when the status is a settle point, or ("", false) for
// intermediate/suppressed states (Running, FakeDead, FakeAlive) that are NOT
// settles. The detector only makes the deterministic classification here; the
// LLM interprets ambiguous kinds (stable vs suspect) downstream.
//
//	completed → SettleCompleted (process exited — definitely done)
//	error → SettleCompleted (settled with failure; caller attaches Err)
//	stable → SettleStable (output stable, process alive — usable/waiting)
//	timed_out → SettleSuspect (quiet beyond fake-dead threshold — likely hung)
func StatusToSettle(s SessionStatus) (task.SettleKind, bool) {
	switch s {
	case SessionCompleted, SessionError:
		return task.SettleCompleted, true
	case SessionStable:
		return task.SettleStable, true
	case SessionTimedOut:
		return task.SettleSuspect, true
	default:
		return "", false
	}
}

// isTerminalStatus reports whether a status ends the task's settle stream (no
// further signals expected). Stable is NOT terminal — an alive session may later
// complete or (for TUI) time out.
func isTerminalStatus(s SessionStatus) bool {
	switch s {
	case SessionCompleted, SessionError, SessionTimedOut:
		return true
	default:
		return false
	}
}

// TmuxSettleDetector adapts a single tmux session's state-change stream into an
// task.SettleDetector. It is fed monitor transitions via OnStateChange (wired
// to the monitor callback for this session in the ActionTool integration) and
// emits task.SettleSignal on its channel, closing it on a terminal status.
//
// LIFETIME: the detector is bound to the SESSION, not to a round. A resume
// round does not replace it — Rearm resets the round state (output baseline +
// a fresh dense→detach timer) on the SAME detector, so the monitor callback
// wiring and the task-layer watch never change hands (no rebinding, no
// ordering discipline, no stale-signal cross-round risk).
type TmuxSettleDetector struct {
	sessionID string
	ch        chan task.SettleSignal
	cancelFn  func()
	closeOnce sync.Once
	reapOnce  sync.Once
	stop      chan struct{}
	denseDur  time.Duration

	mu       sync.Mutex
	detach   <-chan struct{}
	baseline int

	// watchRe Watch: pattern-triggered wakeups for resident sessions.
	// OnWatchOutput receives the WHOLE visible pane buffer each refresh (same
	// semantics as OnStateChange); the hit count is diffed against the last
	// snapshot, so pane scrolling/truncation degrades to "missed hits" rather
	// than corruption. Hits merge inside watchWindow (one pending signal max);
	// the cumulative count is reported in the signal.
	watchRe     *regexp.Regexp
	watchWindow time.Duration
	watchMu     sync.Mutex
	watchSeen   int
	watchHits   int
	watchLast   time.Time

	probeMu     sync.Mutex
	probeFailed bool

	// paneStatusFn reads the dead pane's exit status on demand (failure-polarity
	// passthrough D2, pull mode). Set via SetPaneStatusReader; nil means the
	// detector cannot resolve exit codes (Err text degrades to "unresolvable").
	// Pulled at OnStateChange time — before reap() — so remain-on-exit keeps the
	// pane and its status readable.
	paneStatusFn func() (code int, known bool)
}

// SetPaneStatusReader wires the exit-status pull used to enrich a terminal
// SessionError signal with a concrete exit code. Called once at construction by
// the ActionTool integration with a closure over the executor + session id.
func (d *TmuxSettleDetector) SetPaneStatusReader(fn func() (code int, known bool)) {
	d.paneStatusFn = fn
}

// NewTmuxSettleDetector creates a detector for the given session. cancelFn, when
// non-nil, reaps the underlying tmux session (kills it + drops monitor
// tracking); it runs at most once, on Cancel or on natural process death. An
// optional denseDuration overrides the default dense phase after which, if the
// session has not settled, the detector signals detach (→ async ack).
func NewTmuxSettleDetector(sessionID string, cancelFn func(), denseDuration ...time.Duration) *TmuxSettleDetector {
	d := &TmuxSettleDetector{
		sessionID: sessionID,
		ch:        make(chan task.SettleSignal, 8),
		cancelFn:  cancelFn,
		stop:      make(chan struct{}),
	}
	d.denseDur = DefaultPollSchedule().DenseDuration
	if len(denseDuration) > 0 && denseDuration[0] > 0 {
		d.denseDur = denseDuration[0]
	}
	d.detach = task.DetachAfter(d.denseDur, d.stop)
	return d
}

// Rearm resets the detector for a resume round on the SAME session: a new
// output baseline (settle outputs become this round's increment) and a fresh
// dense→detach timer (the resumed round gets its own sync-wait window).
func (d *TmuxSettleDetector) Rearm(baseline int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baseline = baseline
	d.detach = task.DetachAfter(d.denseDur, d.stop)
}

// Detached implements task.SettleDetector: fires at the dense→sparse boundary
// of the CURRENT round (Rearm re-creates it).
func (d *TmuxSettleDetector) Detached() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.detach
}

// Settled implements task.SettleDetector.
func (d *TmuxSettleDetector) Settled() <-chan task.SettleSignal { return d.ch }

// Done returns a channel closed when the detector closes (terminal status or
// Cancel). Sidecar loops (probe) select on it to exit.
func (d *TmuxSettleDetector) Done() <-chan struct{} { return d.stop }

// Stopped implements task.SettleDetector. For a tmux-backed run the
// producer is an external session, so its stop credential is the same event that
// closes the detector: Cancel reaps the session synchronously (kill + drop
// monitor tracking) and then closes this channel; a terminal status closes it
// after the last settle was emitted. Either way nothing behind the detector can
// still touch shared state once it fires.
func (d *TmuxSettleDetector) Stopped() <-chan struct{} { return d.stop }

// Cancel implements task.SettleDetector: reaps the session and closes the
// settle stream.
func (d *TmuxSettleDetector) Cancel() {
	d.reap()
	d.close()
}

// reap kills the underlying tmux session and drops monitor tracking, at most
// once — whether triggered by Cancel or by natural process death.
func (d *TmuxSettleDetector) reap() {
	if d.cancelFn != nil {
		d.reapOnce.Do(d.cancelFn)
	}
}

// OnStateChange feeds a monitor state transition for this session. It emits a
// settle signal when newStatus is a settle point, and closes the stream on a
// terminal status. Non-settle transitions (e.g. → Running) are ignored. The
// output is trimmed to the current round's baseline (Rearm), so resumed
// rounds settle with their own increment, not the whole scrollback.
func (d *TmuxSettleDetector) OnStateChange(newStatus SessionStatus, output string) {
	kind, ok := StatusToSettle(newStatus)
	if !ok {
		return
	}
	d.mu.Lock()
	baseline := d.baseline
	d.mu.Unlock()
	output = trimToLineOffset(output, baseline)
	var err error
	var exitCode int
	if newStatus == SessionError {
		// Pull the exit code at detection time (before reap, remain-on-exit keeps
		// the pane). A concrete non-zero code carries failure polarity; an
		// unresolvable status (probe失明 / kill-escaped) still errors — "framework
		// blind ≠ task succeeded".
		if d.paneStatusFn != nil {
			code, known := d.paneStatusFn()
			if known && code != 0 {
				exitCode = code
				err = fmt.Errorf("tmux session %s exited with code %d", d.sessionID, code)
			} else {
				err = fmt.Errorf("tmux session %s entered error state (exit status unresolvable)", d.sessionID)
			}
		} else {
			err = fmt.Errorf("tmux session %s entered error state", d.sessionID)
		}
	}
	select {
	case d.ch <- task.SettleSignal{Kind: kind, Output: output, Err: err, ExitCode: exitCode}:
	default:
	}
	if isTerminalStatus(newStatus) {
		d.close()
		if newStatus == SessionCompleted || newStatus == SessionError {
			d.reap()
		}
	}
}

func (d *TmuxSettleDetector) close() {
	d.closeOnce.Do(func() {
		close(d.stop)
		close(d.ch)
	})
}

// SetWatch attaches a pattern-triggered wakeup to the detector (
// C1). The regex is matched against INCREMENTAL output fed via
// OnWatchOutput; hits inside window are merged (one pending signal max),
// with the cumulative hit count in the signal output — a log flood of 50
// ERRORs wakes the agent once with "xN", not 50 times.
func (d *TmuxSettleDetector) SetWatch(pattern string, window time.Duration) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("action: watch pattern: %w", err)
	}
	d.watchMu.Lock()
	defer d.watchMu.Unlock()
	d.watchRe = re
	if window <= 0 {
		window = 5 * time.Second
	}
	d.watchWindow = window
	return nil
}

// OnWatchOutput feeds the current visible pane buffer through the watch
// pattern. Called from the monitor's per-session callback on every refresh
// (including stable refreshes — a resident session's silence must not starve
// its watch). Snapshot-diff model: count hits in the WHOLE buffer, subtract
// the previous snapshot's count — no byte offsets, so pane scrollback
// rotation merely caps delta at 0 instead of mis-slicing content.
func (d *TmuxSettleDetector) OnWatchOutput(output string) {
	d.watchMu.Lock()
	re := d.watchRe
	if re == nil {
		d.watchMu.Unlock()
		return
	}
	hitsNow := len(re.FindAllString(output, -1))
	delta := hitsNow - d.watchSeen
	if delta < 0 {
		delta = 0
	}
	d.watchSeen = hitsNow
	if delta == 0 {
		d.watchMu.Unlock()
		return
	}
	d.watchHits += delta
	total := d.watchHits
	last := d.watchLast
	now := time.Now()
	if !last.IsZero() && now.Sub(last) < d.watchWindow {
		d.watchMu.Unlock()
		return
	}
	d.watchLast = now
	d.watchMu.Unlock()

	select {
	case d.ch <- task.SettleSignal{
		Kind:   task.SettleWatch,
		Output: fmt.Sprintf("watch pattern %q hit x%d (cumulative)", re.String(), delta),
		Err:    fmt.Errorf("%d matches", total),
	}:
	default:
	}
}

// EmitProbeResult reports a liveness-probe outcome. A failure
// emits a watch-kind signal ONCE; repeated failures stay silent until a
// success resets the latch. This is the resident-session "service died"
// wakeup: the agent learns within one probe interval, not when a human notices.
func (d *TmuxSettleDetector) EmitProbeResult(ok bool, detail string) {
	d.probeMu.Lock()
	if ok {
		d.probeFailed = false
		d.probeMu.Unlock()
		return
	}
	if d.probeFailed {
		d.probeMu.Unlock()
		return
	}
	d.probeFailed = true
	d.probeMu.Unlock()

	select {
	case d.ch <- task.SettleSignal{
		Kind:   task.SettleWatch,
		Output: "probe FAILED: " + detail,
		Err:    fmt.Errorf("liveness probe failed: %s", detail),
	}:
	default:
	}
}

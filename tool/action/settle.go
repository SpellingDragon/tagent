package action

import (
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent"
)

// StatusToSettle maps a tmux SessionStatus to a task-layer settle kind.
//
// It returns (kind, true) when the status is a settle point, or ("", false) for
// intermediate/suppressed states (Running, FakeDead, FakeAlive) that are NOT
// settles. The detector only makes the deterministic classification here; the
// LLM interprets ambiguous kinds (stable vs suspect) downstream.
//
//	completed → SettleCompleted   (process exited — definitely done)
//	error     → SettleCompleted   (settled with failure; caller attaches Err)
//	stable    → SettleStable      (output stable, process alive — usable/waiting)
//	timed_out → SettleSuspect     (quiet beyond fake-dead threshold — likely hung)
func StatusToSettle(s SessionStatus) (agent.SettleKind, bool) {
	switch s {
	case SessionCompleted, SessionError:
		return agent.SettleCompleted, true
	case SessionStable:
		return agent.SettleStable, true
	case SessionTimedOut:
		return agent.SettleSuspect, true
	default: // SessionRunning, SessionFakeDead, SessionFakeAlive
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
// agent.SettleDetector. It is fed monitor transitions via OnStateChange (wired
// to the monitor callback for this session in the ActionTool integration) and
// emits agent.SettleSignal on its channel, closing it on a terminal status.
type TmuxSettleDetector struct {
	sessionID string
	ch        chan agent.SettleSignal
	cancelFn  func()
	closeOnce sync.Once
	reapOnce  sync.Once
	stop      chan struct{}   // closed on close(): stops the detach timer
	detach    <-chan struct{} // fires at the dense→sparse boundary (sync→async)
}

// NewTmuxSettleDetector creates a detector for the given session. cancelFn, when
// non-nil, reaps the underlying tmux session (kills it + drops monitor
// tracking); it runs at most once, on Cancel or on natural process death. An
// optional denseDuration overrides the default dense phase after which, if the
// session has not settled, the detector signals detach (→ async ack).
func NewTmuxSettleDetector(sessionID string, cancelFn func(), denseDuration ...time.Duration) *TmuxSettleDetector {
	d := &TmuxSettleDetector{
		sessionID: sessionID,
		ch:        make(chan agent.SettleSignal, 8),
		cancelFn:  cancelFn,
		stop:      make(chan struct{}),
	}
	dd := DefaultPollSchedule().DenseDuration
	if len(denseDuration) > 0 && denseDuration[0] > 0 {
		dd = denseDuration[0]
	}
	d.detach = agent.DetachAfter(dd, d.stop)
	return d
}

// Detached implements agent.SettleDetector: fires at the dense→sparse boundary.
func (d *TmuxSettleDetector) Detached() <-chan struct{} { return d.detach }

// Settled implements agent.SettleDetector.
func (d *TmuxSettleDetector) Settled() <-chan agent.SettleSignal { return d.ch }

// Cancel implements agent.SettleDetector: reaps the session and closes the
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
// terminal status. Non-settle transitions (e.g. → Running) are ignored.
func (d *TmuxSettleDetector) OnStateChange(newStatus SessionStatus, output string) {
	kind, ok := StatusToSettle(newStatus)
	if !ok {
		return
	}
	var err error
	if newStatus == SessionError {
		err = fmt.Errorf("tmux session %s entered error state", d.sessionID)
	}
	select {
	case d.ch <- agent.SettleSignal{Kind: kind, Output: output, Err: err}:
	default: // stream buffer full — drop extra (LLM already has recent signal)
	}
	if isTerminalStatus(newStatus) {
		d.close()
		// Reap the tmux session when the process actually ended (its pane is
		// dead), so completed/errored command sessions don't accumulate — the
		// output is already captured in the emitted signal. A merely-quiet
		// timed-out session may still be alive, so it is NOT auto-reaped.
		if newStatus == SessionCompleted || newStatus == SessionError {
			d.reap()
		}
	}
}

func (d *TmuxSettleDetector) close() {
	d.closeOnce.Do(func() {
		close(d.stop) // stop the detach timer — the session settled/terminated
		close(d.ch)
	})
}

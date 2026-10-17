// 契约: docs/wiki/tool/tool-architecture.md#action-tool
package action

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

func trimToLineOffset(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return ""
	}
	return strings.Join(lines[n:], "\n")
}

// StatusToSettle maps process states to terminal results or observations.
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

func isTerminalStatus(s SessionStatus) bool {
	return s == SessionCompleted || s == SessionError
}

// TmuxSettleDetector emits bounded observations and retains terminal receipts.
type TmuxSettleDetector struct {
	sourceTaskID  string
	sessionID     string
	resourceID    string
	ch            chan task.SettleSignal
	stop          chan struct{}
	stopped       chan struct{}
	stoppedOnce   sync.Once
	closeOnce     sync.Once
	cancelOnce    sync.Once
	signalMu      sync.Mutex
	closed        bool
	seq           uint64
	terminal      *task.SettleSignal
	acked         bool
	cancelFn      func()
	stopFn        func() bool
	ackFn         func() error
	startFn       func() error
	startOnce     sync.Once
	beginRoundFn  func(uint64)
	paneStatusFn  func() (int, bool)
	denseDur      time.Duration
	mu            sync.Mutex
	detach        <-chan struct{}
	baseline      int
	round         uint64
	mode          SessionMode
	readyRe       *regexp.Regexp
	inputRe       *regexp.Regexp
	ready         bool
	inputSnapshot string
	watchRe       *regexp.Regexp
	watchWindow   time.Duration
	watchSnapshot string
	watchHits     int
	watchLast     time.Time
	probeFailed   bool
}

// NewTmuxSettleDetector constructs a detector with a bounded observation mailbox.
func NewTmuxSettleDetector(sessionID string, cancelFn func(), denseDuration ...time.Duration) *TmuxSettleDetector {
	d := &TmuxSettleDetector{sessionID: sessionID, ch: make(chan task.SettleSignal, 8), stop: make(chan struct{}), stopped: make(chan struct{}), cancelFn: cancelFn, denseDur: DefaultPollSchedule().DenseDuration, round: 1}
	if len(denseDuration) > 0 && denseDuration[0] > 0 {
		d.denseDur = denseDuration[0]
	}
	d.detach = task.DetachAfter(d.denseDur, d.stop)
	return d
}

// ConfigureIntent sets explicit readiness and input evidence requirements.
func (d *TmuxSettleDetector) ConfigureIntent(mode SessionMode, readyPattern, inputPattern string) error {
	var ready, input *regexp.Regexp
	var err error
	if readyPattern != "" {
		ready, err = regexp.Compile(readyPattern)
		if err != nil {
			return fmt.Errorf("action: ready_pattern: %w", err)
		}
	}
	if inputPattern != "" {
		input, err = regexp.Compile(inputPattern)
		if err != nil {
			return fmt.Errorf("action: input_pattern: %w", err)
		}
	}
	d.mu.Lock()
	d.mode, d.readyRe, d.inputRe = mode, ready, input
	d.mu.Unlock()
	return nil
}

// SetPaneStatusReader sets the exit status reader for confirmed dead panes.
func (d *TmuxSettleDetector) SetPaneStatusReader(fn func() (int, bool)) { d.paneStatusFn = fn }

// BeginRound fences subsequent producer observations to round.
func (d *TmuxSettleDetector) BeginRound(round uint64) {
	d.mu.Lock()
	if d.round != round {
		d.round, d.ready, d.inputSnapshot = round, false, ""
	}
	fn := d.beginRoundFn
	d.mu.Unlock()
	if fn != nil {
		fn(round)
	}
}

// ProducerID identifies the immutable external resource.
func (d *TmuxSettleDetector) ProducerID() string { return d.resourceID }

// ProducerRound returns the producer round used by emitted receipts.
func (d *TmuxSettleDetector) ProducerRound() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.round
}

// RestoreRound initializes restored receipt numbering before monitoring starts.
func (d *TmuxSettleDetector) RestoreRound(round, seq uint64) {
	if round == 0 {
		round = 1
	}
	d.BeginRound(round)
	d.signalMu.Lock()
	d.seq = seq
	d.signalMu.Unlock()
}

// RoundBaseline returns the output line baseline for the next durable binding.
func (d *TmuxSettleDetector) RoundBaseline() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.baseline
}

// Rearm establishes a resumed output baseline and dense observation window.
func (d *TmuxSettleDetector) Rearm(baseline int) {
	d.mu.Lock()
	d.baseline = baseline
	d.detach = task.DetachAfter(d.denseDur, d.stop)
	d.ready, d.inputSnapshot = false, ""
	d.mu.Unlock()
}

// Detached implements task.SettleDetector.
func (d *TmuxSettleDetector) Detached() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.detach
}

// Settled implements task.SettleDetector.
func (d *TmuxSettleDetector) Settled() <-chan task.SettleSignal { return d.ch }

// Done closes when the observation stream ends.
func (d *TmuxSettleDetector) Done() <-chan struct{} { return d.stop }

// Stopped closes only after the external producer is confirmed stopped.
func (d *TmuxSettleDetector) Stopped() <-chan struct{} { return d.stopped }

func (d *TmuxSettleDetector) confirmStopped() { d.stoppedOnce.Do(func() { close(d.stopped) }) }

// Start activates a prepared producer after its durable task binding.
func (d *TmuxSettleDetector) Start() {
	d.startOnce.Do(func() {
		if d.startFn == nil {
			return
		}
		if err := d.startFn(); err != nil {
			if d.stopFn != nil && d.stopFn() {
				d.confirmStopped()
			}
			d.emit(task.SettleSignal{Kind: task.SettleFailed, Err: err, Final: true})
		}
	})
}

// Cancel requests termination and records only confirmed producer stoppage.
func (d *TmuxSettleDetector) Cancel() {
	d.cancelOnce.Do(func() {
		if d.stopFn != nil {
			if d.stopFn() {
				d.confirmStopped()
			}
		} else if d.cancelFn != nil {
			d.cancelFn()
		} else {
			d.confirmStopped()
		}
		d.close()
	})
}

// AcknowledgeSignal releases terminal evidence for the exact accepted receipt.
func (d *TmuxSettleDetector) AcknowledgeSignal(sig task.SettleSignal) {
	d.signalMu.Lock()
	t := d.terminal
	if t == nil || d.acked || sig.Seq != t.Seq || sig.RoundID != t.RoundID || sig.ProducerRound != t.ProducerRound || sig.SourceID != t.SourceID {
		d.signalMu.Unlock()
		return
	}
	d.acked = true
	d.signalMu.Unlock()
	if d.ackFn != nil {
		if err := d.ackFn(); err != nil {
			d.signalMu.Lock()
			d.acked = false
			d.signalMu.Unlock()
		}
	} else if d.cancelFn != nil {
		d.cancelOnce.Do(d.cancelFn)
	}
}

// OnStateChange accepts a state observed in the current producer round.
func (d *TmuxSettleDetector) OnStateChange(status SessionStatus, output string) {
	d.OnObservation(d.ProducerRound(), status, output)
}

// OnObservation accepts a round-fenced monitor snapshot.
func (d *TmuxSettleDetector) OnObservation(round uint64, status SessionStatus, output string) {
	d.mu.Lock()
	if round != d.round {
		d.mu.Unlock()
		return
	}
	baseline := d.baseline
	d.mu.Unlock()
	increment := trimToLineOffset(output, baseline)
	if !isTerminalStatus(status) {
		d.onOutput(round, increment)
	}
	kind, ok := StatusToSettle(status)
	if !ok {
		return
	}
	sig := task.SettleSignal{Kind: kind, Output: increment, RoundID: round, ProducerRound: round, Final: isTerminalStatus(status)}
	if status == SessionStable {
		sig.Observation = "stable"
	}
	if status == SessionTimedOut {
		sig.Observation = "suspect"
	}
	if status == SessionError {
		code, known := 0, false
		if d.paneStatusFn != nil {
			code, known = d.paneStatusFn()
		}
		if known {
			sig.ExitCode = code
			sig.Err = fmt.Errorf("tmux session %s exited with code %d", d.sessionID, code)
		} else {
			sig.Err = fmt.Errorf("tmux session %s ended with unresolvable exit status", d.sessionID)
		}
	}
	if isTerminalStatus(status) {
		d.confirmStopped()
	}
	d.emit(sig)
}

var defaultInputPrompt = regexp.MustCompile(`(?m)(?:^|\n)(?:>>>|\.\.\.|[[:alnum:]_.~@/:-]*[>$#❯]) ?$`)

func (d *TmuxSettleDetector) onOutput(round uint64, output string) {
	d.mu.Lock()
	if round != d.round {
		d.mu.Unlock()
		return
	}
	ready := d.mode == ModeResident && !d.ready && d.readyRe != nil && d.readyRe.MatchString(output)
	if ready {
		d.ready = true
	}
	input := false
	if d.mode == ModeInteractive && output != "" && output != d.inputSnapshot {
		pattern := d.inputRe
		if pattern == nil {
			pattern = defaultInputPrompt
		}
		input = pattern.MatchString(strings.TrimRight(output, "\r\n"))
		if input {
			d.inputSnapshot = output
		}
	}
	d.mu.Unlock()
	if ready {
		d.emit(task.SettleSignal{Kind: task.SettleReady, Ready: true, WaitSatisfied: true, Output: output, RoundID: round, ProducerRound: round})
	}
	if input {
		d.emit(task.SettleSignal{Kind: task.SettleInput, WaitSatisfied: true, Output: output, RoundID: round, ProducerRound: round})
	}
}

func (d *TmuxSettleDetector) close() {
	d.signalMu.Lock()
	defer d.signalMu.Unlock()
	d.closeLocked()
}

func (d *TmuxSettleDetector) closeLocked() {
	if d.closed {
		return
	}
	d.closed = true
	d.closeOnce.Do(func() { close(d.stop) })
	close(d.ch)
}

func (d *TmuxSettleDetector) emit(sig task.SettleSignal) {
	if sig.RoundID == 0 {
		sig.RoundID = d.ProducerRound()
		sig.ProducerRound = sig.RoundID
	}
	d.signalMu.Lock()
	defer d.signalMu.Unlock()
	if d.closed {
		return
	}
	d.seq++
	sig.Seq, sig.ObservedAt = d.seq, time.Now()
	source := d.sourceTaskID
	if source == "" {
		source = d.sessionID
	}
	sig.SourceID = task.SignalSourceID(source, sig.RoundID, sig.Seq)
	terminal := (sig.Kind == task.SettleCompleted || sig.Kind == task.SettleFailed) && sig.Observation != "round_aborted"
	if terminal {
		copy := sig
		d.terminal = &copy
	}
	select {
	case d.ch <- sig:
	default:
		select {
		case <-d.ch:
		default:
		}
		d.ch <- sig
	}
	if terminal {
		d.closeLocked()
	}
}

// SetWatch configures an output observation pattern.
func (d *TmuxSettleDetector) SetWatch(pattern string, window time.Duration) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("action: watch pattern: %w", err)
	}
	if window <= 0 {
		window = 5 * time.Second
	}
	d.mu.Lock()
	d.watchRe, d.watchWindow = re, window
	d.mu.Unlock()
	return nil
}

// OnWatchOutput reports new matches without satisfying readiness or input waits.
func (d *TmuxSettleDetector) OnWatchOutput(output string) {
	d.OnWatchOutputRound(d.ProducerRound(), output)
}

// OnWatchOutputRound reports matches from a producer-fenced output snapshot.
func (d *TmuxSettleDetector) OnWatchOutputRound(round uint64, output string) {
	d.mu.Lock()
	if d.watchRe == nil || round != d.round {
		d.mu.Unlock()
		return
	}
	fresh := output
	if strings.HasPrefix(output, d.watchSnapshot) {
		fresh = output[len(d.watchSnapshot):]
	} else if output == d.watchSnapshot {
		fresh = ""
	}
	d.watchSnapshot = output
	hits := len(d.watchRe.FindAllString(fresh, -1))
	if hits == 0 {
		d.mu.Unlock()
		return
	}
	d.watchHits += hits
	total, pattern := d.watchHits, d.watchRe.String()
	now := time.Now()
	if !d.watchLast.IsZero() && now.Sub(d.watchLast) < d.watchWindow {
		d.mu.Unlock()
		return
	}
	d.watchLast = now
	d.mu.Unlock()
	d.emit(task.SettleSignal{Kind: task.SettleWatch, Observation: "watch", Output: fmt.Sprintf("watch pattern %q hit x%d (cumulative)", pattern, total), RoundID: round, ProducerRound: round})
}

// EmitProbeResult reports failure and recovery edges and evidenced readiness.
func (d *TmuxSettleDetector) EmitProbeResult(ok bool, detail string) {
	d.EmitProbeResultRound(d.ProducerRound(), ok, detail)
}

// EmitProbeResultRound rejects probe outcomes from previous producer rounds.
func (d *TmuxSettleDetector) EmitProbeResultRound(round uint64, ok bool, detail string) {
	d.mu.Lock()
	if round != d.round {
		d.mu.Unlock()
		return
	}
	failed := d.probeFailed
	if ok {
		d.probeFailed = false
		ready := d.mode == ModeResident && !d.ready
		if ready {
			d.ready = true
		}
		d.mu.Unlock()
		if failed {
			d.emit(task.SettleSignal{Kind: task.SettleWatch, Observation: "probe_recovered", Output: "probe RECOVERED", RoundID: round, ProducerRound: round})
		}
		if ready {
			d.emit(task.SettleSignal{Kind: task.SettleReady, Ready: true, WaitSatisfied: true, Output: "probe succeeded", RoundID: round, ProducerRound: round})
		}
		return
	}
	if failed {
		d.mu.Unlock()
		return
	}
	d.probeFailed = true
	d.mu.Unlock()
	d.emit(task.SettleSignal{Kind: task.SettleWatch, Observation: "probe_failure", Output: "probe FAILED: " + detail, Err: fmt.Errorf("liveness probe failed: %s", detail), RoundID: round, ProducerRound: round})
}

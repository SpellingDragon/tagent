// 契约: docs/wiki/tool/tool-architecture.md#action-tool
//
// The executor face of the tmux mechanism: the TmuxExecutor type, the tmux
// client call, the first-frame observation, and the four Executor methods.
// The session reads live in tmux_session.go, and the command line beside the
// exit record in tmux_command.go.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	osexec "os/exec"
	"strconv"
	"strings"
	"time"
)

// TmuxExecutor is the Executor over real tmux sessions: every resource key names
// one tmux session, the command of a declaration runs inside that session, and
// the pane is kept after its command exits so the recorded output and exit
// status stay observable until the resource is stopped.
type TmuxExecutor struct {
	startWindow   time.Duration
	pollInterval  time.Duration
	commandWindow time.Duration
}

const (
	// tmuxStartWindow bounds the observation Start waits for a first frame.
	tmuxStartWindow = 300 * time.Millisecond
	// tmuxPollInterval is the observation cadence of the Start window.
	tmuxPollInterval = 10 * time.Millisecond
	// tmuxCommandWindow bounds one tmux client invocation.
	tmuxCommandWindow = 5 * time.Second
	// tmuxSessionMaxName is the longest session name one key may name.
	tmuxSessionMaxName = 64
	// tmuxServerAttempts and tmuxServerRetryGap bound the re-reads of a session
	// list whose client reports no server: a busy server refuses a client
	// transiently, and one refusal is no evidence that the sessions left.
	tmuxServerAttempts = 5
	tmuxServerRetryGap = 20 * time.Millisecond
	// tmuxStopRecheckGap is the pause before a stop re-reads a session list that
	// did not name the resource, so a live session is never killed on one
	// transient read.
	tmuxStopRecheckGap = 20 * time.Millisecond
	// tmuxDrainWindow is how long a command's pane stays open after the command
	// exits, so the pane's screen records the output of a command that ends at
	// once even beside concurrent clients.
	tmuxDrainWindow = 100 * time.Millisecond
	// tmuxExitRecordDir names the directory below the user's cache directory
	// that keeps one session's recorded exit status.
	tmuxExitRecordDir = "exec-exit"
)

var (
	// errSessionNameInvalid reports a resource key that is not a usable session name.
	errSessionNameInvalid = errors.New("exec: the resource key is not a usable tmux session name")
	// errSessionExists reports a key whose session is already running.
	errSessionExists = errors.New("exec: the tmux session is already running under that key")
	// errCommandMissing reports a declaration that names no command.
	errCommandMissing = errors.New("exec: the declaration names no command")
)

// NewTmuxExecutor returns the executor over the tmux sessions of this system. A
// system whose PATH carries no tmux binary is refused by name, so no executor
// exists that cannot start a session. The availability verdict is the real PATH
// probe of IsTmuxAvailable.
func NewTmuxExecutor() (*TmuxExecutor, error) {
	return newTmuxExecutor(IsTmuxAvailable)
}

// newTmuxExecutor returns the executor beside the given availability probe.
func newTmuxExecutor(probe func() bool) (*TmuxExecutor, error) {
	if probe == nil || !probe() {
		return nil, ErrTmuxUnavailable
	}
	return &TmuxExecutor{
		startWindow:   tmuxStartWindow,
		pollInterval:  tmuxPollInterval,
		commandWindow: tmuxCommandWindow,
	}, nil
}

// Start creates the session the declaration's resource key names and runs its
// command inside it, with the pane kept after the command exits. The session
// opens on its own shell, the exit-retaining option is applied there, and the
// pane is then respawned with the declared command: the command starts only
// after the window is certain to keep its pane, so an exit status stays
// observable even for a command that ends at once and even beside a busy
// server. The option is read back before the command starts, so a window that
// did not take it fails the start instead of losing the ending later. The
// command runs in a subshell whose status is re-raised after a drain window, so
// a command that ends at once still leaves its output on the pane, and the status
// is also written to the session's exit record, so an ending tmux did not keep
// still answers the status the command ended with. The call answers once the new
// session has produced its first frame, or once the bounded observation window
// passed without one. A key whose session already runs is refused instead of
// opened a second time.
func (e *TmuxExecutor) Start(ctx context.Context, decl ExecutionDecl) (ResourceKey, error) {
	name, err := sessionNameOf(decl.Resource)
	if err != nil {
		return ResourceKey{}, err
	}
	if strings.TrimSpace(decl.Command) == "" {
		return ResourceKey{}, errCommandMissing
	}
	exists, err := e.sessionExists(ctx, name)
	if err != nil {
		return ResourceKey{}, err
	}
	if exists {
		return ResourceKey{}, fmt.Errorf("%w: %s", errSessionExists, name)
	}
	record := newExitRecord(name)
	args := []string{"new-session", "-d", "-s", name}
	if decl.Dir != "" {
		args = append(args, "-c", decl.Dir)
	}
	if _, startErr := e.tmux(ctx, args...); startErr != nil {
		created, probeErr := e.sessionExists(ctx, name)
		if probeErr != nil || !created {
			return ResourceKey{}, startErr
		}
	}
	if _, err := e.tmux(ctx, "set-option", "-w", "-t", name, "remain-on-exit", "on"); err != nil {
		_, _ = e.tmux(ctx, "kill-session", "-t", name)
		return ResourceKey{}, err
	}
	kept, err := e.tmux(ctx, "show-options", "-w", "-v", "-t", name, "remain-on-exit")
	if err != nil {
		_, _ = e.tmux(ctx, "kill-session", "-t", name)
		return ResourceKey{}, err
	}
	if setting := strings.TrimSpace(kept); setting != "on" {
		_, _ = e.tmux(ctx, "kill-session", "-t", name)
		return ResourceKey{}, fmt.Errorf("exec: the tmux window of %s kept remain-on-exit %q, want on", name, setting)
	}
	respawn := []string{"respawn-pane", "-k", "-t", name}
	if decl.Dir != "" {
		respawn = append(respawn, "-c", decl.Dir)
	}
	respawn = append(respawn, commandLineWithDrain(decl, record))
	if _, err := e.tmux(ctx, respawn...); err != nil {
		_, _ = e.tmux(ctx, "kill-session", "-t", name)
		return ResourceKey{}, err
	}
	e.awaitFirstFrame(ctx, decl.Resource)
	return decl.Resource, nil
}

// Probe reports the state one probe observed for key: a key absent from the
// session list is dead, a key whose pane ended is dead, and a probe that could
// not read the session list or the pane reports unknown rather than death.
func (e *TmuxExecutor) Probe(ctx context.Context, key ResourceKey) (ProbeResult, error) {
	name, err := sessionNameOf(key)
	if err != nil {
		return ProbeResult{}, err
	}
	names, err := e.sessionNames(ctx)
	if err != nil {
		return ProbeResult{State: ProbeUnknown, Detail: err.Error()}, nil
	}
	if !containsName(names, name) {
		return ProbeResult{State: ProbeDead, Detail: "the session is absent from the tmux session list"}, nil
	}
	paneDead, err := e.tmux(ctx, "display-message", "-p", "-t", name, "#{pane_dead}")
	if err != nil {
		return ProbeResult{State: ProbeUnknown, Detail: err.Error()}, nil
	}
	if strings.TrimSpace(paneDead) == "1" {
		return ProbeResult{State: ProbeDead, Detail: "the pane behind the session is dead"}, nil
	}
	return ProbeResult{State: ProbeAlive}, nil
}

// Capture returns the frame tmux records for key: the pane's history and its
// visible rows, read as one frame so a later read never replaces the output an
// earlier read observed.
func (e *TmuxExecutor) Capture(ctx context.Context, key ResourceKey) ([]byte, error) {
	name, err := sessionNameOf(key)
	if err != nil {
		return nil, err
	}
	out, err := e.tmux(ctx, "capture-pane", "-p", "-S", "-", "-t", name)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// Stop ends the session behind key and reports what was observed: a pane that
// already ended answers its recorded exit status, a dead pane whose tmux kept no
// status answers the status the session's own record kept, a live pane is killed
// and the stop is confirmed by the session leaving the list, and an exit status
// neither the pane nor its record carried is reported as -1 rather than as a
// clean exit. A session list that does not name the key is read again before the
// ending is confirmed, because a client refused by a busy server reads no
// sessions at all, and a server that left mid-read took every session with it;
// the pane state beside its status is read in one message, and the record is
// removed as it is read, so no status is kept beside a session that left.
func (e *TmuxExecutor) Stop(ctx context.Context, key ResourceKey) (StopEvidence, error) {
	name, err := sessionNameOf(key)
	if err != nil {
		return StopEvidence{Resource: key}, err
	}
	evidence := StopEvidence{Resource: key, ExitCode: -1}
	names, err := e.sessionNames(ctx)
	if err != nil {
		evidence.Reason = err.Error()
		return evidence, err
	}
	if !containsName(names, name) {
		if !sleepContext(ctx, tmuxStopRecheckGap) {
			clearExitRecord(name)
			evidence.Confirmed = true
			return evidence, nil
		}
		again, againErr := e.sessionNames(ctx)
		if againErr != nil {
			evidence.Reason = againErr.Error()
			return evidence, againErr
		}
		if !containsName(again, name) {
			clearExitRecord(name)
			evidence.Confirmed = true
			return evidence, nil
		}
	}
	paneDeath, deadErr := e.tmux(ctx, "display-message", "-p", "-t", name, "#{pane_dead} #{pane_dead_status}")
	if deadErr == nil {
		fields := strings.Fields(paneDeath)
		if len(fields) > 0 && fields[0] == "1" {
			recorded := takeRecordedExitStatus(name)
			status := ""
			if len(fields) > 1 {
				status = fields[1]
			}
			if code, convErr := strconv.Atoi(status); convErr == nil {
				evidence.ExitCode = code
			} else {
				evidence.ExitCode = recorded
			}
		}
	}
	if _, killErr := e.tmux(ctx, "kill-session", "-t", name); killErr != nil {
		absent, probeErr := e.sessionAbsent(ctx, name)
		if probeErr == nil && absent {
			evidence.Confirmed = true
			return evidence, nil
		}
		if probeErr != nil {
			evidence.Reason = probeErr.Error()
		} else {
			evidence.Reason = killErr.Error()
		}
		return evidence, killErr
	}
	absent, err := e.sessionAbsent(ctx, name)
	if err != nil {
		evidence.Reason = err.Error()
		return evidence, err
	}
	if !absent {
		evidence.Reason = "the session survived the stop request"
		return evidence, nil
	}
	evidence.Confirmed = true
	return evidence, nil
}

// awaitFirstFrame observes the new session until its recorded frame carries
// content or the Start window closes.
func (e *TmuxExecutor) awaitFirstFrame(ctx context.Context, key ResourceKey) {
	window := e.startWindow
	if window <= 0 {
		window = tmuxStartWindow
	}
	cadence := e.pollInterval
	if cadence <= 0 {
		cadence = tmuxPollInterval
	}
	deadline := time.Now().Add(window)
	for {
		if frame, err := e.Capture(ctx, key); err == nil && len(bytes.TrimSpace(frame)) > 0 {
			return
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return
		}
		if !sleepContext(ctx, cadence) {
			return
		}
	}
}

// tmux runs one tmux client invocation under the command window and reports its
// standard output beside the tmux diagnostic of a failed call.
func (e *TmuxExecutor) tmux(ctx context.Context, args ...string) (string, error) {
	window := e.commandWindow
	if window <= 0 {
		window = tmuxCommandWindow
	}
	callCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	cmd := osexec.CommandContext(callCtx, "tmux", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return stdout.String(), fmt.Errorf("exec: tmux %s: %w", args[0], err)
		}
		return stdout.String(), fmt.Errorf("exec: tmux %s: %w: %s", args[0], err, detail)
	}
	return stdout.String(), nil
}

// containsName reports whether names carries the given name.
func containsName(names []string, name string) bool {
	for _, listed := range names {
		if listed == name {
			return true
		}
	}
	return false
}

// tmuxServerAbsent reports whether err is a tmux verdict that the server is not
// there: a server that never started, a socket that is already gone, and a
// server that left while the client waited all leave every session of it
// absent, and each is told apart from a call that failed beside a live server.
func tmuxServerAbsent(err error) bool {
	if err == nil {
		return false
	}
	detail := err.Error()
	return strings.Contains(detail, "no server running") ||
		strings.Contains(detail, "No such file or directory") ||
		strings.Contains(detail, "server exited unexpectedly") ||
		strings.Contains(detail, "lost server")
}

// sleepContext waits for one cadence and reports whether the wait finished
// before the context ended.
func sleepContext(ctx context.Context, cadence time.Duration) bool {
	timer := time.NewTimer(cadence)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

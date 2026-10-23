// 契约: docs/wiki/tool/tool-architecture.md#action-tool
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
// command inside it, with the pane kept after the command exits. The call answers
// once the new session has produced its first frame, or once the bounded
// observation window passed without one. A key whose session already runs is
// refused instead of opened a second time.
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
	args := []string{"new-session", "-d", "-s", name}
	if decl.Dir != "" {
		args = append(args, "-c", decl.Dir)
	}
	args = append(args, commandLine(decl))
	args = append(args, ";", "set-option", "-t", name, "remain-on-exit", "on")
	if _, startErr := e.tmux(ctx, args...); startErr != nil {
		created, probeErr := e.sessionExists(ctx, name)
		if probeErr != nil || !created {
			return ResourceKey{}, startErr
		}
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
// already ended answers its recorded exit status, a live pane is killed and the
// stop is confirmed by the session leaving the list, and an exit status the
// stop could not observe is reported as -1 rather than as a clean exit.
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
		evidence.Confirmed = true
		return evidence, nil
	}
	paneDead, deadErr := e.tmux(ctx, "display-message", "-p", "-t", name, "#{pane_dead}")
	if deadErr == nil && strings.TrimSpace(paneDead) == "1" {
		if raw, statusErr := e.tmux(ctx, "display-message", "-p", "-t", name, "#{pane_dead_status}"); statusErr == nil {
			if code, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil {
				evidence.ExitCode = code
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

// sessionNames lists the session names tmux reports, with a server that is not
// running read as an empty list.
func (e *TmuxExecutor) sessionNames(ctx context.Context) ([]string, error) {
	out, err := e.tmux(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		if tmuxServerAbsent(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(out, "\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// sessionExists reports whether one named session is present in the session list.
func (e *TmuxExecutor) sessionExists(ctx context.Context, name string) (bool, error) {
	names, err := e.sessionNames(ctx)
	if err != nil {
		return false, err
	}
	return containsName(names, name), nil
}

// sessionAbsent reports whether one named session left the session list.
func (e *TmuxExecutor) sessionAbsent(ctx context.Context, name string) (bool, error) {
	names, err := e.sessionNames(ctx)
	if err != nil {
		return false, err
	}
	return !containsName(names, name), nil
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

// sessionNameOf reports the tmux session name one resource key names: an empty
// kind selects the tmux family, any other kind is refused, and the key may carry
// only characters a session name holds, so it never reaches tmux as more than one
// word.
func sessionNameOf(key ResourceKey) (string, error) {
	if key.Kind != "" && key.Kind != KindTmux {
		return "", fmt.Errorf("exec: the tmux executor cannot serve resource kind %q", key.Kind)
	}
	if !validSessionName(key.Key) {
		return "", fmt.Errorf("%w: %q", errSessionNameInvalid, key.Key)
	}
	return key.Key, nil
}

// validSessionName reports whether name is a non-empty session name of at most
// tmuxSessionMaxName characters drawn from letters, digits, hyphens and
// underscores.
func validSessionName(name string) bool {
	if name == "" || len(name) > tmuxSessionMaxName {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// commandLine renders one declaration as the shell command line tmux runs: the
// command and every argument are quoted for the shell, so an argument stays one
// word whatever it carries.
func commandLine(decl ExecutionDecl) string {
	parts := make([]string, 0, len(decl.Args)+1)
	parts = append(parts, shellQuote(decl.Command))
	for _, arg := range decl.Args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

// shellQuote renders one value as a single shell word: a value holding only
// safe characters stands as itself, and every other value is single-quoted with
// its embedded quotes escaped.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if shellSafe(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// shellSafe reports whether value may stand as one unquoted shell word.
func shellSafe(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("-_./:=,@%+", r):
		default:
			return false
		}
	}
	return true
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

// tmuxServerAbsent reports whether err is the tmux verdict that no server is
// running, which leaves every session of it absent.
func tmuxServerAbsent(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no server running")
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

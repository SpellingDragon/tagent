// 契约: docs/wiki/tool/tool-architecture.md#action-tool
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
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

// sessionNames lists the session names tmux reports, with a server that is not
// running read as an empty list: a refusal is retried at the server cadence,
// so a busy server's refusal or a server that left mid-read is never read as an
// empty list before the retries pass.
func (e *TmuxExecutor) sessionNames(ctx context.Context) ([]string, error) {
	for attempt := 1; ; attempt++ {
		out, err := e.tmux(ctx, "list-sessions", "-F", "#{session_name}")
		if err == nil {
			lines := strings.Split(out, "\n")
			names := make([]string, 0, len(lines))
			for _, line := range lines {
				if name := strings.TrimSpace(line); name != "" {
					names = append(names, name)
				}
			}
			return names, nil
		}
		if !tmuxServerAbsent(err) {
			return nil, err
		}
		if attempt >= tmuxServerAttempts || !sleepContext(ctx, tmuxServerRetryGap) {
			return nil, nil
		}
	}
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

// commandLineWithDrain renders the declared command as a subshell whose status
// is re-raised after the drain window: a command that ends at once still leaves
// its output on the pane before the pane is reported dead, and a pane whose tmux
// lost the ending still leaves the status in the record beside the session.
func commandLineWithDrain(decl ExecutionDecl, record string) string {
	line := fmt.Sprintf("( %s ); code=$?; ", commandLine(decl))
	if record != "" {
		line += fmt.Sprintf("printf '%%d' \"$code\" > %s 2>/dev/null; ", shellQuote(record))
	}
	return line + fmt.Sprintf("sleep %s; exit $code",
		strconv.FormatFloat(tmuxDrainWindow.Seconds(), 'f', -1, 64))
}

// newExitRecord prepares the exit record of the named session: the record an
// earlier session under the same key left behind is cleared, so no stop reads a
// status this session did not write, and a record that cannot be prepared answers
// no path, so the session runs beside the status tmux keeps alone.
func newExitRecord(name string) string {
	path, err := exitRecordPath(name)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	_ = os.Remove(path)
	return path
}

// exitRecordPath names the file one session's drain wrapper records its command's
// exit status in: the path is derived from the session name alone, so a stop in
// another process still finds the record of the session it stops, and the base is
// the user's own cache directory, so no other user writes a status this session
// reads.
func exitRecordPath(name string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tagent", tmuxExitRecordDir, name), nil
}

// takeRecordedExitStatus reads and removes the exit status the drain wrapper of
// the named session recorded: a tmux that lost the pane's ending keeps a dead pane
// with no pane_dead_status, and the wrapper's record is the status that ending
// still carries. The record is removed whatever it holds, so a stop leaves no
// status beside a session that ended, and a record that is absent or unreadable
// answers the unobserved -1 rather than a status the session did not end with.
func takeRecordedExitStatus(name string) int {
	path, err := exitRecordPath(name)
	if err != nil {
		return -1
	}
	raw, readErr := os.ReadFile(path)
	_ = os.Remove(path)
	if readErr != nil {
		return -1
	}
	code, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil {
		return -1
	}
	return code
}

// clearExitRecord removes the exit record of the named session without reading
// it: a session that left needs no status kept beside it.
func clearExitRecord(name string) {
	path, err := exitRecordPath(name)
	if err != nil {
		return
	}
	_ = os.Remove(path)
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

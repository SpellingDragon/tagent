// 契约: docs/wiki/tool/tool-architecture.md#tmux-monitor
package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTmuxExecutorRunsCommand pins the life of one real tmux execution.
// - start runs echo smoke-exec-marker inside a session of its own
// - capture answers the marker, the first poll watches, and quiet reaches stable
// - stop is confirmed and the session is observed dead afterwards
func TestTmuxExecutorRunsCommand(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v, want a tmux executor", err)
	}
	ctx := context.Background()
	key := tmuxTestKey()
	decl := ExecutionDecl{Command: "echo", Args: []string{"smoke-exec-marker"}, Resource: key}
	if _, err := executor.Start(ctx, decl); err != nil {
		t.Fatalf("start = %v", err)
	}
	t.Cleanup(func() { _, _ = executor.Stop(context.Background(), key) })

	frame, err := executor.Capture(ctx, key)
	if err != nil {
		t.Fatalf("capture = %v", err)
	}
	if !strings.Contains(string(frame), "smoke-exec-marker") {
		t.Fatalf("captured frame = %q, want smoke-exec-marker", frame)
	}

	monitor := NewMonitor(executor, MonitorConfig{
		Interval:         100 * time.Millisecond,
		StableDuration:   300 * time.Millisecond,
		FakeDeadDuration: 5 * time.Second,
	})
	first, err := monitor.Poll(ctx, key)
	if err != nil {
		t.Fatalf("first poll = %v", err)
	}
	if first != MonitorWatch {
		t.Fatalf("first poll = %q, want %q", first, MonitorWatch)
	}
	waitForMonitorStatus(t, monitor, key, MonitorStable, 3*time.Second)

	evidence, err := executor.Stop(ctx, key)
	if err != nil {
		t.Fatalf("stop = %v", err)
	}
	if !evidence.Confirmed {
		t.Fatalf("stop evidence = %+v, want a confirmed stop", evidence)
	}
	probe, err := executor.Probe(ctx, key)
	if err != nil {
		t.Fatalf("probe after stop = %v", err)
	}
	if probe.State != ProbeDead {
		t.Fatalf("probe after stop = %v, want %v", probe.State, ProbeDead)
	}
}

// TestTmuxExecutorKeepsImmediateExitStatus pins the order a start applies its exit-retaining option in.
// - a command that ends at once still leaves its pane and its output behind, so the session is listed and the frame carries the marker
// - stop answers the recorded status instead of reporting an unobserved ending
// - the record beside the session is removed by the stop that read the kept status
func TestTmuxExecutorKeepsImmediateExitStatus(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v, want a tmux executor", err)
	}
	ctx := context.Background()
	key := tmuxTestKey()
	decl := ExecutionDecl{Command: "sh", Args: []string{"-c", "echo smoke-now-marker; exit 7"}, Resource: key}
	if _, err := executor.Start(ctx, decl); err != nil {
		t.Fatalf("start = %v", err)
	}
	t.Cleanup(func() { _, _ = executor.Stop(context.Background(), key) })

	deadline := time.Now().Add(5 * time.Second)
	for {
		probe, err := executor.Probe(ctx, key)
		if err != nil {
			t.Fatalf("probe = %v", err)
		}
		if probe.State == ProbeDead {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the immediate exit was never observed (last state=%v)", probe.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	names, err := executor.sessionNames(ctx)
	if err != nil {
		t.Fatalf("list the sessions after the exit = %v", err)
	}
	if !containsName(names, key.Key) {
		t.Fatalf("the session %s left the list beside its ended pane: the pane was not kept", key.Key)
	}
	frame, err := executor.Capture(ctx, key)
	if err != nil {
		t.Fatalf("capture the immediate exit = %v", err)
	}
	if !strings.Contains(string(frame), "smoke-now-marker") {
		t.Fatalf("captured frame = %q, want the marker of a command that ended at once", frame)
	}
	evidence, err := executor.Stop(ctx, key)
	if err != nil {
		t.Fatalf("stop = %v", err)
	}
	if !evidence.Confirmed {
		t.Fatalf("stop evidence = %+v, want a confirmed stop", evidence)
	}
	if evidence.ExitCode != 7 {
		t.Fatalf("stop exit code = %d, want the recorded 7", evidence.ExitCode)
	}
	record, err := exitRecordPath(key.Key)
	if err != nil {
		t.Fatalf("name the exit record = %v", err)
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record after a stop that read the kept status = %v, want it removed", err)
	}
}

// TestTmuxStopReadsTheStatusTheRecordKept pins the status a tmux did not keep.
// - a command that exits 7 runs in a kept pane, and the pane is observed dead
// - the pane-death read answers a dead pane with no status, as a tmux that lost the ending does
// - the stop answers the recorded 7 and removes the record it read
func TestTmuxStopReadsTheStatusTheRecordKept(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v, want a tmux executor", err)
	}
	ctx := context.Background()
	key := tmuxTestKey()
	decl := ExecutionDecl{Command: "sh", Args: []string{"-c", "echo record-marker; exit 7"}, Resource: key}
	if _, err := executor.Start(ctx, decl); err != nil {
		t.Fatalf("start = %v", err)
	}
	t.Cleanup(func() { _, _ = executor.Stop(context.Background(), key) })
	waitForPaneDead(t, executor, key)
	record, err := exitRecordPath(key.Key)
	if err != nil {
		t.Fatalf("name the exit record = %v", err)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("the record beside the session = %v, want the status the wrapper wrote", err)
	}
	hideStatusBesideThePane(t)
	evidence, err := executor.Stop(ctx, key)
	if err != nil {
		t.Fatalf("stop beside a pane that kept no status = %v", err)
	}
	if !evidence.Confirmed {
		t.Fatalf("stop evidence = %+v, want a confirmed stop", evidence)
	}
	if evidence.ExitCode != 7 {
		t.Fatalf("stop exit code = %d, want the recorded 7", evidence.ExitCode)
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the record after the stop = %v, want it removed as it was read", err)
	}
}

// TestTmuxStopKeepsTheUnobservedStatusBesideNoRecord pins the status neither side kept.
// - a command that exits 7 runs in a kept pane, and the pane is observed dead
// - the pane-death read keeps no status and the record beside the session is gone
// - the stop answers the unobserved -1 instead of the 7 this session ended with
func TestTmuxStopKeepsTheUnobservedStatusBesideNoRecord(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v, want a tmux executor", err)
	}
	ctx := context.Background()
	key := tmuxTestKey()
	decl := ExecutionDecl{Command: "sh", Args: []string{"-c", "echo record-marker; exit 7"}, Resource: key}
	if _, err := executor.Start(ctx, decl); err != nil {
		t.Fatalf("start = %v", err)
	}
	t.Cleanup(func() { _, _ = executor.Stop(context.Background(), key) })
	waitForPaneDead(t, executor, key)
	record, err := exitRecordPath(key.Key)
	if err != nil {
		t.Fatalf("name the exit record = %v", err)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("the record beside the session = %v, want the status the wrapper wrote", err)
	}
	if err := os.Remove(record); err != nil {
		t.Fatalf("remove the record beside the session = %v", err)
	}
	hideStatusBesideThePane(t)
	evidence, err := executor.Stop(ctx, key)
	if err != nil {
		t.Fatalf("stop beside a pane and a record that both kept no status = %v", err)
	}
	if !evidence.Confirmed {
		t.Fatalf("stop evidence = %+v, want a confirmed stop", evidence)
	}
	if evidence.ExitCode != -1 {
		t.Fatalf("stop exit code = %d, want the unobserved -1", evidence.ExitCode)
	}
}

// TestTmuxSessionListRetriesBusyServer pins the re-read of a session list a busy server refused.
// - the first list answers the verdict of a server that is not running
// - the list is read again rather than read as empty, so the running session is still named
func TestTmuxSessionListRetriesBusyServer(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	real, err := osexec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not available on this system")
	}
	ctx := context.Background()
	key := tmuxTestKey()
	if err := osexec.Command(real, "new-session", "-d", "-s", key.Key, "sleep", "30").Run(); err != nil {
		t.Fatalf("open the probe session = %v", err)
	}
	t.Cleanup(func() { _ = osexec.Command(real, "kill-session", "-t", key.Key).Run() })

	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"list-sessions\" ] && [ ! -e \"$TMUX_REFUSED_ONCE\" ]; then\n" +
		"  : > \"$TMUX_REFUSED_ONCE\"\n" +
		"  echo 'no server running on /tmp/tmux-1001/default' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"exec \"$TMUX_REAL_BINARY\" \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatalf("write the tmux shim = %v", err)
	}
	refused := filepath.Join(dir, "refused")
	t.Setenv("PATH", dir)
	t.Setenv("TMUX_REFUSED_ONCE", refused)
	t.Setenv("TMUX_REAL_BINARY", real)

	executor := &TmuxExecutor{}
	names, err := executor.sessionNames(ctx)
	if err != nil {
		t.Fatalf("list the sessions beside the refused read = %v", err)
	}
	if !containsName(names, key.Key) {
		t.Fatalf("the session names = %v, want the running %s named despite the refused read", names, key.Key)
	}
	if _, err := os.Stat(refused); err != nil {
		t.Fatalf("the shim never refused a read: %v", err)
	}
}

// TestTmuxStopConfirmsBesideAServerThatLeft pins the verdict of a server that left while a client waited.
// - the session list answers the exit of the server that held the sessions instead of a name list
// - the stop is confirmed with the status unobserved, so a release beside a gone server names no held resource
func TestTmuxStopConfirmsBesideAServerThatLeft(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	real, err := osexec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not available on this system")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"list-sessions\" ]; then\n" +
		"  echo 'server exited unexpectedly' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"exec \"$TMUX_REAL_BINARY\" \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatalf("write the tmux shim = %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TMUX_REAL_BINARY", real)

	executor := &TmuxExecutor{}
	evidence, err := executor.Stop(context.Background(), tmuxTestKey())
	if err != nil {
		t.Fatalf("stop beside a server that left = %v", err)
	}
	if !evidence.Confirmed {
		t.Fatalf("stop evidence = %+v, want the gone server read as a confirmed stop", evidence)
	}
	if evidence.ExitCode != -1 {
		t.Fatalf("stop exit code = %d, want the unobserved -1", evidence.ExitCode)
	}
}

// TestTmuxStopNamesAReadThatFailedBesideTheServer pins the line between a gone server and a failed read.
// - a list that fails with a verdict other than the server leaving is not read as an absent server
// - the stop answers that failure with the resource unconfirmed instead of reporting a release
func TestTmuxStopNamesAReadThatFailedBesideTheServer(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\n" +
		"echo 'usage: tmux [-2CDlNuUVv] [-c shell-command] [-f file] [-L socket-name]' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatalf("write the tmux shim = %v", err)
	}
	t.Setenv("PATH", dir)

	executor := &TmuxExecutor{}
	evidence, err := executor.Stop(context.Background(), tmuxTestKey())
	if err == nil {
		t.Fatalf("stop beside a failed read = %+v, want the read failure named", evidence)
	}
	if evidence.Confirmed {
		t.Fatalf("stop evidence = %+v, want the resource left unconfirmed", evidence)
	}
}

// TestMonitorSuspectAfterQuiet pins the fake-dead classification of one live session.
// - a session running sleep records no frame, so the first poll only watches
// - quiet past the fake-dead threshold of its rhythm is reported as suspect
func TestMonitorSuspectAfterQuiet(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v, want a tmux executor", err)
	}
	ctx := context.Background()
	key := tmuxTestKey()
	decl := ExecutionDecl{Command: "sleep", Args: []string{"5"}, Resource: key}
	if _, err := executor.Start(ctx, decl); err != nil {
		t.Fatalf("start = %v", err)
	}
	t.Cleanup(func() { _, _ = executor.Stop(context.Background(), key) })

	monitor := NewMonitor(executor, MonitorConfig{
		Interval:         100 * time.Millisecond,
		StableDuration:   250 * time.Millisecond,
		FakeDeadDuration: 500 * time.Millisecond,
	})
	first, err := monitor.Poll(ctx, key)
	if err != nil {
		t.Fatalf("first poll = %v", err)
	}
	if first != MonitorWatch {
		t.Fatalf("first poll = %q, want %q", first, MonitorWatch)
	}
	waitForMonitorStatus(t, monitor, key, MonitorSuspect, 3*time.Second)
}

// TestExecWithoutTmuxNamesRefusal pins the construction refusal of a system without tmux.
// - an injected availability probe answers no tmux and the construction names ErrTmuxUnavailable
// - a PATH carrying no tmux binary refuses the same way, without panicking
func TestExecWithoutTmuxNamesRefusal(t *testing.T) {
	t.Run("injected probe", func(t *testing.T) {
		executor, err := newTmuxExecutor(func() bool { return false })
		if executor != nil {
			t.Fatalf("executor = %+v, want none", executor)
		}
		if !errors.Is(err, ErrTmuxUnavailable) {
			t.Fatalf("construction = %v, want ErrTmuxUnavailable", err)
		}
	})
	t.Run("path without tmux", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		executor, err := NewTmuxExecutor()
		if executor != nil {
			t.Fatalf("executor = %+v, want none", executor)
		}
		if !errors.Is(err, ErrTmuxUnavailable) {
			t.Fatalf("construction = %v, want ErrTmuxUnavailable", err)
		}
	})
}

// TestRunToolExecutesThroughTmuxExecutor pins the tool face over the real tmux executor.
// - exec_run acquires one session and runs the command the arguments declare
// - the answer carries the marker the captured frame holds
func TestRunToolExecutesThroughTmuxExecutor(t *testing.T) {
	if !IsTmuxAvailable() {
		t.Skip("tmux is not available on this system")
	}
	isolateTmuxServer(t)
	executor, err := NewTmuxExecutor()
	if err != nil {
		t.Fatalf("executor construction = %v", err)
	}
	manager := NewManager(executor)
	key := tmuxTestKey()
	t.Cleanup(func() { _, _ = manager.Release(context.Background(), key) })
	args, err := json.Marshal(map[string]any{
		"command":  "echo",
		"args":     []string{"smoke-exec-marker"},
		"kind":     KindTmux,
		"resource": key.Key,
	})
	if err != nil {
		t.Fatalf("arguments = %v", err)
	}
	answer, err := NewRunTool(manager).Call(context.Background(), args)
	if err != nil {
		t.Fatalf("exec_run = %v", err)
	}
	if !strings.Contains(fmt.Sprint(answer), "smoke-exec-marker") {
		t.Fatalf("exec_run answer = %q, want smoke-exec-marker", answer)
	}
}

// isolateTmuxServer points this test at a tmux server of its own: another
// package finishing the shared server must not take the sessions under test
// with it, and the socket directory stays short enough for the platform limit.
func isolateTmuxServer(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tagent-tmux-")
	if err != nil {
		t.Fatalf("create the private tmux directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
}

// waitForPaneDead observes one execution until its pane is reported dead, and
// fails the test when the ending is not observed inside the wait.
func waitForPaneDead(t *testing.T, executor *TmuxExecutor, key ResourceKey) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		probe, err := executor.Probe(context.Background(), key)
		if err != nil {
			t.Fatalf("probe = %v", err)
		}
		if probe.State == ProbeDead {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the ending was never observed (last state=%v)", probe.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// hideStatusBesideThePane points this test at a tmux whose pane-death read keeps
// no status: a dead pane keeps answering as dead with the status unread, as a
// tmux that lost the ending does, and every other call reaches the real binary.
func hideStatusBesideThePane(t *testing.T) {
	t.Helper()
	real, err := osexec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not available on this system")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"display-message\" ] && [ \"$5\" = \"#{pane_dead} #{pane_dead_status}\" ]; then\n" +
		"  echo 1\n" +
		"  exit 0\n" +
		"fi\n" +
		"exec \"$TMUX_REAL_BINARY\" \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatalf("write the tmux shim = %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TMUX_REAL_BINARY", real)
}

// tmuxTestKey returns a tmux resource key whose session name belongs to this
// test run.
func tmuxTestKey() ResourceKey {
	return ResourceKey{Kind: KindTmux, Key: fmt.Sprintf("tagent-test-%d", time.Now().UnixNano())}
}

// waitForMonitorStatus polls one execution until the wanted classification or
// the deadline.
func waitForMonitorStatus(t *testing.T, monitor *Monitor, key ResourceKey, want MonitorStatus, timeout time.Duration) MonitorStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last MonitorStatus
	for time.Now().Before(deadline) {
		status, err := monitor.Poll(context.Background(), key)
		if err != nil {
			t.Fatalf("monitor poll = %v", err)
		}
		last = status
		if status == want {
			return status
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("monitor status = %q after %s, want %q", last, timeout, want)
	return last
}

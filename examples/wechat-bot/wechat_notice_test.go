//go:build crash

// notice_crash_test.go 钉住启动通知的持久接纳：进程在四个窗口被 SIGKILL 后，重启的进程
// 从同一身份读回同一则通知，并由同一回合恰好消费一次。
//
// - 运行: go test -tags crash -run '^TestNoticeSymptomCrashNoLoss$' -count=1 -v
// - 四个窗口: admitted / drained / marker-renamed / submit-failed，各起一个子进程并 kill -9
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
)

// noticeCrashModeEnv, noticeCrashDirEnv, noticeCrashMarkerEnv and
// noticeCrashLedgerEnv name the work order one crash child reads: the window it
// stops at, the root its inbox is kept under, the startup marker it admits and
// the account file it appends its window to.
const (
	noticeCrashModeEnv   = "TAGENT_NOTICE_CRASH_MODE"
	noticeCrashDirEnv    = "TAGENT_NOTICE_CRASH_DIR"
	noticeCrashMarkerEnv = "TAGENT_NOTICE_CRASH_MARKER"
	noticeCrashLedgerEnv = "TAGENT_NOTICE_CRASH_LEDGER"
)

// noticeCrashAdmitted, noticeCrashDrained, noticeCrashRenamed and
// noticeCrashSubmitFailed name the four windows a stop can lose a notice at: the
// admission landed with no reader, the turn read the notice and never handed it
// to the host, the confirmation renamed the marker and never retired the record,
// and the host refused the material the turn read.
const (
	noticeCrashAdmitted     = "admitted"
	noticeCrashDrained      = "drained"
	noticeCrashRenamed      = "marker-renamed"
	noticeCrashSubmitFailed = "submit-failed"
)

// noticeCrashBody is the body every crash window admits: the check reads it
// back out of the material the recovered turn drives.
const noticeCrashBody = "the reincarnation notice the previous generation owed"

// noticeCrashReadyWait bounds the wait for the child's window account, and
// noticeCrashHoldMax bounds the child's hold for the parent's kill.
const (
	noticeCrashReadyWait = 90 * time.Second
	noticeCrashHoldMax   = 120 * time.Second
)

// TestNoticeSymptomCrashNoLoss pins the startup notice a stop can lose: the process is killed at each inbox write point and the next process consumes it once.
//   - the kill lands after the admission, after the drain, after the marker rename and beside a refused submit, one child each
//   - a restarted hook re-admits the same marker and opens no second record: the identity and the admitted body survive the stop
//   - the restarted process reads the owed notice in exactly one turn and writes the consumed marker beside the confirmation
//   - a child that read before the stop may see the same identity once more after restart; the confirmation closes it and no later turn reads it
//   - a further restart reads nothing back, so the recovery is not a second effective processing
//   - marker-renamed stops after the consumption marker moved and before the record retired
//   - submit-failed stops at a host that refused the material; a refused outbound send is settled by delivery.settlement instead
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestNoticeSymptomCrashNoLoss(t *testing.T) {
	if mode := os.Getenv(noticeCrashModeEnv); mode != "" {
		noticeCrashChild(t, mode)
		return
	}
	windows := []struct {
		name string
		mode string
		read int
	}{
		{"the kill lands after the admission", noticeCrashAdmitted, 0},
		{"the kill lands after the drain", noticeCrashDrained, 1},
		{"the kill lands after the marker rename", noticeCrashRenamed, 1},
		{"the kill lands beside a refused submit", noticeCrashSubmitFailed, 1},
	}
	seen := map[string]bool{}
	for _, window := range windows {
		window := window
		t.Run(window.name, func(t *testing.T) {
			runNoticeCrashWindow(t, window.mode, window.read)
			seen[window.mode] = true
		})
	}
	for _, mode := range []string{noticeCrashAdmitted, noticeCrashDrained, noticeCrashRenamed, noticeCrashSubmitFailed} {
		if !seen[mode] {
			t.Fatalf("the %q crash window was never exercised, want every point a stop can lose a notice at", mode)
		}
	}
}

// noticeCrashWindowEntry is the account line one child appends when it reaches
// its window: the window, the child process, the admitted identity and what the
// child did before it held.
type noticeCrashWindowEntry struct {
	Window        string `json:"window"`
	PID           int    `json:"pid"`
	ID            string `json:"id"`
	Read          int    `json:"read"`
	Refused       bool   `json:"refused,omitempty"`
	MarkerRenamed bool   `json:"marker_renamed,omitempty"`
}

// noticeCrashChild is the resident process one crash window kills: it reaches
// its window on the real inbox and holds, so the kill lands at that window.
func noticeCrashChild(t *testing.T, mode string) {
	t.Helper()
	dir := os.Getenv(noticeCrashDirEnv)
	marker := os.Getenv(noticeCrashMarkerEnv)
	ledger := os.Getenv(noticeCrashLedgerEnv)
	addr := testAddress("chat-a")
	inbox := durableInbox(t, filepath.Join(dir, testNoticeInboxDir))

	id, err := inbox.AdmitNotice(addr, "reincarnation", tagentmodel.Message{Content: noticeCrashBody}, marker, noticeConsumedSuffix)
	if err != nil {
		t.Fatalf("admit the notice: %v", err)
	}
	entry := noticeCrashWindowEntry{Window: mode, PID: os.Getpid(), ID: id}

	switch mode {
	case noticeCrashAdmitted:
	case noticeCrashDrained:
		msgs, ids := inbox.take(addr)
		if len(msgs) != 1 || len(ids) != 1 {
			t.Fatalf("the drain = %d messages under %d records, want one", len(msgs), len(ids))
		}
		entry.Read = len(msgs)
	case noticeCrashRenamed:
		msgs, ids := inbox.take(addr)
		if len(msgs) != 1 || len(ids) != 1 {
			t.Fatalf("the drain = %d messages under %d records, want one", len(msgs), len(ids))
		}
		entry.Read = len(msgs)
		inbox.observe = func(step string) {
			if step == noticeCrashRenamed {
				entry.MarkerRenamed = true
				noticeCrashHold(t, ledger, entry)
			}
		}
		inbox.Confirm(ids)
		t.Fatalf("the confirmation returned instead of stopping at the marker rename")
	case noticeCrashSubmitFailed:
		rig := newNoticeDriveRig(t, inbox)
		rig.refuseNext(errors.New("the host refused the material"))
		if err := rig.drive(t, "chat-a", "the question beside the refused submit"); err == nil {
			t.Fatalf("the refused submit answered no error")
		}
		entry.Read = rig.turns(noticeCrashBody)
		entry.Refused = true
	default:
		t.Fatalf("unknown crash window %q", mode)
	}

	noticeCrashHold(t, ledger, entry)
}

// runNoticeCrashWindow stops one child at its window and reads the recovery
// back: the restarted hook re-admits the same identity, the owed notice is
// delivered in exactly one turn, the confirmation writes the consumed marker,
// and the next process reads nothing.
func runNoticeCrashWindow(t *testing.T, mode string, wantRead int) {
	t.Helper()
	dir := t.TempDir()
	marker := writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
	ledger := filepath.Join(dir, "windows.jsonl")

	child := startNoticeCrashChild(t, mode, dir, marker, ledger)
	t.Cleanup(child.stop)
	entry := child.waitWindow(t, mode)
	child.kill(t)

	if entry.PID != child.pid {
		t.Fatalf("the %q account names pid %d, want the killed child %d", mode, entry.PID, child.pid)
	}
	if entry.ID == "" {
		t.Fatalf("the %q account names no admitted identity", mode)
	}
	if entry.Read != wantRead {
		t.Fatalf("the %q child read the notice %d time(s), want %d", mode, entry.Read, wantRead)
	}
	if got := entry.Refused; got != (mode == noticeCrashSubmitFailed) {
		t.Fatalf("the %q account refused = %t, want %t", mode, got, mode == noticeCrashSubmitFailed)
	}
	if got := entry.MarkerRenamed; got != (mode == noticeCrashRenamed) {
		t.Fatalf("the %q account marker-renamed = %t, want %t", mode, got, mode == noticeCrashRenamed)
	}

	inboxDir := filepath.Join(dir, testNoticeInboxDir)
	inbox := durableInbox(t, inboxDir)
	if len(inbox.records) != 1 {
		t.Fatalf("records after the %q stop = %d, want the one the child admitted", mode, len(inbox.records))
	}
	rec := inbox.records[entry.ID]
	if rec == nil {
		t.Fatalf("the %q stop lost the admitted identity %s", mode, entry.ID)
	}
	if rec.Content != noticeCrashBody {
		t.Fatalf("the %q stop changed the admitted body: %q", mode, rec.Content)
	}
	if rec.Source != "reincarnation" || rec.Marker != marker {
		t.Fatalf("the %q record = source %q marker %q, want the admission the child made", mode, rec.Source, rec.Marker)
	}
	noticeCrashMarkerState(t, marker, mode == noticeCrashRenamed)

	injectRestartNotice(inbox, noticeOps{address: testAddress("chat-a")}, nil, "tagent", dir, 0)
	if len(inbox.records) != 1 {
		t.Fatalf("records after the restarted hook = %d, want the one the %q stop left", len(inbox.records), mode)
	}
	if got := inbox.records[entry.ID]; got == nil || got.Content != noticeCrashBody {
		t.Fatalf("the restarted hook replaced the admitted notice: %+v", got)
	}

	rig := newNoticeDriveRig(t, inbox)
	if err := rig.drive(t, "chat-a", "the question after the restart"); err != nil {
		t.Fatalf("the turn after the %q restart: %v", mode, err)
	}
	if got := rig.turns(noticeCrashBody); got != 1 {
		t.Fatalf("turns that read the notice after the %q restart = %d, want the one the stop left owed", mode, got)
	}
	req := rig.lastRequest(t)
	if len(req.Input) != 2 || len(req.InputMeta) != 2 {
		t.Fatalf("the recovered turn carried %d messages under %d meta entries, want the notice and the question", len(req.Input), len(req.InputMeta))
	}
	if req.Input[0].Content != noticeCrashBody || req.Input[1].Content != "the question after the restart" {
		t.Fatalf("the recovered turn material = %q then %q, want the notice then the question", req.Input[0].Content, req.Input[1].Content)
	}
	if got := req.InputMeta[0][event.MetaKeyNoticeSource]; got != "reincarnation" {
		t.Fatalf("the recovered notice source = %q, want reincarnation", got)
	}
	if got, ok := req.InputMeta[1][event.MetaKeyNoticeSource]; ok {
		t.Fatalf("the user message of the recovered turn carried the notice source %q", got)
	}
	if req.TriggerSource != "user" {
		t.Fatalf("the recovered round trigger source = %q, want the user lineage", req.TriggerSource)
	}

	noticeCrashMarkerState(t, marker, true)
	if len(inbox.records) != 0 {
		t.Fatalf("records after the %q recovery = %d, want the notice confirmed", mode, len(inbox.records))
	}

	if err := rig.drive(t, "chat-a", "the question after the confirmation"); err != nil {
		t.Fatalf("the turn after the %q confirmation: %v", mode, err)
	}
	if got := rig.turns(noticeCrashBody); got != 1 {
		t.Fatalf("turns that read the confirmed notice after %q = %d, want one effective processing", mode, got)
	}

	restarted := durableInbox(t, inboxDir)
	if len(restarted.records) != 0 {
		t.Fatalf("records a further restart read after %q = %d, want none", mode, len(restarted.records))
	}
	if msgs, _ := restarted.take(testAddress("chat-a")); len(msgs) != 0 {
		t.Fatalf("a further restart reads the confirmed notice back: %+v", msgs)
	}
	injectRestartNotice(restarted, noticeOps{address: testAddress("chat-a")}, nil, "tagent", dir, 0)
	if len(restarted.records) != 0 {
		t.Fatalf("a consumed notice was admitted again after %q (%d records)", mode, len(restarted.records))
	}
	t.Logf("crash window evidence: window=%s pid=%d id=%s child_read=%d recovery_turns=1 marker=%s",
		mode, child.pid, entry.ID, entry.Read, marker+noticeConsumedSuffix)
}

// noticeCrashMarkerState pins the two states of the startup marker around one
// window: either the source marker is still waiting for a reader, or the
// confirmation renamed it and left the consumed marker beside it.
func noticeCrashMarkerState(t *testing.T, marker string, consumed bool) {
	t.Helper()
	_, sourceErr := os.Stat(marker)
	_, consumedErr := os.Stat(marker + noticeConsumedSuffix)
	if consumed {
		if !os.IsNotExist(sourceErr) {
			t.Fatalf("the source marker %s survived its confirmation (stat err %v)", marker, sourceErr)
		}
		if consumedErr != nil {
			t.Fatalf("the confirmation wrote no consumed marker %s: %v", marker+noticeConsumedSuffix, consumedErr)
		}
		return
	}
	if sourceErr != nil {
		t.Fatalf("the source marker %s disappeared before confirmation: %v", marker, sourceErr)
	}
	if !os.IsNotExist(consumedErr) {
		t.Fatalf("the consumed marker %s exists before confirmation (stat err %v)", marker+noticeConsumedSuffix, consumedErr)
	}
}

// noticeCrashChildProcess is the resident process one crash window kills.
type noticeCrashChildProcess struct {
	cmd     *exec.Cmd
	pid     int
	ledger  string
	output  bytes.Buffer
	done    chan struct{}
	waitErr error
}

// startNoticeCrashChild starts the resident process of one crash window.
func startNoticeCrashChild(t *testing.T, mode, dir, marker, ledger string) *noticeCrashChildProcess {
	t.Helper()
	child := &noticeCrashChildProcess{ledger: ledger, done: make(chan struct{})}
	child.cmd = exec.Command(os.Args[0], "-test.run=^TestNoticeSymptomCrashNoLoss$", "-test.timeout=5m")
	child.cmd.Env = append(os.Environ(),
		noticeCrashModeEnv+"="+mode,
		noticeCrashDirEnv+"="+dir,
		noticeCrashMarkerEnv+"="+marker,
		noticeCrashLedgerEnv+"="+ledger,
	)
	child.cmd.Stdout = &child.output
	child.cmd.Stderr = &child.output
	if err := child.cmd.Start(); err != nil {
		t.Fatalf("start the crash child: %v", err)
	}
	child.pid = child.cmd.Process.Pid
	go func() {
		child.waitErr = child.cmd.Wait()
		close(child.done)
	}()
	return child
}

// waitWindow waits until the child accounted for its window, so the parent
// kills a process that already reached it.
func (c *noticeCrashChildProcess) waitWindow(t *testing.T, window string) noticeCrashWindowEntry {
	t.Helper()
	deadline := time.Now().Add(noticeCrashReadyWait)
	for {
		if entry, ok := noticeCrashLedgerEntry(c.ledger, window); ok {
			return entry
		}
		select {
		case <-c.done:
			t.Fatalf("the child ended before its %q window: %v\n%s", window, c.waitErr, c.output.String())
		default:
		}
		if time.Now().After(deadline) {
			c.stop()
			t.Fatalf("the child reached no %q window within %s\n%s", window, noticeCrashReadyWait, c.output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// kill ends the child with the signal no graceful path produces and reports
// what it left behind.
func (c *noticeCrashChildProcess) kill(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
		t.Fatalf("the child ended before the kill: %v\n%s", c.waitErr, c.output.String())
	default:
	}
	if err := c.cmd.Process.Kill(); err != nil {
		<-c.done
		t.Fatalf("kill -9 the crash child %d: %v; child ended with %v\n%s", c.pid, err, c.waitErr, c.output.String())
	}
	<-c.done
	status, ok := c.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("the crash child state = %+v, want a process ended by SIGKILL", c.cmd.ProcessState)
	}
	if c.cmd.ProcessState.ExitCode() != -1 {
		t.Fatalf("the crash child exit = %d, want the signal exit no graceful path produces", c.cmd.ProcessState.ExitCode())
	}
	if strings.Contains(c.output.String(), "DATA RACE") {
		t.Fatalf("the crash child reported a data race: %s", c.output.String())
	}
}

// stop ends a child the check has finished with, so a failed assertion never
// leaves a held subprocess behind.
func (c *noticeCrashChildProcess) stop() {
	select {
	case <-c.done:
		return
	default:
	}
	_ = c.cmd.Process.Kill()
	<-c.done
}

// noticeCrashHold accounts for the window the child reached and holds the
// process open, so the kill lands at that window instead of at a convenient
// point beside it.
func noticeCrashHold(t *testing.T, ledger string, entry noticeCrashWindowEntry) {
	t.Helper()
	if err := noticeCrashAccount(ledger, entry); err != nil {
		t.Fatalf("account for the %q window: %v", entry.Window, err)
	}
	select {
	case <-time.After(noticeCrashHoldMax):
		t.Fatalf("the parent never killed the process at the %q window", entry.Window)
	}
}

// noticeCrashAccount appends one account line to the ledger the parent reads.
func noticeCrashAccount(path string, entry noticeCrashWindowEntry) error {
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(body, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

// noticeCrashLedgerEntry answers the account line of the named window.
func noticeCrashLedgerEntry(path, window string) (noticeCrashWindowEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return noticeCrashWindowEntry{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry noticeCrashWindowEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Window == window {
			return entry, true
		}
	}
	return noticeCrashWindowEntry{}, false
}

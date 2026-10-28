// The single-drive checks of the settle face: one notification starts exactly
// one follow-up turn across a process stop. A kill that lands after the
// terminal fact and before the answer left the sender is read back by a second
// process that closes the entry beside the landed attempt instead of driving a
// second turn, an attempt already landed is closed beside its receipt, and a
// stopped attempt resumes without a second instruction in the transcript.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// settleDriveModeEnv, settleDriveConfigEnv, settleDriveRootEnv and
// settleDriveOutEnv name the work order a single-drive child reads: the resident
// process boots the configuration the parent wrote, drives one delegating turn
// and holds the follow-up delivery open until the parent kills it.
const (
	settleDriveModeEnv   = "TAGENT_SETTLE_DRIVE_MODE"
	settleDriveConfigEnv = "TAGENT_SETTLE_DRIVE_CONFIG"
	settleDriveRootEnv   = "TAGENT_SETTLE_DRIVE_ROOT"
	settleDriveOutEnv    = "TAGENT_SETTLE_DRIVE_OUT"
)

// settleDriveHeld names the one work order the child runs in, and
// settleDriveLedgerName names the account file it appends its held delivery to.
const (
	settleDriveHeld       = "held"
	settleDriveLedgerName = "deliveries.jsonl"
	settleDriveReadyWait  = 90 * time.Second
	settleDriveQuiet      = 3 * time.Second
)

// settleDriveSession names the one session every single-drive check drives.
const settleDriveSession = "session-settle-single-drive"

// settleDriveWorkerBody, settleDriveEntryBody and settleDriveFollowUpBody are
// the three bodies the scripted provider answers with: the worker report, the
// entry turn's own answer, and the follow-up answer the child holds in its
// sender.
const (
	settleDriveWorkerBody   = "the delegated worker reports the total is 42"
	settleDriveEntryBody    = "the total the worker computed is 42"
	settleDriveFollowUpBody = "the settle reached the user exactly once across the kill"
)

// TestSettleWakeSingleDrive pins the one-drive semantics of one notification across a process stop and a restart.
//   - a kill between the landed terminal fact and the held delivery closes the entry beside its receipt, so one follow-up turn answers the provider
//   - a landed attempt is closed with its receipt and drives no turn, while an in-flight attempt drives the one turn it owes
//   - a stopped attempt resumes with one instruction in the transcript and the entry consumed once
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
func TestSettleWakeSingleDrive(t *testing.T) {
	if mode := os.Getenv(settleDriveModeEnv); mode != "" {
		settleDriveChild(t, mode)
		return
	}
	t.Run("a kill after the terminal fact drives exactly one follow-up turn", runSingleDriveAcrossKill)
	t.Run("a landed attempt is closed beside its receipt instead of driven again", runSingleDriveLandedAttempt)
	t.Run("a stopped attempt resumes without a second instruction", runSingleDriveResume)
}

// settleDriveChild is the resident process one single-drive check kills: it
// boots the configuration the parent wrote, drives the one delegating turn and
// then holds the process open, so the follow-up delivery the sender holds is the
// only thing the parent waits for before the kill.
func settleDriveChild(t *testing.T, mode string) {
	t.Helper()
	if mode != settleDriveHeld {
		t.Fatalf("unknown single-drive work order %q", mode)
	}
	ledger := filepath.Join(os.Getenv(settleDriveOutEnv), settleDriveLedgerName)
	host, err := assembly.Boot(os.Getenv(settleDriveConfigEnv), assembly.WithSender(&settleDriveHeldSender{ledger: ledger}))
	if err != nil {
		t.Fatalf("boot the resident host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: settleDriveSession}
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
	}); err != nil {
		t.Fatalf("the delegating turn ended by error: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("the parent never killed the resident process")
		case <-time.After(time.Second):
		}
	}
}

// settleDriveHeldSender is the outbound face of the resident process: every
// body but the follow-up answer passes through, and the follow-up answer is
// accounted for and then held, so the kill lands after the turn landed its
// terminal fact and before the delivery it confirms.
type settleDriveHeldSender struct {
	ledger string
}

// Send records the follow-up answer and holds the delivery open.
func (s *settleDriveHeldSender) Send(ctx context.Context, target event.Address, content string) error {
	if content != settleDriveFollowUpBody {
		return nil
	}
	settleDriveAppendLine(s.ledger, map[string]any{
		"event":  "delivering",
		"pid":    os.Getpid(),
		"target": target.SessionID,
		"body":   content,
	})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Hour):
		}
	}
}

// settleDriveAppendLine appends one account line to the ledger the parent reads,
// synced so the parent never reads a line still in flight.
func settleDriveAppendLine(path string, line map[string]any) {
	body, err := json.Marshal(line)
	if err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	if _, err := file.Write(append(body, '\n')); err != nil {
		return
	}
	_ = file.Sync()
}

// settleDriveChildProcess is the resident process one single-drive check starts
// as its own process over the storage root the parent names.
type settleDriveChildProcess struct {
	cmd      *exec.Cmd
	ledger   string
	pid      int
	exitCode int
	stderr   bytes.Buffer
}

// startSettleDriveChild starts the resident process of one single-drive check.
func startSettleDriveChild(t *testing.T, configPath, root, out string) *settleDriveChildProcess {
	t.Helper()
	child := &settleDriveChildProcess{ledger: filepath.Join(out, settleDriveLedgerName)}
	child.cmd = exec.Command(os.Args[0], "-test.run=^TestSettleWakeSingleDrive$", "-test.timeout=10m")
	child.cmd.Env = append(os.Environ(),
		settleDriveModeEnv+"="+settleDriveHeld,
		settleDriveConfigEnv+"="+configPath,
		settleDriveRootEnv+"="+root,
		settleDriveOutEnv+"="+out,
	)
	child.cmd.Stderr = &child.stderr
	if err := child.cmd.Start(); err != nil {
		t.Fatalf("start the resident child: %v", err)
	}
	child.pid = child.cmd.Process.Pid
	return child
}

// waitLedgerEvent waits until the child accounted for the named event, so the
// parent kills a process that already reached that point.
func (c *settleDriveChildProcess) waitLedgerEvent(t *testing.T, name string) {
	t.Helper()
	deadline := time.Now().Add(settleDriveReadyWait)
	for time.Now().Before(deadline) {
		if settleDriveEventsNamed(c.ledger, name) > 0 {
			return
		}
		if c.cmd.ProcessState != nil {
			t.Fatalf("the child ended before its %q account line; stderr = %s", name, c.stderr.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("the child accounted for no %q line within %s; stderr = %s", name, settleDriveReadyWait, c.stderr.String())
}

// kill ends the child with the signal no graceful path produces and reports
// what it left behind.
func (c *settleDriveChildProcess) kill(t *testing.T) {
	t.Helper()
	if err := c.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill -9 the resident child %d: %v", c.pid, err)
	}
	_ = c.cmd.Wait()
	if c.cmd.ProcessState != nil {
		c.exitCode = c.cmd.ProcessState.ExitCode()
	}
	status, ok := c.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("the resident child state = %+v, want a process ended by SIGKILL", c.cmd.ProcessState)
	}
	if c.exitCode != -1 {
		t.Fatalf("the resident child exit = %d, want the signal exit no graceful path produces", c.exitCode)
	}
	if strings.Contains(c.stderr.String(), "DATA RACE") {
		t.Fatalf("the resident child reported a data race: %s", c.stderr.String())
	}
}

// settleDriveEventsNamed counts the account lines the child wrote for one named
// event, dropping a line still in flight.
func settleDriveEventsNamed(path, name string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Event == name {
			count++
		}
	}
	return count
}

// settleDriveTally is the account a single-drive check keeps of what the
// provider was asked: the entry turns answered beside the follow-up turns among
// them.
type settleDriveTally struct {
	mu         sync.Mutex
	entryCalls int
	followUps  int
}

// counts reports the two numbers the check holds the provider to.
func (a *settleDriveTally) counts() (entryCalls, followUps int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.entryCalls, a.followUps
}

// runSingleDriveAcrossKill drives one delegated settle in a real process, kills
// that process after the follow-up turn landed its terminal fact and while its
// answer is held in the sender, and boots the same root again: the restarted
// process reads the landed attempt beside its receipt, so the notification
// starts exactly one follow-up turn, and the answer the kill left owed is
// delivered once by the restart instead of being lost with the sender that never
// confirmed it.
func runSingleDriveAcrossKill(t *testing.T) {
	wakeEnv(t)

	provider := newScriptedProvider(t)
	tally := &settleDriveTally{}
	provider.answer = func(index int) string {
		if provider.servedModels()[index] == "model-worker" {
			return scriptedAnswerOf(settleDriveWorkerBody)
		}
		tally.mu.Lock()
		defer tally.mu.Unlock()
		tally.entryCalls++
		switch tally.entryCalls {
		case 1:
			return wakeDelegate("call-worker", "compute the total")
		case 2:
			return scriptedAnswerOf(settleDriveEntryBody)
		default:
			tally.followUps++
			return scriptedAnswerOf(settleDriveFollowUpBody)
		}
	}
	root, out := t.TempDir(), t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))

	child := startSettleDriveChild(t, configPath, root, out)
	child.waitLedgerEvent(t, "delivering")
	child.kill(t)

	entryCalls, followUps := tally.counts()
	if entryCalls != 3 || followUps != 1 {
		t.Fatalf("entry turns before the kill = %d beside %d follow-ups, want the delegating decision, its answer and the one follow-up", entryCalls, followUps)
	}

	restored, sender := bootRestoredSettleDrive(t, configPath)
	defer restored.Close(context.Background())
	time.Sleep(settleDriveQuiet)

	entryAfter, followAfter := tally.counts()
	if followAfter != followUps {
		t.Fatalf("follow-up turns after the restart = %d, want the %d the landed attempt closes", followAfter, followUps)
	}
	if entryAfter != entryCalls {
		t.Fatalf("entry turns after the restart = %d, want the %d the landed attempt closes", entryAfter, entryCalls)
	}
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != settleDriveFollowUpBody {
		t.Fatalf("the restarted process delivered %q, want the one owed copy of %q the kill left behind", bodies, settleDriveFollowUpBody)
	}
	facts := restored.Run().SessionFacts(event.Address{AgentID: "user", SessionID: settleDriveSession})
	if got := settleDriveNoticeFacts(facts); got != 1 {
		t.Fatalf("notice facts after the restart = %d, want the one instruction the notification commits under its own identity", got)
	}
	if !settleDriveCarriesBody(facts, settleDriveFollowUpBody) {
		t.Fatalf("the restarted chain carries no %q output, want the landed terminal fact of the held turn", settleDriveFollowUpBody)
	}
	t.Logf("single-drive kill evidence: pid=%d entry_turns=%d follow_ups=%d delivered_after_restart=%d", child.pid, entryCalls, followUps, len(sender.bodies()))
}

// runSingleDriveLandedAttempt drives a real settle driver over a mailbox whose
// account it scripts: an attempt already landed its completed terminal is
// closed with its receipt instead of driving a second turn, while an in-flight
// attempt drives the one turn it owes and lands its in-flight marker before the
// drive.
func runSingleDriveLandedAttempt(t *testing.T) {
	landed := newSettleDriveWake(state.SettleAttempt{Terminal: model.KindCompleted.String()})
	landedFires := driveScriptedSeat(t, landed)
	if landedFires != 0 {
		t.Fatalf("follow-up turns beside a landed attempt = %d, want none: the entry is closed with its receipt", landedFires)
	}
	if got := landed.receipts(); got != 1 {
		t.Fatalf("consumption receipts beside a landed attempt = %d, want the one the close writes", got)
	}
	if got := landed.recordCount(); got != 0 {
		t.Fatalf("attempt records written beside a landed attempt = %d, want none", got)
	}

	inFlight := newSettleDriveWake(state.SettleAttempt{})
	inFlightFires := driveScriptedSeat(t, inFlight)
	if inFlightFires != 1 {
		t.Fatalf("follow-up turns beside an in-flight attempt = %d, want the one turn it owes", inFlightFires)
	}
	if got := inFlight.receipts(); got != 1 {
		t.Fatalf("consumption receipts beside an in-flight attempt = %d, want the one the finished turn writes", got)
	}
	recorded := inFlight.recordedAttempts()
	if len(recorded) != 1 || recorded[0].Terminal != "" {
		t.Fatalf("attempt records beside an in-flight attempt = %+v, want the one in-flight marker written before the drive", recorded)
	}
}

// runSingleDriveResume stops the first attempt of one notification mid-flight,
// boots the same root again and reads the resume back: the one owed turn runs
// again, its answer is delivered once, and the transcript carries the
// instruction once.
func runSingleDriveResume(t *testing.T) {
	wakeEnv(t)

	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var workerOnce sync.Once
	followUpStarted := make(chan struct{})
	holdFollowUp := make(chan struct{})
	var holdOnce sync.Once
	releaseHold := func() { holdOnce.Do(func() { close(holdFollowUp) }) }
	defer releaseHold()

	var mu sync.Mutex
	entryCalls := 0
	provider.answer = func(index int) string {
		if provider.servedModels()[index] == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(settleDriveWorkerBody)
		}
		mu.Lock()
		entryCalls++
		call := entryCalls
		mu.Unlock()
		if call == 1 {
			return wakeDelegate("call-worker", "compute the total")
		}
		if call == 2 {
			close(followUpStarted)
			<-holdFollowUp
		}
		return scriptedAnswerOf(settleDriveFollowUpBody)
	}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-entry", wakeAgentSections))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: settleDriveSession}

	invokeCtx, invokeCancel := context.WithCancel(ctx)
	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(invokeCtx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
		})
		invoked <- err
	}()
	select {
	case <-workerSeen:
	case <-time.After(30 * time.Second):
		invokeCancel()
		t.Fatal("the delegated turn never reached its provider")
	}
	invokeCancel()
	<-invoked
	close(releaseWorker)
	select {
	case <-followUpStarted:
	case <-time.After(30 * time.Second):
		t.Fatal("the follow-up turn never reached its provider")
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host beside the stopped attempt: %v", err)
	}
	releaseHold()

	restored, restoredSender := bootRestoredSettleDrive(t, configPath)
	defer restored.Close(context.Background())
	waitForSettle(t, "the owed notice to resume", func() bool {
		return len(restoredSender.bodies()) >= 1
	})
	mu.Lock()
	turns := entryCalls
	mu.Unlock()
	if turns < 3 {
		t.Fatalf("entry model turns = %d, want the delegating decision, the stopped attempt and the one resumed turn", turns)
	}
	if bodies := restoredSender.bodies(); len(bodies) != 1 || bodies[0] != settleDriveFollowUpBody {
		t.Fatalf("delivered bodies = %q, want the one resumed answer", bodies)
	}
	facts := restored.Run().SessionFacts(addr)
	if got := settleDriveNoticeFacts(facts); got != 1 {
		t.Fatalf("notice facts after the resume = %d, want the one instruction the notification commits under its own identity", got)
	}
	if got := settleDriveOutputsOf(facts, settleDriveFollowUpBody); got != 1 {
		t.Fatalf("follow-up outputs after the resume = %d, want the one answer the resumed turn landed", got)
	}
}

// settleDriveWake is the mailbox account the attempt checks drive a real settle
// driver from: one pending seat whose attempt record the check scripts, beside
// the receipts and releases the driver writes on it.
type settleDriveWake struct {
	mu       sync.Mutex
	seat     state.WorkSeat
	attempt  state.SettleAttempt
	held     bool
	claimed  bool
	consumed int
	released int
	recorded []state.SettleAttempt
}

// newSettleDriveWake returns the one-seat account with the attempt record given.
func newSettleDriveWake(attempt state.SettleAttempt) *settleDriveWake {
	return &settleDriveWake{
		seat: state.WorkSeat{
			Address:   event.Address{AgentID: "user", SessionID: settleDriveSession},
			Instance:  event.InstanceID("instance-single-drive"),
			AcceptSeq: 1,
			Notice: task.Notice{
				TaskID: "task-1",
				Round:  1,
				Kind:   task.NoticeSettled,
				Seq:    0,
				Event:  "settle-1",
			},
			Claim: state.ClaimPending,
		},
		attempt: attempt,
		held:    true,
	}
}

// RestoreOwed reports nothing to restore: the account already carries its seat.
func (w *settleDriveWake) RestoreOwed(context.Context) error { return nil }

// Unconsumed reports the one seat the account owes until it is consumed.
func (w *settleDriveWake) Unconsumed() []state.WorkSeat {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.consumed > 0 {
		return nil
	}
	return []state.WorkSeat{w.seat}
}

// Claim takes the one seat for this executor alone.
func (w *settleDriveWake) Claim(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.claimed {
		return state.ErrClaimConflict
	}
	w.claimed = true
	return nil
}

// Release hands the claimed seat back to the pending pool.
func (w *settleDriveWake) Release(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.released++
	w.claimed = false
	return nil
}

// Consume records the completed consumption of the claimed seat.
func (w *settleDriveWake) Consume(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.consumed++
	w.claimed = false
	return nil
}

// Attempt reports the attempt record the check scripted.
func (w *settleDriveWake) Attempt(context.Context, task.Notice) (state.SettleAttempt, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.attempt, w.held, nil
}

// RecordAttempt records one attempt the driver writes.
func (w *settleDriveWake) RecordAttempt(_ context.Context, _ task.Notice, attempt state.SettleAttempt) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recorded = append(w.recorded, attempt)
	w.attempt, w.held = attempt, true
	return nil
}

// receipts reports how many consumption receipts the driver wrote.
func (w *settleDriveWake) receipts() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.consumed
}

// recordCount reports how many attempt records the driver wrote.
func (w *settleDriveWake) recordCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.recorded)
}

// recordedAttempts reports the attempt records the driver wrote, in order.
func (w *settleDriveWake) recordedAttempts() []state.SettleAttempt {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]state.SettleAttempt(nil), w.recorded...)
}

// driveScriptedSeat drives one real settle driver over the scripted account
// until the pump rests, and reports how many follow-up turns the drive fired.
func driveScriptedSeat(t *testing.T, wake *settleDriveWake) int {
	t.Helper()
	fires := 0
	lookup := func(_ context.Context, id event.EventID) (event.Event, error) {
		return event.Event{
			Key:    id,
			Type:   event.TypeTaskSettled,
			TaskID: wake.seat.Notice.TaskID,
			Meta:   map[string]string{event.MetaKeyNoticeKind: string(wake.seat.Notice.Kind)},
		}, nil
	}
	fire := func(context.Context, event.Address, event.Event) error {
		wake.mu.Lock()
		fires++
		wake.mu.Unlock()
		return nil
	}
	driver := assembly.NewSettleDriver(lookup, func() assembly.SettleWake { return wake }, fire)
	defer driver.Stop()
	driver.Wake()
	waitForSettle(t, "the settle pump to close the scripted seat", func() bool {
		return wake.receipts() > 0
	})
	return fires
}

// bootRestoredSettleDrive boots the host over the root the check already wrote
// and answers the host beside the recorder of what it delivers.
func bootRestoredSettleDrive(t *testing.T, configPath string) (*assembly.Host, *deliveryRecorder) {
	t.Helper()
	sender := &deliveryRecorder{}
	restored, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the restored host: %v", err)
	}
	return restored, sender
}

// settleDriveNoticeFacts counts the notice facts of one session: the feedback
// facts the follow-up range commits, stamped with the task trigger source.
func settleDriveNoticeFacts(facts []event.Event) int {
	count := 0
	for _, fact := range facts {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyTriggerSource] == "task" {
			count++
		}
	}
	return count
}

// settleDriveCarriesBody reports whether one session carries the assistant body
// given as a committed output fact.
func settleDriveCarriesBody(facts []event.Event, body string) bool {
	return settleDriveOutputsOf(facts, body) > 0
}

// settleDriveOutputsOf counts the output facts of one session whose assistant
// body is the text given.
func settleDriveOutputsOf(facts []event.Event, body string) int {
	count := 0
	for _, fact := range facts {
		if fact.Type == event.TypeAgentOutput && strings.TrimSpace(fact.Message.Content) == body {
			count++
		}
	}
	return count
}

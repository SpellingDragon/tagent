// The notice recovery lattice of the host: a notification is settled by the
// durable consumption result the target records, never by acceptance, by the
// committed material or by a hand-over to a range whose current set already
// stopped. The checks walk the recovery boundaries in order — accepted, material
// committed, result recorded, consumed — beside the three terminals a follow-up
// turn can record and the three notification kinds, the real-host failed result
// that settles beside its observation without replaying across a restart, and
// the backflow that must keep the notice owed once the current consumer set
// stopped.
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
package host

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// latticeSession names the session the real-host checks of the lattice drive.
const latticeSession = "session-notice-lattice"

// TestNoticeConsumptionRecoveryMatrix pins the one settlement point of a notification over the formal settle account and the real host.
//   - positive: a recorded result (completed, failed or cancelled) closes the notice beside its consumption receipt and drives no second turn
//   - positive: accepted-only and in-flight-attempt notices stay owed and the one turn they owe is driven
//   - positive: a consumed notice never drives again and its receipt stays the one completion
//   - negative: a failed follow-up result is a consumption result and does not replay across a restart
//   - negative: a backflow beside a stopped current set consumes nothing and keeps the durable owed notice
//   - consumer: the restart anchors boot the real Host over the storage the first process wrote and deliver through its own settle driver
func TestNoticeConsumptionRecoveryMatrix(t *testing.T) {
	t.Run("a recorded result closes the notice for every terminal", runLatticeRecordedResult)
	t.Run("a notice without a result stays owed and drives once", runLatticeWithoutResult)
	t.Run("every terminal kind is driven once and settled", runLatticeTerminalKinds)
	t.Run("a consumed notice never drives again", runLatticeConsumedStaysClosed)
	t.Run("a failed follow-up result does not replay across a restart", runLatticeFailedFollowUp)
	t.Run("a backflow beside a stopped current keeps the notice owed", runLatticeBackflowStopped)
}

// latticeWake is the one-seat settle account the boundary checks drive a real
// settle driver over: the attempt the account holds, the claim and release the
// driver writes, and the receipt a completed consumption records. The account
// answers as the durable mailbox does across a restart: the consumed receipt is
// the only state that survives, so a second driver over it reads no seat.
type latticeWake struct {
	mu       sync.Mutex
	seat     state.WorkSeat
	attempt  state.SettleAttempt
	held     bool
	claimed  bool
	consumed int
	released int
	recorded []state.SettleAttempt
}

// newLatticeWake returns one seat of the named notification kind beside the
// attempt record the check scripts.
func newLatticeWake(kind task.NoticeKind, attempt state.SettleAttempt, held bool) *latticeWake {
	return &latticeWake{
		seat: state.WorkSeat{
			Address:   event.Address{AgentID: "user", SessionID: latticeSession},
			Instance:  event.InstanceID("instance-notice-lattice"),
			AcceptSeq: 1,
			Notice: task.Notice{
				TaskID: "task-notice-lattice",
				Round:  1,
				Kind:   kind,
				Seq:    0,
				Event:  "fact-notice-lattice",
			},
			Claim: state.ClaimPending,
		},
		attempt: attempt,
		held:    held,
	}
}

// Unconsumed reports the one seat while its consumption has not completed.
func (w *latticeWake) Unconsumed() []state.WorkSeat {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.consumed > 0 {
		return nil
	}
	return []state.WorkSeat{w.seat}
}

// Claim takes the one seat for this executor alone.
func (w *latticeWake) Claim(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.claimed {
		return state.ErrClaimConflict
	}
	w.claimed = true
	return nil
}

// Release hands the claimed seat back to the pending pool.
func (w *latticeWake) Release(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.released++
	w.claimed = false
	return nil
}

// Consume records the completed consumption of the claimed seat.
func (w *latticeWake) Consume(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.consumed++
	w.claimed = false
	return nil
}

// Attempt reports the attempt record the account holds.
func (w *latticeWake) Attempt(context.Context, task.Notice) (state.SettleAttempt, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.attempt, w.held, nil
}

// RecordAttempt records one attempt the driver writes.
func (w *latticeWake) RecordAttempt(_ context.Context, _ task.Notice, attempt state.SettleAttempt) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recorded = append(w.recorded, attempt)
	w.attempt, w.held = attempt, true
	return nil
}

// receipts reports how many consumption receipts the account holds.
func (w *latticeWake) receipts() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.consumed
}

// recordCount reports how many attempt records the driver wrote.
func (w *latticeWake) recordCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.recorded)
}

// recordedAttempts reports the attempt records the driver wrote, in order.
func (w *latticeWake) recordedAttempts() []state.SettleAttempt {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]state.SettleAttempt(nil), w.recorded...)
}

// attemptNow reports the attempt record the account holds.
func (w *latticeWake) attemptNow() (state.SettleAttempt, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.attempt, w.held
}

// latticeDrive drives one real settle driver over the scripted account until
// the account answers a completed consumption, and reports how many follow-up
// turns the drive fired. The lookup answers the terminal delivery fact of the
// seat's own notification, exactly as the run's committed fact does.
func latticeDrive(t *testing.T, wake *latticeWake) int {
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
	driver.DeclareScope(string(wake.seat.Address.AgentID), string(wake.seat.Address.AgentID))
	defer driver.Stop()
	driver.Wake()
	waitForSettle(t, "the settle pump to complete one consumption", func() bool {
		return wake.receipts() > 0
	})
	return fires
}

// runLatticeRecordedResult walks the consumption-result boundary: a turn that
// already recorded completed, failed or cancelled is a consumption result, so
// the notice closes beside its receipt and no second turn is driven.
func runLatticeRecordedResult(t *testing.T) {
	for _, terminal := range []model.Kind{model.KindCompleted, model.KindFailed, model.KindCancelled} {
		t.Run(terminal.String(), func(t *testing.T) {
			wake := newLatticeWake(task.NoticeSettled, state.SettleAttempt{Terminal: terminal.String()}, true)
			if fires := latticeDrive(t, wake); fires != 0 {
				t.Fatalf("follow-up turns beside a recorded %s result = %d, want none: the notice is already processed", terminal, fires)
			}
			if got := wake.receipts(); got != 1 {
				t.Fatalf("consumption receipts beside a recorded %s result = %d, want the one the close writes", terminal, got)
			}
			if got := wake.recordCount(); got != 0 {
				t.Fatalf("attempt records beside a recorded %s result = %+v, want none: the result is not overwritten by a retry", terminal, wake.recordedAttempts())
			}
			if attempt, held := wake.attemptNow(); !held || attempt.Terminal != terminal.String() {
				t.Fatalf("attempt after the close = (%+v, %t), want the recorded %s result kept", attempt, held, terminal)
			}
		})
	}
}

// runLatticeWithoutResult walks the two boundaries before a consumption result:
// accepted only, and accepted with the material committed and the turn in
// flight. Neither may settle the notice; the one turn it owes is driven and the
// attempt it leaves before the drive stays in flight.
func runLatticeWithoutResult(t *testing.T) {
	cases := []struct {
		name    string
		attempt state.SettleAttempt
		held    bool
	}{
		{name: "accepted only", attempt: state.SettleAttempt{}, held: false},
		{name: "material committed and attempt in flight", attempt: state.SettleAttempt{}, held: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wake := newLatticeWake(task.NoticeSettled, c.attempt, c.held)
			if fires := latticeDrive(t, wake); fires != 1 {
				t.Fatalf("follow-up turns beside %s = %d, want the one turn the notice owes", c.name, fires)
			}
			if got := wake.receipts(); got != 1 {
				t.Fatalf("consumption receipts beside %s = %d, want the one the finished turn writes", c.name, got)
			}
			recorded := wake.recordedAttempts()
			if len(recorded) != 1 || recorded[0].Terminal != "" {
				t.Fatalf("attempt records beside %s = %+v, want the one in-flight marker written before the drive", c.name, recorded)
			}
		})
	}
}

// runLatticeTerminalKinds walks the three terminal notification kinds over the
// same boundary: every kind is a notice the settle face drives exactly once and
// closes beside its receipt.
func runLatticeTerminalKinds(t *testing.T) {
	for _, kind := range []task.NoticeKind{task.NoticeSettled, task.NoticeFailed, task.NoticeCancelled} {
		t.Run(string(kind), func(t *testing.T) {
			wake := newLatticeWake(kind, state.SettleAttempt{}, false)
			if fires := latticeDrive(t, wake); fires != 1 {
				t.Fatalf("follow-up turns of the %s notice = %d, want the one turn it owes", kind, fires)
			}
			if got := wake.receipts(); got != 1 {
				t.Fatalf("consumption receipts of the %s notice = %d, want the one the finished turn writes", kind, got)
			}
		})
	}
}

// runLatticeConsumedStaysClosed walks the consumed boundary: a second driver
// over the same account — the restart shape — reads no seat and drives nothing.
func runLatticeConsumedStaysClosed(t *testing.T) {
	wake := newLatticeWake(task.NoticeSettled, state.SettleAttempt{Terminal: model.KindCompleted.String()}, true)
	if fires := latticeDrive(t, wake); fires != 0 {
		t.Fatalf("follow-up turns beside a recorded result = %d, want none", fires)
	}
	before := wake.receipts()
	if fires := latticeDrive(t, wake); fires != 0 {
		t.Fatalf("follow-up turns after the consumption = %d, want none: a consumed notice never replays", fires)
	}
	if got := wake.receipts(); got != before {
		t.Fatalf("consumption receipts after the second read = %d, want the %d the completed consumption recorded", got, before)
	}
}

// runLatticeFailedFollowUp drives one real delegated settle whose follow-up
// turn ends failed, closes the host and boots the same storage again: the
// failed result is the notification's consumption result, so it closes beside
// its receipt, lands the failure as an observation the address reads in a real
// model request, and the restart drives no second turn.
func runLatticeFailedFollowUp(t *testing.T) {
	wakeEnv(t)

	const (
		workerBody   = "the delegated worker reports the total is 9"
		entryBody    = "the entry accepted the delegation"
		failureCause = "the follow-up provider refused the request"
	)
	provider := newScriptedProvider(t)
	workerSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	observationSeen := make(chan struct{})
	var workerOnce, observationOnce sync.Once
	var mu sync.Mutex
	entryCalls, followUps := 0, 0
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Model == "model-worker" {
			workerOnce.Do(func() { close(workerSeen) })
			<-releaseWorker
			return scriptedAnswerOf(workerBody), nil
		}
		if scriptedRequestCarriesText(request, failureCause) {
			observationOnce.Do(func() { close(observationSeen) })
			return scriptedAnswerOf("the failure observation reached the model"), nil
		}
		mu.Lock()
		defer mu.Unlock()
		if settleDriveReadsMaterial(request, workerBody) {
			followUps++
			return "", fmt.Errorf(failureCause)
		}
		entryCalls++
		if entryCalls == 1 {
			return wakeDelegate("call-worker", "compute the total"), nil
		}
		return scriptedAnswerOf(entryBody), nil
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
	addr := event.Address{AgentID: "user", SessionID: latticeSession}

	invoked := make(chan error, 1)
	go func() {
		_, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address: addr,
			Input:   []model.Message{{Role: model.RoleUser, Content: "delegate the total to the worker"}},
		})
		invoked <- err
	}()
	select {
	case <-workerSeen:
	case <-time.After(30 * time.Second):
		t.Fatal("the delegated turn never reached its provider")
	}
	waitForSettle(t, "the entry turn to end on its own answer", func() bool {
		return settleDriveOutputsOf(host.Run().SessionFacts(addr), entryBody) > 0
	})
	if err := <-invoked; err != nil {
		t.Fatalf("the entry turn ended by error: %v", err)
	}
	close(releaseWorker)

	waitForSettle(t, "the follow-up turn to end failed", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return followUps >= 1
	})
	taskID := latticeSettleTaskID(host.Run().SessionFacts(addr))
	if taskID == "" {
		t.Fatalf("the session carries no delivered settle fact to read the task from: %+v", host.Run().SessionFacts(addr))
	}

	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host beside the failed result: %v", err)
	}
	mu.Lock()
	closed := followUps
	mu.Unlock()
	if closed != 1 {
		t.Fatalf("follow-up turns beside the failed result = %d, want the one turn: a failed result must not be replayed", closed)
	}
	if !latticeConsumptionRecorded(t, root, taskID) {
		t.Fatalf("no consumption receipt stands beside the failed result of task %s, want the one the close writes", taskID)
	}
	report := host.CloseReport()
	if report.Outcome != agent.CloseQuiesced || !report.Drained {
		t.Fatalf("close report = (%+v), want the failed result settled and the close at rest", report)
	}
	failed := settledFailureInvocationsOf(host.Run().SessionFacts(addr))
	if len(failed) != 1 {
		t.Fatalf("settled failures = %v, want the one follow-up answer the failed result closed", failed)
	}
	if !slices.Equal(report.Failed, failed) {
		t.Fatalf("close report names failed answers %v, want the settled failure %v the chain records", report.Failed, failed)
	}
	observations := failureObservationFactsOf(host.Run().SessionFacts(addr))
	if len(observations) != 1 || !strings.Contains(observations[0].Message.Content, failureCause) {
		t.Fatalf("failure observations = %+v, want the one carrying the raw cause %q", observations, failureCause)
	}
	if observations[0].Owner != addr || observations[0].Source != addr || observations[0].Target != addr {
		t.Fatalf("failure observation addresses = (owner %+v, source %+v, target %+v), want the driven address %+v", observations[0].Owner, observations[0].Source, observations[0].Target, addr)
	}
	select {
	case <-observationSeen:
	default:
		t.Fatal("no real model request carried the settled failure, want the failure observation driven to its address")
	}

	restored, _ := bootRestoredSettleDrive(t, configPath)
	defer restored.Close(context.Background())
	time.Sleep(1500 * time.Millisecond)
	mu.Lock()
	afterRestart := followUps
	mu.Unlock()
	if afterRestart != closed {
		t.Fatalf("follow-up turns after the restart = %d, want the %d the failed result already closed: no replay", afterRestart, closed)
	}
	if readBack := settledFailureInvocationsOf(restored.Run().SessionFacts(addr)); !slices.Equal(readBack, failed) {
		t.Fatalf("settled failures after the restart = %v, want the %v the first process named", readBack, failed)
	}
}

// latticeSettleTaskID reports the task identity of the delivered settle fact the
// session carries, so a check reads the durable receipt of the notification
// instead of guessing the identity.
func latticeSettleTaskID(facts []event.Event) string {
	for _, fact := range facts {
		if fact.Type == event.TypeTaskSettled && fact.Meta[event.MetaKeySettleTriggerSource] != "" && fact.TaskID != "" {
			return string(fact.TaskID)
		}
	}
	return ""
}

// latticeConsumptionRecorded reports whether the storage root holds the
// consumption receipt of one task: the durable record of one completed logical
// consumption, read back after the close.
func latticeConsumptionRecorded(t *testing.T, root, taskID string) bool {
	t.Helper()
	for key := range tierKeys(t, root, "tagent/task/v1/consume:") {
		if strings.Contains(key, taskID) {
			return true
		}
	}
	return false
}

// runLatticeBackflowStopped drives one native call whose result lands after the
// run's current consumer set stopped: the settled notification must stay owed on
// the durable account and the stopped current must not record a consumption
// receipt for a hand-over nobody read.
func runLatticeBackflowStopped(t *testing.T) {
	root := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	hold := &latticeHoldTool{name: "hold", entered: entered, release: release}
	scriptedModel := &entryModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		if latticeSawToolResult(req, "hold") {
			return entryFinalTurn("the held result arrived")
		}
		return entryToolTurn(entryCall("call-hold", "hold", "{}"))
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run, err := assembly.Assemble(ctx,
		&config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}},
		map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: scriptedModel, Tools: []trpctool.CallableTool{hold}},
		},
		assembly.WithGovernance(passPolicy{}),
		assembly.WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if err != nil {
		t.Fatalf("assemble the runtime beside the held call: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: latticeSession}
	invoked := make(chan error, 1)
	go func() {
		_, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-notice-lattice",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hold the result under way"}},
		})
		invoked <- err
	}()
	select {
	case <-entered:
	case err := <-invoked:
		t.Fatalf("the native call never reached its execution: the invocation ended: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatalf("the native call never reached its execution: the invocation still runs; facts=%+v", run.SessionFacts(addr))
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer closeCancel()
	if _, err := run.Close(closeCtx, agent.CloseRequest{Timeout: time.Now().Add(300 * time.Millisecond)}); err != nil {
		t.Fatalf("close the run beside the held call: %v", err)
	}
	close(release)
	waitForSettle(t, "the held result to record its fact", func() bool {
		return latticeHasResultFact(run, addr, "hold")
	})
	time.Sleep(300 * time.Millisecond)

	if latticeHasDeliveryFact(run, addr) {
		t.Fatalf("the stopped current accepted a delivery fact for the held result: %+v", run.SessionFacts(addr))
	}
	read := run.Unsettled(context.Background())
	owed := 0
	for _, notice := range read.Notices {
		if notice.Address == addr {
			owed++
		}
	}
	if owed == 0 {
		t.Fatalf("the settled result stands not owed after the stopped backflow: notices=%+v work=%+v", read.Notices, read.Work)
	}
	if keys := tierKeys(t, root, "tagent/task/v1/consume:"); len(keys) != 0 {
		t.Fatalf("consumption receipts beside a stopped current = %d, want none: the notice stays owed", len(keys))
	}
}

// latticeSawToolResult reports whether one model request already carries the
// tool result of the named call.
func latticeSawToolResult(req *model.Request, tool string) bool {
	for _, message := range req.Messages {
		if message.Role == model.RoleTool && message.ToolName == tool {
			return true
		}
	}
	return false
}

// latticeHasResultFact reports whether the named session carries the recorded
// result fact of the named tool call.
func latticeHasResultFact(run *assembly.Run, addr event.Address, tool string) bool {
	for _, fact := range run.SessionFacts(addr) {
		if fact.Type == event.TypeActionCommand && fact.Message.ToolName == tool {
			return true
		}
	}
	return false
}

// latticeHasDeliveryFact reports whether the named session carries a delivery
// fact: the marker a live backflow commits inside the range it hands its result
// to.
func latticeHasDeliveryFact(run *assembly.Run, addr event.Address) bool {
	for _, fact := range run.SessionFacts(addr) {
		if fact.Type == event.TypeTaskSettled {
			return true
		}
	}
	return false
}

// latticeHoldTool is one native tool whose execution the check holds open, so
// the settlement of its call lands after the current consumer set stopped.
type latticeHoldTool struct {
	name    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Declaration returns the declaration naming the held tool.
func (t *latticeHoldTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call holds the call open until the check releases it, and never observes the
// cancellation of the run that closed beside it: the production is detached so
// the result lands after the stop, which is the boundary under check.
func (t *latticeHoldTool) Call(context.Context, []byte) (any, error) {
	t.once.Do(func() { close(t.entered) })
	<-t.release
	return "the held result", nil
}

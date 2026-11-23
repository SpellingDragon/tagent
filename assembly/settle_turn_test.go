// The scope gate of the plugin dispatch path: the terminal notification one
// settle driver delivers reads the target set the host document declares, so a
// dispatch naming a session outside that declaration is refused by name and the
// obligation it carried stays owed on the mailbox account.
package assembly

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

const (
	// scopeGateDeclaring is the party the checks declare their target set under.
	scopeGateDeclaring = "user"
	// scopeGateInstance is the runtime instance serving the scripted seat.
	scopeGateInstance event.InstanceID = "instance-scope-gate"
	// scopeGateWindow bounds every wait of the gate checks.
	scopeGateWindow = 10 * time.Second
)

// TestPluginPathScopeGate judges one plugin dispatch against the declared target set.
// - declared_target_dispatches: a target the declaration names is dispatched once, and the mailbox entry closes beside the turn it drove
// - denies_out_of_scope: a target the declaration does not name starts no turn, is refused by name, and leaves the entry owed
// - fail_closed_beside_empty_list: a declaration naming no target starts no turn either
// - undeclared_refuses_every_dispatch: a driver that declares no table at all refuses every dispatch
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
func TestPluginPathScopeGate(t *testing.T) {
	t.Run("declared_target_dispatches", func(t *testing.T) {
		wake := newScopeGateWake(scopeGateDeclaring)
		fired := driveScopeGate(t, wake, func(driver *SettleDriver) {
			driver.DeclareScope(scopeGateDeclaring, scopeGateDeclaring)
		})
		if !fired {
			t.Fatalf("the dispatch to the declared target fired no turn, want the one turn the notification owes")
		}
		if got := wake.claim(); got != state.ClaimConsumed {
			t.Fatalf("claim of the dispatched entry = %d, want %d beside the turn it drove", got, state.ClaimConsumed)
		}
	})

	t.Run("denies_out_of_scope", func(t *testing.T) {
		wake := newScopeGateWake("agent-outside")
		fired := driveScopeGate(t, wake, func(driver *SettleDriver) {
			driver.DeclareScope(scopeGateDeclaring, scopeGateDeclaring)
		})
		if fired {
			t.Fatalf("the dispatch to a target the declaration does not name fired a turn, want the named refusal instead")
		}
		if got := wake.claim(); got != state.ClaimPending {
			t.Fatalf("claim of the refused entry = %d, want %d: a refused dispatch leaves the obligation owed", got, state.ClaimPending)
		}
		if got := wake.unconsumed(); got != 1 {
			t.Fatalf("entries the account still owes beside a refused dispatch = %d, want 1", got)
		}
	})

	t.Run("fail_closed_beside_empty_list", func(t *testing.T) {
		wake := newScopeGateWake(scopeGateDeclaring)
		fired := driveScopeGate(t, wake, func(driver *SettleDriver) {
			driver.DeclareScope(scopeGateDeclaring)
		})
		if fired {
			t.Fatalf("the dispatch beside a declaration naming no target fired a turn, want every dispatch refused closed")
		}
		if got := wake.claim(); got != state.ClaimPending {
			t.Fatalf("claim of the entry beside an empty declaration = %d, want %d", got, state.ClaimPending)
		}
	})

	t.Run("undeclared_refuses_every_dispatch", func(t *testing.T) {
		wake := newScopeGateWake(scopeGateDeclaring)
		fired := driveScopeGate(t, wake, nil)
		if fired {
			t.Fatalf("the dispatch of a driver that declared no target set fired a turn, want every dispatch refused closed")
		}
		if got := wake.claim(); got != state.ClaimPending {
			t.Fatalf("claim of the entry beside no declaration = %d, want %d", got, state.ClaimPending)
		}
	})
}

// driveScopeGate wakes one real settle driver over the scripted seat and reports
// whether the follow-up turn fired. The declaration is wired before the wake, so
// the pump reads it the way the host wires it at assembly.
func driveScopeGate(t *testing.T, wake *scopeGateWake, declare func(*SettleDriver)) bool {
	t.Helper()
	seat := wake.seat()
	lookup := func(_ context.Context, id event.EventID) (event.Event, error) {
		return event.Event{
			Key:    id,
			Type:   event.TypeTaskSettled,
			TaskID: seat.Notice.TaskID,
			Meta:   map[string]string{event.MetaKeyNoticeKind: string(seat.Notice.Kind)},
		}, nil
	}
	driver := NewSettleDriver(lookup, func() SettleWake { return wake }, wake.fire)
	if declare != nil {
		declare(driver)
	}
	defer driver.Stop()
	driver.Wake()
	waitScopeGate(t, "the settle pump to rest beside the scripted seat", wake.rested)
	return wake.fires() > 0
}

// newScopeGateWake returns the scripted account holding one terminal
// notification owed to the session of one target agent.
func newScopeGateWake(agent string) *scopeGateWake {
	return &scopeGateWake{entry: state.WorkSeat{
		Address:   event.Address{AgentID: event.AgentID(agent), SessionID: "session-scope-gate"},
		Instance:  scopeGateInstance,
		AcceptSeq: 1,
		Notice:    task.Notice{TaskID: "task-scope-gate", Round: 1, Kind: task.NoticeSettled, Seq: 1},
		Claim:     state.ClaimPending,
	}}
}

// scopeGateWake is the mailbox account the gate checks drive: one entry whose
// claim state moves through the pump's own calls, beside the count of turns the
// follow-up face was asked to drive.
type scopeGateWake struct {
	mu       sync.Mutex
	entry    state.WorkSeat
	turns    int
	claiming int
}

// Unconsumed reports the entry while its consumption has not completed.
func (w *scopeGateWake) Unconsumed() []state.WorkSeat {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.entry.Claim == state.ClaimConsumed {
		return nil
	}
	return []state.WorkSeat{w.entry}
}

// Claim takes the pending entry for the pump alone.
func (w *scopeGateWake) Claim(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.entry.Claim != state.ClaimPending {
		return state.ErrClaimConflict
	}
	w.entry.Claim = state.ClaimClaimed
	w.claiming++
	return nil
}

// Release hands the claimed entry back to the pending pool.
func (w *scopeGateWake) Release(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entry.Claim = state.ClaimPending
	return nil
}

// Consume records the completed consumption of the claimed entry.
func (w *scopeGateWake) Consume(context.Context, event.Address, event.InstanceID, state.AcceptSeq) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entry.Claim = state.ClaimConsumed
	return nil
}

// fire is the follow-up face of the driver: it counts the turns the gate let
// through and drives nothing else.
func (w *scopeGateWake) fire(context.Context, event.Address, event.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.turns++
	return nil
}

// seat reports the entry the account holds.
func (w *scopeGateWake) seat() state.WorkSeat {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.entry
}

// claim reports the claim state the entry stands at.
func (w *scopeGateWake) claim() state.ClaimState {
	return w.seat().Claim
}

// fires reports how many turns the follow-up face drove.
func (w *scopeGateWake) fires() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.turns
}

// unconsumed reports how many entries the account still owes.
func (w *scopeGateWake) unconsumed() int {
	return len(w.Unconsumed())
}

// rested reports whether the pump finished the one drive it was woken for: the
// entry was claimed and then either consumed or handed back to the pending pool.
func (w *scopeGateWake) rested() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.claiming > 0 && w.entry.Claim != state.ClaimClaimed
}

// waitScopeGate waits for one account position inside the gate window, so a
// gate that never answers reports the named gap instead of a timeout alone.
func waitScopeGate(t *testing.T, what string, landed func() bool) {
	t.Helper()
	deadline := time.Now().Add(scopeGateWindow)
	for time.Now().Before(deadline) {
		if landed() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waited %s for %s, want it landed", scopeGateWindow, what)
}

// TestInProcessSettleFailureDrivesNoSecondTurn pins the one turn a refused delivery fact leaves this process to drive.
//   - the settled call of one native tool commits the delivery fact of its range, and the commit face refuses it
//   - the range drives the handed-on result in this process: the completed turn of two decisions is that hand-over
//   - the pump of the same process reads the account the hand-over left and drives no second turn
//   - the entry keeps the claim the hand-over took; it stood pending while the refusal handed the claim back, which let a pump here drive the notice twice
//   - the notification stays owed to the next process: the entry is unconsumed under that claim, the chain holds no delivery fact and the root holds no receipt
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestInProcessSettleFailureDrivesNoSecondTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	root := t.TempDir()
	addr := deliveryTestAddress("session-in-process-refusal")
	refusal := errors.New("the commit face refused the delivery fact of the settled call")
	tool := &deliveryFactObligationTool{}
	run := deliveryFactObligationRun(t, ctx, root, tool)

	var mu sync.Mutex
	refused := 0
	run.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		if evt.Type == event.TypeTaskSettled {
			mu.Lock()
			refused++
			mu.Unlock()
			return "", refusal
		}
		return run.commitFactChecked(ctx, evt)
	})

	turn, err := run.Invoke(ctx, InvokeRequest{
		InvocationID:  "inv-in-process-refusal",
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "call the tool once"}},
	})
	if err != nil {
		t.Fatalf("invoke the run beside the refused delivery fact: %v", err)
	}
	mu.Lock()
	struck := refused
	mu.Unlock()
	if struck != 1 {
		t.Fatalf("the commit face refused %d delivery fact(s), want the one the settled call commits", struck)
	}
	if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
		t.Fatalf("the turn beside the refused delivery fact = (%v, %d decisions), want the completed turn of two decisions: the hand-over ran in this process, so a second drive is the same notice handed on twice",
			turn.Terminal, turn.DecisionCount)
	}

	turns := 0
	fire := func(ctx context.Context, _ event.Address, _ event.Event) error {
		mu.Lock()
		turns++
		mu.Unlock()
		if barrier, held := settleBarrierFrom(ctx); held {
			return barrier.settle(ctx, model.KindCompleted)
		}
		return nil
	}
	account := &settleAccountReading{inner: run.store}
	driver := NewSettleDriver(run.CommittedEvent, func() SettleWake { return account }, fire,
		withSettleFactCommit(run.commitFactChecked))
	driver.DeclareScope(string(addr.AgentID), string(addr.AgentID))
	driver.Wake()
	waitScopeGate(t, "the settle pump to read the account the refused hand-over left", func() bool {
		return account.readCount() >= 1
	})
	driver.Wake()
	waitScopeGate(t, "the settle pump to read that account again, which it takes only after returning from the first reading", func() bool {
		return account.readCount() >= 2
	})
	driver.Stop()

	mu.Lock()
	driven := turns
	mu.Unlock()
	if driven != 0 {
		t.Fatalf("the pump of this process drove %d follow-up turn(s) beside the refused delivery fact (%d claim(s)), want none: the result was handed on here already, so a second turn is the same notice driven twice",
			driven, account.claimCount())
	}
	if claims := account.claimCount(); claims != 0 {
		t.Fatalf("the pump of this process claimed %d entry(s) of the account the hand-over left, want none: the claim the hand-over took leaves no pending work to read", claims)
	}
	seats := run.store.Unconsumed()
	if len(seats) != 1 || seats[0].Claim != state.ClaimClaimed {
		t.Fatalf("the account after the pump = %+v, want the one notification owed under the claim this process took (%d)", seats, state.ClaimClaimed)
	}
	if facts := deliveryFactsOf(run.SessionFacts(addr)); len(facts) != 0 {
		t.Fatalf("the chain holds %d delivery fact(s) after the pump, want none: the record of the refused hand-over lands with the process that restores the owed notification", len(facts))
	}
	if keys := consumeReceiptKeys(t, ctx, run.tier); len(keys) != 0 {
		t.Fatalf("the root holds %d consumption receipt(s) after the pump, want none: the notification stays owed", len(keys))
	}
	if _, err := run.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the process beside the owed notification: %v", err)
	}
}

// settleAccountReading is the mailbox account one pump drives, read through the
// production faces that pump calls: the reads of the account and the claims taken
// from it are counted, so a check reads whether the pump of one process found
// work where the hand-over left none.
type settleAccountReading struct {
	inner SettleWake

	mu     sync.Mutex
	reads  int
	claims int
}

// Unconsumed reports the account and counts the reading.
func (a *settleAccountReading) Unconsumed() []state.WorkSeat {
	a.mu.Lock()
	a.reads++
	a.mu.Unlock()
	return a.inner.Unconsumed()
}

// Claim takes one pending entry for the pump and counts the attempt.
func (a *settleAccountReading) Claim(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error {
	a.mu.Lock()
	a.claims++
	a.mu.Unlock()
	return a.inner.Claim(ctx, addr, instance, seq)
}

// Release hands one claimed entry back to the pending pool.
func (a *settleAccountReading) Release(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error {
	return a.inner.Release(ctx, addr, instance, seq)
}

// Consume records the completed consumption of one claimed entry.
func (a *settleAccountReading) Consume(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error {
	return a.inner.Consume(ctx, addr, instance, seq)
}

// readCount reports how often the pump read the account.
func (a *settleAccountReading) readCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reads
}

// claimCount reports how often the pump claimed an entry of the account.
func (a *settleAccountReading) claimCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.claims
}

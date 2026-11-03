// The scope gate of the plugin dispatch path: the terminal notification one
// settle driver delivers reads the target set the host document declares, so a
// dispatch naming a session outside that declaration is refused by name and the
// obligation it carried stays owed on the mailbox account.
package assembly

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
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

// RestoreOwed keeps the entry as it stands: the check wrote the account itself.
func (w *scopeGateWake) RestoreOwed(context.Context) error { return nil }

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

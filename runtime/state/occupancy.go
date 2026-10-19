package state

import (
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

var (
	// ErrDecisionIdentityMissing reports an occupancy entry that carries no
	// invocation identity or no decision identity, so no decision slot could be
	// held for it.
	ErrDecisionIdentityMissing = errors.New("state: occupancy entry carries no decision identity")
	// ErrSlotHeld reports a session decision slot another invocation already
	// holds, so the second effective model decision is refused instead of
	// running beside the first.
	ErrSlotHeld = errors.New("state: the session decision slot is already held")
	// ErrSlotNotHeld reports a vacating of the session decision slot by an
	// invocation that does not hold it.
	ErrSlotNotHeld = errors.New("state: the named invocation holds no decision slot of the session")
)

// occupant is the invocation whose effective model decision holds one session
// decision slot, beside the identity of that decision.
type occupant struct {
	invocation event.InvocationID
	decision   event.DecisionID
}

// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
//
// Occupancy holds the execution occupancy of the session sites: the one model
// decision each site may have in flight, and the queued inputs a caller
// withdrew from execution before any decision started on them.
//
// The occupancy ledger is decoupled from the mailbox acceptance path: an input
// is accepted and a queued input is withdrawn while a slow model decision holds
// the slot, and neither path waits for that decision to end. A queued input is
// never bound to a binding in advance, so it takes what is effective when its
// own execution begins.
type Occupancy struct {
	mu        sync.Mutex
	slots     map[siteKey]occupant
	withdrawn map[siteKey]map[AcceptSeq]struct{}
}

// NewOccupancy returns an empty execution occupancy ledger: no session holds a
// decision slot and no queued input is withdrawn.
func NewOccupancy() *Occupancy {
	return &Occupancy{
		slots:     map[siteKey]occupant{},
		withdrawn: map[siteKey]map[AcceptSeq]struct{}{},
	}
}

// Occupy takes the session decision slot of one site for the named invocation
// decision, so at most one effective model decision runs per session. A slot
// another invocation already holds refuses the second occupant by name.
func (o *Occupancy) Occupy(addr event.Address, instance event.InstanceID, invocation event.InvocationID, decision event.DecisionID) error {
	if invocation == "" || decision == "" {
		return ErrDecisionIdentityMissing
	}
	key, err := occupancyKey(addr, instance)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if held, ok := o.slots[key]; ok {
		return fmtSlotHeld(held)
	}
	o.slots[key] = occupant{invocation: invocation, decision: decision}
	return nil
}

// Vacate releases the session decision slot of one site, so the queued inputs
// of the session take their turn. The invocation named must be the one holding
// the slot; any other name is refused.
func (o *Occupancy) Vacate(addr event.Address, instance event.InstanceID, invocation event.InvocationID) error {
	key, err := occupancyKey(addr, instance)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	held, ok := o.slots[key]
	if !ok || held.invocation != invocation {
		return ErrSlotNotHeld
	}
	delete(o.slots, key)
	return nil
}

// Slot reports the invocation and decision holding the session decision slot of
// one site, or that no decision is in flight there.
func (o *Occupancy) Slot(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool) {
	key, err := occupancyKey(addr, instance)
	if err != nil {
		return "", "", false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	held, ok := o.slots[key]
	if !ok {
		return "", "", false
	}
	return held.invocation, held.decision, true
}

// Withdraw removes one queued input from execution beside the site that
// accepted it: the input stays accepted and readable in the mailbox, and it is
// never started on a decision. Withdrawal is recorded immediately and never
// waits for a decision in flight to end.
func (o *Occupancy) Withdraw(addr event.Address, instance event.InstanceID, seq AcceptSeq) error {
	key, err := occupancyKey(addr, instance)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	marks, ok := o.withdrawn[key]
	if !ok {
		marks = map[AcceptSeq]struct{}{}
		o.withdrawn[key] = marks
	}
	marks[seq] = struct{}{}
	return nil
}

// Withdrawn reports whether the queued input at one acceptance sequence was
// withdrawn from execution beside the site named.
func (o *Occupancy) Withdrawn(addr event.Address, instance event.InstanceID, seq AcceptSeq) bool {
	key, err := occupancyKey(addr, instance)
	if err != nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.withdrawn[key][seq]
	return ok
}

// occupancyKey returns the site identity one occupancy operation names, or the
// error naming why no site identity answers.
func occupancyKey(addr event.Address, instance event.InstanceID) (siteKey, error) {
	if !addr.IsComplete() {
		return siteKey{}, ErrIncompleteAddress
	}
	if instance == "" {
		return siteKey{}, ErrInstanceMissing
	}
	return siteKey{addr: addr, instance: instance}, nil
}

// fmtSlotHeld returns the named refusal of a second occupant beside the
// identity of the decision already in flight.
func fmtSlotHeld(held occupant) error {
	return fmt.Errorf("%w: %s holds it as decision %s", ErrSlotHeld, held.invocation, held.decision)
}

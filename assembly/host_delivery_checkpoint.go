// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The delivery state slice: the host's whole deliverable account as the
// framework checkpoint carries it. The account is the obligations still owed
// with the body, target, lineage, hold moment and retry attempt they stand
// under, the hold ordering sequence, and the pins kept hot for them. A slice of
// this size is the size of the work in flight, never the size of the history the
// process committed — that is the whole point of the checkpoint: a restarted
// host restores an account that does not grow with the chains it already
// settled.
//
// The settled set is stated by the cut instead of by a token per obligation: a
// settlement fact whose landing sits at or before the checkpoint's offset is
// covered, so every obligation outside the owed list that landed before the cut
// is settled by construction. A state token per settled identity would make the
// carrier grow with the history it exists to bound, and the chain behind it
// still answers any one obligation by its exact key.
package assembly

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// deliverySliceWire is the payload of the delivery state slice: the obligations
// still owed with the state they stand in, the redrive each of them is waiting
// on, the pins kept hot for them and the sequence their holds are ordered by.
// The settled history is not stated here: it is the cut (see the package
// comment), so the payload is the size of the open account.
type deliverySliceWire struct {
	// HeldSeq is the sequence the parked holds are ordered by.
	HeldSeq uint64 `json:"held_seq"`
	// Attempts is the redrive still pending for the obligations the account
	// retried, so a restarted host resumes the bounded backoff where the process
	// before it stopped instead of starting it over or forgetting the redrive.
	Attempts []deliverySliceRetry `json:"attempts,omitempty"`
	// Pins names the settlement fact kept hot for every obligation still owed.
	Pins map[string]event.EventID `json:"pins,omitempty"`
	// Owed is every obligation the account still owes a target, in the order
	// the holds were parked.
	Owed []deliverySliceOwed `json:"owed,omitempty"`
}

// deliverySliceOwed is one obligation the account still owes: the identity an
// approval or refusal names, the body it delivers, the target and lineage it was
// declared with, the moment it was parked and the settlement state it stands at.
type deliverySliceOwed struct {
	// Identity is the identity the obligation is deduplicated and resolved by.
	Identity string `json:"identity"`
	// Content, Lineage, Agent and Session are the delivery the obligation owes.
	Content string `json:"content,omitempty"`
	Lineage string `json:"lineage,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Session string `json:"session,omitempty"`
	// HeldAt is the moment the obligation was parked, stated at the fidelity the
	// settlement fact states it: the account's carrier and its chain spell one
	// moment one way, so a restart that reads the carrier holds the moment the
	// process recorded rather than the millisecond it fell in.
	HeldAt time.Time `json:"held_at"`
	// State is the settlement state token the obligation stands at.
	State string `json:"state,omitempty"`
}

// deliverySliceRetry is one redrive the account still owes: the identity, the
// delivery to send again and the attempt number it stands at.
type deliverySliceRetry struct {
	// Identity is the identity the redrive is deduplicated under.
	Identity string `json:"identity"`
	// Content, Lineage, Agent and Session are the delivery to send again.
	Content string `json:"content,omitempty"`
	Lineage string `json:"lineage,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Session string `json:"session,omitempty"`
	// Attempt is the attempt number the redrive resumes at.
	Attempt int `json:"attempt,omitempty"`
}

// pendingRedrives reports the redrives the consumer still owes, in identity
// order, so a checkpoint states the redrive the account is waiting on.
func (c *deliveryConsumer) pendingRedrives() []deliverySliceRetry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	identities := make([]string, 0, len(c.queue))
	for identity := range c.queue {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	out := make([]deliverySliceRetry, 0, len(identities))
	for _, identity := range identities {
		retry := c.queue[identity]
		if retry == nil {
			continue
		}
		out = append(out, deliverySliceRetry{
			Identity: identity,
			Content:  retry.delivery.Content,
			Lineage:  retry.delivery.Lineage,
			Agent:    string(retry.delivery.Target.AgentID),
			Session:  string(retry.delivery.Target.SessionID),
			Attempt:  retry.attempt,
		})
	}
	return out
}

// deliveryStateSlice is the Host's recoverable state as the framework sees it.
type deliveryStateSlice struct {
	host *Host
}

// StateName reports the name the delivery slice is keyed by.
func (s deliveryStateSlice) StateName() string { return checkpointSliceDelivery }

// SnapshotState states the whole deliverable account of the host.
func (s deliveryStateSlice) SnapshotState(ctx context.Context) ([]byte, error) {
	return s.host.deliverySlice()
}

// SnapshotSessionState states the part of the account one session chain owns: the
// obligations whose target is that chain, with the redrive, the hold moment and
// the state they stand under. The account of a chain is what the chain is owed,
// so the carrier of one session states nothing about the obligations another
// chain is owed.
func (s deliveryStateSlice) SnapshotSessionState(ctx context.Context, session event.SessionID) ([]byte, error) {
	return s.host.deliverySliceOf(session)
}

// RestoreState puts the account back: every settlement state, every owed obligation
// with the delivery it carries, the retry attempts and the hold sequence.
func (s deliveryStateSlice) RestoreState(ctx context.Context, payload []byte) error {
	return s.host.restoreDeliverySlice(payload)
}

// registerDeliverySlice mounts the delivery account on the run's state seam, so
// every checkpoint the framework files carries it and a boot restores it instead
// of scanning the committed history for settlements. The same mount seats the
// account among the consumers a landed fact is folded into, so the obligation a
// fact declares enters the account inside that fact's landing unit and no cut
// falls between the declaration and the account that owes it.
func (h *Host) registerDeliverySlice() error {
	if h == nil || h.run == nil {
		return nil
	}
	_, err := h.run.RegisterStateSlice(deliveryStateSlice{host: h})
	return err
}

// deliverySlice states the whole open account of the host: the obligations still
// owed with the delivery they carry and the state they stand in, the retry
// attempt they stand at, the pins kept hot for them and the sequence the holds
// are ordered by. The settled history is stated by the cut the checkpoint
// carries, so the payload is the open account alone.
//
// An obligation is open whether the account holds it in the review table or in
// the declarations no settlement has answered, so both sets are stated: a body
// the chain declared and the live path had not settled when the cut was taken is
// owed a send, and a carrier that omitted it would leave the cut claiming a
// settlement that never happened.
func (h *Host) deliverySlice() ([]byte, error) {
	return h.deliverySliceWhere("")
}

// deliverySliceOf states the open account one session chain owns: the obligations
// whose target is that chain, the redrive still pending for them and the pins kept
// hot for them. A chain the account owes nothing answers nil, so the carrier of a
// settled chain carries no delivery slice.
func (h *Host) deliverySliceOf(session event.SessionID) ([]byte, error) {
	if h == nil || session == "" {
		return nil, nil
	}
	payload, err := h.deliverySliceWhere(session)
	if err != nil {
		return nil, err
	}
	var wire deliverySliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, err
	}
	if len(wire.Owed) == 0 {
		return nil, nil
	}
	return payload, nil
}

// deliverySliceWhere states the open account of the chains the named session
// selects, or the whole account beside no session. A session's payload is read
// from that chain's slice alone, so the carrier of one chain costs what that
// chain owes and never what another chain owes beside it. A pin is named by the
// obligation it keeps hot, so a chain's carrier states the pins of the
// obligations that chain owes and never keeps another chain's fact hot.
func (h *Host) deliverySliceWhere(session event.SessionID) ([]byte, error) {
	if h == nil {
		return []byte("null"), nil
	}
	slices := make([]*deliveryAccount, 0, len(h.deliveryAccounts))
	h.mu.Lock()
	wire := deliverySliceWire{
		HeldSeq:  h.heldSeq,
		Attempts: []deliverySliceRetry{},
		Pins:     make(map[string]event.EventID, len(h.deliveryPins)),
		Owed:     make([]deliverySliceOwed, 0),
	}
	retries := h.retries
	if session == "" {
		names := make([]event.SessionID, 0, len(h.deliveryAccounts))
		for name := range h.deliveryAccounts {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
		for _, name := range names {
			slices = append(slices, h.deliveryAccounts[name])
		}
	} else if account := h.deliveryAccountLocked(session, false); account != nil {
		slices = append(slices, account)
	}
	for _, slice := range slices {
		identities := make([]string, 0, len(slice.held)+len(slice.declared))
		for identity := range slice.held {
			identities = append(identities, identity)
		}
		for identity := range slice.declared {
			if _, parked := slice.held[identity]; parked {
				continue
			}
			identities = append(identities, identity)
		}
		sort.Strings(identities)
		for _, identity := range identities {
			held := slice.held[identity]
			if held == nil {
				held = slice.declared[identity]
			}
			if held == nil {
				continue
			}
			wire.Owed = append(wire.Owed, deliverySliceOwed{
				Identity: identity,
				Content:  held.delivery.Content,
				Lineage:  held.delivery.Lineage,
				Agent:    string(held.delivery.Target.AgentID),
				Session:  string(held.delivery.Target.SessionID),
				HeldAt:   held.heldAt,
				State:    slice.states[identity],
			})
			if pin, pinned := h.deliveryPins[identity]; pinned {
				wire.Pins[identity] = pin
			}
		}
	}
	if session == "" {
		for identity, pin := range h.deliveryPins {
			wire.Pins[identity] = pin
		}
	}
	h.mu.Unlock()
	if retries != nil {
		for _, retry := range retries.pendingRedrives() {
			if session != "" && retry.Session != string(session) {
				continue
			}
			wire.Attempts = append(wire.Attempts, retry)
		}
	}
	return json.Marshal(wire)
}

// restoreDeliverySlice puts the open account back: every owed obligation under
// the identity, delivery, hold moment and state the checkpoint recorded, in the
// slice of the chain the carrier states it against, and the sequence the holds
// are ordered by. The obligations already settled are not stated by the carrier
// at all — they are the cut — so the restart never delivers a body the process
// before it delivered, and a state the table does not hold is answered by the
// settlement facts the chain keeps.
//
//   - the state an owed entry carries decides what the account owes: a cut taken between a settlement and the release of its hold states both, and the settled token is the one that stands, so the restart never parks an obligation its own account already discharged.
//   - a pin the carrier states is an obligation the hot window must keep, so the run's own pin registry is seeded from it: the boot trim releases facts by the pins the run holds, and a pin that reached the host table alone would let the trim drop the very fact the account still owes.
func (h *Host) restoreDeliverySlice(payload []byte) error {
	if h == nil {
		return nil
	}
	var wire deliverySliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	h.mu.Lock()
	if wire.HeldSeq > h.heldSeq {
		h.heldSeq = wire.HeldSeq
	}
	if h.deliveryPins == nil {
		h.deliveryPins = map[string]event.EventID{}
	}
	for identity, pin := range wire.Pins {
		h.deliveryPins[identity] = pin
	}
	for _, owed := range wire.Owed {
		if owed.Identity == "" {
			continue
		}
		account := h.deliveryAccountLocked(event.SessionID(owed.Session), true)
		if owed.State != "" {
			if account.states == nil {
				account.states = map[string]string{}
			}
			account.states[owed.Identity] = owed.State
		}
		if deliveryStateSettled(owed.State) {
			continue
		}
		restored := &heldDelivery{
			id: owed.Identity,
			delivery: plugin.Delivery{
				Content: owed.Content,
				Lineage: owed.Lineage,
				Target: event.Address{
					AgentID:   event.AgentID(owed.Agent),
					SessionID: event.SessionID(owed.Session),
				},
			},
			heldAt: owed.HeldAt,
		}
		if owed.State == "" {
			if _, stated := account.declared[owed.Identity]; !stated {
				if account.declared == nil {
					account.declared = map[string]*heldDelivery{}
				}
				account.declared[owed.Identity] = restored
			}
			continue
		}
		if _, parked := account.held[owed.Identity]; parked {
			continue
		}
		if account.held == nil {
			account.held = map[string]*heldDelivery{}
		}
		account.held[owed.Identity] = restored
	}
	h.restoredDelivery = true
	h.mu.Unlock()
	if h.run != nil {
		for _, pin := range wire.Pins {
			if pin != "" {
				h.run.pinFact(pin)
			}
		}
	}
	for _, retry := range wire.Attempts {
		if retry.Identity == "" {
			continue
		}
		h.retryConsumer().schedule(retry.Identity, plugin.Delivery{
			Content: retry.Content,
			Lineage: retry.Lineage,
			Target: event.Address{
				AgentID:   event.AgentID(retry.Agent),
				SessionID: event.SessionID(retry.Session),
			},
		}, retry.Attempt)
	}
	return nil
}

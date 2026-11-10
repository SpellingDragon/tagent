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

// RestoreState puts the account back: every settlement state, every owed obligation
// with the delivery it carries, the retry attempts and the hold sequence.
func (s deliveryStateSlice) RestoreState(ctx context.Context, payload []byte) error {
	return s.host.restoreDeliverySlice(payload)
}

// registerDeliverySlice mounts the delivery account on the run's state seam, so
// every checkpoint the framework files carries it and a boot restores it instead
// of scanning the committed history for settlements.
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
func (h *Host) deliverySlice() ([]byte, error) {
	if h == nil {
		return []byte("null"), nil
	}
	h.mu.Lock()
	wire := deliverySliceWire{
		HeldSeq:  h.heldSeq,
		Attempts: []deliverySliceRetry{},
		Pins:     make(map[string]event.EventID, len(h.deliveryPins)),
		Owed:     make([]deliverySliceOwed, 0, len(h.pendingDeliveries)),
	}
	retries := h.retries
	identities := make([]string, 0, len(h.pendingDeliveries))
	for identity := range h.pendingDeliveries {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		held := h.pendingDeliveries[identity]
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
			State:    h.deliveryStates[identity],
		})
	}
	for identity, pin := range h.deliveryPins {
		wire.Pins[identity] = pin
	}
	h.mu.Unlock()
	if retries != nil {
		wire.Attempts = retries.pendingRedrives()
	}
	return json.Marshal(wire)
}

// restoreDeliverySlice puts the open account back: every owed obligation under
// the identity, delivery, hold moment and state the checkpoint recorded, and the
// sequence the holds are ordered by. The obligations already settled are not
// stated by the carrier at all — they are the cut — so the restart never
// delivers a body the process before it delivered, and a state the table no
// longer holds is answered by the settlement facts the chain keeps.
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
	if h.deliveryStates == nil {
		h.deliveryStates = map[string]string{}
	}
	if wire.HeldSeq > h.heldSeq {
		h.heldSeq = wire.HeldSeq
	}
	if h.deliveryPins == nil {
		h.deliveryPins = map[string]event.EventID{}
	}
	for identity, pin := range wire.Pins {
		h.deliveryPins[identity] = pin
	}
	if h.pendingDeliveries == nil {
		h.pendingDeliveries = map[string]*heldDelivery{}
	}
	for _, owed := range wire.Owed {
		if owed.Identity == "" {
			continue
		}
		if owed.State != "" {
			h.deliveryStates[owed.Identity] = owed.State
		}
		if deliveryStateSettled(owed.State) {
			continue
		}
		if _, parked := h.pendingDeliveries[owed.Identity]; parked {
			continue
		}
		h.pendingDeliveries[owed.Identity] = &heldDelivery{
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

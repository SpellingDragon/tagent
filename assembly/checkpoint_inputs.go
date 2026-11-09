// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The owed-input state slice: the input rounds the chains of this run hold with
// no answer, as the framework checkpoint carries them. The projection slice
// states the material a restart presents, not the rounds nobody drove, so a cut
// that covered an unanswered input would otherwise lose the drive it owes. The
// slice states each owed round beside the address it belongs to and the instance
// that accepted it, and a boot reads it as the rounds committed before the cut so
// an answer that landed past the cut still settles the round it answers.
package assembly

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// restoredInput is one owed input round a checkpoint stated: the chain it
// belongs to, the instance that accepted it and the identity of the fact the
// drive answers.
type restoredInput struct {
	address  event.Address
	instance event.InstanceID
	key      event.EventID
}

// inputsStateSlice is the owed input rounds of the run as the framework sees it.
type inputsStateSlice struct {
	run *Run
}

// inputsSliceWire is the payload of the owed-input slice: one entry per session
// chain, in address order.
type inputsSliceWire struct {
	Sessions []inputsSliceSession `json:"sessions"`
}

// inputsSliceSession is the owed rounds of one chain: the address they belong
// to, the instance that accepted them and the identity of each round.
type inputsSliceSession struct {
	Agent    string          `json:"agent"`
	Session  string          `json:"session"`
	Instance string          `json:"instance,omitempty"`
	Inputs   []event.EventID `json:"inputs,omitempty"`
}

// StateName reports the name the owed-input slice is keyed by.
func (s inputsStateSlice) StateName() string { return checkpointSliceInputs }

// SnapshotState states the input rounds the resident chains hold with no answer.
func (s inputsStateSlice) SnapshotState(ctx context.Context) ([]byte, error) {
	return s.run.pendingInputsSlice(ctx)
}

// RestoreState puts the owed rounds back as rounds committed before the cut, so
// the boot judges them beside the material that landed past it.
func (s inputsStateSlice) RestoreState(ctx context.Context, payload []byte) error {
	return s.run.restoreInputsSlice(payload)
}

// pendingInputsSlice states the input rounds every resident chain holds with no
// answer, in address order.
func (r *Run) pendingInputsSlice(ctx context.Context) ([]byte, error) {
	addresses := r.ActiveSessions("")
	sortAddresses(addresses)
	wire := inputsSliceWire{Sessions: make([]inputsSliceSession, 0, len(addresses))}
	for _, addr := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pending := agent.PendingInputs(r.SessionFacts(addr))
		if len(pending) == 0 {
			continue
		}
		session := inputsSliceSession{Agent: string(addr.AgentID), Session: string(addr.SessionID)}
		if r.store != nil {
			session.Instance = string(r.store.InstanceOf(addr))
		}
		for _, evt := range pending {
			if evt.Key == "" {
				continue
			}
			session.Inputs = append(session.Inputs, evt.Key)
		}
		wire.Sessions = append(wire.Sessions, session)
	}
	return json.Marshal(wire)
}

// restoreInputsSlice puts the owed rounds of one checkpoint back as the rounds
// committed before the cut they state.
func (r *Run) restoreInputsSlice(payload []byte) error {
	var wire inputsSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	restored := make([]restoredInput, 0, len(wire.Sessions))
	for _, session := range wire.Sessions {
		addr := event.Address{AgentID: event.AgentID(session.Agent), SessionID: event.SessionID(session.Session)}
		if !addr.IsComplete() {
			return fmt.Errorf("assembly: the owed-input slice names the incomplete address %v", addr)
		}
		for _, key := range session.Inputs {
			if key == "" {
				return fmt.Errorf("assembly: the owed-input slice of %v names a round with no identity", addr)
			}
			restored = append(restored, restoredInput{
				address:  addr,
				instance: event.InstanceID(session.Instance),
				key:      key,
			})
		}
	}
	r.mu.Lock()
	r.restoredInputs = restored
	r.mu.Unlock()
	return nil
}

// restoredInputFacts reports the owed rounds a checkpoint restored, as the facts
// the rest criterion judges: each stands before every fact that landed past the
// cut, so an answer committed after the cut still closes the round it answers.
//
// The carrier states the identity of each round and the boot reads that fact
// back from the chain, so the round reaches the criterion as the driving fact it
// is — its frozen type and the metadata that decides whether it opens a range —
// instead of as a placeholder no criterion recognises. The cost is the rounds
// still owed, never the history: a round the cut covered and nothing answered is
// one read by identity.
func (r *Run) restoredInputFacts(ctx context.Context, addr event.Address) []event.Event {
	r.mu.Lock()
	keys := make([]event.EventID, 0, len(r.restoredInputs))
	for _, input := range r.restoredInputs {
		if input.address != addr || input.key == "" {
			continue
		}
		keys = append(keys, input.key)
	}
	r.mu.Unlock()
	facts := make([]event.Event, 0, len(keys))
	for _, key := range keys {
		evt, err := r.CommittedEvent(ctx, key)
		if err != nil {
			assemblyWarn("tagent: the owed round %s the checkpoint stated could not be read back: %v", key, err)
			continue
		}
		facts = append(facts, evt)
	}
	return facts
}

// sortAddresses orders the addresses of the active set so a checkpoint states
// the same slices for the same state.
func sortAddresses(addresses []event.Address) {
	sort.Slice(addresses, func(i, j int) bool {
		if addresses[i].AgentID != addresses[j].AgentID {
			return addresses[i].AgentID < addresses[j].AgentID
		}
		return addresses[i].SessionID < addresses[j].SessionID
	})
}

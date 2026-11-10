// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The state seam of a runtime: a consumer that holds recoverable state delegates
// the state to the framework here, and the framework aggregates every registered
// slice into one checkpoint cut and restores them from one. The consumer never
// owns a recovery read path of its own, because a bespoke reader grows with the
// history it reads while a checkpoint slice stays the size of the state it
// states. Registration is an effect: the returned disposer is the only way a
// slice leaves the registry.
package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// Stateful is one consumer's recoverable state as the framework sees it: the
// name the slice is keyed by, the state it states, and the restore that puts
// that state back. The payload is the holder's own business — it is JSON, and no
// other holder and no reader of the checkpoint interprets it.
//
// The snapshot is a pure read: it states the state the holder already derived
// and never flushes, commits, asks another party for work, or waits for a lock a
// commit of its own holds. The framework reads every slice at the quiet point of
// the fact pipeline with no lock of the run held, so a holder that commits under
// its own lock (the task registry's durable commit, the host's settlement write)
// can never wait on the cut that is asking it for its state. A holder whose state
// follows a fact it committed applies that fact inside the commit's flight, never
// after it, so the state read at the quiet point states every fact the cut covers.
type Stateful interface {
	// StateName is the name the slice is keyed by in a checkpoint.
	StateName() string
	// SnapshotState states the recoverable state as JSON. The read is pure: the
	// state the holder already derived, with no flush, commit or blocking wait.
	SnapshotState(ctx context.Context) ([]byte, error)
	// RestoreState puts one snapshot back. A payload the holder does not recognize is
	// refused by name rather than restored as an empty state.
	RestoreState(ctx context.Context, payload []byte) error
}

// emptyStateJSON is the state a cut that states nothing about a holder carries:
// the payload a snapshot states for a holder that states nothing, so the two
// readings of "no state" are one JSON.
const emptyStateJSON = "null"

// reducer is the derivation face of a holder whose recoverable state is folded
// from committed facts: one fact in, the same derived state either way. A holder
// that states sums of its own — the task registry's account, the mailbox's owed
// notices — implements no such face, because its state follows an account this
// seam does not read.
//
// The face is one derivation and not a second reader beside the restore: the
// live commit folds a fact through it, and so does the replay of a chain past
// its cut, so the state a cut states and the state a boot rebuilds are the same
// function of the same facts. The fold is idempotent by the identity the fact
// carries: a fact a restore already folded, and a fact two read paths both
// reach, leave the state it states once. The fold carries no duty of its own: it
// applies a fact and answers an error, and never fires the work a consumer owes
// (see emitter).
type reducer interface {
	// Reduce folds one committed fact into the holder's derived state.
	Reduce(ctx context.Context, evt event.Event) error
}

// emitter is the declaration face of a holder that also drives observation: the
// consumer seats its state through Reduce and starts work of its own — a send, a
// fold, a follow-up turn — beside it. The declaration is what keeps the two apart:
// the seam's own pass folds state and drives nothing, and the recovery a boot runs
// leaves every declared drive to the owner that drives it after the recovery
// returned.
//
// A holder that declares no such face reads facts and owes nothing of them: its
// failure is a gap beside the committed fact, never work left owed.
type emitter interface {
	// DrivesObservation marks the holder as one whose consumer drives work beside
	// the state it states. The method carries no answer of its own: the duty is the
	// declaration, and the account the work rests in is the consumer's.
	DrivesObservation()
}

// sessionSnapshotter is the session face of a holder whose state is spelled per
// session chain. A cut taken for one session reads that face instead of the whole
// account, so a checkpoint states the material of the session that asked for it and
// pays nothing for the chains beside it. A holder that keeps no state of a
// session — the task registry's account is one registry-wide fold, the mailbox's
// owed notices are routed by site — implements no such face: the session cut
// states nothing about it and the framework's own cut keeps stating it.
//
// The read is the same pure read SnapshotState is: the state the holder already
// derived, with no flush, commit or blocking wait. A holder that states nothing
// about the session answers nil, so a cut of a session holding no state files no
// slice of that holder.
type sessionSnapshotter interface {
	SnapshotSessionState(ctx context.Context, session event.SessionID) ([]byte, error)
}

// StateRegistry is the ordered account of the state holders mounted on one
// runtime. The order is the registration order, never map iteration, so the same
// wiring aggregates the same slice order and a checkpoint renders the same bytes
// for the same state.
type StateRegistry struct {
	mu      sync.Mutex
	names   []string
	holders map[string]Stateful
	// pending holds the slices a restore named before the holder that states
	// them was mounted: a checkpoint restored during the assembly names the
	// slices of consumers the assembly mounts afterwards, so the payload waits
	// here and lands the moment its holder is mounted. A name still pending once
	// the wiring is complete is refused by the caller that can tell. One name
	// accumulates every payload a restore named for it: per-session checkpoints each
	// state a slice of the same name, and the holder restores them in order rather
	// than the last checkpoint overwriting the sessions beside it.
	pending map[string][][]byte
}

// NewStateRegistry returns an empty state registry.
func NewStateRegistry() *StateRegistry {
	return &StateRegistry{holders: map[string]Stateful{}, pending: map[string][][]byte{}}
}

// Register mounts one holder and answers the disposer that unmounts it. A holder
// naming no state and a name already mounted refuse the registration by name, so
// a wiring mistake stops the assembly instead of silently overwriting a slice
// another consumer owns.
func (r *StateRegistry) Register(holder Stateful) (func(), error) {
	if r == nil {
		return nil, errors.New("state: the registry is nil")
	}
	if holder == nil {
		return nil, errors.New("state: the state holder is nil")
	}
	name := holder.StateName()
	if name == "" {
		return nil, errors.New("state: the state holder names no state")
	}
	r.mu.Lock()
	if r.holders == nil {
		r.holders = map[string]Stateful{}
	}
	if _, held := r.holders[name]; held {
		r.mu.Unlock()
		return nil, fmt.Errorf("state: the state slice %q is already mounted", name)
	}
	r.holders[name] = holder
	r.names = append(r.names, name)
	payloads, stated := r.pending[name]
	delete(r.pending, name)
	r.mu.Unlock()
	if stated {
		for _, payload := range payloads {
			if err := holder.RestoreState(context.Background(), payload); err != nil {
				r.mu.Lock()
				delete(r.holders, name)
				kept := r.names[:0]
				for _, held := range r.names {
					if held != name {
						kept = append(kept, held)
					}
				}
				r.names = kept
				r.mu.Unlock()
				return nil, fmt.Errorf("state: restore the %q state slice: %w", name, err)
			}
		}
	}
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, held := r.holders[name]; !held {
			return
		}
		delete(r.holders, name)
		kept := r.names[:0]
		for _, held := range r.names {
			if held != name {
				kept = append(kept, held)
			}
		}
		r.names = kept
	}, nil
}

// Names reports the mounted state slice names in registration order.
func (r *StateRegistry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.names...)
}

// mounted reports the mounted holders in registration order, so every walk of
// them states and restores the same slices in the same order.
func (r *StateRegistry) mounted() []Stateful {
	r.mu.Lock()
	defer r.mu.Unlock()
	ordered := make([]Stateful, 0, len(r.names))
	for _, name := range r.names {
		ordered = append(ordered, r.holders[name])
	}
	return ordered
}

// Reduce folds one committed fact into the derived state of every mounted holder
// that derives its state from facts, in registration order, and answers the first
// refusal by the holder that made it: a fact one consumer cannot fold is a state
// this runtime would read as behind the chain it serves. A holder that states an
// account of its own is not asked — it implements no reducer face — so the pass
// names no consumer and needs no hook per consumer: a slice mounted through
// Register takes part the moment it is mounted.
//
// The pass is the recovery's face as much as the live commit's: a boot replays the
// facts committed past a cut through the same call, so the state a restart derives
// is the state the live process derived from the same facts. Nothing here drives
// an emitter: the work a consumer owes is started by the owner of that duty, never
// by a fold of the fact it owes it for.
func (r *StateRegistry) Reduce(ctx context.Context, evt event.Event) error {
	if r == nil {
		return nil
	}
	for _, holder := range r.mounted() {
		if err := reduceInto(ctx, holder, evt); err != nil {
			return fmt.Errorf("state: fold the fact %s into the %q state slice: %w", evt.Key, holder.StateName(), err)
		}
	}
	return nil
}

// ReduceInto folds one committed fact into one named holder's derived state, so a
// consumer that reads the facts past the cut of its own slice replays them through
// the same derivation the live commit drives. A holder that derives nothing from
// facts is not asked, and a name no holder answers is refused by name rather than
// silently folding nothing: a recovery reading a cut of a slice this runtime does
// not mount would leave that consumer behind the chain it serves.
func (r *StateRegistry) ReduceInto(ctx context.Context, name string, evt event.Event) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	holder, mounted := r.holders[name]
	r.mu.Unlock()
	if !mounted {
		return fmt.Errorf("state: no state holder answers the name %q", name)
	}
	if err := reduceInto(ctx, holder, evt); err != nil {
		return fmt.Errorf("state: fold the fact %s into the %q state slice: %w", evt.Key, name, err)
	}
	return nil
}

// reduceInto folds one fact into one holder when that holder derives its state
// from facts. The read is a type assertion on the declaration, never a table of
// consumer names: a holder that states an account of its own answers nothing.
func reduceInto(ctx context.Context, holder Stateful, evt event.Event) error {
	derives, declared := holder.(reducer)
	if !declared {
		return nil
	}
	return derives.Reduce(ctx, evt)
}

// ObservationDrivers reports the mounted holders that declare an observation
// drive, in registration order. The names are what a recovery states it folded
// without driving: the seam's pass folds state alone, and the owner of each duty
// starts it after the recovery returned.
func (r *StateRegistry) ObservationDrivers() []string {
	if r == nil {
		return nil
	}
	var drivers []string
	for _, holder := range r.mounted() {
		if _, drives := holder.(emitter); !drives {
			continue
		}
		drivers = append(drivers, holder.StateName())
	}
	return drivers
}

// SnapshotState aggregates the state of every mounted holder in registration order. A
// holder that fails to state its state refuses the whole snapshot by name: a
// checkpoint missing one slice would restore that consumer beside an empty state,
// which is worse than filing no checkpoint at all. The reads happen with no
// framework lock held and take only the holder's own, so a holder that is
// committing beside the cut answers it instead of waiting on it.
func (r *StateRegistry) SnapshotState(ctx context.Context) ([]event.CheckpointSlice, error) {
	if r == nil {
		return nil, nil
	}
	ordered := r.mounted()
	slices := make([]event.CheckpointSlice, 0, len(ordered))
	for _, holder := range ordered {
		payload, err := holder.SnapshotState(ctx)
		if err != nil {
			return nil, fmt.Errorf("state: snapshot the %q state slice: %w", holder.StateName(), err)
		}
		if payload == nil {
			payload = []byte(emptyStateJSON)
		}
		if !json.Valid(payload) {
			return nil, fmt.Errorf("state: the %q state slice states no JSON", holder.StateName())
		}
		slices = append(slices, event.CheckpointSlice{Name: holder.StateName(), Payload: json.RawMessage(payload)})
	}
	return slices, nil
}

// SnapshotSessionState aggregates the state the mounted holders state about one
// session chain, in registration order. A holder that keeps no state of a session
// is asked nothing and states no slice, so the checkpoint of one session is the size
// of that session's state and never of the account the run holds beside it. The
// same read rules hold as for the whole account: no framework lock is held across
// the holders' reads, and a holder that refuses to state itself refuses the whole
// cut by name rather than filing a checkpoint missing its slice.
func (r *StateRegistry) SnapshotSessionState(ctx context.Context, session event.SessionID) ([]event.CheckpointSlice, error) {
	if r == nil || session == "" {
		return nil, nil
	}
	var slices []event.CheckpointSlice
	for _, holder := range r.mounted() {
		scoped, statesSession := holder.(sessionSnapshotter)
		if !statesSession {
			continue
		}
		payload, err := scoped.SnapshotSessionState(ctx, session)
		if err != nil {
			return nil, fmt.Errorf("state: snapshot the %q state slice of session %s: %w", holder.StateName(), session, err)
		}
		if payload == nil {
			continue
		}
		if !json.Valid(payload) {
			return nil, fmt.Errorf("state: the %q state slice of session %s states no JSON", holder.StateName(), session)
		}
		slices = append(slices, event.CheckpointSlice{Name: holder.StateName(), Payload: json.RawMessage(payload)})
	}
	return slices, nil
}

// RestoreState hands every stated slice to the holder that owns it, in the order the
// checkpoint states. A slice no mounted holder owns is reported by name instead
// of dropped: a checkpoint whose state cannot be restored is not a checkpoint
// this runtime can start from.
func (r *StateRegistry) RestoreState(ctx context.Context, slices []event.CheckpointSlice) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	held := make(map[string]Stateful, len(r.holders))
	for name, holder := range r.holders {
		held[name] = holder
	}
	r.mu.Unlock()
	for _, slice := range slices {
		holder, mounted := held[slice.Name]
		if !mounted {
			r.mu.Lock()
			if r.pending == nil {
				r.pending = map[string][][]byte{}
			}
			r.pending[slice.Name] = append(r.pending[slice.Name], []byte(slice.Payload))
			r.mu.Unlock()
			continue
		}
		if err := holder.RestoreState(ctx, []byte(slice.Payload)); err != nil {
			return fmt.Errorf("state: restore the %q state slice: %w", slice.Name, err)
		}
	}
	return nil
}

// RestoreEmptyState restores every mounted holder from the state a cut that
// states no slice carries: the empty state. A boot whose tier holds no checkpoint
// states that cut, so it restores the holders the way a filed cut restores them
// instead of skipping them, and the replay past the cut rebuilds each one — past
// the zero cut that replay is the whole chain. The restore is the framework's, so
// a holder that can state itself from what it serves states that work here
// instead of through a reader of its own: the mailbox seats the obligations its
// durable account still owes beside the ones a cut states, by the notification
// identity acceptance answers to. A holder that refuses the empty state refuses
// the boot by name, because a runtime that starts beside a holder it could not
// restore reads work it does not hold.
func (r *StateRegistry) RestoreEmptyState(ctx context.Context) error {
	if r == nil {
		return nil
	}
	for _, holder := range r.mounted() {
		if err := holder.RestoreState(ctx, []byte(emptyStateJSON)); err != nil {
			return fmt.Errorf("state: restore the %q state slice: %w", holder.StateName(), err)
		}
	}
	return nil
}

// PendingRestore reports the state slices a restore named that no mounted holder
// states yet, in name order. A wiring that answers no name once it is complete
// has restored every slice the checkpoint carries.
func (r *StateRegistry) PendingRestore() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.pending))
	for name := range r.pending {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

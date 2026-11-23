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

// repeatReducer is the derivation face of a holder whose idempotence rests on the
// identity standing where it was folded: a holder that states a sequence and drops
// the identity of the material a rewrite moves out of it reads one identity reaching
// it twice as new material and states it a second time, so the seam tells it which
// fact repeats one the chain already carries. The commit classifies the fact — one
// the live ledger or the storage tier already holds is a repeat — and a holder that
// declares no such face is folded either way, because its fold keeps nothing the
// identity of a dropped entry would answer for.
type repeatReducer interface {
	// ReduceRepeat folds one fact the chain already carries into the holder's state.
	ReduceRepeat(ctx context.Context, evt event.Event) error
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

// unfinishedWork is the declaration face of a holder that keeps work its snapshot
// does not state back: obligations no committed fact records, which a slice leaving
// the registry would lose. The count is the holder's own business — the registry
// never reads what the items are, only how many the unmount would lose — and a
// holder that declares no such face is asked nothing, because the registry reads no
// account a holder did not open.
type unfinishedWork interface {
	// UnfinishedWork reports how many obligations leaving the registry would lose.
	UnfinishedWork() int
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
	pending   map[string][][]byte
	unapplied map[event.EventID]error
}

// NewStateRegistry returns an empty state registry.
func NewStateRegistry() *StateRegistry {
	return &StateRegistry{holders: map[string]Stateful{}, pending: map[string][][]byte{}}
}

// Register mounts one holder and answers the disposer that unmounts it. A holder
// naming no state and a name already mounted refuse the registration by name, so
// a wiring mistake stops the assembly instead of silently overwriting a slice
// another consumer owns. The disposer refuses by name while the holder declares
// unfinished work: the obligations are the slice's own, and unmounting it would
// state them nowhere, so the holder keeps the name, the state and the accounts
// until it settles. A slice that owes nothing leaves as it registered.
func (r *StateRegistry) Register(holder Stateful) (func() error, error) {
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
	return func() error {
		r.mu.Lock()
		held, mounted := r.holders[name]
		r.mu.Unlock()
		if !mounted {
			return nil
		}
		if owes, declares := held.(unfinishedWork); declares {
			if count := owes.UnfinishedWork(); count > 0 {
				return fmt.Errorf("state: refuse to unmount the %q state slice: %d unfinished obligation(s) would be lost", name, count)
			}
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, still := r.holders[name]; !still {
			return nil
		}
		delete(r.holders, name)
		kept := r.names[:0]
		for _, mounted := range r.names {
			if mounted != name {
				kept = append(kept, mounted)
			}
		}
		r.names = kept
		return nil
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
// refusal by the holder that made it. An unapplied fact prevents a checkpoint of
// its state until a later successful pass over that identity resolves the refusal.
// A pass clears only the refusal it read when it began, so a refusal recorded
// during that pass stands. Holders without a reducer face are not asked; no
// observation driver is invoked.
func (r *StateRegistry) Reduce(ctx context.Context, evt event.Event) error {
	return r.fold(ctx, evt, false)
}

// ReduceRepeat folds one fact the chain already carries into the derived state of
// every mounted holder, under the same order and the same refusal account Reduce
// keeps. A holder that declares the repeat face is told the fact repeats one the
// chain holds, so a sequence is not asked to state an identity twice; every other
// holder is folded as a filing commit folds it, because a fold idempotent by the
// identity the fact carries answers the same state either way.
func (r *StateRegistry) ReduceRepeat(ctx context.Context, evt event.Event) error {
	return r.fold(ctx, evt, true)
}

// fold drives one pass over the mounted holders, stating which of the two commits
// the fact is: one the ledger files, or one repeating a fact the chain already
// carries. The refusal account is the pass's own: a fact that landed in every
// holder clears the identity, and a fact a holder refused keeps it until a later
// pass resolves it.
func (r *StateRegistry) fold(ctx context.Context, evt event.Event, repeat bool) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	prior := r.unapplied[evt.Key]
	r.mu.Unlock()
	for _, holder := range r.mounted() {
		if err := reduceInto(ctx, holder, evt, repeat); err != nil {
			refused := fmt.Errorf("state: fold the fact %s into the %q state slice: %w", evt.Key, holder.StateName(), err)
			r.mu.Lock()
			if r.unapplied == nil {
				r.unapplied = map[event.EventID]error{}
			}
			r.unapplied[evt.Key] = refused
			r.mu.Unlock()
			return refused
		}
	}
	r.mu.Lock()
	if r.unapplied[evt.Key] == prior {
		delete(r.unapplied, evt.Key)
	}
	r.mu.Unlock()
	return nil
}

// reduceInto folds one fact into one holder when that holder derives its state
// from facts. The read is a type assertion on the declaration, never a table of
// consumer names: a holder that states an account of its own answers nothing. A
// repeat reaches the holder through the face it declared for it, and a holder
// declaring none is folded by the one derivation it states.
func reduceInto(ctx context.Context, holder Stateful, evt event.Event, repeat bool) error {
	if repeat {
		if told, declared := holder.(repeatReducer); declared {
			return told.ReduceRepeat(ctx, evt)
		}
	}
	derives, declared := holder.(reducer)
	if !declared {
		return nil
	}
	return derives.Reduce(ctx, evt)
}

// unappliedCutRefusal answers the refusal a cut states while a committed fact
// stands unapplied in any holder, naming each fact in identity order. No session
// scopes a refusal here: the holder that refused a fact reads it by its own
// session measure — the delivery slice reads the session the fact is delivered
// to, the chain the session that owns it — and the framework keeps no measure of
// its own to guess with, so the refusal holds every cut until the same identity
// lands again.
func (r *StateRegistry) unappliedCutRefusal() error {
	r.mu.Lock()
	keys := make([]event.EventID, 0, len(r.unapplied))
	refusals := make(map[event.EventID]error, len(r.unapplied))
	for key, err := range r.unapplied {
		keys = append(keys, key)
		refusals[key] = err
	}
	r.mu.Unlock()
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	named := make([]error, 0, len(keys))
	for _, key := range keys {
		named = append(named, fmt.Errorf("the committed fact %s stands unapplied: %w", key, refusals[key]))
	}
	return errors.Join(named...)
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
// which is worse than filing no checkpoint at all. A fact some holder has not
// applied refuses the cut for the same reason one step earlier: the state a cut
// states is the state a restart starts from, and a cut taken past an unapplied fact
// would start the next boot beside a consumer the chain has already left behind.
// The reads happen with no framework lock held and take only the holder's own, so a
// holder that is committing beside the cut answers it instead of waiting on it.
func (r *StateRegistry) SnapshotState(ctx context.Context) ([]event.CheckpointSlice, error) {
	if r == nil {
		return nil, nil
	}
	if err := r.unappliedCutRefusal(); err != nil {
		return nil, fmt.Errorf("state: refuse the checkpoint: %w", err)
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
// cut by name rather than filing a checkpoint missing its slice. A fact some
// holder has not applied refuses this cut as it refuses the whole account's: the
// framework keeps no session measure of its own to scope a refusal by — the
// holder that refused the fact reads it by the session it is delivered to, the
// chain by the session that owns it — so the refusal holds every cut until the
// same identity lands again.
func (r *StateRegistry) SnapshotSessionState(ctx context.Context, session event.SessionID) ([]event.CheckpointSlice, error) {
	if r == nil || session == "" {
		return nil, nil
	}
	if err := r.unappliedCutRefusal(); err != nil {
		return nil, fmt.Errorf("state: refuse the checkpoint of session %s: %w", session, err)
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

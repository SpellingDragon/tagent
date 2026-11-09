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
type Stateful interface {
	// StateName is the name the slice is keyed by in a checkpoint.
	StateName() string
	// SnapshotState states the recoverable state as JSON.
	SnapshotState(ctx context.Context) ([]byte, error)
	// RestoreState puts one snapshot back. A payload the holder does not recognize is
	// refused by name rather than restored as an empty state.
	RestoreState(ctx context.Context, payload []byte) error
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
	// the wiring is complete is refused by the caller that can tell.
	pending map[string][]byte
}

// NewStateRegistry returns an empty state registry.
func NewStateRegistry() *StateRegistry {
	return &StateRegistry{holders: map[string]Stateful{}, pending: map[string][]byte{}}
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
	payload, stated := r.pending[name]
	delete(r.pending, name)
	r.mu.Unlock()
	if stated {
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

// SnapshotState aggregates the state of every mounted holder in registration order. A
// holder that fails to state its state refuses the whole snapshot by name: a
// checkpoint missing one slice would restore that consumer beside an empty state,
// which is worse than filing no checkpoint at all.
func (r *StateRegistry) SnapshotState(ctx context.Context) ([]event.CheckpointSlice, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.Lock()
	ordered := make([]Stateful, 0, len(r.names))
	for _, name := range r.names {
		ordered = append(ordered, r.holders[name])
	}
	r.mu.Unlock()
	slices := make([]event.CheckpointSlice, 0, len(ordered))
	for _, holder := range ordered {
		payload, err := holder.SnapshotState(ctx)
		if err != nil {
			return nil, fmt.Errorf("state: snapshot the %q state slice: %w", holder.StateName(), err)
		}
		if payload == nil {
			payload = []byte("null")
		}
		if !json.Valid(payload) {
			return nil, fmt.Errorf("state: the %q state slice states no JSON", holder.StateName())
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
				r.pending = map[string][]byte{}
			}
			r.pending[slice.Name] = []byte(slice.Payload)
			r.mu.Unlock()
			continue
		}
		if err := holder.RestoreState(ctx, []byte(slice.Payload)); err != nil {
			return fmt.Errorf("state: restore the %q state slice: %w", slice.Name, err)
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

// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// The in-memory parent map used by a canonical replay. The zero value is ready
// to use; durable recall reads parents from the event chain instead of a journal.
package replay

import (
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// MemoryRelations is the causal parent map between committed facts. Parents are
// set and read here, never on the fact payload, inside one
// (PartitionID, SessionID) chain. Its zero value is an empty usable map.
type MemoryRelations struct {
	mu      sync.Mutex
	parents map[event.EventID]event.EventID
}

// SetParent sets the parent of child, or clears it when parent is empty.
func (r *MemoryRelations) SetParent(child event.EventID, parent event.EventID) error {
	if child == "" {
		return ledger.ErrKeyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	held, linked := r.parents[child]
	if linked && held == parent {
		return nil
	}
	if !linked && parent == "" {
		return nil
	}
	if r.parents == nil {
		r.parents = map[event.EventID]event.EventID{}
	}
	if parent == "" {
		delete(r.parents, child)
		return nil
	}
	r.parents[child] = parent
	return nil
}

// GetParent returns the parent of child, empty when there is none.
func (r *MemoryRelations) GetParent(child event.EventID) (event.EventID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.parents[child], nil
}

// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// The relation attachment is the one parent-graph face of a run. The host opens
// the journal before assembly, live commits append through the same ledger, and
// the full boot walk backfills an empty graph without replacing the journal.
package assembly

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// withRelations attaches the host relation ledger before the chain is replayed.
// It is host wiring rather than a second public registration surface.
func withRelations(relations *replay.MemoryRelations) Option {
	return func(c *assemblyConfig) { c.relations = relations }
}

// linkRelationLocked links one committed fact to its chain predecessor. The
// caller holds landing, so commit order and parent order cannot diverge. The
// head advances even when the journal refuses the link: the fact is already on
// the chain, and the full boot walk can retry the derived edge later.
func (r *Run) linkRelationLocked(chain replay.ChainKey, key event.EventID) error {
	if r.relations == nil || chain.SessionID == "" || key == "" || replay.SyntheticKey(key) {
		return nil
	}
	previous := r.relationHeads[chain]
	err := r.relations.SetParent(key, previous)
	if r.relationHeads == nil {
		r.relationHeads = map[replay.ChainKey]event.EventID{}
	}
	r.relationHeads[chain] = key
	return err
}

// linkRelation restores one edge during the full boot walk.
func (r *Run) linkRelation(chain replay.ChainKey, key event.EventID) error {
	r.landing.Lock()
	defer r.landing.Unlock()
	return r.linkRelationLocked(chain, key)
}

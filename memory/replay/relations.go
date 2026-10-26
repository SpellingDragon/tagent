// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// The causal relation ledger: the parent chain one fact chain's replay
// maintains, committed to the append-only journal before the held link moves,
// and rebuilt from that journal when the ledger is opened again.
package replay

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

var (
	// ErrRelationsJournalUnwired reports a replay check asked of a ledger that
	// holds no journal, so it has no durable source to compare against.
	ErrRelationsJournalUnwired = errors.New("memory: the relation ledger holds no journal")
	// ErrRelationsReplayMismatch reports a journal whose replay names a chain
	// other than the one the ledger holds, so the served chain is not the durable
	// record.
	ErrRelationsReplayMismatch = errors.New("memory: the replayed relation chain differs from the held chain")
)

// relationsJournal is the durable record the ledger commits through: one append
// per link and the replay of every appended record.
type relationsJournal interface {
	Append(record ledger.RelationRecord) error
	Records(ctx context.Context) ([]ledger.RelationRecord, error)
}

// MemoryRelations is the causal parent ledger between committed facts: the
// parent of one fact is set and read back here, never on the fact payload. The
// replay maintains these links inside one (PartitionID, SessionID) chain, so a
// walk never crosses from one session's chain into another's.
//
// A ledger opened over a journal commits a link to that journal before the held
// link moves: a SetParent that returns success names a link a later replay reads
// back, and one that returns an error leaves the held chain as it was. The ledger
// is the one relation surface a rebuild writes through: it is the only
// implementation, so the rebuild names it directly rather than declaring a port
// with no second implementer behind it.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
type MemoryRelations struct {
	mu      sync.Mutex
	parents map[event.EventID]event.EventID
	journal relationsJournal
}

// NewMemoryRelations returns an empty relation ledger with no journal behind it:
// its links live for the life of the process and declare no recovery across
// process exit.
func NewMemoryRelations() *MemoryRelations {
	return &MemoryRelations{parents: map[event.EventID]event.EventID{}}
}

// OpenMemoryRelations opens the relation ledger rooted at root: every record the
// journal holds is replayed into the held chain, and every link a SetParent
// commits reaches that same journal before the held chain moves.
func OpenMemoryRelations(root string) (*MemoryRelations, error) {
	journal, err := ledger.OpenRelationsJournal(root)
	if err != nil {
		return nil, err
	}
	records, err := journal.Records(context.Background())
	if err != nil {
		return nil, err
	}
	return &MemoryRelations{parents: replayedParents(records), journal: journal}, nil
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
	if r.journal != nil {
		if err := r.journal.Append(ledger.RelationRecord{Child: child, Parent: parent}); err != nil {
			return err
		}
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

// Links returns a copy of the parent links the ledger holds, so a caller can
// compare a rebuilt chain against the one it serves.
func (r *MemoryRelations) Links() map[event.EventID]event.EventID {
	r.mu.Lock()
	defer r.mu.Unlock()
	links := make(map[event.EventID]event.EventID, len(r.parents))
	for child, parent := range r.parents {
		links[child] = parent
	}
	return links
}

// VerifyReplay reports whether the journal replays to the chain the ledger
// holds, so a caller can assert the durable record and the served chain agree.
// A ledger opened without a journal has no replay source and says so.
func (r *MemoryRelations) VerifyReplay(ctx context.Context) error {
	if r.journal == nil {
		return ErrRelationsJournalUnwired
	}
	records, err := r.journal.Records(ctx)
	if err != nil {
		return err
	}
	replayed := replayedParents(records)
	r.mu.Lock()
	defer r.mu.Unlock()
	if !maps.Equal(replayed, r.parents) {
		return fmt.Errorf("%w: %d replayed, %d held", ErrRelationsReplayMismatch, len(replayed), len(r.parents))
	}
	return nil
}

// replayedParents rebuilds the parent chain from the records a journal holds:
// each record names the link that record commits, and a later record of the same
// child replaces the earlier one.
func replayedParents(records []ledger.RelationRecord) map[event.EventID]event.EventID {
	parents := make(map[event.EventID]event.EventID, len(records))
	for _, record := range records {
		if record.Parent == "" {
			delete(parents, record.Child)
			continue
		}
		parents[record.Child] = record.Parent
	}
	return parents
}

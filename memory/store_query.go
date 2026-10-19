// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// The observation table between the segment store and the query face: a
// committed event reaches the query face by being decoded and landed here
// first, and only the events landed are visible to QueryEvents and to the
// retrieval built over it. The table keeps the consumption order of the read
// path honest — a query consumes an event after the table observes it, and
// never at the moment its bytes happen to be committed.
package memory

import (
	"context"
	"sync"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// observedEntry is one landed event: the query record the read face filters
// and orders on, and the reference the recall path reads the original under.
type observedEntry struct {
	record EventRecord
	ref    ledger.OriginalRef
}

// StoreQuerySource is the query source over the observation table: it answers
// QueryEvents on exactly the query semantics of the memory read face, over the
// events landed from the committed store rather than over records a caller
// assembled by hand.
type StoreQuerySource struct {
	mu      sync.Mutex
	entries map[event.EventID]observedEntry
}

// NewStoreQuerySource returns an empty observation table.
func NewStoreQuerySource() *StoreQuerySource {
	return &StoreQuerySource{entries: map[event.EventID]observedEntry{}}
}

// LandEvent decodes one committed event under the body schema and lands it in
// the table: the record a query reads and the reference the recall path reads
// the original under. A body whose canonical key disagrees with the stored key
// is refused by name. A repeat of one event key keeps the record of the higher
// window, and a repeat at the same window is an idempotent no-op.
func (s *StoreQuerySource) LandEvent(ctx context.Context, stored ledger.StoredEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := DecodeEventBody(stored.Body)
	if err != nil {
		return err
	}
	if body.Key != stored.Key {
		return ErrBodyKeyMismatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, held := s.entries[stored.Key]; held && prior.record.WindowTS >= stored.WindowTS {
		return nil
	}
	s.entries[stored.Key] = observedEntry{
		record: body.Record(stored.PartitionID, stored.WindowTS),
		ref:    body.Ref(),
	}
	return nil
}

// ObserveStore lands every event the store has published into the table. An
// already landed event keeps its record and only the events not yet landed are
// decoded and added, so observing twice over unchanged state moves nothing.
func (s *StoreQuerySource) ObserveStore(ctx context.Context, store *ledger.FileSegmentStore) error {
	events, err := store.Events(ctx)
	if err != nil {
		return err
	}
	for _, stored := range events {
		if err := s.LandEvent(ctx, stored); err != nil {
			return err
		}
	}
	return nil
}

// Observed reports whether the table holds the event key.
func (s *StoreQuerySource) Observed(key event.EventID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, held := s.entries[key]
	return held
}

// ObservedCount returns how many event keys the table holds.
func (s *StoreQuerySource) ObservedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Reference returns the recall reference the event key was landed with, and
// reports whether the table holds one.
func (s *StoreQuerySource) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, held := s.entries[key]
	if !held {
		return ledger.OriginalRef{}, false
	}
	return entry.ref, true
}

// QueryEvents returns the references matching opts over the landed events, on
// exactly the declarative terms of QueryOptions. An event the table has not
// landed takes no part in the answer, whatever the store behind the table
// holds.
func (s *StoreQuerySource) QueryEvents(ctx context.Context, opts QueryOptions) ([]EventReference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make(map[event.EventID]EventRecord, len(s.entries))
	for key, entry := range s.entries {
		records[key] = entry.record
	}
	return queryEventRecords(records, opts)
}

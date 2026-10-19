package memoryledgerexample

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/api/event"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// errEnumeration is the named failure a store reports instead of a partial key list.
var errEnumeration = errors.New("memoryexample: key enumeration failed")

// keyStore is the canonical reclamation surface: it enumerates the keys it holds,
// tombstones an expired one and reclaims the tombstoned ones at compaction.
type keyStore struct {
	keys       []ledger.StoredKey
	tombstoned map[event.EventID]struct{}
	failing    bool
}

// EnumerateKeys lists every key the store holds, or fails by name.
func (s *keyStore) EnumerateKeys(context.Context) ([]ledger.StoredKey, error) {
	if s.failing {
		return nil, errEnumeration
	}
	return append([]ledger.StoredKey(nil), s.keys...), nil
}

// Tombstone marks one key for reclamation at compaction, leaving it readable until then.
func (s *keyStore) Tombstone(_ context.Context, key event.EventID, _ time.Time) error {
	if s.tombstoned == nil {
		s.tombstoned = map[event.EventID]struct{}{}
	}
	s.tombstoned[key] = struct{}{}
	return nil
}

// ReclaimTombstoned physically destroys the given tombstoned keys and reports how many
// were destroyed.
func (s *keyStore) ReclaimTombstoned(_ context.Context, keys []event.EventID) (int, error) {
	reclaimed := 0
	for _, key := range keys {
		if _, ok := s.tombstoned[key]; !ok {
			continue
		}
		delete(s.tombstoned, key)
		reclaimed++
	}
	return reclaimed, nil
}

var _ ledger.LifecycleStore = (*keyStore)(nil)

// ExampleLifecycleStore enumerates the keys a store holds, tombstones the one past its
// window and reports that compaction destroyed exactly the tombstoned key.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func ExampleLifecycleStore() {
	ctx := context.Background()
	store := &keyStore{keys: []ledger.StoredKey{
		{Key: "event-1", Type: "conversation", Partition: 1},
		{Key: "event-2", Type: "tool_result", Partition: 1},
	}}

	keys, err := store.EnumerateKeys(ctx)
	if err != nil {
		return
	}
	if err := store.Tombstone(ctx, keys[0].Key, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		return
	}
	reclaimed, err := store.ReclaimTombstoned(ctx, []event.EventID{keys[0].Key})
	if err != nil || reclaimed != 1 {
		return
	}
	if _, err := store.ReclaimTombstoned(ctx, []event.EventID{keys[1].Key}); err != nil {
		return
	}
}

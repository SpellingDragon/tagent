// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// journalKV is the key-value seam of the localfile tier over its append-only
// journal. A commit appends one record and publishes its values to the resident
// set only after that append returned success, so a commit whose write failed
// leaves the view answering exactly the records the file holds. Appending is not
// the barrier: the barrier is the log's own sync, which a commit takes when it
// carries nothing to append, so the tier that reports durable success pays for
// one barrier per commit instead of one per stage and one per confirmation.
package ledger

import (
	"context"
	"fmt"
	"sync"
)

// journalKV is the localfile tier's key-value view over one journal: positions
// in the log are the persistent state, and memory residency is entry-level with
// a byte budget, so a cold value is evicted from memory and read back from the
// record that carries it.
type journalKV struct {
	mu sync.Mutex
	// log is the append-only record of every commit.
	log *journalLog
	// resident is the entry-level LRU: the keys and values the process holds.
	resident *kvResidentSet
}

var _ AtomicStore = (*journalKV)(nil)

// openJournalKV opens the key-value view rooted at dir, replaying the index of
// every record the log already holds. The directory that holds the retired
// sharded-snapshot layout is refused by name, never migrated.
func openJournalKV(dir string) (*journalKV, error) {
	log, err := openJournalLog(dir)
	if err != nil {
		return nil, err
	}
	return &journalKV{log: log, resident: newKVResidentSet()}, nil
}

// Read returns the pairs the fetch names: the one value held under the key it
// names, or every held pair whose key carries the named prefix, ordered by key.
// A value the resident set evicted is read back from the record that carries it.
func (k *journalKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if fetch.Key != "" {
		value, held, err := k.valueLocked(fetch.Key)
		if err != nil || !held {
			return nil, err
		}
		return []Pair{{Key: fetch.Key, Value: value}}, nil
	}
	keys := k.log.keys(fetch.Prefix)
	out := make([]Pair, 0, len(keys))
	for _, key := range keys {
		value, held, err := k.valueLocked(key)
		if err != nil {
			return nil, err
		}
		if !held {
			continue
		}
		out = append(out, Pair{Key: key, Value: value})
	}
	return out, nil
}

// Keys returns the keys the prefix names, in key order, from the index alone:
// no value is read, so the enumeration a loader pays for stays small.
func (k *journalKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.log.keys(prefix), nil
}

// Namespaces returns the key namespaces the held keys carry, without reading
// values.
func (k *journalKV) Namespaces(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.log.namespaces(), nil
}

// Commit appends one record of the batch and publishes its values once that
// append returned success. An empty batch carries nothing to append and only
// completes the barrier, which is how a commit after earlier ones confirms what
// they already wrote. The caller holds no staged state across a failed append:
// the record is one frame, so it is all of the batch or none of it. A failed
// append or sync leaves the log's own state that the process cannot name, so the
// failure is reported as ErrCommitUncertain and the caller re-reads.
func (k *journalKV) Commit(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(batch.Puts) == 0 && len(batch.Deletes) == 0 {
		if err := k.log.sync(); err != nil {
			return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
		}
		return nil
	}
	record := journalRecord{Version: journalFormatVersion}
	for _, put := range batch.Puts {
		record.Puts = append(record.Puts, journalPut{Key: put.Key, Value: append([]byte{}, put.Value...)})
	}
	record.Deletes = append(record.Deletes, batch.Deletes...)
	if err := k.log.append(record); err != nil {
		return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
	}
	for _, put := range batch.Puts {
		k.resident.put(put.Key, append([]byte(nil), put.Value...))
	}
	for _, key := range batch.Deletes {
		k.resident.remove(key)
	}
	return nil
}

// valueLocked answers one key's value from the resident set, reading it back
// from its record on a miss, and answers whether the view holds the key at all.
// A held value of zero bytes is a held key: its emptiness is the value the
// caller committed, not the absence of one. The caller holds the lock.
func (k *journalKV) valueLocked(key string) ([]byte, bool, error) {
	if value, held := k.resident.get(key); held {
		return append([]byte{}, value...), true, nil
	}
	value, held, err := k.log.read(key)
	if err != nil || !held {
		return nil, false, err
	}
	k.resident.put(key, value)
	return append([]byte{}, value...), true, nil
}

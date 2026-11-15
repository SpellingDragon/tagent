// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// journalKV is the key-value seam of the localfile tier over its append-only
// journal. A commit appends one record, completes the log's barrier over it and
// only then publishes its values to the resident set, so a successful return
// means the record is durable and a commit whose write or barrier failed leaves
// the view answering exactly the records the file holds.
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

// JournalBytesOpen reports the journal bytes this view's open replayed: the read
// cost of one boot at the engine layer, fixed at open and never the append offset
// the log grew to afterwards. It is read-only and takes no lock, because the open
// sets it before any caller holds the view, and it exists so a boot measure names
// the engine's own read instead of a copy kept beside it. A caller that holds the
// engine as an AtomicStore reads it through the interface the method states.
//
// The read it states is the tail past the index cut: an open that places the cut
// pays the records the last rewrite left and never the history behind it.
func (k *journalKV) JournalBytesOpen() int64 {
	if k == nil || k.log == nil {
		return 0
	}
	return k.log.openReadBytes()
}

// TailKeys reports the keys the records past this view's index cut wrote, and nil
// beside a view whose open read the whole log: a caller that reads this set reads
// the work the cut does not cover, and nil states that nothing is covered rather
// than that nothing is there.
func (k *journalKV) TailKeys() map[string]struct{} {
	if k == nil || k.log == nil {
		return nil
	}
	return k.log.tailKeys()
}

// IndexCutNotice states why the open read the whole log beside a cut it could not
// place, and is empty beside an open that placed its cut or found none. The ledger
// package keeps no logger, so the open states the fallback here and the caller that
// holds one states it.
func (k *journalKV) IndexCutNotice() string {
	if k == nil || k.log == nil {
		return ""
	}
	return k.log.cutNotice
}

// Commit appends one record of the batch, completes the barrier over it and
// publishes its values once that barrier returned success. An empty batch
// carries nothing to append and only completes the barrier, which confirms what
// earlier commits appended. The caller holds no staged state across a failed
// append: the record is one frame, so it is all of the batch or none of it. A
// failed append or sync leaves the log's own state that the process cannot name,
// so the failure is reported as ErrCommitUncertain and the caller re-reads.
func (k *journalKV) Commit(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(batch.Puts) > 0 || len(batch.Deletes) > 0 {
		record := journalRecord{Version: journalFormatVersion}
		for _, put := range batch.Puts {
			record.Puts = append(record.Puts, journalPut{Key: put.Key, Value: append([]byte{}, put.Value...)})
		}
		record.Deletes = append(record.Deletes, batch.Deletes...)
		if err := k.log.append(record); err != nil {
			return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
		}
	}
	if err := k.log.sync(); err != nil {
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

// coldRetirement is the journal-only engine capability of moving the keys a caller
// did not keep out of the hot log and into a cold root: the copy, its byte-for-byte
// verification and the rewrite of the hot log are one call, so no caller observes a
// hot log whose settled keys are half copied. An engine that cannot express the
// rewrite — the in-process tier, which holds nothing across exit — does not answer
// this capability at all.
//
//   - settle names the keys this pass moves, read from the one snapshot the caller took: a key a writer commits after that snapshot is not in the list and stays in the hot log, so a family whose reader never reads the cold journal cannot be moved out from under it.
//   - replaced carries the records the rewrite files after the kept keys, so the state the caller derives from the relocation lands in the same atomic replacement: a process that stops during the pass reads either the log it started with or the rewritten one.
type coldRetirement interface {
	retireSettled(ctx context.Context, coldDir string, settle []string, replaced []journalPut) (RelocateReport, error)
}

// retireSettled copies the settled keys of this journal into coldDir, verifies the
// copy and rewrites the journal to hold every key the pass was not told to settle,
// beside the replaced records. The caller holds no writer of this view while it
// runs and states the keys the pass settles: which facts are still needed is the
// consumer's answer, never the engine's, and a key the caller did not name is
// never moved by the pass that did not see it.
//
//   - The resident set drops the settled keys, because a value kept here would answer a key this log does not hold, which is a view the next process to open the log cannot reproduce.
func (k *journalKV) retireSettled(ctx context.Context, coldDir string, settle []string, replaced []journalPut) (RelocateReport, error) {
	if err := ctx.Err(); err != nil {
		return RelocateReport{}, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	report, err := k.log.relocate(ctx, coldDir, settle, replaced)
	if err != nil {
		return report, err
	}
	for _, key := range settle {
		k.resident.remove(key)
	}
	for _, put := range replaced {
		k.resident.put(put.Key, append([]byte(nil), put.Value...))
	}
	return report, nil
}

// eachCommittedNewestFirst names the held keys from the newest commit to the
// oldest and stops when visit answers false. It walks the order the log appended
// its frames in, so the answer costs the records walked and never a sort of the
// index; a key the view deleted is not held and is skipped, and a key a later
// record rewrote is named at each of its records, newest first.
func (k *journalKV) eachCommittedNewestFirst(ctx context.Context, visit func(key string) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.log.eachKeyNewestFirst(func(key string) (bool, error) {
		if value, held := k.log.index[key]; !held || value.deleted {
			return true, nil
		}
		return visit(key)
	})
}

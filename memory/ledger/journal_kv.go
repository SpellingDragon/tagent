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
	"errors"
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
	// lock is the write rights this open took on its storage root, nil beside a
	// read view. The view holds it from the open until Close, which is what makes
	// the duplicate check and the commit of one fact one critical section against
	// every other process.
	lock *rootWriteLock
	// closed reports whether Close ended this view, so a repeated Close releases
	// nothing a second time and a commit states that it met a closed view.
	closed bool
}

var _ AtomicStore = (*journalKV)(nil)

// ErrCommitWithoutWriteRights reports a commit carrying keys through a view that
// declared no write rights at open: one root carries one writer, so a view holding
// none reads beside that writer and its commit is refused by name before any byte
// is appended, never admitted as a second writer of the root.
var ErrCommitWithoutWriteRights = errors.New("memory: commit through a view that holds no write rights")

// openJournalKV opens the key-value view a writer holds over the journal of dir: the
// view that declares the root's write rights.
func openJournalKV(dir string) (*journalKV, error) {
	return openJournalView(dir, true)
}

// openJournalView opens the key-value view over the journal of dir for the face the caller
// declares. The directory that holds the retired sharded-snapshot layout is refused by
// name, never migrated.
//   - A view holding the root's write rights opens the log for append and cuts a torn tail a stopped writer left
//   - A view holding none reads beside the writer, creates no file, cuts nothing, and refuses a commit carrying keys as ErrCommitWithoutWriteRights
func openJournalView(dir string, writeRights bool) (*journalKV, error) {
	log, err := openJournalLogFace(dir, writeRights)
	if err != nil {
		return nil, err
	}
	return &journalKV{log: log, resident: newKVResidentSet()}, nil
}

// Read returns the pairs the fetch names: the one value held under the key it
// names, or every held pair whose key carries the named prefix, ordered by key.
// A value the resident set evicted is read back from the record that carries it.
//   - A read through a view Close ended is refused as ErrReadAfterClose, never answered as a key the log holds no body for
func (k *journalKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, ErrReadAfterClose
	}
	if fetch.Key != "" {
		value, held, err := k.valueLocked(fetch.Key)
		if err != nil || !held {
			return nil, err
		}
		return []Pair{{Key: fetch.Key, Value: value}}, nil
	}
	out := make([]Pair, 0, 16)
	err := k.log.eachHeld(fetch.Prefix, func(key string, span journalAddressSpan) error {
		value, err := k.valueOfSpanLocked(key, span)
		if err != nil {
			return err
		}
		out = append(out, Pair{Key: key, Value: value})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Keys returns the keys the prefix names, in key order, from the index alone:
// no value is read, so the enumeration a loader pays for stays small.
//   - An enumeration through a closed view is refused as ErrReadAfterClose, never answered as a store holding no key
func (k *journalKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, ErrReadAfterClose
	}
	return k.log.keys(prefix)
}

// Namespaces returns the key namespaces the held keys carry, without reading
// values.
//   - An enumeration through a closed view is refused as ErrReadAfterClose, never answered as a store holding no namespace
func (k *journalKV) Namespaces(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, ErrReadAfterClose
	}
	return k.log.namespaces()
}

// JournalBytesOpen reports the journal bytes this view's open replayed: the read
// cost of one boot at the engine layer, fixed at open and never the append offset
// the log grew to afterwards. It is read-only and takes no lock, because the open
// sets it before any caller holds the view, and it exists so a boot measure names
// the engine's own read instead of a copy kept beside it. A caller that holds the
// engine as an AtomicStore reads it through the interface the method states.
//
// The read it states is the frames past the watermark the index files state, so it
// is zero beside a root those files already reach: whether a log holds history is a
// fact of its length, and this count states the window the open replays into its
// index, not the integrity verification of the whole log.
func (k *journalKV) JournalBytesOpen() int64 {
	if k == nil || k.log == nil {
		return 0
	}
	return k.log.openReadBytes()
}

// IndexUnstatedNotice states why this view read the whole log instead of the commits
// past an index watermark: the index files beside the log state no watermark this build
// reads, so the open replayed every frame and the answer states the read that cost. It
// is empty beside a view whose index files state a watermark. The ledger package keeps
// no logger, so the open states the fallback here and the caller that holds one states it.
func (k *journalKV) IndexUnstatedNotice() string {
	if k == nil || k.log == nil || k.log.addr == nil {
		return ""
	}
	return k.log.addr.notice
}

// TornTailBytesOpen reports the byte of the log the last frame the open read starts at
// when that frame is not whole, and whether the log ended in a torn tail at all. The two
// answers are one measure because the byte alone cannot state the second: a log whose one
// frame is torn from its first byte stops at the zero a log of whole frames stops at.
//   - A view holding the root's write rights cut those bytes off the log at open, and a view holding none served the whole frames before them and cut nothing
func (k *journalKV) TornTailBytesOpen() (int64, bool) {
	if k == nil || k.log == nil {
		return 0, false
	}
	return k.log.tornTail, k.log.torn
}

// Commit appends one record of the batch, completes the barrier over it and
// publishes its values once that barrier returned success. An empty batch
// carries nothing to append and only completes the barrier, which confirms what
// earlier commits appended. The caller holds no staged state across a failed
// append: the record is one frame, so it is all of the batch or none of it. A
// record the frame bound refuses is answered by name: that refusal is decided
// before the write, so it states a commit that landed no key rather than an
// uncertain one. Any other failed append or sync leaves the log's own state that
// the process cannot name, so the failure is reported as ErrCommitUncertain and
// the caller re-reads. That re-read answers absence for the keys of the failed
// batch even beside a batch that landed whole: a view publishes nothing from a
// commit it did not see return success, so an append whose barrier failed is
// reported and not served. A batch that carries keys through a view Close ended is
// refused by name instead, because the record it would append belongs to no
// writer of the root; an empty batch still completes the barrier, so a repeated
// Close stays a no-op.
//   - A batch carrying keys through a view holding no write rights is refused as ErrCommitWithoutWriteRights before any append
func (k *journalKV) Commit(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed && (len(batch.Puts) > 0 || len(batch.Deletes) > 0) {
		return ErrCommitAfterClose
	}
	if len(batch.Puts) > 0 || len(batch.Deletes) > 0 {
		if k.log.addr != nil && k.log.addr.window != nil {
			return ErrCommitWithoutWriteRights
		}
		record := journalRecord{Version: journalFormatVersion}
		for _, put := range batch.Puts {
			record.Puts = append(record.Puts, journalPut{Key: put.Key, Value: append([]byte{}, put.Value...)})
		}
		record.Deletes = append(record.Deletes, batch.Deletes...)
		if err := k.log.append(record); err != nil {
			if errors.Is(err, ErrJournalFrameTooLarge) {
				return err
			}
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

// Close ends this view: it releases the journal handle and gives the storage root
// up to the next writer, and repeats as a no-op.
//   - The release is the point the root admits its next writer, so a commit after Close is refused by name rather than appended beside that writer
//   - A read view holds no write rights and releases those of the open it took, so closing one gives up nothing another process was waiting for
func (k *journalKV) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	logErr := k.log.close()
	return errors.Join(logErr, k.lock.release())
}

// valueOfSpanLocked answers the value one enumerated key states, from the resident
// set or from the span the enumeration already read, so a prefix read probes the
// table once per key instead of once per listing and once per value. The caller
// holds the lock.
func (k *journalKV) valueOfSpanLocked(key string, span journalAddressSpan) ([]byte, error) {
	if value, held := k.resident.get(key); held {
		return append([]byte{}, value...), nil
	}
	value, _, err := k.log.addr.value(span)
	if err != nil {
		return nil, err
	}
	k.resident.put(key, value)
	return append([]byte{}, value...), nil
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

// eachCommittedNewestFirst names the held keys from the newest commit to the oldest
// and stops when visit answers false. It walks the commit order the engine's address
// states, so the answer costs the records walked and never a sort of the index; a key
// the newest commit that touched it deleted is not held and is skipped, and a key
// several commits rewrote is named once, at its newest commit. The walk hands the view
// back before each visit, the way EachCommittedNewestFirst does, so a caller reads the
// value of a key it names beside the walk instead of under its lock.
func (k *journalKV) eachCommittedNewestFirst(ctx context.Context, visit func(key string) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return ErrReadAfterClose
	}
	return k.log.eachKeyNewestFirst(func(key string) (bool, error) {
		k.mu.Unlock()
		defer k.mu.Lock()
		return visit(key)
	})
}

// EachCommittedKeyNewestFirst names the held keys from the newest commit to the
// oldest and stops when visit answers false. The walk pays the records it walks and
// reads no value, so it orders the statements one log holds without the values those
// statements carry; a caller that reads the value of each key it names asks
// EachCommittedNewestFirst, which answers that value beside the order.
func (k *journalKV) EachCommittedKeyNewestFirst(ctx context.Context, visit func(key string) (bool, error)) error {
	return k.eachCommittedNewestFirst(ctx, visit)
}

// EachCommittedNewestFirst names the held keys from the newest commit to the oldest
// beside the value each key's newest commit left under it, and stops when visit
// answers false. The walk reads the order the address states and one value span per
// key it names, so a reader that ranks entries beside their values holds the view for
// the commits it walks and nothing behind them. It is the CommitOrder of this engine.
//
// The walk hands the view back before each visit and takes it again behind it, so a
// visit reads this view — a key, a prefix, a whole event — instead of waiting on the
// lock the walk holds. The order it states is the one its start stated: a commit that
// lands while a visit runs is named by the walk that starts after it, never by this one.
func (k *journalKV) EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return ErrReadAfterClose
	}
	return k.log.eachKeyNewestFirstValue(func(key string, value []byte) (bool, error) {
		k.mu.Unlock()
		defer k.mu.Lock()
		return visit(key, value)
	})
}

// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The one atomic storage seam a tier opens: engines implement it, the segment
// and original stores read and commit through it, and a caller injects failures
// by wrapping it. No caller names an engine's file layout, so switching engines
// needs no adaptation and the same contract test runs over every engine.
package ledger

import (
	"context"
	"errors"
	"strings"
)

// Pair is one key-value entry a read returns or a commit writes.
type Pair struct {
	// Key is the entry's key.
	Key string
	// Value is the entry's bytes.
	Value []byte
}

// Fetch names one read: the single key it names, or every key carrying the
// prefix it names when no key is named. An empty fetch reads the whole store,
// ordered by key.
type Fetch struct {
	// Key names the one entry to read.
	Key string
	// Prefix names the entries to read when Key is empty.
	Prefix string
}

// Batch is one write set committed as a unit: the keys it puts and the keys it
// deletes. A key stands on one side or the other, so the order the two sides are
// applied in names no different state.
type Batch struct {
	// Puts are the entries the commit writes.
	Puts []Pair
	// Deletes are the keys the commit removes.
	Deletes []string
}

// AtomicStore is the storage engine every tier opens: one atomic commit unit and one read face that answers only bytes a completed commit landed.
//   - Commit is all or nothing: a commit that returns an error publishes no part of its batch, and no later commit carries its bytes into a read
//   - A commit takes exactly one barrier: a successful return means every value of the batch is durable and readable by a later process the engine persists across
//   - An empty batch writes nothing and only completes that barrier
//   - Read answers landed bytes only: an engine never serves a value whose commit did not return success
//   - A commit whose durable outcome the engine cannot name returns ErrCommitUncertain, and the caller re-reads to decide instead of reporting success or failure
//   - An engine that can decide before writing that it cannot carry the batch refuses it by name instead, so a refusal that landed no key never answers ErrCommitUncertain
//   - That re-read is an open that took no part in the commit, because the view that committed publishes nothing from a commit it did not see return success: its own read answers absence even beside a batch that landed whole
//   - ErrCommitUncertain never means that nothing landed: a caller that reads a refusal as zero writes re-commits a fact the store already holds
//   - An engine whose every call lands one key keeps this contract by refusing a batch that carries several keys: it names ErrBatchNotAtomic and no key lands, and the tier's StoreCapabilities.AtomicBatch states the same limit
//   - Failure injection belongs at this seam: a caller wraps an AtomicStore and fails, delays or drops a commit or a read, so the same injection runs over any engine and never names a file layout
type AtomicStore interface {
	// Read returns the pairs the fetch names.
	Read(ctx context.Context, fetch Fetch) ([]Pair, error)
	// Keys returns the keys the prefix names, in key order, without reading
	// their values.
	Keys(ctx context.Context, prefix string) ([]string, error)
	// Namespaces returns the key namespaces the store holds, in name order,
	// without reading any value.
	Namespaces(ctx context.Context) ([]string, error)
	// Commit applies one batch behind one barrier.
	Commit(ctx context.Context, batch Batch) error
}

// CommitOrder is the capability of stating the order an engine's commits landed in:
// the keys it holds beside the value each key's newest statement carries, named from
// the newest committed record to the oldest, stopping when visit answers false. An
// append-log engine keeps that order on disk, so naming it costs the records walked
// and the values those records state; a key-ordered key-value engine keeps no such
// order and does not answer this capability at all, so a caller reads the answer
// through the type assertion instead of a method every engine carries.
//   - EachCommittedNewestFirst names a key once, at its newest commit, and skips a key the newest commit that touched it deleted
//   - The value is the one that newest statement left under the key, so a caller identifies an entry without a second read
//   - visit answers false to end the walk where it stands, so a caller reads the prefix its order bounds instead of the whole history
//
// The redline list (4 rules):
// MUST answer every key once, from the newest committed record to the oldest.
// MUST end the walk where visit answers false and return the error visit reports.
// MUST answer the value the newest statement left under the key, so a caller never reads it twice.
// MUST NOT name a key at an older commit or name a key the newest commit that touched it deleted.
type CommitOrder interface {
	EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) error
}

// ErrCommitUncertain reports a commit whose durable outcome the engine cannot
// name: the write or its barrier did not return success and the engine could not
// confirm what the store holds, so the caller re-reads instead of reporting
// either outcome.
var ErrCommitUncertain = errors.New("memory: commit outcome uncertain")

// ErrBatchNotAtomic reports a commit of a batch carrying several keys handed to an
// engine whose every call lands one key: the engine refuses the batch before any
// call runs, so a reader never meets the first keys landed beside a key that
// failed. A caller reads the same limit off StoreCapabilities.AtomicBatch instead
// of inferring it from this error.
var ErrBatchNotAtomic = errors.New("memory: this engine lands one key per call and cannot commit a batch whole")

// keyNamespaceOf returns the key namespace of a key: the text before its first
// colon, which one segment partition or one fact family shares, and "global" for
// a key that carries no namespace.
func keyNamespaceOf(key string) string {
	if at := strings.IndexByte(key, ':'); at > 0 {
		return key[:at]
	}
	return "global"
}

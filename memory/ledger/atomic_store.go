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

// ErrCommitUncertain reports a commit whose durable outcome the engine cannot
// name: the write or its barrier did not return success and the engine could not
// confirm what the store holds, so the caller re-reads instead of reporting
// either outcome.
var ErrCommitUncertain = errors.New("memory: commit outcome uncertain")

// keyNamespaceOf returns the key namespace of a key: the text before its first
// colon, which one segment partition or one fact family shares, and "global" for
// a key that carries no namespace.
func keyNamespaceOf(key string) string {
	if at := strings.IndexByte(key, ':'); at > 0 {
		return key[:at]
	}
	return "global"
}

// The checks of the original store every durable engine holds: a shared-body record written by an earlier format is refused by name, and the stored state loads on first access rather than at open.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestOriginalStoreRefusesLegacySharedBodies pins a clean format boundary.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOriginalStoreRefusesLegacySharedBodies(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	body := []byte("shared in the old format")
	digest := digestOf(body)
	stored, err := json.Marshal([]storedRef{{Format: "text", StoredAt: time.Now(), Digest: digest, Length: int64(len(body))}})
	if err != nil {
		t.Fatalf("encode the legacy reference: %v", err)
	}
	if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey("legacy-event"), Value: stored}, {Key: "blob:" + digest, Value: body}}}); err != nil {
		t.Fatalf("seed the legacy shared body: %v", err)
	}
	reopened, err := newKVOriginalStore(backing, nil)
	if err != nil {
		t.Fatalf("open the legacy format lazily: %v", err)
	}
	if got := reopened.Get(ctx, "legacy-event"); got.Outcome != RecallFailed || !errors.Is(got.Err, ErrSnapshotCorrupt) {
		t.Fatalf("the first legacy shared-body read = %+v, want a named refusal", got)
	}
}

// countingKV counts original-store reads without changing storage semantics.
type countingKV struct {
	inner AtomicStore
	reads int
}

func (k *countingKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	k.reads++
	return k.inner.Read(ctx, fetch)
}
func (k *countingKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	return k.inner.Keys(ctx, prefix)
}
func (k *countingKV) Namespaces(ctx context.Context) ([]string, error) {
	return k.inner.Namespaces(ctx)
}
func (k *countingKV) Commit(ctx context.Context, batch Batch) error {
	return k.inner.Commit(ctx, batch)
}

// TestKVOriginalStoreDefersLoadUntilFirstRead pins zero original reads at open.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestKVOriginalStoreDefersLoadUntilFirstRead(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	first, err := newKVOriginalStore(backing, nil)
	if err != nil {
		t.Fatalf("open the seed original store: %v", err)
	}
	ref := OriginalRef{Key: "lazy-original", Format: "text"}
	body := []byte("the original text")
	if err := first.Put(ctx, ref, body); err != nil {
		t.Fatalf("commit the original: %v", err)
	}
	counted := &countingKV{inner: backing}
	reopened, err := newKVOriginalStore(counted, nil)
	if err != nil {
		t.Fatalf("open the lazy original store: %v", err)
	}
	if counted.reads != 0 {
		t.Fatalf("opening the original store performed %d reads, want zero", counted.reads)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || !bytes.Equal(got.Body, body) || got.Err != nil {
		t.Fatalf("the first lazy read = %+v, want the committed original", got)
	}
	if counted.reads == 0 {
		t.Fatal("the first read did not load the original state")
	}
}

// TestKVOriginalStoreLazyLoadRetriesAfterRepair pins retryable first-read failure.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestKVOriginalStoreLazyLoadRetriesAfterRepair(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	first, _ := newKVOriginalStore(backing, nil)
	ref := OriginalRef{Key: "retry-original", Format: "text"}
	if err := first.Put(ctx, ref, []byte("repaired")); err != nil {
		t.Fatalf("commit the original: %v", err)
	}
	pairs, err := backing.Read(ctx, Fetch{Key: refKVKey(ref.Key)})
	if err != nil || len(pairs) != 1 {
		t.Fatalf("read the valid reference fixture: %v", err)
	}
	valid := append([]byte(nil), pairs[0].Value...)
	if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey(ref.Key), Value: []byte("not-json")}}}); err != nil {
		t.Fatalf("corrupt the reference: %v", err)
	}
	reopened, err := newKVOriginalStore(backing, nil)
	if err != nil {
		t.Fatalf("open still loads eagerly: %v", err)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFailed || !errors.Is(got.Err, ErrSnapshotCorrupt) {
		t.Fatalf("the first corrupt read = %+v, want the named failure", got)
	}
	if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey(ref.Key), Value: valid}}}); err != nil {
		t.Fatalf("repair the reference: %v", err)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || string(got.Body) != "repaired" || got.Err != nil {
		t.Fatalf("the read after repair = %+v, want a retry that finds the original", got)
	}
}

// filteredKV records the fetches one original-store load makes, so a load that
// reads the whole store is observable as the unfiltered read it is.
type filteredKV struct {
	inner      AtomicStore
	filtered   []Fetch
	unfiltered int
}

func (k *filteredKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	if fetch.Key == "" && fetch.Prefix == "" {
		k.unfiltered++
	} else {
		k.filtered = append(k.filtered, fetch)
	}
	return k.inner.Read(ctx, fetch)
}
func (k *filteredKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	return k.inner.Keys(ctx, prefix)
}
func (k *filteredKV) Namespaces(ctx context.Context) ([]string, error) {
	return k.inner.Namespaces(ctx)
}
func (k *filteredKV) Commit(ctx context.Context, batch Batch) error {
	return k.inner.Commit(ctx, batch)
}

// TestKVOriginalStoreLoadReadsItsOwnEntries pins the read scope of one load.
//
// - The load reads the body and reference entries it files, never the whole store
//   - the durable tier shares its keyspace with the segment store, whose event bodies the load never reads
func TestKVOriginalStoreLoadReadsItsOwnEntries(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	first, _ := newKVOriginalStore(backing, nil)
	ref := OriginalRef{Key: "scoped-original", Format: "text"}
	body := []byte("the original text")
	if err := first.Put(ctx, ref, body); err != nil {
		t.Fatalf("commit the original: %v", err)
	}
	events := make([]Pair, 0, 64)
	for at := 0; at < 64; at++ {
		events = append(events, Pair{Key: fmt.Sprintf("0:evt:17000000%02d:%d", at, at), Value: []byte(fmt.Sprintf("body %d", at))})
	}
	if err := backing.Commit(ctx, Batch{Puts: events}); err != nil {
		t.Fatalf("seed the segment events: %v", err)
	}
	filtered := &filteredKV{inner: backing}
	reopened, err := newKVOriginalStore(filtered, nil)
	if err != nil {
		t.Fatalf("open the original store: %v", err)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || !bytes.Equal(got.Body, body) {
		t.Fatalf("the lazy read = %+v, want the committed original", got)
	}
	if filtered.unfiltered != 0 {
		t.Fatalf("the load read the whole store %d time(s), want the two entries it files", filtered.unfiltered)
	}
	prefixes := make([]string, 0, len(filtered.filtered))
	for _, fetch := range filtered.filtered {
		prefixes = append(prefixes, fetch.Prefix)
	}
	sort.Strings(prefixes)
	if got, want := strings.Join(prefixes, ","), "blob:,ref:"; got != want {
		t.Fatalf("the load read under %q, want %q", got, want)
	}
}

// orderedStore records the commits a store makes, in order, so a check can name
// the one commit that carried both the body and the reference of an original.
type orderedStore struct {
	inner AtomicStore
	mu    sync.Mutex
	// carried lists the keys of each commit, in commit order.
	carried [][]string
}

// Read passes the read to the wrapped engine.
func (s *orderedStore) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	return s.inner.Read(ctx, fetch)
}

// Keys enumerates the wrapped engine's keys.
func (s *orderedStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	return s.inner.Keys(ctx, prefix)
}

// Namespaces enumerates the wrapped engine's namespaces.
func (s *orderedStore) Namespaces(ctx context.Context) ([]string, error) {
	return s.inner.Namespaces(ctx)
}

// Commit records the keys of the batch and then commits it.
func (s *orderedStore) Commit(ctx context.Context, batch Batch) error {
	keys := make([]string, 0, len(batch.Puts)+len(batch.Deletes))
	for _, put := range batch.Puts {
		keys = append(keys, put.Key)
	}
	keys = append(keys, batch.Deletes...)
	s.mu.Lock()
	s.carried = append(s.carried, keys)
	s.mu.Unlock()
	return s.inner.Commit(ctx, batch)
}

// carriesOne reports whether one commit wrote a key carrying the prefix.
func carriesOne(keys []string, prefix string) bool {
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// TestOriginalStoreCommitsBodyAndReferenceInOneBatch pins the commit unit of one original.
//
//   - one Put commits the body and the reference that names it in the same batch, so no reader ever reaches a reference whose body is not stored beside it
//   - a commit that fails leaves neither behind: no body no reference names, and no reference naming a missing body
//   - the original reads back after the process reopens the store
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOriginalStoreCommitsBodyAndReferenceInOneBatch(t *testing.T) {
	ctx := context.Background()
	for _, engine := range contractEngines() {
		t.Run(engine.name, func(t *testing.T) {
			t.Run("one_commit_carries_body_and_reference", func(t *testing.T) {
				root := t.TempDir()
				ordered := &orderedStore{inner: engine.openAt(t, root)}
				store, err := newKVOriginalStore(ordered, nil)
				if err != nil {
					t.Fatalf("open the original store: %v", err)
				}
				ref := OriginalRef{Key: "single-batch", Owner: event.Address{AgentID: "agent-a", SessionID: "session-a"}, Format: "text"}
				body := []byte("the original text")
				if err := store.Put(ctx, ref, body); err != nil {
					t.Fatalf("commit the original: %v", err)
				}
				if carried := len(ordered.carried); carried != 1 {
					t.Fatalf("the Put made %d commit(s), want the one batch carrying the body and its reference", carried)
				}
				if keys := ordered.carried[0]; !carriesOne(keys, "blob:") || !carriesOne(keys, "ref:") {
					t.Fatalf("the one commit carried %v, want the body beside the reference that names it", keys)
				}
				stored, held := readOne(t, ordered, blobKVKey(ref.Key))
				if !held || !bytes.Equal(stored, body) {
					t.Fatalf("the stored body = (%q, %v), want the committed bytes", stored, held)
				}
				reopened, err := newKVOriginalStore(engine.openAt(t, root), nil)
				if err != nil {
					t.Fatalf("reopen the original store: %v", err)
				}
				if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || !bytes.Equal(got.Body, body) {
					t.Fatalf("the reopened read = %+v, want the committed original", got)
				}
			})

			t.Run("failed_commit_leaves_neither_body_nor_reference", func(t *testing.T) {
				root := t.TempDir()
				faults := &faultStore{inner: engine.openAt(t, root)}
				store, err := newKVOriginalStore(faults, nil)
				if err != nil {
					t.Fatalf("open the original store: %v", err)
				}
				ref := OriginalRef{Key: "single-batch-failed", Format: "text"}
				body := []byte("the body whose reference never landed")
				faults.setCommitFault(func(batch Batch) error {
					for _, put := range batch.Puts {
						if strings.HasPrefix(put.Key, "ref:") {
							return fmt.Errorf("%w: injected on the commit that carries the reference", ErrCommitUncertain)
						}
					}
					return nil
				})
				if err := store.Put(ctx, ref, body); !errors.Is(err, ErrCommitBarrier) {
					t.Fatalf("the original whose commit failed returned %v, want the named commit barrier", err)
				}
				if stored, held := readOne(t, faults, blobKVKey(ref.Key)); held {
					t.Fatalf("the body of the failed commit is stored as %q, want nothing beside a reference that never landed", stored)
				}
				if got := store.Get(ctx, ref.Key); got.Outcome != RecallMissing {
					t.Fatalf("the original beside no reference reads %+v, want it unreadable", got)
				}
				faults.setCommitFault(nil)
				reopened, err := newKVOriginalStore(engine.openAt(t, root), nil)
				if err != nil {
					t.Fatalf("reopen the original store: %v", err)
				}
				if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallMissing {
					t.Fatalf("a reopened process reads the failed commit as %+v, want it unreadable", got)
				}
			})
		})
	}
}

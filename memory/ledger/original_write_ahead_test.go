// The write-ahead boundary of one original: the body lands before the reference that names it, so a reader never sees a reference whose body is not durable, and a body whose reference commit failed is an orphan the store collects.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// orderedStore records the commits a store makes, in order, so a check can name
// which one carried the body and which one carried the reference.
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

// commitsCarrying returns the position of every commit that wrote a key carrying
// the prefix, in commit order.
func (s *orderedStore) commitsCarrying(prefix string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var positions []int
	for at, keys := range s.carried {
		for _, key := range keys {
			if strings.HasPrefix(key, prefix) {
				positions = append(positions, at)
				break
			}
		}
	}
	return positions
}

// TestBodyWriteAheadAtomicFact pins the write-ahead order of one original.
//
//   - the body of an original reaches the store before the reference that names it
//   - a reader sees the original only once both are committed
//   - a reference commit that fails leaves the body durable and the original unreadable, never a reference whose body is missing
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestBodyWriteAheadAtomicFact(t *testing.T) {
	ctx := context.Background()
	for _, engine := range contractEngines() {
		t.Run(engine.name, func(t *testing.T) {
			t.Run("body_lands_before_reference", func(t *testing.T) {
				root := t.TempDir()
				ordered := &orderedStore{inner: engine.openAt(t, root)}
				store, err := newKVOriginalStore(ordered, nil)
				if err != nil {
					t.Fatalf("open the original store: %v", err)
				}
				ref := OriginalRef{Key: "write-ahead", Owner: event.Address{AgentID: "agent-a", SessionID: "session-a"}, Format: "text"}
				body := []byte("the original text")
				if err := store.Put(ctx, ref, body); err != nil {
					t.Fatalf("commit the original: %v", err)
				}
				bodies := ordered.commitsCarrying("blob:")
				references := ordered.commitsCarrying("ref:")
				if len(bodies) != 1 || len(references) != 1 {
					t.Fatalf("commits carrying body=%v reference=%v, want one of each", bodies, references)
				}
				if bodies[0] >= references[0] {
					t.Fatalf("the reference commit is at %d and the body commit at %d, want the body first", references[0], bodies[0])
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

			t.Run("reference_commit_failure_leaves_body_only", func(t *testing.T) {
				root := t.TempDir()
				faults := &faultStore{inner: engine.openAt(t, root)}
				store, err := newKVOriginalStore(faults, nil)
				if err != nil {
					t.Fatalf("open the original store: %v", err)
				}
				ref := OriginalRef{Key: "write-ahead-failed", Format: "text"}
				body := []byte("the body whose reference never landed")
				faults.setCommitFault(func(batch Batch) error {
					for _, put := range batch.Puts {
						if strings.HasPrefix(put.Key, "ref:") {
							return fmt.Errorf("%w: injected on the reference commit", ErrCommitUncertain)
						}
					}
					return nil
				})
				if err := store.Put(ctx, ref, body); !errors.Is(err, ErrCommitBarrier) {
					t.Fatalf("the original whose reference commit failed returned %v, want the named commit barrier", err)
				}
				stored, held := readOne(t, faults, blobKVKey(ref.Key))
				if !held || !bytes.Equal(stored, body) {
					t.Fatalf("the body of the failed reference = (%q, %v), want the durable bytes written ahead", stored, held)
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
					t.Fatalf("a reopened process reads the orphan as %+v, want it unreadable", got)
				}
			})
		})
	}
}

// TestOrphanBodyGC pins the collection of the bodies no reference names.
//
//   - a body whose reference commit failed is collected, and a body a readable reference still names stays
//   - an orphan is never served as a recall, before or after the collection
//   - a collection beside no orphan removes nothing
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOrphanBodyGC(t *testing.T) {
	ctx := context.Background()
	for _, engine := range contractEngines() {
		t.Run(engine.name, func(t *testing.T) {
			root := t.TempDir()
			faults := &faultStore{inner: engine.openAt(t, root)}
			store, err := newKVOriginalStore(faults, nil)
			if err != nil {
				t.Fatalf("open the original store: %v", err)
			}
			kept := OriginalRef{Key: "kept-original", Format: "text"}
			keptBody := []byte("the original with a live reference")
			if err := store.Put(ctx, kept, keptBody); err != nil {
				t.Fatalf("commit the kept original: %v", err)
			}
			orphan := OriginalRef{Key: "orphan-original", Format: "text"}
			faults.setCommitFault(func(batch Batch) error {
				for _, put := range batch.Puts {
					if strings.HasPrefix(put.Key, "ref:") {
						return fmt.Errorf("%w: injected on the reference commit", ErrCommitUncertain)
					}
				}
				return nil
			})
			if err := store.Put(ctx, orphan, []byte("the body no reference names")); err == nil {
				t.Fatal("the original whose reference commit failed reported success")
			}
			faults.setCommitFault(nil)
			if got := store.Get(ctx, orphan.Key); got.Outcome != RecallMissing {
				t.Fatalf("the orphan reads %+v before the collection, want it unreadable", got)
			}
			collected, err := store.CollectOrphanBodies(ctx)
			if err != nil {
				t.Fatalf("collect the orphan bodies: %v", err)
			}
			if collected != 1 {
				t.Fatalf("the collection removed %d bodies, want the one orphan", collected)
			}
			if _, held := readOne(t, faults, blobKVKey(orphan.Key)); held {
				t.Fatal("the collected orphan body is still stored")
			}
			if _, held := readOne(t, faults, blobKVKey(kept.Key)); !held {
				t.Fatal("the collection removed a body a live reference names")
			}
			if got := store.Get(ctx, kept.Key); got.Outcome != RecallFound || !bytes.Equal(got.Body, keptBody) {
				t.Fatalf("the kept original reads %+v after the collection, want the committed bytes", got)
			}
			if got := store.Get(ctx, orphan.Key); got.Outcome != RecallMissing {
				t.Fatalf("the orphan reads %+v after the collection, want it unreadable", got)
			}
			again, err := store.CollectOrphanBodies(ctx)
			if err != nil {
				t.Fatalf("collect beside no orphan: %v", err)
			}
			if again != 0 {
				t.Fatalf("a collection beside no orphan removed %d bodies, want none", again)
			}
		})
	}
}

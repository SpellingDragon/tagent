// The contract every storage engine meets behind the one atomic store seam: the
// same body runs over every tier that opens it, so a caller and these checks
// never name a file layout, and a failure injected at the seam fails the same way
// over each.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// engineUnderTest names one engine the contract runs over: how to open it over a
// root, and how to open a second view of the same root, which is the reopen a
// process restart performs.
type engineUnderTest struct {
	name string
	cfg  StorageConfig
}

// contractEngines are the tiers the contract runs over: the durable engine under
// its own name, and the retired name that now opens the same engine, so a
// deployment that still selects it keeps the guarantees below.
func contractEngines() []engineUnderTest {
	return []engineUnderTest{
		{name: "journal", cfg: StorageConfig{Type: StorageJournal}},
		{name: "retired_name", cfg: StorageConfig{Type: StorageLocalFile}},
	}
}

// openAt opens the engine over root.
func (e engineUnderTest) openAt(t *testing.T, root string) AtomicStore {
	t.Helper()
	cfg := e.cfg
	cfg.Root = root
	store, err := OpenAtomicStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open the %s engine over %s: %v", e.name, root, err)
	}
	return store
}

// faultStore is a port decorator: it wraps an engine and fails the commits and
// reads the check names, so the injection works over any engine and never names
// a file layout.
type faultStore struct {
	inner AtomicStore
	mu    sync.Mutex
	// commitFault answers the failure of one commit, or nil to let it through.
	commitFault func(batch Batch) error
	// readFault answers the failure of one read, or nil to let it through.
	readFault func(fetch Fetch) error
	// commits counts the commits that reached the wrapped engine.
	commits int
}

// Read passes the read through the injected fault to the wrapped engine.
func (s *faultStore) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	s.mu.Lock()
	fault := s.readFault
	s.mu.Unlock()
	if fault != nil {
		if err := fault(fetch); err != nil {
			return nil, err
		}
	}
	return s.inner.Read(ctx, fetch)
}

// Keys enumerates the wrapped engine's keys.
func (s *faultStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	return s.inner.Keys(ctx, prefix)
}

// Namespaces enumerates the wrapped engine's namespaces.
func (s *faultStore) Namespaces(ctx context.Context) ([]string, error) {
	return s.inner.Namespaces(ctx)
}

// Commit passes the batch through the injected fault to the wrapped engine.
func (s *faultStore) Commit(ctx context.Context, batch Batch) error {
	s.mu.Lock()
	fault := s.commitFault
	s.mu.Unlock()
	if fault != nil {
		if err := fault(batch); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.commits++
	s.mu.Unlock()
	return s.inner.Commit(ctx, batch)
}

// setCommitFault replaces the commit fault and returns the previous one, so a
// check can arm a failure for one phase and disarm it afterwards.
func (s *faultStore) setCommitFault(fault func(batch Batch) error) func(batch Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.commitFault
	s.commitFault = fault
	return previous
}

// readOne reads the one value held under key and reports whether it is held.
func readOne(t *testing.T, store AtomicStore, key string) ([]byte, bool) {
	t.Helper()
	pairs, err := store.Read(context.Background(), Fetch{Key: key})
	if err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	if len(pairs) == 0 {
		return nil, false
	}
	return pairs[0].Value, true
}

// TestAtomicStoreContract pins the guarantee every engine meets behind the atomic store seam, over each engine in turn.
//   - a committed batch reads back exactly, under its key, its prefix and its namespace
//   - a deleted key answers nothing, and a reopened view answers neither the deleted key nor anything the failed commit carried
//   - a commit the seam refused publishes no part of its batch, and no later commit carries it into a read
//   - an empty commit completes the barrier alone
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAtomicStoreContract(t *testing.T) {
	ctx := context.Background()
	for _, engine := range contractEngines() {
		t.Run(engine.name, func(t *testing.T) {
			t.Run("commit_reads_back", func(t *testing.T) {
				root := t.TempDir()
				store := engine.openAt(t, root)
				batch := Batch{Puts: []Pair{
					{Key: "0:evt:1:1", Value: []byte("first")},
					{Key: "0:idx:alpha", Value: []byte("1:1")},
					{Key: "ref:alpha", Value: []byte("reference")},
				}}
				if err := store.Commit(ctx, batch); err != nil {
					t.Fatalf("commit the batch: %v", err)
				}
				value, held := readOne(t, store, "0:evt:1:1")
				if !held || !bytes.Equal(value, []byte("first")) {
					t.Fatalf("read of the committed key = (%q, %v), want the committed bytes", value, held)
				}
				pairs, err := store.Read(ctx, Fetch{Prefix: "0:"})
				if err != nil {
					t.Fatalf("read the namespace prefix: %v", err)
				}
				if len(pairs) != 2 || pairs[0].Key != "0:evt:1:1" || pairs[1].Key != "0:idx:alpha" {
					t.Fatalf("prefix read = %+v, want the two committed entries in key order", pairs)
				}
				keys, err := store.Keys(ctx, "ref:")
				if err != nil {
					t.Fatalf("enumerate the prefix keys: %v", err)
				}
				if len(keys) != 1 || keys[0] != "ref:alpha" {
					t.Fatalf("prefix keys = %v, want the one committed key", keys)
				}
				namespaces, err := store.Namespaces(ctx)
				if err != nil {
					t.Fatalf("enumerate the namespaces: %v", err)
				}
				if len(namespaces) != 2 || namespaces[0] != "0" || namespaces[1] != "ref" {
					t.Fatalf("namespaces = %v, want the two key namespaces in name order", namespaces)
				}
			})

			t.Run("delete_and_reopen", func(t *testing.T) {
				root := t.TempDir()
				store := engine.openAt(t, root)
				if err := store.Commit(ctx, Batch{Puts: []Pair{
					{Key: "0:evt:1:1", Value: []byte("first")},
					{Key: "0:evt:1:2", Value: []byte("second")},
				}}); err != nil {
					t.Fatalf("commit the batch: %v", err)
				}
				if err := store.Commit(ctx, Batch{Deletes: []string{"0:evt:1:2"}}); err != nil {
					t.Fatalf("commit the delete: %v", err)
				}
				if _, held := readOne(t, store, "0:evt:1:2"); held {
					t.Fatal("the deleted key still reads in the committing view")
				}
				reopened := engine.openAt(t, root)
				value, held := readOne(t, reopened, "0:evt:1:1")
				if !held || !bytes.Equal(value, []byte("first")) {
					t.Fatalf("the reopened view read (%q, %v), want the committed bytes", value, held)
				}
				if _, held := readOne(t, reopened, "0:evt:1:2"); held {
					t.Fatal("the reopened view reads the deleted key")
				}
			})

			t.Run("refused_commit_publishes_nothing", func(t *testing.T) {
				root := t.TempDir()
				faults := &faultStore{inner: engine.openAt(t, root)}
				if err := faults.Commit(ctx, Batch{Puts: []Pair{{Key: "0:evt:1:1", Value: []byte("landed")}}}); err != nil {
					t.Fatalf("commit the landed value: %v", err)
				}
				faults.setCommitFault(func(batch Batch) error {
					return fmt.Errorf("%w: injected before the %s engine saw the batch", ErrCommitUncertain, engine.name)
				})
				refused := faults.Commit(ctx, Batch{Puts: []Pair{
					{Key: "0:evt:1:1", Value: []byte("unlanded")},
					{Key: "0:evt:1:2", Value: []byte("unlanded-second")},
				}})
				if !errors.Is(refused, ErrCommitUncertain) {
					t.Fatalf("the refused commit returned %v, want the named uncertain outcome", refused)
				}
				value, held := readOne(t, faults, "0:evt:1:1")
				if !held || !bytes.Equal(value, []byte("landed")) {
					t.Fatalf("the refused commit served (%q, %v), want the landed value", value, held)
				}
				if _, held := readOne(t, faults, "0:evt:1:2"); held {
					t.Fatal("the refused commit published a key of its batch")
				}
				faults.setCommitFault(nil)
				if err := faults.Commit(ctx, Batch{Puts: []Pair{{Key: "0:evt:1:3", Value: []byte("later")}}}); err != nil {
					t.Fatalf("commit after the refusal: %v", err)
				}
				reopened := engine.openAt(t, root)
				if _, held := readOne(t, reopened, "0:evt:1:2"); held {
					t.Fatal("a later commit carried the refused bytes into a read")
				}
				if value, held := readOne(t, reopened, "0:evt:1:3"); !held || !bytes.Equal(value, []byte("later")) {
					t.Fatalf("the reopened view read (%q, %v), want the value committed after the refusal", value, held)
				}
			})

			t.Run("empty_value_put_survives_reopen", func(t *testing.T) {
				root := t.TempDir()
				store := engine.openAt(t, root)
				if err := store.Commit(ctx, Batch{Puts: []Pair{
					{Key: "0:evt:1:1", Value: nil},
					{Key: "0:evt:1:2", Value: []byte{}},
					{Key: "0:evt:1:3", Value: []byte("held")},
				}}); err != nil {
					t.Fatalf("commit the empty-value puts: %v", err)
				}
				reopened := engine.openAt(t, root)
				for _, key := range []string{"0:evt:1:1", "0:evt:1:2"} {
					value, held := readOne(t, reopened, key)
					if !held {
						t.Fatalf("the reopened view answers no value for the empty-value put %q: a put is not a delete", key)
					}
					if len(value) != 0 {
						t.Fatalf("the reopened view read %q for the empty-value put %q, want no bytes", value, key)
					}
				}
				if err := store.Commit(ctx, Batch{Deletes: []string{"0:evt:1:1"}}); err != nil {
					t.Fatalf("commit the delete of the empty-value key: %v", err)
				}
				if _, held := readOne(t, engine.openAt(t, root), "0:evt:1:1"); held {
					t.Fatal("the deleted empty-value key still reads after the delete")
				}
			})

			t.Run("barrier_commits_nothing", func(t *testing.T) {
				root := t.TempDir()
				store := engine.openAt(t, root)
				if err := store.Commit(ctx, Batch{}); err != nil {
					t.Fatalf("complete the barrier: %v", err)
				}
				if pairs, err := store.Read(ctx, Fetch{}); err != nil || len(pairs) != 0 {
					t.Fatalf("the barrier left (%d pairs, %v), want an empty store", len(pairs), err)
				}
			})
		})
	}
}

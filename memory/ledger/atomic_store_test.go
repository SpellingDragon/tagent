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
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
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
		{name: "journal", cfg: StorageConfig{Type: StorageJournal, WriteRights: true}},
		{name: "retired_name", cfg: StorageConfig{Type: StorageLocalFile, WriteRights: true}},
	}
}

// openAt opens the engine over root as the view that declares the root's write
// rights, which is the view a process commits through.
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

// openReaderAt opens a second view of root beside the writer that owns it, which is
// the view a reader holds while the writer keeps committing.
func (e engineUnderTest) openReaderAt(t *testing.T, root string) AtomicStore {
	t.Helper()
	cfg := e.cfg
	cfg.Root = root
	cfg.WriteRights = false
	store, err := OpenAtomicStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open a reading view of the %s engine over %s: %v", e.name, root, err)
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
				reopened := engine.openReaderAt(t, root)
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
				reopened := engine.openReaderAt(t, root)
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
				reopened := engine.openReaderAt(t, root)
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
				if _, held := readOne(t, engine.openReaderAt(t, root), "0:evt:1:1"); held {
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

// capabilityEngine names one engine the capability check opens: the tier config it
// opens over a root, with the stub CLI's state directory filled in for the file tier.
type capabilityEngine struct {
	name string
	cfg  func(root, cli string) StorageConfig
}

// capabilityEngines are the engines the capability check runs over: every tier the
// build opens, the file tier through a stub CLI so the check runs without the real one.
func capabilityEngines() []capabilityEngine {
	return []capabilityEngine{
		{name: "journal", cfg: func(root, _ string) StorageConfig {
			return StorageConfig{Type: StorageJournal, Root: root, WriteRights: true}
		}},
		{name: "retired_name", cfg: func(root, _ string) StorageConfig {
			return StorageConfig{Type: StorageLocalFile, Root: root, WriteRights: true}
		}},
		{name: "memory", cfg: func(_, _ string) StorageConfig {
			return StorageConfig{Type: StorageMemory}
		}},
		{name: "file", cfg: func(root, cli string) StorageConfig {
			cfg := StorageConfig{Type: StorageFile, Root: root}
			cfg.RustVikingBinary = rustVikingStubPath(cli)
			cfg.RustVikingConfig = cli
			return cfg
		}},
	}
}

// TestEveryEngineCommitsABatchWholeOrNamesTheCapabilityItLacks holds every opened tier to its AtomicBatch declaration.
//   - a tier declaring AtomicBatch lands every key of a three-key batch, and a durable one reads them back after a reopen
//   - a tier declaring no AtomicBatch answers ErrBatchNotAtomic and lands no key of the batch, in this view and in a reopened one
//
// 契约: docs/wiki/memory/memory-architecture.md#rv-cli
func TestEveryEngineCommitsABatchWholeOrNamesTheCapabilityItLacks(t *testing.T) {
	ctx := context.Background()
	pairs := []Pair{
		{Key: "0:evt:1:1", Value: []byte("first")},
		{Key: "0:idx:alpha", Value: []byte("1:1")},
		{Key: "ref:alpha", Value: []byte("reference")},
	}
	for _, engine := range capabilityEngines() {
		t.Run(engine.name, func(t *testing.T) {
			root, cli := t.TempDir(), t.TempDir()
			writeRustVikingStub(t, cli)
			cfg := engine.cfg(root, cli)
			tier, err := OpenTier(ctx, cfg)
			if err != nil {
				t.Fatalf("open the %s tier: %v", engine.name, err)
			}
			committed := tier.Store.Commit(ctx, Batch{Puts: pairs})
			if tier.Capabilities.AtomicBatch != (committed == nil) {
				t.Fatalf("the %s tier declares AtomicBatch=%v and answered %v for a three-key batch, want the declaration to name what the engine did", engine.name, tier.Capabilities.AtomicBatch, committed)
			}
			if committed != nil {
				if !errors.Is(committed, ErrBatchNotAtomic) {
					t.Fatalf("the refused three-key batch answered %v, want the capability the tier lacks named", committed)
				}
				assertNoKeyHeld(t, tier.Store, pairs)
				if tier.Capabilities.Durable {
					assertNoKeyHeld(t, reopenTier(t, cfg).Store, pairs)
				}
				return
			}
			assertKeysHeld(t, tier.Store, pairs)
			if tier.Capabilities.Durable {
				assertKeysHeld(t, reopenTier(t, cfg).Store, pairs)
			}
		})
	}
}

// TestASecondKeyFailureLeavesNoFirstKeyPublished commits two keys to the file engine whose CLI stub fails its second mutation.
//   - the batch answers ErrBatchNotAtomic, so the store never meets the second-key failure the stub would have produced
//   - the engine ran no CLI mutation, so the first key of the batch landed nowhere
//   - neither the committing view nor a reopened one answers a key of the batch
//
// 契约: docs/wiki/memory/memory-architecture.md#rv-cli
func TestASecondKeyFailureLeavesNoFirstKeyPublished(t *testing.T) {
	ctx := context.Background()
	root, cli := t.TempDir(), t.TempDir()
	writeRustVikingStub(t, cli)
	if err := os.WriteFile(filepath.Join(cli, "fail-at"), []byte("2\n"), 0o600); err != nil {
		t.Fatalf("arm the stub to fail its second mutation: %v", err)
	}
	cfg := StorageConfig{Type: StorageFile, Root: root}
	cfg.RustVikingBinary = rustVikingStubPath(cli)
	cfg.RustVikingConfig = cli
	store, err := OpenAtomicStore(ctx, cfg)
	if err != nil {
		t.Fatalf("open the file engine: %v", err)
	}
	pairs := []Pair{
		{Key: "0:evt:1:1", Value: []byte("first")},
		{Key: "0:evt:1:2", Value: []byte("second")},
	}
	refused := store.Commit(ctx, Batch{Puts: pairs})
	assertNoKeyHeld(t, store, pairs)
	assertNoKeyHeld(t, reopenStore(t, cfg), pairs)
	if ran := rustVikingMutations(t, cli); ran != 0 {
		t.Fatalf("the engine ran %d CLI mutation(s) of a batch it cannot commit whole, want none", ran)
	}
	if !errors.Is(refused, ErrBatchNotAtomic) {
		t.Fatalf("the two-key commit answered %v, want the capability the engine lacks named", refused)
	}
}

// TestTheFileTierNamesTheFactLayerItCannotCarry commits one fact through the two stores of the file tier over a stub CLI.
//   - the original store refuses the fact's two-key batch and the segment store refuses its three-key batch, each naming ErrBatchNotAtomic
//   - the CLI ran no mutation and neither store landed a key, so the tier fails at its first fact instead of half-committing one
//
// 契约: docs/wiki/memory/memory-architecture.md#rv-cli
func TestTheFileTierNamesTheFactLayerItCannotCarry(t *testing.T) {
	ctx := context.Background()
	root, cli := t.TempDir(), t.TempDir()
	writeRustVikingStub(t, cli)
	cfg := StorageConfig{Type: StorageFile, Root: root}
	cfg.RustVikingBinary = rustVikingStubPath(cli)
	cfg.RustVikingConfig = cli
	tier, err := OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the file tier: %v", err)
	}
	ref := OriginalRef{Key: "f1f1", Owner: event.Address{AgentID: "writer", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
	if err := tier.Originals.Put(ctx, ref, []byte("first")); !errors.Is(err, ErrBatchNotAtomic) {
		t.Fatalf("Put(f1f1) = %v, want the capability the tier lacks named", err)
	}
	rec := SegmentRecord{PartitionID: 2, WindowTS: 200, Seq: 1, EventKey: "f1f1", EventTime: 5, Body: []byte(`{"fact":"first"}`)}
	if err := tier.Segments.StoreEvent(ctx, rec); !errors.Is(err, ErrBatchNotAtomic) {
		t.Fatalf("StoreEvent(f1f1) = %v, want the capability the tier lacks named", err)
	}
	if ran := rustVikingMutations(t, cli); ran != 0 {
		t.Fatalf("the tier ran %d CLI mutation(s) of batches it cannot commit whole, want none", ran)
	}
	if got := tier.Originals.Get(ctx, ref.Key); got.Outcome != RecallMissing {
		t.Fatalf("Get(f1f1) = outcome %v, want missing", got.Outcome)
	}
	if n := tier.Segments.EventCount(); n != 0 {
		t.Fatalf("EventCount() = %d after a refused fact, want none published", n)
	}
}

// assertNoKeyHeld fails when a store answers a key of a batch it refused.
func assertNoKeyHeld(t *testing.T, store AtomicStore, pairs []Pair) {
	t.Helper()
	for _, pair := range pairs {
		if value, held := readOne(t, store, pair.Key); held {
			t.Fatalf("the refused batch published %q = %q, want no key of it", pair.Key, value)
		}
	}
}

// assertKeysHeld fails when a store answers anything but the bytes a commit carried.
func assertKeysHeld(t *testing.T, store AtomicStore, pairs []Pair) {
	t.Helper()
	for _, pair := range pairs {
		value, held := readOne(t, store, pair.Key)
		if !held || !bytes.Equal(value, pair.Value) {
			t.Fatalf("the committed batch read %q = (%q, %v), want the bytes it carried", pair.Key, value, held)
		}
	}
}

// reopenTier opens a second tier over the same config, which is the reopen a
// process restart performs.
// reopenTier opens a second view of the same root beside the tier already open,
// which is the view a reader holds while the writer keeps committing.
func reopenTier(t *testing.T, cfg StorageConfig) *Tier {
	t.Helper()
	cfg.WriteRights = false
	tier, err := OpenTier(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reopen the tier: %v", err)
	}
	return tier
}

// reopenStore opens a second engine over the same config.
func reopenStore(t *testing.T, cfg StorageConfig) AtomicStore {
	t.Helper()
	store, err := OpenAtomicStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("reopen the engine: %v", err)
	}
	return store
}

// rustVikingStubPath names the stub CLI the capability checks drive.
func rustVikingStubPath(dir string) string {
	return filepath.Join(dir, "rustviking")
}

// writeRustVikingStub writes the stub rustviking CLI into dir: it keeps one file
// per key holding the value a put carried, answers a get from that file, and fails
// the mutation numbered in its fail-at file so a check can fail the Nth key.
func writeRustVikingStub(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
store="$2"
sub="$4"
key="$6"
case "$sub" in
put)
	n=$(cat "$store/mutations" 2>/dev/null || echo 0)
	n=$((n + 1))
	printf '%s\n' "$n" >> "$store/mutations"
	if [ -f "$store/fail-at" ] && [ "$n" -eq "$(cat "$store/fail-at")" ]; then
		printf 'the stub fails mutation %s\n' "$n" >&2
		exit 1
	fi
	mkdir -p "$store/keys"
	printf '%s' "$8" > "$store/keys/$key"
	printf '{"success":true,"data":null}'
	;;
del)
	n=$(cat "$store/mutations" 2>/dev/null || echo 0)
	n=$((n + 1))
	printf '%s\n' "$n" >> "$store/mutations"
	if [ -f "$store/fail-at" ] && [ "$n" -eq "$(cat "$store/fail-at")" ]; then
		printf 'the stub fails mutation %s\n' "$n" >&2
		exit 1
	fi
	rm -f "$store/keys/$key"
	printf '{"success":true,"data":null}'
	;;
get)
	if [ -f "$store/keys/$key" ]; then
		printf '{"success":true,"data":{"key":"%s","value":"%s"}}' "$key" "$(cat "$store/keys/$key")"
	else
		printf '{"success":true,"data":{"key":"%s","value":null}}' "$key"
	fi
	;;
scan)
	printf '{"success":true,"data":{"prefix":"","count":0,"entries":[]}}'
	;;
*)
	printf 'the stub carries no subcommand %s\n' "$sub" >&2
	exit 1
	;;
esac
`
	if err := os.WriteFile(rustVikingStubPath(dir), []byte(script), 0o700); err != nil {
		t.Fatalf("write the stub CLI: %v", err)
	}
}

// rustVikingMutations counts the mutations the stub CLI ran, so a check tells a
// refused batch from one that reached the CLI.
func rustVikingMutations(t *testing.T, dir string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, "mutations"))
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read the stub CLI's mutation log: %v", err)
	}
	return bytes.Count(content, []byte("\n"))
}

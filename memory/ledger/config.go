package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// StorageType names one storage tier of the original and segment stores.
type StorageType string

const (
	// StorageMemory is the in-process tier: it holds only what the process holds
	// and declares no recovery across process exit.
	StorageMemory StorageType = "memory"
	// StorageLocalFile is the retired name of the durable local tier. The build
	// opens it as the journal, the one durable local engine, and refuses a root
	// that still holds the retired sharded-snapshot layout by name: a deployment
	// moves such a root with MigrateShardedRoot and a caller that names no layout
	// selects StorageJournal. The name is kept because deployments and checks
	// still carry it; renaming it is a separate cleanup, not a second engine.
	StorageLocalFile StorageType = "localfile"
	// StorageJournal is the durable local tier: key values are the records of one
	// append-only journal under a root directory. A commit is one frame, and the
	// log's sync is its only barrier.
	StorageJournal StorageType = "journal"
	// StorageFile is the tier whose key values live in a rustviking store reached
	// through the rustviking CLI.
	StorageFile StorageType = "file"
)

// kvDirName is the directory under a root that holds the key-value store: the
// journal of the tier this build opens, and the shard directories of the retired
// layout it refuses.
const kvDirName = "kv"

// StorageConfig locates one storage tier: the tier name, the root directory the
// persistent tiers persist under, and the rustviking executable and config the
// file tier drives.
type StorageConfig struct {
	// Type names the tier to open.
	Type StorageType
	// Root is the directory the localfile and file tiers persist under.
	Root string
	// RustVikingBinary names or locates the rustviking executable; empty means
	// "rustviking" looked up on PATH.
	RustVikingBinary string
	// RustVikingConfig is the config file passed to the rustviking CLI; empty
	// means the config.toml under Root.
	RustVikingConfig string
	// Engine, when set, is the engine the tier opens over instead of the one Type
	// names. It is the seam a caller wraps to observe, delay or fail commits: the
	// wrapper implements the same AtomicStore, so the injection works over any
	// engine and never names a file layout. A tier beside no wrapper opens the
	// engine Type names.
	Engine AtomicStore
}

// StoreCapabilities names what one opened tier guarantees, so a tier that holds
// nothing across process exit says so instead of appearing durable.
type StoreCapabilities struct {
	// Tier is the tier the capabilities describe.
	Tier StorageType
	// Durable reports whether a successful commit outlives the process that made
	// it.
	Durable bool
}

// Tier is one opened storage tier: the original store and the segment store of
// the same backend, together with the capabilities that backend guarantees and
// the engine both stores commit through.
type Tier struct {
	// Originals commits and recalls original bytes.
	Originals OriginalStore
	// Segments commits events behind the segment commit barrier.
	Segments *FileSegmentStore
	// Store is the engine this tier holds, as the interface every engine
	// implements: the stores above read and commit through it, and a caller never
	// names the engine behind it.
	Store AtomicStore
	// Capabilities names what the backend guarantees.
	Capabilities StoreCapabilities
}

// ErrUnknownTier reports a storage config whose tier name matches no known tier.
var ErrUnknownTier = errors.New("memory: unknown storage tier")

// ErrRootRequired reports a persistent tier opened without the directory it
// persists under.
var ErrRootRequired = errors.New("memory: persistent tiers require a root directory")

// ErrJournalLegacyLayout reports a data directory that holds the retired
// sharded-snapshot layout instead of a journal, so the caller is told to open a
// new directory rather than being handed a half-read store.
var ErrJournalLegacyLayout = errors.New("memory: data directory holds the retired shard layout, open a new directory")

// rejectLegacyShardLayout refuses a journal directory that still holds the
// retired sharded-snapshot layout: a shard directory, or a shard_*.json file.
// The refused directory is left exactly as it was — nothing is migrated, cleaned
// or overwritten, so the build that wrote that layout still reads it.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func rejectLegacyShardLayout(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return fmt.Errorf("%w: %s", ErrJournalLegacyLayout, filepath.Join(dir, entry.Name()))
		}
		if name := entry.Name(); strings.HasPrefix(name, "shard_") && strings.HasSuffix(name, ".json") {
			return fmt.Errorf("%w: %s", ErrJournalLegacyLayout, filepath.Join(dir, name))
		}
	}
	return nil
}

// ErrRustVikingMissing reports a file tier whose rustviking CLI is not executable
// on this machine.
var ErrRustVikingMissing = errors.New("memory: rustviking CLI is not executable")

// OpenAtomicStore opens the engine the config names, without opening a tier over
// it. A caller that wants to observe or fail commits opens the engine here,
// wraps it, and hands the wrapper back through StorageConfig.Engine: the wrapper
// is the same interface, so every engine takes the same injection.
func OpenAtomicStore(ctx context.Context, cfg StorageConfig) (AtomicStore, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch cfg.Type {
	case StorageMemory:
		return newMemKV(), nil
	case StorageJournal, StorageLocalFile:
		if cfg.Root == "" {
			return nil, ErrRootRequired
		}
		return openJournalKV(filepath.Join(cfg.Root, kvDirName))
	case StorageFile:
		if cfg.Root == "" {
			return nil, ErrRootRequired
		}
		binary := cfg.RustVikingBinary
		if binary == "" {
			binary = "rustviking"
		}
		path, err := exec.LookPath(binary)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrRustVikingMissing, binary)
		}
		config := cfg.RustVikingConfig
		if config == "" {
			config = filepath.Join(cfg.Root, "config.toml")
		}
		return newRustVikingKV(path, config), nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownTier, cfg.Type)
}

// OpenTier opens one storage tier over its backend.
//   - The memory tier holds only what the process holds; the durable local tier appends to the journal under Root; the file tier drives the rustviking CLI and reports ErrRustVikingMissing when no such executable is found
//   - Each call opens its own store over the same backend, so a caller reads exactly what an earlier process committed
//   - A config that carries an engine opens the tier over that engine instead, so a caller wraps the engine at the seam and the tier takes the wrapper unchanged
//   - The journal tier's write path is one append-only file with one writer handle per opened view, and each commit is appended as one whole frame, so two views of the same directory cannot interleave half frames
//   - A second runtime instance over one directory is refused by the production open path that owns that root, not by an exclusive lock here: in-process checks of the same root appear in the test suite of this repository
//   - Durability holds without that lock, because a frame is appended whole and the barrier is the log's own sync
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func OpenTier(ctx context.Context, cfg StorageConfig) (*Tier, error) {
	store := cfg.Engine
	if store == nil {
		opened, err := OpenAtomicStore(ctx, cfg)
		if err != nil {
			return nil, err
		}
		store = opened
	}
	if cfg.Type == StorageMemory && cfg.Engine == nil {
		segments, err := newFileSegmentStore(store)
		if err != nil {
			return nil, err
		}
		return &Tier{
			Originals:    NewMemoryOriginalStore(),
			Segments:     segments,
			Store:        store,
			Capabilities: StoreCapabilities{Tier: StorageMemory, Durable: false},
		}, nil
	}
	originals, err := newKVOriginalStore(store, nil)
	if err != nil {
		return nil, err
	}
	segments, err := newFileSegmentStore(store)
	if err != nil {
		return nil, err
	}
	return &Tier{
		Originals:    originals,
		Segments:     segments,
		Store:        store,
		Capabilities: StoreCapabilities{Tier: cfg.Type, Durable: cfg.Type != StorageMemory},
	}, nil
}

// memKV is the memory tier key-value view: it holds every value for the life of
// the process and hands nothing to disk, so a commit completes a barrier over
// state that any later process starts without.
type memKV struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// newMemKV returns an empty in-memory key-value view.
func newMemKV() *memKV {
	return &memKV{data: map[string][]byte{}}
}

var _ AtomicStore = (*memKV)(nil)

// Read returns the pairs the fetch names: the one value held under the key it
// names, or every held pair whose key carries the named prefix, ordered by key.
func (m *memKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if fetch.Key != "" {
		value, held := m.data[fetch.Key]
		if !held {
			return nil, nil
		}
		return []Pair{{Key: fetch.Key, Value: append([]byte(nil), value...)}}, nil
	}
	var out []Pair
	for key, value := range m.data {
		if strings.HasPrefix(key, fetch.Prefix) {
			out = append(out, Pair{Key: key, Value: append([]byte(nil), value...)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Keys returns the keys the prefix names, in key order.
func (m *memKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.data))
	for key := range m.data {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Namespaces returns the key namespaces of the in-process keys without reading
// values.
func (m *memKV) Namespaces(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	namespaces := map[string]struct{}{}
	for key := range m.data {
		namespaces[keyNamespaceOf(key)] = struct{}{}
	}
	out := make([]string, 0, len(namespaces))
	for namespace := range namespaces {
		out = append(out, namespace)
	}
	sort.Strings(out)
	return out, nil
}

// Commit applies the batch in place of whatever was held there; the in-process
// view already holds what a barrier would publish.
func (m *memKV) Commit(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range batch.Deletes {
		delete(m.data, key)
	}
	for _, put := range batch.Puts {
		m.data[put.Key] = append([]byte(nil), put.Value...)
	}
	return ctx.Err()
}

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
	"syscall"
	"time"
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
	// WriteRights declares that this open writes the root it names: it holds that
	// root's exclusive write lock for as long as the view lives, so a second such
	// open, in this process or another, is refused by name. A caller that commits
	// to the root declares it, and the boot that stands for one runtime instance
	// over that root does too. An open that declares nothing is a component view:
	// it holds no write rights and reads beside the writer that does.
	WriteRights bool
	// RetentionWindows registers, per retention policy, the window an original
	// recall keeps the references of that policy readable for. A policy with no
	// registered window keeps its references readable indefinitely, and a window
	// of zero or less reports every read of that policy as expired.
	RetentionWindows map[string]time.Duration
}

// StoreCapabilities names what one opened tier guarantees, so a tier that holds
// nothing across process exit says so instead of appearing durable.
type StoreCapabilities struct {
	// Tier is the tier the capabilities describe.
	Tier StorageType
	// Durable reports whether a successful commit outlives the process that made
	// it.
	Durable bool
	// AtomicBatch reports whether a commit carrying several keys lands whole or
	// not at all. A tier that declares false lands one key per call and answers
	// ErrBatchNotAtomic for every larger batch, having landed none of it.
	AtomicBatch bool
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
	// mu makes the end of the view one event, so two closes of one tier cannot
	// both end it.
	mu sync.Mutex
	// closed reports whether Close ended this tier, so a repeated Close releases
	// nothing a second time.
	closed bool
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

// rootWriteLockFileName names the file inside a storage root that carries the
// root's write lock: the kernel records which process holds the lock, so the file
// stays a zero-byte marker the lock never writes to.
const rootWriteLockFileName = ".write-lock"

// ErrRootWriteLocked reports a write-rights open of a storage root another process
// already holds write rights on: one root carries one writer, so the second open
// is refused by name instead of committing over the slots the first one's index
// points at.
var ErrRootWriteLocked = errors.New("memory: another process holds write rights on this storage root")

// ErrCommitAfterClose reports a commit through a view whose Close ended it, so a
// caller that kept writing states the closed view instead of writing beside the
// process that took the root over.
var ErrCommitAfterClose = errors.New("memory: commit through a closed view")

// ErrReadAfterClose reports a read through a view whose Close ended it: the view
// states the closed view by name instead of answering the absence its released
// handle states, so a caller tells a view it ended from a body the log cannot answer.
var ErrReadAfterClose = errors.New("memory: read through a closed view")

// rootWriteLock is the exclusive lock one write-rights open holds on a storage
// root for as long as that view lives.
//   - The lock is the kernel's flock on a marker file inside the root, so the kernel drops it when the holding process exits and no recovery path exists that would clear a stale lock
//   - Every acquisition is non-blocking, so a writer that cannot take a root is refused at once instead of waiting behind the writer holding it
//   - One root carries one lock: every write the root admits holds it and takes no lock after it, so the order is that lock and nothing after
type rootWriteLock struct {
	// root is the storage root this lock covers.
	root string
	// file is the marker the kernel holds the lock on.
	file *os.File
}

// acquireRootWriteLock takes the exclusive write lock of root and answers the lock
// the caller releases when its view ends.
func acquireRootWriteLock(root string) (*rootWriteLock, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(root, rootWriteLockFileName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("tagent: open the write lock of %s: %w", root, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: %s", ErrRootWriteLocked, root)
		}
		return nil, fmt.Errorf("tagent: take the write lock of %s: %w", root, err)
	}
	return &rootWriteLock{root: root, file: file}, nil
}

// release gives root up to a later writer and repeats as a no-op, so a view that
// closes twice releases nothing the second time.
func (l *rootWriteLock) release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return file.Close()
}

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
		var lock *rootWriteLock
		if cfg.WriteRights {
			held, err := acquireRootWriteLock(cfg.Root)
			if err != nil {
				return nil, err
			}
			lock = held
		}
		store, err := openJournalView(filepath.Join(cfg.Root, kvDirName), cfg.WriteRights)
		if err != nil {
			lock.release()
			return nil, err
		}
		store.lock = lock
		return store, nil
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
//   - The journal tier's write path is one append-only file with one writer handle per opened view, and each commit is appended as one whole frame, so a view reading beside a writer cannot see half a frame
//   - One directory carries one writer: an open that declares StorageConfig.WriteRights holds the root's write lock, and a second such open, in this process or another, is refused by name with ErrRootWriteLocked, so two writers never commit over one slot
//   - This open hands the declaration through unchanged, so a caller that commits to the root declares the rights and every other caller holds none: a component view opens beside the live writer, a commit carrying keys through it is refused as ErrCommitWithoutWriteRights, and Close gives the root up to the next writer
//   - The kernel holds the lock, so it drops when the holding process exits; no recovery pass clears a stale lock, and durability never depended on the lock, because the barrier is the log's own sync
//   - The config's RetentionWindows register the original-recall window of each retention policy on the tier's original store, so a read of a reference outside its registered window is named expired; a policy the config leaves unregistered keeps its references readable indefinitely
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
	originals, err := newKVOriginalStore(store, cfg.RetentionWindows)
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
		Capabilities: StoreCapabilities{Tier: cfg.Type, Durable: cfg.Type != StorageMemory, AtomicBatch: cfg.Type != StorageFile},
	}, nil
}

// Close ends this tier's view of its backend: the segment store completes its
// final barrier, the engine behind it closes, and a durable tier gives its
// storage root up to the next writer.
//   - The root's write rights leave with this close, so the next writer — in this process or another — is admitted instead of being refused for a view that already ended
//   - The release happens however the barrier ended, because the engine closes the journal handle and the write lock together, so a failed barrier never leaves the root locked
//   - A repeated Close ends nothing twice and answers nil, so a caller that closes a run twice does not see an error the first close did not report
//   - The engine closes once, through the segment store, because both stores of a tier read and commit through that one engine
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func (t *Tier) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return t.Segments.Close()
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

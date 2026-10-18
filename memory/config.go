package memory

import (
	"context"
	"errors"
	"fmt"
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
	// StorageLocalFile is the tier whose key values are sharded snapshots under a
	// root directory.
	StorageLocalFile StorageType = "localfile"
	// StorageFile is the tier whose key values live in a rustviking store reached
	// through the rustviking CLI.
	StorageFile StorageType = "file"
)

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
// the same backend, together with the capabilities that backend guarantees.
type Tier struct {
	// Originals commits and recalls original bytes.
	Originals OriginalStore
	// Segments commits events behind the segment commit barrier.
	Segments *FileSegmentStore
	// Capabilities names what the backend guarantees.
	Capabilities StoreCapabilities
}

// ErrUnknownTier reports a storage config whose tier name matches no known tier.
var ErrUnknownTier = errors.New("memory: unknown storage tier")

// ErrRootRequired reports a persistent tier opened without the directory it
// persists under.
var ErrRootRequired = errors.New("memory: persistent tiers require a root directory")

// ErrRustVikingMissing reports a file tier whose rustviking CLI is not executable
// on this machine.
var ErrRustVikingMissing = errors.New("memory: rustviking CLI is not executable")

// OpenTier opens one storage tier over its backend.
//
// The memory tier holds only what the process holds; the localfile tier persists
// sharded snapshots under Root; the file tier drives the rustviking CLI and
// reports ErrRustVikingMissing when no such executable is found. Each call opens
// its own store over the same backend, so a caller reads exactly what an earlier
// process committed.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func OpenTier(ctx context.Context, cfg StorageConfig) (*Tier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch cfg.Type {
	case StorageMemory:
		segments, err := newFileSegmentStore(newMemKV())
		if err != nil {
			return nil, err
		}
		return &Tier{
			Originals:    NewMemoryOriginalStore(),
			Segments:     segments,
			Capabilities: StoreCapabilities{Tier: StorageMemory, Durable: false},
		}, nil
	case StorageLocalFile:
		if cfg.Root == "" {
			return nil, ErrRootRequired
		}
		kv, err := openLocalFileKV(cfg.Root)
		if err != nil {
			return nil, err
		}
		originals, err := newKVOriginalStore(kv, nil)
		if err != nil {
			return nil, err
		}
		segments, err := newFileSegmentStore(kv)
		if err != nil {
			return nil, err
		}
		return &Tier{
			Originals:    originals,
			Segments:     segments,
			Capabilities: StoreCapabilities{Tier: StorageLocalFile, Durable: true},
		}, nil
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
		kv := newRustVikingKV(path, config)
		originals, err := newKVOriginalStore(kv, nil)
		if err != nil {
			return nil, err
		}
		segments, err := newFileSegmentStore(kv)
		if err != nil {
			return nil, err
		}
		return &Tier{
			Originals:    originals,
			Segments:     segments,
			Capabilities: StoreCapabilities{Tier: StorageFile, Durable: true},
		}, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownTier, cfg.Type)
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

var _ kvPort = (*memKV)(nil)

// kvRead returns the pairs the fetch names: the one value held under the key it
// names, or every held pair whose key carries the named prefix, ordered by key.
func (m *memKV) kvRead(ctx context.Context, fetch kvFetch) ([]kvPair, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if fetch.key != "" {
		value, held := m.data[fetch.key]
		if !held {
			return nil, nil
		}
		return []kvPair{{key: fetch.key, value: append([]byte(nil), value...)}}, nil
	}
	var out []kvPair
	for key, value := range m.data {
		if strings.HasPrefix(key, fetch.prefix) {
			out = append(out, kvPair{key: key, value: append([]byte(nil), value...)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out, nil
}

// kvCommit applies the batch in place of whatever was held there; the in-process
// view already holds what a barrier would publish.
func (m *memKV) kvCommit(ctx context.Context, batch kvBatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range batch.deletes {
		delete(m.data, key)
	}
	for _, put := range batch.puts {
		m.data[put.key] = append([]byte(nil), put.value...)
	}
	return ctx.Err()
}

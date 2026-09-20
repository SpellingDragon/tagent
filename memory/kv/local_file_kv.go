package kv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/memory"
)

// LocalFileKV is a MINIMAL file-backed memory.KVStore, used ONLY as the MVP
// cross-process verification backend for the resident reliability protocol. It
// is a deliberately temporary model: an in-memory map persisted as a single
// JSON snapshot. It provides NO production durability, security, or long-term
// availability/maintainability guarantees — those are deferred to a dedicated
// storage engine (e.g. rustviking) wired in a later phase.
//
// Layout: kv.json — a full-map snapshot rewritten atomically (write tmp +
// rename) on Sync/Close. A POSIX rename is atomic, so a process KILL can never
// leave a torn snapshot: a reopen always sees the last successfully Synced state.
//
// Durability model (verification-grade only): writes update the in-memory map
// immediately, so in-process reads are always consistent; Sync() is the barrier
// that persists the map. A committed fact becomes visible to a fresh process
// only after its commit barrier ran Sync() — which is exactly what
// FileSegmentStore does. There is intentionally NO fsync: this backend survives
// a process restart / reopen (the guarantee actually verified), NOT an OS power
// loss. A write that was never Synced is lost on restart (honest "flush-only"
// semantics). A later storage-engine backend replaces this whole file.
type LocalFileKV struct {
	mu       sync.Mutex
	data     map[string]string
	snapPath string
	dirty    bool // unsynced changes pending
	closed   bool
}

// LocalFileKVOption is retained purely for call-site compatibility (wiring
// passes WithFSync from MemoryConfig). In the minimal model it is a no-op.
type LocalFileKVOption func(*LocalFileKV)

// WithFSync is accepted but IGNORED by the minimal verification backend (there
// is no fsync either way). It remains only so existing config plumbing stays
// stable until a real storage engine gives durability modes meaning again.
func WithFSync(enabled bool) LocalFileKVOption {
	return func(*LocalFileKV) {}
}

// NewLocalFileKV opens (creating if needed) the directory and loads any existing
// kv.json snapshot.
func NewLocalFileKV(dataDir string, opts ...LocalFileKVOption) (*LocalFileKV, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create kv data dir %s: %w", dataDir, err)
	}
	kv := &LocalFileKV{
		data:     make(map[string]string),
		snapPath: filepath.Join(dataDir, "kv.json"),
	}
	for _, opt := range opts {
		opt(kv)
	}
	// Clean up a leftover tmp from a process killed mid-rename.
	if err := os.Remove(kv.snapPath + ".tmp"); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cleanup kv tmp file: %w", err)
	}
	if raw, err := os.ReadFile(kv.snapPath); err == nil {
		if len(raw) > 0 {
			if uerr := json.Unmarshal(raw, &kv.data); uerr != nil {
				return nil, fmt.Errorf("parse kv snapshot %s: %w", kv.snapPath, uerr)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read kv snapshot %s: %w", kv.snapPath, err)
	}
	return kv, nil
}

// Sync is the durability barrier: it persists the in-memory map to the snapshot
// via an atomic tmp+rename. After a successful Sync the data is visible to a
// fresh process. A no-op when nothing changed since the last flush. Safe to call
// concurrently.
func (k *LocalFileKV) Sync() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.flushLocked()
}

// Close persists any pending changes so every acknowledged write is on disk
// before returning. Idempotent.
func (k *LocalFileKV) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	return k.flushLocked()
}

// flushLocked writes the snapshot atomically if dirty. Caller must hold the mutex.
func (k *LocalFileKV) flushLocked() error {
	if !k.dirty {
		return nil
	}
	raw, err := json.Marshal(k.data)
	if err != nil {
		return fmt.Errorf("marshal kv snapshot: %w", err)
	}
	tmp := k.snapPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return fmt.Errorf("write kv snapshot tmp: %w", err)
	}
	if err := os.Rename(tmp, k.snapPath); err != nil {
		return fmt.Errorf("rename kv snapshot: %w", err)
	}
	k.dirty = false
	return nil
}

// ListPartitionIDs enumerates persisted partition IDs from key namespaces
// (`{pid}:evt|idx|meta|tomb:…`; any persisted key in a partition's namespace
// proves the partition exists). Optional capability consumed by FileSegmentStore
// via a type assertion for cold-partition discovery; non-partition namespaces
// (e.g. `global:*`) are ignored.
func (k *LocalFileKV) ListPartitionIDs() []int {
	k.mu.Lock()
	defer k.mu.Unlock()
	seen := make(map[int]struct{})
	for key := range k.data {
		sep := strings.IndexByte(key, ':')
		if sep <= 0 {
			continue
		}
		pid, err := strconv.Atoi(key[:sep])
		if err != nil {
			continue
		}
		seen[pid] = struct{}{}
	}
	out := make([]int, 0, len(seen))
	for pid := range seen {
		out = append(out, pid)
	}
	sort.Ints(out)
	return out
}

// KVPut stores a key-value pair in the in-memory map. In-process reads see it
// immediately; it is durable to a fresh process only after a subsequent Sync()
// barrier. A nil return is NOT a durability guarantee.
func (k *LocalFileKV) KVPut(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	k.dirty = true
	return nil
}

// KVGet retrieves the value for the key. A missing key returns an error wrapping
// memory.ErrKeyNotFound.
func (k *LocalFileKV) KVGet(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.data[key]
	if !ok {
		return "", memory.KeyNotFound(key, nil)
	}
	return value, nil
}

// KVDelete removes a key (durable after the next Sync barrier).
func (k *LocalFileKV) KVDelete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	k.dirty = true
	return nil
}

// KVScan returns all key-value pairs whose keys start with the given prefix,
// sorted lexicographically by key. If limit > 0, at most limit pairs are returned.
func (k *LocalFileKV) KVScan(prefix string, limit int) ([]memory.KVPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var results []memory.KVPair
	for key, val := range k.data {
		if strings.HasPrefix(key, prefix) {
			results = append(results, memory.KVPair{Key: key, Value: val})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Key < results[j].Key })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// KVRange returns all key-value pairs whose keys fall in [start, end), sorted
// lexicographically by key. If limit > 0, at most limit pairs are returned.
func (k *LocalFileKV) KVRange(start, end string, limit int) ([]memory.KVPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var results []memory.KVPair
	for key, val := range k.data {
		if key >= start && key < end {
			results = append(results, memory.KVPair{Key: key, Value: val})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Key < results[j].Key })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// KVBatch applies a batch of put/delete operations to the in-memory map (durable
// after the next Sync barrier).
func (k *LocalFileKV) KVBatch(ops []memory.KVOp) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, op := range ops {
		switch op.Type {
		case "put":
			k.data[op.Key] = op.Value
		case "delete":
			delete(k.data, op.Key)
		default:
			return fmt.Errorf("unknown batch op type: %s", op.Type)
		}
	}
	k.dirty = true
	return nil
}

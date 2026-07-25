package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LocalFileKV is a file-backed KVStore that persists all key-value pairs
// to a single JSON file (kv.json) in the given data directory.
//
// It loads all data into memory on startup and uses a deferred flush strategy:
// writes update the in-memory map immediately (reads are always consistent) but
// disk persistence is batched — flushed every flushInterval or every flushThreshold
// writes, whichever comes first. This avoids serializing the entire map on every
// single KVPut call, which becomes O(n) expensive as the store grows.
//
// It has no external binary dependencies (unlike RustVikingClient which requires
// the rustviking CLI).
type LocalFileKV struct {
	mu       sync.Mutex
	data     map[string]string
	filePath string

	// Deferred flush state
	dirty     bool
	writeCnt  int
	flushDone chan struct{}
}

const (
	// flushInterval is the maximum delay between a write and its disk persistence.
	flushInterval = 2 * time.Second
	// flushThreshold forces a flush after this many unflushed writes.
	flushThreshold = 50
)

// NewLocalFileKV creates a LocalFileKV backed by kv.json in the given dataDir.
// If kv.json already exists, its contents are loaded into memory.
// The dataDir is created if it does not exist.
// Any leftover .tmp files from a crashed flush are cleaned up on startup.
// A background goroutine is started to periodically flush dirty data to disk.
func NewLocalFileKV(dataDir string) (*LocalFileKV, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create kv data dir %s: %w", dataDir, err)
	}

	// Clean up leftover .tmp files from a crashed flush
	tmpPath := filepath.Join(dataDir, "kv.json.tmp")
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cleanup kv tmp file %s: %w", tmpPath, err)
	}

	kv := &LocalFileKV{
		data:      make(map[string]string),
		filePath:  filepath.Join(dataDir, "kv.json"),
		flushDone: make(chan struct{}),
	}

	// Load existing data if the file exists
	if _, err := os.Stat(kv.filePath); err == nil {
		if err := kv.load(); err != nil {
			return nil, fmt.Errorf("load kv file %s: %w", kv.filePath, err)
		}
	}

	// Start background flush goroutine
	go kv.flushLoop()

	return kv, nil
}

// flushLoop periodically flushes dirty data to disk.
func (k *LocalFileKV) flushLoop() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = k.Sync() // best-effort periodic flush
		case <-k.flushDone:
			// Final flush on close — must take the lock like every other
			// flush path (flushLocked requires it; calling it bare races
			// with concurrent writers on the data map).
			_ = k.Sync()
			return
		}
	}
}

// load reads the JSON file into the in-memory map.
// Caller must hold the mutex.
func (k *LocalFileKV) load() error {
	raw, err := os.ReadFile(k.filePath)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, &k.data)
}

// flush writes the in-memory map to the JSON file atomically.
// Caller must hold the mutex.
func (k *LocalFileKV) flush() error {
	raw, err := json.Marshal(k.data)
	if err != nil {
		return fmt.Errorf("marshal kv data: %w", err)
	}
	// Write to temp file then rename for atomicity
	tmpPath := k.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0644); err != nil {
		return fmt.Errorf("write kv tmp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, k.filePath); err != nil {
		return fmt.Errorf("rename kv file %s: %w", k.filePath, err)
	}
	return nil
}

// flushLocked flushes if dirty, clearing the dirty flag.
// Caller must hold the mutex.
func (k *LocalFileKV) flushLocked() error {
	if !k.dirty {
		return nil
	}
	if err := k.flush(); err != nil {
		return err
	}
	k.dirty = false
	k.writeCnt = 0
	return nil
}

// Sync forces an immediate flush of any pending writes to disk.
// Safe to call concurrently.
func (k *LocalFileKV) Sync() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.flushLocked()
}

// Close flushes pending writes and stops the background flush goroutine.
// The final flush is performed synchronously here (not delegated to the
// flush goroutine) so that callers get a durability guarantee: when Close
// returns, all acknowledged writes are on disk. After Close, the KV is no
// longer usable.
func (k *LocalFileKV) Close() error {
	close(k.flushDone)
	return k.Sync()
}

// markDirty marks the store as needing a flush and triggers an immediate
// flush if the write threshold has been reached.
// Caller must hold the mutex.
func (k *LocalFileKV) markDirty() {
	k.dirty = true
	k.writeCnt++
	if k.writeCnt >= flushThreshold {
		_ = k.flushLocked()
	}
}

// KVPut stores a key-value pair. The write is persisted to disk
// asynchronously (within flushInterval) or immediately if the write
// threshold is reached.
func (k *LocalFileKV) KVPut(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	k.markDirty()
	return nil
}

// KVGet retrieves the value for a key.
// Returns an error if the key does not exist.
func (k *LocalFileKV) KVGet(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.data[key]
	if !ok {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return value, nil
}

// KVDelete removes a key. The change is persisted to disk asynchronously.
func (k *LocalFileKV) KVDelete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	k.markDirty()
	return nil
}

// KVScan returns all key-value pairs whose keys start with the given prefix,
// sorted lexicographically by key. If limit > 0, at most limit pairs are returned.
func (k *LocalFileKV) KVScan(prefix string, limit int) ([]KVPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	var results []KVPair
	for key, val := range k.data {
		if strings.HasPrefix(key, prefix) {
			results = append(results, KVPair{Key: key, Value: val})
		}
	}

	// Sort by key for deterministic ordering (matches RustVikingClient behavior)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// KVRange returns all key-value pairs whose keys fall in [start, end),
// sorted lexicographically by key. If limit > 0, at most limit pairs are returned.
func (k *LocalFileKV) KVRange(start, end string, limit int) ([]KVPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	var results []KVPair
	for key, val := range k.data {
		if key >= start && key < end {
			results = append(results, KVPair{Key: key, Value: val})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// KVBatch applies a batch of put/delete operations atomically and persists
// to disk asynchronously.
func (k *LocalFileKV) KVBatch(ops []KVOp) error {
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
	k.markDirty()
	return nil
}

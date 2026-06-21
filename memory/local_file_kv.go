package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// LocalFileKV is a file-backed KVStore that persists all key-value pairs
// to a single JSON file (kv.json) in the given data directory.
//
// It loads all data into memory on startup and flushes to disk on every
// write operation. This is suitable for low-concurrency, single-process
// scenarios such as example applications. It has no external binary
// dependencies (unlike RustVikingClient which requires the rustviking CLI).
type LocalFileKV struct {
	mu       sync.Mutex
	data     map[string]string
	filePath string
}

// NewLocalFileKV creates a LocalFileKV backed by kv.json in the given dataDir.
// If kv.json already exists, its contents are loaded into memory.
// The dataDir is created if it does not exist.
func NewLocalFileKV(dataDir string) (*LocalFileKV, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create kv data dir %s: %w", dataDir, err)
	}

	kv := &LocalFileKV{
		data:     make(map[string]string),
		filePath: filepath.Join(dataDir, "kv.json"),
	}

	// Load existing data if the file exists
	if _, err := os.Stat(kv.filePath); err == nil {
		if err := kv.load(); err != nil {
			return nil, fmt.Errorf("load kv file %s: %w", kv.filePath, err)
		}
	}

	return kv, nil
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

// KVPut stores a key-value pair and persists to disk.
func (k *LocalFileKV) KVPut(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	return k.flush()
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

// KVDelete removes a key and persists the change to disk.
func (k *LocalFileKV) KVDelete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	return k.flush()
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

// KVBatch applies a batch of put/delete operations atomically and persists to disk.
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
	return k.flush()
}

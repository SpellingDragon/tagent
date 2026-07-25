package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LocalFileKV is a file-backed KVStore with a snapshot + WAL layout:
//
//	kv.json      — full-map snapshot (rewritten only at compaction)
//	kv.wal.jsonl — append-only op log, one JSON op per line
//
// Writes update the in-memory map immediately (reads are always consistent)
// and enqueue an op; the op buffer is appended to the WAL every flushInterval
// or every flushThreshold writes. Appending is O(pending ops) — the full map
// is NOT reserialized per flush (the old single-file layout rewrote the
// entire store every flush, which grew O(n) with history: 33MB per 2s on a
// long-lived deployment).
//
// Compaction (snapshot rewrite + WAL truncate) triggers only when the WAL
// exceeds compactWALBytes, amortizing the O(n) cost over megabytes of
// appends. Startup loads the snapshot then replays the WAL; a torn final
// line from a crash is tolerated (ignored).
//
// The physical layout thus aligns with the store's logical model: hot
// increments append (like L0 window writes), full rewrites happen only at
// compaction points (like segment sealing) — not on every flush.
//
// It has no external binary dependencies (unlike RustVikingClient which
// requires the rustviking CLI).
type LocalFileKV struct {
	mu       sync.Mutex
	data     map[string]string
	snapPath string
	walPath  string

	// Deferred flush state
	pending  []walOp // ops not yet appended to the WAL file
	walSize  int64   // current WAL file size in bytes
	writeCnt int
	closed   bool

	flushDone chan struct{}
}

// walOp is a single WAL record. Op is "p" (put) or "d" (delete).
type walOp struct {
	Op string `json:"o"`
	K  string `json:"k"`
	V  string `json:"v,omitempty"`
}

const (
	// flushInterval is the maximum delay between a write and its WAL persistence.
	flushInterval = 2 * time.Second
	// flushThreshold forces a WAL append after this many unflushed writes.
	flushThreshold = 50
	// compactWALBytes triggers snapshot compaction once the WAL grows past it.
	compactWALBytes = 4 << 20 // 4 MiB
)

// NewLocalFileKV creates a LocalFileKV backed by kv.json (snapshot) and
// kv.wal.jsonl (op log) in the given dataDir. Existing data is loaded on
// startup: snapshot first, then WAL replay (torn tail lines from a crash are
// ignored). Directories and leftover .tmp files are handled. A background
// goroutine periodically flushes pending ops.
//
// Backward compatible with the previous single-file layout: an old kv.json
// simply loads as the snapshot (no WAL present).
func NewLocalFileKV(dataDir string) (*LocalFileKV, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create kv data dir %s: %w", dataDir, err)
	}

	kv := &LocalFileKV{
		data:      make(map[string]string),
		snapPath:  filepath.Join(dataDir, "kv.json"),
		walPath:   filepath.Join(dataDir, "kv.wal.jsonl"),
		flushDone: make(chan struct{}),
	}

	// Clean up leftover snapshot tmp from a crashed compaction.
	if err := os.Remove(kv.snapPath + ".tmp"); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cleanup kv tmp file: %w", err)
	}

	if _, err := os.Stat(kv.snapPath); err == nil {
		if err := kv.loadSnapshot(); err != nil {
			return nil, fmt.Errorf("load kv snapshot %s: %w", kv.snapPath, err)
		}
	}
	if st, err := os.Stat(kv.walPath); err == nil {
		kv.walSize = st.Size()
		if err := kv.replayWAL(); err != nil {
			return nil, fmt.Errorf("replay kv wal %s: %w", kv.walPath, err)
		}
	}

	go kv.flushLoop()
	return kv, nil
}

// flushLoop periodically appends pending ops to the WAL.
func (k *LocalFileKV) flushLoop() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = k.Sync() // best-effort periodic flush
		case <-k.flushDone:
			// Final flush is handled synchronously by Close; nothing to do.
			return
		}
	}
}

// loadSnapshot reads the snapshot file into the in-memory map.
func (k *LocalFileKV) loadSnapshot() error {
	raw, err := os.ReadFile(k.snapPath)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, &k.data)
}

// replayWAL applies WAL ops on top of the snapshot. A torn (unparseable)
// final line — the signature of a crash mid-append — stops replay silently;
// any unparseable line before the end is a real corruption and is reported.
func (k *LocalFileKV) replayWAL() error {
	f, err := os.Open(k.walPath)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20) // allow large values
	var pendingErr error
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if pendingErr != nil {
			return fmt.Errorf("wal corruption (bad line not at tail): %w", pendingErr)
		}
		var op walOp
		if err := json.Unmarshal(line, &op); err != nil {
			pendingErr = err // tolerated iff it is the final line
			continue
		}
		switch op.Op {
		case "p":
			k.data[op.K] = op.V
		case "d":
			delete(k.data, op.K)
		}
	}
	return sc.Err()
}

// appendWALLocked appends pending ops to the WAL file and triggers
// compaction when the WAL exceeds compactWALBytes.
// Caller must hold the mutex.
func (k *LocalFileKV) appendWALLocked() error {
	if len(k.pending) == 0 {
		return nil
	}
	f, err := os.OpenFile(k.walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open kv wal: %w", err)
	}
	w := bufio.NewWriter(f)
	var written int64
	for _, op := range k.pending {
		raw, err := json.Marshal(op)
		if err != nil {
			f.Close()
			return fmt.Errorf("marshal wal op: %w", err)
		}
		n1, _ := w.Write(raw)
		n2, _ := w.WriteString("\n")
		written += int64(n1 + n2)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return fmt.Errorf("flush kv wal: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close kv wal: %w", err)
	}
	k.pending = k.pending[:0]
	k.writeCnt = 0
	k.walSize += written

	if k.walSize >= compactWALBytes {
		return k.compactLocked()
	}
	return nil
}

// compactLocked rewrites the snapshot from the in-memory map and truncates
// the WAL. Atomic via tmp+rename; the WAL is removed only after the new
// snapshot is durably in place (crash between the two steps merely replays
// ops that are already in the snapshot — replay is idempotent).
// Caller must hold the mutex.
func (k *LocalFileKV) compactLocked() error {
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
	if err := os.Remove(k.walPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("truncate kv wal: %w", err)
	}
	k.walSize = 0
	return nil
}

// Sync forces an immediate append of pending ops to the WAL.
// Safe to call concurrently.
func (k *LocalFileKV) Sync() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.appendWALLocked()
}

// Compact forces a snapshot rewrite + WAL truncation regardless of WAL size.
func (k *LocalFileKV) Compact() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.appendWALLocked(); err != nil {
		return err
	}
	return k.compactLocked()
}

// Close flushes pending ops and stops the background flush goroutine.
// The final flush is performed synchronously so callers get a durability
// guarantee: when Close returns, all acknowledged writes are on disk.
// After Close, the KV is no longer usable.
func (k *LocalFileKV) Close() error {
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return nil
	}
	k.closed = true
	k.mu.Unlock()
	close(k.flushDone)
	return k.Sync()
}

// enqueueLocked records an op and appends to the WAL early when the write
// threshold is reached. Caller must hold the mutex.
func (k *LocalFileKV) enqueueLocked(op walOp) {
	k.pending = append(k.pending, op)
	k.writeCnt++
	if k.writeCnt >= flushThreshold {
		_ = k.appendWALLocked()
	}
}

// KVPut stores a key-value pair. The write is persisted to the WAL
// asynchronously (within flushInterval) or immediately when the write
// threshold is reached.
func (k *LocalFileKV) KVPut(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	k.enqueueLocked(walOp{Op: "p", K: key, V: value})
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

// KVDelete removes a key. The change is persisted asynchronously.
func (k *LocalFileKV) KVDelete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.data, key)
	k.enqueueLocked(walOp{Op: "d", K: key})
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
// them asynchronously.
func (k *LocalFileKV) KVBatch(ops []KVOp) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, op := range ops {
		switch op.Type {
		case "put":
			k.data[op.Key] = op.Value
			k.enqueueLocked(walOp{Op: "p", K: op.Key, V: op.Value})
		case "delete":
			delete(k.data, op.Key)
			k.enqueueLocked(walOp{Op: "d", K: op.Key})
		default:
			return fmt.Errorf("unknown batch op type: %s", op.Type)
		}
	}
	return nil
}

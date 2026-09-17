package kv

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/memory"
)

// LocalFileKV is a file-backed memory.KVStore with a snapshot + WAL layout:
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

	// quarantined counts WAL lines skipped at replay due to mid-file
	// corruption (F3, design-report-closeout): a single bit flip must not
	// cost the whole store. Torn tail lines (crash mid-append) are not
	// counted — they are the normal crash signature.
	quarantined atomic.Int64

	// Deferred flush state
	pending  []walOp // ops not yet appended to the WAL file
	walSize  int64   // current WAL file size in bytes
	writeCnt int
	closed   bool

	// fsync (durability, implementation-hardening D1): when true (default),
	// every WAL append f.Sync()s and snapshot compaction syncs the tmp file
	// + the directory before rename/removal — acknowledged writes survive
	// power loss, not just process death. RelationStore already ran per-line
	// Sync; this aligns the two durability standards.
	fsync bool

	// walNeedsDirSync (resident-readiness-plan 2.3): set at open when the
	// WAL file did not exist yet — its FIRST creation must be followed by a
	// directory sync so the file's directory entry itself is durable.
	walNeedsDirSync bool

	// lastErr records the most recent DEFERRED-flush failure (threshold or
	// periodic). Pending ops are retained by appendWALLocked's error paths;
	// the next Sync() (explicit or event barrier) re-attempts and surfaces
	// the error — a deferred failure never upgrades to a silent success
	// (resident-readiness-plan 2.4). Exposed via LastError for diagnostics.
	lastErr error

	// ops abstracts the durability syscalls so tests can inject failures at
	// each boundary (resident-readiness-plan 2.2) without touching real data
	// directories. Production code must only use k.ops.*, never os.*.
	ops fileOps

	flushDone chan struct{}
}

// fileOps is the injectable syscall surface of LocalFileKV.
type fileOps struct {
	openFile func(name string, flag int, perm os.FileMode) (*os.File, error)
	syncFile func(f *os.File) error
	rename   func(old, new string) error
	remove   func(name string) error
	syncDir  func(dir string) error
}

var defaultFileOps = fileOps{
	openFile: os.OpenFile,
	syncFile: func(f *os.File) error { return f.Sync() },
	rename:   os.Rename,
	remove:   os.Remove,
	syncDir:  syncDir,
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

// LocalFileKVOption customizes a LocalFileKV at construction.
type LocalFileKVOption func(*LocalFileKV)

// WithFSync disables (false) per-append fsync. Default is enabled: the store
// guarantees acknowledged writes survive power loss. Disabling trades that
// for throughput — the store logs a one-time durability downgrade warning.
func WithFSync(enabled bool) LocalFileKVOption {
	return func(k *LocalFileKV) { k.fsync = enabled }
}

// syncDir best-effort syncs a directory so a rename/create inside it is
// itself durable. Platform-unsupported directory fsync is recorded as a
// degraded durability capability (logged once per call site) and swallowed;
// any OTHER error propagates — it means the rename's durability is NOT
// guaranteed and must not be reported as success (delta spec「屏障失败」).
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open dir for sync %s: %w", dir, err)
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		if isUnsupportedDirSync(syncErr) {
			log.Warnf("[LocalFileKV] directory fsync unsupported on this platform (%v) — durability of renames in %s is best-effort (degraded capability)", syncErr, dir)
			return nil
		}
		return fmt.Errorf("fsync dir %s: %w", dir, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close dir %s: %w", dir, closeErr)
	}
	return nil
}

// isUnsupportedDirSync reports whether the error means "this platform or
// filesystem does not support directory fsync" (as opposed to a real I/O
// failure that must propagate).
func isUnsupportedDirSync(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EINVAL || errno == syscall.ENOTSUP || errno == syscall.EOPNOTSUPP
	}
	return false
}

// NewLocalFileKV creates a LocalFileKV backed by kv.json (snapshot) and
// kv.wal.jsonl (op log) in the given dataDir. Existing data is loaded on
// startup: snapshot first, then WAL replay (torn tail lines from a crash are
// ignored). Directories and leftover .tmp files are handled. A background
// goroutine periodically flushes pending ops.
//
// Backward compatible with the previous single-file layout: an old kv.json
// simply loads as the snapshot (no WAL present).
func NewLocalFileKV(dataDir string, opts ...LocalFileKVOption) (*LocalFileKV, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create kv data dir %s: %w", dataDir, err)
	}

	kv := &LocalFileKV{
		data:      make(map[string]string),
		snapPath:  filepath.Join(dataDir, "kv.json"),
		walPath:   filepath.Join(dataDir, "kv.wal.jsonl"),
		fsync:     true, // durability default: acknowledged writes survive power loss
		ops:       defaultFileOps,
		flushDone: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(kv)
	}
	if !kv.fsync {
		log.Warnf("[LocalFileKV] fsync disabled — acknowledged writes may be lost on power loss (durability downgrade)")
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
	} else {
		// WAL absent (or stat failed): its first creation will need a
		// directory sync so the directory entry is durable (2.3).
		kv.walNeedsDirSync = true
	}

	go kv.flushLoop()
	return kv, nil
}

// flushLoop periodically appends pending ops to the WAL. A failure here is
// recorded in lastErr (pending retained) — never silently dropped (2.4).
func (k *LocalFileKV) flushLoop() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := k.Sync(); err != nil {
				k.mu.Lock()
				k.lastErr = err
				k.mu.Unlock()
			}
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
// final line — the signature of a crash mid-append — stops replay silently.
// An unparseable line followed by further good lines is mid-file corruption:
// it is skipped and counted in the quarantine counter (F3) instead of
// failing startup — observability over data loss of a single op.
func (k *LocalFileKV) replayWAL() error {
	f, err := os.Open(k.walPath)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20) // allow large values
	var (
		badLines  int
		firstErr  error
		badLineNo int
	)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var op walOp
		if err := json.Unmarshal(line, &op); err != nil {
			if firstErr == nil {
				firstErr = err
				badLineNo = lineNo
			}
			badLines++
			continue
		}
		// A good line after bad lines proves the corruption is mid-file
		// (not a torn tail): flush the quarantine batch and keep going.
		if badLines > 0 {
			k.quarantined.Add(int64(badLines))
			log.Warnf("[kv] wal corruption: quarantined %d bad line(s) starting at line %d of %s (first err: %v); replay continues",
				badLines, badLineNo, k.walPath, firstErr)
			badLines, firstErr, badLineNo = 0, nil, 0
		}
		switch op.Op {
		case "p":
			k.data[op.K] = op.V
		case "d":
			delete(k.data, op.K)
		}
	}
	// Trailing bad lines: torn tail from a crash mid-append — tolerated
	// silently (not quarantined), same as the previous behavior.
	return sc.Err()
}

// WalQuarantined returns the number of WAL lines skipped at replay due to
// mid-file corruption (F3). Exposed for diagnostics/observability.
func (k *LocalFileKV) WalQuarantined() int64 { return k.quarantined.Load() }

// appendWALLocked appends pending ops to the WAL file and triggers
// compaction when the WAL exceeds compactWALBytes.
// Caller must hold the mutex.
func (k *LocalFileKV) appendWALLocked() error {
	if len(k.pending) == 0 {
		return nil
	}
	f, err := k.ops.openFile(k.walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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
	// Durability (D1): fsync the WAL before acknowledging — without this the
	// OS page cache can lose acknowledged writes on power loss (process-death
	// survival is NOT the same guarantee).
	if k.fsync {
		if err := k.ops.syncFile(f); err != nil {
			f.Close()
			return fmt.Errorf("fsync kv wal: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close kv wal: %w", err)
	}
	// First WAL creation: make the directory entry itself durable before any
	// caller treats the append as a barrier (resident-readiness-plan 2.3).
	if k.fsync && k.walNeedsDirSync {
		if err := k.ops.syncDir(filepath.Dir(k.walPath)); err != nil {
			return fmt.Errorf("fsync kv wal dir: %w", err)
		}
		k.walNeedsDirSync = false
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
	// Write + fsync the tmp file BEFORE rename: the rename is only as durable
	// as the file it exposes.
	tf, err := k.ops.openFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create kv snapshot tmp: %w", err)
	}
	if _, err := tf.Write(raw); err != nil {
		tf.Close()
		return fmt.Errorf("write kv snapshot tmp: %w", err)
	}
	if k.fsync {
		if err := k.ops.syncFile(tf); err != nil {
			tf.Close()
			return fmt.Errorf("fsync kv snapshot tmp: %w", err)
		}
	}
	if err := tf.Close(); err != nil {
		return fmt.Errorf("close kv snapshot tmp: %w", err)
	}
	if err := k.ops.rename(tmp, k.snapPath); err != nil {
		return fmt.Errorf("rename kv snapshot: %w", err)
	}
	if k.fsync {
		// Make the rename itself durable; a real I/O failure here must fail
		// the barrier (the caller keeps pending and re-attempts — replay is
		// idempotent), only platform-unsupported dir fsync degrades.
		if err := k.ops.syncDir(filepath.Dir(k.snapPath)); err != nil {
			return fmt.Errorf("fsync kv snapshot dir: %w", err)
		}
	}
	if err := k.ops.remove(k.walPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("truncate kv wal: %w", err)
	}
	k.walSize = 0
	return nil
}

// Sync forces an immediate append of all pending ops to the WAL and (when
// fsync is on) fsyncs it — a durability barrier: writes acknowledged before
// Sync survive power loss. Safe to call concurrently.
func (k *LocalFileKV) Sync() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.appendWALLocked()
}

// ListPartitionIDs enumerates persisted partition IDs from key namespaces
// (`{pid}:evt|idx|meta|tomb:…` — any persisted key in a partition's namespace
// proves the partition exists; segment-meta alone only appears after a window
// seal). Optional capability (implementation-hardening 2.4): deliberately NOT
// part of the KVStore six-method interface — FileSegmentStore consumes it via
// a type assertion, so backends without enumeration keep the old
// lazy-discovery behavior.
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
			continue // non-partition namespace (e.g. global:* keys)
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

// LastError returns the most recent deferred-flush failure, if any (2.4).
// Pending ops are retained; the next Sync re-attempts them. Exposed for the
// diagnostics chain — a deferred failure must stay observable.
func (k *LocalFileKV) LastError() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.lastErr
}

// enqueueLocked records an op and appends to the WAL early when the write
// threshold is reached. Caller must hold the mutex. A threshold-flush
// failure is recorded in lastErr (pending retained) — the KVPut itself
// stays an async acceptance; only Sync constitutes the durability barrier.
func (k *LocalFileKV) enqueueLocked(op walOp) {
	k.pending = append(k.pending, op)
	k.writeCnt++
	if k.writeCnt >= flushThreshold {
		if err := k.appendWALLocked(); err != nil {
			k.lastErr = err
		}
	}
}

// KVPut stores a key-value pair. THIS IS AN ASYNC ACCEPTANCE ONLY: the
// write lands in memory immediately and persists to the WAL within
// flushInterval / flushThreshold writes. Only a subsequent Sync() (or an
// event-level barrier via FileSegmentStore) makes it durable — the return
// value nil MUST NOT be read as a durability guarantee.
func (k *LocalFileKV) KVPut(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.data[key] = value
	k.enqueueLocked(walOp{Op: "p", K: key, V: value})
	return nil
}

// KVGet retrieves the value for the key. A missing key returns an error
// wrapping memory.ErrKeyNotFound — any other error is storage I/O.
func (k *LocalFileKV) KVGet(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.data[key]
	if !ok {
		return "", memory.KeyNotFound(key, nil)
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
func (k *LocalFileKV) KVScan(prefix string, limit int) ([]memory.KVPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	var results []memory.KVPair
	for key, val := range k.data {
		if strings.HasPrefix(key, prefix) {
			results = append(results, memory.KVPair{Key: key, Value: val})
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
func (k *LocalFileKV) KVRange(start, end string, limit int) ([]memory.KVPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	var results []memory.KVPair
	for key, val := range k.data {
		if key >= start && key < end {
			results = append(results, memory.KVPair{Key: key, Value: val})
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
func (k *LocalFileKV) KVBatch(ops []memory.KVOp) error {
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

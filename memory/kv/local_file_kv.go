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
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// LocalFileKV is a MINIMAL file-backed memory.KVStore, used ONLY as the MVP
// cross-process verification backend for the resident reliability protocol. It
// is a deliberately temporary model: in-memory maps persisted as per-partition
// JSON snapshots. It provides NO production durability, security, or long-term
// availability/maintainability guarantees — those are deferred to a dedicated
// storage engine (e.g. rustviking) wired in a later phase.
//
// Layout ( ): one snapshot file per key namespace —
// kv-<pid>.json for a partition's `{pid}:evt|idx|meta|tomb:…` keys and
// kv-global.json for every non-partition namespace. A Sync serializes ONLY the
// buckets touched since the last barrier (a dirty set), so one partition
// commit's write amplification is bounded by that partition's own key count —
// decoupled from the whole-library size, which is the growth ceiling the old
// single kv.json snapshot carried.
//
// Durability model (verification-grade only): writes update the in-memory maps
// immediately, so in-process reads are always consistent; Sync() is the barrier
// that persists the dirty buckets. A committed fact becomes visible to a fresh
// process only after its commit barrier ran Sync() — which is exactly what
// FileSegmentStore does. Each bucket file is replaced by an atomic POSIX
// rename of its tmp write, so a process KILL can never leave a torn snapshot:
// a reopen always sees the last successfully Synced state per bucket. There is
// intentionally NO fsync: this backend survives a process restart / reopen (the
// guarantee actually verified), NOT an OS power loss. A write that was never
// Synced is lost on restart (honest "flush-only" semantics).
//
// The old single kv.json snapshot is a DIFFERENT format and is deliberately NOT
// migrated: a pre-release library cold-rebuilds (the change's declared
// stance). A leftover kv.json is ignored and reported once at open.
type LocalFileKV struct {
	mu      sync.Mutex
	parts   map[int]map[string]string // partition buckets keyed by namespace pid
	global  map[string]string         // non-partition namespaces (`global:*`, …)
	dataDir string
	// dirtyPids holds partition labels with unsynced changes; the ok-valued
	// map distinguishes "touched" from "empty after deletes" (delete file).
	dirtyPids map[string]bool
	closed    bool
}

// bucketLabelOf maps a key to its snapshot bucket label: the numeric pid for
// `{pid}:…` namespaces (identical parse to the historical ListPartitionIDs
// rule), or "global" for anything else.
func bucketLabelOf(key string) string {
	sep := strings.IndexByte(key, ':')
	if sep > 0 {
		if pid, err := strconv.Atoi(key[:sep]); err == nil {
			return strconv.Itoa(pid)
		}
	}
	return kvGlobalLabel
}

const (
	kvGlobalLabel  = "global"
	kvFilePrefix   = "kv-"
	kvFileSuffix   = ".json"
	kvTmpSuffix    = kvFileSuffix + ".tmp"
	kvLegacySingle = "kv.json"
)

func kvFileName(label string) string { return kvFilePrefix + label + kvFileSuffix }

// NewLocalFileKV opens (creating if needed) the directory and loads every
// per-partition snapshot (kv-*.json) found there. The legacy single kv.json is
// NOT loaded or migrated — cold rebuild is the declared stance.
func NewLocalFileKV(dataDir string) (*LocalFileKV, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create kv data dir %s: %w", dataDir, err)
	}
	kv := &LocalFileKV{
		parts:     make(map[int]map[string]string),
		global:    make(map[string]string),
		dataDir:   dataDir,
		dirtyPids: make(map[string]bool),
	}
	// Clear crash residue: interrupted syncs leave *.json.tmp behind.
	tmps, err := filepath.Glob(filepath.Join(dataDir, kvFilePrefix+"*"+kvTmpSuffix))
	if err != nil {
		return nil, fmt.Errorf("scan kv tmp files: %w", err)
	}
	for _, t := range tmps {
		if rmErr := os.Remove(t); rmErr != nil && !os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("cleanup kv tmp file %s: %w", t, rmErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, kvLegacySingle)); statErr == nil {
		log.Warnf("[LocalFileKV] legacy %s found — the %s layout is not migrated (pre-release cold rebuild by design); ignoring it", kvLegacySingle, kvFilePrefix+"*"+kvFileSuffix)
	}
	files, err := filepath.Glob(filepath.Join(dataDir, kvFilePrefix+"*"+kvFileSuffix))
	if err != nil {
		return nil, fmt.Errorf("scan kv snapshots: %w", err)
	}
	sort.Strings(files)
	for _, f := range files {
		label := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), kvFilePrefix), kvFileSuffix)
		if label == "" || label == "global.tmp" {
			continue
		}
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			return nil, fmt.Errorf("read kv snapshot %s: %w", f, rerr)
		}
		m := make(map[string]string)
		if len(raw) > 0 {
			if uerr := json.Unmarshal(raw, &m); uerr != nil {
				return nil, fmt.Errorf("parse kv snapshot %s: %w", f, uerr)
			}
		}
		kv.applyBucket(label, m)
	}
	return kv, nil
}

// applyBucket installs a loaded bucket into the routed maps.
func (k *LocalFileKV) applyBucket(label string, m map[string]string) {
	if pid, err := strconv.Atoi(label); err == nil {
		k.parts[pid] = m
		return
	}
	for key, val := range m {
		k.global[key] = val
	}
}

// Sync is the durability barrier: it persists ONLY the buckets changed since
// the last barrier via per-bucket atomic tmp+rename. After a successful Sync
// the data is visible to a fresh process. Empty-after-delete buckets have their
// file removed (a partition with no keys must not present itself as existing).
// Safe to call concurrently.
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

// flushLocked writes every dirty bucket atomically, clearing dirty marks only
// for the buckets that landed. A failed bucket keeps its dirty flag so the
// next barrier retries it (per-bucket isolation of the write amplification is
// the point of the partition layout — a failure in one partition's snapshot
// must not re-commit, or drop, another partition's pending state).
func (k *LocalFileKV) flushLocked() error {
	if len(k.dirtyPids) == 0 {
		return nil
	}
	var firstErr error
	for label := range k.dirtyPids {
		m := k.bucketOfLabel(label)
		path := filepath.Join(k.dataDir, kvFileName(label))
		if len(m) == 0 {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				if firstErr == nil {
					firstErr = fmt.Errorf("remove emptied kv snapshot %s: %w", path, err)
				}
				continue
			}
			// The partition no longer exists at all once its last key is
			// deleted AND the removal landed: drop the empty in-memory bucket
			// too, so ListPartitionIDs and the on-disk layout agree.
			k.dropEmptyBucket(label)
			delete(k.dirtyPids, label)
			continue
		}
		raw, err := json.Marshal(m)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("marshal kv snapshot %s: %w", label, err)
			}
			continue
		}
		tmp := path + kvTmpSuffix
		if err := os.WriteFile(tmp, raw, 0644); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("write kv snapshot tmp %s: %w", tmp, err)
			}
			continue
		}
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			if firstErr == nil {
				firstErr = fmt.Errorf("rename kv snapshot %s: %w", path, err)
			}
			continue
		}
		delete(k.dirtyPids, label)
	}
	return firstErr
}

// bucketOfLabel resolves a label to its live map (same routing as writes).
func (k *LocalFileKV) bucketOfLabel(label string) map[string]string {
	if label == kvGlobalLabel {
		return k.global
	}
	if pid, err := strconv.Atoi(label); err == nil {
		if m, ok := k.parts[pid]; ok {
			return m
		}
	}
	return nil
}

// dropEmptyBucket removes an emptied partition from the in-memory registry
// (the global bucket is permanent namespace infrastructure, never dropped).
func (k *LocalFileKV) dropEmptyBucket(label string) {
	if pid, err := strconv.Atoi(label); err == nil {
		delete(k.parts, pid)
	}
}

// bucketFor returns (and creates if needed) the mutable map owning key,
// marking its label dirty.
func (k *LocalFileKV) bucketFor(key string) map[string]string {
	label := bucketLabelOf(key)
	if label == kvGlobalLabel {
		k.dirtyPids[kvGlobalLabel] = true
		return k.global
	}
	pid, _ := strconv.Atoi(label)
	m, ok := k.parts[pid]
	if !ok {
		m = make(map[string]string)
		k.parts[pid] = m
	}
	k.dirtyPids[label] = true
	return m
}

// ListPartitionIDs enumerates partition IDs known to the store: every bucket
// loaded from a kv-<pid>.json plus partitions created by in-process writes.
// Any key in a partition's namespace proves the partition exists (historical
// semantics preserved — the routing maps are the same source a persisted
// bucket loads into); non-partition namespaces never appear.
func (k *LocalFileKV) ListPartitionIDs() []int {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]int, 0, len(k.parts))
	for pid := range k.parts {
		out = append(out, pid)
	}
	sort.Ints(out)
	return out
}

// KVPut stores a key-value pair in the owning bucket's in-memory map.
// In-process reads see it immediately; it is durable to a fresh process only
// after a subsequent Sync() barrier. A nil return is NOT a durability guarantee.
func (k *LocalFileKV) KVPut(key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.bucketFor(key)[key] = value
	return nil
}

// KVGet retrieves the value for the key. A missing key returns an error wrapping
// memory.ErrKeyNotFound.
func (k *LocalFileKV) KVGet(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if m := k.bucketOfLabel(bucketLabelOf(key)); m != nil {
		if value, ok := m[key]; ok {
			return value, nil
		}
	}
	return "", memory.KeyNotFound(key, nil)
}

// KVDelete removes a key from its bucket (durable after the next Sync
// barrier; a bucket emptied by deletes has its snapshot file removed then).
func (k *LocalFileKV) KVDelete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.bucketFor(key), key)
	return nil
}

// allEntries flattens every bucket for scans (the maps are small per bucket;
// scans still bound results via limit after the global sort).
func (k *LocalFileKV) allEntries() []memory.KVPair {
	var results []memory.KVPair
	for _, m := range k.parts {
		for key, val := range m {
			results = append(results, memory.KVPair{Key: key, Value: val})
		}
	}
	for key, val := range k.global {
		results = append(results, memory.KVPair{Key: key, Value: val})
	}
	return results
}

// KVScan returns all key-value pairs whose keys start with the given prefix,
// sorted lexicographically by key. If limit > 0, at most limit pairs are returned.
func (k *LocalFileKV) KVScan(prefix string, limit int) ([]memory.KVPair, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var results []memory.KVPair
	for _, pair := range k.allEntries() {
		if strings.HasPrefix(pair.Key, prefix) {
			results = append(results, pair)
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
	for _, pair := range k.allEntries() {
		if pair.Key >= start && pair.Key < end {
			results = append(results, pair)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Key < results[j].Key })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// KVBatch applies a batch of put/delete operations to the in-memory buckets
// (durable after the next Sync barrier).
func (k *LocalFileKV) KVBatch(ops []memory.KVOp) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, op := range ops {
		switch op.Type {
		case "put":
			k.bucketFor(op.Key)[op.Key] = op.Value
		case "delete":
			delete(k.bucketFor(op.Key), op.Key)
		default:
			return fmt.Errorf("unknown batch op type: %s", op.Type)
		}
	}
	return nil
}

package tagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/SpellingDragon/tagent/memory"
)

// RuntimeResources (resident-readiness-plan 4.2/4.3, design D4): the owner
// registry for shared persistent stores. One entry per (kind, canonical
// path); every consumer acquires a lease; the LAST lease release closes the
// store and frees the directory lock — the next New gets a genuinely reopened
// instance. Incompatible fingerprints on the same path are REJECTED (never a
// second writer, never silent first-config-wins). A cross-process flock on a
// lockfile inside the directory enforces the single-writer invariant.
//
// Isolated stores (empty path) bypass the registry entirely — each New owns
// its own instance exclusively.

var (
	// ErrResourceConflict: same path, incompatible fingerprint (4.1 T3).
	ErrResourceConflict = errors.New("resource conflict: path already open with an incompatible config")
	// ErrStoreLocked: another process holds the single-writer lock (4.3).
	ErrStoreLocked = errors.New("store is locked by another process (single-writer)")
)

// resourceKey identifies one registry entry.
type resourceKey struct {
	kind string // localfile | rv | mem
	path string // canonical
}

// resourceEntry is one open shared store + its leases.
type resourceEntry struct {
	store       memory.MemoryStore
	fingerprint string
	leases      int
	lockFile    *os.File // cross-process single-writer flock
}

// RuntimeResources is the lease registry (concurrency-safe).
type RuntimeResources struct {
	mu      sync.Mutex
	entries map[resourceKey]*resourceEntry
	// opening (cold-eyes Warning 1): per-key open mutexes — same-path reopen
	// races serialize here while unrelated keys open concurrently.
	opening map[resourceKey]*sync.Mutex
}

// NewRuntimeResources creates an empty registry. The process-wide default is
// defaultResources; tests may inject isolated registries.
func NewRuntimeResources() *RuntimeResources {
	return &RuntimeResources{entries: make(map[resourceKey]*resourceEntry), opening: make(map[resourceKey]*sync.Mutex)}
}

// defaultResources keeps the historical process-wide sharing semantics.
var defaultResources = NewRuntimeResources()

// canonicalize resolves symlinks where possible; for not-yet-existing paths
// it falls back to Abs+Clean of the input (best effort, deterministic).
func canonicalize(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	// Canonicalize the closest existing ancestor so symlinked parents match.
	parent := filepath.Dir(abs)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolvedParent, filepath.Base(abs))
	}
	return abs
}

// acquire returns the shared store for (kind, path), opening it via open()
// when no live entry exists. The returned release func MUST be called when
// the consumer shuts down; the last release closes the store and frees the
// directory lock.
func (r *RuntimeResources) acquire(kind, rawPath, fingerprint string, open func() (memory.MemoryStore, error)) (memory.MemoryStore, func(), error) {
	key := resourceKey{kind: kind, path: canonicalize(rawPath)}
	// cold-eyes Warning 1: registry bookkeeping holds r.mu; the potentially
	// slow open (RebuildLiveCounts scan) runs OUTSIDE it — a per-key opening
	// mutex prevents same-path concurrent reopen races without blocking
	// unrelated New() calls behind a slow disk.
	// cold-eyes R2 M-2: read AND insert under r.mu — a lock-free read raced
	// with the insert (two acquirers each installing their own mutex → dual
	// open of the same path, the very thing this registry exists to prevent).
	r.mu.Lock()
	l, lok := r.opening[key]
	if !lok {
		l = &sync.Mutex{}
		r.opening[key] = l
	}
	r.mu.Unlock()
	l.Lock()
	defer l.Unlock()
	r.mu.Lock()
	e, ok := r.entries[key]
	if ok && e != nil {
		if e.fingerprint != fingerprint {
			r.mu.Unlock()
			return nil, nil, fmt.Errorf("%w: %s %s is open with fingerprint %s, requested %s",
				ErrResourceConflict, kind, key.path, e.fingerprint, fingerprint)
		}
		e.leases++
		store := e.store
		r.mu.Unlock()
		return store, func() { r.release(kind, key.path) }, nil
	}
	r.mu.Unlock()

	// cold-eyes R2 W-2: flock BEFORE open — the opened store starts writable
	// background workers (scanners/compactor), so taking the single-writer
	// lock first closes the cross-process second-writer window that existed
	// while open() ran unlocked.
	// The lock file lives INSIDE the store directory, which the store itself
	// would normally create during open — create it first so the lock precedes
	// the writable store (MkdirAll is idempotent and cross-process safe).
	if mkErr := os.MkdirAll(key.path, 0o755); mkErr != nil {
		return nil, nil, fmt.Errorf("create store dir %s: %w", key.path, mkErr)
	}
	lockFile, err := acquireDirLock(key.path)
	if err != nil {
		return nil, nil, err
	}
	store, err := open()
	if err != nil {
		_ = unlockDirLock(lockFile)
		return nil, nil, err
	}
	r.mu.Lock()
	// Re-check under the registry lock: a racing acquirer (same key, same
	// fingerprint) may have registered first while we were opening.
	if e2, ok2 := r.entries[key]; ok2 && e2 != nil {
		r.mu.Unlock()
		_ = closeStore(store)
		_ = unlockDirLock(lockFile)
		if e2.fingerprint != fingerprint {
			return nil, nil, fmt.Errorf("%w: %s %s is open with fingerprint %s, requested %s",
				ErrResourceConflict, kind, key.path, e2.fingerprint, fingerprint)
		}
		e2.leases++
		return e2.store, func() { r.release(kind, key.path) }, nil
	}
	r.entries[key] = &resourceEntry{
		store:       store,
		fingerprint: fingerprint,
		leases:      1,
		lockFile:    lockFile,
	}
	r.mu.Unlock()
	return store, func() { r.release(kind, key.path) }, nil
}

// release drops one lease; the last one closes the store, removes the
// registry entry and frees the single-writer lock — enabling a genuine
// reopen by the next New (4.1 T2).
func (r *RuntimeResources) release(kind, rawPath string) {
	key := resourceKey{kind: kind, path: canonicalize(rawPath)}
	r.mu.Lock()
	e, ok := r.entries[key]
	if !ok || e == nil {
		r.mu.Unlock()
		return
	}
	e.leases--
	if e.leases > 0 {
		r.mu.Unlock()
		return
	}
	// cold-eyes Warning 1: detach the entry under the lock, then run the
	// potentially slow close (fsync flush) OUTSIDE it — a sibling New() must
	// never queue behind a last-release flush.
	store := e.store
	lockFile := e.lockFile
	delete(r.entries, key)
	r.mu.Unlock()
	_ = closeStore(store) // idempotent (sync.Once inside the store)
	if lockFile != nil {
		_ = unlockDirLock(lockFile)
	}
}

// closeStore closes any store exposing the optional Close (idempotent there).
func closeStore(store memory.MemoryStore) error {
	if c, ok := store.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

// acquireDirLock takes the cross-process single-writer flock on
// <path>/.tagent-writer.lock. The flock is released by the OS when the
// process dies — a crashed writer never permanently locks the store.
func acquireDirLock(canonicalPath string) (*os.File, error) {
	lockPath := filepath.Join(canonicalPath, ".tagent-writer.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open writer lock: %w", err)
	}
	if err := flockExclusive(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: %s: %v", ErrStoreLocked, canonicalPath, err)
	}
	return f, nil
}

// unlockDirLock releases the flock and closes the lock file.
func unlockDirLock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.F_UNLCK); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// flockExclusive tries a non-blocking exclusive flock (darwin/linux).
// NOTE: flock is a LOCAL-disk contract — on NFS it is advisory/unreliable, so
// RuntimeResources assumes the store directory is on a local volume.
func flockExclusive(f *os.File) error {
	const lockEx = syscall.LOCK_EX
	const lockNb = syscall.LOCK_NB
	_, _, errno := syscall.Syscall(syscall.SYS_FLOCK, f.Fd(), lockEx|lockNb, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// fingerprintMemory renders the conflict-relevant subset of MemoryConfig as a
// canonical string. Fields NOT here (e.g. read_namespaces) are per-agent view
// config and may differ across sharers.
func fingerprintMemory(mc MemoryConfig) string {
	fsync := "true"
	if mc.FSync != nil {
		fsync = fmt.Sprintf("%v", *mc.FSync)
	}
	lifecycle := "default"
	if mc.Lifecycle != nil {
		if b, err := json.Marshal(mc.Lifecycle); err == nil {
			lifecycle = string(b)
		}
	}
	engine := "off"
	if mc.Engine != nil {
		if b, err := json.Marshal(mc.Engine); err == nil {
			engine = string(b)
		}
	}
	return strings.Join([]string{"v1", mc.Type, mc.Path, "fsync=" + fsync,
		"lifecycle=" + lifecycle, "engine=" + engine, "rvbin=" + mc.RustVikingBinary}, "|")
}

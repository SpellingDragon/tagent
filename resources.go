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

	"trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/memory"
)

// RuntimeResources: the owner
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
	// ErrResourcePoisoned (§6.4, design 决策7): the previous generation at this
	// path could not be CONFIRMED stopped (or its lock could not be confirmed
	// released), so the registry keeps a poisoned entry sealing the path. The
	// seal is an explicit entry — holding the store/engine/lockfile strong
	// references and the failure result — never the accidental leak of a handle
	// nobody can observe.
	ErrResourcePoisoned = errors.New("resource poisoned: previous generation on this path did not confirm a safe stop; path sealed against new generations")
	// ErrReclaimUnconfirmed (§6.5, design 決策7): construction released a
	// partially-built resource WITHOUT a confirmed reclaim. The open closure
	// wraps it so acquire must NOT free the writer lock — an unconfirmed
	// reclaim seals the path exactly like an unconfirmed worker stop.
	ErrReclaimUnconfirmed = errors.New("reclaim of partially built resource unconfirmed")
)

// resourceKey identifies one registry entry.
type resourceKey struct {
	kind string // localfile | rv | mem
	path string // canonical
}

// openedResource is what an acquire open closure builds: the shared backend
// store plus its OPTIONAL same-generation engine (nil when no engine is
// configured or when engine construction degraded). Both are owned by the
// registry entry for its whole generation and torn down together — engine
// worker first (its drain still writes to the live KV), backend flush second —
// by the last lease release.
type openedResource struct {
	store  memory.MemoryStore
	engine memory.MemoryEngine
}

// resourceEntry is one open shared store + its engine + its leases.
type resourceEntry struct {
	store memory.MemoryStore
	// engine is the entry-owned memory engine (nil = degraded / no engine). It
	// shares the store's generation: built in the same open() and closed by the
	// last lease release, so a reopen always gets a fresh engine bound to a
	// fresh backend — never a stale engine bound to an already-closed one (F7/D5).
	engine      memory.MemoryEngine
	fingerprint string
	leases      int
	lockFile    *os.File // cross-process single-writer flock
	// generation uniquely identifies this entry so that a stale release
	// closure (from a previously-closed entry at the same path) cannot
	// decrement the lease count of a new entry that reused the path (F6).
	generation uint64
	// poisoned (§6.4): set when the final release could NOT confirm the
	// engine/backend stopped (or could not confirm the writer lock released).
	// The entry stays in the map holding store/engine/lockFile strong
	// references and closeErr — every later same-path acquire fails explicitly
	// instead of racing a possibly-live writer. Locks are never held by
	// "hoping GC keeps an unreachable fd".
	poisoned bool
	closeErr error
}

// RuntimeResources is the lease registry (concurrency-safe).
type RuntimeResources struct {
	mu      sync.Mutex
	entries map[resourceKey]*resourceEntry
	// opening (cold-eyes Warning 1): per-key open mutexes — same-path reopen
	// races serialize here while unrelated keys open concurrently.
	opening map[resourceKey]*sync.Mutex
	// genNext is a monotonically increasing counter that uniquely identifies
	// each entry so stale release closures cannot affect a new entry (F6).
	genNext uint64
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

// openingLock returns the per-key opening mutex, creating it on first use under
// r.mu. Both acquire and the last-lease release (releaseGen) coordinate on this
// one mutex: a same-path reopen therefore waits for the previous generation to
// finish closing before it opens, while unrelated paths keep their own mutex
// and never block behind a slow flush (D5 line 115).
func (r *RuntimeResources) openingLock(key resourceKey) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.opening[key]
	if !ok {
		l = &sync.Mutex{}
		r.opening[key] = l
	}
	return l
}

// acquire returns the shared store AND its entry-owned engine for (kind, path),
// building them via open() when no live entry exists. The returned release func
// MUST be called when the consumer shuts down; the last release closes the
// engine and the store and frees the directory lock. The release is idempotent:
// repeated calls (e.g. from a second agent Close) cannot over-decrement any
// entry's lease count (F6), and a stale release cannot affect a different
// generation of the entry that reused the same path (F6).
func (r *RuntimeResources) acquire(kind, rawPath, fingerprint string, open func() (openedResource, error)) (memory.MemoryStore, memory.MemoryEngine, func() error, error) {
	key := resourceKey{kind: kind, path: canonicalize(rawPath)}
	// cold-eyes Warning 1: registry bookkeeping holds r.mu; the potentially
	// slow open (RebuildLiveCounts scan) runs OUTSIDE it — a per-key opening
	// mutex prevents same-path concurrent reopen races without blocking
	// unrelated New() calls behind a slow disk. The LAST-lease release takes
	// the SAME per-key mutex (releaseGen), so a same-path reopen waits for the
	// old generation to finish closing before opening a new one (D5 line 115).
	l := r.openingLock(key)
	l.Lock()
	defer l.Unlock()
	r.mu.Lock()
	e, ok := r.entries[key]
	if ok && e != nil {
		if e.poisoned {
			pcerr := e.closeErr
			r.mu.Unlock()
			return nil, nil, nil, fmt.Errorf("%w: %s %s: %v", ErrResourcePoisoned, kind, key.path, pcerr)
		}
		if e.fingerprint != fingerprint {
			r.mu.Unlock()
			return nil, nil, nil, fmt.Errorf("%w: %s %s is open with fingerprint %s, requested %s",
				ErrResourceConflict, kind, key.path, e.fingerprint, fingerprint)
		}
		e.leases++
		store, eng := e.store, e.engine
		gen := e.generation
		r.mu.Unlock()
		return store, eng, onceRelease(func() error { return r.releaseGen(kind, key.path, gen) }), nil
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
		return nil, nil, nil, fmt.Errorf("create store dir %s: %w", key.path, mkErr)
	}
	lockFile, err := acquireDirLock(key.path)
	if err != nil {
		return nil, nil, nil, err
	}
	res, err := open()
	if err != nil {
		if errors.Is(err, ErrReclaimUnconfirmed) {
			// §6.5：无法确认回收不得开放 writer — HOLD the flock and seal the
			// path with a poisoned entry (no store/engine exists, only the lock
			// and the failure), so no new generation can open alongside a
			// possibly-half-live backend. Uniform error shape (review N-1): the
			// sealed state is detectable via ErrResourcePoisoned while the
			// original failure stays matched by errors.Is through the join.
			r.mu.Lock()
			r.genNext++
			poisonGen := r.genNext
			r.mu.Unlock()
			r.poison(key, openedResource{}, lockFile, fingerprint, poisonGen, err)
			return nil, nil, nil, errors.Join(err,
				fmt.Errorf("%w: %s %s sealed after unconfirmed reclaim", ErrResourcePoisoned, kind, key.path))
		}
		if uerr := unlockDirLock(lockFile); uerr != nil {
			// Even the lock release is unconfirmed here — same discipline.
			r.mu.Lock()
			r.genNext++
			uGen := r.genNext
			r.mu.Unlock()
			r.poison(key, openedResource{}, lockFile, fingerprint, uGen, fmt.Errorf("%w (after open failure %v: unlock: %v)", ErrReclaimUnconfirmed, err, uerr))
			return nil, nil, nil, fmt.Errorf("%w: %s %s: open failed (%v) and the writer lock could not be released (%v)", ErrResourcePoisoned, kind, key.path, err, uerr)
		}
		return nil, nil, nil, err
	}
	r.mu.Lock()
	// Re-check under the registry lock: a racing acquirer (same key, same
	// fingerprint) may have registered first while we were opening.
	if e2, ok2 := r.entries[key]; ok2 && e2 != nil {
		r.mu.Unlock()
		// Discard the freshly-opened racing resource. Its writer lock is ours to
		// release (we never published) — but only when the discard's own stop is
		// CONFIRMED (review N-1: same discipline as every other teardown).
		workerStopped, cerr := closeResource(res)
		if cerr != nil {
			log.Warnf("[tagent] discarding racing resource: %v", cerr)
		}
		if !workerStopped && lockFile != nil {
			// Defensive leg (unreachable while acquires serialize on the per-key
			// opening mutex): an unconfirmed discard must NOT free the lock; the
			// live entry owns the map slot, so hold the lock and report the seal
			// to this caller instead of overwriting anything.
			return nil, nil, nil, fmt.Errorf("%w: %s %s: racing open could not confirm its discard stopped; its writer lock is held", ErrResourcePoisoned, kind, key.path)
		}
		_ = unlockDirLock(lockFile)
		if e2.poisoned {
			return nil, nil, nil, fmt.Errorf("%w: %s %s: %v", ErrResourcePoisoned, kind, key.path, e2.closeErr)
		}
		if e2.fingerprint != fingerprint {
			return nil, nil, nil, fmt.Errorf("%w: %s %s is open with fingerprint %s, requested %s",
				ErrResourceConflict, kind, key.path, e2.fingerprint, fingerprint)
		}
		e2.leases++
		gen2 := e2.generation
		return e2.store, e2.engine, onceRelease(func() error { return r.releaseGen(kind, key.path, gen2) }), nil
	}
	r.genNext++
	newGen := r.genNext
	r.entries[key] = &resourceEntry{
		store:       res.store,
		engine:      res.engine,
		fingerprint: fingerprint,
		leases:      1,
		lockFile:    lockFile,
		generation:  newGen,
	}
	r.mu.Unlock()
	return res.store, res.engine, onceRelease(func() error { return r.releaseGen(kind, key.path, newGen) }), nil
}

// onceRelease wraps fn so that repeated calls are idempotent — fn runs at most
// once per acquired lease, and EVERY later call returns the first call's cached
// result. This prevents double-release from a second agent Close from
// over-decrementing the lease count (F6) and surfaces the first close error to
// every caller rather than swallowing it (D5: a close error must reach Close).
func onceRelease(fn func() error) func() error {
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() { err = fn() })
		return err
	}
}

// releaseGen drops one lease on the entry at (kind, path) only if its
// generation matches gen. This prevents stale release closures (F6): a release
// from a previously-closed entry cannot decrement a new entry that reused the
// same path. When the last matching lease is released, the store is closed and
// the registry entry removed, enabling a genuine reopen by the next New (4.1 T2).
func (r *RuntimeResources) releaseGen(kind, rawPath string, gen uint64) error {
	key := resourceKey{kind: kind, path: canonicalize(rawPath)}
	// Serialize with a same-path acquire on the shared per-key opening mutex.
	// The whole detach→close→flock-unlock runs inside it, so a concurrent
	// reopen cannot observe "entry gone but flock still held" and mis-report
	// ErrStoreLocked — it WAITS for this generation to finish closing, then
	// opens cleanly (D5 line 115). Lock order matches acquire (opening → r.mu);
	// r.mu is still never held across the close, and unrelated paths use their
	// own mutex and are not blocked behind this flush.
	opening := r.openingLock(key)
	opening.Lock()
	defer opening.Unlock()

	r.mu.Lock()
	e, ok := r.entries[key]
	if !ok || e == nil || e.generation != gen {
		r.mu.Unlock()
		return nil // stale release: entry gone or a different generation now owns the path
	}
	e.leases--
	if e.leases > 0 {
		r.mu.Unlock()
		return nil
	}
	// Detach the entry under r.mu, then run the potentially slow close (engine
	// drain + fsync flush) OUTSIDE r.mu — a sibling New() on a DIFFERENT path
	// never queues behind it; a same-path New() waits on `opening` above (D5).
	res := openedResource{store: e.store, engine: e.engine}
	lockFile := e.lockFile
	fingerprint := e.fingerprint
	delete(r.entries, key)
	r.mu.Unlock()
	workerStopped, cerr := closeResource(res) // producers → engine → backend flush (D5 close order)
	if workerStopped {
		// Stop is CONFIRMED. A failing final flush still returns its error —
		// never claim durable success (design 決策7) — but the path reopens.
		if lockFile != nil {
			if uerr := unlockDirLock(lockFile); uerr != nil {
				// Lock release unconfirmed ⇒ the path cannot be safely reopened
				// either: seal it with the same poisoned discipline (§6.4).
				r.poison(key, res, lockFile, fingerprint, gen, fmt.Errorf("release writer lock: %w", uerr))
				return errors.Join(cerr, fmt.Errorf("release writer lock: %w", uerr))
			}
		}
		return cerr
	}
	// Could not confirm the engine worker stopped: a stale worker might still
	// write the KV. §6.4/D5: KEEP an explicit poisoned entry — strong
	// references to store/engine/lockfile (flock fd stays live AND observable
	// to the registry, never a silently unreachable leak) plus the failure
	// result — so every same-path acquire fails explicitly while unrelated
	// paths keep working. A new generation must never open alongside it.
	r.poison(key, res, lockFile, fingerprint, gen, cerr)
	if cerr == nil {
		cerr = errors.New("engine worker stop unconfirmed")
	}
	return cerr
}

// poison re-registers a failed final release as a sealed poisoned entry
// (§6.4). It runs on the per-key opening mutex, so no acquire can have
// published a new generation between the detach and this re-insert.
func (r *RuntimeResources) poison(key resourceKey, res openedResource, lockFile *os.File, fingerprint string, gen uint64, cause error) {
	r.mu.Lock()
	r.entries[key] = &resourceEntry{
		store:       res.store,
		engine:      res.engine,
		fingerprint: fingerprint,
		leases:      0,
		lockFile:    lockFile,
		generation:  gen,
		poisoned:    true,
		closeErr:    cause,
	}
	r.mu.Unlock()
	log.Warnf("[tagent] path %s SEALED poisoned: %v (entry holds store/engine/lockfile strong references)", key.path, cause)
}

// closeResource tears down a resource in the D5 close order: stop the
// forgetting producers FIRST (the compactor/lifecycle scanners call the
// engine's vector remover while sweeping), THEN stop and wait for the engine
// worker (whose drain still persists to the live KV), THEN flush and close the
// backend store (relation + KV final durability flush).
//
// It reports whether the engine worker was CONFIRMED stopped. A false return
// means a stale worker might still write the KV, so the caller must NOT release
// the writer lock / reopen the path (D5: never two writers). It also returns the
// first close diagnostic — close errors are surfaced, not swallowed. All steps
// are idempotent (closeOnce in engine and store), so a discard of a freshly
// opened racing resource and a last-lease release share one path without
// double-closing.
func closeResource(res openedResource) (workerStopped bool, err error) {
	if ps, ok := res.store.(interface{ StopProducers() }); ok {
		ps.StopProducers()
	}
	if res.engine != nil {
		// POISON primary trigger leg: a failed engine stop reports workerStopped
		// == false, so the caller seals the path with a poisoned entry (never two
		// writers on an unconfirmed stop). NOTE: the
		// current InMemoryEngine.Close returns nil unconditionally, so this MAIN
		// trigger leg is UNREACHABLE with today's engine — the seal is still
		// reachable via unlock/reclaim failures (§6.4). The mechanism is kept as a
		// FORWARD CONTRACT for a future engine whose Close can fail; no un-seal
		// (de-poison) exit is built — restart recovery is acceptable when it fires.
		if e := res.engine.Close(); e != nil {
			return false, fmt.Errorf("close memory engine: %w", e)
		}
	}
	if e := closeStore(res.store); e != nil {
		return true, fmt.Errorf("close memory store: %w", e)
	}
	return true, nil
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
// canonical string. Fields NOT here are per-agent view config (e.g.
// read_namespaces) or accepted-and-ignored axes with ZERO behavioral difference
// (localfile fsync) — the latter MUST NOT join the fingerprint, or two configs
// that behave identically would be rejected as a false conflict (resident-review-
// fixes 3.1; runtime-resource-ownership「零行为差异轴不制造假冲突」).
func fingerprintMemory(mc MemoryConfig) string {
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
	return strings.Join([]string{"v1", mc.Type, mc.Path,
		"lifecycle=" + lifecycle, "engine=" + engine, "rvbin=" + mc.RustVikingBinary}, "|")
}

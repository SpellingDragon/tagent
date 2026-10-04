package resources // import "github.com/SpellingDragon/tagent/agent/resources"

VARIABLES

var (
	// ErrResourceConflict reports the same path already open with an incompatible fingerprint.
	ErrResourceConflict = errors.New("resource conflict: path already open with an incompatible config")
	// ErrStoreLocked reports another process holding the directory's single-writer lock.
	ErrStoreLocked = errors.New("store is locked by another process (single-writer)")
	// ErrResourcePoisoned reports that a generation at this path did not confirm
	// its stop (or did not confirm its lock release), so the registry keeps a
	// poisoned entry sealing the path. The
	// seal is an explicit entry — holding the store/engine/lockfile strong
	// references and the failure result — never the accidental leak of a handle
	// nobody can observe.
	ErrResourcePoisoned = errors.New("resource poisoned: previous generation on this path did not confirm a safe stop; path sealed against new generations")
	// ErrReclaimUnconfirmed reports that construction released a
	// partially-built resource WITHOUT a confirmed reclaim. The open closure
	// wraps it so acquire must NOT free the writer lock — an unconfirmed
	// reclaim seals the path exactly like an unconfirmed worker stop.
	ErrReclaimUnconfirmed = errors.New("reclaim of partially built resource unconfirmed")
)
var DefaultResources = NewRuntimeResources()
    DefaultResources keeps the historical process-wide sharing semantics.
    DefaultResources is the process-wide shared-resource registry the
    composition root wires every store open through.

FUNCTIONS

func AcquireDirLock(canonicalPath string) (*os.File, error)
    AcquireDirLock takes the cross-process single-writer flock on
    <path>/.tagent-writer.lock. The flock is released by the OS when the process
    dies — a crashed writer never permanently locks the store.

func Canonicalize(path string) string
    Canonicalize resolves symlinks where possible; for not-yet-existing paths it
    falls back to Abs+Clean of the input (best effort, deterministic).

func CloseResource(res OpenedResource) (workerStopped bool, err error)
    CloseResource tears down one resource and reports whether the engine
    worker was CONFIRMED stopped, together with the first close diagnostic
    — close errors are surfaced, never swallowed. A false workerStopped
    means a stale worker might still write the KV, so the caller must NOT
    release the writer lock or reopen the path. Every step is idempotent,
    so discarding a freshly opened racing resource and releasing the last
    lease share one path without double-closing. The teardown order,
    why the engine's stop can fail on its own account, and the sealing rule
    that follows such an unconfirmed stop are specified in the documents

func CloseStore(store memory.MemoryStore) error
    CloseStore closes any store exposing the optional Close (idempotent there).

func FingerprintMemory(mc config.MemoryConfig) string
    FingerprintMemory renders the conflict-relevant subset of MemoryConfig
    as a canonical string. Fields NOT here are per-agent view config (e.g.
    read_namespaces) or axes with ZERO behavioral difference — the latter MUST
    NOT join the fingerprint, or two configs that behave identically would be
    rejected as a false conflict.

func FlockExclusive(f *os.File) error
    FlockExclusive tries a non-blocking exclusive flock (darwin/linux).
    NOTE: flock is a LOCAL-disk contract — on NFS it is advisory/unreliable,
    so RuntimeResources assumes the store directory is on a local volume.

func OnceRelease(fn func() error) func() error
    OnceRelease wraps fn so that repeated calls are idempotent — fn runs at
    most once per acquired lease, and EVERY later call returns the first call's
    cached result. A double release from a second agent Close therefore cannot
    over-decrement the lease count, and the first close error reaches every
    caller instead of being swallowed — a close error must surface to Close.

func UnlockDirLock(f *os.File) error
    UnlockDirLock releases the flock and closes the lock file.

TYPES

type OpenedResource struct {
	Store  memory.MemoryStore
	Engine memory.MemoryEngine
}
    OpenedResource is what an acquire open closure builds: the shared backend
    store plus its OPTIONAL same-generation engine (nil when no engine is
    configured or when engine construction degraded). Both are owned by the
    registry entry for its whole generation and torn down together — engine
    worker first (its drain still writes to the live KV), backend flush second —
    by the last lease release.

type RuntimeResources struct {
	// Has unexported fields.
}
    RuntimeResources is the owner registry for shared persistent stores
    (concurrency-safe). One entry per (kind, canonical path); every consumer
    acquires a lease, and the LAST lease release closes the store and frees
    the directory lock, so the next New gets a genuinely reopened instance.
    Incompatible fingerprints on the same path are rejected — never a second
    writer, never silent first-config-wins. A cross-process flock on a lockfile
    inside the directory enforces single-writer. Isolated stores (empty path)
    bypass the registry entirely: each New owns its instance exclusively.

func NewRuntimeResources() *RuntimeResources
    NewRuntimeResources creates an empty registry. The process-wide default is
    DefaultResources; tests may inject isolated registries.

func (r *RuntimeResources) Acquire(kind, rawPath, fingerprint string, open func() (OpenedResource, error)) (memory.MemoryStore, memory.MemoryEngine, func() error, error)
    Acquire returns the shared store AND its entry-owned engine for (kind,
    path), building them via open when no live entry exists. The returned
    release func MUST be called when the consumer shuts down; the last release
    closes the engine and the store and frees the directory lock. The release
    is idempotent: repeated calls (e.g. from a second agent Close) cannot
    over-decrement any entry's lease count, and a stale release cannot affect a
    different generation of the entry that reused the same path.

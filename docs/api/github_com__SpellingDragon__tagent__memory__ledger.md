package ledger // import "github.com/SpellingDragon/tagent/memory/ledger"

Package ledger is the runtime's durable original-text layer: it names one stored
original and the reference metadata that scopes it, commits those bytes and
returns them byte for byte, and carries the retention guard, the lifecycle and
the storage tiers built beside them.

The package speaks the fact vocabulary of the event package and nothing above
it, so the durable side cannot be reversed into the intelligent or governing
side. Its files sit under the ledger unit by the mechanism-subpackage rule of
CONTRIBUTING.md: the fact vocabulary (OriginalRef, Integrity), the consumer
wiring (Retention, Lifecycle, the stores) and the per-tier strategies
(localfile, rustviking).

Storage lifecycle: retention, tombstones, recovery inventory and reclamation
coordination.

TTL expiry and capacity eviction only tombstone a key; the physical destroy
happens at compaction, and never to an original a retention lease still
protects. The first destructive pass waits for the lease arming and the
inventory barrier to lift — the recovery inventory runs before any reclamation —
and a store that cannot protect or coordinate reclamation is reported by name on
the durable recovery tier instead of being treated as unprotected success.

Declarative lifecycle policy.

The lifecycle policy is injected declaratively through the agent's memory
configuration: the age window, the per-type overrides, the scan cadence and
the per-partition capacity bound. An unset field falls back to the default and
an explicit value wins, so no composition root hardcodes a second copy of the
default beside the config it was handed.

Original references and the read scope that authorizes them: one reference names
one stored original together with the metadata that scopes it, and the scoped
read face serves only the references registered inside the scope the read runs
under. See doc.go for the package's place in the memory axis.

Retention leases.

A retention lease is held by the shared resource owner — the recovery resource
or the unsettled obligation the hold protects, never "an agent" — and protection
is reference counted across holders: it stands until the last holder releases
it, and every hold and release is idempotent. The guard is the single source of
truth for that count; this layer registers what is held and the original time
the key reclassifies under once released, and keeps no second lease system of
its own.

Skeleton compile-time assertions for the ledger shapes: every declared field
name and type, every declared method signature, and every constant that must
stay a compile-time constant. A rename or a widened signature stops this file
from compiling, so the storage face cannot drift silently.

TierFacts is the durable fact surface of one opened tier: put, get, prefix scan
and the sync barrier a commit reports its success behind. It commits through
the same key-value backend the tier's segment store uses, so the replay and the
durable task commit share one database with the events already stored there.
Its signatures carry built-in types only, so it matches the state layer's
fact-log port structurally and no package edge is required in either direction
— the port is declared where it is consumed (see state's FactLog) and this tier
names the one surface it offers.

CONSTANTS

const DefaultSegmentMaxBytes int64 = 64 << 10
    DefaultSegmentMaxBytes is the finite bound served when a SegmentRequest
    carries a zero MaxBytes, so a request that states no limit stays bounded
    instead of reading an original whole.

VARIABLES

var (
	// ErrCapabilityIncomplete reports a durable recovery tier whose store lacks
	// the protection or reclamation coordination the tier promises: the system
	// refuses to start instead of treating "no protection" as success.
	ErrCapabilityIncomplete = errors.New("memory: storage lacks protection/reclamation coordination for the durable recovery tier")
	// ErrRecoveryIncomplete reports a destructive pass attempted before the
	// recovery inventory completed: potential recovery material is never let
	// through by the grace deadline alone.
	ErrRecoveryIncomplete = errors.New("memory: recovery inventory incomplete, destructive reclamation withheld")
	// ErrEnumerationIncomplete reports a failed key enumeration: the partial
	// view is never reconciled and no key is tombstoned off it.
	ErrEnumerationIncomplete = errors.New("memory: key enumeration failed, no partial view is reconciled")
	// ErrProtected reports a destructive action on a key a standing retention
	// lease still protects: only a refusal by name, never a silent destroy.
	ErrProtected = errors.New("memory: key is protected by a standing retention lease")
	// ErrLifecycleUnwired reports a lifecycle constructed without the retention
	// ledger or the store it coordinates.
	ErrLifecycleUnwired = errors.New("memory: lifecycle needs a retention ledger and a store")
)
var ErrCommitBarrier = errors.New("memory: commit barrier failed")
    ErrCommitBarrier reports a commit whose barrier did not complete, so nothing
    of it is published.

var ErrContentConflict = errors.New("memory: key already names other bytes")
    ErrContentConflict reports a commit whose key already names other bytes:
    one key names one original, so the second content is refused.

var ErrEventKeyRequired = errors.New("memory: segment record carries no event key")
    ErrEventKeyRequired reports a segment record that carries no event key,
    so no read could ever name the event again.

var ErrHolderRequired = errors.New("memory: protection hold names no holder")
    ErrHolderRequired reports a protection hold that names no holder, so its
    release could never be recognized as the same hold.

var ErrIntegrityMismatch = errors.New("memory: bytes do not match the recorded integrity")
    ErrIntegrityMismatch reports original bytes that do not match the
    completeness information recorded for them, so the read is refused instead
    of served.

var ErrKeyRequired = errors.New("memory: original reference carries no key")
    ErrKeyRequired reports a commit that carries no key, so no read could ever
    name the original again.

var ErrLeaseMismatch = errors.New("memory: release carries a lease that is not the hold's current lease")
    ErrLeaseMismatch reports a release whose lease is not the current lease of
    the hold it names: a superseded token from before a re-hold, or a token that
    belongs to another hold. The named hold stays held.

var ErrOriginalExpired = errors.New("memory: every reference is outside its retention window")
    ErrOriginalExpired reports a segmented read of a key whose references are
    all outside their retention windows.

var ErrOriginalMissing = errors.New("memory: no reference names the key")
    ErrOriginalMissing reports a segmented read of a key no reference names.

var ErrRootRequired = errors.New("memory: persistent tiers require a root directory")
    ErrRootRequired reports a persistent tier opened without the directory it
    persists under.

var ErrRustViking = errors.New("memory: rustviking CLI call failed")
    ErrRustViking reports a rustviking CLI call that did not succeed, carrying
    the report the CLI made of it.

var ErrRustVikingMissing = errors.New("memory: rustviking CLI is not executable")
    ErrRustVikingMissing reports a file tier whose rustviking CLI is not
    executable on this machine.

var ErrScopeRequired = errors.New("memory: read scope names no owner, format or retention policy")
    ErrScopeRequired reports a read scope that names no owner, format or
    retention policy: it aligns with no reference, so binding it would authorize
    nothing and granting it would authorize everything, and it is refused by
    name instead.

var ErrSegmentBarrier = errors.New("memory: segment commit barrier failed")
    ErrSegmentBarrier reports a commit whose barrier did not complete, so the
    record reaches no reader and no count.

var ErrSegmentLimit = errors.New("memory: segment request carries a negative byte limit")
    ErrSegmentLimit reports a segment request carrying a negative byte limit,
    so no bounded segment could answer it.

var ErrSegmentOffset = errors.New("memory: segment request carries a negative byte offset")
    ErrSegmentOffset reports a segment request carrying a negative byte offset,
    so no position could name where the segment starts.

var ErrSegmentPastEnd = errors.New("memory: segment starts past the last line")
    ErrSegmentPastEnd reports a segment request whose start line lies past the
    last stored line.

var ErrSegmentPosition = errors.New("memory: segment record carries a negative window or sequence")
    ErrSegmentPosition reports a segment record whose window or sequence slot is
    negative, so its key could not name a position.

var ErrSegmentRange = errors.New("memory: segment must start at line 1 or later and read at least one line")
    ErrSegmentRange reports a segment request that starts before the first line
    or asks for no lines at all.

var ErrSnapshotCorrupt = errors.New("memory: stored snapshot is corrupt")
    ErrSnapshotCorrupt reports stored snapshot content that cannot be read back
    as the key-value state it claims to hold.

var ErrUnknownTier = errors.New("memory: unknown storage tier")
    ErrUnknownTier reports a storage config whose tier name matches no known
    tier.

var ErrUnstage = errors.New("memory: staged writes could not be unstaged")
    ErrUnstage reports staged writes that could not be rolled back after a
    failed barrier, kept as the evidence of the failure alongside its cause.

TYPES

type FileSegmentStore struct {
	// Has unexported fields.
}
    FileSegmentStore commits events into time-window segments over one key-value
    backend. One commit stages the event body, its index entry and the window
    meta the window needs as one batch, and publishes the event to the read
    cache and the event count only when that commit returns success. A commit
    that fails names the barrier as its step, carries its cause, unstages what
    it wrote, and publishes no part of the record. Readers are served from
    published state only, so a load publishes exactly the records whose body,
    index and meta all arrived.

func (s *FileSegmentStore) Close() error
    Close ends the store with a final barrier over whatever the backend still
    holds staged, and repeats as a no-op.

func (s *FileSegmentStore) EventCount() int
    EventCount returns the number of events published by successful commits,
    so the count moves only where durable success was reported.

func (s *FileSegmentStore) Events(ctx context.Context) ([]StoredEvent, error)
    Events returns every published event in index order: partition first,
    then the window it is grouped under, then the sequence slot it takes inside
    that window, with the event key as the last tie-break. Only records whose
    commits published are listed, so the enumeration never exposes a record a
    read cannot serve.

func (s *FileSegmentStore) GetEvent(ctx context.Context, key event.EventID) (StoredEvent, bool, error)
    GetEvent returns the committed event under key together with the index
    position it was committed at, and reports whether any is published.

func (s *FileSegmentStore) Load(ctx context.Context) error
    Load publishes every stored record whose body, index and meta all arrived,
    so a record one commit left incomplete never reaches a reader or a count.

func (s *FileSegmentStore) StoreEvent(ctx context.Context, rec SegmentRecord) error
    StoreEvent commits one event behind the segment commit barrier: the event
    body, its index entry and the window meta the window needs are staged as one
    batch and committed before the event reaches the read cache, the event count
    or the caller. A repeat of the committed record returns success without a
    second fact, other bytes under the same event key are refused, and a failed
    commit leaves nothing published.

type Guard interface {
	// Hold registers one protection hold on the original of ref for holder and
	// returns the lease its release must carry back. Holding again as the same
	// holder while the hold stands is idempotent: the hold counts once and the
	// retry gets the lease it already holds, while a hold made after that holder
	// released is a new hold with a fresh lease. A hold that names no holder is
	// refused by name with ErrHolderRequired, so its release could always be
	// recognized as the same hold.
	Hold(ctx context.Context, ref OriginalRef, holder HolderID) (ProtectionLease, error)

	// Release drops the hold of holder on the original of ref under the lease
	// that hold returned. Releasing a hold that has already been released is a
	// no-op, so a repeated release is not an error; a release whose lease is not
	// the current lease of the named hold is refused by name with ErrLeaseMismatch
	// and leaves every hold standing, so a late or foreign lease never releases
	// the protection another hold keeps. The original is unprotected only once
	// the last holder has released it.
	Release(ctx context.Context, ref OriginalRef, holder HolderID, lease ProtectionLease) error

	// Protected reports whether the original of ref is still held, so a
	// reclaimer asks before it reclaims anything.
	Protected(ctx context.Context, ref OriginalRef) (bool, error)
}

    Guard is the retention protection port: it holds and releases protection
    on originals, and reports whether a reference is still held, so a reclaimer
    consults it before any destructive reclamation rather than assuming that an
    absence of protection is safety.

    The redline list (4 rules): MUST keep a hold owned by the shared resource,
    so it outlives any single agent relying on it. MUST answer whether a
    reference is still held before any destructive reclamation. MUST NOT treat
    the absence of protection, a timeout or an absent capability as safety.
    MUST NOT treat a released hold as an unreadable reference.

type HolderID string
    HolderID is the stable identity of one protection holder: the recovery
    resource or unsettled obligation the hold protects, not the agent name and
    not the owner of the reference it protects. It is stable across retries,
    so a retried hold is recognized as the same hold and counts once, while two
    holders on one original hold and release independently of each other.

type Integrity struct {
	// Digest is the digest over the stored bytes, in the canonical text form of
	// the algorithm that produced it.
	Digest string
	// Length is the exact byte length the stored original must have.
	Length int64
}
    Integrity is the completeness information of one original blob: the digest
    computed over its stored bytes and the byte length those bytes must have.

    A read returns the digest and the length together with the bytes, so a
    truncated or substituted original is reported as a failed read instead of
    being served as if it were complete.

type Lifecycle struct {
	// Has unexported fields.
}
    Lifecycle coordinates the storage lifecycle with the retention leases: the
    recovery inventory runs before the first destructive pass, TTL and capacity
    tombstone rather than destroy, protected material is never tombstoned,
    and the physical destroy waits for compaction.

func NewLifecycle(cfg LifecycleConfig, retention *Retention, store LifecycleStore, caps LifecycleCapabilities) (*Lifecycle, error)
    NewLifecycle coordinates one storage lifecycle over the retention
    ledger, the store and the capabilities that store was opened with.
    On the durable recovery tier a store missing protection or reclamation
    coordination is refused by name with ErrCapabilityIncomplete (fail-closed):
    the tier promises recovery, so "no protection" is never treated as success.
    A non-durable tier keeps its capability gap explicit in the capabilities it
    was opened under, and never fakes protection that did not take effect.

func (l *Lifecycle) ArmGrace(deadline time.Time)
    ArmGrace arms the retention lease wait of the first destructive pass under
    the given deadline: destructive reclamation waits for the arming and the
    inventory barrier, and once the bounded grace expires only a tier that
    promises no process recovery and holds no registered recovery material lets
    go — a missing lease must not stop forgetting forever, while the durable
    tier's pending material is never let through by the clock alone.

func (l *Lifecycle) Delete(ctx context.Context, key event.EventID, now time.Time) error
    Delete is the explicit destroy request for one key: it tombstones the key,
    and a key a standing retention lease protects is refused by name with
    ErrProtected instead of being destroyed under the protection.

func (l *Lifecycle) Reclaim(ctx context.Context, now time.Time) (int, error)
    Reclaim physically destroys the tombstoned keys at compaction. Keys are
    only ever destroyed here, so a release of protection never makes an original
    immediately unreadable: the original stays readable between its tombstone
    and this pass.

func (l *Lifecycle) RecoveryComplete()
    RecoveryComplete lifts the inventory barrier: the recovery inventory is done
    and the first destructive pass may run once the lease arming stands.

func (l *Lifecycle) RegisterRecovered(ctx context.Context, key StoredKey) error
    RegisterRecovered registers one recovered key during the recovery inventory:
    the restart registration that must precede every eviction. Registrations are
    taken before the barrier lifts, so material a restart still has to account
    for is never swept away by the first pass.

func (l *Lifecycle) Scan(ctx context.Context, now time.Time) (ScanReport, error)
    Scan runs one TTL/capacity pass: expired and over-capacity keys are
    tombstoned for reclamation at compaction, never physically destroyed here,
    and a key a retention lease still protects is kept out of the scan entirely.
    A failed enumeration is reported by name and tombstones nothing, so a
    partial view is never reconciled.

func (l *Lifecycle) Tombstoned(key event.EventID) bool
    Tombstoned reports whether one key is marked for reclamation at compaction.

type LifecycleCapabilities struct {
	// DurableRecovery reports the durable recovery tier: the store commits
	// across process exit, so its material is potential recovery material.
	DurableRecovery bool
	// Protection reports that originals can be kept from physical destruction
	// while a retention lease stands.
	Protection bool
	// Reclamation reports that tombstoned keys can be physically reclaimed at
	// compaction.
	Reclamation bool
}
    LifecycleCapabilities names the coordination capabilities of one store:
    the durable recovery tier it commits under, and whether it can protect
    originals against destruction and coordinate their reclamation. A tier that
    holds nothing across process exit says so instead of appearing durable,
    and a tier missing protection or reclamation coordination says so instead of
    faking protection that never took effect.

type LifecycleConfig struct {
	// GlobalTTLDays is the age window every event type ages out under, in days.
	// Zero means the default window; a negative value exempts every type from
	// TTL tombstoning.
	GlobalTTLDays int `json:"global_ttl_days" yaml:"global_ttl_days"`
	// TypeTTL overrides the age window per frozen event type, in days; a
	// negative value exempts that type from TTL tombstoning. A type absent from
	// the map ages out under the global window.
	TypeTTL map[string]int `json:"type_ttl" yaml:"type_ttl"`
	// CheckInterval is the cadence the periodic TTL scan runs at. Zero means
	// the default cadence.
	CheckInterval time.Duration `json:"check_interval" yaml:"check_interval"`
	// MaxEventsPerPartition bounds the event count of one partition; zero means
	// unbounded.
	MaxEventsPerPartition int `json:"max_events_per_partition" yaml:"max_events_per_partition"`
}
    LifecycleConfig is the declarative lifecycle policy: the age window every
    event type ages out under, the per-type overrides, the TTL scan cadence and
    the per-partition capacity bound. The fields are named for the memory config
    map they are injected from.

func DefaultLifecycleConfig() LifecycleConfig
    DefaultLifecycleConfig returns the default lifecycle policy: the seven-day
    global window, no per-type overrides, the one-hour scan cadence and no
    capacity bound. It is the fallback every unset field resolves to — never a
    value a composition root hardcodes beside its config.

func (c LifecycleConfig) Normalized() LifecycleConfig
    Normalized returns the policy with every unset field filled from the default
    and every explicit value kept as given, so the injected policy and the one
    the lifecycle runs under are the same object.

func (c LifecycleConfig) Window(eventType string) (time.Duration, bool)
    Window returns the age window one frozen event type ages out under and
    whether that type is exempt from TTL tombstoning entirely: a negative
    override exempts the type, a negative global window exempts every type,
    and an absent override falls back to the global window.

type LifecycleStore interface {
	// EnumerateKeys lists every key the store holds, or fails by name: a
	// partial list is never returned as an enumeration.
	EnumerateKeys(ctx context.Context) ([]StoredKey, error)
	// Tombstone marks one key for reclamation at compaction. Tombstoning is not
	// a physical destroy: the original stays readable until compaction.
	Tombstone(ctx context.Context, key event.EventID, at time.Time) error
	// ReclaimTombstoned physically destroys the given tombstoned keys at
	// compaction and returns how many were destroyed.
	ReclaimTombstoned(ctx context.Context, keys []event.EventID) (int, error)
}
    LifecycleStore is the reclamation surface the storage lifecycle coordinates
    with: enumerate the keys it holds, tombstone an expired key, and physically
    reclaim the tombstoned ones at compaction. A failed enumeration is
    reported by name and never returned as a partial list, so a reclaimer never
    reconciles a partial view.

    The redline list (4 rules): MUST enumerate every key it holds, or fail by
    name. MUST keep an original readable until compaction once it is tombstoned.
    MUST NOT answer a failed enumeration as a partial list. MUST NOT take the
    capabilities of a store from the store's own claim: they are settled by the
    wiring that opens it.

type MemoryGuard struct {
	// Has unexported fields.
}

    MemoryGuard is the in-memory retention protection: it keeps one hold per
    key and holder, hands each hold the lease its release must carry back,
    and reports to a reclaimer whether any holder still protects an original.
    Protection is counted across holders, so a hold is gone only when the last
    holder releases it.

func NewMemoryGuard() *MemoryGuard
    NewMemoryGuard returns an empty in-memory retention protection.

func (g *MemoryGuard) Hold(ctx context.Context, ref OriginalRef, holder HolderID) (ProtectionLease, error)
    Hold registers one protection hold on the original of ref for holder and
    returns the lease its release must carry back. Holding the same original
    again as the same holder while the hold stands stays one hold and hands
    back the lease it already holds, so one release by that holder clears it;
    a hold made after that release is a new hold under a fresh lease.

func (g *MemoryGuard) Protected(ctx context.Context, ref OriginalRef) (bool, error)
    Protected reports whether any holder still protects the original of ref,
    so a reclaimer asks before it reclaims anything.

func (g *MemoryGuard) Release(ctx context.Context, ref OriginalRef, holder HolderID, lease ProtectionLease) error
    Release drops the hold of holder on the original of ref under lease.
    Releasing a hold that has already been released is a no-op, and a release
    whose lease is not the current lease of the named hold is refused by name
    with ErrLeaseMismatch and leaves every hold standing: a late lease from
    before a re-hold, or a lease belonging to another hold, releases nothing.

type MemoryLifecycleStore struct {
	// Has unexported fields.
}
    MemoryLifecycleStore is the in-memory lifecycle store: the reference
    implementation of the reclamation surface, holding only what the process
    holds and declaring no recovery across process exit.

func NewMemoryLifecycleStore(caps LifecycleCapabilities) *MemoryLifecycleStore
    NewMemoryLifecycleStore returns an empty in-memory lifecycle store under the
    given declared capabilities.

func (s *MemoryLifecycleStore) Capabilities() LifecycleCapabilities
    Capabilities reports the capabilities the store was opened under, so the
    wiring that constructs a lifecycle hands them back beside the store.

func (s *MemoryLifecycleStore) EnumerateKeys(ctx context.Context) ([]StoredKey, error)
    EnumerateKeys implements LifecycleStore.EnumerateKeys.

func (s *MemoryLifecycleStore) Put(key StoredKey)
    Put files one key under the store's enumeration.

func (s *MemoryLifecycleStore) ReclaimTombstoned(ctx context.Context, keys []event.EventID) (int, error)
    ReclaimTombstoned implements LifecycleStore.ReclaimTombstoned: only a key
    that was tombstoned is physically destroyed, and destroying it once removes
    it from the store.

func (s *MemoryLifecycleStore) Tombstone(ctx context.Context, key event.EventID, at time.Time) error
    Tombstone implements LifecycleStore.Tombstone.

func (s *MemoryLifecycleStore) Tombstoned(key event.EventID) (time.Time, bool)
    Tombstoned reports the tombstone time of one key, and whether it is marked.

type MemoryOption func(*MemoryOriginalStore)
    MemoryOption shapes one MemoryOriginalStore at construction.

func WithRetentionWindow(policy string, window time.Duration) MemoryOption
    WithRetentionWindow registers the window one retention policy keeps its
    references readable for. A policy with no registered window keeps its
    references readable indefinitely, and a window of zero or less reports every
    read as expired.

type MemoryOriginalStore struct {
	// Has unexported fields.
}

    MemoryOriginalStore is the in-memory OriginalStore: the reference and blob
    model of the original text — bytes committed once per distinct content,
    every reference registered under its own owner, format and retention policy,
    exact recall served byte for byte and segmented reading handing out whole
    lines.

    The model is implemented once over the key-value seam, and this tier is that
    implementation held in a view that never leaves the process: a durable tier
    runs the same code over its own view, so nothing about recall, retention
    or deduplication is restated per tier. The tier holds only what the process
    holds: it survives no restart, and no result it returns claims otherwise.

func NewMemoryOriginalStore(options ...MemoryOption) *MemoryOriginalStore
    NewMemoryOriginalStore returns an empty in-memory original store, shaped by
    the given options.

func (s MemoryOriginalStore) BlobCount() int
    BlobCount returns the number of distinct contents held, so content
    deduplication stays observable.

func (s MemoryOriginalStore) Get(ctx context.Context, key event.EventID) RecallResult
    Get returns the exact recall result for key, classified exactly as the
    in-memory tier classifies it: the committed bytes while a reference of the
    key is inside its retention window, and a named missing, expired or failed
    result otherwise.

func (s MemoryOriginalStore) GetScoped(ctx context.Context, key event.EventID, scope ReadScope) RecallResult
    GetScoped returns the exact recall of key as visible inside scope, exactly
    as the in-memory tier serves it.

func (s MemoryOriginalStore) Put(ctx context.Context, ref OriginalRef, body []byte) error
    Put commits the original bytes of ref and publishes the reference once
    its barrier returns success. Identical bytes are stored once and shared,
    a reference already accepted is kept, a second scope on the same key joins
    the first, and other bytes under a held key are refused.

func (s MemoryOriginalStore) ReadSegment(ctx context.Context, key event.EventID, startLine, numLines int) ([]byte, error)
    ReadSegment returns at most numLines whole lines of the original stored
    under key, starting at line startLine, and names every read it cannot serve
    exactly as the in-memory tier names it.

func (s MemoryOriginalStore) ReadSegmentScoped(ctx context.Context, key event.EventID, scope ReadScope, req SegmentRequest) (SegmentResult, error)
    ReadSegmentScoped returns one byte-bounded segment of the original stored
    under key as visible inside scope, exactly as the in-memory tier serves it.

func (s MemoryOriginalStore) References(key event.EventID) []OriginalRef
    References returns every reference registered under key, one per
    deduplication scope in a stable order, so which owners, formats and
    retention policies still name the original stays observable.

type OriginalRef struct {
	// Key is the canonical identity of the original blob, in its hex form. Two
	// references may share it while their content is byte-identical; the
	// metadata below is what keeps them apart.
	Key event.EventID
	// Owner is the address the reference belongs to. It is part of the
	// deduplication scope, so two owners never overwrite each other.
	Owner event.Address
	// Format is the original format the reference was registered in. It is part
	// of the deduplication scope.
	Format string
	// RetentionPolicy is the retention policy the reference is held under. It is
	// part of the deduplication scope.
	RetentionPolicy string
	// Integrity is the completeness information of the stored bytes.
	Integrity Integrity
}
    OriginalRef names one stored original blob together with the reference
    metadata that scopes it: the owner that registered the reference,
    the format it was registered in, the retention policy it is held under,
    and the completeness information of its bytes.

    The bytes of a blob may be deduplicated by content, while the reference
    metadata stays separate: references that differ in owner, format or
    retention policy MUST NOT overwrite one another, and a later commit MUST NOT
    invalidate an earlier effective reference.

func (r OriginalRef) FactKey() string
    FactKey returns the reference's identity as a fact key: the canonical hex
    key of the original blob it names. The deduplication scope (owner, format
    and retention policy) keeps references registered side by side and never
    re-identifies the blob, so two tickets of byte-identical content answer the
    same key.

func (r OriginalRef) FactType() string
    FactType returns the frozen type a reference ticket is read as.

func (r OriginalRef) ReadScope() ReadScope
    ReadScope returns the read scope this reference was written under:
    the owner, format and retention policy it registered with. A reader bound to
    exactly this scope is a reader this reference is visible to; a reader bound
    to any other scope never aggregates it.

type OriginalStore interface {
	// Put commits the original bytes of ref. Content may be deduplicated by
	// blob, while owner, format and retention policy scope the reference
	// metadata, so an accepted reference is never invalidated by a later Put.
	Put(ctx context.Context, ref OriginalRef, body []byte) error

	// Get returns the exact recall result for key: the original bytes,
	// byte-identical to the bytes Put accepted, or a named
	// missing/expired/failed classification (see RecallResult), so a caller
	// tells the three outcomes apart instead of reading one bare error.
	//
	// A bare key is not a read authorization (see ReadScope): the visibility
	// decision here aggregates every reference of the key whatever scope it was
	// registered under. Authorized reads run through ScopedOriginalStore and
	// ScopedReader, where the trusted scope the read runs inside decides which
	// references take part.
	Get(ctx context.Context, key event.EventID) RecallResult

	// ReadSegment returns at most numLines lines of the original stored under
	// key starting at line startLine, so an oversized original is consumed by
	// segment rather than returned whole.
	//
	// The bound is a line count, not a byte count: one oversized line is
	// returned whole whatever numLines is. The byte-bounded face is
	// SegmentRequest and SegmentResult, served by ScopedReader.ReadSegment and
	// ScopedOriginalStore.ReadSegmentScoped, which cut at byte boundaries and
	// never at line boundaries.
	ReadSegment(ctx context.Context, key event.EventID, startLine, numLines int) ([]byte, error)
}
    OriginalStore is the original-text port: it commits the bytes a reference
    names and returns them byte for byte, whole or as segments.

    The redline list (4 rules): MUST commit the bytes a reference names before
    any bounded preview or segment entry exists. MUST return the original byte
    for byte, whole or as segments. MUST NOT answer a read with a model view in
    place of the original. MUST NOT invalidate an accepted reference by a later
    Put.

type ProtectionLease string
    ProtectionLease is the opaque token one accepted hold returns and its
    release must carry back. A lease is fresh per hold: a retry of a hold that
    still stands gets the same token, while a hold made after an earlier release
    gets a new one, so a late release carrying the earlier token matches nothing
    and releases nothing. Callers treat a lease as opaque: they never construct
    one, and its encoding is not a contract.

type ReadScope struct {
	// Owner is the address the reads are authorized for.
	Owner event.Address
	// Format is the original format the reads are authorized for.
	Format string
	// RetentionPolicy is the retention policy the reads are authorized under.
	RetentionPolicy string
}
    ReadScope is the trusted read scope one reader runs inside: the owner,
    format and retention policy its reads are authorized under, aligned with the
    metadata a reference was written under.

    - A read scope is not a caller's claim: it is bound once from trusted state
    when a scoped reader is constructed, and the values a caller merely types
    grant nothing. - Holding a bare key is not a read authorization: a read
    aggregates only the references registered inside its scope, so a key whose
    references all lie outside it reads as missing, its existence not leaked.
    - The triple is the one references are registered under (see refScope),
    read here as authorization rather than as deduplication identity: the two
    scopes decide different things and neither stands in for the other. - The
    tiers keep their baseline faces beside their storage logic; the visibility
    contract they share lives here, beside the read contract it serves.

func (s ReadScope) Validate() error
    Validate returns the named refusal that rejects this scope before any read
    runs under it, or nil when the scope may be bound. The only refusal today
    is the zero scope: it names no owner, no format and no retention policy,
    aligns with no reference, and is refused by name instead of being read as
    authorization for everything.

type RecallOutcome int
    RecallOutcome names the classification of one exact recall result.

const (
	// RecallUnclassified is the zero value: the outcome was not determined, so
	// it is never reported as a successful recall.
	RecallUnclassified RecallOutcome = iota
	// RecallFound means the original bytes were returned.
	RecallFound
	// RecallMissing means no original is stored under the key.
	RecallMissing
	// RecallExpired means the original is outside the retention window of its policy.
	RecallExpired
	// RecallFailed means the read itself failed and Err carries the cause.
	RecallFailed
)
type RecallResult struct {
	// Key is the identity that was recalled.
	Key event.EventID
	// Outcome is the named classification of this result.
	Outcome RecallOutcome
	// Body holds the returned original bytes, and is set only when Outcome is
	// RecallFound.
	Body []byte
	// Err carries the cause when Outcome is RecallFailed, and is nil otherwise.
	Err error
}
    RecallResult is the outcome of exact recall for one key: the original bytes
    when they were found, and otherwise a named classification of why they were
    not, so a miss is reported instead of silently omitted.

type Retention struct {
	// Has unexported fields.
}
    Retention is the retention lease ledger over the shared guard: it registers
    what each hold protects and hands back the guard's leases, so a reclaimer
    asks one surface whether an original is still protected and on what original
    timestamp it will reclassify. Holders are the stable identities of recovery
    resources and unsettled obligations, so two holders on one key protect it
    independently and the protection is gone only once the last one releases.

    The ledger registers and retires holds: the guard is the one port that holds
    them, so the ledger never declares a second surface of the same name.

func NewRetention(guard Guard) *Retention
    NewRetention returns a retention ledger over the given guard. The guard owns
    the protection state and its reference count; the ledger adds registrations
    and keeps no second truth.

func (r *Retention) Held(ctx context.Context, key event.EventID) (bool, error)
    Held reports whether any holder still protects the key, so a reclaimer asks
    before it reclaims anything. Protection is reference counted in the guard:
    it stands until the last holder has released.

func (r *Retention) Record(key event.EventID) (RetentionRecord, bool)
    Record returns the registration of one key: what its hold protects and the
    original recorded time it reclassifies under once released.

func (r *Retention) Register(ctx context.Context, key event.EventID, holder HolderID, record RetentionRecord) (ProtectionLease, error)
    Register files one protection hold on the key for holder and returns the
    lease its release must carry back, recording what the hold protects.
    A repeat hold by the same holder while the hold stands is recognized as the
    same hold and counts once, and the registered original time of a key is kept
    from its first registration: a re-hold never re-stamps the material.

func (r *Retention) ReleaseHolder(ctx context.Context, holder HolderID) (int, error)
    ReleaseHolder drops every hold one holder still stands on and returns how
    many were released. It is what a closing owner runs: it releases exactly
    that holder's holds, so a shared protection another holder keeps on the same
    key stays standing until that holder releases too.

func (r *Retention) Retire(ctx context.Context, key event.EventID, holder HolderID, lease ProtectionLease) error
    Retire drops the hold of holder on the key under the lease that hold
    returned. Releasing a hold that is not standing is a no-op; a release whose
    lease is not the current lease is refused by name with ErrLeaseMismatch
    and leaves every hold standing. The registration is kept after release:
    the key reclassifies under its recorded original time, which a release never
    re-stamps.

type RetentionKind string
    RetentionKind names why one registration holds its material: an unsettled
    delivery obligation, or material a recovery still has to take inventory of.

const (
	// RetentionUnconfirmed is the kind of a registration protecting material
	// whose delivery is still unconfirmed.
	RetentionUnconfirmed RetentionKind = "unconfirmed_delivery"
	// RetentionRecovery is the kind of a registration protecting material a
	// recovery inventory has not accounted for yet.
	RetentionRecovery RetentionKind = "recovery_material"
)
type RetentionRecord struct {
	// Kind names why the material is held.
	Kind RetentionKind
	// Type is the frozen event type of the material.
	Type string
	// RecordedAt is the original recorded time of the material, kept verbatim
	// and never re-stamped.
	RecordedAt time.Time
}
    RetentionRecord is what one retention registration remembers about the
    material it protects: the kind of material, its frozen event type and the
    original recorded time. The recorded time is the timestamp the key returns
    to the age window under once its protection is released — a release never
    re-stamps the material, so it reclassifies exactly as old as it always was.

type ScanReport struct {
	// Tombstoned lists the keys this scan marked for reclamation at compaction.
	Tombstoned []event.EventID
	// Protected lists the keys a standing retention lease kept out of the scan:
	// within retention, TTL and capacity never tombstone what is protected.
	Protected []event.EventID
	// Exempt lists the keys the policy exempts from TTL tombstoning.
	Exempt []event.EventID
}
    ScanReport is what one TTL/capacity scan did: the keys it tombstoned,
    the keys a standing retention lease kept out of the scan, and the keys the
    policy exempts from TTL tombstoning entirely.

type ScopedOriginalStore interface {
	// GetScoped returns the exact recall of key as visible inside scope: the
	// committed bytes while the reference registered inside scope is inside its
	// retention window, a named expired result when that reference is past its
	// window whatever the other references of the key say, a named missing
	// result when no reference names the key inside scope, and a named failed
	// result carrying the cause when the read itself fails. A key whose
	// references all lie outside scope reads as missing, so its existence is
	// not leaked.
	GetScoped(ctx context.Context, key event.EventID, scope ReadScope) RecallResult

	// ReadSegmentScoped returns one byte-bounded segment of the original stored
	// under key as visible inside scope, on exactly the terms of SegmentRequest
	// and SegmentResult. A read it cannot serve inside scope is named by the
	// same errors GetScoped classifies with — ErrOriginalMissing for a key with
	// no reference inside scope, ErrOriginalExpired for one past its window —
	// so an out-of-scope key is refused as missing and leaks nothing.
	ReadSegmentScoped(ctx context.Context, key event.EventID, scope ReadScope, req SegmentRequest) (SegmentResult, error)
}
    ScopedOriginalStore is the original-text read face that decides visibility
    inside one trusted read scope: it serves the reads that aggregate only
    the references registered inside the scope they name, and nothing else.
    The bare commit and recall face of OriginalStore stays on the store itself,
    so holding this port never hands a caller an unauthorized read.

    The redline list (3 rules): MUST aggregate only the references registered
    inside the scope the read names. MUST NOT hand a caller the bare commit or
    recall face of the store itself. MUST NOT leak the existence of a key whose
    references all lie outside the scope, nor be implemented by a store that
    cannot decide visibility inside one.

type ScopedReader struct {
	// Has unexported fields.
}
    ScopedReader is an original-text reader bound to one trusted read scope:
    the scope is bound once at construction, and every read it serves runs
    inside that scope, so authorization travels with the reader and never with a
    bare key.

    Holding a key is not a read authorization (see ReadScope): a read through
    this reader aggregates only the references registered inside the bound
    scope, and a key whose references lie outside it reads as missing.

func NewScopedReader(store ScopedOriginalStore, scope ReadScope) (*ScopedReader, error)
    NewScopedReader derives a reader bound to scope from store. The scope comes
    from the trusted state of the caller, not from the per-read calls the
    reader then serves, and a scope that names nothing is refused by name with
    ErrScopeRequired before any read runs.

func (r *ScopedReader) Get(ctx context.Context, key event.EventID) RecallResult
    Get returns the exact recall of key inside the bound scope, on the terms of
    ScopedOriginalStore.GetScoped: references registered outside the scope take
    no part in the visibility decision, so a key the scope cannot see reads as
    missing and leaks nothing.

func (r *ScopedReader) ReadSegment(ctx context.Context, key event.EventID, req SegmentRequest) (SegmentResult, error)
    ReadSegment returns one byte-bounded segment of the original stored under
    key, inside the bound scope: the request names the byte position to resume
    from — carried forward from the Next of the previous result — and the
    byte bound one segment may return, and the result carries the segment,
    the position the next segment resumes from and whether the end of the
    original was reached. Segments are cut at byte boundaries whatever the
    content holds and concatenate back to the exact bytes; a request with a zero
    MaxBytes is served under DefaultSegmentMaxBytes and never without a bound
    (see SegmentRequest).

func (r *ScopedReader) Scope() ReadScope
    Scope returns the trusted read scope the reader is bound to, so a consumer
    can record which authority a result was read under.

type SegmentPosition struct {
	// Offset is the byte offset from the start of the stored original.
	Offset int64
}
    SegmentPosition names one byte position inside a stored original, so one
    segmented read resumes exactly where the segment before it ended.

type SegmentRecord struct {
	// PartitionID is the partition the event belongs to.
	PartitionID int
	// WindowTS is the time window the event is grouped under.
	WindowTS int64
	// Seq is the sequence slot the event takes inside that window.
	Seq int64
	// EventKey is the canonical identity of the event.
	EventKey event.EventID
	// EventTime is the actual event time the window meta accounts for.
	EventTime int64
	// Body is the event payload, committed byte for byte.
	Body []byte
}
    SegmentRecord is one event on its way into a segment: the partition and
    window it belongs to, the sequence slot it takes, its canonical event key,
    the event time the window meta covers, and the event bytes committed
    verbatim.

type SegmentRequest struct {
	// From is the byte position the segment starts at.
	From SegmentPosition
	// MaxBytes bounds the bytes one segment returns. Zero means
	// DefaultSegmentMaxBytes.
	MaxBytes int64
}
    SegmentRequest asks for one byte-bounded segment of a stored original: the
    byte position to resume from and the maximum number of bytes to hand back.

    The position is the continuation form: it is filled from the Next of the
    previous SegmentResult, so one original is consumed segment by segment
    and the segments of one original concatenate back to its exact bytes.
    The bound is in bytes and always finite: a zero MaxBytes is served as
    DefaultSegmentMaxBytes and never as an unbounded read, a negative MaxBytes
    is refused by ErrSegmentLimit, a negative offset by ErrSegmentOffset,
    and an offset past the end of the stored original by ErrSegmentPastEnd.

type SegmentResult struct {
	// Body holds the bytes of this segment.
	Body []byte
	// Next is the byte position the following segment resumes from.
	Next SegmentPosition
	// EOF reports that this segment reached the end of the stored original.
	EOF bool
}
    SegmentResult is one byte-bounded segment of a stored original: the bytes of
    this segment, the byte position the next segment resumes from, and whether
    the end of the original was reached. Segments are cut at byte boundaries
    whatever the content holds, so a multi-byte character or a line ending may
    span two segments and the segments of one original concatenate back to
    its exact bytes. Next is the zero position when EOF is set and carries no
    resumption point then.

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
    StorageConfig locates one storage tier: the tier name, the root directory
    the persistent tiers persist under, and the rustviking executable and config
    the file tier drives.

type StorageType string
    StorageType names one storage tier of the original and segment stores.

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
type StoreCapabilities struct {
	// Tier is the tier the capabilities describe.
	Tier StorageType
	// Durable reports whether a successful commit outlives the process that made
	// it.
	Durable bool
}
    StoreCapabilities names what one opened tier guarantees, so a tier that
    holds nothing across process exit says so instead of appearing durable.

type StoredEvent struct {
	// Key is the canonical identity of the event.
	Key event.EventID
	// PartitionID is the partition the event was committed into.
	PartitionID int
	// WindowTS is the time window the event was grouped under.
	WindowTS int64
	// Seq is the sequence slot the event was committed into.
	Seq int64
	// Body is the committed event payload.
	Body []byte
}
    StoredEvent is one committed event as a read returns it: the bytes that were
    committed and the index position they were committed at.

type StoredKey struct {
	// Key is the canonical identity of the key.
	Key event.EventID
	// Type is the frozen event type of the material.
	Type string
	// Partition is the partition the key is filed under.
	Partition int
	// RecordedAt is the time the material was recorded.
	RecordedAt time.Time
}
    StoredKey is one key as the lifecycle enumerates it: its canonical identity,
    its frozen event type, the partition it is filed under and the time it was
    recorded, which is what its age window is measured from.

type Tier struct {
	// Originals commits and recalls original bytes.
	Originals OriginalStore
	// Segments commits events behind the segment commit barrier.
	Segments *FileSegmentStore
	// Capabilities names what the backend guarantees.
	Capabilities StoreCapabilities
}
    Tier is one opened storage tier: the original store and the segment store of
    the same backend, together with the capabilities that backend guarantees.

func OpenTier(ctx context.Context, cfg StorageConfig) (*Tier, error)
    OpenTier opens one storage tier over its backend.

    The memory tier holds only what the process holds; the localfile tier
    persists sharded snapshots under Root; the file tier drives the rustviking
    CLI and reports ErrRustVikingMissing when no such executable is found.
    Each call opens its own store over the same backend, so a caller reads
    exactly what an earlier process committed.

type TierFacts struct {
	// Has unexported fields.
}
    TierFacts is the durable fact surface of one opened tier.

func TierFactLog(tier *Tier) *TierFacts
    TierFactLog returns the durable fact surface of one opened tier, so a
    durable commit and a replay run on the same backend the tier's events are
    committed through — no second database is introduced for either.

func (l TierFacts) Get(ctx context.Context, key string) ([]byte, bool, error)
    Get returns the value stored under key and whether one is held.

func (l TierFacts) Put(ctx context.Context, key string, value []byte) error
    Put stages one key and its value.

func (l TierFacts) Scan(ctx context.Context, prefix string) (map[string][]byte, error)
    Scan returns every stored pair under the key prefix as key and value.

func (l TierFacts) Sync(ctx context.Context) error
    Sync completes the write barrier over everything staged so far.

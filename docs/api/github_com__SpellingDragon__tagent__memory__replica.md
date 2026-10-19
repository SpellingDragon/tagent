package replica // import "github.com/SpellingDragon/tagent/memory/replica"

Temporary-replica reclamation: the startup and periodic cleanup that take the
replicas back, and the active read/write protection that keeps a replica in use
out of every reclamation pass.

Reclamation owns exactly one area on disk: the replica area the materializer
writes into, `<root>/replica/`. Everything outside that area is a user file
and is never touched, and inside the area only a regular file is ever removed:
a traversal never follows a symbolic link and a removal is preceded by a
link-free check, so reclamation can never cross a link out of the replica root.
A replica being read or written is held by an active read/write handle and is
left alone by every trigger until the handle is released.

Package replica is the memory domain's on-disk scratch layer: it holds the
temporary replicas materialised from durable originals and reclaims them,
and it carries the port that commits an oversized result as an original before
it serves a bounded preview of that result.

The package reads originals through the memory port and publishes nothing but
facts about scratch files, so it can never be reversed into the durable side or
the governing side. Replicas are materialised under stable paths and read back
in byte-bounded segments through the scoped read face of memory, so a replica
never outruns the authorization it was materialised under.

Skeleton compile-time assertions for the workspace ports and their
implementations: every declared field name and type, every declared method
signature, the named refusals, and the reclamation defaults that must stay
compile-time constants. A renamed field or a widened signature stops this file
from compiling.

CONSTANTS

const (
	// DefaultCleanupInterval is the period between two temporary-replica reclamation
	// passes.
	DefaultCleanupInterval = time.Hour
	// DefaultReplicaMaxAge is the idle age above which a temporary replica is reclaimed.
	DefaultReplicaMaxAge = 24 * time.Hour
	// DefaultReplicaMaxCount is the number of temporary replicas kept before the oldest
	// are reclaimed.
	DefaultReplicaMaxCount = 200
)
const (
	// FormatToolResult is the format of a generic tool result.
	FormatToolResult = "tool-output"
	// FormatTaskResult is the format of a task result.
	FormatTaskResult = "task-result"
	// FormatTmuxOutput is the format of a tmux output.
	FormatTmuxOutput = "tmux-output"
	// FormatOverflow is the format of a result externalized because it overflowed its
	// bound.
	FormatOverflow = "overflow"
)
    FormatToolResult names the result formats the four producers of oversized
    results commit their originals under. Every producer takes the one
    externalize path below and differs only in the format it is committed in,
    so a generic tool result, a task result, a tmux output and an overflow-bound
    result share one threshold and one preview form, and none of them grows a
    truncation of its own.

const (
	// CompressProducerToolOverflow names the tool-result threshold judgment.
	CompressProducerToolOverflow = "workspace.tool-overflow"
	// CompressReasonToolOverflow states why the tool-result judgment files a
	// request.
	CompressReasonToolOverflow = "the tool result exceeds the interception threshold"
)
    CompressProducerToolOverflow names the threshold judgment of a tool
    result and CompressReasonToolOverflow states why that judgment files a
    compress request: a result over the interception threshold is material
    due for compression, and the judgment is on record whether or not the
    externalization that answers it holds.

const CharsPerToken = 4
    CharsPerToken is the character estimate one token carries when the
    interception threshold is computed, so the bound is expressed in the
    characters a model result actually spends.

const DefaultMaxTokens = 8000
    DefaultMaxTokens is the model window the interception threshold falls
    back to when a caller names none: a zero MaxTokens is read as 8000, so the
    threshold is never computed from a window no model has.

const DefaultPreviewRunes = 2000
    DefaultPreviewRunes is the number of leading characters one bounded preview
    keeps before the ticket names the whole original. The preview is further
    narrowed to a quarter of the threshold, so a preview never spends the budget
    it stands in for.

VARIABLES

var ErrCleanerAlreadyRunning = errors.New("workspace: cleaner is already running")
    ErrCleanerAlreadyRunning reports a second Run started on a cleaner whose
    reclamation loop is already running, so two loops would reclaim over one
    another.

var ErrCleanerRootMismatch = errors.New("workspace: the wired cleaner reclaims a different replica area")
    ErrCleanerRootMismatch reports a materializer wired to a replica cleaner
    that reclaims a replica area other than the one under this materializer's
    root: the protection behind a working copy would name an area its bytes
    never stand in. The construction is refused by name.

var ErrCleanupAgeInvalid = errors.New("workspace: cleanup max age must not be negative")
    ErrCleanupAgeInvalid reports a reclamation pass started with a negative age
    bound, which would delete replicas that were never idle at all.

var ErrCleanupCountInvalid = errors.New("workspace: cleanup max count must not be negative")
    ErrCleanupCountInvalid reports a reclamation pass started with a negative
    count bound, which has no meaning as a number of replicas to keep.

var ErrCleanupIntervalInvalid = errors.New("workspace: cleanup interval must be positive")
    ErrCleanupIntervalInvalid reports a cleaner constructed without a positive
    period, which could never trigger a periodic reclamation pass at all.

var ErrCleanupTargetMissing = errors.New("workspace: cleanup target is not a standing replica file")
    ErrCleanupTargetMissing reports an active read/write handle asked to protect
    a path that is not a standing regular file in the replica area, so there is
    no replica there to hold a read or a write on.

var ErrCleanupTargetOutOfRange = errors.New("workspace: cleanup target lies outside the replica root")
    ErrCleanupTargetOutOfRange reports an active read/write handle asked
    to protect a path outside this workspace's own replica area. Nothing is
    protected and nothing is ever reclaimed outside that area.

var ErrCompressRequestUnfiled = errors.New("workspace: the compress request of an overflow judgment was not filed")
    ErrCompressRequestUnfiled reports an overflow judgment whose compress
    request could not be filed. The judgment is the record that the result was
    over its bound, so the refusal is reported beside the preview the committed
    original still answers instead of dropping a result whose original is safe.

var ErrOriginalIneffective = errors.New("workspace: the original is ineffective, its replica cannot be rebuilt")
    ErrOriginalIneffective reports a replica that cannot be materialised again
    because the original it would be rebuilt from is missing or outside its
    retention window inside the bound read scope. The refusal names the absent
    rebuild instead of declaring a replica whose bytes were never written.

var ErrOriginalStoreRequired = errors.New("workspace: externalizing a result requires an original store")
    ErrOriginalStoreRequired reports an externalizer constructed without the
    original store it commits results into: no original could be kept before a
    preview of it is served.

var ErrReplicaParentLinked = errors.New("workspace: a parent of the replica path is a symbolic link")
    ErrReplicaParentLinked reports a materialisation refused by name because
    a parent of the replica path is a symbolic link: the declared path would
    cross a link to reach its bytes, so no replica is written and nothing stands
    beside what the link points at.

var ErrReplicaProtectionUnavailable = errors.New("workspace: active replica protection requires a wired replica cleaner")
    ErrReplicaProtectionUnavailable reports an active-use replica request on
    a materializer with no replica cleaner wired, so there is no protection to
    hold over the working copy it would serve. The request is refused by name
    instead of serving a copy no protection covers.

var ErrReplicaRootRequired = errors.New("workspace: replica root is required")
    ErrReplicaRootRequired reports a materializer constructed without a replica
    root, so no replica could ever be written under a declared path.

var ErrReplicaUnauthorized = errors.New("workspace: replica request lies outside the read scope")
    ErrReplicaUnauthorized reports a replica request that lies outside the
    trusted read scope the materializer runs in: no replica is written and no
    byte is served, and the refusal names the authorization boundary rather than
    the content behind it.

var ErrReplicaWriteFailed = errors.New("workspace: replica write failed")
    ErrReplicaWriteFailed reports that an oversized result was committed as
    an original, but its on-disk replica could not be written. The durable
    reference of that original stays effective, so the result is still served
    from the original.

var ErrResultScopeRequired = errors.New("workspace: the result scope names no owner, format or retention policy")
    ErrResultScopeRequired reports a result scope that names no owner, format or
    retention policy: a reference committed under it aligns with no read scope,
    so the original could never be recalled under the authority it was produced
    for.

var ErrToolRequired = errors.New("workspace: the bounded tool wraps no callable tool")
    ErrToolRequired reports a bounded tool constructed without the native
    callable tool it intercepts, so there is no call whose result could be
    bounded.

FUNCTIONS

func NewBoundedTool(inner trpctool.CallableTool, externalizer *ResultExternalizer, scope ResultScope, options ...BoundedToolOption) (*BoundedTool, error)
    NewBoundedTool returns the bounded tool over inner, committing oversized
    results under scope through externalizer and filing the judgment of each
    overflow where the options name. A missing tool, a missing externalizer and
    a scope that names no owner, format or retention policy are each refused by
    name.

func NewMaterializer(root string, store ledger.ScopedOriginalStore, scope ledger.ReadScope, options ...MaterializerOption) (*Materializer, error)
    NewMaterializer returns a materializer that declares replicas under root
    and reads originals only through store, inside scope. The scope comes from
    the trusted state of the caller rather than from any per-replica request,
    and a scope that names nothing is refused by name before any replica exists.
    The options may wire the active read/write protection behind the replicas
    written here, and a cleaner reclaiming another replica area is refused by
    name.

func NewReplicaCleaner(root string, options ...CleanupOption) (*ReplicaCleaner, error)
    NewReplicaCleaner returns a cleaner that reclaims only the replicas
    materialised under root, on the given period and within the given bounds.
    A root that names nothing is refused by name before any cleaner exists,
    and a period that could never trigger is refused the same way.

func NewResultExternalizer(originals ledger.OriginalStore, options ...ExternalizerOption) (*ResultExternalizer, error)
    NewResultExternalizer returns the externalize path over originals,
    with the threshold, the preview bound and the optional replica materializer
    the caller names. A nil original store is refused by name before any result
    could be externalized.

func OriginalRefFor(scope ResultScope, body []byte) ledger.OriginalRef
    OriginalRefFor returns the reference one result body is committed under
    inside scope: the key is the SHA-256 digest of the body in hex, so the same
    bytes produce the same key whatever producer meets them, while the owner,
    format and retention policy keep the references of different scopes apart.
    Every producer derives its reference here, so a result is committed under
    one identity regardless of which of the four producers externalized it.

func ThresholdForMaxTokens(maxTokens int) int
    ThresholdForMaxTokens returns the character count above which a result is
    externalized: MaxTokens/2*4, the one formula every producer decides on.
    A zero or negative MaxTokens is read as DefaultMaxTokens, so the threshold
    is always finite and always aligned with the model window the result is
    spent in.

TYPES

type ActiveReplica struct {
	Replica
	// Has unexported fields.
}
    ActiveReplica is one working copy of a durable original on disk: the replica
    the copy stands at and the close that ends the protection behind it.
    The replica keeps its exact bytes while the handle is open, and every
    reclamation pass leaves it alone until Close ends the protection.

func (a *ActiveReplica) Close()
    Close ends the active read/write protection behind this working copy,
    so a reclamation pass may take the replica back and a later materialisation
    may shadow it again. Closing twice ends the protection once, and closing a
    nil handle ends nothing and breaks nothing.

type BoundedTool struct {
	// Has unexported fields.
}
    BoundedTool is the native wrapper injection port of the externalize path:
    it wraps one native callable tool and routes an oversized result through
    the one externalize path before that result reaches the model. A result
    at or below the threshold is handed back unchanged, a nil result is not
    intercepted at all, and the declaration of the wrapped tool is passed
    through untouched.

    The wrapper is a tagent-face type rather than a second tool protocol:
    the composition root builds it over the native callable tool of an agent,
    so every tool family — the generic shell and file tools and the agent-kind
    tools alike — is bounded on the same path without a root tool package or a
    truncation of its own.

func (t *BoundedTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call runs one native call and bounds what it returns: a result whose
    serialized form is over the threshold files the compress request of
    that judgment and is committed as a durable original, answered with its
    bounded preview and ticket — an error from the wrapped tool is passed
    through untouched, a nil result is not intercepted, a result that cannot
    be serialized is handed back unchanged because no size could bound it,
    and a result inside the bound is returned exactly as the tool produced it.

    An original that could not be committed is reported as an error with
    no preview, so a ticket never names an original that was not kept.
    A replica that could not be written is reported beside the preview the
    committed original still answers: the preview is returned together with
    ErrReplicaWriteFailed rather than dropping a result whose original is safe.
    A request that could not be filed is reported beside that same preview as
    ErrCompressRequestUnfiled, so an unfiled judgment is never taken for none.

func (t *BoundedTool) Declaration() *trpctool.Declaration
    Declaration returns the declaration of the wrapped tool unchanged, so
    bounding a result changes nothing the model is told about the tool itself.

type BoundedToolOption func(*BoundedTool)
    BoundedToolOption names one setting of the bounded tool at construction
    time.

func WithCompressRequestFiling(filing memory.CompressRequestFiling) BoundedToolOption
    WithCompressRequestFiling wires where an overflow judgment files the
    compress request fact it is. The request is filed before the externalization
    that still completes inside the same call, so the preview is served without
    waiting for a consumer while the judgment stays on record for one.

type Cleaner interface {
	// CleanupStartup reclaims the temporary replicas left behind by a previous run.
	CleanupStartup(ctx context.Context) error
	// CleanupByAge reclaims every replica that has been idle longer than maxAge.
	CleanupByAge(ctx context.Context, maxAge time.Duration) error
	// CleanupByCount keeps at most maxCount replicas, reclaiming the oldest first.
	CleanupByCount(ctx context.Context, maxCount int) error
	// Run reclaims on the configured period until ctx is done or the cleaner is stopped.
	Run(ctx context.Context) error
	// Wait blocks until a running cleaner has stopped, so a close sequence confirms the
	// reclaimer ended instead of assuming it did.
	Wait(ctx context.Context) error
}

    Cleaner is the temporary-replica reclamation port: it reclaims replicas at
    startup and on a period, bounded by age and by count, and it waits for its
    own stop so the close sequence confirms the reclaimer ended.

    The redline list (4 rules): MUST leave a replica that is being read or
    written alone. MUST limit reclamation to this workspace's own temporary
    replicas. MUST NOT remove a user file. MUST NOT follow a symbolic link out
    of the replica root.

type CleanupOption func(*cleanupConfig)
    CleanupOption names one setting of a cleaner at construction time.

func WithCleanupInterval(interval time.Duration) CleanupOption
    WithCleanupInterval sets the period between two periodic reclamation passes.

func WithReplicaMaxAge(maxAge time.Duration) CleanupOption
    WithReplicaMaxAge sets the idle age above which the periodic pass reclaims a
    replica.

func WithReplicaMaxCount(maxCount int) CleanupOption
    WithReplicaMaxCount sets the number of idle replicas the periodic pass keeps
    before the oldest are reclaimed.

type Externalized struct {
	// Original is the reference the original bytes were committed under. It stays
	// effective even when the replica write failed.
	Original ledger.OriginalRef
	// Preview is the bounded text presented in place of the whole result.
	Preview string
	// Segment is the byte-bounded entry a later read reaches the whole original
	// through, on exactly the terms of ledger.SegmentRequest and
	// ledger.SegmentResult: the entry names the byte position the read starts at
	// and the maximum number of bytes one segment returns, and the read answers
	// with the segment bytes, the byte position the next segment resumes from and
	// whether the end of the original was reached. A zero MaxBytes is served as
	// ledger.DefaultSegmentMaxBytes and never as an unbounded read, segments are
	// cut at byte boundaries whatever the content holds and concatenate back to
	// the exact bytes of the original, and a segment that reaches the end reports
	// EOF with a zero Next that carries no resumption point. A line count bounds
	// no byte at all, so the entry carries a byte offset and a byte bound only.
	Segment ledger.SegmentRequest
}
    Externalized is one externalized oversized result: the durable reference
    its bytes were committed under, the bounded preview that stands in for the
    whole result, and the byte-bounded segment entry through which the whole
    original is read back. The ticket a preview carries is a rendering of that
    ledger reference: its vocabulary and the read contract behind it belong to
    memory/ledger, and this package only renders it.

type Externalizer interface {
	// Externalize commits body as the durable original of ref, materialises a replica
	// of it, and returns the bounded preview and the segment entry for it. A replica
	// write that fails is reported as ErrReplicaWriteFailed, while the returned
	// reference stays effective.
	Externalize(ctx context.Context, ref ledger.OriginalRef, body []byte) (Externalized, error)
}

    Externalizer is the oversized-output port: it commits the durable original
    of a result, materialises a replica of it on disk, and returns a bounded
    preview together with the byte-bounded segment entry that reaches the whole
    result.

    The redline list (4 rules): MUST commit the durable original before the
    replica or the preview exists. MUST report a replica write that failed as
    ErrReplicaWriteFailed beside the still-effective reference. MUST NOT serve
    a bounded preview in place of an original that was never kept. MUST NOT
    discard the original a failed materialisation was made from.

type ExternalizerOption func(*ResultExternalizer)
    ExternalizerOption names one setting of the externalize path at construction
    time.

func WithMaxTokens(maxTokens int) ExternalizerOption
    WithMaxTokens sets the model window the threshold is computed from,
    through ThresholdForMaxTokens. A zero or negative value is served as
    DefaultMaxTokens.

func WithPreviewRunes(runes int) ExternalizerOption
    WithPreviewRunes bounds the preview to at most runes leading characters.
    A zero or negative bound keeps DefaultPreviewRunes; either way the preview
    is narrowed to a quarter of the threshold, so it stays well below the bound
    it stands in for.

func WithResultMaterializer(materializer *Materializer) ExternalizerOption
    WithResultMaterializer wires the replica materializer an externalized result
    is materialised through, so the oversized result stands on disk beside the
    original it was committed as. A materializer bound to another read scope
    than the references externalized here is refused by name at externalization,
    before any original is committed.

type Materializer struct {
	// Has unexported fields.
}
    Materializer materialises replicas of durable originals on disk inside
    one trusted read scope: it reads originals only through that scope,
    and it declares a replica path only where the replica bytes were written
    successfully.

    One materialisation target is the pair of a registered reference and the
    consumer it is materialised for. Every target resolves to its own stable
    path derived from those two alone, so the same target lands on the same
    path across process restarts and across owner, tool and session boundaries
    no two targets share a path or overwrite one another. A replica standing
    at its path is evaluated against its original at every materialisation:
    bytes that still answer the effective original are reused as they stand,
    and an original that moved on is shadowed again in place at that evaluation,
    while a read or write already covering those bytes defers the rewrite to a
    later one. A missing replica of an ineffective original is refused by name,
    so the original's validity and the replica's lifetime stay two separate
    facts.

func (m *Materializer) Materialize(ctx context.Context, ref ledger.OriginalRef, consumer string) (Replica, error)
    Materialize materialises one replica of ref for consumer and returns its
    stable reference. A reference outside the bound read scope is refused
    by name and writes no replica, a replica standing at the target path is
    evaluated against its original and answered as it stands or shadowed again
    on the answer, and a missing one is written again from the original while
    that original is effective inside the bound scope. The path is part of the
    returned reference only once the replica bytes were written successfully,
    so a declared path always names a replica that exists whole. The call holds
    active protection on the target until it returns, so no reclamation pass
    takes the replica away across a materialisation.

func (m *Materializer) Open(ctx context.Context, ref ledger.OriginalRef, consumer string) (*ActiveReplica, error)
    Open materialises one working copy of ref for consumer and returns the
    handle that ends its active read/write protection. The protection is
    held before the copy's bytes land and stays held until Close, so every
    reclamation pass leaves the copy alone and every evaluation of its path
    defers a rewrite while the copy is open. A reference outside the bound read
    scope is refused by name and holds nothing, and a materializer wired to no
    cleaner is refused the same way: a working copy is served only where its
    protection exists.

func (m *Materializer) Reader() *ReplicaReader
    Reader returns the authorized segmented read face of this materializer,
    bound to the same trusted read scope and the same replica root.

func (m *Materializer) Rebuildable(ctx context.Context, key event.EventID) (bool, error)
    Rebuildable reports whether a replica of key can be materialised again from
    its original: true while the original is still effective inside the bound
    read scope, false once it is missing or outside its retention window there.
    The answer is presented as such, so a missing replica is never claimed as
    rebuildable once the original behind it is ineffective.

type MaterializerOption func(*Materializer)
    MaterializerOption names one setting of a materializer at construction time.

func WithCleaner(cleaner *ReplicaCleaner) MaterializerOption
    WithCleaner wires the replica cleaner whose active read/write protection
    covers the replicas materialised here: a working copy and a materialisation
    span hold their marks on that cleaner, so every reclamation pass it runs
    leaves those replicas alone. A cleaner that reclaims another replica
    area than the one under this materializer's root is refused by name at
    construction.

type Release func()
    Release frees one active read/write handle acquired by Acquire. Releasing
    twice frees the protection once.

type Replica struct {
	// Original is the durable original this replica was materialised from.
	Original ledger.OriginalRef
	// Path is the on-disk location of the replica, declared once the replica was
	// written successfully.
	Path string
	// CreatedAt is the time the replica was materialised.
	CreatedAt time.Time
	// ActiveRW reports whether the replica is being read or written, so reclamation
	// leaves it alone while it is in use.
	ActiveRW bool
}
    Replica is one materialised copy of a durable original on disk: the original
    it was materialised from, the path it was written to, the time it was
    created, and whether it is being read or written at this moment.

    A replica's life is separate from the original's: reclaiming a replica never
    invalidates the original, and a replica that is missing while its original
    is still effective is materialised again. The path is declared only after
    the replica was written successfully, and a replica's identity is stable
    across the tool and the owner that asked for it, across sessions, and across
    a process restart, so two replicas of the same original never overwrite one
    another.

type ReplicaCleaner struct {
	// Has unexported fields.
}
    ReplicaCleaner is the temporary-replica reclaimer behind the Cleaner port:
    it reclaims the replicas left behind at startup and on the configured
    period, bounded by age and by count, and it leaves every replica an active
    read/write handle holds.

    Reclamation is limited to this workspace's own ownership on disk:
    the replica area under the root the materializer writes into. A user file
    is never touched whatever its name, and no symbolic link is ever followed
    out of the replica root, so a link planted in that area neither hides a user
    file from its owner nor exposes it to reclamation. The age and count bounds
    apply to the replicas no handle holds; a held replica is kept regardless of
    either bound and is not counted against the number kept.

func (c *ReplicaCleaner) Acquire(path string) (Release, error)
    Acquire marks the replica at path as being read or written right now and
    returns the handle that releases it. Every reclamation pass leaves that
    replica alone until the handle is released, so the bytes a reader or writer
    holds are never pulled away underneath them. A path outside the replica
    area is refused by name and protects nothing, and a path where no standing
    regular file exists is refused the same way: a handle holds a replica that
    is there, not one that might appear later.

func (c *ReplicaCleaner) CleanupByAge(ctx context.Context, maxAge time.Duration) error
    CleanupByAge reclaims every replica that has been idle longer than maxAge
    and holds no active read/write handle. Idle time is measured on the
    file itself, so it survives a process restart the way the replica does.
    A negative maxAge is refused by name and reclaims nothing.

func (c *ReplicaCleaner) CleanupByCount(ctx context.Context, maxCount int) error
    CleanupByCount keeps at most maxCount replicas no active read/write handle
    holds, reclaiming the oldest first. A held replica is kept whatever the
    bound and is not counted against the number kept, so the count can never
    reclaim one that is in use. A negative maxCount is refused by name and
    reclaims nothing.

func (c *ReplicaCleaner) CleanupStartup(ctx context.Context) error
    CleanupStartup reclaims the temporary replicas left behind by a previous
    run: every replica in this workspace's own area that no active read/write
    handle holds. A replica reclaimed here is written again from its original
    the next time it is needed, so startup reclamation costs a replica, never an
    original.

func (c *ReplicaCleaner) Run(ctx context.Context) error
    Run reclaims on the configured period: one pass by age and one by count
    every period, until ctx is done or Stop is called. Run reports the end of
    the loop through Wait, so a close sequence confirms the reclaimer ended
    instead of assuming it did. A second Run on a running cleaner is refused by
    name instead of reclaiming over the loop that is already there.

func (c *ReplicaCleaner) Stop()
    Stop ends a running reclamation loop. Stopping a cleaner whose loop is not
    running ends nothing and breaks nothing, so the close sequence can call it
    unconditionally.

func (c *ReplicaCleaner) Wait(ctx context.Context) error
    Wait blocks until a running reclamation loop has stopped, so a close
    sequence confirms the reclaimer ended instead of assuming it did. A cleaner
    whose loop never started or has already ended reports the end immediately;
    a ctx that runs out first reports its own error instead.

type ReplicaReader struct {
	// Has unexported fields.
}
    ReplicaReader is the authorized segmented read face over replicas:
    it serves byte-bounded segments of replica content on exactly the terms of
    ledger.SegmentRequest and ledger.SegmentResult, and only inside one trusted
    read scope.

    Authorization runs through the scoped read face of memory: a request
    outside the bound scope is refused by name before any replica is touched,
    the bytes are reached through the scope the replica was materialised under,
    and a replica that is missing while its original is still effective is
    materialised again before the read is served. The segments of one replica
    concatenate back to its exact bytes, cut at byte boundaries whatever the
    content holds.

func (r *ReplicaReader) ReadSegment(ctx context.Context, ref ledger.OriginalRef, consumer string, req ledger.SegmentRequest) (ledger.SegmentResult, error)
    ReadSegment returns one byte-bounded segment of the replica materialised
    for ref under consumer, on exactly the terms of ledger.SegmentRequest and
    ledger.SegmentResult. A request outside the bound read scope is refused by
    name and serves no byte, a missing replica is materialised again from its
    original before any byte is served, and an ineffective original refuses the
    read by name instead of handing out bytes no replica carries.

func (r *ReplicaReader) Scope() ledger.ReadScope
    Scope returns the trusted read scope the reader serves inside, so a consumer
    can record which authority a result was read under.

type ResultExternalizer struct {
	// Has unexported fields.
}
    ResultExternalizer is the one externalize path of the runtime: it commits
    the original of an oversized result before any preview of that result
    exists, and it answers with a bounded preview and the byte-bounded segment
    entry the whole original is read back through.

    Generic tool results, task results, tmux outputs and overflow-bound
    results all run through this one path, each under its own Format* name,
    so the threshold, the preview form and the commit order are stated once.
    A replica materialised on disk is optional and stays beside the original:
    a failed replica write never discards the original it was made from,
    and a replica reclaimed later is materialised again from that original.

func (e *ResultExternalizer) DecideExternalize(ctx context.Context, _ event.EventID, size int) (bool, error)
    DecideExternalize answers the overflow decision face of the plugin pipeline:
    a result of size characters is externalized exactly when it exceeds the
    one threshold, so an overflowing result takes the same path as every other
    oversized result instead of a policy of its own. The decision reads the size
    alone; the identity the result is keyed by is accepted so every decision
    face of the pipeline takes the same arguments.

func (e *ResultExternalizer) Exceeds(chars int) bool
    Exceeds reports whether a serialized result of the given character count is
    over the threshold and has to take the externalize path. The four producers
    decide here, so no producer carries a bound of its own.

func (e *ResultExternalizer) Externalize(ctx context.Context, ref ledger.OriginalRef, body []byte) (Externalized, error)
    Externalize commits body as the durable original of ref, materialises
    an optional replica of it, and answers with the bounded preview and the
    segment entry for it. The original is committed before the preview exists,
    so a preview is never served in place of an original that was never kept:
    a commit that fails is reported with no preview at all.

    A reference outside the scope the wired materializer reads in is refused
    by name before any original is committed, and a replica write that fails is
    reported as ErrReplicaWriteFailed beside the still effective reference and
    its preview.

func (e *ResultExternalizer) Threshold() int
    Threshold returns the character count above which a result is externalized,
    so a producer can decide on exactly the bound this path commits under.

type ResultScope struct {
	// Owner is the address the result is produced for.
	Owner event.Address
	// Format is the format the result is produced in, one of the Format* constants for
	// the four registered producers.
	Format string
	// RetentionPolicy is the retention policy the original is held under.
	RetentionPolicy string
}
    ResultScope names the reference metadata one producer commits its oversized
    results under: the owner the result is produced for, the format it is
    produced in, and the retention policy its original is held under. It is
    the deduplication scope 03.1 registers references with, so two producers of
    byte-identical results share the blob while their references stay side by
    side.

package memory // import "github.com/SpellingDragon/tagent/memory"

The compress request family: the judgment that material is due for compression,
stated as the fact it is — who judged, why, what was judged and the identity
the judgment is quoted by — and never as the instruction that performs the
compression. A producer files the request it judged, a consumer reads it,
compresses the material the request names and files the product it minted. A
repeated judgment keeps the identity and the first filing moment of the request
already on record, so one body of material carries one request however often it
is judged.

The memory curation pass: the deterministic value assessment, the trigger and
the condensed artifact of one over-limit card sequence. Curation files every
card's fate under exactly one of the three channels — retain, evict or protect
— never a fourth mechanism: an eviction travels the one tombstone channel the
lifecycle already owns, a referenced original is held against that channel and
reads back byte for byte, and no pass ever rewrites, overwrites or migrates
original event bytes or their owner/format attribution. A failing condensation
sinks into the engineering fallback — the overflow run collapses into an
"(earlier n items)" count line — instead of collapsing the pass, so the recall
chain stays whole through every outcome.

The deterministic embedding stand-in of the memory side: it maps text to a
fixed-dimension unit vector with no endpoint behind it, so the vector leg of
a retrieval runs to completion offline and a runtime without an embedding
configuration still exercises the whole retrieval mechanism end to end.
The vectors it carries are content fingerprints and not semantic measurements:
identical text always yields identical vectors and text sharing more character
n-grams scores a higher cosine. That is the mechanism guarantee a retrieval test
stands on, and no claim of semantic recall quality.

The body schema one committed event carries through the segment store: the
canonical identity and the light fields a query filters and matches on, and the
owner, format and retention policy the reference beside it is registered under.
A stored body is self-describing: it decodes back into the query record and the
recall reference, so a committed body becomes queryable and readable without a
second registry. The original text behind the event stays in the original store
and is read byte for byte through the scoped path.

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

The identity to partition mapping of the memory side: one name resolves to one
stable partition ID, so every writer and every authorized read scope that names
it meets the same partition and no two runs disagree.

The pre-fold face of the compression consumers: the fold a consumer runs when
it consumes a compress request, the compaction fact it files into the state, and
the fold identity that fact is quoted by. A pre-fold runs ahead of the request
boundary, so the assembly path reads the folded projection and never folds it.

Cold-start replay: the projection is the pure replay of one fact chain.

The projection holds no state outside the fact chain: a rebuild replays the
committed facts and the compaction events among them, and nothing else.
A compaction payload is restored from the chain exactly as the fold left it
— synthetic refs carry their full text in the payload under a negative key,
positive keys hydrate from the chain — and the tail is paged by minimum event
key and appended in EventKey write order, never by timestamp approximation.

The QueryEvents read face: the time, type and keyword legs one conditional
retrieval runs on, under the query semantics the event-segment-store read port
states. A query is declarative: its result is the whole matching set filtered,
totally ordered and then paged, so no optimisation behind the read face may
change what the query answers.

The offline binding seam of vector generation for recall: an embedder is bound
per generation and borrowed by the async pipeline that runs after the event is
already stored, so the event path never waits for a vector and a runtime without
an embedder configured behaves exactly as it did before the seam existed. The
real embedding endpoint and the recall quality run belong to the resource-gated
acceptance work; this file only carries the seam.

Package memory is the runtime's durable original-text layer: it names one stored
original and the reference metadata that scopes it, commits those bytes and
returns them byte for byte, and carries the ordered projection and the retention
guard built beside them.

The package speaks the fact vocabulary of the event package and nothing above
it, so the durable side cannot be reversed into the intelligent or governing
side. This is the phase 1 skeleton of that layer: the shapes and the contracts
that bind them, together with the scoped read face an authorized read runs
through.

Presentation decisions and budget assembly.

The projection layers the original text, the model view and the presentation
decision: the original is kept apart from what the model sees, and only a
full-text decision makes the two identical. A recorded decision is the basis a
replay follows, so a recorded preview is never promoted back to the full text
and the original is never reconstructed from a decision.

The replay settle family: the presentation decision one assembly settled on,
stated as the fact it is — which entry, which presentation and the text the
decision carries — and quoted by the identity that decision answers. A settled
decision lands one fact; an entry the budget skipped settles nothing and lands
none.

Retention leases.

A retention lease is held by the shared resource owner — the recovery resource
or the unsettled obligation the hold protects, never "an agent" — and protection
is reference counted across holders: it stands until the last holder releases
it, and every hold and release is idempotent. The guard is the single source of
truth for that count; this layer registers what is held and the original time
the key reclassifies under once released, and keeps no second lease system of
its own.

Skeleton compile-time assertions for the phase 1 memory shapes. The phase
2 implementation types that will satisfy the three ports do not exist yet,
so there is no `var _ Iface = (*Impl)(nil)` to write; the assertions below
prove what the skeleton can prove on its own, pinning every declared field name
and type, every declared method signature, and every constant that must stay a
compile-time constant.

The observation table between the segment store and the query face: a committed
event reaches the query face by being decoded and landed here first, and only
the events landed are visible to QueryEvents and to the retrieval built over it.
The table keeps the consumption order of the read path honest — a query consumes
an event after the table observes it, and never at the moment its bytes happen
to be committed.

CONSTANTS

const (
	// CompressProducerCuration names the curation bound judgment.
	CompressProducerCuration = "memory.curation"
	// CompressReasonCuration states why the curation bound judgment files a
	// request.
	CompressReasonCuration = "the card sequence exceeds the curation bound"
)
    CompressProducerCuration names the mechanism that judges a card sequence
    against the curation bound, and CompressReasonCuration states why that
    judgment files a request. The producer and the reason are part of the
    request identity, so the same material judged by two mechanisms stays two
    facts.

const (
	// CurationLinkWeight is the value one standing causal link adds.
	CurationLinkWeight = 10
	// CurationSkeletonWeight is the value a boundary fact (external input or
	// agent output) adds: the task skeleton the condensation keeps.
	CurationSkeletonWeight = 5
	// CurationRecencyWeight is the value a fact inside the recent window adds.
	CurationRecencyWeight = 2
)
    CurationLinkWeight, CurationSkeletonWeight and CurationRecencyWeight are
    the value assessment weights: a standing causal link is worth more than the
    boundary skeleton weight, which is worth more than the recency bonus.

const (
	// RebuildPageSize is the page size the rebuild pages the fact chain with:
	// minimum-event-key paging in EventKey write order.
	RebuildPageSize = 64
	// FallbackEventLimit bounds the fallback replay of a chain that carries no
	// compaction event: the most recent facts are kept and the count left out is
	// reported, so a non-empty chain is never rebuilt as an empty session.
	FallbackEventLimit = 128
)
const (
	QueryOrderByAsc  = "timestamp_asc"
	QueryOrderByDesc = "timestamp_desc"
)
    QueryOrderByAsc and QueryOrderByDesc are the two orderings a query names:
    oldest first and newest first. An empty OrderBy runs as the ascending
    default — matching the read port, where the default walk is old to new —
    and any other value is refused by name with ErrQueryOrder instead of being
    silently coerced into one of the two.

const (
	// ProjectionTypeRecoveryHint is the entry type of the recovery hint an
	// assembly carries first after a cold-start replay, so the model is told the
	// context it sees was rebuilt rather than silently presented.
	ProjectionTypeRecoveryHint = "recovery_hint"
)
const CompactionEventType = event.TypeContextCompress
    CompactionEventType names the frozen event type a compaction payload is
    committed to the fact chain under. A compaction fact is consumed as the
    snapshot it carries and never enters the projection as an entry of its own.

const CompressRequestType = "compress.request"
    CompressRequestType is the frozen type of a compress request fact.

const CurationArtifactType = "memory.condensed_card"
    CurationArtifactType is the frozen type label of the condensed card artifact
    one curation pass leaves behind.

const DefaultHashDimensions = 128
    DefaultHashDimensions is the vector width a HashEmbedder runs at when it
    names none.

const DefaultSegmentMaxBytes int64 = 64 << 10
    DefaultSegmentMaxBytes is the finite bound served when a SegmentRequest
    carries a zero MaxBytes, so a request that states no limit stays bounded
    instead of reading an original whole.

const DefaultVectorQueue = 64
    DefaultVectorQueue is the queue bound a vector seam runs under when its
    caller names none: every seam is bounded, so a slow embedder can never grow
    the queue without limit.

const PartitionBits = 10
    PartitionBits is the width of the partition range one name is folded into:
    the ten bits the segment key layout reserves for partitions.

const RebuildSkipNotEmpty = "projection not empty"
    RebuildSkipNotEmpty is the recorded reason of a rebuild that found a
    non-empty projection: the projection is never replaced by a replay.

const ReplaySettleType = "replay.settle"
    ReplaySettleType is the frozen type of a replay settle fact.

VARIABLES

var (
	// ErrCompressRequestUnnamed reports a judgment that names no producer, no
	// subject or no reason: no identity is derived from it and no consumer could
	// tell what it asks for.
	ErrCompressRequestUnnamed = errors.New("memory: a compress request names no producer, subject or reason")
	// ErrCompressRequestNotPending reports a retirement of a request that does not
	// await consumption: a repeated retirement never retires a second one.
	ErrCompressRequestNotPending = errors.New("memory: no compress request awaits consumption under that identity")
)
var (
	// ErrCurationUnwired reports a curation engine constructed without the
	// retention ledger or the lifecycle it coordinates with.
	ErrCurationUnwired = errors.New("memory: curation needs a retention ledger and a lifecycle")
	// ErrCurationPolicy reports a curation policy that names no sequence
	// bound, so no pass could ever know when to trigger.
	ErrCurationPolicy = errors.New("memory: curation policy names no sequence bound")
	// ErrCurationCardKey reports a card that carries no event key, or a
	// sequence that repeats one: no fate could be filed under it.
	ErrCurationCardKey = errors.New("memory: curation card carries no unique event key")
	// ErrCurationRequestsUnwired reports a consumer tick attempted without the
	// request log it consumes: a tick that cannot read the judgments it answers
	// would compress material no producer judged due.
	ErrCurationRequestsUnwired = errors.New("memory: curation consumer needs the compress request log")
	// ErrCurationCardsUnwired reports a consumer tick attempted without the card
	// sequence source: the tick names no material it could curate.
	ErrCurationCardsUnwired = errors.New("memory: curation consumer needs the card sequence source")
	// ErrCurationProductsUnwired reports a condensed pass whose product has
	// nowhere to be committed: the artifact would be counted as held while no
	// state carries it.
	ErrCurationProductsUnwired = errors.New("memory: curation consumer needs the product sink")
)
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
var (
	// ErrFoldEmpty reports a fold of an empty projection: there is no run to
	// collapse and no compaction payload to leave on the fact chain.
	ErrFoldEmpty = errors.New("memory: fold needs at least one projection entry")
	// ErrPrefoldUnwired reports a pre-fold asked without the projection it
	// folds: no material is named and no compaction fact could be filed.
	ErrPrefoldUnwired = errors.New("memory: pre-fold needs the projection of the material it folds")
	// ErrCurationProjectionUnwired reports a consumer tick pre-folding without
	// the projection source: the tick names no material it could fold.
	ErrCurationProjectionUnwired = errors.New("memory: curation consumer pre-fold needs the projection source")
	// ErrCurationFoldsUnwired reports a pre-fold whose compaction fact has
	// nowhere to be committed: the fold would be counted while no state carries
	// it.
	ErrCurationFoldsUnwired = errors.New("memory: curation consumer pre-fold needs the fold sink")
)
var (
	// ErrChainKeyRequired reports a chain key that names no session, so a replay
	// under it would join one user's facts to a shared partition chain.
	ErrChainKeyRequired = errors.New("memory: chain key names no session")
	// ErrRebuildUnwired reports a rebuild without the target projection, the fact
	// chain or the relation ledger it maintains the causal chain through.
	ErrRebuildUnwired = errors.New("memory: rebuild needs a target projection, a fact chain and a relation ledger")
	// ErrChainPageLimit reports a chain page request that asks for no fact at all.
	ErrChainPageLimit = errors.New("memory: chain page must read at least one fact")
	// ErrChainScan reports a fact chain read that failed, so an empty result is
	// never read as an empty chain.
	ErrChainScan = errors.New("memory: fact chain read failed")
	// ErrCompactionCorrupt reports a compaction payload that cannot be read back
	// as the fold snapshot it claims to carry.
	ErrCompactionCorrupt = errors.New("memory: compaction payload cannot be read")
	// ErrRebuildPartial reports a rebuild that could not hydrate every positive
	// ref of the snapshot from the fact chain. The result it accompanies is
	// partial and is never reported as a byte-exact rebuild.
	ErrRebuildPartial = errors.New("memory: rebuild is partial: the fact chain misses snapshot keys")
)
var ErrBodyKeyMismatch = errors.New("memory: event body key disagrees with the stored event key")
    ErrBodyKeyMismatch reports a committed body whose canonical key disagrees
    with the key it is stored under, so the body cannot stand for that event.

var (
	// ErrBudgetInvalid reports a budget assembly asked to pack a request under a
	// non-positive token budget.
	ErrBudgetInvalid = errors.New("memory: budget assembly needs a positive token budget")
)
var ErrCommitBarrier = errors.New("memory: commit barrier failed")
    ErrCommitBarrier reports a commit whose barrier did not complete, so nothing
    of it is published.

var ErrContentConflict = errors.New("memory: key already names other bytes")
    ErrContentConflict reports a commit whose key already names other bytes:
    one key names one original, so the second content is refused.

var ErrEventBody = errors.New("memory: event body does not decode under the body schema")
    ErrEventBody reports a committed body that does not decode under the body
    schema, so no query record and no reference can be derived from its bytes.

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

var ErrQueryOrder = errors.New("memory: query names an unknown order")
    ErrQueryOrder reports a query that names an order other than the two
    declared ones: the read face would have to invent a third ordering rule to
    answer it, so it is refused by name instead.

var ErrQueryPaging = errors.New("memory: query carries a negative offset or limit")
    ErrQueryPaging reports a query carrying a negative offset or limit, so no
    declarative page could answer it.

var ErrReplayUnwired = errors.New("memory: the replay is not wired to an owner resolver and a delivery sink")
    ErrReplayUnwired reports a replay that is not wired to an owner resolver and
    a delivery sink, so no delivery could be located or admitted.

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

FUNCTIONS

func CompactionFactKey(payload CompactionFact) event.EventID
    CompactionFactKey derives the identity the fold fact of one compaction
    payload is filed under: content-addressed over the summary it reports,
    the boundary it covers and the refs it retains in payload order, so one fold
    files one fact and a re-file keeps that identity.

func PartitionIDFromName(name string) int
    PartitionIDFromName derives the stable partition ID of one name: the FNV-1a
    hash of the name folded into the ten-bit partition range. The same name
    always derives the same ID and the derivation is cached per name.

func RenderProjection(entries []ProjectionEntry) []byte
    RenderProjection renders the projection in its canonical form: one JSON
    line per entry, in order, carrying the entry's identity, type, summary,
    model view content and time. A rebuild that replays the same fact chain
    reproduces these bytes exactly — the byte-for-byte comparison a cold-start
    rebuild is judged by.

func RetainRefType(typ string) bool
    RetainRefType reports whether an entry of the frozen event type is kept as
    a retained ref by a fold instead of being represented by the summary alone:
    the tool and task facts a fold must not lose, while the conversational
    entries are what the summary speaks for.

func SyntheticKey(key event.EventID) bool
    SyntheticKey reports whether a ref key names a synthetic ref, whose full
    text the payload carries, rather than a fact the chain hydrates.

TYPES

type ArchiveOriginals struct {
	// Has unexported fields.
}

    ArchiveOriginals is the original-text face one archive migration writes and
    reads back through: it maps the byte identity and the reference metadata
    a migration carries onto the OriginalStore of the tier it stands over. Its
    method signatures carry built-in and event types only, so the migration's
    original port matches it structurally and no package edge is required in
    either direction.

func NewArchiveOriginals(store OriginalStore) *ArchiveOriginals
    NewArchiveOriginals returns the archive face over one original store.

func (a *ArchiveOriginals) GetOriginal(ctx context.Context, key event.EventID) ([]byte, bool, error)
    GetOriginal returns the exact bytes the store holds under key and whether it
    holds any: a key nothing is registered under reads back as absent beside no
    error, and a failed read reports its cause.

func (a *ArchiveOriginals) PutOriginal(ctx context.Context, key event.EventID, owner event.Address, format, retention, digest string, length int64, body []byte) error
    PutOriginal commits body under key beside the reference metadata the
    migration names, so a repeated import of the same dump lands the same bytes
    under the same reference.

type ChainFact struct {
	// Key is the canonical identity of the fact.
	Key event.EventID
	// Chain is the causal chain the fact belongs to.
	Chain ChainKey
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact, which is not the full text.
	Summary string
	// Content is the original text of the fact.
	Content string
	// Time is the time the fact was recorded.
	Time time.Time
}
    ChainFact is one committed fact as the fact chain replays it: the canonical
    identity and frozen type of the fact, the isolated chain it belongs to,
    the summary stored beside it and the original text it carries. The fields
    carry built-in and event types only, so the storage layers' canonical facts
    map onto them without a package edge.

func CompactionChainFact(chain ChainKey, payload CompactionFact, at time.Time) (ChainFact, error)
    CompactionChainFact returns the state fact one compaction payload is
    committed as: the compaction event a replay reads back, filed under the fold
    identity the payload answers. A payload that cannot be written back as a
    chain fact is reported by name.

type ChainKey struct {
	// PartitionID is the partition the chain's facts are filed under.
	PartitionID int
	// SessionID is the session the chain belongs to. It is part of the isolation
	// key: an empty session is refused by name instead of degrading to a
	// partition-only chain that joins every user of the partition.
	SessionID event.SessionID
}
    ChainKey is the causal chain identity one fact chain is isolated under:
    the partition the facts are filed under beside the session they belong to.
    Two users of one agent name share a partition but never a session, so their
    chains never link into one (see causal-chain-isolation).

type CompactionFact struct {
	// Summary is the summary entry the fold reports first on restore.
	Summary CompactionRef `json:"summary"`
	// Refs are the retained refs of the folded run, in payload order: a
	// synthetic and a positive ref keep the order the payload recorded.
	Refs []CompactionRef `json:"refs"`
	// FullBoundary is the event key the snapshot covers up to. The rebuild pages
	// the tail strictly after it, using this payload value and never a timestamp
	// approximation.
	FullBoundary event.EventID `json:"full_boundary"`
	// MeditationKeys are the meditation keys the snapshot reseeds; the tail's own
	// meditation keys are reseeded beside them from the replayed tail.
	MeditationKeys []event.EventID `json:"meditation_keys,omitempty"`
}
    CompactionFact is the compaction payload one fold leaves on the fact chain:
    the summary entry the fold reports first, the retained refs of the folded
    run in their interleaved payload order, the event-key boundary the snapshot
    covers and the meditation keys it reseeds. The payload is committed to the
    chain as one compaction event, and the compaction event itself never enters
    the projection as an entry.

func Prefold(ctx context.Context, projection *ReplayProjection) (CompactionFact, error)
    Prefold folds the projection a consumer owns ahead of assembly and answers
    the compaction payload the fold left, so the fold fact is filed when
    its request is consumed and the request boundary only reads the folded
    projection. A projection already carrying a compaction payload is answered
    with it: one body of material folds once.

type CompactionRef struct {
	// Key is the ref identity: negative for a synthetic ref, a canonical fact
	// key for a ref the chain hydrates.
	Key event.EventID `json:"key"`
	// Type is the frozen event type of the entry the ref restores.
	Type string `json:"type"`
	// Summary is the stored summary of the entry.
	Summary string `json:"summary"`
	// Content is the full text of a synthetic ref, and is empty for a positive
	// key, which hydrates from the fact chain instead.
	Content string `json:"content,omitempty"`
	// Time is the time the entry was recorded.
	Time time.Time `json:"time"`
}
    CompactionRef is one ref of a compaction payload. A synthetic ref carries
    its full text in the payload under a negative key, while a positive key
    hydrates its entry from the fact chain and carries no text of its own — the
    payload never stands in for the chain.

type CompressRequest struct {
	// ID is the identity of the judgment: the content-addressed key the request
	// is quoted by, derived from the producer, the subject and the reason.
	ID event.EventID
	// Producer names the mechanism that judged the material due for compression.
	Producer string
	// Reason states why that mechanism judged it due.
	Reason string
	// Subject is the identity of the material judged: the content-addressed key
	// of the sequence or result the request names.
	Subject string
	// At is the moment the judgment was filed.
	At time.Time
}
    CompressRequest is one compress request fact: the judgment that material is
    over its bound. It names who judged, why, what was judged and the identity
    the request is quoted by, and it is immutable once filed — the same judgment
    files the same identity, and a retry never refiles it under a second one.

func CompressRequestFor(producer, subject, reason string) CompressRequest
    CompressRequestFor returns the request fact one judgment files: the identity
    is content-addressed over the producer, the subject and the reason, so the
    same judgment always answers the same identity and a re-judgment adds no
    second request. The filing moment stays zero for the log to stamp on first
    filing.

func (r CompressRequest) FactKey() string
    FactKey returns the compress request's identity as a fact key:
    the content-addressed key one judgment is quoted by, unchanged by every
    re-filing of that judgment.

func (r CompressRequest) FactType() string
    FactType returns the frozen type a compress request is read as.

type CompressRequestFiling func(ctx context.Context, request CompressRequest) (CompressRequest, error)
    CompressRequestFiling files one compress request into the log its consumer
    reads and answers the request as it stands on record, so a repeated judgment
    keeps the identity and the filing moment of the first one. A filing that
    fails is reported by name and a judgment is never dropped silently.

type CompressRequests struct {
	// Has unexported fields.
}
    CompressRequests is the in-process log of compress request facts:
    every request filed, the order it was filed in and the ones still awaiting
    consumption. The log is the identity authority of the family — a filed
    request is answered as it stands on record, so one judgment keeps its
    identity and first filing moment — and a request leaves the pending set
    exactly once, when its consumer retires it after the products of its
    compression are committed.

func NewCompressRequests() *CompressRequests
    NewCompressRequests returns an empty request log.

func (l *CompressRequests) Consume(ctx context.Context, key event.EventID) error
    Consume retires one request once the products of its compression are
    committed. A request that does not await consumption is refused by name,
    so a retirement is never counted twice.

func (l *CompressRequests) Pending(ctx context.Context) ([]CompressRequest, error)
    Pending lists the requests awaiting consumption, in filing order, exactly as
    they stand on record.

func (l *CompressRequests) Produce(ctx context.Context, request CompressRequest) (CompressRequest, error)
    Produce files one compress request and answers it as it stands on record:
    a judgment already filed is answered with the recorded request,
    so its identity and filing moment never move and no second entry is added.
    A request naming no producer, subject or reason is refused by name, and the
    filing moment of a request that carries none is stamped on the first filing.

type Condenser func(ctx context.Context, run CurationRun, now time.Time) (string, error)
    Condenser folds one over-limit card run into its condensed summary:
    the seat an LLM curation would take beside the deterministic engineering
    condensation. A condenser that fails degrades the pass to that engineering
    fallback — the overflow run sinks into an "(earlier n items)" count line —
    and never collapses the pass or rewrites a fact.

type Curation struct {
	// Has unexported fields.
}
    Curation is the memory curation engine: it assesses card value
    deterministically, triggers on the sequence bound, condenses the over-limit
    run into a new artifact that relates to the existing facts through its key
    references, holds those references against eviction, and evicts the rest
    through the one tombstone channel the lifecycle owns. The engine rewrites no
    original fact and no reference metadata, whatever the outcome.

    A consumer tick drives the same pass from the compress request facts its
    producers filed: the request is the judgment that material is over its
    bound and the tick is the compression that answers it. A wired pre-fold
    lets the tick fold the compressed projection and commit its compaction fact,
    so the assembly path reads a folded projection.

func NewCuration(policy CurationPolicy, retention *Retention, lifecycle *Lifecycle, condense Condenser, options ...CurationOption) (*Curation, error)
    NewCuration returns the curation engine over the retention ledger and the
    lifecycle it drives: the ledger holds what the artifact references and the
    lifecycle owns the tombstone channel every eviction travels. A nil condenser
    runs the deterministic engineering condensation. The retention ledger must
    be the one the lifecycle was built over, so a reference the pass holds is
    the protection the eviction consults. The options wire the consumer face:
    the request log a tick consumes, the card sequence it curates and the state
    its product facts are committed into.

func (c *Curation) Curate(ctx context.Context, now time.Time, cards []CurationCard) (CurationReport, error)
    Curate runs one curation pass over the card sequence at the given time.

    A sequence within the bound is never curated: every card retains and no
    artifact is produced. A sequence over the bound triggers the pass: the
    over-limit run is the oldest cards beyond the bound, the condensation folds
    that run into the artifact that relates to the existing facts through its
    key references, each referenced key is held against eviction (the referenced
    original is never deleted), the unprotected cards below the value floor are
    evicted through the tombstone channel alone, and everything else retains.
    A failing condensation sinks into the engineering fallback — the overflow
    run collapses into an "(earlier n items)" count line — and a failing channel
    step degrades that card to retention with its failure recorded in the sink
    account, so the pass degrades instead of collapsing and no outcome rewrites
    an original fact.

func (c *Curation) Judge(ctx context.Context, now time.Time, cards []CurationCard) (CompressRequest, bool, error)
    Judge files the request fact the card sequence's bound judgment is:
    a sequence at or within the bound files nothing and answers false, and an
    over-limit one files the request naming the sequence, the curation producer
    and the bound reason, and answers that request with true. The judgment is
    filed under the sequence's content-addressed identity, so judging one body
    of material again files no second request and keeps the first filing moment.
    A sequence no fate could be filed over is refused by name and files nothing.

func (c *Curation) Tick(ctx context.Context, now time.Time) (CurationTick, error)
    Tick runs one curation tick at now: it reads the card sequence it curates
    and the compress requests pending in the request log, and for each pending
    request naming that sequence it runs one pass, commits the condensed
    artifact the pass minted into the state, pre-folds the compressed projection
    once through the wired pre-fold, commits the compaction fact and retires the
    request. A request naming other material stays pending, and a pass, commit,
    pre-fold or retirement that failed keeps its request pending and lands in
    the tick account, so a request is never counted consumed before the products
    of its compression are held.

type CurationArtifact struct {
	// Key is the content-addressed identity of the artifact: the same
	// condensation always mints the same identity.
	Key event.EventID
	// Type is the frozen type label of the artifact.
	Type string
	// Summary is the condensed text of the overflow run, or the count line
	// the degraded pass sank it into.
	Summary string
	// Refs are the key references the condensed result keeps, in value order.
	Refs []event.EventID
	// Sunk is how many card lines the degraded pass sank into the count line.
	Sunk int
	// CreatedAt is the time the pass created the artifact.
	CreatedAt time.Time
}
    CurationArtifact is the condensed card one pass leaves behind:
    the new reference the curation produces, never a rewrite of any fact it
    condenses. Its refs are the [evt_key] tickets of the key cards it keeps,
    so the condensed result stays traceable and recall still refills by key.
    Sunk counts the card lines the pass collapsed into its count line when it
    degraded to the engineering fallback, and is zero while a condensation
    holds.

func (a CurationArtifact) FactKey() string
    FactKey returns the condensed artifact's identity as a fact key:
    the content-addressed key of the card references it carries, so the same
    condensation answers the same key every time it is asked.

func (a CurationArtifact) FactType() string
    FactType returns the frozen type a condensed artifact is read as.

type CurationCard struct {
	// Key is the [evt_key] ticket of the fact the card line carries.
	Key event.EventID
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the small rendering text of the card line.
	Summary string
	// RecordedAt is the time the fact was recorded.
	RecordedAt time.Time
	// Links counts the standing causal links (references and parent keys)
	// pointing at the fact.
	Links int
}
    CurationCard is one card line of the sequence a curation pass runs over:
    the [evt_key] ticket of the fact behind it, its frozen type, the small
    rendering text of the line, the time the fact was recorded, and the count of
    standing causal links — references and parent keys — that point at the fact.

type CurationCards func(ctx context.Context) ([]CurationCard, error)
    CurationCards reads the card sequence one curation tick curates: the cards
    of the partition the tick owns, oldest first as the caller renders them.
    A read that fails is reported by name and a tick never curates a partial
    sequence.

type CurationFate string
    CurationFate names the one channel a card's curation fate is filed under:
    exactly one of retain, evict or protect for every card of a pass. The fates
    are mutually exclusive and exhaustive — a card belongs to one channel and
    never to a fourth — so the eviction a pass declares is the whole eviction it
    performs, and it is performed on the tombstone channel alone.

const (
	// CurationRetain is the fate of a card the pass keeps: the fact stays as
	// it is and takes no part in any eviction.
	CurationRetain CurationFate = "retain"
	// CurationEvict is the fate of a card the pass evicts: the fact is
	// tombstoned through the one eviction channel and stays readable until
	// compaction reclaims it. Eviction never rewrites or destroys bytes
	// directly.
	CurationEvict CurationFate = "evict"
	// CurationProtect is the fate of a card whose original is protected from
	// eviction: the condensed artifact references it or another holder already
	// holds it, so the referenced original is never deleted while that
	// reference stands.
	CurationProtect CurationFate = "protect"
)
type CurationFolds func(ctx context.Context, fact CompactionFact) error
    CurationFolds commits the compaction fact one pre-fold left into the state:
    the fold fact its request is answered with. A commit that fails is reported
    by name, so a fold is never counted as held before the state carries it.

type CurationOption func(*Curation)
    CurationOption names one setting of the curation engine at construction
    time.

func WithCompressRequests(requests *CompressRequests) CurationOption
    WithCompressRequests wires the compress request log the consumer tick reads:
    the requests its producers filed, and the retirement a tick performs once
    the products of one compression are committed.

func WithCurationCards(cards CurationCards) CurationOption
    WithCurationCards wires the card sequence source the consumer tick curates.
    A failed read is reported by name and never read as an empty sequence.

func WithCurationPreFold(projection CurationProjection, folds CurationFolds) CurationOption
    WithCurationPreFold wires the consumer-side fold: the projection a tick
    folds when it consumes a compress request and the state its compaction fact
    is committed into. The request stays pending until the fold fact is held,
    so the assembly path never folds inline.

func WithCurationProducts(products CurationSink) CurationOption
    WithCurationProducts wires the state face the consumer tick commits its
    product facts into: the condensed artifact of every pass that condensed a
    run.

type CurationPolicy struct {
	// MaxCards is the card sequence bound: a sequence of at most this many
	// cards is never curated, and a longer one triggers the pass. A bound
	// below one is refused by name at construction.
	MaxCards int
	// Keep caps how many overflow cards survive as the key references of the
	// condensed artifact, highest value first. A non-positive cap keeps every
	// card that clears the value floor.
	Keep int
	// MinScore is the value floor: an overflow card whose assessment lands
	// below it and that no holder protects is evicted through the tombstone
	// channel. A floor-clearing overflow card the Keep cap cuts stays retained.
	MinScore int
}
    CurationPolicy shapes one curation pass: the sequence bound that triggers
    it, how many key cards survive as artifact references, and the value floor
    below which an unprotected overflow card is evicted.

type CurationProjection func(ctx context.Context) (*ReplayProjection, error)
    CurationProjection reads the replay projection one consumer tick pre-folds:
    the material its compress requests name. A read that fails is reported by
    name and a tick never folds a partial projection.

type CurationReport struct {
	// Triggered reports whether the card sequence ran over the bound and the
	// pass curated it.
	Triggered bool
	// Fates is the one channel each card's fate was filed under. Every card
	// of the sequence carries exactly one fate.
	Fates map[event.EventID]CurationFate
	// Artifact is the condensed card the pass left behind, or nil beside an
	// untriggered sequence.
	Artifact *CurationArtifact
	// Evicted lists the keys the pass evicted, in overflow order: every one
	// of them was tombstoned through the one eviction channel.
	Evicted []event.EventID
	// Failures is the engineering sink account: the condensation and channel
	// failures the pass degraded under, recorded and never swallowed silently.
	Failures []error
}
    CurationReport is what one pass did: whether the sequence bound triggered
    it, the one fate each card was filed under, the condensed artifact it
    left behind, the keys it evicted through the tombstone channel, and the
    engineering sink account holding every failure the pass degraded under.

type CurationRun struct {
	// Overflow is the over-limit card run, oldest first.
	Overflow []CurationCard
	// Keep is the key card set the artifact references, highest value first.
	Keep []CurationCard
}
    CurationRun is the over-limit card run one condensation folds: the cards
    that overflowed the sequence bound, oldest first, and the key cards the pass
    keeps as artifact references beside them.

type CurationSink func(ctx context.Context, artifact CurationArtifact) error
    CurationSink commits the product fact one curation pass minted into the
    state: the condensed artifact, filed under its content-addressed identity.
    A commit that fails is reported by name, so a product is never counted as
    held before the state carries it.

type CurationTick struct {
	// Read is the requests the tick took in: the pending requests naming the card
	// sequence it curates, in filing order.
	Read []CompressRequest
	// Reports is one curation report per request whose pass completed, in the
	// order the requests were read.
	Reports []CurationReport
	// Products is the artifact facts the tick committed into the state, one per
	// condensed pass.
	Products []CurationArtifact
	// Folds is the compaction facts the tick's pre-folds committed into the
	// state, one per folded projection of the tick.
	Folds []CompactionFact
	// Failures is the sink account of the tick: the pass, commit, pre-fold and
	// retirement failures it degraded under, recorded and never swallowed
	// silently.
	Failures []error
}
    CurationTick is what one curation tick did: the request facts it took in,
    the passes they drove, the product facts it committed into the state, the
    fold facts its pre-folds left and the failures it degraded under. A request
    stays pending until the products and the fold fact of its compression are
    committed, so the account names every request whose compression is still
    owed.

type CurationValue struct {
	// Key is the card the assessment describes.
	Key event.EventID
	// Score is the assessed value of the card.
	Score int
}
    CurationValue is the deterministic value assessment of one card: the score
    the pass decides keep and evict against, and nothing else. The assessment
    is a pure function of the card and the pass time, so two identical passes
    always reach identical verdicts and no LLM judgment enters the decision.

func AssessValue(card CurationCard, now time.Time) CurationValue
    AssessValue returns the deterministic value assessment of one card: a pure
    function of its causal link count, its frozen type and its age against the
    pass time. The same inputs always return the same score, so the pass that
    runs twice reaches the same fates.

type DeliveryRecord struct {
	// TaskID is the task the notification belongs to.
	TaskID string
	// Round is the execution round the notification belongs to.
	Round int64
	// Kind is the notification category within the round.
	Kind string
	// Seq is the stable sequence number within the category.
	Seq int64
	// Event is the identity of the committed fact the notification points at.
	Event event.EventID
	// Target is the address the notification is delivered to, frozen at the
	// transition that committed it.
	Target event.Address
	// Source is the address the notification comes from.
	Source event.Address
	// TargetInstance is the runtime instance that receives the notification.
	TargetInstance event.InstanceID
}
    DeliveryRecord is one delivery obligation as the replay surface carries it:
    the stable notice identity beside the delivery routing frozen at the
    transition that committed it. Its fields carry built-in and event types
    only, so the task layer's committed envelopes map onto it without a package
    edge.

type DeliverySink func(ctx context.Context, rec DeliveryRecord) error
    DeliverySink admits one replayed delivery at its frozen target. A nil error
    states the explicit acceptance of the target — the only answer that advances
    the delivery state of that notification — so the engine marks the record
    confirmed behind it and nothing else.

type Embedder interface {
	// Embed returns the embedding vector of text, or a named failure.
	Embed(ctx context.Context, text string) ([]float32, error)
}
    Embedder generates the embedding vector of one text under the binding of
    the generation that borrowed it. A seam never owns the endpoint: it holds
    the binding and calls through it, and a generation's jobs run against the
    binding that was current when the job was submitted, never against one
    swapped in later.

    The redline list (3 rules): MUST generate the vector under the binding of
    the generation that borrowed it. MUST report a named failure rather than a
    zero vector. MUST NOT own an endpoint or run a job against a binding swapped
    in after the job was submitted.

type EventBody struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID `json:"key"`
	// Type is the frozen event type of the event.
	Type string `json:"type"`
	// Summary is the stored summary of the event, the small rendering text.
	Summary string `json:"summary"`
	// Content is the stored content of the event, matched by the keyword leg.
	Content string `json:"content"`
	// Timestamp is the event time in Unix milliseconds.
	Timestamp int64 `json:"timestamp"`
	// Owner is the address the reference of the body is registered for.
	Owner event.Address `json:"owner"`
	// Format is the original format the reference is registered in.
	Format string `json:"format"`
	// RetentionPolicy is the retention policy the reference is held under.
	RetentionPolicy string `json:"retention_policy"`
}
    EventBody is the body schema of one committed event: the fields a query
    filters and orders on together with the reference scope the recall of the
    event runs under.

func BodyFromEvent(ev event.Event, format, retentionPolicy string) (EventBody, error)
    BodyFromEvent derives the body of one event: its canonical key, frozen type,
    summary and content, its event time on the millisecond axis, and the owner,
    format and retention policy the reference beside it is registered under.
    An event carrying no key is refused by name.

func DecodeEventBody(body []byte) (EventBody, error)
    DecodeEventBody decodes one committed body under the body schema, or names
    the refusal for bytes that carry no body of this schema.

func (b EventBody) Encode() ([]byte, error)
    Encode returns the committed bytes of the body under the body schema.

func (b EventBody) Record(partitionID int, windowTS int64) EventRecord
    Record returns the query record of the body under the partition and segment
    window its event is committed into.

func (b EventBody) Ref() OriginalRef
    Ref returns the reference of the body: the key it names and the owner,
    format and retention policy its reference is registered under. The integrity
    stays undeclared here — the original store computes it over the bytes it
    commits, so a derived reference never carries a completeness claim of its
    own.

type EventChain interface {
	// ScanFrom pages the chain: at most limit facts whose key sorts strictly
	// after after (the empty key starts the first page), in EventKey write order.
	// A limit below one is refused by name with ErrChainPageLimit.
	ScanFrom(ctx context.Context, after event.EventID, limit int) ([]ChainFact, error)
	// Fact returns the committed fact under key, and reports whether one is held.
	Fact(ctx context.Context, key event.EventID) (ChainFact, bool, error)
}
    EventChain is the fact chain a projection is replayed from: the committed
    facts in EventKey write order, paged by minimum event key, plus the lookup a
    positive compaction ref hydrates through. A failed read is reported by name
    and is never returned as an empty chain.

    The redline list (3 rules): MUST page the chain in EventKey write order,
    after the given key. MUST refuse a page limit below one by name. MUST NOT
    answer a failed read as an empty chain.

type EventRecord struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID
	// PartitionID is the partition the event is filed under.
	PartitionID int
	// Type is the frozen event type of the event.
	Type string
	// Summary is the stored summary of the event.
	Summary string
	// Content is the stored content of the event, matched by the keyword leg.
	Content string
	// Timestamp is the event time in Unix milliseconds: the one semantic time
	// axis sorting, window filtering and age all read.
	Timestamp int64
	// WindowTS is the time window the record was committed under; a higher
	// window is the higher compaction layer and wins the event key.
	WindowTS int64
}
    EventRecord is one event as the query source holds it: the light reference
    fields a query filters and orders on, the content the keyword leg may match,
    and the segment window the record was committed under.

    The window is the deduplication version marker: where one event key exists
    in two window layers at once — the crash window of a compaction that writes
    its target layer before deleting the source — the record of the higher
    window is the canonical one and the query returns it exactly once.

type EventReference struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID
	// PartitionID is the partition the event is filed under, so a consumer
	// can re-check the attribution a hit claims.
	PartitionID int
	// Type is the frozen event type of the event.
	Type string
	// Summary is the stored summary of the event, which is not the full text.
	Summary string
	// Timestamp is the event time in Unix milliseconds, on the same axis the
	// query window bounds are stated in.
	Timestamp int64
}
    EventReference is the light reference one query answers with: the canonical
    key of the event and the small rendering subset of it. The reference carries
    no content and no tool payload — the text behind a hit stays behind the
    original read path, so a page of references is never a second copy of the
    events it names.

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

type HashEmbedder struct {
	// Has unexported fields.
}
    HashEmbedder is the Embedder that hashes character n-grams into
    a fixed-dimension unit vector. The mapping is deterministic and
    honours cancellation, and the zero value is usable and runs at
    DefaultHashDimensions.

func NewHashEmbedder(dimensions int) *HashEmbedder
    NewHashEmbedder returns a hash embedder of the given vector width, or one at
    DefaultHashDimensions when the width names none.

func (e *HashEmbedder) Embed(ctx context.Context, text string) ([]float32, error)
    Embed returns the deterministic embedding vector of text: a unit vector over
    the character n-gram fingerprint of the text, identical for identical text.

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

type MemoryEventChain struct {
	// Has unexported fields.
}
    MemoryEventChain is the in-memory fact chain: the reference implementation
    of the replay source, holding only what the process holds.

func NewMemoryEventChain() *MemoryEventChain
    NewMemoryEventChain returns an empty in-memory fact chain.

func (c *MemoryEventChain) Commit(ctx context.Context, fact ChainFact) error
    Commit stores one fact of the chain. A repeat of the committed fact is
    idempotent, and other content under the same key is refused.

func (c *MemoryEventChain) Fact(ctx context.Context, key event.EventID) (ChainFact, bool, error)
    Fact implements EventChain.Fact.

func (c *MemoryEventChain) ScanFrom(ctx context.Context, after event.EventID, limit int) ([]ChainFact, error)
    ScanFrom implements EventChain.ScanFrom: the chain is paged in EventKey
    write order, which the canonical key encodes.

type MemoryEventQuery struct {
	// Has unexported fields.
}
    MemoryEventQuery is the in-process query source over recorded events. It is
    the deterministic reference implementation of the read-port query semantics;
    it keeps no durability promise of its own — a new instance answers only
    the records stored into it, and nothing it holds survives a process restart
    unless a durable tier stores and reloads them.

func NewMemoryEventQuery() *MemoryEventQuery
    NewMemoryEventQuery returns an empty query source.

func (q *MemoryEventQuery) QueryEvents(ctx context.Context, opts QueryOptions) ([]EventReference, error)
    QueryEvents returns the references matching opts on exactly the declarative
    terms of QueryOptions: the matching set is filtered first (partition scope,
    time window, event types, keyword), then totally ordered by (Timestamp,
    Key) under the named direction, then paged by Offset and Limit.

    The total order is the tie-breaker contract: events sharing one millisecond
    are ordered by their event key, and the descending order reverses both
    keys together, so any two runs of the same query return the same sequence
    position by position.

func (q *MemoryEventQuery) Store(ctx context.Context, rec EventRecord) error
    Store records one event. A repeat of the same event key keeps the record of
    the higher window — the higher compaction layer is canonical — and a repeat
    at the same window is an idempotent no-op, so a crash window that replays a
    commit never yields two records for one event key.

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

type MemoryRelations struct {
	// Has unexported fields.
}
    MemoryRelations is the causal parent ledger between committed facts:
    the parent of one fact is set and read back here, never on the fact payload.
    The replay maintains these links inside one (PartitionID, SessionID) chain,
    so a walk never crosses from one session's chain into another's.

    The ledger is the one relation surface a rebuild writes through: it is the
    only implementation, so the rebuild names it directly rather than declaring
    a port with no second implementer behind it.

func NewMemoryRelations() *MemoryRelations
    NewMemoryRelations returns an empty in-memory relation store.

func (r *MemoryRelations) GetParent(child event.EventID) (event.EventID, error)
    GetParent returns the parent of child, empty when there is none.

func (r *MemoryRelations) SetParent(child event.EventID, parent event.EventID) error
    SetParent sets the parent of child, or clears it when parent is empty.

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
	// missing/expired/failed classification (see RecallResult). Wiring Get to
	// RecallResult gives the classification a real consumer (review gate P2-2).
	//
	// Migration form: a bare key is not a read authorization (see ReadScope),
	// and the visibility decision here aggregates every reference of the key
	// whatever scope it was registered under. Authorized reads run through
	// ScopedOriginalStore and ScopedReader, where the trusted scope the read
	// runs inside decides which references take part.
	Get(ctx context.Context, key event.EventID) RecallResult

	// ReadSegment returns at most numLines lines of the original stored under
	// key starting at line startLine, so an oversized original is consumed by
	// segment rather than returned whole.
	//
	// Migration form: this bounds lines, not bytes — one oversized line is
	// returned whole whatever numLines is, so it is no byte bound at all. The
	// byte-bounded face is SegmentRequest and SegmentResult, served by
	// ScopedReader.ReadSegment and ScopedOriginalStore.ReadSegmentScoped, which
	// cut at byte boundaries and never at line boundaries.
	ReadSegment(ctx context.Context, key event.EventID, startLine, numLines int) ([]byte, error)
}
    OriginalStore is the original-text port: it commits the bytes a reference
    names and returns them byte for byte, whole or as segments.

    The redline list (4 rules): MUST commit the bytes a reference names before
    any bounded preview or segment entry exists. MUST return the original byte
    for byte, whole or as segments. MUST NOT answer a read with a model view in
    place of the original. MUST NOT invalidate an accepted reference by a later
    Put.

type OwnerLookup func(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error)
    OwnerLookup resolves the owner a delivery's target belongs to under the
    current orchestration binding. It is the same function seam the delivery
    sink beside it is: one operation, supplied per call, with nothing captured
    from an earlier run. Its signature carries built-in and event types only,
    so the state layer's binding owners satisfy it structurally and no package
    edge is required in either direction. A target the current binding refuses
    is reported by name through the error.

type PresentationDecision struct {
	// Key identifies the entry the decision was recorded for.
	Key event.EventID
	// Mode is the presentation chosen for the entry.
	Mode PresentationMode
	// Preview is the bounded text shown when Mode is not PresentationFull, and
	// is empty for a full-text decision.
	Preview string
}
    PresentationDecision is the recorded choice of how one projection entry was
    presented to the model, and the basis a replay reproduces.

    The recorded decision is what a replay follows, so a recorded preview is
    never promoted to the full original and the original is never reconstructed
    from the decision.

type PresentationMode int
    PresentationMode names how one projection entry is presented to the model.

const (
	// PresentationUnrecorded is the zero value: no decision was recorded for the
	// entry, so it is never presented as if one had been.
	PresentationUnrecorded PresentationMode = iota
	// PresentationFull presents the full original text of the entry.
	PresentationFull
	// PresentationSummary presents the stored summary in place of the original.
	PresentationSummary
	// PresentationTicket presents a bounded preview together with the ticket
	// that reaches the original by segment.
	PresentationTicket
)
type ProjectionEntry struct {
	// Key is the canonical identity of the fact, in its hex form.
	Key event.EventID
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact, which is not the full text.
	Summary string
	// Content is what the model view carries for this entry: the full original
	// when the recorded decision is full text, and otherwise the bounded preview
	// or ticket that decision recorded.
	Content string
	// Time is the time the fact was recorded.
	Time time.Time
}
    ProjectionEntry is one ordered item of a projection: the canonical identity
    of a committed fact, its type and summary, the content presented for it,
    and its time, in the same shape a recall card entry uses.

type ProtectionLease string
    ProtectionLease is the opaque token one accepted hold returns and its
    release must carry back. A lease is fresh per hold: a retry of a hold that
    still stands gets the same token, while a hold made after an earlier release
    gets a new one, so a late release carrying the earlier token matches nothing
    and releases nothing. Callers treat a lease as opaque: they never construct
    one, and its encoding is not a contract.

type QueryOptions struct {
	// PartitionID filters to one partition; zero means it names none.
	PartitionID int
	// PartitionIDs filters to the given partitions and takes precedence over
	// PartitionID. Both fields empty means no partition is scanned.
	PartitionIDs []int
	// EventTypes restricts the query to the given frozen event types; empty
	// means every type.
	EventTypes []string
	// StartTime is the inclusive lower time bound in Unix milliseconds; zero
	// leaves the lower side open.
	StartTime int64
	// EndTime is the inclusive upper time bound in Unix milliseconds; zero
	// leaves the upper side open.
	EndTime int64
	// Limit is the page bound: zero means the page is unbounded, never that
	// the query is refused.
	Limit int
	// Offset skips that many events of the totally ordered result before the
	// page is cut.
	Offset int
	// OrderBy names the ordering, one of QueryOrderByAsc and
	// QueryOrderByDesc; empty runs as the ascending default.
	OrderBy string
	// Keyword matches the stored summary or content case-insensitively;
	// empty narrows nothing.
	Keyword string
}
    QueryOptions is the filter one event query runs under: the partitions it may
    scan, the time window in Unix milliseconds, the event types and the keyword
    it narrows to, and the page it asks for. The time axis is milliseconds —
    the same axis as the event time and the recall tool documentation — and
    both bounds are inclusive of the events recorded at the bound itself;
    a zero bound leaves that side open. StartTime and EndTime are never compared
    against second-granularity window boundaries.

    The partition scope is default-nothing, not a fallback to the whole store:
    when PartitionIDs and PartitionID both name nothing, the query scans no
    partition at all and answers an empty result rather than every event, and a
    caller authorized to read partitions receives them injected here. Keyword
    is case-insensitive and matches the stored summary or the stored content;
    an empty keyword narrows nothing. EventTypes empty means every type.
    Limit is the page bound where zero means unbounded, and Offset skips that
    many events of the ordered result.

func (o QueryOptions) Validate() error
    Validate returns the named refusal that rejects this query before it runs,
    or nil when the query may run: an order that is neither declared ordering,
    or a negative offset or limit.

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

type RebuildOptions struct {
	// PageSize is the minimum-event-key page size of the tail pagination; zero
	// means RebuildPageSize.
	PageSize int
	// FallbackBound bounds the fallback replay of a chain without compaction;
	// zero means FallbackEventLimit.
	FallbackBound int
}
    RebuildOptions shapes one cold-start replay.

type RebuildReport struct {
	// Entries is the number of entries the rebuild appended.
	Entries int
	// Fallback reports a chain without compaction, replayed under the bound.
	Fallback bool
	// Dropped is the count of chain facts the bounded fallback left out.
	Dropped int
	// Scanned reports a completed chain scan: an empty chain is a scanned empty
	// chain and is never confused with a failed read.
	Scanned bool
	// Skipped is the recorded reason no rebuild ran, empty when one did.
	Skipped string
	// Partial reports snapshot keys the chain could not hydrate.
	Partial bool
	// Missing lists the snapshot keys the chain could not hydrate.
	Missing []event.EventID
	// Meditation lists the meditation keys reseeded, from the snapshot and the
	// tail together.
	Meditation []event.EventID
	// FullBoundary is the boundary value the compaction payload carried.
	FullBoundary event.EventID
}
    RebuildReport is the structured result of one cold-start replay: what the
    rebuild appended, whether it ran the bounded fallback, and — for a partial
    result that carries a non-nil error — the snapshot keys the chain could not
    hydrate, so a partial rebuild is never reported as byte-exact.

func RebuildProjectionFromWAL(ctx context.Context, target *ReplayProjection, chain EventChain, relations *MemoryRelations, session ChainKey, opts RebuildOptions) (RebuildReport, error)
    RebuildProjectionFromWAL replays one fact chain into an empty projection:
    the cold-start rebuild that runs before any other recovery replay.
    A non-empty projection is never replaced and the skip reason is recorded.
    With a valid compaction the snapshot is restored first — the summary entry
    first, then the retained refs in payload order, positive keys hydrated from
    the chain and synthetic refs from the payload text — and the tail is paged
    by minimum event key and appended in EventKey write order after the payload
    boundary. Without a compaction the rebuild runs the bounded fallback instead
    of a no-op, and a successfully scanned empty chain is reported as empty.

    The causal chain is maintained through relations.SetParent inside this
    (PartitionID, SessionID) chain alone: a replay never links one session's
    facts into another's, and an empty session is refused by name.

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

type Replay struct {
	// Has unexported fields.
}
    Replay is the unconfirmed-delivery replay engine: it re-delivers the
    obligations a committed record still owes, locating each delivery's
    owner under the current orchestration binding before it admits anything.
    A delivery whose owner resolution is refused keeps its obligation,
    is re-routed nowhere and is never reported as delivered; only a sink that
    answers with explicit acceptance marks a record confirmed.

    The engine holds no wrapper, binding or lease: the resolver and the sink are
    supplied per call and nothing an earlier run captured is revived through it.
    The engine persists nothing of its own, so a replay reuses the committed
    task records and introduces no new orchestration persistence format.

func NewReplay() *Replay
    NewReplay returns an empty replay engine. A fresh engine confirms
    nothing yet, so every obligation it is handed is replayed; the targets it
    re-delivers to accept idempotently by the notification's stable identity,
    so a repeated replay records no second fact.

func (r *Replay) Ack(rec DeliveryRecord)
    Ack records the explicit acceptance the target stated for one delivered
    record, so a later replay of the same identity delivers nothing again.

func (r *Replay) Confirmed(rec DeliveryRecord) bool
    Confirmed reports whether the target explicitly accepted one delivery.

func (r *Replay) Replay(ctx context.Context, pending []DeliveryRecord, owners OwnerLookup, sink DeliverySink) (ReplayReport, error)
    Replay re-delivers every unconfirmed delivery in pending. Each delivery
    is located first: its target's owner is resolved under the current
    orchestration binding, and a resolution refused by name keeps the obligation
    open and delivers nothing — the delivery is never faked as a success and
    never silently re-routed to another target. Only a sink that answers with
    explicit acceptance marks the record confirmed.

type ReplayOutcome struct {
	// Delivery is the obligation the outcome is about.
	Delivery DeliveryRecord
	// Owner is the owner the current binding resolved the delivery's target
	// to; it is empty when the resolution refused.
	Owner event.AgentID
	// Delivered reports whether the target explicitly accepted the delivery.
	Delivered bool
	// Skipped reports a delivery an earlier acceptance already confirmed, so
	// this replay delivered it nothing a second time.
	Skipped bool
	// Refused is the named refusal of the delivery: the owner resolution or
	// the target refused it. The obligation stays open either way.
	Refused error
}
    ReplayOutcome is the per-delivery result of one replay: the owner the
    current binding resolved the target to, and the named refusal of a delivery
    that was not admitted.

type ReplayProjection struct {
	// Has unexported fields.
}

    ReplayProjection is the replayable projection: the ordered model view of
    one fact chain, the recorded presentation decisions that view was assembled
    under, and the compaction payload a fold left on the chain. It is what
    RebuildProjectionFromWAL replays into.

    A projection holds no state outside the fact chain, so replaying the
    recorded decisions reproduces the same view. The entries hold the canonical
    form (the original text of each keyed fact), while a recorded decision
    shapes what an assembly presents, and the layering is never collapsed.
    Constraints:

      - a keyed entry already present is appended once, and a keyless entry
        always is;
      - the original text, the model view and the presentation decision stay
        layered;
      - a rebuild recomputes the deduplication key set from the entries it
        keeps.

func NewReplayProjection() *ReplayProjection
    NewReplayProjection returns an empty replayable projection.

func (p *ReplayProjection) Append(ctx context.Context, entry ProjectionEntry)
    Append adds the canonical entry of one committed fact: a keyed entry already
    present is appended once, a keyless entry always is. The original text is
    kept apart from the model view, and a recorded decision shapes the view —
    never the original.

func (p *ReplayProjection) AssembleBudget(ctx context.Context, budget int) ([]ProjectionEntry, error)
    AssembleBudget packs the projection into one agent request of the given
    token budget and returns the entries the request is assembled from, recovery
    hint first when one applies. A recorded decision is followed unconditionally
    — the view a replay reproduces never drifts with the remaining budget —
    while an unrecorded entry degrades from the full text to a ticket and then
    to the summary until the budget is met, settling on the decision it recorded
    as one replay-settle fact. An entry that fits none of them is skipped
    without a record and without a settle fact, so a later assembly under a
    larger budget can still present it in full.

    The source facts are read and never written: the assembly files settle facts
    on the projection's own settle account, so the fact chain it replayed is
    exactly as it stood.

func (p *ReplayProjection) Compaction() *CompactionFact
    Compaction returns the compaction payload the last fold left, nil before
    any.

func (p *ReplayProjection) Decision(key event.EventID) (PresentationDecision, bool)
    Decision returns the recorded presentation decision of one key.

func (p *ReplayProjection) Entries() []ProjectionEntry
    Entries returns a copy of the projection entries in their canonical form.

func (p *ReplayProjection) Fold(ctx context.Context) (ProjectionEntry, error)
    Fold collapses the foldable run of the projection into one compaction entry
    and returns its summary entry, so the collapsed text is reported as a fact
    of the chain rather than kept beside it.

    The retained refs keep the payload order: a keyless tool-chain entry becomes
    a synthetic ref carrying its full text in the payload, a keyed entry of
    a retained type becomes a positive ref the fact chain hydrates back, and
    the remaining entries are represented by the summary alone. The compaction
    payload records the fullBoundary value the tail is replayed after, and the
    summary entry is returned first on every restore.

func (p *ReplayProjection) FullBoundary() event.EventID
    FullBoundary returns the event-key boundary the compaction payload carried.

func (p *ReplayProjection) Len() int
    Len returns how many entries the projection holds.

func (p *ReplayProjection) MeditationKeys() []event.EventID
    MeditationKeys returns the reseeded meditation keys in order.

func (p *ReplayProjection) Original(key event.EventID) (string, bool)
    Original returns the kept original text of one keyed entry.

func (p *ReplayProjection) Rebuild(ctx context.Context, decisions []PresentationDecision) ([]ProjectionEntry, error)
    Rebuild replaces the whole projection with the one the recorded decisions
    describe and recomputes the deduplication key set from the entries it keeps.
    Decisions passed in are recorded first-time-wins: a decision already
    recorded for a key is never upgraded by a later one, so a recorded preview
    is never promoted back to the full text.

func (p *ReplayProjection) RecordDecision(key event.EventID, decision PresentationDecision)
    RecordDecision records the presentation decision of one entry and reshapes
    its model view accordingly. First time wins: a decision already recorded
    is never upgraded, so a recorded preview is never promoted back to the full
    text.

func (p *ReplayProjection) RecoveryHint() *ProjectionEntry
    RecoveryHint returns the recovery hint entry a rebuild left, nil before one.

func (p *ReplayProjection) Settles() []ReplaySettle
    Settles returns a copy of the replay-settle facts the assembly recorded,
    in settlement order: one fact per decision the assembly settled on, and none
    for an entry it skipped.

type ReplayReport struct {
	// Outcomes lists the per-delivery outcomes of the replay.
	Outcomes []ReplayOutcome
}
    ReplayReport is the report one replay returns, one outcome per delivery,
    in the order the pending obligations were handed in.

type ReplaySettle struct {
	// ID is the identity of the settled decision: the content-addressed key the
	// fact is quoted by, derived from the entry, the mode and the preview.
	ID event.EventID
	// Entry is the entry the assembly settled.
	Entry event.EventID
	// Mode is the presentation the assembly settled on.
	Mode PresentationMode
	// Preview is the bounded text the settled presentation carries, empty for a
	// full-text settlement.
	Preview string
	// At is the moment the settlement was recorded.
	At time.Time
}
    ReplaySettle is the fact one presentation decision is filed as: the entry
    the assembly settled, the presentation it settled on, the bounded text
    that presentation carries and the moment the settlement was recorded.
    The identity is the decision's own, so the same decision always answers the
    same fact.

func ReplaySettleFor(decision PresentationDecision) ReplaySettle
    ReplaySettleFor returns the settle fact one presentation decision is
    filed as: the identity is content-addressed over the entry, the mode and
    the preview, so the same decision always answers the same identity and a
    repeated settlement adds no second fact. The recording moment stays zero for
    the caller to stamp.

func (s ReplaySettle) FactKey() string
    FactKey returns the settle fact's identity: the decision identity the fact
    is quoted by, unchanged by the moment it was recorded.

func (s ReplaySettle) FactType() string
    FactType returns the frozen type a settle fact is read as.

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

func (r *Retention) Holders(key event.EventID) []HolderID
    Holders returns the holders still standing on one key, in order.

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
const RetentionCuration RetentionKind = "curation_reference"
    RetentionCuration is the kind of a retention registration protecting
    material a condensed artifact still references: the hold a curation pass
    takes on each key its artifact cites, so the referenced original is never
    tombstoned or reclaimed while the artifact can still recall it by key.

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

type StoreQuerySource struct {
	// Has unexported fields.
}
    StoreQuerySource is the query source over the observation table: it answers
    QueryEvents on exactly the query semantics of the memory read face, over the
    events landed from the committed store rather than over records a caller
    assembled by hand.

func NewStoreQuerySource() *StoreQuerySource
    NewStoreQuerySource returns an empty observation table.

func (s *StoreQuerySource) LandEvent(ctx context.Context, stored StoredEvent) error
    LandEvent decodes one committed event under the body schema and lands it in
    the table: the record a query reads and the reference the recall path reads
    the original under. A body whose canonical key disagrees with the stored key
    is refused by name. A repeat of one event key keeps the record of the higher
    window, and a repeat at the same window is an idempotent no-op.

func (s *StoreQuerySource) ObserveStore(ctx context.Context, store *FileSegmentStore) error
    ObserveStore lands every event the store has published into the table. An
    already landed event keeps its record and only the events not yet landed are
    decoded and added, so observing twice over unchanged state moves nothing.

func (s *StoreQuerySource) Observed(key event.EventID) bool
    Observed reports whether the table holds the event key.

func (s *StoreQuerySource) ObservedCount() int
    ObservedCount returns how many event keys the table holds.

func (s *StoreQuerySource) QueryEvents(ctx context.Context, opts QueryOptions) ([]EventReference, error)
    QueryEvents returns the references matching opts over the landed events,
    on exactly the declarative terms of QueryOptions. An event the table has
    not landed takes no part in the answer, whatever the store behind the table
    holds.

func (s *StoreQuerySource) Reference(key event.EventID) (OriginalRef, bool)
    Reference returns the recall reference the event key was landed with,
    and reports whether the table holds one.

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

    TierFacts is the durable fact surface of one opened tier: put, get,
    prefix scan and the sync barrier a commit reports its success behind.
    It commits through the same key-value backend the tier's segment store uses,
    so the replay and the durable task commit share one database with the
    events already stored there. Its signatures carry built-in types only, so
    it matches the state layer's fact-log port structurally and no package edge
    is required in either direction — the port is declared where it is consumed
    (see state's FactLog) and this tier names the one surface it offers.

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

type VectorSeam struct {
	// Has unexported fields.
}
    VectorSeam is the async vector generation pipeline beside the event store:
    submitting an event never waits for its vector, the vector becomes ready
    later under the binding that generation borrowed, and an unconfigured seam
    is a no-op rather than a degraded behaviour.

    The seam holds vectors only. It never touches event originals, tool
    declarations or any governing counter, so vector generation cannot erode
    what it runs beside.

func NewVectorSeam(queueSize int) *VectorSeam
    NewVectorSeam returns a seam with the given queue bound (a non-positive
    bound runs under DefaultVectorQueue) and no embedder bound: an unconfigured
    seam accepts nothing and changes nothing.

func (s *VectorSeam) Bind(embedder Embedder)
    Bind installs the embedder binding of the current generation, or clears it
    when nil: jobs already submitted keep the binding they borrowed, so a later
    generation can never rewrite the terms an earlier one runs under.

func (s *VectorSeam) Close()
    Close ends the pipeline: no further submission is accepted and the worker
    returns. It repeats as a no-op.

func (s *VectorSeam) Dropped() int64
    Dropped returns how many event vectors were dropped so far: refused by a
    full queue or lost to a failed generation. A negative result is recorded,
    not hidden.

func (s *VectorSeam) Submit(key event.EventID, text string) bool
    Submit offers one event for async vector generation and returns whether the
    job was accepted.

    - An unconfigured seam (no embedder bound) accepts nothing and moves
    nothing: no job is queued, no embedder is called and the drop counter
    stays where it is, so a runtime without embeddings behaves exactly as it
    does without the seam. - A full queue drops the job by name and moves the
    drop counter, and the event itself is stored regardless: submission is
    non-blocking by contract, never a back-pressure wait on the store path.
    - A job whose generation later fails is dropped the same observable way,
    so a lost vector is always counted and never silently absent.

func (s *VectorSeam) Vector(key event.EventID) ([]float32, bool)
    Vector returns the embedding vector generated for key and whether it is
    ready. A vector is never served before its generation completed, and its
    absence is reported as absence rather than as an error.

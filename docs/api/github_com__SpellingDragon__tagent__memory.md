package memory // import "github.com/SpellingDragon/tagent/memory"

The filing surface of the compress request family: the producer and the reason
one mechanism states when it judges material due for compression, and the
in-process log a filed request waits in until its consumer retires it. The fact
itself — who judged, why, what was judged, and the identity the judgment is
quoted by — is the event package's vocabulary; this file keeps what the memory
side owns: the judgment the meditation bound states and the log that holds it.

Package memory is the runtime's durable original-text layer: it names one stored
original and the reference metadata that scopes it, commits those bytes and
returns them byte for byte, and carries the ordered projection and the retention
guard built beside them.

The package speaks the fact vocabulary of the event package and nothing above
it, so the durable side cannot be reversed into the intelligent or governing
side. The storage unit itself lives in memory/ledger and the replay unit in
memory/replay; this package keeps the read face (queries, segment reads),
the projection and the assembly seam over them.

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

The identity to partition mapping of the memory side: one name resolves to one
stable partition ID, so every writer and every authorized read scope that names
it meets the same partition and no two runs disagree.

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

The unconfirmed-delivery replay engine: the obligations a committed record still
owes, re-delivered at the owner the current orchestration binding resolves. The
durable fact surface beside it lives with the storage tiers in memory/ledger.

Skeleton compile-time assertions for the read face of the memory package:
the delivery replay engine, the compress request family and the archive
original-text adapter. The storage shapes are pinned beside their declarations
in memory/ledger, the projection shapes in memory/replay and the meditation
shapes in evolution/meditation; this file pins what this package itself
declares, so a renamed field or a widened signature stops it from compiling.

The observation table between the segment store and the query face: a committed
event reaches the query face by being decoded and landed here first, and only
the events landed are visible to QueryEvents and to the retrieval built over it.
The table keeps the consumption order of the read path honest — a query consumes
an event after the table observes it, and never at the moment its bytes happen
to be committed.

CONSTANTS

const (
	// CompressProducerMeditation names the meditation bound judgment.
	CompressProducerMeditation = "memory.curation"
	// CompressReasonMeditation states why the meditation bound judgment files a
	// request.
	CompressReasonMeditation = "the card sequence exceeds the curation bound"
)
    CompressProducerMeditation names the mechanism that judges a card sequence
    against the meditation bound, and CompressReasonMeditation states why
    that judgment files a request. The producer and the reason are part of the
    request identity, so the same material judged by two mechanisms stays two
    facts.

const (
	QueryOrderByAsc  = "timestamp_asc"
	QueryOrderByDesc = "timestamp_desc"
)
    QueryOrderByAsc and QueryOrderByDesc are the two orderings a query names:
    oldest first and newest first. An empty OrderBy runs as the ascending
    default — matching the read port, where the default walk is old to new —
    and any other value is refused by name with ErrQueryOrder instead of being
    silently coerced into one of the two.

const DefaultHashDimensions = 128
    DefaultHashDimensions is the vector width a HashEmbedder runs at when it
    names none.

const DefaultVectorQueue = 64
    DefaultVectorQueue is the queue bound a vector seam runs under when its
    caller names none: every seam is bounded, so a slow embedder can never grow
    the queue without limit.

const PartitionBits = 10
    PartitionBits is the width of the partition range one name is folded into:
    the ten bits the segment key layout reserves for partitions.

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
var ErrBodyKeyMismatch = errors.New("memory: event body key disagrees with the stored event key")
    ErrBodyKeyMismatch reports a committed body whose canonical key disagrees
    with the key it is stored under, so the body cannot stand for that event.

var ErrEventBody = errors.New("memory: event body does not decode under the body schema")
    ErrEventBody reports a committed body that does not decode under the body
    schema, so no query record and no reference can be derived from its bytes.

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

FUNCTIONS

func PartitionIDFromName(name string) int
    PartitionIDFromName derives the stable partition ID of one name: the FNV-1a
    hash of the name folded into the ten-bit partition range. The same name
    always derives the same ID and the derivation is cached per name.

TYPES

type ArchiveOriginals struct {
	// Has unexported fields.
}

    ArchiveOriginals is the original-text face one archive migration writes
    and reads back through: it maps the byte identity and the reference
    metadata a migration carries onto the ledger.OriginalStore of the tier it
    stands over. Its method signatures carry built-in and event types only,
    so the migration's original port matches it structurally and no package edge
    is required in either direction.

func NewArchiveOriginals(store ledger.OriginalStore) *ArchiveOriginals
    NewArchiveOriginals returns the archive face over one original store.

func (a *ArchiveOriginals) GetOriginal(ctx context.Context, key event.EventID) ([]byte, bool, error)
    GetOriginal returns the exact bytes the store holds under key and whether it
    holds any: a key nothing is registered under reads back as absent beside no
    error, and a failed read reports its cause.

func (a *ArchiveOriginals) PutOriginal(ctx context.Context, key event.EventID, owner event.Address, format, retention, digest string, length int64, body []byte) error
    PutOriginal commits body under key beside the reference metadata the
    migration names, so a repeated import of the same dump lands the same bytes
    under the same reference.

type CompressRequestFiling func(ctx context.Context, request event.CompressRequest) (event.CompressRequest, error)
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

func (l *CompressRequests) Pending(ctx context.Context) ([]event.CompressRequest, error)
    Pending lists the requests awaiting consumption, in filing order, exactly as
    they stand on record.

func (l *CompressRequests) Produce(ctx context.Context, request event.CompressRequest) (event.CompressRequest, error)
    Produce files one compress request and answers it as it stands on record:
    a judgment already filed is answered with the recorded request,
    so its identity and filing moment never move and no second entry is added.
    A request naming no producer, subject or reason is refused by name, and the
    filing moment of a request that carries none is stamped on the first filing.

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

func (b EventBody) Ref() ledger.OriginalRef
    Ref returns the reference of the body: the key it names and the owner,
    format and retention policy its reference is registered under. The integrity
    stays undeclared here — the original store computes it over the bytes it
    commits, so a derived reference never carries a completeness claim of its
    own.

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

type OwnerLookup func(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error)
    OwnerLookup resolves the owner a delivery's target belongs to under the
    current orchestration binding. It is the same function seam the delivery
    sink beside it is: one operation, supplied per call, with nothing captured
    from an earlier run. Its signature carries built-in and event types only,
    so the state layer's binding owners satisfy it structurally and no package
    edge is required in either direction. A target the current binding refuses
    is reported by name through the error.

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

type ReplayReport struct {
	// Outcomes lists the per-delivery outcomes of the replay.
	Outcomes []ReplayOutcome
}
    ReplayReport is the report one replay returns, one outcome per delivery,
    in the order the pending obligations were handed in.

type StoreQuerySource struct {
	// Has unexported fields.
}
    StoreQuerySource is the query source over the observation table: it answers
    QueryEvents on exactly the query semantics of the memory read face, over the
    events landed from the committed store rather than over records a caller
    assembled by hand.

func NewStoreQuerySource() *StoreQuerySource
    NewStoreQuerySource returns an empty observation table.

func (s *StoreQuerySource) LandEvent(ctx context.Context, stored ledger.StoredEvent) error
    LandEvent decodes one committed event under the body schema and lands it in
    the table: the record a query reads and the reference the recall path reads
    the original under. A body whose canonical key disagrees with the stored key
    is refused by name. A repeat of one event key keeps the record of the higher
    window, and a repeat at the same window is an idempotent no-op.

func (s *StoreQuerySource) ObserveStore(ctx context.Context, store *ledger.FileSegmentStore) error
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

func (s *StoreQuerySource) Reference(key event.EventID) (ledger.OriginalRef, bool)
    Reference returns the recall reference the event key was landed with,
    and reports whether the table holds one.

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

package memory // import "github.com/SpellingDragon/tagent/memory"

Package memory is the runtime's durable original-text layer: it names one stored
original and the reference metadata that scopes it, commits those bytes and
returns them byte for byte, and carries the ordered projection and the retention
guard built beside them.

The package speaks the fact vocabulary of the event package and nothing above
it, so the durable side cannot be reversed into the intelligent or governing
side. The storage unit itself lives in memory/ledger and the replay unit in
memory/replay; this package keeps the read face (queries, segment reads),
the projection and the assembly seam over them.

The body schema one committed event carries through the segment store:
the canonical identity and every field the event itself carries — the metadata
projection, the native payload and the lineage identifiers — and the owner,
format and retention policy the reference beside it is registered under.
The light fields a query filters and matches on are not stored beside those:
the summary is read out of the metadata projection and the content out of the
native payload when the record is built, so no field of a committed event
is held twice and no second copy of it can drift from the event it names.
A stored body is self-describing: it decodes back into the query record,
the recall reference and the very event that committed, so a committed body
becomes queryable and readable without a second registry and a fact restored
across a restart keeps every field it committed with. One body format exists:
bytes an earlier writer committed under another identity field carry no identity
under this schema and are refused by name, because that state is rebuilt cold
instead of migrated. The original text behind the event stays in the original
store and is read byte for byte through the scoped path.

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
an embedder configured behaves exactly as it did before the seam existed.
An embedder that is never bound leaves the seam idle: the fact is stored and no
vector is produced, so recall runs on keywords alone.

The unconfirmed-delivery replay engine: the obligations a committed record still
owes, re-delivered at the owner the current orchestration binding resolves. The
durable fact surface beside it lives with the storage tiers in memory/ledger.

Skeleton compile-time assertions for the read face of the memory package: the
delivery replay engine. The storage shapes are pinned beside their declarations
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
	QueryOrderByAsc  = "timestamp_asc"
	QueryOrderByDesc = "timestamp_desc"
)
    QueryOrderByAsc and QueryOrderByDesc are the two orderings a query names:
    oldest first and newest first. An empty OrderBy runs as the ascending
    default — matching the read port, where the default walk is old to new —
    and any other value is refused by name with ErrQueryOrder instead of being
    silently coerced into one of the two.

const DefaultVectorQueue = 64
    DefaultVectorQueue is the queue bound a vector seam runs under when its
    caller names none: every seam is bounded, so a slow embedder can never grow
    the queue without limit.

const PartitionBits = 10
    PartitionBits is the width of the partition range one name is folded into:
    the ten bits the segment key layout reserves for partitions.

VARIABLES

var ErrBodyKeyMismatch = errors.New("memory: event body key disagrees with the stored event key")
    ErrBodyKeyMismatch reports a committed body whose canonical key disagrees
    with the key it is stored under, so the body cannot stand for that event.

var ErrEventBody = errors.New("memory: event body does not decode under the body schema")
    ErrEventBody reports a committed body that does not decode under the body
    schema, so no query record and no reference can be derived from its bytes.

var ErrFactKeyConflict = errors.New("memory: the event key already names another fact")
    ErrFactKeyConflict reports a commit whose canonical key already names
    another fact: one identity stands for one immutable fact, so a second,
    different content under one key is refused by name instead of overwriting
    the fact a reader already holds.

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

func PartitionsOf(id string, extra []string) []string
    PartitionsOf reports the partitions one agent may read across: its own
    namespace first, then every cross-namespace read its memory declaration
    grants, deduplicated in declaration order. The granted set is what the
    recall tools of that agent are built over, so no call can widen it.

TYPES

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
	// Timestamp is the event time in Unix milliseconds.
	Timestamp int64 `json:"timestamp"`
	// Owner is the address the reference of the body is registered for.
	Owner event.Address `json:"owner"`
	// Format is the original format the reference is registered in.
	Format string `json:"format"`
	// RetentionPolicy is the retention policy the reference is held under.
	RetentionPolicy string `json:"retention_policy"`

	// Meta carries the metadata projection the event committed with: every
	// entry kept whole, so a restored fact reads its summary, its trigger
	// source and its audit entries exactly as it committed them.
	Meta map[string]string `json:"meta,omitempty"`
	// Message is the native model payload the event carries, kept whole: the
	// role, the content, the tool calls and the reasoning a fact commits with
	// are what it says, and no projection of them stands for them.
	Message model.Message `json:"message"`
	// Source is the address the event came from.
	Source event.Address `json:"source"`
	// Target is the address the event is delivered to.
	Target event.Address `json:"target"`
	// InvocationID is the responsibility range the event is scoped to.
	InvocationID event.InvocationID `json:"invocation_id,omitempty"`
	// TaskID is the task the event belongs to.
	TaskID event.TaskID `json:"task_id,omitempty"`
	// InstanceID is the runtime instance the event belongs to.
	InstanceID event.InstanceID `json:"instance_id,omitempty"`
	// EpisodeID is the application level episode the event belongs to.
	EpisodeID event.EpisodeID `json:"episode_id,omitempty"`
	// DecisionID is the model decision the event is scoped to.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID is the model attempt the event is scoped to.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// CallID is the native provider call identifier the event records.
	CallID string `json:"call_id,omitempty"`
	// ApprovalID is the approval ticket identity of the call the event records.
	ApprovalID string `json:"approval_id,omitempty"`
}
    EventBody is the body schema of one committed event: the fields a query
    filters and orders on together with the reference scope the recall of the
    event runs under.

func BodyFromEvent(ev event.Event, format, retentionPolicy string) (EventBody, error)
    BodyFromEvent derives the body of one event: its canonical key, frozen type,
    its event time on the millisecond axis, the whole metadata projection,
    native payload and lineage identifiers the event carries, and the owner,
    format and retention policy the reference beside it is registered under.
    Nothing the event already carries is copied into a light field of its own,
    so the body keeps one copy of every field and the record of a query derives
    the rest. An event carrying no key is refused by name.

func DecodeEventBody(body []byte) (EventBody, error)
    DecodeEventBody decodes one committed body under the body schema, or names
    the refusal for bytes that carry no body of this schema.

    One body format exists. Bytes an earlier writer committed spell their
    identity as event_key beside event_type and event_summary, which is no
    identity under this schema: they are refused by name so an event key can
    never be invented from the bytes beside it. Such state is not migrated — a
    store holding it is rebuilt cold (the kv.json precedent), so the refusal
    names the bytes that must be dropped rather than standing in for a migration
    path.

func (b EventBody) Encode() ([]byte, error)
    Encode returns the committed bytes of the body under the body schema.

func (b EventBody) Event() event.Event
    Event restores the very event the body carries: every field it committed
    with — the whole metadata projection, the native payload and the lineage
    identifiers — read back exactly as it stands. Nothing is backfilled into it:
    the body keeps no light field beside the event, so the summary stays the
    one entry of the metadata projection and the content the one text of the
    native payload, and a field the bytes never carried stays empty instead of
    an invented one.

func (b EventBody) Record(partitionID int, windowTS int64) EventRecord
    Record returns the query record of the body under the partition and segment
    window its event is committed into. The summary and the content of the
    record are derived on read — the summary out of the metadata projection,
    the content out of the native payload — because the body stores neither
    beside the event it carries: a record therefore reads the very fields the
    event committed with, and no second copy of them can stand for them.

func (b EventBody) Ref() ledger.OriginalRef
    Ref returns the reference of the body: the key it names and the owner,
    format and retention policy its reference is registered under. The integrity
    stays undeclared here — the original store computes it over the bytes it
    commits, so a derived reference never carries a completeness claim of its
    own.

func (b EventBody) SameFact(other EventBody) bool
    SameFact reports whether one body names the same fact as another:
    one identity stands for one immutable fact, so a second commit under an
    identity is answered as the fact it already names only when nothing of
    the fact itself moved. The stamps the commit protocol generates beside a
    fact — the record time, the invocation identity, the runtime instance,
    the format and the retention policy — describe the commit rather than the
    fact and decide nothing here; the format and the retention policy describe
    the reference registered beside it, likewise. The stamps a caller may
    declare — the source, the target and the task — join the judgment only where
    both sides declare them: a stamp one body left unstated matches anything,
    and two different declared stamps never name the same fact.

type EventRecord struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID
	// PartitionID is the partition the event is filed under.
	PartitionID int
	// Type is the frozen event type of the event.
	Type string
	// Summary is the summary the event committed with, read back through its
	// metadata projection.
	Summary string
	// Content is the content of the event's native message, matched by the
	// keyword leg.
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

func (r EventRecord) Reference() EventReference
    Reference returns the light reference of this record: the rendering subset a
    query answers with, carrying no content and no payload. It is the one place
    the reference fields are derived, so a record and the reference a query
    answers for it never disagree.

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

func (s *StoreQuerySource) Event(key event.EventID) (EventReference, bool)
    Event returns the reference of the landed event under the key, and reports
    whether the table holds it. A reader that already holds a key re-checks the
    attribution the key claims against this one record instead of trusting the
    key alone.

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

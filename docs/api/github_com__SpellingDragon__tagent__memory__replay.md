package replay // import "github.com/SpellingDragon/tagent/memory/replay"

The fold of one projection: the collapse of its foldable run into one compaction
entry, stated as a fact of the chain rather than kept beside it. The retained
refs keep the payload order, the payload records the boundary the tail is
replayed after, and the summary entry is returned first on every restore.

Cold-start replay: the projection is the pure replay of one fact chain.

The projection holds no state outside the fact chain: a rebuild replays the
committed facts and the compaction events among them, and nothing else.
A compaction payload is restored from the chain exactly as the fold left it
— synthetic refs carry their full text in the payload under a negative key,
positive keys hydrate from the chain — and the tail is paged by minimum event
key and appended in EventKey write order, never by timestamp approximation.

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

Skeleton compile-time assertions for the replay shapes: the presentation
decision, the chain-isolated replay source and relation store, the compaction
payload and its refs, and the cold-start rebuild that replays a fact chain into
an empty projection. A renamed field or a widened signature stops this file from
compiling.

CONSTANTS

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
	// ProjectionTypeRecoveryHint is the entry type of the recovery hint an
	// assembly carries first after a cold-start replay, so the model is told the
	// context it sees was rebuilt rather than silently presented.
	ProjectionTypeRecoveryHint = "recovery_hint"
)
const CompactionEventType = event.TypeContextCompress
    CompactionEventType names the frozen event type a compaction payload is
    committed to the fact chain under. A compaction fact is consumed as the
    snapshot it carries and never enters the projection as an entry of its own.

const RebuildSkipNotEmpty = "projection not empty"
    RebuildSkipNotEmpty is the recorded reason of a rebuild that found a
    non-empty projection: the projection is never replaced by a replay.

const ReplaySettleType = "replay.settle"
    ReplaySettleType is the frozen type of a replay settle fact.

VARIABLES

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
var (
	// ErrBudgetInvalid reports a budget assembly asked to pack a request under a
	// non-positive token budget.
	ErrBudgetInvalid = errors.New("memory: budget assembly needs a positive token budget")
)
var ErrFoldEmpty = errors.New("memory: fold needs at least one projection entry")
    ErrFoldEmpty reports a fold of an empty projection: there is no run to
    collapse and no compaction payload to leave on the fact chain.

FUNCTIONS

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
	// ConsumedKeys are the consumed keys the snapshot reseeds; the tail's own
	// consumed keys are reseeded beside them from the replayed tail.
	ConsumedKeys []event.EventID `json:"consumed_keys,omitempty"`
}
    CompactionFact is the compaction payload one fold leaves on the fact chain:
    the summary entry the fold reports first, the retained refs of the folded
    run in their interleaved payload order, the event-key boundary the snapshot
    covers and the consumed keys it reseeds. The payload is committed to the
    chain as one compaction event, and the compaction event itself never enters
    the projection as an entry.

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
	// Consumed lists the consumed keys reseeded, from the snapshot and the
	// tail together.
	Consumed []event.EventID
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

func (p *ReplayProjection) ConsumedKeys() []event.EventID
    ConsumedKeys returns the reseeded consumed keys in order.

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

package replay // import "github.com/SpellingDragon/tagent/memory/replay"

The fold plan: the judgment one producer files and one consumer executes.
A plan reads a projection and answers the products a fold would mint without
touching it; applying the products answers the entries one direct fold answers,
so the two halves of compression never share a step.

The segment-age axis of the mapped sequence: a projection is partitioned
into task-turn segments that close at an agent output, exactly the way the
deterministic compress level reads them, and each segment is placed in one
of three age bands by its age — the newest band the segments still inside the
recent window, the middle band the segments one and two windows back, the oldest
band everything before that. A fold acts on the band next to the material it
consumes: the process detail of the newest band folds into one middle-band
product, and the middle band folds into the one history summary the oldest band
reads as.

The fold is segment-grained while AssembleBudget is entry-grained: the assembly
decides how one entry is presented under its budget and never removes an entry,
and a fold decides which material a segment still carries and replaces the
consumed entries with the product it minted. Both read the same projection and
neither writes the fact chain.

The fold of one projection: the collapse of its foldable run into one compaction
entry, stated as a fact of the chain rather than kept beside it. The retained
refs keep the payload order, the payload records the boundary the tail is
replayed after, and the summary entry is returned first on every restore.

A fold is idempotent: every product is filed under a content-addressed negative
key — the summary under the digest of the folded run, each synthetic ref under
the digest of the entry it carries — so the same material always folds under the
same keys and a second fold mints no second product. A projection that already
carries a compaction payload answers it as it stands: one body of material folds
once, and a projection rebuilt from the chain is folded no further.

Cold-start replay: the projection is the pure replay of one fact chain.

The projection holds no state outside the fact chain: a rebuild replays the
committed facts and the compaction events among them, and nothing else.
A compaction payload is restored from the chain exactly as the fold left it
— synthetic refs carry their full text in the payload under a negative key,
positive keys hydrate from the chain — and the tail is paged by minimum event
key and appended in EventKey write order, never by timestamp approximation.

The causal relation ledger: the parent chain one fact chain's replay maintains,
committed to the append-only journal before the held link moves, and rebuilt
from that journal when the ledger is opened again.

Budget assembly of one projection.

A projection keeps the canonical text of every fact it holds and nothing about
how a request presented it: the view a request carries is computed for that
request alone, from the facts, the declaration and the budget it names. A later
assembly under another budget is judged on its own, so material a tight budget
shortened is presented whole again once the budget allows it and no assembly
inherits the shape of an earlier one. What a past request actually showed is the
request snapshot's record, never a decision the projection keeps.

Skeleton compile-time assertions for the replay shapes: the projection entry,
the chain-isolated replay source and relation store, the compaction payload
and its refs, the cold-start rebuild that replays a fact chain into an empty
projection, and the segment-age axis the bands and their folds are declared
under. A renamed field or a widened signature stops this file from compiling.

CONSTANTS

const (
	// DefaultAgeKeepRecent is the number of recent segments the age rules keep
	// whole when a plan names no window, the same default the deterministic
	// compress level reads under.
	DefaultAgeKeepRecent = 2
	// DefaultAgeVolumeThreshold is the rendered volume a projection may carry
	// before an age fold is due, in bytes.
	DefaultAgeVolumeThreshold = 4096

	// ProjectionTypeSegmentFold names the product entry the process detail of
	// one new-band segment folds into.
	ProjectionTypeSegmentFold = "segment_fold"
	// ProjectionTypeHistorySummary names the product entry the middle and
	// oldest bands fold into.
	ProjectionTypeHistorySummary = "history_summary"

	// AgeProducerSegmentAge names the mechanism that judges a projection due for
	// an age fold.
	AgeProducerSegmentAge = "memory.replay.segment-age"
	// AgeReasonSegmentFold states why the new band is folded.
	AgeReasonSegmentFold = "the process detail of the new band is over its volume threshold"
	// AgeReasonHistorySummary states why the middle band is folded away.
	AgeReasonHistorySummary = "the middle band is over its volume threshold"
)
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

	// BudgetCharsPerToken is that ratio, read by a consumer that measures a
	// projection's rendered volume against a model window: the fold and the
	// assembly convert at the one ratio, so they never disagree about how much
	// window the material costs.
	BudgetCharsPerToken = budgetCharsPerToken
)
const CompactionEventType = event.TypeContextCompress
    CompactionEventType names the frozen event type a compaction payload is
    committed to the fact chain under. A compaction fact is consumed as the
    snapshot it carries and never enters the projection as an entry of its own.

const RebuildSkipNotEmpty = "projection not empty"
    RebuildSkipNotEmpty is the recorded reason of a rebuild that found a
    non-empty projection: the projection is never replaced by a replay.

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
	// ErrRelationsJournalUnwired reports a replay check asked of a ledger that
	// holds no journal, so it has no durable source to compare against.
	ErrRelationsJournalUnwired = errors.New("memory: the relation ledger holds no journal")
	// ErrRelationsReplayMismatch reports a journal whose replay names a chain
	// other than the one the ledger holds, so the served chain is not the durable
	// record.
	ErrRelationsReplayMismatch = errors.New("memory: the replayed relation chain differs from the held chain")
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

func LevelOfAge(age, keepRecent int) int
    LevelOfAge reports the compress level the age of one closed segment reads
    under the deterministic rule the age axis is shared with: the recent window
    whole, the next window dropping tool commands, the window after that keeping
    the skeleton only, and everything older archived.

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

type AgeFoldPlan struct {
	// KeepRecent is the number of recent segments kept whole; zero reads
	// DefaultAgeKeepRecent.
	KeepRecent int
	// VolumeThreshold is the rendered volume a projection may carry, in bytes;
	// zero reads DefaultAgeVolumeThreshold.
	VolumeThreshold int
}
    AgeFoldPlan shapes one age-band fold: the window the age rule keeps whole
    and the rendered volume a projection may carry before a fold is due.
    A plan naming no window or no threshold reads the defaults.

type AgeFoldPlanReport struct {
	// Volume is the rendered volume the judgment read.
	Volume int
	// Threshold is the volume the plan measures against.
	Threshold int
	// Triggered reports whether the material is over its bound.
	Triggered bool
	// Counts reports what the products would consume.
	Counts FoldedCounts
	// Products lists the products the plan would mint, in application order.
	Products []FoldProduct
}
    AgeFoldPlanReport is the judgment of one fold plan: what the plan would
    consume and the products it would mint, beside the volume it read. Reading a
    plan never touches the projection.

type AgeFoldReport struct {
	// Volume is the rendered volume the projection carried, in bytes.
	Volume int
	// Threshold is the volume the fold was judged under, in bytes.
	Threshold int
	// Triggered reports whether the volume passed the threshold and a fold ran.
	Triggered bool
	// NewFolded is how many entries the new-band fold consumed.
	NewFolded int
	// MidFolded is how many entries the middle and oldest bands contributed to
	// the history summary.
	MidFolded int
	// Products lists the product entries the fold minted, in projection order:
	// one segment fold per folded new-band segment, then the history summary.
	Products []ProjectionEntry
	// Requests lists the compress requests the fold filed, one per product, in
	// the order the products were minted.
	Requests []event.CompressRequest
}
    AgeFoldReport is the structured result of one age-band fold: the volume
    the projection carried beside the threshold it was judged under, how many
    entries each fold consumed, and the products and compress requests it
    minted.

type Band int
    Band names one band of the segment-age axis a segment belongs to.

const (
	// BandNew is the newest band, level L0: the projection of these segments
	// corresponds to the context text as it stands.
	BandNew Band = iota
	// BandMid is the middle band, levels L1 and L2: the process detail between
	// the user input and the agent output of these segments is what folds.
	BandMid
	// BandOld is the oldest band, level L3: these segments read as one history
	// summary.
	BandOld
)
func BandOfAge(age, keepRecent int) Band
    BandOfAge reports the band a closed segment of the given age belongs to.

func BandOfLevel(level int) Band
    BandOfLevel reports the band one compress level belongs to: L0 the new band,
    L1 and L2 the middle band, L3 the old band.

func (b Band) String() string
    String reports the band as the age axis names it.

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

type FoldProduct struct {
	// Entry is the synthetic entry the projection carries under Entry.Key.
	Entry ProjectionEntry
	// Covered lists the entry keys the product consumes, in their original
	// order: the process detail of one closed segment, or the middle bands.
	Covered []event.EventID
	// Request is the judgment this product answers.
	Request event.CompressRequest
}
    FoldProduct is one product a plan would mint: the synthetic entry to place,
    the entries it covers and the compress request it answers. A product is
    what a producer judges and what a consumer executes, so the judgment and the
    execution never share a step.

type FoldedCounts struct {
	// Segment counts the process-detail entries a segment product consumes.
	Segment int
	// Middle counts the entries a history summary consumes.
	Middle int
}
    FoldedCounts reports what one fold judgment would consume.

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

    A ledger opened over a journal commits a link to that journal before the
    held link moves: a SetParent that returns success names a link a later
    replay reads back, and one that returns an error leaves the held chain as
    it was. The ledger is the one relation surface a rebuild writes through:
    it is the only implementation, so the rebuild names it directly rather than
    declaring a port with no second implementer behind it.

func NewMemoryRelations() *MemoryRelations
    NewMemoryRelations returns an empty relation ledger with no journal behind
    it: its links live for the life of the process and declare no recovery
    across process exit.

func OpenMemoryRelations(root string) (*MemoryRelations, error)
    OpenMemoryRelations opens the relation ledger rooted at root: every
    record the journal holds is replayed into the held chain, and every link a
    SetParent commits reaches that same journal before the held chain moves.

func (r *MemoryRelations) GetParent(child event.EventID) (event.EventID, error)
    GetParent returns the parent of child, empty when there is none.

func (r *MemoryRelations) Links() map[event.EventID]event.EventID
    Links returns a copy of the parent links the ledger holds, so a caller can
    compare a rebuilt chain against the one it serves.

func (r *MemoryRelations) SetParent(child event.EventID, parent event.EventID) error
    SetParent sets the parent of child, or clears it when parent is empty.

func (r *MemoryRelations) VerifyReplay(ctx context.Context) error
    VerifyReplay reports whether the journal replays to the chain the ledger
    holds, so a caller can assert the durable record and the served chain agree.
    A ledger opened without a journal has no replay source and says so.

type ProjectionEntry struct {
	// Key is the canonical identity of the fact, in its hex form.
	Key event.EventID
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact, which is not the full text.
	Summary string
	// Content is the original text of the fact: immutable input, never a view.
	// An assembly shapes the copy a request carries and leaves this text as the
	// fact stated it, so the same entry presents differently under different
	// budgets.
	Content string
	// Time is the time the fact was recorded.
	Time time.Time
}
    ProjectionEntry is one ordered item of a projection: the canonical identity
    of a committed fact, its type and summary, the original text it carries,
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
    boundary. The restored payload is kept as the projection's compaction
    payload, so a fold offered to the rebuilt projection answers it instead of
    folding a second time. Without a compaction the rebuild runs the bounded
    fallback instead of a no-op, and a successfully scanned empty chain is
    reported as empty.

    The causal chain is maintained through relations.SetParent inside this
    (PartitionID, SessionID) chain alone: a replay never links one session's
    facts into another's, and an empty session is refused by name.

type ReplayProjection struct {
	// Has unexported fields.
}

    ReplayProjection is the replayable projection: the ordered model view of one
    fact chain and the compaction payload a fold left on the chain. It is what
    RebuildProjectionFromWAL replays into.

    A projection holds no state outside the fact chain, so replaying the
    recorded facts reproduces the same entries, and it holds no presentation
    decision: the view a request carries belongs to that request and is computed
    when it is assembled. The entries hold the canonical form (the original text
    of each keyed fact), and an assembly shapes a copy of it rather than the
    entry. Constraints:

      - a keyed entry already present is appended once, and a keyless entry
        always is;
      - the original text of an entry is immutable input, and the model view is
        the assembly's output;
      - a rebuild recomputes the deduplication key set from the entries it
        keeps.

func NewReplayProjection() *ReplayProjection
    NewReplayProjection returns an empty replayable projection.

func (p *ReplayProjection) Append(_ context.Context, entry ProjectionEntry)
    Append adds the canonical entry of one committed fact: a keyed entry already
    present is appended once, a keyless entry always is. The entry carries the
    fact's own text, which is what the projection keeps — an append never shapes
    the text for a model, so the same entry presents differently under different
    budgets and the fact reads back unchanged either way. An append is an
    in-memory mutation, so it takes the port's context with nothing to cancel.

func (p *ReplayProjection) ApplyProducts(products []FoldProduct)
    ApplyProducts places the products a judgment minted onto the projection:
    each covered entry is replaced by its product, a history summary lands ahead
    of the material it summarizes, and a product whose entry is already held
    changes nothing — applying one judgment twice is applying it once. A covered
    entry leaves the projection with its per-key material (the kept original,
    the dedup key and the consumed mark), so the maps say what the rewritten
    entries say.

func (p *ReplayProjection) AssembleBudget(ctx context.Context, budget int) ([]ProjectionEntry, error)
    AssembleBudget packs the projection into one agent request of the given
    token budget and returns the entries the request is assembled from,
    recovery hint first when one applies. Every entry is presented at the first
    form that fits the room the budget leaves — the full text, the ticket,
    the stored summary and for a notice its identity card — computed for this
    assembly alone: nothing is read back from how an earlier assembly presented
    the same fact, so a budget that shrinks presents less and a budget that
    widens presents more again. The source facts are read and never written, and
    the assembly files no settle fact and keeps no decision, so the projection
    is left as the next assembly found it.

    The assembled request never runs past the budget it names: every entry it
    carries, the recovery hint and the notice card included, is cut to the room
    that remains. An entry that fits none of the forms is skipped, so a later
    assembly under a larger budget can still present it in full — except a
    notice entry, which the budget may shorten to its identity card but never
    drop: a notice the model never read is an event the model never saw arrive.

func (p *ReplayProjection) AssembleBudgetPrefix(ctx context.Context, budget, prefix int) ([]ProjectionEntry, error)
    AssembleBudgetPrefix assembles the leading prefix of the projection under
    the same budget rules, so a consumer presents the material that has ended
    while the material still in flight stays with the transcript that carries
    it: a negative prefix assembles the whole projection, and a prefix past its
    length assembles what it holds.

func (p *ReplayProjection) Compaction() *CompactionFact
    Compaction returns the compaction payload the last fold left, nil before
    any.

func (p *ReplayProjection) ConsumedKeys() []event.EventID
    ConsumedKeys returns the reseeded consumed keys in order.

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

    - The entries the summary alone represents leave the projection with their
    per-key material (the original text, the dedup key and the consumed mark)
    while a retained ref keeps its own, so the rewritten sequence carries no
    copy of the material the summary speaks for.

    - A projection already carrying a compaction payload answers that payload's
    summary entry and moves nothing, so one body of material folds once even
    when a rebuilt projection is offered to the fold again.

func (p *ReplayProjection) FoldByAge(ctx context.Context, plan AgeFoldPlan) (AgeFoldReport, error)
    FoldByAge folds the projection along its segment-age bands once its rendered
    volume passes the plan's threshold: the process detail of every closed
    new-band segment is replaced by one segment-fold product, and the middle and
    oldest bands are replaced by one history summary placed first. A projection
    whose volume stays inside the threshold is left exactly as it stands.

    - The entries a product replaces leave the projection with their per-key
    material (the kept original, the dedup key and the consumed mark), so a fold
    of the same sequence twice finds nothing left to drop.

    - The fold is deterministic over the sequence it reads and files the
    judgment as a compress request rather than rewriting any fact: the products
    enter the projection under content-addressed synthetic keys, so folding the
    same sequence twice mints the same keys and appends no second product.

func (p *ReplayProjection) FullBoundary() event.EventID
    FullBoundary returns the event-key boundary the compaction payload carried.

func (p *ReplayProjection) Len() int
    Len returns how many entries the projection holds.

func (p *ReplayProjection) Original(key event.EventID) (string, bool)
    Original returns the kept original text of one keyed entry.

func (p *ReplayProjection) PlanByAge(ctx context.Context, plan AgeFoldPlan) (AgeFoldPlanReport, error)
    PlanByAge judges one fold plan without touching the projection: it answers
    the products the fold would mint, so a producer can file the judgment and a
    consumer can execute it later, each on its own.

func (p *ReplayProjection) RecoveryHint() *ProjectionEntry
    RecoveryHint returns the recovery hint entry a rebuild left, nil before one.

func (p *ReplayProjection) RestoreProducts(products []event.CompressProduct)
    RestoreProducts places the products a chain recorded onto the projection:
    the product fact names the key, the text and the entries it covers, so a
    restore rebuilds the entry the fold minted instead of folding the material
    again. The kind of product is read off the key's prefix and its moment off
    the first entry it covers, which is the moment the fold itself gave it.
    A key no fold mints, and a product whose entry the projection already holds,
    change nothing.

type Segment struct {
	// Index is the position of the segment in the projection, oldest first.
	Index int
	// Age is the distance of the segment from the newest one, zero for it.
	Age int
	// Level is the compress level the age reads, zero for a segment in progress.
	Level int
	// Band is the age band the level belongs to.
	Band Band
	// Complete reports whether an agent output closed the segment.
	Complete bool
	// Entries are the projection entries the segment spans, in projection order.
	Entries []ProjectionEntry
}
    Segment is one task-turn segment of a projection: the entries it spans, the
    age and level it reads, whether an agent output closed it, and the band it
    therefore belongs to. A segment no agent output closed is still in progress,
    reads L0 and is never folded.

func SegmentsOf(entries []ProjectionEntry, keepRecent int) []Segment
    SegmentsOf partitions the entries of one projection into its task-turn
    segments, oldest first: a segment spans an external input beside the process
    entries that follow it up to the agent output that closes it, and a tail no
    agent output closed stays its own segment in progress.

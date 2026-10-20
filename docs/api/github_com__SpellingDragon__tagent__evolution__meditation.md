package meditation // import "github.com/SpellingDragon/tagent/evolution/meditation"

The scheduled reflection driver of one meditation loop: the plan it runs,
the delivery port a fired reflection flows back through, one Tick per declared
interval, and the named refusals a plan that cannot drive answers at assembly.

The declaration face of one scheduled reflection: a document states which
agent observes, at which cadence, over which namespaces, with which prompt
and to which declared targets, and PlanOf resolves that section once into the
plan a driver runs. The host maps its own document onto document.Document;
the resolution rules — one loop per host, cadence bounds, the observation face
inside the authorized read scope, the declared delivery targets and the anchor
record path — live here, not in the host.

The pre-fold face of the meditation consumer: the fold a consumer runs when it
consumes a compress request, the compaction fact it files into the state, and
the fold identity that fact is quoted by. A pre-fold runs ahead of the request
boundary, so the assembly path reads the folded projection and never folds it
inline.

Skeleton compile-time assertions for the meditation shapes: the consumer faces
a tick reads, the engine's pass, judgment and tick signatures, the pre-fold seam
and the refusals each unwired face answers by name. The reflection loop and the
pass it reads keep their own declared surface here as well: the loop's assembly
and tick operations, the observation face it reads, the digest it renders and
the execution watermark it advances only on a consumed turn. One file holds the
one shape pin of the package: a renamed field or a widened signature stops it
from compiling.

The meditation strategy face: the deterministic value assessment of one card,
the policy bound a pass is judged against and the condenser seat an LLM would
take beside the engineering condensation. The assessment is a pure function of
the card and the pass time, so two identical passes reach identical fates.

The meditation vocabulary: the fate one card is filed under, the retention kind
a held reference is registered as, and the condensed card a pass leaves behind
as a fact of its own — the artifact is a new reference over the refs it keeps,
never a rewrite of a fact it condenses. The card sequence and the artifact are
identified by content, so one body of material and one condensation each answer
one identity.

The meditation consumer wiring: the engine that assesses a card sequence and
condenses its over-limit run, the tick that drives it from the compress request
facts its producers filed, and the option surface that wires the card source,
the product sink and the pre-fold. The engine rewrites no original fact and no
reference metadata, whatever the outcome.

CONSTANTS

const (
	// DefaultObservationPageLimit bounds one pass over the observed face; a face
	// with a longer backlog reports the bound instead of growing the pass.
	DefaultObservationPageLimit = 1000
	// DefaultDigestFactLimit bounds how many hydrated facts one digest lists.
	DefaultDigestFactLimit = 8
)
const (
	// MeditationLinkWeight is the value one standing causal link adds.
	MeditationLinkWeight = 10
	// MeditationSkeletonWeight is the value a boundary fact (external input or
	// agent output) adds: the task skeleton the condensation keeps.
	MeditationSkeletonWeight = 5
	// MeditationRecencyWeight is the value a fact inside the recent window adds.
	MeditationRecencyWeight = 2
)
    MeditationLinkWeight, MeditationSkeletonWeight and MeditationRecencyWeight
    are the value assessment weights: a standing causal link is worth more than
    the boundary skeleton weight, which is worth more than the recency bonus.

const DefaultInterval = 30 * time.Minute
    DefaultInterval is the tick cadence of a driver whose plan names none.

const LineageReflection = "meditation"

    LineageReflection is the trigger-source stamp a reflection input carries:
    the reflection turn's own lineage, which is self-managed traffic, so an
    agent's own reflection never makes it reflect on itself again.

const MeditationArtifactType = "memory.condensed_card"
    MeditationArtifactType is the frozen type label of the condensed card
    artifact one meditation pass leaves behind.

const MeditationHolderPrefix = "curation:"
    MeditationHolderPrefix is the identity prefix of the holder a meditation
    pass registers its retention hold under. A holder id is durable ledger
    identity carried by every reference the hold protects, so the prefix is
    fixed vocabulary of the ledger rather than a name this package chooses.

const RetentionMeditation ledger.RetentionKind = "curation_reference"
    RetentionMeditation is the kind of a retention registration protecting
    material a condensed artifact still references: the hold a meditation pass
    takes on each key its artifact cites, so the referenced original is never
    tombstoned or reclaimed while the artifact can still recall it by key.

    The registered kind rides the ledger as durable identity: it answers the
    window lookup of every reference held under it, so its value is fixed
    vocabulary of the ledger rather than a name this package chooses.

VARIABLES

var (
	// ErrPrefoldUnwired reports a pre-fold asked without the projection it
	// folds: no material is named and no compaction fact could be filed.
	ErrPrefoldUnwired = errors.New("meditation: pre-fold needs the projection of the material it folds")
	// ErrMeditationProjectionUnwired reports a consumer tick pre-folding without
	// the projection source: the tick names no material it could fold.
	ErrMeditationProjectionUnwired = errors.New("meditation: consumer pre-fold needs the projection source")
	// ErrMeditationFoldsUnwired reports a pre-fold whose compaction fact has
	// nowhere to be committed: the fold would be counted while no state carries
	// it.
	ErrMeditationFoldsUnwired = errors.New("meditation: consumer pre-fold needs the fold sink")
)
var (
	// ErrMeditationUnwired reports a meditation engine constructed without the
	// retention ledger or the lifecycle it coordinates with.
	ErrMeditationUnwired = errors.New("memory: meditation needs a retention ledger and a lifecycle")
	// ErrMeditationPolicy reports a meditation policy that names no sequence
	// bound, so no pass could ever know when to trigger.
	ErrMeditationPolicy = errors.New("memory: meditation policy names no sequence bound")
	// ErrMeditationCardKey reports a card that carries no event key, or a
	// sequence that repeats one: no fate could be filed under it.
	ErrMeditationCardKey = errors.New("memory: meditation card carries no unique event key")
	// ErrMeditationRequestsUnwired reports a consumer tick attempted without the
	// request log it consumes: a tick that cannot read the judgments it answers
	// would compress material no producer judged due.
	ErrMeditationRequestsUnwired = errors.New("memory: meditation consumer needs the compress request log")
	// ErrMeditationCardsUnwired reports a consumer tick attempted without the card
	// sequence source: the tick names no material it could curate.
	ErrMeditationCardsUnwired = errors.New("memory: meditation consumer needs the card sequence source")
	// ErrMeditationProductsUnwired reports a condensed pass whose product has
	// nowhere to be committed: the artifact would be counted as held while no
	// state carries it.
	ErrMeditationProductsUnwired = errors.New("memory: meditation consumer needs the product sink")
)
var ErrAnchorStoreUnusable = errors.New("evolution: the anchor store requires a path")
    ErrAnchorStoreUnusable reports an anchor store built without a path:
    a watermark that has nowhere to be recorded cannot carry across a restart.

var ErrAnchorUnreadable = errors.New("evolution: the anchor record cannot be read")
    ErrAnchorUnreadable reports an anchor record that exists but cannot be read
    or parsed. The gate reads it as an unestablished watermark and stays closed;
    no default and no fallback criterion is substituted for it.

var ErrDeclaration = errors.New("tagent: the meditation declaration is inconsistent")
    ErrDeclaration reports a meditation section that cannot be resolved into
    one scheduled reflection: more than one agent enabling the loop, a negative
    cadence, an unreadable or empty prompt, an observation face outside the
    authorized read scope, an undeclared delivery target, or an enabled section
    beside no shared storage tier for its anchor record.

var ErrDigestFaceUnavailable = errors.New("evolution: the digest has no observed pass to render")
    ErrDigestFaceUnavailable reports a digest asked to render without the pass
    it summarizes. An absent pass is named as unavailable, never presented as a
    quiet face.

var ErrDigestScanMismatch = errors.New("evolution: the observed pass opened at another watermark")
    ErrDigestScanMismatch reports a digest asked to render a pass that opened
    at a watermark other than the one it was asked about. The pass a digest
    summarizes is the one its caller already read, so the rendering never
    triggers a second pass over the fact chain.

var ErrDriverCadence = errors.New("evolution: the meditation driver requires a non-negative cadence")
    ErrDriverCadence reports a plan whose interval or rhythm floor is negative:
    no ticker can run at such a cadence.

var ErrDriverUntargeted = errors.New("evolution: the meditation driver requires a declared delivery target")
    ErrDriverUntargeted reports a plan that names no delivery target: a fired
    reflection whose result nobody receives is refused at assembly.

var ErrDriverUnwired = errors.New("evolution: the meditation driver requires a delivery port")
    ErrDriverUnwired reports a driver assembled without the delivery port a
    fired reflection hands its result to.

var ErrObservationScopeOutOfBounds = errors.New("evolution: the observed namespaces reach outside the authorized read scope")
    ErrObservationScopeOutOfBounds reports an observation face that names a
    namespace outside the authorized read scope. The face is refused by name at
    assembly instead of being trimmed: reading less than declared would silently
    change what the reflection observes.

var ErrObservedFaceUnwired = errors.New("evolution: the observed fact-chain read face is not wired")
    ErrObservedFaceUnwired reports a pass attempted while the read face of
    the observed fact chain is not wired. The reflection gate stays closed:
    the fact chain is the only data face of the freshness judgment.

var ErrReflectionAgentUnnamed = errors.New("evolution: reflection requires the observing agent's namespace")

    ErrReflectionAgentUnnamed reports a reflection assembled without the
    observing agent's own identity: an observation face cannot be resolved and
    no loop session can be addressed without it.

var ErrReflectionInputUnkeyed = errors.New("evolution: the reflection input committed no identity")
    ErrReflectionInputUnkeyed reports a reflection input that committed no
    identity: without one, a consumption report cannot name the turn it settles.

var ErrReflectionKeyMismatch = errors.New("evolution: the outcome names a reflection input other than the one awaiting consumption")
    ErrReflectionKeyMismatch reports an outcome naming a reflection input other
    than the one awaiting consumption. The report is refused, so a report about
    an older turn can never settle a newer one.

var ErrReflectionNotPending = errors.New("evolution: no reflection input awaits an outcome")
    ErrReflectionNotPending reports an outcome for a reflection input that is
    not awaiting one: a repeated or stale report never moves the watermark.

var ErrWatermarkSourceUnwired = errors.New("evolution: the execution watermark source is not wired")
    ErrWatermarkSourceUnwired reports a tick attempted while the execution
    watermark has no source. The gate stays closed instead of reading a fresh
    default.

FUNCTIONS

func CompactionChainFact(chain replay.ChainKey, payload replay.CompactionFact, at time.Time) (replay.ChainFact, error)
    CompactionChainFact returns the state fact one compaction payload is
    committed as: the compaction event a replay reads back, filed under the fold
    identity the payload answers. A payload that cannot be written back as a
    chain fact is reported by name.

func CompactionFactKey(payload replay.CompactionFact) event.EventID
    CompactionFactKey derives the identity the fold fact of one compaction
    payload is filed under: content-addressed over the summary it reports,
    the boundary it covers and the refs it retains in payload order, so one fold
    files one fact and a re-file keeps that identity.

func DeliverableLineage(stamp string) bool
    DeliverableLineage reports whether a trigger-source stamp names traffic the
    host may see delivered. A stamp outside the declared vocabulary is internal
    and withheld, so an unrecognised stamp is never treated as host traffic.

func NewAnchorStore(path string) (*AnchorStore, error)
    NewAnchorStore returns the anchor store of the record at path, creating
    its directory when the directory is absent. A store built without a path is
    refused with ErrAnchorStoreUnusable.

func NewDriver(plan DriverPlan, gate WatermarkGate, facts ObservedFacts, input ReflectionInput, deliver Deliver) (*Driver, error)
    NewDriver assembles the scheduled reflection of one plan over its watermark
    gate, its observed fact-chain face, its loop-session injection face and
    its delivery port. A negative cadence, a plan naming no target and a
    missing port are refused by name; a plan leaving the interval unset runs at
    DefaultInterval.

func NewMeditation(policy MeditationPolicy, retention *ledger.Retention, lifecycle *ledger.Lifecycle, condense Condenser, options ...MeditationOption) (*Meditation, error)
    NewMeditation returns the meditation engine over the retention ledger and
    the lifecycle it drives: the ledger holds what the artifact references
    and the lifecycle owns the tombstone channel every eviction travels. A nil
    condenser runs the deterministic engineering condensation. The retention
    ledger must be the one the lifecycle was built over, so a reference the pass
    holds is the protection the eviction consults. The options wire the consumer
    face: the request log a tick consumes, the card sequence it curates, the
    state its product facts are committed into and the plugin face its requests
    are dispatched to.

func NewReflection(cfg ReflectionConfig, gate WatermarkGate, facts ObservedFacts, input ReflectionInput) (*Reflection, error)
    NewReflection assembles the reflection loop of one agent over its watermark
    gate, its observed fact-chain face and the loop-session injection face. A
    declared observation face outside the authorized scope refuses the assembly
    by name.

func PlanOf(doc *document.Document, promptDir, storageRoot string) (*Plan, error)
    PlanOf resolves the one meditation section doc enables: the entry agent is
    resolved first and the remaining agents in declared order, so one document
    resolves to one plan across restarts. A document enabling none reports a nil
    plan. Every inconsistent section is refused by name at assembly rather than
    driven into a reflection the declaration did not state.

func Prefold(ctx context.Context, projection *replay.ReplayProjection) (replay.CompactionFact, error)
    Prefold folds the projection a consumer owns ahead of assembly and answers
    the compaction payload the fold left, so the fold fact is filed when
    its request is consumed and the request boundary only reads the folded
    projection. A projection already carrying a compaction payload is answered
    with it: one body of material folds once.

func ResolveObservationScope(own string, declared, read []string) (ReflectionScope, error)
    ResolveObservationScope resolves the observation face of one agent from its
    declared namespaces, the namespaces it is authorized to read and its own
    namespace.

    An undeclared face is the agent's own partition and never falls back to the
    authorized read scope: what an agent may read is not what it reflects on.
    A declared name that is neither the agent's own nor inside the read scope is
    refused with ErrObservationScopeOutOfBounds naming every such namespace.

func SelfManagedLineage(stamp string) bool
    SelfManagedLineage reports whether a trigger-source stamp names traffic
    the agent initiated for itself rather than an interaction someone awaits,
    from the same vocabulary as DeliverableLineage so no consumer holds a list
    of its own.

    A stamp the vocabulary does not name derives as self-managed: an event
    without a readable trigger source is never counted as freshness, and a
    reflection input or a consolidation hint never re-arms the judgment that
    produced it.

TYPES

type AnchorStore struct {
	// Has unexported fields.
}
    AnchorStore is the anchor record of one observing agent: the pair of
    loop anchors kept in one file and rewritten atomically, so a reader never
    observes a half-written record.

    The store is the gate's only watermark source. A record that is absent reads
    as a start with no prior execution, while a record that exists but cannot be
    read or parsed is refused by name: an unreadable watermark is never reported
    as a fresh one.

func (s *AnchorStore) Advance(ctx context.Context, moment time.Time) error
    Advance moves the execution watermark to the moment a consumed reflection
    turn was injected at and keeps the session anchor as it stands. A moment
    that is not after the recorded watermark leaves the record as it is,
    so the watermark never moves backwards.

func (s *AnchorStore) Current(ctx context.Context) (Watermark, error)
    Current reports the execution watermark the anchor record holds. An absent
    record reports the zero watermark of a start with no prior execution,
    and an unreadable record is refused with ErrAnchorUnreadable so the gate
    stays closed.

func (s *AnchorStore) Load() (Anchors, error)
    Load reads the anchor record. An absent record is a start with no prior
    execution and carries no error; a record that cannot be read or parsed is
    refused with ErrAnchorUnreadable.

func (s *AnchorStore) Path() string
    Path reports the record path this store reads and writes.

func (s *AnchorStore) Save(record Anchors) error
    Save rewrites the anchor record atomically: the new record is written beside
    it and renamed over it, so no reader observes a partial record.

type Anchors struct {
	// LastTurnEnd is the moment the last turn of the session ended.
	LastTurnEnd time.Time `json:"last_turn_end"`
	// LastMeditation is the moment of the last consumed reflection turn.
	LastMeditation time.Time `json:"last_meditation"`
}
    Anchors is the persisted pair of loop anchors: the moment the last
    reflection turn was consumed and the moment the last turn of the session
    ended.

    The reflection watermark heads the freshness window and the rhythm floor,
    so it is the anchor the gate reads and the anchor a consumed reflection
    advances. The session anchor is carried beside it in the same record,
    and an entry this structure does not name is ignored when the record is read
    rather than refused.

type CompressPhase interface {
	// TriggerCompress dispatches one filed compress request. A nil error carries the
	// request forward into the pass that compresses it; a non-nil error refuses it
	// by name, so no material is curated and the request stays pending.
	TriggerCompress(ctx context.Context, request event.CompressRequest) error
}
    CompressPhase is the plugin face a meditation tick hands the compress
    requests it reads to: the face answers one filed judgment with the
    compression it owes, and the request it is dispatched for is the one the log
    filed, value for value. The tick curates the material only after the face
    answered it, so a compression face that refused keeps its request pending
    instead of being counted compressed.

    The redline list (3 rules): MUST take the request as the log filed it:
    the face answers the compression, never the judgment. MUST NOT retire
    the request: the tick retires it once the products of its compression are
    committed. MUST NOT curate material no filed request names.

type Condenser func(ctx context.Context, run MeditationRun, now time.Time) (string, error)
    Condenser folds one over-limit card run into its condensed summary:
    the seat an LLM meditation would take beside the deterministic engineering
    condensation. A condenser that fails degrades the pass to that engineering
    fallback — the overflow run sinks into an "(earlier n items)" count line —
    and never collapses the pass or rewrites a fact.

type Deliver func(ctx context.Context, source event.Address, targets []event.AgentID)
    Deliver hands the output of one fired reflection to the declared targets:
    the source is the observing agent's own loop address, and the targets are
    the declared recipients in declaration order.

type Digest interface {
	// Render returns the bounded self-state summary of the observed face since
	// the given watermark. When the face holds more than the bound renders, the
	// count left out is reported in the result instead of being dropped in
	// silence.
	Render(ctx context.Context, since Watermark) (DigestSummary, error)
}

    Digest is the reflection loop's self-state summary: a deterministic,
    bounded rendering of the observed face since a watermark that a reflection
    turn reads without spending a model call.

    The rendering is bounded, so a face with a long backlog reports how many
    of its facts the bound left out instead of growing without limit, and the
    same face at the same watermark renders the same summary. A render that
    cannot reach the face reports that it is unavailable by name rather than
    presenting an empty summary as a quiet face. The redline list (3 rules):
    MUST render the same summary for the same face at the same watermark.
    MUST report the count a bound left out beside the rendering it bounded.
    MUST NOT present an unreachable face as a quiet one.

type DigestSummary struct {
	// Body is the rendered summary of the observed face.
	Body string
	// Omitted is the number of facts the render bound left out of Body.
	Omitted int
}
    DigestSummary is the rendered self-state summary together with the count of
    facts the render bound left out.

type Driver struct {
	// Has unexported fields.
}
    Driver drives one declared reflection loop: each tick reads the observed
    face through the reflection chain, injects the reflection input into the
    observing agent's own loop session, and hands a fired reflection's output
    to the delivery port. The zero value is not usable; build a driver with
    NewDriver.

func (d *Driver) Scope() ReflectionScope
    Scope reports the resolved observation face of this driver's reflection.

func (d *Driver) Start(ctx context.Context)
    Start begins the tick loop: the first tick runs one interval after the
    start, so a restart never fires a reflection the previous process just
    consumed.

func (d *Driver) Stop()
    Stop ends the tick loop and waits for the tick in flight to return. A driver
    never started returns immediately.

func (d *Driver) Tick(ctx context.Context) TickOutcome
    Tick runs one scheduled reflection and, when it fired, hands what the
    reflection turn produced to the delivery port. A tick that decided not to
    fire delivers nothing.

type DriverPlan struct {
	ReflectionConfig
	// Targets is the declared delivery white list of one fired reflection.
	Targets []event.AgentID
}
    DriverPlan is the one scheduled reflection a host declares: the reflection
    loop, face, cadence and prompt, beside the declared delivery targets.

type FactAttribution struct {
	// Lineage is the trigger-source stamp the fact carries; empty means the fact
	// carries none.
	Lineage string
	// SelfManaged reports the lineage derivation of this fact.
	SelfManaged bool
}
    FactAttribution is whose traffic a fact is: the trigger-source stamp it
    carries and the derivation of that stamp.

type FactContent struct {
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact.
	Summary string
	// Timestamp is the moment the fact was recorded.
	Timestamp time.Time
}
    FactContent is what a fact recorded: its frozen type, its stored summary and
    the moment it was recorded.

type FactIdentity struct {
	// Key is the canonical identity of the fact.
	Key event.EventID
	// Partition is the namespace name the fact was filed under.
	Partition string
}
    FactIdentity is which fact a pass read and where it was filed: the canonical
    identity the fact committed under and the namespace name it was filed as.

type Meditation struct {
	// Has unexported fields.
}
    Meditation is the memory meditation engine: it assesses card value
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

func (c *Meditation) Curate(ctx context.Context, now time.Time, cards []MeditationCard) (MeditationReport, error)
    Curate runs one meditation pass over the card sequence at the given time.

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

func (c *Meditation) Judge(ctx context.Context, now time.Time, cards []MeditationCard) (event.CompressRequest, bool, error)
    Judge files the request fact the card sequence's bound judgment is:
    a sequence at or within the bound files nothing and answers false,
    and an over-limit one files the request naming the sequence, the meditation
    producer and the bound reason, and answers that request with true.
    The judgment is filed under the sequence's content-addressed identity,
    so judging one body of material again files no second request and keeps the
    first filing moment. A sequence no fate could be filed over is refused by
    name and files nothing.

func (c *Meditation) Tick(ctx context.Context, now time.Time) (MeditationTick, error)
    Tick runs one meditation tick at now: it reads the card sequence it curates
    and the compress requests pending in the request log, and for each pending
    request naming that sequence it dispatches the compress phase, runs one
    pass, commits the condensed artifact the pass minted into the state,
    pre-folds the compressed projection once through the wired pre-fold, commits
    the compaction fact and retires the request. A request naming other material
    stays pending, and a phase, pass, commit, pre-fold or retirement that failed
    keeps its request pending and lands in the tick account, so a request is
    never counted consumed before the products of its compression are held.

type MeditationArtifact struct {
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
    MeditationArtifact is the condensed card one pass leaves behind:
    the new reference the meditation produces, never a rewrite of any fact it
    condenses. Its refs are the [evt_key] tickets of the key cards it keeps,
    so the condensed result stays traceable and recall still refills by key.
    Sunk counts the card lines the pass collapsed into its count line when it
    degraded to the engineering fallback, and is zero while a condensation
    holds.

func (a MeditationArtifact) FactKey() string
    FactKey returns the condensed artifact's identity as a fact key:
    the content-addressed key of the card references it carries, so the same
    condensation answers the same key every time it is asked.

func (a MeditationArtifact) FactType() string
    FactType returns the frozen type a condensed artifact is read as.

type MeditationCard struct {
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
    MeditationCard is one card line of the sequence a meditation pass runs over:
    the [evt_key] ticket of the fact behind it, its frozen type, the small
    rendering text of the line, the time the fact was recorded, and the count of
    standing causal links — references and parent keys — that point at the fact.

type MeditationCards func(ctx context.Context) ([]MeditationCard, error)
    MeditationCards reads the card sequence one meditation tick curates:
    the cards of the partition the tick owns, oldest first as the caller renders
    them. A read that fails is reported by name and a tick never curates a
    partial sequence.

type MeditationFate string
    MeditationFate names the one channel a card's meditation fate is filed
    under: exactly one of retain, evict or protect for every card of a pass.
    The fates are mutually exclusive and exhaustive — a card belongs to one
    channel and never to a fourth — so the eviction a pass declares is the whole
    eviction it performs, and it is performed on the tombstone channel alone.

const (
	// MeditationRetain is the fate of a card the pass keeps: the fact stays as
	// it is and takes no part in any eviction.
	MeditationRetain MeditationFate = "retain"
	// MeditationEvict is the fate of a card the pass evicts: the fact is
	// tombstoned through the one eviction channel and stays readable until
	// compaction reclaims it. Eviction never rewrites or destroys bytes
	// directly.
	MeditationEvict MeditationFate = "evict"
	// MeditationProtect is the fate of a card whose original is protected from
	// eviction: the condensed artifact references it or another holder already
	// holds it, so the referenced original is never deleted while that
	// reference stands.
	MeditationProtect MeditationFate = "protect"
)
type MeditationFolds func(ctx context.Context, fact replay.CompactionFact) error
    MeditationFolds commits the compaction fact one pre-fold left into the
    state: the fold fact its request is answered with. A commit that fails
    is reported by name, so a fold is never counted as held before the state
    carries it.

type MeditationOption func(*Meditation)
    MeditationOption names one setting of the meditation engine at construction
    time.

func WithCompressPhase(phase CompressPhase) MeditationOption
    WithCompressPhase wires the plugin face the consumer tick dispatches every
    compress request it reads to. A tick wired without one curates the material
    its requests name and retires them as it stands: no compression face stands
    between the judgment and the pass.

func WithCompressRequests(requests *memory.CompressRequests) MeditationOption
    WithCompressRequests wires the compress request log the consumer tick reads:
    the requests its producers filed, and the retirement a tick performs once
    the products of one compression are committed.

func WithMeditationCards(cards MeditationCards) MeditationOption
    WithMeditationCards wires the card sequence source the consumer tick
    curates. A failed read is reported by name and never read as an empty
    sequence.

func WithMeditationPreFold(projection MeditationProjection, folds MeditationFolds) MeditationOption
    WithMeditationPreFold wires the consumer-side fold: the projection a tick
    folds when it consumes a compress request and the state its compaction fact
    is committed into. The request stays pending until the fold fact is held,
    so the assembly path never folds inline.

func WithMeditationProducts(products MeditationSink) MeditationOption
    WithMeditationProducts wires the state face the consumer tick commits its
    product facts into: the condensed artifact of every pass that condensed a
    run.

type MeditationPolicy struct {
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
    MeditationPolicy shapes one meditation pass: the sequence bound that
    triggers it, how many key cards survive as artifact references, and the
    value floor below which an unprotected overflow card is evicted.

type MeditationProjection func(ctx context.Context) (*replay.ReplayProjection, error)
    MeditationProjection reads the replay projection one consumer tick
    pre-folds: the material its compress requests name. A read that fails is
    reported by name and a tick never folds a partial projection.

type MeditationReport struct {
	// Triggered reports whether the card sequence ran over the bound and the
	// pass curated it.
	Triggered bool
	// Fates is the one channel each card's fate was filed under. Every card
	// of the sequence carries exactly one fate.
	Fates map[event.EventID]MeditationFate
	// Artifact is the condensed card the pass left behind, or nil beside an
	// untriggered sequence.
	Artifact *MeditationArtifact
	// Evicted lists the keys the pass evicted, in overflow order: every one
	// of them was tombstoned through the one eviction channel.
	Evicted []event.EventID
	// Failures is the engineering sink account: the condensation and channel
	// failures the pass degraded under, recorded and never swallowed silently.
	Failures []error
}
    MeditationReport is what one pass did: whether the sequence bound triggered
    it, the one fate each card was filed under, the condensed artifact it
    left behind, the keys it evicted through the tombstone channel, and the
    engineering sink account holding every failure the pass degraded under.

type MeditationRun struct {
	// Overflow is the over-limit card run, oldest first.
	Overflow []MeditationCard
	// Keep is the key card set the artifact references, highest value first.
	Keep []MeditationCard
}
    MeditationRun is the over-limit card run one condensation folds: the cards
    that overflowed the sequence bound, oldest first, and the key cards the pass
    keeps as artifact references beside them.

type MeditationSink func(ctx context.Context, artifact MeditationArtifact) error
    MeditationSink commits the product fact one meditation pass minted into the
    state: the condensed artifact, filed under its content-addressed identity.
    A commit that fails is reported by name, so a product is never counted as
    held before the state carries it.

type MeditationTick struct {
	// Read is the requests the tick took in: the pending requests naming the card
	// sequence it curates, in filing order.
	Read []event.CompressRequest
	// Reports is one meditation report per request whose pass completed, in the
	// order the requests were read.
	Reports []MeditationReport
	// Products is the artifact facts the tick committed into the state, one per
	// condensed pass.
	Products []MeditationArtifact
	// Folds is the compaction facts the tick's pre-folds committed into the
	// state, one per folded projection of the tick.
	Folds []replay.CompactionFact
	// Failures is the sink account of the tick: the pass, commit, pre-fold and
	// retirement failures it degraded under, recorded and never swallowed
	// silently.
	Failures []error
}
    MeditationTick is what one meditation tick did: the request facts it took
    in, the passes they drove, the product facts it committed into the state,
    the fold facts its pre-folds left and the failures it degraded under.
    A request stays pending until the products and the fold fact of its
    compression are committed, so the account names every request whose
    compression is still owed.

type MeditationValue struct {
	// Key is the card the assessment describes.
	Key event.EventID
	// Score is the assessed value of the card.
	Score int
}
    MeditationValue is the deterministic value assessment of one card: the score
    the pass decides keep and evict against, and nothing else. The assessment
    is a pure function of the card and the pass time, so two identical passes
    always reach identical verdicts and no LLM judgment enters the decision.

func AssessValue(card MeditationCard, now time.Time) MeditationValue
    AssessValue returns the deterministic value assessment of one card: a pure
    function of its causal link count, its frozen type and its age against the
    pass time. The same inputs always return the same score, so the pass that
    runs twice reaches the same fates.

type ObservationScan struct {
	// Watermark is the execution watermark the window opened after.
	Watermark time.Time
	// Partitions holds one tally line per observed partition, in declared order.
	Partitions []PartitionActivity
	// Facts is the hydrated sample in read order, bounded by the pass's fact limit.
	Facts []ScannedFact
	// Counts is the accounting of the read.
	Counts ScanCounts
	// Verdict is what the freshness judgment read from this pass.
	Verdict ScanVerdict
}
    ObservationScan is the bounded evidence of one pass over the observed face:
    the watermark the window opened at, the per-partition tallies, the hydrated
    sample, the accounting of that read and the verdict the freshness judgment
    read from it.

    The evidence is kept so the digest renders from this pass and never reads
    the fact chain a second time.

type ObservationScanRequest struct {
	// Since is the execution watermark the window opens strictly after; the zero
	// value opens the window at the epoch.
	Since time.Time
	// PageLimit bounds the reference page; zero runs the default bound.
	PageLimit int
	// FactLimit bounds the hydrated sample; zero runs the default bound.
	FactLimit int
}
    ObservationScanRequest is the window and bounds of one pass over the
    observed face.

type ObservedFacts interface {
	// QueryEvents returns the references matching opts.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
	// CommittedFact returns the committed fact the identity names; an identity
	// with no committed fact behind it is an error, never an empty fact.
	CommittedFact(ctx context.Context, id event.EventID) (event.Event, error)
}
    ObservedFacts is the read-only fact-chain face the reflection judgment
    runs on: the conditional retrieval the authorization-scoped query answers
    through, beside the committed-fact read that hydrates one reference into the
    fact it names.

    A query answers references only, which carry no trigger source,
    so the judgment hydrates the newest references one by one and stops at
    its first hit. Both calls are reads: a pass never writes the chain it
    observes. The redline list (3 rules): MUST answer references through the
    authorization-scoped query and hydrate each one through its committed fact.
    MUST NOT write the chain it observes. MUST NOT answer an identity that never
    committed as an empty fact.

type PartitionActivity struct {
	// Name is the namespace name the partition was resolved from.
	Name string
	// Partition is the storage partition the name resolves to.
	Partition int
	// References counts this partition's entries on the reference page, which
	// the read face answers without any hydration.
	References int
	// External counts hydrated facts whose lineage is not self-managed: the
	// freshness evidence.
	External int
	// SelfManaged counts hydrated facts the lineage derivation calls self-managed,
	// unknown attribution included.
	SelfManaged int
}
    PartitionActivity is one observed partition's tally in a pass: how much
    of the reference page belongs to it and how the hydrated sample splits by
    lineage.

type Plan struct {
	// Has unexported fields.
}
    Plan is the one scheduled reflection a document declares, resolved at
    assembly: the observing agent, the cadence, the prompt the input carries,
    the resolved observation face, the declared delivery targets and the anchor
    record its watermark persists in. A document enabling no meditation resolves
    to a nil plan, which starts no driver.

func (p *Plan) Agent() string
    Agent reports the observing agent's identity.

func (p *Plan) AnchorsPath() string
    AnchorsPath reports the anchor record the reflection watermark persists in:
    the store beside the shared storage tier the document names.

func (p *Plan) DeliverTo() []string
    DeliverTo reports the declared delivery white list the fired reflection
    flows back through.

func (p *Plan) Interval() time.Duration
    Interval reports the tick cadence the reflection loop runs at.

func (p *Plan) MinGap() time.Duration
    MinGap reports the rhythm floor between two fired reflections; zero keeps
    the cadence as the only bound.

func (p *Plan) ObservedNamespaces() []string
    ObservedNamespaces reports the resolved observation face: the declared
    namespaces inside the authorized read scope, or the agent's own namespace
    beside no declaration.

func (p *Plan) Prompt() string
    Prompt reports the static prompt every reflection input carries.

func (p *Plan) ReadNamespaces() []string
    ReadNamespaces reports the namespaces the observing agent may read across,
    its own first and every granted cross-namespace read after it.

type Reflection struct {
	// Has unexported fields.
}
    Reflection is the reflection loop of one agent: it reads the authorized
    observation face, decides whether a reflection is due, injects the input
    event into the agent's own loop session and advances the execution watermark
    when that turn is consumed.

    One mechanism covers every observation face: observing the agent's own
    partition is self-maintenance and observing others' partitions is the
    side-car meditation, and the action is the same explicit input in either
    case. A fire only writes the input; the watermark moves when the turn is
    consumed, to the injection moment, so every fact a yield window covered
    stays inside the next window. A read the pass cannot complete keeps the gate
    closed by name, and no fallback criterion is substituted for an unreachable
    chain.

func (r *Reflection) DeferredCount() int
    DeferredCount reports how many reflections yielded since the last consumed
    one.

func (r *Reflection) FreshSince(ctx context.Context, since time.Time) (bool, error)
    FreshSince reports whether the observed face holds a fact recorded after
    since whose lineage is not managed by the observing agent itself.

    A face that cannot be read is reported as an error and never as "nothing
    fresh": the fact chain is the judgment's only data face.

func (r *Reflection) LastDeferredAt() time.Time
    LastDeferredAt reports the moment of the most recent deferral.

func (r *Reflection) NoteOutcome(ctx context.Context, receipt ReflectionReceipt) error
    NoteOutcome settles what became of the reflection input awaiting one.

    A consumed turn advances the execution watermark to that input's injection
    moment and clears the deferral tally; a deferred turn clears the pending
    input and leaves the watermark in place; an in-flight turn keeps it pending.
    A report naming an input other than the pending one, or arriving while
    none is pending, is refused by name so a repeated report can never move the
    watermark again.

func (r *Reflection) ObservedNamespaces() []string
    ObservedNamespaces reports the resolved observation face of this reflection.

func (r *Reflection) Pending() (event.EventID, bool)
    Pending reports the identity of the reflection input awaiting its outcome,
    if one is pending.

func (r *Reflection) Scope() ReflectionScope
    Scope reports the resolved observation face, names and partitions together.

func (r *Reflection) Tick(ctx context.Context, now time.Time) TickOutcome
    Tick runs one reflection tick at now: it establishes the execution
    watermark, reads the observed face once, renders the digest from that same
    pass, applies the pending guard, the freshness judgment and the rhythm
    floor, and injects one explicit input event when all three open.

    A tick whose gate cannot be established reports TickClosed with the fact it
    could not establish and injects nothing; a tick that injected reports the
    input and settles the outcome the loop side handed back.

type ReflectionCadence struct {
	// Interval is the tick cadence, reported beside a pending input that has
	// outlived it by three intervals.
	Interval time.Duration
	// MinGap is the floor between two consumed reflections.
	MinGap time.Duration
}
    ReflectionCadence is how often one reflection may fire: the tick cadence and
    the floor between two consumed reflections.

type ReflectionConfig struct {
	// Loop is the loop session the reflection reflects into.
	Loop ReflectionLoop
	// Face is the observation face and the bounds it is read under.
	Face ReflectionFace
	// Cadence is how often one reflection may fire.
	Cadence ReflectionCadence
	// Prompt is the static reflection prompt carried beside the digest.
	Prompt string
}
    ReflectionConfig is the assembly input of one reflecting agent's loop:
    the loop session it reflects into, the face it observes and the bounds it
    reads it under, its cadence and the static prompt its input carries.

    The prompt is text the reflection renders without spending a model call.

type ReflectionFace struct {
	// ObservedNamespaces declares the face; empty means the agent's own partition.
	ObservedNamespaces []string
	// ReadNamespaces is the authorized read scope of the agent.
	ReadNamespaces []string
	// PageLimit bounds one pass over the observed face; zero runs the default.
	PageLimit int
	// FactLimit bounds the facts one digest lists; zero runs the default.
	FactLimit int
}
    ReflectionFace is the face one reflection observes and the bounds it reads
    it under: the declared namespaces, the authorized read scope they must sit
    inside and the per-pass and per-digest bounds.

type ReflectionInjection struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// At is the injection moment, which is the watermark candidate a consumed
	// turn advances the gate to.
	At time.Time
	// Event is the explicit input event as it was handed to the loop session.
	Event event.Event
	// Digest is the self-state summary the input carried.
	Digest DigestSummary
}
    ReflectionInjection is one injected reflection input as the firing tick
    reports it: the identity it committed under, the moment it was injected at,
    the event itself and the digest it carried.

type ReflectionInput interface {
	// InjectReflection commits one reflection input into the agent's own loop
	// session and reports the outcome of its turn.
	InjectReflection(ctx context.Context, evt event.Event) (ReflectionReceipt, error)
}
    ReflectionInput is the loop-session face one fired reflection injects its
    explicit input event through: the assembly commits the event as a fact of
    the agent's own partition, drives the observing agent's own loop session
    with it and reports what became of that turn.

    The receipt is the only evidence that a reflection turn ran: a fire that
    was only written never advances the watermark. The redline list (3 rules):
    MUST commit the reflection input as a fact of the agent's own partition.
    MUST report the outcome of the turn the injected input drove. MUST NOT
    report a reflection turn as consumed from a fire that was only written.

type ReflectionLoop struct {
	// Agent is the observing agent's own identity and namespace.
	Agent string
	// Session is the loop session the reflection input is injected into, which is
	// always this agent's own session.
	Session string
}

    ReflectionLoop is the loop session one reflection reflects into: the
    observing agent's own identity and namespace, and the session its input is
    injected into.

type ReflectionOutcome string
    ReflectionOutcome is what became of one injected reflection input.

const (
	// ReflectionConsumed reports a reflection turn that ran: the watermark
	// advances to the moment the turn was injected at.
	ReflectionConsumed ReflectionOutcome = "consumed"
	// ReflectionDeferred reports a reflection turn that yielded to real input at
	// either yield point: the watermark stays where it stands and the very same
	// facts are re-evaluated on the next tick.
	ReflectionDeferred ReflectionOutcome = "deferred"
	// ReflectionInFlight reports an injected input whose turn has not reached an
	// outcome yet; the input stays pending and no second one is injected.
	ReflectionInFlight ReflectionOutcome = "in-flight"
)
type ReflectionReceipt struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// Outcome is what became of the injected turn.
	Outcome ReflectionOutcome
	// At is the moment the outcome was reached; it dates the deferral tally and
	// never moves the watermark.
	At time.Time
	// Reason is the named ground of a deferral, when the loop side reports one.
	Reason string
}
    ReflectionReceipt is the loop side's report of one injected reflection
    input: the identity the fact committed under, what became of its turn and
    the moment that outcome was reached.

type ReflectionScope struct {
	// Own is the observing agent's own namespace, which is always observably
	// legal and is the face of an undeclared observation surface.
	Own string
	// Names lists the observed namespaces in declared order, deduplicated.
	Names []string
	// Partitions lists the storage partition each name resolves to, in the same
	// order as Names; the query scans exactly these partitions.
	Partitions []int
}

    ReflectionScope is the resolved observation face of one reflecting agent:
    the namespaces it observes and the storage partitions those namespaces
    resolve to.

    The face is an application authorization rather than a capability the
    reflection grants itself. A declared face stays within the namespaces
    the application authorized for reading plus the agent's own partition,
    an undeclared face is the agent's own partition, and a declared face that
    reaches past that boundary is refused at assembly.

type ScanCounts struct {
	// ReferenceTotal is the size of the reference page the pass read.
	ReferenceTotal int
	// HydratedTotal is the number of facts the pass hydrated.
	HydratedTotal int
	// Unreadable counts references the pass could not hydrate; an unreadable fact
	// is never counted as freshness.
	Unreadable int
	// UnknownLineage counts hydrated facts that carry no trigger-source stamp.
	UnknownLineage int
	// Foreign counts hydrated facts filed under a partition outside the observed
	// face; such a fact is never counted as freshness either.
	Foreign int
}
    ScanCounts is the accounting of one pass: the reference page it read,
    how many facts it hydrated, and the references that yielded no freshness
    evidence.

type ScanDigest struct {
	// Has unexported fields.
}
    ScanDigest renders the self-state summary out of one completed observation
    pass: the watermark the window opened at, one tally line per observed
    partition and the hydrated facts the pass kept, all in the deterministic
    order the pass read them.

    The rendering spends no model call, blocks on no external surface, reads the
    fact chain no further than the pass already did and stays bounded whatever
    the backlog: partitions and facts beyond the bounds, and references the pass
    did not hydrate, are counted in Omitted instead of being dropped in silence.

func NewScanDigest(scan *ObservationScan, factLimit int) *ScanDigest
    NewScanDigest builds a digest over one completed pass, limiting the listed
    facts to factLimit entries; a non-positive limit runs the default bound.

func (d *ScanDigest) Render(ctx context.Context, since Watermark) (DigestSummary, error)
    Render returns the bounded self-state summary of the pass this digest was
    built over, refusing by name when there is no pass or when the pass opened
    at another watermark than the one since names.

type ScanVerdict struct {
	// Novelty reports whether the face holds a fact newer than the watermark whose
	// lineage is not self-managed.
	Novelty bool
	// Recent is the newest fact whose lineage is not self-managed, when the pass
	// found one.
	Recent *ScannedFact
}
    ScanVerdict is what the freshness judgment read off one pass: whether
    the face holds a fact newer than the watermark that is not self-managed,
    and that fact.

type ScannedFact struct {
	// Identity is the fact and the partition it was filed under.
	Identity FactIdentity
	// Content is what the fact recorded.
	Content FactContent
	// Attribution is whose traffic the fact is.
	Attribution FactAttribution
}
    ScannedFact is one hydrated fact of a pass, kept in the deterministic order
    the pass read it.

type TickDecision struct {
	// Disposition is what this tick decided.
	Disposition TickDisposition
	// Reason is the named ground of the disposition; a closed gate names the fact
	// it could not establish.
	Reason string
	// Cause is the named failure behind a closed gate, when one was returned by a
	// read or by the watermark source.
	Cause error
}
    TickDecision is what one reflection tick decided and the named ground behind
    that decision.

type TickDisposition string
    TickDisposition is what one reflection tick decided.

const (
	// TickFired reports that this tick injected a reflection input.
	TickFired TickDisposition = "fired"
	// TickPending reports that an injected reflection input still awaits its
	// outcome, so this tick injected nothing.
	TickPending TickDisposition = "pending"
	// TickClosed reports that the gate stayed closed because a fact it needs
	// could not be established.
	TickClosed TickDisposition = "closed"
	// TickNoNovelty reports a readable face holding nothing newer than the
	// watermark with non-self-managed lineage.
	TickNoNovelty TickDisposition = "no-novelty"
	// TickRhythm reports that the rhythm floor has not elapsed since the last
	// consumed reflection.
	TickRhythm TickDisposition = "rhythm"
)
type TickOutcome struct {
	// Decision is what this tick decided.
	Decision TickDecision
	// Scan is the evidence of the pass this tick read, when a pass ran.
	Scan *ObservationScan
	// Digest is the self-state summary rendered from that same pass.
	Digest DigestSummary
	// Injection is the reflection input this tick injected, when it fired.
	Injection *ReflectionInjection
}
    TickOutcome is the decision one reflection tick reached, the evidence of the
    pass it read and the injection it made.

type Watermark struct {
	// Moment is the moment the last consumed reflection turn advanced the
	// watermark to. Its zero value marks a start with no prior execution.
	Moment time.Time
}

    Watermark is the execution watermark of the reflection loop: the moment the
    last reflection turn advanced it to, which heads the freshness window and
    bounds how soon the next reflection may run.

    Injection alone never moves the watermark; only a reflection turn that is
    actually consumed advances it, to the moment the turn was injected at.
    A reflection that yields leaves the watermark in place, so the facts that
    reflection covered stay no older than the watermark instead of being burned
    out of the window, and a start with no watermark passes once and then locks
    itself. The watermark is advanced by consumption only, and the freshness
    judgment excludes the agent's own managed lineage, so a notification the
    agent sends about its own work never makes the agent reflect on itself
    again.

type WatermarkGate interface {
	// Current returns the current execution watermark. A watermark that cannot
	// be established is reported as an error, and the gate stays closed.
	Current(ctx context.Context) (Watermark, error)

	// Advance moves the watermark to the moment a consumed reflection turn was
	// injected at, and leaves it unmoved when that turn was deferred.
	Advance(ctx context.Context, moment time.Time) error
}
    WatermarkGate is the reflection loop's freshness gate over the execution
    watermark.

    The gate reads the watermark and advances it after a reflection turn is
    consumed. A watermark that is missing, or that cannot be read or parsed,
    keeps the gate closed and is reported by name; the gate is never opened
    on a fresh default or on a fallback criterion when the watermark cannot be
    established. The redline list (3 rules): MUST report a watermark it cannot
    establish as an error and keep the gate closed. MUST NOT open the gate on
    a fresh default or on a fallback criterion. MUST NOT advance the watermark
    past a reflection turn that was deferred.

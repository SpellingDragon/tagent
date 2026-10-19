package meditation // import "github.com/SpellingDragon/tagent/evolution/meditation"

The pre-fold face of the meditation consumer: the fold a consumer runs when it
consumes a compress request, the compaction fact it files into the state, and
the fold identity that fact is quoted by. A pre-fold runs ahead of the request
boundary, so the assembly path reads the folded projection and never folds it
inline.

Skeleton compile-time assertions for the meditation shapes: the consumer faces
a tick reads, the engine's pass, judgment and tick signatures, the pre-fold seam
and the refusals each unwired face answers by name. A renamed field or a widened
signature stops this file from compiling.

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

const MeditationArtifactType = "memory.condensed_card"
    MeditationArtifactType is the frozen type label of the condensed card
    artifact one meditation pass leaves behind.

const RetentionMeditation ledger.RetentionKind = "curation_reference"
    RetentionMeditation is the kind of a retention registration protecting
    material a condensed artifact still references: the hold a meditation pass
    takes on each key its artifact cites, so the referenced original is never
    tombstoned or reclaimed while the artifact can still recall it by key.

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

func NewMeditation(policy MeditationPolicy, retention *ledger.Retention, lifecycle *ledger.Lifecycle, condense Condenser, options ...MeditationOption) (*Meditation, error)
    NewMeditation returns the meditation engine over the retention ledger and
    the lifecycle it drives: the ledger holds what the artifact references
    and the lifecycle owns the tombstone channel every eviction travels. A nil
    condenser runs the deterministic engineering condensation. The retention
    ledger must be the one the lifecycle was built over, so a reference the pass
    holds is the protection the eviction consults. The options wire the consumer
    face: the request log a tick consumes, the card sequence it curates and the
    state its product facts are committed into.

func Prefold(ctx context.Context, projection *replay.ReplayProjection) (replay.CompactionFact, error)
    Prefold folds the projection a consumer owns ahead of assembly and answers
    the compaction payload the fold left, so the fold fact is filed when
    its request is consumed and the request boundary only reads the folded
    projection. A projection already carrying a compaction payload is answered
    with it: one body of material folds once.

TYPES

type Condenser func(ctx context.Context, run MeditationRun, now time.Time) (string, error)
    Condenser folds one over-limit card run into its condensed summary:
    the seat an LLM meditation would take beside the deterministic engineering
    condensation. A condenser that fails degrades the pass to that engineering
    fallback — the overflow run sinks into an "(earlier n items)" count line —
    and never collapses the pass or rewrites a fact.

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
    request naming that sequence it runs one pass, commits the condensed
    artifact the pass minted into the state, pre-folds the compressed projection
    once through the wired pre-fold, commits the compaction fact and retires the
    request. A request naming other material stays pending, and a pass, commit,
    pre-fold or retirement that failed keeps its request pending and lands in
    the tick account, so a request is never counted consumed before the products
    of its compression are held.

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

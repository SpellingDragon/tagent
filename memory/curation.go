// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// The memory curation pass: the deterministic value assessment, the trigger
// and the condensed artifact of one over-limit card sequence. Curation files
// every card's fate under exactly one of the three channels — retain, evict or
// protect — never a fourth mechanism: an eviction travels the one tombstone
// channel the lifecycle already owns, a referenced original is held against
// that channel and reads back byte for byte, and no pass ever rewrites,
// overwrites or migrates original event bytes or their owner/format
// attribution. A failing condensation sinks into the engineering fallback —
// the overflow run collapses into an "(earlier n items)" count line — instead
// of collapsing the pass, so the recall chain stays whole through every
// outcome.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// CurationFate names the one channel a card's curation fate is filed under:
// exactly one of retain, evict or protect for every card of a pass. The fates
// are mutually exclusive and exhaustive — a card belongs to one channel and
// never to a fourth — so the eviction a pass declares is the whole eviction it
// performs, and it is performed on the tombstone channel alone.
type CurationFate string

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

// RetentionCuration is the kind of a retention registration protecting
// material a condensed artifact still references: the hold a curation pass
// takes on each key its artifact cites, so the referenced original is never
// tombstoned or reclaimed while the artifact can still recall it by key.
const RetentionCuration RetentionKind = "curation_reference"

// CurationArtifactType is the frozen type label of the condensed card
// artifact one curation pass leaves behind.
const CurationArtifactType = "memory.condensed_card"

// CurationCard is one card line of the sequence a curation pass runs over: the
// [evt_key] ticket of the fact behind it, its frozen type, the small rendering
// text of the line, the time the fact was recorded, and the count of standing
// causal links — references and parent keys — that point at the fact.
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

// CurationValue is the deterministic value assessment of one card: the score
// the pass decides keep and evict against, and nothing else. The assessment is
// a pure function of the card and the pass time, so two identical passes
// always reach identical verdicts and no LLM judgment enters the decision.
type CurationValue struct {
	// Key is the card the assessment describes.
	Key event.EventID
	// Score is the assessed value of the card.
	Score int
}

// CurationLinkWeight, CurationSkeletonWeight and CurationRecencyWeight are the
// value assessment weights: a standing causal link is worth more than the
// boundary skeleton weight, which is worth more than the recency bonus.
const (
	// CurationLinkWeight is the value one standing causal link adds.
	CurationLinkWeight = 10
	// CurationSkeletonWeight is the value a boundary fact (external input or
	// agent output) adds: the task skeleton the condensation keeps.
	CurationSkeletonWeight = 5
	// CurationRecencyWeight is the value a fact inside the recent window adds.
	CurationRecencyWeight = 2
)

// curationRecentWindow is the age window the recency bonus is measured over.
const curationRecentWindow = 24 * time.Hour

// curationSummaryLimit bounds the condensed summary one pass writes, so a
// condensation of a long overflow run stays one bounded line.
const curationSummaryLimit = 240

// AssessValue returns the deterministic value assessment of one card: a pure
// function of its causal link count, its frozen type and its age against the
// pass time. The same inputs always return the same score, so the pass that
// runs twice reaches the same fates.
func AssessValue(card CurationCard, now time.Time) CurationValue {
	score := card.Links * CurationLinkWeight
	if card.Type == event.TypeExternalInput || card.Type == event.TypeAgentOutput {
		score += CurationSkeletonWeight
	}
	if age := now.Sub(card.RecordedAt); age >= 0 && age <= curationRecentWindow {
		score += CurationRecencyWeight
	}
	return CurationValue{Key: card.Key, Score: score}
}

// CurationPolicy shapes one curation pass: the sequence bound that triggers
// it, how many key cards survive as artifact references, and the value floor
// below which an unprotected overflow card is evicted.
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

// CurationArtifact is the condensed card one pass leaves behind: the new
// reference the curation produces, never a rewrite of any fact it condenses.
// Its refs are the [evt_key] tickets of the key cards it keeps, so the
// condensed result stays traceable and recall still refills by key. Sunk
// counts the card lines the pass collapsed into its count line when it
// degraded to the engineering fallback, and is zero while a condensation
// holds.
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

// CurationRun is the over-limit card run one condensation folds: the cards
// that overflowed the sequence bound, oldest first, and the key cards the pass
// keeps as artifact references beside them.
type CurationRun struct {
	// Overflow is the over-limit card run, oldest first.
	Overflow []CurationCard
	// Keep is the key card set the artifact references, highest value first.
	Keep []CurationCard
}

// Condenser folds one over-limit card run into its condensed summary: the
// seat an LLM curation would take beside the deterministic engineering
// condensation. A condenser that fails degrades the pass to that engineering
// fallback — the overflow run sinks into an "(earlier n items)" count line —
// and never collapses the pass or rewrites a fact.
type Condenser func(ctx context.Context, run CurationRun, now time.Time) (string, error)

// CurationReport is what one pass did: whether the sequence bound triggered
// it, the one fate each card was filed under, the condensed artifact it left
// behind, the keys it evicted through the tombstone channel, and the
// engineering sink account holding every failure the pass degraded under.
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

// CurationCards reads the card sequence one curation tick curates: the cards of
// the partition the tick owns, oldest first as the caller renders them. A read
// that fails is reported by name and a tick never curates a partial sequence.
type CurationCards func(ctx context.Context) ([]CurationCard, error)

// CurationSink commits the product fact one curation pass minted into the state:
// the condensed artifact, filed under its content-addressed identity. A commit
// that fails is reported by name, so a product is never counted as held before
// the state carries it.
type CurationSink func(ctx context.Context, artifact CurationArtifact) error

// CurationTick is what one curation tick did: the request facts it took in, the
// passes they drove, the product facts it committed into the state, the fold
// facts its pre-folds left and the failures it degraded under. A request stays
// pending until the products and the fold fact of its compression are committed,
// so the account names every request whose compression is still owed.
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

// Curation is the memory curation engine: it assesses card value
// deterministically, triggers on the sequence bound, condenses the over-limit
// run into a new artifact that relates to the existing facts through its key
// references, holds those references against eviction, and evicts the rest
// through the one tombstone channel the lifecycle owns. The engine rewrites
// no original fact and no reference metadata, whatever the outcome.
//
// A consumer tick drives the same pass from the compress request facts its
// producers filed: the request is the judgment that material is over its bound
// and the tick is the compression that answers it. A wired pre-fold lets the
// tick fold the compressed projection and commit its compaction fact, so the
// assembly path reads a folded projection.
type Curation struct {
	policy     CurationPolicy
	retention  *Retention
	lifecycle  *Lifecycle
	condense   Condenser
	requests   *CompressRequests
	cards      CurationCards
	products   CurationSink
	projection CurationProjection
	folds      CurationFolds
}

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

// CurationOption names one setting of the curation engine at construction time.
type CurationOption func(*Curation)

// WithCompressRequests wires the compress request log the consumer tick reads:
// the requests its producers filed, and the retirement a tick performs once the
// products of one compression are committed.
func WithCompressRequests(requests *CompressRequests) CurationOption {
	return func(curation *Curation) { curation.requests = requests }
}

// WithCurationCards wires the card sequence source the consumer tick curates. A
// failed read is reported by name and never read as an empty sequence.
func WithCurationCards(cards CurationCards) CurationOption {
	return func(curation *Curation) { curation.cards = cards }
}

// WithCurationProducts wires the state face the consumer tick commits its product
// facts into: the condensed artifact of every pass that condensed a run.
func WithCurationProducts(products CurationSink) CurationOption {
	return func(curation *Curation) { curation.products = products }
}

// NewCuration returns the curation engine over the retention ledger and the
// lifecycle it drives: the ledger holds what the artifact references and the
// lifecycle owns the tombstone channel every eviction travels. A nil
// condenser runs the deterministic engineering condensation. The retention
// ledger must be the one the lifecycle was built over, so a reference the pass
// holds is the protection the eviction consults. The options wire the consumer
// face: the request log a tick consumes, the card sequence it curates and the
// state its product facts are committed into.
func NewCuration(policy CurationPolicy, retention *Retention, lifecycle *Lifecycle, condense Condenser, options ...CurationOption) (*Curation, error) {
	if retention == nil || lifecycle == nil {
		return nil, ErrCurationUnwired
	}
	if policy.MaxCards < 1 {
		return nil, ErrCurationPolicy
	}
	if condense == nil {
		condense = condenseCards
	}
	curation := &Curation{policy: policy, retention: retention, lifecycle: lifecycle, condense: condense}
	for _, option := range options {
		option(curation)
	}
	return curation, nil
}

// Curate runs one curation pass over the card sequence at the given time.
//
// A sequence within the bound is never curated: every card retains and no
// artifact is produced. A sequence over the bound triggers the pass: the
// over-limit run is the oldest cards beyond the bound, the condensation folds
// that run into the artifact that relates to the existing facts through its
// key references, each referenced key is held against eviction (the referenced
// original is never deleted), the unprotected cards below the value floor are
// evicted through the tombstone channel alone, and everything else retains. A
// failing condensation sinks into the engineering fallback — the overflow run
// collapses into an "(earlier n items)" count line — and a failing channel
// step degrades that card to retention with its failure recorded in the sink
// account, so the pass degrades instead of collapsing and no outcome rewrites
// an original fact.
func (c *Curation) Curate(ctx context.Context, now time.Time, cards []CurationCard) (CurationReport, error) {
	report := CurationReport{Fates: map[event.EventID]CurationFate{}}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	seen := map[event.EventID]bool{}
	for _, card := range cards {
		if card.Key == "" || seen[card.Key] {
			return report, fmt.Errorf("%w: %q", ErrCurationCardKey, card.Key)
		}
		seen[card.Key] = true
	}
	if len(cards) <= c.policy.MaxCards {
		for _, card := range cards {
			report.Fates[card.Key] = CurationRetain
		}
		return report, nil
	}
	report.Triggered = true
	ordered := append([]CurationCard(nil), cards...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].RecordedAt.Equal(ordered[j].RecordedAt) {
			return ordered[i].RecordedAt.Before(ordered[j].RecordedAt)
		}
		return ordered[i].Key < ordered[j].Key
	})
	overflow := ordered[:len(ordered)-c.policy.MaxCards]
	window := ordered[len(ordered)-c.policy.MaxCards:]

	keep := make([]CurationCard, 0, len(overflow))
	for _, card := range overflow {
		if AssessValue(card, now).Score >= c.policy.MinScore {
			keep = append(keep, card)
		}
	}
	sort.SliceStable(keep, func(i, j int) bool {
		left, right := AssessValue(keep[i], now), AssessValue(keep[j], now)
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		return keep[i].Key < keep[j].Key
	})
	if c.policy.Keep > 0 && len(keep) > c.policy.Keep {
		keep = keep[:c.policy.Keep]
	}

	summary, err := c.condense(ctx, CurationRun{Overflow: overflow, Keep: keep}, now)
	sunk := 0
	if err != nil {
		report.Failures = append(report.Failures, err)
		sunk = len(overflow)
		summary = fmt.Sprintf("(earlier %d items)", sunk)
	}
	refKeys := make([]event.EventID, 0, len(keep))
	for _, card := range keep {
		refKeys = append(refKeys, card.Key)
	}
	artifact := CurationArtifact{
		Key:       curationArtifactKey(refKeys),
		Type:      CurationArtifactType,
		Summary:   summary,
		Refs:      refKeys,
		Sunk:      sunk,
		CreatedAt: now,
	}
	report.Artifact = &artifact

	holder := HolderID("curation:" + string(artifact.Key))
	for _, card := range keep {
		if _, err := c.retention.Register(ctx, card.Key, holder, RetentionRecord{
			Kind:       RetentionCuration,
			Type:       card.Type,
			RecordedAt: card.RecordedAt,
		}); err != nil {
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = CurationRetain
			continue
		}
		report.Fates[card.Key] = CurationProtect
	}
	for _, card := range overflow {
		if _, decided := report.Fates[card.Key]; decided {
			continue
		}
		if AssessValue(card, now).Score >= c.policy.MinScore {
			report.Fates[card.Key] = CurationRetain
			continue
		}
		protected, err := c.retention.Held(ctx, card.Key)
		if err != nil {
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = CurationRetain
			continue
		}
		if protected {
			report.Fates[card.Key] = CurationProtect
			continue
		}
		if err := c.lifecycle.Delete(ctx, card.Key, now); err != nil {
			if errors.Is(err, ErrProtected) {
				report.Fates[card.Key] = CurationProtect
				continue
			}
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = CurationRetain
			continue
		}
		report.Fates[card.Key] = CurationEvict
		report.Evicted = append(report.Evicted, card.Key)
	}
	for _, card := range window {
		if _, decided := report.Fates[card.Key]; decided {
			continue
		}
		protected, err := c.retention.Held(ctx, card.Key)
		if err != nil {
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = CurationRetain
			continue
		}
		if protected {
			report.Fates[card.Key] = CurationProtect
			continue
		}
		report.Fates[card.Key] = CurationRetain
	}
	return report, nil
}

// Judge files the request fact the card sequence's bound judgment is: a sequence
// at or within the bound files nothing and answers false, and an over-limit one
// files the request naming the sequence, the curation producer and the bound
// reason, and answers that request with true. The judgment is filed under the
// sequence's content-addressed identity, so judging one body of material again
// files no second request and keeps the first filing moment. A sequence no fate
// could be filed over is refused by name and files nothing.
func (c *Curation) Judge(ctx context.Context, now time.Time, cards []CurationCard) (CompressRequest, bool, error) {
	if err := ctx.Err(); err != nil {
		return CompressRequest{}, false, err
	}
	if c.requests == nil {
		return CompressRequest{}, false, ErrCurationRequestsUnwired
	}
	seen := map[event.EventID]bool{}
	for _, card := range cards {
		if card.Key == "" || seen[card.Key] {
			return CompressRequest{}, false, fmt.Errorf("%w: %q", ErrCurationCardKey, card.Key)
		}
		seen[card.Key] = true
	}
	if len(cards) <= c.policy.MaxCards {
		return CompressRequest{}, false, nil
	}
	request := CompressRequestFor(CompressProducerCuration, string(curationSequenceKey(cards)), CompressReasonCuration)
	request.At = now
	filed, err := c.requests.Produce(ctx, request)
	if err != nil {
		return CompressRequest{}, false, err
	}
	return filed, true, nil
}

// Tick runs one curation tick at now: it reads the card sequence it curates and
// the compress requests pending in the request log, and for each pending request
// naming that sequence it runs one pass, commits the condensed artifact the pass
// minted into the state, pre-folds the compressed projection once through the
// wired pre-fold, commits the compaction fact and retires the request. A request
// naming other material stays pending, and a pass, commit, pre-fold or retirement
// that failed keeps its request pending and lands in the tick account, so a
// request is never counted consumed before the products of its compression are
// held.
func (c *Curation) Tick(ctx context.Context, now time.Time) (CurationTick, error) {
	var tick CurationTick
	if err := ctx.Err(); err != nil {
		return tick, err
	}
	if c.requests == nil {
		return tick, ErrCurationRequestsUnwired
	}
	if c.cards == nil {
		return tick, ErrCurationCardsUnwired
	}
	cards, err := c.cards(ctx)
	if err != nil {
		return tick, fmt.Errorf("memory: reading the curation card sequence: %w", err)
	}
	requests, err := c.requests.Pending(ctx)
	if err != nil {
		return tick, fmt.Errorf("memory: reading the compress requests: %w", err)
	}
	subject := string(curationSequenceKey(cards))
	prefolded := false
	for _, request := range requests {
		if request.Subject != subject {
			continue
		}
		tick.Read = append(tick.Read, request)
		report, err := c.Curate(ctx, now, cards)
		if err != nil {
			tick.Failures = append(tick.Failures, err)
			continue
		}
		tick.Reports = append(tick.Reports, report)
		if report.Artifact != nil {
			if c.products == nil {
				tick.Failures = append(tick.Failures, ErrCurationProductsUnwired)
				continue
			}
			if err := c.products(ctx, *report.Artifact); err != nil {
				tick.Failures = append(tick.Failures, err)
				continue
			}
			tick.Products = append(tick.Products, *report.Artifact)
		}
		if c.projection != nil && !prefolded {
			payload, err := c.prefold(ctx)
			if err != nil {
				tick.Failures = append(tick.Failures, err)
				continue
			}
			tick.Folds = append(tick.Folds, payload)
			prefolded = true
		}
		if err := c.requests.Consume(ctx, request.ID); err != nil {
			tick.Failures = append(tick.Failures, err)
		}
	}
	return tick, nil
}

// prefold folds the projection the tick's consumer owns and commits the
// compaction fact it left into the state: the fold runs at consumption time and
// the request boundary reads its result.
func (c *Curation) prefold(ctx context.Context) (CompactionFact, error) {
	if c.projection == nil {
		return CompactionFact{}, ErrCurationProjectionUnwired
	}
	if c.folds == nil {
		return CompactionFact{}, ErrCurationFoldsUnwired
	}
	projection, err := c.projection(ctx)
	if err != nil {
		return CompactionFact{}, fmt.Errorf("memory: reading the projection to pre-fold: %w", err)
	}
	payload, err := Prefold(ctx, projection)
	if err != nil {
		return CompactionFact{}, err
	}
	if err := c.folds(ctx, payload); err != nil {
		return CompactionFact{}, err
	}
	return payload, nil
}

// condenseCards is the deterministic engineering condensation: the overflow
// run folded into one bounded line, oldest first. It is the production
// condenser beside no model and the shape the degraded fallback returns to,
// and it never touches a fact.
func condenseCards(ctx context.Context, run CurationRun, _ time.Time) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	lines := make([]string, 0, len(run.Overflow))
	for _, card := range run.Overflow {
		if card.Summary == "" {
			continue
		}
		lines = append(lines, card.Summary)
	}
	joined := strings.Join(lines, " / ")
	if len(joined) > curationSummaryLimit {
		joined = joined[:curationSummaryLimit]
	}
	return joined, nil
}

// curationSequenceKey derives the identity of one card sequence from the keys,
// types and recorded times of its cards: the same body of material always answers
// the same identity, so the bound judgment filed over it is quoted by one key for
// as long as that material stands. The order the cards arrived in names nothing
// about the material, so the parts are sorted before they are hashed.
func curationSequenceKey(cards []CurationCard) event.EventID {
	parts := make([]string, 0, len(cards))
	for _, card := range cards {
		parts = append(parts, string(card.Key)+"|"+card.Type+"|"+card.RecordedAt.UTC().Format(time.RFC3339Nano))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return event.EventID("seq-" + hex.EncodeToString(sum[:8]))
}

// curationArtifactKey derives the content-addressed identity of one condensed
// artifact from the key references it carries: the same condensation always
// mints the same identity, so a re-run of one pass never leaves two artifacts
// of the same refs behind.
func curationArtifactKey(refs []event.EventID) event.EventID {
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		parts = append(parts, string(ref))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return event.EventID("cur-" + hex.EncodeToString(sum[:8]))
}

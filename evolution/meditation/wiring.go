// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// The meditation consumer wiring: the engine that assesses a card sequence and
// condenses its over-limit run, the tick that drives it from the compress
// request facts its producers filed, and the option surface that wires the card
// source, the product sink and the pre-fold. The engine rewrites no original
// fact and no reference metadata, whatever the outcome.
package meditation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// MeditationReport is what one pass did: whether the sequence bound triggered
// it, the one fate each card was filed under, the condensed artifact it left
// behind, the keys it evicted through the tombstone channel, and the
// engineering sink account holding every failure the pass degraded under.
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

// MeditationCards reads the card sequence one meditation tick curates: the cards of
// the partition the tick owns, oldest first as the caller renders them. A read
// that fails is reported by name and a tick never curates a partial sequence.
type MeditationCards func(ctx context.Context) ([]MeditationCard, error)

// MeditationSink commits the product fact one meditation pass minted into the state:
// the condensed artifact, filed under its content-addressed identity. A commit
// that fails is reported by name, so a product is never counted as held before
// the state carries it.
type MeditationSink func(ctx context.Context, artifact MeditationArtifact) error

// MeditationTick is what one meditation tick did: the request facts it took in, the
// passes they drove, the product facts it committed into the state, the fold
// facts its pre-folds left and the failures it degraded under. A request stays
// pending until the products and the fold fact of its compression are committed,
// so the account names every request whose compression is still owed.
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

// Meditation is the memory meditation engine: it assesses card value
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
type Meditation struct {
	policy     MeditationPolicy
	retention  *ledger.Retention
	lifecycle  *ledger.Lifecycle
	condense   Condenser
	requests   *memory.CompressRequests
	cards      MeditationCards
	products   MeditationSink
	projection MeditationProjection
	folds      MeditationFolds
}

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

// MeditationOption names one setting of the meditation engine at construction time.
type MeditationOption func(*Meditation)

// WithCompressRequests wires the compress request log the consumer tick reads:
// the requests its producers filed, and the retirement a tick performs once the
// products of one compression are committed.
func WithCompressRequests(requests *memory.CompressRequests) MeditationOption {
	return func(meditation *Meditation) { meditation.requests = requests }
}

// WithMeditationCards wires the card sequence source the consumer tick curates. A
// failed read is reported by name and never read as an empty sequence.
func WithMeditationCards(cards MeditationCards) MeditationOption {
	return func(meditation *Meditation) { meditation.cards = cards }
}

// WithMeditationProducts wires the state face the consumer tick commits its product
// facts into: the condensed artifact of every pass that condensed a run.
func WithMeditationProducts(products MeditationSink) MeditationOption {
	return func(meditation *Meditation) { meditation.products = products }
}

// NewMeditation returns the meditation engine over the retention ledger and the
// lifecycle it drives: the ledger holds what the artifact references and the
// lifecycle owns the tombstone channel every eviction travels. A nil
// condenser runs the deterministic engineering condensation. The retention
// ledger must be the one the lifecycle was built over, so a reference the pass
// holds is the protection the eviction consults. The options wire the consumer
// face: the request log a tick consumes, the card sequence it curates and the
// state its product facts are committed into.
func NewMeditation(policy MeditationPolicy, retention *ledger.Retention, lifecycle *ledger.Lifecycle, condense Condenser, options ...MeditationOption) (*Meditation, error) {
	if retention == nil || lifecycle == nil {
		return nil, ErrMeditationUnwired
	}
	if policy.MaxCards < 1 {
		return nil, ErrMeditationPolicy
	}
	if condense == nil {
		condense = condenseCards
	}
	meditation := &Meditation{policy: policy, retention: retention, lifecycle: lifecycle, condense: condense}
	for _, option := range options {
		option(meditation)
	}
	return meditation, nil
}

// Curate runs one meditation pass over the card sequence at the given time.
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
func (c *Meditation) Curate(ctx context.Context, now time.Time, cards []MeditationCard) (MeditationReport, error) {
	report := MeditationReport{Fates: map[event.EventID]MeditationFate{}}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	seen := map[event.EventID]bool{}
	for _, card := range cards {
		if card.Key == "" || seen[card.Key] {
			return report, fmt.Errorf("%w: %q", ErrMeditationCardKey, card.Key)
		}
		seen[card.Key] = true
	}
	if len(cards) <= c.policy.MaxCards {
		for _, card := range cards {
			report.Fates[card.Key] = MeditationRetain
		}
		return report, nil
	}
	report.Triggered = true
	ordered := append([]MeditationCard(nil), cards...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].RecordedAt.Equal(ordered[j].RecordedAt) {
			return ordered[i].RecordedAt.Before(ordered[j].RecordedAt)
		}
		return ordered[i].Key < ordered[j].Key
	})
	overflow := ordered[:len(ordered)-c.policy.MaxCards]
	window := ordered[len(ordered)-c.policy.MaxCards:]

	keep := make([]MeditationCard, 0, len(overflow))
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

	summary, err := c.condense(ctx, MeditationRun{Overflow: overflow, Keep: keep}, now)
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
	artifact := MeditationArtifact{
		Key:       meditationArtifactKey(refKeys),
		Type:      MeditationArtifactType,
		Summary:   summary,
		Refs:      refKeys,
		Sunk:      sunk,
		CreatedAt: now,
	}
	report.Artifact = &artifact

	holder := ledger.HolderID(MeditationHolderPrefix + string(artifact.Key))
	for _, card := range keep {
		if _, err := c.retention.Register(ctx, card.Key, holder, ledger.RetentionRecord{
			Kind:       RetentionMeditation,
			Type:       card.Type,
			RecordedAt: card.RecordedAt,
		}); err != nil {
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = MeditationRetain
			continue
		}
		report.Fates[card.Key] = MeditationProtect
	}
	for _, card := range overflow {
		if _, decided := report.Fates[card.Key]; decided {
			continue
		}
		if AssessValue(card, now).Score >= c.policy.MinScore {
			report.Fates[card.Key] = MeditationRetain
			continue
		}
		protected, err := c.retention.Held(ctx, card.Key)
		if err != nil {
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = MeditationRetain
			continue
		}
		if protected {
			report.Fates[card.Key] = MeditationProtect
			continue
		}
		if err := c.lifecycle.Delete(ctx, card.Key, now); err != nil {
			if errors.Is(err, ledger.ErrProtected) {
				report.Fates[card.Key] = MeditationProtect
				continue
			}
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = MeditationRetain
			continue
		}
		report.Fates[card.Key] = MeditationEvict
		report.Evicted = append(report.Evicted, card.Key)
	}
	for _, card := range window {
		if _, decided := report.Fates[card.Key]; decided {
			continue
		}
		protected, err := c.retention.Held(ctx, card.Key)
		if err != nil {
			report.Failures = append(report.Failures, err)
			report.Fates[card.Key] = MeditationRetain
			continue
		}
		if protected {
			report.Fates[card.Key] = MeditationProtect
			continue
		}
		report.Fates[card.Key] = MeditationRetain
	}
	return report, nil
}

// Judge files the request fact the card sequence's bound judgment is: a sequence
// at or within the bound files nothing and answers false, and an over-limit one
// files the request naming the sequence, the meditation producer and the bound
// reason, and answers that request with true. The judgment is filed under the
// sequence's content-addressed identity, so judging one body of material again
// files no second request and keeps the first filing moment. A sequence no fate
// could be filed over is refused by name and files nothing.
func (c *Meditation) Judge(ctx context.Context, now time.Time, cards []MeditationCard) (event.CompressRequest, bool, error) {
	if err := ctx.Err(); err != nil {
		return event.CompressRequest{}, false, err
	}
	if c.requests == nil {
		return event.CompressRequest{}, false, ErrMeditationRequestsUnwired
	}
	seen := map[event.EventID]bool{}
	for _, card := range cards {
		if card.Key == "" || seen[card.Key] {
			return event.CompressRequest{}, false, fmt.Errorf("%w: %q", ErrMeditationCardKey, card.Key)
		}
		seen[card.Key] = true
	}
	if len(cards) <= c.policy.MaxCards {
		return event.CompressRequest{}, false, nil
	}
	request := event.CompressRequestFor(memory.CompressProducerMeditation, string(meditationSequenceKey(cards)), memory.CompressReasonMeditation)
	request.At = now
	filed, err := c.requests.Produce(ctx, request)
	if err != nil {
		return event.CompressRequest{}, false, err
	}
	return filed, true, nil
}

// Tick runs one meditation tick at now: it reads the card sequence it curates and
// the compress requests pending in the request log, and for each pending request
// naming that sequence it runs one pass, commits the condensed artifact the pass
// minted into the state, pre-folds the compressed projection once through the
// wired pre-fold, commits the compaction fact and retires the request. A request
// naming other material stays pending, and a pass, commit, pre-fold or retirement
// that failed keeps its request pending and lands in the tick account, so a
// request is never counted consumed before the products of its compression are
// held.
func (c *Meditation) Tick(ctx context.Context, now time.Time) (MeditationTick, error) {
	var tick MeditationTick
	if err := ctx.Err(); err != nil {
		return tick, err
	}
	if c.requests == nil {
		return tick, ErrMeditationRequestsUnwired
	}
	if c.cards == nil {
		return tick, ErrMeditationCardsUnwired
	}
	cards, err := c.cards(ctx)
	if err != nil {
		return tick, fmt.Errorf("memory: reading the meditation card sequence: %w", err)
	}
	requests, err := c.requests.Pending(ctx)
	if err != nil {
		return tick, fmt.Errorf("memory: reading the compress requests: %w", err)
	}
	subject := string(meditationSequenceKey(cards))
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
				tick.Failures = append(tick.Failures, ErrMeditationProductsUnwired)
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
func (c *Meditation) prefold(ctx context.Context) (replay.CompactionFact, error) {
	if c.projection == nil {
		return replay.CompactionFact{}, ErrMeditationProjectionUnwired
	}
	if c.folds == nil {
		return replay.CompactionFact{}, ErrMeditationFoldsUnwired
	}
	projection, err := c.projection(ctx)
	if err != nil {
		return replay.CompactionFact{}, fmt.Errorf("memory: reading the projection to pre-fold: %w", err)
	}
	payload, err := Prefold(ctx, projection)
	if err != nil {
		return replay.CompactionFact{}, err
	}
	if err := c.folds(ctx, payload); err != nil {
		return replay.CompactionFact{}, err
	}
	return payload, nil
}

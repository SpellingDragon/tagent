// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// The meditation strategy face: the deterministic value assessment of one card,
// the policy bound a pass is judged against and the condenser seat an LLM would
// take beside the engineering condensation. The assessment is a pure function of
// the card and the pass time, so two identical passes reach identical fates.
package meditation

import (
	"context"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// MeditationValue is the deterministic value assessment of one card: the score
// the pass decides keep and evict against, and nothing else. The assessment is
// a pure function of the card and the pass time, so two identical passes
// always reach identical verdicts and no LLM judgment enters the decision.
type MeditationValue struct {
	// Key is the card the assessment describes.
	Key event.EventID
	// Score is the assessed value of the card.
	Score int
}

// MeditationLinkWeight, MeditationSkeletonWeight and MeditationRecencyWeight are the
// value assessment weights: a standing causal link is worth more than the
// boundary skeleton weight, which is worth more than the recency bonus.
const (
	// MeditationLinkWeight is the value one standing causal link adds.
	MeditationLinkWeight = 10
	// MeditationSkeletonWeight is the value a boundary fact (external input or
	// agent output) adds: the task skeleton the condensation keeps.
	MeditationSkeletonWeight = 5
	// MeditationRecencyWeight is the value a fact inside the recent window adds.
	MeditationRecencyWeight = 2
)

// meditationRecentWindow is the age window the recency bonus is measured over.
const meditationRecentWindow = 24 * time.Hour

// meditationSummaryLimit bounds the condensed summary one pass writes, so a
// condensation of a long overflow run stays one bounded line.
const meditationSummaryLimit = 240

// AssessValue returns the deterministic value assessment of one card: a pure
// function of its causal link count, its frozen type and its age against the
// pass time. The same inputs always return the same score, so the pass that
// runs twice reaches the same fates.
func AssessValue(card MeditationCard, now time.Time) MeditationValue {
	score := card.Links * MeditationLinkWeight
	if card.Type == event.TypeExternalInput || card.Type == event.TypeAgentOutput {
		score += MeditationSkeletonWeight
	}
	if age := now.Sub(card.RecordedAt); age >= 0 && age <= meditationRecentWindow {
		score += MeditationRecencyWeight
	}
	return MeditationValue{Key: card.Key, Score: score}
}

// MeditationPolicy shapes one meditation pass: the sequence bound that triggers
// it, how many key cards survive as artifact references, and the value floor
// below which an unprotected overflow card is evicted.
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

// MeditationRun is the over-limit card run one condensation folds: the cards
// that overflowed the sequence bound, oldest first, and the key cards the pass
// keeps as artifact references beside them.
type MeditationRun struct {
	// Overflow is the over-limit card run, oldest first.
	Overflow []MeditationCard
	// Keep is the key card set the artifact references, highest value first.
	Keep []MeditationCard
}

// Condenser folds one over-limit card run into its condensed summary: the
// seat an LLM meditation would take beside the deterministic engineering
// condensation. A condenser that fails degrades the pass to that engineering
// fallback — the overflow run sinks into an "(earlier n items)" count line —
// and never collapses the pass or rewrites a fact.
type Condenser func(ctx context.Context, run MeditationRun, now time.Time) (string, error)

// condenseCards is the deterministic engineering condensation: the overflow
// run folded into one bounded line, oldest first. It is the production
// condenser beside no model and the shape the degraded fallback returns to,
// and it never touches a fact.
func condenseCards(ctx context.Context, run MeditationRun, _ time.Time) (string, error) {
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
	if len(joined) > meditationSummaryLimit {
		joined = joined[:meditationSummaryLimit]
	}
	return joined, nil
}

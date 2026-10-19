// 契约: docs/FACT-VOCABULARY.md#families
//
// The pre-fold face of the meditation consumer: the fold a consumer runs when it
// consumes a compress request, the compaction fact it files into the state, and
// the fold identity that fact is quoted by. A pre-fold runs ahead of the request
// boundary, so the assembly path reads the folded projection and never folds it
// inline.
package meditation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replay"
)

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

// Prefold folds the projection a consumer owns ahead of assembly and answers the
// compaction payload the fold left, so the fold fact is filed when its request
// is consumed and the request boundary only reads the folded projection. A
// projection already carrying a compaction payload is answered with it: one
// body of material folds once.
func Prefold(ctx context.Context, projection *replay.ReplayProjection) (replay.CompactionFact, error) {
	if projection == nil {
		return replay.CompactionFact{}, ErrPrefoldUnwired
	}
	if payload := projection.Compaction(); payload != nil {
		return *payload, nil
	}
	if _, err := projection.Fold(ctx); err != nil {
		return replay.CompactionFact{}, err
	}
	payload := projection.Compaction()
	if payload == nil {
		return replay.CompactionFact{}, ErrPrefoldUnwired
	}
	return *payload, nil
}

// CompactionFactKey derives the identity the fold fact of one compaction payload
// is filed under: content-addressed over the summary it reports, the boundary it
// covers and the refs it retains in payload order, so one fold files one fact
// and a re-file keeps that identity.
func CompactionFactKey(payload replay.CompactionFact) event.EventID {
	parts := []string{
		string(payload.Summary.Key),
		payload.Summary.Type,
		payload.Summary.Summary,
		payload.Summary.Content,
		string(payload.FullBoundary),
	}
	for _, key := range payload.ConsumedKeys {
		parts = append(parts, string(key))
	}
	for _, ref := range payload.Refs {
		parts = append(parts, string(ref.Key), ref.Type, ref.Summary, ref.Content)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return event.EventID("fold-" + hex.EncodeToString(sum[:8]))
}

// CompactionChainFact returns the state fact one compaction payload is committed
// as: the compaction event a replay reads back, filed under the fold identity
// the payload answers. A payload that cannot be written back as a chain fact is
// reported by name.
func CompactionChainFact(chain replay.ChainKey, payload replay.CompactionFact, at time.Time) (replay.ChainFact, error) {
	content, err := json.Marshal(payload)
	if err != nil {
		return replay.ChainFact{}, err
	}
	return replay.ChainFact{
		Key:     CompactionFactKey(payload),
		Chain:   chain,
		Type:    replay.CompactionEventType,
		Summary: "compaction",
		Content: string(content),
		Time:    at,
	}, nil
}

// MeditationProjection reads the replay projection one consumer tick pre-folds:
// the material its compress requests name. A read that fails is reported by name
// and a tick never folds a partial projection.
type MeditationProjection func(ctx context.Context) (*replay.ReplayProjection, error)

// MeditationFolds commits the compaction fact one pre-fold left into the state:
// the fold fact its request is answered with. A commit that fails is reported by
// name, so a fold is never counted as held before the state carries it.
type MeditationFolds func(ctx context.Context, fact replay.CompactionFact) error

// WithMeditationPreFold wires the consumer-side fold: the projection a tick folds
// when it consumes a compress request and the state its compaction fact is
// committed into. The request stays pending until the fold fact is held, so the
// assembly path never folds inline.
func WithMeditationPreFold(projection MeditationProjection, folds MeditationFolds) MeditationOption {
	return func(meditation *Meditation) {
		meditation.projection = projection
		meditation.folds = folds
	}
}

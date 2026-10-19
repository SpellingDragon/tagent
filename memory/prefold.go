// 契约: docs/FACT-VOCABULARY.md#families
//
// The pre-fold face of the compression consumers: the fold a consumer runs when
// it consumes a compress request, the compaction fact it files into the state,
// and the fold identity that fact is quoted by. A pre-fold runs ahead of the
// request boundary, so the assembly path reads the folded projection and never
// folds it.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

var (
	// ErrFoldEmpty reports a fold of an empty projection: there is no run to
	// collapse and no compaction payload to leave on the fact chain.
	ErrFoldEmpty = errors.New("memory: fold needs at least one projection entry")
	// ErrPrefoldUnwired reports a pre-fold asked without the projection it
	// folds: no material is named and no compaction fact could be filed.
	ErrPrefoldUnwired = errors.New("memory: pre-fold needs the projection of the material it folds")
	// ErrCurationProjectionUnwired reports a consumer tick pre-folding without
	// the projection source: the tick names no material it could fold.
	ErrCurationProjectionUnwired = errors.New("memory: curation consumer pre-fold needs the projection source")
	// ErrCurationFoldsUnwired reports a pre-fold whose compaction fact has
	// nowhere to be committed: the fold would be counted while no state carries
	// it.
	ErrCurationFoldsUnwired = errors.New("memory: curation consumer pre-fold needs the fold sink")
)

// Fold collapses the foldable run of the projection into one compaction entry
// and returns its summary entry, so the collapsed text is reported as a fact of
// the chain rather than kept beside it.
//
// The retained refs keep the payload order: a keyless tool-chain entry becomes a
// synthetic ref carrying its full text in the payload, a keyed entry of a
// retained type becomes a positive ref the fact chain hydrates back, and the
// remaining entries are represented by the summary alone. The compaction payload
// records the fullBoundary value the tail is replayed after, and the summary
// entry is returned first on every restore.
func (p *ReplayProjection) Fold(ctx context.Context) (ProjectionEntry, error) {
	if err := ctx.Err(); err != nil {
		return ProjectionEntry{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return ProjectionEntry{}, ErrFoldEmpty
	}
	p.foldSeq++
	seq := strconv.Itoa(p.foldSeq)
	minTs := p.entries[0].Time
	summaries := make([]string, 0, len(p.entries))
	refEntries := make([]ProjectionEntry, 0, len(p.entries))
	refs := make([]CompactionRef, 0, len(p.entries))
	boundary := event.EventID("")
	for i, entry := range p.entries {
		if entry.Time.Before(minTs) {
			minTs = entry.Time
		}
		summaries = append(summaries, entry.Summary)
		if entry.Key == "" || SyntheticKey(entry.Key) {
			ref := CompactionRef{
				Key:     event.EventID("-ref-" + seq + "-" + strconv.Itoa(i)),
				Type:    entry.Type,
				Summary: entry.Summary,
				Content: entry.Content,
				Time:    entry.Time,
			}
			refs = append(refs, ref)
			refEntries = append(refEntries, entryOfRef(ref))
			continue
		}
		if RetainRefType(entry.Type) {
			refs = append(refs, CompactionRef{
				Key:     entry.Key,
				Type:    entry.Type,
				Summary: entry.Summary,
				Time:    entry.Time,
			})
			refEntries = append(refEntries, ProjectionEntry{
				Key:     entry.Key,
				Type:    entry.Type,
				Summary: entry.Summary,
				Content: p.originals[entry.Key],
				Time:    entry.Time,
			})
		}
		if string(entry.Key) > string(boundary) {
			boundary = entry.Key
		}
	}
	summaryRef := CompactionRef{
		Key:     event.EventID("-" + strconv.FormatInt(minTs.UnixNano(), 10)),
		Type:    CompactionEventType,
		Summary: "compaction",
		Content: "compaction: " + strings.Join(summaries, "; "),
		Time:    minTs,
	}
	payload := CompactionFact{
		Summary:        summaryRef,
		Refs:           refs,
		FullBoundary:   boundary,
		MeditationKeys: p.meditationKeysLocked(),
	}
	p.fold = &payload
	p.boundary = boundary
	summaryEntry := entryOfRef(summaryRef)
	p.entries = append([]ProjectionEntry{summaryEntry}, refEntries...)
	return summaryEntry, nil
}

// RetainRefType reports whether an entry of the frozen event type is kept as a
// retained ref by a fold instead of being represented by the summary alone: the
// tool and task facts a fold must not lose, while the conversational entries are
// what the summary speaks for.
func RetainRefType(typ string) bool {
	return typ == event.TypeActionCommand || typ == event.TypeTaskSettled
}

// Prefold folds the projection a consumer owns ahead of assembly and answers the
// compaction payload the fold left, so the fold fact is filed when its request
// is consumed and the request boundary only reads the folded projection. A
// projection already carrying a compaction payload is answered with it: one
// body of material folds once.
func Prefold(ctx context.Context, projection *ReplayProjection) (CompactionFact, error) {
	if projection == nil {
		return CompactionFact{}, ErrPrefoldUnwired
	}
	if payload := projection.Compaction(); payload != nil {
		return *payload, nil
	}
	if _, err := projection.Fold(ctx); err != nil {
		return CompactionFact{}, err
	}
	payload := projection.Compaction()
	if payload == nil {
		return CompactionFact{}, ErrPrefoldUnwired
	}
	return *payload, nil
}

// CompactionFactKey derives the identity the fold fact of one compaction payload
// is filed under: content-addressed over the summary it reports, the boundary it
// covers and the refs it retains in payload order, so one fold files one fact
// and a re-file keeps that identity.
func CompactionFactKey(payload CompactionFact) event.EventID {
	parts := []string{
		string(payload.Summary.Key),
		payload.Summary.Type,
		payload.Summary.Summary,
		payload.Summary.Content,
		string(payload.FullBoundary),
	}
	for _, key := range payload.MeditationKeys {
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
func CompactionChainFact(chain ChainKey, payload CompactionFact, at time.Time) (ChainFact, error) {
	content, err := json.Marshal(payload)
	if err != nil {
		return ChainFact{}, err
	}
	return ChainFact{
		Key:     CompactionFactKey(payload),
		Chain:   chain,
		Type:    CompactionEventType,
		Summary: "compaction",
		Content: string(content),
		Time:    at,
	}, nil
}

// CurationProjection reads the replay projection one consumer tick pre-folds:
// the material its compress requests name. A read that fails is reported by name
// and a tick never folds a partial projection.
type CurationProjection func(ctx context.Context) (*ReplayProjection, error)

// CurationFolds commits the compaction fact one pre-fold left into the state: the
// fold fact its request is answered with. A commit that fails is reported by
// name, so a fold is never counted as held before the state carries it.
type CurationFolds func(ctx context.Context, fact CompactionFact) error

// WithCurationPreFold wires the consumer-side fold: the projection a tick folds
// when it consumes a compress request and the state its compaction fact is
// committed into. The request stays pending until the fold fact is held, so the
// assembly path never folds inline.
func WithCurationPreFold(projection CurationProjection, folds CurationFolds) CurationOption {
	return func(curation *Curation) {
		curation.projection = projection
		curation.folds = folds
	}
}

// 契约: docs/FACT-VOCABULARY.md#families
//
// The fold of one projection: the collapse of its foldable run into one
// compaction entry, stated as a fact of the chain rather than kept beside it.
// The retained refs keep the payload order, the payload records the boundary the
// tail is replayed after, and the summary entry is returned first on every
// restore.
package replay

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
)

// ErrFoldEmpty reports a fold of an empty projection: there is no run to
// collapse and no compaction payload to leave on the fact chain.
var ErrFoldEmpty = errors.New("memory: fold needs at least one projection entry")

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
		Summary:      summaryRef,
		Refs:         refs,
		FullBoundary: boundary,
		ConsumedKeys: p.consumedKeysLocked(),
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

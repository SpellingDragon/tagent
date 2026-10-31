// 契约: docs/FACT-VOCABULARY.md#families
//
// The fold of one projection: the collapse of its foldable run into one
// compaction entry, stated as a fact of the chain rather than kept beside it.
// The retained refs keep the payload order, the payload records the boundary the
// tail is replayed after, and the summary entry is returned first on every
// restore.
//
// A fold is idempotent: every product is filed under a content-addressed
// negative key — the summary under the digest of the folded run, each synthetic
// ref under the digest of the entry it carries — so the same material always
// folds under the same keys and a second fold mints no second product. A
// projection that already carries a compaction payload answers it as it stands:
// one body of material folds once, and a projection rebuilt from the chain is
// folded no further.
package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"strconv"
	"strings"
	"time"

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
//
// - The entries the summary alone represents leave the projection with their per-key material (the original text, the dedup key and the consumed mark) while a retained ref keeps its own, so the rewritten sequence carries no copy of the material the summary speaks for.
//
// - A projection already carrying a compaction payload answers that payload's summary entry and moves nothing, so one body of material folds once even when a rebuilt projection is offered to the fold again.
func (p *ReplayProjection) Fold(ctx context.Context) (ProjectionEntry, error) {
	if err := ctx.Err(); err != nil {
		return ProjectionEntry{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return ProjectionEntry{}, ErrFoldEmpty
	}
	if p.fold != nil {
		return entryOfRef(p.fold.Summary), nil
	}
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
				Key:     foldRefKey(i, entry),
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
		Key:     foldSummaryKey(p.entries),
		Type:    CompactionEventType,
		Summary: "compaction",
		Content: "compaction: " + strings.Join(summaries, "; "),
		Time:    minTs,
	}
	consumed := p.consumedKeysLocked()
	if len(consumed) == 0 {
		consumed = nil
	}
	payload := CompactionFact{
		Summary:      summaryRef,
		Refs:         refs,
		FullBoundary: boundary,
		ConsumedKeys: consumed,
	}
	p.fold = &payload
	p.boundary = boundary
	summaryEntry := entryOfRef(summaryRef)
	p.entries = append([]ProjectionEntry{summaryEntry}, refEntries...)
	p.forgetDroppedLocked()
	return summaryEntry, nil
}

// RetainRefType reports whether an entry of the frozen event type is kept as a
// retained ref by a fold instead of being represented by the summary alone: the
// tool and task facts a fold must not lose, while the conversational entries are
// what the summary speaks for.
func RetainRefType(typ string) bool {
	return typ == event.TypeActionCommand || typ == event.TypeTaskSettled
}

// foldSummaryKey reports the content-addressed negative key one fold files its
// summary under: the digest of the folded run in payload order, so the same
// material always folds under the same key.
func foldSummaryKey(entries []ProjectionEntry) event.EventID {
	digest := sha256.New()
	for _, entry := range entries {
		foldDigestEntry(digest, entry)
	}
	return event.EventID("-fold-" + hex.EncodeToString(digest.Sum(nil)[:8]))
}

// foldRefKey reports the content-addressed negative key one synthetic ref is
// filed under: the entry's position beside its content, because two identical
// keyless entries must keep two refs while the same material keeps one key.
func foldRefKey(index int, entry ProjectionEntry) event.EventID {
	digest := sha256.New()
	digest.Write([]byte(strconv.Itoa(index)))
	digest.Write([]byte{0})
	foldDigestEntry(digest, entry)
	return event.EventID("-ref-" + hex.EncodeToString(digest.Sum(nil)[:8]))
}

// foldDigestEntry writes one entry into a product digest: identity, frozen type,
// summary, full text and recorded time, so two entries of the same text but a
// different identity never share a product key.
func foldDigestEntry(digest hash.Hash, entry ProjectionEntry) {
	for _, part := range []string{string(entry.Key), entry.Type, entry.Summary, entry.Content, entry.Time.UTC().Format(time.RFC3339Nano)} {
		digest.Write([]byte(part))
		digest.Write([]byte{0})
	}
}

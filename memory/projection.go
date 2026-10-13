package memory

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// ProjectionEntry is one ordered item of a projection: the canonical identity of
// a committed fact, its type and summary, the content presented for it, and its
// time, in the same shape a recall card entry uses.
type ProjectionEntry struct {
	// Key is the canonical identity of the fact, in its hex form.
	Key event.EventID
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact, which is not the full text.
	Summary string
	// Content is what the model view carries for this entry: the full original
	// when the recorded decision is full text, and otherwise the bounded preview
	// or ticket that decision recorded.
	Content string
	// Time is the time the fact was recorded.
	Time time.Time
}

// PresentationMode names how one projection entry is presented to the model.
type PresentationMode int

const (
	// PresentationUnrecorded is the zero value: no decision was recorded for the
	// entry, so it is never presented as if one had been.
	PresentationUnrecorded PresentationMode = iota
	// PresentationFull presents the full original text of the entry.
	PresentationFull
	// PresentationSummary presents the stored summary in place of the original.
	PresentationSummary
	// PresentationTicket presents a bounded preview together with the ticket
	// that reaches the original by segment.
	PresentationTicket
)

// PresentationDecision is the recorded choice of how one projection entry was
// presented to the model, and the basis a replay reproduces.
//
// The recorded decision is what a replay follows, so a recorded preview is never
// promoted to the full original and the original is never reconstructed from the
// decision.
type PresentationDecision struct {
	// Key identifies the entry the decision was recorded for.
	Key event.EventID
	// Mode is the presentation chosen for the entry.
	Mode PresentationMode
	// Preview is the bounded text shown when Mode is not PresentationFull, and
	// is empty for a full-text decision.
	Preview string
}

// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// Projection is the ordered, bounded view of the fact chain that an agent
// request is assembled from: every committed fact is appended once, presentation
// decisions are recorded as they are made, and the view is replayed from those
// records rather than from a second writable source.
//
// A projection holds no state outside the fact chain, so replaying the recorded
// decisions reproduces the same view. Constraints:
//
//   - a keyed entry already present is appended once, and a keyless entry always is;
//   - the original text, the model view and the presentation decision stay layered;
//   - a rebuild recomputes the deduplication key set from the entries it keeps.
type Projection interface {
	// Append adds the canonical entry of one committed fact in event-key order.
	// It is the deduplication point: an entry whose key is already present is
	// skipped, while an entry without a key is always appended.
	Append(ctx context.Context, entry ProjectionEntry)

	// Fold collapses the foldable run of the projection into one compaction
	// entry and returns it, so the collapsed text is reported as a fact of the
	// chain rather than kept beside it.
	Fold(ctx context.Context) (ProjectionEntry, error)

	// Rebuild replaces the whole projection with the one the recorded decisions
	// describe, recomputing the deduplication key set from scratch.
	Rebuild(ctx context.Context, decisions []PresentationDecision) ([]ProjectionEntry, error)

	// AssembleBudget packs the projection into one agent request of the given
	// token budget and returns the entries the request is assembled from,
	// including the recovery hint entry when one applies.
	AssembleBudget(ctx context.Context, budget int) ([]ProjectionEntry, error)
}

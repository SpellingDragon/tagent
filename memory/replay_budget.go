// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// Presentation decisions and budget assembly.
//
// The projection layers the original text, the model view and the presentation
// decision: the original is kept apart from what the model sees, and only a
// full-text decision makes the two identical. A recorded decision is the basis a
// replay follows, so a recorded preview is never promoted back to the full text
// and the original is never reconstructed from a decision.
package memory

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

const (
	// ProjectionTypeRecoveryHint is the entry type of the recovery hint an
	// assembly carries first after a cold-start replay, so the model is told the
	// context it sees was rebuilt rather than silently presented.
	ProjectionTypeRecoveryHint = "recovery_hint"

	// budgetCharsPerToken is the conservative character-to-token ratio the
	// budget assembly packs a request under.
	budgetCharsPerToken = 4
	// ticketPreviewBytes bounds the preview a ticket decision records and shows.
	ticketPreviewBytes = 128
	// recoveryHintBytes bounds the recovery hint entry text.
	recoveryHintBytes = 200
)

var (
	// ErrFoldEmpty reports a fold of an empty projection: there is no run to
	// collapse and no compaction payload to leave on the fact chain.
	ErrFoldEmpty = errors.New("memory: fold needs at least one projection entry")
	// ErrBudgetInvalid reports a budget assembly asked to pack a request under a
	// non-positive token budget.
	ErrBudgetInvalid = errors.New("memory: budget assembly needs a positive token budget")
)

// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// ReplayProjection is the replayable projection: the ordered model view of one
// fact chain, the recorded presentation decisions that view was assembled
// under, and the compaction payload a fold left on the chain. It is what
// RebuildProjectionFromWAL replays into.
//
// A projection holds no state outside the fact chain, so replaying the recorded
// decisions reproduces the same view. The entries hold the canonical form (the
// original text of each keyed fact), while a recorded decision shapes what an
// assembly presents, and the layering is never collapsed. Constraints:
//
//   - a keyed entry already present is appended once, and a keyless entry always is;
//   - the original text, the model view and the presentation decision stay layered;
//   - a rebuild recomputes the deduplication key set from the entries it keeps.
type ReplayProjection struct {
	mu         sync.Mutex
	entries    []ProjectionEntry
	seen       map[event.EventID]struct{}
	originals  map[event.EventID]string
	decisions  map[event.EventID]PresentationDecision
	meditation map[event.EventID]struct{}
	foldSeq    int
	fold       *CompactionFact
	boundary   event.EventID
	recovery   *ProjectionEntry
}

// NewReplayProjection returns an empty replayable projection.
func NewReplayProjection() *ReplayProjection {
	return &ReplayProjection{
		seen:       map[event.EventID]struct{}{},
		originals:  map[event.EventID]string{},
		decisions:  map[event.EventID]PresentationDecision{},
		meditation: map[event.EventID]struct{}{},
	}
}

// Append adds the canonical entry of one committed fact: a keyed entry already
// present is appended once, a keyless entry always is. The original text is
// kept apart from the model view, and a recorded decision shapes the view —
// never the original.
func (p *ReplayProjection) Append(ctx context.Context, entry ProjectionEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry.Key != "" {
		if _, dup := p.seen[entry.Key]; dup {
			return
		}
		p.seen[entry.Key] = struct{}{}
		p.originals[entry.Key] = entry.Content
		if decision, recorded := p.decisions[entry.Key]; recorded {
			entry.Content = p.viewOf(decision, entry)
		}
		if entry.Type == event.TypeThinkingPlan {
			p.meditation[entry.Key] = struct{}{}
		}
	}
	p.entries = append(p.entries, entry)
}

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

// Rebuild replaces the whole projection with the one the recorded decisions
// describe and recomputes the deduplication key set from the entries it keeps.
// Decisions passed in are recorded first-time-wins: a decision already recorded
// for a key is never upgraded by a later one, so a recorded preview is never
// promoted back to the full text.
func (p *ReplayProjection) Rebuild(ctx context.Context, decisions []PresentationDecision) ([]ProjectionEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, decision := range decisions {
		if decision.Key == "" {
			continue
		}
		if _, recorded := p.decisions[decision.Key]; !recorded {
			p.decisions[decision.Key] = decision
		}
	}
	p.seen = map[event.EventID]struct{}{}
	out := make([]ProjectionEntry, len(p.entries))
	for i, entry := range p.entries {
		if entry.Key != "" {
			p.seen[entry.Key] = struct{}{}
			if decision, recorded := p.decisions[entry.Key]; recorded {
				entry.Content = p.viewOf(decision, entry)
			} else {
				entry.Content = p.originals[entry.Key]
			}
		}
		out[i] = entry
	}
	p.entries = out
	result := make([]ProjectionEntry, len(p.entries))
	copy(result, p.entries)
	return result, nil
}

// AssembleBudget packs the projection into one agent request of the given token
// budget and returns the entries the request is assembled from, recovery hint
// first when one applies. A recorded decision is followed unconditionally — the
// view a replay reproduces never drifts with the remaining budget — while an
// unrecorded entry degrades from the full text to a ticket and then to the
// summary until the budget is met, recording the decision it settled on. An
// entry that fits none of them is skipped without a record, so a later assembly
// under a larger budget can still present it in full.
func (p *ReplayProjection) AssembleBudget(ctx context.Context, budget int) ([]ProjectionEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if budget < 1 {
		return nil, ErrBudgetInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	limit := budget * budgetCharsPerToken
	used := 0
	out := make([]ProjectionEntry, 0, len(p.entries)+1)
	if p.recovery != nil {
		hint := *p.recovery
		out = append(out, hint)
		used += len(hint.Content)
	}
	for _, entry := range p.entries {
		if entry.Key != "" {
			if decision, recorded := p.decisions[entry.Key]; recorded {
				view := p.viewOf(decision, entry)
				shown := entry
				shown.Content = view
				out = append(out, shown)
				used += len(view)
				continue
			}
		}
		original := p.originalOf(entry)
		if used+len(original) <= limit {
			if entry.Key != "" {
				p.recordLocked(entry.Key, PresentationDecision{Key: entry.Key, Mode: PresentationFull})
			}
			shown := entry
			shown.Content = original
			out = append(out, shown)
			used += len(original)
			continue
		}
		preview := boundedText(original, ticketPreviewBytes)
		ticket := preview + "\n" + ticketText(entry.Key)
		if used+len(ticket) <= limit {
			if entry.Key != "" {
				p.recordLocked(entry.Key, PresentationDecision{Key: entry.Key, Mode: PresentationTicket, Preview: preview})
			}
			shown := entry
			shown.Content = ticket
			out = append(out, shown)
			used += len(ticket)
			continue
		}
		if used+len(entry.Summary) <= limit {
			if entry.Key != "" {
				p.recordLocked(entry.Key, PresentationDecision{Key: entry.Key, Mode: PresentationSummary, Preview: entry.Summary})
			}
			shown := entry
			shown.Content = entry.Summary
			out = append(out, shown)
			used += len(entry.Summary)
			continue
		}
	}
	return out, nil
}

// RecordDecision records the presentation decision of one entry and reshapes
// its model view accordingly. First time wins: a decision already recorded is
// never upgraded, so a recorded preview is never promoted back to the full
// text.
func (p *ReplayProjection) RecordDecision(key event.EventID, decision PresentationDecision) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recordLocked(key, decision)
}

func (p *ReplayProjection) recordLocked(key event.EventID, decision PresentationDecision) {
	if key == "" {
		return
	}
	if _, recorded := p.decisions[key]; recorded {
		return
	}
	p.decisions[key] = decision
	for i, entry := range p.entries {
		if entry.Key == key {
			p.entries[i].Content = p.viewOf(decision, entry)
		}
	}
}

// viewOf renders the model view one decision describes: the full original only
// for a full-text decision, the recorded text otherwise. The original is never
// reconstructed from the decision — it is only ever read back from the layer
// that kept it.
func (p *ReplayProjection) viewOf(decision PresentationDecision, entry ProjectionEntry) string {
	switch decision.Mode {
	case PresentationFull:
		return p.originalOf(entry)
	case PresentationSummary:
		return decision.Preview
	case PresentationTicket:
		return decision.Preview + "\n" + ticketText(decision.Key)
	default:
		return p.originalOf(entry)
	}
}

// originalOf returns the original text of an entry: the kept original of a keyed
// entry, and the entry's own text for a keyless one.
func (p *ReplayProjection) originalOf(entry ProjectionEntry) string {
	if entry.Key != "" {
		if original, kept := p.originals[entry.Key]; kept {
			return original
		}
	}
	return entry.Content
}

// ticketText renders the ticket that reaches the full original by segment, so a
// presented ticket stays usable without re-carrying the full text.
func ticketText(key event.EventID) string {
	if key == "" {
		return "[ticket: full text retrievable by segment]"
	}
	return "[ticket " + string(key) + ": full text retrievable by segment]"
}

// boundedText bounds text to n bytes on a rune boundary, marking the cut.
func boundedText(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}

// seedMeditation marks one meditation key as reseeded, so the meditation state a
// snapshot and its tail carried survives a rebuild.
func (p *ReplayProjection) seedMeditation(key event.EventID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if key != "" {
		p.meditation[key] = struct{}{}
	}
}

// noteRecovery records the recovery hint one cold-start replay leaves behind,
// so the first assembly after a rebuild tells the model the context it sees was
// rebuilt and under which bound.
func (p *ReplayProjection) noteRecovery(entries int, fallback bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	text := "recovery: replayed " + strconv.Itoa(entries) + " entries from the fact chain"
	if fallback {
		text += " under the bounded fallback"
	}
	hint := ProjectionEntry{
		Key:     event.EventID("-recovery"),
		Type:    ProjectionTypeRecoveryHint,
		Summary: "recovery hint",
		Content: boundedText(text, recoveryHintBytes),
		Time:    time.Now(),
	}
	p.recovery = &hint
}

// Len returns how many entries the projection holds.
func (p *ReplayProjection) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Entries returns a copy of the projection entries in their canonical form.
func (p *ReplayProjection) Entries() []ProjectionEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ProjectionEntry, len(p.entries))
	copy(out, p.entries)
	return out
}

// Original returns the kept original text of one keyed entry.
func (p *ReplayProjection) Original(key event.EventID) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	original, kept := p.originals[key]
	return original, kept
}

// Decision returns the recorded presentation decision of one key.
func (p *ReplayProjection) Decision(key event.EventID) (PresentationDecision, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	decision, recorded := p.decisions[key]
	return decision, recorded
}

// Compaction returns the compaction payload the last fold left, nil before any.
func (p *ReplayProjection) Compaction() *CompactionFact {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fold
}

// FullBoundary returns the event-key boundary the compaction payload carried.
func (p *ReplayProjection) FullBoundary() event.EventID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.boundary
}

// MeditationKeys returns the reseeded meditation keys in order.
func (p *ReplayProjection) MeditationKeys() []event.EventID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.meditationKeysLocked()
}

func (p *ReplayProjection) meditationKeysLocked() []event.EventID {
	keys := make([]string, 0, len(p.meditation))
	for key := range p.meditation {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	out := make([]event.EventID, 0, len(keys))
	for _, key := range keys {
		out = append(out, event.EventID(key))
	}
	return out
}

// RecoveryHint returns the recovery hint entry a rebuild left, nil before one.
func (p *ReplayProjection) RecoveryHint() *ProjectionEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.recovery
}

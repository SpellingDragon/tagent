// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// Presentation decisions and budget assembly.
//
// The projection layers the original text, the model view and the presentation
// decision: the original is kept apart from what the model sees, and only a
// full-text decision makes the two identical. A recorded decision is the basis a
// replay follows, so a recorded preview is never promoted back to the full text
// and the original is never reconstructed from a decision.
package replay

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
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
	mu        sync.Mutex
	entries   []ProjectionEntry
	seen      map[event.EventID]struct{}
	originals map[event.EventID]string
	decisions map[event.EventID]PresentationDecision
	consumed  map[event.EventID]struct{}
	fold      *CompactionFact
	settles   []ReplaySettle
	boundary  event.EventID
	recovery  *ProjectionEntry
}

// NewReplayProjection returns an empty replayable projection.
func NewReplayProjection() *ReplayProjection {
	return &ReplayProjection{
		seen:      map[event.EventID]struct{}{},
		originals: map[event.EventID]string{},
		decisions: map[event.EventID]PresentationDecision{},
		consumed:  map[event.EventID]struct{}{},
	}
}

// Append adds the canonical entry of one committed fact: a keyed entry already
// present is appended once, a keyless entry always is. The original text is
// kept apart from the model view, and a recorded decision shapes the view —
// never the original. An append is an in-memory mutation, so it takes the port's
// context with nothing to cancel.
func (p *ReplayProjection) Append(_ context.Context, entry ProjectionEntry) {
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
			p.consumed[entry.Key] = struct{}{}
		}
	}
	p.entries = append(p.entries, entry)
}

// AssembleBudget packs the projection into one agent request of the given token
// budget and returns the entries the request is assembled from, recovery hint
// first when one applies. A recorded decision is followed unconditionally — the
// view a replay reproduces never drifts with the remaining budget — while an
// unrecorded entry degrades from the full text to a ticket and then to the
// summary until the budget is met, settling on the decision it recorded as one
// replay-settle fact. An entry that fits none of them is skipped without a
// record and without a settle fact, so a later assembly under a larger budget
// can still present it in full.
//
// The source facts are read and never written: the assembly files settle facts
// on the projection's own settle account, so the fact chain it replayed is
// exactly as it stood.
func (p *ReplayProjection) AssembleBudget(ctx context.Context, budget int) ([]ProjectionEntry, error) {
	return p.AssembleBudgetPrefix(ctx, budget, -1)
}

// AssembleBudgetPrefix assembles the leading prefix of the projection under the
// same budget rules, so a consumer presents the material that has ended while
// the material still in flight stays with the transcript that carries it: a
// negative prefix assembles the whole projection, and a prefix past its length
// assembles what it holds.
func (p *ReplayProjection) AssembleBudgetPrefix(ctx context.Context, budget, prefix int) ([]ProjectionEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if budget < 1 {
		return nil, ErrBudgetInvalid
	}
	return p.assembleBudgetLocked(budget, prefix), nil
}

// assembleBudgetLocked runs the assembly over the leading prefix of the entries
// under the recorded decisions and the remaining budget.
func (p *ReplayProjection) assembleBudgetLocked(budget, prefix int) []ProjectionEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	entries := p.entries
	if prefix >= 0 && prefix < len(entries) {
		entries = entries[:prefix]
	}
	limit := budget * budgetCharsPerToken
	used := 0
	out := make([]ProjectionEntry, 0, len(entries)+1)
	if p.recovery != nil {
		hint := *p.recovery
		out = append(out, hint)
		used += len(hint.Content)
	}
	for _, entry := range entries {
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
	return out
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
	settle := ReplaySettleFor(decision)
	settle.At = time.Now().UTC()
	p.settles = append(p.settles, settle)
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

// seedConsumed marks one consumed key as reseeded, so the consumed state a
// snapshot and its tail carried survives a rebuild.
func (p *ReplayProjection) seedConsumed(key event.EventID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if key != "" {
		p.consumed[key] = struct{}{}
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

// Settles returns a copy of the replay-settle facts the assembly recorded, in
// settlement order: one fact per decision the assembly settled on, and none for
// an entry it skipped.
func (p *ReplayProjection) Settles() []ReplaySettle {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ReplaySettle, len(p.settles))
	copy(out, p.settles)
	return out
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

// ConsumedKeys returns the reseeded consumed keys in order.
func (p *ReplayProjection) ConsumedKeys() []event.EventID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.consumedKeysLocked()
}

func (p *ReplayProjection) consumedKeysLocked() []event.EventID {
	keys := make([]string, 0, len(p.consumed))
	for key := range p.consumed {
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

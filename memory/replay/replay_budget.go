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
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	// BudgetCharsPerToken is that ratio, read by a consumer that measures a
	// projection's rendered volume against a model window: the fold and the
	// assembly convert at the one ratio, so they never disagree about how much
	// window the material costs.
	BudgetCharsPerToken = budgetCharsPerToken
	// ticketPreviewBytes bounds the preview a ticket decision records and shows.
	ticketPreviewBytes = 128
	// noticeCardChars bounds the identity card a request presents for a notice
	// the budget cannot carry in any fuller form: the notice's status, the head
	// of its material and the ticket that reaches the full original.
	noticeCardChars = 300
	// noticeStatusChars bounds the status a notice card names, so a long stored
	// summary never eats the room the identity and the ticket need.
	noticeStatusChars = 60
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
	settled   map[event.EventID]string
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
		settled:   map[event.EventID]string{},
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
			if _, bound := p.settled[entry.Key]; !bound {
				p.settled[entry.Key] = entry.Content
			}
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
// first when one applies. A recorded decision is followed while the fact it
// settled over is unchanged and its view fits the budget — the view a replay
// reproduces never drifts with the remaining budget. A budget too small for
// the recorded view degrades below it for that assembly alone, leaving the
// recorded decision and its settle fact as they stand; a fact that changed under
// the decision recomputes the presentation over the changed fact and replaces
// the decision. An entry without a recorded decision degrades from the full text
// to a ticket and then to the summary until the budget is met, settling on the
// decision it records as one replay-settle fact. The assembled request never
// runs past the budget it names: every entry it carries, the recovery hint and
// the notice card included, is cut to the room that remains. An entry that fits
// none of them is skipped without a record and without a settle fact, so a later
// assembly under a larger budget can still present it in full — except a notice
// entry, which the budget may shorten to its identity card but never drop: a
// notice the model never read is an event the model never saw arrive.
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
// under the recorded decisions, the facts those decisions settled over and the
// remaining budget.
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
		hint.Content = cutBytes(hint.Content, limit)
		out = append(out, hint)
		used += len(hint.Content)
	}
	for _, entry := range entries {
		original := p.originalOf(entry)
		room := limit - used
		if entry.Key != "" {
			if decision, recorded := p.decisions[entry.Key]; recorded {
				if p.settledOnLocked(entry.Key, original) {
					view := p.viewOf(decision, entry)
					if len(view) <= room {
						shown := entry
						shown.Content = view
						out = append(out, shown)
						used += len(view)
						continue
					}
					if shown, ok := p.presentWithin(entry, original, room, belowForm(decision.Mode), settleNone); ok {
						out = append(out, shown)
						used += len(shown.Content)
					}
					continue
				}
				if shown, ok := p.presentWithin(entry, original, room, formFull, settleReplace); ok {
					out = append(out, shown)
					used += len(shown.Content)
				}
				continue
			}
		}
		if shown, ok := p.presentWithin(entry, original, room, formFull, settleRecord); ok {
			out = append(out, shown)
			used += len(shown.Content)
		}
	}
	return out
}

// noticeCard renders the least a request may present for a notice the budget
// cannot carry in any fuller form: the notice's status, the head of its material
// and the ticket that reaches the full original. The notice fact family commits
// as a feedback fact (the progress and settled reports of a call, and the
// execution-state reports of the host), so a feedback entry is identity-bearing
// material: the model must be able to tell that it arrived and what it reported,
// and budget pressure may shorten it but never drop it from the prefix the way
// it drops a plain entry. The ticket is reserved before the head is cut, so the
// identity reaches its original even when the bound leaves the head no room.
func noticeCard(entry ProjectionEntry, original string) string {
	status := boundedText(entry.Summary, noticeStatusChars)
	if status == "" {
		status = "notice"
	}
	head := original
	if at := strings.IndexByte(head, '\n'); at >= 0 {
		head = head[:at]
	}
	prefix := "[notice " + status + "] "
	ticket := ticketText(entry.Key)
	room := noticeCardChars - len(prefix) - len(ticket) - 2
	if room < 0 {
		room = 0
	}
	return prefix + boundedText(head, room) + " " + ticket
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
	p.bindSettledLocked(key, false)
	p.landSettleLocked(decision)
	p.reshapeLocked(key, decision)
}

// replaceLocked replaces the recorded decision of one entry with the decision
// its changed fact now settles on and lands that decision's settle fact: the
// fact baseline is rebound to the changed fact, and a settle fact the same
// decision already landed is not landed twice.
func (p *ReplayProjection) replaceLocked(key event.EventID, decision PresentationDecision) {
	if key == "" {
		return
	}
	p.decisions[key] = decision
	p.bindSettledLocked(key, true)
	p.landSettleLocked(decision)
	p.reshapeLocked(key, decision)
}

// landSettleLocked files the settle fact of one decision, skipping the fact a
// decision with the same identity already landed.
func (p *ReplayProjection) landSettleLocked(decision PresentationDecision) {
	settle := ReplaySettleFor(decision)
	for _, landed := range p.settles {
		if landed.ID == settle.ID {
			return
		}
	}
	settle.At = time.Now().UTC()
	p.settles = append(p.settles, settle)
}

// reshapeLocked rewrites the model view of the entries one key holds to the view
// the decision describes, leaving the kept original untouched.
func (p *ReplayProjection) reshapeLocked(key event.EventID, decision PresentationDecision) {
	for i, entry := range p.entries {
		if entry.Key == key {
			p.entries[i].Content = p.viewOf(decision, entry)
		}
	}
}

// bindSettledLocked binds the fact baseline a decision settles over: the kept
// original at the moment the decision was taken, so a later assembly can tell a
// changed fact from a changed budget. A rebinding replaces the baseline of a
// decision recomputed over a changed fact.
func (p *ReplayProjection) bindSettledLocked(key event.EventID, rebind bool) {
	if !rebind {
		if _, bound := p.settled[key]; bound {
			return
		}
	}
	if original, kept := p.originals[key]; kept {
		p.settled[key] = original
	}
}

// settledOnLocked reports whether the fact a recorded decision settled over is
// still the fact the entry carries: only a changed fact recomputes a recorded
// presentation, never a changed budget.
func (p *ReplayProjection) settledOnLocked(key event.EventID, original string) bool {
	baseline, bound := p.settled[key]
	if !bound {
		return true
	}
	return baseline == original
}

// settleAction names what a presentation settles on once one fits the room:
// recording the first decision of an entry, replacing the decision a changed
// fact invalidated, or settling nothing for a degraded replay of a decision that
// stands.
type settleAction int

const (
	settleNone settleAction = iota
	settleRecord
	settleReplace
)

// presentationForm names one shape an entry is presented in, ordered from the
// fullest to the shortest: the full text, the ticket, the stored summary and —
// for a notice only — the identity card.
type presentationForm int

const (
	formFull presentationForm = iota
	formTicket
	formSummary
	formCard
)

// belowForm returns the form one step below a recorded presentation, so a
// degraded replay re-presents below the decision it degraded from and never at
// or above it.
func belowForm(mode PresentationMode) presentationForm {
	switch mode {
	case PresentationFull:
		return formTicket
	case PresentationTicket:
		return formSummary
	default:
		return formCard
	}
}

// presentWithin presents the entry at the first form from the one named that
// fits the room left in the budget, settling the decision it presents under
// record. A notice card is the one form that is cut to the room instead of
// skipped when it does not fit: a notice the model never read is an event the
// model never saw arrive, and cutting it keeps the assembled request inside the
// budget without dropping the arrival.
func (p *ReplayProjection) presentWithin(entry ProjectionEntry, original string, room int, from presentationForm, record settleAction) (ProjectionEntry, bool) {
	for form := from; form <= formCard; form++ {
		decision := PresentationDecision{Key: entry.Key}
		content := original
		switch form {
		case formFull:
			decision.Mode = PresentationFull
		case formTicket:
			preview := boundedText(original, ticketPreviewBytes)
			decision.Mode = PresentationTicket
			decision.Preview = preview
			content = preview + "\n" + ticketText(entry.Key)
		case formSummary:
			decision.Mode = PresentationSummary
			decision.Preview = entry.Summary
			content = entry.Summary
		case formCard:
			if entry.Type != event.TypeFeedback {
				continue
			}
			card := noticeCard(entry, original)
			decision.Mode = PresentationSummary
			decision.Preview = card
			if room <= 0 {
				return ProjectionEntry{}, false
			}
			content = cutBytes(card, room)
		}
		if form != formCard && len(content) > room {
			continue
		}
		switch record {
		case settleRecord:
			p.recordLocked(entry.Key, decision)
		case settleReplace:
			p.replaceLocked(entry.Key, decision)
		}
		shown := entry
		shown.Content = content
		return shown, true
	}
	return ProjectionEntry{}, false
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

// cutBytes bounds text to n bytes on a rune boundary without a cut mark, so the
// cut result never runs past the hard byte bound it is cut for.
func cutBytes(text string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n]
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

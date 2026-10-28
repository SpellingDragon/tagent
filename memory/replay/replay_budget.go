// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// Budget assembly of one projection.
//
// A projection keeps the canonical text of every fact it holds and nothing about
// how a request presented it: the view a request carries is computed for that
// request alone, from the facts, the declaration and the budget it names. A later
// assembly under another budget is judged on its own, so material a tight budget
// shortened is presented whole again once the budget allows it and no assembly
// inherits the shape of an earlier one. What a past request actually showed is
// the request snapshot's record, never a decision the projection keeps.
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
	// ticketPreviewBytes bounds the preview a ticket presentation shows.
	ticketPreviewBytes = 128
	// noticeCardChars bounds the identity card a request presents for a notice
	// the budget cannot carry in any fuller form: the notice's status, the head
	// of its material and the ticket that reaches the full original.
	noticeCardChars = 300
	// noticeStatusChars bounds the status a notice card names, so a long stored
	// summary never eats the room the identity and the ticket need.
	noticeStatusChars = 60
	// noticeStatusFrame is the card frame the status is stated in: the room the
	// status may take is what the bound leaves beside this frame and the ticket.
	noticeStatusFrame = "[notice ] "
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
// fact chain and the compaction payload a fold left on the chain. It is what
// RebuildProjectionFromWAL replays into.
//
// A projection holds no state outside the fact chain, so replaying the recorded
// facts reproduces the same entries, and it holds no presentation decision: the
// view a request carries belongs to that request and is computed when it is
// assembled. The entries hold the canonical form (the original text of each
// keyed fact), and an assembly shapes a copy of it rather than the entry.
// Constraints:
//
//   - a keyed entry already present is appended once, and a keyless entry always is;
//   - the original text of an entry is immutable input, and the model view is the assembly's output;
//   - a rebuild recomputes the deduplication key set from the entries it keeps.
type ReplayProjection struct {
	mu        sync.Mutex
	entries   []ProjectionEntry
	seen      map[event.EventID]struct{}
	originals map[event.EventID]string
	consumed  map[event.EventID]struct{}
	fold      *CompactionFact
	boundary  event.EventID
	recovery  *ProjectionEntry
}

// NewReplayProjection returns an empty replayable projection.
func NewReplayProjection() *ReplayProjection {
	return &ReplayProjection{
		seen:      map[event.EventID]struct{}{},
		originals: map[event.EventID]string{},
		consumed:  map[event.EventID]struct{}{},
	}
}

// Append adds the canonical entry of one committed fact: a keyed entry already
// present is appended once, a keyless entry always is. The entry carries the
// fact's own text, which is what the projection keeps — an append never shapes
// the text for a model, so the same entry presents differently under different
// budgets and the fact reads back unchanged either way. An append is an
// in-memory mutation, so it takes the port's context with nothing to cancel.
func (p *ReplayProjection) Append(_ context.Context, entry ProjectionEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry.Key != "" {
		if _, dup := p.seen[entry.Key]; dup {
			return
		}
		p.seen[entry.Key] = struct{}{}
		p.originals[entry.Key] = entry.Content
		if entry.Type == event.TypeThinkingPlan {
			p.consumed[entry.Key] = struct{}{}
		}
	}
	p.entries = append(p.entries, entry)
}

// AssembleBudget packs the projection into one agent request of the given token
// budget and returns the entries the request is assembled from, recovery hint
// first when one applies. Every entry is presented at the first form that fits
// the room the budget leaves — the full text, the ticket, the stored summary and
// for a notice its identity card — computed for this assembly alone: nothing is
// read back from how an earlier assembly presented the same fact, so a budget
// that shrinks presents less and a budget that widens presents more again. The
// source facts are read and never written, and the assembly files no settle fact
// and keeps no decision, so the projection is left as the next assembly found it.
//
// The assembled request never runs past the budget it names: every entry it
// carries, the recovery hint and the notice card included, is cut to the room
// that remains. An entry that fits none of the forms is skipped, so a later
// assembly under a larger budget can still present it in full — except a notice
// entry, which the budget may shorten to its identity card but never drop: a
// notice the model never read is an event the model never saw arrive.
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
// under the room the budget leaves.
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
		if view, ok := presentWithin(entry, p.originalOf(entry), limit-used); ok {
			out = append(out, view)
			used += len(view.Content)
		}
	}
	return out
}

// noticeCard renders the least a request may present for a notice the budget
// cannot carry in any fuller form: the notice's status, the head of its
// material and the ticket that reaches the full original. The notice fact family
// commits as a feedback fact (the progress and settled reports of a call, and the
// execution-state reports of the host), so a feedback entry is identity-bearing
// material: the model must be able to tell that it arrived and what it reported,
// and budget pressure may shorten it but never drop it from the prefix the way
// it drops a plain entry.
//
// The card is bounded by the room the assembly leaves and by noticeCardChars,
// and the ticket is reserved first: the identity that reaches the original is the
// last part the bound takes away, so a bound with no room left for the material
// head still presents the status and the ticket, and a bound with no room left
// for the status still presents the ticket.
func noticeCard(entry ProjectionEntry, original string, room int) string {
	bound := room
	if bound > noticeCardChars {
		bound = noticeCardChars
	}
	ticket := ticketText(entry.Key)
	if bound <= len(ticket) {
		return cutBytes(ticket, bound)
	}
	status := boundedText(entry.Summary, noticeStatusChars)
	if status == "" {
		status = "notice"
	}
	if statusRoom := bound - len(ticket) - 1 - len(noticeStatusFrame); statusRoom < len(status) {
		status = cutBytes(status, statusRoom)
	}
	if status == "" {
		return ticket
	}
	head := original
	if at := strings.IndexByte(head, '\n'); at >= 0 {
		head = head[:at]
	}
	prefix := "[notice " + status + "] "
	headRoom := bound - len(prefix) - len(ticket) - 1
	if headRoom <= 0 || head == "" {
		return prefix + ticket
	}
	return prefix + cutBytes(head, headRoom) + " " + ticket
}

// shown returns one entry as the assembly presents it: the fact's own text stays
// where the projection keeps it, and the view the request carries is the copy
// beside it, so presenting never writes back into the entry.
func shown(entry ProjectionEntry, content string) ProjectionEntry {
	entry.Content = content
	return entry
}

// presentWithin presents one entry at the first form from the full text down
// that fits the room the assembly leaves: the full text, the ticket, the stored
// summary and, for a notice, the identity card. An entry no form fits is
// presented by nothing at all, so a roomier assembly can still present it in
// full; a notice card is the one form cut to the room instead of skipped, because
// the arrival of a notice is material the model must see.
func presentWithin(entry ProjectionEntry, original string, room int) (ProjectionEntry, bool) {
	if room <= 0 {
		return ProjectionEntry{}, false
	}
	if len(original) <= room {
		return shown(entry, original), true
	}
	if ticket := boundedText(original, ticketPreviewBytes) + "\n" + ticketText(entry.Key); len(ticket) <= room {
		return shown(entry, ticket), true
	}
	if len(entry.Summary) <= room {
		return shown(entry, entry.Summary), true
	}
	if entry.Type != event.TypeFeedback {
		return ProjectionEntry{}, false
	}
	return shown(entry, noticeCard(entry, original, room)), true
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

// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The attention face's own tests: the round trip of one question through the
// chain, the refusals that keep an orphan answer and an orphan clearance off it,
// the presence a ruling or a clearance removes, the presence a restarted face
// answers over the chain it is handed, and the validity a read judges.
package task

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// attentionNowMs is the millisecond the attention tests read a chain at.
const attentionNowMs = int64(1_700_000_000_000)

// errAttentionChainConflict is the refusal the fake chain answers a second fact
// filed under a key that already names other content with.
var errAttentionChainConflict = errors.New("attention chain: the key already names other content")

// attentionChainFake is the in-memory attention chain the face is filed through
// and read from: one fact per identity key, in landing order, with the
// same-content repeat and the other-content refusal the chain contract states.
type attentionChainFake struct {
	facts []event.Event
	byKey map[event.EventID]event.Event
}

// newAttentionChainFake returns an empty chain.
func newAttentionChainFake() *attentionChainFake {
	return &attentionChainFake{byKey: map[event.EventID]event.Event{}}
}

// newAttentionChainFakeOf returns a chain holding the facts of another, as a
// process that starts over the same chain reads it.
func newAttentionChainFakeOf(facts []event.Event) *attentionChainFake {
	chain := newAttentionChainFake()
	chain.facts = append(chain.facts, facts...)
	for _, fact := range facts {
		chain.byKey[fact.Key] = fact
	}
	return chain
}

// RecordAttention stores one attention fact and reports whether this call stored
// it; a repeat of the fact the key names answers false, and other content under
// that key is refused by name. The filing moment is not part of that judgment: a
// fact refiled at a later millisecond is the fact the key already names, exactly
// as the ledger's own identity comparison reads it.
func (f *attentionChainFake) RecordAttention(ctx context.Context, fact event.Event) (bool, error) {
	if held, ok := f.byKey[fact.Key]; ok {
		if !attentionSameFact(held, fact) {
			return false, errAttentionChainConflict
		}
		return false, nil
	}
	f.byKey[fact.Key] = fact
	f.facts = append(f.facts, fact)
	return true, nil
}

// attentionSameFact reports whether two events state the same fact: every field
// but the filing moment, which one identity deliberately leaves out.
func attentionSameFact(held, filed event.Event) bool {
	held.Timestamp = time.Time{}
	filed.Timestamp = time.Time{}
	return reflect.DeepEqual(held, filed)
}

// AttentionFacts returns every attention fact the chain holds, in landing order.
func (f *attentionChainFake) AttentionFacts(ctx context.Context) ([]event.Event, error) {
	return append([]event.Event(nil), f.facts...), nil
}

// attentionClockAt returns a clock that answers one fixed millisecond.
func attentionClockAt(nowMs *int64) func() time.Time {
	return func() time.Time { return time.UnixMilli(*nowMs) }
}

// attentionQuestion builds one question fact the tests file on a chain.
func attentionQuestion(target, lineage string, expiresMs int64) AttentionFact {
	return AttentionFact{
		Type:    event.TypeAttentionQuestion,
		Kind:    AttentionKindApproval,
		Target:  target,
		Lineage: lineage,
		Detail:  AttentionDetail{Text: "run the send call?", ExpiresMs: expiresMs},
	}
}

// attentionAnswer builds one answer fact the tests file on a chain.
func attentionAnswer(target, lineage string) AttentionFact {
	return AttentionFact{
		Type:    event.TypeAttentionAnswer,
		Kind:    AttentionKindApproval,
		Target:  target,
		Lineage: lineage,
		Detail:  AttentionDetail{Text: "approved", By: "operator"},
	}
}

// TestAttentionFactRoundTrip pins the round trip of one question through the chain.
// - a committed question is read back with the kind, target, lineage and validity it was filed under
// - a repeated filing of that question, a second later, answers the same key and leaves one fact on the chain
// - a filter naming another target lists nothing
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionFactRoundTrip(t *testing.T) {
	ctx := context.Background()
	nowMs := attentionNowMs
	chain := newAttentionChainFake()
	face := NewAttention(chain, WithAttentionClock(attentionClockAt(&nowMs)))
	question := attentionQuestion("session-a", "call-1", nowMs+60_000)

	key, err := face.Commit(ctx, question)
	if err != nil {
		t.Fatalf("commit the question = %v, want it filed", err)
	}
	if key == "" {
		t.Fatalf("commit the question answered no identity")
	}
	held, err := face.Present(ctx, AttentionFilter{})
	if err != nil {
		t.Fatalf("read the presence = %v, want the filed question", err)
	}
	wanted := []AttentionQuestion{{Kind: AttentionKindApproval, Target: "session-a", Lineage: "call-1", ExpiresMs: nowMs + 60_000}}
	if !reflect.DeepEqual(held, wanted) {
		t.Fatalf("presence = %+v, want the one question it was filed under %+v", held, wanted)
	}
	again, err := face.Commit(ctx, question)
	if err != nil || again != key {
		t.Fatalf("a repeated filing = (%q, %v), want the identity already filed %q", again, err, key)
	}
	if got := len(chain.facts); got != 1 {
		t.Fatalf("facts after the repeated filing = %d, want the one question alone", got)
	}
	nowMs += 1_000
	if later, err := face.Commit(ctx, question); err != nil || later != key {
		t.Fatalf("a filing a second later = (%q, %v), want the identity already filed %q", later, err, key)
	}
	if got := len(chain.facts); got != 1 {
		t.Fatalf("facts after the filing a second later = %d, want the one question alone", got)
	}
	if got := chain.facts[0].Timestamp.UnixMilli(); got != attentionNowMs {
		t.Fatalf("the filing moment the chain kept = %d, want the first one %d", got, attentionNowMs)
	}
	elsewhere, err := face.Present(ctx, AttentionFilter{Target: "session-b"})
	if err != nil || len(elsewhere) != 0 {
		t.Fatalf("presence of another target = (%+v, %v), want nothing", elsewhere, err)
	}
}

// TestAttentionAnswerWithoutQuestionRefused pins the refusal of a fact naming no question.
// - an answer naming a question the chain does not hold is refused by name and files nothing
// - a clearance naming no question is refused by name and files nothing
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionAnswerWithoutQuestionRefused(t *testing.T) {
	ctx := context.Background()
	nowMs := attentionNowMs
	chain := newAttentionChainFake()
	face := NewAttention(chain, WithAttentionClock(attentionClockAt(&nowMs)))

	key, err := face.Commit(ctx, attentionAnswer("session-a", "call-1"))
	if !errors.Is(err, ErrAttentionQuestionMissing) {
		t.Fatalf("an answer naming no question = %v, want %v", err, ErrAttentionQuestionMissing)
	}
	if key != "" {
		t.Fatalf("a refused answer answered the identity %q, want none", key)
	}
	orphan := AttentionFact{
		Type:    event.TypeAttentionClearance,
		Kind:    AttentionKindApproval,
		Target:  "session-a",
		Lineage: "call-1",
		Detail:  AttentionDetail{Clearance: AttentionClearanceWorkEnd},
	}
	if _, err := face.Commit(ctx, orphan); !errors.Is(err, ErrAttentionQuestionMissing) {
		t.Fatalf("a clearance naming no question = %v, want %v", err, ErrAttentionQuestionMissing)
	}
	if got := len(chain.facts); got != 0 {
		t.Fatalf("facts after the refused filings = %d, want none", got)
	}
}

// TestAttentionClearsOnAnswer pins the presence an answer removes.
// - a question stands in the presence until its answer lands
// - the question fact carries its text as a user message, and the answer fact carries the ruling, the ruler and both identities
// - the answer removes the question from the presence and leaves both facts on the chain
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionClearsOnAnswer(t *testing.T) {
	ctx := context.Background()
	nowMs := attentionNowMs
	chain := newAttentionChainFake()
	face := NewAttention(chain, WithAttentionClock(attentionClockAt(&nowMs)))

	if _, err := face.Commit(ctx, attentionQuestion("session-a", "call-1", nowMs+60_000)); err != nil {
		t.Fatalf("commit the question = %v, want it filed", err)
	}
	standing, err := face.Present(ctx, AttentionFilter{})
	if err != nil || len(standing) != 1 {
		t.Fatalf("presence before the answer = (%+v, %v), want the one question", standing, err)
	}
	if _, err := face.Commit(ctx, attentionAnswer("session-a", "call-1")); err != nil {
		t.Fatalf("commit the answer = %v, want it filed", err)
	}
	after, err := face.Present(ctx, AttentionFilter{})
	if err != nil {
		t.Fatalf("read the presence after the answer = %v, want nothing", err)
	}
	if len(after) != 0 {
		t.Fatalf("presence after the answer = %+v, want the answered question gone", after)
	}
	if got := len(chain.facts); got != 2 {
		t.Fatalf("facts after the answer = %d, want the question and its answer", got)
	}
	asked := chain.facts[0]
	if asked.Message.Role != model.RoleUser || asked.Message.Content != "run the send call?" {
		t.Fatalf("the question body = %+v, want the question text as a user message", asked.Message)
	}
	answer := chain.facts[1]
	if answer.Type != event.TypeAttentionAnswer {
		t.Fatalf("the second fact = %q, want the answer", answer.Type)
	}
	if answer.Message.Role != model.RoleUser || answer.Message.Content != "approved" {
		t.Fatalf("the answer body = %+v, want the ruling as a user message", answer.Message)
	}
	if summary := answer.Meta[event.MetaKeyEventSummary]; !strings.Contains(summary, "by=operator") {
		t.Fatalf("the answer summary = %q, want the ruler it was ruled by", summary)
	}
}

// TestAttentionSurvivesRestart pins the presence a restarted face answers.
// - two questions are filed and one of them answered, and the presence lists the other
// - a face built over a chain rebuilt from those facts answers the same presence
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	nowMs := attentionNowMs
	chain := newAttentionChainFake()
	face := NewAttention(chain, WithAttentionClock(attentionClockAt(&nowMs)))

	if _, err := face.Commit(ctx, attentionQuestion("session-a", "call-1", nowMs+60_000)); err != nil {
		t.Fatalf("commit the first question = %v, want it filed", err)
	}
	if _, err := face.Commit(ctx, attentionQuestion("session-b", "call-2", 0)); err != nil {
		t.Fatalf("commit the second question = %v, want it filed", err)
	}
	if _, err := face.Commit(ctx, attentionAnswer("session-a", "call-1")); err != nil {
		t.Fatalf("commit the answer = %v, want it filed", err)
	}
	before, err := face.Present(ctx, AttentionFilter{})
	if err != nil || len(before) != 1 {
		t.Fatalf("presence before the restart = (%+v, %v), want the open question alone", before, err)
	}
	restarted := NewAttention(newAttentionChainFakeOf(chain.facts), WithAttentionClock(attentionClockAt(&nowMs)))
	after, err := restarted.Present(ctx, AttentionFilter{})
	if err != nil {
		t.Fatalf("read the presence after the restart = %v, want the chain's questions", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("presence after the restart = %+v, want the one held before it %+v", after, before)
	}
	if after[0].Lineage != "call-2" {
		t.Fatalf("the question the restart holds = %+v, want the unanswered one", after[0])
	}
}

// TestAttentionExpiresOnRead pins the validity a presence read judges.
// - a question inside its validity is read as one awaiting an answer
// - the same question past its validity is reported expired, never as awaiting an answer
// - a clearance removes it from the presence and leaves the question and the clearance on the chain
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionExpiresOnRead(t *testing.T) {
	ctx := context.Background()
	nowMs := attentionNowMs
	chain := newAttentionChainFake()
	face := NewAttention(chain, WithAttentionClock(attentionClockAt(&nowMs)))

	if _, err := face.Commit(ctx, attentionQuestion("session-a", "call-1", nowMs+60_000)); err != nil {
		t.Fatalf("commit the question = %v, want it filed", err)
	}
	inside, err := face.Present(ctx, AttentionFilter{})
	if err != nil || len(inside) != 1 || inside[0].Expired {
		t.Fatalf("presence inside the validity = (%+v, %v), want one question awaiting an answer", inside, err)
	}
	nowMs += 60_000
	expired, err := face.Present(ctx, AttentionFilter{})
	if err != nil || len(expired) != 1 || !expired[0].Expired {
		t.Fatalf("presence past the validity = (%+v, %v), want the question reported expired", expired, err)
	}
	clearance := AttentionFact{
		Type:    event.TypeAttentionClearance,
		Kind:    AttentionKindApproval,
		Target:  "session-a",
		Lineage: "call-1",
		Detail:  AttentionDetail{Clearance: AttentionClearanceExpiry},
	}
	if _, err := face.Commit(ctx, clearance); err != nil {
		t.Fatalf("commit the clearance = %v, want it filed", err)
	}
	cleared, err := face.Present(ctx, AttentionFilter{})
	if err != nil || len(cleared) != 0 {
		t.Fatalf("presence after the clearance = (%+v, %v), want nothing", cleared, err)
	}
	if got := len(chain.facts); got != 2 {
		t.Fatalf("facts after the clearance = %d, want the question and its clearance", got)
	}
}

// TestAttentionRefusalsAreNamed pins the named refusals the filing face answers.
// - a fact naming no frozen role, no known kind or no question identity is refused by name
// - a clearance word outside the three frozen words is refused by name
// - a face attached to no chain refuses both faces by name
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAttentionRefusalsAreNamed(t *testing.T) {
	ctx := context.Background()
	nowMs := attentionNowMs
	chain := newAttentionChainFake()
	face := NewAttention(chain, WithAttentionClock(attentionClockAt(&nowMs)))

	unknownRole := attentionQuestion("session-a", "call-1", 0)
	unknownRole.Type = "attention.unknown"
	if _, err := face.Commit(ctx, unknownRole); !errors.Is(err, ErrAttentionRoleUnknown) {
		t.Fatalf("a fact of no frozen role = %v, want %v", err, ErrAttentionRoleUnknown)
	}
	unknownKind := attentionQuestion("session-a", "call-1", 0)
	unknownKind.Kind = "reminder"
	if _, err := face.Commit(ctx, unknownKind); !errors.Is(err, ErrAttentionKindUnknown) {
		t.Fatalf("a question of no known kind = %v, want %v", err, ErrAttentionKindUnknown)
	}
	noTarget := attentionQuestion("", "call-1", 0)
	if _, err := face.Commit(ctx, noTarget); !errors.Is(err, ErrAttentionIdentityMissing) {
		t.Fatalf("a question naming no target = %v, want %v", err, ErrAttentionIdentityMissing)
	}
	unknownWord := AttentionFact{
		Type:    event.TypeAttentionClearance,
		Kind:    AttentionKindApproval,
		Target:  "session-a",
		Lineage: "call-1",
		Detail:  AttentionDetail{Clearance: "dismissed"},
	}
	if _, err := face.Commit(ctx, unknownWord); !errors.Is(err, ErrAttentionClearanceUnknown) {
		t.Fatalf("a clearance of no frozen word = %v, want %v", err, ErrAttentionClearanceUnknown)
	}
	detached := NewAttention(nil)
	if _, err := detached.Commit(ctx, attentionQuestion("session-a", "call-1", 0)); !errors.Is(err, ErrAttentionNoChain) {
		t.Fatalf("a filing on a detached face = %v, want %v", err, ErrAttentionNoChain)
	}
	if held, err := detached.Present(ctx, AttentionFilter{}); !errors.Is(err, ErrAttentionNoChain) || held != nil {
		t.Fatalf("a read on a detached face = (%+v, %v), want %v", held, err, ErrAttentionNoChain)
	}
}

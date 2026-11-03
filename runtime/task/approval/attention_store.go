// The attention face of the approval channel: the question one held call raises,
// the answer an approver rules it with and the clearance that removes it, each
// filed as a fact on a chain instead of an index of the store's own. The manager
// keeps the evidence an approver reads and the file an external approver answers;
// presence and ruling are folded from the facts, so what a restart reads back is
// what landed.
//
// - one approval question is identified by its kind, its target and its lineage; the lineage names the tool, the digest of the exact argument bytes and the approval identity the registration was issued under
// - presence is a question the chain holds with no answer and no clearance beside it, judged against the validity it was filed under
// - a ruling is the answer's own body, read back as the status word it was filed with
// - identities are compared through the fact key, so the lineage text is never parsed back apart
//
// The redline list (3 rules):
// MUST derive every presence and every ruling from the facts the chain holds, never from the request file.
// MUST compare one question, its answer and its clearance by fact key, never by splitting the lineage back into fields.
// MUST NOT keep a presence or a ruling beside the chain: the evidence block and the request file only mirror what the facts state.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package approval

import (
	"context"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// approvalAttentionTarget is the target one approval question is put to: the
// approval channel itself, so an approval question never shares an identity with
// a question another channel raises.
const approvalAttentionTarget = "approval"

// errApprovalFactConflict reports one fact key offered twice for other content:
// an identity stands for one fact, so the second offer is refused by name instead
// of overwriting the fact the chain already holds.
var errApprovalFactConflict = fmt.Errorf("approval: the attention fact key stands for other content")

// approvalLineage answers the lineage of one approval question: the tool the held
// call names, the digest of the exact argument bytes that were held and the
// approval identity the registration was issued under. A ruling reaches the very
// arguments and the very registration it was rendered over, so a changed argument
// never rides an old approval.
func approvalLineage(tool, argsDigest, id string) string {
	return "tool=" + tool + "\x00" + "digest=" + argsDigest + "\x00" + "approval=" + id
}

// approvalFact renders one attention fact of the approval channel: the role it
// plays and the question identity it is about. One call answers the fact a
// question, its answer and its clearance meet on, so the three never drift apart.
func approvalFact(role, tool, argsDigest, id string) task.AttentionFact {
	return task.AttentionFact{
		Type:    role,
		Kind:    task.AttentionKindApproval,
		Target:  approvalAttentionTarget,
		Lineage: approvalLineage(tool, argsDigest, id),
	}
}

// rulingIn reads the ruling an answer states: the status word the answer was
// filed with, and whether it names one of the two rulings the channel honours.
func rulingIn(answer event.Event) (ApprovalStatus, bool) {
	switch status := ApprovalStatus(answer.Message.Content); status {
	case ApprovalApproved, ApprovalDenied:
		return status, true
	default:
		return "", false
	}
}

// attentionLedger is the fact chain the manager files attention facts on when the
// composition wires no chain of its own: the facts in landing order beside the
// fact each key landed as. It carries no presence of its own, so a face rebuilt
// over it answers the same presence.
type attentionLedger struct {
	mu    sync.Mutex
	order []event.Event
	held  map[event.EventID]event.Event
}

// newAttentionLedger returns an empty fact chain the manager files on.
func newAttentionLedger() *attentionLedger {
	return &attentionLedger{held: map[event.EventID]event.Event{}}
}

// RecordAttention stores one attention fact and reports whether this call stored
// it: a repeat of the fact the key already names answers false and stores
// nothing, and a key already standing for other content is refused by name.
func (l *attentionLedger) RecordAttention(_ context.Context, fact event.Event) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if held, ok := l.held[fact.Key]; ok {
		if held.Type != fact.Type || held.Message.Content != fact.Message.Content || !sameMeta(held.Meta, fact.Meta) {
			return false, fmt.Errorf("%w: %s", errApprovalFactConflict, fact.Key)
		}
		return false, nil
	}
	l.held[fact.Key] = fact
	l.order = append(l.order, fact)
	return true, nil
}

// AttentionFacts returns every attention fact the chain holds, in landing order.
func (l *attentionLedger) AttentionFacts(_ context.Context) ([]event.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]event.Event(nil), l.order...), nil
}

// sameMeta reports whether two metadata blocks state the same entries, so a
// repeat of one fact reads as the fact already held instead of as other content.
func sameMeta(one, other map[string]string) bool {
	if len(one) != len(other) {
		return false
	}
	for key, value := range one {
		if other[key] != value {
			return false
		}
	}
	return true
}

// approvalFacts is one read of the chain as the approval channel reads it: the
// fact every identity landed as, so presence and ruling are answered once per
// read instead of once per request.
type approvalFacts struct {
	held map[event.EventID]event.Event
}

// factsOf folds one read of the chain into the fact every identity landed as,
// keyed by the identity the fact carries.
func factsOf(held []event.Event) approvalFacts {
	facts := approvalFacts{held: make(map[event.EventID]event.Event, len(held))}
	for _, fact := range held {
		facts.held[fact.Key] = fact
	}
	return facts
}

// question reports the question fact one request was registered under, and
// whether the chain holds it at all.
func (f approvalFacts) question(tool, argsDigest, id string) (event.Event, bool) {
	fact, ok := f.held[event.EventID(approvalFact(event.TypeAttentionQuestion, tool, argsDigest, id).FactKey())]
	return fact, ok
}

// answer reports the answer fact one request was ruled with, and whether a
// ruling landed for it at all.
func (f approvalFacts) answer(tool, argsDigest, id string) (event.Event, bool) {
	fact, ok := f.held[event.EventID(approvalFact(event.TypeAttentionAnswer, tool, argsDigest, id).FactKey())]
	return fact, ok
}

// cleared reports whether a clearance removed the question one request raised.
func (f approvalFacts) cleared(tool, argsDigest, id string) bool {
	_, ok := f.held[event.EventID(approvalFact(event.TypeAttentionClearance, tool, argsDigest, id).FactKey())]
	return ok
}

// present reports whether the chain still holds the question one request raised
// as a question: filed, unanswered and uncleared.
func (f approvalFacts) present(tool, argsDigest, id string) bool {
	if _, ok := f.question(tool, argsDigest, id); !ok {
		return false
	}
	if _, ok := f.answer(tool, argsDigest, id); ok {
		return false
	}
	return !f.cleared(tool, argsDigest, id)
}

// stateOf reads the state one request is recorded in: the ruling an answer
// stated, or pending while the question stands awaiting one. The validity is
// judged by the reader that reports an effective status, so this read states what
// was recorded and never what the clock makes of it.
func stateOf(facts approvalFacts, held *ApprovalRequest) ApprovalState {
	state := ApprovalState{
		CreatedMs: held.CreatedMs,
		ExpiresMs: held.ExpiresMs,
		Status:    ApprovalPending,
	}
	if answer, ok := facts.answer(held.ToolName, held.ArgsDigest, held.ID); ok {
		if ruling, known := rulingIn(answer); known {
			state.Status = ruling
			state.DecidedBy = held.DecidedBy
		}
	}
	return state
}

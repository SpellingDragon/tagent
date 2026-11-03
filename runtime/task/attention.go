// 契约: docs/wiki/event/event-architecture.md#fact-identity
//
// The attention face: one question put to a target thread or person, the answer
// it is ruled with, and the clearance that removes it, each kept as a fact on
// the chain instead of in a store of its own. The face offers two methods — a
// commit that files one question, answer or clearance, and a read that answers
// the questions the chain still holds as questions — and every reading is
// derived from the facts alone, so a process that starts over the same chain
// reads back what it held before it stopped.
//
// - a question names the kind it is asked under, the target it is put to, the lineage it came from and how long it stands
// - one question identity is that kind, target and lineage; the role a fact plays is hashed beside it
// - an answer and a clearance name a question the chain already holds, and one naming no such question is refused by name
// - presence is a question no answer and no clearance names, judged against the validity it was filed under
// - validity is judged when the chain is read, with the clock the face reads its time from
//
// The redline list (4 rules):
// MUST derive every reading from the facts the chain holds, never from process memory.
// MUST refuse an answer or a clearance naming no question on the chain, by name, before anything lands.
// MUST NOT hold, scan or expire an attention fact on a goroutine or a timer of its own.
// MUST NOT reach the composition root: the chain is a narrow port the composition supplies to this package.
package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// AttentionKindApproval, AttentionKindNotice and AttentionKindSummary name the
// kind words an attention question is asked under.
const (
	// AttentionKindApproval is the kind of a question a pending approval raises.
	AttentionKindApproval = "approval"
	// AttentionKindNotice is the kind of a question the human half of a notification raises.
	AttentionKindNotice = "notice"
	// AttentionKindSummary is the kind of a question a reflection delivery asks.
	AttentionKindSummary = "summary"
)

// AttentionClearanceAnswer, AttentionClearanceExpiry and
// AttentionClearanceWorkEnd name the clearance words one clearance fact carries.
const (
	// AttentionClearanceAnswer is the clearance of a question its answer removed.
	AttentionClearanceAnswer = "answer"
	// AttentionClearanceExpiry is the clearance of a question whose validity ran out.
	AttentionClearanceExpiry = "expiry"
	// AttentionClearanceWorkEnd is the clearance of a question the work ended on.
	AttentionClearanceWorkEnd = "work-end"
)

// defaultAttentionKind is the kind a question is filed under when the caller names none.
const defaultAttentionKind = AttentionKindApproval

// attentionKeyPrefix marks an attention identity in the event key space, so an
// attention fact is never quoted as one of the session chain's own events.
const attentionKeyPrefix = "attention:"

// ErrAttentionNoChain reports a face attached to no fact chain, so nothing can be
// filed and no presence can be read.
var ErrAttentionNoChain = errors.New("task: the attention face is attached to no fact chain")

// ErrAttentionRoleUnknown reports an attention fact whose type is none of the
// three frozen roles, so the chain is offered nothing.
var ErrAttentionRoleUnknown = errors.New("task: the attention fact names no question, answer or clearance role")

// ErrAttentionKindUnknown reports a question asked under a kind word the frozen
// vocabulary does not carry.
var ErrAttentionKindUnknown = errors.New("task: the attention question names no known kind")

// ErrAttentionIdentityMissing reports an attention fact naming no target or no
// lineage, so two questions could not be told apart by identity.
var ErrAttentionIdentityMissing = errors.New("task: the attention fact names no target or no lineage")

// ErrAttentionClearanceUnknown reports a clearance fact whose word is none of
// answer, expiry and work-end.
var ErrAttentionClearanceUnknown = errors.New("task: the clearance names no known clearance word")

// ErrAttentionQuestionMissing reports an answer or a clearance naming a question
// the chain does not hold, so no orphan answer or clearance lands.
var ErrAttentionQuestionMissing = errors.New("task: the attention fact names a question the chain does not hold")

// attentionChain is the narrow chain port the attention face is filed through
// and read from: one record of a fact under its identity key and one read of the
// attention facts the chain holds. Its signatures carry built-in and event types
// only, so the chain the composition supplies satisfies it structurally and no
// package edge is required in either direction.
//
// The redline list (3 rules):
// MUST answer one fact under one identity key without recording a second fact for it.
// MUST refuse, by name, a fact whose key already names other content.
// MUST answer the attention facts in landing order, so a presence read follows the order the chain recorded.
type attentionChain interface {
	// RecordAttention stores one attention fact and reports whether this call
	// stored it: a repeat of the fact the key already names answers false and
	// stores nothing.
	RecordAttention(ctx context.Context, fact event.Event) (bool, error)
	// AttentionFacts returns every attention fact the chain holds, in landing order.
	AttentionFacts(ctx context.Context) ([]event.Event, error)
}

// AttentionDetail is the detail one attention fact states beside its identity:
// the text a question puts or an answer rules, the person who ruled, the
// validity a question stands under and the word a clearance names. A field is
// zero on the roles that state nothing through it.
type AttentionDetail struct {
	// Text is the question a question puts or the ruling an answer states; a
	// clearance states no model-readable text.
	Text string
	// By names the person who ruled, on an answer, and is empty on every other role.
	By string
	// ExpiresMs is the validity of a question as an absolute millisecond stamp;
	// zero reads as cleared when the work ends.
	ExpiresMs int64
	// Clearance is the word a clearance names: answer, expiry or work-end.
	Clearance string
}

// AttentionFact is one attention fact offered to the commit face: the role it
// plays, the question identity it names and the detail that role states. Kind,
// Target and Lineage together name one question, so a question, its answer and
// its clearance always meet on the same identity.
type AttentionFact struct {
	// Type is the role the fact plays: question, answer or clearance.
	Type string
	// Kind is the kind word the question is asked under, empty reading as approval.
	Kind string
	// Target is the thread or person the question is put to.
	Target string
	// Lineage is the context the question came from: a call identity, a task
	// identity or a reflection source.
	Lineage string
	// Detail is what the role states beside its identity.
	Detail AttentionDetail
}

// FactKey returns the fact's stable identity: the role it plays hashed beside
// the question identity it names, so a repeated filing of one role answers the
// key already on the chain.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (f AttentionFact) FactKey() string {
	sum := sha256.Sum256([]byte(f.Type + "\x00" + f.identity()))
	return attentionKeyPrefix + hex.EncodeToString(sum[:8])
}

// FactType returns the frozen type the fact is read as: the role it plays.
func (f AttentionFact) FactType() string { return f.Type }

// The attention family satisfies the fact category contract.
var _ event.Fact = AttentionFact{}

// AttentionQuestion is one question the chain still holds as a question: the
// identity it was asked under, the validity it stands on and whether that
// validity had run out when the chain was read.
type AttentionQuestion struct {
	// Kind is the kind word the question was asked under.
	Kind string
	// Target is the thread or person the question was put to.
	Target string
	// Lineage is the context the question came from.
	Lineage string
	// ExpiresMs is the validity the question was filed under, zero reading as
	// cleared when the work ends.
	ExpiresMs int64
	// Expired reports whether the validity had run out at the read, so the
	// question is reported expired and never as one awaiting an answer.
	Expired bool
}

// AttentionFilter narrows a presence read to one kind, one target or both; an
// empty field matches every value.
type AttentionFilter struct {
	// Kind matches the questions asked under that kind word, empty matching every kind.
	Kind string
	// Target matches the questions put to that thread or person, empty matching every target.
	Target string
}

// Attention is the attention face over one fact chain: it files questions,
// answers and clearances, and reads the questions the chain still holds as
// questions. The face keeps no state of its own, so a rebuilt face over the same
// chain answers the same presence.
type Attention struct {
	chain attentionChain
	now   func() time.Time
}

// AttentionOption configures an attention face at construction.
type AttentionOption func(*Attention)

// WithAttentionClock injects the clock a presence read judges validity by, so a
// test drives an expiry deterministically instead of waiting for one.
func WithAttentionClock(now func() time.Time) AttentionOption {
	return func(a *Attention) { a.now = now }
}

// NewAttention returns the attention face over one fact chain. A nil chain is
// accepted, and every call on the face is refused beside ErrAttentionNoChain, so
// a composition that attaches no chain fails loudly instead of reading an empty
// presence.
func NewAttention(chain attentionChain, opts ...AttentionOption) *Attention {
	a := &Attention{chain: chain, now: time.Now}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Commit files one attention fact on the chain and answers the identity it is
// filed under. A question is filed under the kind, target and lineage it names.
// An answer or a clearance names a question the chain already holds, and one
// naming no such question is refused beside ErrAttentionQuestionMissing before
// anything lands. A repeated filing of one role answers the identity already on
// the chain and records no second fact, and the fact already recorded stands.
func (a *Attention) Commit(ctx context.Context, fact AttentionFact) (event.EventID, error) {
	if a.chain == nil {
		return "", ErrAttentionNoChain
	}
	if err := fact.check(); err != nil {
		return "", err
	}
	filing := fact.event(a.now())
	if fact.Type != event.TypeAttentionQuestion {
		held, err := a.chain.AttentionFacts(ctx)
		if err != nil {
			return "", err
		}
		if !attentionHoldsQuestion(held, fact) {
			return "", fmt.Errorf("%w: %s", ErrAttentionQuestionMissing, filing.Key)
		}
	}
	if _, err := a.chain.RecordAttention(ctx, filing); err != nil {
		return "", err
	}
	return filing.Key, nil
}

// Present answers the questions the chain still holds as questions: those no
// answer and no clearance names, each beside whether its validity had run out
// when the chain was read. An answered or cleared question is not reported at
// all, and one whose validity ran out is reported expired rather than as one
// awaiting an answer.
func (a *Attention) Present(ctx context.Context, filter AttentionFilter) ([]AttentionQuestion, error) {
	if a.chain == nil {
		return nil, ErrAttentionNoChain
	}
	held, err := a.chain.AttentionFacts(ctx)
	if err != nil {
		return nil, err
	}
	return attentionPresent(held, filter, a.now().UnixMilli()), nil
}

// check refuses a fact that names no frozen role, no known kind, no question
// identity, or a clearance word outside the three frozen words.
func (f AttentionFact) check() error {
	switch f.Type {
	case event.TypeAttentionQuestion, event.TypeAttentionAnswer, event.TypeAttentionClearance:
	default:
		return ErrAttentionRoleUnknown
	}
	switch f.kind() {
	case AttentionKindApproval, AttentionKindNotice, AttentionKindSummary:
	default:
		return ErrAttentionKindUnknown
	}
	if f.Target == "" || f.Lineage == "" {
		return ErrAttentionIdentityMissing
	}
	if f.Type == event.TypeAttentionClearance && !isAttentionClearance(f.Detail.Clearance) {
		return ErrAttentionClearanceUnknown
	}
	return nil
}

// kind answers the kind word the fact is filed under: the kind the caller named,
// or the default kind when none was named.
func (f AttentionFact) kind() string {
	if f.Kind == "" {
		return defaultAttentionKind
	}
	return f.Kind
}

// identity answers the question the fact is about: the kind, the target and the
// lineage it names, joined with a separator that keeps the three apart whatever
// they carry. A question, its answer and its clearance answer the same identity.
func (f AttentionFact) identity() string {
	return f.kind() + "\x00" + f.Target + "\x00" + f.Lineage
}

// event renders the fact as the event the chain stores: the role as the frozen
// type, its identity as the key, the question identity and the role's detail in
// the metadata, the one-line read of the fact in the summary — which is where
// the ruler an answer states stays readable — and the text as the message a
// reader sees. A clearance carries no model-readable body.
func (f AttentionFact) event(at time.Time) event.Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	meta := map[string]string{
		event.MetaKeyEventSummary:     f.summaryLine(),
		event.MetaKeyAttentionKind:    f.kind(),
		event.MetaKeyAttentionTarget:  f.Target,
		event.MetaKeyAttentionLineage: f.Lineage,
	}
	if f.Detail.Clearance != "" {
		meta[event.MetaKeyAttentionClearance] = f.Detail.Clearance
	}
	if f.Type == event.TypeAttentionQuestion {
		meta[event.MetaKeyAttentionExpiresMs] = strconv.FormatInt(f.Detail.ExpiresMs, 10)
	}
	rendered := event.Event{
		Key:       event.EventID(f.FactKey()),
		Type:      f.Type,
		Timestamp: at,
		Meta:      meta,
	}
	if f.Type != event.TypeAttentionClearance && f.Detail.Text != "" {
		rendered.Message = model.Message{Role: model.RoleUser, Content: f.Detail.Text}
	}
	return rendered
}

// summaryLine reads one fact in a line: the role it plays, the identity it
// names, the validity a question stands under, the word a clearance carries and
// the person an answer was ruled by, so a committed fact reads in the sequence
// exactly as it was filed.
func (f AttentionFact) summaryLine() string {
	line := f.Type + " kind=" + f.kind() + " target=" + f.Target + " lineage=" + f.Lineage
	if f.Type == event.TypeAttentionQuestion {
		line += " expires_ms=" + strconv.FormatInt(f.Detail.ExpiresMs, 10)
	}
	if f.Type == event.TypeAttentionClearance {
		line += " clearance=" + f.Detail.Clearance
	}
	if f.Detail.By != "" {
		line += " by=" + f.Detail.By
	}
	return line
}

// attentionPresent folds the attention facts into the questions the chain still
// holds as questions, judged at nowMs: a question stands until an answer or a
// clearance names its identity, and a validity that ran out reports it expired.
func attentionPresent(facts []event.Event, filter AttentionFilter, nowMs int64) []AttentionQuestion {
	closed := map[string]bool{}
	for _, fact := range facts {
		if fact.Type == event.TypeAttentionQuestion {
			continue
		}
		closed[attentionIdentityOf(fact.Meta)] = true
	}
	held := make([]AttentionQuestion, 0, len(facts))
	for _, fact := range facts {
		if fact.Type != event.TypeAttentionQuestion {
			continue
		}
		if closed[attentionIdentityOf(fact.Meta)] {
			continue
		}
		question := attentionQuestionOf(fact, nowMs)
		if !filter.matches(question) {
			continue
		}
		held = append(held, question)
	}
	return held
}

// attentionQuestionOf reads one question fact as the question it states, judged
// against the millisecond the chain is read at.
func attentionQuestionOf(fact event.Event, nowMs int64) AttentionQuestion {
	question := AttentionQuestion{
		Kind:      fact.Meta[event.MetaKeyAttentionKind],
		Target:    fact.Meta[event.MetaKeyAttentionTarget],
		Lineage:   fact.Meta[event.MetaKeyAttentionLineage],
		ExpiresMs: attentionExpiresOf(fact.Meta),
	}
	question.Expired = question.ExpiresMs > 0 && nowMs >= question.ExpiresMs
	return question
}

// attentionHoldsQuestion reports whether the chain holds the question a fact
// names, so an answer and a clearance reach a question already filed.
func attentionHoldsQuestion(facts []event.Event, fact AttentionFact) bool {
	wanted := fact.identity()
	for _, held := range facts {
		if held.Type == event.TypeAttentionQuestion && attentionIdentityOf(held.Meta) == wanted {
			return true
		}
	}
	return false
}

// attentionIdentityOf answers the question identity a fact is about: the kind,
// the target and the lineage it names, joined with a separator that keeps the
// three apart whatever they contain.
func attentionIdentityOf(meta map[string]string) string {
	return meta[event.MetaKeyAttentionKind] + "\x00" + meta[event.MetaKeyAttentionTarget] + "\x00" + meta[event.MetaKeyAttentionLineage]
}

// attentionExpiresOf reads the validity stamp a question carries; an entry that
// is absent or unreadable reads as no validity of its own.
func attentionExpiresOf(meta map[string]string) int64 {
	stamp, err := strconv.ParseInt(meta[event.MetaKeyAttentionExpiresMs], 10, 64)
	if err != nil {
		return 0
	}
	return stamp
}

// isAttentionClearance reports whether a word is one of the three frozen
// clearance words.
func isAttentionClearance(word string) bool {
	switch word {
	case AttentionClearanceAnswer, AttentionClearanceExpiry, AttentionClearanceWorkEnd:
		return true
	default:
		return false
	}
}

// matches reports whether one question falls under the filter the read was asked with.
func (f AttentionFilter) matches(question AttentionQuestion) bool {
	if f.Kind != "" && f.Kind != question.Kind {
		return false
	}
	if f.Target != "" && f.Target != question.Target {
		return false
	}
	return true
}

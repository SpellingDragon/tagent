package query

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// ErrAttentionUnwired and the attention dimension beside it: the dimension is
// projected from an attention read face the port may wire beside its three
// readings, and a port that wires none answers the dimension by name instead of
// as an empty presence. The dimension itself keeps no state: every question it
// lists is read from the owner that holds it at read time.
var ErrAttentionUnwired = errors.New("query: the inventory source carries no attention read face")

// AttentionPhase is the phase one attention question stands in, read off the
// attention facts of its kind: a question nobody answered or cleared and whose
// validity has not run out is in the question phase, and a question whose
// validity ran out is in the expired phase. The vocabulary names the reading the
// owner reports; the projection adds no phase of its own and computes no total
// across phases.
//
// The answered and cleared phases name the two closings the attention chain
// records for a question. A question closed by one of them leaves the presence
// the read face answers at read time, so the projection reports that closing as
// the identity leaving the snapshot and as the delete increment of Follow
// rather than as one more entry carrying the closed phase.
type AttentionPhase string

const (
	// AttentionPhaseQuestion names a question still awaiting an answer.
	AttentionPhaseQuestion AttentionPhase = "question"
	// AttentionPhaseAnswered names a question an answer closed.
	AttentionPhaseAnswered AttentionPhase = "answered"
	// AttentionPhaseExpired names a question whose validity ran out.
	AttentionPhaseExpired AttentionPhase = "expired"
	// AttentionPhaseCleared names a question a work-end clearance closed.
	AttentionPhaseCleared AttentionPhase = "cleared"
)

// Attention reads the attention dimension alone and returns the questions the
// read face still holds as questions at read time, in the stable identity order
// of the dimension. A port that wires no attention face refuses the dimension
// beside ErrAttentionUnwired instead of answering an empty presence that would
// read as "nothing is waiting".
func (inv *Inventory) Attention(ctx context.Context) ([]task.AttentionQuestion, error) {
	if inv == nil || inv.reads == nil {
		return nil, &SourceError{Dimension: ObjectAttention, Cause: ErrSourceUnreadable}
	}
	if _, ok := inv.reads.(AttentionReading); !ok {
		return nil, &SourceError{Dimension: ObjectAttention, Cause: ErrAttentionUnwired}
	}
	g := inv.gather(ctx)
	if cause, ok := g.errs[ObjectAttention]; ok {
		return nil, &SourceError{Dimension: ObjectAttention, Cause: cause}
	}
	return append([]task.AttentionQuestion(nil), g.attention...), nil
}

// readAttention projects the attention dimension from the read face the port
// wires beside its three readings. A port that wires none answers no question
// here and leaves the naming of that absence to Inventory.Attention, so the six
// dimensions of the total view keep answering from the port they always read.
func (inv *Inventory) readAttention(ctx context.Context, g *gathering) {
	face, ok := inv.reads.(AttentionReading)
	if !ok {
		return
	}
	questions, err := face.AttentionQuestions(ctx)
	if err != nil {
		g.fail(err, ObjectAttention)
		return
	}
	sort.Slice(questions, func(i, j int) bool { return attentionLess(questions[i], questions[j]) })
	g.attention = questions
}

// attentionLess orders attention questions by the identity the dimension names
// them by: the kind they were asked under, the target they were put to and the
// lineage they came from.
func attentionLess(a, b task.AttentionQuestion) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Target != b.Target {
		return a.Target < b.Target
	}
	return a.Lineage < b.Lineage
}

// attentionDetailOf projects one attention question into its snapshot object:
// the phase its owner read it in. An expired question keeps the stamp it was
// filed under and reads as expired, so a caller telling one phase from another
// reads the phase and never the presence of an entry.
func attentionDetailOf(question task.AttentionQuestion) Detail {
	phase := AttentionPhaseQuestion
	if question.Expired {
		phase = AttentionPhaseExpired
	}
	return Detail{
		Ref:   ObjectRef{Kind: ObjectAttention, Address: attentionAddressOf(question.Target), attention: question},
		State: DetailState{attention: phase},
	}
}

// attentionKeyOf answers the identity one attention reference names: the kind
// the question was asked under, the target it was put to and the lineage it came
// from. The validity it was filed under is content beside that identity, so a
// question whose stamp is judged again keeps its identity and is explained as
// one changed entry rather than as a new object and a vanished one.
func attentionKeyOf(question task.AttentionQuestion) (kind, target, lineage string) {
	return question.Kind, question.Target, question.Lineage
}

// attentionTargetSep is the separator one attention target names an agent
// session with. A question put to a session reads as "agent/session" and is
// scoped to that session; a question whose target names no session belongs to no
// address and stays readable under any scope, the way a lease does.
const attentionTargetSep = "/"

// attentionAddressOf answers the address one attention target names, so a
// session-scoped read keeps the questions put to its own session and drops the
// ones another session holds beside it. A target that names no session - an
// approval book names its channel and not a session - answers the zero address,
// which reads as scoped by nothing but the scope itself.
func attentionAddressOf(target string) event.Address {
	agent, session, split := strings.Cut(target, attentionTargetSep)
	if !split || agent == "" || session == "" {
		return event.Address{}
	}
	return event.Address{AgentID: event.AgentID(agent), SessionID: event.SessionID(session)}
}

// attentionRefLess orders two attention identities the way the dimension orders
// them: the kind the question was asked under first, then its target and then the
// lineage it came from.
func attentionRefLess(a, b refID) bool {
	if a.attentionKind != b.attentionKind {
		return a.attentionKind < b.attentionKind
	}
	if a.target != b.target {
		return a.target < b.target
	}
	return a.lineage < b.lineage
}

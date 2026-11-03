package memoryqueryexample

import (
	"context"

	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// attentionReads is the canonical port of the optional attention read face: it
// answers the three readings of the diagnostic port and the attention questions
// its owners still hold as questions, so one port serves the six dimensions of
// the inventory and the attention dimension beside them.
type attentionReads struct {
	ownerReads
	questions []task.AttentionQuestion
}

// AttentionQuestions reads every attention question still present at read time.
func (r *attentionReads) AttentionQuestions(context.Context) ([]task.AttentionQuestion, error) {
	return append([]task.AttentionQuestion(nil), r.questions...), nil
}

var (
	_ query.Reading          = (*attentionReads)(nil)
	_ query.AttentionReading = (*attentionReads)(nil)
)

// ExampleAttentionReading gathers the attention dimension through the optional
// read face: the questions the owners still hold as questions are read from that
// face, and a port that wires none refuses the dimension by name instead of
// answering an empty presence.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func ExampleAttentionReading() {
	ctx := context.Background()
	reads := &attentionReads{
		questions: []task.AttentionQuestion{
			{
				Kind:      task.AttentionKindApproval,
				Target:    "approval",
				Lineage:   "tool=shell\x00digest=abc\x00approval=approval-1",
				ExpiresMs: 0,
			},
			{
				Kind:    task.AttentionKindSummary,
				Target:  "assistant/session-1",
				Lineage: "event=evt-1",
			},
		},
	}

	questions, err := reads.AttentionQuestions(ctx)
	if err != nil {
		return
	}
	inventory := query.NewInventory(reads)
	presence, err := inventory.Attention(ctx)
	if err != nil {
		return
	}
	_ = len(questions)
	_ = len(presence)
}

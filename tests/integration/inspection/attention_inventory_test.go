// The 03.1 positive anchor of the attention dimension: an inventory whose port
// wires the attention read face reports the approval, the notice and the summary
// in one ObjectAttention dimension, walks that dimension through the projection
// and its Follow increment, and refuses the dimension by name beside a port that
// wires no such face.
//
// - three kinds, one dimension: one question per kind enters the same reading and the same count
// - a question its owner dropped leaves the snapshot and enters Follow as the delete of its identity
// - a port beside no attention face refuses the dimension by name and keeps answering the six dimensions beside it
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
package inspection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// attentionPort is the read port of this case: no configured agent, no session
// site and no task, beside the attention questions it holds as questions. The
// questions answer through their own face, so the dimension is read from the
// owner that holds it and never derived from the three readings beside it.
type attentionPort struct {
	// questions is the presence the port answers at read time.
	questions []task.AttentionQuestion
}

// Published reads no binding generation.
func (p *attentionPort) Published(context.Context) (query.Generation, error) {
	return query.Generation{}, nil
}

// Sites reads no session site.
func (p *attentionPort) Sites(context.Context) ([]query.SiteReading, error) {
	return nil, nil
}

// Tasks reads no task.
func (p *attentionPort) Tasks(context.Context) ([]query.TaskReading, error) {
	return nil, nil
}

// AttentionQuestions reads the questions this port still holds.
func (p *attentionPort) AttentionQuestions(context.Context) ([]task.AttentionQuestion, error) {
	return append([]task.AttentionQuestion(nil), p.questions...), nil
}

// plainPort is the same port beside no attention face at all: the shape every
// composition that wires the three readings alone has.
type plainPort struct{}

// Published reads no binding generation.
func (plainPort) Published(context.Context) (query.Generation, error) { return query.Generation{}, nil }

// Sites reads no session site.
func (plainPort) Sites(context.Context) ([]query.SiteReading, error) { return nil, nil }

// Tasks reads no task.
func (plainPort) Tasks(context.Context) ([]query.TaskReading, error) { return nil, nil }

// TestAttentionInventoryDimension proves the ObjectAttention dimension answers the three kinds of question, follows one out, and refuses itself by name.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestAttentionInventoryDimension(t *testing.T) {
	ctx := context.Background()
	scope := query.AuthorizationScope{Agents: []event.AgentID{"assistant"}}
	filter := query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectAttention}}
	approval := task.AttentionQuestion{
		Kind:    task.AttentionKindApproval,
		Target:  "approval",
		Lineage: "tool=shell\x00digest=abc\x00approval=approval-1",
	}
	port := &attentionPort{questions: []task.AttentionQuestion{
		approval,
		{
			Kind:      task.AttentionKindNotice,
			Target:    "assistant/session-1",
			Lineage:   "task=task-1\x00round=1\x00kind=settled\x00seq=1",
			ExpiresMs: time.Now().Add(time.Hour).UnixMilli(),
		},
		{Kind: task.AttentionKindSummary, Target: "assistant/session-1", Lineage: "event=evt-1"},
	}}
	inventory := query.NewInventory(port)

	view, err := inventory.View(ctx)
	if err != nil {
		t.Fatalf("view = %v, want one reading over the port", err)
	}
	if got := len(view.Attention()); got != 3 {
		t.Fatalf("attention entries = %d, want the three kinds counted off one detail", got)
	}
	kinds := make([]string, 0, 3)
	for _, question := range view.Attention() {
		kinds = append(kinds, question.Kind)
	}
	for _, want := range []string{task.AttentionKindApproval, task.AttentionKindNotice, task.AttentionKindSummary} {
		if !containsString(kinds, want) {
			t.Fatalf("attention kinds = %v, want the %s question beside the other two", kinds, want)
		}
	}

	projection := query.NewProjection(inventory, 4)
	snapshot, _, err := projection.Snapshot(ctx, filter)
	if err != nil {
		t.Fatalf("snapshot = %v, want the attention dimension projected", err)
	}
	if len(snapshot.Details) != 3 {
		t.Fatalf("snapshot details = %d, want three attention objects", len(snapshot.Details))
	}
	for _, detail := range snapshot.Details {
		if detail.Ref.Kind != query.ObjectAttention {
			t.Fatalf("detail kind = %q, want the attention kind", detail.Ref.Kind)
		}
		if detail.State.AttentionPhase() != query.AttentionPhaseQuestion {
			t.Fatalf("attention phase = %q, want every listed question in the question phase", detail.State.AttentionPhase())
		}
	}

	port.questions = []task.AttentionQuestion{port.questions[1], port.questions[2]}
	after, _, err := projection.Snapshot(ctx, filter)
	if err != nil {
		t.Fatalf("snapshot after the answer = %v", err)
	}
	if len(after.Details) != 2 {
		t.Fatalf("snapshot details after the answer = %d, want the two questions still in presence", len(after.Details))
	}
	_, updates, err := projection.Follow(ctx, query.Subscription{Revision: snapshot.Revision, Filter: filter}, 8)
	if err != nil {
		t.Fatalf("follow = %v, want one increment over the answered question", err)
	}
	deleted := false
	for _, update := range updates {
		if update.Op != query.UpdateDelete {
			continue
		}
		if update.Ref.AttentionQuestion().Lineage == approval.Lineage {
			deleted = true
		}
	}
	if !deleted {
		t.Fatalf("follow updates = %+v, want the answered approval identity deleted from the presence", updates)
	}

	plain := query.NewInventory(plainPort{})
	if _, err := plain.Attention(ctx); !errors.Is(err, query.ErrAttentionUnwired) {
		t.Fatalf("unwired attention = %v, want the named refusal beside a port with no attention face", err)
	}
	if _, err := plain.View(ctx); err != nil {
		t.Fatalf("view beside no attention face = %v, want the six dimensions still answering", err)
	}
}

// containsString reports whether the list holds the value.
func containsString(list []string, value string) bool {
	for _, got := range list {
		if got == value {
			return true
		}
	}
	return false
}

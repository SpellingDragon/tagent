package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegatedInputRefusalLandsNothing is the negative anchor of the delegated
// input commit: the input of a delegated round commits as a batch of ordinary
// input facts through the commit face, and a commit the ledger refuses must leave
// the round unstarted instead of driving it beside a fact that never landed.
//
//   - the caller reads the named cause of the refusal, not a silently empty round
//   - the chain holds no input fact of the site: the refused fact was never landed
//   - exactly one commit was attempted, and it is the first fact of the batch: the
//     instruction as a system message under the stable site key, stamped with the
//     delegated part and the delegation lineage — no round stamp rides the fact
//   - no segment of the site was driven: no model request was made and the resident
//     instance stands at rest
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedInputRefusalLandsNothing(t *testing.T) {
	refused := errors.New("agent: the ledger refused the delegated input")
	site := event.Address{AgentID: "agent-refused", SessionID: "session-refused"}
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-refused", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)

	var mu sync.Mutex
	var attempts []event.Event
	loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		mu.Lock()
		attempts = append(attempts, evt)
		mu.Unlock()
		return "", refused
	})

	consumers := NewConsumers(loop, context.Background(), nil)
	loop.SetConsumers(consumers)
	t.Cleanup(consumers.Stop)
	script := newInvariantLoopModel("the delegated turn must not run", nil)
	binding := state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}}

	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer cancel()
	answer, err := loop.SubmitDelegated(ctx, DelegatedInput{
		Site:        site,
		Binding:     binding,
		Instruction: "run only after the input commits",
		Request:     "the task whose commit the ledger refuses",
		Materials:   []event.Event{{Key: "ev-refused-source", Type: event.TypeExternalInput}},
	})
	if err == nil {
		t.Fatalf("the refused delegated input answered no error: the round ran beside a fact that never landed (answer=%+v)", answer)
	}
	if !errors.Is(err, refused) {
		t.Fatalf("the refused delegated input answered %v, want the named cause %v", err, refused)
	}
	if !strings.Contains(err.Error(), refused.Error()) {
		t.Fatalf("the refusal text %q does not name the cause %q", err.Error(), refused.Error())
	}

	if facts := history.SessionFacts(site); len(facts) != 0 {
		t.Fatalf("the chain of %s holds %d fact(s) beside a refused commit: %+v", site.SessionID, len(facts), facts)
	}

	mu.Lock()
	got := append([]event.Event(nil), attempts...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("commit attempts = %d, want exactly the first fact of the batch the round commits", len(got))
	}
	if got[0].Type != event.TypeExternalInput {
		t.Fatalf("the attempted fact type = %q, want the external input %q", got[0].Type, event.TypeExternalInput)
	}
	if got[0].Message.Role != model.RoleSystem {
		t.Fatalf("the attempted fact role = %q, want the system message the instruction lands as", got[0].Message.Role)
	}
	if got[0].Message.Content != "run only after the input commits" {
		t.Fatalf("the attempted fact body = %q, want the instruction the round runs under", got[0].Message.Content)
	}
	if want := "delegated-agent-refused@session-refused-instruction"; string(got[0].Key) != want {
		t.Fatalf("the attempted fact key = %q, want the stable site key %q of the instruction", got[0].Key, want)
	}
	if got[0].Meta[delegatedPartMeta] != delegatedPartInstruction {
		t.Fatalf("the attempted fact part = %q, want the instruction part %q", got[0].Meta[delegatedPartMeta], delegatedPartInstruction)
	}
	if got[0].Meta[event.MetaKeyTriggerSource] != event.LineageDelegation {
		t.Fatalf("the attempted fact lineage = %q, want the delegation lineage %q", got[0].Meta[event.MetaKeyTriggerSource], event.LineageDelegation)
	}
	if got[0].DecisionID != "" || got[0].AttemptID != "" || got[0].CallID != "" || got[0].EpisodeID != "" {
		t.Fatalf("the attempted fact carries round stamps (%q, %q, %q, %q): the instruction is one fact of the site, never a fact of the round",
			got[0].DecisionID, got[0].AttemptID, got[0].CallID, got[0].EpisodeID)
	}

	if requests := script.requestMessages(); len(requests) != 0 {
		t.Fatalf("model requests = %d beside a refused input commit, want no segment driven: %+v", len(requests), requests)
	}
	resident := consumers.Ensure(site)
	if resident == nil {
		t.Fatalf("the resident instance of %s = nil, want the mounted instance the round would run on", site.SessionID)
	}
	invariantAwait(t, resident.Resting)
	if answer.Status != task.StatusFailed {
		t.Fatalf("the refused round answered status %d, want the failed terminal %d", answer.Status, task.StatusFailed)
	}
	if !strings.Contains(answer.Failure, refused.Error()) {
		t.Fatalf("the refused round answers failure %q, want it to name the cause %q", answer.Failure, refused.Error())
	}
}

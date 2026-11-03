package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegatedInstructionLandsOncePerSite is the positive anchor of the
// instruction fact: the instruction a delegated round runs under commits as a
// system message of the execution site's own chain, so the round reads it as
// material of the site rather than as a meta entry of the task.
//
//   - the site's chain holds exactly one system-message fact, under the stable
//     site key, carrying the instruction, the instruction part and the delegation
//     lineage — and no stamp of the round it was committed beside
//   - a second round of the same site commits that instruction identity again and
//     lands no second fact, while both rounds read the instruction as a system
//     message of their request
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedInstructionLandsOncePerSite(t *testing.T) {
	const instruction = "record every finding beside the file it names"
	site := event.Address{AgentID: "agent-reviewer", SessionID: "session-caller#sub:call-review"}
	loop := NewLoop(state.NewBindings(), state.NewOccupancy(), "instance-instruction", nil)
	history := newInvariantLoopHistory()
	loop.SetHistory(history)
	ledger := newDelegatedBatchLedger(history)
	loop.SetFactCommit(ledger.commit)

	script := newInvariantLoopModel("review recorded", nil)
	binding := state.AgentBinding{Model: script, PromptSource: state.PromptSource{Version: "v1"}}
	consumers := NewConsumers(loop, context.Background(), nil)
	loop.SetConsumers(consumers)
	t.Cleanup(consumers.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), backflowNestedBudget)
	defer cancel()
	for _, body := range []string{"review the first change", "review the second change"} {
		answer, err := loop.SubmitDelegated(ctx, DelegatedInput{
			Site:        site,
			Binding:     binding,
			Instruction: instruction,
			Request:     body,
		})
		if err != nil {
			t.Fatalf("the delegated round of %q = %v, want the settled answer", body, err)
		}
		if answer.Status != task.StatusSettled || answer.Body != "review recorded" {
			t.Fatalf("the delegated round of %q = (%q, %q), want the settled body the site produced", body, answer.Body, answer.Status)
		}
	}

	facts := history.SessionFacts(site)
	instructions := make([]event.Event, 0, 1)
	for _, fact := range facts {
		if fact.Meta[delegatedPartMeta] == delegatedPartInstruction {
			instructions = append(instructions, fact)
		}
	}
	if len(instructions) != 1 {
		t.Fatalf("instruction facts on the site chain = %d, want the one instruction of the site: %+v", len(instructions), facts)
	}
	got := instructions[0]
	if want := "delegated-agent-reviewer@session-caller#sub:call-review-instruction"; string(got.Key) != want {
		t.Fatalf("instruction fact key = %q, want the stable site key %q", got.Key, want)
	}
	if got.Type != event.TypeExternalInput {
		t.Fatalf("instruction fact type = %q, want the external input %q", got.Type, event.TypeExternalInput)
	}
	if got.Message.Role != model.RoleSystem {
		t.Fatalf("instruction fact role = %q, want the system message the instruction lands as", got.Message.Role)
	}
	if got.Message.Content != instruction {
		t.Fatalf("instruction fact body = %q, want the instruction the round runs under", got.Message.Content)
	}
	if got.Meta[event.MetaKeyTriggerSource] != event.LineageDelegation {
		t.Fatalf("instruction fact lineage = %q, want the delegation lineage %q", got.Meta[event.MetaKeyTriggerSource], event.LineageDelegation)
	}
	if got.DecisionID != "" || got.AttemptID != "" || got.CallID != "" || got.EpisodeID != "" || got.TaskID != "" {
		t.Fatalf("instruction fact carries round stamps (%q, %q, %q, %q, %q): the fact is one fact of the site",
			got.DecisionID, got.AttemptID, got.CallID, got.EpisodeID, got.TaskID)
	}

	if attempted := ledger.attempted(delegatedPartInstruction); attempted != 2 {
		t.Fatalf("instruction commits attempted = %d, want one per round so a re-entry keeps the fact of the site", attempted)
	}
	if landed := ledger.landed(delegatedPartInstruction); landed != 1 {
		t.Fatalf("instruction facts landed = %d, want the one identity the site already names", landed)
	}

	// One task fact per round, and neither of them lands the instruction a second
	// time: the batch of a round is the instruction of the site beside its own task.
	tasks := 0
	for _, fact := range facts {
		if fact.Meta[delegatedPartMeta] == delegatedPartTask {
			tasks++
		}
	}
	if tasks != 2 {
		t.Fatalf("task facts on the site chain = %d, want one per round", tasks)
	}

	requests := script.requestMessages()
	if len(requests) != 2 {
		t.Fatalf("model requests = %d, want the one request of each round", len(requests))
	}
	for at, messages := range requests {
		if !carriesSystemMessage(messages, instruction) {
			t.Fatalf("round %d request carries no system message with the instruction: %+v", at+1, messages)
		}
	}
}

// carriesSystemMessage reports whether one request carries the text as a system
// message.
func carriesSystemMessage(messages []model.Message, text string) bool {
	for _, message := range messages {
		if message.Role == model.RoleSystem && message.Content == text {
			return true
		}
	}
	return false
}

// delegatedBatchLedger is the commit face of one delegated batch. It lands each
// fact into the chain face the way the run's ledger does — one record per
// identity, and a second commit under an identity the ledger already names is
// the commitment it already is — and counts what it saw per delegated part, so a
// check tells a commit that landed from one that was answered as an idempotent
// replay.
type delegatedBatchLedger struct {
	mu      sync.Mutex
	history *invariantLoopHistory
	facts   map[event.EventID]event.Event
	seq     int
	seen    map[string]int
	land    map[string]int
}

// newDelegatedBatchLedger returns a ledger landing into the named chain face.
func newDelegatedBatchLedger(history *invariantLoopHistory) *delegatedBatchLedger {
	return &delegatedBatchLedger{
		history: history,
		facts:   map[event.EventID]event.Event{},
		seen:    map[string]int{},
		land:    map[string]int{},
	}
}

// commit keeps one committed fact under its identity: a key the ledger already
// holds is answered as the fact it already names and lands nothing.
func (l *delegatedBatchLedger) commit(evt event.Event) (event.EventID, error) {
	l.mu.Lock()
	if evt.Key == "" {
		l.seq++
		evt.Key = event.EventID(fmt.Sprintf("minted-instruction-%d", l.seq))
	}
	part := evt.Meta[delegatedPartMeta]
	l.seen[part]++
	_, held := l.facts[evt.Key]
	if !held {
		l.facts[evt.Key] = evt
		l.land[part]++
	}
	l.mu.Unlock()
	if !held {
		l.history.land(evt)
	}
	return evt.Key, nil
}

// attempted reports how many commits of one delegated part the ledger saw.
func (l *delegatedBatchLedger) attempted(part string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[part]
}

// landed reports how many facts of one delegated part the ledger kept.
func (l *delegatedBatchLedger) landed(part string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.land[part]
}

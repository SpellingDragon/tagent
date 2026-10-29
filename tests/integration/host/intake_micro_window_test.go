// The micro-window anchor of the intake: a message that arrives after the turn in
// flight ran its last decision boundary must be consumed by the next turn. The
// turn reads its chain again at every decision boundary, so a message that lands
// beside a range already past its last read is material no decision of that range
// will ever speak from; landing it there and answering "accepted" leaves it
// dangling — on the chain, read by nobody, answered by nobody.
//
// 契约: docs/wiki/agent/event-flow.md
package host

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// microWindowInputOne and the markers beside it spell the turn whose delivery the
// anchor parks: the turn already ran its decision, and it is still delivering its
// answer when the second message arrives.
const (
	microWindowInputOne  = "micro-window turn one input marker"
	microWindowAnswerOne = "micro-window turn one answer marker"
	microWindowInputTwo  = "micro-window message that arrives after the last decision"
	microWindowAnswerTwo = "micro-window answer of the turn that followed"
)

// microWindowProbe is how long the anchor waits for the arriving message to be
// answered while the turn in flight is still parked at its delivery. The turn
// holds the address gate until its drive returns, so the arriving call can only
// answer inside this window by being accepted into a range that already read its
// last boundary — the receipt this anchor refuses.
const microWindowProbe = 2 * time.Second

// TestTheMicroWindowMessageIsConsumedByTheNextTurn requires a message that arrives after the turn's last decision to be answered by a turn of its own.
//   - positive: the arriving message drives the next turn of the session, and that turn completes
//   - negative: the message commits once, under the turn that followed — never under the range that already read its last boundary
//   - negative: no request carries the message twice, so the ended turn's chain holds no second copy of it
//   - consumer: the formal assembly booted from a real document, driven through Host.Invoke beside a real sender
//
// 契约: docs/wiki/agent/event-flow.md
func TestTheMicroWindowMessageIsConsumedByTheNextTurn(t *testing.T) {
	reproductionEnv(t)
	provider := newScriptedProvider(t)
	script := newIntegrityScript()
	script.expect("model-entry", "the turn before the arrival answers its input", requestTail(microWindowInputOne)).
		answers(scriptedAnswerOf(microWindowAnswerOne)).
		delivers(microWindowAnswerOne)
	script.expect("model-entry", "the turn that follows answers the arriving message", requestTail(microWindowInputTwo)).
		answers(scriptedAnswerOf(microWindowAnswerTwo)).
		delivers(microWindowAnswerTwo)
	provider.decide = fidelityGate(script.decide)

	sender, gate := newGatedRecordingSender()
	host, _ := bootFidelityHost(t, provider, sender, t.TempDir(), "    max_tokens: 0\n")
	keepIntegrityHost(t, script, host)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-micro-window"}

	ended := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, microWindowInputOne)
		ended <- integrityOutcome{turn: turn, err: err}
	}()
	gate.wait(t, "the delivery of the turn that ran its last decision")

	arrived := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, microWindowInputTwo)
		arrived <- integrityOutcome{turn: turn, err: err}
	}()
	select {
	case out := <-arrived:
		if out.err != nil {
			t.Fatalf("the call of the arriving message = %v, want it answered by the turn that follows", out.err)
		}
		t.Fatalf("the arriving message was answered with (%v, %d decision(s)) while the turn in flight still held the gate: "+
			"a message that lands past the last decision boundary of that range is no input of it, so answering it here leaves it dangling",
			out.turn.Terminal, len(out.turn.Decisions))
	case <-time.After(microWindowProbe):
	}

	gate.releaseAll()
	first := awaitMicroWindowTurn(t, ended, "the turn that ran its last decision")
	if first.turn.Terminal != model.KindCompleted {
		t.Fatalf("the turn before the arrival = %v, want the completed turn", first.turn.Terminal)
	}
	second := awaitMicroWindowTurn(t, arrived, "the turn that follows the arrival")
	if second.err != nil {
		t.Fatalf("the turn that follows the arrival ended by error: %v", second.err)
	}
	if second.turn.Terminal != model.KindCompleted {
		t.Fatalf("the turn that follows the arrival = %v, want the completed turn", second.turn.Terminal)
	}
	if second.turn.InvocationID == first.turn.InvocationID {
		t.Fatalf("the turn that follows the arrival drives the range %q, the range of the turn that already ended: "+
			"the arriving message opened no range of its own", second.turn.InvocationID)
	}

	landed := microWindowFactsCarrying(host.Run().SessionFacts(addr), microWindowInputTwo)
	if len(landed) != 1 {
		t.Fatalf("facts carrying the arriving message = %d, want the one fact the turn that follows commits as its own input", len(landed))
	}
	if landed[0].InvocationID != second.turn.InvocationID {
		t.Fatalf("the fact carrying the arriving message belongs to the range %q, want the range that followed %q: "+
			"a fact landed under a range past its last boundary is material no decision of it reads",
			landed[0].InvocationID, second.turn.InvocationID)
	}

	requests := provider.servedRequests()
	endsTheFirstTurn := requestTail(microWindowInputOne)
	for _, request := range requests {
		if endsTheFirstTurn(request) && microWindowCopies(request, microWindowInputTwo) != 0 {
			t.Fatalf("the decision of the turn that ran its last boundary carried the arriving message: "+
				"that turn read it after it stopped reading, which is the dangling this anchor refuses (%d message(s))", len(request.Messages))
		}
	}
	answered := 0
	endsTheSecondTurn := requestTail(microWindowInputTwo)
	for _, request := range requests {
		if !endsTheSecondTurn(request) {
			continue
		}
		answered++
		if copies := microWindowCopies(request, microWindowInputTwo); copies != 1 {
			t.Fatalf("copies of the arriving message in the decision that answers it = %d, want exactly 1: "+
				"a second copy means the ended range's chain still presents it as ended material", copies)
		}
	}
	if answered != 1 {
		t.Fatalf("decisions that answer the arriving message = %d, want the one decision of the turn that follows", answered)
	}
	t.Logf("[micro-window] positive: the arriving message drove the turn %q and stands in its request once", second.turn.InvocationID)
}

// awaitMicroWindowTurn waits for one drive of the anchor to answer, so a check
// never reads an empty channel as an outcome.
func awaitMicroWindowTurn(t *testing.T, done <-chan integrityOutcome, what string) integrityOutcome {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(60 * time.Second):
		t.Fatalf("%s did not answer", what)
		return integrityOutcome{}
	}
}

// microWindowFactsCarrying reports the facts of one chain whose input carries the
// named content: the identity the message committed under, read from the chain
// instead of from the requests the model served.
func microWindowFactsCarrying(facts []event.Event, content string) []event.Event {
	var out []event.Event
	for _, fact := range facts {
		if fact.Message.Role == model.RoleUser && fact.Message.Content == content {
			out = append(out, fact)
		}
	}
	return out
}

// microWindowCopies reports how many messages of one request carry the named
// content, so a check reads every copy instead of the first one it meets.
func microWindowCopies(request scriptedRequest, content string) int {
	copies := 0
	for _, message := range request.Messages {
		if message.Content == content {
			copies++
		}
	}
	return copies
}

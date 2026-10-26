// J2 is the consumer anchor of the settle driver: a delegated result lands in
// the session chain after the turn that admitted the delegation already ended,
// so the result reaches the user only when the settle face drives the follow-up
// turn itself.
//
// The journey asserts on what the user received, in order: the first turn
// answers beside the admission, and the settled result arrives after the
// delegated turn settles, with no second user input.
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
package journey

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// TestJourneySettledResultReachesUser drives one user turn that delegates:
// - the caller answers with the admission receipt and the user receives that first answer
// - the delegated turn is held open, and no settled result is delivered while it still runs
// - releasing the delegated turn lets it settle, and the user receives the follow-up answer
// - the follow-up request reads the terminal notice, and that material carries the worker body
//
// 契约: docs/wiki/agent/agent-architecture.md#settle-driver
func TestJourneySettledResultReachesUser(t *testing.T) {
	const (
		firstAnswer  = "accepted: the worker is on it"
		workerResult = "the worker finished the artifact alpha"
		settledBody  = "the settled result reached you: the worker finished the artifact alpha"
		session      = event.SessionID("session-journey-delegate")
		userInput    = "please delegate the artifact"
	)
	gate := make(chan struct{})
	provider := newJourneyProvider(t)
	provider.script(journeyEntryModel, journeyReply{body: journeyToolCall("call-worker-1", "worker", `{"input":"produce the artifact alpha"}`)})
	provider.scriptAnswers(journeyEntryModel, firstAnswer)
	provider.scriptAnswers(journeyEntryModel, settledBody)
	provider.scriptGated(journeyWorkerModel, gate, workerResult)
	host := bootJourney(t, provider, journeySpec{Delegate: "worker", MaxTokens: 4096})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	host.invokeOK(t, ctx, string(session), userInput)

	if bodies := host.sender.bodies(); len(bodies) != 1 || bodies[0] != firstAnswer {
		t.Fatalf("delivered bodies after the calling turn = %q, want exactly the first answer %q", bodies, firstAnswer)
	}
	provider.waitForRequests(t, journeyWorkerModel, 1, 15*time.Second)
	if bodies := host.sender.bodies(); len(bodies) != 1 {
		t.Fatalf("delivered bodies while the worker is held = %q, want no settled result before the worker settles", bodies)
	}

	close(gate)
	host.sender.waitFor(t, settledBody, 15*time.Second)

	deliveries := host.sender.deliveries()
	if len(deliveries) != 2 {
		t.Fatalf("deliveries = %+v, want the first answer and then the settled result", deliveries)
	}
	if deliveries[0].Content != firstAnswer || deliveries[1].Content != settledBody {
		t.Fatalf("delivered bodies = %q, want [%q %q] in order", host.sender.bodies(), firstAnswer, settledBody)
	}
	wantTarget := event.Address{AgentID: journeyEntryAgent, SessionID: session}
	if deliveries[1].Target != wantTarget {
		t.Fatalf("settled delivery target = %+v, want the user session %+v", deliveries[1].Target, wantTarget)
	}

	requests := provider.waitForRequests(t, journeyEntryModel, 3, 15*time.Second)
	followUp := requests[2]
	messages := followUp.messages()
	if len(messages) == 0 {
		t.Fatal("the follow-up request carries no messages")
	}
	last := messages[len(messages)-1]
	if last.Role != string(model.RoleUser) || !strings.Contains(last.Content, "tagent: task") || !strings.Contains(last.Content, "settled") {
		t.Fatalf("follow-up request last message = %+v (request text %q), want the terminal settle notice as the turn's input", last, followUp.text())
	}
	if !strings.Contains(last.Content, workerResult) {
		t.Fatalf("follow-up request last message = %q, want the worker result body %q it settled on", last.Content, workerResult)
	}
	if strings.Contains(last.Content, userInput) {
		t.Fatalf("follow-up request last message = %q, want no second user input driving the turn", last.Content)
	}
}

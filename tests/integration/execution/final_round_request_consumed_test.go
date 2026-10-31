// 契约: docs/wiki/agent/event-flow.md#runner-flow
//
// The red anchors of the budget fact on the consumption side: the consumption
// loop runs the closing round the final_round.request standing on the chain of
// its segment names, so the judgment the producer files is the judgment the loop
// obeys — and a request naming another range closes nothing.
package execution

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// finalRoundConsumedSession, finalRoundOtherSession and finalRoundProducedSession
// name the sessions these anchors boot; finalRoundConsumedPrompt is the declared
// closing instruction, and the four *Body/*CallID constants are the bodies and the
// call identities the rounds carry.
const (
	finalRoundConsumedSession = "session-final-round-consumed"
	finalRoundOtherSession    = "session-final-round-other-range"
	finalRoundProducedSession = "session-final-round-produced"
	finalRoundConsumedPrompt  = "budget: close out with what this turn already produced"
	finalRoundConsumedOpening = "budget: the message that opens the turn"
	finalRoundWorkBody        = "budget: the call before the closing round finished"
	finalRoundRefusedBody     = "budget: the call the closing round issued"
	finalRoundWorkCallID      = "call-final-round-work"
	finalRoundRefusedCallID   = "call-final-round-refused"
)

// finalRoundRangeMeta, finalRoundReasonMeta, finalRoundRoundsMeta and
// finalRoundElapsedMeta are the metadata keys a final round request states its
// judgment under, as the durable chain spells them: the reader matches a request to
// the segment it closes by the range key, so an anchor that writes one writes these
// keys.
const (
	finalRoundRangeMeta   = "final_round_range"
	finalRoundReasonMeta  = "final_round_reason"
	finalRoundRoundsMeta  = "final_round_rounds"
	finalRoundElapsedMeta = "final_round_elapsed"
)

// finalRoundHostDocument renders the host these anchors boot: the entry agent
// keeps the declared send tool beside the closing round its document declares,
// so one real decision runs under the budget the document states.
func finalRoundHostDocument(root, endpoint, finalRound string) string {
	var b strings.Builder
	b.WriteString("entry: " + intakeEntryAgent + "\n")
	b.WriteString("provider: local\n")
	b.WriteString("model: " + intakeEntryModel + "\n")
	b.WriteString("providers:\n")
	b.WriteString("  local: { provider: openai, api_endpoint: \"" + endpoint + "\", api_key_env: TAGENT_TEST_API_KEY }\n")
	b.WriteString("agents:\n")
	b.WriteString("  " + intakeEntryAgent + ":\n")
	b.WriteString("    model: " + intakeEntryModel + "\n")
	b.WriteString("    tools:\n")
	b.WriteString("      - { kind: tool, id: send }\n")
	if finalRound != "" {
		b.WriteString("    final_round:\n")
		b.WriteString(finalRound)
	}
	b.WriteString("storage: { type: localfile, root: " + root + " }\n")
	return b.String()
}

// finalRoundFileRequest commits one final round request on the chain of one
// address through the public commit face of the run, so the request the
// consumption loop reads is the very fact a producer files: the judgment
// identity as the key, its range as the range of the fact, and the judgment
// fields in the metadata the chain spells them in.
func finalRoundFileRequest(t *testing.T, ctx context.Context, run *assembly.Run, addr event.Address, rangeID event.InvocationID) event.FinalRoundRequest {
	t.Helper()
	request := event.FinalRoundRequestFor(rangeID, string(agent.FinalRoundRounds), 1, time.Second)
	_, err := run.CommitToSession(ctx, addr, event.Event{
		Key:       request.ID,
		Type:      request.FactType(),
		Timestamp: time.Now(),
		Meta: map[string]string{
			finalRoundRangeMeta:   string(request.Range),
			finalRoundReasonMeta:  request.Reason,
			finalRoundRoundsMeta:  strconv.Itoa(request.Rounds),
			finalRoundElapsedMeta: request.Elapsed.String(),
		},
		Owner:        addr,
		InvocationID: request.Range,
	})
	if err != nil {
		t.Fatalf("commit the final round request of range %s: %v", rangeID, err)
	}
	return request
}

// finalRoundRequestsOf reports the final round request facts one chain holds, in
// commit order.
func finalRoundRequestsOf(facts []event.Event) []event.Event {
	out := make([]event.Event, 0, 1)
	for _, fact := range facts {
		if fact.Type == event.FinalRoundRequestType {
			out = append(out, fact)
		}
	}
	return out
}

// finalRoundLastUser reports the content of the last user message one request
// reads: the closing instruction stands there, past the material the segment
// accumulated, and a system block may follow it.
func finalRoundLastUser(request intakeRequest) string {
	last := ""
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) {
			last = message.Content
		}
	}
	return last
}

// finalRoundCopies counts the accepted bodies of one outbound face that carry
// the named body, so an anchor reads a delivery apart from the answer body the
// same face accepts when a turn ends.
func finalRoundCopies(bodies []string, body string) int {
	count := 0
	for _, accepted := range bodies {
		if accepted == body {
			count++
		}
	}
	return count
}

// TestConsumptionLoopRunsTheClosingRoundTheChainNames is the discriminating red anchor of the consumption side.
//   - no budget is declared, so no local judgment can close the round: the request on the chain is the whole reason it closes
//   - the judgment of the range the first segment mints stands on the chain before the turn opens, as a producer files it beside the segment
//   - positive: the first decision is the closing round the request names, so the turn reports its reason
//   - positive: the closing round reads the declared instruction as the last user message of its request
//   - negative: the call the closing round issues executes nowhere, because the closing round admits none
//   - negative: the loop files no judgment of its own, so the chain carries the one request it read
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestConsumptionLoopRunsTheClosingRoundTheChainNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newIntakeProvider(t)
	provider.answers = map[int]string{0: intakeToolCallAnswer("send", finalRoundRefusedCallID, `{"content":`+intakeQuoted(finalRoundRefusedBody)+`}`)}
	sender := newIntakeSender()
	host := intakeBootDocument(t, provider, sender, func(root, endpoint string) string {
		return finalRoundHostDocument(root, endpoint, "      prompt: \""+finalRoundConsumedPrompt+"\"\n")
	})
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: finalRoundConsumedSession}

	rangeID := event.InvocationID(finalRoundConsumedSession + "#c1")
	request := finalRoundFileRequest(t, ctx, host.Run(), addr, rangeID)

	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-final-round-consumed",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: finalRoundConsumedOpening}},
	})
	if err != nil {
		t.Fatalf("invoke beside a filed judgment: %v", err)
	}
	if turn.FinalRound != agent.FinalRoundRounds {
		t.Fatalf("final round reason = %q, want %q beside the request %s of range %s: "+
			"the loop runs the closing round the chain names (%s)",
			turn.FinalRound, agent.FinalRoundRounds, request.ID, rangeID, intakeDescribe(provider.requests()))
	}
	if len(turn.Decisions) != 1 {
		t.Fatalf("decisions = %d, want the one closing round the request names: "+
			"the closing round admits none of the calls it issues (%s)",
			len(turn.Decisions), intakeDescribe(provider.requests()))
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn the closing round answered", turn.Terminal)
	}
	requests := provider.requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want the one decision the closing round runs (%s)", len(requests), intakeDescribe(requests))
	}
	if last := finalRoundLastUser(requests[0]); last != finalRoundConsumedPrompt {
		t.Fatalf("the closing request reads %q as its last user message, want the declared instruction %q",
			last, finalRoundConsumedPrompt)
	}
	if bodies := sender.accepted(); len(bodies) != 0 {
		t.Fatalf("the outbound face accepted %q, want nothing: the call of the closing round is refused, never dispatched", bodies)
	}
	if facts := finalRoundRequestsOf(host.Run().SessionFacts(addr)); len(facts) != 1 || facts[0].Key != request.ID {
		t.Fatalf("the chain carries %d request(s), want the one the loop read and no judgment of its own", len(facts))
	}
	t.Logf("[final-round 5] consumed: the closing round %q ran on the request %s of range %s", turn.FinalRound, request.ID, rangeID)
}

// TestConsumptionLoopIgnoresARequestOfAnotherRange is the negative anchor of the same reading: a request of another range closes nothing.
//   - a request of a range this session never mints stands on the chain and closes nothing
//   - the request names a range the turn never mints: the segment it opens mints #c1, so this judgment closes nothing of it
//   - positive: the round that runs is the ordinary one, so the call it issues is dispatched and delivers
//   - negative: no decision of the turn is a closing round, whatever request the chain carries
//   - negative: no request reads the declared instruction, so the fact of another range never spoke
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestConsumptionLoopIgnoresARequestOfAnotherRange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newIntakeProvider(t)
	provider.answers = map[int]string{0: intakeToolCallAnswer("send", finalRoundWorkCallID, `{"content":`+intakeQuoted(finalRoundWorkBody)+`}`)}
	sender := newIntakeSender()
	host := intakeBootDocument(t, provider, sender, func(root, endpoint string) string {
		return finalRoundHostDocument(root, endpoint, "      prompt: \""+finalRoundConsumedPrompt+"\"\n")
	})
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: finalRoundOtherSession}

	finalRoundFileRequest(t, ctx, host.Run(), addr, event.InvocationID(finalRoundOtherSession+"#c9"))

	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-final-round-other-range",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: finalRoundConsumedOpening}},
	})
	if err != nil {
		t.Fatalf("invoke beside a request of another range: %v", err)
	}
	if turn.FinalRound != agent.FinalRoundNone {
		t.Fatalf("final round reason = %q, want none: a request of another range closes no round of this one (%s)",
			turn.FinalRound, intakeDescribe(provider.requests()))
	}
	if len(turn.Decisions) != 2 {
		t.Fatalf("decisions = %d, want the ordinary round beside the one that reads its result (%s)",
			len(turn.Decisions), intakeDescribe(provider.requests()))
	}
	requests := provider.requests()
	for _, request := range requests {
		if copies := intakeUserCopies(request, finalRoundConsumedPrompt); copies != 0 {
			t.Fatalf("request %d reads the declared instruction %d time(s), want none: the document declares no budget, "+
				"so the request of another range must not speak for this one (%s)",
				request.Index, copies, intakeDescribe(requests))
		}
	}
	if delivered := finalRoundCopies(sender.accepted(), finalRoundWorkBody); delivered != 1 {
		t.Fatalf("the outbound face accepted the ordinary round's own call %d time(s), want the one the dispatched call delivered "+
			"(%q): a request of another range closes no round, so the round runs as the policy declares",
			delivered, sender.accepted())
	}
	t.Logf("[final-round 6] negative: the request of #c9 closed nothing of #c1, so the round ran as the policy declares")
}

// TestConsumptionLoopReadsTheFiledBudgetRequest is the end-to-end anchor of the two halves.
//   - the producer judges the spent budget of the segment and files its request, and the next decision of that same segment runs the closing round it names
//   - positive: the turn reports the reason the filed request states, on the range the segment minted
//   - positive: the closing round reads the declared instruction as the last user message of the second decision
//   - positive: the chain carries exactly one request, and it names that range, that reason and the rounds it read
//   - negative: the call the closing round issued executes nowhere, while the round before it delivered
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestConsumptionLoopReadsTheFiledBudgetRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newIntakeProvider(t)
	provider.answers = map[int]string{
		0: intakeToolCallAnswer("send", finalRoundWorkCallID, `{"content":`+intakeQuoted(finalRoundWorkBody)+`}`),
		1: intakeToolCallAnswer("send", finalRoundRefusedCallID, `{"content":`+intakeQuoted(finalRoundRefusedBody)+`}`),
	}
	sender := newIntakeSender()
	host := intakeBootDocument(t, provider, sender, func(root, endpoint string) string {
		return finalRoundHostDocument(root, endpoint, "      max_rounds: 1\n      prompt: \""+finalRoundConsumedPrompt+"\"\n")
	})
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: finalRoundProducedSession}

	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-final-round-produced",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: finalRoundConsumedOpening}},
	})
	if err != nil {
		t.Fatalf("invoke under a declared round budget: %v", err)
	}
	if turn.FinalRound != agent.FinalRoundRounds {
		t.Fatalf("final round reason = %q, want %q: the loop runs the closing round the filed request names (%s)",
			turn.FinalRound, agent.FinalRoundRounds, intakeDescribe(provider.requests()))
	}
	if len(turn.Decisions) != 2 {
		t.Fatalf("decisions = %d, want the round that spent the budget beside the closing round it filed (%s)",
			len(turn.Decisions), intakeDescribe(provider.requests()))
	}
	requests := provider.requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want the two decisions the turn ran (%s)", len(requests), intakeDescribe(requests))
	}
	if last := finalRoundLastUser(requests[1]); last != finalRoundConsumedPrompt {
		t.Fatalf("the second decision reads %q as its last user message, want the declared instruction %q",
			last, finalRoundConsumedPrompt)
	}
	bodies := sender.accepted()
	if len(bodies) != 1 || bodies[0] != finalRoundWorkBody {
		t.Fatalf("the outbound face accepted %q, want the budgeted round's own call %q alone: "+
			"the closing round admits none of the calls it issues", bodies, finalRoundWorkBody)
	}
	facts := finalRoundRequestsOf(host.Run().SessionFacts(addr))
	if len(facts) != 1 {
		t.Fatalf("the chain carries %d request(s), want the one the spent budget filed", len(facts))
	}
	filing := facts[0]
	if string(filing.InvocationID) != string(turn.InvocationID) {
		t.Fatalf("the filed request names the range %q, want the segment %q the turn ran",
			filing.InvocationID, turn.InvocationID)
	}
	if reason := filing.Meta[finalRoundReasonMeta]; reason != string(agent.FinalRoundRounds) {
		t.Fatalf("the filed request names the reason %q, want %q", reason, agent.FinalRoundRounds)
	}
	if rounds := filing.Meta[finalRoundRoundsMeta]; rounds != "1" {
		t.Fatalf("the filed request read %q rounds, want the 1 the budget was spent at", rounds)
	}
	t.Logf("[final-round 7] produced and consumed: the request %s of range %s closed the round the producer judged",
		filing.Key, filing.InvocationID)
}

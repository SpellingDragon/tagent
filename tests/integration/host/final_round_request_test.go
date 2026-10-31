// The final round anchors: a spent budget is a fact of the chain, not a line the
// loop keeps to itself. A producer reads the segment one commit grew, judges the
// budget its own agent declares and files the final_round.request of the range
// whose budget is spent; the drive still runs the closing round it would have
// run, so the judgment is filed beside the behavior it names and never in place
// of it.
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
package host

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// finalRoundFactRange, finalRoundFactReason, finalRoundFactRounds and
// finalRoundFactElapsed are the metadata keys a final round request fact states its
// judgment under, as the durable chain spells them: a consumer reads the range a
// judgment names back from exactly these keys, so a green anchor here proves the
// fact a reader sees.
const (
	finalRoundFactRange   = "final_round_range"
	finalRoundFactReason  = "final_round_reason"
	finalRoundFactRounds  = "final_round_rounds"
	finalRoundFactElapsed = "final_round_elapsed"
)

// finalRoundCloseOut is the closing instruction the budgeted declarations of
// these anchors name.
const finalRoundCloseOut = "close out: deliver what this invocation already produced"

// finalRoundFact is one final round request fact as the chain states it.
type finalRoundFact struct {
	// Key is the identity the judgment is quoted by.
	Key event.EventID
	// Range is the range whose budget the judgment read.
	Range string
	// Reason names why the budget is spent.
	Reason string
	// Rounds is the tool round count the judgment read.
	Rounds string
	// Elapsed is the time the judgment read.
	Elapsed string
}

// finalRoundFactsOf reports the final round request facts one session chain
// holds, in commit order.
func finalRoundFactsOf(run *assembly.Run, session event.SessionID) []finalRoundFact {
	var out []finalRoundFact
	for _, evt := range run.SessionFacts(event.Address{SessionID: session}) {
		if evt.Type != event.FinalRoundRequestType {
			continue
		}
		out = append(out, finalRoundFact{
			Key:     evt.Key,
			Range:   evt.Meta[finalRoundFactRange],
			Reason:  evt.Meta[finalRoundFactReason],
			Rounds:  evt.Meta[finalRoundFactRounds],
			Elapsed: evt.Meta[finalRoundFactElapsed],
		})
	}
	return out
}

// finalRoundChainStub is the chain face one test hands the planner directly: one
// address beside the segment that address flies and the judgments the planner
// files on it.
type finalRoundChainStub struct {
	addr    event.Address
	segment agent.FinalRoundSegment
	carried bool
	filed   []event.FinalRoundRequest
}

// AddressOf reports the address of the one chain this stub stands for.
func (s *finalRoundChainStub) AddressOf(_ context.Context, _ event.EventID) (event.Address, bool) {
	return s.addr, s.carried
}

// SegmentOf reports the segment the stub was handed.
func (s *finalRoundChainStub) SegmentOf(_ context.Context, _ event.Address) (agent.FinalRoundSegment, bool) {
	return s.segment, s.carried
}

// FileFinalRoundRequest records one judgment the planner filed.
func (s *finalRoundChainStub) FileFinalRoundRequest(_ context.Context, _ event.Address, request event.FinalRoundRequest) error {
	s.filed = append(s.filed, request)
	return nil
}

// budgetedPolicy answers one fixed budget for every agent.
func budgetedPolicy(policy agent.FinalRoundPolicy) func(event.AgentID) agent.FinalRoundPolicy {
	return func(event.AgentID) agent.FinalRoundPolicy { return policy }
}

// TestFinalRoundPlannerJudgesTheSpentBudget checks the judgment the producer makes off the chain it reads:
//   - a round budget the chain has reached files the closing round of that range as "rounds"
//   - a time budget the segment has spent files it as "budget"
//   - an agent that declares no budget, a segment within both budgets and a chain beside no segment each file nothing
//   - a re-evaluation of the same range answers the same judgment identity, so no second request can land
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestFinalRoundPlannerJudgesTheSpentBudget(t *testing.T) {
	addr := event.Address{AgentID: "agent-a", SessionID: "session-a"}
	cases := []struct {
		name       string
		policy     agent.FinalRoundPolicy
		segment    agent.FinalRoundSegment
		carried    bool
		wantReason string
	}{
		{
			name:       "the round budget is spent",
			policy:     agent.FinalRoundPolicy{MaxRounds: 3},
			segment:    agent.FinalRoundSegment{Range: "inv-a", Rounds: 3, Started: time.Now()},
			carried:    true,
			wantReason: string(agent.FinalRoundRounds),
		},
		{
			name:       "the time budget is spent",
			policy:     agent.FinalRoundPolicy{Budget: time.Minute},
			segment:    agent.FinalRoundSegment{Range: "inv-a", Rounds: 0, Started: time.Now().Add(-time.Hour)},
			carried:    true,
			wantReason: string(agent.FinalRoundBudget),
		},
		{
			name:    "the agent declares no budget",
			policy:  agent.FinalRoundPolicy{},
			segment: agent.FinalRoundSegment{Range: "inv-a", Rounds: 9, Started: time.Now().Add(-time.Hour)},
			carried: true,
		},
		{
			name:    "the segment is within both budgets",
			policy:  agent.FinalRoundPolicy{MaxRounds: 5, Budget: time.Hour},
			segment: agent.FinalRoundSegment{Range: "inv-a", Rounds: 2, Started: time.Now()},
			carried: true,
		},
		{
			name:    "the chain flies no segment",
			policy:  agent.FinalRoundPolicy{MaxRounds: 1},
			carried: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &finalRoundChainStub{addr: addr, segment: tc.segment, carried: tc.carried}
			planner := agent.NewFinalRoundPlanner(stub, budgetedPolicy(tc.policy))
			if err := planner.ObserveCommit(context.Background(), "evt-call"); err != nil {
				t.Fatalf("judge the budget of %q: %v", tc.name, err)
			}
			want := 0
			if tc.wantReason != "" {
				want = 1
			}
			if len(stub.filed) != want {
				t.Fatalf("%q filed %d judgment(s), want %d", tc.name, len(stub.filed), want)
			}
			if want == 0 {
				return
			}
			request := stub.filed[0]
			if request.Reason != tc.wantReason {
				t.Fatalf("%q filed the reason %q, want %q", tc.name, request.Reason, tc.wantReason)
			}
			if request.Range != tc.segment.Range {
				t.Fatalf("%q named the range %q, want the segment's %q", tc.name, request.Range, tc.segment.Range)
			}
			if request.Rounds != tc.segment.Rounds {
				t.Fatalf("%q read %d rounds, want the %d the segment carries", tc.name, request.Rounds, tc.segment.Rounds)
			}
			if err := planner.ObserveCommit(context.Background(), "evt-call-again"); err != nil {
				t.Fatalf("re-judge the budget of %q: %v", tc.name, err)
			}
			if len(stub.filed) != 2 {
				t.Fatalf("%q re-judged into %d filings, want the judgment asked once more", tc.name, len(stub.filed))
			}
			if stub.filed[1].ID != request.ID {
				t.Fatalf("%q re-judged the range under the identity %s, want the same judgment %s", tc.name, stub.filed[1].ID, request.ID)
			}
		})
	}
	t.Logf("[final-round 1] judgment: %d case(s) judged off the chain material alone", len(cases))
}

// TestFinalRoundRequestLandsOnTheChainOfASpentBudget is the red anchor of the budget fact, committed through the public commit face of the assembled run:
//   - the first round of a two-round budget files nothing, the second files exactly one request
//   - that request names the range in flight beside the reason, the round count and the elapsed time it read
//   - a further commit of the same segment files no second request, whatever the measured inputs
//   - the segment the agent_output closes carries the next range's judgment, not the closed one's
//   - no projection entry carries a request, because a judgment about material is not material of its own
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestFinalRoundRequestLandsOnTheChainOfASpentBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the final round answer")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:         "agent-a",
			Model:      "model-a",
			FinalRound: config.FinalRoundSpec{MaxRounds: 2, TimeoutSeconds: 5, Prompt: finalRoundCloseOut},
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-final-round"}

	commitFinalRoundLeg(t, ctx, run, addr, "01-in", event.TypeExternalInput, "inv-segment-one", model.Message{Role: model.RoleUser, Content: "start"})
	commitFinalRoundLeg(t, ctx, run, addr, "02-call", event.TypeThinkingPlan, "inv-segment-one", entryCallTurn("call-1"))
	if facts := finalRoundFactsOf(run, addr.SessionID); len(facts) != 0 {
		t.Fatalf("the first of two rounds filed %d request(s), want none: the budget is not spent yet", len(facts))
	}
	commitFinalRoundLeg(t, ctx, run, addr, "03-call", event.TypeThinkingPlan, "inv-segment-one", entryCallTurn("call-2"))
	facts := finalRoundFactsOf(run, addr.SessionID)
	if len(facts) != 1 {
		t.Fatalf("the spent round budget filed %d request(s), want exactly one", len(facts))
	}
	request := facts[0]
	if request.Range != "inv-segment-one" {
		t.Fatalf("the request names the range %q, want the range in flight %q", request.Range, "inv-segment-one")
	}
	if request.Reason != string(agent.FinalRoundRounds) {
		t.Fatalf("the request names the reason %q, want %q", request.Reason, agent.FinalRoundRounds)
	}
	if request.Rounds != "2" {
		t.Fatalf("the request read %q rounds, want the 2 the budget was spent at", request.Rounds)
	}
	if request.Elapsed == "" {
		t.Fatalf("the request states no elapsed time, want the input the judgment read: %+v", request)
	}
	commitFinalRoundLeg(t, ctx, run, addr, "04-call", event.TypeThinkingPlan, "inv-segment-one", entryCallTurn("call-3"))
	if facts := finalRoundFactsOf(run, addr.SessionID); len(facts) != 1 {
		t.Fatalf("a further commit of the judged range filed %d request(s), want the one the range carries", len(facts))
	}
	commitFinalRoundLeg(t, ctx, run, addr, "05-out", event.TypeAgentOutput, "inv-segment-one", model.Message{Role: model.RoleAssistant, Content: "the first answer"})
	commitFinalRoundLeg(t, ctx, run, addr, "06-in", event.TypeExternalInput, "inv-segment-two", model.Message{Role: model.RoleUser, Content: "again"})
	if facts := finalRoundFactsOf(run, addr.SessionID); len(facts) != 1 {
		t.Fatalf("the segment after the close filed %d request(s), want the closed range's judgment alone", len(facts))
	}
	commitFinalRoundLeg(t, ctx, run, addr, "07-call", event.TypeThinkingPlan, "inv-segment-two", entryCallTurn("call-4"))
	commitFinalRoundLeg(t, ctx, run, addr, "08-call", event.TypeThinkingPlan, "inv-segment-two", entryCallTurn("call-5"))
	facts = finalRoundFactsOf(run, addr.SessionID)
	if len(facts) != 2 {
		t.Fatalf("the second segment's spent budget filed %d request(s) in total, want the two ranges' judgments", len(facts))
	}
	if facts[1].Range != "inv-segment-two" || facts[1].Rounds != "2" {
		t.Fatalf("the second request names (%q, %q rounds), want (%q, 2 rounds)", facts[1].Range, facts[1].Rounds, "inv-segment-two")
	}
	entries, held := run.ReplayEntries(addr.SessionID)
	if !held {
		t.Fatalf("session %q registered no projection", addr.SessionID)
	}
	judged := map[event.EventID]bool{facts[0].Key: true, facts[1].Key: true}
	for _, entry := range entries {
		if judged[entry.Key] {
			t.Fatalf("the projection carries the judgment %s: a judgment about material is not material of its own", entry.Key)
		}
	}
	t.Logf("[final-round 2] chain: %d request(s) filed off the committed facts; the range, the reason and the inputs each state where they were read", len(facts))
}

// TestFinalRoundRequestLandsOnTheDrivenRange is the consumer anchor: a drive under a declared budget still runs the closing round it judges.
//   - the drive closes on the declared round budget, exactly as it did with no producer mounted
//   - the chain of its session carries one final_round.request naming the invocation the drive ran
//   - the call of the closing round executes nowhere, so the fact names a round that admitted nothing
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestFinalRoundRequestLandsOnTheDrivenRange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tool := &entryTool{name: "budget_tool", output: "delivered"}
	provider := &entryModel{name: "model-a", answer: func(_ *model.Request) *model.Response {
		return entryToolTurn(entryCall("call-open", tool.name, `{"step":1}`))
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:         "agent-a",
			Model:      "model-a",
			Tools:      []config.ToolRef{{Name: tool.name}},
			FinalRound: config.FinalRoundSpec{MaxRounds: 1, TimeoutSeconds: 5, Prompt: finalRoundCloseOut},
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-driven-budget"}
	turn, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-driven-budget",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "start"}},
	})
	if err != nil {
		t.Fatalf("invoke under the declared budget: %v", err)
	}
	if turn.FinalRound != agent.FinalRoundRounds {
		t.Fatalf("final round reason = %q, want %q beside the filed judgment", turn.FinalRound, agent.FinalRoundRounds)
	}
	if len(turn.Decisions) != 2 {
		t.Fatalf("decisions = %d, want the tool round beside the closing round", len(turn.Decisions))
	}
	if got := tool.calls(); got != 1 {
		t.Fatalf("tool executions = %d, want the closing round's call admitted nowhere", got)
	}
	facts := finalRoundFactsOf(run, addr.SessionID)
	if len(facts) != 1 {
		t.Fatalf("the driven range filed %d request(s), want exactly one", len(facts))
	}
	if facts[0].Range != "inv-driven-budget" {
		t.Fatalf("the request names the range %q, want the driven %q", facts[0].Range, "inv-driven-budget")
	}
	if facts[0].Reason != string(agent.FinalRoundRounds) || facts[0].Rounds != "1" {
		t.Fatalf("the request states (%q, %q rounds), want (%q, 1 round)", facts[0].Reason, facts[0].Rounds, agent.FinalRoundRounds)
	}
	t.Logf("[final-round 3] driven range: the closing round stands on the chain as %s beside the turn the drive answered", facts[0].Key)
}

// TestFinalRoundRequestStaysUnfiledWithoutADeclaredBudget is the negative anchor of the producer beside no declared budget.
//   - the very chain that files a judgment under a declared budget files none here, and the producer reports no gap for it
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestFinalRoundRequestStaysUnfiledWithoutADeclaredBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the unbounded answer")
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-unbounded-budget"}
	commitFinalRoundLeg(t, ctx, run, addr, "01-in", event.TypeExternalInput, "inv-unbounded", model.Message{Role: model.RoleUser, Content: "start"})
	for round := 1; round <= 4; round++ {
		commitFinalRoundLeg(t, ctx, run, addr,
			fmt.Sprintf("%02d-call", round+1), event.TypeThinkingPlan, "inv-unbounded",
			entryCallTurn(fmt.Sprintf("call-%d", round)))
	}
	if facts := finalRoundFactsOf(run, addr.SessionID); len(facts) != 0 {
		t.Fatalf("a declaration naming no budget filed %d request(s), want none", len(facts))
	}
	for _, presentation := range run.Presentations() {
		for _, gap := range presentation.Gaps {
			if gap.Plugin == agent.FinalRoundPlannerName {
				t.Fatalf("the producer reported the gap %v on a commit it judges nothing of", gap.Err)
			}
		}
	}
	t.Logf("[final-round 4] negative: four rounds of an unbounded chain filed no request and no observation gap")
}

// commitFinalRoundLeg commits one fact of a chain under test through the public
// commit face of the assembled run, so the producer observes the very commit a
// host's commit is.
func commitFinalRoundLeg(t *testing.T, ctx context.Context, run *assembly.Run, addr event.Address, key string, typ string, inv event.InvocationID, msg model.Message) {
	t.Helper()
	if _, err := run.CommitToSession(ctx, addr, event.Event{
		Key:          event.EventID(key),
		Type:         typ,
		Timestamp:    time.Now(),
		InvocationID: inv,
		Message:      msg,
	}); err != nil {
		t.Fatalf("commit the %s fact %s of range %s: %v", typ, key, inv, err)
	}
}

// entryCallTurn builds the assistant message one tool round commits as its call
// fact.
func entryCallTurn(callID string) model.Message {
	return model.Message{
		Role:      model.RoleAssistant,
		ToolCalls: []model.ToolCall{entryCall(callID, "budget_tool", `{"step":1}`)},
	}
}

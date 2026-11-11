// 契约: docs/wiki/agent/execution-generations.md#lease-holds-reference
package assembly

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// restartSitesRun assembles one process over a durable root the way Boot does,
// so a second call on the same root is the restart the chain is read by.
func restartSitesRun(t *testing.T, ctx context.Context, root string) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	run, err := assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 4096}, {ID: "agent-b", Model: "model-a", MaxTokens: 4096}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, true,
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}))
	if err != nil {
		t.Fatalf("assemble the run over %s: %v", root, err)
	}
	return run
}

// activeAddresses lists the complete addresses a run answers as active, in the
// order the read answers them.
func activeAddresses(run *Run) []event.Address {
	out := []event.Address{}
	for _, addr := range run.ActiveSessions("") {
		if addr.IsComplete() {
			out = append(out, addr)
		}
	}
	return out
}

// unansweredKeys lists the fact identities one run reports as owed a drive.
func unansweredKeys(t *testing.T, ctx context.Context, run *Run) []event.EventID {
	t.Helper()
	read := run.Unsettled(ctx)
	if read.Unjudged != "" {
		t.Fatalf("the unsettled read answered unjudged: %s", read.Unjudged)
	}
	out := make([]event.EventID, 0, len(read.Inputs))
	for _, input := range read.Inputs {
		out = append(out, input.Event)
	}
	return out
}

// TestRestartRebuildsActiveSitesAndUnansweredRounds pins the sites and rounds a restart rebuilds.
// - the active addresses a restart answers are the addresses the chain holds
// - the rounds owed a drive are the same before and after the restart
// - a round a committed answer closed is never reported as owed (negative)
//
// 契约: docs/wiki/agent/execution-generations.md#lease-holds-reference
func TestRestartRebuildsActiveSitesAndUnansweredRounds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	answered := event.Address{AgentID: "agent-a", SessionID: "session-answered"}
	owed := event.Address{AgentID: "agent-a", SessionID: "session-owed"}

	first := restartSitesRun(t, ctx, root)
	commitWindowFact(t, ctx, first, answered, windowEvent(answered, "a1-in", "inv-answered", "the input an answer closed", event.TypeExternalInput))
	commitWindowFact(t, ctx, first, answered, windowEvent(answered, "a2-out", "inv-answered", "the answer that closed it", event.TypeAgentOutput))
	commitWindowFact(t, ctx, first, owed, windowEvent(owed, "b1-in", "inv-owed", "the input still owed a drive", event.TypeExternalInput))

	wantSites := activeAddresses(first)
	if len(wantSites) != 2 {
		t.Fatalf("the first process answers %d active addresses, want the 2 it established", len(wantSites))
	}
	wantOwed := unansweredKeys(t, ctx, first)
	if len(wantOwed) != 1 || wantOwed[0] != event.EventID("b1-in") {
		t.Fatalf("the first process owes %v, want the one unclosed input b1-in", wantOwed)
	}
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the first process: %v", err)
	}

	restarted := restartSitesRun(t, ctx, root)
	gotSites := activeAddresses(restarted)
	if len(gotSites) != len(wantSites) {
		t.Fatalf("the restarted run answers %d active addresses %v, want the %d the chain holds %v", len(gotSites), gotSites, len(wantSites), wantSites)
	}
	for at := range wantSites {
		if gotSites[at] != wantSites[at] {
			t.Fatalf("restarted address %d = %v, want %v", at, gotSites[at], wantSites[at])
		}
	}
	gotOwed := unansweredKeys(t, ctx, restarted)
	if len(gotOwed) != len(wantOwed) {
		t.Fatalf("the restarted run owes %v, want the %v the process before it owed", gotOwed, wantOwed)
	}
	for at := range wantOwed {
		if gotOwed[at] != wantOwed[at] {
			t.Fatalf("restarted owed round %d = %s, want %s", at, gotOwed[at], wantOwed[at])
		}
	}
	for _, key := range gotOwed {
		if key == event.EventID("a1-in") {
			t.Fatal("the restarted run reports the answered input a1-in as owed, want only the round no answer closed")
		}
	}
}

// TestUnansweredRoundsStayInsideTheirChain pins the scope of the owed read.
// - each session chain answers only its own owed round
// - a chain whose last fact is an answer answers none (negative)
// - the chain read and the resident window agree on an untrimmed chain (parity)
//
// 契约: docs/wiki/agent/execution-generations.md#lease-holds-reference
func TestUnansweredRoundsStayInsideTheirChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	closed := event.Address{AgentID: "agent-a", SessionID: "session-closed"}
	open := event.Address{AgentID: "agent-b", SessionID: "session-open"}

	first := restartSitesRun(t, ctx, root)
	commitWindowFact(t, ctx, first, closed, windowEvent(closed, "c1-in", "inv-closed", "the input an answer closed", event.TypeExternalInput))
	commitWindowFact(t, ctx, first, closed, windowEvent(closed, "c2-out", "inv-closed", "the answer that closed it", event.TypeAgentOutput))
	commitWindowFact(t, ctx, first, open, windowEvent(open, "d1-in", "inv-open", "the input still owed a drive", event.TypeExternalInput))
	if _, err := first.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(20 * time.Second)}); err != nil {
		t.Fatalf("close the first process: %v", err)
	}

	restarted := restartSitesRun(t, ctx, root)
	byAddress := map[event.Address][]event.EventID{}
	read := restarted.Unsettled(ctx)
	if read.Unjudged != "" {
		t.Fatalf("the unsettled read answered unjudged: %s", read.Unjudged)
	}
	for _, input := range read.Inputs {
		byAddress[input.Address] = append(byAddress[input.Address], input.Event)
	}
	if got := byAddress[closed]; len(got) != 0 {
		t.Fatalf("the chain whose last fact is an answer answers %v as owed, want none", got)
	}
	if got := byAddress[open]; len(got) != 1 || got[0] != event.EventID("d1-in") {
		t.Fatalf("the open chain answers %v as owed, want its own unclosed input d1-in", got)
	}
	if len(read.Inputs) != 1 {
		t.Fatalf("the restart owes %d rounds, want the one input no answer closed", len(read.Inputs))
	}
}

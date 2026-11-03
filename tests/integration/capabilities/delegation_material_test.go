package capabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// TestDelegatedMaterialReachesTheRequest is the positive anchor of the delegated materials.
//   - the site's chain holds one material fact per resolved source, under the site's projection key and with the source key as lineage
//   - the delegated request carries the source content as material beside the task, on the real assembly surface
//   - a reference the run holds nothing under lands no material fact and reaches no request: hallucinated references are silent (Q2)
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedMaterialReachesTheRequest(t *testing.T) {
	const brief = "the brief the reviewer must read"
	const task = "the change under review"
	const instruction = "review the change"

	t.Run("the material reaches the request", func(t *testing.T) {
		run, reviewer, site := driveDelegationWithMaterial(t, brief, task, nil)
		source := callerInputFact(t, run, brief)
		materials := delegatedMaterialFacts(run, site)
		if len(materials) != 1 {
			t.Fatalf("material facts on the site chain = %d, want the one projection of the resolved source", len(materials))
		}
		material := materials[0]
		if want := fmt.Sprintf("delegated-reviewer@%s-material-%s", site.SessionID, source.Key); string(material.Key) != want {
			t.Fatalf("material fact key = %q, want the site projection key %q", material.Key, want)
		}
		if got := material.Meta["delegated_source"]; got != string(source.Key) {
			t.Fatalf("material fact lineage = %q, want the source key %q", got, source.Key)
		}
		if material.Message.Content != brief {
			t.Fatalf("material fact body = %q, want the source content %q", material.Message.Content, brief)
		}
		requests := reviewer.recorded()
		if len(requests) != 1 {
			t.Fatalf("delegated requests = %d, want the one request of the round: %+v", len(requests), requests)
		}
		rendered := requestText(requests[0].Messages)
		if !strings.Contains(rendered, brief) {
			t.Fatalf("delegated request carries no material content %q: %s", brief, rendered)
		}
		if !strings.Contains(rendered, task) {
			t.Fatalf("delegated request carries no task %q: %s", task, rendered)
		}
		if !strings.Contains(rendered, instruction) {
			t.Fatalf("delegated request carries no instruction %q: %s", instruction, rendered)
		}
	})

	t.Run("hallucinated_ref_lands_nothing", func(t *testing.T) {
		run, reviewer, site := driveDelegationWithMaterial(t, brief, task, []event.EventID{"ev-hallucinated"})
		if materials := delegatedMaterialFacts(run, site); len(materials) != 0 {
			t.Fatalf("material facts on the site chain = %+v, want none for a reference this run holds nothing under", materials)
		}
		requests := reviewer.recorded()
		if len(requests) != 1 {
			t.Fatalf("delegated requests = %d, want the round to run beside an unbacked reference: %+v", len(requests), requests)
		}
		rendered := requestText(requests[0].Messages)
		if strings.Contains(rendered, "ev-hallucinated") {
			t.Fatalf("delegated request carries the bare identity of an unbacked reference: %s", rendered)
		}
		if !strings.Contains(rendered, task) {
			t.Fatalf("delegated request dropped the task beside an unbacked reference: %s", rendered)
		}
	})
}

// driveDelegationWithMaterial assembles the real runtime of a caller and one
// declared reviewer, drives the caller twice — once so its chain holds the brief a
// material reference names, once to issue the delegated call — and answers the
// reviewer's model requests it recorded beside the address of its execution site.
// The refs the call hands over are named by refs: nil names the brief's own
// committed identity — a fact the run really holds — while a non-nil list is
// handed over exactly as stated, so a case names an unbacked identity on purpose.
func driveDelegationWithMaterial(t *testing.T, brief, task string, refs []event.EventID) (*assembly.Run, *ntModel, event.Address) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	caller := &ntModel{name: "model-caller"}
	reviewer := &ntModel{name: "model-reviewer", answer: func(*model.Request) *model.Response {
		return ntFinalTurn("review recorded")
	}}
	rounds := 0
	handed := refs
	caller.answer = func(*model.Request) *model.Response {
		rounds++
		if rounds == 1 {
			return ntFinalTurn("the brief is recorded")
		}
		if rounds == 2 {
			return ntToolTurn(ntCall("call-review", subagent.ToolName, delegationMaterialArgs(t, task, handed)))
		}
		return ntFinalTurn("the delegation was admitted")
	}
	decl := &config.Declaration{
		Entry: "caller",
		Agents: []config.AgentSpec{
			{ID: "caller", Model: "model-caller", Tools: []config.ToolRef{{Name: subagent.ToolName}}},
			{ID: "reviewer", Model: "model-reviewer"},
		},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"caller":   {Model: caller},
		"reviewer": {Model: reviewer},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "caller", SessionID: "session-caller"}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-brief",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: brief}},
	}); err != nil {
		t.Fatalf("invoke the caller beside its brief: %v", err)
	}
	if handed == nil {
		handed = []event.EventID{callerInputFact(t, run, brief).Key}
	}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-delegate",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "delegate the review"}},
	}); err != nil {
		t.Fatalf("invoke the caller that delegates: %v", err)
	}
	site := event.Address{AgentID: "reviewer", SessionID: "session-caller#sub:call-review"}
	deadline := time.Now().Add(5 * time.Second)
	for len(reviewer.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(reviewer.recorded()) == 0 {
		t.Fatalf("the delegated round of %s never reached its model", site.SessionID)
	}
	return run, reviewer, site
}

// callerInputFact reports the input fact the caller's chain holds under one body.
func callerInputFact(t *testing.T, run *assembly.Run, body string) event.Event {
	t.Helper()
	for _, fact := range run.SessionFacts(event.Address{AgentID: "caller", SessionID: "session-caller"}) {
		if fact.Type == event.TypeExternalInput && fact.Message.Content == body && fact.Key != "" {
			return fact
		}
	}
	t.Fatalf("the caller's chain holds no input fact under %q", body)
	return event.Event{}
}

// delegatedMaterialFacts reports the material facts one execution site's chain
// committed, in commit order.
func delegatedMaterialFacts(run *assembly.Run, site event.Address) []event.Event {
	var materials []event.Event
	for _, fact := range run.SessionFacts(site) {
		if fact.Type == event.TypeExternalInput && fact.Meta["delegated_part"] == "material" {
			materials = append(materials, fact)
		}
	}
	return materials
}

// delegationMaterialArgs renders the call arguments of one inline delegated call:
// the child's declaration, the task and the handoff whose refs name the sources
// the round reads from.
func delegationMaterialArgs(t *testing.T, task string, refs []event.EventID) string {
	t.Helper()
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, string(ref))
	}
	args, err := json.Marshal(map[string]any{
		"name":        "reviewer",
		"description": "reviews one change",
		"prompt":      "review the change",
		"model":       "model-reviewer",
		"input":       task,
		"handoff": map[string]any{
			"intent":      "review the change",
			"constraints": []string{"every finding names a file"},
			"refs":        names,
		},
	})
	if err != nil {
		t.Fatalf("encode the delegated call: %v", err)
	}
	return string(args)
}

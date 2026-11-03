package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// TestDelegatedMaterialSurvivesRestart is the restart guard of the delegated materials.
//   - a material fact is the execution site's own record of the source it projects
//   - the site chain holds one material fact carrying the resolved source content
//   - a second assembly over the same storage holds the same material fact, key for key, with its source lineage intact
//   - a request driven at the site after the restart presents that content without the source chain or the delegating tool
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedMaterialSurvivesRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	storage := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()}
	const brief = "the brief the reviewer must read"
	const task = "the change under review"
	decl := &config.Declaration{
		Entry: "caller",
		Agents: []config.AgentSpec{
			{ID: "caller", Model: "model-caller", Tools: []config.ToolRef{{Name: subagent.ToolName}}},
			{ID: "reviewer", Model: "model-reviewer"},
		},
	}
	agents := func(caller, reviewer *entryModel) map[event.AgentID]assembly.NativeAgent {
		return map[event.AgentID]assembly.NativeAgent{
			"caller":   {Model: caller},
			"reviewer": {Model: reviewer},
		}
	}

	caller := &entryModel{name: "model-caller"}
	reviewer := &entryModel{name: "model-reviewer", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("review recorded")
	}}
	rounds := 0
	var handed []event.EventID
	caller.answer = func(*model.Request) *model.Response {
		rounds++
		if rounds == 1 {
			return entryFinalTurn("the brief is recorded")
		}
		if rounds == 2 {
			return entryToolTurn(entryCall("call-review", subagent.ToolName, delegationRestartArgs(t, task, handed)))
		}
		return entryFinalTurn("the delegation was admitted")
	}
	run, err := assembly.Assemble(ctx, decl, agents(caller, reviewer), assembly.WithGovernance(passPolicy{}), assembly.WithStorage(storage))
	if err != nil {
		t.Fatalf("assemble the durable runtime: %v", err)
	}
	callerAddr := event.Address{AgentID: "caller", SessionID: "session-caller"}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-brief",
		Address:      callerAddr,
		Input:        []model.Message{{Role: model.RoleUser, Content: brief}},
	}); err != nil {
		t.Fatalf("invoke the caller beside its brief: %v", err)
	}
	handed = []event.EventID{callerBriefFact(t, run, brief).Key}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-delegate",
		Address:      callerAddr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "delegate the review"}},
	}); err != nil {
		t.Fatalf("invoke the caller that delegates: %v", err)
	}
	site := event.Address{AgentID: "reviewer", SessionID: "session-caller#sub:call-review"}
	waitForSettle(t, "the delegated round to close on the site's chain", func() bool {
		for _, fact := range run.SessionFacts(site) {
			if fact.Type == event.TypeAgentOutput {
				return true
			}
		}
		return false
	})
	online := delegatedRestartMaterials(run, site)
	if len(online) != 1 || online[0].Message.Content != brief {
		t.Fatalf("material facts of the site before the restart = %+v, want the one fact carrying %q", online, brief)
	}

	restartedCaller := &entryModel{name: "model-caller", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the restarted caller decides alone")
	}}
	probe := &entryModel{name: "model-reviewer", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("the restarted review")
	}}
	restarted, err := assembly.Assemble(ctx, decl, agents(restartedCaller, probe), assembly.WithGovernance(passPolicy{}), assembly.WithStorage(storage))
	if err != nil {
		t.Fatalf("restart over the same storage: %v", err)
	}
	restored := delegatedRestartMaterials(restarted, site)
	if len(restored) != 1 {
		t.Fatalf("material facts of the site after the restart = %d, want the one the chain holds", len(restored))
	}
	if restored[0].Key != online[0].Key {
		t.Fatalf("material fact key after the restart = %q, want the key the chain recorded %q", restored[0].Key, online[0].Key)
	}
	if got := restored[0].Meta["delegated_source"]; got != string(handed[0]) {
		t.Fatalf("material fact lineage after the restart = %q, want the source key %q", got, handed[0])
	}
	if restored[0].Message.Content != brief {
		t.Fatalf("material fact body after the restart = %q, want the source content %q", restored[0].Message.Content, brief)
	}
	if _, err := restarted.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-site-probe",
		Address:      site,
		Input:        []model.Message{{Role: model.RoleUser, Content: "the restarted probe"}},
	}); err != nil {
		t.Fatalf("drive a request at the restarted site: %v", err)
	}
	requests := probe.recorded()
	if len(requests) == 0 {
		t.Fatal("the restarted site served no model request")
	}
	if !restartRequestCarries(requests[0], brief) {
		t.Fatalf("the restarted site request carries no material content %q: %+v", brief, requests[0].Messages)
	}
}

// delegatedRestartMaterials reports the material facts one site's chain holds, in
// commit order.
func delegatedRestartMaterials(run *assembly.Run, site event.Address) []event.Event {
	var materials []event.Event
	for _, fact := range run.SessionFacts(site) {
		if fact.Type == event.TypeExternalInput && fact.Meta["delegated_part"] == "material" {
			materials = append(materials, fact)
		}
	}
	return materials
}

// callerBriefFact reports the caller's input fact one body was committed under.
func callerBriefFact(t *testing.T, run *assembly.Run, body string) event.Event {
	t.Helper()
	for _, fact := range run.SessionFacts(event.Address{AgentID: "caller", SessionID: "session-caller"}) {
		if fact.Type == event.TypeExternalInput && fact.Message.Content == body && fact.Key != "" {
			return fact
		}
	}
	t.Fatalf("the caller's chain holds no input fact under %q", body)
	return event.Event{}
}

// restartRequestCarries reports whether one model request presents the text in any
// message.
func restartRequestCarries(request model.Request, text string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

// delegationRestartArgs renders the inline delegated call of the restart guard: the
// reviewer's declaration, its task and the handoff naming the source it reads from.
func delegationRestartArgs(t *testing.T, task string, refs []event.EventID) string {
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

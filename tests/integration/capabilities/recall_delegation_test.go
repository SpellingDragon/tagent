package capabilities

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	memoryembedder "github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/tool/recall"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestRecallDelegationConsumesBothStages runs both stages of a recall delegation over the real surface.
//   - the delegated turn's request carries the four recall tools and none of the caller's own
//   - the rule stage runs for real: the retrieval tool answers the landed hits of the corpus
//   - the model stage consumes that answer: the second request of the turn carries the retrieved keys
//   - the declared event params travel as the input refs the delegated input fact commits, and an unbacked reference renders nothing (Q2)
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestRecallDelegationConsumesBothStages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	surface := newRecallSurface(t, memoryembedder.NewHashEmbedder(0))
	tools := make([]trpctool.CallableTool, 0, len(surface.tools))
	for _, name := range []string{recall.QueryToolName, recall.GetToolName, recall.RecentToolName, recall.TraceToolName} {
		tools = append(tools, surface.tools[name])
	}

	probe := &delegationProbeTool{name: "caller_probe"}
	settled := make(chan struct{})
	recallRounds := 0
	delegated := &ntModel{name: "model-recall", answer: func(req *model.Request) *model.Response {
		recallRounds++
		if recallRounds == 1 {
			return ntToolTurn(ntCall("call-query", recall.QueryToolName, fmt.Sprintf(`{"text":%q,"limit":5}`, realRecallText)))
		}
		close(settled)
		return ntFinalTurn("the migration decision is recorded under 0a01 through 0a03")
	}}
	callerRounds := 0
	caller := &ntModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
		callerRounds++
		if callerRounds == 1 {
			return ntToolTurn(ntCall("call-recall", "recall",
				`{"input":"what did we decide about the migration?","event_keys":["0a01"]}`))
		}
		return ntFinalTurn("the recall turn was admitted")
	}}
	decl := &config.Declaration{
		Entry: "caller",
		Agents: []config.AgentSpec{
			{
				ID:    "caller",
				Model: "model-caller",
				Tools: []config.ToolRef{{
					Name: "recall",
					Params: map[string]string{
						subagent.ParamAgent:       "recall",
						subagent.ParamEventParams: "event_keys",
					},
				}},
			},
			{
				ID:    "recall",
				Model: "model-recall",
				Tools: []config.ToolRef{
					{Name: recall.QueryToolName},
					{Name: recall.GetToolName},
					{Name: recall.RecentToolName},
					{Name: recall.TraceToolName},
				},
			},
		},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"caller": {Model: caller, Tools: []trpctool.CallableTool{probe}},
		"recall": {Model: delegated, Tools: tools},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	addr := event.Address{AgentID: "caller", SessionID: "session-recall-delegation"}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-recall-delegation",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "what did we decide about the migration?"}},
	}); err != nil {
		t.Fatalf("invoke the caller: %v", err)
	}
	select {
	case <-settled:
	case <-time.After(10 * time.Second):
		t.Fatal("the delegated recall turn never reached its second decision")
	}

	requests := delegated.recorded()
	if len(requests) != 2 {
		t.Fatalf("delegated requests = %d, want the retrieval decision beside the answer it feeds", len(requests))
	}
	for _, name := range []string{recall.QueryToolName, recall.GetToolName, recall.RecentToolName, recall.TraceToolName} {
		if requests[0].Tools[name] == nil {
			t.Fatalf("the delegated request carries no %q, want the recall agent's own four-tool surface: %v", name, requests[0].Tools)
		}
	}
	if requests[0].Tools[probe.name] != nil {
		t.Fatalf("the delegated request carries the caller's %q tool: the surface is the target's own declaration, never the caller's assembled set", probe.name)
	}

	rule := findToolResult(requests[1].Messages, "call-query")
	if rule == nil {
		t.Fatalf("the second delegated request carries no retrieval result, so the model stage consumed nothing: %+v", requests[1].Messages)
	}
	for _, key := range []string{"0a01", "0a02", "0a03"} {
		if !strings.Contains(rule.Content, key) {
			t.Fatalf("the retrieval result carries no %s: %s", key, rule.Content)
		}
	}

	site := event.Address{AgentID: "recall", SessionID: "session-recall-delegation#sub:call-recall"}
	var task *event.Event
	materials := 0
	for _, fact := range run.SessionFacts(site) {
		if fact.Type != event.TypeExternalInput || fact.Meta[event.MetaKeyTriggerSource] != event.LineageDelegation {
			continue
		}
		switch fact.Meta["delegated_part"] {
		case "task":
			named := fact
			task = &named
		case "material":
			materials++
			if got := fact.Meta["delegated_source"]; got == "0a01" {
				t.Fatalf("the unbacked reference 0a01 projected a material fact: %+v", fact)
			}
		}
	}
	if task == nil {
		t.Fatalf("the delegated site %s/%s committed no task fact that closes its batch", site.AgentID, site.SessionID)
	}
	if task.Message.Content != "what did we decide about the migration?" {
		t.Fatalf("the delegated task fact body = %q, want the request the caller stated", task.Message.Content)
	}
	if materials != 0 {
		t.Fatalf("material facts of the delegated round = %d, want none for a reference this run does not hold", materials)
	}
	rendered := requestText(requests[0].Messages)
	if strings.Contains(rendered, "[handoff envelope]") {
		t.Fatalf("the delegated turn carries the dissolved envelope block:\n%s", rendered)
	}
	if !strings.Contains(rendered, "what did we decide about the migration?") {
		t.Fatalf("the delegated turn carries no request of the caller:\n%s", rendered)
	}
	if strings.Contains(rendered, "0a01") {
		t.Fatalf("the delegated turn carries the bare identity of an unbacked reference:\n%s", rendered)
	}
	if got := probe.calls(); got != 0 {
		t.Fatalf("the caller's probe tool ran %d times, want none: the delegated turn runs on the target's surface alone", got)
	}
}

// delegationProbeTool is a native tool of the caller that counts its own
// executions, so a check names the tool a delegated turn must never see.
type delegationProbeTool struct {
	name string

	mu   sync.Mutex
	runs int
}

// Declaration returns the probe's declaration under its own name.
func (p *delegationProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: p.name}
}

// Call counts one execution of the probe.
func (p *delegationProbeTool) Call(context.Context, []byte) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runs++
	return "probe", nil
}

// calls reports how often the probe executed.
func (p *delegationProbeTool) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runs
}

// findToolResult reports the tool result the transcript carries under one call
// identity, or nil when it carries none.
func findToolResult(messages []model.Message, callID string) *model.Message {
	for i := range messages {
		if messages[i].ToolID == callID && messages[i].Role == model.RoleTool {
			return &messages[i]
		}
	}
	return nil
}

// requestText renders every message of one delegated request, so a presence
// check reads material that arrives as its own message beside the task.
func requestText(messages []model.Message) string {
	var out strings.Builder
	for _, message := range messages {
		out.WriteString(message.Content)
		out.WriteString("\n")
	}
	return out.String()
}

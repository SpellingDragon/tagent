package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestAgentReferenceDelegatesToTheDeclaredTarget turns one agent reference into the target it declares:
// - the caller's request carries a tool of the declared target's own name, described by the file the reference names
// - the delegated turn runs on the target's own tool surface, never on the caller's assembled set
// - the values of the declared event params travel as the handoff refs of that turn
// - a rewritten description file reaches the next request without a reload
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestAgentReferenceDelegatesToTheDeclaredTarget(t *testing.T) {
	t.Run("the target runs its own surface beside the caller's", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir := t.TempDir()
		descriptionFile := writeAgentDescriptionFile(t, dir, "the workspace research turn")

		skillTool := &entryTool{name: "skill_search", output: "the deploy runbook lives at skills/deploy"}
		settled := make(chan struct{})
		delegatedRounds := 0
		knowledge := &entryModel{name: "model-knowledge", answer: func(req *model.Request) *model.Response {
			delegatedRounds++
			if delegatedRounds == 1 {
				return entryToolTurn(entryCall("call-skill", "skill_search", `{"query":"deploy"}`))
			}
			close(settled)
			return entryFinalTurn("skill_search found the deploy runbook")
		}}
		callerRounds := 0
		caller := &entryModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
			callerRounds++
			if callerRounds == 1 {
				return entryToolTurn(entryCall("call-knowledge", "knowledge",
					`{"input":"find the deploy procedure","event_keys":["ev-knowledge-1"]}`))
			}
			return entryFinalTurn("the knowledge turn was admitted")
		}}
		decl := &config.Declaration{
			Entry: "caller",
			Agents: []config.AgentSpec{
				{
					ID:    "caller",
					Model: "model-caller",
					Tools: []config.ToolRef{{
						Name: "knowledge",
						Params: map[string]string{
							subagent.ParamAgent:           "knowledge",
							subagent.ParamDescriptionFile: descriptionFile,
							subagent.ParamEventParams:     "event_keys",
						},
					}},
				},
				{ID: "knowledge", Model: "model-knowledge", Tools: []config.ToolRef{{Name: "skill_search"}}},
			},
		}
		run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"caller":    {Model: caller, Tools: []trpctool.CallableTool{&entryTool{name: "send", output: "sent"}}},
			"knowledge": {Model: knowledge, Tools: []trpctool.CallableTool{skillTool}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}

		addr := event.Address{AgentID: "caller", SessionID: "session-agent-reference"}
		invokeTurn(t, run, "inv-agent-reference", addr, "find the deploy procedure")

		select {
		case <-settled:
		case <-time.After(10 * time.Second):
			t.Fatal("the delegated turn of the declared target never reached its second decision")
		}

		callerRequests := caller.recorded()
		if len(callerRequests) != 2 {
			t.Fatalf("caller requests = %d, want the tool decision beside the decision after it", len(callerRequests))
		}
		tool := callerRequests[0].Tools["knowledge"]
		if tool == nil {
			t.Fatalf("the caller's request carries no tool named after the declared target: %v", callerRequests[0].Tools)
		}
		if got := tool.Declaration().Description; !strings.Contains(got, "the workspace research turn") {
			t.Fatalf("the target tool's description = %q, want the text of the description file the reference names", got)
		}
		receipt := findToolMessage(callerRequests[1].Messages, "call-knowledge")
		if receipt == nil || !strings.Contains(receipt.Content, "accepted task-") {
			t.Fatalf("the second caller request carries no admission receipt: %+v", callerRequests[1].Messages)
		}

		knowledgeRequests := knowledge.recorded()
		if len(knowledgeRequests) != 2 {
			t.Fatalf("delegated requests = %d, want the tool decision beside the answer", len(knowledgeRequests))
		}
		if knowledgeRequests[0].Tools["skill_search"] == nil {
			t.Fatalf("the delegated turn's request carries no skill_search: %v", knowledgeRequests[0].Tools)
		}
		if knowledgeRequests[0].Tools["send"] != nil {
			t.Fatalf("the delegated turn's request carries the caller's send tool: the surface must be the target's own, not the caller's assembled set")
		}
		if got := knowledgeRequests[0].Tools["knowledge"]; got != nil {
			t.Fatalf("the delegated turn's request carries the caller's own target tool")
		}
		first := handoffBlockOf(t, knowledgeRequests[0].Messages)
		if !strings.Contains(first, "refs:\n- ev-knowledge-1\n") {
			t.Fatalf("the delegated turn's request carries no declared event reference in its handoff block:\n%s", first)
		}
		if !strings.Contains(first, "find the deploy procedure") {
			t.Fatalf("the delegated turn's request carries no request of the caller:\n%s", first)
		}
		if got := skillTool.calls(); got != 1 {
			t.Fatalf("skill_search calls = %d, want the one the delegated turn issued", got)
		}
	})

	t.Run("a rewritten description file reaches the next request", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir := t.TempDir()
		descriptionFile := writeAgentDescriptionFile(t, dir, "the old purpose")
		var descriptions []string
		caller := &entryModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
			tool := req.Tools["knowledge"]
			if tool == nil {
				t.Errorf("the caller's request carries no tool named after the declared target")
				return entryFinalTurn("answered")
			}
			descriptions = append(descriptions, tool.Declaration().Description)
			return entryFinalTurn("answered")
		}}
		decl := &config.Declaration{
			Entry: "caller",
			Agents: []config.AgentSpec{
				{
					ID:    "caller",
					Model: "model-caller",
					Tools: []config.ToolRef{{
						Name:   "knowledge",
						Params: map[string]string{subagent.ParamAgent: "knowledge", subagent.ParamDescriptionFile: descriptionFile},
					}},
				},
				{ID: "knowledge", Model: "model-knowledge"},
			},
		}
		run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"caller":    {Model: caller},
			"knowledge": {Model: &entryModel{name: "model-knowledge", answer: func(*model.Request) *model.Response { return entryFinalTurn("") }}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		addr := event.Address{AgentID: "caller", SessionID: "session-description-reload"}
		invokeTurn(t, run, "inv-description-1", addr, "first")

		writeAgentDescriptionFile(t, dir, "the new purpose")
		invokeTurn(t, run, "inv-description-2", addr, "second")

		requests := caller.recorded()
		if len(requests) != 2 {
			t.Fatalf("caller requests = %d, want one per invocation", len(requests))
		}
		if len(descriptions) != 2 {
			t.Fatalf("declaration reads = %d, want one per request", len(descriptions))
		}
		first, second := descriptions[0], descriptions[1]
		if !strings.Contains(first, "the old purpose") {
			t.Fatalf("first request description = %q, want the file's text before the rewrite", first)
		}
		if !strings.Contains(second, "the new purpose") {
			t.Fatalf("second request description = %q, want the rewritten file's text without a reload", second)
		}
	})
}

// TestAgentReferenceRefusesAnUndeclaredTarget refuses a reference to an agent the declaration never names:
// - the declaration is the authorization, so the assembly stops
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestAgentReferenceRefusesAnUndeclaredTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	decl := &config.Declaration{
		Entry: "caller",
		Agents: []config.AgentSpec{{
			ID:    "caller",
			Model: "model-caller",
			Tools: []config.ToolRef{{Name: "ghost", Params: map[string]string{subagent.ParamAgent: "ghost"}}},
		}},
	}
	_, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"caller": {Model: &entryModel{name: "model-caller", answer: func(*model.Request) *model.Response { return entryFinalTurn("") }}},
	}, tagent.WithGovernance(passPolicy{}))
	if err == nil {
		t.Fatal("the assembly admitted an agent reference to an agent the declaration does not declare")
	}
	if !strings.Contains(err.Error(), "the agent reference names no declared agent") {
		t.Fatalf("assembly error = %v, want the named refusal that the reference names no declared agent", err)
	}
}

// writeAgentDescriptionFile writes the description file of the target under one
// fixed name in the given directory and returns its path, so the reference names
// a stable file a check can rewrite.
func writeAgentDescriptionFile(t *testing.T, dir, text string) string {
	t.Helper()
	path := filepath.Join(dir, "target.md")
	if err := os.WriteFile(path, []byte(text+"\n"), 0o600); err != nil {
		t.Fatalf("write the description file %s: %v", path, err)
	}
	return path
}

// handoffBlockOf returns the external-context block the delegated turn's
// request carries, so a check reads the envelope the call handed over instead
// of the framework's own prompt messages.
func handoffBlockOf(t *testing.T, messages []model.Message) string {
	t.Helper()
	for _, message := range messages {
		if strings.Contains(message.Content, "[handoff envelope]") {
			return message.Content
		}
	}
	t.Fatalf("the request carries no handoff block: %+v", messages)
	return ""
}

// findToolMessage reports the tool result the transcript carries under one call
// identity, or nil when it carries none.
func findToolMessage(messages []model.Message, callID string) *model.Message {
	for i := range messages {
		if messages[i].ToolID == callID && messages[i].Role == model.RoleTool {
			return &messages[i]
		}
	}
	return nil
}

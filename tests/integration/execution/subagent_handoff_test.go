package execution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestSubagentHandoffEnvelope drives one call_subagent call carrying a structured handoff:
// - the delegated turn reads the envelope block in front of its natural-language request, field by field
// - the grants of that block are the tool references the assembled sub-agent declares
// - the natural-language request and the assembled instruction still reach the delegated turn beside the block
// - a handoff naming no intent refuses the call beside no task and runs no delegated turn
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestSubagentHandoffEnvelope(t *testing.T) {
	t.Run("the declared handoff reaches the delegated turn as structured context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer"}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, handoffBuilder(delegated, "exec_run"))
		call := `{"name":"reviewer","description":"reviews one change","prompt":"review the change",` +
			`"model":"model-reviewer","tools":["exec_run"],"input":"the change under review",` +
			`"handoff":{"intent":"review the change","constraints":["every finding names a file"],` +
			`"pending_decisions":["whether the change ships"],"refs":["ev-context-1"]}}`
		caller := &convModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-review", subagent.ToolName, call))
			}
			return convFinalTurn("the delegation was admitted")
		}}
		rig.start(t, caller, []trpctool.Tool{tool}, issuer)

		rig.drive(t, ctx, issuer, "review the change")

		if got := delegated.count(); got != 1 {
			t.Fatalf("delegated turns = %d, want exactly the one assembled turn", got)
		}
		request, ok := delegated.lastRequest()
		if !ok {
			t.Fatal("the delegated turn recorded no model request")
		}
		content := lastMessageContent(request)
		block := strings.Index(content, "[handoff envelope]")
		natural := strings.Index(content, "the change under review")
		if block < 0 {
			t.Fatalf("delegated request carries no handoff block: %q", content)
		}
		if natural < 0 || block > natural {
			t.Fatalf("handoff block sits at %d and the request at %d, want the block in front of the request: %q", block, natural, content)
		}
		for _, want := range []string{
			"intent: review the change",
			"- every finding names a file",
			"- tool:exec_run",
			"pending_decisions:",
			"- whether the change ships",
			"- ev-context-1",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("delegated request carries no %q of the handoff: %q", want, content)
			}
		}
		if !carriesInstruction(request, "review the change") {
			t.Fatalf("delegated request carries no assembled instruction: %+v", request.Messages)
		}
		if got := rig.ledger.View(rig.invocationID); len(got) != 1 || got[0].Status != task.StatusSettled {
			t.Fatalf("delegation view = %+v, want the call landed at its terminal", got)
		}
	})

	t.Run("a handoff naming no intent refuses the call beside no task", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer"}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, handoffBuilder(delegated))
		call := `{"name":"reviewer","description":"reviews one change","prompt":"review the change",` +
			`"model":"model-reviewer","input":"the change under review",` +
			`"handoff":{"constraints":["every finding names a file"]}}`
		caller := &convModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-review", subagent.ToolName, call))
			}
			return convFinalTurn("the refusal was read")
		}}
		rig.start(t, caller, []trpctool.Tool{tool}, issuer)

		rig.drive(t, ctx, issuer, "review the change")

		requests := caller.recorded()
		if len(requests) != 2 {
			t.Fatalf("caller requests = %d, want one request per decision", len(requests))
		}
		refusal := findMessage(requests[1].Messages, "call-review", model.RoleTool)
		if refusal == nil {
			t.Fatalf("second request carries no refusal result: %+v", requests[1].Messages)
		}
		for _, want := range []string{"call_subagent_denied", agent.ErrHandoffIntentMissing.Error(), "no task was admitted"} {
			if !strings.Contains(refusal.Content, want) {
				t.Fatalf("refusal result carries no %q: %q", want, refusal.Content)
			}
		}
		if got := rig.ledger.View(rig.invocationID); len(got) != 0 {
			t.Fatalf("delegation view = %+v, want no task from a handoff naming no intent", got)
		}
		if got := delegated.count(); got != 0 {
			t.Fatalf("delegated turns = %d, want none from a refused handoff", got)
		}
	})
}

// handoffBuilder resolves the delegated model beside the tool references the assembled
// sub-agent declares, so the grants of one envelope can be read from that assembly.
func handoffBuilder(provider trpcmodel.Model, tools ...string) subagent.Builder {
	return subagent.Builder{
		Model: func(_ context.Context, name string) (trpcmodel.Model, error) {
			if name != provider.Info().Name {
				return nil, fmt.Errorf("model reference %q names no scripted model", name)
			}
			return provider, nil
		},
		Tools: func(_ context.Context, names []string) ([]trpctool.Tool, error) {
			resolved := make([]trpctool.Tool, 0, len(names))
			for _, name := range names {
				resolved = append(resolved, handoffTool{name: name})
			}
			return resolved, nil
		},
	}
}

// handoffTool is the tool stand-in the delegated assembly declares.
type handoffTool struct {
	name string
}

// Declaration returns the declaration naming the stand-in tool.
func (h handoffTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: h.name}
}

// lastRequest reports the last request the delegated model was driven with.
func (m *heldServeModel) lastRequest() (trpcmodel.Request, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return trpcmodel.Request{}, false
	}
	return m.requests[len(m.requests)-1], true
}

// lastMessageContent reports the content of the last message one request carries.
func lastMessageContent(req trpcmodel.Request) string {
	if len(req.Messages) == 0 {
		return ""
	}
	return req.Messages[len(req.Messages)-1].Content
}

// carriesInstruction reports whether one request carries the instruction as a system message.
func carriesInstruction(req trpcmodel.Request, instruction string) bool {
	for _, message := range req.Messages {
		if message.Role == trpcmodel.RoleSystem && strings.Contains(message.Content, instruction) {
			return true
		}
	}
	return false
}

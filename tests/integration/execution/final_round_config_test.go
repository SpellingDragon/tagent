package execution

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	// budgetToolName is the tool the declaration-driven closing-round cases call.
	budgetToolName = "budget_tool"
	// budgetToolOutput is what that tool answers.
	budgetToolOutput = "delivered"
	// budgetPrompt is the closing instruction the declaration names, so the
	// closing request is read by the instruction it carries.
	budgetPrompt = "close out: deliver what this invocation already produced"
)

// TestDeclarationDrivesTheClosingRound checks the watchdog budget on the formal assembly entry:
// - a round budget the declaration names makes the decision after it the closing round, read from the turn
// - that round carries the instruction the declaration names at the tail of its request
// - the call that round issues executes nowhere, while the call of the round before it runs once
// - a declaration naming no budget leaves the drive unbounded, so it ends on its own terminal
// - a budget named as a count of seconds reaches the drive as the time budget it declares
// - two agents close on the round budgets their own declarations name, each with its own instruction
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestDeclarationDrivesTheClosingRound(t *testing.T) {
	t.Run("a declared round budget closes the next decision", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tool := &convTool{name: budgetToolName, output: budgetToolOutput}
		provider := &convModel{name: "model-a", answer: alwaysToolTurn(tool)}
		decl := &config.Declaration{
			Entry: "agent-a",
			Agents: []config.AgentSpec{{
				ID:         "agent-a",
				Model:      "model-a",
				Tools:      []config.ToolRef{{Name: budgetToolName}},
				FinalRound: config.FinalRoundSpec{MaxRounds: 1, TimeoutSeconds: 5, Prompt: budgetPrompt},
			}},
		}
		run := assembleBudgeted(t, ctx, decl, provider, tool)

		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-budget",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-budget"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "start"}},
		})
		if err != nil {
			t.Fatalf("invoke under the declared budget: %v", err)
		}
		if turn.FinalRound != agent.FinalRoundRounds {
			t.Fatalf("final round reason = %q, want %q", turn.FinalRound, agent.FinalRoundRounds)
		}
		if len(turn.Decisions) != 2 || turn.Terminal != model.KindCompleted {
			t.Fatalf("turn = (%v, %d decisions), want the tool round beside the closing round", turn.Terminal, len(turn.Decisions))
		}
		if got := closingTail(t, turn); got != budgetPrompt {
			t.Fatalf("closing request ends on %q, want the instruction the declaration names", got)
		}
		if got := tool.calls(); got != 1 {
			t.Fatalf("tool executions = %d, want the closing round's call admitted nowhere", got)
		}
	})

	t.Run("each agent closes on the round budget its own declaration names", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tool := &convTool{name: budgetToolName, output: budgetToolOutput}
		modelA := &convModel{name: "model-a", answer: alwaysToolTurn(tool)}
		modelB := &convModel{name: "model-b", answer: alwaysToolTurn(tool)}
		decl := &config.Declaration{
			Entry: "agent-a",
			Agents: []config.AgentSpec{
				{
					ID:         "agent-a",
					Model:      "model-a",
					Tools:      []config.ToolRef{{Name: budgetToolName}},
					FinalRound: config.FinalRoundSpec{MaxRounds: 2, TimeoutSeconds: 5, Prompt: budgetPrompt},
				},
				{
					ID:         "agent-b",
					Model:      "model-b",
					Tools:      []config.ToolRef{{Name: budgetToolName}},
					FinalRound: config.FinalRoundSpec{MaxRounds: 1, TimeoutSeconds: 5, Prompt: "close agent b out"},
				},
			},
		}
		run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: modelA, Tools: []trpctool.CallableTool{tool}},
			"agent-b": {Model: modelB, Tools: []trpctool.CallableTool{tool}},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		cases := []struct {
			agent     event.AgentID
			decisions int
			prompt    string
		}{
			{agent: "agent-a", decisions: 3, prompt: budgetPrompt},
			{agent: "agent-b", decisions: 2, prompt: "close agent b out"},
		}
		for _, tc := range cases {
			turn, err := run.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: event.InvocationID("inv-budget-" + string(tc.agent)),
				Address:      event.Address{AgentID: tc.agent, SessionID: event.SessionID("session-budget-" + string(tc.agent))},
				Input:        []model.Message{{Role: model.RoleUser, Content: "start"}},
			})
			if err != nil {
				t.Fatalf("invoke %q under its declared budget: %v", tc.agent, err)
			}
			if turn.FinalRound != agent.FinalRoundRounds {
				t.Fatalf("final round reason of %q = %q, want %q", tc.agent, turn.FinalRound, agent.FinalRoundRounds)
			}
			if len(turn.Decisions) != tc.decisions {
				t.Fatalf("decisions of %q = %d, want %d beside its own round budget", tc.agent, len(turn.Decisions), tc.decisions)
			}
			if got := closingTail(t, turn); got != tc.prompt {
				t.Fatalf("closing request of %q ends on %q, want %q", tc.agent, got, tc.prompt)
			}
		}
	})

	t.Run("a declaration naming no budget keeps the drive unbounded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tool := &convTool{name: budgetToolName, output: budgetToolOutput}
		var mu sync.Mutex
		round := 0
		provider := &convModel{name: "model-a", answer: func(*model.Request) *model.Response {
			mu.Lock()
			round++
			current := round
			mu.Unlock()
			if current == 1 {
				return convToolTurn(convCall("call-open", budgetToolName, `{"step":1}`))
			}
			return convFinalTurn("settled")
		}}
		decl := &config.Declaration{
			Entry: "agent-a",
			Agents: []config.AgentSpec{{
				ID:    "agent-a",
				Model: "model-a",
				Tools: []config.ToolRef{{Name: budgetToolName}},
			}},
		}
		run := assembleBudgeted(t, ctx, decl, provider, tool)

		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-unbounded",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-unbounded"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "start"}},
		})
		if err != nil {
			t.Fatalf("invoke beside no budget: %v", err)
		}
		if turn.FinalRound != agent.FinalRoundNone {
			t.Fatalf("final round reason = %q, want no closing round on a declaration naming no budget", turn.FinalRound)
		}
		if len(turn.Decisions) != 2 || turn.Terminal != model.KindCompleted {
			t.Fatalf("turn = (%v, %d decisions), want the tool round beside the answer", turn.Terminal, len(turn.Decisions))
		}
		if got := tool.calls(); got != 1 {
			t.Fatalf("tool executions = %d, want the admitted call executed once", got)
		}
	})

	t.Run("a budget named in seconds reaches the drive as a time budget", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tool := &convTool{name: budgetToolName, output: budgetToolOutput}
		provider := &convModel{name: "model-a", answer: alwaysToolTurn(tool)}
		decl := &config.Declaration{
			Entry: "agent-a",
			Agents: []config.AgentSpec{{
				ID:    "agent-a",
				Model: "model-a",
				Tools: []config.ToolRef{{Name: budgetToolName}},
				FinalRound: config.FinalRoundSpec{
					BudgetSeconds:  0.001,
					TimeoutSeconds: 5,
					Prompt:         budgetPrompt,
				},
			}},
		}
		run := assembleBudgeted(t, ctx, decl, provider, tool)

		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-budget-seconds",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-budget-seconds"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "start"}},
		})
		if err != nil {
			t.Fatalf("invoke under the declared time budget: %v", err)
		}
		if turn.FinalRound != agent.FinalRoundBudget {
			t.Fatalf("final round reason = %q, want %q", turn.FinalRound, agent.FinalRoundBudget)
		}
		if got := closingTail(t, turn); got != budgetPrompt {
			t.Fatalf("closing request ends on %q, want the instruction the declaration names", got)
		}
		if got, want := tool.calls(), len(turn.Decisions)-1; got != want {
			t.Fatalf("tool executions = %d, want one per ordinary round and none past the spent time budget (%d decisions)", got, len(turn.Decisions))
		}
	})
}

// alwaysToolTurn scripts one provider answer that always issues a tool call, so
// a drive keeps deciding until its budget ends it.
func alwaysToolTurn(tool *convTool) func(*model.Request) *model.Response {
	var mu sync.Mutex
	round := 0
	return func(*model.Request) *model.Response {
		mu.Lock()
		round++
		current := round
		mu.Unlock()
		return convToolTurn(convCall(fmt.Sprintf("call-%d", current), tool.name, fmt.Sprintf(`{"step":%d}`, current)))
	}
}

// assembleBudgeted assembles the formal runtime over one declaration and
// registers the tool it declares, so the budget reaches the loop the way a host
// hands it over.
func assembleBudgeted(t *testing.T, ctx context.Context, decl *config.Declaration, provider model.Model, tool *convTool) *assembly.Run {
	t.Helper()
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// closingTail reports the content of the last message the latest decision
// assembled, which is where a drive appends the closing instruction.
func closingTail(t *testing.T, turn agent.Turn) string {
	t.Helper()
	if len(turn.Decisions) == 0 {
		t.Fatal("turn carries no decision, want the closing round among them")
	}
	messages := turn.Decisions[len(turn.Decisions)-1].Request.Messages
	if len(messages) == 0 {
		t.Fatal("closing request carries no message")
	}
	last := messages[len(messages)-1]
	if last.Role != model.RoleUser {
		t.Fatalf("closing request ends on role %q, want the instruction appended after the transcript", last.Role)
	}
	return last.Content
}

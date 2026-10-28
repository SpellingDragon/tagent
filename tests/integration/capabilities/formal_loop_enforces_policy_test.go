package capabilities

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestFormalLoopEnforcesPolicy drives the policy ruling and the After read-out of the formal loop through the public entries of the module root.
//   - a strict policy ruling refuses a violating call before any execution: the probe runs zero times and the paired result names the refusal and its rule
//   - the ruling is rendered exactly once per call from the risk grade the one Before pass carried, so the refusal proves that grade reached the ruling
//   - the After presentation reaches the run read face: a failed observation is a named gap beside the rendered view and the committed fact stays readable
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestFormalLoopEnforcesPolicy(t *testing.T) {
	t.Run("a strict policy ruling refuses a violating call before any execution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		probe := &policyProbe{name: "danger_tool"}
		governor := &countingGovernor{inner: governance.Open(
			governance.WithStrict(),
			governance.WithRules(governance.RiskRule{
				ID:      "no-danger",
				MinRisk: plugin.RiskHigh,
				Match:   func(in governance.RiskContext) bool { return in.ToolName == "danger_tool" },
				Reason:  "danger_tool is refused at the high grade",
			}),
		)}
		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			if round == 1 {
				return ntToolTurn(
					ntCall("call-danger-1", "danger_tool", `{"x":1}`),
					ntCall("call-danger-2", "danger_tool", `{"x":2}`),
				)
			}
			if msg := req.Messages[0]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "system prompt") {
				t.Errorf("second request prompt body = %+v, want the prompt file fixed at the request boundary", msg)
			}
			if msg := req.Messages[1]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "body v1") {
				t.Errorf("second request skill body = %+v, want the skill file fixed at the request boundary", msg)
			}
			checkPair(t, req.Messages[4], "call-danger-1", "danger_tool", task.ErrDenied.Error())
			checkPair(t, req.Messages[4], "call-danger-1", "danger_tool", "rule: no-danger")
			checkPair(t, req.Messages[4], "call-danger-1", "danger_tool", "danger_tool is refused at the high grade")
			checkPair(t, req.Messages[5], "call-danger-2", "danger_tool", task.ErrDenied.Error())
			checkPair(t, req.Messages[5], "call-danger-2", "danger_tool", "rule: no-danger")
			return ntFinalTurn("settled")
		}}
		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {
				Model:   provider,
				Tools:   []trpctool.CallableTool{probe},
				Plugins: []plugin.Instance{&policyGrader{high: map[string]bool{"danger_tool": true}}},
			},
		}, tagent.WithGovernance(passPolicy{}), tagent.WithPolicy(governor))
		if err != nil {
			t.Fatalf("assemble the policy runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-policy"}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-policy",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the policy runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the refusal closing the turn completed beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := probe.calls(); got != 0 {
			t.Fatalf("danger_tool executions = %d, want the strict refusal leaving zero executions", got)
		}
		if got := governor.calls(); got != 2 {
			t.Fatalf("pre-execution rulings = %d, want exactly one ruling per refused call (2)", got)
		}
		scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}
		reading, err := run.Query(ctx, query.ListFilter{Scope: scope})
		if err != nil {
			t.Fatalf("query the policy scene: %v", err)
		}
		for _, detail := range reading.Details {
			if detail.Ref.Kind == query.ObjectTask {
				t.Fatalf("task detail = %+v, want no task registered for a call refused before execution", detail.Ref)
			}
		}
	})

	t.Run("the After presentation reaches the diagnostic read face of the run", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, "prompts/system.md", "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")
		probe := &policyProbe{name: "echo_tool"}
		var mu sync.Mutex
		round := 0
		provider := &ntModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			if round == 1 {
				return ntToolTurn(ntCall("call-echo", "echo_tool", `{"x":1}`))
			}
			if msg := req.Messages[0]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "system prompt") {
				t.Errorf("second request prompt body = %+v, want the prompt file fixed at the request boundary", msg)
			}
			if msg := req.Messages[1]; msg.Role != model.RoleSystem || !strings.Contains(msg.Content, "body v1") {
				t.Errorf("second request skill body = %+v, want the skill file fixed at the request boundary", msg)
			}
			checkObservation(t, req.Messages[5], "call-echo", "echo_tool", "probe-out:echo_tool")
			return ntFinalTurn("settled")
		}}
		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {
				Model: provider,
				Tools: []trpctool.CallableTool{probe},
				Plugins: []plugin.Instance{
					&policyObserver{name: "down-observer"},
					&policyPresenter{name: "final-presenter"},
				},
			},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the presentation runtime: %v", err)
		}
		addr := event.Address{AgentID: "agent-a", SessionID: "session-presentation"}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-presentation",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the presentation runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the settled observation closing the turn completed", turn.Terminal, len(turn.Decisions))
		}
		if got := probe.calls(); got != 1 {
			t.Fatalf("echo_tool executions = %d, want the allowed call executed once", got)
		}
		shown := run.Presentations()
		if len(shown) != 5 {
			t.Fatalf("presentations = %+v, want one After presentation per committed fact of the turn (entry input, assistant call, settled delivery, call result and output)", shown)
		}
		facts := map[event.EventID]bool{}
		for _, presentation := range shown {
			if presentation.Fact == "" {
				t.Fatalf("presentation = %+v, want the committed fact it was rendered from", presentation)
			}
			if facts[presentation.Fact] {
				t.Fatalf("presentation fact = %s, want one presentation per committed fact", presentation.Fact)
			}
			facts[presentation.Fact] = true
			if len(presentation.Gaps) != 1 {
				t.Fatalf("gaps = %+v, want the failed observation left as one visible gap", presentation.Gaps)
			}
			gap := presentation.Gaps[0]
			if gap.Plugin != "down-observer" || gap.Fact != presentation.Fact || gap.Err == nil || !strings.Contains(gap.Err.Error(), "down-observer") {
				t.Fatalf("gap = %+v, want the failed observation named beside its plugin, fact and error", gap)
			}
			if len(presentation.Views) != 1 {
				t.Fatalf("views = %+v, want the rendered presentation beside the gap", presentation.Views)
			}
			view := presentation.Views[0]
			if view.Plugin != "final-presenter" || !strings.Contains(view.Text, string(presentation.Fact)) || view.Dropped {
				t.Fatalf("view = %+v, want the rendered view kept beside the gap and not dropped", view)
			}
		}
	})
}

// policyProbe is the native tool stand-in of these sub-cases: it counts the
// real executions its side effect ran and reports the output the settled
// observation carries.
type policyProbe struct {
	name string

	mu       sync.Mutex
	executed int
}

// Declaration returns the declaration naming the probe tool.
func (p *policyProbe) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: p.name}
}

// Call counts the execution and reports the probe output.
func (p *policyProbe) Call(_ context.Context, _ []byte) (any, error) {
	p.mu.Lock()
	p.executed++
	p.mu.Unlock()
	return "probe-out:" + p.name, nil
}

// calls reports how often the probe side effect really ran.
func (p *policyProbe) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executed
}

// policyGrader is the risk face of these sub-cases: it grades the armed tool
// names at the high grade and everything else at the low grade, so the ruling
// only fires when the risk grade of the one Before pass really reached it.
type policyGrader struct {
	high map[string]bool
}

// Name returns the stable name of the test plugin.
func (g *policyGrader) Name() string { return "policy-grader" }

// Port returns the single port the test instance serves.
func (g *policyGrader) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductRuling}
}

// AssessRisk grades one tool call from the name it carries.
func (g *policyGrader) AssessRisk(_ context.Context, call *model.ToolCall) plugin.RiskLevel {
	if g.high[call.Function.Name] {
		return plugin.RiskHigh
	}
	return plugin.RiskLow
}

// countingGovernor is the ruling counter of these sub-cases: it wraps the
// policy under test and counts every ruling the wrapper drew from it, so the
// exactly-once duty of the admission ruling is observable.
type countingGovernor struct {
	inner plugin.Governor

	mu     sync.Mutex
	judged int
}

// Judge counts the ruling and delegates to the wrapped policy.
func (g *countingGovernor) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	g.mu.Lock()
	g.judged++
	g.mu.Unlock()
	return g.inner.Judge(pass)
}

// calls reports how many rulings were drawn from the policy.
func (g *countingGovernor) calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.judged
}

// policyObserver is the failing observation face of these sub-cases: every
// observation fails beside the plugin's own name, so the gap is attributable.
type policyObserver struct {
	name string
}

// Name returns the stable name of the test plugin.
func (o *policyObserver) Name() string { return o.name }

// Port returns the single port the test instance serves.
func (o *policyObserver) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

// ObserveCommit fails the observation beside the plugin name and the fact.
func (o *policyObserver) ObserveCommit(_ context.Context, id event.EventID) error {
	return fmt.Errorf("%s is down observing %s", o.name, id)
}

// policyPresenter is the rendering face of these sub-cases: it renders the fact
// identity into its view and keeps the view on its channel.
type policyPresenter struct {
	name string
}

// Name returns the stable name of the test plugin.
func (p *policyPresenter) Name() string { return p.name }

// Port returns the single port the test instance serves.
func (p *policyPresenter) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductView}
}

// Present renders the presentation text of one committed fact.
func (p *policyPresenter) Present(_ context.Context, id event.EventID) (string, bool) {
	return "presented:" + string(id), false
}

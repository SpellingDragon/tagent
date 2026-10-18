package acceptance

import (
	"context"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// scenarioTransform is the call-transform element of the drive: it rewrites the
// arguments of every call it sees to the final argument body.
type scenarioTransform struct {
	mu     sync.Mutex
	final  string
	passed int
}

// Name reports the stable name of the element.
func (p *scenarioTransform) Name() string { return "scenario-transform" }

// Phase reports the pre-execution call phase.
func (p *scenarioTransform) Phase() plugin.Phase { return plugin.PhaseCallTransform }

// TransformCall rewrites one call's arguments to the final body.
func (p *scenarioTransform) TransformCall(ctx context.Context, call *trpcmodel.ToolCall) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.passed++
	call.Function.Arguments = []byte(p.final)
	return nil
}

// transformed reports how many calls the transform rewrote.
func (p *scenarioTransform) transformed() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.passed
}

// scenarioPolicy is the pre-execution ruling of the drive: it allows calls of one
// tool and refuses calls of another beside their named denial.
type scenarioPolicy struct {
	mu      sync.Mutex
	refuse  string
	judged  map[string]int
	rulings []plugin.Decision
}

// Name reports the stable name of the element.
func (p *scenarioPolicy) Name() string { return "scenario-policy" }

// Phase reports the risk-policy phase the ruling belongs to.
func (p *scenarioPolicy) Phase() plugin.Phase { return plugin.PhaseRiskPolicy }

// Judge renders the one ruling of a call.
func (p *scenarioPolicy) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.judged == nil {
		p.judged = map[string]int{}
	}
	tool := pass.Final.Function.Name
	p.judged[tool]++
	if tool == p.refuse {
		ruling := plugin.Decision{
			Disposition: plugin.DispositionAllow,
			Risk:        plugin.RiskHigh,
			Rule:        "scenario-refusal",
			Reason:      "the scenario policy refuses this call",
			Denial: &plugin.Denial{
				CallID: pass.Final.ID,
				Tool:   tool,
				Risk:   plugin.RiskHigh,
				Rule:   "scenario-refusal",
				Result: model.Message{
					Role:     model.RoleTool,
					ToolID:   pass.Final.ID,
					ToolName: tool,
					Content:  "refused: the scenario policy refuses this call",
				},
			},
		}
		p.rulings = append(p.rulings, ruling)
		return ruling, nil
	}
	ruling := plugin.Decision{Disposition: plugin.DispositionAllow, Risk: plugin.RiskMedium, Reason: "the scenario policy allows this call"}
	p.rulings = append(p.rulings, ruling)
	return ruling, nil
}

// counts reports how often each tool was ruled.
func (p *scenarioPolicy) counts() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]int{}
	for tool, count := range p.judged {
		out[tool] = count
	}
	return out
}

// scenarioPresenter is the final-presentation element of the drive: it renders the
// presentation text of one committed fact.
type scenarioPresenter struct{}

// Name reports the stable name of the element.
func (p *scenarioPresenter) Name() string { return "scenario-presenter" }

// Phase reports the final-presentation phase.
func (p *scenarioPresenter) Phase() plugin.Phase { return plugin.PhaseFinalPresentation }

// Present renders the presentation text of one fact identity.
func (p *scenarioPresenter) Present(ctx context.Context, id event.EventID) (string, bool) {
	return "presentation:" + string(id), false
}

// TestScenario04PluginPhaseAndGovernance drives one decision carrying an allowed call beside a refused one.
// - one pre-execution ruling per call, and the transformed final arguments are what executes
// - a refused call leaves no side effect behind
// - the post-commit presentation carries the committed fact it was rendered from
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
func TestScenario04PluginPhaseAndGovernance(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S04")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	transform := &scenarioTransform{final: `{"v":2}`}
	policy := &scenarioPolicy{refuse: "deny-tool"}
	presenter := &scenarioPresenter{}
	allowed := &scenarioTool{name: "allow-tool", output: "allow-output"}
	refused := &scenarioTool{name: "deny-tool", output: "deny-output"}
	turns := 0
	provider := &scenarioModel{name: "model-a"}
	provider.answer = func(req *model.Request) *model.Response {
		turns++
		if turns == 1 {
			return scenarioToolTurn(
				scenarioCall("call-allow", "allow-tool", `{"v":1}`),
				scenarioCall("call-deny", "deny-tool", `{"v":1}`),
			)
		}
		return scenarioFinalTurn("settled")
	}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}}
	governance := &scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {
			Model:   provider,
			Tools:   []trpctool.CallableTool{allowed, refused},
			Plugins: []plugin.Plugin{transform, policy, presenter},
		},
	}, tagent.WithGovernance(governance), tagent.WithPolicy(policy))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-plugin",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-plugin"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "run both calls"}},
	}); err != nil {
		t.Fatalf("invoke the plugin scenario: %v", err)
	}

	counts := policy.counts()
	observations["pre_execution_policy_runs"] = scenarioUniformCount(counts)
	observations["final_params_executed"] = transform.transformed() == 2 && allowed.lastArgs() == `{"v":2}` && allowed.calls() == 1
	observations["denied_tool_side_effects"] = refused.calls()
	presentations := run.Presentations()
	preserved := len(presentations) > 0
	for _, presentation := range presentations {
		if presentation.Fact == "" || len(presentation.Views) == 0 {
			preserved = false
			continue
		}
		if presentation.Views[0].Text != "presentation:"+string(presentation.Fact) {
			preserved = false
		}
	}
	observations["post_commit_presentation_preserves_fact"] = preserved
	for _, presentation := range presentations {
		refs = append(refs, string(presentation.Fact))
	}

	EmitScenarioRecord(t, scenario, refs, observations)
}

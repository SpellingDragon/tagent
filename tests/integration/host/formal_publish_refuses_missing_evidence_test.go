package host

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// formalRequiredChecks names the checks the formal publish precondition contract requires a disposition for.
var formalRequiredChecks = []state.CheckName{"eval", "benchmark", "apply-gate"}

// evidencePolicy is the deterministic stand-in of the real governance policy the formal publish gate is driven under here: it answers one passing disposition per required check beside the names it withholds. Its answers are mechanism inputs carrying no quality conclusion, and the formal quality assessment lives in the structural evaluation gate.
type evidencePolicy struct {
	mu      sync.Mutex
	answers map[state.CheckName]state.CheckAnswer
}

// newEvidencePolicy returns the stand-in beside one passing disposition per required check.
func newEvidencePolicy() *evidencePolicy {
	p := &evidencePolicy{answers: map[state.CheckName]state.CheckAnswer{}}
	p.restore()
	return p
}

// Check implements state.Governance with the dispositions the stand-in carries beside the names it withholds.
func (p *evidencePolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[state.CheckName]state.CheckAnswer, len(p.answers))
	for name, answer := range p.answers {
		out[name] = answer
	}
	return out, nil
}

// withhold removes the named check from the dispositions the stand-in answers.
func (p *evidencePolicy) withhold(name state.CheckName) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.answers, name)
}

// answer stages the disposition the stand-in reports under the name it carries.
func (p *evidencePolicy) answer(answer state.CheckAnswer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answers[answer.Name] = answer
}

// restore returns the stand-in to one passing disposition per required check.
func (p *evidencePolicy) restore() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, name := range formalRequiredChecks {
		p.answers[name] = passingCheck(name)
	}
}

// TestFormalPublishRefusesMissingEvidence drives the formal publish entry and checks that a publication missing required evidence never takes effect.
//   - beside a governance that answers only part of the required checks the formal publish is refused beside the named missing check and the zero outcome
//   - beside a check that answers no disposition the formal publish is refused the same way, since silence is missing evidence and never a passing disposition
//   - every refusal keeps the publication in effect and the configured account exactly as they were
//   - the candidate every refusal stopped takes the next generation as one fresh change once the required checks carry dispositions
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestFormalPublishRefusesMissingEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	policy := newEvidencePolicy()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	tool := &entryTool{name: "echo", output: "out-a"}
	declA := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	agentsA := map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}
	run, err := tagent.Assemble(ctx, declA, agentsA, tagent.WithGovernance(policy))
	if err != nil {
		t.Fatalf("assemble the formal run: %v", err)
	}
	before, err := run.Publish(ctx, declA, agentsA)
	if err != nil {
		t.Fatalf("read the publication in effect: %v", err)
	}
	if before.Result != state.PublishUnchanged || before.Publication.Generation == 0 {
		t.Fatalf("probe before the refusals = %+v, want the byte-identical reload to report the publication in effect", before)
	}

	providerB := &entryModel{name: "model-b", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	declB := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{
			{ID: "agent-a", Model: "model-a"},
			{ID: "agent-b", Model: "model-b"},
		},
	}
	agentsB := map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
		"agent-b": {Model: providerB},
	}

	policy.withhold("eval")
	outcome, err := run.Publish(ctx, declB, agentsB)
	if !errors.Is(err, state.ErrRequiredCheckMissing) {
		t.Fatalf("publish beside one required check left out = (%+v, %v), want the refusal beside %v", outcome, err, state.ErrRequiredCheckMissing)
	}
	if !strings.Contains(err.Error(), "check eval") {
		t.Fatalf("refusal reason = %v, want the undispositioned check named beside the refusal", err)
	}
	if outcome != (state.PublishOutcome{}) {
		t.Fatalf("refusal outcome = %+v, want the zero outcome beside the refusal", outcome)
	}
	after, err := run.Publish(ctx, declA, agentsA)
	if err != nil {
		t.Fatalf("read the publication beside the refusal: %v", err)
	}
	if after != before {
		t.Fatalf("publication in effect = %+v, want the one the refusals kept (%+v)", after, before)
	}

	policy.answer(state.CheckAnswer{Name: "eval"})
	outcome, err = run.Publish(ctx, declB, agentsB)
	if !errors.Is(err, state.ErrRequiredCheckMissing) {
		t.Fatalf("publish beside a check that answers no disposition = (%+v, %v), want the refusal beside %v", outcome, err, state.ErrRequiredCheckMissing)
	}
	if outcome != (state.PublishOutcome{}) {
		t.Fatalf("refusal outcome = %+v, want the zero outcome beside the refusal", outcome)
	}
	snapshot, err := run.Query(ctx, query.ListFilter{
		Scope: query.AuthorizationScope{Agents: []event.AgentID{"agent-a", "agent-b"}},
		Kinds: []query.ObjectKind{query.ObjectAgent},
	})
	if err != nil {
		t.Fatalf("read the configured account beside the refusals: %v", err)
	}
	var configured []event.AgentID
	for _, detail := range snapshot.Details {
		configured = append(configured, detail.Ref.Address.AgentID)
	}
	if len(configured) != 1 || configured[0] != "agent-a" {
		t.Fatalf("configured agents = %v, want the account the publication in effect names", configured)
	}
	probe, err := run.Publish(ctx, declA, agentsA)
	if err != nil {
		t.Fatalf("read the publication beside the refusals: %v", err)
	}
	if probe != before {
		t.Fatalf("publication in effect = %+v, want the one the refusals kept (%+v)", probe, before)
	}

	policy.restore()
	outcome, err = run.Publish(ctx, declB, agentsB)
	if err != nil {
		t.Fatalf("publish the candidate beside answered checks: %v", err)
	}
	if outcome.Result != state.PublishApplied {
		t.Fatalf("publish result = %v, want the candidate admitted as one fresh change", outcome.Result)
	}
	if want := before.Publication.Generation + 1; outcome.Publication.Generation != want {
		t.Fatalf("published generation = %d, want %d: every refusal stopped its candidate beside the generation in effect", outcome.Publication.Generation, want)
	}
}

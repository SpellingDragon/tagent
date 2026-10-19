package evolution

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/runtime/state"
)

const (
	// CheckEval names the evaluation check a publication must carry a
	// disposition for.
	CheckEval state.CheckName = state.CheckName(SegmentEval)

	// CheckBenchmark names the benchmark check a publication must carry a
	// disposition for.
	CheckBenchmark state.CheckName = state.CheckName(SegmentBenchmark)

	// CheckApplyGate names the apply gate check a publication must carry a
	// disposition for.
	CheckApplyGate state.CheckName = state.CheckName(SegmentApplyGate)
)

// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
//
// Policy is the governance adapter of the structural gate: it dispositions the
// checks of one publication candidate by running the gate's segments over the
// same declaration plan and the same generation of the execution binding the
// candidate carries. Each disposition is filed under its check name beside the
// reason, scope and evidence references of the gate report it comes from, and a
// failing segment keeps its named root cause chain in the returned error so the
// refusal it causes stays attributable. Missing evidence is filed as an
// unanswered check and never as a passing one.
type Policy struct {
	gate StructuralGate
}

// NewPolicy returns the governance policy over one structural gate.
func NewPolicy(gate StructuralGate) *Policy {
	return &Policy{gate: gate}
}

// state.Governance is satisfied by the policy adapter.
var _ state.Governance = (*Policy)(nil)

// Check evaluates one publication candidate and answers each check under the
// name it is filed by. The gate segments run over the declaration plan the
// candidate carries beside the candidate itself, and the errors of the failing
// segments are returned beside the answers so a refused publication reaches
// their named roots.
func (p *Policy) Check(ctx context.Context, plan state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	answers := map[state.CheckName]state.CheckAnswer{}
	var failures []error

	evaluated, err := p.gate.Eval(ctx, plan.Plan, plan)
	answers[CheckEval] = checkAnswer(CheckEval, evaluated)
	if err != nil {
		failures = append(failures, err)
	}

	benchmarked, err := p.gate.Benchmark(ctx, plan.Plan, plan)
	answers[CheckBenchmark] = checkAnswer(CheckBenchmark, benchmarked)
	if err != nil {
		failures = append(failures, err)
	}

	applied, err := p.gate.ApplyGate(ctx, plan.Plan, plan)
	answers[CheckApplyGate] = checkAnswer(CheckApplyGate, applied)
	if err != nil {
		failures = append(failures, err)
	}

	return answers, errors.Join(failures...)
}

// checkAnswer files one gate report as the disposition of one named check: the
// verdict it reports, the reason and scope it rests on and the evidence it cites.
func checkAnswer(name state.CheckName, report GateReport) state.CheckAnswer {
	answer := state.CheckAnswer{Name: name, Verdict: state.CheckUnanswered, Reason: report.Reason, Scope: report.Scope}
	for _, ref := range report.EvidenceRefs {
		answer.Evidence = append(answer.Evidence, string(ref))
	}
	switch report.Verdict {
	case GateVerdictPass:
		answer.Verdict = state.CheckPass
	case GateVerdictReject:
		answer.Verdict = state.CheckReject
	case GateVerdictNotApplicable:
		answer.Verdict = state.CheckExempt
	case GateVerdictInsufficientEvidence:
		answer.Verdict = state.CheckUnanswered
	}
	return answer
}

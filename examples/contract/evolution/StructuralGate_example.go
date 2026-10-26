package evolutionexample

import (
	"context"

	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// evidenceGate is the canonical structural gate: one ordered pass over the same
// declaration plan and the same prepared binding, each segment reporting its disposition
// beside the evidence it rests on.
type evidenceGate struct {
	// evidence names the evaluation evidence the candidate's declarations were checked
	// against; an empty list is reported as insufficient evidence and never as a pass.
	evidence []evolution.EvidenceRef
}

// Eval reports whether the candidate's declarations meet their evaluation evidence.
func (g evidenceGate) Eval(_ context.Context, plan state.OrgPlan, _ state.StatePlan) (evolution.GateReport, error) {
	return g.report(evolution.SegmentEval, plan)
}

// Benchmark reports whether the candidate's declarations meet their benchmark evidence.
func (g evidenceGate) Benchmark(_ context.Context, plan state.OrgPlan, _ state.StatePlan) (evolution.GateReport, error) {
	return g.report(evolution.SegmentBenchmark, plan)
}

// ApplyGate reports whether the candidate's declaration and binding may be applied as the
// next generation.
func (g evidenceGate) ApplyGate(_ context.Context, plan state.OrgPlan, _ state.StatePlan) (evolution.GateReport, error) {
	return g.report(evolution.SegmentApplyGate, plan)
}

// report renders one segment's disposition over the candidate's declaration fingerprint.
func (g evidenceGate) report(segment evolution.Segment, plan state.OrgPlan) (evolution.GateReport, error) {
	if len(g.evidence) == 0 {
		return evolution.GateReport{
			Verdict: evolution.GateVerdictInsufficientEvidence,
			Reason:  "no evidence is recorded for this candidate",
			Scope:   string(segment),
		}, nil
	}
	return evolution.GateReport{
		Verdict:      evolution.GateVerdictPass,
		Reason:       "the recorded evidence covers the candidate",
		Scope:        string(segment),
		EvidenceRefs: g.evidence,
	}, nil
}

var _ evolution.StructuralGate = evidenceGate{}

// ExampleStructuralGate passes one candidate through the three ordered segments: a
// candidate with recorded evidence passes, and one without it is reported as
// insufficient evidence rather than as a pass.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func ExampleStructuralGate() {
	ctx := context.Background()
	plan := state.OrgPlan{Fingerprint: "fingerprint-1"}
	binding := state.StatePlan{}

	passing := evidenceGate{evidence: []evolution.EvidenceRef{"eval-report-1", "benchmark-1"}}
	evaluated, err := passing.Eval(ctx, plan, binding)
	if err != nil {
		return
	}
	benched, err := passing.Benchmark(ctx, plan, binding)
	if err != nil {
		return
	}
	applied, err := passing.ApplyGate(ctx, plan, binding)
	if err != nil {
		return
	}

	missing := evidenceGate{}
	insufficient, err := missing.Eval(ctx, plan, binding)
	if err != nil {
		return
	}
	if insufficient.Verdict != evolution.GateVerdictInsufficientEvidence {
		return
	}
	_ = evaluated.Verdict
	_ = benched.Verdict
	_ = applied.Verdict
}

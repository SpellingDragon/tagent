// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
//
// Package evolution owns the self-improvement channel: it records an improvement
// a real agent already landed, evaluates that improvement after real requests
// consumed it, and proposes a rollback without ever performing one.
//
// An edit to a prompt, skill or script takes effect by itself, so this package
// tracks the edit instead of gating it: a file is read hot at the next request
// boundary, and only a structural change passes the gate declared here.
package evolution

import (
	"context"

	"github.com/SpellingDragon/tagent/state"
)

// StructuralGate is the ordered gate a candidate organization plan passes
// through before it becomes the next effective generation: Eval, then Benchmark,
// then ApplyGate.
//
// Every segment receives the same declaration plan and the same generation of
// the prepared execution binding, so no segment compiles a second declaration or
// a shadow binding of its own. A segment reports its disposition in a
// GateVerdict and keeps a failure's named root cause reachable through
// errors.Is and errors.As rather than flattened into a bare string. A segment
// whose evidence is missing is never reported as a pass.
type StructuralGate interface {
	// Eval reports whether the candidate's declarations meet their evaluation
	// evidence. Evidence that is missing or unreachable yields insufficient
	// evidence, which is never a pass.
	Eval(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateVerdict, error)

	// Benchmark reports whether the candidate's declarations meet their benchmark
	// evidence. Evidence that is missing or unreachable yields insufficient
	// evidence, which is never a pass.
	Benchmark(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateVerdict, error)

	// ApplyGate reports whether the candidate's declaration and binding may be
	// applied as the next generation. A missing required check refuses the
	// publication, and a check that does not apply to this candidate is recorded
	// with its reason and scope instead of being counted as evidence.
	ApplyGate(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateVerdict, error)
}

// GateVerdict is the disposition one structural gate segment reports for a
// candidate declaration plan.
//
// The verdicts are mutually exclusive and the three non-passing dispositions stay
// distinguishable: a segment that does not apply, a segment whose evidence is
// missing and a segment that refuses are three different facts. Insufficient
// evidence is never reported as a pass.
type GateVerdict string

const (
	// GateVerdictPass reports that the segment ran and its evidence passed.
	GateVerdictPass GateVerdict = "pass"

	// GateVerdictReject reports that the segment ran and refused the candidate.
	GateVerdictReject GateVerdict = "reject"

	// GateVerdictNotApplicable reports that the segment does not apply to this
	// candidate; the exclusion carries its reason and scope.
	GateVerdictNotApplicable GateVerdict = "not-applicable"

	// GateVerdictInsufficientEvidence reports that the segment's evidence is
	// missing or unreachable. It is never reported as a pass.
	GateVerdictInsufficientEvidence GateVerdict = "insufficient-evidence"
)

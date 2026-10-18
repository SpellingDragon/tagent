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
// GateReport — verdict, reason, scope and evidence references together — and
// keeps a failure's named root cause reachable through errors.Is and errors.As
// rather than flattened into a bare string. A segment whose evidence is missing
// is never reported as a pass.
// The redline list (4 rules):
// MUST hand every segment the same declaration plan and the same prepared binding generation.
// MUST keep a named failure's root cause reachable through errors.Is and errors.As.
// MUST NOT report a segment whose evidence is missing as a pass.
// MUST NOT compile a second declaration or a shadow binding inside a segment.
type StructuralGate interface {
	// Eval reports whether the candidate's declarations meet their evaluation
	// evidence. The report carries the disposition with its reason, scope and
	// evidence references. Evidence that is missing or unreachable yields
	// insufficient evidence, which is never a pass.
	Eval(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error)

	// Benchmark reports whether the candidate's declarations meet their benchmark
	// evidence. The report carries the disposition with its reason, scope and
	// evidence references. Evidence that is missing or unreachable yields
	// insufficient evidence, which is never a pass.
	Benchmark(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error)

	// ApplyGate reports whether the candidate's declaration and binding may be
	// applied as the next generation. A missing required check refuses the
	// publication, and a check that does not apply to this candidate is recorded
	// in the report with its reason and scope instead of being counted as
	// evidence.
	ApplyGate(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error)
}

// GateVerdict is the disposition one structural gate segment reports for a
// candidate declaration plan.
//
// The verdicts are mutually exclusive and the three non-passing dispositions stay
// distinguishable: a segment that does not apply, a segment whose evidence is
// missing and a segment that refuses are three different facts. Insufficient
// evidence is never reported as a pass. This vocabulary judges a structural
// candidate before publication and is never reused as the post-hoc evaluation
// states of an improvement version; those live in PosteriorVerdict and never here.
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

// EvidenceRef names one piece of evidence a gate or post-hoc report cites: a
// run identifier, a check result or another durable reference a reader can
// resolve and re-verify. A verdict that is not backed by resolvable evidence is
// never a pass.
type EvidenceRef string

// GateReport is the disposition one structural gate segment returns for a
// candidate declaration plan, with everything a reader needs to audit it: the
// verdict, the reason behind it, the scope the disposition covers and the
// evidence the verdict rests on.
//
// A gate never returns a bare verdict. A not-applicable disposition carries its
// exemption reason and scope instead of counting as evidence, an insufficient
// evidence disposition names what is missing, and a disposition without
// reachable evidence is never a pass. The structural vocabulary of Verdict
// judges a candidate before publication and is never reused as the post-hoc
// evaluation states of an improvement version (PosteriorVerdict): the two
// vocabularies are disjoint. The zero GateReport carries no verdict and must
// not be read as a pass.
type GateReport struct {
	// Verdict is the segment's disposition for the candidate.
	Verdict GateVerdict
	// Reason is the named reason behind the verdict: the exemption reason for
	// not-applicable, the named missing evidence for insufficient evidence, and
	// the named ground for a refusal.
	Reason string
	// Scope is the scope the disposition covers: the part of the candidate or
	// the set of checks the verdict, exemption or refusal applies to.
	Scope string
	// EvidenceRefs names the evidence the verdict rests on. A verdict of pass
	// rests on reachable evidence and never on an empty list.
	EvidenceRefs []EvidenceRef
}

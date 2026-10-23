package evolution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// Segment names one segment of the structural gate over one candidate: the
// evaluation segment, the benchmark segment or the apply gate.
type Segment string

const (
	// SegmentEval names the evaluation segment of the structural gate.
	SegmentEval Segment = "eval"

	// SegmentBenchmark names the benchmark segment of the structural gate.
	SegmentBenchmark Segment = "benchmark"

	// SegmentApplyGate names the apply gate of the structural gate.
	SegmentApplyGate Segment = "apply-gate"
)

// ErrDeclarationDiverged reports a gate call whose execution binding was
// prepared from a declaration plan other than the declaration plan the gate
// consumes. The pair is refused outright and never resolved to either side.
var ErrDeclarationDiverged = errors.New("evolution: the execution binding diverges from the declaration plan the gate consumes")

// ErrEvidenceMissing reports gate evidence for a declared change that is
// missing or unreachable. The segment that observes it reports insufficient
// evidence beside this cause, and that verdict is never a pass.
var ErrEvidenceMissing = errors.New("evolution: the gate holds no complete evidence for a declared change")

// ErrCandidateRefused reports a declared change whose probe the candidate
// refused. The refusal carries the probe's own named root cause underneath it.
var ErrCandidateRefused = errors.New("evolution: the candidate refused the gate probe")

// ErrCheckMissing reports an apply gate that holds no evidence for a required
// check. A missing required check refuses the publication.
var ErrCheckMissing = errors.New("evolution: the apply gate holds no evidence for a required check")

// ErrCriterionMissing reports a declared change whose declaration carries no
// evaluation criterion on the judged face. A face without a declared criterion
// cannot be judged, the segment that observes it reports insufficient evidence
// beside this cause, and that verdict is never a pass.
var ErrCriterionMissing = errors.New("evolution: the declaration carries no evaluation criterion for a declared change")

// ErrCriterionNotMet reports a declared change whose produced answer
// contradicts the evaluation criterion its declaration carries. The refusal
// names the produced answer beside the declared value underneath it, and a
// contradiction is never reported as a pass.
var ErrCriterionNotMet = errors.New("evolution: the produced answer does not meet the declared evaluation criterion")

// GateError is the named failure of one structural gate segment over one
// declaration scope: the segment that failed, the scope the failure covers and
// the root cause chain underneath. Unwrap exposes that chain, so a caller
// reaches the named root of a failure through errors.Is and errors.As instead
// of matching rendered text.
type GateError struct {
	// Segment names the structural gate segment that failed.
	Segment Segment
	// Scope names the declaration scope the failure covers.
	Scope string
	// Cause is the root cause chain of the failure.
	Cause error
}

// Error renders the failed segment, the scope it covered and the cause chain.
func (e *GateError) Error() string {
	return fmt.Sprintf("evolution: %s failed scope %s: %v", e.Segment, e.Scope, e.Cause)
}

// Unwrap exposes the root cause chain of the failure.
func (e *GateError) Unwrap() error {
	return e.Cause
}

// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
//
// NewStructuralGate returns the structural gate over the declaration plan and
// the prepared execution binding each call carries in. Every segment consumes
// that one pair through the same admission and the same evidence evaluation, so
// the gate parses no configuration of its own and holds no second declaration
// beside the one it was handed. The gate files what its evaluation and
// benchmark segments observed per pair, and the apply gate consumes those very
// records instead of driving the candidate binding again.
func NewStructuralGate() StructuralGate {
	return &structuralGate{}
}

// structuralGate is the gate implementation over the pair one call carries in.
// It holds one evidence record per pair and evidence class, so the apply gate
// reads the evaluation evidence the pair already produced.
type structuralGate struct {
	mu      sync.Mutex
	records []*evidenceRecord
}

// StructuralGate is satisfied by the gate implementation.
var _ StructuralGate = (*structuralGate)(nil)

// Eval reports whether the candidate's declarations meet their evaluation
// evidence.
func (g *structuralGate) Eval(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error) {
	return g.segment(ctx, SegmentEval, classEval, plan, binding)
}

// Benchmark reports whether the candidate's declarations meet their benchmark
// evidence.
func (g *structuralGate) Benchmark(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error) {
	return g.segment(ctx, SegmentBenchmark, classBenchmark, plan, binding)
}

// ApplyGate reports whether the candidate's declaration and binding may be
// applied as the next generation. It consumes the evidence its required checks
// already recorded for this very pair and drives no probe of its own: a required
// check whose evidence is absent from the record refuses the publication.
func (g *structuralGate) ApplyGate(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error) {
	if err := admit(plan, binding); err != nil {
		return GateReport{}, &GateError{Segment: SegmentApplyGate, Cause: err}
	}
	scope := declarationScope(plan.Fingerprint)
	evaluated := assess("check eval: ", leaseRefs(binding.Leases), g.evidenceOf(classEval, plan, binding))
	evaluated.absorb(assess("check benchmark: ", nil, g.evidenceOf(classBenchmark, plan, binding)))
	return reportOf(SegmentApplyGate, scope, evaluated, true)
}

// segment evaluates one evidence class over the pair one call carries in, files
// what it observed under that pair and dispositions it.
func (g *structuralGate) segment(ctx context.Context, segment Segment, class evidenceClass, plan state.OrgPlan, binding state.StatePlan) (GateReport, error) {
	if err := admit(plan, binding); err != nil {
		return GateReport{}, &GateError{Segment: segment, Cause: err}
	}
	findings := evaluate(ctx, class, plan, binding)
	g.remember(&evidenceRecord{class: class, identity: identityOf(plan, binding), findings: findings})
	return reportOf(segment, declarationScope(plan.Fingerprint),
		assess("", leaseRefs(binding.Leases), findings), false)
}

// admit accepts one declaration plan beside one execution binding only while
// the binding carries the same declaration plan it is judged as. A diverged
// pair is refused by name.
func admit(plan state.OrgPlan, binding state.StatePlan) error {
	if plan.Declaration == nil {
		return state.ErrPlanMissing
	}
	if binding.Plan.Declaration != plan.Declaration ||
		binding.Plan.Fingerprint != plan.Fingerprint ||
		binding.Plan.Baseline != plan.Baseline {
		return ErrDeclarationDiverged
	}
	return nil
}

// evidenceClass names one class of gate evidence: the evaluation probes or the
// benchmark probes.
type evidenceClass string

const (
	// classEval marks the evaluation probes.
	classEval evidenceClass = "eval"

	// classBenchmark marks the benchmark probes.
	classBenchmark evidenceClass = "benchmark"
)

// findingDisposition is what one evidence class observed for one declared
// change: a criterion met, a criterion contradicted, a refused probe, missing
// evidence or an exemption.
type findingDisposition int

const (
	// findingComplete is a declared change whose produced answers meet every
	// declared criterion on the judged face.
	findingComplete findingDisposition = iota

	// findingRefused is a declared change whose probe the candidate refused.
	findingRefused

	// findingUnmet is a declared change whose produced answer contradicts the
	// declared criterion on the judged face.
	findingUnmet

	// findingMissing is a declared change whose evidence is missing or
	// unreachable.
	findingMissing

	// findingExempt is a declared change the evidence class does not apply to.
	findingExempt
)

// changeFinding is one evidence class's observation for one declared change: the
// agent it names, the evidence references its judged cases cite, what the class
// observed, the named label beside it and the cause chain underneath a failure.
type changeFinding struct {
	agent string
	refs  []EvidenceRef
	dispo findingDisposition
	label string
	cause error
}

// traceCause returns the error the reduced observation carries, or nil when it
// carries none.
func traceCause(outcome model.Outcome) error {
	for _, item := range outcome.Trace {
		if item.Err != nil {
			return item.Err
		}
	}
	return nil
}

// refusalCause builds the cause chain of a refused probe: the candidate refusal
// beside the entry error or the observed response error underneath it.
func refusalCause(agent string, entryErr, observed error) error {
	named := fmt.Errorf("%w: agent %s", ErrCandidateRefused, agent)
	switch {
	case entryErr != nil:
		return fmt.Errorf("%w: %w", named, entryErr)
	case observed != nil:
		return fmt.Errorf("%w: %w", named, observed)
	}
	return named
}

// insufficiencyCause builds the cause chain of missing evidence beside the
// terminal it was last seen at.
func insufficiencyCause(agent string, kind model.Kind, observed error) error {
	if kind == model.KindUnknown {
		return fmt.Errorf("%w: agent %s: %w", ErrEvidenceMissing, agent, model.ErrNoTerminal)
	}
	if observed != nil {
		return fmt.Errorf("%w: agent %s: probe terminal is %s: %w", ErrEvidenceMissing, agent, terminalName(kind), observed)
	}
	return fmt.Errorf("%w: agent %s: probe terminal is %s", ErrEvidenceMissing, agent, terminalName(kind))
}

// insufficiencyLabel names what is missing for one non-complete terminal.
func insufficiencyLabel(kind model.Kind) string {
	return "probe terminal is " + terminalName(kind)
}

// terminalName renders one native terminal kind beside the terminal reference it
// is cited by.
func terminalName(kind model.Kind) string {
	switch kind {
	case model.KindCompleted:
		return "completed"
	case model.KindFailed:
		return "failed"
	case model.KindCancelled:
		return "cancelled"
	case model.KindPartialOnly:
		return "partial-only"
	default:
		return "unknown"
	}
}

// assessment is the verdict material one gate segment holds over one assessed
// change set: the evidence it cites, the named reasons beside the verdict and
// the failure causes of the refusals, the contradicted criteria and the missing
// evidence.
type assessment struct {
	refs     []EvidenceRef
	reasons  []string
	rejects  []error
	missing  []error
	complete bool
}

// assess turns the findings of one evidence class into verdict material under
// one reason prefix. The reference list continues the one handed in, so the
// apply gate can hold the evidence of both required checks in one report.
func assess(prefix string, refs []EvidenceRef, findings []changeFinding) assessment {
	material := assessment{refs: refs}
	for _, finding := range findings {
		material.refs = append(material.refs, finding.refs...)
		switch finding.dispo {
		case findingComplete:
			material.complete = true
		case findingRefused, findingUnmet:
			material.rejects = append(material.rejects, finding.cause)
			material.reasons = append(material.reasons, prefix+"agent "+finding.agent+": "+finding.label)
		case findingMissing:
			material.missing = append(material.missing, finding.cause)
			material.reasons = append(material.reasons, prefix+"agent "+finding.agent+": "+finding.label)
		case findingExempt:
			material.reasons = append(material.reasons, prefix+"agent "+finding.agent+": "+finding.label)
		}
	}
	if len(findings) == 0 {
		material.reasons = append(material.reasons, prefix+"the declaration plan carries no change: the check does not apply")
	}
	return material
}

// absorb folds the material of one required check into another, so the apply
// gate holds both checks beside one declaration scope.
func (a *assessment) absorb(b assessment) {
	a.refs = append(a.refs, b.refs...)
	a.reasons = append(a.reasons, b.reasons...)
	a.rejects = append(a.rejects, b.rejects...)
	a.missing = append(a.missing, b.missing...)
	a.complete = a.complete || b.complete
}

// reportOf dispositions one assessed change set. The apply gate refuses a
// missing required check while the other segments report missing evidence as
// insufficient evidence; neither ever reports missing evidence as a pass.
func reportOf(segment Segment, scope string, material assessment, apply bool) (GateReport, error) {
	report := GateReport{Scope: scope, Reason: strings.Join(material.reasons, "; "), EvidenceRefs: material.refs}
	var causes []error
	switch {
	case len(material.rejects) > 0:
		report.Verdict = GateVerdictReject
		causes = append(causes, material.rejects...)
		if len(material.missing) > 0 {
			causes = append(causes, checkMissingCause(material.missing, apply))
		}
	case len(material.missing) > 0:
		causes = append(causes, checkMissingCause(material.missing, apply))
		if apply {
			report.Verdict = GateVerdictReject
		} else {
			report.Verdict = GateVerdictInsufficientEvidence
		}
	case material.complete:
		report.Verdict = GateVerdictPass
	default:
		report.Verdict, report.EvidenceRefs = GateVerdictNotApplicable, nil
	}
	if len(causes) == 0 {
		return report, nil
	}
	return report, &GateError{Segment: segment, Scope: scope, Cause: joinCauses(causes)}
}

// checkMissingCause keeps the named causes of missing evidence underneath the
// apply gate's refusal of a missing required check, and as they are elsewhere.
func checkMissingCause(missing []error, apply bool) error {
	if !apply {
		return joinCauses(missing)
	}
	return fmt.Errorf("%w: %w", ErrCheckMissing, joinCauses(missing))
}

// joinCauses keeps one cause as itself and several causes as one chain that
// reaches every member.
func joinCauses(causes []error) error {
	if len(causes) == 1 {
		return causes[0]
	}
	return errors.Join(causes...)
}

// declarationScope names the declaration surface one disposition covers.
func declarationScope(fingerprint string) string {
	return "declaration:" + fingerprint
}

// leaseRefs renders the candidate resource registrations the execution binding
// carries as the same-generation references one report cites.
func leaseRefs(leases []state.LeaseRef) []EvidenceRef {
	refs := make([]EvidenceRef, 0, len(leases))
	for _, lease := range leases {
		refs = append(refs, EvidenceRef(fmt.Sprintf("lease:%s:%s:%s", lease.Kind, lease.Key, lease.Ref)))
	}
	return refs
}

// evidenceRecordLimit bounds how many pair records one gate holds at once; the
// oldest record is dropped beyond it.
const evidenceRecordLimit = 32

// evidenceRecord holds what one evidence class observed for one pair of a
// declaration plan and one generation of the execution binding.
type evidenceRecord struct {
	class    evidenceClass
	identity pairIdentity
	findings []changeFinding
}

// pairIdentity names one pair of a declaration plan and one generation of the
// execution binding: the declaration it holds, its fingerprint and baseline,
// the native model instance each agent is bound to and the candidate resource
// registrations the binding carries.
type pairIdentity struct {
	declaration string
	fingerprint string
	baseline    string
	models      map[string]string
	leases      string
}

// identityOf renders the identity of the pair one call carries in.
func identityOf(plan state.OrgPlan, binding state.StatePlan) pairIdentity {
	identity := pairIdentity{
		declaration: fmt.Sprintf("%p", plan.Declaration),
		fingerprint: plan.Fingerprint,
		baseline:    plan.Baseline,
		models:      make(map[string]string, len(binding.Agents)),
		leases:      fmt.Sprintf("%v", binding.Leases),
	}
	for id, bound := range binding.Agents {
		identity.models[string(id)] = instanceRef(bound.Model)
	}
	return identity
}

// matches reports whether the recorded identity is the very pair one call
// carries in. Evidence recorded for one generation is never read as the
// evidence of another.
func (r *evidenceRecord) matches(identity pairIdentity) bool {
	if r.identity.declaration != identity.declaration ||
		r.identity.fingerprint != identity.fingerprint ||
		r.identity.baseline != identity.baseline ||
		r.identity.leases != identity.leases ||
		len(r.identity.models) != len(identity.models) {
		return false
	}
	for id, instance := range r.identity.models {
		if identity.models[id] != instance {
			return false
		}
	}
	return true
}

// remember files what one evidence class observed under the pair it ran over,
// replacing the record of an earlier run over the very same pair.
func (g *structuralGate) remember(record *evidenceRecord) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, held := range g.records {
		if held.class == record.class && held.matches(record.identity) {
			g.records[i] = record
			return
		}
	}
	g.records = append(g.records, record)
	if len(g.records) > evidenceRecordLimit {
		g.records = g.records[len(g.records)-evidenceRecordLimit:]
	}
}

// evidenceOf consumes what one evidence class recorded for the pair one call
// carries in. A gate that holds no record of that pair reports every declared
// change as missing evidence instead of driving the candidate binding again.
func (g *structuralGate) evidenceOf(class evidenceClass, plan state.OrgPlan, binding state.StatePlan) []changeFinding {
	identity := identityOf(plan, binding)
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, record := range g.records {
		if record.class == class && record.matches(identity) {
			return record.findings
		}
	}
	return unrecordedFindings(class, plan)
}

// unrecordedFindings names every judged declared change as missing evidence for
// an evidence class this gate holds no record of, so an unrecorded required
// check is answered as the absence it is.
func unrecordedFindings(class evidenceClass, plan state.OrgPlan) []changeFinding {
	var findings []changeFinding
	for _, change := range plan.Changes {
		agent := string(change.AgentID)
		if change.Kind == state.PlanChangeRemoved {
			findings = append(findings, changeFinding{agent: agent, dispo: findingExempt, label: "the change removes the agent and the check does not apply"})
			continue
		}
		findings = append(findings, changeFinding{
			agent: agent,
			dispo: findingMissing,
			label: "no " + string(class) + " evidence is recorded for this pair",
			cause: fmt.Errorf("%w: check %s: agent %s", ErrEvidenceMissing, class, agent),
		})
	}
	return findings
}

// instanceRef renders the runtime identity of one native model instance, so a
// record is matched to the very binding generation its evidence came from.
func instanceRef(native model.Model) string {
	value := reflect.ValueOf(native)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return fmt.Sprintf("%T:%#x", native, value.Pointer())
	default:
		return fmt.Sprintf("%T:%v", native, native)
	}
}

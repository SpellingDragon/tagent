package evolution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// gateProbe is the native model stand-in of one declared change: it answers the
// scripted value and counts every probe it served, so a segment never drives a
// binding the apply gate is only allowed to read.
type gateProbe struct {
	answer string
	calls  int
}

// GenerateContent streams one completed answer carrying the scripted value.
func (m *gateProbe) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	m.calls++
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{Content: m.answer}}}, Done: true}
	close(ch)
	return ch, nil
}

// Info reports the stand-in's descriptor.
func (m *gateProbe) Info() model.Info { return model.Info{Name: "gate-probe"} }

// gateDeclaration declares one agent carrying one evaluation fixture whose two
// judged faces hold the given values; an empty value leaves that face undeclared.
func gateDeclaration(expect, baseline string) *config.Declaration {
	params := map[string]string{
		CriterionParamID:   "c1",
		CriterionParamTask: "answer the gate probe",
	}
	if expect != "" {
		params[CriterionParamExpect] = expect
	}
	if baseline != "" {
		params[CriterionParamBaseline] = baseline
	}
	return &config.Declaration{
		Entry: "a",
		Agents: []config.AgentSpec{{
			ID:    "a",
			Tools: []config.ToolRef{{Name: EvaluationCriterionRef, Params: params}},
		}},
	}
}

// gatePair builds the declaration plan and one candidate binding generation the
// gate is handed: the plan names one updated agent and the binding binds it to the
// given model beside one candidate resource registration.
func gatePair(declaration *config.Declaration, fingerprint, baseline string, probe model.Model) (state.OrgPlan, state.StatePlan) {
	plan := state.OrgPlan{
		Declaration: declaration,
		Baseline:    baseline,
		Fingerprint: fingerprint,
		Changes:     []state.PlanChange{{Kind: state.PlanChangeUpdated, AgentID: event.AgentID("a")}},
	}
	binding := state.StatePlan{
		Plan:   plan,
		Agents: map[event.AgentID]state.AgentBinding{event.AgentID("a"): {Model: probe}},
		Leases: []state.LeaseRef{{Kind: "model", Key: "a", Ref: "lease:a"}},
	}
	return plan, binding
}

// mentionsRef reports whether one report cites the named evidence reference.
func mentionsRef(refs []EvidenceRef, want string) bool {
	for _, ref := range refs {
		if string(ref) == want {
			return true
		}
	}
	return false
}

// TestApplyGateAdoptsOnlyRecordedSegments pins the apply gate: it consumes the two checks' records, refuses an absent one, and probes nothing itself.
// - an apply gate over a pair no segment recorded refuses by name and probes nothing
// - an eval record alone still refuses, naming the missing benchmark check
// - once both segments recorded the pair, the apply gate passes on their evidence
// - the applied report cites the lease references and both checks' criteria
// - a plan carrying no change is exempt from the apply gate, never passing with empty evidence
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestApplyGateAdoptsOnlyRecordedSegments(t *testing.T) {
	ctx := context.Background()
	probe := &gateProbe{answer: "yes"}
	plan, binding := gatePair(gateDeclaration("yes", "yes"), "f1", "b1", probe)
	gate := NewStructuralGate()

	report, err := gate.ApplyGate(ctx, plan, binding)
	if !errors.Is(err, ErrCheckMissing) {
		t.Fatalf("apply gate before any record = %v, want ErrCheckMissing", err)
	}
	if report.Verdict != GateVerdictReject {
		t.Fatalf("verdict = %q, want the publication refused for the missing checks", report.Verdict)
	}
	if probe.calls != 0 {
		t.Fatalf("probe calls = %d, want the apply gate to drive no candidate of its own", probe.calls)
	}

	evaluated, err := gate.Eval(ctx, plan, binding)
	if err != nil {
		t.Fatalf("evaluate the pair: %v", err)
	}
	if evaluated.Verdict != GateVerdictPass {
		t.Fatalf("eval verdict = %q, want the met criterion to pass", evaluated.Verdict)
	}
	if probe.calls != 1 {
		t.Fatalf("probe calls after eval = %d, want the one evaluated case", probe.calls)
	}

	report, err = gate.ApplyGate(ctx, plan, binding)
	if !errors.Is(err, ErrCheckMissing) {
		t.Fatalf("apply gate with only eval recorded = %v, want ErrCheckMissing", err)
	}
	if report.Verdict != GateVerdictReject || !strings.Contains(report.Reason, "check benchmark: agent a") {
		t.Fatalf("report = %+v, want the missing benchmark check named", report)
	}
	if probe.calls != 1 {
		t.Fatalf("probe calls = %d, want the refused apply gate to probe nothing", probe.calls)
	}

	benchmarked, err := gate.Benchmark(ctx, plan, binding)
	if err != nil {
		t.Fatalf("benchmark the pair: %v", err)
	}
	if benchmarked.Verdict != GateVerdictPass {
		t.Fatalf("benchmark verdict = %q, want the met baseline to pass", benchmarked.Verdict)
	}

	applied, err := gate.ApplyGate(ctx, plan, binding)
	if err != nil {
		t.Fatalf("apply the recorded pair: %v", err)
	}
	if applied.Verdict != GateVerdictPass {
		t.Fatalf("applied verdict = %q, want a pass over both recorded checks", applied.Verdict)
	}
	if applied.Scope != "declaration:f1" {
		t.Fatalf("applied scope = %q, want the declaration scope the checks cover", applied.Scope)
	}
	for _, want := range []string{"lease:model:a:lease:a", "criterion:eval:a:c1", "criterion:benchmark:a:c1"} {
		if !mentionsRef(applied.EvidenceRefs, want) {
			t.Fatalf("applied evidence = %v, want %q cited", applied.EvidenceRefs, want)
		}
	}
	if probe.calls != 2 {
		t.Fatalf("probe calls after both segments = %d, want one per recorded check", probe.calls)
	}

	emptyPlan := state.OrgPlan{Declaration: &config.Declaration{Entry: "a"}, Fingerprint: "f0", Baseline: "b0"}
	exempt, err := gate.ApplyGate(ctx, emptyPlan, state.StatePlan{Plan: emptyPlan})
	if err != nil {
		t.Fatalf("apply gate over a plan carrying no change: %v", err)
	}
	if exempt.Verdict != GateVerdictNotApplicable || len(exempt.EvidenceRefs) != 0 {
		t.Fatalf("report = %+v, want an exemption without evidence instead of a pass", exempt)
	}
}

// TestEvidenceNeverCrossesGenerations pins the pair identity: a record for another model instance, declaration pointer, fingerprint or baseline is never adopted.
// - a rebuilt model instance is another generation and its eval record is not adopted
// - the same content behind another declaration pointer is another pair
// - another fingerprint and another baseline each name another pair
// - evaluating that generation records it, so the eval check alone still leaves the benchmark check missing
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestEvidenceNeverCrossesGenerations(t *testing.T) {
	ctx := context.Background()
	declaration := gateDeclaration("yes", "yes")
	first := &gateProbe{answer: "yes"}
	plan, binding := gatePair(declaration, "f1", "b1", first)
	gate := NewStructuralGate()
	if report, err := gate.Eval(ctx, plan, binding); err != nil || report.Verdict != GateVerdictPass {
		t.Fatalf("record the first generation = (%+v, %v), want a passing eval", report, err)
	}

	t.Run("a rebuilt model instance is another generation", func(t *testing.T) {
		second := &gateProbe{answer: "yes"}
		rebuilt := binding
		rebuilt.Agents = map[event.AgentID]state.AgentBinding{event.AgentID("a"): {Model: second}}
		report, err := gate.ApplyGate(ctx, plan, rebuilt)
		if !errors.Is(err, ErrCheckMissing) || report.Verdict != GateVerdictReject {
			t.Fatalf("apply a rebuilt generation = (%+v, %v), want the missing checks refused", report, err)
		}
		if second.calls != 0 {
			t.Fatalf("rebuilt model calls = %d, want the refusal to probe no candidate", second.calls)
		}
		if report, err := gate.Eval(ctx, plan, rebuilt); err != nil || report.Verdict != GateVerdictPass {
			t.Fatalf("evaluate the rebuilt generation = (%+v, %v), want its own record", report, err)
		}
		if report, err := gate.ApplyGate(ctx, plan, rebuilt); !errors.Is(err, ErrCheckMissing) || report.Verdict != GateVerdictReject {
			t.Fatalf("apply with only the rebuilt eval recorded = (%+v, %v), want the benchmark check still missing", report, err)
		}
	})

	t.Run("the same content behind another declaration pointer is another pair", func(t *testing.T) {
		clone := *declaration
		clonePlan := plan
		clonePlan.Declaration = &clone
		cloneBinding := binding
		cloneBinding.Plan = clonePlan
		report, err := gate.ApplyGate(ctx, clonePlan, cloneBinding)
		if !errors.Is(err, ErrCheckMissing) || report.Verdict != GateVerdictReject {
			t.Fatalf("apply a cloned declaration = (%+v, %v), want the earlier record left behind", report, err)
		}
	})

	t.Run("another fingerprint or baseline is another pair", func(t *testing.T) {
		for name, mutate := range map[string]func(*state.OrgPlan){
			"fingerprint": func(plan *state.OrgPlan) { plan.Fingerprint = "f2" },
			"baseline":    func(plan *state.OrgPlan) { plan.Baseline = "b2" },
		} {
			otherPlan := plan
			mutate(&otherPlan)
			otherBinding := binding
			otherBinding.Plan = otherPlan
			report, err := gate.ApplyGate(ctx, otherPlan, otherBinding)
			if !errors.Is(err, ErrCheckMissing) || report.Verdict != GateVerdictReject {
				t.Fatalf("%s moved = (%+v, %v), want the earlier record left behind", name, report, err)
			}
		}
	})
}

// TestSegmentRefusesADivergedPair pins admission: a binding prepared from another declaration plan, or no declaration, fails by name before any evidence work.
// - a binding whose plan diverged from the judged plan is refused by name for that segment
// - the diverged pair returns no report and probes no candidate
// - a plan carrying no declaration is refused by name at the apply gate too
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestSegmentRefusesADivergedPair(t *testing.T) {
	ctx := context.Background()
	probe := &gateProbe{answer: "yes"}
	plan, binding := gatePair(gateDeclaration("yes", "yes"), "f1", "b1", probe)
	gate := NewStructuralGate()

	diverged := binding
	diverged.Plan.Fingerprint = "f2"
	report, err := gate.Eval(ctx, plan, diverged)
	var failure *GateError
	if !errors.As(err, &failure) {
		t.Fatalf("diverged pair = %v, want a named gate failure", err)
	}
	if failure.Segment != SegmentEval {
		t.Fatalf("failed segment = %q, want the eval segment", failure.Segment)
	}
	if !errors.Is(err, ErrDeclarationDiverged) {
		t.Fatalf("diverged pair = %v, want ErrDeclarationDiverged reachable", err)
	}
	if report.Verdict != "" || len(report.EvidenceRefs) != 0 {
		t.Fatalf("report = %+v, want the zero report a refused pair never fills", report)
	}
	if probe.calls != 0 {
		t.Fatalf("probe calls = %d, want no candidate driven before admission", probe.calls)
	}

	absent := state.OrgPlan{Fingerprint: "f1"}
	report, err = gate.ApplyGate(ctx, absent, state.StatePlan{Plan: absent})
	if !errors.As(err, &failure) || failure.Segment != SegmentApplyGate {
		t.Fatalf("absent declaration = %v, want a named apply gate failure", err)
	}
	if !errors.Is(err, state.ErrPlanMissing) {
		t.Fatalf("absent declaration = %v, want state.ErrPlanMissing reachable", err)
	}
	if report.Verdict != "" {
		t.Fatalf("verdict = %q, want no verdict over a pair that never stood", report.Verdict)
	}
}

// TestContradictedCriterionIsNeverAPass pins the contradiction rule: a disagreeing answer refuses, and an undeclared face is missing evidence.
// - a contradicting answer refuses the eval segment by name and cites the criterion and terminal
// - the benchmark face refuses the same contradiction under its own criterion
// - the apply gate keeps the contradiction reachable instead of flattening it
// - an undeclared expect value is missing evidence and probes nothing
// - an undeclared baseline value is missing evidence under the benchmark check
// - the apply gate refuses the missing required checks, keeping both causes reachable
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestContradictedCriterionIsNeverAPass(t *testing.T) {
	ctx := context.Background()
	plan, binding := gatePair(gateDeclaration("yes", "yes"), "f1", "b1", &gateProbe{answer: "no"})
	gate := NewStructuralGate()

	evaluated, err := gate.Eval(ctx, plan, binding)
	if evaluated.Verdict != GateVerdictReject {
		t.Fatalf("eval verdict = %q, want the contradicted criterion refused", evaluated.Verdict)
	}
	if !errors.Is(err, ErrCriterionNotMet) {
		t.Fatalf("eval contradiction = %v, want ErrCriterionNotMet", err)
	}
	if !strings.Contains(evaluated.Reason, "contradicts the declared evaluation criterion") {
		t.Fatalf("reason = %q, want the contradicted criterion named", evaluated.Reason)
	}
	for _, want := range []string{"criterion:eval:a:c1", "terminal:eval:a:c1:completed"} {
		if !mentionsRef(evaluated.EvidenceRefs, want) {
			t.Fatalf("eval evidence = %v, want %q cited", evaluated.EvidenceRefs, want)
		}
	}

	if _, err := gate.Benchmark(ctx, plan, binding); !errors.Is(err, ErrCriterionNotMet) {
		t.Fatalf("benchmark contradiction = %v, want its own refusal", err)
	}
	applied, err := gate.ApplyGate(ctx, plan, binding)
	if applied.Verdict != GateVerdictReject || !errors.Is(err, ErrCriterionNotMet) {
		t.Fatalf("apply a contradicted pair = (%+v, %v), want the contradiction still reachable", applied, err)
	}

	t.Run("an undeclared face is missing evidence", func(t *testing.T) {
		probe := &gateProbe{answer: "yes"}
		plan, binding := gatePair(gateDeclaration("", ""), "f2", "b2", probe)
		gate := NewStructuralGate()

		evaluated, err := gate.Eval(ctx, plan, binding)
		if evaluated.Verdict != GateVerdictInsufficientEvidence || !errors.Is(err, ErrCriterionMissing) {
			t.Fatalf("eval an undeclared expect value = (%+v, %v), want missing evidence", evaluated, err)
		}
		if probe.calls != 0 {
			t.Fatalf("probe calls = %d, want the missing criterion judged before any probe", probe.calls)
		}

		benchmarked, err := gate.Benchmark(ctx, plan, binding)
		if benchmarked.Verdict != GateVerdictInsufficientEvidence || !errors.Is(err, ErrCriterionMissing) {
			t.Fatalf("benchmark an undeclared baseline = (%+v, %v), want missing evidence", benchmarked, err)
		}
		if probe.calls != 0 {
			t.Fatalf("probe calls = %d, want the missing baseline judged before any probe", probe.calls)
		}

		applied, err := gate.ApplyGate(ctx, plan, binding)
		if applied.Verdict != GateVerdictReject {
			t.Fatalf("apply verdict = %q, want the missing required checks refused", applied.Verdict)
		}
		if !errors.Is(err, ErrCheckMissing) || !errors.Is(err, ErrCriterionMissing) {
			t.Fatalf("apply an undeclared pair = %v, want both the missing check and the missing criterion", err)
		}
	})
}

// TestPolicyFilesEachCheckUnderItsName pins the adapter: each segment lands under its own check name, and missing evidence is filed unanswered.
// - a passing candidate files pass under the eval, benchmark and apply gate names
// - each filed answer carries its declaration scope and non-empty evidence
// - the adapter drives one probe per evidence class and none for the apply gate
// - missing evidence is filed unanswered for both checks and a refusal for the apply gate
// - the returned error keeps both the missing criterion and the missing check reachable
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestPolicyFilesEachCheckUnderItsName(t *testing.T) {
	ctx := context.Background()
	probe := &gateProbe{answer: "yes"}
	_, binding := gatePair(gateDeclaration("yes", "yes"), "f1", "b1", probe)

	answers, err := NewPolicy(NewStructuralGate()).Check(ctx, binding)
	if err != nil {
		t.Fatalf("check a passing candidate: %v", err)
	}
	for name, want := range map[state.CheckName]state.CheckVerdict{
		CheckEval:      state.CheckPass,
		CheckBenchmark: state.CheckPass,
		CheckApplyGate: state.CheckPass,
	} {
		answer, ok := answers[name]
		if !ok {
			t.Fatalf("answers = %v, want the %q check filed", answers, name)
		}
		if answer.Name != name || answer.Verdict != want {
			t.Fatalf("answer %q = %+v, want the %q check filed under its own name", name, answer, name)
		}
		if answer.Scope != "declaration:f1" || len(answer.Evidence) == 0 {
			t.Fatalf("answer %q = %+v, want its scope and evidence beside the disposition", name, answer)
		}
	}
	if probe.calls != 2 {
		t.Fatalf("probe calls = %d, want one per evidence class and none for the apply gate", probe.calls)
	}

	t.Run("missing evidence is filed unanswered", func(t *testing.T) {
		_, binding := gatePair(gateDeclaration("", ""), "f2", "b2", &gateProbe{answer: "yes"})
		answers, err := NewPolicy(NewStructuralGate()).Check(ctx, binding)
		if err == nil {
			t.Fatal("checking a candidate without criteria reported no failure")
		}
		if answers[CheckEval].Verdict != state.CheckUnanswered || answers[CheckBenchmark].Verdict != state.CheckUnanswered {
			t.Fatalf("answers = %+v, want both evidence checks filed unanswered", answers)
		}
		if answers[CheckApplyGate].Verdict != state.CheckReject {
			t.Fatalf("apply gate answer = %+v, want the missing checks refused", answers[CheckApplyGate])
		}
		if !errors.Is(err, ErrCriterionMissing) || !errors.Is(err, ErrCheckMissing) {
			t.Fatalf("failure = %v, want both the missing criterion and the missing check reachable", err)
		}
	})
}

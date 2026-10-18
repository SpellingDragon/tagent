// Package evolution holds the integration tests of the evolution cluster over
// the publisher and the structural gate surfaces.
package evolution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"

	evo "github.com/SpellingDragon/tagent/evolution"

	orgstate "github.com/SpellingDragon/tagent/state"
)

// probeModel is the deterministic stand-in for one agent's native model: it
// records every probe marker it receives and streams the scripted response for
// it, or fails at the entry when an entry error is scripted.
type probeModel struct {
	name     string
	entryErr error
	streams  func(mark string) []*model.Response

	marks []string
}

// GenerateContent records the probe marker and streams the scripted responses.
func (m *probeModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mark(req.Messages[0].Content)
	if m.entryErr != nil {
		return nil, m.entryErr
	}
	var responses []*model.Response
	if m.streams != nil {
		responses = m.streams(req.Messages[0].Content)
	}
	ch := make(chan *model.Response, len(responses))
	for _, resp := range responses {
		ch <- resp
	}
	close(ch)
	return ch, nil
}

// Info reports the stand-in's descriptor.
func (m *probeModel) Info() model.Info {
	return model.Info{Name: m.name}
}

func (m *probeModel) mark(mark string) {
	m.marks = append(m.marks, mark)
}

// seen returns the probe markers the model received, in order.
func (m *probeModel) seen() string {
	return strings.Join(m.marks, " | ")
}

// completeResponse builds the response of a complete answer.
func completeResponse(content string) *model.Response {
	return &model.Response{Choices: []model.Choice{{Message: model.Message{Content: content}}}, Done: true}
}

// partialResponse builds the response of an answer carrying only partial output.
func partialResponse(content string) *model.Response {
	return &model.Response{Choices: []model.Choice{{Message: model.Message{Content: content}}}, IsPartial: true}
}

// releaseLog records the release of each candidate resource by name, in the
// order the releases run.
type releaseLog struct {
	names []string
}

// track returns the release of one named resource that reports itself here.
func (l *releaseLog) track(name string) func(context.Context) error {
	return func(context.Context) error {
		l.names = append(l.names, name)
		return nil
	}
}

// recordSink records the publication records committed through it.
type recordSink struct {
	records []orgstate.PublicationRecord
}

// CommitPublication records one publication record.
func (s *recordSink) CommitPublication(ctx context.Context, record orgstate.PublicationRecord) error {
	s.records = append(s.records, record)
	return nil
}

// fixture is one evaluation case one agent declares: the task its candidate
// binding answers and the values the evaluation and benchmark faces judge the
// produced answer against.
type fixture struct {
	id       string
	task     string
	expect   string
	baseline string
}

// fixturesByAgent is the declared evaluation fixture set, keyed by agent id.
type fixturesByAgent map[string][]fixture

// criterionRefs compiles one agent's fixtures into the evaluation references
// its declaration carries.
func (f fixturesByAgent) criterionRefs(agent string) []config.ToolRef {
	var refs []config.ToolRef
	for _, item := range f[agent] {
		refs = append(refs, config.ToolRef{Name: evo.EvaluationCriterionRef, Params: map[string]string{
			evo.CriterionParamID:       item.id,
			evo.CriterionParamTask:     item.task,
			evo.CriterionParamExpect:   item.expect,
			evo.CriterionParamBaseline: item.baseline,
		}})
	}
	return refs
}

// declared compiles one declaration plan over the named agents and changes.
func declared(fingerprint, baseline string, ids []string, changes []orgstate.PlanChange, fixtures fixturesByAgent) orgstate.OrgPlan {
	specs := make([]config.AgentSpec, 0, len(ids))
	for _, id := range ids {
		specs = append(specs, config.AgentSpec{ID: id, Model: "model-" + id, Tools: fixtures.criterionRefs(id)})
	}
	return orgstate.OrgPlan{
		Declaration: &config.Declaration{Entry: ids[0], Agents: specs},
		Baseline:    baseline,
		Fingerprint: fingerprint,
		Changes:     changes,
		NextPrompt:  "prompt-1",
	}
}

// leased registers one candidate resource per name, in order, beside the log
// their releases report to.
func leased(names ...string) (*orgstate.Leases, *releaseLog) {
	leases := orgstate.NewLeases()
	log := &releaseLog{}
	for _, name := range names {
		leases.Register("model", name, log.track(name))
	}
	return leases, log
}

// candidate is one declaration plan beside the execution binding prepared from
// it and the models its agents are bound to.
type candidate struct {
	plan    orgstate.OrgPlan
	binding orgstate.StatePlan
	models  map[string]*probeModel
}

// prepare instantiates the candidate binding of the declared agents beside their
// stand-in models and the candidate resource registrations handed in.
func prepare(t *testing.T, fingerprint, baseline string, models []*probeModel, changes []orgstate.PlanChange, leases *orgstate.Leases, fixtures fixturesByAgent) candidate {
	t.Helper()
	ids := make([]string, 0, len(models))
	bindings := map[event.AgentID]orgstate.AgentBinding{}
	byName := map[string]*probeModel{}
	for _, m := range models {
		ids = append(ids, m.name)
		bindings[event.AgentID(m.name)] = orgstate.AgentBinding{Model: m}
		byName[m.name] = m
	}
	plan := declared(fingerprint, baseline, ids, changes, fixtures)
	built, err := orgstate.NewStatePlan(plan, bindings, leases.Refs())
	if err != nil {
		t.Fatalf("instantiate the candidate plan: %v", err)
	}
	return candidate{plan: plan, binding: built, models: byName}
}

// refTexts renders the evidence references of one report in order.
func refTexts(refs []evo.EvidenceRef) string {
	texts := make([]string, 0, len(refs))
	for _, ref := range refs {
		texts = append(texts, string(ref))
	}
	return strings.Join(texts, " | ")
}

// chain lists every error the chain reaches structurally: the single-cause
// Unwrap and the multi-cause Unwrap alike, in walk order.
func chain(err error) []error {
	if err == nil {
		return nil
	}
	out := []error{err}
	switch unwrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range unwrapped.Unwrap() {
			out = append(out, chain(cause)...)
		}
	case interface{ Unwrap() error }:
		out = append(out, chain(unwrapped.Unwrap())...)
	}
	return out
}

// reachable reports whether the chain holds the target as the very error it is.
// Reachability is decided on the chain structure, never on rendered text.
func reachable(err, target error) bool {
	for _, cause := range chain(err) {
		if cause == target {
			return true
		}
	}
	return false
}

// mustNotPass fails the case when a verdict reports a pass.
func mustNotPass(t *testing.T, label string, verdict evo.GateVerdict) {
	t.Helper()
	if verdict == evo.GateVerdictPass {
		t.Fatalf("%s must never report a pass, got %q", label, verdict)
	}
}

// TestStructuralEvaluation checks the structural gate over one declaration plan and one generation of the prepared execution binding.
// - Eval, Benchmark and ApplyGate consume one declaration plan and one generation of the binding and cite both in every report
// - Eval and Benchmark judge produced answers against the declared evaluation fixtures
// - the apply gate consumes the recorded evaluation evidence and drives the binding no more
// - a produced answer contradicting its declared criterion yields a rejection and never a pass
// - evidence that is missing or unreachable yields insufficient evidence and never a pass
// - a gate failure keeps its named root cause reachable through errors.Is and errors.As over the Unwrap chain rather than rendered text
// - the gate reads the pair it is handed, holds no second configuration and refuses a diverged pair outright
// - the governance answers file each disposition under its check name, and a publication the gate refuses keeps the state it had
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestStructuralEvaluation(t *testing.T) {
	t.Run("one declaration plan and one generation of the binding reach every segment", func(t *testing.T) {
		ctx := context.Background()
		a1 := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("answer-a1")}
		}}
		a2 := &probeModel{name: "a2", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("answer-a2")}
		}}
		fixtures := fixturesByAgent{
			"a1": {{id: "c1", task: "produce the answer of a1", expect: "answer-a1", baseline: "answer-a1"}},
			"a2": {{id: "c1", task: "produce the answer of a2", expect: "answer-a2", baseline: "answer-a2"}},
		}
		leases, _ := leased("model-a1", "model-a2")
		cand := prepare(t, "fp-1", "baseline-1", []*probeModel{a1, a2}, []orgstate.PlanChange{
			{Kind: orgstate.PlanChangeAdded, AgentID: "a1"},
			{Kind: orgstate.PlanChangeUpdated, AgentID: "a2"},
		}, leases, fixtures)
		gate := evo.NewStructuralGate()

		eval, err := gate.Eval(ctx, cand.plan, cand.binding)
		if err != nil {
			t.Fatalf("eval over complete evidence must not fail: %v", err)
		}
		benchmark, err := gate.Benchmark(ctx, cand.plan, cand.binding)
		if err != nil {
			t.Fatalf("benchmark over complete evidence must not fail: %v", err)
		}
		apply, err := gate.ApplyGate(ctx, cand.plan, cand.binding)
		if err != nil {
			t.Fatalf("apply gate over complete evidence must not fail: %v", err)
		}
		for _, named := range []struct {
			name   string
			report evo.GateReport
		}{{"eval", eval}, {"benchmark", benchmark}, {"apply gate", apply}} {
			if named.report.Verdict != evo.GateVerdictPass {
				t.Fatalf("%s over complete evidence reports %q", named.name, named.report.Verdict)
			}
			if named.report.Scope != "declaration:fp-1" {
				t.Fatalf("%s covers scope %q, the declaration it consumes is declaration:fp-1", named.name, named.report.Scope)
			}
		}

		const leases12 = "lease:model:model-a1:lease-1 | lease:model:model-a2:lease-2"
		if got := refTexts(eval.EvidenceRefs); got != leases12+" | criterion:eval:a1:c1 | terminal:eval:a1:c1:completed | criterion:eval:a2:c1 | terminal:eval:a2:c1:completed" {
			t.Fatalf("eval cites %q", got)
		}
		if got := refTexts(benchmark.EvidenceRefs); got != leases12+" | criterion:benchmark:a1:c1 | terminal:benchmark:a1:c1:completed | criterion:benchmark:a2:c1 | terminal:benchmark:a2:c1:completed" {
			t.Fatalf("benchmark cites %q", got)
		}
		if got := refTexts(apply.EvidenceRefs); got != leases12+" | criterion:eval:a1:c1 | terminal:eval:a1:c1:completed | criterion:eval:a2:c1 | terminal:eval:a2:c1:completed | criterion:benchmark:a1:c1 | terminal:benchmark:a1:c1:completed | criterion:benchmark:a2:c1 | terminal:benchmark:a2:c1:completed" {
			t.Fatalf("apply gate cites %q", got)
		}

		wantA1 := "gate-eval eval a1 c1 fp-1 | gate-eval benchmark a1 c1 fp-1"
		if got := a1.seen(); got != wantA1 {
			t.Fatalf("agent a1 binding saw %q, want %q", got, wantA1)
		}
		wantA2 := "gate-eval eval a2 c1 fp-1 | gate-eval benchmark a2 c1 fp-1"
		if got := a2.seen(); got != wantA2 {
			t.Fatalf("agent a2 binding saw %q, want %q", got, wantA2)
		}
	})

	t.Run("each face judges produced answers against its own declared value", func(t *testing.T) {
		ctx := context.Background()
		fixtures := fixturesByAgent{"a1": {{id: "c1", task: "produce the answer", expect: "answer-eval", baseline: "answer-bench"}}}
		changes := []orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}
		gate := evo.NewStructuralGate()

		both := &probeModel{name: "a1", streams: func(mark string) []*model.Response {
			if strings.Contains(mark, " eval ") {
				return []*model.Response{completeResponse("answer-eval")}
			}
			return []*model.Response{completeResponse("answer-bench")}
		}}
		bothLeases, _ := leased("model-a1")
		bothCand := prepare(t, "fp-faces", "baseline-1", []*probeModel{both}, changes, bothLeases, fixtures)
		eval, err := gate.Eval(ctx, bothCand.plan, bothCand.binding)
		if err != nil || eval.Verdict != evo.GateVerdictPass {
			t.Fatalf("the evaluation face judges its own declared value, got %q beside %v", eval.Verdict, err)
		}
		benchmark, err := gate.Benchmark(ctx, bothCand.plan, bothCand.binding)
		if err != nil || benchmark.Verdict != evo.GateVerdictPass {
			t.Fatalf("the benchmark face judges its own declared value, got %q beside %v", benchmark.Verdict, err)
		}

		evalOnly := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("answer-eval")}
		}}
		oneLeases, _ := leased("model-a1")
		oneCand := prepare(t, "fp-oneface", "baseline-1", []*probeModel{evalOnly}, changes, oneLeases, fixtures)
		report, err := gate.Eval(ctx, oneCand.plan, oneCand.binding)
		if err != nil || report.Verdict != evo.GateVerdictPass {
			t.Fatalf("the evaluation face accepts the answer of its own value, got %q beside %v", report.Verdict, err)
		}
		report, err = gate.Benchmark(ctx, oneCand.plan, oneCand.binding)
		mustNotPass(t, "the benchmark face over an answer of the other face's value", report.Verdict)
		if report.Verdict != evo.GateVerdictReject || !errors.Is(err, evo.ErrCriterionNotMet) {
			t.Fatalf("the benchmark face rejects the answer against its own declared value, got %q beside %v", report.Verdict, err)
		}
		if !strings.Contains(err.Error(), `produced "answer-eval"`) || !strings.Contains(err.Error(), `declared "answer-bench"`) {
			t.Fatalf("the refusal names the produced answer beside the declared value, got %v", err)
		}
	})

	t.Run("evidence that is missing is never a pass", func(t *testing.T) {
		ctx := context.Background()
		gate := evo.NewStructuralGate()
		changes := []orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}

		quiet := &probeModel{name: "a1"}
		judged := fixturesByAgent{"a1": {{id: "c1", task: "produce the answer", expect: "answer-a1", baseline: "answer-a1"}}}
		quietLeases, _ := leased("model-a1")
		quietCand := prepare(t, "fp-missing", "baseline-1", []*probeModel{quiet}, changes, quietLeases, judged)
		for _, segment := range []struct {
			name   string
			report func() (evo.GateReport, error)
		}{
			{"eval", func() (evo.GateReport, error) { return gate.Eval(ctx, quietCand.plan, quietCand.binding) }},
			{"benchmark", func() (evo.GateReport, error) { return gate.Benchmark(ctx, quietCand.plan, quietCand.binding) }},
		} {
			report, err := segment.report()
			mustNotPass(t, segment.name+" over an answer without terminal evidence", report.Verdict)
			if report.Verdict != evo.GateVerdictInsufficientEvidence {
				t.Fatalf("%s over an answer without terminal evidence reports %q", segment.name, report.Verdict)
			}
			if !errors.Is(err, evo.ErrEvidenceMissing) || !errors.Is(err, model.ErrNoTerminal) {
				t.Fatalf("%s must keep its named missing evidence reachable, got %v", segment.name, err)
			}
		}
		apply, err := gate.ApplyGate(ctx, quietCand.plan, quietCand.binding)
		mustNotPass(t, "apply gate over an answer without terminal evidence", apply.Verdict)
		if apply.Verdict != evo.GateVerdictReject {
			t.Fatalf("a missing required check refuses the publication, apply gate reports %q", apply.Verdict)
		}
		if !errors.Is(err, evo.ErrCheckMissing) {
			t.Fatalf("apply gate must name the missing required check, got %v", err)
		}

		criterionlessCand := prepare(t, "fp-criterionless", "baseline-1", []*probeModel{quiet}, changes, quietLeases, nil)
		facelessCand := prepare(t, "fp-faceless", "baseline-1", []*probeModel{quiet}, changes, quietLeases, fixturesByAgent{
			"a1": {{id: "c1", task: "produce the answer", expect: "", baseline: ""}},
		})
		for _, bare := range []struct {
			name string
			cand candidate
		}{
			{"a declaration carrying no evaluation criterion", criterionlessCand},
			{"a declaration carrying no value on either judged face", facelessCand},
		} {
			for _, segment := range []struct {
				name   string
				report func() (evo.GateReport, error)
			}{
				{"eval", func() (evo.GateReport, error) { return gate.Eval(ctx, bare.cand.plan, bare.cand.binding) }},
				{"benchmark", func() (evo.GateReport, error) { return gate.Benchmark(ctx, bare.cand.plan, bare.cand.binding) }},
			} {
				report, err := segment.report()
				mustNotPass(t, segment.name+" over "+bare.name, report.Verdict)
				if report.Verdict != evo.GateVerdictInsufficientEvidence || !errors.Is(err, evo.ErrCriterionMissing) {
					t.Fatalf("%s over %s reports %q beside %v", segment.name, bare.name, report.Verdict, err)
				}
			}
			apply, err := gate.ApplyGate(ctx, bare.cand.plan, bare.cand.binding)
			mustNotPass(t, "apply gate over "+bare.name, apply.Verdict)
			if apply.Verdict != evo.GateVerdictReject || !errors.Is(err, evo.ErrCheckMissing) || !errors.Is(err, evo.ErrCriterionMissing) {
				t.Fatalf("apply gate over %s reports %q beside %v", bare.name, apply.Verdict, err)
			}
		}

		plan := declared("fp-unbound", "baseline-1", []string{"a1"}, changes, judged)
		unbound := orgstate.StatePlan{Plan: plan, Agents: map[event.AgentID]orgstate.AgentBinding{}, Leases: []orgstate.LeaseRef{}}
		report, err := gate.Eval(ctx, plan, unbound)
		mustNotPass(t, "eval over an unbound declared agent", report.Verdict)
		if report.Verdict != evo.GateVerdictInsufficientEvidence || !errors.Is(err, orgstate.ErrAgentBindingMissing) {
			t.Fatalf("eval over an unbound declared agent reports %q beside %v", report.Verdict, err)
		}

		emptyBinding := orgstate.StatePlan{Plan: plan, Agents: map[event.AgentID]orgstate.AgentBinding{"a1": {}}, Leases: []orgstate.LeaseRef{}}
		report, err = gate.Eval(ctx, plan, emptyBinding)
		mustNotPass(t, "eval over a binding that carries no model", report.Verdict)
		if report.Verdict != evo.GateVerdictInsufficientEvidence || !errors.Is(err, orgstate.ErrBindingIncomplete) {
			t.Fatalf("eval over a binding that carries no model reports %q beside %v", report.Verdict, err)
		}

		partial := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{partialResponse("half")}
		}}
		partialLeases, _ := leased("model-a1")
		partialCand := prepare(t, "fp-partial", "baseline-1", []*probeModel{partial}, changes, partialLeases, judged)
		report, err = gate.Eval(ctx, partialCand.plan, partialCand.binding)
		mustNotPass(t, "eval over a partial-only answer", report.Verdict)
		if report.Verdict != evo.GateVerdictInsufficientEvidence || !errors.Is(err, evo.ErrEvidenceMissing) {
			t.Fatalf("eval over a partial-only answer reports %q beside %v", report.Verdict, err)
		}

		removedLeases, _ := leased("model-a1")
		neverProbed := &probeModel{name: "a1"}
		removedCand := prepare(t, "fp-removed", "baseline-1", []*probeModel{neverProbed}, []orgstate.PlanChange{
			{Kind: orgstate.PlanChangeRemoved, AgentID: "a1"},
		}, removedLeases, nil)
		report, err = gate.Eval(ctx, removedCand.plan, removedCand.binding)
		mustNotPass(t, "eval over a change set of removals", report.Verdict)
		if report.Verdict != evo.GateVerdictNotApplicable || err != nil {
			t.Fatalf("eval over a change set of removals reports %q beside %v", report.Verdict, err)
		}
		if report.Reason == "" || report.Scope != "declaration:fp-removed" {
			t.Fatalf("an exemption must record its reason and scope, got reason %q scope %q", report.Reason, report.Scope)
		}
		if len(report.EvidenceRefs) != 0 {
			t.Fatalf("an exemption counts as no evidence, got %q", refTexts(report.EvidenceRefs))
		}
		if neverProbed.seen() != "" {
			t.Fatalf("a removed agent is probed by no segment, got %q", neverProbed.seen())
		}

		noChange := declared("fp-idle", "baseline-1", []string{"a1"}, nil, judged)
		report, err = gate.Benchmark(ctx, noChange, orgstate.StatePlan{Plan: noChange, Agents: map[event.AgentID]orgstate.AgentBinding{"a1": {Model: quiet}}, Leases: []orgstate.LeaseRef{}})
		mustNotPass(t, "benchmark over a plan that carries no change", report.Verdict)
		if report.Verdict != evo.GateVerdictNotApplicable || err != nil {
			t.Fatalf("benchmark over a plan that carries no change reports %q beside %v", report.Verdict, err)
		}
	})

	t.Run("a gate failure keeps its named root cause reachable", func(t *testing.T) {
		ctx := context.Background()
		gate := evo.NewStructuralGate()
		changes := []orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}

		boom := errors.New("stub probe entry failure")
		lookalike := errors.New("stub probe entry failure")
		judged := fixturesByAgent{"a1": {{id: "c1", task: "produce the answer", expect: "answer-a1", baseline: "answer-a1"}}}
		entryFailing := &probeModel{name: "a1", entryErr: boom}
		entryLeases, _ := leased("model-a1")
		entryCand := prepare(t, "fp-entry", "baseline-1", []*probeModel{entryFailing}, changes, entryLeases, judged)
		report, err := gate.Eval(ctx, entryCand.plan, entryCand.binding)
		if report.Verdict != evo.GateVerdictReject {
			t.Fatalf("a refused probe reports %q", report.Verdict)
		}
		if !errors.Is(err, evo.ErrCandidateRefused) || !errors.Is(err, boom) {
			t.Fatalf("the refusal must reach its named root, got %v", err)
		}
		var gateErr *evo.GateError
		if !errors.As(err, &gateErr) || gateErr.Segment != evo.SegmentEval || gateErr.Scope != "declaration:fp-entry" {
			t.Fatalf("the failure must carry its segment and scope, got %#v", gateErr)
		}
		if !reachable(err, boom) {
			t.Fatalf("the Unwrap chain must reach the entry error itself, chain %v", chain(err))
		}
		if !strings.Contains(err.Error(), lookalike.Error()) || errors.Is(err, lookalike) {
			t.Fatalf("reachability must hold on the chain identity and not on rendered text, got %v", err)
		}

		responseErr := &model.ResponseError{Message: "provider refused the probe", Type: model.ErrorTypeStreamError}
		refusing := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{{Done: true, Error: responseErr}}
		}}
		refuseLeases, _ := leased("model-a1")
		refuseCand := prepare(t, "fp-refused", "baseline-1", []*probeModel{refusing}, changes, refuseLeases, judged)
		report, err = gate.Eval(ctx, refuseCand.plan, refuseCand.binding)
		if report.Verdict != evo.GateVerdictReject || !errors.Is(err, evo.ErrCandidateRefused) {
			t.Fatalf("a response-carried refusal reports %q beside %v", report.Verdict, err)
		}
		if !reachable(err, responseErr) {
			t.Fatalf("the Unwrap chain must reach the response error itself, chain %v", chain(err))
		}

		quiet := &probeModel{name: "a1"}
		quietLeases, _ := leased("model-a1")
		quietCand := prepare(t, "fp-quiet", "baseline-1", []*probeModel{quiet}, changes, quietLeases, judged)
		_, err = gate.Eval(ctx, quietCand.plan, quietCand.binding)
		if !reachable(err, model.ErrNoTerminal) {
			t.Fatalf("the Unwrap chain must reach the missing terminal evidence, chain %v", chain(err))
		}
		_, err = gate.ApplyGate(ctx, quietCand.plan, quietCand.binding)
		if !errors.Is(err, evo.ErrCheckMissing) || !errors.Is(err, evo.ErrEvidenceMissing) || !errors.Is(err, model.ErrNoTerminal) {
			t.Fatalf("the apply gate must reach every named root of its refusal, got %v", err)
		}
		if !errors.As(err, &gateErr) || gateErr.Segment != evo.SegmentApplyGate {
			t.Fatalf("the apply gate failure must carry its segment, got %#v", gateErr)
		}
		if !reachable(err, evo.ErrCheckMissing) || !reachable(err, model.ErrNoTerminal) {
			t.Fatalf("the Unwrap chain must reach the apply gate roots, chain %v", chain(err))
		}
	})

	t.Run("the gate reads the pair it is handed and holds no second configuration", func(t *testing.T) {
		ctx := context.Background()
		gate := evo.NewStructuralGate()

		modelA := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("answer-a")}
		}}
		modelB := &probeModel{name: "b1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("answer-b")}
		}}
		leasesA, _ := leased("model-a1")
		leasesB, _ := leased("model-b1")
		pairA := prepare(t, "fp-a", "baseline-1", []*probeModel{modelA}, []orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}, leasesA, fixturesByAgent{
			"a1": {{id: "c1", task: "produce the answer of a", expect: "answer-a", baseline: "answer-a"}},
		})
		pairB := prepare(t, "fp-b", "baseline-1", []*probeModel{modelB}, []orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "b1"}}, leasesB, fixturesByAgent{
			"b1": {{id: "c1", task: "produce the answer of b", expect: "answer-b", baseline: "answer-b"}},
		})

		firstA, err := gate.Eval(ctx, pairA.plan, pairA.binding)
		if err != nil {
			t.Fatalf("eval over pair A must not fail: %v", err)
		}
		firstB, err := gate.Eval(ctx, pairB.plan, pairB.binding)
		if err != nil {
			t.Fatalf("eval over pair B must not fail: %v", err)
		}
		if firstA.Scope != "declaration:fp-a" || firstB.Scope != "declaration:fp-b" {
			t.Fatalf("each report covers the declaration it was handed, got %q and %q", firstA.Scope, firstB.Scope)
		}
		if !strings.Contains(refTexts(firstA.EvidenceRefs), "lease:model:model-a1:lease-1") ||
			strings.Contains(refTexts(firstA.EvidenceRefs), "model-b1") {
			t.Fatalf("pair A cites the generation it was handed, got %q", refTexts(firstA.EvidenceRefs))
		}
		if !strings.Contains(refTexts(firstB.EvidenceRefs), "lease:model:model-b1:lease-1") ||
			strings.Contains(refTexts(firstB.EvidenceRefs), "model-a1") {
			t.Fatalf("pair B cites the generation it was handed, got %q", refTexts(firstB.EvidenceRefs))
		}
		secondA, err := gate.Eval(ctx, pairA.plan, pairA.binding)
		if err != nil {
			t.Fatalf("repeated eval over pair A must not fail: %v", err)
		}
		if refTexts(firstA.EvidenceRefs) != refTexts(secondA.EvidenceRefs) ||
			firstA.Scope != secondA.Scope || firstA.Reason != secondA.Reason {
			t.Fatalf("the same pair reads the same way twice, got %#v beside %#v", firstA, secondA)
		}
		if got := modelA.seen(); got != "gate-eval eval a1 c1 fp-a | gate-eval eval a1 c1 fp-a" {
			t.Fatalf("pair A is driven through its own binding only, got %q", got)
		}
		if got := modelB.seen(); got != "gate-eval eval b1 c1 fp-b" {
			t.Fatalf("pair B is driven through its own binding only, got %q", got)
		}

		diverged := pairA.binding
		diverged.Plan.Fingerprint = "fp-b"
		report, err := gate.Eval(ctx, pairA.plan, diverged)
		if err == nil || !errors.Is(err, evo.ErrDeclarationDiverged) {
			t.Fatalf("a diverged pair must be refused by name, got %v", err)
		}
		if !reachable(err, evo.ErrDeclarationDiverged) {
			t.Fatalf("the Unwrap chain must reach the divergence, chain %v", chain(err))
		}
		mustNotPass(t, "a diverged pair", report.Verdict)
		if report.Verdict != "" || len(report.EvidenceRefs) != 0 {
			t.Fatalf("a refused pair carries no disposition, got %#v", report)
		}

		noDeclaration := pairA.binding
		noDeclaration.Plan.Declaration = nil
		_, err = gate.Benchmark(ctx, orgstate.OrgPlan{}, noDeclaration)
		if !errors.Is(err, orgstate.ErrPlanMissing) {
			t.Fatalf("a declaration plan without a declaration must be named, got %v", err)
		}
	})

	t.Run("the governance answers file each disposition under its check name", func(t *testing.T) {
		ctx := context.Background()
		policy := evo.NewPolicy(evo.NewStructuralGate())
		changes := []orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}

		ok := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("answer")}
		}}
		okLeases, _ := leased("model-a1")
		judged := fixturesByAgent{"a1": {{id: "c1", task: "produce the answer", expect: "answer", baseline: "answer"}}}
		okCand := prepare(t, "fp-1", "baseline-1", []*probeModel{ok}, changes, okLeases, judged)
		answers, err := policy.Check(ctx, okCand.binding)
		if err != nil {
			t.Fatalf("a complete candidate must answer without an error: %v", err)
		}
		for _, name := range []orgstate.CheckName{evo.CheckEval, evo.CheckBenchmark, evo.CheckApplyGate} {
			answer, filed := answers[name]
			if !filed || answer.Verdict != orgstate.CheckPass {
				t.Fatalf("check %s is filed as %#v", name, answer)
			}
			if answer.Scope != "declaration:fp-1" || len(answer.Evidence) == 0 {
				t.Fatalf("check %s cites its declaration and its evidence, got %#v", name, answer)
			}
		}

		removedLeases, _ := leased("model-a1")
		removedCand := prepare(t, "fp-removed", "baseline-1", []*probeModel{ok}, []orgstate.PlanChange{
			{Kind: orgstate.PlanChangeRemoved, AgentID: "a1"},
		}, removedLeases, nil)
		answers, err = policy.Check(ctx, removedCand.binding)
		if err != nil {
			t.Fatalf("an exemption is recorded and not an error: %v", err)
		}
		for _, name := range []orgstate.CheckName{evo.CheckEval, evo.CheckBenchmark, evo.CheckApplyGate} {
			answer := answers[name]
			if answer.Verdict != orgstate.CheckExempt {
				t.Fatalf("check %s over a change set of removals is filed as %#v", name, answer)
			}
			if answer.Reason == "" || answer.Scope == "" {
				t.Fatalf("check %s records its exemption reason and scope, got %#v", name, answer)
			}
			if len(answer.Evidence) != 0 {
				t.Fatalf("check %s counts its exemption as no evidence, got %#v", name, answer)
			}
		}

		quiet := &probeModel{name: "a1"}
		quietLeases, _ := leased("model-a1")
		quietCand := prepare(t, "fp-quiet", "baseline-1", []*probeModel{quiet}, changes, quietLeases, judged)
		answers, err = policy.Check(ctx, quietCand.binding)
		if err == nil {
			t.Fatal("missing evidence must surface as a named failure")
		}
		for _, want := range []struct {
			name    orgstate.CheckName
			verdict orgstate.CheckVerdict
		}{
			{evo.CheckEval, orgstate.CheckUnanswered},
			{evo.CheckBenchmark, orgstate.CheckUnanswered},
			{evo.CheckApplyGate, orgstate.CheckReject},
		} {
			if answers[want.name].Verdict != want.verdict {
				t.Fatalf("check %s over missing evidence is filed as %#v, want %v", want.name, answers[want.name], want.verdict)
			}
		}
	})

	t.Run("a publication the gate refuses keeps the state it had", func(t *testing.T) {
		ctx := context.Background()
		sink := &recordSink{}
		publisher := orgstate.NewPublisher(
			orgstate.NewBindings(),
			orgstate.PublishContract{Required: []orgstate.CheckName{evo.CheckEval, evo.CheckBenchmark, evo.CheckApplyGate}},
			evo.NewPolicy(evo.NewStructuralGate()),
			sink,
		)

		ok := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("answer")}
		}}
		okLeases, okLog := leased("service-model")
		okCand := prepare(t, "fp-1", publisher.Baseline(), []*probeModel{ok}, []orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}, okLeases, fixturesByAgent{
			"a1": {{id: "c1", task: "produce the answer", expect: "answer", baseline: "answer"}},
		})
		out, err := publisher.Publish(ctx, orgstate.Candidate{Plan: okCand.binding, Resources: okLeases})
		if err != nil {
			t.Fatalf("the candidate backed by evidence must publish: %v", err)
		}
		if out.Result != orgstate.PublishApplied || out.Publication.Generation != 1 {
			t.Fatalf("the candidate backed by evidence publishes one generation, got %#v", out)
		}
		if len(sink.records) != 1 || len(sink.records[0].Checks) != 3 {
			t.Fatalf("the publication records the three check dispositions, got %#v", sink.records)
		}

		quiet := &probeModel{name: "a1"}
		badLeases, badLog := leased("res-1", "res-2")
		badCand := prepare(t, "fp-2", publisher.Baseline(), []*probeModel{quiet}, []orgstate.PlanChange{{Kind: orgstate.PlanChangeUpdated, AgentID: "a1"}}, badLeases, fixturesByAgent{
			"a1": {{id: "c1", task: "produce the answer", expect: "answer", baseline: "answer"}},
		})
		out, err = publisher.Publish(ctx, orgstate.Candidate{Plan: badCand.binding, Resources: badLeases})
		if err == nil {
			t.Fatal("a candidate whose evidence is missing must be refused")
		}
		if out.Publication.Generation != 0 {
			t.Fatalf("a refused candidate takes no effect, got %#v", out)
		}
		if got := publisher.Current(); got.Generation != 1 || got.Fingerprint != "fp-1" {
			t.Fatalf("the state keeps the generation it had, got %#v", got)
		}
		if len(sink.records) != 1 {
			t.Fatalf("a refused candidate commits no publication record, got %#v", sink.records)
		}
		if got := strings.Join(badLog.names, ","); got != "res-2,res-1" {
			t.Fatalf("a refused candidate frees its own resources in reverse, got %q", got)
		}
		if len(okLog.names) != 0 {
			t.Fatalf("a refused candidate frees none of the resources already in service, got %q", okLog.names)
		}
		if !errors.Is(err, evo.ErrCheckMissing) || !errors.Is(err, evo.ErrEvidenceMissing) || !errors.Is(err, model.ErrNoTerminal) {
			t.Fatalf("the refusal must reach the named roots of the gate failure, got %v", err)
		}
		var gateErr *evo.GateError
		if !errors.As(err, &gateErr) || !reachable(err, model.ErrNoTerminal) {
			t.Fatalf("the refusal must keep the gate failure chain reachable, got %v", err)
		}
	})
}

// TestEvalRejectsWrongContentCompletion checks that a completed terminal of wrong content never carries the structural gate.
// - a produced answer contradicting the declared evaluation criterion reports a rejection beside its named cause, never a pass
// - the evaluation and the benchmark face each judge against their own declared value
// - the apply gate consumes the recorded evaluation evidence and drives the binding no more
// - a pair holding no recorded evidence refuses as the missing evidence it is
// - a publication of wrong content is refused through the publisher and takes no effect on the state
//
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestEvalRejectsWrongContentCompletion(t *testing.T) {
	t.Run("a completed answer of wrong content never carries either judged face", func(t *testing.T) {
		ctx := context.Background()
		wrong := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("wrong-answer")}
		}}
		leases, _ := leased("model-a1")
		cand := prepare(t, "fp-wrong", "baseline-1", []*probeModel{wrong},
			[]orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}, leases, fixturesByAgent{
				"a1": {{id: "c1", task: "produce the answer", expect: "right-answer", baseline: "right-answer"}},
			})
		gate := evo.NewStructuralGate()

		eval, err := gate.Eval(ctx, cand.plan, cand.binding)
		mustNotPass(t, "eval over a completed answer of wrong content", eval.Verdict)
		if eval.Verdict != evo.GateVerdictReject {
			t.Fatalf("eval over a completed answer of wrong content reports %q", eval.Verdict)
		}
		if !errors.Is(err, evo.ErrCriterionNotMet) {
			t.Fatalf("eval must name the contradicted criterion, got %v", err)
		}
		var gateErr *evo.GateError
		if !errors.As(err, &gateErr) || gateErr.Segment != evo.SegmentEval || gateErr.Scope != "declaration:fp-wrong" {
			t.Fatalf("the refusal must carry its segment and scope, got %#v", gateErr)
		}
		if !reachable(err, evo.ErrCriterionNotMet) {
			t.Fatalf("the Unwrap chain must reach the contradicted criterion, chain %v", chain(err))
		}

		benchmark, err := gate.Benchmark(ctx, cand.plan, cand.binding)
		mustNotPass(t, "benchmark over a completed answer of wrong content", benchmark.Verdict)
		if benchmark.Verdict != evo.GateVerdictReject || !errors.Is(err, evo.ErrCriterionNotMet) {
			t.Fatalf("benchmark over a completed answer of wrong content reports %q beside %v", benchmark.Verdict, err)
		}
	})

	t.Run("the apply gate consumes the recorded evaluation evidence and drives no probe", func(t *testing.T) {
		ctx := context.Background()
		wrong := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("wrong-answer")}
		}}
		leases, _ := leased("model-a1")
		cand := prepare(t, "fp-consume", "baseline-1", []*probeModel{wrong},
			[]orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}, leases, fixturesByAgent{
				"a1": {{id: "c1", task: "produce the answer", expect: "right-answer", baseline: "right-answer"}},
			})
		gate := evo.NewStructuralGate()

		if _, err := gate.Eval(ctx, cand.plan, cand.binding); err == nil {
			t.Fatal("a contradicted evaluation criterion must surface as a named failure")
		}
		if _, err := gate.Benchmark(ctx, cand.plan, cand.binding); err == nil {
			t.Fatal("a contradicted benchmark criterion must surface as a named failure")
		}
		driven := len(wrong.marks)
		if driven != 2 {
			t.Fatalf("the two judging segments each drive one evaluation case, got %d drives", driven)
		}

		apply, err := gate.ApplyGate(ctx, cand.plan, cand.binding)
		mustNotPass(t, "apply gate over recorded contradicted criteria", apply.Verdict)
		if apply.Verdict != evo.GateVerdictReject || !errors.Is(err, evo.ErrCriterionNotMet) {
			t.Fatalf("the apply gate consumes the recorded refusals, got %q beside %v", apply.Verdict, err)
		}
		if got := len(wrong.marks); got != driven {
			t.Fatalf("the apply gate consumes the recorded evaluation evidence and drives the binding no more, got %d drives", got)
		}

		other := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("right-answer")}
		}}
		otherLeases, _ := leased("model-a1")
		otherCand := prepare(t, "fp-unrecorded", "baseline-1", []*probeModel{other},
			[]orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}, otherLeases, fixturesByAgent{
				"a1": {{id: "c1", task: "produce the answer", expect: "right-answer", baseline: "right-answer"}},
			})
		apply, err = gate.ApplyGate(ctx, otherCand.plan, otherCand.binding)
		mustNotPass(t, "apply gate over a pair holding no recorded evaluation evidence", apply.Verdict)
		if apply.Verdict != evo.GateVerdictReject || !errors.Is(err, evo.ErrCheckMissing) {
			t.Fatalf("an unrecorded pair refuses as the missing evidence it is, got %q beside %v", apply.Verdict, err)
		}
		if got := len(other.marks); got != 0 {
			t.Fatalf("an unrecorded pair drives no probe of its own, got %d drives", got)
		}
	})

	t.Run("a publication of wrong content is refused and takes no effect", func(t *testing.T) {
		ctx := context.Background()
		sink := &recordSink{}
		gate := evo.NewStructuralGate()
		publisher := orgstate.NewPublisher(
			orgstate.NewBindings(),
			orgstate.PublishContract{Required: []orgstate.CheckName{evo.CheckEval, evo.CheckBenchmark, evo.CheckApplyGate}},
			evo.NewPolicy(gate),
			sink,
		)

		wrong := &probeModel{name: "a1", streams: func(string) []*model.Response {
			return []*model.Response{completeResponse("wrong-answer")}
		}}
		leases, log := leased("res-1", "res-2")
		cand := prepare(t, "fp-wrong", publisher.Baseline(), []*probeModel{wrong},
			[]orgstate.PlanChange{{Kind: orgstate.PlanChangeAdded, AgentID: "a1"}}, leases, fixturesByAgent{
				"a1": {{id: "c1", task: "produce the answer", expect: "right-answer", baseline: "right-answer"}},
			})
		out, err := publisher.Publish(ctx, orgstate.Candidate{Plan: cand.binding, Resources: leases})
		if err == nil {
			t.Fatal("a publication of wrong content must be refused")
		}
		if out.Publication.Generation != 0 || out.Result == orgstate.PublishApplied {
			t.Fatalf("a refused publication takes no effect, got %#v", out)
		}
		if got := publisher.Current(); got.Generation != 0 {
			t.Fatalf("the state keeps the generation it had, got %#v", got)
		}
		if len(sink.records) != 0 {
			t.Fatalf("a refused publication commits no record, got %#v", sink.records)
		}
		if got := len(wrong.marks); got != 2 {
			t.Fatalf("the publication drives one evaluation case per judged face, got %d drives", got)
		}
		if !errors.Is(err, evo.ErrCriterionNotMet) {
			t.Fatalf("the refusal must reach the contradicted criterion, got %v", err)
		}
		var gateErr *evo.GateError
		if !errors.As(err, &gateErr) || !reachable(err, evo.ErrCriterionNotMet) {
			t.Fatalf("the refusal must keep the gate failure chain reachable, got %v", err)
		}
		if got := strings.Join(log.names, ","); got != "res-2,res-1" {
			t.Fatalf("a refused candidate frees its own resources in reverse, got %q", got)
		}
	})
}

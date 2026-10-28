package evolution

import (
	"context"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
//
// EvaluationCriterionRef names the declaration reference that carries the
// evaluation fixtures of one agent: one reference per fixture, with the case
// parameters beside it. The reference is declaration data the composition root
// resolves nothing of, so an agent tooling a real tool never collides with it.
const EvaluationCriterionRef = "evaluation"

const (
	// CriterionParamID names the fixture parameter carrying the case id.
	CriterionParamID = "id"

	// CriterionParamTask names the fixture parameter carrying the task the
	// candidate binding answers.
	CriterionParamTask = "task"

	// CriterionParamExpect names the fixture parameter carrying the declared
	// value the evaluation face judges the produced answer against.
	CriterionParamExpect = "expect"

	// CriterionParamBaseline names the fixture parameter carrying the declared
	// comparison value the benchmark face judges the produced answer against.
	CriterionParamBaseline = "baseline"
)

// evaluationCase is one evaluation fixture of one agent: the case it names, the
// task the candidate binding answers and the declared values the two judged
// faces compare the produced answer against.
type evaluationCase struct {
	id       string
	task     string
	expect   string
	baseline string
}

// face returns the parameter name and the declared value one evidence class
// judges produced answers against: the expected answer on the evaluation face
// and the comparison baseline on the benchmark face.
func (c evaluationCase) face(class evidenceClass) (param, declared string) {
	if class == classBenchmark {
		return CriterionParamBaseline, c.baseline
	}
	return CriterionParamExpect, c.expect
}

// evaluationCases returns the declared evaluation fixtures of one agent. They
// are read from the declaration plan the gate was handed, so no configuration is
// parsed beside the pair one call carries in.
func evaluationCases(plan state.OrgPlan, agent string) []evaluationCase {
	if plan.Declaration == nil {
		return nil
	}
	for _, spec := range plan.Declaration.Agents {
		if spec.ID != agent {
			continue
		}
		var cases []evaluationCase
		for _, ref := range spec.Tools {
			if ref.Name != EvaluationCriterionRef {
				continue
			}
			id := ref.Params[CriterionParamID]
			if id == "" {
				id = fmt.Sprintf("%d", len(cases)+1)
			}
			cases = append(cases, evaluationCase{
				id:       id,
				task:     ref.Params[CriterionParamTask],
				expect:   ref.Params[CriterionParamExpect],
				baseline: ref.Params[CriterionParamBaseline],
			})
		}
		return cases
	}
	return nil
}

// evaluate runs one evidence class over the declared changes of the candidate
// and reports one finding per change. Each change is judged against the
// evaluation fixtures its declaration carries, driven through the candidate
// binding of its agent, and the criteria and terminals those runs reach are the
// evidence the finding cites.
func evaluate(ctx context.Context, class evidenceClass, plan state.OrgPlan, binding state.StatePlan) []changeFinding {
	var findings []changeFinding
	for _, change := range plan.Changes {
		agent := string(change.AgentID)
		finding := changeFinding{agent: agent}
		if change.Kind == state.PlanChangeRemoved {
			finding.dispo, finding.label = findingExempt, "the change removes the agent and the check does not apply"
			findings = append(findings, finding)
			continue
		}
		bound, ok := binding.Agents[change.AgentID]
		if !ok {
			finding.dispo, finding.label = findingMissing, "no candidate binding"
			finding.cause = fmt.Errorf("%w: agent %s", state.ErrAgentBindingMissing, agent)
			findings = append(findings, finding)
			continue
		}
		if bound.Model == nil {
			finding.dispo, finding.label = findingMissing, "the candidate binding carries no model"
			finding.cause = fmt.Errorf("%w: agent %s", state.ErrBindingIncomplete, agent)
			findings = append(findings, finding)
			continue
		}
		cases := evaluationCases(plan, agent)
		if len(cases) == 0 {
			finding.dispo, finding.label = findingMissing, "the declaration carries no evaluation criterion"
			finding.cause = fmt.Errorf("%w: class %s: agent %s", ErrCriterionMissing, class, agent)
			findings = append(findings, finding)
			continue
		}
		finding.refs, finding.dispo, finding.label, finding.cause = judgeCases(ctx, class, plan.Fingerprint, agent, cases, bound.Model)
		findings = append(findings, finding)
	}
	return findings
}

// judgeCases runs every declared evaluation case of one declared change through
// the candidate binding and judges each produced answer against the declared
// value of the face the evidence class judges. Every case is run and cited, and
// the finding dispositions the most severe observation of the set beside the
// joined causes underneath it.
func judgeCases(ctx context.Context, class evidenceClass, fingerprint, agent string, cases []evaluationCase, native model.Model) ([]EvidenceRef, findingDisposition, string, error) {
	var refs []EvidenceRef
	var causes []error
	worst := findingComplete
	label := "every produced answer meets the declared evaluation criterion"
	for _, item := range cases {
		itemRefs, dispo, itemLabel, cause := judgeCase(ctx, class, fingerprint, agent, item, native)
		refs = append(refs, itemRefs...)
		if dispo == findingComplete {
			continue
		}
		causes = append(causes, cause)
		if worst == findingComplete || moreSevere(dispo, worst) {
			worst, label = dispo, itemLabel
		}
	}
	if len(causes) == 0 {
		return refs, findingComplete, label, nil
	}
	return refs, worst, label, joinCauses(causes)
}

// moreSevere reports whether one observation refuses harder than another: a
// refused probe over a contradicted criterion over missing evidence.
func moreSevere(a, b findingDisposition) bool {
	rank := map[findingDisposition]int{findingRefused: 0, findingUnmet: 1, findingMissing: 2, findingExempt: 3}
	return rank[a] < rank[b]
}

// judgeCase drives one declared evaluation case through the candidate binding of
// its agent and judges the produced answer against the declared value on the
// face the evidence class judges. A completed answer contradicting that value is
// refused beside the produced and the declared one, and a face the declaration
// carries no value on cannot be judged at all. The criterion reference beside
// the terminal the run reached is the evidence the case cites.
func judgeCase(ctx context.Context, class evidenceClass, fingerprint, agent string, item evaluationCase, native model.Model) ([]EvidenceRef, findingDisposition, string, error) {
	param, declared := item.face(class)
	if strings.TrimSpace(declared) == "" {
		return nil, findingMissing,
			"the declaration carries no " + param + " value for evaluation case " + item.id,
			fmt.Errorf("%w: class %s: agent %s: case %s: face %s", ErrCriterionMissing, class, agent, item.id, param)
	}
	stream, entryErr := native.GenerateContent(ctx, evaluationRequest(class, fingerprint, agent, item))
	if entryErr != nil {
		stream = nil
	}
	outcome := model.ReduceChannel(ctx, stream, nil)
	refs := []EvidenceRef{
		EvidenceRef(fmt.Sprintf("criterion:%s:%s:%s", class, agent, item.id)),
		EvidenceRef(fmt.Sprintf("terminal:%s:%s:%s:%s", class, agent, item.id, terminalName(outcome.Kind))),
	}
	observed := traceCause(outcome)
	if observed == nil {
		observed = ctx.Err()
	}
	switch outcome.Kind {
	case model.KindCompleted:
		produced := strings.TrimSpace(producedAnswer(outcome))
		if produced == strings.TrimSpace(declared) {
			return refs, findingComplete, "the produced answer meets the declared evaluation criterion", nil
		}
		return refs, findingUnmet,
			"the produced answer contradicts the declared evaluation criterion of case " + item.id,
			fmt.Errorf("%w: agent %s: case %s: produced %q, declared %q", ErrCriterionNotMet, agent, item.id, produced, declared)
	case model.KindFailed:
		return refs, findingRefused, "the candidate refused the probe", refusalCause(agent, entryErr, observed)
	default:
		return refs, findingMissing, insufficiencyLabel(outcome.Kind), insufficiencyCause(agent, outcome.Kind, observed)
	}
}

// evaluationRequest builds the request one evaluation case drives through the
// candidate binding of one declared change: the marker naming the class, the
// agent, the case and the declaration fingerprint beside the task of the case.
func evaluationRequest(class evidenceClass, fingerprint, agent string, item evaluationCase) *model.Request {
	return &model.Request{Messages: []model.Message{
		{Role: model.RoleSystem, Content: fmt.Sprintf("gate-eval %s %s %s %s", class, agent, item.id, fingerprint)},
		{Role: model.RoleUser, Content: item.task},
	}}
}

// producedAnswer renders the answer the reduced outcome carries.
func producedAnswer(outcome model.Outcome) string {
	if outcome.Body == nil || len(outcome.Body.Choices) == 0 {
		return ""
	}
	return outcome.Body.Choices[0].Message.Content
}

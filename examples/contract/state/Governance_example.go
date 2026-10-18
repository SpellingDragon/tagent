package stateexample

import (
	"context"

	"github.com/SpellingDragon/tagent/state"
)

// namedChecks is the canonical governance seam: it evaluates one publication candidate and
// answers each check under the name it is filed by.
type namedChecks struct {
	answers map[state.CheckName]state.CheckAnswer
}

// Check evaluates one candidate plan and answers each check under the name it is filed by.
func (g *namedChecks) Check(_ context.Context, _ state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	answers := make(map[state.CheckName]state.CheckAnswer, len(g.answers))
	for name, answer := range g.answers {
		answer.Name = name
		answers[name] = answer
	}
	return answers, nil
}

var _ state.Governance = (*namedChecks)(nil)

// ExampleGovernance answers one publication candidate check by check, so a required check
// that reports a reject and an exempt check that passed nothing both read as what they are.
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func ExampleGovernance() {
	ctx := context.Background()
	plan := state.StatePlan{Plan: state.OrgPlan{Fingerprint: "fingerprint-1"}}
	checks := &namedChecks{answers: map[state.CheckName]state.CheckAnswer{
		"structure": {Verdict: state.CheckPass, Reason: "the declaration parses", Scope: "declaration"},
		"budget":    {Verdict: state.CheckReject, Reason: "the budget is over its bound", Scope: "tokens"},
		"eval":      {Verdict: state.CheckExempt, Reason: "no evaluation source is wired", Scope: "evaluation"},
	}}

	answers, err := checks.Check(ctx, plan)
	if err != nil || len(answers) != 3 {
		return
	}
	if answers["budget"].Verdict != state.CheckReject || answers["eval"].Verdict != state.CheckExempt {
		return
	}
	if answers["structure"].Name != "structure" {
		return
	}
}

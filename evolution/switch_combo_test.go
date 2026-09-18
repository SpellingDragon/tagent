package evolution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// Switch-combination regression for the evolution verdict (K7: an
// under-evidenced window must never masquerade as healthy)
// (resident-remaining-hardening 3.4, archived 7.4).
//
// Two fail-closed invariants the release loop depends on:
//
//   - judge/evidence unavailable ⇒ Verdict "insufficient" (an explicit "I could
//     not evaluate", never a silent "healthy");
//   - governance signals disabled ⇒ even a healthy verdict carries an explicit
//     "unavailable" note, so the reader knows denial/critical evidence was not
//     consulted rather than being green by omission.

type constJudge struct {
	res EvalResult
	err error
}

func (j constJudge) Evaluate(context.Context, string) (EvalResult, error) { return j.res, j.err }

func newEvaluatedJudge(t *testing.T, judge Evaluator, signals func() bool) (string, *GitEvolution) {
	t.Helper()
	dir := initGitRepo(t)
	writeFile(t, dir, "resources/prompts/SOUL.md", "v2")
	g := NewGitEvolution(GitEvolutionConfig{WorkDir: dir})
	g.BindRuntime(memory.NewInMemoryStore(), memory.PartitionIDFromName("tagent"), judge, nil)
	if signals != nil {
		g.SetGovernanceSignalsAvailable(signals)
	}
	sha, _, err := g.Register([]string{"resources/prompts/SOUL.md"}, "combo")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	g.EvaluateNow(sha, 0)
	return sha, g
}

// Evidence/judge unavailable must degrade to an explicit "insufficient" verdict.
func TestSwitchCombo_EvidenceUnavailableIsInsufficient(t *testing.T) {
	sha, g := newEvaluatedJudge(t, constJudge{err: errors.New("judge backend down")}, nil)
	ev := g.Evaluations()[sha]
	if ev.Verdict != "insufficient" {
		t.Fatalf("judge error must yield insufficient verdict, got %q (%+v)", ev.Verdict, ev)
	}
	if !strings.Contains(ev.Reason, "评估失败") {
		t.Fatalf("insufficient verdict must state the failure honestly: %q", ev.Reason)
	}
}

// Governance signals unavailable must be surfaced in the reason even when the
// window is otherwise judged healthy — never a silent green.
func TestSwitchCombo_GovernanceUnavailableIsExplicitNotSilent(t *testing.T) {
	// Signals reported unavailable; judge passes.
	sha, g := newEvaluatedJudge(t,
		constJudge{res: EvalResult{Pass: true, Score: 1.0, Reason: "metrics look fine"}},
		func() bool { return false })
	ev := g.Evaluations()[sha]
	if ev.Verdict != "healthy" {
		t.Fatalf("passing judge with signals off still records healthy, got %q", ev.Verdict)
	}
	if !strings.Contains(ev.Reason, "unavailable") {
		t.Fatalf("healthy verdict with governance off must say signals were unavailable: %q", ev.Reason)
	}
}

// Control: signals available + passing judge → healthy with NO unavailability
// caveat, so the "unavailable" note above is attributable to the switch, not a
// constant string.
func TestSwitchCombo_GovernanceAvailableOmitsCaveat(t *testing.T) {
	sha, g := newEvaluatedJudge(t,
		constJudge{res: EvalResult{Pass: true, Score: 1.0, Reason: "metrics look fine"}},
		func() bool { return true })
	ev := g.Evaluations()[sha]
	if ev.Verdict != "healthy" {
		t.Fatalf("want healthy, got %q", ev.Verdict)
	}
	if strings.Contains(ev.Reason, "unavailable") {
		t.Fatalf("available-governance healthy verdict must not claim unavailability: %q", ev.Reason)
	}
}

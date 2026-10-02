package evolution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

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

// TestSwitchCombo_EvidenceUnavailableIsInsufficient Evidence/judge unavailable must degrade to an explicit "insufficient" verdict.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
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

// TestSwitchCombo_GovernanceUnavailableIsExplicitNotSilent 钉住 治理信号不可用时，即便指标正常，也必须在结论理由里显式说明，不得静默略过。
func TestSwitchCombo_GovernanceUnavailableIsExplicitNotSilent(t *testing.T) {
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

// TestSwitchCombo_GovernanceAvailableOmitsCaveat 钉住 对照：信号可用且判定通过时给出健康结论，且不得附带"不可用"的免责说明。
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

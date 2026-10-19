package evolution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/model"
	evo "github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/evolution/refine"
)

// TestPostActivationEvidence checks the activation-moment evidence window and the post-hoc evaluation over one version:
// - the window anchors at t1, the first real consumption; t0 lands the file and t2 registers it in git and moves nothing
// - later consumptions add records without touching the anchor, and a correlated version never consumed stays without a window
// - the feedback join is the two-condition exact match: stamped with the version or its correlated registration, and really consumed
// - every failed match is excluded and named, and degrading entries that fail the join never leak into the degradation count
// - the same evidence pool serves both windows: a version only ever decides over the requests that really consumed it
// - vA degrades on its own degrading feedback while vB, consumed once and owning none, reports insufficient instead of borrowing
// - judge_delay is kept: before activation + delay the window is not-due, never read as healthy, and it proposes nothing
// - once due the same window is judged normally, and the output is exactly the four states healthy, degraded, insufficient, not-due
// - the consumption precedes the registration and the advice names the correlated registration commit
// - a degraded verdict yields a rollback advice event only: the repository state is identical before and after and no revert exists
// - a registration without any real consumption has no window: insufficient with the missing evidence named and a zero window
// - an insufficient verdict asserts nothing and never folds into healthy; the commit moment is never guessed as the anchor
// - each evidence port reporting an error aborts the judgment as insufficient with the source named
// - with governance disabled the counts are never read and the report names the criteria unavailable
// - the reverse control: the same counts under enabled governance do judge and carry no disabled annotation
// - the LLM judge proposes a rollback only on an explicit low score below the threshold; a healthy score completes healthy
// - a judge that cannot decide (missing score, unparsable answer, failed model entry) reports insufficient, never healthy
// - the deterministic guardrail line stands before the judge: a metric breach degrades even while the judge is down
// - a recorder failure never retracts the judgment: the report stands and the unrecorded event is reported by name
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
func TestPostActivationEvidence(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, time.October, 15, 10, 0, 0, 0, time.UTC)

	t.Run("the window anchors at the first real consumption and a later git registration never moves it", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		t1 := base.Add(time.Minute)
		t3 := base.Add(3 * time.Minute)

		window, opened, err := ledger.Consume(evo.Activation{Version: "v1", At: t1, Evidence: "consumption/1"})
		if err != nil || !opened {
			t.Fatalf("the first real consumption opens the window, got opened=%v err=%v", opened, err)
		}
		if !window.ActivatedAt.Equal(t1) || window.ActivationEvidence != "consumption/1" {
			t.Fatalf("the window anchors at the activation moment with its evidence, got %#v", window)
		}

		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)
		repo.write("resources/prompts/SOUL.md", "improved soul\n")
		record, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "痛点:提示词漂移 → 产物:重写 SOUL.md → 预期收益:回答更稳")
		if err != nil {
			t.Fatalf("the registration must commit: %v", err)
		}
		if err := ledger.Correlate("v1", record.SHA); err != nil {
			t.Fatalf("the registration correlates to the consumed version: %v", err)
		}
		if window, _ := ledger.Window("v1"); !window.ActivatedAt.Equal(t1) || window.ActivatedAt.Equal(record.CommittedAt) {
			t.Fatalf("the registration is correlation only and never the anchor, got window %#v against commit %v", window, record.CommittedAt)
		}

		window, opened, err = ledger.Consume(evo.Activation{Version: "v1", At: t3, Evidence: "consumption/3"})
		if err != nil || opened {
			t.Fatalf("a later consumption never reopens the window, got opened=%v err=%v", opened, err)
		}
		if !window.ActivatedAt.Equal(t1) {
			t.Fatalf("the anchor stays at the first real consumption, got %v", window.ActivatedAt)
		}
		if got := len(ledger.Consumed("v1")); got != 2 {
			t.Fatalf("consumptions before and after the registration are both on record, got %d", got)
		}

		if err := ledger.Correlate("v1", record.SHA); err != nil {
			t.Fatalf("re-correlating the same SHA is idempotent, got %v", err)
		}
		if err := ledger.Correlate("v1", "deadbeef"); !errors.Is(err, evo.ErrCorrelationConflict) {
			t.Fatalf("a contradicting correlation is refused, got %v", err)
		}
		if sha, ok := ledger.RegistrationOf("v1"); !ok || sha != record.SHA {
			t.Fatalf("the recorded mapping stands after the refused conflict, got %q %v", sha, ok)
		}

		if err := ledger.Correlate("v2", record.SHA); err != nil {
			t.Fatalf("correlating another version must record only the mapping: %v", err)
		}
		if _, open := ledger.Window("v2"); open {
			t.Fatalf("a registration alone never opens a window for a version never consumed")
		}
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: t3}); !errors.Is(err, evo.ErrActivationUnusable) {
			t.Fatalf("a consumption without evidence is refused, got %v", err)
		}
	})

	t.Run("feedback joins on the version stamp and the real consumed version together", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: base.Add(time.Minute), Evidence: "consumption/1"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		if err := ledger.Correlate("v1", "S"); err != nil {
			t.Fatalf("correlation must record: %v", err)
		}
		feed := &feedbackFeed{entries: []evo.Feedback{
			{Stamp: "v1", Consumed: "v1", Evidence: "fb/version-stamp"},
			{Stamp: "S", Consumed: "v1", Evidence: "fb/sha-stamp"},
			{Stamp: "S", Consumed: "vA", Degrades: true, Evidence: "fb/other-consumption"},
			{Stamp: "S", Consumed: "", Degrades: true, Evidence: "fb/no-consumption"},
			{Stamp: "vZ", Consumed: "v1", Degrades: true, Evidence: "fb/foreign-stamp"},
		}}

		policy := evo.DefaultJudgePolicy()
		policy.MinSamples = 2
		log := &evalLog{}
		posterior := evo.NewPosterior(ledger, policy, feed.read, guardrailOf(evo.Guardrail{Enabled: true}).read, nil, log.recorder())

		_, report, err := posterior.Evaluate(ctx, "v1", base.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorHealthy {
			t.Fatalf("only the two exact joins decide, and they show no degradation: %v (%s)", report.Verdict, report.Reason)
		}
		if summary := log.last().Summary; !strings.Contains(summary, "excluded") || !strings.Contains(summary, "fb/foreign-stamp") {
			t.Fatalf("the excluded entries are named in the evidence summary, got %q", summary)
		}

		policy.MinSamples = 3
		posterior = evo.NewPosterior(ledger, policy, feed.read, guardrailOf(evo.Guardrail{Enabled: true}).read, nil, log.recorder())
		_, report, err = posterior.Evaluate(ctx, "v1", base.Add(3*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorInsufficient || !strings.Contains(report.Reason, "2 < 3") {
			t.Fatalf("the decidable sample is counted and below the threshold is insufficient, got %v (%s)", report.Verdict, report.Reason)
		}
		if !strings.Contains(report.Reason, "excluded") {
			t.Fatalf("the insufficient reason names what was excluded and why, got %q", report.Reason)
		}
	})

	t.Run("requests attribute to the version they really consumed across windows", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		for i := 0; i < 5; i++ {
			if _, _, err := ledger.Consume(evo.Activation{Version: "vA", At: base.Add(time.Duration(i) * time.Minute), Evidence: "consumption/a"}); err != nil {
				t.Fatalf("consumption must record: %v", err)
			}
		}
		if _, _, err := ledger.Consume(evo.Activation{Version: "vB", At: base.Add(time.Hour), Evidence: "consumption/b"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		feed := &feedbackFeed{entries: []evo.Feedback{
			{Stamp: "vA", Consumed: "vA", Degrades: true, Evidence: "fb/a1"},
			{Stamp: "vA", Consumed: "vA", Degrades: true, Evidence: "fb/a2"},
			{Stamp: "vA", Consumed: "vA", Degrades: true, Evidence: "fb/a3"},
			{Stamp: "vA", Consumed: "vA", Degrades: true, Evidence: "fb/a4"},
			{Stamp: "vA", Consumed: "vA", Degrades: true, Evidence: "fb/a5"},
		}}

		log := &evalLog{}
		posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(), feed.read, guardrailOf(evo.Guardrail{Enabled: true}).read, nil, log.recorder())

		_, reportA, err := posterior.Evaluate(ctx, "vA", base.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if reportA.Verdict != evo.PosteriorDegraded {
			t.Fatalf("vA's own degrading feedback degrades vA, got %v (%s)", reportA.Verdict, reportA.Reason)
		}

		windowB, reportB, err := posterior.Evaluate(ctx, "vB", base.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if reportB.Verdict != evo.PosteriorInsufficient || !strings.Contains(reportB.Reason, "0 < 5") {
			t.Fatalf("vB borrows no samples from another version and reports insufficient, got %v (%s)", reportB.Verdict, reportB.Reason)
		}
		if !windowB.ActivatedAt.Equal(base.Add(time.Hour)) {
			t.Fatalf("vB's window anchors at its own first consumption, got %v", windowB.ActivatedAt)
		}
	})

	t.Run("a window under judge delay reports not-due and yields no advice", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		activated := base.Add(time.Minute)
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: activated, Evidence: "consumption/1"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		feed := &feedbackFeed{entries: positiveFeedback("v1", 5)}
		policy := evo.DefaultJudgePolicy()
		policy.JudgeDelay = time.Hour
		log := &evalLog{}
		posterior := evo.NewPosterior(ledger, policy, feed.read, guardrailOf(evo.Guardrail{Enabled: true}).read, nil, log.recorder())

		_, report, err := posterior.Evaluate(ctx, "v1", activated.Add(10*time.Minute))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorNotDue {
			t.Fatalf("an unripe window reports not-due, got %v (%s)", report.Verdict, report.Reason)
		}
		if event := log.last(); event.Suggestion != "" || event.Verdict != evo.PosteriorNotDue {
			t.Fatalf("not-due yields no advice, got %#v", event)
		}

		_, report, err = posterior.Evaluate(ctx, "v1", activated.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorHealthy {
			t.Fatalf("a due window is judged normally, got %v (%s)", report.Verdict, report.Reason)
		}
	})

	t.Run("a degraded verdict yields a rollback advice event and the framework never reverts", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: base.Add(time.Minute), Evidence: "consumption/1"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		repo := newRefineRepo(t)
		log := &improvementLog{}
		gitRefine := refine.NewGitRefine(repo.root, log.recorder(), nil)
		repo.write("resources/prompts/SOUL.md", "improved soul\n")
		record, err := gitRefine.Register(ctx, []string{"resources/prompts/SOUL.md"}, "痛点:回答漂移 → 产物:收紧 SOUL.md → 预期收益:回答更稳")
		if err != nil {
			t.Fatalf("the registration must commit: %v", err)
		}
		if err := ledger.Correlate("v1", record.SHA); err != nil {
			t.Fatalf("correlation must record: %v", err)
		}

		feed := &feedbackFeed{entries: negativeFeedback("v1", 5)}
		events := &evalLog{}
		posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(), feed.read, guardrailOf(evo.Guardrail{Enabled: true}).read, nil, events.recorder())

		before := repo.state()
		_, report, err := posterior.Evaluate(ctx, "v1", base.Add(2*time.Hour))
		after := repo.state()
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorDegraded {
			t.Fatalf("five degrading entries out of five turns breach the negative rate, got %v (%s)", report.Verdict, report.Reason)
		}
		event := events.last()
		if event.Verdict != evo.PosteriorDegraded || event.SHA != record.SHA {
			t.Fatalf("the event carries the verdict and the correlated SHA, got %#v", event)
		}
		if want := "refine rollback " + record.SHA + "; review with git diff"; event.Suggestion != want {
			t.Fatalf("the degraded verdict advises the rollback command by name, got %q want %q", event.Suggestion, want)
		}
		if before != after {
			t.Fatalf("the framework never reverts: the repository state changed:\nbefore:\n%s\nafter:\n%s", before, after)
		}
		if log := repo.tryGitOut("log", "--oneline", "--all"); strings.Contains(log, "Revert") {
			t.Fatalf("no revert commit is ever written by the evaluation, got %q", log)
		}
	})

	t.Run("missing activation evidence reports insufficient and never guesses the commit moment", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		if err := ledger.Correlate("v1", "S"); err != nil {
			t.Fatalf("correlation must record: %v", err)
		}
		log := &evalLog{}
		posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(), (&feedbackFeed{}).read, guardrailOf(evo.Guardrail{Enabled: true}).read, nil, log.recorder())

		window, report, err := posterior.Evaluate(ctx, "v1", base.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorInsufficient || !strings.Contains(report.Reason, "missing activation evidence") {
			t.Fatalf("missing activation evidence is insufficient and named, got %v (%s)", report.Verdict, report.Reason)
		}
		if !window.ActivatedAt.IsZero() || window.ActivationEvidence != "" {
			t.Fatalf("no activation evidence means no open window, got %#v", window)
		}
		if event := log.last(); event.Verdict != evo.PosteriorInsufficient || event.Suggestion != "" {
			t.Fatalf("the insufficient verdict yields no advice, got %#v", event)
		}
	})

	t.Run("unreachable evidence sources report insufficient, never healthy", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: base.Add(time.Minute), Evidence: "consumption/1"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		log := &evalLog{}
		posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
			(&feedbackFeed{err: errors.New("store offline")}).read,
			guardrailOf(evo.Guardrail{Enabled: true}).read, nil, log.recorder())
		_, report, err := posterior.Evaluate(ctx, "v1", base.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorInsufficient || !strings.Contains(report.Reason, "feedback source unreachable") {
			t.Fatalf("an unreachable feedback source is insufficient and named, got %v (%s)", report.Verdict, report.Reason)
		}

		posterior = evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
			(&feedbackFeed{entries: positiveFeedback("v1", 5)}).read,
			(&guardrailFeed{guard: evo.Guardrail{Enabled: true}, err: errors.New("metrics offline")}).read, nil, log.recorder())
		_, report, err = posterior.Evaluate(ctx, "v1", base.Add(3*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorInsufficient || !strings.Contains(report.Reason, "guardrail source unreachable") {
			t.Fatalf("an unreachable guardrail source is insufficient and named, got %v (%s)", report.Verdict, report.Reason)
		}
	})

	t.Run("governance disabled marks its guardrail criteria unavailable instead of clean zeros", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: base.Add(time.Minute), Evidence: "consumption/1"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		log := &evalLog{}
		posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
			(&feedbackFeed{entries: positiveFeedback("v1", 5)}).read,
			guardrailOf(evo.Guardrail{Enabled: false, DenialCount: 99, CriticalCount: 99}).read, nil, log.recorder())

		_, report, err := posterior.Evaluate(ctx, "v1", base.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorHealthy || !strings.Contains(report.Reason, "governance disabled") {
			t.Fatalf("disabled guardrails judge nothing and are marked unavailable, got %v (%s)", report.Verdict, report.Reason)
		}

		posterior = evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
			(&feedbackFeed{entries: positiveFeedback("v1", 5)}).read,
			guardrailOf(evo.Guardrail{Enabled: true, DenialCount: 99}).read, nil, log.recorder())
		_, report, err = posterior.Evaluate(ctx, "v1", base.Add(3*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorDegraded || !strings.Contains(report.Reason, "denial rate breached") {
			t.Fatalf("enabled guardrails judge their counts, got %v (%s)", report.Verdict, report.Reason)
		}
		if strings.Contains(report.Reason, "governance disabled") {
			t.Fatalf("the annotation comes from the switch, not from a constant, got %q", report.Reason)
		}
	})

	t.Run("the LLM judge proposes a rollback only on an explicit low score", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: base.Add(time.Minute), Evidence: "consumption/1"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		log := &evalLog{}
		judged := func(t *testing.T, content string, feedback []evo.Feedback) evo.PosteriorVerdict {
			t.Helper()
			probe := &probeModel{name: "judge", streams: func(string) []*model.Response {
				return []*model.Response{completeResponse(content)}
			}}
			posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
				(&feedbackFeed{entries: feedback}).read,
				guardrailOf(evo.Guardrail{Enabled: true}).read,
				evo.NewModelJudge(probe, 0.5), log.recorder())
			_, report, err := posterior.Evaluate(ctx, "v1", base.Add(2*time.Hour))
			if err != nil {
				t.Fatalf("a recorded evaluation reports no error: %v", err)
			}
			return report.Verdict
		}

		if verdict := judged(t, `{"score": 0.2, "reason": "answers degraded"}`, positiveFeedback("v1", 5)); verdict != evo.PosteriorDegraded {
			t.Fatalf("an explicit low score degrades, got %v", verdict)
		}
		if verdict := judged(t, `{"score": 0.9, "reason": "answers stayed sharp"}`, positiveFeedback("v1", 5)); verdict != evo.PosteriorHealthy {
			t.Fatalf("a healthy score completes healthy, got %v", verdict)
		}
		if verdict := judged(t, `{"reason": "no score at all"}`, positiveFeedback("v1", 5)); verdict != evo.PosteriorInsufficient {
			t.Fatalf("an answer without a score decides nothing, got %v", verdict)
		}
		if verdict := judged(t, `no json here`, positiveFeedback("v1", 5)); verdict != evo.PosteriorInsufficient {
			t.Fatalf("an unparsable answer decides nothing, got %v", verdict)
		}

		probe := &probeModel{name: "judge", entryErr: errors.New("model entry refused")}
		posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
			(&feedbackFeed{entries: positiveFeedback("v1", 5)}).read,
			guardrailOf(evo.Guardrail{Enabled: true}).read,
			evo.NewModelJudge(probe, 0.5), log.recorder())
		_, report, err := posterior.Evaluate(ctx, "v1", base.Add(3*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorInsufficient || !strings.Contains(report.Reason, "judge model entry failed") {
			t.Fatalf("a failed judge call reports insufficient and names the failure, got %v (%s)", report.Verdict, report.Reason)
		}

		posterior = evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
			(&feedbackFeed{entries: negativeFeedback("v1", 5)}).read,
			guardrailOf(evo.Guardrail{Enabled: true}).read,
			evo.NewModelJudge(probe, 0.5), log.recorder())
		_, report, err = posterior.Evaluate(ctx, "v1", base.Add(4*time.Hour))
		if err != nil {
			t.Fatalf("a recorded evaluation reports no error: %v", err)
		}
		if report.Verdict != evo.PosteriorDegraded || !strings.Contains(report.Reason, "negative feedback rate breached") {
			t.Fatalf("the deterministic breach degrades even while the judge is down, got %v (%s)", report.Verdict, report.Reason)
		}
	})

	t.Run("a failed evaluation recording is reported by name", func(t *testing.T) {
		ledger := evo.NewActivationLedger()
		if _, _, err := ledger.Consume(evo.Activation{Version: "v1", At: base.Add(time.Minute), Evidence: "consumption/1"}); err != nil {
			t.Fatalf("consumption must record: %v", err)
		}
		events := &evalLog{err: errors.New("event bus down")}
		posterior := evo.NewPosterior(ledger, evo.DefaultJudgePolicy(),
			(&feedbackFeed{entries: positiveFeedback("v1", 5)}).read,
			guardrailOf(evo.Guardrail{Enabled: true}).read, nil, events.recorder())

		_, report, err := posterior.Evaluate(ctx, "v1", base.Add(2*time.Hour))
		if !errors.Is(err, evo.ErrEvaluationUnrecorded) {
			t.Fatalf("the unrecorded event is reported by name, got %v", err)
		}
		if report.Verdict != evo.PosteriorHealthy {
			t.Fatalf("the judgment stands despite the recording failure, got %v (%s)", report.Verdict, report.Reason)
		}
	})
}

// feedbackFeed is the recorded feedback source of the tests: it serves the
// scripted entries to every query on purpose — the join, not the source, is what
// attributes an entry to a version — or fails as an unreachable source.
type feedbackFeed struct {
	entries []evo.Feedback
	err     error
}

// read serves the scripted feedback for one version query.
func (f *feedbackFeed) read(_ context.Context, _ string) ([]evo.Feedback, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// guardrailFeed is the guardrail source of the tests: it serves the scripted
// statistics or fails as an unreachable source.
type guardrailFeed struct {
	guard evo.Guardrail
	err   error
}

// guardrailOf builds one scripted guardrail source.
func guardrailOf(guard evo.Guardrail) *guardrailFeed {
	return &guardrailFeed{guard: guard}
}

// read serves the scripted guardrail statistics for one version query.
func (g *guardrailFeed) read(_ context.Context, _ string) (evo.Guardrail, error) {
	if g.err != nil {
		return evo.Guardrail{}, g.err
	}
	return g.guard, nil
}

// evalLog records the evaluation events the post-hoc judgment writes through its
// injected recorder, or fails as a broken event face.
type evalLog struct {
	events []evo.Evaluation
	err    error
}

// record captures one evaluation event or reports the scripted failure.
func (l *evalLog) record(_ context.Context, evaluation evo.Evaluation) error {
	if l.err != nil {
		return l.err
	}
	l.events = append(l.events, evaluation)
	return nil
}

// recorder returns the port the evaluation records through.
func (l *evalLog) recorder() evo.EvaluationRecorder {
	return l.record
}

// last returns the most recently recorded evaluation event.
func (l *evalLog) last() evo.Evaluation {
	if len(l.events) == 0 {
		return evo.Evaluation{}
	}
	return l.events[len(l.events)-1]
}

// positiveFeedback builds one positive feedback entry per turn of a version.
func positiveFeedback(version string, count int) []evo.Feedback {
	entries := make([]evo.Feedback, 0, count)
	for i := 0; i < count; i++ {
		entries = append(entries, evo.Feedback{Stamp: version, Consumed: version, Evidence: evo.EvidenceRef("fb/pos-" + version)})
	}
	return entries
}

// negativeFeedback builds one degrading feedback entry per turn of a version.
func negativeFeedback(version string, count int) []evo.Feedback {
	entries := make([]evo.Feedback, 0, count)
	for i := 0; i < count; i++ {
		entries = append(entries, evo.Feedback{Stamp: version, Consumed: version, Degrades: true, Evidence: evo.EvidenceRef("fb/neg-" + version)})
	}
	return entries
}

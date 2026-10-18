package evolution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// postAt is the activation and judgment moment every post-hoc invariant uses.
var postAt = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// TestPosteriorJudgmentIsDeterministic pins the judgment as a function of its evidence: the same ledger and sources render the same report and record.
// - two judgments over the same evidence render byte-identical reports
// - neither run moves the evidence window the consumption opened
// - each run records one evaluation event carrying the same verdict and summary
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestPosteriorJudgmentIsDeterministic(t *testing.T) {
	ctx := context.Background()
	feedback := func(context.Context, string) ([]Feedback, error) {
		return determinateFeedback(10, 0), nil
	}
	guardrail := func(context.Context, string) (Guardrail, error) {
		return Guardrail{Enabled: true}, nil
	}
	posterior, recorded := posteriorUnderTest(t, 10, JudgePolicy{}, feedback, guardrail, nil)

	firstWindow, first, err := posterior.Evaluate(ctx, "v1", postAt)
	if err != nil {
		t.Fatalf("first judgment: %v", err)
	}
	secondWindow, second, err := posterior.Evaluate(ctx, "v1", postAt)
	if err != nil {
		t.Fatalf("second judgment: %v", err)
	}
	if first.Verdict != PosteriorHealthy || second.Verdict != PosteriorHealthy {
		t.Fatalf("verdicts = (%q, %q), want a healthy judgment over clean determinate evidence", first.Verdict, second.Verdict)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("reports differ over the same evidence:\nfirst  %+v\nsecond %+v", first, second)
	}
	if !reflect.DeepEqual(firstWindow, secondWindow) {
		t.Fatalf("windows differ over the same consumption: %+v and %+v", firstWindow, secondWindow)
	}
	if !firstWindow.ActivatedAt.Equal(postAt) {
		t.Fatalf("window activated at %s, want the first consumption moment %s", firstWindow.ActivatedAt, postAt)
	}
	if len(*recorded) != 2 {
		t.Fatalf("recorded evaluations = %d, want one per judgment", len(*recorded))
	}
	if (*recorded)[0].Verdict != (*recorded)[1].Verdict || (*recorded)[0].Summary != (*recorded)[1].Summary {
		t.Fatalf("recorded evaluations differ: %+v and %+v", (*recorded)[0], (*recorded)[1])
	}
}

// TestPosteriorRefusesBeforeItJudges pins the judgment order: each missing precondition aborts before the next source is read, and none falls through to healthy.
// - a version never consumed reports insufficient with the zero window and reads no source
// - a window before its judgment point reports not-due and reads no feedback
// - an unreachable feedback source reports insufficient before the guardrail is read
// - an unreachable guardrail source reports insufficient instead of judging clean zeros
// - the two-condition join excludes mis-stamped and mis-consumed entries by name instead of time-joining them
// - a failed or undecidable judge reports insufficient, never healthy
// - a breached deterministic criterion reports degraded before the judge is consulted
// - a rate exactly at the ceiling is not a breach
// - disabled governance criteria are named unavailable, and huge counts never breach through them
// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
func TestPosteriorRefusesBeforeItJudges(t *testing.T) {
	ctx := context.Background()

	t.Run("a version never consumed reads no source", func(t *testing.T) {
		reads := 0
		posterior, _ := posteriorUnderTest(t, 0, JudgePolicy{},
			func(context.Context, string) ([]Feedback, error) { reads++; return nil, nil },
			func(context.Context, string) (Guardrail, error) { reads++; return Guardrail{}, nil },
			func(context.Context, Review) (Judgment, error) { reads++; return Judgment{}, nil })
		window, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge an unconsumed version: %v", err)
		}
		if report.Verdict != PosteriorInsufficient || !strings.Contains(report.Reason, "missing activation evidence") {
			t.Fatalf("report = %+v, want insufficient naming the missing activation evidence", report)
		}
		if window.Version != "" || !window.ActivatedAt.IsZero() {
			t.Fatalf("window = %+v, want the zero window that never means an open one", window)
		}
		if reads != 0 {
			t.Fatalf("evidence reads = %d, want none before activation evidence stands", reads)
		}
	})

	t.Run("a window before its judgment point reads no feedback", func(t *testing.T) {
		reads := 0
		posterior, _ := posteriorUnderTest(t, 10, JudgePolicy{JudgeDelay: time.Hour},
			func(context.Context, string) ([]Feedback, error) { reads++; return nil, nil },
			func(context.Context, string) (Guardrail, error) { reads++; return Guardrail{}, nil }, nil)
		_, report, err := posterior.Evaluate(ctx, "v1", postAt.Add(time.Minute))
		if err != nil {
			t.Fatalf("judge a window before its judgment point: %v", err)
		}
		if report.Verdict != PosteriorNotDue || !strings.Contains(report.Reason, "not due before") {
			t.Fatalf("report = %+v, want a not-due verdict naming the due moment", report)
		}
		if reads != 0 {
			t.Fatalf("evidence reads = %d, want none before the window is due", reads)
		}
	})

	t.Run("an unreachable feedback source aborts before the guardrail", func(t *testing.T) {
		guardrailReads := 0
		posterior, _ := posteriorUnderTest(t, 10, JudgePolicy{},
			func(context.Context, string) ([]Feedback, error) {
				return nil, errors.New("feedback store offline")
			},
			func(context.Context, string) (Guardrail, error) { guardrailReads++; return Guardrail{}, nil }, nil)
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge over an unreachable feedback source: %v", err)
		}
		if report.Verdict != PosteriorInsufficient || !strings.Contains(report.Reason, "feedback source unreachable") {
			t.Fatalf("report = %+v, want insufficient naming the unreachable source", report)
		}
		if guardrailReads != 0 {
			t.Fatalf("guardrail reads = %d, want none after the feedback source failed", guardrailReads)
		}
	})

	t.Run("an unreachable guardrail source never judges clean zeros", func(t *testing.T) {
		posterior, _ := posteriorUnderTest(t, 10, JudgePolicy{}, cleanFeedback, func(context.Context, string) (Guardrail, error) {
			return Guardrail{}, errors.New("guardrail store offline")
		}, nil)
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge over an unreachable guardrail source: %v", err)
		}
		if report.Verdict != PosteriorInsufficient || !strings.Contains(report.Reason, "guardrail source unreachable") {
			t.Fatalf("report = %+v, want insufficient instead of a clean judgment", report)
		}
	})

	t.Run("the two-condition join never time-joins a mis-stamped entry", func(t *testing.T) {
		judgeCalls := 0
		posterior, _ := posteriorUnderTest(t, 10, JudgePolicy{}, func(context.Context, string) ([]Feedback, error) {
			entries := determinateFeedback(2, 0)
			for i := 0; i < 4; i++ {
				entries = append(entries, Feedback{Stamp: "sha-1", Consumed: "v0", Evidence: EvidenceRef(fmt.Sprintf("ev:old:%d", i))})
			}
			for i := 0; i < 4; i++ {
				entries = append(entries, Feedback{Stamp: "sha-other", Consumed: "v1", Evidence: EvidenceRef(fmt.Sprintf("ev:other:%d", i))})
			}
			return entries, nil
		}, enabledGuardrail, func(context.Context, Review) (Judgment, error) {
			judgeCalls++
			return Judgment{Decidable: true}, nil
		})
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge over a partially joined sample: %v", err)
		}
		if report.Verdict != PosteriorInsufficient || !strings.Contains(report.Reason, "below threshold") {
			t.Fatalf("report = %+v, want insufficient for the two decidable samples", report)
		}
		if !strings.Contains(report.Reason, "was consumed as") || !strings.Contains(report.Reason, "carries stamp") {
			t.Fatalf("reason = %q, want both failed conditions named", report.Reason)
		}
		if judgeCalls != 0 {
			t.Fatalf("judge calls = %d, want none below the sample threshold", judgeCalls)
		}
	})

	t.Run("a failed judge is insufficient, never healthy", func(t *testing.T) {
		posterior, _ := posteriorUnderTest(t, 10, JudgePolicy{}, cleanFeedback, enabledGuardrail,
			func(context.Context, Review) (Judgment, error) { return Judgment{}, errors.New("judge offline") })
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge over a failing judge: %v", err)
		}
		if report.Verdict != PosteriorInsufficient || !strings.Contains(report.Reason, "llm judge unreachable") {
			t.Fatalf("report = %+v, want insufficient naming the failing judge", report)
		}
	})

	t.Run("an undecidable judge is insufficient, never healthy", func(t *testing.T) {
		posterior, _ := posteriorUnderTest(t, 10, JudgePolicy{}, cleanFeedback, enabledGuardrail,
			func(context.Context, Review) (Judgment, error) {
				return Judgment{Decidable: false, Reason: "answer unparsable"}, nil
			})
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge over an undecidable verdict: %v", err)
		}
		if report.Verdict != PosteriorInsufficient || !strings.Contains(report.Reason, "no decidable verdict") {
			t.Fatalf("report = %+v, want insufficient naming the undecidable verdict", report)
		}
	})

	t.Run("a breached criterion degrades before the judge is consulted", func(t *testing.T) {
		judgeCalls := 0
		posterior, recorded := posteriorUnderTest(t, 10, JudgePolicy{}, func(context.Context, string) ([]Feedback, error) {
			return determinateFeedback(10, 4), nil
		}, enabledGuardrail, func(context.Context, Review) (Judgment, error) {
			judgeCalls++
			return Judgment{Decidable: true}, nil
		})
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge over breached evidence: %v", err)
		}
		if report.Verdict != PosteriorDegraded || !strings.Contains(report.Reason, "negative feedback rate breached") {
			t.Fatalf("report = %+v, want degraded naming the breached criterion", report)
		}
		if len(*recorded) != 1 {
			t.Fatalf("recorded evaluations = %d, want the judgment recorded once", len(*recorded))
		}
		if suggestion := (*recorded)[0].Suggestion; !strings.Contains(suggestion, "refine rollback sha-1") {
			t.Fatalf("suggestion = %q, want the rollback advice naming the registration commit", suggestion)
		}
		if judgeCalls != 0 {
			t.Fatalf("judge calls = %d, want the deterministic criterion judged first", judgeCalls)
		}
	})

	t.Run("a rate exactly at the ceiling is not a breach", func(t *testing.T) {
		posterior, _ := posteriorUnderTest(t, 10, JudgePolicy{}, func(context.Context, string) ([]Feedback, error) {
			return determinateFeedback(10, 3), nil
		}, enabledGuardrail, nil)
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge a rate at the ceiling: %v", err)
		}
		if report.Verdict != PosteriorHealthy {
			t.Fatalf("verdict = %q, want healthy because a rate at the ceiling is not above it", report.Verdict)
		}
	})

	t.Run("disabled governance criteria are named unavailable", func(t *testing.T) {
		posterior, recorded := posteriorUnderTest(t, 10, JudgePolicy{}, cleanFeedback, func(context.Context, string) (Guardrail, error) {
			return Guardrail{Enabled: false, DenialCount: 100, CriticalCount: 100}, nil
		}, nil)
		_, report, err := posterior.Evaluate(ctx, "v1", postAt)
		if err != nil {
			t.Fatalf("judge under disabled governance: %v", err)
		}
		if report.Verdict != PosteriorHealthy {
			t.Fatalf("verdict = %q, want healthy because disabled criteria are not read as breaches", report.Verdict)
		}
		if !strings.Contains(report.Reason, "governance disabled: denial and critical guardrail criteria unavailable") {
			t.Fatalf("reason = %q, want the unavailability annotation beside the verdict", report.Reason)
		}
		if len(*recorded) != 1 {
			t.Fatalf("recorded evaluations = %d, want the judgment recorded once", len(*recorded))
		}
		if summary := (*recorded)[0].Summary; !strings.Contains(summary, "governance disabled") {
			t.Fatalf("summary = %q, want the unavailability annotation in the recorded evidence summary", summary)
		}
	})
}

// TestDigestRendersTheSamePassIdentically pins the digest: one pass at one watermark renders identically, and every reference a bound leaves out is counted.
// - two renders of the same pass at the same watermark are byte-identical
// - the partition lines stop at the partition bound
// - the fact lines stop at the fact bound
// - omitted counts every reference the fact listing left out, and the summary states it
// - a long fact summary is shortened on a rune boundary
// - a pass opened at another watermark is refused by name
// - an absent pass is unavailable by name, never a quiet empty summary
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestDigestRendersTheSamePassIdentically(t *testing.T) {
	ctx := context.Background()
	scan := digestScan(10)
	digest := NewScanDigest(scan, 3)
	since := Watermark{Moment: postAt}

	first, err := digest.Render(ctx, since)
	if err != nil {
		t.Fatalf("render the first digest: %v", err)
	}
	second, err := digest.Render(ctx, since)
	if err != nil {
		t.Fatalf("render the second digest: %v", err)
	}
	if first.Body != second.Body || first.Omitted != second.Omitted {
		t.Fatalf("digests differ over the same pass:\nfirst  %+v\nsecond %+v", first, second)
	}
	if got := strings.Count(first.Body, "\n- p"); got != digestPartitionLimit {
		t.Fatalf("partition lines = %d, want the bound of %d", got, digestPartitionLimit)
	}
	if got := strings.Count(first.Body, "\n- ["); got != 3 {
		t.Fatalf("fact lines = %d, want the bound of 3", got)
	}
	if first.Omitted != 17 {
		t.Fatalf("omitted = %d, want the 17 references beyond the three listed", first.Omitted)
	}
	if !strings.Contains(first.Body, "omitted: 17 fact(s) beyond the listing bound") {
		t.Fatalf("body = %q, want the omitted count stated beside the rendering", first.Body)
	}
	if !strings.Contains(first.Body, "attribution: 3 hydrated fact(s) carry no trigger source") {
		t.Fatalf("body = %q, want the unknown-lineage count stated", first.Body)
	}
	if !strings.Contains(first.Body, "…") || strings.Contains(first.Body, strings.Repeat("x", 121)) {
		t.Fatalf("body = %q, want a long summary shortened on a rune boundary", first.Body)
	}

	if _, err := digest.Render(ctx, Watermark{Moment: postAt.Add(time.Second)}); !errors.Is(err, ErrDigestScanMismatch) {
		t.Fatalf("render at another watermark = %v, want ErrDigestScanMismatch", err)
	}
	if summary, err := NewScanDigest(nil, 0).Render(ctx, since); !errors.Is(err, ErrDigestFaceUnavailable) || summary.Body != "" {
		t.Fatalf("render an absent pass = (%+v, %v), want ErrDigestFaceUnavailable and no body", summary, err)
	}
}

// posteriorUnderTest opens a post-hoc judgment over a ledger whose version was
// consumed turns times at postAt and correlated to one registration commit.
func posteriorUnderTest(t *testing.T, turns int, policy JudgePolicy, feedback FeedbackSource, guardrail GuardrailSource, judge Judge) (*Posterior, *[]Evaluation) {
	t.Helper()
	ledger := NewActivationLedger()
	for i := 0; i < turns; i++ {
		if _, _, err := ledger.Consume(Activation{
			Version:  "v1",
			At:       postAt,
			Evidence: EvidenceRef(fmt.Sprintf("ev:turn:%d", i)),
		}); err != nil {
			t.Fatalf("record consumption %d: %v", i, err)
		}
	}
	if err := ledger.Correlate("v1", "sha-1"); err != nil {
		t.Fatalf("correlate the registration commit: %v", err)
	}
	recorded := []Evaluation{}
	posterior := NewPosterior(ledger, policy, feedback, guardrail, judge, func(_ context.Context, evaluation Evaluation) error {
		recorded = append(recorded, evaluation)
		return nil
	})
	return posterior, &recorded
}

// determinateFeedback returns n feedback entries joined to v1, the first negative
// ones degrading, so a rate is exactly count over the turn count.
func determinateFeedback(n, negative int) []Feedback {
	entries := make([]Feedback, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, Feedback{
			Stamp:    "sha-1",
			Consumed: "v1",
			Degrades: i < negative,
			Evidence: EvidenceRef(fmt.Sprintf("ev:fb:%d", i)),
		})
	}
	return entries
}

// cleanFeedback is the evidence source of a judgment with no degrading feedback.
func cleanFeedback(context.Context, string) ([]Feedback, error) {
	return determinateFeedback(10, 0), nil
}

// enabledGuardrail is the evidence source of an active governance guardrail with no
// refusals and no critical operations.
func enabledGuardrail(context.Context, string) (Guardrail, error) {
	return Guardrail{Enabled: true}, nil
}

// digestScan returns one completed pass over ten partitions and facts hydrated facts,
// each summary long enough to be shortened by the digest bound.
func digestScan(facts int) *ObservationScan {
	scan := &ObservationScan{
		Watermark: postAt,
		Counts:    ScanCounts{ReferenceTotal: 20, HydratedTotal: facts, Unreadable: 1, Foreign: 2, UnknownLineage: 3},
	}
	for i := 0; i < 10; i++ {
		scan.Partitions = append(scan.Partitions, PartitionActivity{
			Name: fmt.Sprintf("p%d", i), Partition: i, References: i, External: i, SelfManaged: 1,
		})
	}
	for i := 0; i < facts; i++ {
		scan.Facts = append(scan.Facts, ScannedFact{
			Identity:    FactIdentity{Key: event.EventID(fmt.Sprintf("fact-%02d", i)), Partition: "p0"},
			Content:     FactContent{Type: "agent.output", Summary: strings.Repeat("x", 130), Timestamp: postAt},
			Attribution: FactAttribution{Lineage: "user", SelfManaged: i%2 == 1},
		})
	}
	return scan
}

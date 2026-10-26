package evolution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#verdict-states
//
// EvidenceWindow is the one-shot evidence window of one improvement version.
//
// Its origin is the activation moment, the moment a real request first consumed
// that version, and the version it names is the one actually consumed. A git
// registration time is a correlation only and never the window's origin, because
// a file takes effect when it lands on disk and registration may follow the
// first real consumption. The window is carried by the refinement ledger entry
// together with the post-hoc report the window produced, so a window always
// connects back to its result. A window that cannot show activation evidence is
// judged insufficient with the missing evidence named, and the zero value never
// means the window is open.
type EvidenceWindow struct {
	// Version names the improvement version whose real consumption opened the
	// window.
	Version string
	// ActivatedAt is the activation moment at which a real request first consumed
	// that version.
	ActivatedAt time.Time
	// ActivationEvidence names the evidence that proves the activation moment:
	// the recorded first real consumption of Version. Its zero value means no
	// activation evidence is on record and is never read as an opened window.
	ActivationEvidence EvidenceRef
}

// PosteriorVerdict is the post-hoc evaluation state of one improvement version after real requests
// consumed it, judged over its evidence window: healthy, degraded, insufficient or not-due.
//
// - healthy: the window reached its judgment point with enough decidable evidence and that evidence shows no degradation.
// - degraded: the same threshold was met but the evidence shows degradation, which yields a rollback proposal.
// - insufficient: an attempted judgment aborted because the decidable evidence fell below the threshold or a source was missing or unreachable; the missing evidence is named.
// - not-due: the window has not reached its judgment point yet, so no judgment is owed.
// - The states are mutually exclusive and never blurred: insufficient never mixes with degraded (an aborted judgment asserts no degradation), not-due is never read as healthy, and insufficient is never folded into healthy.
// - This vocabulary is the post-hoc half only and never reused for the structural gate's pre-publication dispositions (GateVerdict); a verdict not produced yet is the empty PosteriorReport, never a fifth verdict, and that empty value is never read as healthy.
type PosteriorVerdict string

const (
	// PosteriorHealthy reports a due window whose decidable evidence met the
	// threshold and showed no degradation.
	PosteriorHealthy PosteriorVerdict = "healthy"

	// PosteriorDegraded reports a due window whose decidable evidence met the
	// threshold and showed degradation; it yields a rollback proposal.
	PosteriorDegraded PosteriorVerdict = "degraded"

	// PosteriorInsufficient reports an attempted judgment that aborted because
	// the decidable evidence was below threshold or an evidence source was
	// missing or unreachable; the missing evidence is named. It asserts no
	// degradation and is never read as healthy.
	PosteriorInsufficient PosteriorVerdict = "insufficient"

	// PosteriorNotDue reports a window that has not reached its judgment point
	// yet; it is never read as healthy and yields no rollback proposal.
	PosteriorNotDue PosteriorVerdict = "not-due"
)

// PosteriorReport is the post-hoc evaluation report of one improvement version:
// the four-state verdict, the named reason behind it and the evidence it rests
// on.
//
// The report is kept apart from the structural GateReport: the structural
// verdicts judge a candidate declaration before publication, the post-hoc
// states judge an improvement version after real consumption, and neither
// vocabulary is ever substituted for the other. When the verdict is
// insufficient, Reason names the missing evidence and the judgment aborts
// instead of falling back to healthy. Source and statistics fields are declared
// together with their real reviewer or guardrail consumer, so this report
// carries no speculative evaluation table. The zero report means no evaluation
// has run yet; its empty verdict is not a fifth state and is never read as
// healthy.
type PosteriorReport struct {
	// Verdict is the post-hoc state of the improvement version, exactly one of
	// healthy, degraded, insufficient or not-due.
	Verdict PosteriorVerdict
	// Reason is the named reason behind the verdict: the named missing evidence
	// for insufficient, the named ground for degraded.
	Reason string
	// EvidenceRefs names the evidence the verdict rests on.
	EvidenceRefs []EvidenceRef
}

// ErrEvaluationUnrecorded reports a completed post-hoc evaluation whose event
// could not be recorded through the injected recorder. The judgment itself is
// returned as it stands; only the event is missing, and the caller is told so by
// name instead of the failure being swallowed.
var ErrEvaluationUnrecorded = errors.New("evolution: post-hoc evaluation event not recorded")

// Feedback is one user feedback entry joined to an improvement version for the
// post-hoc judgment: the version stamp it carries, the version the producing
// request really consumed, whether it degrades and the evidence it rests on.
//
// Joining is a two-condition exact match and never a time-window guess: the
// entry counts for a version only when the request really consumed that version
// and the stamp names the version or its correlated registration commit. An
// entry missing its consumed version, or carrying a stamp of another version,
// is labeled unknown and excluded from the decidable sample with the reason
// named; it never leaks into a degradation count and never time-joins.
type Feedback struct {
	// Stamp names the version or registration commit the feedback was stamped
	// with when it was recorded.
	Stamp string
	// Consumed names the improvement version the producing request really
	// consumed. It is the attribution fact and is never derived from the stamp.
	Consumed string
	// Degrades reports whether the feedback degrades the consumed version.
	Degrades bool
	// Evidence names the recorded feedback event the entry rests on.
	Evidence EvidenceRef
}

// Guardrail is the governance guardrail statistics of one improvement version:
// the governance refusal count and the critical-operation count observed while
// real requests consumed it.
//
// Enabled is what makes the counts judgeable. When governance is disabled the
// counts are never read and the denial and critical criteria are marked
// unavailable in the report by name, so a silent zero never masquerades as
// "no degradation": the reverse control is that a disabled guardrail carrying
// huge counts still judges nothing, while the same counts under an enabled
// guardrail do.
type Guardrail struct {
	// Enabled reports whether governance guardrails were active while the
	// version was consumed. The counts below are read only when this is true.
	Enabled bool
	// DenialCount counts the governance refusals recorded for the consumed
	// version. It is read only when Enabled is true.
	DenialCount int
	// CriticalCount counts the critical operations recorded for the consumed
	// version. It is read only when Enabled is true.
	CriticalCount int
}

// JudgePolicy is the threshold set of the post-hoc judgment. The wiki defaults
// are carried by DefaultJudgePolicy, and a zero field means "not set" and folds
// to that default; a negative rate ceiling explicitly disables that rate
// criterion instead of pretending it never fires.
//
// A rate is a count over the turn count of the version (the real requests
// recorded as consuming it) and a criterion is breached strictly above its
// ceiling, so a rate exactly at the ceiling is not a breach.
type JudgePolicy struct {
	// JudgeDelay is how long after the activation moment the window becomes
	// due. Zero means due immediately at activation.
	JudgeDelay time.Duration
	// MinSamples is the decidable feedback sample a judgment needs. Zero folds
	// to DefaultMinSamples.
	MinSamples int
	// NegativeRateMax is the negative feedback rate ceiling. Zero folds to the
	// default; a negative value disables this criterion explicitly.
	NegativeRateMax float64
	// DenialRateMax is the governance refusal rate ceiling, judged only while
	// governance is enabled. Zero folds to the default; a negative value
	// disables this criterion explicitly.
	DenialRateMax float64
	// CriticalRateMax is the critical-operation rate ceiling, judged only while
	// governance is enabled. Zero folds to the default; a negative value
	// disables this criterion explicitly.
	CriticalRateMax float64
}

// DefaultMinSamples and the other default guardrail thresholds are the wiki defaults of the post-hoc
// judgment: at least five decidable feedback samples, and the negative feedback, governance refusal
// and critical-operation rates cut at 0.3, 0.3 and 0.2.
const (
	DefaultMinSamples      = 5
	DefaultNegativeRateMax = 0.3
	DefaultDenialRateMax   = 0.3
	DefaultCriticalRateMax = 0.2
)

// DefaultJudgePolicy returns the wiki default thresholds.
func DefaultJudgePolicy() JudgePolicy {
	return JudgePolicy{
		MinSamples:      DefaultMinSamples,
		NegativeRateMax: DefaultNegativeRateMax,
		DenialRateMax:   DefaultDenialRateMax,
		CriticalRateMax: DefaultCriticalRateMax,
	}
}

// normalized folds one policy into its effective form: a zero field takes the
// default and a negative rate ceiling stays negative as the explicit "criterion
// disabled" mark.
func (p JudgePolicy) normalized() JudgePolicy {
	out := p
	if out.MinSamples <= 0 {
		out.MinSamples = DefaultMinSamples
	}
	if out.NegativeRateMax == 0 {
		out.NegativeRateMax = DefaultNegativeRateMax
	}
	if out.DenialRateMax == 0 {
		out.DenialRateMax = DefaultDenialRateMax
	}
	if out.CriticalRateMax == 0 {
		out.CriticalRateMax = DefaultCriticalRateMax
	}
	return out
}

// Review is the evidence set handed to the LLM judge for one due window: the
// version under judgment, its turn count, the decidable feedback entries joined
// to it and its guardrail statistics. The judge reads this set and produces a
// judgment; it never re-derives the joins or the turn count.
type Review struct {
	// Version names the improvement version under judgment.
	Version string
	// Turns is the number of real requests recorded as consuming the version.
	Turns int
	// Joined holds the decidable feedback entries that passed the two-condition
	// join, in recording order.
	Joined []Feedback
	// Guardrail carries the guardrail statistics of the version; its counts are
	// judgeable only when Enabled is true.
	Guardrail Guardrail
}

// Judgment is what the LLM judge answers for one review. Decidable false means
// the judge produced no usable verdict — a failed call, an unparsable answer or
// an answer without a score — and such a judgment asserts nothing: the caller
// reports insufficient and never folds the miss into healthy.
type Judgment struct {
	// Decidable reports whether the judgment carries a usable verdict.
	Decidable bool
	// Degraded reports the verdict: the version degraded. It is read only when
	// Decidable is true.
	Degraded bool
	// Reason states the judge's named ground or the named reason it could not
	// decide.
	Reason string
	// Evidence names the evidence the judgment rests on.
	Evidence []EvidenceRef
}

// Judge is the LLM-judge port of the post-hoc judgment: it reviews one due
// window's evidence set and answers a judgment. A nil Judge means no LLM judge
// is configured and the judgment then rests on the deterministic guardrail
// criteria alone.
type Judge func(ctx context.Context, review Review) (Judgment, error)

// FeedbackSource is the port the post-hoc judgment reads recorded feedback
// through. An error return means the evidence source is unreachable: the
// judgment aborts as insufficient with the source named and never guesses the
// missing feedback.
type FeedbackSource func(ctx context.Context, version string) ([]Feedback, error)

// GuardrailSource is the port the post-hoc judgment reads governance guardrail
// statistics through. An error return means the evidence source is unreachable:
// the judgment aborts as insufficient with the source named.
type GuardrailSource func(ctx context.Context, version string) (Guardrail, error)

// Evaluation is one post-hoc evaluation event: the improvement version, its
// correlated registration commit, the four-state verdict, the evidence summary
// behind it and the advice text the verdict carries.
//
// A degraded verdict carries a rollback advice for the agent to consider — the
// `refine rollback <sha>` command beside a `git diff` review note. The event is
// advice only: the framework never reverts anything itself, reverting is a
// terminal act of the agent. The SHA is the git correlation of the version and
// never the window anchor.
type Evaluation struct {
	// Version names the improvement version the evaluation judged.
	Version string
	// SHA is the registration commit the version correlates to; it is empty
	// when no registration is on record.
	SHA string
	// Verdict is the four-state post-hoc verdict.
	Verdict PosteriorVerdict
	// Summary states the evidence the verdict rests on: sample counts, rates
	// and the named exclusions and unavailable criteria.
	Summary string
	// Suggestion is the advice text of the verdict; it is empty for every
	// verdict that yields no advice.
	Suggestion string
	// EvidenceRefs names the evidence the verdict rests on.
	EvidenceRefs []EvidenceRef
}

// EvaluationRecorder records one post-hoc evaluation event. It is the port
// through which the judgment reaches the event face without importing it. A nil
// recorder records nothing, and a recorder failure never retracts the judgment
// that was already produced; the evaluation reports the unrecorded event with
// ErrEvaluationUnrecorded instead.
type EvaluationRecorder func(ctx context.Context, evaluation Evaluation) error

// Posterior is the post-hoc evaluation of improvement versions: it judges one version over the
// evidence window its real consumption opened, joins feedback by exact attribution, applies the
// deterministic guardrail criteria and the LLM judge, and records one evaluation event per judgment.
//
// - The judgment order is fixed: missing activation evidence, window not due, unreachable evidence source and too few decidable samples each report insufficient or not-due, never healthy.
// - A breached deterministic guardrail criterion reports degraded, judged before and independently of the LLM judge.
// - Only an explicit low judge score reports degraded; a judge that cannot decide reports insufficient, because a failed evaluation never masquerades as healthy.
// - Every degraded verdict yields a rollback advice event and never an automatic revert.
// - Governance-disabled guardrail criteria are marked unavailable by name instead of being read as clean zeros.
type Posterior struct {
	ledger    *ActivationLedger
	policy    JudgePolicy
	feedback  FeedbackSource
	guardrail GuardrailSource
	judge     Judge
	record    EvaluationRecorder
}

// NewPosterior wires the post-hoc evaluation: the consumption ledger whose
// windows it judges, the threshold set, the feedback and guardrail evidence
// ports, the optional LLM judge and the evaluation event recorder. A nil ledger
// is replaced with an empty one so the judgment still answers insufficient
// instead of panicking; nil evidence ports stand in as unreachable sources and
// a nil recorder records nothing.
func NewPosterior(ledger *ActivationLedger, policy JudgePolicy, feedback FeedbackSource, guardrail GuardrailSource, judge Judge, record EvaluationRecorder) *Posterior {
	if ledger == nil {
		ledger = NewActivationLedger()
	}
	return &Posterior{
		ledger:    ledger,
		policy:    policy.normalized(),
		feedback:  feedback,
		guardrail: guardrail,
		judge:     judge,
		record:    record,
	}
}

// joinFeedback runs the two-condition exact join of one feedback list against
// one improvement version: the entry's request must really have consumed the
// version, and the entry's stamp must name the version or its correlated
// registration commit. Entries failing either condition are excluded and named
// with their reason; a time-window join is never substituted for a failed match.
func joinFeedback(entries []Feedback, version, registration string) (joined []Feedback, excluded []string) {
	for _, entry := range entries {
		if entry.Consumed == "" {
			excluded = append(excluded, fmt.Sprintf("feedback %q lacks the consumed version", entry.Evidence))
			continue
		}
		if entry.Consumed != version {
			excluded = append(excluded, fmt.Sprintf("feedback %q was consumed as %q, not %q", entry.Evidence, entry.Consumed, version))
			continue
		}
		if entry.Stamp != version && entry.Stamp != registration {
			excluded = append(excluded, fmt.Sprintf("feedback %q carries stamp %q, neither %q nor its registration", entry.Evidence, entry.Stamp, version))
			continue
		}
		joined = append(joined, entry)
	}
	return joined, excluded
}

// Evaluate runs the post-hoc judgment of one improvement version over the
// evidence window its real consumption opened and records one four-state
// evaluation event for it.
//
// The returned window is the window judged; it is the zero window exactly when
// the verdict is insufficient for missing activation evidence, and that zero
// never means an open window. The returned report is the 00.19 four-state
// PosteriorReport: its Reason names the missing evidence for insufficient and
// the named ground for degraded, and a healthy verdict over disabled governance
// guardrails carries the named unavailability annotation instead of clean
// silence. The error return is nil unless the evaluation event could not be
// recorded, in which case it wraps ErrEvaluationUnrecorded and the judgment is
// still returned as it stands. A zero now is filled with the current time.
func (p *Posterior) Evaluate(ctx context.Context, version string, now time.Time) (EvidenceWindow, PosteriorReport, error) {
	window, open := p.ledger.Window(version)
	if !open || window.ActivationEvidence == "" {
		report := PosteriorReport{
			Verdict: PosteriorInsufficient,
			Reason:  fmt.Sprintf("missing activation evidence: no real consumption of %q is on record; the commit moment is never guessed", version),
		}
		return EvidenceWindow{}, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, "", ""))
	}
	if now.IsZero() {
		now = time.Now()
	}
	if due := window.ActivatedAt.Add(p.policy.JudgeDelay); now.Before(due) {
		report := PosteriorReport{
			Verdict:      PosteriorNotDue,
			Reason:       fmt.Sprintf("judgment not due before %s", due.UTC().Format(time.RFC3339Nano)),
			EvidenceRefs: []EvidenceRef{window.ActivationEvidence},
		}
		return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, "", ""))
	}

	feedbackEntries, err := p.readFeedback(ctx, version)
	if err != nil {
		report := PosteriorReport{
			Verdict:      PosteriorInsufficient,
			Reason:       fmt.Sprintf("feedback source unreachable: %v", err),
			EvidenceRefs: []EvidenceRef{window.ActivationEvidence},
		}
		return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, "", ""))
	}
	guard, err := p.readGuardrail(ctx, version)
	if err != nil {
		report := PosteriorReport{
			Verdict:      PosteriorInsufficient,
			Reason:       fmt.Sprintf("guardrail source unreachable: %v", err),
			EvidenceRefs: []EvidenceRef{window.ActivationEvidence},
		}
		return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, "", ""))
	}

	registration, _ := p.ledger.RegistrationOf(version)
	joined, excluded := joinFeedback(feedbackEntries, version, registration)
	if len(joined) < p.policy.MinSamples {
		report := PosteriorReport{
			Verdict:      PosteriorInsufficient,
			Reason:       fmt.Sprintf("decidable feedback samples below threshold: %d < %d%s", len(joined), p.policy.MinSamples, excludedNote(excluded)),
			EvidenceRefs: append([]EvidenceRef{window.ActivationEvidence}, feedbackEvidence(joined)...),
		}
		return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, "", ""))
	}

	review := Review{
		Version:   version,
		Turns:     len(p.ledger.Consumed(version)),
		Joined:    joined,
		Guardrail: guard,
	}
	summary := reviewSummary(review, excluded)
	rates := rateNotes(review, p.policy)

	if reason := breachReason(review, p.policy); reason != "" {
		report := PosteriorReport{
			Verdict:      PosteriorDegraded,
			Reason:       reason + rates,
			EvidenceRefs: append([]EvidenceRef{window.ActivationEvidence}, feedbackEvidence(joined)...),
		}
		return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, summary, rollbackAdvice(version, registration)))
	}

	if p.judge != nil {
		judgment, err := p.judge(ctx, review)
		if err != nil {
			report := PosteriorReport{
				Verdict:      PosteriorInsufficient,
				Reason:       fmt.Sprintf("llm judge unreachable: %v", err),
				EvidenceRefs: append([]EvidenceRef{window.ActivationEvidence}, feedbackEvidence(joined)...),
			}
			return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, summary, ""))
		}
		if !judgment.Decidable {
			report := PosteriorReport{
				Verdict:      PosteriorInsufficient,
				Reason:       "llm judge produced no decidable verdict: " + judgment.Reason,
				EvidenceRefs: append(append([]EvidenceRef{window.ActivationEvidence}, feedbackEvidence(joined)...), judgment.Evidence...),
			}
			return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, summary, ""))
		}
		if judgment.Degraded {
			report := PosteriorReport{
				Verdict:      PosteriorDegraded,
				Reason:       judgment.Reason + rates,
				EvidenceRefs: append(append([]EvidenceRef{window.ActivationEvidence}, feedbackEvidence(joined)...), judgment.Evidence...),
			}
			return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, summary, rollbackAdvice(version, registration)))
		}
		report := PosteriorReport{
			Verdict:      PosteriorHealthy,
			Reason:       judgment.Reason + rates,
			EvidenceRefs: append(append([]EvidenceRef{window.ActivationEvidence}, feedbackEvidence(joined)...), judgment.Evidence...),
		}
		return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, summary, ""))
	}

	report := PosteriorReport{
		Verdict:      PosteriorHealthy,
		Reason:       "llm judge not configured: the judgment rests on the deterministic guardrail criteria" + rates,
		EvidenceRefs: append([]EvidenceRef{window.ActivationEvidence}, feedbackEvidence(joined)...),
	}
	return window, report, p.recordEvaluation(ctx, evaluationFor(version, p.ledger, report, summary, ""))
}

// readFeedback pulls the recorded feedback of one version through the port; a
// nil port stands in as an unreachable source instead of as an empty list.
func (p *Posterior) readFeedback(ctx context.Context, version string) ([]Feedback, error) {
	if p.feedback == nil {
		return nil, errors.New("feedback source not configured")
	}
	return p.feedback(ctx, version)
}

// readGuardrail pulls the guardrail statistics of one version through the port;
// a nil port stands in as an unreachable source instead of as a disabled one.
func (p *Posterior) readGuardrail(ctx context.Context, version string) (Guardrail, error) {
	if p.guardrail == nil {
		return Guardrail{}, errors.New("guardrail source not configured")
	}
	return p.guardrail(ctx, version)
}

// recordEvaluation writes one evaluation event through the recorder and reports
// its failure by name; a nil recorder records nothing and a failure never
// retracts the judgment, which is handed back through the report return either
// way.
func (p *Posterior) recordEvaluation(ctx context.Context, evaluation Evaluation) error {
	if p.record == nil {
		return nil
	}
	if err := p.record(ctx, evaluation); err != nil {
		return fmt.Errorf("%w: %v", ErrEvaluationUnrecorded, err)
	}
	return nil
}

// evaluationFor assembles the evaluation event of one judgment: the version,
// its correlated registration SHA, the four-state verdict, the evidence summary
// and the advice text the verdict carries.
func evaluationFor(version string, ledger *ActivationLedger, report PosteriorReport, summary, suggestion string) Evaluation {
	sha, _ := ledger.RegistrationOf(version)
	if summary == "" {
		summary = report.Reason
	}
	return Evaluation{
		Version:      version,
		SHA:          sha,
		Verdict:      report.Verdict,
		Summary:      summary,
		Suggestion:   suggestion,
		EvidenceRefs: report.EvidenceRefs,
	}
}

// rollbackAdvice renders the degraded verdict's rollback advice: the refine
// rollback command naming the correlated registration commit beside a git diff
// review note. Without a correlation the advice names the gap instead of
// pointing at an unknown commit; the advice is never executed by the framework.
func rollbackAdvice(version, registration string) string {
	if registration == "" {
		return fmt.Sprintf("refine rollback unavailable: no git registration correlates to %q; review the cited evidence manually", version)
	}
	return fmt.Sprintf("refine rollback %s; review with git diff", registration)
}

// excludedNote renders the named exclusions of the failed joins behind a sample
// count, so an insufficient verdict states what was excluded and why.
func excludedNote(excluded []string) string {
	if len(excluded) == 0 {
		return ""
	}
	return "; excluded: " + strings.Join(excluded, "; ")
}

// feedbackEvidence lists the evidence refs of the joined feedback entries.
func feedbackEvidence(joined []Feedback) []EvidenceRef {
	refs := make([]EvidenceRef, 0, len(joined))
	for _, entry := range joined {
		refs = append(refs, entry.Evidence)
	}
	return refs
}

// reviewSummary renders the evidence summary of one due window: the turn count,
// the decidable sample count, the named exclusions and the guardrail posture.
func reviewSummary(review Review, excluded []string) string {
	parts := []string{
		fmt.Sprintf("turns %d", review.Turns),
		fmt.Sprintf("decidable feedback samples %d", len(review.Joined)),
	}
	if len(excluded) > 0 {
		parts = append(parts, "excluded: "+strings.Join(excluded, ";"))
	}
	if !review.Guardrail.Enabled {
		parts = append(parts, "governance disabled: denial and critical guardrail criteria unavailable")
	}
	return strings.Join(parts, ", ")
}

// rateNotes renders the effective rate posture of one judgment: each computed
// rate, each explicitly disabled criterion and the governance-disabled
// annotation that keeps the denial and critical criteria from reading as clean
// zeros.
func rateNotes(review Review, policy JudgePolicy) string {
	var notes []string
	negative := rate(negativeCount(review.Joined), review.Turns)
	notes = append(notes, fmt.Sprintf("negative feedback rate %.3f", negative))
	if policy.NegativeRateMax < 0 {
		notes = append(notes, "negative feedback rate criterion disabled by policy")
	}
	if !review.Guardrail.Enabled {
		notes = append(notes, "governance disabled: denial and critical guardrail criteria unavailable")
		return " [" + strings.Join(notes, "; ") + "]"
	}
	notes = append(notes, fmt.Sprintf("denial rate %.3f", rate(review.Guardrail.DenialCount, review.Turns)))
	notes = append(notes, fmt.Sprintf("critical rate %.3f", rate(review.Guardrail.CriticalCount, review.Turns)))
	if policy.DenialRateMax < 0 {
		notes = append(notes, "denial rate criterion disabled by policy")
	}
	if policy.CriticalRateMax < 0 {
		notes = append(notes, "critical rate criterion disabled by policy")
	}
	return " [" + strings.Join(notes, "; ") + "]"
}

// breachReason names the first deterministic guardrail criterion breached over
// the review, or reports no breach. A rate is a count over the turn count and a
// criterion is breached strictly above its ceiling. A disabled criterion and
// the governance-disabled denial and critical criteria never breach; their
// counts are not even read.
func breachReason(review Review, policy JudgePolicy) string {
	if policy.NegativeRateMax >= 0 && rate(negativeCount(review.Joined), review.Turns) > policy.NegativeRateMax {
		return fmt.Sprintf("negative feedback rate breached: %.3f > %.3f", rate(negativeCount(review.Joined), review.Turns), policy.NegativeRateMax)
	}
	if !review.Guardrail.Enabled {
		return ""
	}
	if policy.DenialRateMax >= 0 && rate(review.Guardrail.DenialCount, review.Turns) > policy.DenialRateMax {
		return fmt.Sprintf("governance denial rate breached: %.3f > %.3f", rate(review.Guardrail.DenialCount, review.Turns), policy.DenialRateMax)
	}
	if policy.CriticalRateMax >= 0 && rate(review.Guardrail.CriticalCount, review.Turns) > policy.CriticalRateMax {
		return fmt.Sprintf("critical operation rate breached: %.3f > %.3f", rate(review.Guardrail.CriticalCount, review.Turns), policy.CriticalRateMax)
	}
	return ""
}

// negativeCount counts the degrading entries of a joined feedback sample.
func negativeCount(joined []Feedback) int {
	count := 0
	for _, entry := range joined {
		if entry.Degrades {
			count++
		}
	}
	return count
}

// rate computes one count over one turn count. A turn count of zero carries no
// rate at all, so the criterion cannot read a phantom zero as clean.
func rate(count, turns int) float64 {
	if turns <= 0 {
		return 0
	}
	return float64(count) / float64(turns)
}

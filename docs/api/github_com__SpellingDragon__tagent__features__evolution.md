package evolution // import "github.com/SpellingDragon/tagent/features/evolution"

Package evolution owns the self-improvement channel: it records an improvement
a real agent already landed, evaluates that improvement after real requests
consumed it, and proposes a rollback without ever performing one.

An edit to a prompt, skill or script takes effect by itself, so this package
tracks the edit instead of gating it: a file is read hot at the next request
boundary, and only a structural change passes the gate declared here. The three
channels that observe, register and consolidate an improvement live beside it,
one package per channel: meditation reads the fact chain and condenses it,
refine registers a landed edit in git, and consolidation carries the wake-up
vocabulary of the memory channel.

CONSTANTS

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
const (
	// CheckEval names the evaluation check a publication must carry a
	// disposition for.
	CheckEval state.CheckName = state.CheckName(SegmentEval)

	// CheckBenchmark names the benchmark check a publication must carry a
	// disposition for.
	CheckBenchmark state.CheckName = state.CheckName(SegmentBenchmark)

	// CheckApplyGate names the apply gate check a publication must carry a
	// disposition for.
	CheckApplyGate state.CheckName = state.CheckName(SegmentApplyGate)
)
const (
	DefaultMinSamples      = 5
	DefaultNegativeRateMax = 0.3
	DefaultDenialRateMax   = 0.3
	DefaultCriticalRateMax = 0.2
)
    DefaultMinSamples and the other default guardrail thresholds are the wiki
    defaults of the post-hoc judgment: at least five decidable feedback samples,
    and the negative feedback, governance refusal and critical-operation rates
    cut at 0.3, 0.3 and 0.2.

const DefaultJudgeScoreMax = 1.0

    DefaultJudgeScoreMax is the score ceiling of the LLM judge: only an explicit
    low score — strictly below the threshold — proposes a rollback. A judge that
    cannot produce such an explicit score decides nothing.

const EvaluationCriterionRef = "evaluation"

    EvaluationCriterionRef names the declaration reference that carries the
    evaluation fixtures of one agent: one reference per fixture, with the case
    parameters beside it. The reference is declaration data the composition root
    resolves nothing of, so an agent tooling a real tool never collides with it.

VARIABLES

var ErrActivationUnusable = errors.New("evolution: activation record unusable: version, moment and evidence are all required")

    ErrActivationUnusable reports an activation record that cannot prove a
    real consumption: one of its version, moment or evidence is missing. Such
    a record is refused instead of being folded into the consumption ledger,
    because an unusable record can never stand as the activation evidence of a
    window.

var ErrCandidateRefused = errors.New("evolution: the candidate refused the gate probe")
    ErrCandidateRefused reports a declared change whose probe the candidate
    refused. The refusal carries the probe's own named root cause underneath it.

var ErrCheckMissing = errors.New("evolution: the apply gate holds no evidence for a required check")
    ErrCheckMissing reports an apply gate that holds no evidence for a required
    check. A missing required check refuses the publication.

var ErrCorrelationConflict = errors.New("evolution: git correlation conflicts with the recorded mapping")
    ErrCorrelationConflict reports a git correlation that contradicts the
    mapping already on record for the version it names. The recorded mapping
    stands and the conflicting correlation is refused, so a registration can
    never silently relabel a version that consumption evidence already bound to
    another commit.

var ErrCorrelationUnusable = errors.New("evolution: git correlation unusable: version and registration sha are both required")
    ErrCorrelationUnusable reports a git correlation that names nothing:
    an empty version or an empty registration SHA. Such a correlation is refused
    before any mapping is touched.

var ErrCriterionMissing = errors.New("evolution: the declaration carries no evaluation criterion for a declared change")
    ErrCriterionMissing reports a declared change whose declaration carries no
    evaluation criterion on the judged face. A face without a declared criterion
    cannot be judged, the segment that observes it reports insufficient evidence
    beside this cause, and that verdict is never a pass.

var ErrCriterionNotMet = errors.New("evolution: the produced answer does not meet the declared evaluation criterion")
    ErrCriterionNotMet reports a declared change whose produced answer
    contradicts the evaluation criterion its declaration carries. The refusal
    names the produced answer beside the declared value underneath it, and a
    contradiction is never reported as a pass.

var ErrDeclarationDiverged = errors.New("evolution: the execution binding diverges from the declaration plan the gate consumes")
    ErrDeclarationDiverged reports a gate call whose execution binding was
    prepared from a declaration plan other than the declaration plan the gate
    consumes. The pair is refused outright and never resolved to either side.

var ErrEvaluationUnrecorded = errors.New("evolution: post-hoc evaluation event not recorded")
    ErrEvaluationUnrecorded reports a completed post-hoc evaluation whose event
    could not be recorded through the injected recorder. The judgment itself is
    returned as it stands; only the event is missing, and the caller is told so
    by name instead of the failure being swallowed.

var ErrEvidenceMissing = errors.New("evolution: the gate holds no complete evidence for a declared change")
    ErrEvidenceMissing reports gate evidence for a declared change that is
    missing or unreachable. The segment that observes it reports insufficient
    evidence beside this cause, and that verdict is never a pass.

TYPES

type Activation struct {
	// Version names the improvement version the request really consumed.
	Version string
	// At is the moment the real consumption happened.
	At time.Time
	// Evidence names the recorded consumption that proves the moment.
	Evidence EvidenceRef
}
    Activation is one real request consumption of one improvement version:
    the version actually consumed, the moment the consumption happened and the
    evidence that proves it.

    This is the only thing that anchors an evidence window. A file takes effect
    when it lands on disk, so the first real consumption routinely precedes the
    git registration (t0 write < t1 consume < t2 register); the window opens
    at t1 and a registration at t2 never opens, anchors or moves it. All three
    fields are required: a record missing its version, moment or evidence cannot
    prove a consumption and is refused with ErrActivationUnusable.

type ActivationLedger struct {
	// Has unexported fields.
}
    ActivationLedger is the in-memory table of real consumption evidence,
    rebuilt from recorded events and never persisted: the one-shot window,
    the consumptions and the git correlation per version.

    - The window anchor is the activation moment: the first Consume opens it,
    later ones never move it, and a git registration never opens one. -
    Consumption records are never relabeled: Correlate only adds the git mapping
    and refuses a contradicting one with ErrCorrelationConflict.

func NewActivationLedger() *ActivationLedger
    NewActivationLedger builds an empty consumption ledger: no version has a
    window, a consumption or a correlation yet.

func (l *ActivationLedger) Consume(a Activation) (EvidenceWindow, bool, error)
    Consume records one real request consumption of one improvement version and
    returns the version's evidence window together with whether this call opened
    it.

    The first consumption of a version opens the window at that activation
    moment and reports opened=true; every later consumption is appended to the
    consumption records of the version and reports opened=false with the window
    unchanged, so the anchor stays at the first real consumption no matter how
    many requests follow. A record missing its version, moment or evidence is
    refused with ErrActivationUnusable and changes nothing.

func (l *ActivationLedger) Consumed(version string) []Activation
    Consumed lists every real consumption recorded for a version, in recording
    order. The list is the turn count of the version: one entry is one real
    request that consumed it. A defensive copy is returned, so a caller cannot
    rewrite the recorded facts.

func (l *ActivationLedger) Correlate(version, sha string) error
    Correlate records the git registration commit of one improvement version.

    The registration SHA is a git correlation only: it links feedback to the
    real consumption version and never opens, anchors or moves an evaluation
    window. A version never consumed keeps no window no matter how often it
    is correlated, and this call never re-stamps the requests, feedback or
    window already attributed to the version. Correlating the same SHA twice is
    idempotent; a different SHA for a version already correlated is refused with
    ErrCorrelationConflict and the recorded mapping stands. An empty version or
    SHA is refused with ErrCorrelationUnusable.

func (l *ActivationLedger) RegistrationOf(version string) (string, bool)
    RegistrationOf returns the registration commit a version correlates to and
    whether one is on record. The SHA is a git correlation only and never the
    anchor of any window.

func (l *ActivationLedger) Window(version string) (EvidenceWindow, bool)
    Window returns the evidence window of a version and whether it has opened.
    A version never consumed returns the zero window and false; that zero window
    never means an open window.

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
    Evaluation is one post-hoc evaluation event: the improvement version,
    its correlated registration commit, the four-state verdict, the evidence
    summary behind it and the advice text the verdict carries.

    A degraded verdict carries a rollback advice for the agent to consider
    — the `refine rollback <sha>` command beside a `git diff` review note.
    The event is advice only: the framework never reverts anything itself,
    reverting is a terminal act of the agent. The SHA is the git correlation of
    the version and never the window anchor.

type EvaluationRecorder func(ctx context.Context, evaluation Evaluation) error
    EvaluationRecorder records one post-hoc evaluation event. It is the port
    through which the judgment reaches the event face without importing it.
    A nil recorder records nothing, and a recorder failure never retracts the
    judgment that was already produced; the evaluation reports the unrecorded
    event with ErrEvaluationUnrecorded instead.

type EvidenceRef string
    EvidenceRef names one piece of evidence a gate or post-hoc report cites:
    a run identifier, a check result or another durable reference a reader can
    resolve and re-verify. A verdict that is not backed by resolvable evidence
    is never a pass.

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

    EvidenceWindow is the one-shot evidence window of one improvement version.

    Its origin is the activation moment, the moment a real request first
    consumed that version, and the version it names is the one actually
    consumed. A git registration time is a correlation only and never the
    window's origin, because a file takes effect when it lands on disk and
    registration may follow the first real consumption. The window is carried
    by the refinement ledger entry together with the post-hoc report the window
    produced, so a window always connects back to its result. A window that
    cannot show activation evidence is judged insufficient with the missing
    evidence named, and the zero value never means the window is open.

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
    Feedback is one user feedback entry joined to an improvement version for the
    post-hoc judgment: the version stamp it carries, the version the producing
    request really consumed, whether it degrades and the evidence it rests on.

    Joining is a two-condition exact match and never a time-window guess:
    the entry counts for a version only when the request really consumed that
    version and the stamp names the version or its correlated registration
    commit. An entry missing its consumed version, or carrying a stamp of
    another version, is labeled unknown and excluded from the decidable sample
    with the reason named; it never leaks into a degradation count and never
    time-joins.

type FeedbackSource func(ctx context.Context, version string) ([]Feedback, error)
    FeedbackSource is the port the post-hoc judgment reads recorded feedback
    through. An error return means the evidence source is unreachable: the
    judgment aborts as insufficient with the source named and never guesses the
    missing feedback.

type GateError struct {
	// Segment names the structural gate segment that failed.
	Segment Segment
	// Scope names the declaration scope the failure covers.
	Scope string
	// Cause is the root cause chain of the failure.
	Cause error
}
    GateError is the named failure of one structural gate segment over one
    declaration scope: the segment that failed, the scope the failure covers
    and the root cause chain underneath. Unwrap exposes that chain, so a caller
    reaches the named root of a failure through errors.Is and errors.As instead
    of matching rendered text.

func (e *GateError) Error() string
    Error renders the failed segment, the scope it covered and the cause chain.

func (e *GateError) Unwrap() error
    Unwrap exposes the root cause chain of the failure.

type GateReport struct {
	// Verdict is the segment's disposition for the candidate.
	Verdict GateVerdict
	// Reason is the named reason behind the verdict: the exemption reason for
	// not-applicable, the named missing evidence for insufficient evidence, and
	// the named ground for a refusal.
	Reason string
	// Scope is the scope the disposition covers: the part of the candidate or
	// the set of checks the verdict, exemption or refusal applies to.
	Scope string
	// EvidenceRefs names the evidence the verdict rests on. A verdict of pass
	// rests on reachable evidence and never on an empty list.
	EvidenceRefs []EvidenceRef
}
    GateReport is the disposition one structural gate segment returns for a
    candidate declaration plan, with everything a reader needs to audit it:
    the verdict, the reason behind it, the scope the disposition covers and the
    evidence the verdict rests on.

    A gate never returns a bare verdict. A not-applicable disposition carries
    its exemption reason and scope instead of counting as evidence, an
    insufficient evidence disposition names what is missing, and a disposition
    without reachable evidence is never a pass. The structural vocabulary of
    Verdict judges a candidate before publication and is never reused as the
    post-hoc evaluation states of an improvement version (PosteriorVerdict):
    the two vocabularies are disjoint. The zero GateReport carries no verdict
    and must not be read as a pass.

type GateVerdict string
    GateVerdict is the disposition one structural gate segment reports for a
    candidate declaration plan.

    The verdicts are mutually exclusive and the three non-passing dispositions
    stay distinguishable: a segment that does not apply, a segment whose
    evidence is missing and a segment that refuses are three different facts.
    Insufficient evidence is never reported as a pass. This vocabulary judges a
    structural candidate before publication and is never reused as the post-hoc
    evaluation states of an improvement version; those live in PosteriorVerdict
    and never here.

const (
	// GateVerdictPass reports that the segment ran and its evidence passed.
	GateVerdictPass GateVerdict = "pass"

	// GateVerdictReject reports that the segment ran and refused the candidate.
	GateVerdictReject GateVerdict = "reject"

	// GateVerdictNotApplicable reports that the segment does not apply to this
	// candidate; the exclusion carries its reason and scope.
	GateVerdictNotApplicable GateVerdict = "not-applicable"

	// GateVerdictInsufficientEvidence reports that the segment's evidence is
	// missing or unreachable. It is never reported as a pass.
	GateVerdictInsufficientEvidence GateVerdict = "insufficient-evidence"
)
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
    Guardrail is the governance guardrail statistics of one improvement version:
    the governance refusal count and the critical-operation count observed while
    real requests consumed it.

    Enabled is what makes the counts judgeable. When governance is disabled
    the counts are never read and the denial and critical criteria are marked
    unavailable in the report by name, so a silent zero never masquerades as
    "no degradation": the reverse control is that a disabled guardrail carrying
    huge counts still judges nothing, while the same counts under an enabled
    guardrail do.

type GuardrailSource func(ctx context.Context, version string) (Guardrail, error)
    GuardrailSource is the port the post-hoc judgment reads governance
    guardrail statistics through. An error return means the evidence source is
    unreachable: the judgment aborts as insufficient with the source named.

type Judge func(ctx context.Context, review Review) (Judgment, error)
    Judge is the LLM-judge port of the post-hoc judgment: it reviews one due
    window's evidence set and answers a judgment. A nil Judge means no LLM judge
    is configured and the judgment then rests on the deterministic guardrail
    criteria alone.

func NewModelJudge(m model.Model, threshold float64) Judge
    NewModelJudge builds the default LLM judge over the public model face:
    it reviews one due window's evidence set with one model request and folds
    the answer into a Judgment.

    The judge is conservative on every path. Only an explicit score strictly
    below the threshold reports Degraded; a score at or above it reports a
    healthy judgment; and every failure — a failed model entry, a stream that
    never completed, an unparsable answer or an answer without a score — is
    digested into a Judgment with Decidable false and never an error, so a judge
    miss surfaces as "no decidable verdict" and is reported insufficient instead
    of masquerading as healthy. The answer is read as JSON from the first brace
    of the content, so a markdown fence around it is tolerated; the score is
    clamped into [0,1] and a missing reason is filled with a named placeholder.

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
    JudgePolicy is the threshold set of the post-hoc judgment. The wiki defaults
    are carried by DefaultJudgePolicy, and a zero field means "not set" and
    folds to that default; a negative rate ceiling explicitly disables that rate
    criterion instead of pretending it never fires.

    A rate is a count over the turn count of the version (the real requests
    recorded as consuming it) and a criterion is breached strictly above its
    ceiling, so a rate exactly at the ceiling is not a breach.

func DefaultJudgePolicy() JudgePolicy
    DefaultJudgePolicy returns the wiki default thresholds.

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
    Judgment is what the LLM judge answers for one review. Decidable false
    means the judge produced no usable verdict — a failed call, an unparsable
    answer or an answer without a score — and such a judgment asserts nothing:
    the caller reports insufficient and never folds the miss into healthy.

type Policy struct {
	// Has unexported fields.
}

    Policy is the governance adapter of the structural gate: it dispositions the
    checks of one publication candidate by running the gate's segments over the
    same declaration plan and the same generation of the execution binding the
    candidate carries. Each disposition is filed under its check name beside
    the reason, scope and evidence references of the gate report it comes from,
    and a failing segment keeps its named root cause chain in the returned error
    so the refusal it causes stays attributable. Missing evidence is filed as an
    unanswered check and never as a passing one.

func NewPolicy(gate StructuralGate) *Policy
    NewPolicy returns the governance policy over one structural gate.

func (p *Policy) Check(ctx context.Context, plan state.StatePlan) (map[state.CheckName]state.CheckAnswer, error)
    Check evaluates one publication candidate and answers each check under the
    name it is filed by. The gate segments run over the declaration plan the
    candidate carries beside the candidate itself, and the errors of the failing
    segments are returned beside the answers so a refused publication reaches
    their named roots.

type Posterior struct {
	// Has unexported fields.
}
    Posterior is the post-hoc evaluation of improvement versions: it judges one
    version over the evidence window its real consumption opened, joins feedback
    by exact attribution, applies the deterministic guardrail criteria and the
    LLM judge, and records one evaluation event per judgment.

    - The judgment order is fixed: missing activation evidence, window not due,
    unreachable evidence source and too few decidable samples each report
    insufficient or not-due, never healthy. - A breached deterministic
    guardrail criterion reports degraded, judged before and independently
    of the LLM judge. - Only an explicit low judge score reports degraded;
    a judge that cannot decide reports insufficient, because a failed evaluation
    never masquerades as healthy. - Every degraded verdict yields a rollback
    advice event and never an automatic revert. - Governance-disabled guardrail
    criteria are marked unavailable by name instead of being read as clean
    zeros.

func NewPosterior(ledger *ActivationLedger, policy JudgePolicy, feedback FeedbackSource, guardrail GuardrailSource, judge Judge, record EvaluationRecorder) *Posterior
    NewPosterior wires the post-hoc evaluation: the consumption ledger
    whose windows it judges, the threshold set, the feedback and guardrail
    evidence ports, the optional LLM judge and the evaluation event recorder.
    A nil ledger is replaced with an empty one so the judgment still answers
    insufficient instead of panicking; nil evidence ports stand in as
    unreachable sources and a nil recorder records nothing.

func (p *Posterior) Evaluate(ctx context.Context, version string, now time.Time) (EvidenceWindow, PosteriorReport, error)
    Evaluate runs the post-hoc judgment of one improvement version over the
    evidence window its real consumption opened and records one four-state
    evaluation event for it.

    The returned window is the window judged; it is the zero window exactly when
    the verdict is insufficient for missing activation evidence, and that zero
    never means an open window. The returned report is the 00.19 four-state
    PosteriorReport: its Reason names the missing evidence for insufficient
    and the named ground for degraded, and a healthy verdict over disabled
    governance guardrails carries the named unavailability annotation instead of
    clean silence. The error return is nil unless the evaluation event could not
    be recorded, in which case it wraps ErrEvaluationUnrecorded and the judgment
    is still returned as it stands. A zero now is filled with the current time.

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
    PosteriorReport is the post-hoc evaluation report of one improvement
    version: the four-state verdict, the named reason behind it and the evidence
    it rests on.

    The report is kept apart from the structural GateReport: the
    structural verdicts judge a candidate declaration before publication,
    the post-hoc states judge an improvement version after real consumption,
    and neither vocabulary is ever substituted for the other. When the verdict
    is insufficient, Reason names the missing evidence and the judgment aborts
    instead of falling back to healthy. Source and statistics fields are
    declared together with their real reviewer or guardrail consumer, so this
    report carries no speculative evaluation table. The zero report means no
    evaluation has run yet; its empty verdict is not a fifth state and is never
    read as healthy.

type PosteriorVerdict string
    PosteriorVerdict is the post-hoc evaluation state of one improvement version
    after real requests consumed it, judged over its evidence window: healthy,
    degraded, insufficient or not-due.

    - healthy: the window reached its judgment point with enough decidable
    evidence and that evidence shows no degradation. - degraded: the same
    threshold was met but the evidence shows degradation, which yields a
    rollback proposal. - insufficient: an attempted judgment aborted because
    the decidable evidence fell below the threshold or a source was missing or
    unreachable; the missing evidence is named. - not-due: the window has not
    reached its judgment point yet, so no judgment is owed. - The states are
    mutually exclusive and never blurred: insufficient never mixes with degraded
    (an aborted judgment asserts no degradation), not-due is never read as
    healthy, and insufficient is never folded into healthy. - This vocabulary
    is the post-hoc half only and never reused for the structural gate's
    pre-publication dispositions (GateVerdict); a verdict not produced yet is
    the empty PosteriorReport, never a fifth verdict, and that empty value is
    never read as healthy.

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
    Review is the evidence set handed to the LLM judge for one due window:
    the version under judgment, its turn count, the decidable feedback entries
    joined to it and its guardrail statistics. The judge reads this set and
    produces a judgment; it never re-derives the joins or the turn count.

type Segment string
    Segment names one segment of the structural gate over one candidate:
    the evaluation segment, the benchmark segment or the apply gate.

const (
	// SegmentEval names the evaluation segment of the structural gate.
	SegmentEval Segment = "eval"

	// SegmentBenchmark names the benchmark segment of the structural gate.
	SegmentBenchmark Segment = "benchmark"

	// SegmentApplyGate names the apply gate of the structural gate.
	SegmentApplyGate Segment = "apply-gate"
)
type StructuralGate interface {
	// Eval reports whether the candidate's declarations meet their evaluation
	// evidence. The report carries the disposition with its reason, scope and
	// evidence references. Evidence that is missing or unreachable yields
	// insufficient evidence, which is never a pass.
	Eval(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error)

	// Benchmark reports whether the candidate's declarations meet their benchmark
	// evidence. The report carries the disposition with its reason, scope and
	// evidence references. Evidence that is missing or unreachable yields
	// insufficient evidence, which is never a pass.
	Benchmark(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error)

	// ApplyGate reports whether the candidate's declaration and binding may be
	// applied as the next generation. A missing required check refuses the
	// publication, and a check that does not apply to this candidate is recorded
	// in the report with its reason and scope instead of being counted as
	// evidence.
	ApplyGate(ctx context.Context, plan state.OrgPlan, binding state.StatePlan) (GateReport, error)
}
    StructuralGate is the ordered gate a candidate organization plan passes
    through before it becomes the next effective generation: Eval, then
    Benchmark, then ApplyGate.

    Every segment receives the same declaration plan and the same generation of
    the prepared execution binding, so no segment compiles a second declaration
    or a shadow binding of its own. A segment reports its disposition in a
    GateReport — verdict, reason, scope and evidence references together —
    and keeps a failure's named root cause reachable through errors.Is and
    errors.As rather than flattened into a bare string. A segment whose evidence
    is missing is never reported as a pass. The redline list (4 rules):
    MUST hand every segment the same declaration plan and the same prepared
    binding generation. MUST keep a named failure's root cause reachable through
    errors.Is and errors.As. MUST NOT report a segment whose evidence is missing
    as a pass. MUST NOT compile a second declaration or a shadow binding inside
    a segment.

func NewStructuralGate() StructuralGate

    NewStructuralGate returns the structural gate over the declaration plan and
    the prepared execution binding each call carries in. Every segment consumes
    that one pair through the same admission and the same evidence evaluation,
    so the gate parses no configuration of its own and holds no second
    declaration beside the one it was handed. The gate files what its evaluation
    and benchmark segments observed per pair, and the apply gate consumes those
    very records instead of driving the candidate binding again.

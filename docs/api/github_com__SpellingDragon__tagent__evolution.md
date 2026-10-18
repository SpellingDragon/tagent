package evolution // import "github.com/SpellingDragon/tagent/evolution"

Package evolution owns the self-improvement channel: it records an improvement
a real agent already landed, evaluates that improvement after real requests
consumed it, and proposes a rollback without ever performing one.

An edit to a prompt, skill or script takes effect by itself, so this package
tracks the edit instead of gating it: a file is read hot at the next request
boundary, and only a structural change passes the gate declared here.

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
	// DefaultObservationPageLimit bounds one pass over the observed face; a face
	// with a longer backlog reports the bound instead of growing the pass.
	DefaultObservationPageLimit = 1000
	// DefaultDigestFactLimit bounds how many hydrated facts one digest lists.
	DefaultDigestFactLimit = 8
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

const ImprovementMarker = "[self-improve]"
    ImprovementMarker is the line-anchored marker an improvement registration
    commit's subject opens with. Generation, listing and rollback verification
    share this one constant, and its line anchoring keeps the reverts git writes
    as `Revert "[self-improve] …"` out of the ledger, so a rollback is never
    re-read as a fresh improvement.

const LineageConsolidationHint = "consolidation_hint"
    LineageConsolidationHint is the trigger-source stamp of the internal
    consolidation wake-up: self-managed traffic whose payload is a candidate
    list and never a host artifact.

const LineageReflection = "meditation"

    LineageReflection is the trigger-source stamp a reflection input carries:
    the reflection turn's own lineage, which is self-managed traffic, so an
    agent's own reflection never makes it reflect on itself again.

VARIABLES

var (
	// ErrNotGitRepository reports a refine operation whose run directory is not
	// a git work tree. The file edit itself already took effect on disk, so the
	// registration degrades to a plain named failure and never blocks the edit.
	ErrNotGitRepository = errors.New("evolution: refine needs a git repository (改进登记需 git 仓)")

	// ErrGitOperationInProgress reports a refine mutation refused because a git
	// revert or merge is still open in the repository. Registering during an
	// open revert would commit through it and swallow the sequencer state, so
	// the registration stops before any git command mutates the repository.
	ErrGitOperationInProgress = errors.New("evolution: refine refuses to mutate while a git revert or merge is open")

	// ErrImprovementUnrecorded reports a registration whose commit landed but
	// whose improvement event could not be recorded. The commit stays, because
	// its SHA is a correlation only; the unrecorded event is reported by name.
	ErrImprovementUnrecorded = errors.New("evolution: the registration committed but its improvement event was not recorded")

	// ErrImprovementUnknown reports a rollback naming a commit that does not
	// resolve to any commit in the repository.
	ErrImprovementUnknown = errors.New("evolution: no commit resolves to the named improvement")

	// ErrCommitNotImprovement reports a rollback naming a commit that carries no
	// improvement marker. A user's own commit is never reverted, so the rollback
	// is refused before git is touched.
	ErrCommitNotImprovement = errors.New("evolution: only a commit registered under the improvement marker may be rolled back")

	// ErrRevertConflict reports a rollback whose revert stopped on a conflict.
	// The git output and the disposition guidance travel back to the agent, and
	// the repository keeps the open revert for the agent to resolve or abort.
	ErrRevertConflict = errors.New("evolution: the revert left a conflict for the agent to resolve")
)
    ErrNotGitRepository and the other named failures of the git-native refine
    surface are reachable through errors.Is over the Unwrap chain rather than
    rendered text, so a caller reads the refusal it needs without matching
    messages.

var ErrActivationUnusable = errors.New("evolution: activation record unusable: version, moment and evidence are all required")

    ErrActivationUnusable reports an activation record that cannot prove a
    real consumption: one of its version, moment or evidence is missing. Such
    a record is refused instead of being folded into the consumption ledger,
    because an unusable record can never stand as the activation evidence of a
    window.

var ErrAnchorStoreUnusable = errors.New("evolution: the anchor store requires a path")
    ErrAnchorStoreUnusable reports an anchor store built without a path:
    a watermark that has nowhere to be recorded cannot carry across a restart.

var ErrAnchorUnreadable = errors.New("evolution: the anchor record cannot be read")
    ErrAnchorUnreadable reports an anchor record that exists but cannot be read
    or parsed. The gate reads it as an unestablished watermark and stays closed;
    no default and no fallback criterion is substituted for it.

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

var ErrDigestFaceUnavailable = errors.New("evolution: the digest has no observed pass to render")
    ErrDigestFaceUnavailable reports a digest asked to render without the pass
    it summarizes. An absent pass is named as unavailable, never presented as a
    quiet face.

var ErrDigestScanMismatch = errors.New("evolution: the observed pass opened at another watermark")
    ErrDigestScanMismatch reports a digest asked to render a pass that opened
    at a watermark other than the one it was asked about. The pass a digest
    summarizes is the one its caller already read, so the rendering never
    triggers a second pass over the fact chain.

var ErrEvaluationUnrecorded = errors.New("evolution: post-hoc evaluation event not recorded")

    ErrEvaluationUnrecorded reports a completed post-hoc evaluation whose event
    could not be recorded through the injected recorder. The judgment itself is
    returned as it stands; only the event is missing, and the caller is told so
    by name instead of the failure being swallowed.

var ErrEvidenceMissing = errors.New("evolution: the gate holds no complete evidence for a declared change")
    ErrEvidenceMissing reports gate evidence for a declared change that is
    missing or unreachable. The segment that observes it reports insufficient
    evidence beside this cause, and that verdict is never a pass.

var ErrNoRegisterPaths = errors.New("evolution: refine register needs at least one controlled path")
    ErrNoRegisterPaths reports a registration that names no controlled path to
    record. A registration with no explicit pathspec has nothing to stage and
    commit and is refused before git is touched.

var ErrObservationScopeOutOfBounds = errors.New("evolution: the observed namespaces reach outside the authorized read scope")
    ErrObservationScopeOutOfBounds reports an observation face that names a
    namespace outside the authorized read scope. The face is refused by name at
    assembly instead of being trimmed: reading less than declared would silently
    change what the reflection observes.

var ErrObservedFaceUnwired = errors.New("evolution: the observed fact-chain read face is not wired")
    ErrObservedFaceUnwired reports a pass attempted while the read face of
    the observed fact chain is not wired. The reflection gate stays closed:
    the fact chain is the only data face of the freshness judgment.

var ErrPathOutOfScope = errors.New("evolution: refine register refuses a path outside the controlled set")
    ErrPathOutOfScope reports a registration naming a path outside the
    controlled path set. The whole registration is refused before any git
    command runs, so an out-of-scope path never reaches `git add` or `git
    commit` and the repository keeps the state it had.

var ErrReflectionAgentUnnamed = errors.New("evolution: reflection requires the observing agent's namespace")

    ErrReflectionAgentUnnamed reports a reflection assembled without the
    observing agent's own identity: an observation face cannot be resolved and
    no loop session can be addressed without it.

var ErrReflectionInputUnkeyed = errors.New("evolution: the reflection input committed no identity")
    ErrReflectionInputUnkeyed reports a reflection input that committed no
    identity: without one, a consumption report cannot name the turn it settles.

var ErrReflectionKeyMismatch = errors.New("evolution: the outcome names a reflection input other than the one awaiting consumption")
    ErrReflectionKeyMismatch reports an outcome naming a reflection input other
    than the one awaiting consumption. The report is refused, so a report about
    an older turn can never settle a newer one.

var ErrReflectionNotPending = errors.New("evolution: no reflection input awaits an outcome")
    ErrReflectionNotPending reports an outcome for a reflection input that is
    not awaiting one: a repeated or stale report never moves the watermark.

var ErrWatermarkSourceUnwired = errors.New("evolution: the execution watermark source is not wired")
    ErrWatermarkSourceUnwired reports a tick attempted while the execution
    watermark has no source. The gate stays closed instead of reading a fresh
    default.

FUNCTIONS

func DefaultProtectedPaths() []string

    DefaultProtectedPaths returns the controlled path patterns the refine
    surface guards by default: the prompt and skill trees. A registration may
    record only the files under these patterns, so an edit to anything else is
    refused before git is touched, and the file itself still took effect on disk
    the moment it was written.

func DeliverableLineage(stamp string) bool
    DeliverableLineage reports whether a trigger-source stamp names traffic the
    host may see delivered. A stamp outside the declared vocabulary is internal
    and withheld, so an unrecognised stamp is never treated as host traffic.

func SelfManagedLineage(stamp string) bool
    SelfManagedLineage reports whether a trigger-source stamp names traffic
    the agent initiated for itself rather than an interaction someone awaits,
    from the same vocabulary as DeliverableLineage so no consumer holds a list
    of its own.

    A stamp the vocabulary does not name derives as self-managed: an event
    without a readable trigger source is never counted as freshness, and a
    reflection input or a consolidation hint never re-arms the judgment that
    produced it.

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

type AnchorStore struct {
	// Has unexported fields.
}
    AnchorStore is the anchor record of one observing agent: the pair of
    loop anchors kept in one file and rewritten atomically, so a reader never
    observes a half-written record.

    The store is the gate's only watermark source. A record that is absent reads
    as a start with no prior execution, while a record that exists but cannot be
    read or parsed is refused by name: an unreadable watermark is never reported
    as a fresh one.

func NewAnchorStore(path string) (*AnchorStore, error)
    NewAnchorStore returns the anchor store of the record at path, creating
    its directory when the directory is absent. A store built without a path is
    refused with ErrAnchorStoreUnusable.

func (s *AnchorStore) Advance(ctx context.Context, moment time.Time) error
    Advance moves the execution watermark to the moment a consumed reflection
    turn was injected at and keeps the session anchor as it stands. A moment
    that is not after the recorded watermark leaves the record as it is,
    so the watermark never moves backwards.

func (s *AnchorStore) Current(ctx context.Context) (Watermark, error)
    Current reports the execution watermark the anchor record holds. An absent
    record reports the zero watermark of a start with no prior execution,
    and an unreadable record is refused with ErrAnchorUnreadable so the gate
    stays closed.

func (s *AnchorStore) Load() (Anchors, error)
    Load reads the anchor record. An absent record is a start with no prior
    execution and carries no error; a record that cannot be read or parsed is
    refused with ErrAnchorUnreadable.

func (s *AnchorStore) Path() string
    Path reports the record path this store reads and writes.

func (s *AnchorStore) Save(record Anchors) error
    Save rewrites the anchor record atomically: the new record is written beside
    it and renamed over it, so no reader observes a partial record.

type Anchors struct {
	// LastTurnEnd is the moment the last turn of the session ended.
	LastTurnEnd time.Time `json:"last_turn_end"`
	// LastMeditation is the moment of the last consumed reflection turn.
	LastMeditation time.Time `json:"last_meditation"`
}
    Anchors is the persisted pair of loop anchors: the moment the last
    reflection turn was consumed and the moment the last turn of the session
    ended.

    The reflection watermark heads the freshness window and the rhythm floor,
    so it is the anchor the gate reads and the anchor a consumed reflection
    advances. The session anchor is carried beside it in the same record,
    and an entry this structure does not name is ignored when the record is read
    rather than refused.

type Digest interface {
	// Render returns the bounded self-state summary of the observed face since
	// the given watermark. When the face holds more than the bound renders, the
	// count left out is reported in the result instead of being dropped in
	// silence.
	Render(ctx context.Context, since Watermark) (DigestSummary, error)
}

    Digest is the reflection loop's self-state summary: a deterministic,
    bounded rendering of the observed face since a watermark that a reflection
    turn reads without spending a model call.

    The rendering is bounded, so a face with a long backlog reports how many
    of its facts the bound left out instead of growing without limit, and the
    same face at the same watermark renders the same summary. A render that
    cannot reach the face reports that it is unavailable by name rather than
    presenting an empty summary as a quiet face. The redline list (3 rules):
    MUST render the same summary for the same face at the same watermark.
    MUST report the count a bound left out beside the rendering it bounded.
    MUST NOT present an unreachable face as a quiet one.

type DigestSummary struct {
	// Body is the rendered summary of the observed face.
	Body string
	// Omitted is the number of facts the render bound left out of Body.
	Omitted int
}
    DigestSummary is the rendered self-state summary together with the count of
    facts the render bound left out.

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
    by its RefineRecord together with the post-hoc report the window produced,
    so a window always connects back to its result. A window that cannot show
    activation evidence is judged insufficient with the missing evidence named,
    and the zero value never means the window is open.

type FactAttribution struct {
	// Lineage is the trigger-source stamp the fact carries; empty means the fact
	// carries none.
	Lineage string
	// SelfManaged reports the lineage derivation of this fact.
	SelfManaged bool
}
    FactAttribution is whose traffic a fact is: the trigger-source stamp it
    carries and the derivation of that stamp.

type FactContent struct {
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact.
	Summary string
	// Timestamp is the moment the fact was recorded.
	Timestamp time.Time
}
    FactContent is what a fact recorded: its frozen type, its stored summary and
    the moment it was recorded.

type FactIdentity struct {
	// Key is the canonical identity of the fact.
	Key event.EventID
	// Partition is the namespace name the fact was filed under.
	Partition string
}
    FactIdentity is which fact a pass read and where it was filed: the canonical
    identity the fact committed under and the namespace name it was filed as.

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
type GitRefine struct {
	// Has unexported fields.
}

    GitRefine is the git-native refinement surface over one controlled
    repository: it records an explicit set of controlled files as one
    improvement commit, lists the improvement ledger, and safely reverts one
    marked commit. It holds no version store of its own — git is the version
    layer and the files on disk are the source of truth, so an edit takes effect
    the moment it lands and this surface only leaves a trace, an evaluation hook
    and a safe undo.

func NewGitRefine(root string, record ImprovementRecorder, protected []string) *GitRefine
    NewGitRefine returns the refinement surface over the controlled repository
    at root. It records each successful registration through the injected
    recorder, which is optional, and admits only paths under the controlled
    patterns — the defaults when none are supplied. Every git command runs in
    root, so the surface never reaches any repository but the one it is pointed
    at.

func (g *GitRefine) Register(ctx context.Context, paths []string, note string) (RefineRecord, error)
    Register records the explicitly listed controlled files as one improvement.
    It stages exactly those paths and commits only them under the improvement
    marker, leaving the rest of the working tree and anything the user already
    staged untouched. A path outside the controlled set refuses the whole
    registration before git is touched.

    - A commit that finds nothing to commit is a normal outcome, not a failure:
    the files already hold the content the registration would record.

func (g *GitRefine) Rollback(ctx context.Context, sha string) error
    Rollback reverts one recorded improvement named by its full commit or an
    unambiguous prefix. A commit that does not carry the improvement marker is
    refused before git is touched, so a user's own commit is never reverted.
    A revert that stops on a conflict returns the git output and disposition
    guidance and leaves the revert open for the agent; reverting is a terminal
    act of the agent and never of the framework.

func (g *GitRefine) Status(ctx context.Context) ([]RefineRecord, error)
    Status lists the improvement ledger: every commit whose subject opens with
    the improvement marker, newest first, with the note and controlled paths
    each registration carried and the moment its commit landed. A commit without
    the marker — including the reverts git writes back — never enters the
    ledger. The evaluation window and post-hoc report stay zero here and are
    filled by the post-activation evaluation; a zero record is never read as
    healthy.

func (g *GitRefine) Unregistered(ctx context.Context) ([]string, error)
    Unregistered lists the controlled files whose modification time falls after
    the last registration commit: artifacts the agent has already changed
    on disk but not yet recorded. It is a soft reminder beside the ledger,
    never a gate, because a file takes effect the moment it lands regardless of
    registration.

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

type Improvement struct {
	// SHA is the registration commit carrying the improvement marker.
	SHA string
	// Paths lists the controlled paths the registration committed.
	Paths []string
	// Note is the author's pain-point, artifact and expected-benefit note.
	Note string
}
    Improvement is one successful registration the refine surface hands to
    its recorder: the registration commit carrying the improvement marker,
    the controlled paths it committed and the author's note.

    It is the payload of the improvement event a registration writes through
    the injected ImprovementRecorder, and its SHA is a git correlation only:
    it links the event back to the committed version and never opens or anchors
    an evaluation window.

type ImprovementRecorder func(ctx context.Context, improvement Improvement) error
    ImprovementRecorder records one successful registration as a governance
    improvement event. It is the port through which the refine surface reaches
    the governance event face without importing governance, keeping the layering
    line: evolution never depends on governance and writes governance-class
    events straight through this injected port. A nil recorder records nothing,
    and a recorder failure never retracts the commit that already landed;
    the registration reports the unrecorded event by name instead.

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

type ObservationScan struct {
	// Watermark is the execution watermark the window opened after.
	Watermark time.Time
	// Partitions holds one tally line per observed partition, in declared order.
	Partitions []PartitionActivity
	// Facts is the hydrated sample in read order, bounded by the pass's fact limit.
	Facts []ScannedFact
	// Counts is the accounting of the read.
	Counts ScanCounts
	// Verdict is what the freshness judgment read from this pass.
	Verdict ScanVerdict
}
    ObservationScan is the bounded evidence of one pass over the observed face:
    the watermark the window opened at, the per-partition tallies, the hydrated
    sample, the accounting of that read and the verdict the freshness judgment
    read from it.

    The evidence is kept so the digest renders from this pass and never reads
    the fact chain a second time.

type ObservationScanRequest struct {
	// Since is the execution watermark the window opens strictly after; the zero
	// value opens the window at the epoch.
	Since time.Time
	// PageLimit bounds the reference page; zero runs the default bound.
	PageLimit int
	// FactLimit bounds the hydrated sample; zero runs the default bound.
	FactLimit int
}
    ObservationScanRequest is the window and bounds of one pass over the
    observed face.

type ObservedFacts interface {
	// QueryEvents returns the references matching opts.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
	// CommittedFact returns the committed fact the identity names; an identity
	// with no committed fact behind it is an error, never an empty fact.
	CommittedFact(ctx context.Context, id event.EventID) (event.Event, error)
}
    ObservedFacts is the read-only fact-chain face the reflection judgment
    runs on: the conditional retrieval the authorization-scoped query answers
    through, beside the committed-fact read that hydrates one reference into the
    fact it names.

    A query answers references only, which carry no trigger source,
    so the judgment hydrates the newest references one by one and stops at
    its first hit. Both calls are reads: a pass never writes the chain it
    observes. The redline list (3 rules): MUST answer references through the
    authorization-scoped query and hydrate each one through its committed fact.
    MUST NOT write the chain it observes. MUST NOT answer an identity that never
    committed as an empty fact.

type PartitionActivity struct {
	// Name is the namespace name the partition was resolved from.
	Name string
	// Partition is the storage partition the name resolves to.
	Partition int
	// References counts this partition's entries on the reference page, which
	// the read face answers without any hydration.
	References int
	// External counts hydrated facts whose lineage is not self-managed: the
	// freshness evidence.
	External int
	// SelfManaged counts hydrated facts the lineage derivation calls self-managed,
	// unknown attribution included.
	SelfManaged int
}
    PartitionActivity is one observed partition's tally in a pass: how much
    of the reference page belongs to it and how the hydrated sample splits by
    lineage.

type PathScopeError struct {
	// Offending lists the written paths that fall outside the controlled set.
	Offending []string
	// Controlled lists the controlled patterns the paths were judged against.
	Controlled []string
}
    PathScopeError is the named refusal of a registration whose paths leave the
    controlled set: the offending paths and the controlled patterns they were
    judged against. Unwrap exposes ErrPathOutOfScope, so a caller reaches the
    named refusal through errors.Is instead of matching rendered text.

func (e *PathScopeError) Error() string
    Error renders the offending paths beside the controlled set they missed.

func (e *PathScopeError) Unwrap() error
    Unwrap exposes the named refusal underneath the rendered message.

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
type Refine interface {
	// Register records the explicitly listed controlled files as one improvement:
	// it stages exactly those paths and commits them under the improvement
	// marker, leaving other working-tree edits and anything the user already
	// staged untouched. A path outside the controlled set refuses the whole
	// registration before git is touched.
	Register(ctx context.Context, paths []string, note string) (RefineRecord, error)

	// Status lists the improvement ledger together with the current state of each
	// evaluation window and the post-hoc judgment recorded for each entry.
	Status(ctx context.Context) ([]RefineRecord, error)

	// Rollback reverts one recorded improvement, named by its full commit or an
	// unambiguous prefix. A commit that does not carry the improvement marker is
	// refused, so a user's own commit is never reverted. Reverting is a terminal
	// act of the agent, never of the framework.
	Rollback(ctx context.Context, sha string) error
}

    Refine is the git-native half of the self-improvement channel: an edit
    already takes effect when it lands on disk, and this surface records, lists
    and safely reverts such an edit through the host's git. The redline list (4
    rules): MUST stage exactly the listed controlled paths and commit them under
    the improvement marker. MUST refuse a path outside the controlled set before
    git is touched. MUST NOT revert a commit that does not carry the improvement
    marker. MUST NOT touch a working-tree edit outside the listed paths.

type RefineRecord struct {
	// Paths lists the explicitly recorded controlled paths of the improvement.
	Paths []string
	// Note is the author's pain-point, artifact and expected-benefit note.
	Note string
	// SHA is the registration commit carrying the improvement marker. It is a
	// git correlation only and never the window's anchor.
	SHA string
	// CommittedAt is the moment the registration commit landed, read back from
	// git. It orders the ledger and marks the point after which a controlled file
	// is an artifact not yet registered; it is a git fact only and never the
	// anchor of the evaluation window, whose origin is the activation moment.
	CommittedAt time.Time
	// GateBasis is the structural gate report the improvement rests on, kept so
	// the ledger entry traces back through its EvidenceRefs to the evidence
	// behind the judgment. Its zero value means no gate report backs the entry
	// and is never read as a passing gate.
	GateBasis GateReport
	// Window is the one-shot evidence window of the improvement version, opened
	// at the activation moment a real request first consumed that version.
	Window EvidenceWindow
	// Posterior is the post-hoc evaluation report of the improvement version
	// after its window produced a judgment.
	Posterior PosteriorReport
}
    RefineRecord is one entry of the improvement ledger: the controlled paths
    the improvement changed, the note its author attached, and the read face
    — registration SHA, gate report, activation window and post-hoc report —
    through which the entry traces back to the evidence it rests on.

    - The note states the pain point the improvement answers, the artifact
    it produced and the benefit it expects, so a ledger entry explains itself
    without its commit message. - The entry carries its basis GateReport,
    and through that report's EvidenceRefs and the window's activation evidence
    a reader resolves what the improvement was judged against and when it
    took effect. - The registration SHA is a git correlation only: it links
    feedback to the consumed version and never opens or anchors the window,
    whose origin is the activation moment. - A zero record carries no judgment:
    an empty window means none has opened and an empty PosteriorReport means
    none has run, and neither is read as healthy.

type Reflection struct {
	// Has unexported fields.
}
    Reflection is the reflection loop of one agent: it reads the authorized
    observation face, decides whether a reflection is due, injects the input
    event into the agent's own loop session and advances the execution watermark
    when that turn is consumed.

    One mechanism covers every observation face: observing the agent's own
    partition is self-maintenance and observing others' partitions is curation,
    and the action is the same explicit input in either case. A fire only writes
    the input; the watermark moves when the turn is consumed, to the injection
    moment, so every fact a yield window covered stays inside the next window.
    A read the pass cannot complete keeps the gate closed by name, and no
    fallback criterion is substituted for an unreachable chain.

func NewReflection(cfg ReflectionConfig, gate WatermarkGate, facts ObservedFacts, input ReflectionInput) (*Reflection, error)
    NewReflection assembles the reflection loop of one agent over its watermark
    gate, its observed fact-chain face and the loop-session injection face. A
    declared observation face outside the authorized scope refuses the assembly
    by name.

func (r *Reflection) DeferredCount() int
    DeferredCount reports how many reflections yielded since the last consumed
    one.

func (r *Reflection) FreshSince(ctx context.Context, since time.Time) (bool, error)
    FreshSince reports whether the observed face holds a fact recorded after
    since whose lineage is not managed by the observing agent itself.

    A face that cannot be read is reported as an error and never as "nothing
    fresh": the fact chain is the judgment's only data face.

func (r *Reflection) LastDeferredAt() time.Time
    LastDeferredAt reports the moment of the most recent deferral.

func (r *Reflection) NoteOutcome(ctx context.Context, receipt ReflectionReceipt) error
    NoteOutcome settles what became of the reflection input awaiting one.

    A consumed turn advances the execution watermark to that input's injection
    moment and clears the deferral tally; a deferred turn clears the pending
    input and leaves the watermark in place; an in-flight turn keeps it pending.
    A report naming an input other than the pending one, or arriving while
    none is pending, is refused by name so a repeated report can never move the
    watermark again.

func (r *Reflection) ObservedNamespaces() []string
    ObservedNamespaces reports the resolved observation face of this reflection.

func (r *Reflection) Pending() (event.EventID, bool)
    Pending reports the identity of the reflection input awaiting its outcome,
    if one is pending.

func (r *Reflection) Scope() ReflectionScope
    Scope reports the resolved observation face, names and partitions together.

func (r *Reflection) Tick(ctx context.Context, now time.Time) TickOutcome
    Tick runs one reflection tick at now: it establishes the execution
    watermark, reads the observed face once, renders the digest from that same
    pass, applies the pending guard, the freshness judgment and the rhythm
    floor, and injects one explicit input event when all three open.

    A tick whose gate cannot be established reports TickClosed with the fact it
    could not establish and injects nothing; a tick that injected reports the
    input and settles the outcome the loop side handed back.

type ReflectionCadence struct {
	// Interval is the tick cadence, reported beside a pending input that has
	// outlived it by three intervals.
	Interval time.Duration
	// MinGap is the floor between two consumed reflections.
	MinGap time.Duration
}
    ReflectionCadence is how often one reflection may fire: the tick cadence and
    the floor between two consumed reflections.

type ReflectionConfig struct {
	// Loop is the loop session the reflection reflects into.
	Loop ReflectionLoop
	// Face is the observation face and the bounds it is read under.
	Face ReflectionFace
	// Cadence is how often one reflection may fire.
	Cadence ReflectionCadence
	// Prompt is the static reflection prompt carried beside the digest.
	Prompt string
}
    ReflectionConfig is the assembly input of one reflecting agent's loop:
    the loop session it reflects into, the face it observes and the bounds it
    reads it under, its cadence and the static prompt its input carries.

    The prompt is text the reflection renders without spending a model call.

type ReflectionFace struct {
	// ObservedNamespaces declares the face; empty means the agent's own partition.
	ObservedNamespaces []string
	// ReadNamespaces is the authorized read scope of the agent.
	ReadNamespaces []string
	// PageLimit bounds one pass over the observed face; zero runs the default.
	PageLimit int
	// FactLimit bounds the facts one digest lists; zero runs the default.
	FactLimit int
}
    ReflectionFace is the face one reflection observes and the bounds it reads
    it under: the declared namespaces, the authorized read scope they must sit
    inside and the per-pass and per-digest bounds.

type ReflectionInjection struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// At is the injection moment, which is the watermark candidate a consumed
	// turn advances the gate to.
	At time.Time
	// Event is the explicit input event as it was handed to the loop session.
	Event event.Event
	// Digest is the self-state summary the input carried.
	Digest DigestSummary
}
    ReflectionInjection is one injected reflection input as the firing tick
    reports it: the identity it committed under, the moment it was injected at,
    the event itself and the digest it carried.

type ReflectionInput interface {
	// InjectReflection commits one reflection input into the agent's own loop
	// session and reports the outcome of its turn.
	InjectReflection(ctx context.Context, evt event.Event) (ReflectionReceipt, error)
}
    ReflectionInput is the loop-session face one fired reflection injects its
    explicit input event through: the assembly commits the event as a fact of
    the agent's own partition, drives the observing agent's own loop session
    with it and reports what became of that turn.

    The receipt is the only evidence that a reflection turn ran: a fire that
    was only written never advances the watermark. The redline list (3 rules):
    MUST commit the reflection input as a fact of the agent's own partition.
    MUST report the outcome of the turn the injected input drove. MUST NOT
    report a reflection turn as consumed from a fire that was only written.

type ReflectionLoop struct {
	// Agent is the observing agent's own identity and namespace.
	Agent string
	// Session is the loop session the reflection input is injected into, which is
	// always this agent's own session.
	Session string
}

    ReflectionLoop is the loop session one reflection reflects into: the
    observing agent's own identity and namespace, and the session its input is
    injected into.

type ReflectionOutcome string
    ReflectionOutcome is what became of one injected reflection input.

const (
	// ReflectionConsumed reports a reflection turn that ran: the watermark
	// advances to the moment the turn was injected at.
	ReflectionConsumed ReflectionOutcome = "consumed"
	// ReflectionDeferred reports a reflection turn that yielded to real input at
	// either yield point: the watermark stays where it stands and the very same
	// facts are re-evaluated on the next tick.
	ReflectionDeferred ReflectionOutcome = "deferred"
	// ReflectionInFlight reports an injected input whose turn has not reached an
	// outcome yet; the input stays pending and no second one is injected.
	ReflectionInFlight ReflectionOutcome = "in-flight"
)
type ReflectionReceipt struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// Outcome is what became of the injected turn.
	Outcome ReflectionOutcome
	// At is the moment the outcome was reached; it dates the deferral tally and
	// never moves the watermark.
	At time.Time
	// Reason is the named ground of a deferral, when the loop side reports one.
	Reason string
}
    ReflectionReceipt is the loop side's report of one injected reflection
    input: the identity the fact committed under, what became of its turn and
    the moment that outcome was reached.

type ReflectionScope struct {
	// Own is the observing agent's own namespace, which is always observably
	// legal and is the face of an undeclared observation surface.
	Own string
	// Names lists the observed namespaces in declared order, deduplicated.
	Names []string
	// Partitions lists the storage partition each name resolves to, in the same
	// order as Names; the query scans exactly these partitions.
	Partitions []int
}

    ReflectionScope is the resolved observation face of one reflecting agent:
    the namespaces it observes and the storage partitions those namespaces
    resolve to.

    The face is an application authorization rather than a capability the
    reflection grants itself. A declared face stays within the namespaces
    the application authorized for reading plus the agent's own partition,
    an undeclared face is the agent's own partition, and a declared face that
    reaches past that boundary is refused at assembly.

func ResolveObservationScope(own string, declared, read []string) (ReflectionScope, error)
    ResolveObservationScope resolves the observation face of one agent from its
    declared namespaces, the namespaces it is authorized to read and its own
    namespace.

    An undeclared face is the agent's own partition and never falls back to the
    authorized read scope: what an agent may read is not what it reflects on.
    A declared name that is neither the agent's own nor inside the read scope is
    refused with ErrObservationScopeOutOfBounds naming every such namespace.

type RevertConflictError struct {
	// SHA is the resolved commit the revert tried to undo.
	SHA string
	// Output is the git output naming the conflict and the disposition steps.
	Output string
}
    RevertConflictError is the named failure of a rollback whose revert stopped
    on a conflict: the commit it tried to revert and the git output carrying the
    conflict. Unwrap exposes ErrRevertConflict. The repository is left in its
    open revert state, so the agent resolves the conflict and runs `git revert
    --continue`, or abandons it with `git revert --abort`; the framework never
    resolves or aborts a revert on its own.

func (e *RevertConflictError) Error() string
    Error renders the reverted commit and the git output guiding the resolution.

func (e *RevertConflictError) Unwrap() error
    Unwrap exposes the named conflict underneath the rendered message.

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

type ScanCounts struct {
	// ReferenceTotal is the size of the reference page the pass read.
	ReferenceTotal int
	// HydratedTotal is the number of facts the pass hydrated.
	HydratedTotal int
	// Unreadable counts references the pass could not hydrate; an unreadable fact
	// is never counted as freshness.
	Unreadable int
	// UnknownLineage counts hydrated facts that carry no trigger-source stamp.
	UnknownLineage int
	// Foreign counts hydrated facts filed under a partition outside the observed
	// face; such a fact is never counted as freshness either.
	Foreign int
}
    ScanCounts is the accounting of one pass: the reference page it read,
    how many facts it hydrated, and the references that yielded no freshness
    evidence.

type ScanDigest struct {
	// Has unexported fields.
}
    ScanDigest renders the self-state summary out of one completed observation
    pass: the watermark the window opened at, one tally line per observed
    partition and the hydrated facts the pass kept, all in the deterministic
    order the pass read them.

    The rendering spends no model call, blocks on no external surface, reads the
    fact chain no further than the pass already did and stays bounded whatever
    the backlog: partitions and facts beyond the bounds, and references the pass
    did not hydrate, are counted in Omitted instead of being dropped in silence.

func NewScanDigest(scan *ObservationScan, factLimit int) *ScanDigest
    NewScanDigest builds a digest over one completed pass, limiting the listed
    facts to factLimit entries; a non-positive limit runs the default bound.

func (d *ScanDigest) Render(ctx context.Context, since Watermark) (DigestSummary, error)
    Render returns the bounded self-state summary of the pass this digest was
    built over, refusing by name when there is no pass or when the pass opened
    at another watermark than the one since names.

type ScanVerdict struct {
	// Novelty reports whether the face holds a fact newer than the watermark whose
	// lineage is not self-managed.
	Novelty bool
	// Recent is the newest fact whose lineage is not self-managed, when the pass
	// found one.
	Recent *ScannedFact
}
    ScanVerdict is what the freshness judgment read off one pass: whether
    the face holds a fact newer than the watermark that is not self-managed,
    and that fact.

type ScannedFact struct {
	// Identity is the fact and the partition it was filed under.
	Identity FactIdentity
	// Content is what the fact recorded.
	Content FactContent
	// Attribution is whose traffic the fact is.
	Attribution FactAttribution
}
    ScannedFact is one hydrated fact of a pass, kept in the deterministic order
    the pass read it.

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

type TickDecision struct {
	// Disposition is what this tick decided.
	Disposition TickDisposition
	// Reason is the named ground of the disposition; a closed gate names the fact
	// it could not establish.
	Reason string
	// Cause is the named failure behind a closed gate, when one was returned by a
	// read or by the watermark source.
	Cause error
}
    TickDecision is what one reflection tick decided and the named ground behind
    that decision.

type TickDisposition string
    TickDisposition is what one reflection tick decided.

const (
	// TickFired reports that this tick injected a reflection input.
	TickFired TickDisposition = "fired"
	// TickPending reports that an injected reflection input still awaits its
	// outcome, so this tick injected nothing.
	TickPending TickDisposition = "pending"
	// TickClosed reports that the gate stayed closed because a fact it needs
	// could not be established.
	TickClosed TickDisposition = "closed"
	// TickNoNovelty reports a readable face holding nothing newer than the
	// watermark with non-self-managed lineage.
	TickNoNovelty TickDisposition = "no-novelty"
	// TickRhythm reports that the rhythm floor has not elapsed since the last
	// consumed reflection.
	TickRhythm TickDisposition = "rhythm"
)
type TickOutcome struct {
	// Decision is what this tick decided.
	Decision TickDecision
	// Scan is the evidence of the pass this tick read, when a pass ran.
	Scan *ObservationScan
	// Digest is the self-state summary rendered from that same pass.
	Digest DigestSummary
	// Injection is the reflection input this tick injected, when it fired.
	Injection *ReflectionInjection
}
    TickOutcome is the decision one reflection tick reached, the evidence of the
    pass it read and the injection it made.

type Watermark struct {
	// Moment is the moment the last consumed reflection turn advanced the
	// watermark to. Its zero value marks a start with no prior execution.
	Moment time.Time
}

    Watermark is the execution watermark of the reflection loop: the moment the
    last reflection turn advanced it to, which heads the freshness window and
    bounds how soon the next reflection may run.

    Injection alone never moves the watermark; only a reflection turn that is
    actually consumed advances it, to the moment the turn was injected at.
    A reflection that yields leaves the watermark in place, so the facts that
    reflection covered stay no older than the watermark instead of being burned
    out of the window, and a start with no watermark passes once and then locks
    itself. The watermark is advanced by consumption only, and the freshness
    judgment excludes the agent's own managed lineage, so a notification the
    agent sends about its own work never makes the agent reflect on itself
    again.

type WatermarkGate interface {
	// Current returns the current execution watermark. A watermark that cannot
	// be established is reported as an error, and the gate stays closed.
	Current(ctx context.Context) (Watermark, error)

	// Advance moves the watermark to the moment a consumed reflection turn was
	// injected at, and leaves it unmoved when that turn was deferred.
	Advance(ctx context.Context, moment time.Time) error
}
    WatermarkGate is the reflection loop's freshness gate over the execution
    watermark.

    The gate reads the watermark and advances it after a reflection turn is
    consumed. A watermark that is missing, or that cannot be read or parsed,
    keeps the gate closed and is reported by name; the gate is never opened
    on a fresh default or on a fallback criterion when the watermark cannot be
    established. The redline list (3 rules): MUST report a watermark it cannot
    establish as an error and keep the gate closed. MUST NOT open the gate on
    a fresh default or on a fallback criterion. MUST NOT advance the watermark
    past a reflection turn that was deferred.

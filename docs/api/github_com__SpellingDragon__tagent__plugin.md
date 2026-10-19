package plugin // import "github.com/SpellingDragon/tagent/plugin"

The wiring face of the approval channel: the manager that holds requests,
the option seam that injects its clock and rescan window, and the file-backed
request directory an external approver writes into. The words it records
(status, subject, evidence, state, request, channel, delivery gap) are stated
once in approval.go; this file only keeps and resolves them, so the vocabulary
has one spelling and the store has one job.

Package plugin declares the ordered per-phase pipeline through which tagent
injects its differentiated capabilities into the framework event flow: the
pre-execution call transform and risk ruling, and the post-commit observation
and final presentation.

A plugin is a registration unit: it reports the phase it serves and implements
that phase's one narrow interface. The pre-execution faces form one Before
pass a request runs exactly once; the post-commit faces form the After pass,
which reads only facts whose canonical commit already happened and answers with
a Presentation.

  - the extension-face vocabulary: the phases, the four narrow interfaces,
    the pipeline that dispatches them, the post-commit observation wiring and
    the presentation pass;
  - the extension policies at the governance gate: the pre-execution ruling,
    the risk rules, the approval channel, the denial ledger, the goal registry
    and the budget window, each stated in its own file and reaching the pipeline
    only through the one face it serves.

CONSTANTS

const (
	// MetaKeySubtype names the governance subtype entry of one event.
	MetaKeySubtype = "subtype"
	// SubtypeDenial is the subtype of a refused call's governance event.
	SubtypeDenial = "denial"
	// SubtypeApproval is the subtype of a held call's governance event.
	SubtypeApproval = "approval"
	// SubtypeAudit is the subtype of a recorded execution's governance event.
	SubtypeAudit = "audit"
)
    MetaKeySubtype is the metadata key one governance event is filed under, and
    the subtype words below are the vocabulary the governance events speak. The
    words are kept exactly as the governance semantics fix them: a refusal is a
    denial, a privileged execution is an audit fact, and a parked critical call
    is an approval fact. Goal operations file under their own two words, the
    event vocabulary entry the goal contract fixes (see SubtypeGoalDeclared).

const (
	// SubtypeGoalDeclared is the governance event subtype of a declared goal.
	SubtypeGoalDeclared = "goal_declared"
	// SubtypeGoalResolved is the governance event subtype of a resolved goal.
	SubtypeGoalResolved = "goal_resolved"
)
    SubtypeGoalDeclared and SubtypeGoalResolved are the goal operation words a
    goal governance event carries. The state words stay the old ones; these two
    name the operations and are the event vocabulary entry the goal contract
    fixes.

VARIABLES

var ErrApprovalExpired = errors.New("plugin: the approval request expired before it was decided")
    ErrApprovalExpired reports a ruling that arrived after the request expired:
    an expired request is treated as not approved, and a late ruling never makes
    it runnable again.

var ErrApprovalIdentityMissing = errors.New("plugin: the approval request carries no approval identity")
    ErrApprovalIdentityMissing reports a request registration that carries no
    approval identity. The identity is the one tagent issued when the call was
    parked, so it is required rather than replaced by a locally minted one:
    the host response names that one identity across the hold and the request.

var ErrApprovalNotFound = errors.New("plugin: no approval request names that identity")
    ErrApprovalNotFound reports a ruling that names no registered approval
    request, so it is refused by name instead of being applied to a request that
    does not exist.

var ErrApprovalRulingUnknown = errors.New("plugin: the approval response names no known ruling")
    ErrApprovalRulingUnknown reports a ruling whose status names no known
    decision, so it is refused closed instead of being recorded as a guess.

var ErrBudgetExhausted = budgetExhaustedError{}
    ErrBudgetExhausted reports a window whose budget for that risk grade is used
    up. A call beside it never runs: an exhausted budget refuses it whatever the
    enforcement grade says.

var ErrCallIdentityChanged = errors.New("plugin: the transform changed the call identity")
    ErrCallIdentityChanged reports a transform that changed the identity of the
    call it was handed: a transform may correct the tool name and parameters of
    a call and must never change its CallID, which stays as resolved. The Before
    pass reports it beside the plugin that moved the identity and errors.Is
    matches it.

var ErrCommitObserverUnwired = errors.New("plugin: the commit observer is not wired")
    ErrCommitObserverUnwired reports a post-commit observation attempt on an
    observer whose lookup or sink was never wired: a gap by name, not a silent
    no-op that would read as a delivered observation.

FUNCTIONS

func ArgsDigest(jsonArgs []byte) string
    ArgsDigest computes the argument digest of one call: the lowercase hex
    of the SHA-256 of the exact argument bytes the call carries. The digest
    binds an approval to the arguments that were held, so a ruling over other
    arguments is refused instead of approving a changed call.

func WithCallPass(ctx context.Context, pass CallPass) context.Context
    WithCallPass carries the one Before pass of a tool call on the context of
    that call, so the admission ruling of the composition root rules over the
    very pass the dispatch produced. The dispatch is its production writer. The
    pass is the seam's only value: what triggered the call travels inside it,
    and no second context key is ever introduced.

TYPES

type ApprovalChannel interface {
	// Deliver offers one approval request to an external approver.
	Deliver(ctx context.Context, req ApprovalRequest) error
}
    ApprovalChannel is the outbound delivery face of the approval channel:
    one approval request is offered to an external approver over it. The channel
    is a gate, never a wall: a delivery that fails is recorded as a visible
    gap and never blocks the gate, and the request stays answerable through the
    other entries (CLI, WeChat) or through the request file itself.

    The redline list (3 rules): MUST deliver one request per call, keyed by the
    approval identity the request carries. MUST NOT read a failed delivery as a
    ruling: the gate keeps waiting. MUST NOT rewrite the request it was handed.

type ApprovalEvidence struct {
	// ArgsPreview is the bounded preview of the arguments an approver reads.
	ArgsPreview string `json:"args_preview"`
	// RiskLevel is the risk grade the pre-execution ruling was rendered at.
	RiskLevel RiskLevel `json:"risk"`
	// RuleID names the rule the ruling came from, if one matched.
	RuleID string `json:"rule_id,omitempty"`
	// Reason is the readable reason the call was held.
	Reason string `json:"reason"`
}
    ApprovalEvidence is the block an approver reads to decide one held call:
    the bounded argument preview, the grade and rule the ruling was rendered at,
    and the reason the call was held.

type ApprovalManager struct {
	// Has unexported fields.
}
    ApprovalManager is the waiting book of the approval channel: one request per
    file under <dir>/approvals/, an in-memory index beside it, and a throttled
    rescan through which a ruling an external approver wrote to a request file
    becomes visible at runtime. The manager is concurrent safe.

    The manager owns the digest binding: it computes the digest of the exact
    argument bytes at registration and every ruling it honours is matched
    against that digest, so an approval of other arguments never runs.

func NewApprovalManager(dir string, ttl time.Duration, opts ...ApprovalOption) *ApprovalManager
    NewApprovalManager returns the approval manager over one directory.
    A non-empty dir keeps the requests under <dir>/approvals/ as one file per
    request and rebuilds the index from what it finds there; an empty dir keeps
    the manager in memory only. The ttl is the validity a request is issued
    with.

func (a *ApprovalManager) AddChannel(ch ApprovalChannel)
    AddChannel attaches one outbound delivery face the manager offers its
    requests to. A channel that fails records a visible gap and never blocks the
    gate.

func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) (*ApprovalRequest, error)
    Decide records the ruling of one approval request and reports the request
    with the ruling that took effect. The first ruling takes effect and a
    repeated response is idempotent: it reports the first ruling again instead
    of consuming a second one. A ruling over an unknown identity is refused
    beside ErrApprovalNotFound, a ruling that names no known decision is refused
    beside ErrApprovalRulingUnknown, and a ruling that arrived after the request
    expired is refused beside ErrApprovalExpired — an expired request is treated
    as not approved and is never made runnable by a late response.

func (a *ApprovalManager) DeliveryGaps() []DeliveryGap
    DeliveryGaps reports the visible channel failures recorded so far.

func (a *ApprovalManager) Lookup(tool, argsDigest string) *ApprovalRequest
    Lookup reports the request the gate reads one (tool, arguments) pair under:
    an unexpired approved request — the approval the held call may proceed
    on — or, when none is approved, the newest request of that pair with its
    effective status: pending, denied, or expired once its validity ran out.
    The effective status is what the returned copy reports, so a request whose
    validity ran out reads as expired and never as a standing approval.

    An unapproved read triggers a throttled rescan of the request directory,
    so a ruling an external approver wrote to a request file becomes visible at
    runtime instead of waiting for a restart.

func (a *ApprovalManager) Pending() []*ApprovalRequest
    Pending reports the requests still awaiting a ruling and within their
    validity, oldest first, for an approval entry to display and answer.

func (a *ApprovalManager) Request(ctx context.Context, id, tool string, jsonArgs []byte, risk RiskLevel, rule, reason string) (*ApprovalRequest, error)
    Request registers one pending approval request under the approval identity
    tagent issued when the call was parked and writes it to its own file beside
    offering it to every attached channel. The manager computes the argument
    digest itself, so the registration keeps the digest binding the hold was
    issued over.

    A channel that fails records a visible gap and never fails the registration
    (the gate is a gate, never a wall), and a repeated registration under the
    same identity keeps the request already stored. A request that cannot be
    written to its file is reported to the caller beside the request, which
    stays registered in memory: a hold waits either way and is never turned into
    a denial by a channel-side failure.

type ApprovalOption func(*ApprovalManager)
    ApprovalOption configures an approval manager at construction.

func WithApprovalClock(now func() time.Time) ApprovalOption
    WithApprovalClock injects the clock the manager reads its time from,
    so a test drives validity windows deterministically instead of sleeping.

func WithApprovalRescanInterval(window time.Duration) ApprovalOption
    WithApprovalRescanInterval injects the throttled window between two
    directory rescans. The window exists so an external approver's write to a
    request file becomes visible at runtime without rescanning on every read;
    zero rescans on every unapproved read, which a test uses to stay
    deterministic.

type ApprovalRequest struct {
	ApprovalSubject
	ApprovalEvidence
	ApprovalState
}
    ApprovalRequest is one request for a human ruling over one held call:
    one request per file, readable and writable by the external approver.
    Every ruling is bound to ArgsDigest — the digest of the exact argument bytes
    that were held — so an approval of other arguments is refused instead of
    running a changed call.

    The request is three blocks — who was held, what the approver reads,
    and where the request stands — and its file keeps them as one flat document,
    so an approver answers a request without knowing the blocks.

type ApprovalState struct {
	// CreatedMs is the registration time of the request, in Unix milliseconds.
	CreatedMs int64 `json:"created_ms"`
	// ExpiresMs is the moment the request loses its validity, in Unix
	// milliseconds. An undecided request past this moment reads as expired.
	ExpiresMs int64 `json:"expires_ms"`
	// Status is the state the request is in.
	Status ApprovalStatus `json:"status"`
	// DecidedBy names the approver whose ruling took effect.
	DecidedBy string `json:"decided_by,omitempty"`
}
    ApprovalState is the block that reports where one request stands: when it
    was registered, when its validity runs out, and the ruling that took effect.

type ApprovalStatus string
    ApprovalStatus is the state one approval request is in. The four states
    carry one meaning each: pending awaits an external ruling and the governed
    call does not run meanwhile, approved lets that very call proceed, denied
    is a terminal refusal of it, and expired means the request outlived its
    validity without a ruling and is treated as not approved — a later response
    never makes an expired request runnable again.

const (
	// ApprovalPending is waiting for an external approver's ruling; the
	// governed operation does not execute meanwhile.
	ApprovalPending ApprovalStatus = "pending"

	// ApprovalApproved lets the held call proceed.
	ApprovalApproved ApprovalStatus = "approved"

	// ApprovalDenied is a terminal refusal: the call ends and returns as a
	// refused result.
	ApprovalDenied ApprovalStatus = "denied"

	// ApprovalExpired is a request that outlived its validity without a
	// ruling: it is treated as not approved, and nothing proceeds on the old
	// request afterwards.
	ApprovalExpired ApprovalStatus = "expired"
)
type ApprovalSubject struct {
	// ID is the approval identity tagent issued when the call was parked. The
	// host response names this identity, so the hold and the request resolve
	// under one name.
	ID string `json:"id"`
	// ToolName is the tool the held call wants to run.
	ToolName string `json:"tool"`
	// ArgsDigest is the digest of the exact argument bytes the call carries,
	// computed by ArgsDigest. Every ruling is bound to it.
	ArgsDigest string `json:"args_digest"`
}
    ApprovalSubject is the identity block of one approval request: the tool the
    held call wants to run, and the two names the request is resolved under.

type BudgetConfig struct {
	// Window is the width of the sliding window the budget is counted over.
	Window time.Duration
	// BucketCount is how many buckets the window is cut into.
	BucketCount int
	// MaxHighRisk is how many high-grade operations one window admits.
	MaxHighRisk int
	// MaxMediumRisk is how many medium-grade operations one window admits.
	MaxMediumRisk int
}
    BudgetConfig configures the budget window and the per-grade limits one
    budget manager counts over. The defaults are the ones the governance
    semantics fix: a one hour window cut into six buckets, admitting twenty
    high-grade and two hundred medium-grade operations per window.

type BudgetManager struct {
	// Has unexported fields.
}
    BudgetManager is the sliding window budget gate: it counts high-grade and
    medium-grade operations over rolling buckets and refuses the call that
    finds its window spent. The critical and low grades are never counted — the
    critical grade is held by the approval channel and a read-only call needs no
    budget.

    The manager is concurrent safe, and its epoch is persisted with its buckets
    so a restart resumes the window instead of refreshing it: a snapshot that is
    missing, unparseable or carries a zero epoch never overwrites the current
    epoch, because letting a missing value zero the epoch would read accumulated
    consumption as a fresh start and leave the gate worthless. A manager built
    with an empty dir holds its state in memory only and never touches disk.

func NewBudgetManager(cfg BudgetConfig, dir string, opts ...BudgetOption) *BudgetManager
    NewBudgetManager builds one budget gate over the given configuration.
    A non-empty dir keeps the snapshot under <dir>/budget.json and resumes the
    epoch and the buckets it finds there; an empty dir keeps the manager in
    memory only. The dir is judged here exactly as it arrives — the caller must
    not pre-join directory fragments, because an empty piece joined in yields a
    relative path and the budget would be written into the process directory.

func (b *BudgetManager) Admit(level RiskLevel) error
    Admit rules on one operation of the given grade and, when it is admitted,
    counts it into the window. The critical and low grades pass uncounted;
    a high-grade or medium-grade call that finds its window spent is refused
    with ErrBudgetExhausted.

func (b *BudgetManager) Usage(level RiskLevel) int64
    Usage reports how much of the window's budget the given grade has spent,
    for diagnostics and observability.

type BudgetOption func(*BudgetManager)
    BudgetOption configures a budget manager at construction.

func WithBudgetClock(now func() time.Time) BudgetOption
    WithBudgetClock injects the clock the manager reads its window position
    from, so a test drives the sliding window deterministically instead of
    waiting on wall time.

type CallPass struct {
	// Final is the transformed final call the pass produced: the call with the
	// allowed parameter transform applied and its CallID and trusted scope left
	// as resolved. It is the call the core re-checks, executes and records.
	Final trpcmodel.ToolCall

	// Risk is the strongest grade the risk faces rendered for the final call,
	// from RiskLow to RiskCritical; it stays empty when the pipeline carries no
	// risk face. An empty grade is not a grade of its own and never bypasses the
	// gates: the ruling reads it as RiskMedium, so a call beside no risk face is
	// still ruled as an ordinary mutating call.
	Risk RiskLevel

	// TriggerSource names what triggered the call — the source a goal gate and a
	// trigger-scoped rule match on exactly. The dispatcher states it beside the
	// pass it produced; the pass never guesses one, and an empty value means the
	// call declares no source.
	TriggerSource string
}
    CallPass is the whole input of one pre-execution ruling: the final call the
    Before pass produced, the strongest risk grade its faces rendered and what
    triggered the call. The final call is the one both executed and recorded;
    the risk grade advises the pre-execution ruling without ever running a
    second risk face; the trigger source is stated by the dispatching layer that
    knows what started the call, so a ruling has no hidden input of its own.

func CallPassFrom(ctx context.Context) (CallPass, bool)
    CallPassFrom reports the Before pass the context carries and whether one
    is set. It is this seam's single typed accessor: a call site reads the pass
    through it and never through a bare context value, and the composition
    root's admission ruling is its production reader. A call driven beside no
    pass reads not-ok, and that caller decides the default — the admission
    ruling refuses such a call closed rather than ruling at a guess.

type CommitObserver struct {
	// Has unexported fields.
}
    CommitObserver is the post-commit observation wiring of the After pass:
    it resolves the committed fact behind one identity and hands it unchanged to
    the observation sink, as a read-only duty over a fact whose canonical commit
    already happened.

    A failed read or a failed sink is returned so the After pass records it as
    a named gap beside this plugin's name: the committed fact is never rolled
    back, never withheld from any reader, and the observed work is never re-run
    to satisfy an observer.

func NewCommitObserver(facts FactLookup, sink ObservationSink) *CommitObserver
    NewCommitObserver wires one fact lookup and one observation sink behind the
    post-commit face. Both are required: an unwired observer refuses by name
    instead of reporting an observation it never made.

func (o *CommitObserver) ObserveCommit(ctx context.Context, id event.EventID) error
    ObserveCommit resolves one committed fact and hands it unchanged to the
    observation sink. It runs only after the canonical commit of that fact;
    an identity with no committed fact behind it is refused as a gap and never
    observed as if it had committed.

type Decision struct {
	// Disposition is the ruling for this call.
	Disposition Disposition

	// Risk is the grade the ruling was rendered at.
	Risk RiskLevel

	// Rule names the matched rule the ruling came from.
	Rule string

	// Reason is the correction material the model reads when the call is held or
	// refused.
	Reason string

	// Denial is set only for a terminal refusal, and stays nil for an allowed,
	// recorded or held call.
	Denial *Denial
}
    Decision is the outcome of the ruling one tool call receives before it runs.

    An allowed or recorded call runs; a held call waits for approval and keeps
    Denial nil; a refused call carries Denial and does not run. A hold is thus
    never misreported as a denial.

func (d Decision) Refused() bool
    Refused reports whether this ruling is a terminal refusal: a refused call
    carries its Denial and does not run, while an allowed, recorded or held call
    keeps Denial nil. Read the refusal first when consuming a Decision — a held
    call is a wait and never a denial.

type DeliveryGap struct {
	// Request is the approval identity the failed delivery was for.
	Request string
	// Err is the delivery failure the channel reported.
	Err error
}
    DeliveryGap is one visible channel failure: an approval request the channels
    could not be handed. The gap is recorded and exposed instead of being
    swallowed, and the gate keeps waiting rather than treating the failure as an
    approval or as a crash.

type Denial struct {
	// CallID is the native call identifier the paired result answers.
	CallID string

	// Tool is the name of the tool the model called.
	Tool string

	// Risk is the grade the refusal was rendered at.
	Risk RiskLevel

	// Rule names the rule that refused the call.
	Rule string

	// Result is the native tool result paired to the call; its text carries the
	// reason and the rule for the model to read.
	Result trpcmodel.Message
}
    Denial is the terminal refusal of one tool call.

    The call does not run, and the model receives the native result paired to
    the call instead of an error, so it can correct itself on its next turn.
    A denial is never a hold: a held call has not been refused and may still run
    after approval.

type DenialLedger struct {
	// Has unexported fields.
}
    DenialLedger is the governance ledger: an in-memory index beside the durable
    governance events every record also writes. A refusal leaves both halves —
    the ledger record and the governance event — so the model reads the refusal
    and the host and the observation face see it too.

    The ledger is concurrent safe, and the durable face is late-bound and nil
    safe: without a store it degrades to memory state instead of failing,
    and binding one replays the stored governance events back into the index,
    so a restart keeps the records it already made.

func NewDenialLedger() *DenialLedger
    NewDenialLedger builds one ledger in memory state. BindStore hands it the
    durable face whenever the wiring has one.

func (l *DenialLedger) BindStore(log GovernanceEventLog)
    BindStore hands the ledger its durable face and replays the governance
    events already stored there back into the index. It is nil safe, and it runs
    single threaded at construction — the same discipline the goal registry
    binds under.

func (l *DenialLedger) Count() int
    Count reports how many records the ledger holds.

func (l *DenialLedger) Query(limit int) []DenialRecord
    Query returns the newest records, at most limit of them. A limit of zero or
    less returns all of them. The records are copies, so a reader never observes
    a concurrent write.

func (l *DenialLedger) Record(rec DenialRecord)
    Record writes the governance double of one record: the ledger entry and the
    governance event beside it. A record that carries no timestamp is stamped
    here, and an event write that fails never blocks the operation — the ledger
    entry stands and the record keeps its meaning.

type DenialRecord struct {
	// Subtype is the governance subtype the record is filed under.
	Subtype string `json:"subtype"`
	// ToolName is the tool the ruled call wanted to run.
	ToolName string `json:"tool"`
	// Level is the risk grade the ruling was rendered at.
	Level RiskLevel `json:"level"`
	// RuleID names the rule the ruling came from, when one matched.
	RuleID string `json:"rule_id"`
	// Reason is the readable reason behind the record.
	Reason string `json:"reason"`
	// ArgsDigest is the digest of the exact argument bytes the call carried.
	ArgsDigest string `json:"args_digest,omitempty"`
	// GoalID is the goal context the record permeates. Filling it is the
	// denial's goal permeation: a refusal records beside the goal the call was
	// hanging on, so the audit reads which autonomous scope it happened in.
	GoalID string `json:"goal_id,omitempty"`
	// AgentName names the agent the record came from, when the wiring knows one.
	AgentName string `json:"agent,omitempty"`
	// Timestamp is when the record was made, in Unix milliseconds.
	Timestamp int64 `json:"ts"`
}
    DenialRecord is one governance record: the fact of a refused call or of a
    privileged execution, kept for the model to correct itself on and for the
    audit to read back. GoalID carries the goal context the record permeates:
    the goal the governed call was hanging on when the ruling was rendered.

type Disposition string

    Disposition is the ruling a tool call receives before its side effect runs:
    the call is allowed, allowed with an audit record, or held for approval.
    A hold is a wait, not a denial.

const (
	// DispositionAllow lets the call run with no extra trace duty.
	DispositionAllow Disposition = "allow"

	// DispositionRecord lets the call run and requires this execution to be
	// written to the audit ledger.
	DispositionRecord Disposition = "record"

	// DispositionHold stops the call before its side effect and hands it to the
	// approval channel. The call has not run and may still be approved, so a
	// hold is a wait and is never reported as a denial.
	DispositionHold Disposition = "hold"
)
type Enforcement string
    Enforcement is the enforcement grade of the governance gate: it alone
    decides whether a violation is really refused.

const (
	// EnforcementWarn records and exposes a violation without blocking the
	// execution. It is the default of an enabled gate.
	EnforcementWarn Enforcement = "warn"

	// EnforcementStrict refuses a violation. Whether a violation is really
	// refused is decided by this grade alone.
	EnforcementStrict Enforcement = "strict"
)
type FactLookup interface {
	// CommittedFact returns the committed fact the identity names. An identity
	// with no committed fact behind it is an error, never an empty fact.
	CommittedFact(ctx context.Context, id event.EventID) (event.Event, error)
}
    FactLookup reads one committed fact by identity: the read-only face the
    post-commit wiring resolves an already committed fact through. It answers
    with the fact as committed and never with a reconstruction, so an identity
    that did not commit is refused rather than fabricated.

    The redline list (2 rules): MUST answer the fact as it committed, and report
    an identity with no committed fact as an error. MUST NOT fabricate a fact or
    answer a reconstruction.

type FinalPresenter interface {
	Present(ctx context.Context, id event.EventID) (string, bool)
}
    FinalPresenter is the final-presentation face: Present renders the
    presentation of one fact for its channel and reports the presentation text
    with whether it was dropped.

    The redline list (3 rules): MUST answer the presentation text beside whether
    the channel dropped it. MUST NOT drop a presentation without reporting the
    drop. MUST NOT retract the committed fact it was rendered from.

type Goal struct {
	// ID is the goal identity, minted in declaration order.
	ID string `json:"id"`
	// Statement is what the goal wants done.
	Statement string `json:"statement"`
	// CreatedBy names who declared the goal.
	CreatedBy string `json:"created_by"`
	// Status is the lifecycle state the goal is in.
	Status GoalStatus `json:"status"`
	// CreatedMs is the declaration time, in Unix milliseconds.
	CreatedMs int64 `json:"created_ms"`
	// ExpiresMs is the moment the goal stops counting as active, in Unix
	// milliseconds. Zero means the goal never expires.
	ExpiresMs int64 `json:"expires_ms,omitempty"`
}
    Goal is one autonomous goal declaration.

type GoalRegistry struct {
	// Has unexported fields.
}
    GoalRegistry is the goal registry: every declared goal in memory beside the
    governance events the declarations and resolutions also write. A declaration
    activates the governance goal gate, and a refusal of a goal-required call
    permeates the goal context into its record.

    The registry is concurrent safe, and its durable face is late-bound and
    nil safe: without a store it degrades to memory state instead of failing,
    and binding one replays the stored goal events back into the registry,
    so a restart keeps the goals it already declared.

func NewGoalRegistry() *GoalRegistry
    NewGoalRegistry builds one goal registry in memory state. BindStore hands it
    the durable face whenever the wiring has one.

func (g *GoalRegistry) Active() []*Goal
    Active returns every unexpired active goal, for diagnostics and tools.

func (g *GoalRegistry) BindStore(log GovernanceEventLog)
    BindStore hands the registry its durable face and replays the goal events
    already stored there back into it — declarations and resolutions in the
    order the operations happened, so a resolved goal is never resurrected and
    a later declaration never reuses an identity. It is nil safe, and it runs
    single threaded at construction.

func (g *GoalRegistry) Current() *Goal
    Current returns the goal the registry reads as the current goal context:
    the newest goal still in flight, or nil when none is. A refusal permeates
    this goal into its record, so the audit reads which autonomous scope the
    ruled call was hanging on.

func (g *GoalRegistry) Declare(statement, createdBy string, expiresMs int64) string
    Declare registers one goal and returns its identity. The declaration writes
    its governance event beside the memory entry, so a restart replays it back.

func (g *GoalRegistry) HasActive() bool
    HasActive reports whether an unexpired active goal exists — the judgment the
    governance goal gate rules on.

func (g *GoalRegistry) List() []*Goal
    List returns a snapshot copy of every goal the registry holds, in no
    particular order. The copy keeps a reader from mutating registry state.

func (g *GoalRegistry) Resolve(id string, status GoalStatus) bool
    Resolve moves one goal to its resolution and reports whether the goal
    exists. The two writable resolutions are achieved and abandoned; expired
    is derived and never written here. The registry guards no terminal state: a
    repeated resolution is applied as it arrives, and the tool layer's two-value
    enumeration is what keeps an unwritten word out.

type GoalStatus string
    GoalStatus is the lifecycle state of one goal. The four words are the ones
    the governance semantics fix and they are kept verbatim: active is the goal
    in flight — it occupies governance budget and takes part in the gate ruling;
    achieved is reached, so it holds no budget and takes no part in the ruling;
    abandoned is given up on deliberately; expired is the goal that outlived
    its validity unreached, kept apart from abandoned so statistics and budget
    attribution can tell the two apart.

    Only two of the four are ever written: a goal is explicitly resolved to
    achieved or abandoned, and expired is derived from ExpiresMs — a goal past
    its validity simply stops counting as active and its Status is never flipped
    by the registry. The resolution face takes the two writable words and the
    tool layer's two-value enumeration guards the rest; the registry itself
    guards no terminal state.

const (
	// GoalActive is the goal in flight: it occupies governance budget and
	// takes part in the gate ruling.
	GoalActive GoalStatus = "active"
	// GoalAchieved is the reached goal: it holds no budget and takes no part in
	// the gate ruling.
	GoalAchieved GoalStatus = "achieved"
	// GoalAbandoned is the goal given up on deliberately, for example because
	// a better path replaced it or an operator ended it.
	GoalAbandoned GoalStatus = "abandoned"
	// GoalExpired is the goal that outlived its validity unreached. It is
	// derived from ExpiresMs and never written, kept apart from abandoned for
	// statistics and budget attribution.
	GoalExpired GoalStatus = "expired"
)
type GovernanceEventLog interface {
	// Put stages one governance event under its key.
	Put(ctx context.Context, key string, value []byte) error
	// Scan returns every stored governance event under the key prefix, keyed by
	// the event key it was written under.
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)
	// Sync completes the write barrier over everything staged so far.
	Sync(ctx context.Context) error
}
    GovernanceEventLog is the durable face one governance double is written to
    and replayed from: put, prefix scan and the sync barrier a durable write
    reports its success behind. Its signatures carry built-in types only,
    so any durable backend — the memory tier's fact log among them — satisfies
    it structurally and no package edge is required in either direction.

    The redline list (3 rules): MUST key every governance event under the
    stream key its family numbers. MUST NOT report a staged write as durable
    before Sync returns nil. MUST NOT rewrite or drop a governance event already
    stored.

type Governor interface {
	// Judge returns the ruling for one tool call before its side effect runs,
	// over the one Before pass the call carries: the final call, the risk grade
	// the risk faces already rendered and the trigger source the dispatch
	// stated. The ruling is rendered exactly once per call and is used both for
	// the execution and for the record. A hold is a wait, not a denial.
	Judge(pass CallPass) (Decision, error)
}
    Governor renders the single pre-execution ruling for one tool call.

    The redline list (3 rules): MUST render one ruling per call, over the one
    Before pass that call carries. MUST NOT execute the call, and MUST NOT
    bypass a hold it rendered. MUST NOT report a hold as a denial.

type ObservationSink interface {
	// Observe reports one committed fact to the observation side. The fact is
	// passed unchanged and is never rewritten here. A returned error records a
	// missing observation, not an undone fact.
	Observe(ctx context.Context, evt event.Event) error
}
    ObservationSink is the outbound port the post-commit observation wiring
    hands one committed fact to. It keeps the exact shape of the runtime's
    observation port, so the OTel/audit observer plugs in at the composition
    root without this package depending on it: the pipeline stays on its single
    leaf edge to event, and the sink is a choice the composition root makes
    rather than a fact the plugin layer records.

    The redline list (3 rules): MUST pass the fact it was handed unchanged.
    MUST NOT rewrite the committed fact or hold it back from any reader.
    MUST NOT report a failed observation as a delivered one.

type ObservationSource interface {
	// ObservedNamespaces returns the partitions this source is authorized to
	// observe. An empty result means the face was not declared, and the agent's
	// own partition is the face.
	ObservedNamespaces() []string

	// FreshSince reports whether the observed face holds a fact recorded after
	// since whose lineage is not managed by the observing agent itself, so a
	// notification the agent produces about its own work never makes it reflect
	// on itself again.
	//
	// The judgment reads the fact chain by identity, so a chain that cannot be
	// read keeps the gate closed: a face with nothing fresh and a failed read
	// both leave reflection off, and no fallback criterion is substituted for an
	// unreachable chain.
	FreshSince(ctx context.Context, since time.Time) (bool, error)
}

    ObservationSource declares the observation face a reflection may read
    and judges whether that face holds activity fresh enough to reflect on.
    The face is an application authorization rather than a capability the source
    grants itself: the declared namespaces must stay within the namespaces the
    application already authorized for reading plus the agent's own partition,
    an undeclared face is the agent's own partition, and a declared face that
    reaches past that boundary is refused by name when the source is assembled
    instead of being trimmed in silence.

    The redline list (3 rules): MUST keep the observed face within the
    authorized namespaces plus the agent's own partition. MUST NOT treat a
    fact of the observing agent's own lineage as fresh. MUST NOT substitute a
    fallback criterion when the chain cannot be read.

type Phase int
    Phase names one stage of the single ordered plugin pipeline; the constant
    order is the pipeline order. Each phase owns one narrow interface set, and
    the phase a plugin reports selects the set that plugin is dispatched into:

      - PhaseCallTransform → TransformCall
      - PhaseRiskPolicy → RiskAssessor and Governor
      - PhasePostCommitObserve → PostCommitObserver
      - PhaseFinalPresentation → FinalPresenter

    The risk phase carries two faces of one ruling: a RiskAssessor renders the
    grade and a Governor renders the one pre-execution ruling over that grade.
    The two stand beside each other as two faces of one phase, never as two risk
    faces — no grade is rendered twice — and one element may implement either
    face or both.

const (
	// PhaseCallTransform is the pre-execution call phase: the call is corrected
	// before its side effect and before its admission ruling, and the corrected
	// call is the one re-checked, executed and recorded.
	PhaseCallTransform Phase = iota

	// PhaseRiskPolicy grades the risk of a tool call before its side effect and
	// renders the pre-execution ruling. The phase carries two faces of that one
	// ruling: a RiskAssessor renders the grade and a Governor renders the ruling
	// over it; the grade is read once and never recomputed by the ruling.
	PhaseRiskPolicy

	// PhasePostCommitObserve observes a fact after its canonical commit, as a
	// read-only duty.
	PhasePostCommitObserve

	// PhaseFinalPresentation renders the final presentation of a fact for its
	// channel.
	PhaseFinalPresentation
)
type Pipeline struct {
	// Has unexported fields.
}
    Pipeline is the phase-grouped face set of one ordered plugin pipeline:
    every registered Plugin is dispatched into its own phase's narrow interface
    set in the declared order, and one plugin serves exactly one phase.

    The pre-execution faces run as the one Before pass of a tool call; the
    post-commit faces run as the After pass of a committed fact. The pipeline
    is assembled once per dispatch from the borrowed generation's plugins and is
    never reassembled mid-request.

func NewPipeline(plugins []Plugin) (Pipeline, error)
    NewPipeline groups the registered plugins into their phase's narrow
    interface set, in the declared order. A plugin whose phase is not one of the
    four declared phases, a nil plugin, and a plugin that implements no narrow
    interface of its own phase each refuse the pipeline by name instead of being
    trimmed in silence.

func (p Pipeline) After(ctx context.Context, fact event.EventID) view.Presentation
    After runs the After pass over one fact whose canonical commit already
    happened: every observer reads the fact first, then every presenter renders
    it. The pass runs after the commit and never before it, so a refused call
    and a call whose commit failed present nothing at all.

    An observation that fails is recorded as a named gap in the returned
    presentation and never rolls the committed fact back; a presenter that drops
    its text drops the presentation only. The pass carries no error return:
    a failure here is a gap or a dropped view, never a reason to touch the
    committed fact.

func (p Pipeline) Before(ctx context.Context, call trpcmodel.ToolCall) (CallPass, error)
    Before runs the one Before pass of one tool call: every transform face
    corrects a private copy of the call in pipeline order, then every risk face
    grades the transformed call, and the pass reports the final call beside the
    strongest grade. The pass runs exactly once per call — before the call is
    admitted and before its side effect — and is never run again when a recorded
    fact is replayed. The call the model recorded is left byte-identical:
    the transform works on its own copy.

    A transform that answers an error refuses the whole pass at that face:
    no later face runs, no ruling is rendered, and the call is neither executed
    nor recorded. A transform that moves the call's CallID refuses the pass by
    name. The transformed final call is re-checked by the core before it runs —
    identity and permission at the wrapper's admission path — and the same final
    call is the one both executed and recorded.

type Plugin interface {
	// Name returns the stable name of this plugin.
	Name() string

	// Phase reports the single phase this plugin serves, which both orders the
	// plugin inside the pipeline and selects the narrow interface it implements.
	Phase() Phase
}
    Plugin is a tagent registration unit, distinct from the framework's
    plugin.Plugin: it reports the stable name of one pipeline element and the
    single phase that element serves, and the element implements that phase's
    narrow interface. The composition root reads the phase of every registered
    Plugin and dispatches the element into that phase's narrow interface set —
    TransformCall, RiskAssessor, Governor, PostCommitObserver, FinalPresenter —
    in the declared order.

    The redline list (3 rules): MUST report the one phase the element serves
    and implement that phase's narrow interface. MUST NOT report a phase
    whose narrow interface it does not implement. MUST NOT serve two phases:
    one registration element serves one phase and may carry the faces of that
    phase (the risk phase carries two faces of one ruling).

type Policy struct {
	// Has unexported fields.
}
    Policy is the pre-execution enforcement policy of the governance gate: the
    warn/strict grade, the violation rules, and the goal, budget and approval
    gates beside the ledger every ruling leaves its governance double on.

    The gate carries no configuration field of its own: Open starts it and
    every gate is added by the option that names it. Policy renders the single
    pre-execution ruling of one call — exactly once per call, over the one
    Before pass that call carries — and that one ruling is used both for the
    execution and for the record.

func Open(opts ...PolicyOption) *Policy
    Open starts the governance gate with the warn grade and every gate off. Each
    option names one gate, so a caller states the gates the run really carries:

      - WithRules and WithApprovals are the violation rules and the approval
        channel
      - WithGoals and WithGoalRequired are the goal gate and the sources it
        covers
      - WithBudget and WithLedger are the budget window and the governance
        double
      - WithDefaultRisk and WithStrict are the fallback grade and the refusing
        grade

    The zero value and a nil policy are the gate that is not open: every call is
    allowed with no ruling duty.

func (p *Policy) Active() bool
    Active reports whether the governance gate is on. A nil policy and the zero
    value are a gate that is not on.

func (p *Policy) Judge(pass CallPass) (Decision, error)
    Judge renders the single pre-execution ruling of one tool call — the
    Governor face of the policy — over the one Before pass that call carries:
    the final call the pass produced, the risk grade its risk faces already
    rendered and the trigger source the dispatch stated. The pass is the whole
    input, so the ruling has no hidden input and consumes the grade instead of
    rendering it a second time: no second risk face runs inside the ruling.
    A pass carrying no grade is ruled at the conservative fallback grade.

    The ruling runs exactly once per call and is used both for the execution and
    for the record. A held call is a wait and keeps Denial nil; a refused call
    carries its Denial and does not run.

func (p *Policy) Mode() Enforcement
    Mode reports the effective enforcement grade of the gate: an enabled gate
    that chose no grade runs under warn, the default. A gate that is not on
    reports the empty grade.

type PolicyOption func(*Policy)
    PolicyOption configures the governance gate at construction.

func WithApprovals(approvals *ApprovalManager) PolicyOption
    WithApprovals attaches the approval channel the critical grade always takes,
    whatever the rest of the policy says. A critical call beside no channel is
    refused: there is nobody who could approve it.

func WithBudget(budget *BudgetManager) PolicyOption
    WithBudget attaches the budget gate the ruling counts high-grade and
    medium-grade operations into: a call that finds its window spent is refused
    whatever the enforcement grade says.

func WithDefaultRisk(level RiskLevel) PolicyOption
    WithDefaultRisk sets the conservative grade the ruling falls back to when
    the one Before pass carries none. An empty value reads as RiskMedium.

func WithGoalRequired(sources ...string) PolicyOption
    WithGoalRequired names the trigger sources an operation must hang on an
    active goal for. The names match trigger sources exactly, as the governance
    semantics define it, and an empty list leaves the goal gate off.

func WithGoals(goals *GoalRegistry) PolicyOption
    WithGoals attaches the goal registry the goal gate rules over: a call at the
    high grade and above from a trigger source that requires a goal is refused
    while no goal is in flight. Every ruling beside a registry permeates the
    current goal context into its record.

func WithLedger(ledger *DenialLedger) PolicyOption
    WithLedger attaches the governance ledger every refusal and every recorded
    execution leaves its governance double on. A gate beside no ledger keeps
    ruling and drops the records.

func WithRules(rules ...RiskRule) PolicyOption
    WithRules attaches the data-driven violation rules, read in order; the first
    fired rule is the violation the ruling reads.

func WithStrict() PolicyOption
    WithStrict raises the grade of the gate to strict: a violation is refused
    before its side effect instead of being recorded and exposed.

type PostCommitObserver interface {
	ObserveCommit(ctx context.Context, id event.EventID) error
}
    PostCommitObserver is the post-commit observation face: ObserveCommit
    observes one fact after its canonical commit, as a read-only duty.

    The redline list (3 rules): MUST observe a fact whose canonical commit
    already happened. MUST NOT roll the committed fact back or re-run the work
    when the observation fails. MUST NOT write the fact it observes.

type RiskAssessor interface {
	AssessRisk(ctx context.Context, call *trpcmodel.ToolCall) RiskLevel
}
    RiskAssessor is the pre-side-effect risk face: AssessRisk grades one tool
    call before its side effect runs, from the parameters and identity already
    resolved for that call.

    The redline list (2 rules): MUST grade the call it was handed and answer
    a declared RiskLevel. MUST NOT run a side effect or render the ruling:
    the ruling reads the grade it rendered.

type RiskContext struct {
	// ToolName is the tool the call wants to run.
	ToolName string
	// ArgsJSON is the exact argument bytes the call carries.
	ArgsJSON []byte
	// TriggerSource names what triggered the call.
	TriggerSource string
}
    RiskContext is the plain grading input one ruling is rendered over:
    the tool name, the argument JSON and the trigger source, and nothing else.
    A rule matches over this data as a pure function.

type RiskLevel string
    RiskLevel grades the risk of one tool call, from low to critical. The
    critical grade always takes the asynchronous approval path, and the grading
    input is plain data: the tool name, the argument JSON and the trigger
    source.

    The zero value is not a grade a face renders: it reads as "no grade was
    rendered". A ruling that receives it reads RiskMedium, so the call is ruled
    as an ordinary mutating call instead of passing ungraded; a rule threshold
    that carries it fires at every grade instead of at none.

const (
	// RiskLow is the grade of a read-only call; it runs with no trace duty.
	RiskLow RiskLevel = "low"

	// RiskMedium is the grade of an ordinary mutating call; it runs and is
	// recorded.
	RiskMedium RiskLevel = "medium"

	// RiskHigh is the grade of an irreversible or privileged call; it runs and
	// is recorded, and an exhausted budget refuses it.
	RiskHigh RiskLevel = "high"

	// RiskCritical is the grade of a call that always needs approval.
	RiskCritical RiskLevel = "critical"
)
type RiskRule struct {
	// ID names the rule in the ruling and in the refusal.
	ID string
	// MinRisk is the minimum risk grade at which this rule fires: the rule
	// consumes the grade of the Before pass against this threshold. An empty
	// threshold fires at every grade.
	MinRisk RiskLevel
	// Match reports whether this rule covers the call. It is a pure function
	// over the plain grading input.
	Match func(RiskContext) bool
	// Reason is the correction material the model reads when the rule refuses
	// or records the call.
	Reason string
}
    RiskRule is one data-driven violation rule over the plain grading input.
    The rule consumes the risk grade the RiskAssessor face rendered — the grade
    the one Before pass carried — and fires as a violation at the minimum grade
    it declares; it never renders a grade of its own, so no second risk face
    ever runs. The first fired rule is the violation the ruling reads.

type TransformCall interface {
	// TransformCall transforms one call before its side effect runs. It runs once
	// per call and before admission, and the transformed call is re-checked by the
	// core before it runs, as the one both executed and recorded. A nil error
	// carries the transform forward; a non-nil error refuses the pass by name, so
	// the call is neither executed nor recorded.
	TransformCall(ctx context.Context, call *trpcmodel.ToolCall) error
}
    TransformCall is the tool-call-level pre-side-effect face, and it is where
    the allowed tool-call parameter transform is carried: it runs once per
    tool call before that call's side effect, not once at request assembly.
    The transform happens before the tool runs, earlier than every execution
    side effect other than admission, so it may correct or constrain the tool
    name and parameters of the call and must not change the call's identity: its
    CallID and trusted scope stay as resolved. A transformed call never bypasses
    the core: the transformed call is admitted through the wrapper's admission
    path, approval and dedup included, before it runs, the plugin never executes
    from the transform result directly, and the transformed call is the one both
    executed and recorded. A transform that answers an error refuses the pass:
    the call does not run, nothing of it is recorded, and the pass answers no
    CallPass.

    The redline list (3 rules): MUST keep the ID the call was resolved under;
    the pass refuses a transform that moves it. MUST NOT execute the call
    itself: admission, approval and dedup stay with the core. MUST NOT hold on
    to the call pointer past the call it was handed.

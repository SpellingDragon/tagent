package governance // import "github.com/SpellingDragon/tagent/evolution/governance"

Package governance implements the extension points plugin declares: the risk
policy that rules each tool call, the budget and goal registry it consults, and
the denial ledger a refusal leaves behind. The abstract faces stay in plugin;
this package is their implementation domain and owns no runtime state.

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

var ErrBudgetExhausted = budgetExhaustedError{}
    ErrBudgetExhausted reports a window whose budget for that risk grade is used
    up. A call beside it never runs: an exhausted budget refuses it whatever the
    enforcement grade says.

FUNCTIONS

func CallPassFrom(ctx context.Context) (plugin.CallPass, bool)
    CallPassFrom reports the Before pass the context carries and whether one
    is set. It is this seam's single typed accessor: a call site reads the pass
    through it and never through a bare context value, and the composition
    root's admission ruling is its production reader. A call driven beside no
    pass reads not-ok, and that caller decides the default — the admission
    ruling refuses such a call closed rather than ruling at a guess.

func WithCallPass(ctx context.Context, pass plugin.CallPass) context.Context
    WithCallPass carries the one Before pass of a tool call on the context of
    that call, so the admission ruling of the composition root rules over the
    very pass the dispatch produced. The dispatch is its production writer. The
    pass is the seam's only value: what triggered the call travels inside it,
    and no second context key is ever introduced.

TYPES

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

func (b *BudgetManager) Admit(level plugin.RiskLevel) error
    Admit rules on one operation of the given grade and, when it is admitted,
    counts it into the window. The critical and low grades pass uncounted;
    a high-grade or medium-grade call that finds its window spent is refused
    with ErrBudgetExhausted.

func (b *BudgetManager) Usage(level plugin.RiskLevel) int64
    Usage reports how much of the window's budget the given grade has spent,
    for diagnostics and observability.

type BudgetOption func(*BudgetManager)
    BudgetOption configures a budget manager at construction.

func WithBudgetClock(now func() time.Time) BudgetOption
    WithBudgetClock injects the clock the manager reads its window position
    from, so a test drives the sliding window deterministically instead of
    waiting on wall time.

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
	Level plugin.RiskLevel `json:"level"`
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

func (p *Policy) Judge(pass plugin.CallPass) (plugin.Decision, error)
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

func (p *Policy) Name() string
    Name returns the stable name of the governance gate in the flow: the one
    element a rendered ruling traces back to.

func (p *Policy) Port() plugin.Port
    Port reports the material this policy reads and the product it answers:
    one tool call before its side effect, answered with the pre-execution ruling
    over it. The grade the ruling consumes is the risk face's product, so this
    face declares the call->ruling port without rendering a second grade.

type PolicyOption func(*Policy)
    PolicyOption configures the governance gate at construction.

func WithApprovals(approvals *approval.ApprovalManager) PolicyOption
    WithApprovals attaches the approval channel the critical grade always takes,
    whatever the rest of the policy says. A critical call beside no channel is
    refused: there is nobody who could approve it.

func WithBudget(budget *BudgetManager) PolicyOption
    WithBudget attaches the budget gate the ruling counts high-grade and
    medium-grade operations into: a call that finds its window spent is refused
    whatever the enforcement grade says.

func WithDefaultRisk(level plugin.RiskLevel) PolicyOption
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

type RiskRule struct {
	// ID names the rule in the ruling and in the refusal.
	ID string
	// MinRisk is the minimum risk grade at which this rule fires: the rule
	// consumes the grade of the Before pass against this threshold. An empty
	// threshold fires at every grade.
	MinRisk plugin.RiskLevel
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

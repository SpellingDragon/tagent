package governance // import "github.com/SpellingDragon/tagent/features/evolution/governance"

Package governance implements the extension points plugin declares: the risk
policy that rules each tool call, the budget and goal registry it consults, and
the denial ledger a refusal leaves behind. The abstract faces stay in plugin;
this package is their implementation domain and owns no runtime state.

The governance state slice: the ledger's and the goal registry's derived state,
the two event streams' tails and the approval book as one framework checkpoint
slice. The governance events stay the truth of the governance history and the
replay source; this slice is the state they already folded, so a restarted
process reads a cut the size of the state it holds instead of scanning the
stream it committed. The slice carries the cut beside the state it cut:
the tail each stream ended at, so a restarted writer continues its numbering
past the keys already written instead of re-keying over commitments, and a boot
that finds a cut reads the events past it alone.

The redline list (3 rules): MUST keep the governance events as the truth:
the slice is derived, never the source, and no statement here rewrites or drops
a committed event. MUST keep the stream keys at their 19-digit form, so the
lexicographic order of the keys stays the order the operations happened in.
MUST refuse a payload it does not recognize by name instead of restoring an
empty state over a cut the process committed.

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

    - concurrent safe, with a late-bound and nil safe durable face:
    without a store it degrades to memory state, and binding one folds the
    events past the stated cut so a restart keeps the records it made - the
    write path is serialized and numbers behind a barrier it confirmed:
    a refused write hands its number back, and a barrier that did not confirm
    stops the stream numbering on - a hole is not the chain's end: the first
    writer continuing a seated cut resolves the tail from the highest committed
    sequence and names every missing sequence (see resolveContinuationTail),
    so a restart never re-keys over an event already stored - a write meets
    the store under its key before it lands: the same bytes replay as the
    commitment already held, and other bytes under that key are refused by name
    (see errGovernanceEventConflict), so an identity is never overwritten - the
    records a query reads are the derived index: a cut states the tail alone and
    the history behind it is read from the stream on demand

    The redline list (2 rules): MUST key every governance event under the stream
    key its family numbers. MUST NOT rewrite or drop a governance event already
    stored.

func NewDenialLedger() *DenialLedger
    NewDenialLedger builds one ledger in memory state. Bind hands it the durable
    face whenever the wiring has one.

func (l *DenialLedger) Bind(ctx context.Context, log durable.FactLog) error
    Bind hands the ledger its durable face and folds the stored governance
    events that landed past the state cut this ledger holds back into the index.
    It is nil safe, and it runs single threaded at construction — the same
    discipline the goal registry binds under.

    The error of a fold belongs to the caller that can stop the boot: a stream
    this ledger could not read leaves the index behind the chain it serves,
    and the caller that holds the cut decides whether that is a boot to stop.
    The framework state seam is that caller (see StateSeat.Bind).

func (l *DenialLedger) Count() int
    Count reports how many records the ledger holds: the index it folded, which
    a seated cut derives from the stream it numbers on the first question. It
    never answers the tail sequence, because a cut numbers events a window may
    leave out of the index, and a count that reported those would over-report
    the records.

func (l *DenialLedger) Query(limit int) []DenialRecord
    Query returns the newest records, at most limit of them. A limit of zero or
    less returns all of them. The records are copies, so a reader never observes
    a concurrent write. A ledger seated by a cut reads the history its cut
    covers from the stream on the first question, so the cut carries the live
    state and the history is paid for by the reader that asks for it.

func (l *DenialLedger) Record(rec DenialRecord)
    Record writes the governance double of one record: the ledger entry and the
    governance event beside it. A record that carries no timestamp is stamped
    here. The durable face decides the entry: a landed event indexes the record,
    a replay of the event already held indexes nothing twice, and a key naming
    other content is refused by name and indexes nothing. A store-side failure
    never blocks the operation — the ledger entry stands and the failure is
    named at ERROR beside it, a gap in the stream the next writer must not close
    by re-keying over it.

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
    GoalRegistry is the goal registry: the goals in force in memory beside the
    governance events the declarations and resolutions also write. A declaration
    activates the governance goal gate, and a refusal of a goal-required call
    permeates the goal context into its record.

    The registry is concurrent safe, and its durable face is late-bound and nil
    safe: without a store it degrades to memory state instead of failing, and
    binding one folds the stored goal events that landed past the state cut this
    registry holds, so a restart keeps the goals it already declared. The cut
    is seated by the framework state slice (see StateSeat); a registry beside no
    seat holds the zero cut, so the events past it are the whole stream.

      - a registry with a durable face holds the work in force alone: a goal
        whose resolution landed, or whose validity passed, belongs to the stream
        from then on, so the memory state and the cut stated from it follow the
        in-flight work instead of the history it accumulates (see List for the
        read that answers the history).
      - a registry beside no store holds every goal it declared, because the
        memory state is the only place they live.

func NewGoalRegistry() *GoalRegistry
    NewGoalRegistry builds one goal registry in memory state. Bind hands it the
    durable face whenever the wiring has one.

func (g *GoalRegistry) Active() []*Goal
    Active returns every unexpired active goal, for diagnostics and tools.

func (g *GoalRegistry) Bind(ctx context.Context, log durable.FactLog) error
    Bind hands the registry its durable face and folds the goal events
    that landed past the state cut it holds back into it — declarations and
    resolutions in the order the operations happened, so a resolved goal is
    never resurrected and a later declaration never reuses an identity. It is
    nil safe, and it runs single threaded at construction.

    The error of a fold belongs to the caller that can stop the boot: a stream
    this registry could not read leaves the goals behind the chain that serves
    them.

func (g *GoalRegistry) Current() *Goal
    Current returns the goal the registry reads as the current goal context:
    the newest goal still in flight, or nil when none is. A refusal permeates
    this goal into its record, so the audit reads which autonomous scope the
    ruled call was hanging on.

func (g *GoalRegistry) Declare(statement, createdBy string, expiresMs int64) string
    Declare registers one goal and returns its identity. The declaration writes
    its governance event first and mints the goal only once that event landed,
    so the goals the gate rules on are the goals a restart reads back: a
    declaration the stream refused answers no identity and leaves the effective
    state where the stream stands. The declaration is the write boundary that
    takes the goals holding nothing in force out of the memory state (see
    pruneOutOfForceLocked), and it runs before the new identity is minted, so a
    goal just declared is never the one taken out. A registry beside no stream
    mints the goal on the memory state alone, because nothing else keeps it.

func (g *GoalRegistry) HasActive() bool
    HasActive reports whether an unexpired active goal exists — the judgment the
    governance goal gate rules on.

func (g *GoalRegistry) List() []*Goal
    List returns every goal the registry answers, in declaration order: the work
    in force it holds in memory and the history the stream behind it keeps.
    A registry with a durable face derives the history on each question,
    so a cut that states the live work alone never shortens the list of goals a
    reader asks about; the read costs the stream behind the cut, and the memory
    state never keeps the history it serves. A registry beside no store answers
    its memory state, which is every goal it declared.

func (g *GoalRegistry) Resolve(id string, status GoalStatus) bool
    Resolve moves one goal to its resolution and reports whether the registry
    held it and the resolution became the recorded one. The two writable
    resolutions are achieved and abandoned; expired is derived and never written
    here. The registry guards no terminal state: a repeated resolution is
    applied as it arrives, and the tool layer's two-value enumeration is what
    keeps an unwritten word out.

    A resolution writes its event before the memory state follows it,
    so a refused write answers false and leaves the goal in the state the stream
    keeps. A landed resolution takes the goal out of the memory state, beside
    the goals that already left force: from then on the stream is the record of
    that goal and List answers it from there. A registry beside no store keeps
    it, because nothing else would. A goal the registry has released answers
    false — the live state is the work in force, not every goal ever declared.

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
type Policy struct {
	// Has unexported fields.
}
    Policy is the pre-execution enforcement policy of the governance gate:
    the warn/strict grade, the violation rules, the declared risk table, and the
    goal, budget and approval gates beside the ledger every ruling leaves its
    governance double on.

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
      - WithRiskRules is the declared risk table an ungraded pass is ruled at
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

func (p *Policy) Material() plugin.Material
    Material declares what this policy reads, when it reads it and what a failed
    ruling leaves behind: one tool call read before its side effect, where a
    failed ruling refuses the pass so the call neither runs nor is recorded.

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
    neither the one Before pass nor the declared risk table names a grade.
    An empty value reads as RiskMedium.

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

func WithRiskRules(rules ...ToolRiskRule) PolicyOption
    WithRiskRules attaches the declared risk table the policy grades with
    when the one Before pass carries no grade. The entries are validated at
    construction: an empty tool name or an unknown grade refuses the policy by
    panicking, so a table that could grade nothing never starts as a gate.

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

type RiskRules []ToolRiskRule
    RiskRules is the declared risk table: the ordered entries one policy grades
    tool calls with. The table is the default risk assessment — it renders a
    grade only when the one Before pass carries none — and the selection prefers
    an exact tool name over the longest matching prefix, then the policy's
    default grade.

type StateSeat struct {
	// Has unexported fields.
}
    StateSeat is the governance recoverable state as the framework sees it:
    the ledger and the goal registry that fold the two governance streams,
    and the approval book those rulings park calls on. The seat is what the
    composition mounts on the runtime's state seam, so one checkpoint cut states
    the governance state and one restore puts it back.

func NewStateSeat(ledger *DenialLedger, goals *GoalRegistry, approvals *approval.ApprovalManager) *StateSeat
    NewStateSeat returns the governance state of one process as a state seam
    slice: the ledger, the goal registry and the approval book that stand behind
    it.

func (s *StateSeat) Bind(ctx context.Context, log durable.FactLog) error
    Bind hands the governance state the durable face the slice's cut belongs to:
    each stream folds the events that landed past the cut it restored, so the
    state this process serves is the state the cut plus its tail states. The
    approval book's attention chain is written to and folded from the same face,
    so the questions and the rulings behind the gate outlive the process beside
    the two streams they are read with. A nil log is a process with no durable
    tier: the state stays the memory state the restore seated.

func (s *StateSeat) RestoreState(ctx context.Context, payload []byte) error
    RestoreState puts one governance state back: the cut each stream continues
    from, the state the cut kept in force and the approval requests the book
    held. The events past the cut are folded beside it when the durable face is
    bound (StateSeat.Bind), so a boot states the same governance as the process
    that cut it.

    A cut that states nothing is the zero cut: the streams continue from their
    first key, the events past it are the whole stream, and the approval book
    reads the request files, because a process that took no cut left them as its
    only record.

func (s *StateSeat) SnapshotState(_ context.Context) ([]byte, error)
    SnapshotState states the governance state as JSON: the tail each stream
    ended at, the state those streams keep in force and the approval book. The
    read takes the holders' own read locks and nothing else — it never flushes,
    commits or waits for a commit of its own — so the cut never stands between
    a governance write and its landing. The denial history stays behind the tail
    it numbers: a cut costs the live state instead of the history the stream
    already keeps.

func (s *StateSeat) StateName() string
    StateName reports the name the governance state is keyed by in a checkpoint.

type ToolRiskRule struct {
	// Tool is the tool name, or the tool-name prefix when Prefix is set.
	Tool string
	// Prefix reports whether Tool names a prefix instead of an exact name.
	Prefix bool
	// Level is the grade the assessment renders for a matched tool.
	Level plugin.RiskLevel
}
    ToolRiskRule is one entry of the declared risk table: a tool name or a
    tool-name prefix beside the grade the default risk assessment renders for
    it. The entry is plain configuration — a name, the prefix marker and a level
    — and carries no match function of its own.

    A rule whose Prefix is set matches every tool whose name starts with the
    rule's Tool prefix, and a trailing "*" in Tool marks that prefix explicitly;
    a rule without Prefix matches the tool name exactly.

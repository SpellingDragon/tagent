package plugin // import "github.com/SpellingDragon/tagent/api/plugin"

Package plugin declares the ordered per-phase pipeline through which tagent
injects its differentiated capabilities into the framework event flow:
the pre-execution call transform and risk ruling, the post-commit observation
and final presentation, the request-assembly environment injection and the
compress-request consumption.

A plugin is a registration unit: it reports the phase it serves and implements
that phase's one narrow interface. The pre-execution faces form one Before
pass a request runs exactly once; the post-commit faces form the After pass,
which reads only facts whose canonical commit already happened and answers
with a Presentation. The two further phases answer their own pass: one request
assembly asks for its environment block, and one compress request is handed to
the faces that answer it.

  - the extension-face vocabulary: the phases, the narrow interfaces each phase
    owns, the pipeline that dispatches them, the post-commit observation wiring
    and the presentation pass;
  - the extension policies at the governance gate: the pre-execution ruling,
    the risk rules, the approval channel, the denial ledger, the goal registry
    and the budget window, each stated in its own file and reaching the pipeline
    only through the one face it serves.

VARIABLES

var ErrCallIdentityChanged = errors.New("plugin: the transform changed the call identity")
    ErrCallIdentityChanged reports a transform that changed the identity of the
    call it was handed: a transform may correct the tool name and parameters of
    a call and must never change its CallID, which stays as resolved. The Before
    pass reports it beside the plugin that moved the identity and errors.Is
    matches it.

TYPES

type Attempt struct {
	// Layer names the layer the attempt was made at.
	Layer Layer

	// Skipped reports the attempt was not made because the observation is
	// switched off. A layer that ran records the attempt as made, whether or
	// not the framework behind it holds a real exporter yet.
	Skipped bool
}
    Attempt is one SDK observation attempt at one layer: the layer it was made
    at and whether it was skipped because the observation is switched off.
    A skipped attempt is a legitimate recorded state, not a silent omission: an
    audit reader sees which layers the SDK attempted and which it deliberately
    left alone.

    An attempt here is an observation attempt at one SDK layer, never a model
    attempt: the remembered form of a model attempt is a record of the training
    surface, and the identity of a model attempt is the event package's
    AttemptID. The three readings stay apart and none is decoded as another.

type AuditRecord struct {
	// Fact is the identity of the committed fact the attempts were made for.
	Fact event.EventID

	// Attempts holds one entry per SDK layer, in the fixed layer order
	// trace then metric.
	Attempts []Attempt
}
    AuditRecord is the layered audit record of one committed fact's observation:
    one entry per SDK layer, in the fixed layer order, describing the attempt
    that layer made for that fact. The record is evidence about the observation,
    never about the fact: it carries the fact's identity and the layered
    attempts and rewrites nothing the fact already holds.

type AuditSink interface {
	// Audit hands one layered audit record to the audit side. A returned
	// error means the record did not land and is reported to the caller as a
	// missing observation; it never touches the committed fact the record
	// describes.
	Audit(ctx context.Context, record AuditRecord) error
}
    AuditSink is the audit side of the observation bypass: it receives the
    layered audit record of one observed fact and reports whether the record
    landed. An implementation must be safe for concurrent use, because the
    post-commit pass may observe facts from several writers at once.

    The redline list (3 rules): MUST be safe for concurrent use. MUST report
    a record that did not land as a missing observation. MUST NOT touch the
    committed fact the record describes.

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

type CompressTrigger interface {
	// TriggerCompress dispatches one filed compress request. A nil error carries the
	// request forward into the compression that answers it; a non-nil error refuses
	// it by name, so the material is not compressed and the request stays pending.
	TriggerCompress(ctx context.Context, request event.CompressRequest) error
}
    CompressTrigger is the compress-consumption face: TriggerCompress answers
    one filed compress request with the compression it owes. The request is the
    judgment that material is over its bound; the face is the mechanism that
    answers it.

    The redline list (3 rules): MUST answer the request it was handed:
    its identity and subject stay as filed. MUST NOT retire the request:
    the consumer retires it once the products of its compression are committed.
    MUST NOT compress material no filed request names.

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
type EnvironmentInject interface {
	// InjectEnvironment renders the environment block of one request assembly. A nil
	// error carries the rendered text; a non-nil error refuses the assembly by name,
	// so no request is built over a block that failed to render.
	InjectEnvironment(ctx context.Context, request EnvironmentRequest) (string, error)
}
    EnvironmentInject is the request-assembly face: InjectEnvironment renders
    the dynamic environment material one request carries, and the assembly
    places that text where its own layout puts it.

    The redline list (3 rules): MUST render the material the request assembly
    holds at this boundary, and never a snapshot taken earlier. MUST NOT write
    the runtime it reads or change the request it serves. MUST NOT refuse a
    request to hide an empty face: an empty face is stated as empty, not as no
    block.

type EnvironmentRequest struct {
	// Agent is the agent the request assembly runs as.
	Agent event.AgentID
}
    EnvironmentRequest names the request assembly one environment block is
    rendered for: the agent the request runs as. The material itself is read
    by the face from the runtime it holds, so an assembly never has to hand a
    bundle of its own state across the phase.

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

    The redline list (4 rules): MUST answer the presentation text beside whether
    the channel dropped it. MUST NOT drop a presentation without reporting the
    drop. MUST NOT retract the committed fact it was rendered from. MUST NOT be
    registered beside a second presenter: one fact carries one presentation.

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

type Layer string
    Layer names one layer of the layered SDK observation attempted for one
    committed fact. The layers are attempted in the fixed order the constants
    declare, so one audit record always reads trace then metric regardless of
    how many facts one run observes.

const (
	// LayerTrace is the trace layer: the span the SDK records for one
	// committed fact.
	LayerTrace Layer = "trace"

	// LayerMetric is the metric layer: the counter the SDK adds for one
	// committed fact.
	LayerMetric Layer = "metric"
)
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

type Observer interface {
	// Observe reports one committed fact to the observation side.
	//
	// The fact is passed unchanged and is never rewritten here. A returned
	// error records a missing observation, not an undone fact.
	Observe(ctx context.Context, evt event.Event) error
}
    Observer is the post-commit observation entry: the single callback a
    runtime invokes for each fact that has committed successfully. It is one
    outbound port of the observation bypass: it consumes committed facts,
    never blocks the caller that produced one, and never withholds a fact from a
    reader because an observer is slow or broken; with observation switched off,
    the committed facts stay available to every reader.

    The redline list (4 rules): MUST run after the fact is durable. MUST record
    a failure as a gap in the observation record and report it to the caller.
    MUST NOT hold back or reorder a committed fact for a slow or failing
    observer. MUST NOT roll back the committed fact or re-run the observed work
    to satisfy an observer.

type Phase int
    Phase names one stage of the single ordered plugin pipeline; the constant
    order is the pipeline order. Each phase owns one narrow interface set, and
    the phase a plugin reports selects the set that plugin is dispatched into:

      - PhaseCallTransform → TransformCall
      - PhaseRiskPolicy → RiskAssessor and Governor
      - PhasePostCommitObserve → PostCommitObserver
      - PhaseFinalPresentation → FinalPresenter
      - PhaseCompressTrigger → CompressTrigger
      - PhaseEnvironmentInject → EnvironmentInject

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

	// PhaseCompressTrigger is the consumption phase of a compress request: the
	// filed judgment that material is over its bound is handed to the faces that
	// answer it with the compression it owes, before the request is retired.
	PhaseCompressTrigger

	// PhaseEnvironmentInject is the request-assembly phase: the faces render the
	// dynamic environment material one request carries at the bottom of its
	// assembly, so what changes with the run never moves the stable prefix.
	PhaseEnvironmentInject
)
type Pipeline struct {
	// Has unexported fields.
}
    Pipeline is the phase-grouped face set of one ordered plugin pipeline:
    every registered Plugin is dispatched into its own phase's narrow interface
    set in the declared order, and one plugin serves exactly one phase.

    The pre-execution faces run as the one Before pass of a tool call; the
    post-commit faces run as the After pass of a committed fact. The pipeline
    is assembled once per dispatch from the borrowed generation's plugins and
    is never reassembled mid-request. The presentation phase holds at most one
    face, because the fact-to-presentation mapping is one-to-one.

func NewPipeline(plugins []Plugin) (Pipeline, error)
    NewPipeline groups the registered plugins into their phase's narrow
    interface set, in the declared order. A plugin whose phase is not one of the
    declared phases, a nil plugin, a plugin that implements no narrow interface
    of its own phase, and a second final presenter each refuse the pipeline by
    name instead of being trimmed in silence.

func (p Pipeline) After(ctx context.Context, fact event.EventID) view.Presentation
    After runs the After pass over one fact whose canonical commit already
    happened: every observer reads the fact first, then the presenter renders
    it. The pass runs after the commit and never before it, so a refused call
    and a call whose commit failed present nothing at all. An observation that
    fails is recorded as a named gap in the returned presentation and never
    rolls the committed fact back; a presenter that drops its text drops the
    presentation only. The pass carries no error return: a failure here is a gap
    or a dropped view, never a reason to touch the committed fact.

    The fact enters the presentation pass exactly once, because the pipeline
    holds at most one presenter: one fact carries one presentation, so the
    returned views hold the one view that presenter rendered or none when the
    pipeline carries no presenter at all.

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

func (p Pipeline) InjectEnvironment(ctx context.Context, request EnvironmentRequest) (string, error)
    InjectEnvironment runs the injection pass of one request assembly: every
    environment face renders its block in pipeline order, and the pass answers
    the blocks that carried text, in that order. A face answering no text
    contributes no block, and a pipeline carrying no environment face answers no
    block at all.

    A face that answers an error refuses the pass at that face by name: no later
    face runs and the assembly answers with the refusal instead of carrying a
    block it could not render.

func (p Pipeline) TriggerCompress(ctx context.Context, request event.CompressRequest) error
    TriggerCompress runs the trigger pass of one filed compress request: every
    compress face answers the request in pipeline order, so the compression
    a producer judged due is the one the wired faces deliver. The request is
    dispatched as the log filed it, and the consumer retires it afterwards —
    never here — so a request whose compression failed stays pending.

    A face that answers an error refuses the pass at that face by name: no later
    face runs, and the caller answers with the refusal instead of counting the
    request as compressed. A pipeline carrying no compress face answers nil,
    so a consumer wired without one refuses nothing of the request it read.

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
    TransformCall, RiskAssessor, Governor, PostCommitObserver, FinalPresenter,
    CompressTrigger, EnvironmentInject — in the declared order.

    The redline list (3 rules): MUST report the one phase the element serves
    and implement that phase's narrow interface. MUST NOT report a phase
    whose narrow interface it does not implement. MUST NOT serve two phases:
    one registration element serves one phase and may carry the faces of that
    phase (the risk phase carries two faces of one ruling).

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
func (r RiskLevel) Rank() int
    Rank reports the strength of one risk grade: critical is the strongest and
    an empty grade is the weakest. The ordering is the one the Before pass reads
    when it keeps the strongest grade its risk faces rendered and the one the
    ruling reads when it compares a grade against a rule threshold, so the two
    readings share this single ordering instead of each spelling its own.

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

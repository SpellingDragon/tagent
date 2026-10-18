package tagent // import "github.com/SpellingDragon/tagent"

The diagnostic visibility face of the host: the unified runtime reading beside
the reflection, training and observation faces, each opened by the switch the
host was started with. Every section reads the owner that already holds the fact
— the query projection, the publication entry, the reflection anchor store,
the recording loss account and the presentation sink — and keeps no second copy
of the live scene.

A face whose switch is off, or whose subsystem the assembly carries no wiring
for, names that state in its reason instead of answering a zero reading that
would pass for an idle subsystem.

The minimal public assembly entry of the runtime: one Run assembles the shared
faces once, publishes the candidate bindings of one declaration through the
sole publication entry, and drives the agent's own persistent event loop per
invocation. This entry is the only public door the execution domain opens;
a later host assembly builds on it instead of assembling a second runtime beside
it.

The assembly is real, not a test stand-in: the model, tools and binding
generation come from prepare and the state owner exactly as a production
assembly would, the tool calls are admitted through the task registration
wrapper, and the terminal of every decision is reduced by the model package's
single reduction entry.

Package tagent is the composition root of the runtime: the single point
where a pure organization declaration becomes a live runtime, and the only
dependency-injection site of the process. Assemble is the sole assembly entry
and the Run it returns is the face the host drives; no second assembly entry
sits beside it.

The root establishes the shared faces before any candidate binding is prepared
and assembles the execution face on top of them, and it keeps no second
copy of the live scene: the effective bindings belong to the state owner,
the obligations it admits belong to the task layer, and the readings it serves
are derived from the facts those owners already hold. The one piece of the
scene the run holds in its own right is the session transcript, which is the
execution scene's read-out face and not a copy of anything an owner holds;
the serializable scene is its facts beside its configuration, never a snapshot
of the conversation body, so a later process recovers the body by replaying the
durable fact chain instead of restoring a body snapshot kept beside the facts.

CONSTANTS

const DefaultPendingDeliveries = 64
    DefaultPendingDeliveries is the finite bound served when a
    PendingDeliveryRequest carries a zero Max, so a read that states no limit
    stays bounded instead of draining the pending queue whole.

const DiagnosticSwitchEnv = "TAGENT_DIAGNOSTICS"
    DiagnosticSwitchEnv names the environment variable the host reads the
    diagnostic switches from: a comma-separated list of subsystem names.

VARIABLES

var (
	// ErrApprovalIdentityMismatch reports an approval response that does not name
	// the approval identity the hold was issued under.
	ErrApprovalIdentityMismatch = task.ErrApprovalIdentityMismatch
	// ErrApprovalDigestMismatch reports an approval response whose argument
	// digest names other arguments than the ones that were held.
	ErrApprovalDigestMismatch = task.ErrApprovalDigestMismatch
)
    ErrApprovalIdentityMismatch and ErrApprovalDigestMismatch are the named
    refusals beside which an approval response is turned away. The authoritative
    hold record refuses a response whose approval identity or argument digest
    does not match the hold it was issued under; the bridge reports the same
    named errors a host reads its contract against.

var ErrDiagnosticNotOpen = errors.New("tagent: the diagnostic endpoints are not open: no request authentication is configured")
    ErrDiagnosticNotOpen reports a diagnostic request on a host shell beside
    no request authentication: the diagnostic face stays closed until a request
    credential is configured for it.

var ErrDiagnosticSwitchUnknown = errors.New("tagent: the diagnostic switch names no subsystem of this host")
    ErrDiagnosticSwitchUnknown reports a diagnostic switch naming no subsystem
    of this host: the switch is refused by name instead of being dropped in
    silence, so a misspelt face never reads as a closed one.

var ErrGovernanceMissing = errors.New("tagent: the formal assembly carries no governance")
    ErrGovernanceMissing reports a formal assembly that carries no governance:
    the sole publication entry is driven under a real governance and never under
    a stand-in that passes every required check by construction.

var ErrPendingDeliveryLimit = errors.New("tagent: pending-delivery read request carries a negative bound")
    ErrPendingDeliveryLimit reports a pending-delivery read request carrying a
    negative bound, so no bounded read could answer it.

var ErrRequestNotAuthenticated = errors.New("tagent: the request subject is not authenticated")
    ErrRequestNotAuthenticated reports a request whose subject did not
    authenticate: the credential is missing, malformed or unmatched, and this
    named refusal is the only answer the endpoint serves for it.

var ErrRetrievalUnwired = errors.New("tagent: the run carries no retrieval face (assembled beside no storage)")
    ErrRetrievalUnwired reports a recall against a run assembled beside no
    storage tier: the retrieval face reads through the observation table over
    the committed store, and a run whose facts live only in memory carries no
    such store, so the recall is refused by name instead of answered as an empty
    reference set that would read as "nothing was found".

FUNCTIONS

func ExecLeaseExplainer(manager *tagentexec.Manager) query.LeaseExplainer
    ExecLeaseExplainer returns the blocker explanation face over one execution
    resource ledger. The manager may be absent for an assembly beside no
    execution resources, in which case no ledger is attached to the read.

TYPES

type ApprovalDecision struct {
	// ApprovalID is the approval ticket the hold was issued under, echoed back by
	// the host. It is the identity the held call is resolved against, never a
	// native CallID that is not globally unique.
	ApprovalID string
	// ExpectedDigest is the digest of the call arguments the hold was issued
	// over. A decision carrying another digest names other arguments than the
	// ones that were held and is refused beside ErrApprovalDigestMismatch.
	ExpectedDigest string
	// InstanceID is the runtime instance the held call was admitted in.
	InstanceID event.InstanceID
	// InvocationID is the responsibility range the held call belongs to.
	InvocationID event.InvocationID
	// DecisionID is the model decision the held call came from.
	DecisionID event.DecisionID
	// CallID is the native provider call identifier of the held call, kept
	// byte-identical to the identifier the model produced.
	CallID string
	// Granted reports whether the held call proceeds; false denies it.
	Granted bool
	// Reason is the host's stated ground for the decision, kept with the record.
	Reason string
}
    ApprovalDecision is a host's resolution of one held call: the approval
    identity and argument digest the hold was issued under, the call scope the
    resolution is bound to, and whether that call may proceed.

type CLI struct {
	// Has unexported fields.
}
    CLI is the command shell of one assembled run beside the authorization scope
    its caller already holds. The shell widens no authorization of its own:
    every reading it serves runs under the very scope it was constructed with.
    The request subject of every HTTP request authenticates at one gate ahead

func NewCLI(run *Run, scope query.AuthorizationScope, opts ...CLIOption) *CLI
    NewCLI returns the thin command shell over one assembled run, reading under
    the authorization scope the caller already holds and carrying the options
    the host configures it with.

func (c *CLI) Main(ctx context.Context, args []string, stdout, stderr io.Writer) int
    Main dispatches one command against the assembled run and reports the exit
    code: "invoke" drives one invocation, "query" serves one diagnostic reading
    and "start" runs the host HTTP service until the context is cancelled or the
    process is asked to terminate. An unknown command or a malformed argument is
    refused with a usage line and a non-zero exit code.

func (c *CLI) Serve(ctx context.Context, listener net.Listener) error
    Serve runs the host HTTP service on the given listener until the context is
    cancelled or the process is asked to terminate, then closes the run through
    the SDK close entry and reports its outcome. The server is held here (not by
    the SDK), so a host embeds the same handler under its own server instead of
    getting a second one from the runtime.

type CLIOption func(*CLI)
    CLIOption configures the host shell at construction.

func WithDiagnostics(switches DiagnosticSwitches) CLIOption
    WithDiagnostics configures the diagnostic switches the host serves its
    optional faces with: the unified runtime map keeps its own route, and
    each switch opens the reflection, training or observation face beside it.
    The start command reads the same switches from the process environment.

func WithFeedbackEndpoint(endpoint http.Handler) CLIOption
    WithFeedbackEndpoint mounts the RL feedback face at POST /feedback behind
    the single authentication gate of the host face. The endpoint is reached
    only for an authenticated request subject, and it rechecks the same
    credential at its own point, so a feedback submission has no unauthenticated
    path into the runtime.

func WithRequestCredential(credential string) CLIOption
    WithRequestCredential configures the request credential the host HTTP face
    authenticates every request subject with. The credential is compared in
    constant time and never logged or echoed back.

type DeliveryID string
    DeliveryID is the stable identity of one outbound delivery: minted when the
    runtime asks its host to make that delivery, and kept stable across every
    bounded read that serves the delivery and every receipt that resolves it.
    A receipt is reconciled against this identity — the delivery it names is the
    delivery that was read — so what became of a delivery is never guessed from
    the most recent message.

type DeliveryReceipt struct {
	// DeliveryID names the stable identity of the outbound delivery this
	// receipt resolves, echoed back by the host. The receipt is reconciled
	// against that identity — the delivery the read face served under it — and
	// never against a guess at the most recent message.
	DeliveryID DeliveryID
	// Accepted reports whether the target accepted the message; the delivery
	// obligation clears only when it is true.
	Accepted bool
	// Reason names why the target did not accept the message; it is empty when
	// Accepted is true.
	Reason string
}
    DeliveryReceipt is the outcome a host reports for one outbound delivery:
    the stable delivery identity of the message the runtime asked it to send,
    whether the target accepted that message, and the reason it did not.

type DiagnosticFace struct {
	// Subsystem names the face this reading belongs to.
	Subsystem DiagnosticSubsystem
	// Open reports whether the face is open: its switch was on and the
	// assembly wired the subsystem behind it.
	Open bool
	// Available reports whether the assembly wired the subsystem behind the
	// face; a face beside no wiring stays closed whatever the switch says.
	Available bool
	// Reason names the state of the face instead of leaving a zero reading to
	// be mistaken for an idle subsystem.
	Reason string
}
    DiagnosticFace reports the state of one optional diagnostic face: the
    subsystem it belongs to, whether its switch opened it, whether the assembly
    wired the subsystem behind it, and the named reason of the reading.

type DiagnosticLifecycle struct {
	// Closing reports whether the close of this run has begun.
	Closing bool
	// Recovered lists one entry per task the startup fold covered.
	Recovered []DiagnosticRecovery
	// Reconciliation is the delegation ledger reading.
	Reconciliation DiagnosticReconciliation
}
    DiagnosticLifecycle is the host lifecycle reading: whether the close of this
    run has begun, the tasks the startup fold restored, and the reconciliation
    face the close settles delegated terminals through.

type DiagnosticReconciliation struct {
	// Wired reports whether the run carries a delegation ledger attached to
	// its close gate.
	Wired bool
	// Booked counts the fresh admissions the ledger still lands.
	Booked int
}
    DiagnosticReconciliation is the delegation ledger reading the close lands
    admitted call terminals through.

type DiagnosticRecordingLoss struct {
	// Kind names the record kind the failed write belonged to.
	Kind string
	// ID is the identity of the record the failed write belonged to.
	ID string
	// Reason is the named failure that stopped the write.
	Reason string
}
    DiagnosticRecordingLoss is one training record the production points could
    not write: the record kind and identity beside the named failure.

type DiagnosticRecovery struct {
	// TaskID is the task the durable chain covers.
	TaskID event.TaskID
	// Owner is the owner the admission material resolved against.
	Owner event.AgentID
	// Status is the status the chain folds to.
	Status task.Status
	// Recovered reports whether the task was rebuilt.
	Recovered bool
	// Refusal names why a chain rebuilt nothing.
	Refusal string
}
    DiagnosticRecovery is one restored task of the startup fold: the identity it
    was admitted under, the owner it resolved to, the status its chain folds to,
    whether it was rebuilt, and the named refusal beside a chain that rebuilt
    nothing.

type DiagnosticReflection struct {
	// Face is the switch and wiring reading of this subsystem.
	Face DiagnosticFace
	// Publication is the generation the sole publication entry currently holds.
	Publication state.Publication
	// Watermark is the reflection watermark reading.
	Watermark DiagnosticWatermark
}
    DiagnosticReflection is the reflection and self-evolution reading: the
    publication account of the run beside the reflection watermark of its anchor
    store.

type DiagnosticReport struct {
	// Instance is the identity of the assembled runtime this reading covers.
	Instance event.InstanceID
	// Switches are the diagnostic switches the reading was taken with.
	Switches DiagnosticSwitches
	// Query is the unified runtime map: the projection reading every section
	// above is derived from, opened whatever the switches say.
	Query query.Snapshot
	// Reflection is the reflection and self-evolution reading.
	Reflection DiagnosticReflection
	// Training is the training record reading.
	Training DiagnosticTraining
	// Telemetry is the post-commit observation reading.
	Telemetry DiagnosticTelemetry
	// Lifecycle is the host lifecycle reading.
	Lifecycle DiagnosticLifecycle
}
    DiagnosticReport is one diagnostic reading of the host: the unified runtime
    map under the caller's scope beside the optional faces the switches opened
    and the lifecycle reading of the run.

type DiagnosticSubsystem string
    DiagnosticSubsystem names one diagnostic face of the host beside the unified
    runtime map.

const SubsystemReflection DiagnosticSubsystem = "reflection"
    SubsystemReflection names the reflection and self-evolution face.

const SubsystemTelemetry DiagnosticSubsystem = "telemetry"
    SubsystemTelemetry names the post-commit observation face.

const SubsystemTraining DiagnosticSubsystem = "training"
    SubsystemTraining names the training record face.

type DiagnosticSwitches struct {
	// Reflection opens the reflection and self-evolution face.
	Reflection bool
	// Training opens the training record face.
	Training bool
	// Telemetry opens the post-commit observation face.
	Telemetry bool
}
    DiagnosticSwitches names the diagnostic faces the host was started with.

    The unified runtime map carries no switch: it is the runtime's own read face
    and stays readable while every switch below is off. Each switch below opens
    one optional face over the subsystem the assembly actually wired.

func DiagnosticSwitchesFromEnv() (DiagnosticSwitches, error)
    DiagnosticSwitchesFromEnv reads the diagnostic switches the host process
    was started with. The variable carries a comma-separated list of subsystem
    names; an empty variable opens no optional face, and a name outside the
    three subsystems is refused with ErrDiagnosticSwitchUnknown.

type DiagnosticTelemetry struct {
	// Face is the switch and wiring reading of this subsystem.
	Face DiagnosticFace
	// Observer reports whether the assembly carries an observation face.
	Observer bool
	// Presentations counts the After pass presentations the run took.
	Presentations int
	// Views counts the rendered views those presentations carry.
	Views int
	// Gaps counts the visible observation gaps those presentations carry.
	Gaps int
}
    DiagnosticTelemetry is the post-commit observation reading: the observer
    wiring beside the presentations the run handed to its read face.

type DiagnosticTraining struct {
	// Face is the switch and wiring reading of this subsystem.
	Face DiagnosticFace
	// Recorder reports whether the assembly carries a training recorder.
	Recorder bool
	// Snapshots counts the request snapshots the run holds.
	Snapshots int
	// Losses lists the records the production points could not write.
	Losses []DiagnosticRecordingLoss
	// Feedback is the attribution of the mounted feedback face, and nil beside
	// no mounted face.
	Feedback *rl.FeedbackAttribution
}
    DiagnosticTraining is the training record reading: the recorder wiring, the
    request snapshots the run holds, the explicit recording loss account and,
    beside a mounted feedback face, the attribution of the submissions it took.

type DiagnosticWatermark struct {
	// Wired reports whether the assembly carries an anchor store.
	Wired bool
	// Established reports whether the watermark was read from the store.
	Established bool
	// Moment is the watermark the store holds; the zero moment marks a store
	// with no consumed turn yet.
	Moment time.Time
	// Reason names an unestablished watermark instead of answering a fresh one.
	Reason string
}
    DiagnosticWatermark is the reflection watermark reading: the moment the last
    consumed reflection turn advanced it to, read from the anchor store that
    persists it.

type HostBridge interface {
	// AdmitInbound converts one inbound host message into an explicit-address
	// delivery at addr and returns the invocation that owns the admitted input.
	//
	// The address is required: the message is admitted to the mailbox of the named
	// session, and no default or most-recent session is substituted for it.
	AdmitInbound(ctx context.Context, addr event.Address, message model.Message) (event.InvocationID, error)

	// ConsumeReceipt consumes the outcome a host reports for one outbound delivery
	// the runtime asked it to make.
	//
	// A delivery stays an open obligation until its receipt says the target accepted
	// it, so a failure or a rejection is recorded as a fact at addr rather than
	// dropped.
	ConsumeReceipt(ctx context.Context, addr event.Address, receipt DeliveryReceipt) error

	// ResolveApproval injects a host's decision for one held call at addr.
	//
	// - A held call is a registered waiting task, never a denied one, so the injected decision carries the explicit address the held call was admitted under.
	// - The decision is bound to the hold it resolves: decision.ApprovalID must name the approval identity tagent issued when that call was parked, and decision.ExpectedDigest must match the argument digest computed at that moment, beside the call scope the hold was issued in.
	// - A decision that does not match is refused by name — ErrApprovalIdentityMismatch or ErrApprovalDigestMismatch — and is never silently applied to another call or to the same call over changed arguments.
	// - A repeated response under the same approval identity is idempotent: the first response takes effect and the repeat reports that first outcome, so the held call is resolved once and executed no more than once.
	ResolveApproval(ctx context.Context, addr event.Address, decision ApprovalDecision) error
}
    HostBridge is the module-root conversion face a host process drives the
    runtime through: the narrow seam by which the WeChat gateway, and any other
    process that embeds the runtime, hands inbound messages in and reports what
    became of the deliveries the runtime asked it to make.

    The bridge only converts. It holds no runtime, no state and no event loop
    of its own, so it never becomes a second runtime beside the one Assemble
    returned. Every entry below addresses its target with the same explicit
    event.Address the runtime uses internally, so a host names its (agent,
    session) and never leaves the runtime to guess the most recent one.
    The redline list (4 rules): MUST convert only: hold no runtime, no state
    and no event loop of its own. MUST address every entry by the explicit
    event.Address it is handed. MUST refuse a decision that does not match the
    approval identity and the argument digest the hold was issued under. MUST
    NOT substitute a most-recent session for an address, nor apply a decision to
    another call.

type InvokeRequest struct {
	// InvocationID identifies the responsibility range.
	InvocationID event.InvocationID
	// Address is the explicit agent session address of the range; it is never
	// defaulted to a most-recent session.
	Address event.Address
	// Episode names the application's own episode boundary the range belongs
	// to. It is read from this input alone and never guessed from a session or
	// a trace: a range that names no episode records no episode.
	Episode event.EpisodeID
	// Input is the native input of the range, in the order the caller admits it.
	Input []model.Message
}
    InvokeRequest is one invocation admitted at the assembly entry: the
    responsibility range identity, the explicit agent session address it belongs
    to, the application's own episode boundary it belongs to and the native
    input messages ready for its first decision.

    The redline list (4 rules): MUST read the episode boundary from this input
    alone. MUST keep the input in the order the caller admits it. MUST NOT
    default the address to a most-recent session. MUST NOT derive an address or
    an episode from a session or a trace.

type NativeAgent struct {
	// Model is the native model of the agent; a nil model refuses the assembly.
	Model model.Model
	// Tools lists the native callable tools of the agent.
	Tools []trpctool.CallableTool
	// Plugins lists the plugin pipeline elements the agent's binding generation
	// carries: the one Before pass grades and the After pass presents every call
	// of that generation over these elements, in the declared order.
	Plugins []plugin.Plugin
}
    NativeAgent is what the embedding process contributes for one declared
    agent: the native model the agent runs against and the native tools visible
    to it. The assembly registers those tools through the task registration
    wrapper before the binding is published, so every call is admitted and
    settled through the task face.

    The redline list (4 rules): MUST register every declared tool through the
    task wrapper before the binding is published. MUST refuse the assembly by
    name when the declared model is nil. MUST NOT replace, cache or reorder the
    tools and plugin elements the host declares. MUST NOT let a native tool run
    beside the admission the task face keeps.

type Option func(*assemblyConfig)
    Option configures the formal assembly at construction.

func WithApprover(approve task.Approver) Option
    WithApprover attaches the approval ruling every call admitted through the
    registration wrapper of this assembly is decided under. A policy attached
    through WithPolicy occupies this seat instead: the pre-execution ruling then
    comes from that policy alone and this approver is not consulted.

func WithEmbedder(embedder memory.Embedder) Option
    WithEmbedder attaches the embedding binding the retrieval face of this
    assembly generates its event vectors under. An assembly beside no embedder
    builds no vector generation at all: recall answers through the keyword leg
    alone and behaves exactly as it did before the embedding existed. The seam
    borrows the binding per vector generation and never owns the endpoint.

func WithEnforcement(grade plugin.Enforcement) Option
    WithEnforcement names the enforcement grade this assembly admits calls
    under. The zero grade behaves as the warn grade beside no approver: a call
    is admitted for execution. The strict grade beside no approver refuses the
    call before any execution instead of admitting it by default.

func WithExecManager(manager *tagentexec.Manager) Option
    WithExecManager attaches the execution resource ledger this assembly
    explains its registered leases through: the blocker explanation read of the
    run reports each registration beside the retention facts the ledger records
    under its key, and beside the accounting the ledger keeps over its leases.
    An assembly beside no ledger still explains its task and notification
    blockers, and refuses to explain a registered lease by name.

func WithGovernance(governance state.Governance) Option
    WithGovernance attaches the governance every publication of this assembly
    is decided under. The formal assembly carries no stand-in governance: an
    assembly beside no governance is refused by name before anything is built.

func WithPolicy(policy plugin.Governor) Option
    WithPolicy attaches the governance policy every call admitted through the
    registration wrapper of this assembly is ruled by: the ruling consumes the
    one Before pass the call carries — the final call beside the risk grade
    its risk faces rendered — exactly once per call, before any execution.
    The policy occupies the approval seat, so it is the sole pre-execution
    ruling of the assembly: a refused call is turned away before any execution
    with its named paired result, and a held call parks in the approval wait.

func WithRecorder(recorder rl.Recorder) Option
    WithRecorder attaches the training record production face this assembly
    writes its episode, decision and attempt records through: the production
    points of the formal root record exactly what they observed, and a write the
    recorder refuses is counted in the explicit recording-loss account of the
    run instead of being dropped silently.

func WithReflectionAnchors(path string) Option
    WithReflectionAnchors points the reflection face of this assembly at the
    anchor record its watermark is read from: the store owns that record and
    answers the watermark the diagnostic reading reports. An assembly beside
    no path carries no reflection anchor store, and its reflection face reports
    that absence instead of a watermark of its own.

func WithStorage(cfg memory.StorageConfig) Option
    WithStorage opens the named storage tier for this assembly and stores the
    task facts through it: every fact the run commits lands on the tier's own
    fact log, and the chains that log already holds are folded back at assembly.
    An assembly beside no storage keeps its facts in memory and restores
    nothing.

func WithTelemetry(observer telemetry.Observer) Option
    WithTelemetry attaches the post-commit observation face this assembly
    observes every committed fact through: the fact is read back from the
    committed ledger unchanged and handed to the observer, so the observation
    is a read-only attachment to facts that already committed and never a second
    source of truth beside that ledger.

type PendingDelivery struct {
	// DeliveryID is the stable identity this delivery is reconciled under: the
	// receipt reporting what became of it names the same identity (see
	// DeliveryReceipt).
	DeliveryID DeliveryID
	// TargetInstance is the runtime instance the delivery is routed to, frozen
	// beside the delivery, so a rebuilt or handed-off instance never re-routes
	// a delivery already asked for.
	TargetInstance event.InstanceID
	// Message is the native message the host delivers, in the native model form
	// the runtime produced it in.
	Message model.Message
}
    PendingDelivery is one outbound delivery the runtime asked its host to
    make and that no receipt has resolved yet: the stable delivery identity the
    receipt is reconciled against, the runtime instance the delivery is routed
    to, and the native message to deliver.

type PendingDeliveryRequest struct {
	// Max bounds the entries one read returns. Zero means
	// DefaultPendingDeliveries.
	Max int
	// After is the continuation form: the DeliveryID of the last entry of the
	// previous result, so the pending deliveries are consumed delivery by
	// delivery. The zero identity starts at the first pending delivery.
	After DeliveryID
}
    PendingDeliveryRequest asks for one bounded read of the outbound deliveries
    waiting for their receipt: the maximum number of entries to hand back and
    the delivery to resume after.

    The bound is in entries and always finite: a zero Max is served as
    DefaultPendingDeliveries and never as an unbounded read, so the pending
    deliveries are consumed in bounded reads and are never buffered into one
    unbounded result.

type PendingDeliveryResult struct {
	// Deliveries holds at most the requested number of pending deliveries.
	Deliveries []PendingDelivery
	// Next is the delivery the following read resumes after.
	Next DeliveryID
	// EOF reports that this read reached the last pending delivery.
	EOF bool
}
    PendingDeliveryResult is one bounded read of the outbound deliveries
    still waiting for their receipt: at most the requested number of entries,
    the delivery the next read resumes after, and whether the end of the pending
    queue was reached. Next is the zero identity when EOF is set and carries no
    resumption point then.

type RequestSnapshotRecord struct {
	// Decision is the decision whose request the snapshot was fixed for.
	Decision event.DecisionID
	// Snapshot is the safe fixed copy of what the model actually saw.
	Snapshot rl.RequestSnapshot
}

    RequestSnapshotRecord is one safe request snapshot kept beside the decision
    whose request it was fixed for: the decision identity of that request and
    the snapshot itself.

type Run struct {
	// Has unexported fields.
}
    Run is the assembled runtime of the minimal entry: the shared faces,
    the sole publication entry, the execution loop over them, the fact ledger
    the tool results are recorded in, and the close and read faces the public
    SDK drives the runtime through. Every face it reads is read from the owner
    that already holds that fact, so the run keeps no second copy of the live
    scene; the one part of the scene it holds in its own right is the session
    transcript, which is the execution scene's read-out face rather than a
    copy of what an owner holds. The serializable scene is its facts beside
    its configuration, never a snapshot of the conversation body: the durable
    fact chain carries what survives a restart, and the body a later process
    reads back is recovered by replaying that chain instead of restoring a body
    snapshot kept beside the facts.

    The redline list (4 rules): MUST borrow through the sole publication entry
    and carry that generation's retirement drain. MUST read every face from
    the owner that already holds the fact. MUST NOT keep a second live copy of
    the scene inside the run. MUST NOT admit new input past the gate the close
    already closed.

func Assemble(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, opts ...Option) (*Run, error)
    Assemble builds the minimal public assembly of the runtime and publishes the
    first binding generation of the declaration before it returns: the shared
    faces are established in order, the tools of every agent are registered
    through the task wrapper, and the candidate passes the sole publication
    entry under the governance the assembly is configured with. A formal
    assembly carries no stand-in governance and is refused by name beside none.
    With a storage tier configured, the task facts persist through that tier
    and the chains it already holds are folded back into the registry before the
    assembly returns. A construction or publication failure frees only what that
    candidate acquired and returns the failure as the cause.

    The loop borrows through the sole publication entry, so every borrow carries
    the retirement drain of the generation it took. The redline list (4 rules):
    MUST establish the shared faces in order and publish the first binding
    generation before it returns. MUST free only what the failed candidate
    acquired. MUST NOT carry a stand-in governance in a formal assembly.
    MUST NOT return a half-assembled run beside a publication failure.

func (r *Run) AttachReplay(chain agent.RequestChain, budget int, projection *memory.ReplayProjection) error
    AttachReplay registers the replayed projection of one session chain on this
    assembly: every request of that session carries the chain's budget assembly
    in front of its transcript, mapped under the frozen event-type→role table.
    A chain that names no session is refused by name: assembling one would join
    every user of the partition into one transcript.

func (r *Run) Close(ctx context.Context, req agent.CloseRequest) (agent.DrainReport, error)
    Close closes the runtime's own side for the named session address and
    drains what it already holds, exactly as the close face of the execution
    domain prescribes: new input is refused before the first settlement pass,
    the obligations already accepted settle only by real settlement beside the
    real stop confirmations of the producers behind them, and a drain that did
    not converge is reported beside the named reason of everything that stayed
    open. The session side closes only after that settlement, so the mailbox
    bookkeeping an in-flight tool result still lands through stays open exactly
    as long as the drain may need it. A repeated close returns the first result
    and runs no second settlement. The redline list (3 rules): MUST refuse new
    input before the first settlement pass. MUST report a drain only beside
    real settlement and the real stop confirmations behind it. MUST NOT read a
    cancelled request, an emptied queue or an elapsed timeout as settlement.

func (r *Run) CommitHistory(addr event.Address, msgs []model.Message)
    CommitHistory appends the transcript one ended drive accumulated to the
    history of its session, so the next invocation of the session reads the
    range's history as it actually ended while no two invocations ever batch
    their ready input together.

func (r *Run) Diagnostics(ctx context.Context, filter query.ListFilter, switches DiagnosticSwitches) (DiagnosticReport, error)
    Diagnostics reads every diagnostic face of this run under the caller's
    authorization scope: the unified runtime map beside the reflection,
    training, observation and lifecycle readings, each taken from the owner that
    already holds it. The read moves nothing and keeps no copy, so two reads of
    one unchanged scene answer the same report and a later admission shows up in
    the next read.

    The unified map carries no switch and stays readable while every optional
    face is closed. A face the assembly carries no wiring for names that state
    in its reason instead of failing the whole read.

func (r *Run) Explain(ctx context.Context, filter query.ListFilter) (query.BlockerReport, error)
    Explain returns one authorized explanation of what keeps this run from
    advancing: every blocker named beside its readable reason and the party that
    advances it, every obligation still open, and every registered lease beside
    the retention facts of its resource, under the authorization scope the
    caller presents. The read grants no authorization of its own and reads every
    fact from the owners at read time.

func (r *Run) Invoke(ctx context.Context, req InvokeRequest) (agent.Turn, error)

    Invoke drives one invocation through the persistent event loop: the input
    is aggregated into the range's own transcript, every decision borrows
    the binding generation effective at its own start, and the settled tool
    notifications backflow to this invocation to drive its next decision. The
    turn reports the decisions it ran and the terminal the latest one reduced
    to; two invocations of one session are never batched together.

    The composition root establishes the session site of the explicit address on
    this instance and grants the diagnostic read face of the run a read of it,
    so the site is owned and read back from the state owner rather than mirrored
    beside it. The redline list (3 rules): MUST aggregate the input into the
    range's own transcript and never batch two invocations of one session.
    MUST borrow the binding generation effective at each decision's own start.
    MUST NOT let an admission receipt stand for the result of the call.

func (r *Run) Presentations() []plugin.Presentation
    Presentations reports the After pass presentations this run handed to its
    diagnostic read face, in the order the facts committed, as a copy the caller
    owns. The presentations are read-out material beside the committed facts;
    reading them moves nothing.

func (r *Run) Publish(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent) (state.PublishOutcome, error)
    Publish runs one declaration's candidate binding through the sole
    publication entry and reports how the attempt took effect. The candidate
    records the baseline of the entry at construction and carries the canonical
    fingerprint of the declaration, so a byte-identical reload publishes nothing
    while a changed declaration takes effect as the generation a request that
    begins executing afterwards borrows.

    The configuration and lease dimensions of the diagnostic read take the
    account the publication just admitted, so the read source follows the owner
    of the live scene instead of a static copy or an empty ledger.

func (r *Run) Query(ctx context.Context, filter query.ListFilter) (query.Snapshot, error)

    Query serves one bounded, immutable reading of the live scene through the
    read-only projection, filtered by the authorization scope the caller already
    holds.

    The reading grants no authorization of its own and derives every count from
    the detail entries it returns: an object outside the presented scope is
    excluded rather than rewritten, and a source that cannot be read is refused
    by name instead of being answered as empty detail.

func (r *Run) Recall(ctx context.Context, q search.Query) (search.Result, error)
    Recall runs one retrieval over the recall face of this run and answers the
    reference set the query hit, ordered from the best hit down and cut to the
    page the query asks for. The retrieval reads through the observation table
    over the committed store and the searcher fusing its two legs, so a hit is
    answered as the recall reference its event was landed with and never as a
    copy of the text behind it: the original stays behind the scoped read path.
    The query carries its own authorized partition set — the recall grants no
    authorization of its own — and a run assembled beside no storage tier is
    refused by name with ErrRetrievalUnwired instead of answered as an empty
    set.

func (r *Run) RecordingLosses() []agent.RecordingLoss
    RecordingLosses reports the training records the production points of this
    assembly could not write, in arrival order, each beside the record identity
    and the named failure that stopped the write. The account is the explicit
    gap ledger of the run: nothing the recorder refused is silently forgotten.

func (r *Run) Recovery() task.RestoreReport
    Recovery reports how the assembly folded the durable chains its storage
    already held at startup: one outcome per task the chains covered. The report
    is empty for an assembly beside no storage.

func (r *Run) RequestSnapshots() []RequestSnapshotRecord
    RequestSnapshots reports the safe request snapshots this assembly fixed
    before its model calls, in call order, as a structural-layer copy the
    caller owns: each Message, VisibleTools and VisibleSkills slice is copied,
    while nested ContentParts share the underlying parts of the fixed snapshot.
    Writing a shared part would be a caller bug; the snapshots themselves
    were fixed before the call. The snapshots are read-out material beside the
    requests they were fixed for; reading them moves nothing.

func (r *Run) SessionHistory(addr event.Address) []model.Message
    SessionHistory reports the transcript the named session accumulated across
    the drives that already ended on it, as a copy the caller owns. The history
    of one session never mixes with the history of another.

    This ledger is the read-out face of the execution scene the run holds, not a
    durability record: what a restarted process reads back is recovered from the
    durable fact chain by replay, so no body snapshot is kept beside the facts.

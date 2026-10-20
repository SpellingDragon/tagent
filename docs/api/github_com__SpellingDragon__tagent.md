package tagent // import "github.com/SpellingDragon/tagent"

Boot is the turnkey entry of the composition root: it reads one host document,
resolves the declaration it carries, and returns the Host the application
drives. The Host owns the assembled run, the scheduled reflection and the
two loopback endpoints; the application fills the outbound message face,
the approval face and the reflection delivery face through the options,
and reads nothing else out of the document.

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

const DiagnosticSwitchEnv = "TAGENT_DIAGNOSTICS"
    DiagnosticSwitchEnv names the environment variable the host reads the
    diagnostic switches from: a comma-separated list of subsystem names.

VARIABLES

var ErrCommittedFactMissing = errors.New("tagent: the committed fact is not held by this run")
    ErrCommittedFactMissing reports a fact identity this run holds neither in
    its live ledger nor in the storage tier it was assembled over.

var ErrDiagnosticSwitchUnknown = errors.New("tagent: the diagnostic switch names no subsystem of this host")
    ErrDiagnosticSwitchUnknown reports a diagnostic switch naming no subsystem
    of this host: the switch is refused by name instead of being dropped in
    silence, so a misspelt face never reads as a closed one.

var ErrGovernanceMissing = errors.New("tagent: the formal assembly carries no governance")
    ErrGovernanceMissing reports a formal assembly that carries no governance:
    the sole publication entry is driven under a real governance and never under
    a stand-in that passes every required check by construction.

var ErrHostAddressMissing = errors.New("tagent: the invocation names no complete session address")
    ErrHostAddressMissing reports an invocation that names no complete session
    address: the runtime is driven at an explicit address and never at a
    most-recent one.

var ErrHostClosed = errors.New("tagent: the host is closed")
    ErrHostClosed reports an invocation or endpoint drive that reached a host
    whose close already began.

var ErrHostConfigMissing = errors.New("tagent: the host names no configuration path")
    ErrHostConfigMissing reports a Boot call that names no configuration path:
    the host has no document to read, so no runtime is assembled.

var ErrHostDelivererMissing = errors.New("tagent: the host enables meditation beside no reflection deliverer")
    ErrHostDelivererMissing reports a document that enables meditation beside
    no reflection deliverer: a fired reflection would have nowhere to go,
    so the host is refused by name instead of running a silent loop.

var ErrHostEmptyTask = errors.New("tagent: the task request carries no user message")
    ErrHostEmptyTask reports a task request whose messages carry no user
    content: a turn reads user input only, so the request is refused by name.

var ErrHostNoActiveSession = errors.New("tagent: no user session has been admitted yet")
    ErrHostNoActiveSession reports a task request arriving before any user turn:
    the endpoint carries no session identity of its own, so it drives the one
    session the host admitted last.

var ErrHostSenderMissing = errors.New("tagent: the host carries no outbound message sender")
    ErrHostSenderMissing reports a host that grants the send tool or admits an
    invocation at the user address beside no outbound message sender: the turn
    would produce output nothing can deliver, so the run is refused by name.

var ErrHostUnauthenticated = errors.New("tagent: the task request is not authenticated")
    ErrHostUnauthenticated reports a task request whose subject did not
    authenticate: a configured credential must arrive as a bearer token,
    and a face beside no credential admits the loopback peer only.

var ErrMemoryDeclaration = errors.New("tagent: the memory declaration is inconsistent")
    ErrMemoryDeclaration reports a memory declaration that cannot be merged into
    one storage tier and one semantic engine: a declaration that names half a
    storage tier, two agents that name different tiers or engines, or an engine
    beside no tier at all.

var ErrMemoryEngine = errors.New("tagent: the memory engine declaration is invalid")
    ErrMemoryEngine reports a semantic engine declaration the core cannot
    assemble: an unknown backend, a negative retrieval bound, or an embedding
    connection that does not name what its provider needs.

var ErrObservationUnwired = errors.New("tagent: the run is assembled beside no observable storage tier")
    ErrObservationUnwired reports an observation read attempted on a run
    assembled beside no storage tier: the face is refused by name rather than
    answered as an empty chain.

var ErrRetrievalUnwired = errors.New("tagent: the run carries no retrieval face (assembled beside no storage)")
    ErrRetrievalUnwired reports a recall against a run assembled beside no
    storage tier: the retrieval face reads through the observation table over
    the committed store, and a run whose facts live only in memory carries no
    such store, so the recall is refused by name instead of answered as an empty
    reference set that would read as "nothing was found".

var ErrUnknownAgentReference = errors.New("tagent: the agent reference names no declared agent")
    ErrUnknownAgentReference reports an agent reference that names no declared
    agent.

var ErrUnknownHostTool = errors.New("tagent: the declared tool is not contributed by this host")
    ErrUnknownHostTool reports a declared tool this host does not contribute.

FUNCTIONS

func EnvironmentBlockGenerations() int64
    EnvironmentBlockGenerations reports how many environment blocks the request
    boundary has rendered in this process so far.

TYPES

type DeliverFunc func(ctx context.Context, source string, targets []event.Address, content string)
    DeliverFunc is the reflection delivery face the host hands a fired
    reflection to: the source agent the reflection belongs to, the declared
    target agents it flows back to and one body per call. A target is named by
    its agent identity alone; the application resolves the sessions it has bound
    to that agent.

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

type Host struct {
	// Has unexported fields.
}
    Host is the assembled runtime of one host document: the run the entry
    drives, the scheduled reflection beside it and the two loopback endpoints
    a supervisor and an inbound channel reach. The host holds no application
    concept of its own: every channel identity, approval ruling and delivery
    target arrives through the options and the invocation address.

func Boot(configPath string, opts ...HostOption) (*Host, error)
    Boot reads one host document and returns the assembled host: the declaration
    is resolved from the document, the models, tool instances, storage tier,
    embedder and reflection plan are built for it, the run is assembled through
    Assemble, and the reflection driver and the loopback endpoints start beside
    it. Every failure before the assembly returns the named refusal and leaves
    nothing running.

func (h *Host) Close(ctx context.Context) error
    Close stops the endpoints and the reflection driver, drains the run and
    closes the MCP servers. A repeated close reports the first result and runs
    no second drain.

func (h *Host) FaceAddr() string
    FaceAddr reports the address the loopback endpoints listen on, and an empty
    address beside a host that serves none.

func (h *Host) Invoke(ctx context.Context, req InvokeRequest) (agent.Turn, error)
    Invoke drives one invocation through the assembled run. An invocation at the
    user address of the entry agent delivers the fresh assistant output through
    the message sender; every other address is driven headless.

func (h *Host) Run() *Run
    Run reports the assembled run the host drives.

type HostOption = Option
    HostOption configures one Boot call; it aliases the assembly Option.

func WithDeliverer(deliver DeliverFunc) HostOption
    WithDeliverer attaches the reflection delivery face of a document that
    enables meditation.

func WithSender(sender MessageSender) HostOption
    WithSender attaches the outbound message face every turn output of the host
    is delivered through.

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
	// TriggerSource is the trigger-source stamp every input fact of the range
	// carries. An empty stamp records none, so an assembly that names no
	// lineage behaves exactly as it did before the stamp existed.
	TriggerSource string
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

type MemoryPlan struct {
	// Has unexported fields.
}
    MemoryPlan is the merged memory declaration of one organization: the one
    storage tier every declaring agent shares and the one semantic engine its
    recall vocabulary generates embeddings under. The plan is resolved once
    at assembly and caches the binding it built, so the storage face and the
    retrieval face of one run run over the one embedder.

func MemoryPlanOf(decl *config.Declaration, storageType, storageRoot string) (*MemoryPlan, error)
    MemoryPlanOf merges every agent's memory declaration into the one plan the
    assembly opens, beside the storage section the document states at its top
    level. Every declaring agent must name the same tier and the same engine:
    a shared store read under two different engines would answer one query two
    ways. An engine beside no tier is refused by name, because an embedding with
    nowhere to persist its vectors is no engine at all. A document that declares
    neither reports a nil plan, which wires no storage face and no embedder.

func (p *MemoryPlan) Embedder() (memory.Embedder, error)
    Embedder reports the embedding binding the plan generates its vectors under:
    the core default implementation over the declared engine, built on the first
    call and cached so one run runs over one binding. A plan beside no engine
    reports no embedder, which is the keyword-only retrieval.

func (p *MemoryPlan) Engine() *config.EngineSpec
    Engine reports the merged semantic engine, or nil beside no engine.

func (p *MemoryPlan) Storage() *ledger.StorageConfig
    Storage reports the storage tier the plan opens, or nil beside no tier.

func (p *MemoryPlan) Tuning() (int, int, int)
    Tuning reports the two leg bounds and the rank constant the merged engine
    declares. A plan beside no engine reports the zero tuning, which keeps the
    retrieval exactly as it stood.

type MessageSender interface {
	// Send delivers one body to the target address.
	Send(ctx context.Context, target event.Address, content string) error
}
    MessageSender is the outbound message face the host delivers turn output
    through: one body, one explicit address, and the delivery outcome reported
    to the caller.

    The redline list (3 rules): MUST deliver the body to the explicit address
    the host names. MUST NOT resolve a target from a most-recent session or from
    the body. MUST NOT report a refused delivery as a delivered one.

type NativeAgent struct {
	// Model is the native model of the agent; a nil model refuses the assembly.
	Model model.Model
	// Tools lists the native callable tools of the agent.
	Tools []trpctool.CallableTool
	// Plugins lists the plugin instances the agent's binding generation
	// carries: the one Before pass grades and the After pass presents every call
	// of that generation over these instances, in the declared order.
	Plugins []plugin.Instance
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

func WithEnforcement(grade governance.Enforcement) Option
    WithEnforcement names the enforcement grade this assembly admits calls
    under. The zero grade behaves as the warn grade beside no approver: a call
    is admitted for execution. The strict grade beside no approver refuses the
    call before any execution instead of admitting it by default.

func WithExecManager(manager *tagentexec.Manager) Option
    WithExecManager attaches the execution resource ledger this assembly
    acquires through and explains its registered leases by: a declaration that
    grants the exec face acquires through this ledger under the monitor rhythm
    its own declaration fixes, and the blocker explanation read of the run
    reports each registration beside the retention facts the ledger records
    under its key. An assembly beside no ledger still explains its task and
    notification blockers, refuses to explain a registered lease by name,
    and refuses a call of the granted exec face by name instead of starting
    anything.

func WithGovernance(governance state.Governance) Option
    WithGovernance attaches the governance every publication of this assembly
    is decided under. The formal assembly carries no stand-in governance: an
    assembly beside no governance is refused by name before anything is built.

func WithMemoryPlan(plan *MemoryPlan) Option
    WithMemoryPlan opens the merged memory declaration of one organization: the
    storage tier every declaring agent shares, the embedding binding its recall
    vocabulary generates its vectors under and the retrieval tuning its engine
    names. The plan is resolved once, so the storage face and the retrieval face
    of this assembly run over the one binding it built. An assembly beside no
    plan keeps the faces its other options state, exactly as it did before the
    plan existed.

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

func WithRetrievalTuning(vectorTopK, keywordTopK, rrfK int) Option
    WithRetrievalTuning bounds the two retrieval legs and names the rank
    constant their fusion scores under. A zero bound keeps that leg unbounded
    and a zero constant keeps search.RRFRankConstant, so the zero value is the
    untuned retrieval exactly as it stood.

func WithStorage(cfg ledger.StorageConfig) Option
    WithStorage opens the named storage tier for this assembly and stores the
    task facts through it: every fact the run commits lands on the tier's own
    fact log, and the chains that log already holds are folded back at assembly.
    An assembly beside no storage keeps its facts in memory and restores
    nothing.

func WithTelemetry(observer plugin.Observer) Option
    WithTelemetry attaches the post-commit observation face this assembly
    observes every committed fact through: the fact is read back from the
    committed ledger unchanged and handed to the observer, so the observation
    is a read-only attachment to facts that already committed and never a second
    source of truth beside that ledger.

type RecallPort struct {
	// Has unexported fields.
}
    RecallPort is the late-bound recall read face a host assembles its recall
    tools over: the tools are constructed before the run exists, so every face
    is bound to the assembled run once the assembly returns. A call before that
    binding answers the named refusal of the recall surface instead of an empty
    page that would read as "nothing was found".

    The port carries the read faces the tools consume and forwards each to
    the face the run wires: the retrieval port, the observation table over
    the committed store and the scoped original store of the storage tier.
    The relation ledger is the host's own, because a run assembled beside no
    relation face wires none, so it is held here from the first call and never
    rebound.

func NewRecallPort(embedder memory.Embedder, trace *replay.MemoryRelations) *RecallPort
    NewRecallPort opens the port over the embedding binding the recall tools are
    built with and the relation ledger the host holds. The binding is fixed at
    construction because the tools read it once when they are built; every other
    face is attached by Bind.

func (p *RecallPort) Bind(r *Run)
    Bind attaches the read faces of the assembled run.

func (p *RecallPort) Event(key event.EventID) (memory.EventReference, bool)
    Event answers the reference of one landed event through the bound table.

func (p *RecallPort) GetScoped(ctx context.Context, key event.EventID, scope ledger.ReadScope) ledger.RecallResult
    GetScoped answers one original read through the bound scoped original store.

func (p *RecallPort) QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
    QueryEvents answers the event references one query matches through the bound
    observation table.

func (p *RecallPort) ReadSegmentScoped(ctx context.Context, key event.EventID, scope ledger.ReadScope, req ledger.SegmentRequest) (ledger.SegmentResult, error)
    ReadSegmentScoped answers one segment read through the bound scoped original
    store.

func (p *RecallPort) Reads() recall.Reads
    Reads reports the read face this port wires for the recall tools:
    the retrieval port, the embedding binding and the table and original store
    of the bound run, beside the relation ledger the host holds. A face the
    deployment wires nothing for stays absent, so the tool that needs it refuses
    by name instead of answering an empty result.

func (p *RecallPort) Reference(key event.EventID) (ledger.OriginalRef, bool)
    Reference answers the original reference one landed event carries through
    the bound table.

func (p *RecallPort) Search(ctx context.Context, query search.Query) (search.Result, error)
    Search answers one retrieval through the bound face.

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

func (r *Run) AttachReplay(chain agent.RequestChain, budget int, projection *replay.ReplayProjection) error
    AttachReplay registers the replayed projection of one session chain on this
    assembly: every request of that session carries the chain's budget assembly
    in front of its transcript, mapped under the frozen event-type→role table.
    The entries a registration carries were replayed from the chain, so they
    are ended material and are presented at once; the facts the run commits
    from here on present themselves once their range ends. A chain that names
    no session is refused by name: assembling one would join every user of the
    partition into one transcript.

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

func (r *Run) CloseMCPServers() error
    CloseMCPServers releases every MCP server the live registry holds: the close
    closes each server's connection exactly once, waits for the calls still in
    flight and answers the release errors it accumulated. An embedding process
    calls it beside the close of the run; a repeated call releases nothing anew.

func (r *Run) CommitHistory(addr event.Address, msgs []model.Message)
    CommitHistory appends the transcript one ended drive accumulated to the
    history of its session, so the next invocation of the session reads the
    range's history as it actually ended while no two invocations ever batch
    their ready input together. A session whose chain carries a mapped sequence
    commits no transcript here: that sequence carried every fact of the range
    as it committed, and the range reads back from it instead of from a second
    transcript of the same facts.

func (r *Run) CommittedEvent(ctx context.Context, id event.EventID) (event.Event, error)
    CommittedEvent reports the fact an identity names: the live fact of this
    process when it holds one, and otherwise the event the storage tier already
    committed. A committed body carries the light fields and no trigger source,
    so a fact restored from the tier carries the lineage its bytes carry and
    never an invented one.

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

func (r *Run) InjectEvent(ctx context.Context, evt event.Event, invocationID event.InvocationID) (agent.Turn, event.EventID, error)
    InjectEvent commits one explicit input event of the named responsibility
    range and drives the loop session of its address with it, exactly as the
    Invoke entry drives a range the host admitted. The caller owns the event's
    address and lineage; this entry stamps only the identities that scope it to
    this run and range, and reports the identity the fact committed under.

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
    beside it. Every input message commits as an entry fact of the range before
    the drive begins, so the turn-start boundary is one committed fact the
    observation reads back. The redline list (3 rules): MUST aggregate the
    input into the range's own transcript and never batch two invocations of one
    session. MUST borrow the binding generation effective at each decision's own
    start. MUST NOT let an admission receipt stand for the result of the call.

func (r *Run) ObservationFacts() meditation.ObservedFacts
    ObservationFacts returns the read-only fact-chain face the scheduled
    reflection of this run judges on: the committed fact chain the run reads,
    carrying the lineage each fact was committed under.

func (r *Run) ObservedEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
    ObservedEvents answers one query over the fact chain this run can observe:
    the events its storage tier already committed, merged with every fact this
    process committed, landed idempotently under the identity each fact carries.
    A run assembled beside no storage tier refuses the read by name.

func (r *Run) Presentations() []view.Presentation
    Presentations reports the After pass presentations this run handed to
    its diagnostic read face, in the order those passes ran — each only
    after the canonical commit of its own fact — as a copy the caller owns.
    The presentations are read-out material beside the committed facts; reading
    them moves nothing.

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

func (r *Run) RecallReads() recall.Reads
    RecallReads returns the recall read faces this run wires beside the
    retrieval face Run.Recall serves: the observation table over the events
    the storage tier committed, the scoped original store of that tier and the
    embedding binding of this assembly. A run assembled beside no storage tier
    wires no table and no original store, and a run beside no embedder wires no
    embedding, so a caller binds exactly the faces that answer and leaves the
    rest as the named refusal the recall surface already states.

func (r *Run) RecordingLosses() []agent.RecordingLoss
    RecordingLosses reports the training records the production points of this
    assembly could not write, in arrival order, each beside the record identity
    and the named failure that stopped the write. The account is the explicit
    gap ledger of the run: nothing the recorder refused is silently forgotten.

func (r *Run) Recovery() task.RestoreReport
    Recovery reports how the assembly folded the durable chains its storage
    already held at startup: one outcome per task the chains covered. The report
    is empty for an assembly beside no storage.

func (r *Run) ReflectionGate() meditation.WatermarkGate
    ReflectionGate reports the watermark gate the reflection face of this run
    reads, or nil when the assembly was configured beside none. The gate is the
    one anchor store the assembly built, so a reflection never opens a second
    store over the same record.

func (r *Run) ReflectionInput() meditation.ReflectionInput
    ReflectionInput returns the injection face the scheduled reflection of this
    run drives its own loop session through: it commits the reflection input
    as a fact of the observing agent's own partition, drives that agent's loop
    session with it and reports what became of the turn.

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

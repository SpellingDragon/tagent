package assembly // import "github.com/SpellingDragon/tagent/assembly"

The compression consumer: the execution half of compression. A producer judges
material over its bound and files a compress request; the consumer reads the
material that request names, folds it through the projection and files the
compress product the execution minted. Judgment and execution share no step,
so what material went is a fact of the chain beside why it went.

Boot is the turnkey entry of the composition root: it reads one host document,
resolves the declaration it carries, and returns the Host the application
drives. The Host owns the assembled run, the scheduled reflection and the
two loopback endpoints; the application fills the outbound message face,
the approval face and the reflection delivery face through the options,
and reads nothing else out of the document.

The trajectory capture of a booted host: one account per assembled run.
Boot opens the file writer under the storage tier's trajectory directory and one
bounded sink over it, and the run fixes, admits and seals every turn through the
scope below.

The capture is one account however many turns it spans, while the execution
loop seals the capture it holds at the end of every drive. The scope therefore
keeps the loop's per-turn seal from closing the run-wide sink: a turn ends
without ending the account, and Close seals the real capture once, after the run
drained.

The delivery ledger of the host: the assistant bodies one drive generates become
durable delivery obligations before any body leaves, a held body waits for the
approval face, and a restart resumes every obligation the ledger still names.
The body, the hold and the refusal each stand as one record under one identity,
so a delivery settled twice is settled once.

The diagnostic visibility face of the host: the unified runtime reading beside
the reflection, training and observation faces, each opened by the switch the
host was started with. Every section reads the owner that already holds the fact
— the query projection, the publication entry, the reflection anchor store,
the recording loss account and the presentation sink — and keeps no second copy
of the live scene.

A face whose switch is off, or whose subsystem the assembly carries no wiring
for, names that state in its reason instead of answering a zero reading that
would pass for an idle subsystem.

The host document dialect one Boot call decodes and the assembly it spells:
the declared agents, their prompts, tools and providers, and the model each
agent samples through. The document is the host's own reading of the shared
declaration shape, so a section the core shares is read here exactly as the
declaration reads it, and only the application section stays opaque.

The loopback endpoints of the host: the liveness probe a supervisor polls, the
task endpoint an inbound channel posts to, and the reload endpoint a supervisor
posts to after it rewrites the configuration source. Every endpoint authorizes
before it reads a body, and a face that is switched off names that state in its
reason instead of answering a request it never accepted.

The model sampling of one declared agent: the temperature, thinking switch
and reasoning effort a host document declares flow onto every request of that
agent's model. The wrapper sits between the published binding and the model the
provider registry built, so a parameter declared in the document reaches the
provider even though the trpc model constructors accept no per-model sampling of
their own.

The wrapper fills a value only while the request itself names none:
an undeclared parameter is never written as a zero, and a sampling a request
already carries is never overridden.

The configuration reload of a booted host: the document is read again, the
declaration it carries is published through the one publication entry, and every
agent whose model reference moved has the move judged as a model replacement
fact on its own pipeline. The lease mounted on the agent runs the rebuild,
publication and installation order, so a reload never installs a model outside
the event-driven replacement path the run already owns.

The document is the host's own dialect, so a reload reads it with the same
strict decoder Boot reads it with; a document the decoder refuses changes
nothing at all.

The host-owned tools and ports: the outbound send tool the model calls,
the scheduled reflection plan and the port that commits its body to a session,
and the request shape an inbound channel posts to the task endpoint. Each one is
a face the host contributes to the assembly, so the host builds it here rather
than leaving the wiring to every deployment.

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

SettleDriver is the event-driven face of one unattended settle: it observes the
delivery facts this run commits for a terminal task notification and drives one
follow-up turn at the address the notification froze its routing to.

The trigger surface is closed, and the anti-loop is semantic rather than a
blacklist:

  - Only the terminal kinds settled, failed and cancelled fire; accepted and
    progress do not.
  - Only a delivery fact carries the settle-lineage meta key, so a backflow
    settle never fires.
  - The fired turn commits no settle fact with a lineage, so it can never fire
    itself again.
  - The mailbox is the one account that answers whether work waits:
    a notification waits there until a turn claims it, drives it and consumes it
    on that same account, so one notification starts one turn however often it
    is observed.
  - The attempt record is the second fence a restart reads: a turn that already
    landed its terminal is closed with its consumption receipt instead of driven
    again, so a stop between the terminal fact and the receipt replays no second
    turn.

Package assembly is the composition root of the runtime: the single point
where a pure organization declaration becomes a live runtime, and the only
dependency-injection site of the process. Assemble is the sole assembly entry
and the Run it returns is the face the host drives; no second assembly entry
sits beside it. The package was the module-root package tagent until D16 moved
it whole into assembly/.

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

const AgeFoldPluginName = "age-fold"
    AgeFoldPluginName is the stable name the age fold plugin instance answers.

const CompressionConsumerName = "compress-consumer"
    CompressionConsumerName is the stable name the compression consumer answers.

const DiagnosticSwitchEnv = "TAGENT_DIAGNOSTICS"
    DiagnosticSwitchEnv names the environment variable the host reads the
    diagnostic switches from: a comma-separated list of subsystem names.

VARIABLES

var (
	// ErrModelLeaseMissing reports a model lease option that names no lease: a
	// version in effect is never invented by the assembly.
	ErrModelLeaseMissing = errors.New("tagent: the model lease option names no lease")
	// ErrModelLeaseUndeclared reports a model lease attached to an agent the
	// declaration does not declare: the declaration is the authorization, so a
	// lease beside no declared agent is refused rather than mounted unwired.
	ErrModelLeaseUndeclared = errors.New("tagent: the model lease names an agent the declaration does not declare")
	// ErrModelChangeOwnerMissing reports a model change naming an agent this
	// run holds no lease for: no lease means no version to move, and the change
	// is refused by name rather than committed against nobody.
	ErrModelChangeOwnerMissing = errors.New("tagent: the model change names an agent this run holds no model lease for")
	// ErrModelChangeTargetMissing reports a model change that names no target
	// version: an unidentified version is never installed.
	ErrModelChangeTargetMissing = errors.New("tagent: the model change names no target version")
	// ErrModelChangeUncommitted reports a model change whose fact did not commit:
	// the judgment reached no ledger, so no observation can read it back.
	ErrModelChangeUncommitted = errors.New("tagent: the model change fact did not commit")
	// ErrCaptureUnscoped reports a capture this assembly cannot scope to one
	// request view: the loop hands a decision's view material per request, and a
	// capture beside no scope would have to invent the view it fixes.
	ErrCaptureUnscoped = errors.New("tagent: the capture cannot be scoped to one request view")
)
    ErrModelLeaseMissing and the lease wiring errors beside it name the way the
    model replacement wiring was refused, so a host routes each by name instead
    of matching on message text.

var ErrCommittedFactMissing = errors.New("tagent: the committed fact is not held by this run")
    ErrCommittedFactMissing reports a fact identity this run holds neither in
    its live ledger nor in the storage tier it was assembled over.

var ErrDeliveryNotFound = errors.New("tagent: no held delivery names that identity")
    ErrDeliveryNotFound reports an approval or refusal that names no held
    delivery: no hold was issued under that identity, or a decision already
    consumed it. A repeated decision is refused by name and delivers nothing.

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

var ErrHostEmptyTask = errors.New("tagent: the task request carries no user message")
    ErrHostEmptyTask reports a task request whose messages carry no user
    content: a turn reads user input only, so the request is refused by name.

var ErrHostMeditationToolMisplaced = errors.New("tagent: a meditation tool is granted to the wrong agent")
    ErrHostMeditationToolMisplaced reports a document that grants the
    cross-session write tool outside the meditation session, or grants the
    send tool to the meditation session itself: the summary tool belongs to the
    reflection turn alone, and send is a host channel face a reflection session
    never carries.

var ErrHostNoActiveSession = errors.New("tagent: no user session has been admitted yet")
    ErrHostNoActiveSession reports a task request arriving before any user turn:
    the endpoint carries no session identity of its own, so it drives the one
    session the host admitted last.

var ErrHostReloadRestartRequired error = errHostReloadRestartRequired{}
    ErrHostReloadRestartRequired reports a reload that moves a field whose only
    consumer Boot built: the storage tier, the request timeout, the observation
    face, the governance rules, and per agent the tool, skill, read, file,
    execution, memory and meditation sections. No path in a running host
    rebuilds those consumers, so the reload refuses the change by name rather
    than answering success for a value nothing reads; a host that needs it is
    restarted with the document.

var ErrHostReloadStructural = errors.New("tagent: the reload changes the declared agent set")
    ErrHostReloadStructural reports a reload that changes the entry agent or the
    declared agent set. Boot wires the model leases, the reflection plan and the
    contributed tool faces of the declared agents once, so a structural change
    is refused by name instead of being half-applied; a host that needs another
    organization is restarted with it.

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

var ErrNoticeTurnIncomplete = errors.New("tagent: the follow-up turn did not complete its delivery")
    ErrNoticeTurnIncomplete reports a follow-up turn that ended without the
    completed delivery its notification owed: a stopped or failed turn hands
    its mailbox entry back unconsumed, so the work stays owed and the next wake
    drives it again, while only a turn that really delivered closes the entry
    beside its consumption receipt.

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

var ErrSettleDriverUnwired = errors.New("tagent: the settle driver is not wired")
    ErrSettleDriverUnwired reports an observation on a driver whose fact lookup,
    mailbox account or follow-up face was never wired: a gap by name instead of
    a silent no-op that reads as a delivered turn.

var ErrUnknownAgentReference = errors.New("tagent: the agent reference names no declared agent")
    ErrUnknownAgentReference reports an agent reference that names no declared
    agent.

var ErrUnknownHostTool = errors.New("tagent: the declared tool is not contributed by this host")
    ErrUnknownHostTool reports a declared tool this host does not contribute.

FUNCTIONS

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

    - The producer of the closing round reads the budget its own agent declares
    over the same declaration the loop's table was set from, so the judgment
    the chain carries names the policy the drive runs under. - The consumer of
    compression judgments is an instance of the material flow like the fold that
    judges them, so every agent's After pass executes the judgments already
    filed instead of waiting for a producer to hand them over. - The producer of
    the closing round is an instance of the same flow: every agent's After pass
    judges the budget of the segment its commit grew, so the closing round of a
    spent budget stands on the chain as the fact it is.

func Boot(configPath string, opts ...HostOption) (*Host, error)
    Boot reads one host document and returns the assembled host: the declaration
    is resolved from the document, the models, tool instances, storage tier,
    embedder and reflection plan are built for it, the run is assembled through
    Assemble, and the reflection driver and the loopback endpoints start beside
    it. Every failure before the assembly returns the named refusal and leaves
    nothing running.

    - The meditation pass yields to the model requests of the observed session,
    and the read of them is the consumption loop's own: the driver is assembled
    before the loop is, so the face is a method value that reads it when a pass
    runs.

    - Every decision of a segment runs under the lineage of the material that
    opened it, so a body a call of that decision sends leaves under the line of
    its own turn instead of the send tool's own default; the direct drive stamps
    the same line on its context.

    - The one approval book of a boot is the host-attached book when the caller
    mounted it, and a boot-local book under <root>/approvals only fills a
    process that declared none, so a caller that mounted the default book keeps
    ruling the very book it handed over; the risk-rule gate and the loop's batch
    fuse read that same book.

    - A fuse declaration on the entry agent wires the loop: a response above the
    call cap or past the long-context threshold parks its whole batch on that
    one book before anything executes, and a declaration that names neither knob
    leaves the limits the process already declared in place.

    - TAGENT_GCTRACE=1 asks for the runtime's GC trace, which the runtime reads
    when the process starts: a boot merges GODEBUG=gctrace=1 so the subprocesses
    this host starts trace, and names in one line that the host process itself
    traces only when it was launched beside it.

func DiagnosticSwitchesFromEnv() (DiagnosticSwitches, error)
    DiagnosticSwitchesFromEnv reads the diagnostic switches the host process
    was started with. The variable carries a comma-separated list of subsystem
    names; an empty variable opens no optional face, and a name outside the
    three subsystems is refused with ErrDiagnosticSwitchUnknown.

func EnvironmentBlockGenerations() int64
    EnvironmentBlockGenerations reports how many environment blocks the request
    boundary has rendered in this process so far.

func MemoryPlanOf(decl *config.Declaration, storageType, storageRoot string) (*MemoryPlan, error)
    MemoryPlanOf merges every agent's memory declaration into the one plan the
    assembly opens, beside the storage section the document states at its top
    level. Every declaring agent must name the same tier and the same engine:
    a shared store read under two different engines would answer one query two
    ways. An engine beside no tier is refused by name, because an embedding with
    nowhere to persist its vectors is no engine at all. A document that declares
    neither reports a nil plan, which wires no storage face and no embedder.

func NewSkillLoadSink(agent event.AgentID) knowledge.LoadSink
    NewSkillLoadSink mounts a fresh loaded-skill ledger on one agent and answers
    the sink that agent's knowledge surface publishes loaded bodies to. The host
    mounts one sink per knowledge surface it constructs, so a surface rebuilt
    on reload replaces its agent's ledger instead of inheriting the bodies the
    previous generation loaded. The request boundary of the agent reads the
    ledger back through the agent identity, so the bodies a load published enter
    the next request of that agent and no other. A host that mounts no sink
    keeps the load's name-level answer exactly as before.

TYPES

type AgeFoldPlugin struct {
	// Has unexported fields.
}
    AgeFoldPlugin judges the projection of the session the committed fact
    belongs to once its rendered volume passes the window threshold: the process
    detail of the older task turns is due for a fold and the middle band for one
    history summary. The plugin files each judgment as a compress request and
    executes nothing of it, so why material goes is a fact beside the product
    that says what went, and the consumer that reads the request does the
    folding.

    The plugin is an instance of the material flow like any other: it reads
    the fact material and answers an observation, so the pipeline dispatches it
    beside the reflection driver and the commit that grew the projection is the
    trigger. How material folds stays the projection's own rule — the plugin
    judges nothing about it.

func NewAgeFoldPlugin(sessions SessionFolds) *AgeFoldPlugin
    NewAgeFoldPlugin assembles the fold plugin over the session face of one run.
    A nil face answers no session, so an assembly mounted beside none folds
    nothing instead of failing on the first commit.

func (p *AgeFoldPlugin) Name() string
    Name reports the stable name of this plugin instance.

func (p *AgeFoldPlugin) ObserveCommit(ctx context.Context, id event.EventID) error
    ObserveCommit judges the projection of the session the committed fact
    belongs to once that projection's rendered volume passes the window
    threshold, and files the judgment of every product the fold would mint. The
    material itself is left as it stands: the consumer that reads the judgment
    does the folding.

    The duty is read-only over the fact and over the projection: a fact whose
    session registered no projection, a session whose window names no positive
    token window and a projection still inside its threshold each leave the
    commit as it stands. A failed judgment is reported without rolling the
    commit back.

func (p *AgeFoldPlugin) Port() plugin.Port
    Port reports the consumption the plugin serves: one committed fact in,
    one observation out. The observation is the fold the fact may trigger.

type CompressionConsumer struct {
	// Has unexported fields.
}
    CompressionConsumer executes the compression judgments filed on a chain.
    It reads the material one request names, executes the fold the judgment
    already made and files the product, so the projection restores that product
    from the chain instead of folding the material a second time.

    The redline list (3 rules): MUST file at most one product per request:
    the product identity is content-addressed over the judgment. MUST NOT judge
    material of its own: only the requests already on the chain are executed.
    MUST NOT execute a judgment whose material does not stand: it stays pending
    rather than answering a product of other material.

func NewCompressionConsumer(sessions compressSessions) *CompressionConsumer
    NewCompressionConsumer assembles the consumer over the session face of
    one run. A nil face answers no session, so an assembly mounted beside none
    executes nothing instead of failing on the first commit.

func (c *CompressionConsumer) Execute(ctx context.Context, session event.SessionID) error
    Execute runs every judgment of one session no product answers yet: it reads
    the material the judgment named, folds it and files the product beside it.
    A session whose chain registered no projection, and a window naming no
    positive token window, execute nothing — the judgment stays pending as the
    unexecuted judgment it is.

    The pass reads the projection and never judges it: whether material is over
    its bound is already the request on the chain, and the plan only answers
    which product that request names.

func (c *CompressionConsumer) Name() string
    Name reports the stable name of this plugin instance.

func (c *CompressionConsumer) NoteProduct(product event.CompressProduct)
    NoteProduct notes one product a chain already carries, so the judgment it
    answers is never executed a second time: this is the restart path, where the
    products of a chain answer judgments the process does not hold.

func (c *CompressionConsumer) ObserveCommit(ctx context.Context, id event.EventID) error
    ObserveCommit executes the judgments one session filed that no product
    answers yet: the fact that triggered it names the session, and every
    judgment of that session stands where the producer left it.

func (c *CompressionConsumer) Port() plugin.Port
    Port reports the consumption the consumer serves: one committed fact in,
    one execution of the judgments already on the chain.

func (c *CompressionConsumer) Record(session event.SessionID, request event.CompressRequest)
    Record notes one judgment a producer filed for a session, so the consumer
    executes it. A judgment already noted is noted once: the identity is
    content-addressed, so a re-filed judgment is the same judgment.

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

type FoldWindow struct {
	// MaxTokens is the model window the chain is replayed into, in tokens.
	MaxTokens int
	// CompressThreshold is the share of that window the replayed projection may
	// fill before the age fold runs.
	CompressThreshold float64
	// KeepRecentTasks is the number of recent task turns the age fold keeps whole.
	KeepRecentTasks int
}
    FoldWindow is the replay window one session chain registered: the model
    window the chain is replayed into, in tokens, and the fold settings the
    declaration gave that window. The unset settings read their declaration
    defaults when the window is normalized, so a declaration naming only
    max_tokens folds under the default share of its own window.

func (w FoldWindow) VolumeThresholdBytes() int
    VolumeThresholdBytes reports the rendered volume a projection of this window
    may carry before an age fold is due, in bytes: the share of the window the
    declaration allows, converted at the one character-to-token ratio the budget
    assembly packs requests under, so the fold fires where the assembly would
    start degrading the projection rather than after it already had. A window
    naming no tokens reports no threshold, and a fold read under it never fires.

type HeldDelivery struct {
	// ID names this hold and is the identity ApproveDelivery and
	// RefuseDelivery resolve.
	ID string

	// Delivery is the semantic the ruler held.
	Delivery plugin.Delivery

	// HeldAt is the moment the host recorded the hold.
	HeldAt time.Time
}
    HeldDelivery is one outbound body the review plugin held before the sending
    plugin delivered it: the identity an approval or refusal names, the delivery
    semantic the ruler read, and the moment the hold was recorded.

type Host struct {
	// Has unexported fields.
}
    Host is the assembled runtime of one host document: the run the entry
    drives, the scheduled reflection beside it and the two loopback endpoints
    a supervisor and an inbound channel reach. The host holds no application
    concept of its own: every channel identity, approval ruling and delivery
    target arrives through the options and the invocation address.

func (h *Host) ApproveDelivery(ctx context.Context, id string) error
    ApproveDelivery re-delivers one held body exactly once: the body is sent to
    the target it named, and the hold is consumed, so a repeated approval names
    no hold and delivers no second copy. A send the channel refuses restores
    the hold for a later approval and is reported to the caller. Every hold is
    recorded beside the review table, so the approval resolves the body across a
    restart as well.

    The receipt of the approval lands only after the send it confirms returned:
    a receipt the tier refuses leaves the obligation owed and answers the landed
    delivery by name, so the caller learns the body reached the channel and the
    account did not — a restart delivers the body once more, which is the one
    extra copy an unconfirmed receipt tolerates.

func (h *Host) Close(ctx context.Context) error
    Close stops the endpoints and the reflection driver, drains the run and
    closes the MCP servers. A repeated close reports the first result and runs
    no second drain.

func (h *Host) FaceAddr() string
    FaceAddr reports the address the loopback endpoints listen on, and an empty
    address beside a host that serves none.

func (h *Host) HeldDeliveries() []HeldDelivery
    HeldDeliveries reports the bodies the host owes a target before delivery:
    the holds the review plugin parked and the redeliveries a refused send owes,
    oldest first. The result is a copy the caller owns: reading here changes no
    hold, and only ApproveDelivery or RefuseDelivery resolves one.

func (h *Host) Invoke(ctx context.Context, req InvokeRequest) (agent.Turn, error)
    Invoke drives one invocation through the assembled run. An invocation at the
    user address of the entry agent delivers the fresh assistant output through
    the message sender; every other address is driven headless.

    A user message commits as a fact of its session and wakes the resident
    consumption loop of its address: the loop pulls the message with whatever
    else arrived, batches them into one model request, and the call answers with
    the aggregated turn of the segment that consumed it — a second entry whose
    message the same segment consumed answers the same turn. Every other address
    drives a turn of its own through the run, exactly as before.

func (h *Host) RefuseDelivery(ctx context.Context, id string) error
    RefuseDelivery discards one held body: nothing is delivered, the hold is
    consumed, and an audit feedback fact without a model payload records the
    refusal at the target address. The refusal consumes the durable record as
    well, so no later boot owes the body again.

    Both commits are checked before the refusal publishes: a record or an audit
    fact the tier refused answers the caller by name and leaves the body in the
    review table, so a refusal no account accepted is never read as a decision.

func (h *Host) Reload(ctx context.Context) (hostReloadReport, error)
    Reload reads the configuration source again, publishes the declaration
    it carries, and files the model replacement of every agent whose model
    reference moved. The publication is the one entry the run already owns;
    the replacement facts are the same facts a declaration-driven change files,
    so the lease mounted on each agent decides whether the new version takes
    effect.

func (h *Host) Run() *Run
    Run reports the assembled run the host drives.

type HostOption func(*assemblyConfig)
    HostOption configures one Boot call. It is the host-facing name of the
    assembly option, so the host protocol's option type is declared here rather
    than borrowed by alias.

func WithDeliveryRuler(ruler plugin.DeliveryRuler) HostOption
    WithDeliveryRuler attaches the review plugin's delivery face: the host rules
    every outbound body through it before the sender is called. A host beside no
    ruler delivers every body, so the default review allows every delivery.

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

type ModelLeaseOption func(*leaseState)
    ModelLeaseOption configures the model lease one agent is assembled with.

func WithLeaseRebuild(rebuild func(version string) (model.Model, error)) ModelLeaseOption
    WithLeaseRebuild names the rebuild step a model replacement fact runs:
    the step receives the version the fact asks for and answers the model that
    version is installed as. A rebuild that fails installs no version, and the
    lease keeps the version in effect.

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

type Option = HostOption
    Option configures the formal assembly at construction. It is the assembly
    name of the host option: one config struct, so the manual and the host entry
    accept the same constructors.

func WithApprovals(approvals *approval.ApprovalManager) Option
    WithApprovals attaches the approval book a host document's risk table grades
    through: a document that declares risk_rules is opened as a policy over this
    book, so a call at the critical grade registers one approval request and
    parks in the approval wait, and the book stays the one external approvers
    rule through. An assembly beside no book keeps the named refusal of the
    critical grade instead of parking anything.

func WithApprover(approve task.Approver) Option
    WithApprover attaches the approval ruling every call admitted through the
    registration wrapper of this assembly is decided under. A policy attached
    through WithPolicy occupies this seat instead: the pre-execution ruling then
    comes from that policy alone and this approver is not consulted.

func WithCapture(capture rl.Capture) Option
    WithCapture attaches the trajectory capture this assembly fixes each request
    view through, admits each observed attempt through and seals each turn
    through. The capture must be scoped, because the loop hands a decision's
    view material per request and the capture fixes the snapshot against that
    view; a capture beside no scope is refused by name at assembly.

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

func WithModelLease(agentID event.AgentID, lease *rl.ModelLease, opts ...ModelLeaseOption) Option
    WithModelLease attaches the model lease of one agent to this assembly:
    the agent's binding carries the lease's stable model handle instead of the
    model the host contributed, every committed model replacement fact owned
    by the agent is consumed by the lease observer mounted on its pipeline,
    and the lease's rebuild-publication-installation order decides whether
    the requested version takes effect. A rebuild and a publication entry are
    configured through the model lease options; a replacement beside no rebuild
    takes the model the publication that judged the change contributed.

    The lease agent must be declared by the assembly's declaration: the
    declaration is the authorization, and a lease beside no declared agent is
    refused by name.

func WithPlugins(build func(*Run) ([]plugin.Instance, error)) Option
    WithPlugins builds the run-level plugin instances every agent generation
    carries: an event-driven face attached here observes every commit of
    the run, whichever agent session committed it, exactly as a contributed
    instance observes the commits of its own agent. The builder runs once,
    after the watermark gate is established and before the first publication,
    and the instances it answers are never replaced.

func WithPolicy(policy plugin.Governor) Option
    WithPolicy attaches the governance policy every call admitted through the
    registration wrapper of this assembly is ruled by: the ruling consumes the
    one Before pass the call carries — the final call beside the risk grade
    its risk faces rendered — exactly once per call, before any execution.
    The policy occupies the approval seat, so it is the sole pre-execution
    ruling of the assembly: a refused call is turned away before any execution
    with its named paired result, and a held call parks in the approval wait.

func WithRecorder(recorder rl.Recorder) Option
    WithRecorder attaches the training record pipeline this assembly derives
    its attempt records through: the run admits a decision's request view and an
    attempt's reduced outcome once, and the pipeline writes one attempt record
    per admission exactly as the attempt observed it, so a recorded outcome
    is never re-derived beside the model's own. The episode boundary and the
    stored input of a decision are not observed at this admission: a pipeline
    that needs those derives them from the capture's evidence beside the chain
    the run keeps. A write the recorder refuses is counted in the explicit
    recording-loss account of the run instead of being dropped silently.

func WithReflectionAnchors(path string) Option
    WithReflectionAnchors points the reflection face of this assembly at the
    anchor record its watermark is read from: the store owns that record and
    answers the watermark the diagnostic reading reports. An assembly beside
    no path carries no reflection anchor store, and its reflection face reports
    that absence instead of a watermark of its own.

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

func (r *Run) ActiveSessions(agentID event.AgentID) []event.Address
    ActiveSessions reports the complete addresses this run has established,
    filtered by agent: the live bindings a cross-session delivery resolves
    against, ordered by agent and session so the same scene always answers the
    same list.

func (r *Run) AttachReplay(chain agent.RequestChain, window FoldWindow, projection *replay.ReplayProjection) error
    AttachReplay registers the replayed projection of one session chain on this
    assembly: every request of that session carries the chain's window assembly
    in front of its transcript, mapped under the frozen event-type→role table.
    The entries a registration carries were replayed from the chain, so they
    are ended material and are presented at once; the facts the run commits
    from here on present themselves once their range ends. A chain that names
    no session is refused by name: assembling one would join every user of
    the partition into one transcript. A window of no tokens is accepted as
    the no-budget window it means: the requests present the whole projection
    with no budget bound, and the age fold measures against the default window
    instead of none. The seam carries no agent identity, so the registration
    is kept for every user of the session it names; the chains this run commits
    register under the full address of the agent that owns them through
    attachScopedReplay, so they never cross agents that share a session id.

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
    CommitHistory is the write side of the same one face: the transcript one
    ended drive accumulated is already carried by the mapped sequence of the
    session — every fact of the range entered it as it committed, in commit
    order, with the boundary the next request presents — so committing it here
    keeps no second copy and appends nothing.

    The call is kept as the seam the loop commits an ended drive's transcript
    through: a loop beside this face needs no branch of its own, and a run
    beside no mapped sequence records no transcript at all, exactly as a chain
    whose facts committed nothing has nothing to present. The history a restart
    reads comes back from the durable fact chain by replay, never from this
    seam.

func (r *Run) CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error)
    CommitToSession commits one event into the fact chain of an explicit
    address, under a fresh invocation identity when the caller states none.
    It is the cross-session write face a reflection turn delivers a summary
    through: the event keeps the address it names, the commit is observed
    through the target agent's own published pipeline exactly as any other
    commit of this run, and an address the run cannot establish is refused by
    name.

    The commit is a write of this run, never a second store beside it: the fact
    lands in the same live ledger, on the same durable log and in the same
    mapped sequence as the target session's own input, so that session's next
    assembly reads it like any other external input. The address block of the
    event is backfilled, never overwritten: a source, target or owner the caller
    already stamped is kept as the caller wrote it, and only an unstated stamp
    reads as the target address of the commit.

func (r *Run) CommittedEvent(ctx context.Context, id event.EventID) (event.Event, error)
    CommittedEvent reports the fact an identity names: the live fact of this
    process when it holds one, and otherwise the event the storage tier already
    committed. A committed body carries every field the event committed with,
    so a fact restored from the tier carries the whole metadata projection, the
    native payload and the lineage its bytes carry and never an invented one.

func (r *Run) CommittedFacts() int
    CommittedFacts reports how many facts this run holds: the counter the
    metrics endpoint reports beside the turns and deliveries of the host.

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

func (r *Run) ExecState(key tagentexec.ResourceKey) (tagentexec.Explanation, error)
    ExecState reports the retained state of one execution resource the ledger
    of this run holds: the lease deadline, the renewals observed after the
    acquiring call returned and the stop the resource was last seen under.
    The read is the diagnostic face of the same ledger the exec face acquires
    through, so it moves nothing; a run assembled beside no ledger refuses the
    read by name instead of answering an empty state.

func (r *Run) InjectEvent(ctx context.Context, evt event.Event, invocationID event.InvocationID) (agent.Turn, event.EventID, error)
    InjectEvent commits one explicit input event of the named responsibility
    range and drives the loop session of its address with it, exactly as the
    Invoke entry drives a range the host admitted. The caller owns the event's
    address and lineage; this entry backfills only the stamps the caller left
    unstated — an unstated source and target read as the owner of the event, an
    unstated invocation identity as the one this call names — while the runtime
    instance stamp is always this run's own and never the caller's to write.
    The fact commits under the identity the event carries after the backfill,
    and the drive runs the range under that same identity.

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

func (r *Run) LeaseLosses() []error
    LeaseLosses reports the declared model changes whose replacement fact could
    not commit, as a copy the caller owns: a judgment that reached no ledger is
    named here rather than read as a replacement that happened.

func (r *Run) LeasePublications() []rl.ModelIdentity
    LeasePublications reports the versions the model leases installed through
    this run's default publication entry, in installation order, as a copy the
    caller owns.

func (r *Run) ModelInFlight(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool)
    ModelInFlight reports the model request one site is executing, read off the
    consumption loop of this run: the range the decision serves and the identity
    of the decision itself. A site in another instance, a run beside no loop and
    a site running no request each answer no flight.

func (r *Run) NarrowFace(ctx context.Context, agent event.AgentID, selection state.VisibleSelection) error
    NarrowFace narrows the face one agent may see from the next request boundary
    on, or opens it again when the selection carries no list: the binding is not
    republished, so the change reaches the very next request. The host and the
    plugins that guard one agent call this; the request's tools, the environment
    block and the capture view all read the narrowed face.

func (r *Run) NoteModelChange(agentID event.AgentID, from, to, source string) (event.EventID, error)
    NoteModelChange files the judgment that one agent's model reference moves
    from one version to another and commits it as a model replacement fact
    on the agent's own pipeline, the explicit entry a host drives when its
    own configuration source observed the change; a publication that moves
    an agent's declared model reference files the same fact itself, with the
    configuration source stamp. The lease mounted on the agent runs the rebuild,
    publication and installation order and decides whether the version takes
    effect.

    The identity of the committed fact is returned; a change this run cannot
    file — no lease for the agent, no target version, no commit face — is
    refused by name, and an empty from version is the version the lease holds.

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

func (r *Run) ReplayEntries(session event.SessionID) ([]replay.ProjectionEntry, bool)
    ReplayEntries reports the projection entries one session chain registered
    for replay, in canonical form, and whether the session registered one
    at all. The read is the diagnostic face of the mapped sequence: the fold
    products a commit-driven fold minted are entries of this projection, so this
    is how a caller reads the sequence the request boundary presents without
    driving a request.

func (r *Run) RequestSnapshots() []RequestSnapshotRecord
    RequestSnapshots reports the safe request snapshots this assembly fixed
    before its model calls, in call order, as a structural-layer copy the
    caller owns: each Message, VisibleTools and VisibleSkills slice is copied,
    while nested ContentParts share the underlying parts of the fixed snapshot.
    Writing a shared part would be a caller bug; the snapshots themselves were
    fixed before the call. The snapshots are read back from the capture that
    fixed them — their one home — and a run assembled beside no capture answers
    none. Reading them moves nothing.

func (r *Run) SessionFacts(addr event.Address) []event.Event
    SessionFacts reports the facts of one session in the order they committed:
    the chain one address wrote, as the run holds it — the facts this process
    committed beside every fact the storage tier already held at assembly,
    both read through the one ledger. A fact outside the session the address
    names is answered never.

func (r *Run) SessionHistory(addr event.Address) []model.Message
    SessionHistory reports the transcript the named session accumulated across
    the drives that already ended on it, as a copy the caller owns.

    The history of one session chain is one face: the mapped sequence its
    commits record, read through the request boundary of the next invocation.
    This face carries no second copy of it, so it answers an empty transcript
    for every chain — a session whose material exists reads it from that
    sequence, and a session whose commits recorded none has none to read.
    A reader that keeps a transcript beside the sequence would present the same
    facts twice, once from the sequence and once from the copy, and a restart
    would present a different history than the live process, because the copy is
    not a durability record while the sequence is replayed from the durable fact
    chain.

type SessionFolds interface {
	// SessionOf reports the session chain one committed fact belongs to.
	SessionOf(ctx context.Context, id event.EventID) (event.SessionID, bool)
	// ProjectionOf reports the projection one session registered for replay
	// beside the window it was registered under.
	ProjectionOf(ctx context.Context, session event.SessionID) (*replay.ReplayProjection, FoldWindow, bool)
	// FileCompressRequest files one compression judgment the fold answered on the
	// chain of the session whose material was judged.
	FileCompressRequest(ctx context.Context, session event.SessionID, request event.CompressRequest) error
}
    SessionFolds is the session face the age fold plugin observes one commit
    through: the session chain a committed fact belongs to, the projection and
    the window that chain registered, and the filing of the compression judgment
    the fold answered. The composition root implements the face over the live
    ledger and the replay registrations of its run, so the plugin holds no chain
    and no ledger of its own.

    The redline list (3 rules): MUST answer a fact no session chain owns as
    unowned. MUST answer the projection and the window the session registered,
    never one the plugin invents. MUST NOT fold a session whose registered
    window names no positive token window.

type SettleDriver struct {
	// Has unexported fields.
}
    SettleDriver is the fact->observation face of the one turn a terminal notice
    fires at the address its routing froze. An observation wakes the pump,
    and the pump reads the mailbox account: every entry there is claimed, driven
    beside the committed fact its notification points at, and consumed on that
    same account once the turn returned. The driver keeps no queue of its own,
    so nothing it holds can be lost by a stop or a restart.

func NewSettleDriver(
	lookup func(context.Context, event.EventID) (event.Event, error),
	wake func() SettleWake,
	fire func(context.Context, event.Address, event.Event) error,
) *SettleDriver
    NewSettleDriver assembles the settle face over the fact lookup of one run,
    the mailbox account of that run and the follow-up drive of its host.
    The pump starts beside the driver and rests until a wake or an observation
    asks it to read the account.

func (d *SettleDriver) DrivesObligation()
    DrivesObligation marks this face's duty: the observation wakes the pump,
    and the work it woke is owed on the mailbox account, so a failed observation
    leaves the follow-up turn owed and the account hands it to the next
    decision.

func (d *SettleDriver) Name() string
    Name reports the stable name of this plugin instance.

func (d *SettleDriver) ObserveCommit(ctx context.Context, id event.EventID) error
    ObserveCommit reads the fact one commit wrote and wakes the pump when the
    fact is one the settle trigger table fires on. The observation itself starts
    nothing: the mailbox is the one account that answers whether work waits,
    so a repeated observation of one delivered notification claims no second
    turn — the entry is either consumed already or claimed by the turn it
    started. A fact that is not a terminal delivery fact is ignored in silence:
    those are not settles nobody drives, they are settles somebody already read.
    A failed read is reported back and never rolls the committed fact back.

func (d *SettleDriver) Port() plugin.Port
    Port reports the consumption this driver serves: one committed fact in, one
    observation out. The observation is the follow-up turn the fact may fire.

func (d *SettleDriver) Stop()
    Stop ends the driver: no further commit wakes the pump, the turn in flight
    is cancelled, and Stop waits for the pump to return. What the mailbox
    still holds is kept as it stands — an unconsumed notice outlives the stop,
    so the next process resumes the turn it owes. A repeated stop is a no-op.

func (d *SettleDriver) Wake()
    Wake asks the pump to read the mailbox account again. The boot calls it once
    the restored account is in place, and any number of wakes collapse into the
    work the account already carries: a wake starts no turn of its own.

type SettleWake interface {
	// RestoreOwed folds the unconsumed work a durable account still owes back
	// into the mailbox, so a stopped run's notices wait for the next decision
	// instead of dying with the process that accepted them.
	RestoreOwed(ctx context.Context) error
	// Unconsumed reports every mailbox entry whose consumption has not
	// completed, in the one order the account answers in.
	Unconsumed() []state.WorkSeat
	// Claim takes one pending entry for this executor alone, so one
	// notification starts one turn however often it is observed.
	Claim(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error
	// Release hands one claimed entry back to the pending pool, so an executor
	// that stopped beside a claim keeps the work owed instead of holding it.
	Release(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error
	// Consume records the completed consumption of one claimed entry, on the
	// same account the claim was taken from.
	Consume(ctx context.Context, addr event.Address, instance event.InstanceID, seq state.AcceptSeq) error
}
    SettleWake is the mailbox account the settle pump drives its turns from:
    one face that answers whether work waits and moves one entry through claim,
    release and consumption on the same ledger. The state store behind one run
    satisfies it.

    The redline list (4 rules): MUST start exactly one follow-up turn per
    claimed entry and close it beside its consumption receipt. MUST close an
    entry whose recorded attempt already landed its terminal with that turn's
    receipt and drive no second turn. MUST NOT replay an entry whose consumption
    receipt is already recorded. MUST NOT hold a claimed entry beside a turn
    that stopped before its delivery.

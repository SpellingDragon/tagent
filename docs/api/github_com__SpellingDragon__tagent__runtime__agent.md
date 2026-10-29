package agent // import "github.com/SpellingDragon/tagent/runtime/agent"

Package agent is the runtime's event scheduling and single model decision layer:
it drives the events of one invocation with the binding generation that
state lent it, admits a sub-agent call as a task, and closes an agent by real
obligation settlement.

The package owns no state of its own. A decision borrows one generation of the
state binding and keeps it for the whole decision, and the task layer keeps the
obligations of every call the agent admitted. The execution face is a port the
composition root wires; the loop, the delegation ledger and the close gate are
the behaviour behind it.

VARIABLES

var (
	// ErrDelegationIdentityMissing reports a delegation that carries no complete
	// call identity, so no later progress or terminal could ever associate with
	// the call that issued it.
	ErrDelegationIdentityMissing = errors.New("agent: the delegation carries no call identity")
	// ErrDelegationAddressIncomplete reports a delegation whose target names no
	// complete agent session. The ledger never selects a session of its own.
	ErrDelegationAddressIncomplete = errors.New("agent: the delegation target names no complete agent session")
	// ErrDelegationUnknown reports a task this ledger holds no delegation for.
	ErrDelegationUnknown = errors.New("agent: the task is no delegation this ledger holds")
	// ErrDelegationNotTerminal reports a completion that carries no terminal
	// status.
	ErrDelegationNotTerminal = errors.New("agent: the completion carries no terminal status")
	// ErrDelegationTerminal reports a delegation that already reached a terminal.
	ErrDelegationTerminal = errors.New("agent: the delegation already reached a terminal")
	// ErrDelegationAdmissionRefused reports a delegated call the task face
	// refused to admit without naming a cause of its own.
	ErrDelegationAdmissionRefused = errors.New("agent: the delegated call was refused admission")
	// ErrDelegationCommitRefused reports a settle the commit carrier did not
	// store: the task moves nowhere and the ledger reports no terminal.
	ErrDelegationCommitRefused = errors.New("agent: the delegation settle was not committed")
)
    ErrDelegationIdentityMissing, ErrDelegationAddressIncomplete,
    ErrDelegationUnknown, ErrDelegationNotTerminal, ErrDelegationTerminal,
    ErrDelegationAdmissionRefused and ErrDelegationCommitRefused are the named
    refusals of the ledger: each one names the case it refuses instead of
    answering a silent default.

var (
	// ErrShutdownRefused reports new input offered to an agent whose close has
	// begun. The close refuses new input first and admits nothing beside the
	// drain.
	ErrShutdownRefused = errors.New("agent: the close of this agent refuses new input")
	// ErrDrainUnsettled reports a close that ended with obligations still open.
	// An honest close reports this named unsettled state beside the reasons of
	// what stayed open instead of a clean end.
	ErrDrainUnsettled = errors.New("agent: the close did not reach real termination")
	// ErrProducerIdentityMissing reports a producer watched under no task
	// identity, so an unsettled report could never name the work it runs behind.
	ErrProducerIdentityMissing = errors.New("agent: the producer is watched under no task identity")
	// ErrProducerMissing reports a watch that names no producer.
	ErrProducerMissing = errors.New("agent: the watch names no producer")
)
    ErrShutdownRefused, ErrDrainUnsettled, ErrProducerIdentityMissing and
    ErrProducerMissing are the named refusals of the close: each one names the
    case it refuses instead of answering a silent default.

var ErrFactPipelineUnwired = errors.New("agent: no fact pipeline is attached to observe through")
    ErrFactPipelineUnwired reports a committed fact the loop could not observe
    because no pipeline face was attached: a gap by name, never a silently
    dropped observation.

var ErrHandoffConstraintsMissing = errors.New("agent: the handoff envelope names no acceptance constraint")
    ErrHandoffConstraintsMissing reports an envelope that carries no non-empty
    acceptance constraint, so no handed-back result could be accepted or refused
    against it.

var ErrHandoffIntentMissing = errors.New("agent: the handoff envelope names no intent")
    ErrHandoffIntentMissing reports an envelope that names no intent, so no
    delegated turn could tell which task it serves.

var ErrHandoffPartNotData = errors.New("agent: the part is not a data part")
    ErrHandoffPartNotData reports a part that is not an A2A data part, so no
    envelope payload sits in it.

var ErrHandoffPartPayload = errors.New("agent: the data part carries no handoff envelope")
    ErrHandoffPartPayload reports a data part whose payload does not decode as a
    handoff envelope.

var ErrProjectionTypeUnmapped = errors.New("agent: the projection type maps to no message role")
    ErrProjectionTypeUnmapped reports a projection entry whose frozen type the
    role table does not declare: the table routes the declared types onto roles
    deterministically — every declared type names exactly one role, and several
    types may share one role — so a type outside that table is refused by name
    instead of rendering under a role no declaration gave it.

var ErrRequestChainSessionRequired = errors.New("agent: projected request assembly needs a named session chain")
    ErrRequestChainSessionRequired reports a request assembly under a chain that
    names no session: assembling one would join every user of the partition into
    one transcript.

FUNCTIONS

func NoticeFactKey(invocationID event.InvocationID, notice task.Notice) event.EventID
    NoticeFactKey reports the canonical identity one notification commits under:
    the identity prefix of its range beside the notification's own stable
    four-tuple. A retried delivery of one notification keeps that identity,
    so the chain appends the notice once.

func NoticeFactPrefix(invocationID event.InvocationID) event.EventID
    NoticeFactPrefix reports the identity prefix every notice fact of one range
    carries: the range marker of the fact family, spelled as hex so the identity
    stays a canonical event key. A read face selects the notifications of that
    range alone through it.

func RoleOf(eventType string) (model.Role, error)
    RoleOf reports the native message role one presented projection entry
    renders as on the replay path, keeping the transcript shape the event loop
    speaks. The table is the whole of the routing and it is deterministic:
    every declared entry type names exactly one role, several types may
    name the same role, and a type the table does not declare is refused by
    ErrProjectionTypeUnmapped rather than rendered under a fallback role.
    The table routes what one consumption reads and binds the reading side only:
    the core logic of a consumption may mint several events from the one
    projection it read — the calls it admits beside the answer it gives — and
    those products owe no traceability, because a new fact is its own source.

    The settled result and the replay products each render under the one role
    their entry type declares: - a replayed settled fact is history replayed
    as it stood, so it renders under the tool role its result fact answered
    a call with - the live settled observation a dispatch feeds the next
    decision is input of the range, and it renders under its call identity
    (see observationOf) - the recovery hint, the folded segment and the history
    summary render as user input material, the history a range reads

TYPES

type Agent interface {
	// Decide runs one model decision for one invocation: it takes the session's
	// decision slot, borrows the binding generation effective at that moment, and
	// reduces the model stream to its single terminal. A session grants at most
	// one effective model decision at a time, so input admitted while a decision
	// is in flight waits for the next round instead of starting a second one.
	Decide(ctx context.Context, inv Invocation) (Decision, error)

	// Delegate admits one sub-agent call as an asynchronous task and returns the
	// admission receipt of that call. The receipt states admission rather than a
	// result: the call settles only after its own obligations are cleared, and
	// its progress and terminal arrive as notifications of the admitted task.
	Delegate(ctx context.Context, req Delegation) (task.AdmissionReceipt, error)

	// Close refuses new input, settles the obligations that were already
	// accepted, and reports whether the drain reached real termination.
	Close(ctx context.Context, req CloseRequest) (DrainReport, error)
}
    Agent is one agent's execution face: the single model decision entry that
    drives the ready events of an invocation, together with the sub-delegation
    and close faces of the same execution.

    The decision entry takes the binding generation effective when the decision
    starts executing, so a publication during a decision never changes the
    binding it runs with, and input that is still queued is not bound in
    advance. The redline list (4 rules): MUST take the binding generation
    effective at the start of the decision and keep it for what it sets in
    motion. MUST reduce the model stream to its single terminal through the one
    reduction entry. MUST NOT run a second tool-iteration loop beside the event
    loop, and MUST NOT judge a terminal on stream mechanics. MUST NOT report a
    receipt as a result: an admitted call settles only after its own obligations
    clear.

type BoundaryView struct {
	// Messages are the prefix messages of the request, in fixed layout order.
	Messages []model.Message
	// Environment is the dynamic environment block the request closes with,
	// rendered under its declared markers: the faces the request carries beside
	// the open asynchronous work at this boundary. The loop appends it as the
	// last message of the request, behind the transcript. An empty block appends
	// nothing, so an agent declaring none keeps the request at its transcript.
	Environment string
	// PromptVersion is the content version the request boundary fixed.
	PromptVersion string
	// VisibleSkills lists the skill names the boundary material makes visible to
	// the request, in name order and once each: the skill bodies it fixed into
	// the prefix beside the bodies the binding already loaded. A boundary that
	// names no skill reports none, and the names carry no version of their own.
	VisibleSkills []string
}
    BoundaryView is what the request boundary settled on: the prefix messages
    the request carries in front of the transcript, in the fixed layout the
    request face assembles, the dynamic environment block the request closes
    with, the content version the request runs on, and the skill names that
    material makes visible. An empty PromptVersion keeps the version the binding
    already carries: a binding that names no files fixes no content version of
    its own.

type Capture interface {
	// Snapshot fixes the view one decision's request runs on before the request
	// starts: the model name, the prompt version, the tools visible to it and the
	// skills visible to it, beside the request as it stands. The request is
	// observed, never edited.
	Snapshot(ctx context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools, visibleSkills []string, req model.Request) error
	// Record admits one observed attempt beside the response that produced it:
	// the outcome arrives reduced, and the record side shares that terminal
	// rather than deriving a second one.
	Record(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error
	// Seal closes the turn's capture and reports the manifest a real writer
	// confirmed; a seal that found records in flight is a named failure, never a
	// completed capture.
	Seal(ctx context.Context) error
}
    Capture is the narrow trajectory capture face this loop fixes and records
    through: the request view is fixed before the request starts, every observed
    attempt is admitted under the capture's own bound, and the turn seals what
    it held when it ends. The observations cross as the loop observed them
    — the decision identity, the request view material, the reduced outcome
    — and never as a record of another package's shape, so the capture side
    attaches through this face with no package edge into the execution core.
    The attempt admission is the loop's only record-producing point: the episode
    identity travels on the ready input, the decision's model view material is
    what Snapshot fixed before the request started, and the stored input events
    are the chain the ledger keeps, so no second record kind is minted here to
    repeat what the run holds.

    The redline list (4 rules): MUST fix the request view before the request
    starts and never rebuild it from the options of a finished call. MUST admit
    every observed attempt beside the response that produced it and report a
    refused admission as a counted loss. MUST seal the turn once its productions
    stopped and report the seal failure as a counted loss. MUST NOT let a
    capture failure reach the model decision, and MUST NOT make admission wait
    on a writer.

type CloseRequest struct {
	// Address is the address being closed; new input to it is refused first.
	Address event.Address
	// Deadline is the instant after which the close reports the obligations it
	// could not settle instead of waiting longer; a zero deadline waits without a
	// deadline.
	Deadline time.Time
}
    CloseRequest is one close of an agent: the address being closed and the
    deadline the close may spend before it reports what is still open.

    A close refuses new input first and settles the obligations already
    accepted, and it never reports a drain while a producer has not stopped:
    a producer that answers that it has not stopped keeps the drain open until
    real termination is confirmed or the deadline arrives.

type Decision struct {
	// DecisionID identifies this decision.
	DecisionID event.DecisionID
	// InvocationID is the range this decision belongs to.
	InvocationID event.InvocationID
	// Binding is the binding generation borrowed from state when the decision
	// started executing; a publication during the decision does not change it.
	Binding state.AgentBinding
	// Request is the native request assembled from the ready input of the range.
	Request model.Request
	// Terminal is the single terminal the model stream of this decision reduced
	// to; exactly one terminal is recorded per decision.
	Terminal model.Kind
}
    Decision is one model decision of an invocation: the generation of the state
    binding it borrowed, the native request assembled from the ready input of
    the range, and the single terminal its stream reduced to.

    Admission is not settlement: a call this decision admitted keeps its
    obligation until that obligation is explicitly cleared. A response that
    arrives after the terminal is kept as diagnostics only, so it dispatches no
    tool and never revives the terminal.

type Delegation struct {
	// InvocationID is the invocation that issued the delegation.
	InvocationID event.InvocationID
	// DecisionID is the decision that issued the delegation; it keeps the native
	// call identifier scoped to the call that produced it, because a native call
	// identifier is not globally unique.
	DecisionID event.DecisionID
	// CallID is the native provider call identifier of the delegated call, kept
	// byte-identical and never regenerated.
	CallID string
	// TaskID is the task the delegated call was admitted as, so its progress and
	// terminal associate with the call that issued it.
	TaskID event.TaskID
	// Target is the agent session address the delegated turn runs in.
	Target event.Address
	// TriggerSource is the trigger-source stamp the admission record carries.
	// The call_subagent tool stamps event.LineageDelegation, so a delegated call
	// is recorded as traffic the agent manages for itself; an empty stamp
	// records no source.
	TriggerSource string
	// Ref is the initiating binding reference the delegated request carries: the
	// delegated turn binds the target the application selected at the generation
	// its initiator took, and never the generation effective at its own start.
	Ref state.BindingRef
	// Status is the phase the admitted task reached; a terminal status is
	// reported only after the obligations of the delegated call are cleared.
	Status task.Status
	// Progress lists the notifications the admitted task produced, each with its
	// stable identity, so a repeated delivery of one notification records no
	// second fact.
	Progress []task.Notice
}

    Delegation is the record of one sub-agent call the agent admitted: the
    invocation and native call it is associated with, the address the delegated
    turn runs in, and the progress and terminal the admitted task reports under.

    The call is admitted as a task, so the caller is answered by a receipt and
    this record carries the later progress and terminal of that same task.
    The delegated admission keys its own suffix beside the call identity:
    the tool-call task the wrapper admits for the same native call is a distinct
    admission, and sharing the key would dedup the delegation into the wrapper's
    receipt. The delegated turn runs in the session the application selected
    explicitly, and a delegation never creates a private writable history of its
    own.

type DelegationTasks interface {
	// Admit admits one delegated call as a task. A repeated admission of the same
	// dedup key reports the task of the first admission as a dedup and adds no
	// obligation.
	Admit(ctx context.Context, spec task.Spec) (task.AdmissionReceipt, error)
	// Advance moves one task to the next status and submits the change beside its
	// pending notifications to the commit carrier first.
	Advance(ctx context.Context, taskID event.TaskID, to task.Status, mutation task.MutationID, outbox []task.Envelope) (task.CommitResult, error)
	// Obligations reports the obligations the task still owes, each classified
	// by the wait it is in.
	Obligations(ctx context.Context, taskID event.TaskID) (task.Obligations, error)
	// Flush delivers the notifications of one task whose obligations are still
	// open. Only an acceptance the target states explicitly clears one.
	Flush(ctx context.Context, taskID event.TaskID) error
}
    DelegationTasks is the task face the ledger settles its delegations through:
    the admission of one delegated call, the transition that carries its
    notifications beside the settle, the obligations that still owe a settle or
    a delivery, and the flush that delivers one task's notifications.

    The redline list (4 rules): MUST report the obligation a fresh admission
    added, and keep it until real settlement. MUST commit a transition beside
    its pending notifications before the settle is reported. MUST NOT count a
    dedup or a refused admission as an obligation, nor withdraw one already
    booked. MUST NOT treat an offered notification as delivered before the
    target accepts it.

type Delegator struct {
	// Has unexported fields.
}
    Delegator is one agent's sub-agent delegation ledger. It owns the call
    association, the progress and the terminal bookkeeping of every delegated
    call; the delegated turn itself runs on the borrowing face it hands out.

    The ledger never holds its own lock while it calls the task face or the
    state owner, and the Observe entry touches neither: a delivery runs while
    the manager holds its registry, so an observation may only record the fact.

func NewDelegator(tasks DelegationTasks, holder state.Borrow, instance event.InstanceID) *Delegator
    NewDelegator returns an empty delegation ledger over the task face and the
    state owner's borrowing face, scoped to one runtime instance.

func (d *Delegator) Complete(ctx context.Context, taskID event.TaskID, status task.Status, fact event.EventID) (task.Notice, error)
    Complete settles one admitted delegation with the terminal status and the
    fact identity the outcome points at, and returns the notification the
    settle carries. The settle is committed beside that notification first;
    the terminal is reported only once the task's obligations are cleared,
    so an obligation still open keeps the delegation where it is and a later
    Reconcile lands it.

func (d *Delegator) CompleteWithResult(ctx context.Context, taskID event.TaskID, status task.Status, fact event.EventID, text string) (task.Notice, error)
    CompleteWithResult settles one admitted delegation exactly as Complete does,
    and carries the result body the settled call produced into the notification.
    The body is what a consumer of a task nobody drives reads: the turn that
    admitted the delegation has already ended, so the notification is the only
    material left. The body is bounded, and it stays out of the notification
    identity, so a retry of the same settle keeps its fact key.

func (d *Delegator) Delegate(ctx context.Context, req Delegation) (task.AdmissionReceipt, error)
    Delegate admits one delegated call as a task and returns its admission
    receipt. The call identity and the application-selected target are required;
    the ledger neither invents a session nor accepts an incomplete target.

    The delivery reconciliation barrier of the call is booked before the call
    is spawned, and the initiating generation is pinned beside it: a fresh
    admission keeps both until its terminal lands, while a dedup or a refused
    admission withdraws only this admission's booking and pin — the obligations
    that already exist are untouched. - The pin binds the application-selected
    target at the generation the initiating reference names, taken before the
    spawn: a publication landing mid-call never retargets the turn. - A dedup
    added no obligation: only this admission's pre-registration is withdrawn,
    and the obligations of the first admission stay untouched.

func (d *Delegator) Observe(_ context.Context, notice task.Notice) error
    Observe records one notification of an admitted delegation into the progress
    of the call it belongs to. A repeated delivery under the same stable
    identity records no second fact, and a terminal notification waits for the
    task's obligations to clear before it is reported.

    Observe touches no face but this ledger's own state: it may run while the
    manager holds its registry during a delivery.

func (d *Delegator) OwnerOf(taskID event.TaskID) (event.InvocationID, bool)
    OwnerOf reports the invocation that issued the delegation a task was
    admitted as, so a notification of a multi-level task always finds the owner
    that issued its call and never a neighbour.

func (d *Delegator) Pending() int
    Pending reports how many admitted delegations this ledger still books: each
    fresh admission books one before the spawn and keeps it until its terminal
    lands, while a dedup or a refused admission books none.

func (d *Delegator) Reconcile(ctx context.Context, taskID event.TaskID) (task.Status, error)
    Reconcile attempts the landing of one delegation again and reports the
    status the record stands at afterwards: the entry an earlier settle left
    pending lands here once every obligation has cleared.

func (d *Delegator) TurnBorrow(taskID event.TaskID) (state.Borrow, error)
    TurnBorrow returns the borrowing face the delegated turn of one admitted
    delegation runs under. Every borrow it opens resolves on the generation
    the initiating reference names — the application-selected target at that
    generation — and a derived borrow of a nested delegation is forced onto the
    same generation, so a chain of delegations never drifts onto the latest one.

func (d *Delegator) View(owner event.InvocationID) []Delegation
    View reports the delegations one invocation issued, in the order their
    tasks were admitted, each as a copy the caller may read freely: the call
    association, the progress and the status the record stands at.

type DrainReport struct {
	// Drained reports whether every accepted obligation reached real termination.
	// It stays false while any producer has not confirmed its stop.
	Drained bool
	// Unsettled lists the obligations that were still open when the close
	// reported, each with the reason it could not be settled.
	Unsettled []UnsettledObligation
}
    DrainReport is the outcome of one close: whether every accepted obligation
    reached real termination, and the obligations that were still open when the
    close reported.

type FactCommit func(evt event.Event) (event.EventID, error)
    FactCommit is the one commit face of the facts the loop produces: it stores
    the whole event as the ledger of the run keeps it and reports the identity
    that fact committed under, minting one for an event that arrives without
    an identity. The identity it answers is the one the After pass reads back,
    so an observation never reconstructs the fact it observes. A commit that
    did not land reports no identity beside the named failure, so no caller ever
    answers a commitment over a fact the ledger refused.

    The redline list (3 rules): MUST store the whole event as it stands, minting
    an identity only for an event that carries none. MUST report the identity
    that fact committed under, so the observation reads that very fact back.
    MUST report a refused commit beside no identity, so the caller decides what
    a fact that never landed means.

type FactPipelines func(agentID event.AgentID) (plugin.Pipeline, error)
    FactPipelines assembles the ordered plugin pipeline of one agent,
    so the After pass of a fact the loop commits outside a dispatch — the ready
    input of a range, the output of a drive, the settled delivery of a call —
    runs through the very faces the published generation of that agent carries.
    A refused assembly stays a named gap beside the committed fact: observation
    is a bypass, never part of the commit.

    The redline list (2 rules): MUST assemble the faces of the published
    generation of the named agent, in its declared order. MUST NOT run a second
    observation beside the pipeline it answers.

type FactResolver func(id event.EventID) (model.Message, bool)
    FactResolver reports the native tool message one recorded result fact
    carries. The fact identity comes from the settled notification of a call,
    so the loop pairs the tool message with the call it answers.

type FinalRoundPolicy struct {
	// MaxRounds bounds the tool rounds one drive may run; 0 removes the bound.
	MaxRounds int
	// Budget bounds the time one drive may spend; 0 removes the bound.
	Budget time.Duration
	// Timeout bounds the closing round itself; 0 takes defaultFinalRoundTimeout.
	Timeout time.Duration
	// Prompt is the closing instruction handed to the model; "" takes
	// defaultFinalRoundPrompt.
	Prompt string
}
    FinalRoundPolicy is the watchdog budget of one drive: how many tool rounds
    the drive may run and how long it may spend before its next decision becomes
    the closing round, the instruction that round carries and the short timeout
    it runs under.

    The zero policy keeps every drive of the loop unbounded, exactly as a drive
    runs with no watchdog attached: a loop that attaches no policy never meets
    a closing round. The closing round is the round the model delivers in —
    it runs once the budget is spent, it is marked as the closing round on
    the request boundary of its decision, and the drive ends when it returns.
    The redline list (4 rules): MUST keep every drive unbounded while both
    budgets are zero. MUST read the budget at a round boundary and never cut a
    decision already in flight. MUST run one closing round at most, and end the
    drive when it returns. MUST NOT admit a tool call the closing round issues:
    delivering opens no new obligation.

type FinalRoundReason string
    FinalRoundReason names why the closing round of a drive ran, and stands
    empty on a drive that ended on its own terminal.

const (
	// FinalRoundNone reports a round that is not the closing round of its drive.
	FinalRoundNone FinalRoundReason = ""
	// FinalRoundRounds reports a closing round the spent round budget owed.
	FinalRoundRounds FinalRoundReason = "rounds"
	// FinalRoundBudget reports a closing round the spent time budget owed.
	FinalRoundBudget FinalRoundReason = "budget"
)
    FinalRoundNone, FinalRoundRounds and FinalRoundBudget name the reasons a
    closing round runs under.

type HandoffEnvelope struct {
	// Intent is the task the delegated turn exists to serve.
	Intent string `json:"intent"`
	// Constraints lists the acceptance criteria the handed-back result must satisfy.
	Constraints []string `json:"constraints,omitempty"`
	// Grants lists the capabilities the handoff grants the delegated turn, each
	// named with the kind it belongs to (tool, skill or mcp).
	Grants []string `json:"grants,omitempty"`
	// PendingDecisions lists the decisions the delegated turn may not take on its own.
	PendingDecisions []string `json:"pending_decisions,omitempty"`
	// Refs names the stored facts the handed-off context starts from.
	Refs []event.EventID `json:"refs,omitempty"`
}
    HandoffEnvelope is one cross-agent handoff in structured form: the task the
    delegated turn serves, the acceptance constraints its result is held to, the
    capabilities the handoff grants it, the decisions it may not take on its own
    and the stored facts it starts from. It travels beside the natural-language
    request of the call and never replaces it, and one schema serves every
    carrier: the JSON field names below are the wire names it travels under,
    an A2A data part carries that JSON as its payload, and the local channel
    renders the same fields as the text block ExternalContext states.

    The four segments of the handoff contract map onto this record: - the
    task is Intent; - the context summary is Refs beside the natural-language
    request; - the deliverable stays the natural-language request's own outcome;
    - the acceptance criteria are Constraints.

func FromDataPart(part a2aprotocol.Part) (HandoffEnvelope, error)
    FromDataPart reports the handoff envelope one A2A data part carries.
    A payload already decoded into a map, an envelope carried in process and
    JSON kept as bytes are each read. A part of another kind, or a payload that
    does not decode, is refused by name.

    The envelope is decoded, not accepted: the caller holds it to Validate
    against the acceptance the receiving side owes, so a decoded envelope naming
    no intent is still refused there.

func (h HandoffEnvelope) DataPart() a2aprotocol.DataPart
    DataPart returns the A2A data part one handoff travels as on the remote
    channel: the envelope itself as the structured payload, under the field
    names the record declares, so the receiver decodes the same schema the local
    channel renders as text.

func (h HandoffEnvelope) ExternalContext() string
    ExternalContext renders the envelope as the structured block one delegated
    turn carries in front of its natural-language request: the intent,
    the acceptance constraints, the grants, the pending decisions and the refs,
    each under its own heading in that fixed order. A list with no entry keeps
    its heading and states that it carries none, so the shape of the block never
    depends on its content.

func (h HandoffEnvelope) Validate() error
    Validate reports whether the envelope names what a delegated turn could
    serve and be accepted against: a non-empty intent and at least one non-empty
    constraint.

type HistoryFace interface {
	// SessionHistory reports the messages the named session accumulated across
	// the drives that already ended on it.
	SessionHistory(addr event.Address) []model.Message
	// CommitHistory appends the transcript one ended drive accumulated to the
	// history of the named session.
	CommitHistory(addr event.Address, msgs []model.Message)
	// SessionFacts reports the facts the named session committed, in commit
	// order: the chain a drive reads the input that landed in its own range
	// from at a decision boundary rather than at the start of the drive alone.
	// The signature is the one the run's own chain read carries, so the drive
	// reads the very ledger it commits through instead of a lane beside it.
	SessionFacts(addr event.Address) []event.Event
}
    HistoryFace is the session transcript face the loop reads the accumulated
    history of a session from and writes one ended drive's transcript back to:
    the invocations of one session share the history the drives that already
    ended on it committed, while each drive aggregates only its own ready
    input beside that history and never batches with a neighbour. A session
    whose transcript a mapped sequence carries needs no second face here:
    that sequence is the one history of the session, so an implementation may
    answer none of its own beside it — and the facts of that chain are what the
    drive reads its range's input again from at every decision boundary.

    The redline list (4 rules): MUST report the history the named session
    accumulated, as a copy the caller owns. MUST report the facts of the named
    session in commit order, as a copy the caller owns. MUST NOT batch the ready
    input of one invocation into another invocation's range. MUST NOT rewrite or
    drop the history a committed drive left behind.

type Invocation struct {
	// InvocationID identifies the responsibility range.
	InvocationID event.InvocationID
	// Address is the explicit agent session address the range belongs to; it is
	// never defaulted to a most-recent session.
	Address event.Address
	// Instance is the runtime instance the range was admitted on; rebuilding an
	// agent under the same name yields a different instance.
	Instance event.InstanceID
	// Ready lists the input aggregated for the next decision of this range, in
	// the order the session admitted it.
	Ready []event.Event
}

    Invocation is the scheduling record of one responsibility range: the
    identity and address of the range and the input that is ready for its next
    decision.

    The record is the agent's own view of the range: the session site it belongs
    to stays the state owner's ledger, and the terminal of each decision is
    reported on the turn beside it. One invocation may hold several decisions,
    because an observation that returns from a background task makes new input
    ready and drives the next decision of the same range.

type Loop struct {
	// Has unexported fields.
}
    Loop drives the persistent event loop of one invocation: borrow the binding
    generation effective at execution start, aggregate the range's own input,
    assemble one native model request, reduce that request through the single
    terminal entry point, admit every tool call through the task wrapper,
    and let the settled notification backflow drive the next decision of
    the same invocation. One invocation runs its own decisions over its own
    transcript, so two invocations of one session are never batched together:
    each aggregates only its own ready input and each keeps its own decisions
    independent of the other's terminal. - This loop is the runtime's only
    tool-call loop: no framework Runner or LLMAgent drives a second loop
    beside it. The terminal of a decision is read from the reduction outcome
    and never re-derived here: this file does not classify the six states
    and does not judge the terminal on stream mechanics. - A decision
    borrows the binding generation effective when it starts executing and
    keeps that handle until the work it set in motion has ended, while a
    publication during the decision never changes the generation it runs with.
    - A settled task notification backflows to the invocation that issued
    the call and drives that invocation's next decision, which borrows the
    generation effective at its own start — never one chosen in advance.
    - The watchdog budget of a drive is read at the boundary between its rounds:
    the round that boundary finds the budget spent is the closing round, and the
    drive ends when that round returns. The budget is the one the drive's own

func NewLoop(borrow state.Borrow, slots *state.Occupancy, instance event.InstanceID, facts FactResolver) *Loop
    NewLoop returns the event loop over the state owner's borrowing face,
    the session decision-slot ledger and the fact resolver that turns a recorded
    result fact into the native tool message it carries.

func (l *Loop) Backflow(invocationID event.InvocationID, notice task.Notice)
    Backflow admits one notification of a call into the range of the invocation
    that issued that call, so it drives that range's next decision and no other.
    A progress notification commits as a notice fact of that range and nothing
    more: the chain is the truth of the report, and a report of the pipeline's
    own rounds is never material a decision reads. A non-terminal notification
    that is not progress carries no result to pair and is ignored here.
    The settled delivery commits as the delivery fact of the range before it is
    handed on, so the delivery boundary of a call is observed as the fact it is;
    the After pass of that fact runs beside the notification rather than under
    the cancellation of the caller that delivered it, because observation is a
    bypass.

func (l *Loop) CommitFactFor(ctx context.Context, agentID event.AgentID, evt event.Event) (event.EventID, error)
    CommitFactFor commits one fact on behalf of the named agent and runs the
    After pass of that fact, and reports the identity the fact committed under.
    It is the same commit the loop's own production points use, exposed so a
    producer outside a dispatch — the configuration source judging a model
    replacement — lands its fact in the one ledger and is observed through the
    faces of that agent's published generation exactly as the loop's own facts
    are. An empty identity beside no error reports a loop beside no commit face,
    and a refused commit reports the failure beside no identity.

func (l *Loop) Drive(ctx context.Context, inv Invocation) (turn Turn, err error)
    Drive runs one invocation until its latest decision reaches a terminal that
    dispatches no tool: every decision aggregates the transcript the range has
    accumulated beside the session history the drives that already ended on this
    session committed, and the receipts and settled observations of the tools
    one decision admitted enter that transcript before the next decision begins.
    The ready input of the range is never mixed with the input of another
    invocation. - The episode record is written where the business process
    actually starts: at the first drive of the invocation's own ready input.
    - The settled result facts one decision paired are the stored input the
    next decision reduces. - The drive reads its range's chain again at every
    decision boundary: input that landed in the range while a decision ran
    enters the next decision of that same range, and a decision that opened no
    obligation still ends the round while that input stands unread. The read
    starts behind the head the chain held when the drive began, so the material
    an earlier range already took is never this range's input. - The transcript
    of this drive is committed beside the terminal answer of its last decision,
    so the next invocation of this session reads the range's history as it
    actually ended. - The whole drive is watched as one in-flight production
    beside its invocation identity — the queueing waits included — and confirms
    its stop only when the drive really ended. - A progress notification of the
    range's own call commits as a fact of its chain and enters no round head:
    the report is audit and projection material, never input a decision speaks
    from. - A terminal notification of the range's own call enters the round
    head as input material: a settle, a failure or a cancellation is what an
    unattended follow-up turn speaks from, and the progress reports beside it
    are skipped by their own category. - A drive whose budget is spent runs
    one closing round and ends there: the closing round delivers what the
    range already produced, its transcript is committed the same way, and the
    obligation it opens is none. - The drive is observed as one consumer run:
    its start and its end are DEBUG lines carrying identities, counts and the
    terminal, never transcript content.

func (l *Loop) RecordingLosses() []RecordingLoss
    RecordingLosses reports the training records the production points could
    not write, in arrival order, each beside its record identity and the named
    failure that stopped the write. The account is the explicit gap ledger of
    the run: nothing the recorder refused is silently forgotten here.

func (l *Loop) SetAgentFinalRound(agent event.AgentID, policy FinalRoundPolicy)
    SetAgentFinalRound attaches the watchdog budget the drives of one agent run
    under: a drive whose address names this agent reads the policy set here,
    and every other drive reads the loop's default. The budget is per agent,
    so two agents of one declaration close their own drives on their own bounds.

func (l *Loop) SetAgentFinalRounds(policies map[event.AgentID]FinalRoundPolicy)
    SetAgentFinalRounds replaces the per-agent budgets of this loop with the
    table given: an agent the table does not name reads the loop's default,
    so a publication that drops a budget leaves no earlier entry behind.
    A nil or empty table clears every per-agent entry.

func (l *Loop) SetCapture(capture Capture)
    SetCapture attaches the trajectory capture face this loop fixes each
    decision's request view through, admits each observed attempt through and
    seals the turn through. A nil face captures nothing. A refused fix, record
    or seal is a counted recording loss and never reaches the model decision,
    and the setter is separate from the constructor so the declared construction
    shape of the loop stays as it is.

func (l *Loop) SetFactCommit(commit FactCommit)
    SetFactCommit attaches the commit face the loop commits the facts it
    produces through — the ready input of a range, the output of a drive and the
    settled delivery of a call — so every one of them lands in the one ledger
    of the run and is observed back as it committed. A nil face keeps the loop
    committing nothing of its own. The setter is separate from the constructor
    so the declared construction shape of the loop stays as it is.

func (l *Loop) SetFactPipelines(pipelines FactPipelines)
    SetFactPipelines attaches the pipeline face the After pass of a fact
    committed outside a dispatch is assembled through, so that pass runs over
    the faces of the published generation rather than a pipeline of the loop's
    own. A nil face beside a commit face is a wiring gap reported on the first
    committed fact. The setter is separate from the constructor so the declared
    construction shape of the loop stays as it is.

func (l *Loop) SetFinalRound(policy FinalRoundPolicy)
    SetFinalRound attaches the watchdog budget the drives of this loop run under
    while their agent declares none of its own. A zero policy keeps every drive
    unbounded. The setter is separate from the constructor so the declared
    construction shape of the loop stays as it is.

func (l *Loop) SetHistory(face HistoryFace)
    SetHistory attaches the session transcript face this loop reads the shared
    session history from and commits one ended drive's transcript back to.
    The setter is separate from the constructor so the declared construction
    shape of the loop stays as it is.

func (l *Loop) SetNoticeFeed(feed NoticeFeed)
    SetNoticeFeed attaches the incremental read face the drive reads the
    progress notifications of its range through: the notices are facts of
    the session chain, so the read is a projection increment seeded at the
    ended-material boundary. A nil face keeps the read at the cache the loop
    keeps beside each commit. The setter is separate from the constructor so the
    declared construction shape of the loop stays as it is.

func (l *Loop) SetPresentations(sink PresentationSink)
    SetPresentations attaches the presentation sink this loop hands each After
    pass outcome to. The After pass itself runs either way — exactly once per
    committed fact and only after that fact's canonical commit — and a nil sink
    drops the presentation only, never the fact it was rendered from. The setter
    is separate from the constructor so the declared construction shape of the
    loop stays as it is.

func (l *Loop) SetRequestFace(face RequestFace)
    SetRequestFace attaches the request-boundary face this loop assembles each
    decision's request prefix through: the fixed file contents, the loaded
    skill bodies and the replayed projection of the session chain. A nil face
    keeps every request at its transcript alone. The setter is separate from the
    constructor so the declared construction shape of the loop stays as it is.

func (l *Loop) SetWatch(watch func(taskID event.TaskID, stopped func() bool) error)
    SetWatch attaches the registration the close of this agent confirms the
    real stop of every in-flight production through: the whole drive of one
    invocation and the model stream of each of its decisions, each registered
    beside the work identity it runs behind, with the stop confirmation that
    work is polled through. A nil registration keeps the loop registering
    nothing. The setter is separate from the constructor so the declared
    construction shape of the loop stays as it is.

    The redline list (2 rules): MUST register a production when it really starts
    and confirm its stop only when it really ended. MUST NOT read a cancelled
    request, an emptied queue or an elapsed timeout as a stop.

type NoticeFeed interface {
	// NoticeBoundary reports the position the ended material of the named chain
	// ends at, resolved from the fact identity the boundary records, and whether
	// that chain carries a projection at all.
	NoticeBoundary(chain RequestChain) (int, bool)
	// NoticesFrom reports the notification facts of the named chain from cursor
	// on under the given identity prefix, in chain order, beside the end the read
	// reached and whether the chain carries a projection. The cursor is resolved
	// against the sequence as it stands at the read; it is never a position the
	// caller may keep across a fold rewrite.
	NoticesFrom(chain RequestChain, prefix event.EventID, cursor int) ([]NoticeView, int, bool)
}
    NoticeFeed is the incremental read face of a session chain's notification
    facts: the boundary the ended material ends at and the notices the chain
    carries from that boundary on. A notification is a fact of the chain,
    so a drive reads the notices of its range as a projection increment rather
    than from a lane of its own; a chain beside no projection says so and the
    loop reads the cache it keeps beside the commit instead. The face crosses
    built-in and event types only, so the composition root attaches it with no
    package edge into the projection layer.

    - The boundary is re-resolved by identity each drive, not a kept position:
    a fold rewrite moves the sequence; remembered identities report each notice
    once.

    The redline list (3 rules): MUST report the ended-material boundary and the
    notices the chain carries from it on, in chain order. MUST report a chain
    that carries no projection as carrying none, never as an empty projection.
    MUST NOT answer an entry outside the identity prefix it was handed.

type NoticeView struct {
	// Key is the canonical identity the notification fact committed under.
	Key event.EventID
	// Text is the input material the notification reads as.
	Text string
}
    NoticeView is one notification fact as the session chain's projection
    carries it: the canonical identity the fact committed under beside the input
    material it reads as.

type PresentationSink func(presentation view.Presentation)
    PresentationSink consumes the After pass presentation of one committed fact:
    the views its presenters rendered beside the visible gaps its failed
    observers left. The sink is the read-out face the presentation is handed to
    — the inspection face of the run — while the loop keeps the committed fact
    itself and never retracts it for anything this presentation carries.

type Reconciler interface {
	// Reconcile attempts the landing of one admitted call's terminal again and
	// reports the status the record stands at afterwards.
	Reconcile(ctx context.Context, taskID event.TaskID) (task.Status, error)
	// Pending reports how many fresh admissions the ledger still books; each
	// booking closes only when its terminal really lands.
	Pending() int
}
    Reconciler is the delegation ledger the close lands the terminals of
    admitted calls through: a terminal whose obligations have cleared is
    reported and the booking beside it closes, while an obligation still open
    keeps the terminal withheld exactly as it does beside a live turn.

    The redline list (3 rules): MUST land a pending terminal only once the
    obligations of that call are cleared. MUST report every booking it still
    holds, so a close never reports a drain beside an unlanded terminal. MUST
    NOT hold a terminal past the obligations it waits on, and MUST NOT report
    one before them.

type RecordingLoss struct {
	// Kind names the record kind the failed write belonged to: "episode",
	// "decision" or "attempt".
	Kind string
	// ID is the identity of the record the failed write belonged to.
	ID string
	// Err is the named failure that stopped the write.
	Err error
}
    RecordingLoss is one training record a production point could not write:
    the record kind and the identity it belonged to beside the named failure
    that stopped the write. A loss is counted here — never silently dropped
    — and never carried into the model decision: recording is observation,
    not execution.

type RequestBoundary struct {
	// Agent is the agent whose request is being assembled.
	Agent event.AgentID
	// Chain is the causal chain the projected replay belongs to.
	Chain RequestChain
	// Binding is the borrowed binding generation the request runs on.
	Binding state.AgentBinding
	// FinalRound marks the request of a drive's closing round: the round the
	// model delivers in after its budget is spent, whose tool calls are admitted
	// nowhere. It stands false for every ordinary request.
	FinalRound bool
}
    RequestBoundary is the material one request boundary assembles its prefix
    from: the agent whose request it is, the causal chain the replay is read
    under, the borrowed binding the fixed file contents are read for and whether
    the request is the closing round of its drive.

type RequestChain struct {
	// PartitionID is the partition the chain's facts are filed under.
	PartitionID int
	// SessionID is the session the chain belongs to; empty is refused by name.
	SessionID event.SessionID
}
    RequestChain is the causal chain identity one assembled request belongs to:
    the partition and the session the projected facts were replayed under.
    It is part of the assembly boundary, so one user's session chain is never
    joined into another's by a shared partition.

type RequestFace func(ctx context.Context, boundary RequestBoundary) (BoundaryView, error)
    RequestFace assembles the request-boundary material of one decision: the
    fixed file contents read fresh at this boundary, the skill bodies already
    loaded on the binding beside them and the replayed projection of the named
    session chain. The prefix follows one fixed layout: the fixed file contents
    first — the prompt and skill bodies the binding names, in the structural
    order the binding carries — then the skill bodies already loaded on the
    binding's tool resources beside them (skill-name order, once each), then
    the replayed projection of the session chain (each presented entry under the
    frozen event-type→role table), then the transcript the drive accumulates
    behind the prefix. An agent declaring the environment block receives
    the block itself, and the loop appends it as the last message behind the
    transcript, so the dynamic material trails the request instead of entering
    the prefix. The boundary material enters the actual GenerateContent request
    at the loop, and no projection entry shape is mirrored into this package:
    the face crosses built-in, event, model and state types only, so the
    composition root attaches it with no package edge into the projection layer,
    the same stitching the recording face uses. A chain that names no session is
    refused by name through ErrRequestChainSessionRequired; a nil face keeps the
    request at its transcript alone. The redline list (3 rules): MUST assemble
    the prefix in the fixed layout from contents read fresh at this boundary.
    MUST refuse a chain that names no session by name. MUST NOT cross any
    package edge into the projection layer.

type Shutdown struct {
	// Has unexported fields.
}
    Shutdown is one agent's close face: the gate new work is admitted through,
    the ledger of the obligations that gate accepted, and the real stop
    confirmations of the producers behind that work.

    It fronts the task face the agent settles through as a DelegationTasks
    of its own, so the delegation ledger admits through the gate and a close
    refuses new input at the very point a new obligation would open. Everything
    already accepted keeps its obligation until real settlement: the close
    offers the notifications a task still owes beside their frozen routing,
    lands the terminals whose obligations cleared, and polls the producers until
    each one confirms its stop or the deadline arrives. The redline list (4
    rules): MUST refuse new input at the gate before the first settlement pass
    of the close. MUST settle an accepted obligation only by real settlement,
    and report a drain only beside real termination. MUST NOT read a cancelled
    request, an emptied queue or an elapsed timeout as a settled obligation.
    MUST NOT report a clean drain beside an obligation or a production that
    stayed open.

func NewShutdown(face DelegationTasks) *Shutdown
    NewShutdown returns the close face of one agent over the task face its work
    is admitted and settled through. The returned face is the one the agent's
    delegation ledger admits through, so the close can refuse new input at the
    gate where new obligations are opened and settle exactly what it accepted.

func (s *Shutdown) Admit(ctx context.Context, spec task.Spec) (task.AdmissionReceipt, error)
    Admit admits one new call as a task and returns its admission receipt, or
    refuses it by name once the close of this agent has begun. A fresh admission
    is recorded beside the close face so its obligation stays in the drain until
    real settlement; a dedup or a refusal added no obligation and settles none.

func (s *Shutdown) Advance(ctx context.Context, taskID event.TaskID, to task.Status, mutation task.MutationID, outbox []task.Envelope) (task.CommitResult, error)
    Advance moves one task to the next status beside its pending notifications.
    A transition of work already accepted is settlement, so the gate never
    refuses it.

func (s *Shutdown) Close(ctx context.Context, req CloseRequest) (DrainReport, error)
    Close refuses new input first, settles the obligations that were already
    accepted, and reports whether the drain reached real termination.

    The gate closes before the first settlement pass, so no new obligation opens
    beside the drain. An accepted obligation is settled only by real settlement
    — a notification its target explicitly accepted, a terminal that landed
    beside a cleared booking — and every watched producer must confirm its stop.
    A producer answering false has not stopped: the close keeps polling it
    until the confirmation arrives or the deadline does, and a deadline that
    arrives first returns the named unsettled state beside the reason of every
    obligation that stayed open. A second close returns the first result and
    runs no second settlement.

func (s *Shutdown) Flush(ctx context.Context, taskID event.TaskID) error
    Flush delivers the notifications of one task whose obligations are still
    open, beside the routing each envelope froze. Only an acceptance the target
    states explicitly clears one.

func (s *Shutdown) Obligations(ctx context.Context, taskID event.TaskID) (task.Obligations, error)
    Obligations reports the obligations the task face still holds for one task,
    each classified by the wait it is in.

func (s *Shutdown) SetReconciler(reconciler Reconciler)
    SetReconciler attaches the delegation ledger the close lands the terminals
    of admitted calls through. The setter is separate from the constructor
    because the ledger admits through this very face: the face exists first and
    the ledger is built on it.

func (s *Shutdown) Track(taskID event.TaskID)
    Track records one already admitted task beside the close face, so the drain
    settles the obligations of that task and names them beside the deadline when
    they stay open. A call admitted through the task face of the run is recorded
    here at its admission exactly as a call admitted through this gate is, and
    recording is no admission of its own: a task recorded twice is the one task.

func (s *Shutdown) Watch(taskID event.TaskID, stopped func() bool) error
    Watch registers one in-flight producer behind the named task's execution:
    its real stop is one of the confirmations the close waits for, and a
    producer that never confirms keeps the drain open beside the task it runs
    behind. Several producers may stand behind one task and each confirms its
    own stop.

    The stopped answer is polled and only a true is taken as an end: a producer
    answering false has not stopped, and neither a cancelled request nor an
    emptied queue stands in for the confirmation. The producer answers for
    itself alone, and the registration carries the identity of the work it runs
    behind. The redline list (4 rules): MUST poll the stop confirmation until
    it answers true or the deadline arrives. MUST refuse a registration that
    names no work identity or carries no stop confirmation, by name. MUST NOT
    take a cancelled request, an emptied queue or an elapsed timeout as a stop.
    MUST NOT report a drain while any registered production has not confirmed
    its stop.

type Turn struct {
	// InvocationID is the range the turn belongs to.
	InvocationID event.InvocationID
	// Decisions lists the model decisions the turn ran, in execution order.
	Decisions []Decision
	// Terminal is the terminal the latest decision reduced to, or KindUnknown
	// while no decision reached one.
	Terminal model.Kind
	// Failure is the raw cause the latest decision's reduction recorded when its
	// terminal is KindFailed, and it stands empty otherwise. It is the text a
	// host delivers when a failed turn produced no deliverable body; a reduction
	// that observed no cause leaves it empty rather than inventing one.
	Failure string
	// FinalRound names why the closing round of this drive ran: the round budget
	// or the time budget was spent, so the latest decision delivers instead of
	// continuing. It stands empty for a drive that ended on its own terminal.
	FinalRound FinalRoundReason
}
    Turn is the outcome one invocation's drive reports: the decisions it ran in
    order and the terminal its latest decision reduced to.

type UnsettledObligation struct {
	// TaskID is the task whose obligation stayed open.
	TaskID event.TaskID
	// Reason is the readable reason the obligation could not be settled.
	Reason string
}
    UnsettledObligation is one obligation a close could not settle, paired with
    the reason it stayed open.

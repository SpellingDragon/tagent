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

The resident consumption loop of one (agent, session) address: pull the events
that arrived → dispatch them by what they are → call the model once when they
drive → return to the pull. The loop never waits for a dispatched tool to
settle: each settled result arrives as the event it is and drives the next
decision of the segment it belongs to.

The producing half of the final round judgment: the watchdog budget of one
(agent, session) range is judged off the chain the range committed, and a spent
budget is filed as the final_round.request fact of that range. The fact states
which range, why and the inputs the judgment read; what the consumer runs from
it — the closing round — is the reading half, and this planner never decides
anything else.

The execution-side storm fuses of the loop: one model response that issues more
tool calls than the loop's declaration admits is parked as one approval batch
instead of being dispatched, and both fuses — the per-response call cap and
the long-context threshold — park through this one code point, so neither the
direct drive nor a consumption segment carries a storm the other path would have
stopped.

A parked batch is neither truncated nor degraded: it is registered as one
attention question of the approval channel a human answers, the wait happens
inside the dispatch that would have admitted it, and an approved batch runs
every call in the order the response issued them. A batch that is denied,
that outlives its validity, or whose invocation is cancelled runs nothing,
and a batch beside no wired approval book is refused whole rather than cut down
to a limit nobody approved. The redline list (4 rules):

  - MUST park one over-limit response as exactly one approval request, and
    answer every call of it with its own receipt.
  - MUST admit an approved batch whole and in the order the response issued it,
    with no call dropped.
  - MUST hold no model seat while a batch waits: the decision slot is vacated
    before the dispatch that parks it.
  - MUST NOT change anything when both limits stand at zero, so a loop beside no
    declaration admits exactly as it did before.

The idle-address reclamation checks: a resident consumption instance whose
address holds no work and no unsettled delivery is reclaimed once it rested a
full idle window, an address that still works or still owes a target never is,
and the reclamation is observable as a log line carrying the address and the
reason. The sweep runs at the input boundary of the set (see Consumers.Ensure),
so no ticker of its own decides when an address is read as idle.

CONSTANTS

const (
	// DefaultResponseCallCap is the call cap a producer applies when it wants
	// the documented production default: one percent of the storm the fuse was
	// built for, and well above the peak of a healthy session.
	DefaultResponseCallCap = 64
	// DefaultLongContextTokens is the assembled prompt size past which a
	// producer holds a response for approval, chosen where the model starts
	// degrading on a long conversation.
	DefaultLongContextTokens = 50_000
)
const (
	// BudgetFaceFixedPrompt is the declared prompt the binding's prompt files
	// fix into the prefix.
	BudgetFaceFixedPrompt = "fixed_prompt"
	// BudgetFaceSkillBodies is the skill bodies the boundary fixes into the
	// prefix beside the bodies a load publishes into the dynamic section.
	BudgetFaceSkillBodies = "skill_bodies"
	// BudgetFaceEnvironment is the environment block a declaration closes every
	// request with.
	BudgetFaceEnvironment = "environment"
	// BudgetFaceHistory is the replayed projection and the transcript.
	BudgetFaceHistory = "history"
	// BudgetFaceToolDeclarations is the tools the request admits, their names,
	// descriptions and schemas, beside the tool calls it replays.
	BudgetFaceToolDeclarations = "tool_declarations"
	// BudgetFaceReasoning is the reasoning text and signature one message
	// carries.
	BudgetFaceReasoning = "reasoning"
	// BudgetFaceContentParts is the text content parts of a message.
	BudgetFaceContentParts = "content_parts"
	// BudgetFaceTaskBoard is the task board material the request carries.
	BudgetFaceTaskBoard = "task_board"
	// BudgetFaceRecoveryHint is the recovery hint the replay projection presents
	// when a fold cut its material.
	BudgetFaceRecoveryHint = "recovery_hint"
)
    BudgetFaceFixedPrompt and the other BudgetFace constants are the nine input
    faces the final request accounts for. Every face is a bucket of the one
    meter; the total is their sum, so a material the meter cannot measure still
    reports its face instead of folding into another.

const DefaultOutputReserveDivisor = 4
    DefaultOutputReserveDivisor is the fixed ratio of the provider context
    window a declaration that names no output reserve leaves for the model's
    answer. The input budget is the window minus that share; the constant
    is frozen here because a silent change of the default would move every
    undeclared budget.

const FinalRoundPlannerName = "final-round-planner"
    FinalRoundPlannerName is the stable name the final round planner answers.

VARIABLES

var (
	// ErrShutdownRefused reports new input offered to an agent whose close has
	// begun. The close refuses new input first and admits nothing beside the
	// drain.
	ErrShutdownRefused = errors.New("agent: the close of this agent refuses new input")
	// ErrDrainUnsettled reports a close that ended with obligations still open.
	// An honest close reports this named unsettled state beside the reasons of
	// what stayed open instead of a clean end.
	ErrDrainUnsettled = errors.New("agent: the close did not reach real termination")
)
    ErrShutdownRefused and ErrDrainUnsettled are the named refusals of the
    close: each one names the case it refuses instead of answering a silent
    default.

var (
	// ErrCloseTimeoutMissing reports a close request that names no timeout.
	ErrCloseTimeoutMissing = errors.New("agent: the close names no timeout")
	// ErrCloseTimeoutConflict reports a close request that names two timeouts
	// that disagree.
	ErrCloseTimeoutConflict = errors.New("agent: the close names two timeouts that disagree")
)
    ErrCloseTimeoutMissing reports a close that names no timeout, and
    ErrCloseTimeoutConflict one that names two that disagree: an unbounded close
    is refused by name instead of waiting forever, and two instants are refused
    instead of one being preferred silently.

var (
	// ErrDelegationIdentityMissing reports a delegation that carries no complete
	// call identity, so no later progress or terminal could ever associate with
	// the call that issued it.
	ErrDelegationIdentityMissing = errors.New("agent: the delegation carries no call identity")
	// ErrDelegationAddressIncomplete reports a delegation whose target names no
	// complete agent session. The ledger never selects a session of its own.
	ErrDelegationAddressIncomplete = errors.New("agent: the delegation target names no complete agent session")
	// ErrDelegationIssuerIncomplete reports a delegation whose issuer names no
	// complete agent session, so no responsibility scope could receive its
	// notifications.
	ErrDelegationIssuerIncomplete = errors.New("agent: the delegation issuer names no complete agent session")
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
	// ErrDelegationNotSettled reports a reopen of a delegation whose round has
	// not settled: only a settled round is followed by another one of the same
	// task, so a round still in flight is refused by name beside no second round.
	ErrDelegationNotSettled = errors.New("agent: the delegation has not settled, so no next round follows it")
	// ErrDelegationUnwired reports a delegated round asked for while this ledger
	// carries no resident loop, so nothing exists to run the execution site it
	// names.
	ErrDelegationUnwired = errors.New("agent: the delegation ledger carries no resident loop")
)
    ErrDelegationIdentityMissing, ErrDelegationAddressIncomplete,
    ErrDelegationUnknown, ErrDelegationNotTerminal, ErrDelegationTerminal,
    ErrDelegationAdmissionRefused and ErrDelegationCommitRefused are the named
    refusals of the ledger: each one names the case it refuses instead of
    answering a silent default.

var ErrDelegatedInstanceUnavailable = errors.New("agent: no resident instance is available for the delegated site")
    ErrDelegatedInstanceUnavailable reports a delegated turn asked for while the
    loop carries no set of resident instances, or while the set already stopped.

var ErrDelegatedSiteIncomplete = errors.New("agent: the delegated site names no complete agent session")
    ErrDelegatedSiteIncomplete reports a delegated turn whose execution site
    names no complete agent session, so no resident instance could run it.

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

var ErrRequestBudgetExceeded = errors.New("agent: the request exceeds the declared input budget")
    ErrRequestBudgetExceeded is the refusal the final assembly returns when
    the declared input budget cannot carry the request in any admissible form:
    the environment block, the skill bodies and the projection have all yielded
    and the system-required material still does not fit. The refusal travels
    the loop's existing failure path (a failed terminal and the host's existing
    failure notice), never a new output channel.

var ErrRequestChainSessionRequired = errors.New("agent: projected request assembly needs a named session chain")
    ErrRequestChainSessionRequired reports a request assembly under a chain that
    names no session: assembling one would join every user of the partition into
    one transcript.

var ErrSegmentCancelled = errors.New("agent: the consumption loop ended before it answered the segment")
    ErrSegmentCancelled reports a wait that the end of the consumption loop
    answers instead of a segment: the loop that would have consumed the named
    inputs stopped, so the inputs stay owed on the chain for a restart and the
    entry reads the named cancellation instead of blocking on a driver that
    already returned.

FUNCTIONS

func AccountRequestBudget(messages []model.Message, sources []string, tools map[string]trpctool.Tool, taskBoard string) (map[string]int, []string, int)
    AccountRequestBudget measures one final request's input material and
    answers the tokens each of the nine input faces takes, the media it
    could not measure and the total. The total is the sum of the nine faces;
    a media the meter cannot measure (an image, audio, video or file part) is
    named in unknown and counted nowhere, so the total is a floor the caller
    must never read as a proven fit. The source slice runs parallel to messages:
    a message with no source, or a source that names no face, renders under the
    history face. tools are accounted once each in deterministic name order,
    and taskBoard accounts the task-board material the request carries.

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

func OutputReserveOf(providerWindow int) int
    OutputReserveOf reports the default output allowance of one provider context
    window: the fixed share of the window the declaration leaves for the model's
    answer. A window of zero declares no budget and both sides report zero;
    a window that divides below one token still reserves the one token it can,
    so the input budget never silently swallows the whole window.

func PendingInputs(facts []event.Event) []event.Event
    PendingInputs reports the driving facts one session chain holds with no
    round answered for them: the facts that start a range and stand past
    the last agent_output the chain committed, in commit order. It is the
    consumption position of a chain read back from the chain itself, never from
    a ledger kept beside it: the answer a round closes on is the one boundary
    the chain records, so an input the chain committed and no answer closed on
    is still owed a drive.

    - A chain beside no agent_output is one range in flight from its head,
    so every driving fact it holds is pending. - A fact that starts no range
    belongs to the range that committed it and is pending never, since its
    range's answer is what closes it.

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

func SwapFrameworkLogger(observer trpclog.Logger) func()
    SwapFrameworkLogger points the framework logger at the observer and answers
    the restore that puts the default back, both under the swap lock.

    The redline list (3 rules): MUST swap under the same lock every framework
    reader takes, so an observation never races a running consumer. MUST answer
    a restore that holds the logger the swap found, never a value read beside
    the lock. MUST NOT read or write the framework logger outside that lock.

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

type ApprovalBook interface {
	// Request registers one pending approval request and reports it as stored.
	Request(ctx context.Context, id, tool string, jsonArgs []byte, risk plugin.RiskLevel, rule, reason string) (*approval.ApprovalRequest, error)
	// Lookup reports the request one (tool, arguments) pair is read under with
	// its effective status.
	Lookup(tool, argsDigest string) *approval.ApprovalRequest
}
    ApprovalBook is the narrow face one parked batch is registered and resolved
    through: the registration an approver reads, and the read the wait polls for
    its ruling. It is exactly the part of the approval channel the fuses use,
    so the approval manager satisfies it as it stands and the execution core
    keeps no second book of its own: the registration files one attention
    question and the ruling is the answer it waits for, so a process that starts
    over the same chain reads the same wait back.

    The redline list (4 rules): MUST register one request per batch under the
    identity the fuse minted, and report the request as registered. MUST read
    the ruling from the book the composition wired, never from a wait the fuse
    keeps itself. MUST report the effective state of that request, so an expired
    request never reads as a standing approval. MUST NOT answer a batch the book
    never filed: an unknown digest answers none, never a fabricated approval.

func DefaultApprovalBook() ApprovalBook
    DefaultApprovalBook reports the approval book a loop is constructed beside.

func SetDefaultApprovalBook(book ApprovalBook) ApprovalBook
    SetDefaultApprovalBook declares the approval book every loop constructed
    after this call parks through, and reports the book it replaced so a caller
    restores the previous one when its own scope ends.

type BoundaryView struct {
	// Messages are the prefix messages of the request, in fixed layout order.
	Messages []model.Message
	// Sources names the input face each Messages entry renders, one entry per
	// message in the same order: fixed_prompt, skill_bodies or history. A view
	// that names fewer sources than messages reports the unnamed tail as
	// history. The loop's request budget reads the sources to let one face yield
	// without touching another, and a request boundary that reports no sources
	// keeps every prefix message on the history face.
	Sources []string
	// DynamicSystem is the dynamic system section the request closes with: the
	// bodies the skill loads published beside the environment block an agent
	// declares, rendered as one system message the loop appends as the last
	// message of the request. The stable prefix ahead of it keeps the provider
	// cache; an empty section appends nothing.
	DynamicSystem string
	// PromptVersion is the content version the request boundary fixed.
	PromptVersion string
	// VisibleSkills lists the skill names the boundary material makes visible to
	// the request, in name order and once each: the skill bodies it fixed into
	// the prefix beside the bodies the binding already loaded. A boundary that
	// names no skill reports none, and the names carry no version of their own.
	VisibleSkills []string

	// Has unexported fields.
}
    BoundaryView is what the request boundary settled on: the prefix messages
    the request carries in front of the transcript, in the fixed layout the
    request face assembles, the dynamic environment block the request closes
    with, the content version the request runs on, and the skill names that
    material makes visible. An empty PromptVersion keeps the version the binding
    already carries: a binding that names no files fixes no content version of
    its own.

func (v BoundaryView) WithBudget(providerWindow, outputReserve int) BoundaryView
    WithBudget declares the request budget policy of this boundary: the provider
    context window and the output reserve the input budget must leave. A value
    zero or below names no window, so the gate stays closed.

func (v BoundaryView) WithDynamicSkills(body string) BoundaryView
    WithDynamicSkills appends the skill bodies a load published to the dynamic
    section, recorded as the skill_bodies face the request budget may let yield.

func (v BoundaryView) WithEnvironmentBlock(body string) BoundaryView
    WithEnvironmentBlock appends the environment block to the dynamic section,
    recorded as the environment face the request budget yields before any other
    dynamic face.

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

type CloseOutcome int
    CloseOutcome is the state one close ended in: at rest, out of budget with
    work still on the chain, or unjudged.

const (
	// CloseUnreached is the zero value: the close never reached its criterion.
	// A face that reports only whether an obligation settled leaves it unset.
	CloseUnreached CloseOutcome = iota
	// CloseQuiesced reports a run that reached rest: nothing it accepted is
	// unfinished and no notification of it stands unconsumed.
	CloseQuiesced
	// CloseCut reports a close that spent its budget with work still on the
	// chain. It is a normal return: the remaining work is on the chain and a
	// restart resumes it, so an offline run that cannot settle still closes.
	CloseCut
	// CloseBroken reports a close that could not judge the run, because an
	// account refused the read or its commit sequence never held.
	CloseBroken
)
func (o CloseOutcome) String() string
    String renders the outcome as the word a report prints.

type CloseRequest struct {
	// Address is the address being closed; new input to it is refused first.
	Address event.Address
	// Timeout is the instant the explicit timeout of this close fires: the close
	// quiesces until that instant and reports what is still on the chain after
	// it.
	Timeout time.Time
	// Deadline is the spelling Timeout carried before it was renamed. A request
	// names the budget through either spelling and never through both at once.
	Deadline time.Time
}
    CloseRequest is one close of an agent: the address being closed and the
    timeout that bounds the quiesce it runs.

    A close stops injection first and quiesces to a fixed point, so it names the
    instant its explicit timeout fires: an unbounded close could wait forever
    beside work that never converges, and a close that returns while work is
    still flying reads an elapsed wait as a settled run. A request naming no
    timeout is refused by name rather than read as a wait without an end.

func (req CloseRequest) Resolve() (time.Time, error)
    Resolve reports the instant the timeout of this close fires: the one instant
    the request names, and a named refusal when it names none or two that
    disagree. Every face that runs a close reads its budget here, so the two
    spellings stay one budget.

type Consumer struct {
	// Has unexported fields.
}
    Consumer is the resident consumption loop of one address. It is the only
    driver of the model decisions of its address: every entry commits its facts
    and wakes the loop, the loop pulls what arrived, and the answer a waiting
    entry reads is the closed segment that consumed its input.

    The redline list (3 rules): MUST return to the pull after each model call,
    never exit beside a dispatched call. MUST NOT wait for every dispatched
    call: the first arrived result drives the next decision. MUST answer a
    waiting entry only from the segment that consumed its input, never from a
    neighbour's.

func (c *Consumer) AwaitTaskState(ctx context.Context, taskID event.TaskID) TaskState
    AwaitTaskState waits until the named task stops being awaited by a flying
    segment and answers the settled state: the segment either delivers the
    answer that pairs the result (consumed) or closes beside it (orphaned). The
    pairing a segment takes before it closes never answers consumed by itself,
    so the wait spans the whole window between admission and delivery. A wait
    the context ends answers the state as it stands, so the caller decides over
    a named moment instead of guessing across that window.

func (c *Consumer) Done() <-chan struct{}
    Done reports the channel that closes when the consumption loop ended.

func (c *Consumer) Drive(keys ...event.EventID)
    Drive names the facts that must drive the next pull of this address,
    whatever the closed segments of the chain present them as: the notification
    an entry still owes stands where the attempt that died left it, behind the
    answer that attempt closed on, and no position of the chain can tell it
    from material that closed segment already carried. The face that fires the
    attempt answers the judgment on itself and names the fact here.

    - A fact a segment of this process already pulled is never named: the
    material is driven here already, and a second segment over it would answer
    one notification twice. - The pull consumes the name it takes, so one name
    drives one segment and a segment that closed beside a failure leaves the
    next attempt free to name the fact again. - A name says the fact drives and
    never what it carries: the material of a fact behind the last closed answer
    stays the pull's judgment, so a fact the closed prefix already presents is
    never read as two.

func (c *Consumer) Resting() bool
    Resting reports whether this loop stands between segments: no segment flies,
    no consumption pass is running, no pass is owed, and no wake waits to open
    one. The loop is the writer of its own address, so a loop that rests writes
    nothing more at that site, and a close that reads it resting reads a writer
    stopped rather than one merely paused. A pass between its pull and the
    segment it opens is in that window, so the pass itself is what rests the
    loop, never the arrival of a segment to fly — and a wake the loop already
    took off its channel is still the pass it owes, never a loop between passes.
    - The first clause is the whole judgment of a dead loop: a loop whose run
    ended leaves the segment it flew closed (see abandonSegment), so an ended
    loop beside a segment still flying answers not resting here instead of
    reading its shutdown as rest.

func (c *Consumer) SetProgress(pulse func())
    SetProgress attaches the pulse this loop reports each movement through,
    or detaches it beside nil. The pulse is a report, never a judgment: a close
    reads it to re-read its criterion at the moment the account moved and keeps
    its own reading as the answer.

func (c *Consumer) TaskState(taskID event.TaskID) TaskState
    TaskState reports what became of the named task on this address. A task this
    address never admitted answers empty, which the notice-driven follow-up
    reads as unattended and drives.

func (c *Consumer) WaitAnswer(ctx context.Context, keys []event.EventID) (SegmentAnswer, error)
    WaitAnswer waits for the segment that consumed any of the named input keys
    and answers its aggregated turn. An input the flying segment already took
    answers when that segment closes; an input no segment took yet answers
    when the next pull drives one. An input whose segment closed before this
    wait began is found by its own key, so a late entry reads the answer of the
    segment that consumed it rather than the answer of a segment that closed
    beside it. A wait the end of the loop meets answers ErrSegmentCancelled.

    - The answer index and the notify channel are swapped under one lock,
    so an atomic miss waits on the channel the next settle closes.

func (c *Consumer) Wake()
    Wake signals the loop that events may have arrived: the signal merges,
    so a wake that lands while the loop consumes keeps it turning once more.

    - Only a wake the channel really accepted queues a pass: a merged wake asks
    for the pass an earlier token already owes, so it repeats no count. - The
    pass is queued before the token lands, so the loop that takes the token and
    has not entered the pass yet is never read as resting.

type Consumers struct {
	// Has unexported fields.
}
    Consumers owns the resident consumption loops of a run: exactly one per
    complete agent address, each the only driver of the model decisions of its
    address. The set is address-blind — an instance mounts at the first activity
    of the address its material names, whether that address belongs to the entry
    agent or to any other agent the run declared, and no address is guessed
    from the entry the document names. The loops live as long as the context
    they were started under; a loop that ended is never restarted in place,
    the chain carries the position a restart resumes from, and a set stops
    mounting instances once the run it belongs to stopped. Every closed segment
    hands its fresh answer to the one delivery face of the set, so the answer
    of a segment no entry waits on leaves through the same path the answer of a
    waited segment does.

func NewConsumers(loop *Loop, base context.Context, deliver SegmentDelivery) *Consumers
    NewConsumers assembles the consumption loops of one run over the loop that
    drives its decisions, living as long as the named context does. The delivery
    face receives the fresh answer of every closed segment and may be nil,
    in which case the answers stay on the chain.

func (cs *Consumers) Ensure(addr event.Address) *Consumer
    Ensure answers the resident instance of one address, assembling and
    starting it on first use. The address is the one the material itself names,
    so an instance mounts for every complete agent address the run drives,
    never for the entry agent alone.

    - Exactly one instance per address: the instance belongs to the loop, not to
    the set. A second Ensure of an address — from this set or from any other set
    mounted over the same loop — answers the instance the loop already carries,
    so one address never runs two drivers of its own decisions, however many
    sets were mounted. - The lifecycle is the run's: an instance lives as long
    as the context the set was built under, and a set that stopped mounts none,
    so a call arriving beside a drained run answers no instance instead of
    starting a driver beside the drain. - The loop resumes behind the last
    agent_output the chain carries — the inputs past it are the material a
    restart still owes — and a chain beside no agent_output at all resumes from
    its head, so an unanswered input stays owed instead of vanishing behind
    a restart. - The call is the input boundary of the set: it sweeps the
    addresses it already drives first, so an address that rested a full idle
    window ends here and the mount this call answers reads the chain from its
    own head instead of a parked driver.

func (cs *Consumers) Has(addr event.Address) bool
    Has reports whether one address carries a resident consumption loop.

func (cs *Consumers) RecycleIdle(now time.Time) int
    RecycleIdle runs one reclamation sweep at the given clock reading and
    answers how many instances it reclaimed. An address is reclaimed only when
    its instance rests, its delivery account holds no unsettled obligation
    and the full TTL passed since the first sweep that found it resting. The
    first sweep only records that moment; a loop that starts working or gains
    an obligation loses its record, so the TTL always counts one uninterrupted
    rest.

func (cs *Consumers) Resting() bool
    Resting reports whether every loop this set mounted stands between segments,
    so the set writes nothing more at any of its addresses. A set this host
    mounts nothing on reports resting: nothing of it flies.

func (cs *Consumers) SetDeliveryPending(pending func(event.Address) bool)
    SetDeliveryPending registers the predicate the sweep reads before it
    reclaims an address: true names a delivery account that still owes a target
    (a held or owed delivery fact), so the instance stays however long it rests.
    A nil predicate names no unsettled delivery anywhere.

func (cs *Consumers) SetIdleTTL(ttl time.Duration)
    SetIdleTTL changes the idle bound of this set. A negative bound keeps every
    instance for the life of the set; a zero restores defaultIdleAddressTTL.

func (cs *Consumers) SetProgress(pulse func())
    SetProgress attaches the pulse every loop of this set reports its movement
    through, the mounted ones included. The setter is separate from the
    constructor so the declared construction shape of the set stays as it is.

func (cs *Consumers) SetTurnContext(decorate func(ctx context.Context, trigger string) context.Context)
    SetTurnContext attaches the decorator every decision of every segment these
    loops fly runs under: the host stamps the lineage of the material that
    opened the segment, so a body a call of that decision sends leaves under
    the line of its own turn. The setter is separate from the constructor so the
    declared construction shape of the set stays as it is.

func (cs *Consumers) Stop()
    Stop ends every consumption loop this set owns and answers only once each
    of them returned: the loops answer their done channels, the segment a loop
    still flew is closed by that loop — the answer of it reached to the entries
    waiting on it and its calls read as orphaned — and no further instance
    mounts for any address. The segments the ended loops left flying stay on the
    chain for a restart, and the instances they mounted leave the registry of
    their loop once they really ended, so the address that mounts next is a new
    driver instead of the dead one this set stopped behind.

    The join is the stop itself, not a courtesy beside it: a loop is the writer
    of its own address, so a set that returned while a loop still unwound a
    segment would answer a stopped set over writes that have not landed yet,
    and a close that read that answer would leave those writes behind it.
    The wait is unbounded for the same reason the settle driver's stop is:
    the cancellation is the request, and only the loop's own return is the
    confirmation. This hard stop belongs to the teardown of a run whose close
    already judged it; the close itself marks the set and awaits it inside its
    own budget through StopResidentsWithin instead.

func (cs *Consumers) Wake(addr event.Address)
    Wake signals the consumption loop of one address; an address beside no loop
    wakes nothing, and the entry that owns its facts ensures the loop itself.

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

type DelegatedAnswer struct {
	// Body is the assistant text the round ended on; empty when it produced none.
	Body string
	// Status is the terminal the delegated task reports.
	Status task.Status
	// Failure is the named cause a failed round ended at, empty otherwise.
	Failure string
}
    DelegatedAnswer is the outcome of one delegated round as its execution site
    reported it: the body its last decision produced and the terminal it ended
    at.

type DelegatedInput struct {
	// Site is the execution address the delegated turn runs in.
	Site event.Address
	// Binding is the binding the decisions of that site run on: the assembled
	// sub-agent's own material, never a generation held for a declared agent.
	Binding state.AgentBinding
	// Instruction is the instruction the turn runs under; empty declares none.
	Instruction string
	// Request is the task this event states: the direct content of the task
	// fact that closes the delegated input batch and drives the round.
	Request string
	// Materials are the source events the task reads from, resolved by the
	// caller: the framework projects each into the site's chain as the material
	// the round receives, and a source the call could not resolve stands here as
	// none. The consuming agent stays unaware of the resolution.
	Materials []event.Event

	// Has unexported fields.
}
    DelegatedInput is what one delegated turn hands the resident instance of its
    execution site: the site, the binding its decisions run on, the instruction
    it runs under, the materials the task reads from and the request it starts
    from.

func (in DelegatedInput) WithDeclaration(declaration []byte) DelegatedInput
    WithDeclaration stamps the resolved declaration of one delegated turn onto
    the input, so the task fact that closes its batch carries the assembly a
    re-entry after a restart serves the next round on. The body stays opaque
    here: the caller that assembled the declaration owns its vocabulary.

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
	// Sites names the execution site the delegated turn runs in and the
	// responsibility scope its notifications return to.
	Sites DelegationSites
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

type DelegationSites struct {
	// Target is the execution site: the agent session address the delegated turn
	// runs in, minted for the delegated turn itself rather than borrowed from the
	// caller.
	Target event.Address
	// Issuer is the return site: the agent session address the delegated call was
	// issued from, the responsibility scope its notifications return to. An empty
	// issuer names the target itself, so a call running where it was issued
	// returns there.
	Issuer event.Address
}
    DelegationSites is the two places one delegated call names: the execution
    site the delegated turn runs in and the responsibility scope its
    notifications return to. The two addresses are read together or not at all
    — a record that named one without the other could not tell a turn running
    where it was issued from a turn running in a site of its own — so they are
    one concept rather than two sibling fields.

    A record rebuilt from the task ledger after a restart carries the return
    site alone: its execution site is the continuation the declaration rebuilds,
    not a second address a task record could carry. The rebuilt entry is the
    settled round a reopen starts from; the site is re-derived when the round is
    served.

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

func (d *Delegator) AbandonRound(taskID event.TaskID, round int64)
    AbandonRound withdraws the reopening of one round, because nothing served
    it: the entry goes back to the terminal it reopened from, the terminal
    waiting to land is forgotten and the pre-registration the reopen booked
    is withdrawn. A ledger that is not holding that round is left as it is,
    so a rollback of a round already settled moves nothing.

func (d *Delegator) CanReopenRound(taskID event.TaskID) error
    CanReopenRound reports whether the ledger would reopen the entry of one
    delegation, without moving it: the entry the ledger holds and the terminal
    it stands at. A re-entry reads this before it opens the round of the task,
    so the only mutation left to put back when the reopen itself is refused is
    the round the task machine already opened.

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
    The execution site the call runs in and the responsibility scope it reports
    back to are two places, so a call running in a site of its own still returns
    its notifications to the invocation that issued it.

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

func (d *Delegator) ReopenRound(taskID event.TaskID, round int64) error
    ReopenRound moves the ledger entry of one delegation to the round its task
    was reactivated into, so a task re-entered out of its settled outcome is
    served and settled as the next round of the same task instead of a second
    terminal of the round already landed.

    The round is the one the task state machine opened, taken from the
    reactivation that opened it: the ledger never counts rounds of its own,
    so the notification a re-entered round settles under keeps the identity
    the task carries. Only a settled entry reopens; a round still in flight is
    refused by name beside no second round, and the pre-registration the first
    round withdrew is booked again because a round of the task is once more in
    flight.

func (d *Delegator) RestoreEntry(taskID event.TaskID, spec task.Spec, status task.Status) error
    RestoreEntry rebuilds the ledger entry of one delegation from the task
    it was admitted as, so a process restarted over the same durable account
    re-enters a delegated task through the round bookkeeping the process that
    served it held: the invocation and return site the entry reports to, the
    call it was admitted under and the terminal the task reached. The rebuilt
    entry books nothing and holds no pin — the generation the admitting process
    borrowed is gone and no round of this process is in flight — so the next
    ReopenRound moves it to the round a reactivation opens exactly as a live
    settled entry would.

    Only a task the delegation face admitted is rebuilt: a task of another
    family carries no delegation this ledger may reopen, and is refused by name.
    A task the ledger already holds is left exactly as it is, because the
    live entry is the one authority of its round. The rebuilt record carries
    the return site alone: the execution site is the address the continuation
    rebuilds from the declaration the delegated input travels with.

func (d *Delegator) SetResident(loop *Loop)
    SetResident attaches the loop whose resident instances run the delegated
    turns this ledger admits. The setter is separate from the constructor so the
    declared construction shape of the ledger stays as it is.

func (d *Delegator) Submit(ctx context.Context, in DelegatedInput) (DelegatedAnswer, error)
    Submit hands one delegated round to the resident instance of its execution
    site and answers the status it ended at beside the body it produced.
    The round runs beside the receipt the admission already returned, so the
    turn that issued the call never waits on it.

func (d *Delegator) SubmitBeside(ctx context.Context, in DelegatedInput, started func(error)) (DelegatedAnswer, error)
    SubmitBeside hands one delegated round over exactly as Submit does,
    and reports the round's first commit to started: the caller that answers a
    delivery receipt hands the body over here, so the receipt it returns is the
    delivery rather than the acceptance of a round that may yet fail to land it.

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
	// Outcome is the state the close ended in.
	Outcome CloseOutcome
	// Drained reports whether every accepted obligation reached real termination.
	// It stays false while any producer has not confirmed its stop.
	Drained bool
	// Unsettled lists the work that was still open when the close reported, each
	// with the address it stands at and the reason it stayed open.
	Unsettled []UnsettledObligation
	// Failed names the answers a notification was settled on with a failed
	// terminal, each by the invocation whose turn ended failed, in the order the
	// chain committed them. An empty list is the close that settled its work on
	// completed answers alone; every entry names one failure whose cause a reader
	// finds on the same chain, so a close over failures is told from a clean one
	// instead of both reading as the same quiesce.
	Failed []event.InvocationID
}
    DrainReport is the outcome of one close: the state the close ended in,
    whether every accepted obligation reached real termination, and the work
    that was still open when the close reported.

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

type FinalRoundChain interface {
	// AddressOf reports the (agent, session) address one committed fact belongs
	// to; a fact beside no agent or no session belongs to no chain.
	AddressOf(ctx context.Context, id event.EventID) (event.Address, bool)
	// SegmentOf reports the segment one address flies: the facts committed past
	// the last agent_output of that chain, read as one judgment's material.
	SegmentOf(ctx context.Context, addr event.Address) (FinalRoundSegment, bool)
	// FileFinalRoundRequest files one judgment on the chain of the address whose
	// budget it judged; a range already carrying a request answers as filed.
	FileFinalRoundRequest(ctx context.Context, addr event.Address, request event.FinalRoundRequest) error
}
    FinalRoundChain is the chain face the planner reads one commit through:
    the address a committed fact belongs to, the segment that address flies and
    the filing of the judgment the segment's spent budget answered.

    The redline list (3 rules): MUST answer a fact no chain owns as unowned,
    and a chain beside no range in flight as no segment. MUST report the rounds
    and the start the chain records, never a count the planner keeps. MUST NOT
    commit a second request for a range that already carries one.

type FinalRoundPlanner struct {
	// Has unexported fields.
}
    FinalRoundPlanner files the closing round of a range as the fact it is:
    it reads the segment one commit grew, judges the budget its own agent
    declares and files the final_round.request of the range whose budget is
    spent. The judgment is read off the chain, so a fresh process judges the
    same chain the same way, and the identity of the fact is content-addressed,
    so a re-evaluation of the same range files nothing twice.

    The planner is an instance of the material flow like any other: it reads
    the committed fact and answers a read-only observation, so the pipeline
    dispatches it beside the reflection driver and the age fold. Its own filing
    is a commit of the range and never a second observation, so the judgment it
    just filed never re-enters this pass.

func NewFinalRoundPlanner(chain FinalRoundChain, policy func(event.AgentID) FinalRoundPolicy) *FinalRoundPlanner
    NewFinalRoundPlanner assembles the planner over the chain face of one run
    and the budget table of the declaration it published last. A nil face or
    a nil table files nothing, so an assembly mounted beside neither judges no
    budget instead of failing on the first commit.

func (p *FinalRoundPlanner) Material() plugin.Material
    Material declares what this planner reads, when it reads it and what a
    failed reading leaves behind: one committed fact read after its commit,
    where a failed judgment is a named gap and the fact keeps standing.

func (p *FinalRoundPlanner) Name() string
    Name reports the stable name of this planner instance.

func (p *FinalRoundPlanner) ObserveCommit(ctx context.Context, id event.EventID) error
    ObserveCommit judges the budget of the segment one committed fact belongs to
    and files the closing round of the range whose budget is spent.

    The duty is read-only over the fact and over the chain: a fact beside no
    agent address, a chain beside no range in flight, a segment within both of
    its budgets and an agent that declares no budget each leave the commit as it
    stands. The filing is the one write, and a failure to file it is reported to
    the pipeline as the named gap beside a fact that stays committed.

func (p *FinalRoundPlanner) Port() plugin.Port
    Port reports the consumption the planner serves: one committed fact in,
    the judgment of one spent budget out.

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

func FinalRoundDue(policy FinalRoundPolicy, rounds int, elapsed time.Duration) FinalRoundReason
    FinalRoundDue reports why the closing round of one range is due at the
    budget it has spent, or FinalRoundNone while the range may continue:
    the judgment the loop reads at its own decision boundary, and the one a
    producer files as the final_round.request fact of the range it judged.
    Both budgets are read at a round boundary, so a decision already in flight
    is never cut by this reading.

type FinalRoundSegment struct {
	// Range is the invocation whose budget the judgment reads.
	Range event.InvocationID
	// Rounds is the number of tool rounds the range ran before the judgment.
	Rounds int
	// Started is the moment the segment's earliest fact landed: the clock the
	// time budget is spent against.
	Started time.Time
}
    FinalRoundSegment is the material one final round judgment reads off the
    chain of one address: the range its next decision serves, the tool rounds
    that range already ran and the moment the segment's first fact landed.

type FuseLimits struct {
	// CallCap bounds the tool calls one model response may admit without an
	// approval.
	CallCap int
	// LongContextTokens is the assembled prompt size past which a response is
	// held for approval whatever it issues.
	LongContextTokens int
}
    FuseLimits is the execution-side fuse declaration of one loop: a member
    at zero is its fuse off, so the zero declaration admits exactly what the
    runtime admitted before the fuses existed.

func DefaultFuseLimits() FuseLimits
    DefaultFuseLimits reports the declaration a loop is constructed beside.

func SetDefaultFuseLimits(limits FuseLimits) FuseLimits
    SetDefaultFuseLimits declares the fuses every loop constructed after this
    call is built beside, and reports the declaration it replaced so a caller
    restores the previous one when its own scope ends. A loop already running
    keeps the declaration it was constructed with until its own setter replaces
    it.

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
    whose transcript a mapped sequence carries needs no second face here: that
    sequence is the one history of the session, so an implementation may answer
    none of its own beside it — and the facts of that chain are what the drive
    reads its range's input again from at every decision boundary. The face
    may also answer the presentation capability: an implementation whose chain
    already knows which facts the request prefix of its session presents reports
    them through SessionPresented(addr event.Address) map[event.EventID]bool,
    and the resident consumption loop reads that capability from this same
    face rather than from a second seam. A face that keeps no prefix answers no
    presentation, and the pull keeps the chain's own boundary. The redline list
    (4 rules):

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

    The committed delivery fact is named to the consumer of the range beside the
    wake: a result whose segment already closed stands behind the answer that
    segment landed, so no position of the chain opens a segment on it by itself,
    and the wake that names it is the account that says this fact still drives.
    The observation the result carries rides on the fact, so the segment the
    name opens reads the very result the notification delivered instead of a
    second reading of it.

    - The wake names the fact only while the task is not awaited by a flying
    segment and not already consumed: an awaited result is paired by that
    segment's harvest, and a consumed one drives nothing, so neither is read
    twice. - A loop that drives no set keeps its backflow parked in the intake;
    a set that carries no instance for the address wakes nothing and starts
    none.

func (w Loop) Begin(name string) func()
    Begin registers one writer that is starting now and answers the confirmation
    that closes its registration. The confirmation is idempotent and safe to
    call from a defer: a second call reports the same stop and drops no other
    writer. A ledger beside nil registers nothing and answers a confirmation
    that reports nothing, so a loop or a run without a ledger keeps running
    unregistered.

func (l *Loop) CommitFactFor(ctx context.Context, agentID event.AgentID, evt event.Event) (event.EventID, error)
    CommitFactFor commits one fact on behalf of the named agent and runs the
    After pass of that fact, and reports the identity the fact committed under.
    It is the same commit the loop's own production points use, exposed so a
    producer outside a dispatch — the configuration source judging a model
    replacement — lands its fact in the one ledger and is observed through the
    faces of that agent's published generation exactly as the loop's own facts
    are. An empty identity beside no error reports a loop beside no commit face,
    and a refused commit reports the failure beside no identity.

func (l *Loop) InFlightModel(addr event.Address) (event.InvocationID, event.DecisionID, bool)
    InFlightModel reports the model decision the consumption state of one
    address carries, or that no model request of that address is executing.

    The redline list (3 rules): MUST report the decision this loop runs,
    never a decision another loop runs. MUST report no flight once the decision
    of that address ended. MUST NOT take a decision just to read it: the read
    starts nothing.

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

func (l *Loop) SetApprovalBook(book ApprovalBook)
    SetApprovalBook attaches the approval book this loop parks a batch through.
    A nil book leaves the fuses with no channel, and a parked batch is then
    refused whole instead of being truncated.

func (l *Loop) SetCapture(capture Capture)
    SetCapture attaches the trajectory capture face this loop fixes each
    decision's request view through, admits each observed attempt through and
    seals the turn through. A nil face captures nothing. A refused fix, record
    or seal is a counted recording loss and never reaches the model decision,
    and the setter is separate from the constructor so the declared construction
    shape of the loop stays as it is.

func (l *Loop) SetConsumers(consumers *Consumers) *Consumers
    SetConsumers assigns the loop's only Consumer set during assembly
    and returns the actual current set. The first non-nil set wins; later
    replacement or clearing does not change it. A loop with no assigned set
    mounts one on first resident use, while a backflow beside no current set
    only parks its result and never mounts one.

func (l *Loop) SetDeliveryPending(pending func(event.Address) bool)
    SetDeliveryPending registers the predicate the idle reaper reads before it
    reclaims an address, and applies it to the set this loop already drives. The
    assembly registers it: the obligation state lives there, and the consumer
    registry only asks. A nil predicate clears the registration.

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

func (l *Loop) SetFuseLimits(limits FuseLimits)
    SetFuseLimits attaches the fuses this loop admits under: the declaration
    the composition root reads off the agent specification, replaced whole so a
    reload of the declaration never leaves half of a previous one in force.

func (l *Loop) SetHistory(face HistoryFace)
    SetHistory attaches the session transcript face this loop reads the shared
    session history from and commits one ended drive's transcript back to. The
    setter is separate from the constructor so the declared construction shape
    of the loop stays as it is. The face also carries the optional presentation
    capability: a chain that knows which facts its request prefix presents is
    read through this same attachment, so the loop has no second presentation
    seam to inject.

func (l *Loop) SetLongContextTokens(tokens int)
    SetLongContextTokens attaches the long-context threshold alone; zero or a
    negative value leaves the fuse off.

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

func (l *Loop) SetResponseCallCap(cap int)
    SetResponseCallCap attaches the per-response tool call cap alone; zero or
    a negative value leaves the cap off. The composition root that carries only
    this key wires it without assembling a second declaration.

func (l *Loop) StopResidents()
    StopResidents ends the loop's one current Consumer set: no further instance
    mounts, each flying segment is answered by cancellation, and its inputs stay
    on the chain for restart. The current set remains as the stopped tombstone.
    No Loop lock is held while joining consumers, so a Consumer tool may
    synchronously re-enter WakeAddress.

func (l *Loop) StopResidentsWithin(deadline time.Time) []string
    StopResidentsWithin ends the loop's one current Consumer set inside the
    named deadline: the set is marked first — no further instance mounts,
    no context of a flying loop is cancelled by the mark itself, and each loop
    the set already holds ends its own life once the work it holds really ends
    — and the join that follows waits for those exits under the same deadline,
    so a close whose budget arrived returns beside the loops still flying
    instead of blocking on them. A loop the budget ended beside inside a model
    production is left flying and named in the answer; its production stays
    named by the writer ledger of the run, so the close that spent its budget
    reports it and a later drive finishes it for real. A loop the budget ended
    beside between turns is closed by its own loop (see Consumers.awaitStopped),
    so no entry waits on a driver that already spent its close. A zero deadline
    joins without one.

func (w Loop) Stopped() (bool, []string)
    Stopped reports whether every registered writer confirmed its stop,
    and names the ones that did not in registration order. A writer whose
    registration is still open has not stopped: the work it runs began and its
    end was never confirmed, whatever the state of the request that started it.

func (l *Loop) SubmitDelegated(ctx context.Context, in DelegatedInput) (DelegatedAnswer, error)
    SubmitDelegated hands one delegated turn to the resident instance of its
    execution site and answers the segment that consumed it: the input commits
    at that site under the delegation lineage, the instance mounted with the
    binding the turn runs on is woken, and the round is the one that segment
    flies. The caller keeps the task face — admission, settle and notice stay
    where they are — and reads only what the turn produced. - The input commits
    as the site's own external input before the wake, so the segment the wake
    reads as its own material. - The answer is the chain the segment closed on:
    a body that never committed is a failed round rather than a copy kept
    beside the chain. - The body is the one the closing decision of that
    segment produced, never whatever the chain of the site ended at meanwhile:
    a second segment that closed beside this wait has no body of its own to
    lend. - A cancelled wait reports the cancelled terminal beside no error, so
    a closing run settles what it already admitted instead of leaving it owed.
    A wait the end of the loop answers is the same cancellation: the segment
    stays owed on the chain for the restart, and the round reports the terminal
    it really reached.

func (l *Loop) SubmitDelegatedBeside(ctx context.Context, in DelegatedInput, started func(error)) (DelegatedAnswer, error)
    SubmitDelegatedBeside hands one delegated turn to the resident instance
    of its site exactly as SubmitDelegated does, and reports the round's first
    commit beside it: the callback hears nil once the input has landed at
    the site, and the named cause of a turn that never landed. A caller that
    answers a delivery receipt uses it to answer only for a body that arrived,
    instead of answering before the input reached the chain the receipt claims.

    The report is the input's commit and nothing else: a round that lands its
    input and then fails still reported nil, because the body did arrive and
    what became of the round is its own terminal's to report.

func (l *Loop) WakeAddress(addr event.Address) *Consumer
    WakeAddress ensures the resident Consumer of one address and signals that
    facts arrived. A committed body never needs a separate drive, whether its
    target was busy, idle, or already bound without a mounted Consumer.

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
    Reconciler is the delegation ledger a close lands the terminals of admitted
    calls through: a terminal whose obligations have cleared is landed and the
    booking beside it closes, while an obligation still open keeps the terminal
    withheld exactly as it does beside a live turn. The close reads it as one
    of its criteria, so a run is never reported at rest beside a terminal the
    ledger still holds.

    The redline list (3 rules): MUST land a pending terminal only once the
    obligations of that call are cleared. MUST report every booking it still
    holds, so a close never reports a rest beside an unlanded terminal. MUST NOT
    hold a terminal past the obligations it waits on, and MUST NOT report one
    before them.

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
	// ProjectionBudget overrides the token budget the projection presents under
	// when the loop's final request budget must fold the history further than
	// the replay window would. Zero presents the projection under the window's
	// own budget (see projectionBudget), so an ordinary request keeps the fold
	// semantics its window declares; a positive value is the room the final
	// request budget computed for the projected material after every other face
	// already yielded.
	ProjectionBudget int
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

type SegmentAnswer struct {
	Turn     Turn
	Err      error
	Trigger  string
	Consumed []event.EventID
	// Body is the assistant text the closing decision of this segment produced,
	// or none beside a segment that produced no text or committed no turn.
	Body string
}
    SegmentAnswer is the closed-segment answer one waiting entry reads:
    the aggregated turn, the named cause of a failed segment, the trigger-source
    stamp the segment opened on, which names the lineage its answer leaves
    under, the keys of the inputs the segment consumed, so a face that delivers
    the answer knows which entries that segment answered, and the body its last
    decision produced, so the entry reads the answer of the segment it waited on
    rather than whatever the chain ended at meanwhile.

type SegmentDelivery func(ctx context.Context, addr event.Address, answer SegmentAnswer)
    SegmentDelivery is the face the host hands one closed segment's fresh
    answer to: the address whose chain the segment committed its output to,
    and the answer the entries waiting on that segment read. A consumer beside
    no face delivers nothing and leaves every answer on the chain, which is the
    shape a loop driven by tests keeps.

type TaskState string
    TaskState names what became of one settled result on this address: a flying
    segment still awaits it, a segment delivered the answer that paired it,
    or a closed segment left it unpaired. The notice-driven follow-up reads the
    state to know whether any consumer already took the result.

const (
	// TaskAwaited: the flying segment admitted the task and its delivery that
	// pairs it has not landed yet.
	TaskAwaited TaskState = "awaited"
	// TaskConsumed: a segment paired the result and delivered the answer of it.
	// The pairing alone never answers this state, and the round leaves it once
	// the pull receives the terminal notice the delivery answered: the state
	// never outlives the round it reports, so a later round of the same task is
	// not read as one this book already took.
	TaskConsumed TaskState = "consumed"
	// TaskOrphaned: the segment that admitted the task closed without pairing it.
	TaskOrphaned TaskState = "orphaned"
)
type Turn struct {
	// InvocationID is the range the turn belongs to.
	InvocationID event.InvocationID
	// DecisionCount is the number of model decisions the turn ran.
	DecisionCount int
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
    Turn is the outcome one invocation's drive reports: the number of decisions
    it ran and the terminal its latest decision reduced to.

type UnsettledObligation struct {
	// TaskID is the task the work belongs to, and empty for a notification no
	// task identity of this run names.
	TaskID event.TaskID
	// Address is the address the work stands at: the target a notification was
	// never accepted at, or the site that accepted one nobody consumed.
	Address event.Address
	// Source is the address the work came from, so a report names both ends of
	// the routing it could not settle.
	Source event.Address
	// FactKey is the stable identity of the fact one item stands on: the
	// notification a delivery waits on, or the driving fact of an input round no
	// answer closed, and empty for work that stands on no single fact.
	FactKey string
	// Reason is the readable reason the work could not be settled.
	Reason string
}
    UnsettledObligation is one piece of work a close could not settle, paired
    with the address it stands at and the reason it stayed open.

func WriterObligations(names []string) []UnsettledObligation
    WriterObligations renders the writers that have not confirmed their stop as
    the report entries of one close: each one named by the production it runs
    instead of by a task identity it never held.

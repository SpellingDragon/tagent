package task // import "github.com/SpellingDragon/tagent/runtime/task"

This file holds the logical status of a task: the classification of how long
the task's recorded state has stood unchanged. It is a second read-out beside
the status machine, never a value the machine stores: the status a task holds is
written by the transitions it commits, and this classification is derived from
the movement stamps those transitions leave, so reading one leaves the other
exactly as it stands.

红线: 逻辑状态=纯观测：只标记，不改状态机、不自动处置（不 kill 不中断）——处置走 Governor 策略面。

Package task is the runtime's task admission and settlement layer. It declares
the declaration a tool, sub-agent or result-delivery call is admitted under,
the status a task moves through, and the ports through which a transition is
committed and its notifications are registered.

A task is the runtime's unit of responsibility and accounting. The admission,
commit and scheduling behaviour runs behind those ports in this package:
the registry owns the life of a task, the manager owns its obligations, and the
commit carrier records the transitions.

CONSTANTS

const (
	// PolicyDefault is the name of DefaultPolicy; the empty name selects it too.
	PolicyDefault = "default"
	// PolicyResponsive is the name of the threshold set for tasks whose caller
	// wants an earlier report of silence.
	PolicyResponsive = "responsive"
	// PolicyPatient is the name of the threshold set for tasks that stay in
	// execution for a long while by design.
	PolicyPatient = "patient"
)
    PolicyDefault, PolicyResponsive and PolicyPatient are the declared task
    policy names: the name an agent declaration selects through its task policy,
    and the threshold sets those names resolve to.

VARIABLES

var DefaultPolicy = Policy{
	WatchAfter:       30 * time.Second,
	StableAfter:      2 * time.Minute,
	SuspectAfter:     10 * time.Minute,
	ExecutionTimeout: 30 * time.Minute,
	TTL:              2 * time.Hour,
}
    DefaultPolicy is the threshold set a task is read under when its declaration
    names no other policy: silence is observed after half a minute, reported
    stable after two minutes and suspected after ten, one round of execution is
    waited for half an hour, and a task unsettled for two hours expires.

var ErrApprovalDigestMismatch = errors.New("task: the approval response names other arguments than the held ones")
    ErrApprovalDigestMismatch reports an approval response whose argument digest
    names other arguments than the ones that were held. The response is refused
    by name and never silently applied to the changed arguments.

var ErrApprovalIdentityMismatch = errors.New("task: the approval response names another approval identity")
    ErrApprovalIdentityMismatch reports an approval response that does not name
    the approval identity the hold was issued under. The response is refused by
    name and never applied to another call or to another hold.

var ErrCallFrameMissing = errors.New("task: the wrapped call carries no call frame")
    ErrCallFrameMissing reports a wrapped call carrying no call frame, so it
    could be admitted under no identity.

var ErrCancelNotRequested = errors.New("task: no cancellation request is open for this task")
    ErrCancelNotRequested reports a confirmed stop with no cancellation request
    open for that task, so the terminal kind is not the one to record here.

var ErrChainBroken = errors.New("task: the fact chain does not follow the record it restores into")
    ErrChainBroken reports a fact chain link whose expected revision does
    not follow the record it restores into, so the chain cannot be folded any
    further.

var ErrCommitUncertain = errors.New("task: the commit outcome of this change is unknown")
    ErrCommitUncertain reports a status change whose storage outcome is unknown.
    The change is reconciled before any repeat of it.

var ErrConcurrencyLimit = errors.New("task: the concurrency limit is reached")
    ErrConcurrencyLimit reports an admission refused because the registry
    already holds its limit of non-terminal tasks. Every task already admitted
    is left untouched.

var ErrDenied = errors.New("task: the call was refused before execution")
    ErrDenied reports a call the approval channel refused before execution. The
    refusal pairs with the named result of the call beside this native error.

var ErrIllegalTransition = errors.New("task: the status change is outside the state machine")
    ErrIllegalTransition reports a status change outside the task state machine.

var ErrInputMissing = errors.New("task: the resume declaration carries no event identity")
    ErrInputMissing reports a resume declaration carrying no event identity,
    so no repeat of that declaration could ever be recognized as the same one.

var ErrInstanceClosed = errors.New("task: the receiving instance is closed and its undelivered notifications are retained")
    ErrInstanceClosed reports a delivery refused because the receiving instance
    is closed. The undelivered notifications of a closed instance are retained
    for the recovery path: they are never dropped, never re-routed, and never
    delivered into a later instance that merely reuses the same address.

var ErrMutationConflict = errors.New("task: mutation identity carries different content")
    ErrMutationConflict reports a commit that reuses a recorded mutation
    identity with different content.

var ErrMutationMissing = errors.New("task: commit record carries no mutation identity")
    ErrMutationMissing reports a commit record that carries no mutation
    identity, so no repeat of that record could ever be recognized as the same
    mutation.

var ErrNoDeliverer = errors.New("task: no deliverer is attached to this manager")
    ErrNoDeliverer reports a flush that could deliver nothing, because no
    deliverer is attached to the manager.

var ErrNoHandoff = errors.New("task: no handoff is recorded for this task")
    ErrNoHandoff reports a handoff record request for a task no explicit handoff
    moved yet.

var ErrNoMailbox = errors.New("task: no mailbox is attached to this wrapper")
    ErrNoMailbox reports a wrapped call whose background result could not travel
    back, because no mailbox is attached to the wrapper.

var ErrNoOwnerBinding = errors.New("task: no owner binding is attached to this restore")
    ErrNoOwnerBinding reports a restore that could resolve no owner, because no
    owner binding was supplied to it.

var ErrNoResultRecorder = errors.New("task: no result recorder is attached to this wrapper")
    ErrNoResultRecorder reports a wrapped call that could not record its result,
    because no recorder is attached to the wrapper.

var ErrResumeCancelled = errors.New("task: a cancelled task is resumed by relaunch or re-spawn")
    ErrResumeCancelled reports a resume refused for a cancelled task.
    A cancelled task is relaunched or re-spawned under a new identity.

var ErrRevisionConflict = errors.New("task: commit record expects another revision")
    ErrRevisionConflict reports a commit whose expected revision names another
    revision than the one the task is at, so the record is known not to be
    stored.

var ErrRoundInFlight = errors.New("task: a round of this task is in flight")
    ErrRoundInFlight reports a resume refused because a round of the task is in
    flight.

var ErrRulingUnknown = errors.New("task: the approval decision names no known class")
    ErrRulingUnknown reports an approval decision whose class is none this
    channel knows, so the call is refused closed rather than admitted under a
    guess.

var ErrSpecMissing = errors.New("task: the fact chain carries no admission material")
    ErrSpecMissing reports a fact chain whose admission link carries no
    admission material, so the task cannot be rebuilt from the chain alone. The
    chain is refused by name and reports no recovery: a chain is self-contained
    by construction and a rebuild from a link that carries no admission material
    is never attempted.

var ErrStatusUnknown = errors.New("task: the unknown status is never recorded")
    ErrStatusUnknown reports an attempt to record the unknown status on a task.
    Unknown means not determinable, so it is surfaced and never stored as a
    status.

var ErrTargetRemoved = errors.New("task: the target is removed from its owner's tool set under the current binding")
    ErrTargetRemoved reports a restored target that the current orchestration
    binding does not hold in the tool set of its owner. The re-delivery is
    refused by name: it is never faked as a success, never silently re-routed,
    and never resurrected through a stale wrapper, binding or lease.

var ErrTaskNotFound = errors.New("task: no task with that identity")

    ErrTaskNotFound reports a task identity the registry holds no record of.

var ErrTerminalState = errors.New("task: the task already holds its terminal fact")
    ErrTerminalState reports an attempt to move a task whose terminal fact is
    already stored. A terminal task keeps that one fact and no later signal
    revives it.

FUNCTIONS

func DigestArgs(jsonArgs []byte) string
    DigestArgs computes the argument digest of one call: the lowercase hex
    of the SHA-256 of the exact argument bytes the call carries. The digest
    binds an approval to the arguments that were held, so a response over other
    arguments is refused instead of approving a changed call.

func EncodeTaskRecord(record TaskRecord) ([]byte, error)
    EncodeTaskRecord returns the canonical bytes of one restore link. The record
    is self-contained: the bytes carry the fact beside the admission material
    that belongs to it.

func NewApprovalID() string
    NewApprovalID issues the approval identity of one hold: "approval-" beside
    the lowercase hex of 16 random bytes. tagent issues it when the host parks
    a call in a hold, so one held call is resolved against a globally unique
    identity rather than against a native CallID, which is not globally unique.

func WithCallFrame(ctx context.Context, frame CallFrame) context.Context
    WithCallFrame carries one call frame on the context, so the wrapped call
    that context reaches is admitted under that identity. The dispatch states
    the frame of the decision that issues the call, and the manager states the
    frame it rebuilds from one admission's stored material on a delivery flush;
    those two are the only writers.

TYPES

type AdmissionKind int
    AdmissionKind classifies one admission receipt. A fresh admission adds an
    obligation; a dedup and a rejected admission add none.

const (
	// AdmissionFresh is the receipt of an admission that newly registered the
	// task: it added an obligation, so the admission keeps that obligation open
	// until it is explicitly settled.
	AdmissionFresh AdmissionKind = iota
	// AdmissionDedup is the receipt of a repeated admission of an existing
	// identity. It adds no new obligation, so only this admission's
	// pre-registration is withdrawn and the existing obligations are untouched.
	AdmissionDedup
	// AdmissionRejected is the receipt of an admission that was refused. Like a
	// dedup it adds no new obligation, so only this admission's pre-registration
	// is withdrawn and the existing obligations are untouched.
	AdmissionRejected
)
type AdmissionReceipt struct {
	// CallID is the native provider call identifier the receipt pairs with.
	CallID string
	// TaskID is the task the admitted call was registered as.
	TaskID event.TaskID
	// Accepted reports whether the call was admitted.
	Accepted bool
	// Kind classifies the receipt. A fresh admission keeps the obligation it
	// added open until it is settled; only a dedup or a rejected admission, which
	// add no new obligation, withdraw this admission's pre-registration.
	Kind AdmissionKind
}
    AdmissionReceipt is the single receipt a registration wrapper returns when
    it admits one call. Admission is not a result, so the receipt reports
    only that the call was admitted; the task's outcome arrives later as an
    independent notification.

type ApprovalAnswer struct {
	// ApprovalID is the approval identity of the hold the response resolves.
	ApprovalID string
	// ExpectedDigest is the argument digest the response is bound to: the digest
	// of the arguments that were held.
	ExpectedDigest string
	// Granted reports whether the held call proceeds; false denies it.
	Granted bool
	// Reason is the host's stated ground for the ruling.
	Reason string
}
    ApprovalAnswer is the ruling one approval response carries: the approval
    identity and argument digest the response is bound to, beside whether the
    held call proceeds and the reason the host gave.

type Approver func(ctx context.Context, tool string, jsonArgs []byte) (Ruling, error)
    Approver reports the approval decision one call is admitted under,
    judging the tool name and the native arguments the call carries. An approver
    reporting an error refuses the call closed rather than guessing a decision.

type BlockedObligation struct {
	// Notice is the identity of the notification whose obligation is blocked.
	Notice Notice
	// Reason is the readable reason the obligation cannot proceed.
	Reason string
}
    BlockedObligation is one obligation that cannot proceed, paired with the
    reason it is blocked.

type CallFrame struct {
	// Identity is the call identity a duplicate admission resolves against.
	Identity CallIdentity
	// Address is the address the task belongs to and its notifications target.
	Address event.Address
	// Execution names the serialized execution the task runs under.
	Execution Serialization
}

    CallFrame is the admission identity one wrapped call carries: the call
    identity a duplicate resolves against, the address the task belongs to,
    and the serialized execution the task runs under. The frame travels on
    the context, so a call is admitted under the identity of the decision that
    issued it.

    The frame is the one relationship the locked native tool port leaves
    implicit: `trpctool.CallableTool.Call(ctx, jsonArgs)` has no other channel
    for a per-call identity, so the frame rides the context and CallFrameFrom is
    its single typed accessor. Every reader below reads it through that accessor
    and never through a bare context value; a new reader states the key,
    the type, the accessor and the default in the change's ctx contract table
    before it lands.

func CallFrameFrom(ctx context.Context) (CallFrame, bool)
    CallFrameFrom reports the call frame the context carries and whether one
    is set. It is this seam's single typed accessor: the admission wrapper, the
    run's fact/notice bookkeeping beside the skill tool of the composition root
    and the mailbox's responsibility range all read the frame here rather than
    through a bare context value. A call driven beside no frame reads not-ok,
    and every reader decides its own default — the wrapper refuses the call
    closed with ErrCallFrameMissing, the mailbox reports an empty responsibility
    range.

type CallIdentity struct {
	// InstanceID is the runtime instance the call was admitted in.
	InstanceID event.InstanceID
	// InvocationID is the responsibility range the call belongs to.
	InvocationID event.InvocationID
	// DecisionID is the model decision the call came from.
	DecisionID event.DecisionID
	// CallID is the native provider call identifier, kept byte-identical.
	CallID string
}

    CallIdentity is the identity one native call admission is resolved against:
    the instance, the invocation, the decision and the native call identifier.
    A native CallID is not globally unique, so the same CallID in another
    instance is a different call.

func (c CallIdentity) DedupKey() DedupKey
    DedupKey composes the dedup key of the identity, in the shape Spec.DedupKey
    carries it: instance, invocation, decision and native call identifier.

type CancelReceipt struct {
	// TaskID is the task the request targets.
	TaskID event.TaskID
	// Requested reports whether the cancellation request is recorded.
	Requested bool
}

    CancelReceipt is the control receipt of one cancellation request.
    It reports the request and nothing more: the task ends when the executor
    reports an actual stop through StopEvidence, so the receipt is never read as
    termination.

type Commit interface {
	// Commit stores one record and reports the commit outcome. A committed
	// record may be made visible; an uncertain record is reconciled before any
	// automatic re-run.
	Commit(ctx context.Context, record CommitRecord) (CommitResult, error)
}
    Commit is the port through which a task status transition is stored.
    A registry takes this port at the transition and submits the status change
    and its pending notifications as one record.

    The redline list (4 rules): MUST store the status change and its pending
    notifications as one self-contained record, at the transition. MUST answer
    committed, known-not-committed and uncertain as three separate values. MUST
    NOT make a status or a notification visible before its record is committed.
    MUST NOT repeat a side effect of an uncertain commit before that commit is
    reconciled.

type CommitRecord struct {
	// MutationID identifies this mutation for idempotent commit.
	MutationID MutationID
	// TaskID is the task whose status this mutation changes.
	TaskID event.TaskID
	// ExpectedRevision is the revision the mutation expects to apply to.
	ExpectedRevision int64
	// Status is the status the task moves to.
	Status Status
	// Outbox lists the notifications the transition intends to send, each as a
	// self-contained envelope with a stable identity and the target, source and
	// target instance frozen at this transition, so a repeated delivery does not
	// record a second fact and a retry never re-resolves a target.
	Outbox []Envelope
}
    CommitRecord is the self-contained unit a commit port stores: the expected
    revision it applies to, the status change it carries, and the notifications
    the transition intends to send, each with the delivery routing it froze.

type CommitResult int
    CommitResult is the outcome a commit port reports for one CommitRecord. The
    three outcomes are reported separately and are never merged into one value.

const (
	// CommitUncertain is the zero value and the conservative outcome: a commit
	// that was not observed is treated as unknown, never as stored. It is
	// surfaced and reconciled before any automatic re-run.
	CommitUncertain CommitResult = iota
	// CommitCommitted is a commit that is durably stored. Only a committed
	// record may have its status and notifications made visible.
	CommitCommitted
	// CommitNotCommitted is a commit known not to have been stored.
	CommitNotCommitted
)
type DedupKey string
    DedupKey is the identity a duplicate admission is resolved against.
    The runtime composes it from the instance, invocation, decision and native
    call identifiers, because a native CallID is not globally unique; an empty
    key disables deduplication.

type Deliverer func(ctx context.Context, notice Notice) (DeliveryAcceptance, error)

    Deliverer reports the delivery-layer outcome of one notification against its
    target. Only an acceptance the target states explicitly advances a delivery.

type DeliveryAcceptance int
    DeliveryAcceptance is the delivery-layer outcome of one notification:
    whether the target accepted the delivery. It is the third result beside the
    admission receipt and the commit result, and the three are never merged into
    one value.

const (
	// DeliveryPending is the zero value: no target acceptance has been observed.
	DeliveryPending DeliveryAcceptance = iota
	// DeliveryAccepted is a delivery the target explicitly accepted.
	DeliveryAccepted
	// DeliveryRejected is a delivery the target refused.
	DeliveryRejected
)
type Denied struct {
	// Tool is the name of the tool the refusal names.
	Tool string
	// Reason is the readable reason the approval refused the call.
	Reason string
}
    Denied is the final refusal of one call: the call executed nothing and the
    refusal names the tool it turns away. The refusal is a terminal fact of its
    own, and the named paired result beside it carries the corrective material
    the model reads.

func (d Denied) PairedResult(callID string) Result
    PairedResult returns the named paired result of one refusal: the result
    record naming the refused tool and pairing with the native call, so the
    refusal reaches the model as corrective material beside the native error
    response.

type Envelope struct {
	// Notice is the stable identity of the notification.
	Notice Notice
	// Target is the address the notification is delivered to, frozen at the
	// transition that committed it.
	Target event.Address
	// Source is the address the notification comes from, frozen beside the
	// target; the source and the target are kept apart.
	Source event.Address
	// TargetInstance is the runtime instance that receives the notification,
	// frozen beside the target address, so a rebuilt or handed-off instance never
	// re-routes a committed notification.
	TargetInstance event.InstanceID
}
    Envelope is the self-contained delivery unit of one notification: the stable
    notice identity beside the delivery routing frozen when the transition
    that carried it was committed. A retry delivers by the envelope it carries
    and never resolves a current target: after a restart or a handoff,
    a committed notification keeps the target it was frozen under, and only the
    notifications created after a handoff freeze the new target.

type EnvelopeDeliverer func(ctx context.Context, envelope Envelope) (DeliveryAcceptance, error)

    EnvelopeDeliverer reports the delivery-layer outcome of one notification
    delivered beside the routing its envelope froze. A manager attached to one
    offers every notification by its own envelope, so a retry repeats the frozen
    routing and no delivery ever resolves a current target.

type HandoffRecord struct {
	// TaskID is the task whose notifications the handoff moves.
	TaskID event.TaskID
	// PreviousTarget is the target the task's notifications moved from: the
	// target already in effect when this handoff was recorded.
	PreviousTarget event.Address
	// SourceInstance is the instance the task was launched under. It stays the
	// owner of every notification created before the handoff.
	SourceInstance event.InstanceID
	// Target is the address the future notifications are delivered to.
	Target event.Address
	// TargetInstance is the instance that receives and owns the future
	// notifications.
	TargetInstance event.InstanceID
}

    HandoffRecord is the recorded link of one explicit handoff: the previous
    target the task's notifications moved from beside the new target they
    move to, with the source instance and the target instance kept apart.
    The notifications created before the handoff stay claimed by the source
    instance and keep the routing their envelopes froze; only the notifications
    created after the handoff freeze the new target and are claimed by the
    target instance. One notification is claimed by exactly one instance and is
    never recorded twice.

type HandoffTarget struct {
	// Target is the address future notifications are delivered to.
	Target event.Address
	// TargetInstance is the instance that receives the future notifications.
	TargetInstance event.InstanceID
}
    HandoffTarget names where a task's future notifications are delivered
    after an explicit handoff: the receiving address and the target instance.
    The source instance and the target instance are kept apart.

type Hold struct {
	// TaskID is the task registered beside the wait; the task holds no execution.
	TaskID event.TaskID
	// Reason is the readable reason the approval is waiting.
	Reason string
	// ApprovalID is the approval ticket identity this hold was issued under:
	// tagent issues it when the host parks the call, and it is globally unique.
	// A host response resolves the hold against that identity, never against a
	// native CallID that is not globally unique.
	ApprovalID string
	// ExpectedDigest is the digest of the call arguments this hold was issued
	// over, computed by DigestArgs. A response whose digest names other
	// arguments is refused beside ErrApprovalDigestMismatch instead of
	// approving a changed call.
	ExpectedDigest string
}

    Hold is the waiting state one approval decision leaves behind: the call is
    registered as a task that executed nothing and the caller receives a waiting
    receipt. The state is a wait, so a query observes it as waiting and reads it
    as no final answer.

type HoldBook struct {
	// Has unexported fields.
}
    HoldBook is the waiting table of the approval channel: it records the hold
    of one parked task beside the first approval response that hold received,
    and answers a query with that hold while the wait is open. The book consumes
    one response per hold and observes waits otherwise, so a query resolves and
    finalizes nothing.

func NewHoldBook() *HoldBook
    NewHoldBook returns an empty waiting table of the approval channel.

func (b *HoldBook) Record(hold Hold)
    Record stores the waiting state of one parked task. A repeated record of the
    same task keeps the waiting state already stored.

func (b *HoldBook) Resolve(taskID event.TaskID, answer ApprovalAnswer) (ApprovalAnswer, error)
    Resolve consumes one approval response for the hold of the named task and
    reports the ruling that took effect. The response is bound to the hold it
    resolves: answer.ApprovalID must name the approval identity issued when
    that call was parked, and answer.ExpectedDigest must match the digest of
    the arguments that were held. A response that matches neither is refused
    beside ErrApprovalIdentityMismatch or ErrApprovalDigestMismatch and is
    never applied to another call or to the same call over changed arguments.
    A repeated response under the same approval identity is idempotent: the
    first response takes effect and the repeat reports that first ruling without
    consuming a second one.

func (b *HoldBook) Waiting(taskID event.TaskID) (Hold, bool)
    Waiting reports the waiting state of one parked task and whether the task is
    held at all. The query reports the open wait and moves nothing.

type HoldReceipt struct {
	// CallID is the native provider call identifier the receipt pairs with.
	CallID string
	// Hold is the waiting state the call was parked in.
	Hold Hold
}
    HoldReceipt is the waiting receipt of one call parked in the approval
    channel. It answers the call with its wait and nothing more: the task it
    names executed nothing, and the receipt is read as no final refusal.

type LogicalChange struct {
	// TaskID is the task the change belongs to.
	TaskID event.TaskID
	// Status is the status the machine holds, unchanged by the change.
	Status Status
	// From is the logical status the task was last reported under.
	From LogicalStatus
	// To is the logical status the task is reported under now.
	To LogicalStatus
	// Since is the instant the change was identified at.
	Since time.Time
}
    LogicalChange is one identified logical status change of one task: the task
    it belongs to, the status the machine holds beside both logical statuses of
    the change and the instant it was identified at.

type LogicalStatus string
    LogicalStatus is the classification of a task by how long its recorded state
    has stood unchanged. A task keeps the status its transitions committed;
    this classification names the silence that state was read under, so a stable
    or a suspected task is reported as such without any status of it moving.

const (
	// LogicalActive is the zero value: the task's state moved within the watch
	// threshold of its policy, or its movement carries no stamp, so no silence
	// is identified.
	LogicalActive LogicalStatus = ""
	// LogicalWatch is a task whose state has stood unchanged for the watch
	// threshold: the silence is observed and nothing is judged of it.
	LogicalWatch LogicalStatus = "watch"
	// LogicalStable is a task whose state has stood unchanged for the stable
	// threshold: the silence itself is the report, and neither the status nor
	// the obligations of the task change with it.
	LogicalStable LogicalStatus = "stable"
	// LogicalSuspect is a task whose state has stood unchanged for the suspect
	// threshold: the task is reported as holding no live execution of its own.
	LogicalSuspect LogicalStatus = "suspect"
	// LogicalTimedOut is a task whose execution has run past the execution
	// timeout of its policy: the caller stops holding the wait for that
	// execution, and its result arrives as a notification when it settles.
	LogicalTimedOut LogicalStatus = "timed-out"
	// LogicalExpired is a task that stayed unsettled past the time to live of
	// its policy.
	LogicalExpired LogicalStatus = "expired"
)
type Manager interface {
	// Admit accepts one task declaration and returns the admission receipt.
	Admit(ctx context.Context, spec Spec) (AdmissionReceipt, error)

	// Obligations reports the manager's outstanding obligations for a task,
	// classified by kind.
	Obligations(ctx context.Context, taskID event.TaskID) (Obligations, error)

	// Handoff moves a task's future notifications to the target address and
	// target instance, keeping the source and target instances distinct.
	Handoff(ctx context.Context, taskID event.TaskID, target HandoffTarget) error

	// Flush delivers the notifications of a task whose obligations are still
	// open.
	Flush(ctx context.Context, taskID event.TaskID) error
}
    Manager is the task admission and settlement face: it admits a task, reports
    the obligations it still holds, hands a task's future notifications to
    another target, and flushes notifications whose obligations are still open.

    The redline list (4 rules): MUST keep the obligation an admission opened
    until an outcome explicitly settles it. MUST report each outstanding
    obligation by kind, with the reason a blocked one cannot proceed. MUST NOT
    read a successful admission as a settled obligation. MUST NOT re-resolve the
    target a committed notification froze at its transition.

type ManagerOption func(*MemoryManager)
    ManagerOption configures an in-memory task manager at construction.

func WithEnvelopeDeliverer(deliver EnvelopeDeliverer) ManagerOption
    WithEnvelopeDeliverer attaches the deliverer a manager offers its
    notifications to beside the routing each notification's envelope froze.
    It reports the same three delivery outcomes as a plain deliverer and changes
    no obligation bookkeeping.

type MemoryAdmission struct {
	// Has unexported fields.
}
    MemoryAdmission is the in-memory admission face: it returns the single
    receipt of one call admission and resolves a repeated admission against the
    dedup key of the call identity. The first admission of a key registers its
    task as a fresh admission; a repeated admission of that key reports the task
    the first one registered and adds no obligation.

func NewMemoryAdmission() *MemoryAdmission
    NewMemoryAdmission returns an empty in-memory admission face.

func (m *MemoryAdmission) Admit(identity CallIdentity, taskID event.TaskID) AdmissionReceipt
    Admit returns the single receipt of one call admission. A fresh admission
    registers taskID and keeps its obligation open; a repeated admission of
    the same dedup key returns the task of the first admission as a dedup,
    which adds no obligation. Admission is not a result, so the receipt reports
    only that the call was admitted.

func (m *MemoryAdmission) AdmitKey(key DedupKey, callID string, taskID event.TaskID) AdmissionReceipt
    AdmitKey returns the single receipt of one call admission resolved against
    a dedup key the caller composed. A fresh admission registers taskID and
    keeps its obligation open; a repeated admission of that key reports the task
    of the first admission as a dedup, which adds no obligation. An empty key
    disables deduplication, so every admission under it is fresh.

func (m *MemoryAdmission) Lookup(key DedupKey) (event.TaskID, bool)
    Lookup reports the task a repeated admission of this dedup key resolves to,
    and whether the key has been admitted at all.

type MemoryCommit struct {
	// Has unexported fields.
}
    MemoryCommit is the in-memory carrier behind the Commit port: it stores
    one self-contained record per mutation identity, keeps the revision each
    record advanced, and holds every committed notification as an open delivery
    obligation. The memory carrier keeps the business contract of a durable one
    and promises no recovery of its own.

func NewMemoryCommit(uncertain UncertainProbe) *MemoryCommit
    NewMemoryCommit returns an empty in-memory commit carrier. uncertain,
    when it is not nil, marks matching commit attempts as uncertain, so all
    three commit outcomes stay observable.

func (m *MemoryCommit) Commit(ctx context.Context, record CommitRecord) (CommitResult, error)
    Commit implements the commit port. A record of a mutation identity the
    carrier already stores returns the fact already stored and records no second
    fact; the same identity with different content reports CommitNotCommitted
    beside a named error, as does a record expecting another revision.
    An uncertain attempt reports CommitUncertain and stores nothing until that
    record is submitted once more.

func (m *MemoryCommit) Facts(taskID event.TaskID) []CommitRecord
    Facts returns the records the carrier stored for one task, in commit order.

func (m *MemoryCommit) Outbox(taskID event.TaskID) []OutboxNotice
    Outbox returns the notifications one task still owes delivery, each paired
    with its delivery-layer state. A commit never moves a delivery state,
    so an idempotent repeat of a committed record leaves its notifications owed.

func (m *MemoryCommit) Revision(taskID event.TaskID) int64
    Revision returns the revision the task is at after the records stored for
    it.

type MemoryManager struct {
	// Has unexported fields.
}
    MemoryManager is the in-memory task manager: it admits a task, reports the
    obligations it still holds for one, hands a task's future notifications
    to another target, and flushes the notifications a task still owes.
    It satisfies Manager and records what its faces report without inferring
    outcomes of its own.

func NewMemoryManager(registry *MemoryRegistry, deliver Deliverer, opts ...ManagerOption) *MemoryManager
    NewMemoryManager returns an in-memory task manager over one registry.
    deliver reports the delivery-layer outcome of a notification and may be nil,
    in which case a flush refuses rather than dropping an obligation. Options
    configure the manager further, beside the deliverer.

func (m *MemoryManager) Admit(ctx context.Context, spec Spec) (AdmissionReceipt, error)
    Admit accepts one task declaration and returns the admission receipt.
    Admission is not a result and does not clear the task's obligation.

func (m *MemoryManager) CloseInstance(ctx context.Context, instance event.InstanceID) error
    CloseInstance closes one runtime instance. The notifications the closed
    instance still owes delivery on are retained beside their frozen routing
    and carried by the recovery path (Retained and RecoveryMaterial):
    a flush refuses them by name as ErrInstanceClosed rather than dropping them
    or delivering them into a later instance that reuses the address. Closing
    an instance that holds nothing, or closing one twice, records nothing and
    reports no error.

func (m *MemoryManager) Flush(ctx context.Context, taskID event.TaskID) error
    Flush delivers the notifications of a task whose obligations are still open,
    each beside the routing its envelope froze, and retries the ones no target
    has answered yet — including the ones a handoff moved beside the task. Only
    an acceptance the target states explicitly clears an obligation; a refusal
    keeps the obligation and records the reason beside that notification.
    A notification whose frozen receiving instance is closed is retained by
    name as ErrInstanceClosed: it is neither dropped nor delivered into a later
    instance that reuses the address.

func (m *MemoryManager) Handoff(ctx context.Context, taskID event.TaskID, target HandoffTarget) error
    Handoff moves a task's future notifications to the target address and the
    target instance, keeping the source and target instances distinct, and
    records the handoff link beside the task as one durable mutation of its fact
    chain, so a registry rebuilt from that chain applies the same move and a
    restarted task routes its future notifications to the new target rather than
    the one its declaration named. A notification whose target already answered
    keeps its old target and its identity; a notification with no answer yet has
    only its open delivery obligation reclassified as handed-off bookkeeping —
    its frozen routing never moves. Every notification already created keeps
    the routing its envelope froze: a retry of one delivers beside that frozen
    routing and re-resolves no current target. A repeat of the handoff already
    recorded applies nothing a second time.

func (m *MemoryManager) HandoffOf(ctx context.Context, taskID event.TaskID) (HandoffRecord, error)
    HandoffOf reports the recorded handoff link of one task: the previous target
    its notifications moved from beside the new target they move to, with the
    source and target instance identities kept apart. A task no explicit handoff
    moved reports ErrNoHandoff.

func (m *MemoryManager) Obligations(ctx context.Context, taskID event.TaskID) (Obligations, error)
    Obligations reports the obligations the manager still holds for a task,
    each entry classified by the wait it is in beside the task and notification
    it points at.

func (m *MemoryManager) RecordStop(ctx context.Context, evidence StopEvidence) error
    RecordStop records the executor's stop evidence beside one task. A report
    that confirms no stop is recorded and moves no status; a confirmed stop of a
    task under a cancellation request stores the cancelled terminal fact beside
    its pending notification and closes the task's execution obligation with the
    reported reason.

func (m *MemoryManager) RecoveryMaterial(ctx context.Context, instance event.InstanceID) ([]TaskRecord, error)
    RecoveryMaterial returns the durable fact chain covering the tasks whose
    notifications the named instance still owes, one chain per task in commit
    order with the admission material on its first link. Fed to Restore,
    the chain re-opens the obligations it carries: the undelivered notifications
    of a closed instance are kept by the recovery path either way, never dropped
    with the instance that owned them.

func (m *MemoryManager) Retained(ctx context.Context, instance event.InstanceID) ([]Envelope, error)
    Retained reports the notifications one instance still owes delivery on, each
    beside the routing its envelope froze, ordered by the stable notification
    identity. This is what CloseInstance keeps and what RecoveryMaterial
    carries: a retained notification is never dropped and never delivered
    elsewhere.

type MemoryRegistry struct {
	// Has unexported fields.
}
    MemoryRegistry is the in-memory task registry: it admits a task declaration,
    moves a task through the state machine on the commit carrier, and answers
    lookups, cancellation requests and resume declarations. It satisfies
    Registry and holds no recovery of its own.

func NewMemoryRegistry(carrier Commit, admission *MemoryAdmission, opts ...RegistryOption) *MemoryRegistry
    NewMemoryRegistry returns an in-memory task registry over one commit carrier
    and one admission face. Each status change is submitted to the carrier
    before the registry reports it, so a change the carrier did not store is
    never made visible.

func (r *MemoryRegistry) Advance(ctx context.Context, taskID event.TaskID, to Status, mutation MutationID, outbox []Envelope) (CommitResult, error)
    Advance moves one task to the next status and submits the change beside
    its pending notifications to the commit carrier first. A repeated mutation
    returns the fact already stored and records no second one; a change the
    carrier refuses leaves the task where it is; a task that holds its terminal
    fact moves no more.

func (r *MemoryRegistry) Cancel(ctx context.Context, taskID event.TaskID) error
    Cancel requests cancellation of the named task only; it does not cancel
    other invocations of the same session.

func (r *MemoryRegistry) IssueNotice(ctx context.Context, taskID event.TaskID, kind NoticeKind, fact event.EventID) (Envelope, error)
    IssueNotice allocates the stable identity of one notification of the named
    task and freezes its delivery routing into the envelope it returns: the
    target, source and target instance of the declaration the task was admitted
    under, or of the explicit handoff already applied. A handoff touches only
    the notifications issued after it, so the envelopes already issued keep the
    routing they froze and a retry of one never re-resolves a current target.

func (r *MemoryRegistry) Logical(ctx context.Context, taskID event.TaskID, at time.Time) (LogicalStatus, error)
    Logical reports the logical status of one task at the given instant: the
    silence its recorded state has stood in, read beside the status machine. The
    status, revision, mutations and obligations of the task stay as they stand.

func (r *MemoryRegistry) Lookup(ctx context.Context, taskID event.TaskID) (Spec, Status, error)
    Lookup returns the declaration and status of the task with the given
    identity.

func (r *MemoryRegistry) Register(ctx context.Context, spec Spec) (AdmissionReceipt, error)
    Register admits one task declaration and returns its admission receipt.
    A repeated admission of the same dedup key reports the task of the
    first admission as a dedup and adds no obligation; an admission past
    the concurrency limit is refused by name and leaves every admitted task
    untouched. A fresh admission keeps its pre-registered obligation open until
    the task settles. The admission material of a fresh task is stored beside
    its first commit through the carrier's admission-material face when the
    carrier offers one, so the stored chain of the task is self-contained and a
    restore needs no external declaration.

func (r *MemoryRegistry) RequestCancel(ctx context.Context, taskID event.TaskID) (CancelReceipt, error)
    RequestCancel records one cancellation request and returns its control
    receipt. A repeated request returns the same receipt and records no second
    one; a task that already holds its terminal fact refuses the request.

func (r *MemoryRegistry) Restore(ctx context.Context, chain []TaskRecord, owners OwnerBinding) (RestoreReport, error)
    Restore rebuilds the registry from one durable fact chain and admits each
    rebuilt task idempotently: a link the registry already folded adds no second
    fact and opens no second obligation, so a repeated restore of the same
    chain changes nothing. Each task's target is resolved against the current
    orchestration binding through owners — never through a top-level singleton
    — and a target the binding refuses keeps its whole task chain and every
    obligation open while the re-delivery is refused by name.

    The restore retains no wrapper, binding or lease: the chain and the owner
    binding are supplied per call and nothing captured by an earlier run is
    revived through it.

func (r *MemoryRegistry) Resume(ctx context.Context, taskID event.TaskID, input event.Event) (AdmissionReceipt, error)
    Resume feeds one declared input into an existing task and re-enters the
    standard lifecycle under the same task identity. A round already in
    flight refuses the declaration, and so does a cancelled task. A repeated
    declaration of the same input returns the fact already stored and opens no
    second obligation.

func (r *MemoryRegistry) Watch(ctx context.Context, at time.Time) []LogicalChange
    Watch identifies the logical status changes of every task the registry holds
    non-terminal at the given instant, in task identity order. Each change is
    reported once: a task already reported under a logical status is reported
    again only when that status moves. The report reads the silence of the tasks
    it covers and moves no status, revision or obligation of any of them.

type MutationID string

    MutationID identifies one task state mutation for idempotent commit.
    A repeated commit of the same mutation does not record a second fact,
    and a commit that reuses an identity with different content is rejected.

type Notice struct {
	// TaskID is the task the notification belongs to.
	TaskID event.TaskID
	// Round is the execution round the notification belongs to; one round may
	// produce several notifications.
	Round int64
	// Kind is the notification category within the round.
	Kind NoticeKind
	// Seq is the stable sequence number of the notification within its kind, so
	// repeated transport of one notification collapses to a single delivery.
	Seq int64
	// Event is the identity of the already-committed fact this notification
	// points at. A consumer restores the input through that fact, while the
	// notification's own identity stays TaskID+Round+Kind+Seq and the fact
	// reference never replaces it.
	Event event.EventID
	// Text is the result material a terminal notification carries beside its
	// identity: the body the settled call produced, bounded by the settle that
	// carries it. It is not part of the notification identity — FactKey reads
	// TaskID+Round+Kind+Seq alone — so a retry carrying the same body keeps the
	// fact key of the first delivery.
	Text string
}

    Notice is the stable identity of one notification a task intends to deliver.
    A repeated delivery of the same identity does not record a second fact,
    and a retry keeps the original identity.

func (n Notice) FactKey() string
    FactKey returns the notice's stable identity as a fact key: the four-tuple
    TaskID+Round+Kind+Seq the notice's own identity stays at, synthesised into
    the one canonical spelling "%s:%d:%s:%d". The event reference the notice
    points at never replaces that identity, so a retried delivery answers the
    same key. The runtime mints task identities as "task-<n>" and every kind is
    one of the closed NoticeKind words, so the colon join stays injective over
    the notices this package produces.

func (n Notice) FactType() string
    FactType returns the notification category as the fact type: the category is
    part of the notice's own identity, so a repeated delivery keeps its type.

type NoticeKind string
    NoticeKind identifies the category of a notification within an execution
    round.

    A round may produce an accepted, a progress and a settled notification,
    and a terminal round carries its polarity as a failed or a cancelled one;
    the category is part of each notification's own identity and a retry keeps
    it.

const (
	// NoticeAccepted reports that the round admitted the input it carries.
	NoticeAccepted NoticeKind = "accepted"
	// NoticeProgress reports in-flight progress of a running round.
	NoticeProgress NoticeKind = "progress"
	// NoticeSettled reports the terminal outcome the round settled on.
	NoticeSettled NoticeKind = "settled"
	// NoticeFailed reports a round whose execution ended in failure.
	NoticeFailed NoticeKind = "failed"
	// NoticeCancelled reports a round cancelled by its caller.
	NoticeCancelled NoticeKind = "cancelled"
)
type NoticeSequencer struct {
	// Has unexported fields.
}

    NoticeSequencer hands out the stable sequence numbers that complete a
    notification's identity within one category of one execution round. The
    first notification of a category takes sequence 0 and each later one takes
    the next, so every notification keeps its own identity: a repeated transport
    is recognized as the same notification and records no second fact.

func NewNoticeSequencer() *NoticeSequencer
    NewNoticeSequencer returns a sequencer that has handed out nothing yet.

func (s *NoticeSequencer) Issue(taskID event.TaskID, round int64, kind NoticeKind, fact event.EventID) Notice
    Issue returns the next notification identity in the given category of one
    execution round, pointing at the fact it reports. The sequence number is
    stable: the identity it returns is the identity a retry keeps.

func (s *NoticeSequencer) Observe(notice Notice)
    Observe records an identity that was already issued — read back from a fact
    chain, for instance — so the sequencer continues past its sequence number
    and never hands that identity out a second time.

type NoticeSink func(ctx context.Context, notice Notice) error
    NoticeSink admits one notification into the mailbox of its target. A nil
    error states the acceptance of the target, the only answer that clears the
    delivery obligation of that notification.

type Obligation struct {
	// Kind is the class the obligation waits in.
	Kind ObligationKind
	// TaskID is the task the obligation belongs to.
	TaskID event.TaskID
	// Notice is the notification the obligation points at; its zero value marks an
	// obligation that points at the task itself.
	Notice Notice
}
    Obligation is one obligation a manager still holds: the class it waits in
    beside the task and the notification it points at.

type ObligationKind string
    ObligationKind classifies one open obligation by the wait it is held under.

const (
	// ObligationAwaitingTool is an obligation whose task has not settled: the
	// admission that opened it stays owed until an outcome is explicitly settled.
	ObligationAwaitingTool ObligationKind = "awaiting-tool"
	// ObligationAwaitingDelivery is an obligation whose committed notification has
	// no acceptance from its target yet.
	ObligationAwaitingDelivery ObligationKind = "awaiting-delivery"
	// ObligationHandedOff is an obligation whose notification moves to the target
	// instance an explicit handoff named.
	ObligationHandedOff ObligationKind = "handed-off"
)
type Obligations struct {
	// Count is the number of obligations still open.
	Count int
	// Owner is the invocation that owns the open obligations.
	Owner event.InvocationID
	// Blocked lists the obligations that cannot proceed, each with its reason.
	Blocked []BlockedObligation
	// ExitReason is the reason an obligation ended without settlement; it is
	// empty while no obligation has exited.
	ExitReason string
	// Logical is the logical status the task is read under at the instant of the
	// report: the silence its recorded state has stood in, beside the status the
	// machine holds. It states which silence the task is in and moves nothing.
	Logical LogicalStatus
	// Open lists the obligations still open as the classified detail, one entry
	// per obligation with the class it waits in.
	Open []Obligation
}
    Obligations classifies the obligations a manager still holds for a task.
    An obligation stays open from admission until it is explicitly cleared,
    so a successful admission keeps its pre-registered obligation open.

type OutboxNotice struct {
	// Notice is the self-contained notification owed: its stable identity beside
	// the delivery routing frozen at the transition that committed it.
	Notice Envelope
	// Delivery is the delivery-layer state of the notification. The storage
	// layer leaves every notification pending: only an explicit acceptance by
	// the target moves it.
	Delivery DeliveryAcceptance
}
    OutboxNotice is one notification a committed record still owes delivery,
    paired with the delivery-layer state of that notification. The envelope it
    carries keeps the routing frozen at the transition that committed it.

type OwnerBinding interface {
	// OwnerOf reports the owner that target belongs to under the current
	// orchestration binding, within the tool set of the named owner. A target
	// the owner does not hold is refused by name.
	OwnerOf(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error)
}
    OwnerBinding resolves the owner a restored target belongs to under the
    current orchestration binding. The resolution reads the tool set the current
    binding holds for the owner — never a top-level singleton — and refuses a
    target the owner does not hold. Its signature carries built-in and event
    types only, so the state layer's binding owners satisfy it structurally and
    no package edge is required in either direction.

    The redline list (3 rules): MUST resolve the owner from the tool set the
    current binding holds for that owner. MUST refuse a target the owner does
    not hold, by name. MUST NOT resolve an owner from a top-level singleton or
    from a most-recent target.

type Policy struct {
	// WatchAfter is how long a task's state may stand unchanged before that
	// silence is observed.
	WatchAfter time.Duration
	// StableAfter is how long it may stand unchanged before the silence is
	// reported as the stable state of the task.
	StableAfter time.Duration
	// SuspectAfter is how long it may stand unchanged before the task is
	// reported as holding no live execution.
	SuspectAfter time.Duration
	// ExecutionTimeout is how long one round of execution may run before the
	// caller stops holding the wait for its result.
	ExecutionTimeout time.Duration
	// TTL is how long a task may stay unsettled after its admission.
	TTL time.Duration
}
    Policy is the threshold set one task's logical status is read under. A zero
    threshold places no bound on the threshold it names, so a policy states only
    the silences it wants identified and the zero policy identifies none.

func PolicyNamed(name string) (Policy, bool)
    PolicyNamed reports the threshold set the declared task policy name selects
    and whether the name is one this table declares. The empty name selects
    DefaultPolicy; a name outside the table is reported as unknown rather than
    read as the default, so a misspelled declaration is visible where it is
    resolved.

type Registry interface {
	// Register admits one task declaration and returns the admission receipt.
	// Admission is not a result and does not clear the task's obligation.
	Register(ctx context.Context, spec Spec) (AdmissionReceipt, error)

	// Lookup returns the declaration and status of the task with the given
	// identity.
	Lookup(ctx context.Context, taskID event.TaskID) (Spec, Status, error)

	// Cancel requests cancellation of the named task only; it does not cancel
	// other invocations of the same session.
	Cancel(ctx context.Context, taskID event.TaskID) error

	// Resume feeds new input into an existing task and re-enters the task's
	// standard lifecycle under the same task identity.
	Resume(ctx context.Context, taskID event.TaskID, input event.Event) (AdmissionReceipt, error)
}
    Registry is the task table through which a task is registered, looked up,
    cancelled and resumed. A registry carries a status transition on the commit
    port, so a transition is stored before it becomes visible.

    The redline list (4 rules): MUST store a transition on the commit port
    before it becomes visible. MUST recognise a repeated mutation identity as
    the same mutation. MUST NOT store a mutation identity that carries different
    content. MUST NOT cancel other invocations of the same session when one task
    is cancelled.

type RegistryOption func(*MemoryRegistry)
    RegistryOption configures an in-memory task registry at construction.

func WithConcurrencyLimit(limit int) RegistryOption
    WithConcurrencyLimit caps how many tasks the registry holds non-terminal at
    once. An admission past that limit is refused by name, and the tasks already
    admitted are kept.

func WithPolicy(policy Policy) RegistryOption
    WithPolicy sets the threshold set every task of the registry is read under.
    A registry built beside no policy reads its tasks under DefaultPolicy.

type RestoreOutcome struct {
	// TaskID is the task the outcome is about.
	TaskID event.TaskID
	// Status is the status the task holds after the fold.
	Status Status
	// Owner is the owner the current binding resolved the task's target to; it
	// is empty when the resolution refused.
	Owner event.AgentID
	// Recovered reports whether this restore rebuilt the task from the chain.
	// False reports an idempotent hit: the registry already held the task and
	// the link added no second fact — or a chain the restore refused without
	// rebuilding anything.
	Recovered bool
	// Refused is the named refusal of this restore, set when the current
	// binding resolves no owner for the task's target or when the chain carries
	// no admission material. The task chain and its obligations are kept when
	// the task itself was rebuilt; errors.Is reports task.ErrTargetRemoved for a
	// target the binding removed and task.ErrSpecMissing for a chain carrying no
	// admission material, which rebuilds nothing and reports no recovery.
	Refused error
}
    RestoreOutcome is the per-task result of one restore: the status the task
    was rebuilt at, the owner the current binding resolved its target to,
    and the named refusal of a re-delivery the binding refused.

type RestoreReport struct {
	// Tasks lists the per-task outcomes of the restore.
	Tasks []RestoreOutcome
}
    RestoreReport is the report one restore returns, one outcome per task the
    chain covers, in first-appearance order.

type Result struct {
	// CallID is the native provider call identifier the result pairs with.
	CallID string
	// Tool is the name of the tool the result belongs to.
	Tool string
	// TaskID is the task the result settles; a refusal registers no task and
	// carries none here.
	TaskID event.TaskID
	// Output is the tool output the wrapped tool returned.
	Output any
	// Err is the tool error the wrapped tool returned.
	Err error
	// Denied is the named paired result of a refusal; it is nil for a result the
	// execution produced.
	Denied *Denied
}
    Result is the recorded outcome of one wrapped call: the tool output or the
    tool error beside the named paired result of a refusal. It is the body the
    background result backflow carries, and the call's own answer never carries
    it a second time.

type ResultRecorder func(ctx context.Context, result Result) (event.EventID, error)
    ResultRecorder records one result as a fact and returns the identity of
    that fact. The recorder is the injected face onto the event storage of the
    runtime, and the wrapper holds no store of its own.

type Ruling struct {
	// Kind is the class the decision falls in.
	Kind RulingKind
	// Reason is the readable reason the approver reports beside the decision.
	Reason string
}
    Ruling is the approval decision over one call: the class it falls in beside
    the reason the approver reports.

type RulingKind string
    RulingKind is the class one approval decision falls in.

const (
	// RulingRun is a ruling that admits the call for execution.
	RulingRun RulingKind = "run"
	// RulingHold is a ruling that parks the call in the approval wait.
	RulingHold RulingKind = "hold"
	// RulingDeny is a ruling that refuses the call before any execution.
	RulingDeny RulingKind = "deny"
)
type Serialization string
    Serialization names the serialized-execution declaration a task was admitted
    under. Serialized execution admits at most one effective model decision per
    session at a time and queues the rest; the declaration names the shape the
    queueing follows.

type Spec struct {
	// CallID is the native provider call identifier, kept byte-identical and
	// never regenerated.
	CallID string
	// DedupKey is the identity a duplicate admission is resolved against: a
	// repeated admission of the same key returns the existing task rather than a
	// copy.
	DedupKey DedupKey
	// InvocationID scopes the task to one responsibility range.
	InvocationID event.InvocationID
	// InstanceID is the runtime instance the task belongs to.
	InstanceID event.InstanceID
	// Description is the short human-readable summary carried into the task's
	// notifications.
	Description string
	// Source is the address the task was admitted from.
	Source event.Address
	// Target is the address the task's notifications are delivered to.
	Target event.Address
	// TriggerSource is the trigger-source stamp the admission carries, recorded
	// on the task_spawned material. A delegated call carries
	// event.LineageDelegation; an empty stamp records no source, so an
	// admission that names none behaves exactly as it did before the stamp
	// existed.
	TriggerSource string
	// Execution references the serialized-execution declaration the task runs
	// under.
	Execution Serialization
}
    Spec is the declaration a task is admitted under. It names the native call
    the task answers, the identity a duplicate admission is resolved against,
    the addresses the task belongs to, and the execution shape the task runs in.

type Status int
    Status is the state a task moves through from admission to settlement.
    The zero value is StatusUnknown, so a task whose status was never set is
    read as unknown rather than as accepted.

const (
	// StatusUnknown is the zero value: the task's status is not determinable. It
	// is surfaced rather than downgraded to a settled outcome.
	StatusUnknown Status = iota
	// StatusAccepted is a task the registry admitted. Admission is not a result;
	// the task's outcome arrives later.
	StatusAccepted
	// StatusRunning is a task whose execution is under way.
	StatusRunning
	// StatusProgress reports in-flight progress of a running task. It is not a
	// terminal status.
	StatusProgress
	// StatusSettled is a task whose terminal outcome has been committed.
	StatusSettled
	// StatusFailed is a task whose execution ended in failure.
	StatusFailed
	// StatusCancelled is a task cancelled by its caller; cancelling one
	// invocation does not cancel other invocations of the same session.
	StatusCancelled
	// StatusAwaitingApproval is a task whose call is parked in the approval wait:
	// the call is registered and executed nothing yet, and the task reports the
	// wait rather than a final answer. The wait is execution-phase information
	// and never occupies a decision slot: it consumes no DecisionID and the
	// decision-stream semantics are unchanged by it.
	StatusAwaitingApproval
)
type StopEvidence struct {
	// TaskID is the task the report is about.
	TaskID event.TaskID
	// Stopped reports whether the execution actually stopped. A report that does not
	// confirm a stop moves no status.
	Stopped bool
	// Reason is the readable reason the executor observed.
	Reason string
}
    StopEvidence is the executor's report about actually stopping one task.
    The manager records the report and infers no stop of its own.

type TaskRecord struct {
	// Spec is the admission material of the task (the task_spawned record). It
	// is carried by the admission link alone and is zero on every later link.
	Spec Spec
	// Fact is the self-contained commit fact this link stores, in exactly the
	// 02.2 commit-port shape.
	Fact CommitRecord
}

    TaskRecord is one link of the durable fact chain a cold start rebuilds a
    task registry from: the commit fact beside the admission material of the
    task it belongs to. The admission link — the task_spawned material — carries
    the Spec; every later link carries none, so the chain reuses the existing
    persistent identity material and declares no new orchestration persistence
    format.

func DecodeTaskRecord(body []byte) (TaskRecord, error)
    DecodeTaskRecord reads one restore link back from its canonical bytes.

type UncertainProbe func(CommitRecord) bool
    UncertainProbe reports whether one commit attempt is observed as uncertain:
    the storage outcome of that attempt is unknown and is reconciled before any
    repeat.

type WrapOption func(*Wrapper)
    WrapOption configures a registration wrapper at construction.

func WithApprover(approve Approver) WrapOption
    WithApprover attaches the approval decision a wrapped call is admitted
    under. A wrapper with no approver admits every call for execution.

func WithMailbox(sink NoticeSink) WrapOption
    WithMailbox attaches the mailbox write a background result travels through.
    A wrapper with no mailbox refuses a call closed before admission.

func WithResultRecorder(record ResultRecorder) WrapOption
    WithResultRecorder attaches the recorder a wrapped call records its result
    through. A wrapper with no recorder refuses a call closed before admission.

type Wrapper struct {
	// Has unexported fields.
}
    Wrapper is the registration wrapper of one native tool: it admits each call
    through the task manager and answers that call with its single receipt,
    while the tool's own outcome returns later as an independent notification.
    It satisfies the native callable tool port of the locked framework.

func Wrap(mgr *MemoryManager, inner trpctool.CallableTool, opts ...WrapOption) *Wrapper
    Wrap returns the registration wrapper of one native tool over a task
    manager: each call is admitted through the manager and answered with its
    single receipt, and the outcome of the tool returns beside the admission as
    an independent notification.

func (w *Wrapper) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call admits one native call and answers it with the single receipt of that
    admission. The receipt is the whole synchronous answer: the outcome of the
    execution returns later as an independent notification, a call the approval
    channel parks answers with its waiting receipt, and a call that cannot be
    admitted or recorded refuses once as a native tool error beside no result.

func (w *Wrapper) Declaration() *trpctool.Declaration
    Declaration returns the declaration of the wrapped tool unchanged.

func (w *Wrapper) Holds() *HoldBook
    Holds returns the waiting table the wrapper parks approval holds in.

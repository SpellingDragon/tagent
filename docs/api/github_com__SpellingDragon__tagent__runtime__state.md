package state // import "github.com/SpellingDragon/tagent/runtime/state"

The heat matrix of the configuration dimensions beside the routing that keeps
the two consumption paths apart. Every dimension of every sub-agent belongs to
exactly one channel — the FP generation face, the SRC source-pull face, the FILE
lazy-read face, or the named RESTART refusal — and one change takes effect only
on the channel its dimension belongs to.

The two paths never stand in for each other: a structural change runs
config.Load → Compile → Prepare → the precondition checks → commit and commits
only through the sole publication entry, while a file change takes effect
directly at the request boundary. A structural difference never rides the file
channel around the precondition checks, and a file change is never claimed to
have passed them.

The failure cause chain of the two reload paths: a structural change that is
refused or fails keeps the chain of the causes it rests on — the source file,
the normalized line within it and the named reason beside it — so the failure is
locatable and is never swallowed into a generic reload failure.

Each link wraps the failure it reports, so the chain stays matchable by
errors.Is and errors.As all the way down: the source and the line locate the
change, the reason names what stopped it, and the wrapped failure keeps the
named refusal underneath.

The FILE lazy-read face of the effective binding: the state owner holds the
structural binding of a prompt — which files, in which order — while the file
content is read here at the request boundary and fixed as the version that one
request uses.

- The two are never conflated: the structural binding belongs to the published
generation, the content version does not. - A file change takes effect at the
next request boundary at once — no register, no evaluation and no precondition
gate stands in its way — and it never assigns a structural generation. - A
request under way keeps the content it fixed and is never half-way spliced with
a newer file. - Skill content is read through the framework skill repository
(the RefreshableRepository rescan that replaces the whole index and the parser
behind it); this face is only the request-boundary glue around it, with no skill
scanner and no parsing copied here.

Package state is the runtime's state owner: it establishes the agent and session
sites an explicit address names, admits a prepared plan as the next effective
binding generation, closes a session's own side, and accepts the notifications a
task intends to deliver.

The package holds the effective bindings and the session sites, and an executor
borrows a binding for one request rather than keeping a second writable source
of its own. The store, the borrowing face, the send seam and the durable commit
carrier are the behaviour behind the ports.

CONSTANTS

const (
	// ArchiveSourcePrefix opens every identity this package derives from an old
	// archive: a migrated task or original carries its source in the identity
	// itself, so imported material stays marked and apart from the live identity
	// space whatever the archive holds.
	ArchiveSourcePrefix = "archive:"
	// ArchiveScope is the isolation scope the migrated addresses, instances and
	// reference metadata register under.
	ArchiveScope = "archive-import"
	// ArchiveOriginalFormat is the original format the migrated dumps register
	// under.
	ArchiveOriginalFormat = "archive-import-v1"
)

VARIABLES

var (
	// ErrPlanMissing reports a StatePlan construction whose declaration plan
	// carries no declaration, so no agent list names the candidates.
	ErrPlanMissing = errors.New("state: the declaration plan carries no declaration")
	// ErrAgentBindingMissing reports a declared agent no candidate binding was
	// constructed for.
	ErrAgentBindingMissing = errors.New("state: no candidate binding was constructed for the declared agent")
	// ErrAgentBindingUnexpected reports a candidate binding for an agent the
	// declaration does not name.
	ErrAgentBindingUnexpected = errors.New("state: candidate binding names an agent the declaration does not")
	// ErrBindingIncomplete reports a candidate binding that carries no model, so
	// no request could ever execute on it.
	ErrBindingIncomplete = errors.New("state: the candidate binding carries no model")
	// ErrPlanEmpty reports a plan that carries no agent binding to hold.
	ErrPlanEmpty = errors.New("state: the candidate plan carries no agent binding")
	// ErrAgentUnknown reports a borrow of an agent no held binding serves.
	ErrAgentUnknown = errors.New("state: no binding is held for the named agent")
	// ErrHandleReleased reports the release of a binding handle already released.
	ErrHandleReleased = errors.New("state: the binding handle is already released")
	// ErrBindingRefUnknown reports a derived borrow through a binding reference
	// that names no borrow this holder issued: the zero reference, a reference
	// issued by another holder, or one carried over from a previous run.
	ErrBindingRefUnknown = errors.New("state: the binding reference names no borrow this holder issued")
	// ErrBindingGenerationReleased reports a derived borrow through a binding
	// reference whose initiating generation has already been released. The
	// named binding is gone, and the current generation is never lent in its
	// place. It is a named case of ErrBindingRefUnknown, so a caller matching
	// the refusal of any reference that resolves here matches it too.
	ErrBindingGenerationReleased = fmt.Errorf("%w: the generation it names is already released", ErrBindingRefUnknown)
)
var (
	// ErrDimensionUnknown reports a change touching a dimension the matrix does
	// not register: the change takes no path until the dimension is registered
	// beside a consumption point.
	ErrDimensionUnknown = errors.New("state: the change touches no registered configuration dimension")
	// ErrChannelMismatch reports a change arriving through a channel other than
	// the one its dimension belongs to. The matrix gives each dimension exactly
	// one channel, and no change chooses a second one to take effect on.
	ErrChannelMismatch = errors.New("state: the change arrives through a channel its dimension does not belong to")
	// ErrStructuralViaFile reports the named case of ErrChannelMismatch the
	// matrix forbids outright: a structural change arriving through the file
	// channel. The structural path runs the precondition checks first, and a
	// structural difference never takes effect through the file channel around
	// them. It is a named case of ErrChannelMismatch, so a caller matching the
	// refusal of any misrouted change matches it too.
	ErrStructuralViaFile = fmt.Errorf("%w: a structural change never takes effect through the file channel", ErrChannelMismatch)
	// ErrRestartRefused reports a change to a dimension the matrix declares
	// RESTART: the resource it names cannot migrate, and refusing the change is
	// the correct behavior rather than a failure to support it.
	ErrRestartRefused = errors.New("state: the dimension names a resource that cannot migrate and is refused until restart")
	// ErrRouterFileSourceMissing reports a file read through a router that
	// carries no file source.
	ErrRouterFileSourceMissing = errors.New("state: the router carries no file source")
	// ErrRouterGateMissing reports a structural change routed through a router
	// that carries no publication entry to gate it.
	ErrRouterGateMissing = errors.New("state: the router carries no publication entry")
)
var (
	// ErrFileRootMissing reports a prompt file read through a file source that
	// names no root to read it under.
	ErrFileRootMissing = errors.New("state: the file source names no root")
	// ErrSkillRepositoryMissing reports a skill file read through a file source
	// that carries no skill repository to read it through.
	ErrSkillRepositoryMissing = errors.New("state: the file source carries no skill repository")
	// ErrFileKindUnknown reports a file reference whose kind is neither prompt
	// nor skill, so no read path belongs to it.
	ErrFileKindUnknown = errors.New("state: the file reference names no known kind")
)
var (
	// ErrDeliveryNotAllowed reports a notification whose task carries no
	// delivery registration, so no target is named for it and the delivery is
	// refused instead of guessed.
	ErrDeliveryNotAllowed = errors.New("state: no delivery registration routes this notification")
	// ErrTaskIdentityMissing reports a delivery registration that carries no
	// task identity, so no notification could ever be routed by it.
	ErrTaskIdentityMissing = errors.New("state: delivery registration carries no task identity")
	// ErrNoticeIdentityMissing reports a notification that carries no task
	// identity, so no repeat of it could ever be recognized as the same
	// notification.
	ErrNoticeIdentityMissing = errors.New("state: notification carries no task identity")
	// ErrNoticeConflict reports a notification that reuses a recorded identity
	// with different content.
	ErrNoticeConflict = errors.New("state: notification identity carries different content")
	// ErrEntryUnknown reports a mailbox entry no acceptance sequence names.
	ErrEntryUnknown = errors.New("state: no mailbox entry at the named acceptance sequence")
	// ErrClaimConflict reports a claim of a mailbox entry another executor
	// already claimed.
	ErrClaimConflict = errors.New("state: the mailbox entry is already claimed")
	// ErrNotClaimed reports a consumption of a mailbox entry no executor has
	// claimed yet.
	ErrNotClaimed = errors.New("state: the mailbox entry is not claimed yet")
	// ErrClaimSettled reports the reclaiming of a mailbox entry whose consumption
	// has already completed.
	ErrClaimSettled = errors.New("state: the mailbox entry is already consumed")
)
var (
	// ErrIncompleteAddress reports an address that does not name both an agent
	// and a session, so no site and no target can hang on it.
	ErrIncompleteAddress = errors.New("state: address does not name both an agent and a session")
	// ErrInstanceMissing reports a site operation that carries no instance
	// identity, so the instance that owns the site cannot be told apart from
	// another one.
	ErrInstanceMissing = errors.New("state: operation carries no instance identity")
	// ErrUnknownTarget reports a target no established site serves.
	ErrUnknownTarget = errors.New("state: no site serves the named target")
	// ErrSessionClosed reports input aimed at a site whose own side is closed
	// to new input.
	ErrSessionClosed = errors.New("state: the session side of the named site is closed")
	// ErrInstanceRetired reports input aimed at the site of an instance another
	// instance has replaced under the same address.
	ErrInstanceRetired = errors.New("state: the named instance is retired")
	// ErrPublishNotCarried reports a publication the memory store holds no
	// effective binding generation for.
	ErrPublishNotCarried = errors.New("state: publication is not carried by this store")
)
var (
	// ErrDecisionIdentityMissing reports an occupancy entry that carries no
	// invocation identity or no decision identity, so no decision slot could be
	// held for it.
	ErrDecisionIdentityMissing = errors.New("state: occupancy entry carries no decision identity")
	// ErrSlotHeld reports a session decision slot another invocation already
	// holds, so the second effective model decision is refused instead of
	// running beside the first.
	ErrSlotHeld = errors.New("state: the session decision slot is already held")
	// ErrSlotNotHeld reports a vacating of the session decision slot by an
	// invocation that does not hold it.
	ErrSlotNotHeld = errors.New("state: the named invocation holds no decision slot of the session")
)
var (
	// ErrRequiredCheckMissing reports a publication refused for missing required
	// evidence: a required check the governance left undispositioned. A check
	// without a disposition is a missing check and is never read as passing.
	ErrRequiredCheckMissing = errors.New("state: the publication carries no disposition for a required check")
	// ErrPublicationContractEmpty reports a publish contract that requires no
	// check at all. It is a named case of ErrRequiredCheckMissing: a contract
	// that demands no evidence refuses every publication.
	ErrPublicationContractEmpty = fmt.Errorf("%w: the publish contract requires no check", ErrRequiredCheckMissing)
	// ErrCheckRefused reports a publication one governance check refused.
	ErrCheckRefused = errors.New("state: a governance check refused the publication")
	// ErrExemptionUnrecorded reports a check exemption that carries no reason or
	// no scope. An exemption is recorded explicitly or it holds nothing.
	ErrExemptionUnrecorded = errors.New("state: the check exemption records no reason or no scope")
	// ErrPublicationCommitterMissing reports a publisher that carries no
	// publication recorder, so no publication record could be committed beside
	// any switch it would make.
	ErrPublicationCommitterMissing = errors.New("state: the publisher carries no publication recorder")
	// ErrBindingHolderMissing reports a publisher that carries no binding holder.
	ErrBindingHolderMissing = errors.New("state: the publisher carries no binding holder")
	// ErrGenerationDiverged reports a hold that assigned a generation other than
	// the one the committed publication record names: a publication outside this
	// entry touched the holder and the numbering it owns.
	ErrGenerationDiverged = errors.New("state: the assigned generation diverges from the committed publication record")
	// ErrRollbackUnavailable reports a rollback with no previous full
	// configuration to rebuild from: no publication has superseded another yet.
	ErrRollbackUnavailable = errors.New("state: no previous full configuration is available to roll back to")
	// ErrRollbackSourceMismatch reports a rollback candidate that carries
	// content other than the rollback source it rebuilds.
	ErrRollbackSourceMismatch = errors.New("state: the rollback candidate does not carry the rollback source fingerprint")
)
var (
	// ErrSkillReadMissing reports a skill load fixed through no read: the load
	// carries no way to read the skill body it is asked to fix.
	ErrSkillReadMissing = errors.New("state: the skill load carries no read")
	// ErrToolSetClosed reports a tool registration on tool resources whose
	// tool sets were already closed.
	ErrToolSetClosed = errors.New("state: the tool resources are already closed")
)

    ErrSkillReadMissing and ErrToolSetClosed are the named failures of the
    tool-resource face of the effective binding: the registration records of the
    native tools the composition root registered — which tool, of which native
    family, read from which source — beside the skill load state those tools
    leave in the state owner.

    - A skill load is a state fact of the binding generation the request
    borrowed, so it is recorded on the tool-set connection that binding carries
    and read back from it. - The body and the content version are fixed at one
    request boundary and kept as that request's fixed view: a file rewritten
    while a request is under way is never spliced into it, and the next request
    boundary reads the rewritten content anew.

var (
	// ErrSendTargetMissing reports an internal send that names no complete
	// target address, so the delivery is refused rather than aimed at a default
	// or at the most recent peer.
	ErrSendTargetMissing = errors.New("state: the send names no explicit target")
	// ErrDeliveryTargetNotRunning reports a delivery to a target whose loop is
	// not running. Opening a loop for that target is never this seam's answer.
	ErrDeliveryTargetNotRunning = errors.New("state: the delivery target is not running")
)
var ErrArchiveUnreadable = errors.New("state: archive checkpoint cannot be read")
    ErrArchiveUnreadable reports an archive root whose checkpoint cannot be read
    or decoded, so neither an import nor a rollback can classify anything under
    it.

var ErrBaselineAdvanced = errors.New("state: the candidate baseline was advanced by a newer publication")

    ErrBaselineAdvanced reports a candidate discarded at the commit point: the
    baseline it recorded at construction was advanced by a newer publication,
    so the candidate is stale and never overwrites the newer publication.

var ErrNoticeNotOwed = errors.New("state: the notification carries no delivery obligation")
    ErrNoticeNotOwed reports an acceptance of a notification the carrier holds
    no delivery obligation for, so there is no delivery state to advance.

var ErrReadNotAllowed = errors.New("state: read is not allowed for this subject and target")
    ErrReadNotAllowed reports a read of a target no grant covers for the subject
    that asked. An authority that holds no grants refuses every read.

var ErrSpecConflict = errors.New("state: the task's admission material is already declared with other content")
    ErrSpecConflict reports a declaration that would replace the admission
    material of a task with other content than the material already stored.

FUNCTIONS

func ArchiveDigest(root string) (string, error)
    ArchiveDigest returns the tree digest of every regular file under root:
    one canonical digest text over each file's relative path, byte length and
    content digest, so any added, removed or altered byte changes the digest and
    a read-only pass over the tree can prove the source held still.

func MigrateArchive(ctx context.Context, root string, tasks ArchiveTaskWriter, originals OriginalMigrator) (MigrationReport, error)
    MigrateArchive imports one old archive into the new path under read-only
    access to the archive: the checkpoint and its external dumps are read,
    every task whose outcome and ownership the checkpoint determines becomes
    one deterministic terminal fact on the task chain, the external dumps become
    originals of the tier, and nothing is written under the archive root. Every
    identity the import lands carries the archive source marker, so migrated
    facts and originals stay marked and apart from the live chain whatever the
    archive holds.

    The import is idempotent and rerunnable: every identity and every record
    it writes derives from the archive content alone, so an interrupted run
    reruns to the report an uninterrupted run returns. A missing external dump
    and a task the checkpoint cannot determine are named in the report gaps
    and imported nowhere, so a refusal stays visible and a rerun repeats it
    verbatim.

func NewDurableCommit(log FactLog, uncertain task.UncertainProbe) (*DurableCommit, error)
    NewDurableCommit returns a durable commit carrier over one fact log and
    loads every fact the log already holds, so a second process over the same
    log sees exactly the facts the first one committed. uncertain, when it is
    not nil, marks matching commit attempts as uncertain, so all three commit
    outcomes stay observable.

func NewStatePlan(plan OrgPlan, agents map[event.AgentID]AgentBinding, leases []LeaseRef) (StatePlan, error)
    NewStatePlan instantiates the candidate StatePlan of one configuration
    change: the Agents map is keyed by the identity of every declared agent,
    and the candidate resource registrations stay in the order the candidates
    were constructed. A declared agent no candidate binding was constructed for,
    a binding for an agent the declaration does not name, and a binding that
    carries no model each refuse the plan by name.

func RollbackArchive(ctx context.Context, root string, tasks ArchiveTaskReader, originals OriginalReader) (RollbackReport, error)
    RollbackArchive reads one archive and its migrated items back under
    read-only access to both sides: the archive bytes are read, the
    deterministic identities they migrate under are looked up on the read faces
    of the new path, and every lookup is accounted in the report beside its
    outcome. The rollback holds no write face at all, so it records nothing,
    rewrites nothing and removes nothing: an item the new path still holds
    is reported as residue, and one the new path holds nothing under is named
    absent rather than written back.

func Route(change Change) (Channel, error)
    Route decides the one channel a submitted change takes effect on. The matrix
    registers each dimension beside exactly one channel, so a change arriving
    on any other channel is refused by name — a structural change never rides
    the file channel — and a change to a RESTART dimension is refused outright,
    because refusing it is the correct behavior. A change the matrix registers
    nowhere is refused by name as well and takes no path at all.

TYPES

type AcceptSeq uint64
    AcceptSeq is the monotonic acceptance sequence of one accepted input. It is
    assigned at acceptance, and a later claim or consumption never rewrites it.

type AgentBinding struct {
	// Model is the native model the agent runs against.
	Model model.Model
	// Tools lists the native tools visible to the agent.
	Tools []trpctool.Tool
	// Plugins is the ordered plugin pipeline of the agent; the order is the
	// published order and is never reassembled mid-request.
	Plugins []plugin.Plugin
	// PromptSource is the prompt source of the agent.
	PromptSource PromptSource
	// Set is the tool-set connection of the agent; its Close is governed by a
	// resource lease rather than by the release of this binding.
	Set trpctool.ToolSet
}
    AgentBinding is the live binding of one agent: the native model, tools and
    ordered plugin pipeline a request executes with, together with the prompt
    source and the tool-set connection.

    The binding is constructed privately for a candidate. A request takes one
    generation of it and keeps that generation for its whole life; the binding
    is released when its user is done, not when a newer generation is published.

type ArchiveTaskReader interface {
	// TaskIDs returns every task the chain holds facts for.
	TaskIDs() []event.TaskID
	// Records returns the fact chain of one task.
	Records(ctx context.Context, taskID event.TaskID) ([]task.TaskRecord, error)
}
    ArchiveTaskReader is the read face of the new path's task chain: the tasks
    it holds facts for and the fact chain of one of them.

    The redline list (3 rules): MUST return every task the chain holds facts
    for. MUST report the fact chain of one task in its recorded order. MUST NOT
    answer a failed read as an empty chain.

type ArchiveTaskWriter interface {
	// Declare stores the admission material of one migrated task.
	Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error
	// Commit stores the deterministic fact of one migrated task.
	Commit(ctx context.Context, record task.CommitRecord) (task.CommitResult, error)
}
    ArchiveTaskWriter is the write face of the new path's task chain: the
    admission material one migrated task declares beside the fact it commits.

    The redline list (3 rules): MUST store the admission material one migrated
    task declares beside the fact it commits. MUST recognise a repeated migrated
    fact as the same fact, so a replayed restore records no second one. MUST NOT
    report a migrated fact as stored before its commit result is committed.

type Authority struct {
	// Has unexported fields.
}

    Authority holds the read grants recorded for subjects: which target each
    subject may read. A check only reads the recorded grants and never records
    one, so asking about a target grants nothing.

func NewAuthority() *Authority
    NewAuthority returns an authority that holds no grants, so every read is
    refused until a grant is recorded.

func (a *Authority) CheckRead(ctx context.Context, subject Subject, target event.Address) error
    CheckRead reports whether one subject may read one target. A missing
    authority, an unnamed subject and a pair no grant covers all refuse the
    read, and the check records nothing.

func (a *Authority) GrantRead(ctx context.Context, subject Subject, target event.Address) error
    GrantRead records the read grant of one subject over one target. The subject
    and the target are named explicitly: a grant covers that pair alone and
    spreads to no neighbouring session.

type BindingHandle struct {
	// Binding is the binding generation this handle holds for the request.
	Binding AgentBinding
	// Ref is the stable reference to this borrow: the reference a delegated
	// request carries to BorrowDerived to inherit the generation this handle
	// holds.
	Ref BindingRef

	// Has unexported fields.
}
    BindingHandle holds the borrowed binding one request uses for the whole of
    its life and ends that use when the request is done.

    Release ends the use of the borrowed binding and is called when the
    request's actual use of the binding has finished and the work producing it
    has ended, never merely because a newer generation was published: a request
    already under way keeps the generation it took until then.

func (h *BindingHandle) Release(ctx context.Context) error
    Release ends the request's use of the borrowed binding. It answers once
    per handle: a handle already released is refused by name, so the use of one
    borrowed generation ends exactly once.

type BindingOwners struct {
	// Has unexported fields.
}
    BindingOwners resolves a restored target's owner against the current
    orchestration binding: the tool set the current effective generation holds
    for the owner. A target the owner's current tool set does not hold is
    refused by name with task.ErrTargetRemoved, so a restore or a replay refuses
    the re-delivery rather than faking it, silently re-routing it, or serving it
    from a stale generation. The holder is supplied at construction and nothing
    is read from a top-level singleton.

func NewBindingOwners(bindings *Bindings) *BindingOwners
    NewBindingOwners returns an owner resolution over one binding holder's
    current effective generation.

func (o *BindingOwners) OwnerOf(ctx context.Context, owner event.AgentID, target event.AgentID) (event.AgentID, error)
    OwnerOf reports the owner that target belongs to under the current
    orchestration binding: the owner whose current tool set still holds the
    target. An owner the binding does not hold and a target the owner's tool set
    does not hold are both refused with task.ErrTargetRemoved beside the cause,
    since either means the current binding carries no such target for that
    owner.

type BindingRef struct {
	// AgentID names the agent whose borrowed binding the reference names.
	AgentID event.AgentID
	// InstanceID identifies the holder that issued the reference, so a
	// reference minted by another holder or by a previous run of this one
	// never resolves against this one.
	InstanceID event.InstanceID
	// Generation names the generation as this holder numbered its
	// publications when the borrow was opened. A publication advances the
	// number and no two publications share one.
	Generation uint64
}
    BindingRef is the stable reference to one borrow: the initiating reference
    a delegated request carries so that it borrows the generation its initiator
    took rather than the generation effective at its own start.

    The reference names one agent of one published generation of this holder.
    Generation numbers come from this holder's publications only and are never
    chosen by a caller, and the instance identity is minted unguessable when
    the holder is created: the zero reference, one issued by another holder, one
    carried over from a previous run and a guessed one each name no borrow here.
    A reference is not a lease; it holds nothing and resolves only while the
    generation it names is still held.

type Bindings struct {
	// Has unexported fields.
}
    Bindings holds the live binding generation the state owner serves to
    requests and hands one generation to each request as its execution begins.

    - A held generation is replaced as a whole: a request under way keeps
    the binding its handle was opened with until it releases that handle,
    an input still queued takes the generation effective when its own execution
    begins, and the handles in use stay countable here. - The holder keeps the
    generations it issued references to, so a delegated request borrows the
    generation its initiator took, and a generation whose last hold is gone is
    dropped by name instead of being silently re-lent. - The hold protocol:
    an input holds the generation effective when it begins executing until it
    releases the handle it borrowed; a delegated input takes its hold on the
    initiating generation before it is queued and the queue hands that hold on
    to the executing request instead of borrowing again. - A publication moves
    no handle already borrowed, and a cancelled or refused input releases the
    hold it took. - A recovery counts the holds that were never released before
    it borrows again and re-establishes the ones it accounts for; a borrow it
    cannot account for is not recoverable, its reference is refused by name,
    and the current generation is never lent in its place.

func NewBindings() *Bindings
    NewBindings returns an empty binding holder that serves no generation yet.

func (b *Bindings) Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error)
    Borrow implements Borrow and opens one request's use of the binding that
    is effective at the moment the call arrives. The returned handle holds that
    binding for the whole life of the request and ends that use through Release.
    Its Ref names the borrow, so a delegated request inherits the generation
    this call took.

func (b *Bindings) BorrowDerived(ctx context.Context, ref BindingRef) (*BindingHandle, error)
    BorrowDerived implements Borrow and opens a delegated request's use of
    the binding of the generation the reference's borrow took. The returned
    handle holds that binding for the whole life of the delegated request,
    keeps its hold observable in Inflight, and ends that use through Release
    exactly once, as the initiating handle does. A reference this holder never
    issued — the zero reference, one issued by another holder, one carried over
    from a previous run or a guessed one — is refused as ErrBindingRefUnknown,
    and one whose initiating generation has already been released is refused
    as ErrBindingGenerationReleased; neither is silently served the generation
    effective at the delegated request's own start.

func (b *Bindings) Hold(plan StatePlan) error
    Hold makes one prepared plan's candidate bindings the effective generation
    the next borrow takes. A plan that carries no agent binding is refused.
    Each hold publishes one generation numbered here, so the number a reference
    carries comes from a real publication and is never chosen by a caller;
    the generation it supersedes is released once no borrow holds it.

func (b *Bindings) Inflight() int
    Inflight reports how many borrowed handles are still in use, so the holders
    of a superseded generation — including the delegated holds opened through a
    reference — are observable while they finish.

type Borrow interface {
	// Borrow returns a handle to the binding effective for one agent at the
	// moment the request begins executing, never one taken before the request
	// started. The borrowed generation is held by the returned handle for the
	// whole of the request's life.
	Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error)
	// BorrowDerived returns a handle to the binding of the generation the
	// reference's borrow took, for the whole of the delegated request's life:
	// the delegated request inherits its initiator's generation and is never
	// silently served the generation effective at its own start. A reference
	// this holder never issued is refused as ErrBindingRefUnknown, and one
	// whose initiating generation has already been released is refused as
	// ErrBindingGenerationReleased.
	BorrowDerived(ctx context.Context, ref BindingRef) (*BindingHandle, error)
}
    Borrow is the per-request borrowing face of the state owner: a request that
    begins executing takes the generation effective at that moment.

    The redline list (4 rules): MUST return the generation effective when the
    request begins executing, and hold it for the whole request. MUST serve a
    delegated request the generation its initiator took. MUST NOT bind a queued
    input in advance, nor swap the generation under a request already under way.
    MUST NOT serve a reference this holder never issued.

type Candidate struct {
	// Plan is the prepared state plan.
	Plan StatePlan
	// Resources is the ledger of the candidate-exclusive resources.
	Resources *Leases
}
    Candidate is one prepared publication candidate: the state plan it
    carries and the resource ledger of the candidate-exclusive resources its
    construction acquired. The ledger is the candidate's own release face,
    so a candidate that leaves the publication frees exactly what it acquired
    and touches neither the shared faces nor the resources already in service.

type Cause struct {
	// Source is the source file the failure comes from.
	Source string
	// Line is the normalized line within the source; it stays zero when the
	// failure names no line.
	Line int
	// Reason is the named reason the failure rests on.
	Reason string
	// Err is the failure the link wraps, kept so the chain stays matchable.
	Err error
}
    Cause is one link of a failure's cause chain: the source file the failure
    comes from, the normalized line within that source and the named reason it
    rests on, beside the failure the link wraps.

    The link is self-contained: a reader holding only the link knows which
    source and which line to look at and what named reason stopped the change.
    The wrapped failure is kept rather than flattened, so no link ever collapses
    a chain into one generic message.

func CauseChain(err error) []*Cause
    CauseChain reports the cause links a failure carries, ordered from the
    outermost link to the innermost one. A failure that carries no link reports
    an empty chain, and a chain never loses the links sitting under a joined
    failure.

func NewCause(source string, line int, reason string, err error) *Cause
    NewCause returns one cause chain link over the given source, normalized
    line and named reason, wrapping the failure it reports. The failure is kept
    beside the link rather than flattened into it.

func (c *Cause) Error() string
    Error reports the link as the source and normalized line it locates beside
    the named reason it rests on and the failure it wraps, so the rendered
    message reads as the chain it belongs to.

func (c *Cause) Unwrap() error
    Unwrap reports the failure the link wraps, so a caller matching the named
    refusal underneath matches it through the chain.

type Change struct {
	// Dimension is the configuration dimension the change touches.
	Dimension Dimension
	// Via is the channel the change arrives through.
	Via Channel
	// Source is the source file the change was written in.
	Source string
	// Line is the normalized line within that source.
	Line int
}
    Change is one submitted configuration change attempt: the dimension it
    touches, the channel it arrives through and the source origin it was written
    in, so a refusal stays locatable beside the normalized line it names.

type Channel int
    Channel names the one heat channel a configuration dimension belongs to.

const (
	// ChannelFP is the FP generation face: a view change takes effect at the
	// next round, a request already under way keeps the generation it took, and
	// the change commits only through the publish precondition gate.
	ChannelFP Channel = iota
	// ChannelSRC is the SRC source-pull face: a parameter change takes effect
	// at the next consumption of that parameter, with no generation assigned.
	ChannelSRC
	// ChannelFile is the FILE lazy-read face: the file is the source of truth
	// and its content is read at the consumption boundary, as the prompt body
	// and the skill body are. No register, no evaluation and no precondition
	// gate stands between the change and its effect.
	ChannelFile
	// ChannelRestart is the named restart refusal: the dimension names a
	// resource that cannot migrate, and refusing the change is the correct
	// behavior.
	ChannelRestart
)
func ChannelOf(d Dimension) (Channel, bool)
    ChannelOf reports the single channel one registered dimension belongs to and
    whether the matrix registers that dimension at all.

func (c Channel) String() string
    String reports the channel by the name the matrix registers it under.

type ChannelRow struct {
	// Dimension is the configuration dimension the row registers.
	Dimension Dimension
	// Channel is the single channel the dimension belongs to.
	Channel Channel
	// Evidence names the contract test or code line of the consumption point.
	Evidence string
}
    ChannelRow is one row of the heat matrix: the dimension, the single channel
    it belongs to and the evidence its consumption point carries — a contract
    test name or a code line. A row whose consumption point never reads what it
    registers is a fake hot update and is a defect rather than partial support,
    so a dimension registers its row only beside the consumption point that is
    wired for it.

func ChannelMatrix() []ChannelRow
    ChannelMatrix returns the registered heat matrix: one row per registered
    dimension beside the channel it belongs to and the evidence of its
    consumption point.

    Rows grow with their consumption points: a later change that wires the
    consumption of another dimension adds its row here beside its evidence. The
    model parameter dimension takes the SRC source-pull face (a parameter change
    takes effect at the next consumption) and the mcp-servers dimension takes
    the FILE lazy-read face; neither is registered yet because their consumption
    points land outside this change, and a row carried without a consumption
    point would be a fake hot update. The RESTART rows are registered beside the
    refusal this matrix itself performs: refusing such a change is the correct
    behavior.

type CheckAnswer struct {
	// Name names the check this answer dispositions.
	Name CheckName
	// Verdict is the disposition reported.
	Verdict CheckVerdict
	// Reason states the ground the disposition rests on.
	Reason string
	// Scope names the surface the disposition covers.
	Scope string
	// Evidence lists the evidence the disposition cites.
	Evidence []string
}
    CheckAnswer is the recorded disposition of one governance check: the verdict
    beside the reason and scope it rests on and the evidence it cites.

type CheckName string
    CheckName names one governance check the publish precondition contract
    declares as required evidence for a publication.

type CheckVerdict int
    CheckVerdict is the disposition one governance check reports for one
    publication candidate.

const (
	// CheckUnanswered is the zero value and the conservative disposition: a
	// check that reports no disposition is never read as passing.
	CheckUnanswered CheckVerdict = iota
	// CheckPass reports a check whose evidence supports the publication.
	CheckPass
	// CheckReject reports a check that refuses the publication.
	CheckReject
	// CheckExempt reports a check that is not applicable to the candidate or is
	// explicitly disabled; its exemption is recorded beside its reason and scope
	// and counts as no passing evidence.
	CheckExempt
)
type ClaimState int
    ClaimState is the claim state of one accepted input, from pending through
    claimed to consumed.

const (
	// ClaimPending is the zero value: the input was accepted but no executor
	// has claimed it yet.
	ClaimPending ClaimState = iota
	// ClaimClaimed is an input one executor has claimed.
	ClaimClaimed
	// ClaimConsumed is an input whose consumption has completed.
	ClaimConsumed
)
type ConsumeReceipt struct {
	// Completed reports whether the consumption of the input finished.
	Completed bool
}
    ConsumeReceipt is the third mailbox time point: the receipt recorded when
    one accepted input was fully consumed.

type Delivery struct {
	// Target is the address of the site the notification enters.
	Target event.Address
	// TargetInstance is the runtime instance serving that site.
	TargetInstance event.InstanceID
	// Notice is the notification the target mailbox accepts. Its stable
	// identity is what a repeated delivery of the same send is recognized by.
	Notice task.Notice
}
    Delivery is one internal send: the explicit target site one notification
    enters.

type Dimension string
    Dimension names one configuration dimension of a sub-agent.

const (
	// DimensionModel names the model the agent runs against.
	DimensionModel Dimension = "model"
	// DimensionTools names the tool and delegation references of the agent.
	DimensionTools Dimension = "tools"
	// DimensionPlugins names the ordered plugin pipeline of the agent.
	DimensionPlugins Dimension = "plugins"
	// DimensionTaskPolicy names the task policy applied to the agent's tasks.
	DimensionTaskPolicy Dimension = "task-policy"
	// DimensionResourceRefs names the resource references the agent holds.
	DimensionResourceRefs Dimension = "resource-refs"
	// DimensionMemory names the memory bindings of the agent.
	DimensionMemory Dimension = "memory"
	// DimensionPromptBody names the prompt file body of the agent.
	DimensionPromptBody Dimension = "prompt-body"
	// DimensionSkillBody names the skill file body of the agent.
	DimensionSkillBody Dimension = "skill-body"
	// DimensionRoots names a directory the agent serves from.
	DimensionRoots Dimension = "roots"
	// DimensionEndpoints names a remote endpoint the agent is wired to.
	DimensionEndpoints Dimension = "endpoints"
)
    DimensionModel and the other dimensions of one sub-agent the matrix
    registers: a structural dimension names a view the generation carries, a
    file dimension names content that is its own source of truth, and a restart
    dimension names a resource that cannot migrate.

type DurableCommit struct {
	// Has unexported fields.
}
    DurableCommit is the durable carrier behind the task commit port: it stores
    one self-contained record per mutation identity through a FactLog behind the
    sync barrier, and holds every committed notification as an open delivery
    obligation. A repeated record of a mutation identity already stored returns
    the fact already stored and records no second one — and never advances a
    delivery state, because an idempotent hit reports a stored fact and not a
    delivered notification. Only an explicit acceptance by the target moves a
    delivery, through Accept.

    The carrier holds no wrapper, binding or lease: the log it stores through is
    supplied at construction and nothing an earlier run captured is revived.

func (d *DurableCommit) Accept(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error)
    Accept advances the delivery state of one notification to accepted.
    This is the only advance there is: a commit never moves a delivery,
    and an idempotent commit repeat leaves every notification owed and pending.
    A notification the carrier holds no obligation for is refused by name.

func (d *DurableCommit) Commit(ctx context.Context, record task.CommitRecord) (task.CommitResult, error)
    Commit implements the task commit port. A record of a mutation identity the
    carrier already stores returns the fact already stored and records no second
    fact — and advances no delivery state: the notifications of the stored fact
    stay owed and pending until their targets explicitly accept them. The same
    identity beside other content is refused by name, as is a record expecting
    another revision. A record is staged behind the sync barrier before the
    carrier reports it committed, so a barrier that fails reports an uncertain
    outcome instead of a durable success.

func (d *DurableCommit) Declare(ctx context.Context, taskID event.TaskID, spec task.Spec) error
    Declare stores the admission material of one task — the task_spawned
    record — beside the commit facts of the same log, so a restore rebuilds
    the task from material the chain already carries. The trigger-source stamp
    of the admission travels inside that material: a restore reads the stamp
    the admission recorded instead of deriving one again. The declaration is
    idempotent for the same content and refuses to replace stored material with
    other content.

func (d *DurableCommit) Facts(taskID event.TaskID) []task.CommitRecord
    Facts returns the records the carrier stored for one task, in commit order.

func (d *DurableCommit) Outbox(taskID event.TaskID) []task.OutboxNotice
    Outbox returns the notifications one task still owes delivery, each paired
    with its delivery-layer state, ordered by round, kind and sequence.

func (d *DurableCommit) Records(ctx context.Context, taskID event.TaskID) ([]task.TaskRecord, error)
    Records returns the durable fact chain of one task as restore links, in
    commit order: the admission link carries the stored task_spawned material,
    every later link carries its fact alone.

func (d *DurableCommit) Revision(taskID event.TaskID) int64
    Revision returns the revision the task is at after the records stored for
    it.

func (d *DurableCommit) TaskIDs() []event.TaskID
    TaskIDs returns every task the carrier holds facts for, in numeric order.

type FactLog interface {
	// Put stages one key and its value.
	Put(ctx context.Context, key string, value []byte) error
	// Get returns the value stored under key and whether one is held.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Scan returns every stored pair under the key prefix as key and value.
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)
	// Sync completes the write barrier over everything staged so far, so a
	// caller reports durable success only behind a barrier that returned.
	Sync(ctx context.Context) error
}

    FactLog is the durable fact surface one durable commit carrier stores
    through: put, get, prefix scan and the sync barrier a commit reports its
    success behind. Its signatures carry built-in types only, so any backend —
    the memory tier's key-value port among them — satisfies it structurally and
    no package edge is required in either direction.

    The redline list (4 rules): MUST stage one key and its value without
    claiming it durable. MUST complete the write barrier over everything staged
    before anyone reports durable success. MUST NOT report staged bytes as
    stored. MUST NOT answer a prefix scan with a subset of the pairs under that
    prefix.

type FileSnapshot struct {
	// Files are the contents fixed in the structural order of the binding.
	Files []FixedFile
	// Version is the content version fixed at this request boundary. It names
	// the contents this snapshot carries and never the structural generation.
	Version string
}
    FileSnapshot is the content version one request fixes at its boundary:
    the contents read in the structural order of the binding beside the version
    computed from exactly those contents.

    The snapshot is immutable once fixed: a later file change never rewrites it,
    so a request under way keeps the version it fixed and no historical snapshot
    pins a later request to its content.

type FileSource struct {
	// Has unexported fields.
}
    FileSource is the file source behind one agent's structural binding:
    it reads the prompt and skill contents the binding names at the request
    boundary and fixes the version one request uses.

    The read is a fresh read at every request boundary, so a file rewritten
    between two requests is read anew by the later one while the structural
    generation the binding belongs to stays where it is.

func NewFileSource(root string, skills skill.Repository) *FileSource
    NewFileSource returns the file source that reads prompt files under the
    given root and skill files through the given framework skill repository.
    The repository may be nil when the binding names no skill file; a skill read
    without one is refused by name.

func (s *FileSource) Fixate(ctx context.Context, prompt PromptSource) (FileSnapshot, error)
    Fixate reads the contents one request uses and fixes their version:
    the files are read in the structural order the binding carries and the
    version is computed from exactly the contents read. The read takes effect at
    once — no register, no evaluation and no precondition gate — and assigns no
    structural generation.

type FixedFile struct {
	// Ref is the file reference the content was read for.
	Ref config.FileRef
	// Content is the content read at this request boundary.
	Content string
}
    FixedFile is one file content fixed at a request boundary: the file
    reference it was read for beside the content read for this request.

type GapKind string
    GapKind names why one archive item was refused by the migration.

const (
	// GapMissingExternalFile names an external dump a checkpoint record points
	// at and the archive root does not hold: the byte source is gone, so the
	// record depending on it is refused beside the named dump.
	GapMissingExternalFile GapKind = "missing-external-file"
	// GapUnrecoverableTask names a task whose outcome or ownership the
	// checkpoint does not determine: an in-flight status, an unknown status
	// spelling or an owner the carrying agent does not match, each refused
	// rather than settled by guess.
	GapUnrecoverableTask GapKind = "unrecoverable-task"
)
type Generation uint64

    Generation is the publication identity of one effective binding generation:
    the monotonically increasing number the sole publication entry assigns
    at each actual publication. The number, never the content fingerprint,
    names the publication: a byte-identical reload assigns no number and builds
    no new resource, and a rollback re-issues the previous full configuration as
    one more number beside freshly constructed live objects.

type Governance interface {
	// Check evaluates one candidate plan and answers each check under the name
	// it is filed by.
	Check(ctx context.Context, plan StatePlan) (map[CheckName]CheckAnswer, error)
}
    Governance is the policy seam the publish precondition contract is enforced
    against: it evaluates one publication candidate and dispositions the checks
    by name. A deterministic stand-in of the real policy may be injected to
    check the publication mechanism; a stand-in's answers are mechanism evidence
    and carry no quality conclusion, and the formal quality assessment lives in
    the structural evaluation gate.

    The redline list (4 rules): MUST answer each check under the name it is
    filed by. MUST refuse a publication whose required check is missing.
    MUST NOT count an inapplicable or disabled check as evidence that passed.
    MUST NOT draw a quality conclusion from the answers of an injected stand-in.

type LeaseRef struct {
	// Kind names the resource kind.
	Kind string
	// Key identifies the resource within its kind.
	Key string
	// Ref is the release reference of the resource.
	Ref string
}
    LeaseRef is the registration of one candidate resource: the kind and key
    that name it and the reference the release path uses to release it once.

type Leases struct {
	// Has unexported fields.
}

    Leases is the resource ledger one candidate preparation accumulates: every
    candidate-exclusive resource is registered beside the release that frees
    it at the moment it is acquired, and a preparation that fails frees what it
    acquired in the reverse of the acquisition order and once each.

    The ledger carries only what the candidate itself acquired. A shared face
    or a resource already in service is registered nowhere here, so a failed
    preparation never touches it.

func NewLeases() *Leases
    NewLeases returns an empty candidate resource ledger.

func (l *Leases) Refs() []LeaseRef
    Refs returns the registrations in the order the resources were acquired.

func (l *Leases) Register(kind, key string, release func(context.Context) error) LeaseRef
    Register records one acquired resource beside the release that frees it and
    returns the registration the release path names it by. The release may be
    nil for a resource that owns nothing to free; such a registration is skipped
    by ReleaseAll.

func (l *Leases) ReleaseAll(ctx context.Context) error
    ReleaseAll frees every registered resource in the reverse of the acquisition
    order and exactly once each. A release that fails does not stop the releases
    behind it: its error is joined with the others into the returned error.

type MailboxEntry struct {
	// Notice is the stable identity of the notification this entry accepted.
	Notice task.Notice
	// EventID is the identity of the committed fact this input points at. It is
	// recorded beside the notice's own fact reference and never chosen against
	// it: the entry's EventID and its Notice.Event always name the same
	// committed fact.
	EventID event.EventID
	// InvocationID is the responsibility range the call that produced the
	// accepted notification belongs to. It is recorded from the call identity
	// the delivery carries, and an entry beside no call identity keeps it
	// empty and resolves its invocation through its notice instead.
	InvocationID event.InvocationID
	// AcceptSeq is the monotonic acceptance sequence assigned at acceptance.
	AcceptSeq AcceptSeq
	// Claim is the claim state of the input.
	Claim ClaimState
	// Receipt is the consumption receipt, recorded when consumption completed.
	Receipt ConsumeReceipt
}
    MailboxEntry is one accepted input in a session's mailbox. The three
    time points it carries — acceptance, claim and consumption — are recorded
    separately and never overwrite one another. An entry is not an anonymous
    payload: beside the stable identity of the notification it accepted,
    it names the committed fact the input points at and the responsibility
    range the producing call belongs to, so a consumer traces the event,
    the invocation and the notification behind the entry back to their sources.

type MemoryStore struct {
	// Has unexported fields.
}

    MemoryStore is the in-memory carrier behind the Store face: it establishes
    one site per explicit address and instance, keeps the instance currently
    serving each address, and routes a notification to the site its delivery
    registration names. The memory carrier keeps the business contract of a
    durable one and promises no recovery of its own.

func NewMemoryStore(authority *Authority) *MemoryStore
    NewMemoryStore returns an empty in-memory state store that checks reads
    against the given authority. An authority that is not present refuses every
    read.

func (m *MemoryStore) AcceptNotice(ctx context.Context, notice task.Notice) error

    AcceptNotice implements Store.AcceptNotice: one notification enters the
    mailbox of the site its delivery registration names beside a monotonic
    acceptance sequence. The entry records the association to its source
    facts — the committed fact the notification points at, the responsibility
    range of the call that produced it, and the stable notification identity
    itself — so the entry is traceable back to the event, the invocation
    and the notification that produced it. Acceptance is idempotent by the
    notification's stable identity, so a replayed delivery records no second
    fact and keeps the target the first acceptance recorded.

func (m *MemoryStore) Claim(ctx context.Context, addr event.Address, instance event.InstanceID, seq AcceptSeq) error
    Claim moves one accepted input from pending to claimed in the mailbox of the
    site named. The acceptance sequence stays as acceptance assigned it and the
    consumption receipt stays unrecorded until consumption completes.

func (m *MemoryStore) CloseSession(ctx context.Context, addr event.Address) error
    CloseSession implements Store.CloseSession. It closes the own side of
    the site currently serving the address: new input is refused while the
    obligations already accepted stay readable and settleable.

func (m *MemoryStore) Consume(ctx context.Context, addr event.Address, instance event.InstanceID, seq AcceptSeq) error
    Consume records the consumption of one claimed input in the mailbox of
    the site named: the entry moves to consumed, its receipt records the
    completion and the consumption position advances over the longest contiguous
    completed prefix of the acceptance sequences. A completion beside an
    earlier unfinished entry is recorded on its own entry while the position
    stays before the hole it leaves. An entry already consumed stays as it is,
    and an entry no executor claimed is refused.

func (m *MemoryStore) Establish(ctx context.Context, addr event.Address, instance event.InstanceID) (*Session, error)
    Establish implements Store.Establish. One explicit address and instance
    names one site: establishing the address on another instance retires the
    instance that held it, so input aimed at the retired instance is refused
    while the obligations already accepted there stay readable. The pair already
    serving the address returns its own bookkeeping.

func (m *MemoryStore) Publish(ctx context.Context, plan StatePlan) (Publication, error)
    Publish implements Store.Publish and reports that the memory store carries
    no effective binding publication.

func (m *MemoryStore) Reclaim(ctx context.Context, addr event.Address, instance event.InstanceID, seq AcceptSeq) error
    Reclaim reasserts the claim of one mailbox entry an earlier claim left
    unfinished, so an executor that stopped beside a claim picks the same input
    up again and finishes it. The acceptance sequence and the claim state stay
    as acceptance and the first claim recorded them: an entry still pending is
    claimed through Claim, and an entry whose consumption completed is refused
    by name.

func (m *MemoryStore) Site(ctx context.Context, subject Subject, addr event.Address, instance event.InstanceID) (*Session, error)
    Site returns a copy of the bookkeeping one established site holds, for the
    subject and instance named. A read is answered only beside a recorded grant,
    and a target no site serves is refused rather than established.

func (m *MemoryStore) TrackDelivery(ctx context.Context, taskID event.TaskID, target task.HandoffTarget) error
    TrackDelivery registers where the future notifications of one task are
    delivered: the target address and the target instance that receive them.
    The registration is the delivery grant a notification is accepted beside,
    a later registration reroutes what is not yet accepted, and the target of an
    accepted notification stays as its acceptance recorded it.

type MigratedOriginal struct {
	// Key is the original identity the bytes landed under.
	Key event.EventID
	// Source names the archive file the bytes were read from.
	Source string
	// Digest is the canonical digest text of the stored bytes.
	Digest string
	// Length is the byte length of the stored bytes.
	Length int64
}
    MigratedOriginal is one external dump the migration stored as an original:
    the key its bytes landed under, the archive file they came from, and the
    digest and length the stored bytes carry.

type MigratedTask struct {
	// TaskID is the deterministic task identity the fact landed under.
	TaskID event.TaskID
	// Source names the checkpoint record the task was imported from.
	Source string
	// Status is the terminal status the fact carries.
	Status task.Status
	// MutationID is the mutation identity the fact is idempotent under.
	MutationID task.MutationID
}
    MigratedTask is one task the migration stored in the new path: the
    deterministic identity its fact landed under, the checkpoint record it came
    from, the terminal status it carries and the mutation identity its fact is
    idempotent under.

type MigrationGap struct {
	// Kind is the classification of the refusal.
	Kind GapKind
	// Name names the refused item: the relative dump path or the task identity.
	Name string
	// Detail carries the named cause of the refusal.
	Detail string
}
    MigrationGap names one archive item the migration refused, with the kind of
    the refusal and its named cause, so nothing is skipped silently.

type MigrationReport struct {
	// SourceDigest is the tree digest of the archive root the import read.
	SourceDigest string
	// Tasks lists every task fact the import stored.
	Tasks []MigratedTask
	// Originals lists every external original the import stored.
	Originals []MigratedOriginal
	// Gaps names every item the import refused.
	Gaps []MigrationGap
}
    MigrationReport is the report one import returns: the digest of the archive
    root it read, every task and every original it stored in the new path,
    and every gap it refused by name.

type Occupancy struct {
	// Has unexported fields.
}

    Occupancy holds the execution occupancy of the session sites: the one
    model decision each site may have in flight, and the queued inputs a caller
    withdrew from execution before any decision started on them.

    The occupancy ledger is decoupled from the mailbox acceptance path:
    an input is accepted and a queued input is withdrawn while a slow model
    decision holds the slot, and neither path waits for that decision to end.
    A queued input is never bound to a binding in advance, so it takes what is
    effective when its own execution begins.

func NewOccupancy() *Occupancy
    NewOccupancy returns an empty execution occupancy ledger: no session holds a
    decision slot and no queued input is withdrawn.

func (o *Occupancy) Occupy(addr event.Address, instance event.InstanceID, invocation event.InvocationID, decision event.DecisionID) error
    Occupy takes the session decision slot of one site for the named invocation
    decision, so at most one effective model decision runs per session. A slot
    another invocation already holds refuses the second occupant by name.

func (o *Occupancy) Slot(addr event.Address, instance event.InstanceID) (event.InvocationID, event.DecisionID, bool)
    Slot reports the invocation and decision holding the session decision slot
    of one site, or that no decision is in flight there.

func (o *Occupancy) Vacate(addr event.Address, instance event.InstanceID, invocation event.InvocationID) error
    Vacate releases the session decision slot of one site, so the queued inputs
    of the session take their turn. The invocation named must be the one holding
    the slot; any other name is refused.

func (o *Occupancy) Withdraw(addr event.Address, instance event.InstanceID, seq AcceptSeq) error
    Withdraw removes one queued input from execution beside the site that
    accepted it: the input stays accepted and readable in the mailbox, and it
    is never started on a decision. Withdrawal is recorded immediately and never
    waits for a decision in flight to end.

func (o *Occupancy) Withdrawn(addr event.Address, instance event.InstanceID, seq AcceptSeq) bool
    Withdrawn reports whether the queued input at one acceptance sequence was
    withdrawn from execution beside the site named.

type OrgPlan struct {
	// Declaration is the pure declaration this plan was compiled from.
	Declaration *config.Declaration
	// Baseline is the version baseline the plan was compiled against. At the
	// commit point it is re-checked: a plan whose baseline was already advanced
	// is discarded instead of overwriting the newer publication.
	Baseline string
	// Fingerprint is the canonical organization fingerprint of the declaration.
	Fingerprint string
	// Changes lists the declaration-level change entries.
	Changes []PlanChange
	// NextPrompt is the next effective prompt the change produces.
	NextPrompt string
}

    OrgPlan is the pure declaration plan one configuration change compiles to:
    the declaration it comes from, the baseline and fingerprint that identify
    it, the declaration-level change entries, and the next effective prompt.

    The plan carries the declaration itself rather than a second copy of the
    agent list, so the declaration stays the single source of truth for the
    agent graph. Its fingerprint is the canonical organization fingerprint of
    the declaration and covers only the declaration surface.

type OriginalMigrator interface {
	OriginalReader

	// PutOriginal stores body under key beside the reference metadata the
	// import names.
	PutOriginal(ctx context.Context, key event.EventID, owner event.Address, format, retention, digest string, length int64, body []byte) error
}
    OriginalMigrator is the write face beside that read face: the bytes one
    archive dump lands under, with the reference metadata the import marks it
    by.

    The redline list (3 rules): MUST store body under key beside the reference
    metadata the import names. MUST keep the reference metadata of one import
    effective after a later Put. MUST NOT expose a reference whose original
    bytes are not stored.

type OriginalReader interface {
	// GetOriginal returns the bytes stored under key and whether the tier holds
	// any under it.
	GetOriginal(ctx context.Context, key event.EventID) ([]byte, bool, error)
}
    OriginalReader is the read face of the migrated originals: the bytes one
    key holds and whether the tier holds any under it. Its signature carries
    built-in and event types only, so the memory tier's archive face satisfies
    it structurally and no package edge is required in either direction.

    The redline list (3 rules): MUST return the bytes stored under key byte
    for byte. MUST report whether the tier holds any bytes under that key.
    MUST NOT answer an absent key as an empty original.

type PlanChange struct {
	// Kind classifies the change.
	Kind PlanChangeKind
	// AgentID names the agent the change applies to.
	AgentID event.AgentID
	// Summary is a short readable summary of the declaration the change carries.
	Summary string
}
    PlanChange is one declaration-level change entry: its kind, the agent it
    applies to, and a short summary of the declaration it carries.

type PlanChangeKind int
    PlanChangeKind classifies one declaration-level change entry as an added,
    updated or removed agent declaration.

const (
	// PlanChangeAdded is an agent declaration the change adds.
	PlanChangeAdded PlanChangeKind = iota
	// PlanChangeUpdated is an agent declaration the change updates.
	PlanChangeUpdated
	// PlanChangeRemoved is an agent declaration the change removes.
	PlanChangeRemoved
)
type PromptSource struct {
	// Files is the ordered structural binding of prompt and skill files.
	Files []config.FileRef
	// Version is the content version fixed at the request boundary.
	Version string
}
    PromptSource is the prompt source of one agent: the structural binding that
    names which files the prompt is assembled from, in order, and the content
    version fixed for one request.

    The structural binding belongs to the published generation, while the
    content is read and fixed at the request boundary, so a file may change
    without a new generation.

type Publication struct {
	// Generation is the monotonically increasing number assigned to each actual
	// publication. Reloading byte-identical content yields no new generation.
	Generation uint64
	// Fingerprint is the organization fingerprint of the published declaration.
	Fingerprint string
	// Baseline is the version baseline the publication was admitted against.
	Baseline string
}
    Publication records one admitted publication of an effective binding:
    the monotonically increasing generation, the fingerprint it published and
    the baseline it was admitted against.

type PublicationRecord struct {
	// Generation is the generation the publication assigned.
	Generation Generation
	// Fingerprint is the organization fingerprint the publication published.
	Fingerprint string
	// Result reports how the publication took effect.
	Result PublishResult
	// RollbackSource names the generation the content came back from; it stays
	// zero for a publication built from a fresh change.
	RollbackSource Generation
	// Checks carries every check disposition the publication recorded, each
	// self-contained.
	Checks []CheckAnswer
}
    PublicationRecord is the self-contained ledger unit of one publication: the
    generation it assigned, the fingerprint it published, the result it took and
    the generation its content came back from, beside every check disposition
    it recorded so an exemption stays visible with its reason and scope.
    The unit is committed through the publication recorder alone and shares no
    commit with a task finality transition or a pending notification.

type PublicationRecorder interface {
	// CommitPublication stores one publication record.
	CommitPublication(ctx context.Context, record PublicationRecord) error
}
    PublicationRecorder is the port one publication record is committed through:
    one self-contained unit per publication, beside the task commit port and
    never inside a task commit record.

    The redline list (3 rules): MUST commit one self-contained record per
    publication. MUST keep the publication record outside any task commit
    record. MUST NOT report a publication as recorded before its record is
    stored.

type PublishContract struct {
	// Required names the checks one publication must carry a disposition for.
	Required []CheckName
}
    PublishContract is the publish precondition contract: the checks one
    publication must carry a disposition for. A contract that requires no
    check refuses every publication, so silence is missing evidence and never a
    passing disposition.

type PublishOutcome struct {
	// Publication is the publication in effect once the attempt ended.
	Publication Publication
	// Result reports how the attempt took effect.
	Result PublishResult
}
    PublishOutcome is what one publication attempt reports: the publication in
    effect beside the result of the attempt. A refusal reports the zero outcome
    beside the error that names it.

type PublishResult int
    PublishResult reports how one publication attempt took effect.

const (
	// PublishUnchanged is the zero value: the attempt published nothing because
	// the candidate carries the content already in effect.
	PublishUnchanged PublishResult = iota
	// PublishApplied is a publication that took effect as one new generation
	// built from a fresh change.
	PublishApplied
	// PublishRolledBack is a publication that took effect as one new generation
	// built from the previous full configuration.
	PublishRolledBack
)
type Publisher struct {
	// Has unexported fields.
}
    Publisher is the sole publication entry over one binding holder:
    the startup generation, a lazy or manual reload and a rollback each publish
    through this entry, and no second path switches the effective binding.
    The entry keeps the candidate construction apart from the publication:
    the baseline re-check, the governance dispositions and the record commit
    each end before the effective binding, the generation number, the rollback
    source and the recorded diagnostics switch as one commit.

    A failure before the switch leaves the effective binding untouched and frees
    the candidate-exclusive resources in the reverse of the construction order;
    the shared faces and the resources already in service stay out of that
    release. A superseded generation keeps its resources while a request still
    holds it and retires when the last hold is gone.

func NewPublisher(bindings *Bindings, contract PublishContract, governance Governance, records PublicationRecorder) *Publisher
    NewPublisher returns the publication entry over one binding holder:
    the publish precondition contract to enforce, the governance to ask and the
    recorder to commit each publication record through. The entry starts with no
    generation published, so no rollback source exists yet.

func (p *Publisher) Baseline() string
    Baseline returns the version baseline a candidate records at construction.
    The commit point re-checks the recorded value and discards the candidate if
    a publication advanced it while the candidate was being constructed.

func (p *Publisher) Borrow(ctx context.Context, agentID event.AgentID) (*BindingHandle, error)
    Borrow implements Borrow and opens one request's use of the binding
    effective at the moment the call arrives, through this entry. The returned
    handle carries the retirement drain beside the borrow: releasing it ends the
    use and advances the retirement of the generation the request held.

func (p *Publisher) BorrowDerived(ctx context.Context, ref BindingRef) (*BindingHandle, error)
    BorrowDerived implements Borrow and opens a delegated request's use of
    the generation its initiating borrow took, through this entry and with the
    retirement drain beside it. A reference this holder never issued is refused
    by name, exactly as it is on the borrowing face.

func (p *Publisher) Current() Publication
    Current returns the publication in effect: the generation, the fingerprint
    it published and the baseline it was admitted against. The zero publication
    stands while nothing has been published.

func (p *Publisher) Publish(ctx context.Context, cand Candidate) (PublishOutcome, error)
    Publish runs one prepared candidate through the sole publication entry
    and reports how the attempt took effect. The candidate records the
    baseline of the entry at construction, and the commit point re-checks it:
    a candidate whose baseline a newer publication advanced is discarded instead
    of overwriting that publication. A candidate carrying the fingerprint
    already in effect publishes nothing: no generation is assigned, no record is
    committed and no resource enters service.

    A publication commits its record before the switch, so a record that cannot
    be committed refuses the publication and the effective binding stays as
    it was. An error returned beside an applied outcome reports a retirement
    release that failed after the publication took effect.

func (p *Publisher) Rollback(ctx context.Context, cand Candidate) (PublishOutcome, error)
    Rollback re-publishes the previous full configuration through the same
    entry and reports how the attempt took effect. The candidate carries that
    configuration rebuilt beside freshly constructed live objects: no retired
    execution object is restored, and the rollback assigns one more generation
    even when the content matches the one in effect.

func (p *Publisher) RollbackConfig() (OrgPlan, bool)
    RollbackConfig returns the previous full configuration the rollback rebuilds
    and whether one exists. The caller rebuilds it beside freshly constructed
    live objects and submits that candidate through Rollback; the source stays
    the same across consecutive rollbacks.

type Refusal struct {
	// Notice is the notification the refused delivery carried.
	Notice task.Notice
	// Target is the address the refused delivery named.
	Target event.Address
	// Category names the gate that refused the delivery.
	Category RefusalCategory
	// At is the time the delivery was refused.
	At time.Time
}
    Refusal is one refused delivery in the sender's ledger: the notification it
    carried, the target it named, the gate that refused it and the time of the
    refusal, so the refusal is queryable by the target it named.

type RefusalCategory string
    RefusalCategory names the gate one delivery was refused at.

const (
	// RefusalNotAuthorized is a target outside the sender's deliver_to
	// whitelist.
	RefusalNotAuthorized RefusalCategory = "not_authorized"
	// RefusalUnknownTarget is a target no established site serves.
	RefusalUnknownTarget RefusalCategory = "unknown_target"
	// RefusalTargetNotRunning is a target whose loop is not running.
	RefusalTargetNotRunning RefusalCategory = "target_not_running"
)
type RollbackItem struct {
	// Name names the item: the task identity or the archive file it came from.
	Name string
	// Present reports whether the new path still reads the item back.
	Present bool
	// Verified reports whether the read-back matches the archive bytes or the
	// deterministic fact content; it is false whenever Present is false.
	Verified bool
}
    RollbackItem is one migrated item's state in the new path as a rollback pass
    observes it: whether the new path still reads the item back, and whether
    that read-back matches what the archive carries.

type RollbackReport struct {
	// SourceDigest is the tree digest of the archive root the rollback read.
	SourceDigest string
	// Items accounts every migrated item the rollback looked for.
	Items []RollbackItem
}
    RollbackReport is the report one rollback returns: the digest of the archive
    observed under read-only access and the state of every migrated item in the
    new path, residue named as residue and absence named as absence.

type Router struct {
	// Has unexported fields.
}
    Router routes one submitted change onto the single path its dimension
    belongs to: the FILE face takes effect at the request boundary with
    no gate in its way, and the structural face commits only through the
    publish precondition gate of the sole publication entry — for a fresh
    structural change and for a rollback alike. The two paths never stand in
    for each other, and every refusal keeps its cause chain: the source file,
    the normalized line and the named reason.

func NewRouter(files *FileSource, gate *Publisher) *Router
    NewRouter returns the router over the given file source and publication
    entry. Either may be nil for a caller that only uses the other face;
    a read without a file source and a structural change without a gate are each
    refused by name.

func (r *Router) Commit(ctx context.Context, change Change, cand Candidate) (PublishOutcome, error)
    Commit takes the structural face: the change is routed by the matrix first,
    then committed through the publish precondition gate of the sole publication
    entry. A refusal anywhere leaves the effective binding as it was and reports
    the failure beside its cause chain — the source file, the normalized line
    and the named reason — never as a generic reload failure.

func (r *Router) Read(ctx context.Context, prompt PromptSource) (FileSnapshot, error)
    Read takes the FILE face: the content is read and fixed at the request
    boundary and takes effect at once — no register, no evaluation and no
    precondition gate stands in its way, and no structural generation is
    assigned or advanced.

func (r *Router) Rollback(ctx context.Context, change Change, cand Candidate) (PublishOutcome, error)
    Rollback takes the structural face as well: a rollback rebuilds the previous
    full configuration as one more generation through the same gate and the
    same publication entry, and a rollback arriving through the file channel is
    refused like any other structural change.

type RunningCheck func(addr event.Address, instance event.InstanceID) bool
    RunningCheck reports whether the execution loop of one target site is
    running.

type SendFace struct {
	// Has unexported fields.
}

    SendFace is the internal send seam of one sender: it delivers a notification
    into an explicitly addressed mailbox after the permission, residency and
    liveness gates, and keeps the ledger of the deliveries it refused.

    The seam resolves no target of its own. A delivery naming no complete target
    is refused, and a target outside the registered whitelist is refused beside
    a ledger entry naming it.

func NewSendFace(store *MemoryStore, options ...SendOption) *SendFace
    NewSendFace returns the send seam of one sender over the given store.

func (f *SendFace) Refusals() []Refusal
    Refusals reports the deliveries this sender refused, in the order it refused
    them.

func (f *SendFace) Send(ctx context.Context, delivery Delivery) (SendReceipt, error)
    Send delivers one notification into the explicit target it names. The gates
    run in order — target named, whitelist, residency, liveness — and the first
    one that refuses records a ledger entry and returns its named error.

    A notification already accepted is answered with the entry it took and the
    site the first acceptance chose: a repeat records no second fact and reopens
    no gate.

type SendOption func(*SendFace)
    SendOption configures the send seam at construction.

func WithDeliverTo(targets ...event.AgentID) SendOption
    WithDeliverTo registers the sender's deliver_to whitelist: the agent names
    it may deliver to. The whitelist starts empty, so a sender that registers
    none refuses every target.

func WithRunningCheck(check RunningCheck) SendOption
    WithRunningCheck attaches the liveness check of the execution loops the
    sender delivers to.

type SendReceipt struct {
	// AcceptSeq is the acceptance sequence of the mailbox entry the
	// notification entered.
	AcceptSeq AcceptSeq
	// Duplicate reports that the notification was already accepted, so this
	// delivery appended no second entry and kept the site the first acceptance
	// recorded.
	Duplicate bool
}
    SendReceipt is the outcome of one delivery that passed the gates.

type Session struct {
	// Address is the explicit address this site was established for.
	Address event.Address
	// Instance is the runtime instance that owns the site; a site rebuilt under
	// the same name belongs to a different instance.
	Instance event.InstanceID
	// Mailbox is the acceptance log of the site: one entry per accepted input,
	// in the monotonic order acceptance assigned.
	Mailbox []MailboxEntry
	// Consumed is the consumption position of the mailbox: the longest
	// contiguous completed prefix of the acceptance sequences. Every sequence
	// at or below the position completed its consumption, and the position
	// never advances past an earlier sequence left unfinished: a completion
	// beside a hole is recorded on its own entry while the position stays
	// before the hole. The position is therefore safe to resume after and
	// never a watermark that discards the entries below it. A new input is
	// accepted without waiting for this position to advance.
	Consumed AcceptSeq
}

    Session is the state owner's site for one explicit (agent, session) address:
    the mailbox bookkeeping of the site.

type SkillLoad struct {
	// Skill is the skill name the load was asked for.
	Skill string
	// Agent is the agent whose request carried the load.
	Agent string
	// Loaded reports whether the skill body was read and fixed.
	Loaded bool
	// Version is the content version of the fixed body. It names the body
	// fixed at one request boundary and never a structural generation.
	Version string
	// Body is the skill body fixed at the request boundary that loaded it.
	Body string
	// Reason reports why a load that did not succeed failed.
	Reason string
}
    SkillLoad is the load state one skill load left in the state owner: the
    skill and agent it names, whether it loaded, the content version it fixed at
    that request boundary beside the body fixed with it, and the reason a load
    that did not succeed reports.

type StatePlan struct {
	// Plan is the declaration plan the candidates were constructed from.
	Plan OrgPlan
	// Agents holds the candidate live binding of each agent, keyed by agent
	// identity.
	Agents map[event.AgentID]AgentBinding
	// Leases lists the candidate resources that were registered while the
	// candidates were constructed, in construction order.
	Leases []LeaseRef
}
    StatePlan is what a configuration change prepares before it is admitted:
    the declaration plan together with the candidate live binding of every agent
    and the candidate resource registrations.

    The plan is admitted as a whole. A candidate that is refused or fails part
    way through construction releases only its own resources, so the shared face
    and the live bindings already in effect are never touched.

type Store interface {
	// Establish creates the session site for one explicit (agent, session)
	// address on the given runtime instance and returns it. The address is never
	// defaulted to a most-recent session, and rebuilding an agent under the same
	// name yields a different instance.
	Establish(ctx context.Context, addr event.Address, instance event.InstanceID) (*Session, error)

	// Publish admits a fully prepared plan as the next effective binding
	// generation and returns the publication record. A plan whose baseline was
	// already advanced is discarded instead of overwriting the newer
	// publication, and reloading byte-identical content yields no new
	// generation.
	Publish(ctx context.Context, plan StatePlan) (Publication, error)

	// CloseSession closes the session's own side: new input is refused while the
	// accepted obligations of the site are kept for a recoverable path.
	CloseSession(ctx context.Context, addr event.Address) error

	// AcceptNotice accepts one notification into the mailbox of the target
	// session. Acceptance is idempotent by the notification's stable identity,
	// so a replayed delivery records no second fact, and acceptance never waits
	// for a slow model decision.
	AcceptNotice(ctx context.Context, notice task.Notice) error
}
    Store is the state owner's shared face: the composition root establishes
    the agent and session sites here, admits one prepared plan as the next
    effective binding generation, closes a session's own side, and forwards the
    notifications a task intends to deliver.

    The redline list (4 rules): MUST establish the shared face before any
    candidate binding is prepared. MUST discard a candidate whose baseline was
    advanced during its preparation. MUST NOT overwrite a newer publication
    with an expired candidate, or derive a new generation from byte-identical
    content. MUST NOT default an address to a most-recent session or drop the
    accepted obligations of a closed site.

type Subject string
    Subject is the trusted identity one read is checked against. A read is
    answered for a subject only beside a grant recorded for that subject and
    target.

type ToolKind string
    ToolKind names the native family one registered tool belongs to.

const (
	// ToolKindFile marks a tool of the native file tool set.
	ToolKindFile ToolKind = "file"
	// ToolKindSkill marks a tool of the native skill family.
	ToolKindSkill ToolKind = "skill"
	// ToolKindFunction marks a contributed native callable tool, including the
	// function-tool shape.
	ToolKindFunction ToolKind = "function"
)
type ToolResource struct {
	// Name is the tool name the request carries.
	Name string
	// Kind is the native family the tool belongs to.
	Kind ToolKind
	// Source names where the tool was constructed from.
	Source string
}
    ToolResource is the registration record of one native tool: the name the
    model sees beside the family it belongs to and the source it was read from.

type ToolResources struct {
	// Has unexported fields.
}
    ToolResources aggregates the registered native tools of one agent behind the
    tool-set slot of its effective binding: the registration records of every
    tool that entered the request beside the skill load state those tools write
    back, and the tool sets whose Close one resource lease governs.

    The skill load view is fixed per request boundary: the first load of one
    skill in a request reads and fixes the body, the loads that follow in the
    same request reuse that fixed view, and the request after it reads anew.

func NewToolResources(name string, sets ...trpctool.ToolSet) *ToolResources
    NewToolResources returns the tool resources of one agent over the tool sets
    whose Close one resource lease governs. A nil set is skipped: it carries
    nothing to close.

func (r *ToolResources) Attach(sets ...trpctool.ToolSet)
    Attach registers tool sets beside the resources: their tools stay wherever
    the caller registered them, while their Close runs with these resources.

func (r *ToolResources) Close() error
    Close closes every tool set these resources hold in reverse registration
    order and once each, joining whatever refuses into the returned cause.
    A second close reports nothing left to close instead of closing twice.

func (r *ToolResources) FixateSkillLoad(requestID, agent, name string, read func(string) (SkillLoad, error)) (SkillLoad, error)
    FixateSkillLoad fixes one skill load at the named request boundary:
    the first load of that skill in one request reads the body through the given
    read and keeps it as that request's fixed view, the loads that follow in the
    same request answer from that view without a second read, and the request
    after it reads anew — so a file rewritten mid-request is never spliced into
    the request under way. Whichever way the load ends, its state is recorded
    on these resources: a successful load carries the fixed body and its content
    version, and a refused one carries the reason it refused by.

    An empty request boundary fixes nothing: the read runs and the state is
    recorded, but no view is kept for a later call to answer from.

func (r *ToolResources) Name() string
    Name reports the name identifying these tool resources.

func (r *ToolResources) Register(kind ToolKind, source string, tools ...trpctool.Tool)
    Register records the given native tools as belonging to one family and makes
    them the tools the request carries. A nil tool is skipped.

func (r *ToolResources) Resources() []ToolResource
    Resources reports the registration records in registration order.

func (r *ToolResources) SkillLoad(name string) (SkillLoad, bool)
    SkillLoad reports the load state one skill left in the state owner and
    whether any load of it was recorded.

func (r *ToolResources) SkillLoads() []SkillLoad
    SkillLoads reports every recorded skill load state in skill-name order.

func (r *ToolResources) Tools(context.Context) []trpctool.Tool
    Tools reports the registered native tools of these resources in registration
    order.

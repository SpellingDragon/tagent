package query // import "github.com/SpellingDragon/tagent/query"

Cursor minting and binding: the opaque continuation token is minted beside
the bounded read that produced it and checked against every read it is later
presented with. The token carries its binding inside itself and exposes nothing,
so no caller forges one and no read serves a page from another view.

The check reports the first binding that diverged, in the order the refusals
answer for: a cursor replayed at another revision is ErrCursorRevisionMismatch,
one presented under another authorization scope is ErrCursorScopeChanged and
one presented under another filter is ErrCursorFilterChanged. The bounded read
window is checked by the read service before the binding, so a cursor whose
window has expired answers ErrCursorWindowExpired rather than any of the three
mismatches.

The read-only diagnostic projection of the runtime: what the owners already
hold, read through one port in three semantic units — what the binding
generation names, the session sites the state owner holds, and the tasks the
task owner holds — so an assembly supplies one reading instead of one face per
dimension.

Package query is the runtime's read-only inspection projection: it carries the
bounded, immutable view through which an already committed runtime state is
listed, described and followed across agents, sessions, invocations, tasks,
notices and resource leases.

The projection is never a second source of truth and never grants authorization
of its own: it reads the facts the state, task, memory and plugin owners
already hold, filters them by the scope the caller already has, and answers with
immutable snapshots whose counts are derived from the detail entries rather than
pre-aggregated. This is the phase 1 skeleton: it declares shapes and holds no
behaviour.

VARIABLES

var (
	// ErrDetailOutsideScope refuses a detail read of an object outside the
	// authorization scope of the read it is presented under. The refusal is
	// named so an authorization boundary is told apart from a missing object,
	// and it reports no detail of the object it refuses.
	ErrDetailOutsideScope = errors.New("query: the object lies outside the authorization scope of the read")
	// ErrDetailAbsent refuses a detail read of an object the bounded snapshot at
	// the presented revision does not hold under the presented filter: the
	// object is inside the scope of the read but its detail is not part of the
	// filtered view.
	ErrDetailAbsent = errors.New("query: the bounded snapshot at this revision holds no such object")
)
    ErrDetailOutsideScope and ErrDetailAbsent are the named refusals of the
    bounded read service beside the cursor refusals already declared: an object
    outside the authorization scope of the read and an object the bounded
    snapshot does not hold each answer by name rather than as an empty page.
    Both are exercised through Projection.Detail by the negative example of this
    package's contract examples.

var (
	// ErrCursorRevisionMismatch refuses a cursor replayed at another revision
	// than the one it was minted at: the page it would continue belongs to
	// another read.
	ErrCursorRevisionMismatch = errors.New("query: the cursor was minted at another revision")
	// ErrCursorScopeChanged refuses a cursor presented under another
	// authorization scope than the one it was minted under: a cursor never
	// survives a change of authorization.
	ErrCursorScopeChanged = errors.New("query: the cursor was minted under another authorization scope")
	// ErrCursorFilterChanged refuses a cursor presented under another filter
	// than the one it was minted under: a cursor never survives a change of
	// filter.
	ErrCursorFilterChanged = errors.New("query: the cursor was minted under another filter")
	// ErrCursorWindowExpired refuses a cursor whose bounded read window has
	// expired: the expiry is named instead of continuing from a position outside
	// the servable window.
	ErrCursorWindowExpired = errors.New("query: the cursor's read window has expired")
	// ErrSnapshotBudgetExceeded refuses a read whose demand is larger than the
	// single-snapshot object budget can hold faithfully. The refusal is named so
	// an oversized request is told apart from an empty view: a page larger than
	// the whole budget is never answered with a silently shortened one.
	ErrSnapshotBudgetExceeded = errors.New("query: the request exceeds the single-snapshot budget")
)
    ErrCursorRevisionMismatch, ErrCursorScopeChanged, ErrCursorFilterChanged and
    ErrCursorWindowExpired are the named cursor refusals: a cursor that does not
    match the read it is presented with is refused by name and never answered
    with a wrong page.

var DefaultBudget = Budget{MaxObjects: 4096, MaxBytes: 4 << 20}
    DefaultBudget is the budget a projection is built under when its caller
    names none: one snapshot holds at most this many objects and this many
    estimated bytes, so an unbounded inventory still answers with a bounded,
    named view instead of an unbounded one.

var DetailRetired = DetailState{NoticePhase: NoticePhaseConsumed}
    DetailRetired is the named anchor of one fully consumed notification: the
    delivery reached its consumed phase and the notification receives nothing
    further. The anchor reads that one phase of a notice and nothing else —
    a settled task beside it keeps its own task status reading and a closed
    invocation keeps its own decision reading, so the two are never collapsed
    into one shared label.

var ErrLeaseExplainerMissing = errors.New("query: no execution resource ledger is attached to this read")
    ErrLeaseExplainerMissing reports an explain read asked to explain the
    retention of a registered lease beside no execution resource ledger to read
    it from. The absence is named instead of answering a retention in silence.

var ErrSourceUnreadable = errors.New("query: an inventory source could not be read")
    ErrSourceUnreadable reports an inventory read whose source could not be
    read, so the dimension it feeds cannot be answered. It is the class every
    SourceError answers for, beside the specific cause the source reported.

FUNCTIONS

func DetailBytes(d Detail) int
    DetailBytes estimates the number of bytes one snapshot object holds: the
    identity fields, the state readings and the blocked records of the entry,
    each string by its own length and every other field by one machine word
    beside the fixed base of the entry and of its blocked records. The estimate
    is deterministic — the same entry always reads the same size — so the byte
    bound of a budget names the same boundary on every run and in every page.

TYPES

type AdvanceState string
    AdvanceState is the advancement state of one blocked obligation as the
    projection reads it. The vocabulary answers "how far has this obligation's
    own advance got" and nothing else: it never reports the advance of one
    obligation as the advance of its siblings, and the zero value carries no
    advancement reading at all and is never read as settled.

const (
	// AdvanceWaiting names an obligation whose advancer is named but has not
	// been observed moving it yet: the obligation stays blocked on that party.
	AdvanceWaiting AdvanceState = "waiting"
	// AdvanceMoving names an obligation whose named advancer is acting on it
	// right now; the action covers this obligation alone.
	AdvanceMoving AdvanceState = "moving"
	// AdvanceSettled names an obligation the named advancer has moved forward
	// on its own: its siblings keep their own states and the object stays
	// blocked as long as any obligation of it is unsettled.
	AdvanceSettled AdvanceState = "settled"
)
type Advancer string
    Advancer names the next party that can move one blocked obligation forward:
    the executor, notification transport or storage action the projection
    identifies as the one that will advance that obligation.

const AdvancerDiagnosis Advancer = "diagnosis"
    AdvancerDiagnosis names the diagnosis escalation of a run of undecidable
    probes as the advancement action of that run: an undecidable observation
    raises diagnosis beside the resource and never decides a death of it.

const AdvancerExecutor Advancer = "executor"
    AdvancerExecutor names the executor running a call as the party that moves
    its open execution obligation forward.

const AdvancerExplicitStop Advancer = "explicit-stop"
    AdvancerExplicitStop names the explicit stop handling of a retained
    execution resource as the party that frees it beside a confirmed stop.

const AdvancerNoticeTransport Advancer = "notice-transport"
    AdvancerNoticeTransport names the notification transport and its replay as
    the party that advances a blocked notification obligation: the obligation
    waits on the delivery or the replay of the notification it names. The named
    advancer stays per obligation, and the projection reports this one for the
    delivery obligations the task owners record; other advancers are named
    beside the obligations they advance.

type AgentCounts struct {
	// Configured counts the agent entries whose agent the configuration names.
	Configured int
	// Resident counts the agent entries holding a session site.
	Resident int
	// Busy counts the agent entries currently holding an execution slot.
	Busy int
}
    AgentCounts reports the three agent readings of one view. They overlap on
    purpose: one agent may be configured, resident and busy at once, and each
    number counts the entries carrying that one reading.

type AgentEntry struct {
	// AgentID names the agent the entry describes.
	AgentID event.AgentID
	// Configured reports whether the runtime configuration names this agent.
	Configured bool
	// Resident reports whether this agent holds a session site.
	Resident bool
	// Busy reports whether this agent currently holds an execution slot.
	Busy bool
}
    AgentEntry is one agent's runtime reading, with its three readings kept
    apart: one agent may be configured, resident and busy all at once, and the
    three flags describe that from three angles instead of collapsing into one
    state.

type AgentState struct {
	// Configured reports whether the runtime configuration names the agent.
	Configured bool
	// Resident reports whether the agent holds a session site.
	Resident bool
	// Busy reports whether the agent currently holds an execution slot.
	Busy bool
}
    AgentState is the agent reading of one detail state: the three readings of
    one agent beside each other. They overlap on purpose and are never added
    together into one "running" number.

type AuthorizationScope struct {
	// Agents lists the agent identities the scope covers.
	Agents []event.AgentID
	// Sessions lists the session addresses the scope covers. A session is always
	// scoped to one agent, so it is named by its full address.
	Sessions []event.Address
}
    AuthorizationScope is the set of objects a caller is already authorized
    to read. It is carried into a filter and is never widened by one: an empty
    scope reads nothing, and a dimension outside the scope is excluded rather
    than rewritten.

type Blocker struct {
	// Ref names the object the blocker sits on: the task whose obligation
	// cannot proceed, or the lease whose resource is retained.
	Ref ObjectRef
	// Notice names the blocked notification; its zero value marks a blocker
	// that sits on a retained resource rather than on a notification.
	Notice task.Notice
	// Reason is the readable reason the blocker cannot proceed, kept exactly as
	// its owner records it.
	Reason string
	// Advancer names the next party that advances this blocker: the executor,
	// notification transport, diagnosis escalation or explicit stop handling it
	// waits on.
	Advancer Advancer
	// State is how far the advance of this blocker has got.
	State AdvanceState
	// Retention carries the retention facts when the blocker sits on a retained
	// execution resource; it stays zero beside a blocked notification.
	Retention LeaseRetention
}
    Blocker is one named runtime blocker: an obligation that cannot proceed,
    or an execution resource retained under its key. Each blocker carries the
    identity it is recorded under, the readable reason beside it and the party
    that advances it, so one blocker is explained by name rather than reported
    as a nameless stall.

type BlockerCounts struct {
	// Blockers counts the blockers listed in the report.
	Blockers int
	// Open counts the obligations still open listed in the report.
	Open int
	// Leases counts the lease retentions listed in the report.
	Leases int
}
    BlockerCounts is the derived reading of one report: the numbers counted off
    the very entries the report lists beside them. The counts are taken after
    the authorization filter, so an object the caller may not read is excluded
    before anything is counted.

type BlockerReport struct {
	// Blockers lists every blocker of the filtered view in the stable identity
	// order of the objects they sit on.
	Blockers []Blocker
	// Open lists every obligation a task manager still holds for the filtered
	// view, each beside its own advancer.
	Open []OpenObligation
	// Leases explains every lease registration of the filtered view beside the
	// retention facts of its resource; a registration whose key the ledger
	// holds nothing under reads as Held false.
	Leases []LeaseRetention
	// Counts is derived from the three lists above after the authorization
	// filter.
	Counts BlockerCounts
	// Ledger is the counting face of the execution resource ledger, read beside
	// the retentions; it stays zero beside a read run with no ledger attached.
	Ledger LeaseLedgerCounts
}
    BlockerReport is one authorized, read-only explanation of what keeps the
    runtime from advancing: every blocker named beside its reason and its next
    advancer, every obligation still open, and every registered lease explained
    beside the retention facts of its resource.

type Budget struct {
	// MaxObjects is the largest number of snapshot objects one read may
	// materialize; zero materializes without an object bound.
	MaxObjects int
	// MaxBytes is the largest number of estimated bytes the snapshot objects of
	// one read may hold; zero holds without a byte bound.
	MaxBytes int
}
    Budget bounds what one snapshot read materializes: the largest number of
    snapshot objects and the largest number of estimated bytes of snapshot
    objects one read holds. A read materializes one snapshot object at a time
    and checks the object bound before it builds the next one, so the objects
    one read materializes never exceed MaxObjects, and an object whose size
    would carry the held bytes beyond MaxBytes is the named boundary of the view
    instead of being held silently.

    A zero bound is not a bound at all: a Budget with both fields zero reads
    the complete view, the same way a window below one reading still keeps one
    reading. The budget bounds snapshot objects and never the frozen source
    reading behind them: it answers "how large may one snapshot grow" and never
    re-opens or re-reads the owners underneath.

type Cursor struct {
	// Has unexported fields.
}
    Cursor is the opaque continuation token of one bounded, paged read.

    A cursor is minted together with the read that produced it and stays bound
    to that read's revision, authorization scope and filter. Presenting it
    under another authorization scope or under another filter is refused by name
    (ErrCursorScopeChanged, ErrCursorFilterChanged), replaying it at another
    revision is refused by name (ErrCursorRevisionMismatch), and presenting one
    whose bounded read window has expired is refused as ErrCursorWindowExpired.
    No path silently serves a page from another view: every refusal is named
    so the caller can tell a changed authorization from a changed filter,
    a replayed revision and an expired window. The token is opaque and cannot
    be constructed or inspected outside this package: it exposes no field,
    the zero value is not a usable cursor, and the revision, scope and filter it
    was minted under stay inside it. The scope a cursor is bound to is the scope
    its caller already held when the read ran; carrying a cursor never widens
    that scope and a cursor never authorizes a read of its own. A page of a list
    read is a bounded run of detail entries of one snapshot revision continued
    by a cursor, so paging stays on one stable sequence while the underlying
    sources advance underneath.

type Detail struct {
	// Ref names the object this detail describes.
	Ref ObjectRef
	// State is the state of the object as the projection read it.
	State DetailState
	// Blocked lists the obligations of the object that cannot proceed, each as
	// its own advancement entry: its own advancer and its own advance state, so
	// an object that waits on a storage reconciliation and on a notice replay at
	// once is explained by two entries and never by one shared advancer.
	Blocked []ObligationProgress
}
    Detail is the projected description of one object: its state and the
    obligations of it that cannot proceed, each with the party that advances
    that obligation and the state of its own advance.

type DetailSource interface {
	// Order returns the stable identity order of every object the frozen
	// reading holds, a copy of its own: kinds in their declared sequence —
	// agent, session, invocation, task, notice, lease — and inside one kind by
	// the identity fields of that kind. No content field takes part, so a changed
	// state never moves an object, and the pages of one reading concatenate into
	// exactly this sequence.
	Order() []ObjectRef
	// DetailOf materializes the snapshot object of one identity and reports
	// whether the frozen reading holds that identity at all.
	DetailOf(ref ObjectRef) (Detail, bool)
}
    DetailSource is the frozen detail source of one reading: the stable identity
    order of the runtime objects beside the snapshot object of one identity,
    materialized one object at a time. One request materializes the snapshot
    objects it serves — a page serves its own entries — and never the whole
    set before serving one page, so the single-snapshot object and byte budget
    bounds what one read holds. The source is frozen beside its reading:
    it answers with the content of that one gathering while the owners advance
    underneath. An identity is read by the identity fields its kind names,
    so two references naming one object answer with one entry (see ObjectRef).
    红线清单（4 条）： MUST 在一次 Freeze 的读数内固定内容，owner 前进不改写已冻结的身份序与明细 MUST
    按需物化单个对象，绝不为服务一页而先物化整个集合 MUST NOT 以空明细冒充未持有的身份：持有性由具名 bool 回答 MUST NOT 跨
    revision 混用身份序与明细

type DetailState struct {
	// Agent carries the three readings of an agent object, kept apart: the
	// same agent may be configured, resident and busy at once and no reading
	// is derived from another.
	Agent AgentState
	// Session carries the mailbox readings of a session object.
	Session SessionState
	// Decision carries the decision that currently holds the execution slot
	// of an invocation object; the zero value reports no slot held for it.
	Decision event.DecisionID
	// TaskStatus carries the native status of a task object in the very
	// vocabulary its owner records it in; no second status vocabulary is
	// introduced beside it.
	TaskStatus task.Status
	// NoticePhase carries the delivery phase of a notice object.
	NoticePhase NoticePhase
	// Holder carries the registration of a lease object as its holder signed
	// it, the release reference included.
	Holder state.LeaseRef
}
    DetailState is the state of one object as the projection reads it.
    The reading is per kind and carries every real reading of that kind instead
    of compressing them into one aggregate value: an agent reads as its three
    readings beside each other — configured, resident and busy are three
    independent readings one agent may carry all at once — a session reads as
    its mailbox readings, an invocation reads as the decision currently holding
    its execution slot, a task reads as its own task status, a notice reads as
    its delivery phase and a lease reads as the registration its holder signed.
    Only the fields belonging to the kind of the object carry a reading;
    every other field stays zero and is never read as a second vocabulary for
    that kind. The states are kept apart and never collapse into one aggregate
    "running" reading.

type Explainer struct {
	// Has unexported fields.
}

    Explainer is the read-only blocker explanation service of the runtime:
    it reads the owners' own records at read time and answers with every
    blocker named beside its reason and the party that advances it, under the
    authorization scope the caller already holds.

    The service holds faces and nothing else: it records nothing, keeps no copy
    of a blocker between two reads and grants no authorization of its own.
    The authorization filter runs before any owner is read and before anything
    is counted, so an object outside the presented scope is never read and never
    counted.

func NewExplainer(inv *Inventory, leases LeaseExplainer) *Explainer
    NewExplainer returns the blocker explanation service over one runtime
    inventory and one execution resource ledger. The lease face may be absent
    for an assembly beside no execution resources: the reads then explain the
    task and notification blockers and refuse a registered lease by name instead
    of answering its retention in silence.

func (e *Explainer) Explain(ctx context.Context, filter ListFilter) (BlockerReport, error)
    Explain returns one authorized explanation of what keeps the runtime
    from advancing: the blockers, the obligations still open and the lease
    retentions, each beside the party that advances it and beside the accounting
    of the execution resource ledger. A source that cannot be read refuses the
    read by name beside the dimension it affects instead of answering an empty
    report as a normal one.

type Gap struct {
	// From is the first missing revision of the range.
	From Revision
	// To is the last missing revision of the range: every revision from From up
	// to and including To is missing.
	To Revision
	// Reason names why the range is missing, in the gap vocabulary the
	// subscription already reports gaps in (GapKind), so the existing
	// classification stands beside the range carrier instead of being replaced
	// by it.
	Reason GapKind
}
    Gap is the payload of an explicit hole in one increment stream: a range of
    entries is missing, the range itself is checkable against the revisions it
    spans, and the hole is carried as data rather than skipped in silence.

type GapKind string
    GapKind names why an increment stream has a gap: the read window expired,
    the subscription overflowed its bound, or an object was deleted between two
    revisions.

    A gap is always named with its kind and is never reported as a silent
    omission; a consumer that receives a reset re-reads a bounded snapshot
    rather than continuing from a position it cannot trust.

const (
	// GapWindowExpired marks a cursor whose read window has expired.
	GapWindowExpired GapKind = "window-expired"
	// GapOverflow marks a consumer that fell behind the overflow bound.
	GapOverflow GapKind = "overflow"
	// GapDeleted marks an object removed between two revisions.
	GapDeleted GapKind = "deleted"
)
type Generation struct {
	// Agents lists the agent identities the generation configures.
	Agents []event.AgentID
	// Leases lists the resource registrations of the generation, in the order
	// the generation acquired them.
	Leases []state.LeaseRef
}
    Generation is what one published binding generation names: the agents it
    configures and the resources it registers. The two readings are taken from
    the same generation, so a lease is never reported beside an agent set that a
    later reload already replaced.

type Inventory struct {
	// Has unexported fields.
}

    Inventory is the read-only runtime inventory: the detail of the six
    runtime dimensions — agents, sessions, invocations, tasks, notifications
    and resource leases — gathered through the read faces of the owners that
    already hold those facts. One view reads every dimension from one gathering,
    and the counts reported beside the detail are derived from that same detail.

    The inventory is a projection and never an owner: it records nothing, widens
    no authorization and computes no total across dimensions. A source that
    cannot be read is refused by name under the dimensions it affects instead of
    being answered as empty detail.

func NewInventory(reads Reading) *Inventory
    NewInventory returns a runtime inventory over the read port given.
    The inventory holds the reading and nothing else: every fact it answers
    with is read from an owner at read time, and a reading that is not wired at
    all refuses the dimensions it would have answered instead of answering them
    empty.

func (inv *Inventory) Agents(ctx context.Context) ([]AgentEntry, error)
    Agents reads the agent dimension alone and returns one entry per agent
    identity with its three readings kept apart. An unreadable source refuses
    the dimension by name instead of answering an empty one.

func (inv *Inventory) Invocations(ctx context.Context) ([]InvocationEntry, error)
    Invocations reads the invocation dimension alone and returns one entry
    per responsibility range beside its active decision. An unreadable source
    refuses the dimension by name instead of answering an empty one.

func (inv *Inventory) Leases(ctx context.Context) ([]LeaseEntry, error)
    Leases reads the resource-lease dimension alone and returns the held
    registrations in their acquisition order. An unreadable source refuses the
    dimension by name instead of answering an empty one.

func (inv *Inventory) Notices(ctx context.Context) ([]NoticeEntry, error)
    Notices reads the notification dimension alone and returns one entry per
    notification with the delivery phase it stands in. An unreadable source
    refuses the dimension by name instead of answering an empty one.

func (inv *Inventory) Sessions(ctx context.Context) ([]SessionEntry, error)
    Sessions reads the session dimension alone and returns one entry per
    session site. An unreadable source refuses the dimension by name instead of
    answering an empty one.

func (inv *Inventory) Tasks(ctx context.Context) ([]TaskEntry, error)
    Tasks reads the task dimension alone and returns one entry per task beside
    the obligations its manager still holds. An unreadable source refuses the
    dimension by name instead of answering an empty one.

func (inv *Inventory) View(ctx context.Context) (InventoryView, error)
    View reads the six dimensions in one gathering and returns the immutable
    total view of the runtime. The detail and the counts of one view describe
    one reading of the owners, and a source that cannot be read refuses the
    whole view by name instead of answering a partial view as a normal one.

type InventoryCounts struct {
	// Agents reports the three agent readings, each counted off the agent
	// entries on its own.
	Agents AgentCounts
	// Sessions counts the session entries.
	Sessions int
	// Invocations counts the invocation entries.
	Invocations int
	// Tasks counts the task entries.
	Tasks int
	// Notices counts the notification entries.
	Notices int
	// Leases counts the resource-lease entries.
	Leases int
}
    InventoryCounts reports how many detail entries one view holds per
    dimension. Every number is derived from the detail entries of the same view,
    so a count is always a reading of the detail the caller holds beside it and
    never a second measurement taken elsewhere.

type InventoryView struct {
	// Has unexported fields.
}
    InventoryView is one immutable total view of the runtime: the detail of the
    six dimensions beside the readings derived from that detail. Every slice a
    caller reads is a copy, so a write through a returned slice reaches neither
    this view nor any later read of it.

func (v InventoryView) Agents() []AgentEntry
    Agents returns a copy of the agent detail of this view.

func (v InventoryView) Counts() InventoryCounts
    Counts derives the per-dimension readings of this view from its own detail
    entries. A count and the detail behind it are one reading: the number is
    counted off the very entries the caller can list beside it.

    The numbers stay separate readings and are never summed into one:
    the count of one dimension is never added to the count of another, since
    an "agent plus session plus task" total counts unlike things and answers
    no question, and the three agent readings are never added together either,
    since configured, resident and busy are three readings over the same agent
    entries and never one "running" number.

func (v InventoryView) Invocations() []InvocationEntry
    Invocations returns a copy of the invocation detail of this view, the
    pending notification identities of every entry copied beside it.

func (v InventoryView) Leases() []LeaseEntry
    Leases returns a copy of the resource-lease detail of this view.

func (v InventoryView) Notices() []NoticeEntry
    Notices returns a copy of the notification detail of this view, the blocked
    records of every entry copied beside it.

func (v InventoryView) Sessions() []SessionEntry
    Sessions returns a copy of the session detail of this view.

func (v InventoryView) Tasks() []TaskEntry
    Tasks returns a copy of the task detail of this view, the obligation detail
    of every entry copied beside it.

type InvocationEntry struct {
	// InvocationID names the responsibility range the entry describes.
	InvocationID event.InvocationID
	// Address is the agent session the invocation runs in.
	Address event.Address
	// Instance is the runtime instance the invocation runs in.
	Instance event.InstanceID
	// Decision is the decision currently holding the site's execution slot for
	// this invocation; the zero value reports no slot held for it.
	Decision event.DecisionID
	// Pending lists the notification identities whose obligations are still
	// open for this invocation, in the order of their identities.
	Pending []task.Notice
}
    InvocationEntry is one invocation's responsibility range beside the active
    decision and the notifications whose obligations are still open for it.

type LeaseEntry struct {
	// Lease is the registration the release path names the resource by.
	Lease state.LeaseRef
}
    LeaseEntry is one held resource lease, kept exactly as the resource ledger
    registered it.

type LeaseExplainer interface {
	// ExplainLease returns the retained state of the resource registered under
	// ref. A key the ledger holds nothing under answers as not held.
	ExplainLease(ref state.LeaseRef) (LeaseRetention, error)
	// LeaseCounts returns the counting face of the ledger.
	LeaseCounts() LeaseLedgerCounts
}
    LeaseExplainer reads one execution resource ledger: the retained state of
    one resource beside the counts the ledger keeps over its leases. It is a
    read face over facts a ledger owner already holds and records nothing of its
    own. 红线清单（3 条）： MUST 只读：回答账本持有者已记录的事实，自身不记录第二份 MUST 让账本未持有的 ref 以 not held
    回答，绝不臆造保留态 MUST NOT 把逐类计数合成一个总量：分类计数就是审计面

type LeaseLedgerCounts struct {
	// Held counts the resources the ledger currently holds.
	Held int
	// Acquired counts the resources started under a lease.
	Acquired int
	// Adopted counts the resources reattached under a lease.
	Adopted int
	// Reused counts the acquisitions answered from a resource already held
	// instead of starting a second one.
	Reused int
	// Released counts the resources freed beside a confirmed stop.
	Released int
	// Escalated counts the diagnosis escalations undecidable probes raised.
	Escalated int
}
    LeaseLedgerCounts is the counting face of one execution resource ledger:
    the counts the ledger keeps over the leases it acquired, renewed, swept
    and freed. Every number is read from the ledger beside the retention of its
    resources, so a caller reads one accounting rather than two.

type LeaseRetention struct {
	// Ref names the lease registration of the resource.
	Ref state.LeaseRef
	// Held reports whether the ledger still holds the resource under its key.
	Held bool
	// Deadline is the lease deadline the resource is held to.
	Deadline time.Time
	// Renewed counts the lease renewals the resource received.
	Renewed int
	// Adopted reports whether the resource was reattached rather than started.
	Adopted bool
	// ConsecutiveUnknowns counts the undecidable probes since the last probe
	// that did decide. The count is a diagnosis reading: it escalates diagnosis
	// beside the resource and never decides a death of it.
	ConsecutiveUnknowns int
	// Escalations counts the diagnosis escalations raised against the resource.
	Escalations int
	// StopReason carries the reason the last stop of the resource stayed
	// unconfirmed; it is empty beside a stop the ledger observed confirmed.
	StopReason string
	// Obligations lists the obligations the ledger keeps open beside the lease.
	Obligations []string
}
    LeaseRetention is the retained state of one execution resource beside the
    facts its ledger holds it on: the lease deadline, the renewals it received,
    the probe readings recorded against it and the obligations kept open beside
    it. The reading is a projection of what the ledger records and adds no fact
    of its own.

type ListFilter struct {
	// Scope is the authorization scope the caller already holds.
	Scope AuthorizationScope
	// Agents narrows the projection to the named agent identities.
	Agents []event.AgentID
	// Sessions narrows the projection to the named session addresses.
	Sessions []event.Address
	// Kinds narrows the projection to the named object kinds.
	Kinds []ObjectKind
	// States narrows the projection to the named detail states. One named
	// state is a complete reading of one kind and keeps exactly the entries
	// that read the same.
	States []DetailState
}
    ListFilter is the one filter a list, detail or subscription request carries:
    the authorization scope the caller already holds together with the dimension
    filters that narrow the projection.

    The authorization scope is applied before every dimension filter,
    so an object the caller may not read is excluded rather than rewritten,
    and no dimensions are summed together. The agent dimensions in particular
    stay separate: a configured agent, a resident agent and a busy agent are
    three distinct readings taken from the detail entries, never one "running"
    number obtained by adding the three counts together.

type NoticeEntry struct {
	// Notice is the stable identity of the notification.
	Notice task.Notice
	// Target is the address the notification is delivered to.
	Target event.Address
	// TargetInstance is the runtime instance that receives the notification.
	TargetInstance event.InstanceID
	// Phase is the delivery phase the notification stands in, derived from the
	// time points recorded beside it.
	Phase NoticePhase
	// Delivery is the delivery-layer state the task's commit record reports. A
	// notification no commit record owes keeps the zero reading here.
	Delivery task.DeliveryAcceptance
	// AcceptSeq is the acceptance sequence the target's mailbox assigned; zero
	// reports that no mailbox accepted the notification.
	AcceptSeq state.AcceptSeq
	// Claim is the claim state of the accepted input.
	Claim state.ClaimState
	// Completed reports whether the consumption of the accepted input
	// completed.
	Completed bool
	// Blocked lists the blocked records pointing at this notification, each
	// with the readable reason it cannot proceed.
	Blocked []task.BlockedObligation
}
    NoticeEntry is one notification's delivery detail as every owner records it:
    the delivery-layer state of the commit record beside the mailbox time points
    of the accepting site and the blocked records pointing at it.

type NoticePhase string
    NoticePhase names the delivery phase one notification stands in. The phases
    are distinct readings of the delivery path: a notification occupies exactly
    one of them at a time and no reading adds them together.

const (
	// NoticePhaseOwed names a notification no target has accepted: the commit
	// record still owes its delivery.
	NoticePhaseOwed NoticePhase = "owed"
	// NoticePhaseAccepted names a notification the target accepted into its
	// mailbox and no executor claimed yet.
	NoticePhaseAccepted NoticePhase = "accepted"
	// NoticePhaseClaimed names a notification one executor claimed and
	// consumed nothing of yet.
	NoticePhaseClaimed NoticePhase = "claimed"
	// NoticePhaseConsumed names a notification whose consumption completed.
	NoticePhaseConsumed NoticePhase = "consumed"
)
type ObjectKind string
    ObjectKind names the kind of object a reference or a detail entry describes.

const (
	// ObjectAgent is an agent object.
	ObjectAgent ObjectKind = "agent"
	// ObjectSession is a session object.
	ObjectSession ObjectKind = "session"
	// ObjectInvocation is an invocation object.
	ObjectInvocation ObjectKind = "invocation"
	// ObjectTask is a task object.
	ObjectTask ObjectKind = "task"
	// ObjectNotice is a notice object.
	ObjectNotice ObjectKind = "notice"
	// ObjectLease is a resource lease object.
	ObjectLease ObjectKind = "lease"
)
type ObjectRef struct {
	// Kind names the kind of object the reference describes.
	Kind ObjectKind
	// Address is the explicit agent session the object belongs to.
	Address event.Address
	// InstanceID names the runtime instance the object belongs to: a reference
	// never names an object without saying which runtime instance holds it, and
	// it uses the same instance identity the event vocabulary already defines
	// rather than a second one invented here.
	InstanceID event.InstanceID
	// InvocationID names an invocation object; it is empty for another kind.
	InvocationID event.InvocationID
	// TaskID names a task object; it is empty for another kind.
	TaskID event.TaskID
	// Notice names a notice object; it is empty for another kind.
	Notice task.Notice
	// Lease names a resource lease object; it is empty for another kind.
	Lease state.LeaseRef
}
    ObjectRef names one object of the projection without carrying its detail:
    the kind of object it is together with the identity that names it.

    Only the identity field matching Kind is populated, and that identity stays
    in the typed form its kind already defines, so no second, flattened identity
    vocabulary is introduced beside the event, task and state identifiers.
    Two references name one object by that identity, never by struct equality:
      - two references name the same object when Kind and the identity fields of
        that kind match
      - an agent is named by Address.AgentID, a session by Address and
        InstanceID
      - an invocation is named by InvocationID, a task by TaskID, a lease by
        Lease.Kind and Lease.Key
      - a notice is named by the TaskID, Round, Kind and Seq of Notice
      - every other field of a reference is content beside that identity and
        takes no part in equality
      - two agent references that differ only in SessionID name one agent,
        not two
      - compare references by that identity, never by struct equality or field
        by field: a key of the whole struct splits one object into several
      - the stable identity order of a reading sorts by this identity, kinds
        first and then the identity fields of the kind (see DetailSource.Order)

type ObligationProgress struct {
	// Obligation is the blocked obligation as its owner reports it.
	Obligation task.BlockedObligation
	// Advancer names the party that moves this obligation forward: the
	// executor, notification transport or storage action this obligation alone
	// waits on.
	Advancer Advancer
	// State is how far the advance of this obligation has got.
	State AdvanceState
}
    ObligationProgress is one blocked obligation of an object together with the
    party that advances that obligation and the state of its own advance.

    Advancement is strictly per obligation: one obligation moves forward on its
    own and never carries its siblings with it, no read ever advances or skips a
    set of obligations in one batch, and every obligation keeps its own advancer
    and its own state so the advancement of the object is readable obligation by
    obligation instead of as one shared step.

type OpenObligation struct {
	// Ref names the task the obligation belongs to.
	Ref ObjectRef
	// Obligation is the open obligation as its owner classifies it.
	Obligation task.Obligation
	// Advancer names the next party that advances this obligation.
	Advancer Advancer
	// State is how far the advance of this obligation has got.
	State AdvanceState
}
    OpenObligation is one obligation a task manager still holds, explained
    beside the party that advances it. An obligation stays listed while its
    owner holds it open: a queue that holds nothing is not read as a settled
    obligation.

type Projection struct {
	// Has unexported fields.
}
    Projection is the bounded snapshot service over the read-only inventory:
    it takes one immutable reading of the owners at a time, keeps a bounded
    window of recent readings under monotonically increasing snapshot revisions,
    and serves the snapshot, the pages, the details and the subscription
    increments of those revisions.

    The projection records nothing and grants no authorization of its own:
    every read is filtered by the authorization scope it is presented under,
    an object outside that scope is excluded rather than rewritten, and every
    count a caller reports stays derived from the detail entries it received.
    The window is bounded in revisions and not in wall-clock time: a read whose
    revision has left the window is refused as ErrCursorWindowExpired and is
    never served from an expired view.

func NewBoundedProjection(src ReadingSource, window int, budget Budget) *Projection
    NewBoundedProjection returns the bounded snapshot service over one frozen
    reading source under the single-snapshot budget named: the source the
    readings and their per-source watermarks are frozen from, how many recent
    readings stay servable and what one snapshot read may materialize. A window
    below one is held at one reading and a Budget with both bounds zero reads
    the complete view.

func NewProjection(inv *Inventory, window int) *Projection
    NewProjection returns the bounded snapshot service over one inventory:
    how many recent readings stay servable. A window below one is held at one
    reading, so every projection keeps at least its newest reading servable.
    The service reads under the DefaultBudget, so an inventory of any size still
    answers with a bounded, named view instead of an unbounded one.

func (p *Projection) Detail(at Revision, cursor Cursor, filter ListFilter, ref ObjectRef) (Detail, error)
    Detail serves one object of the bounded snapshot at the presented revision,
    continued by the presented cursor under the same authorization scope and
    filter the cursor was minted with, so a snapshot, its pages and its detail
    reads all describe one revision. The reference carries the identity fields
    of the object's kind; the detail answered is the frozen entry of that
    revision.

    An object outside the authorization scope of the read is refused as
    ErrDetailOutsideScope and an object the filtered view does not hold is
    refused as ErrDetailAbsent, so an authorization boundary is never disguised
    as a missing object.

func (p *Projection) Follow(ctx context.Context, sub Subscription, overflow int) (Subscription, []Update, error)
    Follow advances one subscription by one increment batch and answers with
    the position to continue from beside the increments between the presented
    position and the newest reading the projection holds.

    - Follow is the incremental read face beside the snapshot and the pages:
    it derives the increments of the revisions the subscription has not
    consumed, restricted to the filter it was opened under, so one subscription
    advances on its own and never carries another consumer's position with it.
    - The increments are the ordinary named delta of the kept view — one upsert
    per new or changed entry and one delete per entry that left it, a narrowed
    view included — and an unbridgeable range is never answered with a silently
    truncated batch. - overflow is how many revisions one subscription may fall
    behind before its range is unbridgeable; a non-positive overflow tolerates
    no backlog at all. - A bridgeable range answers with the increments from
    the presented position up to the newest reading and a continuation anchored
    at that reading. - An unbridgeable one — the oldest reading the range needs
    has left the bounded window, or the backlog stands beyond the overflow
    bound — answers with a named gap beside a reset: the gap names the whole
    missing range (From the first missing revision, To the last) and its reason,
    the reset re-anchors the per-source watermarks of the newest reading,
    and the returned position stays exactly where it was, so no position ever
    advances across a reset and a consumer re-reads a bounded snapshot before
    it continues. - Follow blocks while the projection holds nothing newer than
    the presented position and returns once the next reading is taken or the
    context is cancelled: the waiting caller is the waiter itself, a cancelled
    context releases it with the context's own error, and no waiter is left
    behind. - A position anchored ahead of every reading taken is refused as
    ErrCursorWindowExpired: it names a view that does not exist rather than one
    the window dropped.

func (p *Projection) List(at Revision, cursor Cursor, filter ListFilter, after ObjectRef, limit int) ([]Detail, error)
    List serves one page of the filtered detail sequence of the bounded
    snapshot at the presented revision, continued by the presented cursor.
    The sequence is the stable identity order of that one revision: while the
    underlying sources advance underneath, every page of one read describes the
    same frozen entries, so paging never duplicates and never skips an entry.
    The zero after starts at the first entry and any later page is continued
    from the identity of the last entry of the page before it; limit is the
    largest page to return and a non-positive limit returns an empty page.
    The page is walked out of the same bounded view the snapshot reports — the
    same budget boundary in the same stable identity order — so the pages of one
    read concatenate into exactly its snapshot details, one page materializes
    its own entries instead of the whole view first, and a page larger than the
    object bound of the snapshot budget is refused as ErrSnapshotBudgetExceeded
    instead of being answered with a silently shortened one.

    A cursor presented at another revision, under another authorization scope
    or under another filter is refused by name, and a revision that has left the
    bounded window is refused as ErrCursorWindowExpired rather than misaligned
    into a page of another view.

func (p *Projection) Snapshot(ctx context.Context, filter ListFilter) (Snapshot, Cursor, error)
    Snapshot takes one fresh bounded reading and answers with its immutable,
    filtered view beside the cursor bound to it: the revision the view was read
    at, one watermark per source and the detail entries the presented filter
    keeps inside the single-snapshot budget. The view is fixed once read — later
    source changes never reach it — and every count derived from its detail
    entries is derived from these entries. The read materializes one snapshot
    object at a time and holds the filtered view inside the budget it is built
    under: what the budget left out is named in Truncated beside the entries
    held here instead of being dropped in silence. The returned cursor continues
    that revision and nothing else: carrying it never widens the scope it was
    minted under.

type Reading interface {
	// Published reads the binding generation currently in effect: the agent
	// identities it configures and the resource registrations it holds. Both
	// readings come from the one generation, so an agent listing and a lease
	// listing of one reading are never paired across two generations.
	Published(ctx context.Context) (Generation, error)
	// Sites reads every session site the state owner holds, each beside the
	// call currently holding the site's execution slot. A site the reader is
	// not granted is refused by the owner that holds it.
	Sites(ctx context.Context) ([]SiteReading, error)
	// Tasks reads every task the task owner holds: its declaration, the status
	// it currently stands in, the obligations its manager keeps open and the
	// notifications its commit record still owes.
	Tasks(ctx context.Context) ([]TaskReading, error)
}
    Reading is the single read port the runtime inventory gathers through:
    one reading of what the binding generation in effect names, of the session
    sites the state owner holds and of the tasks the task owner holds. Every
    method is a read of an owner that already holds the facts, and the three
    methods are the three semantic units of that reading rather than one method
    per owner type.

      - the read path one assembly wires, each element a type of this package:
      - Reading is the owner read port the assembly supplies once
      - NewInventory(reads) is the inventory that gathers six dimensions from
        that port at read time
      - NewInventorySource(inventory) is the production ReadingSource over that
        inventory
      - ReadingSource.Freeze answers one DetailSource beside the per-source
        watermarks of the same gathering
      - DetailSource is the frozen view a reading serves from: Order and
        DetailOf answer one identity at a time
      - NewProjection(inventory, window) and NewBoundedProjection(source,
        window, budget) are the Projection over that source
      - Projection.Snapshot, List, Detail and Subscribe read only frozen
        readings and never re-read the owners
      - a frozen reading is never re-read or rewritten: later owner changes
        never reach it

    红线清单（4 条）： MUST 每次调用都从持有该事实的 owner 现读，逐项如实返回或具名失败 MUST
    让源不可读的维度以具名拒绝回答，绝不以空明细冒充可读 MUST NOT 记录、缓存或合成任何事实：读口自身不是第二个真源 MUST NOT
    放宽授权：越权对象被排除而非被改写

type ReadingSource interface {
	// Freeze takes one immutable gathering of the owners and answers with its
	// frozen detail source beside one watermark per source read at that same
	// gathering.
	Freeze(ctx context.Context) (DetailSource, []SourceWatermark, error)
}
    ReadingSource freezes one immutable reading of the owners: one gathering
    of the runtime inventory and the per-source watermarks read at that same
    moment, kept stable together, so the snapshot, the pages and the details of
    one revision all describe the same frozen content beside the positions of
    the sources they were read from. The production source reads the read-only
    inventory; a counting source beside it counts the snapshot objects a request
    materializes.

    红线清单（4 条）： MUST 一次 Freeze 只取一次读数，明细与水位归属同一个 revision MUST
    让不可读的源以具名错误拒绝，绝不以空读数或“已追上”冒充可读 MUST NOT 缓存或重写冻结读数：同一次 Freeze 的结果不被后续读取改写
    MUST NOT 合并多源水位成一个全局位置，也不跨后端计算滞后

func NewInventorySource(inv *Inventory) ReadingSource
    NewInventorySource returns the production reading source of the projection:
    one gathering of the inventory per freeze, read through the read-only
    inventory the owners already serve. The source freezes the gathering the
    owners answer with and materializes its snapshot objects one identity at a
    time, so a bounded read serves what it needs instead of the whole runtime.

type Reset struct {
	// Watermarks are the per-source watermarks the subscription is re-anchored
	// to, one entry per source, each carrying its own opaque position and its
	// own lag reading.
	Watermarks []SourceWatermark
}
    Reset is the payload of a watermark reset: the per-source watermarks a
    subscription is re-anchored to after an overflow or a hole it cannot bridge.

    A reset never claims continuity: the increments before it and the
    increments after it do not form one continuous sequence, the watermarks it
    carries are a new anchor rather than a continuation of the old positions,
    and a consumer that receives a reset re-reads a bounded snapshot instead of
    trusting a position across it. A reset is never used to paper over a hole:
    an unbridgeable hole is reported as a named gap first and a reset never
    pretends the gap did not happen.

type Revision uint64

    Revision is the bounded snapshot revision one read is anchored to.
    Every list, detail and cursor of that read shares it, so paging stays on one
    stable sequence instead of drifting as the underlying sources advance.

type SessionEntry struct {
	// Address is the explicit agent session the site serves.
	Address event.Address
	// Instance is the runtime instance that owns the site.
	Instance event.InstanceID
	// Accepted counts the inputs the site accepted into its mailbox.
	Accepted int
	// Claimed counts the accepted inputs an executor claimed at least once,
	// consumed ones included.
	Claimed int
	// Completed counts the accepted inputs whose consumption completed.
	Completed int
	// Consumed is the site's own consumption position: the longest contiguous
	// completed prefix of the acceptance sequences.
	Consumed state.AcceptSeq
}
    SessionEntry is one session site's mailbox reading: how far the acceptance,
    the claim and the consumption of its accepted inputs have advanced.

type SessionState struct {
	// Accepted counts the inputs the site accepted into its mailbox.
	Accepted int
	// Claimed counts the accepted inputs an executor claimed at least once,
	// consumed ones included.
	Claimed int
	// Completed counts the accepted inputs whose consumption completed.
	Completed int
	// Consumed is the site's own consumption position: the longest contiguous
	// completed prefix of the acceptance sequences.
	Consumed state.AcceptSeq
}
    SessionState is the session reading of one detail state: how far the
    acceptance, the claim and the consumption of the site's accepted inputs have
    advanced. Every field is read from the site's own state.Session:
      - Accepted is the length of the session mailbox
      - Claimed counts the mailbox entries whose Claim is past
        state.ClaimPending
      - Completed counts the mailbox entries whose Receipt.Completed is set
      - Consumed is the session's own consumption position

    The four readings describe one site, and none of them is derived from
    another.

type SiteReading struct {
	// Session is the owner snapshot of the site, its mailbox and its own
	// consumption position included.
	Session state.Session
	// Invocation names the invocation whose call holds the site's execution
	// slot; it is empty when nobody holds the slot.
	Invocation event.InvocationID
	// Decision names the decision holding the site's execution slot; it is
	// empty when nobody holds the slot.
	Decision event.DecisionID
	// Held reports whether anyone holds the site's execution slot at all.
	Held bool
}
    SiteReading is one session site as its owner holds it, beside the call
    currently holding the site's execution slot. The site snapshot and the slot
    reading belong to one reading of the site, so a site is never reported as
    resident here and busy under another reading.

type Snapshot struct {
	// Revision is the snapshot revision every detail entry of this view shares.
	Revision Revision
	// Watermarks reports one watermark per source the view read from, so a
	// source that lags the snapshot revision is named beside it instead of
	// being hidden behind the single revision, and no reading claims one
	// atomic read across every backend.
	Watermarks []SourceWatermark
	// Details lists one entry per object in the filtered view.
	Details []Detail
	// Truncated names the part of the filtered view the snapshot budget left
	// out beside the entries held here: the zero value reports a complete view
	// and a non-zero one names how much the budget omitted, so a bounded view
	// is never read as the complete one.
	Truncated SnapshotTruncation
}
    Snapshot is one bounded, immutable view of the projection: the revision the
    view was read at and the detail entries it contains.

    The view is bounded and immutable once read, and every count the projection
    reports is derived from these detail entries rather than pre-aggregated
    beside them, so a host page, a model and a training reader all read the
    same derived numbers from the same entries. The view reports the source
    revision it read and never claims one atomic snapshot across every backend.
    The snapshot objects of one view stay inside the single-snapshot budget:
    when the filtered view cannot fit it, Truncated names what the budget left
    out beside the entries held here instead of dropping anything in silence.

type SnapshotTruncation struct {
	// OmittedObjects counts the identities the reference dimensions of the
	// filter keep that the budget left out of the view, in the stable identity
	// order of the reading. With a state dimension active the count names the
	// candidates the reference dimensions keep: the state of an unmaterialized
	// entry is unknown by construction, so the count is the honest extent of the
	// omission rather than a guess at how many of those candidates the state
	// dimension would have kept.
	OmittedObjects int
}
    SnapshotTruncation names the part of one filtered view the snapshot budget
    left out beside the view it did answer with, so a bounded view is never
    mistaken for the complete one and no object is dropped in silence. The zero
    value reports a complete view: every entry the filter keeps is held.

type SourceError struct {
	// Dimension names the dimension whose detail cannot be answered.
	Dimension ObjectKind
	// Cause is the error the source itself reported.
	Cause error
}
    SourceError names the dimension one inventory read cannot answer beside the
    cause its source reported, so a storage failure is told apart from an empty
    runtime and an authorization refusal is told apart from a missing source.

func (e *SourceError) Error() string
    Error reports the unreadable dimension beside the cause.

func (e *SourceError) Is(target error) bool
    Is reports that every source error answers for ErrSourceUnreadable beside
    its own cause.

func (e *SourceError) Unwrap() error
    Unwrap returns the cause the source reported, so a caller matches it with
    errors.Is or errors.As and still reads the named dimension above it.

type SourceLag struct {
	// Amount is the measured distance of the source behind the snapshot
	// revision.
	Amount uint64
	// Unit names the unit Amount is measured in (for example revisions or
	// events); an amount is never reported without its unit.
	Unit string
	// Origin names the party that produced the measurement (for example the
	// source owner or the read service); an amount is never reported without
	// saying where it came from.
	Origin string
}
    SourceLag is a measured lag of one source behind the snapshot revision.
    A lag is carried only when its amount is known, and the amount always says
    its unit and where the measurement came from; a lag nobody measured is
    carried as WatermarkLagUnknown instead of being reported as a zero amount.

type SourceName string
    SourceName names one backend source the projection reads from. It names an
    owner the runtime already has and introduces no new source registry; sources
    are read side by side and never added together into one global position.

type SourcePosition string
    SourcePosition is one source's own position in its own stream. The value
    is opaque: it is source-private, it is never parsed, and two positions of
    different sources are never compared or subtracted to fabricate a global lag
    across backends. A position says only where one source stands; how far that
    source lags the snapshot revision is carried by the lag state beside it and
    never folded into the position itself. The zero value is no position at all.
    A position is carried in the text form its source already names, written as
    SourcePosition("...") and never read back field by field.

type SourceWatermark struct {
	// Source names the backend source this watermark is read from.
	Source SourceName
	// Position is the opaque position the source stands at.
	Position SourcePosition
	// LagState names which of the three readings the lag of this source
	// carries: caught up, lag known or lag unknown.
	LagState WatermarkLagState
	// Lag reports the measured lag and is meaningful only when LagState is
	// WatermarkLagKnown; it carries no amount under either of the other two
	// readings.
	Lag SourceLag
}
    SourceWatermark is one source's position in the projection beside what is
    known about how far that source lags the snapshot revision. The projection
    carries one watermark per source and never merges them into one global
    position: the sources advance independently and the view never claims one
    atomic read across every backend.

type Subscription struct {
	// Revision is the snapshot revision the subscription follows.
	Revision Revision
	// Filter is the filter the subscription was opened under.
	Filter ListFilter
}

    Subscription is one independent consumer's read position in the projection:
    the bounded snapshot revision it follows together with the filter it was
    opened under.

    The revision and the filter are bound together, so paging, detail reads and
    the Update increments a subscription receives all describe the same view,
    and a position whose window has expired is named as such rather than
    silently offset. Several consumers advance on their own, and one that falls
    behind the overflow bound is reset from a fresh snapshot instead of losing
    increments in silence.

type TaskEntry struct {
	// TaskID names the task the entry describes.
	TaskID event.TaskID
	// Spec is the admission declaration of the task.
	Spec task.Spec
	// Status is the status the task currently stands in.
	Status task.Status
	// Obligations is the classified obligation reading of the task's manager,
	// carried beside the task detail rather than summarised away.
	Obligations task.Obligations
}
    TaskEntry is one task's declaration and status beside the obligations its
    manager still holds for it.

type TaskReading struct {
	// TaskID names the task the reading describes.
	TaskID event.TaskID
	// Spec is the admission declaration of the task.
	Spec task.Spec
	// Status is the status the task currently stands in.
	Status task.Status
	// Obligations is the classified obligation reading of the task's manager.
	Obligations task.Obligations
	// Outbox lists the notifications the commit record of the task still owes,
	// each beside the delivery-layer state it stands in.
	Outbox []task.OutboxNotice
}
    TaskReading is one task as its owner holds it: the admission declaration,
    the status it currently stands in, the obligations its manager keeps open
    and the notifications its commit record still owes. The four readings
    describe one task, so an obligation and a declaration of one reading always
    name the same task.

type Update struct {
	// Op names the operation this increment carries.
	Op UpdateOp
	// Revision is the revision this increment is anchored to.
	Revision Revision
	// Ref names the object the increment applies to; it is empty for a gap or a
	// reset, which describe the stream rather than one object.
	Ref ObjectRef
	// Detail carries the object as it now reads when Op is UpdateUpsert; it is
	// empty for the other operations.
	Detail Detail
	// Gap carries the missing range and its reason when Op is UpdateGap; it is
	// empty for the other operations.
	Gap Gap
	// Reset carries the re-anchored watermarks when Op is UpdateReset; it is
	// empty for the other operations.
	Reset Reset
}
    Update is one increment of the projection a subscription follows. It carries
    the four operations upsert, delete, gap and reset beside the revision and
    the object the increment applies to, and only the payload matching Op is
    populated, the way an ObjectRef populates only the identity matching its
    kind.

type UpdateOp string
    UpdateOp names which of the four operations one increment of the projection
    carries: upsert, delete, gap and reset. The vocabulary sits beside the gap
    kinds of GapKind and the carriers already declared here; nothing of the
    earlier increment shapes is removed by it.

const (
	// UpdateUpsert names an increment that reports one object as it now reads:
	// the object and its detail arrive together at the revision the increment
	// is anchored to.
	UpdateUpsert UpdateOp = "upsert"
	// UpdateDelete names an increment that reports one object removed from the
	// projection. A deletion is an ordinary delta of the stream: it removes
	// exactly the object it names, it is never defaulted to a data gap, and it
	// is never read as one.
	UpdateDelete UpdateOp = "delete"
	// UpdateGap names an increment that reports an explicit hole: a range of
	// the stream is missing, the missing range is checkable against the
	// revisions it spans, and the hole is reported instead of being silently
	// skipped.
	UpdateGap UpdateOp = "gap"
	// UpdateReset names an increment that re-anchors the per-source watermarks
	// of the subscription; a reset never claims continuity with the increments
	// before it.
	UpdateReset UpdateOp = "reset"
)
type WatermarkLagState string
    WatermarkLagState names which of the three readings a source's lag carries:
    the source has caught up with the snapshot revision, it lags by a known
    amount, or it lags by an amount nobody has measured. The three readings
    stay apart: an unknown lag is its own state, it is never folded into the
    position, never reduced to a boolean and never reported as a zero lag.

const (
	// WatermarkCaughtUp names a source that has reached the snapshot revision,
	// so there is no lag to report.
	WatermarkCaughtUp WatermarkLagState = "caught-up"
	// WatermarkLagKnown names a source that lags the snapshot revision by a
	// measured amount, carried in Lag together with the unit and the origin of
	// the measurement.
	WatermarkLagKnown WatermarkLagState = "lag-known"
	// WatermarkLagUnknown names a source that lags the snapshot revision by an
	// amount nobody has measured. The unknown lag is expressed as this state and
	// as nothing else: not as a position, not as a boolean and not as a zero
	// amount in Lag.
	WatermarkLagUnknown WatermarkLagState = "lag-unknown"
)

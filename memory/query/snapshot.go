package query

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Revision is the bounded snapshot revision one read is anchored to. Every list,
// detail and cursor of that read shares it, so paging stays on one stable
// sequence instead of drifting as the underlying sources advance.
type Revision uint64

// Snapshot is one bounded, immutable view of the projection: the revision the
// view was read at and the detail entries it contains.
//
// The view is bounded and immutable once read, and every count the projection
// reports is derived from these detail entries rather than pre-aggregated beside
// them, so a host page, a model and a training reader all read the same derived
// numbers from the same entries. The view reports the source revision it read
// and never claims one atomic snapshot across every backend. The snapshot
// objects of one view stay inside the single-snapshot budget: when the filtered
// view cannot fit it, Truncated names what the budget left out beside the
// entries held here instead of dropping anything in silence.
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

// Detail is the projected description of one object: its state and the
// obligations of it that cannot proceed, each with the party that advances that
// obligation and the state of its own advance.
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

// ObligationProgress is one blocked obligation of an object together with the
// party that advances that obligation and the state of its own advance.
//
// Advancement is strictly per obligation: one obligation moves forward on its
// own and never carries its siblings with it, no read ever advances or skips a
// set of obligations in one batch, and every obligation keeps its own advancer
// and its own state so the advancement of the object is readable obligation by
// obligation instead of as one shared step.
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

// AdvanceState is the advancement state of one blocked obligation as the
// projection reads it. The vocabulary answers "how far has this obligation's
// own advance got" and nothing else: it never reports the advance of one
// obligation as the advance of its siblings, and the zero value carries no
// advancement reading at all and is never read as settled.
type AdvanceState string

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

// Advancer names the next party that can move one blocked obligation forward:
// the executor, notification transport or storage action the projection
// identifies as the one that will advance that obligation.
type Advancer string

// DetailState is the state of one object as the projection reads it. The
// reading is per kind and carries every real reading of that kind instead of
// compressing them into one aggregate value: an agent reads as its three
// readings beside each other — configured, resident and busy are three
// independent readings one agent may carry all at once — a session reads as
// its mailbox readings, an invocation reads as the decision currently holding
// its execution slot, a task reads as its own task status, a notice reads as
// its delivery phase and a lease reads as the registration its holder signed.
// Only the fields belonging to the kind of the object carry a reading; every
// other field stays zero and is never read as a second vocabulary for that
// kind. The states are kept apart and never collapse into one aggregate
// "running" reading.
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

// AgentState is the agent reading of one detail state: the three readings of
// one agent beside each other. They overlap on purpose and are never added
// together into one "running" number.
type AgentState struct {
	// Configured reports whether the runtime configuration names the agent.
	Configured bool
	// Resident reports whether the agent holds a session site.
	Resident bool
	// Busy reports whether the agent currently holds an execution slot.
	Busy bool
}

// SessionState is the session reading of one detail state: how far the
// acceptance, the claim and the consumption of the site's accepted inputs have
// advanced. Every field is read from the site's own state.Session:
//   - Accepted is the length of the session mailbox
//   - Claimed counts the mailbox entries whose Claim is past state.ClaimPending
//   - Completed counts the mailbox entries whose Receipt.Completed is set
//   - Consumed is the session's own consumption position
//
// The four readings describe one site, and none of them is derived from another.
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

// DetailRetired is the named anchor of one fully consumed notification: the
// delivery reached its consumed phase and the notification receives nothing
// further. The anchor reads that one phase of a notice and nothing else — a
// settled task beside it keeps its own task status reading and a closed
// invocation keeps its own decision reading, so the two are never collapsed
// into one shared label.
var DetailRetired = DetailState{NoticePhase: NoticePhaseConsumed}

// ErrDetailOutsideScope and ErrDetailAbsent are the named refusals of the
// bounded read service beside the cursor refusals already declared: an object
// outside the authorization scope of the read and an object the bounded
// snapshot does not hold each answer by name rather than as an empty page.
// Both are exercised through Projection.Detail by the negative example of this
// package's contract examples.
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

// AdvancerNoticeTransport names the notification transport and its replay as
// the party that advances a blocked notification obligation: the obligation
// waits on the delivery or the replay of the notification it names. The named
// advancer stays per obligation, and the projection reports this one for the
// delivery obligations the task owners record; other advancers are named
// beside the obligations they advance.
const AdvancerNoticeTransport Advancer = "notice-transport"

// DetailSource is the frozen detail source of one reading: the stable identity
// order of the runtime objects beside the snapshot object of one identity,
// materialized one object at a time. One request materializes the snapshot
// objects it serves — a page serves its own entries — and never the whole set
// before serving one page, so the single-snapshot object and byte budget bounds
// what one read holds. The source is frozen beside its reading: it answers with
// the content of that one gathering while the owners advance underneath. An
// identity is read by the identity fields its kind names, so two references
// naming one object answer with one entry (see ObjectRef).
// 红线清单（4 条）：
// MUST 在一次 Freeze 的读数内固定内容，owner 前进不改写已冻结的身份序与明细
// MUST 按需物化单个对象，绝不为服务一页而先物化整个集合
// MUST NOT 以空明细冒充未持有的身份：持有性由具名 bool 回答
// MUST NOT 跨 revision 混用身份序与明细
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

// ReadingSource freezes one immutable reading of the owners: one gathering of
// the runtime inventory and the per-source watermarks read at that same moment,
// kept stable together, so the snapshot, the pages and the details of one
// revision all describe the same frozen content beside the positions of the
// sources they were read from. The production source reads the read-only
// inventory; a counting source beside it counts the snapshot objects a request
// materializes.
//
// 红线清单（4 条）：
// MUST 一次 Freeze 只取一次读数，明细与水位归属同一个 revision
// MUST 让不可读的源以具名错误拒绝，绝不以空读数或“已追上”冒充可读
// MUST NOT 缓存或重写冻结读数：同一次 Freeze 的结果不被后续读取改写
// MUST NOT 合并多源水位成一个全局位置，也不跨后端计算滞后
type ReadingSource interface {
	// Freeze takes one immutable gathering of the owners and answers with its
	// frozen detail source beside one watermark per source read at that same
	// gathering.
	Freeze(ctx context.Context) (DetailSource, []SourceWatermark, error)
}

// reading is one bounded, immutable reading of the projection: the revision it
// was taken at, the per-source watermarks beside it and the frozen detail
// source it serves from. A reading is fixed at construction and never re-read:
// the snapshot, the pages and the details of that revision all describe the same
// frozen content while the owners keep advancing underneath. The snapshot
// objects of one reading are materialized on demand and within the budget of
// the read asking for them, so one read never materializes the whole set before
// it serves one page. The increments between two readings are derived on demand
// per subscription, so one frozen reading is never rewritten into a
// consumer-specific stream.
type reading struct {
	// revision is the bounded snapshot revision this reading is anchored to.
	revision Revision
	// watermarks are the per-source watermarks read beside the detail.
	watermarks []SourceWatermark
	// order is the stable identity order every page and snapshot of this
	// reading follows.
	order []ObjectRef
	// source materializes the snapshot objects of this reading's identities.
	source DetailSource
}

// Projection is the bounded snapshot service over the read-only inventory: it
// takes one immutable reading of the owners at a time, keeps a bounded window
// of recent readings under monotonically increasing snapshot revisions, and
// serves the snapshot, the pages, the details and the subscription increments
// of those revisions.
//
// The projection records nothing and grants no authorization of its own: every
// read is filtered by the authorization scope it is presented under, an object
// outside that scope is excluded rather than rewritten, and every count a
// caller reports stays derived from the detail entries it received. The window
// is bounded in revisions and not in wall-clock time: a read whose revision has
// left the window is refused as ErrCursorWindowExpired and is never served
// from an expired view.
type Projection struct {
	mu       sync.Mutex
	src      ReadingSource
	window   int
	budget   Budget
	readings []*reading
	next     Revision
	minted   chan struct{}
}

// NewProjection returns the bounded snapshot service over one inventory: how
// many recent readings stay servable. A window below one is held at one
// reading, so every projection keeps at least its newest reading servable. The
// service reads under the DefaultBudget, so an inventory of any size still
// answers with a bounded, named view instead of an unbounded one.
func NewProjection(inv *Inventory, window int) *Projection {
	return NewBoundedProjection(NewInventorySource(inv), window, DefaultBudget)
}

// NewBoundedProjection returns the bounded snapshot service over one frozen
// reading source under the single-snapshot budget named: the source the
// readings and their per-source watermarks are frozen from, how many recent
// readings stay servable and what one snapshot read may materialize. A window
// below one is held at one reading and a Budget with both bounds zero reads the
// complete view.
func NewBoundedProjection(src ReadingSource, window int, budget Budget) *Projection {
	if window < 1 {
		window = 1
	}
	return &Projection{
		src:    src,
		window: window,
		budget: budget,
		next:   1,
		minted: make(chan struct{}),
	}
}

// read takes one fresh bounded reading of the owners and freezes it as the
// next snapshot revision. Every snapshot and every subscription advance goes
// through here, so the list, the detail and the increments of one revision
// share one reading and no revision is read twice into different content.
func (p *Projection) read(ctx context.Context) (*reading, error) {
	frozen, watermarks, err := p.src.Freeze(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	r := &reading{
		revision:   p.next,
		watermarks: cloneWatermarks(watermarks),
		order:      append([]ObjectRef(nil), frozen.Order()...),
		source:     frozen,
	}
	p.next++
	p.readings = append(p.readings, r)
	if len(p.readings) > p.window {
		kept := make([]*reading, p.window)
		copy(kept, p.readings[len(p.readings)-p.window:])
		p.readings = kept
	}
	close(p.minted)
	p.minted = make(chan struct{})
	return r, nil
}

// retained returns the frozen reading one revision describes, or refuses the
// read as ErrCursorWindowExpired once that revision has left the bounded
// window. The zero revision and a revision that was never taken are refused
// the same way: no read is served from a view that does not exist.
func (p *Projection) retained(at Revision) (*reading, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.readings {
		if r.revision == at {
			return r, nil
		}
	}
	return nil, ErrCursorWindowExpired
}

// bounds reports the oldest and the newest revision the bounded window still
// serves. Both are zero while no reading has been taken yet.
func (p *Projection) bounds() (floor, current Revision) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.readings) == 0 {
		return 0, 0
	}
	return p.readings[0].revision, p.readings[len(p.readings)-1].revision
}

// mintedChan returns the channel that closes when the next reading is taken. A
// waiter takes it before it reads the window bounds, so a reading taken in
// between still wakes it instead of leaving it asleep over one increment.
func (p *Projection) mintedChan() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.minted
}

// Snapshot takes one fresh bounded reading and answers with its immutable,
// filtered view beside the cursor bound to it: the revision the view was read
// at, one watermark per source and the detail entries the presented filter
// keeps inside the single-snapshot budget. The view is fixed once read — later
// source changes never reach it — and every count derived from its detail
// entries is derived from these entries. The read materializes one snapshot
// object at a time and holds the filtered view inside the budget it is built
// under: what the budget left out is named in Truncated beside the entries
// held here instead of being dropped in silence. The returned cursor continues
// that revision and nothing else: carrying it never widens the scope it was
// minted under.
func (p *Projection) Snapshot(ctx context.Context, filter ListFilter) (Snapshot, Cursor, error) {
	r, err := p.read(ctx)
	if err != nil {
		return Snapshot{}, Cursor{}, err
	}
	details, trunc := walkBounded(r, filter, p.budget, ObjectRef{}, 0)
	snap := Snapshot{
		Revision:   r.revision,
		Watermarks: cloneWatermarks(r.watermarks),
		Details:    details,
		Truncated:  trunc,
	}
	return snap, mintCursor(r.revision, filter), nil
}

// List serves one page of the filtered detail sequence of the bounded snapshot
// at the presented revision, continued by the presented cursor. The sequence
// is the stable identity order of that one revision: while the underlying
// sources advance underneath, every page of one read describes the same frozen
// entries, so paging never duplicates and never skips an entry. The zero after
// starts at the first entry and any later page is continued from the identity
// of the last entry of the page before it; limit is the largest page to return
// and a non-positive limit returns an empty page. The page is walked out of
// the same bounded view the snapshot reports — the same budget boundary in the
// same stable identity order — so the pages of one read concatenate into
// exactly its snapshot details, one page materializes its own entries instead
// of the whole view first, and a page larger than the object bound of the
// snapshot budget is refused as ErrSnapshotBudgetExceeded instead of being
// answered with a silently shortened one.
//
// A cursor presented at another revision, under another authorization scope or
// under another filter is refused by name, and a revision that has left the
// bounded window is refused as ErrCursorWindowExpired rather than misaligned
// into a page of another view.
func (p *Projection) List(at Revision, cursor Cursor, filter ListFilter, after ObjectRef, limit int) ([]Detail, error) {
	r, err := p.retained(at)
	if err != nil {
		return nil, err
	}
	if err := checkBinding(cursor, at, filter); err != nil {
		return nil, err
	}
	if limit < 1 {
		return []Detail{}, nil
	}
	if p.budget.MaxObjects > 0 && limit > p.budget.MaxObjects {
		return nil, ErrSnapshotBudgetExceeded
	}
	page, _ := walkBounded(r, filter, p.budget, after, limit)
	return page, nil
}

// Detail serves one object of the bounded snapshot at the presented revision,
// continued by the presented cursor under the same authorization scope and
// filter the cursor was minted with, so a snapshot, its pages and its detail
// reads all describe one revision. The reference carries the identity fields
// of the object's kind; the detail answered is the frozen entry of that
// revision.
//
// An object outside the authorization scope of the read is refused as
// ErrDetailOutsideScope and an object the filtered view does not hold is
// refused as ErrDetailAbsent, so an authorization boundary is never disguised
// as a missing object.
func (p *Projection) Detail(at Revision, cursor Cursor, filter ListFilter, ref ObjectRef) (Detail, error) {
	r, err := p.retained(at)
	if err != nil {
		return Detail{}, err
	}
	if err := checkBinding(cursor, at, filter); err != nil {
		return Detail{}, err
	}
	stored, ok := r.source.DetailOf(ref)
	if !ok {
		return Detail{}, ErrDetailAbsent
	}
	if !scopeCovers(filter.Scope, stored.Ref) {
		return Detail{}, ErrDetailOutsideScope
	}
	if !detailAllowed(filter, stored) {
		return Detail{}, ErrDetailAbsent
	}
	return cloneDetail(stored), nil
}

// refID is the comparable identity one detail entry merges under across the
// owners that record it. The identity fields match the kind of the object: an
// agent by its agent identity, a session by its site address and instance, an
// invocation by its invocation identity, a task by its task identity, a
// notification by the stable notification identity and a resource lease by the
// resource it names. Everything else a reference or a detail carries is
// content beside that identity: a re-registered lease, a changed state field or
// a changed reason keeps its identity and is explained as one named upsert
// rather than as a new object and a vanished one.
type refID struct {
	kind       ObjectKind
	addr       event.Address
	instance   event.InstanceID
	invocation event.InvocationID
	task       event.TaskID
	notice     noticeID
	leaseKind  string
	leaseKey   string
}

// refIDOf returns the identity one reference names.
func refIDOf(ref ObjectRef) refID {
	id := refID{kind: ref.Kind}
	switch ref.Kind {
	case ObjectAgent:
		id.addr = event.Address{AgentID: ref.Address.AgentID}
	case ObjectSession:
		id.addr = ref.Address
		id.instance = ref.InstanceID
	case ObjectInvocation:
		id.invocation = ref.InvocationID
	case ObjectTask:
		id.task = ref.TaskID
	case ObjectNotice:
		id.notice = noticeIDOf(ref.Notice)
	case ObjectLease:
		id.leaseKind = ref.Lease.Kind
		id.leaseKey = ref.Lease.Key
	}
	return id
}

// kindRank orders the object kinds into one stable sequence.
func kindRank(kind ObjectKind) int {
	switch kind {
	case ObjectAgent:
		return 0
	case ObjectSession:
		return 1
	case ObjectInvocation:
		return 2
	case ObjectTask:
		return 3
	case ObjectNotice:
		return 4
	case ObjectLease:
		return 5
	}
	return 6
}

// refIDLess orders two identities into one total order: object kinds first,
// then the identity fields of that kind. Every list the projection answers with
// is sorted in this order, so the pages of one revision are one stable
// sequence whatever order the owners list in.
func refIDLess(a, b refID) bool {
	if a.kind != b.kind {
		return kindRank(a.kind) < kindRank(b.kind)
	}
	switch a.kind {
	case ObjectAgent:
		return a.addr.AgentID < b.addr.AgentID
	case ObjectSession:
		return siteLess(a.addr, a.instance, b.addr, b.instance)
	case ObjectInvocation:
		return a.invocation < b.invocation
	case ObjectTask:
		return a.task < b.task
	case ObjectNotice:
		if a.notice.task != b.notice.task {
			return a.notice.task < b.notice.task
		}
		if a.notice.round != b.notice.round {
			return a.notice.round < b.notice.round
		}
		if a.notice.kind != b.notice.kind {
			return a.notice.kind < b.notice.kind
		}
		return a.notice.seq < b.notice.seq
	case ObjectLease:
		if a.leaseKind != b.leaseKind {
			return a.leaseKind < b.leaseKind
		}
		return a.leaseKey < b.leaseKey
	}
	return false
}

// refLess orders two references by the identities they name.
func refLess(a, b ObjectRef) bool {
	return refIDLess(refIDOf(a), refIDOf(b))
}

// agentDetailOf projects one agent entry into its snapshot object: the three
// agent readings beside each other, never added into one aggregate label.
func agentDetailOf(entry AgentEntry) Detail {
	return Detail{
		Ref: ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: entry.AgentID}},
		State: DetailState{Agent: AgentState{
			Configured: entry.Configured,
			Resident:   entry.Resident,
			Busy:       entry.Busy,
		}},
	}
}

// sessionDetailOf projects one session entry into its snapshot object: the
// mailbox readings of the site as its owner records them.
func sessionDetailOf(entry SessionEntry) Detail {
	return Detail{
		Ref: ObjectRef{Kind: ObjectSession, Address: entry.Address, InstanceID: entry.Instance},
		State: DetailState{Session: SessionState{
			Accepted:  entry.Accepted,
			Claimed:   entry.Claimed,
			Completed: entry.Completed,
			Consumed:  entry.Consumed,
		}},
	}
}

// invocationDetailOf projects one invocation entry into its snapshot object:
// the decision currently holding the site's execution slot for the
// responsibility range.
func invocationDetailOf(entry InvocationEntry) Detail {
	return Detail{
		Ref:   ObjectRef{Kind: ObjectInvocation, Address: entry.Address, InstanceID: entry.Instance, InvocationID: entry.InvocationID},
		State: DetailState{Decision: entry.Decision},
	}
}

// taskDetailOf projects one task entry into its snapshot object: the native
// task status beside the blocked obligations of its manager, each kept as its
// own advancement entry.
func taskDetailOf(entry TaskEntry) Detail {
	return Detail{
		Ref:     ObjectRef{Kind: ObjectTask, Address: entry.Spec.Source, InstanceID: entry.Spec.InstanceID, TaskID: entry.TaskID},
		State:   DetailState{TaskStatus: entry.Status},
		Blocked: blockedOf(entry.Obligations.Blocked),
	}
}

// noticeDetailOf projects one notification entry into its snapshot object: the
// delivery phase it stands in beside the blocked records pointing at it.
func noticeDetailOf(entry NoticeEntry) Detail {
	return Detail{
		Ref:     ObjectRef{Kind: ObjectNotice, Address: entry.Target, InstanceID: entry.TargetInstance, Notice: entry.Notice},
		State:   DetailState{NoticePhase: entry.Phase},
		Blocked: blockedOf(entry.Blocked),
	}
}

// leaseDetailOf projects one lease entry into its snapshot object: the
// registration its holder signed, the release reference included. Repeated
// registrations of one resource lease collapse to the newest registration
// before the entry is frozen — the resource is one object and its release
// reference is content — so a re-registered lease is one changed entry and
// never two objects.
func leaseDetailOf(entry LeaseEntry) Detail {
	return Detail{
		Ref:   ObjectRef{Kind: ObjectLease, Lease: entry.Lease},
		State: DetailState{Holder: entry.Lease},
	}
}

// blockedOf projects the blocked records of one object into its per-obligation
// advancement entries, one entry per obligation with its own advancer and its
// own advance state. The delivery obligations the task owners record wait on
// the notification transport, so their advancer is named as such and each
// entry stays as waiting as its owner reports it; the entries are ordered by
// the notification identities they name so the blocked list of one object is
// one stable sequence.
func blockedOf(records []task.BlockedObligation) []ObligationProgress {
	if len(records) == 0 {
		return nil
	}
	out := make([]ObligationProgress, 0, len(records))
	for _, record := range records {
		out = append(out, ObligationProgress{Obligation: record, Advancer: AdvancerNoticeTransport, State: AdvanceWaiting})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if noticeLess(out[i].Obligation.Notice, out[j].Obligation.Notice) {
			return true
		}
		if noticeLess(out[j].Obligation.Notice, out[i].Obligation.Notice) {
			return false
		}
		return out[i].Obligation.Reason < out[j].Obligation.Reason
	})
	return out
}

// materializeOf materializes every snapshot object of one frozen reading in its
// stable identity order beside an index of them by identity. It serves the
// delta derivation, which compares two readings complete; every bounded read
// walks its own view through walkBounded instead.
func materializeOf(r *reading) ([]Detail, map[refID]Detail) {
	details := make([]Detail, 0, len(r.order))
	index := make(map[refID]Detail, len(r.order))
	for _, ref := range r.order {
		d, ok := r.source.DetailOf(ref)
		if !ok {
			continue
		}
		details = append(details, d)
		index[refIDOf(d.Ref)] = d
	}
	return details, index
}

// deltaOf projects one reading as the increments that changed against the
// previous reading, restricted to the detail entries the presented predicate
// keeps: one named upsert per kept entry that appeared or changed and one named
// delete per kept entry that left the kept view, in the same stable identity
// order. A change of a state field beside a stable identity — a re-registered
// lease, a changed reason, a changed state — is one upsert and is never
// reported as a new object beside a vanished one; a removed object and an
// object that passes out of a narrowed view are ordinary named deletes and are
// never defaulted to a gap. Each upsert carries a copy of its detail, so no
// write through a delivered increment reaches the frozen reading. The delta is
// the comparison path between two readings and reads both of them complete, so
// the single-snapshot budget — which bounds what one snapshot read holds — is
// not applied to it.
func deltaOf(prev, cur *reading, keep func(Detail) bool) []Update {
	curDetails, curIndex := materializeOf(cur)
	var prevDetails []Detail
	var prevIndex map[refID]Detail
	if prev != nil {
		prevDetails, prevIndex = materializeOf(prev)
	}
	var out []Update
	for _, d := range curDetails {
		if !keep(d) {
			continue
		}
		if prev != nil {
			if before, ok := prevIndex[refIDOf(d.Ref)]; ok && detailEqual(before, d) {
				continue
			}
		}
		out = append(out, Update{Op: UpdateUpsert, Revision: cur.revision, Ref: d.Ref, Detail: cloneDetail(d)})
	}
	if prev != nil {
		for _, d := range prevDetails {
			if !keep(d) {
				continue
			}
			if curd, ok := curIndex[refIDOf(d.Ref)]; ok && keep(curd) {
				continue
			}
			out = append(out, Update{Op: UpdateDelete, Revision: cur.revision, Ref: d.Ref})
		}
	}
	sort.Slice(out, func(i, j int) bool { return refLess(out[i].Ref, out[j].Ref) })
	return out
}

// detailEqual reports whether two detail entries read the same: the same
// reference content, the same state and the same blocked records. The
// comparison is written out field by field on purpose: the frozen carriers
// carry a slice beside comparable fields and the equality an increment is
// decided on must stay exact and explicit.
func detailEqual(a, b Detail) bool {
	if a.Ref != b.Ref || a.State != b.State || len(a.Blocked) != len(b.Blocked) {
		return false
	}
	for i := range a.Blocked {
		if a.Blocked[i] != b.Blocked[i] {
			return false
		}
	}
	return true
}

// scopeCovers reports whether the authorization scope covers the object the
// reference names. The scope is applied before every dimension filter and is
// never widened by a read: an empty scope covers nothing, an agent-level entry
// covers that agent and the sessions it runs, and a session-level entry covers
// that exact session. A resource lease names no session of its own and is
// covered by any non-empty scope: it is visible to an authorized reader and
// named by the resource it holds rather than by an address it does not have.
func scopeCovers(scope AuthorizationScope, ref ObjectRef) bool {
	if ref.Kind == ObjectLease {
		return len(scope.Agents) > 0 || len(scope.Sessions) > 0
	}
	if ref.Address.IsZero() {
		return false
	}
	for _, id := range scope.Agents {
		if id == ref.Address.AgentID {
			return true
		}
	}
	for _, addr := range scope.Sessions {
		if addr == ref.Address {
			return true
		}
	}
	return false
}

// refAllowed reports whether the authorization scope and the reference
// dimensions of the filter keep the object the reference names. The scope is
// applied first, and each active dimension list only ever narrows the view
// further; a resource lease carries no address and drops out of an agent or
// session narrowing the same way an addressless object must.
func refAllowed(filter ListFilter, ref ObjectRef) bool {
	if !scopeCovers(filter.Scope, ref) {
		return false
	}
	if len(filter.Agents) > 0 && !agentListed(filter.Agents, ref.Address.AgentID) {
		return false
	}
	if len(filter.Sessions) > 0 && !addressListed(filter.Sessions, ref.Address) {
		return false
	}
	if len(filter.Kinds) > 0 && !kindListed(filter.Kinds, ref.Kind) {
		return false
	}
	return true
}

// statesAllow reports whether the state dimension of the filter keeps one
// detail state. A state list is applied after the authorization scope and the
// reference dimensions and only ever narrows the view further. One named state
// is a complete reading of one kind — every reading field of that kind filled
// in — and keeps exactly the entries that read the same, so a narrowed view is
// as precise as the reading it names and never an aggregate label.
func statesAllow(filter ListFilter, state DetailState) bool {
	if len(filter.States) == 0 {
		return true
	}
	for _, want := range filter.States {
		if want == state {
			return true
		}
	}
	return false
}

// detailAllowed reports whether the presented filter keeps one detail entry.
func detailAllowed(filter ListFilter, d Detail) bool {
	return refAllowed(filter, d.Ref) && statesAllow(filter, d.State)
}

func agentListed(list []event.AgentID, id event.AgentID) bool {
	for _, want := range list {
		if want == id {
			return true
		}
	}
	return false
}

func addressListed(list []event.Address, addr event.Address) bool {
	for _, want := range list {
		if want == addr {
			return true
		}
	}
	return false
}

func kindListed(list []ObjectKind, kind ObjectKind) bool {
	for _, want := range list {
		if want == kind {
			return true
		}
	}
	return false
}

// walkBounded materializes the bounded filtered view of one frozen reading:
// the detail entries the presented filter keeps inside the single-snapshot
// budget, in the stable identity order of the reading, beside what the budget
// left out. The walk applies the authorization scope and the reference
// dimensions of the filter before anything is materialized, so an unauthorized
// identity is never read and never counted; it then materializes one snapshot
// object at a time and checks the object bound before it builds the next one,
// so the objects one walk materializes never exceed the object bound, and an
// entry whose size would carry the held bytes beyond the byte bound is the
// named boundary of the view — that entry and every identity after it stay
// out of the view and are counted in the truncation instead of being held or
// dropped in silence.
//
// A positive limit serves one page: the walk collects the entries after the
// presented identity until the page is full and stops there, so one page
// materializes its own entries instead of the whole view. The walk is
// deterministic — one reading, filter and budget always stop at the same
// boundary in the same order — so the pages of one read concatenate into
// exactly the detail entries of its snapshot, and the truncation is reported
// for the complete view only: a full page names its own boundary by
// pagination.
func walkBounded(r *reading, filter ListFilter, budget Budget, after ObjectRef, limit int) ([]Detail, SnapshotTruncation) {
	collects := func(ref ObjectRef) bool {
		if refIDOf(after) == (refID{}) {
			return true
		}
		return refLess(after, ref)
	}
	var kept []Detail
	var trunc SnapshotTruncation
	held := 0
	materialized := 0
	bounded := false
	for _, ref := range r.order {
		if !refAllowed(filter, ref) {
			continue
		}
		if limit > 0 && len(kept) >= limit {
			break
		}
		page := collects(ref)
		if bounded {
			if page {
				trunc.OmittedObjects++
			}
			continue
		}
		if budget.MaxObjects > 0 && materialized >= budget.MaxObjects {
			bounded = true
			if page {
				trunc.OmittedObjects++
			}
			continue
		}
		d, ok := r.source.DetailOf(ref)
		if !ok {
			continue
		}
		materialized++
		if !statesAllow(filter, d.State) {
			continue
		}
		size := DetailBytes(d)
		if budget.MaxBytes > 0 && held+size > budget.MaxBytes {
			bounded = true
			if page {
				trunc.OmittedObjects++
			}
			continue
		}
		held += size
		if page {
			kept = append(kept, cloneDetail(d))
		}
	}
	return kept, trunc
}

const (
	// wordBytes is the estimated size of one word-sized carrier field.
	wordBytes = 8
	// detailBaseBytes is the fixed base of one snapshot object beside its fields.
	detailBaseBytes = 64
	// blockedEntryBytes is the fixed base of one blocked record beside its fields.
	blockedEntryBytes = 64
)

// DetailBytes estimates the number of bytes one snapshot object holds: the
// identity fields, the state readings and the blocked records of the entry,
// each string by its own length and every other field by one machine word
// beside the fixed base of the entry and of its blocked records. The estimate
// is deterministic — the same entry always reads the same size — so the byte
// bound of a budget names the same boundary on every run and in every page.
func DetailBytes(d Detail) int {
	total := detailBaseBytes
	total += len(d.Ref.Address.AgentID) + len(d.Ref.Address.SessionID)
	total += len(d.Ref.InstanceID) + len(d.Ref.InvocationID) + len(d.Ref.TaskID)
	total += len(d.Ref.Notice.TaskID) + len(d.Ref.Notice.Kind) + len(d.Ref.Notice.Event)
	total += len(d.Ref.Lease.Kind) + len(d.Ref.Lease.Key) + len(d.Ref.Lease.Ref)
	total += 3 * wordBytes
	total += 4 * wordBytes
	total += len(d.State.Decision) + wordBytes + len(d.State.NoticePhase)
	total += len(d.State.Holder.Kind) + len(d.State.Holder.Key) + len(d.State.Holder.Ref)
	for _, b := range d.Blocked {
		total += blockedEntryBytes
		total += len(b.Obligation.Notice.TaskID) + len(b.Obligation.Notice.Kind) + len(b.Obligation.Notice.Event)
		total += len(b.Obligation.Reason) + len(b.Advancer) + len(b.State)
		total += 2 * wordBytes
	}
	return total
}

// cloneDetail copies one detail entry, blocked records included, so the frozen
// reading and every later read are unreachable through it.
func cloneDetail(d Detail) Detail {
	out := d
	if len(d.Blocked) > 0 {
		out.Blocked = append([]ObligationProgress(nil), d.Blocked...)
	}
	return out
}

// cloneWatermarks copies the per-source watermarks of one reading, so the
// frozen reading and every later read are unreachable through a returned list.
func cloneWatermarks(marks []SourceWatermark) []SourceWatermark {
	return append([]SourceWatermark(nil), marks...)
}

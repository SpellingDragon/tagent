package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// AgentEntry is one agent's runtime reading, with its three readings kept
// apart: one agent may be configured, resident and busy all at once, and the
// three flags describe that from three angles instead of collapsing into one
// state.
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

// SessionEntry is one session site's mailbox reading: how far the acceptance,
// the claim and the consumption of its accepted inputs have advanced.
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

// InvocationEntry is one invocation's responsibility range beside the active
// decision and the notifications whose obligations are still open for it.
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

// TaskEntry is one task's declaration and status beside the obligations its
// manager still holds for it.
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

// NoticeEntry is one notification's delivery detail as every owner records it:
// the delivery-layer state of the commit record beside the mailbox time points
// of the accepting site and the blocked records pointing at it.
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

// LeaseEntry is one held resource lease, kept exactly as the resource ledger
// registered it.
type LeaseEntry struct {
	// Lease is the registration the release path names the resource by.
	Lease state.LeaseRef
}

// NoticePhase names the delivery phase one notification stands in. The phases
// are distinct readings of the delivery path: a notification occupies exactly
// one of them at a time and no reading adds them together.
type NoticePhase string

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

// ErrSourceUnreadable reports an inventory read whose source could not be read,
// so the dimension it feeds cannot be answered. It is the class every
// SourceError answers for, beside the specific cause the source reported.
var ErrSourceUnreadable = errors.New("query: an inventory source could not be read")

// SourceError names the dimension one inventory read cannot answer beside the
// cause its source reported, so a storage failure is told apart from an empty
// runtime and an authorization refusal is told apart from a missing source.
type SourceError struct {
	// Dimension names the dimension whose detail cannot be answered.
	Dimension ObjectKind
	// Cause is the error the source itself reported.
	Cause error
}

// Error reports the unreadable dimension beside the cause.
func (e *SourceError) Error() string {
	return fmt.Sprintf("query: the %s dimension cannot be read: %v", e.Dimension, e.Cause)
}

// Unwrap returns the cause the source reported, so a caller matches it with
// errors.Is or errors.As and still reads the named dimension above it.
func (e *SourceError) Unwrap() error {
	return e.Cause
}

// Is reports that every source error answers for ErrSourceUnreadable beside its
// own cause.
func (e *SourceError) Is(target error) bool {
	return target == ErrSourceUnreadable
}

// 契约: docs/wiki/agent/agent-architecture.md#core-components
//
// Inventory is the read-only runtime inventory: the detail of the six runtime
// dimensions — agents, sessions, invocations, tasks, notifications and resource
// leases — gathered through the read faces of the owners that already hold
// those facts. One view reads every dimension from one gathering, and the
// counts reported beside the detail are derived from that same detail.
//
// The inventory is a projection and never an owner: it records nothing, widens
// no authorization and computes no total across dimensions. A source that
// cannot be read is refused by name under the dimensions it affects instead of
// being answered as empty detail.
type Inventory struct {
	reads Reading
}

// NewInventory returns a runtime inventory over the read port given. The
// inventory holds the reading and nothing else: every fact it answers with is
// read from an owner at read time, and a reading that is not wired at all
// refuses the dimensions it would have answered instead of answering them
// empty.
func NewInventory(reads Reading) *Inventory {
	return &Inventory{reads: reads}
}

// View reads the six dimensions in one gathering and returns the immutable
// total view of the runtime. The detail and the counts of one view describe one
// reading of the owners, and a source that cannot be read refuses the whole
// view by name instead of answering a partial view as a normal one.
func (inv *Inventory) View(ctx context.Context) (InventoryView, error) {
	g := inv.gather(ctx)
	if err := g.namedError(); err != nil {
		return InventoryView{}, err
	}
	return g.view(), nil
}

// Agents reads the agent dimension alone and returns one entry per agent
// identity with its three readings kept apart. An unreadable source refuses the
// dimension by name instead of answering an empty one.
func (inv *Inventory) Agents(ctx context.Context) ([]AgentEntry, error) {
	g := inv.gather(ctx)
	if cause, ok := g.errs[ObjectAgent]; ok {
		return nil, &SourceError{Dimension: ObjectAgent, Cause: cause}
	}
	return g.agents, nil
}

// Sessions reads the session dimension alone and returns one entry per session
// site. An unreadable source refuses the dimension by name instead of answering
// an empty one.
func (inv *Inventory) Sessions(ctx context.Context) ([]SessionEntry, error) {
	g := inv.gather(ctx)
	if cause, ok := g.errs[ObjectSession]; ok {
		return nil, &SourceError{Dimension: ObjectSession, Cause: cause}
	}
	return g.sessions, nil
}

// Invocations reads the invocation dimension alone and returns one entry per
// responsibility range beside its active decision. An unreadable source refuses
// the dimension by name instead of answering an empty one.
func (inv *Inventory) Invocations(ctx context.Context) ([]InvocationEntry, error) {
	g := inv.gather(ctx)
	if cause, ok := g.errs[ObjectInvocation]; ok {
		return nil, &SourceError{Dimension: ObjectInvocation, Cause: cause}
	}
	return g.invocations, nil
}

// Tasks reads the task dimension alone and returns one entry per task beside
// the obligations its manager still holds. An unreadable source refuses the
// dimension by name instead of answering an empty one.
func (inv *Inventory) Tasks(ctx context.Context) ([]TaskEntry, error) {
	g := inv.gather(ctx)
	if cause, ok := g.errs[ObjectTask]; ok {
		return nil, &SourceError{Dimension: ObjectTask, Cause: cause}
	}
	return g.tasks, nil
}

// Notices reads the notification dimension alone and returns one entry per
// notification with the delivery phase it stands in. An unreadable source
// refuses the dimension by name instead of answering an empty one.
func (inv *Inventory) Notices(ctx context.Context) ([]NoticeEntry, error) {
	g := inv.gather(ctx)
	if cause, ok := g.errs[ObjectNotice]; ok {
		return nil, &SourceError{Dimension: ObjectNotice, Cause: cause}
	}
	return g.notices, nil
}

// Leases reads the resource-lease dimension alone and returns the held
// registrations in their acquisition order. An unreadable source refuses the
// dimension by name instead of answering an empty one.
func (inv *Inventory) Leases(ctx context.Context) ([]LeaseEntry, error) {
	g := inv.gather(ctx)
	if cause, ok := g.errs[ObjectLease]; ok {
		return nil, &SourceError{Dimension: ObjectLease, Cause: cause}
	}
	return g.leases, nil
}

// InventoryView is one immutable total view of the runtime: the detail of the
// six dimensions beside the readings derived from that detail. Every slice a
// caller reads is a copy, so a write through a returned slice reaches neither
// this view nor any later read of it.
type InventoryView struct {
	agents      []AgentEntry
	sessions    []SessionEntry
	invocations []InvocationEntry
	tasks       []TaskEntry
	notices     []NoticeEntry
	leases      []LeaseEntry
}

// Agents returns a copy of the agent detail of this view.
func (v InventoryView) Agents() []AgentEntry {
	return append([]AgentEntry(nil), v.agents...)
}

// Sessions returns a copy of the session detail of this view.
func (v InventoryView) Sessions() []SessionEntry {
	return append([]SessionEntry(nil), v.sessions...)
}

// Invocations returns a copy of the invocation detail of this view, the
// pending notification identities of every entry copied beside it.
func (v InventoryView) Invocations() []InvocationEntry {
	out := append([]InvocationEntry(nil), v.invocations...)
	for i := range out {
		out[i].Pending = append([]task.Notice(nil), out[i].Pending...)
	}
	return out
}

// Tasks returns a copy of the task detail of this view, the obligation detail
// of every entry copied beside it.
func (v InventoryView) Tasks() []TaskEntry {
	out := append([]TaskEntry(nil), v.tasks...)
	for i := range out {
		out[i].Obligations.Blocked = append([]task.BlockedObligation(nil), out[i].Obligations.Blocked...)
		out[i].Obligations.Open = append([]task.Obligation(nil), out[i].Obligations.Open...)
	}
	return out
}

// Notices returns a copy of the notification detail of this view, the blocked
// records of every entry copied beside it.
func (v InventoryView) Notices() []NoticeEntry {
	out := append([]NoticeEntry(nil), v.notices...)
	for i := range out {
		out[i].Blocked = append([]task.BlockedObligation(nil), out[i].Blocked...)
	}
	return out
}

// Leases returns a copy of the resource-lease detail of this view.
func (v InventoryView) Leases() []LeaseEntry {
	return append([]LeaseEntry(nil), v.leases...)
}

// Counts derives the per-dimension readings of this view from its own detail
// entries. A count and the detail behind it are one reading: the number is
// counted off the very entries the caller can list beside it.
//
// The numbers stay separate readings and are never summed into one: the count
// of one dimension is never added to the count of another, since an "agent plus
// session plus task" total counts unlike things and answers no question, and
// the three agent readings are never added together either, since configured,
// resident and busy are three readings over the same agent entries and never
// one "running" number.
func (v InventoryView) Counts() InventoryCounts {
	counts := InventoryCounts{
		Sessions:    len(v.sessions),
		Invocations: len(v.invocations),
		Tasks:       len(v.tasks),
		Notices:     len(v.notices),
		Leases:      len(v.leases),
	}
	for _, agent := range v.agents {
		if agent.Configured {
			counts.Agents.Configured++
		}
		if agent.Resident {
			counts.Agents.Resident++
		}
		if agent.Busy {
			counts.Agents.Busy++
		}
	}
	return counts
}

// InventoryCounts reports how many detail entries one view holds per dimension.
// Every number is derived from the detail entries of the same view, so a count
// is always a reading of the detail the caller holds beside it and never a
// second measurement taken elsewhere.
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

// AgentCounts reports the three agent readings of one view. They overlap on
// purpose: one agent may be configured, resident and busy at once, and each
// number counts the entries carrying that one reading.
type AgentCounts struct {
	// Configured counts the agent entries whose agent the configuration names.
	Configured int
	// Resident counts the agent entries holding a session site.
	Resident int
	// Busy counts the agent entries currently holding an execution slot.
	Busy int
}

// inventoryDimensions fixes the order affected dimensions are named in when a
// read fails on more than one of them.
var inventoryDimensions = []ObjectKind{ObjectAgent, ObjectSession, ObjectInvocation, ObjectTask, ObjectNotice, ObjectLease}

// siteReading is one site's owner reading shared by every dimension projecting
// it: the session snapshot of the site beside the execution slot it currently
// holds.
type siteReading struct {
	session    state.Session
	invocation event.InvocationID
	decision   event.DecisionID
	held       bool
}

// gathering is one read pass over the owners: every source answers once and
// every dimension is projected from those answers, so one view and its counts
// share one reading and no dimension is read twice into different numbers.
type gathering struct {
	sites       []siteReading
	agents      []AgentEntry
	sessions    []SessionEntry
	invocations []InvocationEntry
	tasks       []TaskEntry
	notices     []NoticeEntry
	leases      []LeaseEntry
	owed        []task.OutboxNotice
	errs        map[ObjectKind]error
}

// fail records one source failure under every dimension it makes unreadable,
// keeping the first cause seen for a dimension.
func (g *gathering) fail(err error, dims ...ObjectKind) {
	for _, dim := range dims {
		if _, seen := g.errs[dim]; !seen {
			g.errs[dim] = err
		}
	}
}

// namedError returns the named refusal of the affected dimensions in the fixed
// dimension order, or nil when every source answered.
func (g *gathering) namedError() error {
	var named []error
	for _, dim := range inventoryDimensions {
		if cause, ok := g.errs[dim]; ok {
			named = append(named, &SourceError{Dimension: dim, Cause: cause})
		}
	}
	switch len(named) {
	case 0:
		return nil
	case 1:
		return named[0]
	default:
		return errors.Join(named...)
	}
}

// view returns the immutable total view over the gathered detail.
func (g *gathering) view() InventoryView {
	return InventoryView{
		agents:      g.agents,
		sessions:    g.sessions,
		invocations: g.invocations,
		tasks:       g.tasks,
		notices:     g.notices,
		leases:      g.leases,
	}
}

// gather reads every source once and projects the six dimensions from those
// answers.
func (inv *Inventory) gather(ctx context.Context) *gathering {
	g := &gathering{errs: map[ObjectKind]error{}}
	if inv.reads == nil {
		g.fail(ErrSourceUnreadable, inventoryDimensions...)
		return g
	}
	inv.readSites(ctx, g)
	inv.readGeneration(ctx, g)
	inv.readTasks(ctx, g)
	inv.readInvocations(g)
	inv.readNotices(g)
	return g
}

// readSites reads the session sites beside the slot each site currently holds
// and projects the session dimension from those readings.
func (inv *Inventory) readSites(ctx context.Context, g *gathering) {
	sites, err := inv.reads.Sites(ctx)
	if err != nil {
		g.fail(err, ObjectAgent, ObjectSession, ObjectInvocation, ObjectNotice)
		return
	}
	for _, site := range sites {
		g.sites = append(g.sites, siteReading{
			session:    site.Session,
			invocation: site.Invocation,
			decision:   site.Decision,
			held:       site.Held,
		})
		entry := SessionEntry{
			Address:  site.Session.Address,
			Instance: site.Session.Instance,
			Accepted: len(site.Session.Mailbox),
			Consumed: site.Session.Consumed,
		}
		for _, input := range site.Session.Mailbox {
			if input.Claim != state.ClaimPending {
				entry.Claimed++
			}
			if input.Receipt.Completed {
				entry.Completed++
			}
		}
		g.sessions = append(g.sessions, entry)
	}
	sort.Slice(g.sites, func(i, j int) bool {
		return siteLess(g.sites[i].session.Address, g.sites[i].session.Instance, g.sites[j].session.Address, g.sites[j].session.Instance)
	})
	sort.Slice(g.sessions, func(i, j int) bool {
		return siteLess(g.sessions[i].Address, g.sessions[i].Instance, g.sessions[j].Address, g.sessions[j].Instance)
	})
}

// siteLess orders two sites by address and instance, so every dimension
// answers in one stable order whatever order a source lists in.
func siteLess(aAddr event.Address, aInstance event.InstanceID, bAddr event.Address, bInstance event.InstanceID) bool {
	if aAddr.AgentID != bAddr.AgentID {
		return aAddr.AgentID < bAddr.AgentID
	}
	if aAddr.SessionID != bAddr.SessionID {
		return aAddr.SessionID < bAddr.SessionID
	}
	return aInstance < bInstance
}

// readGeneration projects the agent dimension from the identities the binding
// generation configures, the resident sites and the held execution slots —
// keeping the three readings apart — and the lease dimension from the resource
// registrations of that same generation.
func (inv *Inventory) readGeneration(ctx context.Context, g *gathering) {
	generation, err := inv.reads.Published(ctx)
	if err != nil {
		g.fail(err, ObjectAgent, ObjectLease)
		return
	}
	byAgent := map[event.AgentID]*AgentEntry{}
	agent := func(id event.AgentID) *AgentEntry {
		if entry, ok := byAgent[id]; ok {
			return entry
		}
		entry := &AgentEntry{AgentID: id}
		byAgent[id] = entry
		return entry
	}
	for _, id := range generation.Agents {
		agent(id).Configured = true
	}
	for _, reading := range g.sites {
		entry := agent(reading.session.Address.AgentID)
		entry.Resident = true
		if reading.held {
			entry.Busy = true
		}
	}
	for _, entry := range byAgent {
		g.agents = append(g.agents, *entry)
	}
	sort.Slice(g.agents, func(i, j int) bool {
		return g.agents[i].AgentID < g.agents[j].AgentID
	})
	for _, ref := range generation.Leases {
		g.leases = append(g.leases, LeaseEntry{Lease: ref})
	}
}

// readTasks projects the task dimension from the tasks the task owner holds,
// each read beside the obligations its manager still holds and the
// notifications its commit record still owes.
func (inv *Inventory) readTasks(ctx context.Context, g *gathering) {
	readings, err := inv.reads.Tasks(ctx)
	if err != nil {
		g.fail(err, ObjectInvocation, ObjectTask, ObjectNotice)
		return
	}
	for _, reading := range readings {
		g.tasks = append(g.tasks, TaskEntry{
			TaskID:      reading.TaskID,
			Spec:        reading.Spec,
			Status:      reading.Status,
			Obligations: reading.Obligations,
		})
		g.owed = append(g.owed, reading.Outbox...)
	}
	sort.Slice(g.tasks, func(i, j int) bool { return g.tasks[i].TaskID < g.tasks[j].TaskID })
}

// readInvocations projects the invocation dimension from the sites that hold or
// project an invocation and from the responsibility range each task and its
// obligations name.
func (inv *Inventory) readInvocations(g *gathering) {
	drafts := map[event.InvocationID]*invocationDraft{}
	draft := func(id event.InvocationID) *invocationDraft {
		if existing, ok := drafts[id]; ok {
			return existing
		}
		created := &invocationDraft{id: id, pending: map[task.Notice]bool{}, rank: 4}
		drafts[id] = created
		return created
	}
	for _, reading := range g.sites {
		if reading.held {
			entry := draft(reading.invocation)
			entry.locate(1, reading.session.Address, reading.session.Instance)
			entry.decision = reading.decision
		}
		for _, projection := range reading.session.Invocations {
			entry := draft(projection.InvocationID)
			entry.locate(2, reading.session.Address, reading.session.Instance)
			for _, notice := range projection.Pending {
				entry.pending[notice] = true
			}
		}
	}
	for _, t := range g.tasks {
		owner := t.Obligations.Owner
		if owner == "" {
			owner = t.Spec.InvocationID
		}
		if owner == "" {
			continue
		}
		entry := draft(owner)
		entry.locate(3, t.Spec.Source, t.Spec.InstanceID)
		for _, open := range t.Obligations.Open {
			if open.Notice != (task.Notice{}) {
				entry.pending[open.Notice] = true
			}
		}
	}
	ids := make([]event.InvocationID, 0, len(drafts))
	for id := range drafts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		g.invocations = append(g.invocations, drafts[id].entry())
	}
}

// invocationDraft accumulates one invocation entry across the sources that name
// it, locating it by the strongest source that names a site.
type invocationDraft struct {
	id       event.InvocationID
	address  event.Address
	instance event.InstanceID
	decision event.DecisionID
	pending  map[task.Notice]bool
	rank     int
}

// locate records the site of this invocation from the source given, where a
// lower rank is a stronger locator: a held slot, then a session projection,
// then the admission address of a task, and rank four is the unset start.
func (d *invocationDraft) locate(rank int, addr event.Address, instance event.InstanceID) {
	if rank < d.rank {
		d.rank = rank
		d.address = addr
		d.instance = instance
	}
}

// entry finishes the draft as one invocation entry in identity order.
func (d *invocationDraft) entry() InvocationEntry {
	pending := make([]task.Notice, 0, len(d.pending))
	for notice := range d.pending {
		pending = append(pending, notice)
	}
	sort.Slice(pending, func(i, j int) bool { return noticeLess(pending[i], pending[j]) })
	return InvocationEntry{
		InvocationID: d.id,
		Address:      d.address,
		Instance:     d.instance,
		Decision:     d.decision,
		Pending:      pending,
	}
}

// readNotices projects the notification dimension by merging the delivery
// obligations the tasks still owe with the mailbox time points of the accepting
// sites and the blocked records pointing at a notification.
func (inv *Inventory) readNotices(g *gathering) {
	entries := map[noticeID]*NoticeEntry{}
	entryOf := func(notice task.Notice) *NoticeEntry {
		id := noticeIDOf(notice)
		if existing, ok := entries[id]; ok {
			return existing
		}
		created := &NoticeEntry{Notice: notice}
		entries[id] = created
		return created
	}
	for _, owed := range g.owed {
		entry := entryOf(owed.Notice.Notice)
		entry.Target = owed.Notice.Target
		entry.TargetInstance = owed.Notice.TargetInstance
		entry.Delivery = owed.Delivery
	}
	for _, reading := range g.sites {
		for _, input := range reading.session.Mailbox {
			entry := entryOf(input.Notice)
			entry.Target = reading.session.Address
			entry.TargetInstance = reading.session.Instance
			entry.AcceptSeq = input.AcceptSeq
			entry.Claim = input.Claim
			entry.Completed = input.Receipt.Completed
		}
	}
	for _, t := range g.tasks {
		for _, blocked := range t.Obligations.Blocked {
			entry := entryOf(blocked.Notice)
			entry.Blocked = append(entry.Blocked, blocked)
		}
	}
	for _, entry := range entries {
		entry.Phase = noticePhaseOf(*entry)
		g.notices = append(g.notices, *entry)
	}
	sort.Slice(g.notices, func(i, j int) bool {
		return noticeLess(g.notices[i].Notice, g.notices[j].Notice)
	})
}

// noticeID is the stable identity one notification merges under across the
// owners that record it: the task, the round, the category and the sequence
// inside the category. The committed fact a notification points at stays
// content beside that identity and never replaces it.
type noticeID struct {
	task  event.TaskID
	round int64
	kind  task.NoticeKind
	seq   int64
}

// noticeIDOf returns the stable identity of one notification.
func noticeIDOf(notice task.Notice) noticeID {
	return noticeID{task: notice.TaskID, round: notice.Round, kind: notice.Kind, seq: notice.Seq}
}

// noticeLess orders notification identities so every dimension carrying them
// answers in one stable order.
func noticeLess(a, b task.Notice) bool {
	if a.TaskID != b.TaskID {
		return a.TaskID < b.TaskID
	}
	if a.Round != b.Round {
		return a.Round < b.Round
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Seq < b.Seq
}

// noticePhaseOf derives the delivery phase one notification stands in from the
// owner facts recorded beside it: the consumption receipt first, then the
// claim, then the acceptance, and owed where nothing has been accepted.
func noticePhaseOf(entry NoticeEntry) NoticePhase {
	if entry.Completed || entry.Claim == state.ClaimConsumed {
		return NoticePhaseConsumed
	}
	if entry.Claim == state.ClaimClaimed {
		return NoticePhaseClaimed
	}
	if entry.AcceptSeq != 0 || entry.Claim != state.ClaimPending || entry.Delivery == task.DeliveryAccepted {
		return NoticePhaseAccepted
	}
	return NoticePhaseOwed
}

// NewInventorySource returns the production reading source of the projection:
// one gathering of the inventory per freeze, read through the read-only
// inventory the owners already serve. The source freezes the gathering the
// owners answer with and materializes its snapshot objects one identity at a
// time, so a bounded read serves what it needs instead of the whole runtime.
func NewInventorySource(inv *Inventory) ReadingSource {
	return inventorySource{inv: inv}
}

// inventorySource freezes one gathering of the six inventory dimensions: the
// production ReadingSource of the projection, reading the owners through the
// read-only inventory and recording nothing of its own.
type inventorySource struct {
	inv *Inventory
}

// Freeze takes one gathering of the owners and freezes it as the detail source
// of that reading: the stable identity order of the runtime objects beside the
// snapshot object of one identity, materialized on demand. The watermarks of
// that same freeze are returned beside it — the in-memory account these owners
// serve carries no measurable stream position, so its lag is reported as
// unmeasured instead of invented. A source that cannot be read refuses the
// freeze by name instead of freezing a partial reading as a normal one.
func (s inventorySource) Freeze(ctx context.Context) (DetailSource, []SourceWatermark, error) {
	view, err := s.inv.View(ctx)
	if err != nil {
		return nil, nil, err
	}
	return freezeView(view), []SourceWatermark{{
		Source:   "assembly-memory",
		LagState: WatermarkLagUnknown,
	}}, nil
}

// viewDetailSource is one frozen inventory view served one snapshot object at
// a time: the stable identity order of the objects beside their per-identity
// projection. The projections are built per identity at freeze time and run
// when the identity is materialized, so one read materializes the snapshot
// objects it serves and never the whole set before serving one.
type viewDetailSource struct {
	order   []ObjectRef
	entries map[refID]func() Detail
}

// freezeView freezes one inventory view into its detail source: one projection
// per object identity, ordered by the stable identity order every page and
// snapshot of the reading follows. Repeated registrations of one resource
// lease collapse to the newest registration — the resource is one object and
// its release reference is content — so a re-registered lease is one changed
// entry and never two objects.
func freezeView(view InventoryView) *viewDetailSource {
	src := &viewDetailSource{entries: map[refID]func() Detail{}}
	refs := map[refID]ObjectRef{}
	add := func(ref ObjectRef, project func() Detail) {
		id := refIDOf(ref)
		src.entries[id] = project
		refs[id] = ref
	}
	for _, entry := range view.Agents() {
		entry := entry
		ref := ObjectRef{Kind: ObjectAgent, Address: event.Address{AgentID: entry.AgentID}}
		add(ref, func() Detail { return agentDetailOf(entry) })
	}
	for _, entry := range view.Sessions() {
		entry := entry
		ref := ObjectRef{Kind: ObjectSession, Address: entry.Address, InstanceID: entry.Instance}
		add(ref, func() Detail { return sessionDetailOf(entry) })
	}
	for _, entry := range view.Invocations() {
		entry := entry
		ref := ObjectRef{Kind: ObjectInvocation, Address: entry.Address, InstanceID: entry.Instance, InvocationID: entry.InvocationID}
		add(ref, func() Detail { return invocationDetailOf(entry) })
	}
	for _, entry := range view.Tasks() {
		entry := entry
		ref := ObjectRef{Kind: ObjectTask, Address: entry.Spec.Source, InstanceID: entry.Spec.InstanceID, TaskID: entry.TaskID}
		add(ref, func() Detail { return taskDetailOf(entry) })
	}
	for _, entry := range view.Notices() {
		entry := entry
		ref := ObjectRef{Kind: ObjectNotice, Address: entry.Target, InstanceID: entry.TargetInstance, Notice: entry.Notice}
		add(ref, func() Detail { return noticeDetailOf(entry) })
	}
	for _, entry := range view.Leases() {
		entry := entry
		ref := ObjectRef{Kind: ObjectLease, Lease: entry.Lease}
		add(ref, func() Detail { return leaseDetailOf(entry) })
	}
	src.order = make([]ObjectRef, 0, len(refs))
	for _, ref := range refs {
		src.order = append(src.order, ref)
	}
	sort.Slice(src.order, func(i, j int) bool { return refLess(src.order[i], src.order[j]) })
	return src
}

// Order returns the stable identity order of every object the frozen reading
// holds, a copy of its own so no write through a returned order reaches the
// frozen reading.
func (s *viewDetailSource) Order() []ObjectRef {
	return append([]ObjectRef(nil), s.order...)
}

// DetailOf materializes the snapshot object of one identity and reports
// whether the frozen reading holds that identity at all. The entry is built
// when the identity is asked for and answers with the content of the one
// gathering the reading was frozen from, so later owner changes never reach
// it.
func (s *viewDetailSource) DetailOf(ref ObjectRef) (Detail, bool) {
	project, ok := s.entries[refIDOf(ref)]
	if !ok {
		return Detail{}, false
	}
	return project(), true
}

package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// Delegation is the record of one sub-agent call the agent admitted: the
// invocation and native call it is associated with, the address the delegated
// turn runs in, and the progress and terminal the admitted task reports under.
//
// The call is admitted as a task, so the caller is answered by a receipt and this
// record carries the later progress and terminal of that same task. The delegated
// admission keys its own suffix beside the call identity: the tool-call task the
// wrapper admits for the same native call is a distinct admission, and sharing
// the key would dedup the delegation into the wrapper's receipt. The delegated
// turn runs in the session the application selected explicitly, and a delegation
// never creates a private writable history of its own.
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

// CloseRequest is one close of an agent: the address being closed and the
// deadline the close may spend before it reports what is still open.
//
// A close refuses new input first and settles the obligations already accepted,
// and it never reports a drain while a producer has not stopped: a producer that
// answers that it has not stopped keeps the drain open until real termination is
// confirmed or the deadline arrives.
type CloseRequest struct {
	// Address is the address being closed; new input to it is refused first.
	Address event.Address
	// Deadline is the instant after which the close reports the obligations it
	// could not settle instead of waiting longer; a zero deadline waits without a
	// deadline.
	Deadline time.Time
}

// DrainReport is the outcome of one close: whether every accepted obligation
// reached real termination, and the obligations that were still open when the
// close reported.
type DrainReport struct {
	// Drained reports whether every accepted obligation reached real termination.
	// It stays false while any producer has not confirmed its stop.
	Drained bool
	// Unsettled lists the obligations that were still open when the close
	// reported, each with the reason it could not be settled.
	Unsettled []UnsettledObligation
}

// UnsettledObligation is one obligation a close could not settle, paired with the
// reason it stayed open.
type UnsettledObligation struct {
	// TaskID is the task whose obligation stayed open.
	TaskID event.TaskID
	// Reason is the readable reason the obligation could not be settled.
	Reason string
}

// DelegationTasks is the task face the ledger settles its delegations through:
// the admission of one delegated call, the transition that carries its
// notifications beside the settle, the obligations that still owe a settle or
// a delivery, and the flush that delivers one task's notifications.
//
// The redline list (4 rules):
// MUST report the obligation a fresh admission added, and keep it until real settlement.
// MUST commit a transition beside its pending notifications before the settle is reported.
// MUST NOT count a dedup or a refused admission as an obligation, nor withdraw one already booked.
// MUST NOT treat an offered notification as delivered before the target accepts it.
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

// ErrDelegationIdentityMissing, ErrDelegationAddressIncomplete,
// ErrDelegationUnknown, ErrDelegationNotTerminal, ErrDelegationTerminal,
// ErrDelegationAdmissionRefused and ErrDelegationCommitRefused are the named
// refusals of the ledger: each one names the case it refuses instead of
// answering a silent default.
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

// noticeIdentity is the stable identity of one notification. A repeated
// delivery under the same identity records no second fact, and a retry keeps
// the identity it was created under.
type noticeIdentity struct {
	task  event.TaskID
	round int64
	kind  task.NoticeKind
	seq   int64
}

// identityOf reports the stable identity one notification carries.
func identityOf(notice task.Notice) noticeIdentity {
	return noticeIdentity{task: notice.TaskID, round: notice.Round, kind: notice.Kind, seq: notice.Seq}
}

// delegationEntry is the ledger entry of one admitted delegation: the record
// the caller reads, the pin that keeps the initiating generation held for the
// whole delegated turn, the notification identities already recorded, and the
// terminal waiting for the task's obligations to clear before it is reported.
type delegationEntry struct {
	record   Delegation
	pin      *state.BindingHandle
	round    int64
	seen     map[noticeIdentity]bool
	pending  task.Status
	booked   bool
	admitted time.Time
}

// Delegator is one agent's sub-agent delegation ledger. It owns the call
// association, the progress and the terminal bookkeeping of every delegated
// call; the delegated turn itself runs on the borrowing face it hands out.
//
// The ledger never holds its own lock while it calls the task face or the
// state owner, and the Observe entry touches neither: a delivery runs while the
// manager holds its registry, so an observation may only record the fact.
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
type Delegator struct {
	mu       sync.Mutex
	tasks    DelegationTasks
	holder   state.Borrow
	instance event.InstanceID
	entries  map[event.TaskID]*delegationEntry
	open     int
}

// NewDelegator returns an empty delegation ledger over the task face and the
// state owner's borrowing face, scoped to one runtime instance.
func NewDelegator(tasks DelegationTasks, holder state.Borrow, instance event.InstanceID) *Delegator {
	return &Delegator{
		tasks:    tasks,
		holder:   holder,
		instance: instance,
		entries:  map[event.TaskID]*delegationEntry{},
	}
}

// Delegate admits one delegated call as a task and returns its admission
// receipt. The call identity and the application-selected target are required;
// the ledger neither invents a session nor accepts an incomplete target.
//
// The delivery reconciliation barrier of the call is booked before the call is
// spawned, and the initiating generation is pinned beside it: a fresh admission
// keeps both until its terminal lands, while a dedup or a refused admission
// withdraws only this admission's booking and pin — the obligations that already
// exist are untouched.
// - The pin binds the application-selected target at the generation the initiating reference names, taken before the spawn: a publication landing mid-call never retargets the turn.
// - A dedup added no obligation: only this admission's pre-registration is withdrawn, and the obligations of the first admission stay untouched.
func (d *Delegator) Delegate(ctx context.Context, req Delegation) (task.AdmissionReceipt, error) {
	if req.InvocationID == "" || req.DecisionID == "" || req.CallID == "" {
		delegateDebug("tagent: delegation refused result=unnamed call=%q agent=%q", req.CallID, req.Target.AgentID)
		return task.AdmissionReceipt{}, ErrDelegationIdentityMissing
	}
	if !req.Target.IsComplete() {
		delegateDebug("tagent: delegation refused result=address call=%s agent=%s", req.CallID, req.Target.AgentID)
		return task.AdmissionReceipt{}, ErrDelegationAddressIncomplete
	}
	pin, err := d.holder.BorrowDerived(ctx, state.BindingRef{
		AgentID:    req.Target.AgentID,
		InstanceID: req.Ref.InstanceID,
		Generation: req.Ref.Generation,
	})
	if err != nil {
		delegateDebug("tagent: delegation refused result=pin call=%s agent=%s err=%v", req.CallID, req.Target.AgentID, err)
		return task.AdmissionReceipt{}, err
	}
	d.mu.Lock()
	d.open++
	d.mu.Unlock()

	receipt, err := d.tasks.Admit(ctx, task.Spec{
		CallID: req.CallID,
		DedupKey: task.CallIdentity{
			InstanceID:   d.instance,
			InvocationID: req.InvocationID,
			DecisionID:   req.DecisionID,
			CallID:       req.CallID,
		}.DedupKey() + ":delegate",
		InvocationID: req.InvocationID,
		InstanceID:   d.instance,
		Description:  fmt.Sprintf("delegate %s to %s", req.CallID, req.Target.AgentID),
		Source:       req.Target,
		Target:       req.Target,
		Execution:    executionSerialization,
	})
	if err != nil || !receipt.Accepted {
		d.withdrawBooking()
		_ = pin.Release(ctx)
		if err != nil {
			delegateDebug("tagent: delegation refused result=admission call=%s agent=%s err=%v", req.CallID, req.Target.AgentID, err)
			return receipt, err
		}
		delegateDebug("tagent: delegation refused result=admission call=%s agent=%s", req.CallID, req.Target.AgentID)
		return receipt, ErrDelegationAdmissionRefused
	}
	if receipt.Kind != task.AdmissionFresh {
		d.withdrawBooking()
		_ = pin.Release(ctx)
		delegateDebug("tagent: delegation admitted result=dedup task=%s agent=%s call=%s", receipt.TaskID, req.Target.AgentID, req.CallID)
		return receipt, nil
	}
	record := req
	record.TaskID = receipt.TaskID
	record.Status = task.StatusAccepted
	record.Progress = nil
	d.mu.Lock()
	d.entries[receipt.TaskID] = &delegationEntry{
		record:   record,
		pin:      pin,
		round:    1,
		seen:     map[noticeIdentity]bool{},
		booked:   true,
		admitted: time.Now(),
	}
	d.mu.Unlock()
	delegateDebug("tagent: delegation admitted result=fresh task=%s agent=%s call=%s invocation=%s",
		receipt.TaskID, req.Target.AgentID, req.CallID, req.InvocationID)
	return receipt, nil
}

// Observe records one notification of an admitted delegation into the progress
// of the call it belongs to. A repeated delivery under the same stable identity
// records no second fact, and a terminal notification waits for the task's
// obligations to clear before it is reported.
//
// Observe touches no face but this ledger's own state: it may run while the
// manager holds its registry during a delivery.
func (d *Delegator) Observe(_ context.Context, notice task.Notice) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.entries[notice.TaskID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrDelegationUnknown, notice.TaskID)
	}
	id := identityOf(notice)
	if entry.seen[id] {
		return nil
	}
	entry.seen[id] = true
	entry.record.Progress = append(entry.record.Progress, notice)
	switch notice.Kind {
	case task.NoticeSettled:
		d.pendLocked(entry, task.StatusSettled)
	case task.NoticeFailed:
		d.pendLocked(entry, task.StatusFailed)
	case task.NoticeCancelled:
		d.pendLocked(entry, task.StatusCancelled)
	case task.NoticeProgress:
		if entry.pending == task.StatusUnknown {
			entry.record.Status = task.StatusProgress
		}
	case task.NoticeAccepted:
		if entry.pending == task.StatusUnknown {
			entry.record.Status = task.StatusAccepted
		}
	}
	return nil
}

// Complete settles one admitted delegation with the terminal status and the
// fact identity the outcome points at, and returns the notification the settle
// carries. The settle is committed beside that notification first; the terminal
// is reported only once the task's obligations are cleared, so an obligation
// still open keeps the delegation where it is and a later Reconcile lands it.
func (d *Delegator) Complete(ctx context.Context, taskID event.TaskID, status task.Status, fact event.EventID) (task.Notice, error) {
	var kind task.NoticeKind
	switch status {
	case task.StatusSettled:
		kind = task.NoticeSettled
	case task.StatusFailed:
		kind = task.NoticeFailed
	case task.StatusCancelled:
		kind = task.NoticeCancelled
	default:
		delegateDebug("tagent: delegation refused result=status task=%s status=%s", taskID, delegationStatusName(status))
		return task.Notice{}, fmt.Errorf("%w: %v", ErrDelegationNotTerminal, status)
	}
	d.mu.Lock()
	entry, ok := d.entries[taskID]
	if !ok {
		d.mu.Unlock()
		delegateDebug("tagent: delegation refused result=unknown task=%s", taskID)
		return task.Notice{}, fmt.Errorf("%w: %s", ErrDelegationUnknown, taskID)
	}
	if entry.pending != task.StatusUnknown || isTerminalStatus(entry.record.Status) {
		standing := entry.record.Status
		d.mu.Unlock()
		delegateDebug("tagent: delegation refused result=terminal task=%s status=%s", taskID, delegationStatusName(standing))
		return task.Notice{}, fmt.Errorf("%w: %s", ErrDelegationTerminal, taskID)
	}
	notice := task.Notice{TaskID: taskID, Round: entry.round, Kind: kind, Seq: 0, Event: fact}
	envelope := task.Envelope{
		Notice:         notice,
		Target:         entry.record.Target,
		Source:         entry.record.Target,
		TargetInstance: d.instance,
	}
	mutation := task.MutationID(fmt.Sprintf("settle:%s:%d", taskID, entry.round))
	d.mu.Unlock()

	result, err := d.tasks.Advance(ctx, taskID, status, mutation, []task.Envelope{envelope})
	if err != nil {
		delegateDebug("tagent: delegation refused result=advance task=%s err=%v", taskID, err)
		return task.Notice{}, err
	}
	if result != task.CommitCommitted {
		delegateDebug("tagent: delegation refused result=commit task=%s outcome=%v", taskID, result)
		return task.Notice{}, fmt.Errorf("%w: %v", ErrDelegationCommitRefused, result)
	}
	d.mu.Lock()
	if !entry.seen[identityOf(notice)] {
		entry.seen[identityOf(notice)] = true
		entry.record.Progress = append(entry.record.Progress, notice)
	}
	d.pendLocked(entry, status)
	d.mu.Unlock()

	if err := d.tasks.Flush(ctx, taskID); err != nil {
		delegateDebug("tagent: delegation settle withheld task=%s err=%v", taskID, err)
		return notice, err
	}
	if err := d.land(ctx, taskID); err != nil {
		return notice, err
	}
	if !d.terminalReported(taskID) {
		delegateDebug("tagent: delegation settle withheld task=%s status=%s", taskID, delegationStatusName(status))
	}
	return notice, nil
}

// terminalReported reports whether the record of one delegation already stands
// at a terminal status, so a settle that landed is told apart from one the open
// obligations still withhold.
func (d *Delegator) terminalReported(taskID event.TaskID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.entries[taskID]
	return ok && isTerminalStatus(entry.record.Status)
}

// Reconcile attempts the landing of one delegation again and reports the status
// the record stands at afterwards: the entry an earlier settle left pending
// lands here once every obligation has cleared.
func (d *Delegator) Reconcile(ctx context.Context, taskID event.TaskID) (task.Status, error) {
	if err := d.land(ctx, taskID); err != nil {
		return task.StatusUnknown, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.entries[taskID]
	if !ok {
		return task.StatusUnknown, fmt.Errorf("%w: %s", ErrDelegationUnknown, taskID)
	}
	return entry.record.Status, nil
}

// land reports the pending terminal of one delegation once its obligations are
// cleared. While any obligation is still open the terminal stays withheld and
// the record stands where it was; when none is left, the terminal is recorded,
// the delivery reconciliation barrier is closed and the initiating generation
// is released. The terminal that lands here is the one line the delegation face
// writes at INFO, from the settle of the caller and from a later reconcile
// alike: it carries the task, the agent and the elapsed life of the call.
func (d *Delegator) land(ctx context.Context, taskID event.TaskID) error {
	d.mu.Lock()
	entry, ok := d.entries[taskID]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDelegationUnknown, taskID)
	}
	pending := entry.pending
	d.mu.Unlock()
	if pending == task.StatusUnknown {
		return nil
	}
	open, err := d.tasks.Obligations(ctx, taskID)
	if err != nil {
		return err
	}
	if open.Count > 0 {
		return nil
	}
	d.mu.Lock()
	var pin *state.BindingHandle
	landed := task.StatusUnknown
	if entry.pending != task.StatusUnknown {
		entry.record.Status = entry.pending
		landed = entry.pending
		entry.pending = task.StatusUnknown
		if entry.booked {
			entry.booked = false
			d.withdrawBookingLocked()
		}
		pin = entry.pin
		entry.pin = nil
	}
	agentID := entry.record.Target.AgentID
	elapsed := time.Since(entry.admitted)
	d.mu.Unlock()
	if landed != task.StatusUnknown {
		delegateInfo("tagent: delegation settled task=%s agent=%s status=%s elapsed=%s",
			taskID, agentID, delegationStatusName(landed), elapsed.Round(time.Microsecond))
	}
	if pin != nil {
		return pin.Release(ctx)
	}
	return nil
}

// View reports the delegations one invocation issued, in the order their tasks
// were admitted, each as a copy the caller may read freely: the call
// association, the progress and the status the record stands at.
func (d *Delegator) View(owner event.InvocationID) []Delegation {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Delegation, 0, len(d.entries))
	for _, entry := range d.entries {
		if entry.record.InvocationID != owner {
			continue
		}
		record := entry.record
		record.Progress = append([]task.Notice(nil), entry.record.Progress...)
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// OwnerOf reports the invocation that issued the delegation a task was admitted
// as, so a notification of a multi-level task always finds the owner that
// issued its call and never a neighbour.
func (d *Delegator) OwnerOf(taskID event.TaskID) (event.InvocationID, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.entries[taskID]
	if !ok {
		return "", false
	}
	return entry.record.InvocationID, true
}

// Pending reports how many admitted delegations this ledger still books: each
// fresh admission books one before the spawn and keeps it until its terminal
// lands, while a dedup or a refused admission books none.
func (d *Delegator) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.open
}

// TurnBorrow returns the borrowing face the delegated turn of one admitted
// delegation runs under. Every borrow it opens resolves on the generation the
// initiating reference names — the application-selected target at that
// generation — and a derived borrow of a nested delegation is forced onto the
// same generation, so a chain of delegations never drifts onto the latest one.
func (d *Delegator) TurnBorrow(taskID event.TaskID) (state.Borrow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, ok := d.entries[taskID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrDelegationUnknown, taskID)
	}
	return turnBorrow{
		holder: d.holder,
		ref: state.BindingRef{
			InstanceID: entry.record.Ref.InstanceID,
			Generation: entry.record.Ref.Generation,
		},
	}, nil
}

// pendLocked marks the terminal one notification carries as waiting for the
// task's obligations to clear; the record never moves to a terminal before that.
func (d *Delegator) pendLocked(entry *delegationEntry, status task.Status) {
	if entry.pending == task.StatusUnknown && !isTerminalStatus(entry.record.Status) {
		entry.pending = status
	}
}

// withdrawBooking withdraws one admission's pre-registration from the delivery
// reconciliation barrier count.
func (d *Delegator) withdrawBooking() {
	d.mu.Lock()
	d.withdrawBookingLocked()
	d.mu.Unlock()
}

// withdrawBookingLocked withdraws one admission's pre-registration while the
// ledger lock is already held.
func (d *Delegator) withdrawBookingLocked() {
	if d.open > 0 {
		d.open--
	}
}

// isTerminalStatus reports whether one status is a terminal outcome.
func isTerminalStatus(status task.Status) bool {
	return status == task.StatusSettled || status == task.StatusFailed || status == task.StatusCancelled
}

// delegationStatusName renders one task status as the name an observation line
// carries, so a line names the phase it reports instead of an ordinal.
func delegationStatusName(status task.Status) string {
	switch status {
	case task.StatusAccepted:
		return "accepted"
	case task.StatusRunning:
		return "running"
	case task.StatusProgress:
		return "progress"
	case task.StatusSettled:
		return "settled"
	case task.StatusFailed:
		return "failed"
	case task.StatusCancelled:
		return "cancelled"
	case task.StatusAwaitingApproval:
		return "awaiting_approval"
	default:
		return "unknown"
	}
}

// delegateDebug renders one line of the delegation face at DEBUG: the admission
// the ledger admitted, deduplicated or refused, and the settle it withheld
// while an obligation is still open. A line carries the task, the agent, the
// native call identity and the outcome word — labels and identities only.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed delegation.
// MUST keep one line per admission, per refusal and per withheld settle.
// MUST NOT render the delegated prompt, the target input or the model output.
func delegateDebug(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf(format, args...)
}

// delegateInfo renders the one line a delegation writes at INFO when its
// terminal really lands: the task, the agent whose turn ran, the terminal
// status and the elapsed life of the call from admission to the report. A
// delegation is a turn of another agent, so its completion belongs beside the
// turn boundaries rather than in the per-fact detail.
//
// The redline list (3 rules):
// MUST be written once per landed terminal, whatever entry landed it.
// MUST stay a bypass: a failing logger is a missing line, never a withheld terminal.
// MUST NOT render the delegated prompt, the target input or the model output.
func delegateInfo(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Infof(format, args...)
}

// turnBorrow is the borrowing face of one delegated turn: every borrow names
// the agent the turn asks for on the instance and generation the initiating
// reference carried, so the turn never reads the global latest table and a
// nested delegation inherits the very same generation.
type turnBorrow struct {
	holder state.Borrow
	ref    state.BindingRef
}

// Borrow returns a handle to the binding of the initiating generation for one
// agent, held for the whole life of the borrow.
func (t turnBorrow) Borrow(ctx context.Context, agentID event.AgentID) (*state.BindingHandle, error) {
	return t.holder.BorrowDerived(ctx, state.BindingRef{
		AgentID:    agentID,
		InstanceID: t.ref.InstanceID,
		Generation: t.ref.Generation,
	})
}

// BorrowDerived returns a handle on the initiating generation for the agent the
// reference names; the generation of the passed reference is never taken, so a
// delegated borrow cannot drift onto another generation.
func (t turnBorrow) BorrowDerived(ctx context.Context, ref state.BindingRef) (*state.BindingHandle, error) {
	return t.holder.BorrowDerived(ctx, state.BindingRef{
		AgentID:    ref.AgentID,
		InstanceID: t.ref.InstanceID,
		Generation: t.ref.Generation,
	})
}

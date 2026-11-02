package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
//
// ErrTaskNotFound reports a task identity the registry holds no record of.
var ErrTaskNotFound = errors.New("task: no task with that identity")

// ErrConcurrencyLimit reports an admission refused because the registry already holds
// its limit of non-terminal tasks. Every task already admitted is left untouched.
var ErrConcurrencyLimit = errors.New("task: the concurrency limit is reached")

// ErrTerminalState reports an attempt to move a task whose terminal fact is already
// stored. A terminal task keeps that one fact and no later signal revives it.
var ErrTerminalState = errors.New("task: the task already holds its terminal fact")

// ErrStatusUnknown reports an attempt to record the unknown status on a task. Unknown
// means not determinable, so it is surfaced and never stored as a status.
var ErrStatusUnknown = errors.New("task: the unknown status is never recorded")

// ErrIllegalTransition reports a status change outside the task state machine.
var ErrIllegalTransition = errors.New("task: the status change is outside the state machine")

// ErrInputMissing reports a resume declaration carrying no event identity, so no repeat
// of that declaration could ever be recognized as the same one.
var ErrInputMissing = errors.New("task: the resume declaration carries no event identity")

// ErrRoundInFlight reports a resume refused because a round of the task is in flight.
var ErrRoundInFlight = errors.New("task: a round of this task is in flight")

// ErrResumeCancelled reports a resume refused for a cancelled task. A cancelled task is
// relaunched or re-spawned under a new identity.
var ErrResumeCancelled = errors.New("task: a cancelled task is resumed by relaunch or re-spawn")

// ErrCommitUncertain reports a status change whose storage outcome is unknown. The
// change is reconciled before any repeat of it.
var ErrCommitUncertain = errors.New("task: the commit outcome of this change is unknown")

// RegistryOption configures one in-memory task registry beyond its commit carrier
// and admission face.
type RegistryOption func(*MemoryRegistry)

// WithActivity attaches the pulse the registry reports each time it applied a
// committed fact: a close waiting on the rest criterion reads the pulse instead
// of polling the account for the movement it already made. The pulse is a
// report, never a judgment.
func WithActivity(report func()) RegistryOption {
	return func(r *MemoryRegistry) { r.activity = report }
}

// WithConcurrencyLimit caps how many tasks the registry holds non-terminal at once. An
// admission past that limit is refused by name, and the tasks already admitted are kept.
func WithConcurrencyLimit(limit int) RegistryOption {
	return func(r *MemoryRegistry) { r.limit = limit }
}

// DefaultTerminalTTL is how long a registry keeps the record of a task after the terminal
// outcome it holds, once its run applies the terminal TTL: the task_terminal_ttl of the
// declaration vocabulary, read as the bound of the in-memory account rather than of the fact
// chain. The record is a live read of a task, never the fact of it — every fact stays on the
// chain — so a record pruned here costs a re-entry its target, not the history of the task.
const DefaultTerminalTTL = 2 * time.Hour

// WithTerminalTTL bounds how long the record of a terminal task is kept. A record whose
// task reached a terminal outcome at least that long before a sweep reads is dropped, and a
// call naming it answers ErrTaskNotFound — the refusal the send face renders as a task this
// host holds no record of. A zero TTL keeps every record the registry ever admitted, so a
// registry built beside none behaves exactly as it did before the bound existed.
func WithTerminalTTL(ttl time.Duration) RegistryOption {
	return func(r *MemoryRegistry) { r.terminalTTL = ttl }
}

// obligationEntry is one obligation the account still holds for a task: the
// obligation itself and the routing its notification froze.
type obligationEntry struct {
	obligation Obligation
	envelope   Envelope
}

// specDeclarer is the optional admission-material face a commit carrier may
// offer beside the commit port. A registry that registers a fresh task stores
// the task's admission material through this face beside the task's first
// commit, so the fact chain one durable carrier writes is self-contained: a
// restore rebuilds the task from material the chain already carries and no
// external declaration is needed. A carrier that does not offer the face keeps
// its own persistence shape and the registry declares nothing beside it.
type specDeclarer interface {
	Declare(ctx context.Context, taskID event.TaskID, spec Spec) error
}

// memoryTask is one task record: the declaration it was admitted under, the status and
// revision it holds, the mutations recorded against it, and the obligations a manager
// still holds for it.
type memoryTask struct {
	spec        Spec
	status      Status
	revision    int64
	round       int64
	mutations   map[MutationID]CommitRecord
	obligations []obligationEntry
	// blocked keeps the reason beside each notification whose delivery could not
	// proceed. It keeps its write authority for the same reason obligationEntry
	// keeps answered: a refusal's own words have no fact carrier, so the chain
	// cannot compute them back, and the record changes what a reader may conclude
	// about the task — an obligation reported as blocked is not reported as still
	// on its way. Writing the reason into the account is a new persistent surface
	// and stays outside this change.
	blocked []BlockedObligation
	// exitReason is the reason an obligation ended without settlement, reported
	// beside the obligations. It is a report field, not a gate: nothing reads it
	// to decide what the task may do next, and the chain carries the cancelled
	// fact without the executor's words, so a restart reports an empty reason
	// rather than an inferred one.
	exitReason string
	// cancelRequested remembers that a caller asked for this task's cancellation.
	// It keeps its write authority: it gates whether a confirmed stop may record
	// the cancelled fact (a stop with no request open is refused by name), and no
	// committed fact carries the request, so a restart reads it as unrequested
	// rather than inferring a request from a status.
	cancelRequested bool
	stops           []StopEvidence
	handoff         *HandoffTarget
	handoffRecord   HandoffRecord
	admittedAt      time.Time
	changedAt       time.Time
	runSince        time.Time
	// ttlAnchor is the instant the task's time to live is counted from once a
	// write-type re-entry moved it: the round the re-entry opened. It is a
	// movement stamp like changedAt — reading it classifies the task and moves
	// nothing — and a record whose anchor stands at its admission reads the same
	// deadline it always did.
	ttlAnchor time.Time
	// reported is the verdict the detector read over the round the task is in, so it
	// restarts where the round restarts: a suspected verdict read over the round before
	// is never reported against the new one, and the new round is suspected only when
	// its own threshold passes.
	reported LogicalStatus
}

// MemoryRegistry is the in-memory task registry: it admits a task declaration, moves a
// task through the state machine on the commit carrier, and answers lookups,
// cancellation requests and resume declarations. It satisfies Registry and holds no
// recovery of its own.
type MemoryRegistry struct {
	mu        sync.Mutex
	carrier   Commit
	admission *MemoryAdmission
	seq       *NoticeSequencer
	limit     int
	tasks     map[event.TaskID]*memoryTask
	policy    Policy
	now       func() time.Time
	// terminalTTL is how long the record of a terminal task is kept: the sweep reads it on
	// every admission, and a zero bound keeps every record.
	terminalTTL time.Duration
	// activity is the pulse the registry reports each time it applied a
	// committed fact; nil reports nothing.
	activity func()
}

// The registry satisfies the task registry port.
var _ Registry = (*MemoryRegistry)(nil)

// NewMemoryRegistry returns an in-memory task registry over one commit carrier and one
// admission face. Each status change is submitted to the carrier before the registry
// reports it, so a change the carrier did not store is never made visible.
func NewMemoryRegistry(carrier Commit, admission *MemoryAdmission, opts ...RegistryOption) *MemoryRegistry {
	r := &MemoryRegistry{
		carrier:   carrier,
		admission: admission,
		seq:       NewNoticeSequencer(),
		tasks:     map[event.TaskID]*memoryTask{},
		policy:    DefaultPolicy,
		now:       time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register admits one task declaration and returns its admission receipt. A repeated
// admission of the same dedup key reports the task of the first admission as a dedup
// and adds no obligation; an admission past the concurrency limit is refused by name
// and leaves every admitted task untouched. A fresh admission keeps its pre-registered
// obligation open until the task settles. The admission material of a fresh task is
// stored beside its first commit through the carrier's admission-material face when
// the carrier offers one, so the stored chain of the task is self-contained and a
// restore needs no external declaration.
func (r *MemoryRegistry) Register(ctx context.Context, spec Spec) (AdmissionReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneTerminalLocked(r.now())
	if spec.DedupKey != "" {
		if existing, ok := r.admission.Lookup(spec.DedupKey); ok {
			return AdmissionReceipt{
				CallID:   spec.CallID,
				TaskID:   existing,
				Accepted: true,
				Kind:     AdmissionDedup,
			}, nil
		}
	}
	if r.limit > 0 && r.activeCountLocked() >= r.limit {
		return AdmissionReceipt{CallID: spec.CallID, Accepted: false, Kind: AdmissionRejected}, ErrConcurrencyLimit
	}
	admitted := r.now()
	taskID := mintTaskID()
	entry := &memoryTask{
		spec:       spec,
		status:     StatusAccepted,
		round:      1,
		mutations:  map[MutationID]CommitRecord{},
		admittedAt: admitted,
		changedAt:  admitted,
	}
	mutation := MutationID("admit:" + string(taskID))
	record := CommitRecord{
		MutationID:       mutation,
		TaskID:           taskID,
		ExpectedRevision: 0,
		Status:           StatusAccepted,
	}
	if declarer, ok := r.carrier.(specDeclarer); ok {
		if err := declarer.Declare(ctx, taskID, spec); err != nil {
			return AdmissionReceipt{CallID: spec.CallID, Accepted: false, Kind: AdmissionRejected}, err
		}
	}
	result, applied, err := r.applyCommitLocked(ctx, entry, record)
	if result != CommitCommitted {
		if err == nil {
			err = ErrCommitUncertain
		}
		return AdmissionReceipt{CallID: spec.CallID, Accepted: false, Kind: AdmissionRejected}, err
	}
	if applied {
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: ObligationAwaitingTool, TaskID: taskID},
		})
	}
	receipt := r.admission.AdmitKey(spec.DedupKey, spec.CallID, taskID)
	r.tasks[taskID] = entry
	return receipt, nil
}

// Lookup returns the declaration and status of the task with the given identity.
func (r *MemoryRegistry) Lookup(ctx context.Context, taskID event.TaskID) (Spec, Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return Spec{}, StatusUnknown, ErrTaskNotFound
	}
	return entry.spec, entry.status, nil
}

// Advance moves one task to the next status and submits the change beside its pending
// notifications to the commit carrier first. A repeated mutation returns the fact
// already stored and records no second one; a change the carrier refuses leaves the
// task where it is; a task that holds its terminal fact moves no more.
func (r *MemoryRegistry) Advance(ctx context.Context, taskID event.TaskID, to Status, mutation MutationID, outbox []Envelope) (CommitResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return CommitNotCommitted, ErrTaskNotFound
	}
	record := CommitRecord{
		MutationID:       mutation,
		TaskID:           taskID,
		ExpectedRevision: entry.revision,
		Status:           to,
		Outbox:           outbox,
	}
	if result, known, err := entry.repeatOf(record); known {
		return result, err
	}
	switch {
	case to == StatusUnknown:
		return CommitNotCommitted, ErrStatusUnknown
	case to == StatusAccepted:
		return CommitNotCommitted, ErrIllegalTransition
	case isTerminal(entry.status):
		return CommitNotCommitted, ErrTerminalState
	case to == StatusAwaitingApproval && !entersApprovalWait(entry.status):
		return CommitNotCommitted, ErrIllegalTransition
	case entry.status == StatusAwaitingApproval && !leavesApprovalWait(to):
		return CommitNotCommitted, ErrIllegalTransition
	}
	result, applied, err := r.applyCommitLocked(ctx, entry, record)
	if applied && isTerminal(to) {
		entry.closeExecutionObligation("")
	}
	return result, err
}

// Resume feeds one declared input into an existing task and re-enters the standard
// lifecycle under the same task identity. A round already in flight refuses the
// declaration, and so does a cancelled task. A repeated declaration of the same input
// returns the fact already stored and opens no second obligation.
//
// The channel belongs to the state machine and carries no model tool face: the resume
// tool the model used to call is gone (design DEL-3, a tool member the model never
// sees again). The send tool's re-entry of a task reaches this state machine through
// Reactivate and the continuation table instead, so the input channel stays beneath
// both without a declaration of its own. It carries an event identity rather than free
// text, so a call naming no identity opens no round.
func (r *MemoryRegistry) Resume(ctx context.Context, taskID event.TaskID, input event.Event) (AdmissionReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return AdmissionReceipt{TaskID: taskID, Accepted: false, Kind: AdmissionRejected}, ErrTaskNotFound
	}
	if input.Key == "" {
		return AdmissionReceipt{TaskID: taskID, Accepted: false, Kind: AdmissionRejected}, ErrInputMissing
	}
	mutation := MutationID("resume:" + string(taskID) + ":" + string(input.Key))
	record := CommitRecord{
		MutationID:       mutation,
		TaskID:           taskID,
		ExpectedRevision: entry.revision,
		Status:           StatusRunning,
	}
	if result, known, err := entry.repeatOf(record); known {
		if result != CommitCommitted {
			return AdmissionReceipt{TaskID: taskID, Accepted: false, Kind: AdmissionRejected}, err
		}
		return AdmissionReceipt{TaskID: taskID, Accepted: true, Kind: AdmissionFresh}, nil
	}
	switch entry.status {
	case StatusRunning:
		return AdmissionReceipt{TaskID: taskID, Accepted: false, Kind: AdmissionRejected}, ErrRoundInFlight
	case StatusCancelled:
		return AdmissionReceipt{TaskID: taskID, Accepted: false, Kind: AdmissionRejected}, ErrResumeCancelled
	}
	result, applied, err := r.applyCommitLocked(ctx, entry, record)
	if result != CommitCommitted {
		if err == nil {
			err = ErrCommitUncertain
		}
		return AdmissionReceipt{TaskID: taskID, Accepted: false, Kind: AdmissionRejected}, err
	}
	if applied {
		entry.round++
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: ObligationAwaitingTool, TaskID: taskID},
		})
	}
	return AdmissionReceipt{TaskID: taskID, Accepted: true, Kind: AdmissionFresh}, nil
}

// applyCommitLocked submits one record to the commit carrier and applies the stored
// fact to the task: its status and revision move, the mutation is remembered, and each
// notification the record carries opens a delivery obligation. The applied fact is
// reported through the activity pulse once it is visible to every reader, so a close
// waiting on the account re-reads it instead of polling for it.
func (r *MemoryRegistry) applyCommitLocked(ctx context.Context, entry *memoryTask, record CommitRecord) (CommitResult, bool, error) {
	result, err := r.carrier.Commit(ctx, record)
	if result != CommitCommitted {
		return result, false, err
	}
	entry.markMovement(record.Status, r.now())
	entry.status = record.Status
	entry.revision = record.ExpectedRevision + 1
	entry.mutations[record.MutationID] = record
	for _, envelope := range record.Outbox {
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: ObligationAwaitingDelivery, TaskID: record.TaskID, Notice: envelope.Notice},
			envelope:   envelope,
		})
	}
	r.noteActivity()
	return CommitCommitted, true, nil
}

// noteActivity reports one applied fact through the attached pulse, or nothing
// while no pulse is attached. The caller holds the registry lock, so the pulse
// is reported from the one writer that applied the fact.
func (r *MemoryRegistry) noteActivity() {
	if r.activity != nil {
		r.activity()
	}
}

// repeatOf reports the outcome of a mutation the task already stores and whether the
// record names one. An identical repeat returns the fact already stored; the same
// mutation identity beside other content is refused.
func (t *memoryTask) repeatOf(record CommitRecord) (CommitResult, bool, error) {
	if record.MutationID == "" {
		return CommitNotCommitted, false, ErrMutationMissing
	}
	prior, ok := t.mutations[record.MutationID]
	if !ok {
		return CommitUncertain, false, nil
	}
	if prior.Status != record.Status || !slices.Equal(prior.Outbox, record.Outbox) {
		return CommitNotCommitted, true, ErrMutationConflict
	}
	return CommitCommitted, true, nil
}

// closeExecutionObligation drops the awaiting-tool obligation of a task and records the
// reason it ended without settlement when one is given.
func (t *memoryTask) closeExecutionObligation(exitReason string) {
	open := make([]obligationEntry, 0, len(t.obligations))
	for _, entry := range t.obligations {
		if entry.obligation.Kind == ObligationAwaitingTool {
			continue
		}
		open = append(open, entry)
	}
	t.obligations = open
	if exitReason != "" {
		t.exitReason = exitReason
	}
}

// activeCountLocked counts the tasks that hold no terminal fact yet.
func (r *MemoryRegistry) activeCountLocked() int {
	count := 0
	for _, entry := range r.tasks {
		if !isTerminal(entry.status) {
			count++
		}
	}
	return count
}

// PruneTerminal drops the records whose task reached a terminal outcome at least the
// terminal TTL of this registry before at, and reports how many it dropped. The sweep is the
// one place a record leaves the registry, so a re-entry naming a pruned task answers
// ErrTaskNotFound instead of opening a round of a state nobody kept.
//
// The sweep reads the movement stamp that placed the task at its terminal, so a task that was
// re-entered after it settled counts from the terminal of the round it ended in. A
// non-terminal task is never dropped however long it stands: a round in flight is the one
// state a sweep must not silently withdraw, and a task stuck in one is what the broken-round
// account reports instead.
func (r *MemoryRegistry) PruneTerminal(at time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pruneTerminalLocked(at)
}

// pruneTerminalLocked drops one registry's expired terminal records under the lock its reads
// take and reports how many it dropped. Every admission sweeps beside the record it adds, so
// the account of a run that keeps admitting is bounded by the terminal TTL it was built with.
func (r *MemoryRegistry) pruneTerminalLocked(at time.Time) int {
	if r.terminalTTL <= 0 {
		return 0
	}
	dropped := 0
	for taskID, entry := range r.tasks {
		if !isTerminal(entry.status) || entry.changedAt.IsZero() {
			continue
		}
		if at.Sub(entry.changedAt) < r.terminalTTL {
			continue
		}
		delete(r.tasks, taskID)
		r.admission.forget(taskID)
		dropped++
	}
	return dropped
}

// envelope freezes the delivery routing of one notification of this task: the
// target, source and target instance of the declaration the task was admitted
// under, or of the explicit handoff that already moved the task's future
// notifications. The routing is frozen into the notification at its creation and
// is never resolved again later, so a restart or a handoff never re-routes a
// notification that was already created.
func (t *memoryTask) envelope(notice Notice) Envelope {
	envelope := Envelope{
		Notice:         notice,
		Target:         t.spec.Target,
		Source:         t.spec.Source,
		TargetInstance: t.spec.InstanceID,
	}
	if t.handoff != nil {
		envelope.Target = t.handoff.Target
		envelope.TargetInstance = t.handoff.TargetInstance
	}
	return envelope
}

// envelopeOf freezes the delivery routing of one notification of the named task
// and reports whether the registry holds that task at all.
func (r *MemoryRegistry) envelopeOf(taskID event.TaskID, notice Notice) (Envelope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return Envelope{}, false
	}
	return entry.envelope(notice), true
}

// entersApprovalWait reports whether a task may enter the approval wait from the
// status it holds: a call parked at admission beside an execution under way. The
// wait is execution-phase information, so no terminal task enters it.
func entersApprovalWait(from Status) bool {
	return from == StatusAccepted || from == StatusRunning
}

// leavesApprovalWait reports whether a task may leave the approval wait for the
// status it moves to: the execution continues under way, or a terminal fact
// lands on the wait. A wait never re-opens an admission.
func leavesApprovalWait(to Status) bool {
	return to == StatusRunning ||
		to == StatusSettled || to == StatusFailed || to == StatusCancelled
}

// isTerminal reports whether a status holds a terminal fact: a settled, a failed and a
// cancelled task record that fact once and are not moved again.
func isTerminal(status Status) bool {
	return status == StatusSettled || status == StatusFailed || status == StatusCancelled
}

// mintTaskID mints one task identity: a random hex slug, never a sequence
// number. A task identity is never enumerable — an admission receipt carries
// no task id to the model, and the environment block names the open work, so a
// predictable identifier would only serve enumeration across a shared surface.
func mintTaskID() event.TaskID {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("task: no randomness for a task identity: " + err.Error())
	}
	return event.TaskID("task-" + hex.EncodeToString(buf[:]))
}

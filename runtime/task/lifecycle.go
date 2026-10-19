package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

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

// RegistryOption configures an in-memory task registry at construction.
type RegistryOption func(*MemoryRegistry)

// WithConcurrencyLimit caps how many tasks the registry holds non-terminal at once. An
// admission past that limit is refused by name, and the tasks already admitted are kept.
func WithConcurrencyLimit(limit int) RegistryOption {
	return func(r *MemoryRegistry) { r.limit = limit }
}

// obligationEntry pairs one open obligation with the delivery routing its
// notification froze beside its delivery bookkeeping.
type obligationEntry struct {
	obligation Obligation
	envelope   Envelope
	answered   bool
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
	spec            Spec
	status          Status
	revision        int64
	round           int64
	mutations       map[MutationID]CommitRecord
	obligations     []obligationEntry
	blocked         []BlockedObligation
	exitReason      string
	cancelRequested bool
	stops           []StopEvidence
	handoff         *HandoffTarget
	handoffRecord   HandoffRecord
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
	last      int64
	tasks     map[event.TaskID]*memoryTask
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
	r.last++
	taskID := event.TaskID(fmt.Sprintf("task-%d", r.last))
	entry := &memoryTask{
		spec:      spec,
		status:    StatusAccepted,
		round:     1,
		mutations: map[MutationID]CommitRecord{},
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
	case StatusRunning, StatusProgress:
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
// notification the record carries opens a delivery obligation.
func (r *MemoryRegistry) applyCommitLocked(ctx context.Context, entry *memoryTask, record CommitRecord) (CommitResult, bool, error) {
	result, err := r.carrier.Commit(ctx, record)
	if result != CommitCommitted {
		return result, false, err
	}
	entry.status = record.Status
	entry.revision = record.ExpectedRevision + 1
	entry.mutations[record.MutationID] = record
	for _, envelope := range record.Outbox {
		entry.obligations = append(entry.obligations, obligationEntry{
			obligation: Obligation{Kind: ObligationAwaitingDelivery, TaskID: record.TaskID, Notice: envelope.Notice},
			envelope:   envelope,
		})
	}
	return CommitCommitted, true, nil
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
	return from == StatusAccepted || from == StatusRunning || from == StatusProgress
}

// leavesApprovalWait reports whether a task may leave the approval wait for the
// status it moves to: the execution continues under way, or a terminal fact
// lands on the wait. A wait never re-opens an admission.
func leavesApprovalWait(to Status) bool {
	return to == StatusRunning || to == StatusProgress ||
		to == StatusSettled || to == StatusFailed || to == StatusCancelled
}

// isTerminal reports whether a status holds a terminal fact: a settled, a failed and a
// cancelled task record that fact once and are not moved again.
func isTerminal(status Status) bool {
	return status == StatusSettled || status == StatusFailed || status == StatusCancelled
}

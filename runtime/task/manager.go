package task

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// Deliverer reports the delivery-layer outcome of one notification against its target.
// Only an acceptance the target states explicitly advances a delivery.
type Deliverer func(ctx context.Context, notice Notice) (DeliveryAcceptance, error)

// ErrNoDeliverer reports a flush that could deliver nothing, because no deliverer is
// attached to the manager.
var ErrNoDeliverer = errors.New("task: no deliverer is attached to this manager")

// refusalReason is the reason recorded beside a notification whose delivery the target
// refused without naming one.
const refusalReason = "the target did not accept the delivery"

// MemoryManager is the in-memory task manager: it admits a task, reports the
// obligations it still holds for one, hands a task's future notifications to another
// target, and flushes the notifications a task still owes. It satisfies Manager and
// records what its faces report without inferring outcomes of its own.
type MemoryManager struct {
	registry        *MemoryRegistry
	deliver         Deliverer
	deliverEnvelope EnvelopeDeliverer
	closed          map[event.InstanceID]bool
}

// The manager satisfies the task manager port.
var _ Manager = (*MemoryManager)(nil)

// NewMemoryManager returns an in-memory task manager over one registry. deliver reports
// the delivery-layer outcome of a notification and may be nil, in which case a flush
// refuses rather than dropping an obligation. Options configure the manager further,
// beside the deliverer.
func NewMemoryManager(registry *MemoryRegistry, deliver Deliverer, opts ...ManagerOption) *MemoryManager {
	m := &MemoryManager{registry: registry, deliver: deliver, closed: map[event.InstanceID]bool{}}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Admit accepts one task declaration and returns the admission receipt. Admission is
// not a result and does not clear the task's obligation.
func (m *MemoryManager) Admit(ctx context.Context, spec Spec) (AdmissionReceipt, error) {
	return m.registry.Register(ctx, spec)
}

// Obligations reports the obligations the manager still holds for a task, each entry
// classified by the wait it is in beside the task and notification it points at.
func (m *MemoryManager) Obligations(ctx context.Context, taskID event.TaskID) (Obligations, error) {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	entry, ok := m.registry.tasks[taskID]
	if !ok {
		return Obligations{}, ErrTaskNotFound
	}
	report := Obligations{
		Count:      len(entry.obligations),
		Owner:      entry.spec.InvocationID,
		Blocked:    append([]BlockedObligation(nil), entry.blocked...),
		ExitReason: entry.exitReason,
		Open:       make([]Obligation, 0, len(entry.obligations)),
	}
	for _, open := range entry.obligations {
		report.Open = append(report.Open, open.obligation)
	}
	return report, nil
}

// Handoff moves a task's future notifications to the target address and the target
// instance, keeping the source and target instances distinct, and records the handoff
// link beside the task as one durable mutation of its fact chain, so a registry
// rebuilt from that chain applies the same move and a restarted task routes its
// future notifications to the new target rather than the one its declaration named.
// A notification whose target already answered keeps its old
// target and its identity; a notification with no answer yet has only its open delivery obligation reclassified as handed-off bookkeeping — its frozen routing never moves.
// Every notification already created keeps the routing its envelope froze: a retry of
// one delivers beside that frozen routing and re-resolves no current target. A repeat
// of the handoff already recorded applies nothing a second time.
func (m *MemoryManager) Handoff(ctx context.Context, taskID event.TaskID, target HandoffTarget) error {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	entry, ok := m.registry.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if entry.handoff != nil && *entry.handoff == target {
		return nil
	}
	slot := 0
	for mutation := range entry.mutations {
		if _, carried := handoffTargetOf(mutation); carried {
			slot++
		}
	}
	mutation, err := handoffMutation(slot+1, target)
	if err != nil {
		return err
	}
	record := CommitRecord{
		MutationID:       mutation,
		TaskID:           taskID,
		ExpectedRevision: entry.revision,
		Status:           entry.status,
	}
	if result, known, err := entry.repeatOf(record); known {
		if result != CommitCommitted {
			return err
		}
		entry.applyHandoff(taskID, target)
		return nil
	}
	result, applied, err := m.registry.applyCommitLocked(ctx, entry, record)
	if result != CommitCommitted {
		if err == nil {
			err = ErrCommitUncertain
		}
		return err
	}
	if applied {
		entry.applyHandoff(taskID, target)
	}
	return nil
}

// Flush delivers the notifications of a task whose obligations are still open, each
// beside the routing its envelope froze, and retries the ones no target has answered
// yet — including the ones a handoff moved beside the task. Only an acceptance the
// target states explicitly clears an obligation; a refusal keeps the obligation and
// records the reason beside that notification. A notification whose frozen receiving
// instance is closed is retained by name as ErrInstanceClosed: it is neither dropped
// nor delivered into a later instance that reuses the address.
func (m *MemoryManager) Flush(ctx context.Context, taskID event.TaskID) error {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	entry, ok := m.registry.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if m.deliver == nil && m.deliverEnvelope == nil {
		for _, open := range entry.obligations {
			if owesDelivery(open.obligation.Kind) && !open.answered {
				return ErrNoDeliverer
			}
		}
		return nil
	}
	open := entry.obligations[:0]
	delivery := deliveryContext(ctx, entry.spec)
	for _, held := range entry.obligations {
		if !owesDelivery(held.obligation.Kind) || held.answered {
			open = append(open, held)
			continue
		}
		if m.closed[held.envelope.TargetInstance] {
			entry.recordBlocked(held.obligation.Notice, ErrInstanceClosed)
			open = append(open, held)
			continue
		}
		outcome, err := m.deliverOne(delivery, held.envelope)
		switch outcome {
		case DeliveryAccepted:
			entry.dropBlocked(held.obligation.Notice)
			continue
		case DeliveryRejected:
			held.answered = true
			entry.recordBlocked(held.obligation.Notice, err)
			open = append(open, held)
		default:
			open = append(open, held)
		}
	}
	entry.obligations = open
	return nil
}

// deliveryContext carries the call identity the task's admission material
// records on the delivery context of every notification flush offers. The
// attribution of a notification is the responsibility range of the call that
// produced it, read from the stored material rather than from whatever call
// frame the transport happens to carry: a delivery driven beside no call frame
// — after a restart, for instance — reports the same invocation the original
// admission recorded instead of an empty one.
func deliveryContext(ctx context.Context, spec Spec) context.Context {
	return WithCallFrame(ctx, CallFrame{
		Identity: CallIdentity{
			InstanceID:   spec.InstanceID,
			InvocationID: spec.InvocationID,
			CallID:       spec.CallID,
		},
		Address:   spec.Target,
		Execution: spec.Execution,
	})
}

// recordBlocked records the reason beside one notification whose delivery could not
// proceed.
func (t *memoryTask) recordBlocked(notice Notice, err error) {
	reason := refusalReason
	if err != nil {
		reason = err.Error()
	}
	for i := range t.blocked {
		if t.blocked[i].Notice == notice {
			t.blocked[i].Reason = reason
			return
		}
	}
	t.blocked = append(t.blocked, BlockedObligation{Notice: notice, Reason: reason})
}

// dropBlocked withdraws the blocked record beside one notification.
func (t *memoryTask) dropBlocked(notice Notice) {
	kept := t.blocked[:0]
	for _, blocked := range t.blocked {
		if blocked.Notice == notice {
			continue
		}
		kept = append(kept, blocked)
	}
	t.blocked = kept
}

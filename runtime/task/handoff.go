package task

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// HandoffRecord is the recorded link of one explicit handoff: the previous
// target the task's notifications moved from beside the new target they move
// to, with the source instance and the target instance kept apart. The
// notifications created before the handoff stay claimed by the source instance
// and keep the routing their envelopes froze; only the notifications created
// after the handoff freeze the new target and are claimed by the target
// instance. One notification is claimed by exactly one instance and is never
// recorded twice.
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

// ErrNoHandoff reports a handoff record request for a task no explicit handoff
// moved yet.
var ErrNoHandoff = errors.New("task: no handoff is recorded for this task")

// ErrInstanceClosed reports a delivery refused because the receiving instance
// is closed. The undelivered notifications of a closed instance are retained
// for the recovery path: they are never dropped, never re-routed, and never
// delivered into a later instance that merely reuses the same address.
var ErrInstanceClosed = errors.New("task: the receiving instance is closed and its undelivered notifications are retained")

// HandoffOf reports the recorded handoff link of one task: the previous target
// its notifications moved from beside the new target they move to, with the
// source and target instance identities kept apart. A task no explicit
// handoff moved reports ErrNoHandoff.
func (m *MemoryManager) HandoffOf(ctx context.Context, taskID event.TaskID) (HandoffRecord, error) {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	entry, ok := m.registry.tasks[taskID]
	if !ok {
		return HandoffRecord{}, ErrTaskNotFound
	}
	if entry.handoff == nil {
		return HandoffRecord{}, ErrNoHandoff
	}
	return entry.handoffRecord, nil
}

// handoffPrefix names the mutation identities that carry one explicit handoff
// of a task's future notifications through the commit port.
const handoffPrefix = "handoff:"

// handoffMutation composes the mutation identity of one explicit handoff: the
// slot this handoff takes in the task's chain beside the target it moves the
// future notifications to. The identity is the one free field the frozen
// commit fact carries, so the handoff travels the durable fact chain with the
// task and a registry rebuilt from that chain applies the same routing move.
func handoffMutation(slot int, target HandoffTarget) (MutationID, error) {
	payload, err := json.Marshal(target)
	if err != nil {
		return "", err
	}
	return MutationID(handoffPrefix + strconv.Itoa(slot) + ":" + string(payload)), nil
}

// handoffTargetOf reports the handoff one mutation identity carries and whether
// it names one at all. An identity no handoff payload follows reports false, so
// only the mutations a handoff recorded move a task's routing.
func handoffTargetOf(mutation MutationID) (HandoffTarget, bool) {
	rest, ok := strings.CutPrefix(string(mutation), handoffPrefix)
	if !ok {
		return HandoffTarget{}, false
	}
	slot, payload, ok := strings.Cut(rest, ":")
	if !ok {
		return HandoffTarget{}, false
	}
	if _, err := strconv.Atoi(slot); err != nil {
		return HandoffTarget{}, false
	}
	var target HandoffTarget
	if err := json.Unmarshal([]byte(payload), &target); err != nil {
		return HandoffTarget{}, false
	}
	return target, true
}

// applyHandoff applies one explicit handoff to the task: the recorded link is
// rebuilt beside the move, the future notifications freeze the new target and
// target instance, and the delivery obligations of the notifications still awaiting delivery
// are reclassified as handed-off while their frozen routing never moves, and
// the ones already answered keep the target they froze. The same
// application serves the live move and the fold of a stored handoff fact, so a
// rebuilt task routes exactly as the run that recorded the handoff did.
func (t *memoryTask) applyHandoff(taskID event.TaskID, target HandoffTarget) {
	previous := t.spec.Target
	if t.handoff != nil {
		previous = t.handoff.Target
	}
	t.handoffRecord = HandoffRecord{
		TaskID:         taskID,
		PreviousTarget: previous,
		SourceInstance: t.spec.InstanceID,
		Target:         target.Target,
		TargetInstance: target.TargetInstance,
	}
	copied := target
	t.handoff = &copied
	for i := range t.obligations {
		open := &t.obligations[i]
		if open.obligation.Kind == ObligationAwaitingDelivery && !open.answered {
			open.obligation.Kind = ObligationHandedOff
		}
	}
}

// CloseInstance closes one runtime instance. The notifications the closed
// instance still owes delivery on are retained beside their frozen routing and
// carried by the recovery path (Retained and RecoveryMaterial): a flush refuses
// them by name as ErrInstanceClosed rather than dropping them or delivering
// them into a later instance that reuses the address. Closing an instance that
// holds nothing, or closing one twice, records nothing and reports no error.
func (m *MemoryManager) CloseInstance(ctx context.Context, instance event.InstanceID) error {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	m.closed[instance] = true
	return nil
}

// Retained reports the notifications one instance still owes delivery on, each
// beside the routing its envelope froze, ordered by the stable notification
// identity. This is what CloseInstance keeps and what RecoveryMaterial carries:
// a retained notification is never dropped and never delivered elsewhere.
func (m *MemoryManager) Retained(ctx context.Context, instance event.InstanceID) ([]NoticeEnvelope, error) {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	var kept []NoticeEnvelope
	for _, entry := range m.registry.tasks {
		for _, open := range entry.obligations {
			if !owesDelivery(open.obligation.Kind) || open.envelope.TargetInstance != instance {
				continue
			}
			kept = append(kept, open.envelope)
		}
	}
	sortEnvelopes(kept)
	return kept, nil
}

// RecoveryMaterial returns the durable fact chain covering the tasks whose
// notifications the named instance still owes, one chain per task in commit
// order with the admission material on its first link. Fed to Restore, the
// chain re-opens the obligations it carries: the undelivered notifications of
// a closed instance are kept by the recovery path either way, never dropped
// with the instance that owned them.
func (m *MemoryManager) RecoveryMaterial(ctx context.Context, instance event.InstanceID) ([]TaskRecord, error) {
	m.registry.mu.Lock()
	defer m.registry.mu.Unlock()
	owed := map[event.TaskID]bool{}
	for id, entry := range m.registry.tasks {
		for _, open := range entry.obligations {
			if owesDelivery(open.obligation.Kind) && open.envelope.TargetInstance == instance {
				owed[id] = true
				break
			}
		}
	}
	ids := make([]event.TaskID, 0, len(owed))
	for id := range owed {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return taskSerial(ids[i]) < taskSerial(ids[j]) })
	var chain []TaskRecord
	for _, id := range ids {
		entry := m.registry.tasks[id]
		records := make([]CommitRecord, 0, len(entry.mutations))
		for _, record := range entry.mutations {
			records = append(records, record)
		}
		sort.Slice(records, func(i, j int) bool { return records[i].ExpectedRevision < records[j].ExpectedRevision })
		for i, fact := range records {
			link := TaskRecord{Fact: fact}
			if i == 0 {
				link.Spec = entry.spec
			}
			chain = append(chain, link)
		}
	}
	return chain, nil
}

// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// NoticeSequencer hands out the stable sequence numbers that complete a
// notification's identity within one category of one execution round. The
// first notification of a category takes sequence 0 and each later one takes
// the next, so every notification keeps its own identity: a repeated transport
// is recognized as the same notification and records no second fact.
type NoticeSequencer struct {
	mu   sync.Mutex
	next map[noticeSeqKey]int64
}

// noticeSeqKey is the sequence namespace of one notification category: the
// task, the execution round and the category the sequence numbers belong to.
type noticeSeqKey struct {
	task  event.TaskID
	round int64
	kind  NoticeKind
}

// NewNoticeSequencer returns a sequencer that has handed out nothing yet.
func NewNoticeSequencer() *NoticeSequencer {
	return &NoticeSequencer{next: map[noticeSeqKey]int64{}}
}

// Issue returns the next notification identity in the given category of one
// execution round, pointing at the fact it reports. The sequence number is
// stable: the identity it returns is the identity a retry keeps.
func (s *NoticeSequencer) Issue(taskID event.TaskID, round int64, kind NoticeKind, fact event.EventID) Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := noticeSeqKey{task: taskID, round: round, kind: kind}
	seq := s.next[key]
	s.next[key] = seq + 1
	return Notice{TaskID: taskID, Round: round, Kind: kind, Seq: seq, Event: fact}
}

// Observe records an identity that was already issued — read back from a fact
// chain, for instance — so the sequencer continues past its sequence number
// and never hands that identity out a second time.
func (s *NoticeSequencer) Observe(notice Notice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := noticeSeqKey{task: notice.TaskID, round: notice.Round, kind: notice.Kind}
	if s.next[key] <= notice.Seq {
		s.next[key] = notice.Seq + 1
	}
}

// IssueNotice allocates the stable identity of one notification of the named
// task and freezes its delivery routing into the envelope it returns: the
// target, source and target instance of the declaration the task was admitted
// under, or of the explicit handoff already applied. A handoff touches only the
// notifications issued after it, so the envelopes already issued keep the
// routing they froze and a retry of one never re-resolves a current target.
func (r *MemoryRegistry) IssueNotice(ctx context.Context, taskID event.TaskID, kind NoticeKind, fact event.EventID) (NoticeEnvelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return NoticeEnvelope{}, ErrTaskNotFound
	}
	notice := r.seq.Issue(taskID, entry.round, kind, fact)
	return entry.envelope(notice), nil
}

// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// EnvelopeDeliverer reports the delivery-layer outcome of one notification
// delivered beside the routing its envelope froze. A manager attached to one
// offers every notification by its own envelope, so a retry repeats the frozen
// routing and no delivery ever resolves a current target.
type EnvelopeDeliverer func(ctx context.Context, envelope NoticeEnvelope) (DeliveryAcceptance, error)

// ManagerOption configures an in-memory task manager at construction.
type ManagerOption func(*MemoryManager)

// WithEnvelopeDeliverer attaches the deliverer a manager offers its
// notifications to beside the routing each notification's envelope froze. It
// reports the same three delivery outcomes as a plain deliverer and changes no
// obligation bookkeeping.
func WithEnvelopeDeliverer(deliver EnvelopeDeliverer) ManagerOption {
	return func(m *MemoryManager) { m.deliverEnvelope = deliver }
}

// deliverOne offers one notification for delivery, beside the routing its
// envelope froze when an envelope deliverer is attached, and as the bare
// notification identity otherwise.
func (m *MemoryManager) deliverOne(ctx context.Context, envelope NoticeEnvelope) (DeliveryAcceptance, error) {
	if m.deliverEnvelope != nil {
		return m.deliverEnvelope(ctx, envelope)
	}
	return m.deliver(ctx, envelope.Notice)
}

// owesDelivery reports whether an obligation still owes a notification's
// delivery, whatever instance its frozen routing names.
func owesDelivery(kind ObligationKind) bool {
	return kind == ObligationAwaitingDelivery || kind == ObligationHandedOff
}

// sortEnvelopes orders notifications by their stable identity.
func sortEnvelopes(envelopes []NoticeEnvelope) {
	sort.Slice(envelopes, func(i, j int) bool {
		left, right := envelopes[i].Notice, envelopes[j].Notice
		if left.TaskID != right.TaskID {
			return left.TaskID < right.TaskID
		}
		if left.Round != right.Round {
			return left.Round < right.Round
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Seq < right.Seq
	})
}

package task

import (
	"context"
	"errors"
	"slices"
	"sort"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
//
// MutationID identifies one task state mutation for idempotent commit. A repeated
// commit of the same mutation does not record a second fact, and a commit that
// reuses an identity with different content is rejected.
type MutationID string

// NoticeEnvelope is the self-contained delivery unit of one notification: the
// stable notice identity beside the delivery routing frozen when the transition
// that carried it was committed. A retry delivers by the envelope it carries and
// never resolves a current target: after a restart or a handoff, a committed
// notification keeps the target it was frozen under, and only the notifications
// created after a handoff freeze the new target.
type NoticeEnvelope struct {
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

// CommitRecord is the self-contained unit a commit port stores: the expected
// revision it applies to, the status change it carries, and the notifications
// the transition intends to send, each with the delivery routing it froze.
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
	Outbox []NoticeEnvelope
}

// CommitResult is the outcome a commit port reports for one CommitRecord. The
// three outcomes are reported separately and are never merged into one value.
type CommitResult int

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

// Commit is the port through which a task status transition is stored. A
// registry takes this port at the transition and submits the status change and
// its pending notifications as one record.
//
// The redline list (4 rules):
// MUST store the status change and its pending notifications as one self-contained record, at the transition.
// MUST answer committed, known-not-committed and uncertain as three separate values.
// MUST NOT make a status or a notification visible before its record is committed.
// MUST NOT repeat a side effect of an uncertain commit before that commit is reconciled.
type Commit interface {
	// Commit stores one record and reports the commit outcome. A committed
	// record may be made visible; an uncertain record is reconciled before any
	// automatic re-run.
	Commit(ctx context.Context, record CommitRecord) (CommitResult, error)
}

// ErrRevisionConflict reports a commit whose expected revision names another
// revision than the one the task is at, so the record is known not to be stored.
var ErrRevisionConflict = errors.New("task: commit record expects another revision")

// ErrMutationConflict reports a commit that reuses a recorded mutation identity
// with different content.
var ErrMutationConflict = errors.New("task: mutation identity carries different content")

// ErrMutationMissing reports a commit record that carries no mutation identity,
// so no repeat of that record could ever be recognized as the same mutation.
var ErrMutationMissing = errors.New("task: commit record carries no mutation identity")

// DeliveryAcceptance is the delivery-layer outcome of one notification: whether
// the target accepted the delivery. It is the third result beside the admission
// receipt and the commit result, and the three are never merged into one value.
type DeliveryAcceptance int

const (
	// DeliveryPending is the zero value: no target acceptance has been observed.
	DeliveryPending DeliveryAcceptance = iota
	// DeliveryAccepted is a delivery the target explicitly accepted.
	DeliveryAccepted
	// DeliveryRejected is a delivery the target refused.
	DeliveryRejected
)

// UncertainProbe reports whether one commit attempt is observed as uncertain: the
// storage outcome of that attempt is unknown and is reconciled before any repeat.
type UncertainProbe func(CommitRecord) bool

// OutboxNotice is one notification a committed record still owes delivery, paired
// with the delivery-layer state of that notification. The envelope it carries
// keeps the routing frozen at the transition that committed it.
type OutboxNotice struct {
	// Notice is the self-contained notification owed: its stable identity beside
	// the delivery routing frozen at the transition that committed it.
	Notice NoticeEnvelope
	// Delivery is the delivery-layer state of the notification. The storage
	// layer leaves every notification pending: only an explicit acceptance by
	// the target moves it.
	Delivery DeliveryAcceptance
}

// mutationKey is the idempotency identity of one mutation: the task it changes
// and the mutation identifier the record carries. The expected revision, the
// status change and the outbox are the content of that mutation, so the same
// identity with different content is refused rather than stored twice.
type mutationKey struct {
	task     event.TaskID
	mutation MutationID
}

// noticeKey is the stable identity of one notification: the task, the execution
// round, the category and the sequence number inside the category.
type noticeKey struct {
	task  event.TaskID
	round int64
	kind  NoticeKind
	seq   int64
}

// MemoryCommit is the in-memory carrier behind the Commit port: it stores one
// self-contained record per mutation identity, keeps the revision each record
// advanced, and holds every committed notification as an open delivery
// obligation. The memory carrier keeps the business contract of a durable one
// and promises no recovery of its own.
type MemoryCommit struct {
	mu        sync.Mutex
	uncertain UncertainProbe
	revisions map[event.TaskID]int64
	facts     map[event.TaskID][]CommitRecord
	mutations map[mutationKey]CommitRecord
	owed      map[noticeKey]OutboxNotice
}

// MemoryCommit satisfies the commit port.
var _ Commit = (*MemoryCommit)(nil)

// NewMemoryCommit returns an empty in-memory commit carrier. uncertain, when it
// is not nil, marks matching commit attempts as uncertain, so all three commit
// outcomes stay observable.
func NewMemoryCommit(uncertain UncertainProbe) *MemoryCommit {
	return &MemoryCommit{
		uncertain: uncertain,
		revisions: map[event.TaskID]int64{},
		facts:     map[event.TaskID][]CommitRecord{},
		mutations: map[mutationKey]CommitRecord{},
		owed:      map[noticeKey]OutboxNotice{},
	}
}

// Commit implements the commit port. A record of a mutation identity the carrier
// already stores returns the fact already stored and records no second fact; the
// same identity with different content reports CommitNotCommitted beside a named
// error, as does a record expecting another revision. An uncertain attempt
// reports CommitUncertain and stores nothing until that record is submitted
// once more.
func (m *MemoryCommit) Commit(ctx context.Context, record CommitRecord) (CommitResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if record.MutationID == "" {
		return CommitNotCommitted, ErrMutationMissing
	}
	key := mutationKey{task: record.TaskID, mutation: record.MutationID}
	if prior, ok := m.mutations[key]; ok {
		if !sameContent(prior, record) {
			return CommitNotCommitted, ErrMutationConflict
		}
		return CommitCommitted, nil
	}
	if m.uncertain != nil && m.uncertain(record) {
		return CommitUncertain, nil
	}
	if current := m.revisions[record.TaskID]; current != record.ExpectedRevision {
		return CommitNotCommitted, ErrRevisionConflict
	}
	m.revisions[record.TaskID] = record.ExpectedRevision + 1
	m.facts[record.TaskID] = append(m.facts[record.TaskID], record)
	m.mutations[key] = record
	for _, envelope := range record.Outbox {
		m.owed[noticeKey{task: envelope.Notice.TaskID, round: envelope.Notice.Round, kind: envelope.Notice.Kind, seq: envelope.Notice.Seq}] = OutboxNotice{
			Notice:   envelope,
			Delivery: DeliveryPending,
		}
	}
	return CommitCommitted, nil
}

// sameContent reports whether a stored record and a repeated one carry the same
// content: the same expected revision, the same status change and the same
// outbox.
func sameContent(stored, record CommitRecord) bool {
	return stored.ExpectedRevision == record.ExpectedRevision &&
		stored.Status == record.Status &&
		slices.Equal(stored.Outbox, record.Outbox)
}

// Facts returns the records the carrier stored for one task, in commit order.
func (m *MemoryCommit) Facts(taskID event.TaskID) []CommitRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]CommitRecord(nil), m.facts[taskID]...)
}

// Revision returns the revision the task is at after the records stored for it.
func (m *MemoryCommit) Revision(taskID event.TaskID) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.revisions[taskID]
}

// Outbox returns the notifications one task still owes delivery, each paired
// with its delivery-layer state. A commit never moves a delivery state, so an
// idempotent repeat of a committed record leaves its notifications owed.
func (m *MemoryCommit) Outbox(taskID event.TaskID) []OutboxNotice {
	m.mu.Lock()
	defer m.mu.Unlock()
	var owed []OutboxNotice
	for key, entry := range m.owed {
		if key.task == taskID {
			owed = append(owed, entry)
		}
	}
	sort.Slice(owed, func(i, j int) bool {
		left, right := owed[i].Notice.Notice, owed[j].Notice.Notice
		if left.Round != right.Round {
			return left.Round < right.Round
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Seq < right.Seq
	})
	return owed
}

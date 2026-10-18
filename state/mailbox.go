package state

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"
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

// noticeKey is the stable identity of one notification: the task, the execution
// round, the category and the sequence number inside the category.
type noticeKey struct {
	task  event.TaskID
	round int64
	kind  task.NoticeKind
	seq   int64
}

// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// AcceptNotice implements Store.AcceptNotice: one notification enters the
// mailbox of the site its delivery registration names beside a monotonic
// acceptance sequence. The entry records the association to its source facts —
// the committed fact the notification points at, the responsibility range of the
// call that produced it, and the stable notification identity itself — so the
// entry is traceable back to the event, the invocation and the notification
// that produced it. Acceptance is idempotent by the notification's stable
// identity, so a replayed delivery records no second fact and keeps the target
// the first acceptance recorded.
func (m *MemoryStore) AcceptNotice(ctx context.Context, notice task.Notice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, accepted, err := m.noticeSeat(notice); err != nil || accepted {
		return err
	}
	route, ok := m.deliveries[notice.TaskID]
	if !ok {
		return ErrDeliveryNotAllowed
	}
	target := siteKey{addr: route.Target, instance: route.TargetInstance}
	held, err := m.openTarget(target)
	if err != nil {
		return err
	}
	m.appendEntry(ctx, held, notice, target)
	return nil
}

// noticeKeyOf is the stable identity of one notification, taken from the
// identity the notification carries.
func noticeKeyOf(notice task.Notice) noticeKey {
	return noticeKey{task: notice.TaskID, round: notice.Round, kind: notice.Kind, seq: notice.Seq}
}

// noticeSeatOf reads the accepted seat of one notice under the store lock: the read-only precheck
// face for callers that hold no lock.
func (m *MemoryStore) noticeSeatOf(notice task.Notice) (AcceptSeq, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.noticeSeat(notice)
}

// noticeSeat reports where one notification already sits: the acceptance sequence it took beside
// whether it was accepted at all; a notification that reuses a recorded identity with different
// content is refused by name.
func (m *MemoryStore) noticeSeat(notice task.Notice) (AcceptSeq, bool, error) {
	if notice.TaskID == "" {
		return 0, false, ErrNoticeIdentityMissing
	}
	prior, ok := m.accepted[noticeKeyOf(notice)]
	if !ok {
		return 0, false, nil
	}
	if prior.notice != notice {
		return 0, false, ErrNoticeConflict
	}
	return prior.seq, true, nil
}

// appendEntry appends one notification to the mailbox of the target site under
// the acceptance sequence that site assigns next.
func (m *MemoryStore) appendEntry(ctx context.Context, held *site, notice task.Notice, target siteKey) AcceptSeq {
	seq := held.nextSeq
	held.nextSeq++
	held.session.Mailbox = append(held.session.Mailbox, MailboxEntry{
		Notice:       notice,
		EventID:      notice.Event,
		InvocationID: invocationOf(ctx),
		AcceptSeq:    seq,
		Claim:        ClaimPending,
		Receipt:      ConsumeReceipt{},
	})
	m.accepted[noticeKeyOf(notice)] = acceptedNotice{notice: notice, site: target, seq: seq}
	return seq
}

// openTarget returns the site one delivery target names, or the error naming
// why no input may enter it.
func (m *MemoryStore) openTarget(target siteKey) (*site, error) {
	held, ok := m.sites[target]
	if !ok {
		return nil, ErrUnknownTarget
	}
	if held.retired {
		return nil, ErrInstanceRetired
	}
	if held.closed {
		return nil, ErrSessionClosed
	}
	return held, nil
}

// invocationOf reports the responsibility range the call that produced one
// delivery belongs to, taken from the call identity the delivery context
// carries. A delivery beside no call frame reports an empty range.
func invocationOf(ctx context.Context) event.InvocationID {
	if frame, ok := task.CallFrameFrom(ctx); ok {
		return frame.Identity.InvocationID
	}
	return ""
}

// TrackDelivery registers where the future notifications of one task are
// delivered: the target address and the target instance that receive them. The
// registration is the delivery grant a notification is accepted beside, a later
// registration reroutes what is not yet accepted, and the target of an accepted
// notification stays as its acceptance recorded it.
func (m *MemoryStore) TrackDelivery(ctx context.Context, taskID event.TaskID, target task.HandoffTarget) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if taskID == "" {
		return ErrTaskIdentityMissing
	}
	if !target.Target.IsComplete() {
		return ErrIncompleteAddress
	}
	if target.TargetInstance == "" {
		return ErrInstanceMissing
	}
	m.deliveries[taskID] = target
	return nil
}

// Claim moves one accepted input from pending to claimed in the mailbox of the
// site named. The acceptance sequence stays as acceptance assigned it and the
// consumption receipt stays unrecorded until consumption completes.
func (m *MemoryStore) Claim(ctx context.Context, addr event.Address, instance event.InstanceID, seq AcceptSeq) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, err := m.openSite(addr, instance)
	if err != nil {
		return err
	}
	entry := entryAt(&held.session, seq)
	if entry == nil {
		return ErrEntryUnknown
	}
	if entry.Claim != ClaimPending {
		return ErrClaimConflict
	}
	entry.Claim = ClaimClaimed
	return nil
}

// Consume records the consumption of one claimed input in the mailbox of the
// site named: the entry moves to consumed, its receipt records the completion
// and the consumption position advances over the longest contiguous completed
// prefix of the acceptance sequences. A completion beside an earlier unfinished
// entry is recorded on its own entry while the position stays before the hole
// it leaves. An entry already consumed stays as it is, and an entry no executor
// claimed is refused.
func (m *MemoryStore) Consume(ctx context.Context, addr event.Address, instance event.InstanceID, seq AcceptSeq) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, err := m.openSite(addr, instance)
	if err != nil {
		return err
	}
	entry := entryAt(&held.session, seq)
	if entry == nil {
		return ErrEntryUnknown
	}
	if entry.Claim == ClaimConsumed {
		return nil
	}
	if entry.Claim != ClaimClaimed {
		return ErrNotClaimed
	}
	entry.Claim = ClaimConsumed
	entry.Receipt = ConsumeReceipt{Completed: true}
	for {
		next := entryAt(&held.session, held.session.Consumed+1)
		if next == nil || next.Claim != ClaimConsumed {
			break
		}
		held.session.Consumed++
	}
	return nil
}

// Reclaim reasserts the claim of one mailbox entry an earlier claim left
// unfinished, so an executor that stopped beside a claim picks the same input
// up again and finishes it. The acceptance sequence and the claim state stay as
// acceptance and the first claim recorded them: an entry still pending is
// claimed through Claim, and an entry whose consumption completed is refused
// by name.
func (m *MemoryStore) Reclaim(ctx context.Context, addr event.Address, instance event.InstanceID, seq AcceptSeq) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, err := m.openSite(addr, instance)
	if err != nil {
		return err
	}
	entry := entryAt(&held.session, seq)
	if entry == nil {
		return ErrEntryUnknown
	}
	switch entry.Claim {
	case ClaimConsumed:
		return ErrClaimSettled
	case ClaimPending:
		return ErrNotClaimed
	}
	return nil
}

// openSite returns the site one address and instance name, or the error naming
// why no site answers.
func (m *MemoryStore) openSite(addr event.Address, instance event.InstanceID) (*site, error) {
	if !addr.IsComplete() {
		return nil, ErrIncompleteAddress
	}
	if instance == "" {
		return nil, ErrInstanceMissing
	}
	held, ok := m.sites[siteKey{addr: addr, instance: instance}]
	if !ok {
		return nil, ErrUnknownTarget
	}
	return held, nil
}

// entryAt returns the mailbox entry at one acceptance sequence, or nil when the
// mailbox holds no entry there.
func entryAt(s *Session, seq AcceptSeq) *MailboxEntry {
	for i := range s.Mailbox {
		if s.Mailbox[i].AcceptSeq == seq {
			return &s.Mailbox[i]
		}
	}
	return nil
}

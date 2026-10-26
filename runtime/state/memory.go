package state

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

var (
	// ErrIncompleteAddress reports an address that does not name both an agent
	// and a session, so no site and no target can hang on it.
	ErrIncompleteAddress = errors.New("state: address does not name both an agent and a session")
	// ErrInstanceMissing reports a site operation that carries no instance
	// identity, so the instance that owns the site cannot be told apart from
	// another one.
	ErrInstanceMissing = errors.New("state: operation carries no instance identity")
	// ErrUnknownTarget reports a target no established site serves.
	ErrUnknownTarget = errors.New("state: no site serves the named target")
	// ErrSessionClosed reports input aimed at a site whose own side is closed
	// to new input.
	ErrSessionClosed = errors.New("state: the session side of the named site is closed")
	// ErrInstanceRetired reports input aimed at the site of an instance another
	// instance has replaced under the same address.
	ErrInstanceRetired = errors.New("state: the named instance is retired")
	// ErrPublishNotCarried reports a publication the memory store holds no
	// effective binding generation for.
	ErrPublishNotCarried = errors.New("state: publication is not carried by this store")
)

// siteKey is one site's identity: the explicit address it serves and the
// runtime instance that owns it. The same address under another instance names
// another site.
type siteKey struct {
	addr     event.Address
	instance event.InstanceID
}

// site is one established session site: the session bookkeeping it exposes
// beside the flags that govern accepting further input and the acceptance
// sequence acceptance assigns from.
type site struct {
	session Session
	closed  bool
	retired bool
	nextSeq AcceptSeq
}

// acceptedNotice is one accepted notification beside the site whose mailbox
// holds it and the acceptance sequence it took there, kept so a replayed
// delivery recognizes the fact already recorded and the entry it entered.
type acceptedNotice struct {
	notice task.Notice
	site   siteKey
	seq    AcceptSeq
}

// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
//
// MemoryStore is the in-memory carrier behind the Store face: it establishes
// one site per explicit address and instance, keeps the instance currently
// serving each address, and routes a notification to the site its delivery
// registration names. The memory carrier keeps the business contract of a
// durable one and promises no recovery of its own.
type MemoryStore struct {
	mu           sync.Mutex
	authority    *Authority
	sites        map[siteKey]*site
	current      map[event.Address]event.InstanceID
	deliveries   map[event.TaskID]task.HandoffTarget
	accepted     map[noticeKey]acceptedNotice
	consumptions durableConsumption
}

// MemoryStore satisfies the Store face.
var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty in-memory state store that checks reads
// against the given authority. An authority that is not present refuses every
// read. A consumption recorder, when one is supplied, receives the receipt of
// every completed logical consumption behind its own sync barrier before the
// mailbox confirms the entry; a store beside none keeps the consumption in its
// own bookkeeping alone.
func NewMemoryStore(authority *Authority, consumptions ...*DurableCommit) *MemoryStore {
	store := &MemoryStore{
		authority:  authority,
		sites:      map[siteKey]*site{},
		current:    map[event.Address]event.InstanceID{},
		deliveries: map[event.TaskID]task.HandoffTarget{},
		accepted:   map[noticeKey]acceptedNotice{},
	}
	if len(consumptions) > 0 && consumptions[0] != nil {
		store.consumptions = consumptions[0]
	}
	return store
}

// Establish implements Store.Establish. One explicit address and instance names
// one site: establishing the address on another instance retires the instance
// that held it, so input aimed at the retired instance is refused while the
// obligations already accepted there stay readable. The pair already serving
// the address returns its own bookkeeping.
func (m *MemoryStore) Establish(ctx context.Context, addr event.Address, instance event.InstanceID) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !addr.IsComplete() {
		return nil, ErrIncompleteAddress
	}
	if instance == "" {
		return nil, ErrInstanceMissing
	}
	key := siteKey{addr: addr, instance: instance}
	if held, ok := m.sites[key]; ok {
		if held.retired {
			return nil, ErrInstanceRetired
		}
		if held.closed {
			return nil, ErrSessionClosed
		}
		return snapshot(&held.session), nil
	}
	if priorInstance, ok := m.current[addr]; ok && priorInstance != instance {
		if prior, ok := m.sites[siteKey{addr: addr, instance: priorInstance}]; ok {
			prior.retired = true
		}
	}
	opened := &site{
		session: Session{
			Address:  addr,
			Instance: instance,
		},
		nextSeq: 1,
	}
	m.sites[key] = opened
	m.current[addr] = instance
	return snapshot(&opened.session), nil
}

// Publish implements Store.Publish and reports that the memory store carries no
// effective binding publication.
func (m *MemoryStore) Publish(ctx context.Context, plan StatePlan) (Publication, error) {
	return Publication{}, ErrPublishNotCarried
}

// CloseSession implements Store.CloseSession. It closes the own side of the
// site currently serving the address: new input is refused while the
// obligations already accepted stay readable and settleable.
func (m *MemoryStore) CloseSession(ctx context.Context, addr event.Address) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !addr.IsComplete() {
		return ErrIncompleteAddress
	}
	instance, ok := m.current[addr]
	if !ok {
		return ErrUnknownTarget
	}
	held, ok := m.sites[siteKey{addr: addr, instance: instance}]
	if !ok {
		return ErrUnknownTarget
	}
	held.closed = true
	return nil
}

// Site returns a copy of the bookkeeping one established site holds, for the
// subject and instance named. A read is answered only beside a recorded grant,
// and a target no site serves is refused rather than established.
func (m *MemoryStore) Site(ctx context.Context, subject Subject, addr event.Address, instance event.InstanceID) (*Session, error) {
	if err := m.authority.CheckRead(ctx, subject, addr); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
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
	return snapshot(&held.session), nil
}

// snapshot returns a copy of one session's bookkeeping, so a reader holds no
// reference into the site itself.
func snapshot(s *Session) *Session {
	out := &Session{
		Address:  s.Address,
		Instance: s.Instance,
		Consumed: s.Consumed,
		Mailbox:  append([]MailboxEntry(nil), s.Mailbox...),
	}
	return out
}

// WorkSeat is one input the mailbox still owes a decision for: the site it sits
// at, the acceptance sequence it took there, and the notification it accepted
// beside the claim an executor may hold it under. A seat is the one account a
// driven turn is started from, so the question whether work waits is answered
// in one place.
type WorkSeat struct {
	// Address is the explicit address of the site the entry sits at.
	Address event.Address
	// Instance is the runtime instance that owns that site.
	Instance event.InstanceID
	// AcceptSeq is the acceptance sequence the entry took at its site.
	AcceptSeq AcceptSeq
	// Notice is the stable identity of the notification the entry accepted.
	Notice task.Notice
	// Claim is the claim state the entry stands at.
	Claim ClaimState
}

// Unconsumed reports every mailbox entry whose consumption has not completed,
// across every site this store holds, ordered by address, instance and
// acceptance sequence so the same account always answers in the same order. An
// entry an executor claimed but has not consumed yet still owes its work and
// stays listed until its consumption completes.
func (m *MemoryStore) Unconsumed() []WorkSeat {
	m.mu.Lock()
	defer m.mu.Unlock()
	var seats []WorkSeat
	for key, held := range m.sites {
		for i := range held.session.Mailbox {
			entry := &held.session.Mailbox[i]
			if entry.Claim == ClaimConsumed {
				continue
			}
			seats = append(seats, WorkSeat{
				Address:   key.addr,
				Instance:  key.instance,
				AcceptSeq: entry.AcceptSeq,
				Notice:    entry.Notice,
				Claim:     entry.Claim,
			})
		}
	}
	sort.Slice(seats, func(i, j int) bool {
		left, right := seats[i], seats[j]
		if left.Address.AgentID != right.Address.AgentID {
			return left.Address.AgentID < right.Address.AgentID
		}
		if left.Address.SessionID != right.Address.SessionID {
			return left.Address.SessionID < right.Address.SessionID
		}
		if left.Instance != right.Instance {
			return left.Instance < right.Instance
		}
		return left.AcceptSeq < right.AcceptSeq
	})
	return seats
}

// RestoreOwed folds the notifications the durable account behind this mailbox
// still owes back into the mailbox: the target site each notification's frozen
// routing names is opened beside the instance that froze it, so the work a
// stopped run left unconsumed waits for the next decision instead of being lost
// with the process. The restore is idempotent — a notification already sitting
// in its mailbox is accepted once and never appended twice — and the site it
// opens beside a frozen instance never takes the address over from the
// instance currently serving it. A mailbox beside no durable account restores
// nothing, because nothing outside the process owes it work.
func (m *MemoryStore) RestoreOwed(ctx context.Context) error {
	if m.consumptions == nil {
		return nil
	}
	owed := m.consumptions.OwedNotices()
	for _, owed := range owed {
		envelope := owed.Notice
		if err := m.TrackDelivery(ctx, envelope.Notice.TaskID, task.HandoffTarget{
			Target:         envelope.Target,
			TargetInstance: envelope.TargetInstance,
		}); err != nil {
			return err
		}
		if err := m.restoreSeat(envelope.Target, envelope.TargetInstance); err != nil {
			return err
		}
		if err := m.AcceptNotice(ctx, envelope.Notice); err != nil {
			return err
		}
	}
	return nil
}

// restoreSeat opens the site one frozen routing names beside the instance that
// froze it, so an acceptance recorded under a stopped instance lands in the
// mailbox account instead of being refused as unknown. The site is opened
// without touching the instance currently serving the address, so the
// retirement bookkeeping of a live instance is never rewritten by a restore.
func (m *MemoryStore) restoreSeat(addr event.Address, instance event.InstanceID) error {
	if !addr.IsComplete() {
		return ErrIncompleteAddress
	}
	if instance == "" {
		return ErrInstanceMissing
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := siteKey{addr: addr, instance: instance}
	if held, ok := m.sites[key]; ok {
		if held.closed {
			return ErrSessionClosed
		}
		return nil
	}
	m.sites[key] = &site{
		session: Session{Address: addr, Instance: instance},
		nextSeq: 1,
	}
	return nil
}

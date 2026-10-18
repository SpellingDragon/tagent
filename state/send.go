package state

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"
)

var (
	// ErrSendTargetMissing reports an internal send that names no complete
	// target address, so the delivery is refused rather than aimed at a default
	// or at the most recent peer.
	ErrSendTargetMissing = errors.New("state: the send names no explicit target")
	// ErrDeliveryTargetNotRunning reports a delivery to a target whose loop is
	// not running. Opening a loop for that target is never this seam's answer.
	ErrDeliveryTargetNotRunning = errors.New("state: the delivery target is not running")
)

// RefusalCategory names the gate one delivery was refused at.
type RefusalCategory string

const (
	// RefusalNotAuthorized is a target outside the sender's deliver_to
	// whitelist.
	RefusalNotAuthorized RefusalCategory = "not_authorized"
	// RefusalUnknownTarget is a target no established site serves.
	RefusalUnknownTarget RefusalCategory = "unknown_target"
	// RefusalTargetNotRunning is a target whose loop is not running.
	RefusalTargetNotRunning RefusalCategory = "target_not_running"
)

// Refusal is one refused delivery in the sender's ledger: the notification it
// carried, the target it named, the gate that refused it and the time of the
// refusal, so the refusal is queryable by the target it named.
type Refusal struct {
	// Notice is the notification the refused delivery carried.
	Notice task.Notice
	// Target is the address the refused delivery named.
	Target event.Address
	// Category names the gate that refused the delivery.
	Category RefusalCategory
	// At is the time the delivery was refused.
	At time.Time
}

// Delivery is one internal send: the explicit target site one notification
// enters.
type Delivery struct {
	// Target is the address of the site the notification enters.
	Target event.Address
	// TargetInstance is the runtime instance serving that site.
	TargetInstance event.InstanceID
	// Notice is the notification the target mailbox accepts. Its stable
	// identity is what a repeated delivery of the same send is recognized by.
	Notice task.Notice
}

// SendReceipt is the outcome of one delivery that passed the gates.
type SendReceipt struct {
	// AcceptSeq is the acceptance sequence of the mailbox entry the
	// notification entered.
	AcceptSeq AcceptSeq
	// Duplicate reports that the notification was already accepted, so this
	// delivery appended no second entry and kept the site the first acceptance
	// recorded.
	Duplicate bool
}

// RunningCheck reports whether the execution loop of one target site is
// running.
type RunningCheck func(addr event.Address, instance event.InstanceID) bool

// 契约: docs/wiki/reliability/durable-delivery.md#lineage-visibility
//
// SendFace is the internal send seam of one sender: it delivers a notification
// into an explicitly addressed mailbox after the permission, residency and
// liveness gates, and keeps the ledger of the deliveries it refused.
//
// The seam resolves no target of its own. A delivery naming no complete target
// is refused, and a target outside the registered whitelist is refused beside a
// ledger entry naming it.
type SendFace struct {
	store   *MemoryStore
	allowed map[event.AgentID]struct{}
	running RunningCheck
	mu      sync.Mutex
	ledger  []Refusal
}

// SendOption configures the send seam at construction.
type SendOption func(*SendFace)

// WithDeliverTo registers the sender's deliver_to whitelist: the agent names it
// may deliver to. The whitelist starts empty, so a sender that registers none
// refuses every target.
func WithDeliverTo(targets ...event.AgentID) SendOption {
	return func(f *SendFace) {
		for _, target := range targets {
			if target == "" {
				continue
			}
			f.allowed[target] = struct{}{}
		}
	}
}

// WithRunningCheck attaches the liveness check of the execution loops the
// sender delivers to.
func WithRunningCheck(check RunningCheck) SendOption {
	return func(f *SendFace) { f.running = check }
}

// NewSendFace returns the send seam of one sender over the given store.
func NewSendFace(store *MemoryStore, options ...SendOption) *SendFace {
	face := &SendFace{store: store, allowed: map[event.AgentID]struct{}{}}
	for _, option := range options {
		option(face)
	}
	return face
}

// Send delivers one notification into the explicit target it names. The gates
// run in order — target named, whitelist, residency, liveness — and the first
// one that refuses records a ledger entry and returns its named error.
//
// A notification already accepted is answered with the entry it took and the
// site the first acceptance chose: a repeat records no second fact and reopens
// no gate.
func (f *SendFace) Send(ctx context.Context, delivery Delivery) (SendReceipt, error) {
	if !delivery.Target.IsComplete() {
		return SendReceipt{}, ErrSendTargetMissing
	}
	seq, accepted, err := f.store.noticeSeatOf(delivery.Notice)
	if err != nil {
		return SendReceipt{}, err
	}
	if accepted {
		return SendReceipt{AcceptSeq: seq, Duplicate: true}, nil
	}
	if _, ok := f.allowed[delivery.Target.AgentID]; !ok {
		f.refused(delivery, RefusalNotAuthorized)
		return SendReceipt{}, ErrDeliveryNotAllowed
	}
	if !f.store.resident(delivery.Target, delivery.TargetInstance) {
		f.refused(delivery, RefusalUnknownTarget)
		return SendReceipt{}, ErrUnknownTarget
	}
	if f.running != nil && !f.running(delivery.Target, delivery.TargetInstance) {
		f.refused(delivery, RefusalTargetNotRunning)
		return SendReceipt{}, ErrDeliveryTargetNotRunning
	}
	receipt, err := f.store.acceptDelivery(ctx, delivery)
	switch {
	case err == nil:
		return receipt, nil
	case errors.Is(err, ErrUnknownTarget):
		f.refused(delivery, RefusalUnknownTarget)
	case errors.Is(err, ErrSessionClosed), errors.Is(err, ErrInstanceRetired):
		f.refused(delivery, RefusalTargetNotRunning)
	}
	return SendReceipt{}, err
}

// Refusals reports the deliveries this sender refused, in the order it refused
// them.
func (f *SendFace) Refusals() []Refusal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Refusal(nil), f.ledger...)
}

// refused records one refused delivery beside the gate that refused it.
func (f *SendFace) refused(delivery Delivery, category RefusalCategory) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ledger = append(f.ledger, Refusal{
		Notice:   delivery.Notice,
		Target:   delivery.Target,
		Category: category,
		At:       time.Now(),
	})
}

// acceptDelivery accepts one delivery at the explicit target it names: the
// routing the task's future notifications follow and the entry the notification
// takes are recorded under one lock, so no other delivery interleaves between
// them.
func (m *MemoryStore) acceptDelivery(ctx context.Context, delivery Delivery) (SendReceipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seq, accepted, err := m.noticeSeat(delivery.Notice)
	if err != nil {
		return SendReceipt{}, err
	}
	if accepted {
		return SendReceipt{AcceptSeq: seq, Duplicate: true}, nil
	}
	target := siteKey{addr: delivery.Target, instance: delivery.TargetInstance}
	held, err := m.openTarget(target)
	if err != nil {
		return SendReceipt{}, err
	}
	m.deliveries[delivery.Notice.TaskID] = task.HandoffTarget{
		Target:         delivery.Target,
		TargetInstance: delivery.TargetInstance,
	}
	return SendReceipt{AcceptSeq: m.appendEntry(ctx, held, delivery.Notice, target)}, nil
}

// resident reports whether one established site serves the address and instance
// named and still accepts input.
func (m *MemoryStore) resident(addr event.Address, instance event.InstanceID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, ok := m.sites[siteKey{addr: addr, instance: instance}]
	return ok && !held.retired && !held.closed
}

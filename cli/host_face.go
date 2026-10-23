package cli

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// HostBridge is the host conversion face a host process drives the runtime
// through: the narrow seam by which the WeChat gateway, and any other process that
// embeds the runtime, hands inbound messages in and reports what became of the
// deliveries the runtime asked it to make.
//
// The bridge only converts. It holds no runtime, no state and no event loop of its
// own, so it never becomes a second runtime beside the one Assemble returned. Every
// entry below addresses its target with the same explicit event.Address the runtime
// uses internally, so a host names its (agent, session) and never leaves the runtime
// to guess the most recent one.
// The redline list (4 rules):
// MUST convert only: hold no runtime, no state and no event loop of its own.
// MUST address every entry by the explicit event.Address it is handed.
// MUST refuse a decision that does not match the approval identity and the argument digest the hold was issued under.
// MUST NOT substitute a most-recent session for an address, nor apply a decision to another call.
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
type HostBridge interface {
	// AdmitInbound converts one inbound host message into an explicit-address
	// delivery at addr and returns the invocation that owns the admitted input.
	//
	// The address is required: the message is admitted to the mailbox of the named
	// session, and no default or most-recent session is substituted for it.
	AdmitInbound(ctx context.Context, addr event.Address, message model.Message) (event.InvocationID, error)

	// ConsumeReceipt consumes the outcome a host reports for one outbound delivery
	// the runtime asked it to make.
	//
	// A delivery stays an open obligation until its receipt says the target accepted
	// it, so a failure or a rejection is recorded as a fact at addr rather than
	// dropped.
	ConsumeReceipt(ctx context.Context, addr event.Address, receipt DeliveryReceipt) error

	// ResolveApproval injects a host's decision for one held call at addr.
	//
	// - A held call is a registered waiting task, never a denied one, so the injected decision carries the explicit address the held call was admitted under.
	// - The decision is bound to the hold it resolves: decision.ApprovalID must name the approval identity tagent issued when that call was parked, and decision.ExpectedDigest must match the argument digest computed at that moment, beside the call scope the hold was issued in.
	// - A decision that does not match is refused by name — ErrApprovalIdentityMismatch or ErrApprovalDigestMismatch — and is never silently applied to another call or to the same call over changed arguments.
	// - A repeated response under the same approval identity is idempotent: the first response takes effect and the repeat reports that first outcome, so the held call is resolved once and executed no more than once.
	ResolveApproval(ctx context.Context, addr event.Address, decision ApprovalDecision) error
}

// DeliveryID is the stable identity of one outbound delivery: minted when the
// runtime asks its host to make that delivery, and kept stable across every
// bounded read that serves the delivery and every receipt that resolves it. A
// receipt is reconciled against this identity — the delivery it names is the
// delivery that was read — so what became of a delivery is never guessed from
// the most recent message.
type DeliveryID string

// PendingDelivery is one outbound delivery the runtime asked its host to make
// and that no receipt has resolved yet: the stable delivery identity the
// receipt is reconciled against, the runtime instance the delivery is routed
// to, and the native message to deliver.
type PendingDelivery struct {
	// DeliveryID is the stable identity this delivery is reconciled under: the
	// receipt reporting what became of it names the same identity (see
	// DeliveryReceipt).
	DeliveryID DeliveryID
	// TargetInstance is the runtime instance the delivery is routed to, frozen
	// beside the delivery, so a rebuilt or handed-off instance never re-routes
	// a delivery already asked for.
	TargetInstance event.InstanceID
	// Message is the native message the host delivers, in the native model form
	// the runtime produced it in.
	Message model.Message
}

// PendingDeliveryRequest asks for one bounded read of the outbound deliveries
// waiting for their receipt: the maximum number of entries to hand back and the
// delivery to resume after.
//
// The bound is in entries and always finite: a zero Max is served as
// DefaultPendingDeliveries and never as an unbounded read, so the pending
// deliveries are consumed in bounded reads and are never buffered into one
// unbounded result.
type PendingDeliveryRequest struct {
	// Max bounds the entries one read returns. Zero means
	// DefaultPendingDeliveries.
	Max int
	// After is the continuation form: the DeliveryID of the last entry of the
	// previous result, so the pending deliveries are consumed delivery by
	// delivery. The zero identity starts at the first pending delivery.
	After DeliveryID
}

// PendingDeliveryResult is one bounded read of the outbound deliveries still
// waiting for their receipt: at most the requested number of entries, the
// delivery the next read resumes after, and whether the end of the pending
// queue was reached. Next is the zero identity when EOF is set and carries no
// resumption point then.
type PendingDeliveryResult struct {
	// Deliveries holds at most the requested number of pending deliveries.
	Deliveries []PendingDelivery
	// Next is the delivery the following read resumes after.
	Next DeliveryID
	// EOF reports that this read reached the last pending delivery.
	EOF bool
}

// DefaultPendingDeliveries is the finite bound served when a
// PendingDeliveryRequest carries a zero Max, so a read that states no limit
// stays bounded instead of draining the pending queue whole.
const DefaultPendingDeliveries = 64

// ErrPendingDeliveryLimit reports a pending-delivery read request carrying a
// negative bound, so no bounded read could answer it.
var ErrPendingDeliveryLimit = errors.New("tagent: pending-delivery read request carries a negative bound")

// DeliveryReceipt is the outcome a host reports for one outbound delivery: the
// stable delivery identity of the message the runtime asked it to send, whether
// the target accepted that message, and the reason it did not.
type DeliveryReceipt struct {
	// DeliveryID names the stable identity of the outbound delivery this
	// receipt resolves, echoed back by the host. The receipt is reconciled
	// against that identity — the delivery the read face served under it — and
	// never against a guess at the most recent message.
	DeliveryID DeliveryID
	// Accepted reports whether the target accepted the message; the delivery
	// obligation clears only when it is true.
	Accepted bool
	// Reason names why the target did not accept the message; it is empty when
	// Accepted is true.
	Reason string
}

// ApprovalDecision is a host's resolution of one held call: the approval
// identity and argument digest the hold was issued under, the call scope the
// resolution is bound to, and whether that call may proceed.
type ApprovalDecision struct {
	// ApprovalID is the approval ticket the hold was issued under, echoed back by
	// the host. It is the identity the held call is resolved against, never a
	// native CallID that is not globally unique.
	ApprovalID string
	// ExpectedDigest is the digest of the call arguments the hold was issued
	// over. A decision carrying another digest names other arguments than the
	// ones that were held and is refused beside ErrApprovalDigestMismatch.
	ExpectedDigest string
	// InstanceID is the runtime instance the held call was admitted in.
	InstanceID event.InstanceID
	// InvocationID is the responsibility range the held call belongs to.
	InvocationID event.InvocationID
	// DecisionID is the model decision the held call came from.
	DecisionID event.DecisionID
	// CallID is the native provider call identifier of the held call, kept
	// byte-identical to the identifier the model produced.
	CallID string
	// Granted reports whether the held call proceeds; false denies it.
	Granted bool
	// Reason is the host's stated ground for the decision, kept with the record.
	Reason string
}

// ErrApprovalIdentityMismatch and ErrApprovalDigestMismatch are the named refusals beside which an
// approval response is turned away. The authoritative hold record refuses a response whose approval
// identity or argument digest does not match the hold it was issued under; the bridge reports the same
// named errors a host reads its contract against.
var (
	// ErrApprovalIdentityMismatch reports an approval response that does not name
	// the approval identity the hold was issued under.
	ErrApprovalIdentityMismatch = task.ErrApprovalIdentityMismatch
	// ErrApprovalDigestMismatch reports an approval response whose argument
	// digest names other arguments than the ones that were held.
	ErrApprovalDigestMismatch = task.ErrApprovalDigestMismatch
)

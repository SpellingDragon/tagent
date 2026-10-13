package tagent

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// HostBridge is the module-root conversion face a host process drives the runtime
// through: the narrow seam by which the WeChat gateway, and any other process that
// embeds the runtime, hands inbound messages in and reports what became of the
// deliveries the runtime asked it to make.
//
// The bridge only converts. It holds no runtime, no state and no event loop of its
// own, so it never becomes a second runtime beside the one Build assembled. Every
// entry below addresses its target with the same explicit event.Address the runtime
// uses internally, so a host names its (agent, session) and never leaves the runtime
// to guess the most recent one.
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
	// A held call is a registered waiting task, never a denied one, so the injected
	// decision carries the explicit address the held call was admitted under.
	ResolveApproval(ctx context.Context, addr event.Address, decision ApprovalDecision) error
}

// DeliveryReceipt is the outcome a host reports for one outbound delivery: the
// message the runtime asked it to send, whether the target accepted that message,
// and the reason it did not.
type DeliveryReceipt struct {
	// MessageID identifies the outbound message this receipt resolves.
	MessageID string
	// Accepted reports whether the target accepted the message; the delivery
	// obligation clears only when it is true.
	Accepted bool
	// Reason names why the target did not accept the message; it is empty when
	// Accepted is true.
	Reason string
}

// ApprovalDecision is a host's resolution of one held call: the call it resolves
// and whether that call may proceed.
type ApprovalDecision struct {
	// CallID is the native provider call identifier of the held call, kept
	// byte-identical to the identifier the model produced.
	CallID string
	// Granted reports whether the held call proceeds; false denies it.
	Granted bool
	// Reason is the host's stated ground for the decision, kept with the record.
	Reason string
}

// Skeleton compile-time assertions for the phase 1 host bridge declarations above.
//
// The phase 2 bridge implementation does not exist yet, so there is no
// `var _ HostBridge = (*Runtime)(nil)` to write. The assertions pin what the skeleton
// proves on its own: the bridge stays an interface type, and every declared entry
// keeps its exact signature.
var (
	_ HostBridge = (HostBridge)(nil)

	_ func(HostBridge, context.Context, event.Address, model.Message) (event.InvocationID, error) = HostBridge.AdmitInbound
	_ func(HostBridge, context.Context, event.Address, DeliveryReceipt) error                     = HostBridge.ConsumeReceipt
	_ func(HostBridge, context.Context, event.Address, ApprovalDecision) error                    = HostBridge.ResolveApproval
)

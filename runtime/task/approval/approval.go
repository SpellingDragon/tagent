package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/SpellingDragon/tagent/api/plugin"
)

// ApprovalStatus is the state one approval request is in. The four states carry
// one meaning each: pending awaits an external ruling and the governed call
// does not run meanwhile, approved lets that very call proceed, denied is a
// terminal refusal of it, and expired means the request outlived its validity
// without a ruling and is treated as not approved — a later response never
// makes an expired request runnable again.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
type ApprovalStatus string

const (
	// ApprovalPending is waiting for an external approver's ruling; the
	// governed operation does not execute meanwhile.
	ApprovalPending ApprovalStatus = "pending"

	// ApprovalApproved lets the held call proceed.
	ApprovalApproved ApprovalStatus = "approved"

	// ApprovalDenied is a terminal refusal: the call ends and returns as a
	// refused result.
	ApprovalDenied ApprovalStatus = "denied"

	// ApprovalExpired is a request that outlived its validity without a
	// ruling: it is treated as not approved, and nothing proceeds on the old
	// request afterwards.
	ApprovalExpired ApprovalStatus = "expired"
)

// ErrApprovalNotFound reports a ruling that names no registered approval
// request, so it is refused by name instead of being applied to a request that
// does not exist.
var ErrApprovalNotFound = errors.New("approval: no approval request names that identity")

// ErrApprovalExpired reports a ruling that arrived after the request expired:
// an expired request is treated as not approved, and a late ruling never makes
// it runnable again.
var ErrApprovalExpired = errors.New("approval: the approval request expired before it was decided")

// ErrApprovalRulingUnknown reports a ruling whose status names no known
// decision, so it is refused closed instead of being recorded as a guess.
var ErrApprovalRulingUnknown = errors.New("approval: the approval response names no known ruling")

// ErrApprovalIdentityMissing reports a request registration that carries no
// approval identity. The identity is the one tagent issued when the call was
// parked, so it is required rather than replaced by a locally minted one: the
// host response names that one identity across the hold and the request.
var ErrApprovalIdentityMissing = errors.New("approval: the approval request carries no approval identity")

// ApprovalSubject is the identity block of one approval request: the tool the
// held call wants to run, and the two names the request is resolved under.
type ApprovalSubject struct {
	// ID is the approval identity tagent issued when the call was parked. The
	// host response names this identity, so the hold and the request resolve
	// under one name.
	ID string `json:"id"`
	// ToolName is the tool the held call wants to run.
	ToolName string `json:"tool"`
	// ArgsDigest is the digest of the exact argument bytes the call carries,
	// computed by ArgsDigest. Every ruling is bound to it.
	ArgsDigest string `json:"args_digest"`
}

// ApprovalEvidence is the block an approver reads to decide one held call: the
// bounded argument preview, the grade and rule the ruling was rendered at, and
// the reason the call was held.
type ApprovalEvidence struct {
	// ArgsPreview is the bounded preview of the arguments an approver reads.
	ArgsPreview string `json:"args_preview"`
	// RiskLevel is the risk grade the pre-execution ruling was rendered at.
	RiskLevel plugin.RiskLevel `json:"risk"`
	// RuleID names the rule the ruling came from, if one matched.
	RuleID string `json:"rule_id,omitempty"`
	// Reason is the readable reason the call was held.
	Reason string `json:"reason"`
}

// ApprovalState is the block that reports where one request stands: when it was
// registered, when its validity runs out, and the ruling that took effect.
type ApprovalState struct {
	// CreatedMs is the registration time of the request, in Unix milliseconds.
	CreatedMs int64 `json:"created_ms"`
	// ExpiresMs is the moment the request loses its validity, in Unix
	// milliseconds. An undecided request past this moment reads as expired.
	ExpiresMs int64 `json:"expires_ms"`
	// Status is the state the request is in.
	Status ApprovalStatus `json:"status"`
	// DecidedBy names the approver whose ruling took effect.
	DecidedBy string `json:"decided_by,omitempty"`
}

// ApprovalRequest is one request for a human ruling over one held call: one
// request per file, readable and writable by the external approver. Every
// ruling is bound to ArgsDigest — the digest of the exact argument bytes that
// were held — so an approval of other arguments is refused instead of running
// a changed call.
//
// The request is three blocks — who was held, what the approver reads, and
// where the request stands — and its file keeps them as one flat document, so
// an approver answers a request without knowing the blocks.
type ApprovalRequest struct {
	ApprovalSubject
	ApprovalEvidence
	ApprovalState
}

// ApprovalChannel is the outbound delivery face of the approval channel: one
// approval request is offered to an external approver over it. The channel is a
// gate, never a wall: a delivery that fails is recorded as a visible gap and
// never blocks the gate, and the request stays answerable through the other
// entries (CLI, WeChat) or through the request file itself.
//
// The redline list (3 rules):
// MUST deliver one request per call, keyed by the approval identity the request carries.
// MUST NOT read a failed delivery as a ruling: the gate keeps waiting.
// MUST NOT rewrite the request it was handed.
type ApprovalChannel interface {
	// Deliver offers one approval request to an external approver.
	Deliver(ctx context.Context, req ApprovalRequest) error
}

// DeliveryGap is one visible channel failure: an approval request the channels
// could not be handed. The gap is recorded and exposed instead of being
// swallowed, and the gate keeps waiting rather than treating the failure as an
// approval or as a crash.
type DeliveryGap struct {
	// Request is the approval identity the failed delivery was for.
	Request string
	// Err is the delivery failure the channel reported.
	Err error
}

// ArgsDigest computes the argument digest of one call: the lowercase hex of the
// SHA-256 of the exact argument bytes the call carries. The digest binds an
// approval to the arguments that were held, so a ruling over other arguments is
// refused instead of approving a changed call.
func ArgsDigest(jsonArgs []byte) string {
	sum := sha256.Sum256(jsonArgs)
	return hex.EncodeToString(sum[:])
}

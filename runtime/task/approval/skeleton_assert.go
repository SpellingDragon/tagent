package approval

import "context"

// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
//
// This file holds compile-time assertions only: the channel stays an interface
// type, the request keeps its three blocks, the state words stay constants of
// their declared type, and the digest binding keeps its exact shape.
var (
	_ func(ApprovalChannel, context.Context, ApprovalRequest) error = ApprovalChannel.Deliver
)

// The channel stays a declared interface whose delivery face is its only
// method: a concrete type or a widened face stops this assertion.
var _ ApprovalChannel = (ApprovalChannel)(nil)

// The 04.5 declarations keep the three blocks one approval request is read as.
var (
	_ = ApprovalSubject{
		ID:         "",
		ToolName:   "",
		ArgsDigest: "",
	}

	_ = ApprovalEvidence{
		ArgsPreview: "",
		RiskLevel:   "",
		RuleID:      "",
		Reason:      "",
	}

	_ = ApprovalState{
		CreatedMs: 0,
		ExpiresMs: 0,
		Status:    "",
		DecidedBy: "",
	}

	_ = ApprovalRequest{
		ApprovalSubject:  ApprovalSubject{},
		ApprovalEvidence: ApprovalEvidence{},
		ApprovalState:    ApprovalState{},
	}

	_ = DeliveryGap{
		Request: "",
		Err:     nil,
	}
)

// Approval states must stay compile-time constants of their declared type: a
// variable or a bare string stops this block from compiling. The state words
// are the ones the channel contract fixes.
const (
	_ ApprovalStatus = ApprovalPending
	_ ApprovalStatus = ApprovalApproved
	_ ApprovalStatus = ApprovalDenied
	_ ApprovalStatus = ApprovalExpired
)

// The digest binding stays one function from the exact argument bytes to the
// lowercase hex digest every ruling is matched against.
var (
	_ func([]byte) string = ArgsDigest

	_ error = ErrApprovalNotFound

	_ error = ErrApprovalExpired

	_ error = ErrApprovalRulingUnknown

	_ error = ErrApprovalIdentityMissing
)

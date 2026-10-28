// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Compile-time assertions for the host shell: the pending-delivery read face
// and the receipt that reconciles against it keep their documented shapes, and
// the constructor beside the two entries a host drives the shell through keep
// theirs.
package cli

import (
	"context"
	"io"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
)

// The pending-delivery read face and the receipt that reconciles against it
// keep their documented shapes: the stable delivery identity, the target
// instance and the native message beside the bounded-read limit.
var (
	_ = DeliveryID("")

	_ = PendingDelivery{
		DeliveryID:     "",
		TargetInstance: "",
		Message:        model.Message{},
	}

	_ = PendingDeliveryRequest{
		Max:   0,
		After: "",
	}

	_ = PendingDeliveryResult{
		Deliveries: nil,
		Next:       "",
		EOF:        false,
	}

	_ = DeliveryReceipt{
		DeliveryID: "",
		Accepted:   false,
		Reason:     "",
	}

	_ = DefaultPendingDeliveries
	_ = ErrPendingDeliveryLimit
)

// The host HTTP face refuses a request with no credential behind it: the shell
// opens the diagnostic endpoints only beside a configured request credential
// and answers every unauthenticated request by name.
var (
	_ func(*assembly.Run, query.AuthorizationScope, ...CLIOption) *CLI = NewCLI

	_ func(string) CLIOption = WithRequestCredential

	_ func(*CLI, context.Context, []string, io.Writer, io.Writer) int = (*CLI).Main
)

// The host bridge declarations the shell carries keep the entry signatures
// their contract fixes, and no concrete type is pinned as the one bridge: the
// face stays an interface. The approval decision keeps its field set, which is
// the identity the hold was issued under beside the call scope it binds.
var (
	_ HostBridge = (HostBridge)(nil)

	_ func(HostBridge, context.Context, event.Address, model.Message) (event.InvocationID, error) = HostBridge.AdmitInbound
	_ func(HostBridge, context.Context, event.Address, DeliveryReceipt) error                     = HostBridge.ConsumeReceipt
	_ func(HostBridge, context.Context, event.Address, ApprovalDecision) error                    = HostBridge.ResolveApproval

	_ = ApprovalDecision{
		ApprovalID:     "",
		ExpectedDigest: "",
		InstanceID:     "",
		InvocationID:   "",
		DecisionID:     "",
		CallID:         "",
		Granted:        false,
		Reason:         "",
	}
)

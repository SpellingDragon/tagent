package pluginexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/plugin"
)

// stdoutChannel is the canonical approval channel: it offers one approval request to an
// external approver and reports a delivery it could not make.
type stdoutChannel struct{}

// Deliver offers one approval request to the external approver.
func (stdoutChannel) Deliver(_ context.Context, req plugin.ApprovalRequest) error {
	if req.ApprovalSubject.ID == "" {
		return errors.New("approval: the request names no held call")
	}
	return nil
}

var _ plugin.ApprovalChannel = stdoutChannel{}

// ExampleApprovalChannel delivers one approval request: a delivered request stays
// answerable through the approver's other entries, and a failed delivery is a visible gap
// that never blocks the gate.
//
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
func ExampleApprovalChannel() {
	face := stdoutChannel{}
	err := face.Deliver(context.Background(), plugin.ApprovalRequest{
		ApprovalSubject: plugin.ApprovalSubject{ID: "approval-1", ToolName: "write"},
	})
	_ = err
}

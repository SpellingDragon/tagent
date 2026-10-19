package cliexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	cli "github.com/SpellingDragon/tagent/cli"
)

// convertingBridge is the canonical host bridge: it converts the host's inbound messages,
// delivery receipts and approval decisions into the runtime's explicit-address entries and
// holds nothing of its own.
type convertingBridge struct {
	admitted []event.InvocationID
	receipts []cli.DeliveryReceipt
	decided  []cli.ApprovalDecision
}

// AdmitInbound converts one inbound host message into an explicit-address delivery.
func (b *convertingBridge) AdmitInbound(_ context.Context, addr event.Address, _ model.Message) (event.InvocationID, error) {
	if !addr.IsComplete() {
		return "", errors.New("host: the inbound message names an incomplete address")
	}
	id := event.InvocationID("inv-" + string(addr.SessionID))
	b.admitted = append(b.admitted, id)
	return id, nil
}

// ConsumeReceipt consumes the outcome a host reports for one outbound delivery.
func (b *convertingBridge) ConsumeReceipt(_ context.Context, addr event.Address, receipt cli.DeliveryReceipt) error {
	if !addr.IsComplete() {
		return errors.New("host: the receipt names an incomplete address")
	}
	b.receipts = append(b.receipts, receipt)
	return nil
}

// ResolveApproval injects a host's decision for one held call at addr.
func (b *convertingBridge) ResolveApproval(_ context.Context, addr event.Address, decision cli.ApprovalDecision) error {
	if decision.ApprovalID == "" || decision.ExpectedDigest == "" {
		return cli.ErrApprovalIdentityMismatch
	}
	if !addr.IsComplete() {
		return errors.New("host: the decision names an incomplete address")
	}
	b.decided = append(b.decided, decision)
	return nil
}

var _ cli.HostBridge = (*convertingBridge)(nil)

// ExampleHostBridge hands one inbound message in, reports the receipt of one outbound
// delivery and resolves one held call; a decision that names no approval identity is
// refused by name.
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#inbound-intake
func ExampleHostBridge() {
	ctx := context.Background()
	bridge := &convertingBridge{}
	addr := event.Address{AgentID: "assistant", SessionID: "session-1"}

	invocationID, err := bridge.AdmitInbound(ctx, addr, model.Message{Role: model.RoleUser, Content: "hello"})
	if err != nil {
		return
	}
	if err := bridge.ConsumeReceipt(ctx, addr, cli.DeliveryReceipt{
		DeliveryID: "delivery-1",
		Accepted:   true,
	}); err != nil {
		return
	}
	if err := bridge.ResolveApproval(ctx, addr, cli.ApprovalDecision{
		ApprovalID:     "approval-1",
		ExpectedDigest: "digest-1",
		Granted:        true,
	}); err != nil {
		return
	}
	if err := bridge.ResolveApproval(ctx, addr, cli.ApprovalDecision{ExpectedDigest: "digest-1"}); !errors.Is(err, cli.ErrApprovalIdentityMismatch) {
		return
	}
	_ = invocationID
}

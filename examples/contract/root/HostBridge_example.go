package rootexample

import (
	"context"
	"errors"

	tagent "github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// convertingBridge is the canonical host bridge: it converts the host's inbound messages,
// delivery receipts and approval decisions into the runtime's explicit-address entries and
// holds nothing of its own.
type convertingBridge struct {
	admitted []event.InvocationID
	receipts []tagent.DeliveryReceipt
	decided  []tagent.ApprovalDecision
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
func (b *convertingBridge) ConsumeReceipt(_ context.Context, addr event.Address, receipt tagent.DeliveryReceipt) error {
	if !addr.IsComplete() {
		return errors.New("host: the receipt names an incomplete address")
	}
	b.receipts = append(b.receipts, receipt)
	return nil
}

// ResolveApproval injects a host's decision for one held call at addr.
func (b *convertingBridge) ResolveApproval(_ context.Context, addr event.Address, decision tagent.ApprovalDecision) error {
	if decision.ApprovalID == "" || decision.ExpectedDigest == "" {
		return tagent.ErrApprovalIdentityMismatch
	}
	if !addr.IsComplete() {
		return errors.New("host: the decision names an incomplete address")
	}
	b.decided = append(b.decided, decision)
	return nil
}

var _ tagent.HostBridge = (*convertingBridge)(nil)

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
	if err := bridge.ConsumeReceipt(ctx, addr, tagent.DeliveryReceipt{
		DeliveryID: "delivery-1",
		Accepted:   true,
	}); err != nil {
		return
	}
	if err := bridge.ResolveApproval(ctx, addr, tagent.ApprovalDecision{
		ApprovalID:     "approval-1",
		ExpectedDigest: "digest-1",
		Granted:        true,
	}); err != nil {
		return
	}
	if err := bridge.ResolveApproval(ctx, addr, tagent.ApprovalDecision{ExpectedDigest: "digest-1"}); !errors.Is(err, tagent.ErrApprovalIdentityMismatch) {
		return
	}
	_ = invocationID
}

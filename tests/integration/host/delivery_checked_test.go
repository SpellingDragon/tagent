// The checked-state checks of the delivery face: a state commit the durable tier
// refused is answered as a named error, and the review table publishes no
// decision whose record did not land.
package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// TestDeliveryStateCommitFailure pins the refusal of one held body beside a state commit the tier refused.
//   - positive: the commit under a cancelled context answers the refusal by name
//   - negative: the hold stays in the review table, no refusal fact lands, and the same decision resolves beside a live context
//   - consumer: the review entry point answers the caller, instead of a log line beside a success
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveryStateCommitFailure(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(heldReviewBody) }
	sender := &deliveryRecorder{}
	ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
	host := bootDeliveryReviewHost(t, provider, sender, ruler)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer closeDeliveryReviewHost(t, host, ctx)

	addr := event.Address{AgentID: "user", SessionID: "session-delivery-state-commit"}
	if _, err := host.Invoke(ctx, tagent.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("invoke the held turn: %v", err)
	}
	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("held deliveries = %+v, want the one body the review held", held)
	}

	refused, cancelCommit := context.WithCancel(ctx)
	cancelCommit()
	err := host.RefuseDelivery(refused, held[0].ID)
	if err == nil {
		t.Fatal("the refusal beside a cancelled state commit reported no error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the refusal error = %v, want the cancelled state commit as its cause", err)
	}
	if left := host.HeldDeliveries(); len(left) != 1 || left[0].ID != held[0].ID {
		t.Fatalf("held deliveries beside the refused commit = %+v, want the hold unchanged", left)
	}
	if fact := deliveryRefusalFactOf(host, addr); fact != nil {
		t.Fatalf("the refusal beside the refused commit left the audit fact %+v, want none", fact)
	}

	if err := host.RefuseDelivery(ctx, held[0].ID); err != nil {
		t.Fatalf("refuse the held delivery beside a live context: %v", err)
	}
	if left := host.HeldDeliveries(); len(left) != 0 {
		t.Fatalf("held deliveries after the refusal = %+v, want the consumed hold", left)
	}
	if fact := deliveryRefusalFactOf(host, addr); fact == nil {
		t.Fatal("the refusal beside a live context left no audit fact")
	}
}

// TestApproveDeliveryReceiptFailure pins the at-least-once boundary of one approval: the body lands and its receipt does not.
//   - positive: the receipt failure answers the landed delivery by name
//   - negative: the obligation stays owed, and the tolerated single extra copy is the only redelivery
//   - consumer: an approval driven through the host face, not a sender called directly
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestApproveDeliveryReceiptFailure(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(heldReviewBody) }
	sender := &deliveryRecorder{}
	ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
	host := bootDeliveryReviewHost(t, provider, sender, ruler)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer closeDeliveryReviewHost(t, host, ctx)

	addr := event.Address{AgentID: "user", SessionID: "session-delivery-receipt"}
	if _, err := host.Invoke(ctx, tagent.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("invoke the held turn: %v", err)
	}
	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("held deliveries = %+v, want the one body the review held", held)
	}

	approved, cancelReceipt := context.WithCancel(ctx)
	cancelReceipt()
	err := host.ApproveDelivery(approved, held[0].ID)
	if err == nil {
		t.Fatal("the approval beside a refused receipt reported no error")
	}
	if !strings.Contains(err.Error(), "landed") || !errors.Is(err, context.Canceled) {
		t.Fatalf("the approval error = %v, want the landed delivery beside its refused receipt", err)
	}
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != heldReviewBody {
		t.Fatalf("delivered bodies beside the refused receipt = %q, want the one body the sender accepted", bodies)
	}
	if left := host.HeldDeliveries(); len(left) != 1 || left[0].ID != held[0].ID {
		t.Fatalf("held deliveries beside the refused receipt = %+v, want the obligation still owed", left)
	}

	if err := host.ApproveDelivery(ctx, held[0].ID); err != nil {
		t.Fatalf("approve the owed delivery beside a live context: %v", err)
	}
	if bodies := sender.bodies(); len(bodies) != 2 || bodies[1] != heldReviewBody {
		t.Fatalf("delivered bodies after the owed approval = %q, want the tolerated single extra copy", bodies)
	}
}

// deliveryRefusalFactOf reports the refusal audit fact recorded at one address,
// and nil beside an address that holds none.
func deliveryRefusalFactOf(host *tagent.Host, addr event.Address) *event.Event {
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyEventSummary] == "delivery refused" {
			held := fact
			return &held
		}
	}
	return nil
}

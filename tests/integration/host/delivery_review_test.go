// The delivery review checks of the host: a body the review plugin holds waits
// before the user channel, an approval re-delivers it exactly once, and a
// refusal discards it with a payload-free audit fact.
package host

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// heldReviewBody is the assistant body the review checks see the provider
// answer and the host rule before delivery.
const heldReviewBody = "the body the delivery review holds"

// TestDeliveryReviewHold pins the short loop of the delivery review:
// - a hold ruling keeps the turn body off the user channel and records one held entry with content, lineage and target
// - an approval re-delivers that body exactly once, and a repeated approval names no held delivery and sends nothing
// - a refusal delivers nothing and leaves one feedback fact without a model payload
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestDeliveryReviewHold(t *testing.T) {
	t.Run("hold approves exactly one delivery", func(t *testing.T) {
		provider := newScriptedProvider(t)
		provider.answer = func(int) string { return scriptedAnswerOf(heldReviewBody) }
		sender := &deliveryRecorder{}
		ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
		host := bootDeliveryReviewHost(t, provider, sender, ruler)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		defer closeDeliveryReviewHost(t, host, ctx)

		addr := event.Address{AgentID: "user", SessionID: "session-delivery-review-approve"}
		turn, err := host.Invoke(ctx, tagent.InvokeRequest{
			Address:       addr,
			TriggerSource: "user",
			Input:         []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the held turn: %v", err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the completed turn whose body the review rules", turn.Terminal)
		}
		if bodies := sender.bodies(); len(bodies) != 0 {
			t.Fatalf("delivered bodies = %q, want none while the review holds the body", bodies)
		}

		want := plugin.Delivery{Content: heldReviewBody, Lineage: "user", Target: addr}
		reviewed := ruler.seenDeliveries()
		if len(reviewed) != 1 || reviewed[0] != want {
			t.Fatalf("reviewed deliveries = %+v, want the one turn body %+v", reviewed, want)
		}
		held := host.HeldDeliveries()
		if len(held) != 1 {
			t.Fatalf("held deliveries = %+v, want the one body the review held", held)
		}
		if held[0].ID == "" {
			t.Fatal("the held delivery carries no identity to approve")
		}
		if held[0].Delivery != want {
			t.Fatalf("held delivery = %+v, want content, lineage and target %+v", held[0].Delivery, want)
		}

		if err := host.ApproveDelivery(ctx, held[0].ID); err != nil {
			t.Fatalf("approve the held delivery: %v", err)
		}
		if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != heldReviewBody {
			t.Fatalf("delivered bodies after approval = %q, want the held body exactly once", bodies)
		}
		if left := host.HeldDeliveries(); len(left) != 0 {
			t.Fatalf("held deliveries after approval = %+v, want the consumed hold", left)
		}
		if again := ruler.seenDeliveries(); len(again) != 1 {
			t.Fatalf("reviewed deliveries after approval = %+v, want the ruling the hold already carries", again)
		}
		if err := host.ApproveDelivery(ctx, held[0].ID); !errors.Is(err, tagent.ErrDeliveryNotFound) {
			t.Fatalf("the repeated approval = %v, want ErrDeliveryNotFound", err)
		}
		if bodies := sender.bodies(); len(bodies) != 1 {
			t.Fatalf("delivered bodies after the repeated approval = %q, want no second copy", bodies)
		}
	})

	t.Run("refusal delivers nothing and records an audit fact", func(t *testing.T) {
		provider := newScriptedProvider(t)
		provider.answer = func(int) string { return scriptedAnswerOf(heldReviewBody) }
		sender := &deliveryRecorder{}
		ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
		host := bootDeliveryReviewHost(t, provider, sender, ruler)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		defer closeDeliveryReviewHost(t, host, ctx)

		addr := event.Address{AgentID: "user", SessionID: "session-delivery-review-refuse"}
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
		if err := host.RefuseDelivery(ctx, held[0].ID); err != nil {
			t.Fatalf("refuse the held delivery: %v", err)
		}
		if bodies := sender.bodies(); len(bodies) != 0 {
			t.Fatalf("delivered bodies after refusal = %q, want none", bodies)
		}
		if left := host.HeldDeliveries(); len(left) != 0 {
			t.Fatalf("held deliveries after refusal = %+v, want the consumed hold", left)
		}

		fact := deliveryRefusalFact(t, host, addr)
		if fact.Message.Content != "" {
			t.Fatalf("the refusal fact carries the body %q, want no model payload", fact.Message.Content)
		}
		if got := fact.Meta[event.MetaKeyTriggerSource]; got != "host" {
			t.Fatalf("refusal trigger source = %q, want the host audit stamp", got)
		}
		if got := fact.Meta[event.MetaPrefix+"delivery_id"]; got != held[0].ID {
			t.Fatalf("refusal delivery identity = %q, want %q", got, held[0].ID)
		}
		if got := fact.Meta[event.MetaPrefix+"lineage"]; got != "user" {
			t.Fatalf("refusal lineage = %q, want the user stamp", got)
		}
		if got := fact.Meta[event.MetaPrefix+"content_bytes"]; got != strconv.Itoa(len(heldReviewBody)) {
			t.Fatalf("refusal content bytes = %q, want %d", got, len(heldReviewBody))
		}
		if err := host.RefuseDelivery(ctx, held[0].ID); !errors.Is(err, tagent.ErrDeliveryNotFound) {
			t.Fatalf("the repeated refusal = %v, want ErrDeliveryNotFound", err)
		}
	})
}

// scriptedDeliveryRuler is the review face of the delivery checks: it answers
// the scripted disposition and records every semantic it ruled.
type scriptedDeliveryRuler struct {
	mu          sync.Mutex
	disposition plugin.Disposition
	seen        []plugin.Delivery
}

// Rule records one delivery and answers the scripted disposition.
func (r *scriptedDeliveryRuler) Rule(_ context.Context, delivery plugin.Delivery) (plugin.Disposition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, delivery)
	return r.disposition, nil
}

// seenDeliveries reports the ruled deliveries in ruling order, as a copy the
// caller owns.
func (r *scriptedDeliveryRuler) seenDeliveries() []plugin.Delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]plugin.Delivery(nil), r.seen...)
}

// bootDeliveryReviewHost boots the bare entry document with the review face and
// the controlled sender the delivery review checks drive.
func bootDeliveryReviewHost(t *testing.T, provider *scriptedProvider, sender tagent.MessageSender, ruler plugin.DeliveryRuler) *tagent.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	host, err := tagent.Boot(configPath, tagent.WithSender(sender), tagent.WithDeliveryRuler(ruler))
	if err != nil {
		t.Fatalf("boot the delivery review host: %v", err)
	}
	return host
}

// closeDeliveryReviewHost closes the host beside the check and reports a
// refused close without hiding the check's own failure.
func closeDeliveryReviewHost(t *testing.T, host *tagent.Host, ctx context.Context) {
	t.Helper()
	if err := host.Close(ctx); err != nil {
		t.Errorf("close the delivery review host: %v", err)
	}
}

// deliveryRefusalFact reports the refusal feedback fact recorded at one
// address, failing beside the committed facts when the audit fact is absent.
func deliveryRefusalFact(t *testing.T, host *tagent.Host, addr event.Address) event.Event {
	t.Helper()
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyEventSummary] == "delivery refused" {
			return fact
		}
	}
	t.Fatalf("no delivery refusal fact was committed at %+v", addr)
	return event.Event{}
}

// The delivery recovery checks of the host: a body the review held and a body
// the channel refused stay owed across a restart under the identity they were
// recorded with, and each decision resolves one obligation exactly once.
package host

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
)

// recoveryHeldBody is the assistant body the review holds before the restart.
const recoveryHeldBody = "the body the delivery recovery holds"

// recoveryRetryBody is the assistant body the channel refuses before the restart.
const recoveryRetryBody = "the body the delivery recovery redelivers"

// errRecoveryChannelRefusal is the refusal the controlled sender answers for the
// bodies it is scripted to refuse.
var errRecoveryChannelRefusal = errors.New("tagent test: the channel refuses the body")

// TestDeliveryRecovery pins the delivery obligations of the host across a restart:
// - a held delivery waits through the restart and one approval delivers its body exactly once
// - a body the channel refused owes a redelivery that waits through the restart
// - a refusal delivers nothing, records the audit fact and stays refused beside a further restart
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestDeliveryRecovery(t *testing.T) {
	t.Run("a held delivery survives the restart", testDeliveryRecoveryHeld)
	t.Run("a failed send owes a redelivery across the restart", testDeliveryRecoveryRetry)
	t.Run("a refused delivery stays refused and audited", testDeliveryRecoveryRefused)
}

// testDeliveryRecoveryHeld boots the review host, holds one turn body, restarts
// the host over the same storage and approves the restored body.
func testDeliveryRecoveryHeld(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(recoveryHeldBody) }
	sender := &recoverySender{}
	ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr := event.Address{AgentID: "user", SessionID: "session-delivery-recovery-held"}
	host := bootDeliveryRecoveryHost(t, configPath, sender, ruler)
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
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
	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("held deliveries = %+v, want the one body the review held", held)
	}
	want := plugin.Delivery{Content: recoveryHeldBody, Lineage: "user", Target: addr}
	if held[0].ID == "" || held[0].Delivery != want {
		t.Fatalf("held delivery = %+v, want the recorded identity beside %+v", held[0], want)
	}
	if bodies := sender.bodies(); len(bodies) != 0 {
		t.Fatalf("delivered bodies = %q, want none while the review holds the body", bodies)
	}
	closeDeliveryReviewHost(t, host, ctx)

	host = bootDeliveryRecoveryHost(t, configPath, sender, ruler)
	defer closeDeliveryReviewHost(t, host, ctx)
	restored := host.HeldDeliveries()
	if len(restored) != 1 {
		t.Fatalf("held deliveries after the restart = %+v, want the one body the first boot held", restored)
	}
	if restored[0].ID != held[0].ID {
		t.Fatalf("restored delivery identity = %q, want the recorded identity %q", restored[0].ID, held[0].ID)
	}
	if restored[0].Delivery != want {
		t.Fatalf("restored delivery = %+v, want content, lineage and target %+v", restored[0].Delivery, want)
	}
	if !restored[0].HeldAt.Equal(held[0].HeldAt) {
		t.Fatalf("restored hold moment = %v, want the recorded moment %v", restored[0].HeldAt, held[0].HeldAt)
	}
	if err := host.ApproveDelivery(ctx, restored[0].ID); err != nil {
		t.Fatalf("approve the restored delivery: %v", err)
	}
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != recoveryHeldBody {
		t.Fatalf("delivered bodies after approval = %q, want the restored body exactly once", bodies)
	}
	if err := host.ApproveDelivery(ctx, restored[0].ID); !errors.Is(err, assembly.ErrDeliveryNotFound) {
		t.Fatalf("the repeated approval = %v, want ErrDeliveryNotFound", err)
	}
	if bodies := sender.bodies(); len(bodies) != 1 {
		t.Fatalf("delivered bodies after the repeated approval = %q, want no second copy", bodies)
	}
}

// testDeliveryRecoveryRetry refuses the first send of a turn body, restarts the
// host over the same storage and approves the redelivery the refusal owed.
func testDeliveryRecoveryRetry(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(recoveryRetryBody) }
	sender := &recoverySender{refusals: 1}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr := event.Address{AgentID: "user", SessionID: "session-delivery-recovery-retry"}
	host := bootDeliveryRecoveryHost(t, configPath, sender, nil)
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the turn whose send the channel refuses: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn beside the refused send", turn.Terminal)
	}
	if got := sender.refusedCount(); got != 1 {
		t.Fatalf("refused sends = %d, want the one refusal the channel scripted", got)
	}
	owed := host.HeldDeliveries()
	if len(owed) != 1 || owed[0].Delivery.Content != recoveryRetryBody {
		t.Fatalf("owed deliveries beside the refused send = %+v, want the body owed a redelivery", owed)
	}
	closeDeliveryReviewHost(t, host, ctx)

	host = bootDeliveryRecoveryHost(t, configPath, sender, nil)
	defer closeDeliveryReviewHost(t, host, ctx)
	restored := host.HeldDeliveries()
	if len(restored) != 1 || restored[0].Delivery.Content != recoveryRetryBody {
		t.Fatalf("owed deliveries after the restart = %+v, want the refused body still owed", restored)
	}
	if restored[0].ID != owed[0].ID {
		t.Fatalf("restored obligation identity = %q, want the recorded identity %q", restored[0].ID, owed[0].ID)
	}
	if err := host.ApproveDelivery(ctx, restored[0].ID); err != nil {
		t.Fatalf("approve the owed redelivery: %v", err)
	}
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != recoveryRetryBody {
		t.Fatalf("delivered bodies after the redelivery = %q, want the owed body exactly once", bodies)
	}
	if err := host.ApproveDelivery(ctx, restored[0].ID); !errors.Is(err, assembly.ErrDeliveryNotFound) {
		t.Fatalf("the repeated approval = %v, want ErrDeliveryNotFound", err)
	}
	if bodies := sender.bodies(); len(bodies) != 1 {
		t.Fatalf("delivered bodies after the repeated approval = %q, want no second copy", bodies)
	}
}

// testDeliveryRecoveryRefused refuses a restored body beside a second restart
// and reads the audit fact the refusal leaves at the target address.
func testDeliveryRecoveryRefused(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(recoveryHeldBody) }
	sender := &recoverySender{}
	ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr := event.Address{AgentID: "user", SessionID: "session-delivery-recovery-refused"}
	host := bootDeliveryRecoveryHost(t, configPath, sender, ruler)
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: addr,
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("invoke the held turn: %v", err)
	}
	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("held deliveries = %+v, want the one body the review held", held)
	}
	closeDeliveryReviewHost(t, host, ctx)

	host = bootDeliveryRecoveryHost(t, configPath, sender, ruler)
	restored := host.HeldDeliveries()
	if len(restored) != 1 || restored[0].ID != held[0].ID {
		t.Fatalf("held deliveries after the restart = %+v, want the recorded body %q", restored, held[0].ID)
	}
	if err := host.RefuseDelivery(ctx, restored[0].ID); err != nil {
		t.Fatalf("refuse the restored delivery: %v", err)
	}
	if bodies := sender.bodies(); len(bodies) != 0 {
		t.Fatalf("delivered bodies after the refusal = %q, want none", bodies)
	}
	if fact := deliveryRefusalFact(t, host, addr); fact.Meta[event.MetaPrefix+"delivery_id"] != restored[0].ID {
		t.Fatalf("refusal delivery identity = %q, want %q", fact.Meta[event.MetaPrefix+"delivery_id"], restored[0].ID)
	}
	closeDeliveryReviewHost(t, host, ctx)

	host = bootDeliveryRecoveryHost(t, configPath, sender, ruler)
	defer closeDeliveryReviewHost(t, host, ctx)
	if left := host.HeldDeliveries(); len(left) != 0 {
		t.Fatalf("owed deliveries beside the further restart = %+v, want the refusal consumed", left)
	}
	if err := host.RefuseDelivery(ctx, restored[0].ID); !errors.Is(err, assembly.ErrDeliveryNotFound) {
		t.Fatalf("the repeated refusal = %v, want ErrDeliveryNotFound", err)
	}
	if err := host.ApproveDelivery(ctx, restored[0].ID); !errors.Is(err, assembly.ErrDeliveryNotFound) {
		t.Fatalf("the approval beside a refusal = %v, want ErrDeliveryNotFound", err)
	}
	if bodies := sender.bodies(); len(bodies) != 0 {
		t.Fatalf("delivered bodies beside the refused identity = %q, want no redelivery", bodies)
	}
}

// bootDeliveryRecoveryHost boots the entry document named by the configuration
// path beside the controlled sender and review face the recovery checks drive.
func bootDeliveryRecoveryHost(t *testing.T, configPath string, sender assembly.MessageSender, ruler plugin.DeliveryRuler) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithDeliveryRuler(ruler))
	if err != nil {
		t.Fatalf("boot the delivery recovery host: %v", err)
	}
	return host
}

// recoverySender is the outbound face of the recovery checks: the sends it is
// scripted to refuse are answered with a named refusal, and every accepted body
// is recorded beside the address it was delivered to.
type recoverySender struct {
	mu        sync.Mutex
	refusals  int
	refused   int
	delivered []deliveredBody
}

// Send refuses the scripted count of sends and records every other body.
func (s *recoverySender) Send(_ context.Context, target event.Address, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refusals > 0 {
		s.refusals--
		s.refused++
		return errRecoveryChannelRefusal
	}
	s.delivered = append(s.delivered, deliveredBody{target: target, content: content})
	return nil
}

// bodies reports the delivered bodies in delivery order, as a copy the caller owns.
func (s *recoverySender) bodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.delivered))
	for _, body := range s.delivered {
		out = append(out, body.content)
	}
	return out
}

// refusedCount reports the number of sends the scripted refusal answered.
func (s *recoverySender) refusedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

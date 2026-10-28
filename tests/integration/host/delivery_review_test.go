// The delivery review checks of the host: a body the review plugin holds waits
// before the user channel, an approval re-delivers it exactly once, and a
// refusal discards it with a payload-free audit fact.
package host

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
)

// heldReviewBody is the assistant body the review checks see the provider
// answer and the host rule before delivery.
const heldReviewBody = "the body the delivery review holds"

// sendReviewBody is the body the declared send tool hands to the delivery
// review, and sendReviewClosing is the assistant body the same turn delivers
// after the call.
const (
	sendReviewBody    = "the body the send tool delivers"
	sendReviewClosing = "the closing body of the send turn"
)

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
		if err := host.ApproveDelivery(ctx, held[0].ID); !errors.Is(err, assembly.ErrDeliveryNotFound) {
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
		if err := host.RefuseDelivery(ctx, held[0].ID); !errors.Is(err, assembly.ErrDeliveryNotFound) {
			t.Fatalf("the repeated refusal = %v, want ErrDeliveryNotFound", err)
		}
	})
}

// TestDeliveryReviewSend pins the one delivery outlet on the model-visible send tool.
// - positive: an allowed send reaches the review with its body, its turn's lineage and the call's address, and the channel delivers it exactly once
// - negative: a held send stays off the channel, the call is answered as held, and only an approval delivers the body
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestDeliveryReviewSend(t *testing.T) {
	t.Run("an allowed send reaches the review and the channel", func(t *testing.T) {
		provider := newScriptedProvider(t)
		provider.decide = func(request scriptedRequest) (string, error) {
			if request.Index == 0 {
				return scriptedSendCall(sendReviewBody), nil
			}
			return scriptedAnswerOf(sendReviewClosing), nil
		}
		sender := &deliveryRecorder{}
		ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionAllow}
		host := bootDeliveryReviewSendHost(t, provider, sender, ruler)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		defer closeDeliveryReviewHost(t, host, ctx)

		addr := event.Address{AgentID: "user", SessionID: "session-delivery-review-send-allow"}
		turn, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address:       addr,
			TriggerSource: "user",
			Input:         []model.Message{{Role: model.RoleUser, Content: "send it"}},
		})
		if err != nil {
			t.Fatalf("invoke the send turn: %v", err)
		}
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn terminal = %v, want the completed turn of the send script", turn.Terminal)
		}

		send := plugin.Delivery{Content: sendReviewBody, Lineage: "user", Target: addr}
		reviewed := ruler.seenDeliveries()
		if len(reviewed) != 2 {
			t.Fatalf("reviewed deliveries = %+v, want the send body beside the closing body", reviewed)
		}
		if reviewed[0] != send {
			t.Fatalf("reviewed send = %+v, want the call's body, lineage and address %+v", reviewed[0], send)
		}
		if bodies := sender.bodies(); len(bodies) != 2 || bodies[0] != sendReviewBody {
			t.Fatalf("delivered bodies = %q, want the send body before the closing body", bodies)
		}
		if scriptedSawContent(provider, "[send_denied] ") || scriptedSawContent(provider, "[send_held] ") {
			t.Fatal("the send call read back a refusal or a hold beside the allow ruling")
		}
		if held := host.HeldDeliveries(); len(held) != 0 {
			t.Fatalf("held deliveries = %+v, want none beside the allow ruling", held)
		}
	})

	t.Run("a held send waits and its receipt is not a delivery", func(t *testing.T) {
		provider := newScriptedProvider(t)
		provider.decide = func(request scriptedRequest) (string, error) {
			if request.Index == 0 {
				return scriptedSendCall(sendReviewBody), nil
			}
			return scriptedAnswerOf(sendReviewClosing), nil
		}
		sender := &deliveryRecorder{}
		ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
		host := bootDeliveryReviewSendHost(t, provider, sender, ruler)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		defer closeDeliveryReviewHost(t, host, ctx)

		addr := event.Address{AgentID: "user", SessionID: "session-delivery-review-send-hold"}
		if _, err := host.Invoke(ctx, assembly.InvokeRequest{
			Address:       addr,
			TriggerSource: "user",
			Input:         []model.Message{{Role: model.RoleUser, Content: "send it"}},
		}); err != nil {
			t.Fatalf("invoke the held send turn: %v", err)
		}
		if bodies := sender.bodies(); len(bodies) != 0 {
			t.Fatalf("delivered bodies = %q, want none while the review holds", bodies)
		}
		if !scriptedSawContent(provider, "[send_held] ") {
			t.Fatal("the send call read back no held receipt; a hold must never read as a delivery")
		}

		held := host.HeldDeliveries()
		if len(held) != 2 {
			t.Fatalf("held deliveries = %+v, want the send body beside the closing body", held)
		}
		var sendID string
		for _, entry := range held {
			if entry.Delivery.Content == sendReviewBody {
				sendID = entry.ID
				if want := (plugin.Delivery{Content: sendReviewBody, Lineage: "user", Target: addr}); entry.Delivery != want {
					t.Fatalf("held send = %+v, want the call's body, lineage and address %+v", entry.Delivery, want)
				}
			}
		}
		if sendID == "" {
			t.Fatalf("held deliveries = %+v, want the held send body", held)
		}
		if err := host.ApproveDelivery(ctx, sendID); err != nil {
			t.Fatalf("approve the held send: %v", err)
		}
		if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != sendReviewBody {
			t.Fatalf("delivered bodies after approval = %q, want the held send body exactly once", bodies)
		}
	})
}

// TestDeliveryReviewDefaultAllow pins a host with no ruler attached: the send body and the turn body are ordinary deliveries, holding nothing.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestDeliveryReviewDefaultAllow(t *testing.T) {
	provider := newScriptedProvider(t)
	provider.decide = func(request scriptedRequest) (string, error) {
		if request.Index == 0 {
			return scriptedSendCall(sendReviewBody), nil
		}
		return scriptedAnswerOf(sendReviewClosing), nil
	}
	sender := &deliveryRecorder{}
	host := bootDeliveryReviewSendHost(t, provider, sender, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer closeDeliveryReviewHost(t, host, ctx)

	addr := event.Address{AgentID: "user", SessionID: "session-delivery-review-default-allow"}
	if _, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "send it"}},
	}); err != nil {
		t.Fatalf("invoke the send turn beside no ruler: %v", err)
	}
	if bodies := sender.bodies(); len(bodies) != 2 || bodies[0] != sendReviewBody || bodies[1] != sendReviewClosing {
		t.Fatalf("delivered bodies = %q, want the send body and the closing body", bodies)
	}
	if held := host.HeldDeliveries(); len(held) != 0 {
		t.Fatalf("held deliveries = %+v, want none beside no ruler", held)
	}
}

// TestDeliveryReviewFailureNotice pins the failure notice on the one delivery outlet.
// - positive: the notice is ruled with the turn's lineage and the failed turn's target, and nothing reaches the channel while it is held
// - negative: only an approval delivers the notice, and the delivered body still carries the raw cause
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestDeliveryReviewFailureNotice(t *testing.T) {
	provider := newFailureProvider(t, failureModeFails)
	sender := &deliveryRecorder{}
	ruler := &scriptedDeliveryRuler{disposition: plugin.DispositionHold}
	host := bootDeliveryReviewEndpointHost(t, provider.endpoint(), sender, ruler)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	defer closeDeliveryReviewHost(t, host, ctx)

	addr := event.Address{AgentID: "user", SessionID: "session-delivery-review-failure"}
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "the provider refuses this turn"}},
	})
	if err != nil {
		t.Fatalf("invoke the failing turn: %v", err)
	}
	if turn.Terminal != model.KindFailed || !strings.Contains(turn.Failure, "500") {
		t.Fatalf("turn = (%s, %q), want the failed terminal carrying the raw 500 cause", turn.Terminal, turn.Failure)
	}
	if bodies := sender.bodies(); len(bodies) != 0 {
		t.Fatalf("delivered bodies = %q, want none while the review holds the notice", bodies)
	}
	held := host.HeldDeliveries()
	if len(held) != 1 {
		t.Fatalf("held deliveries = %+v, want the one failure notice the review held", held)
	}
	if held[0].Delivery.Lineage != "user" || held[0].Delivery.Target != addr {
		t.Fatalf("held notice = %+v, want the turn's lineage and target", held[0].Delivery)
	}
	if !strings.Contains(held[0].Delivery.Content, "500") {
		t.Fatalf("held notice = %q, want the raw 500 identity of the provider failure", held[0].Delivery.Content)
	}
	if reviewed := ruler.seenDeliveries(); len(reviewed) != 1 || reviewed[0] != held[0].Delivery {
		t.Fatalf("reviewed deliveries = %+v, want the held notice %+v", reviewed, held[0].Delivery)
	}
	if err := host.ApproveDelivery(ctx, held[0].ID); err != nil {
		t.Fatalf("approve the held failure notice: %v", err)
	}
	bodies := sender.bodies()
	if len(bodies) != 1 || bodies[0] != held[0].Delivery.Content {
		t.Fatalf("delivered bodies = %q, want the approved notice exactly once", bodies)
	}
}

// scriptedSendCall renders the assistant answer that issues one send call.
func scriptedSendCall(body string) string {
	args, err := json.Marshal(map[string]string{"content": body})
	if err != nil {
		panic("host: encode the scripted send arguments: " + err.Error())
	}
	return `{"id":"scripted-send","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":"sending","tool_calls":[{"id":"call-send","type":"function","function":{"name":"send","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`
}

// scriptedSawContent reports whether any request the provider answered carried
// a message whose content starts with the given prefix.
func scriptedSawContent(provider *scriptedProvider, prefix string) bool {
	for _, request := range provider.servedRequests() {
		for _, message := range request.Messages {
			if strings.HasPrefix(message.Content, prefix) {
				return true
			}
		}
	}
	return false
}

// bootDeliveryReviewSendHost boots the entry document that declares the send
// tool beside the review face and the controlled sender the send checks drive.
func bootDeliveryReviewSendHost(t *testing.T, provider *scriptedProvider, sender assembly.MessageSender, ruler plugin.DeliveryRuler) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	agentBlock := "    tools:\n" +
		"      - { kind: tool, id: send }\n"
	configPath := filepath.Join(root, "tagent.yaml")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-entry", agentBlock))
	host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithDeliveryRuler(ruler))
	if err != nil {
		t.Fatalf("boot the send review host: %v", err)
	}
	return host
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
func bootDeliveryReviewHost(t *testing.T, provider *scriptedProvider, sender assembly.MessageSender, ruler plugin.DeliveryRuler) *assembly.Host {
	t.Helper()
	return bootDeliveryReviewEndpointHost(t, provider.endpoint(), sender, ruler)
}

// bootDeliveryReviewEndpointHost boots the bare entry document against one
// endpoint, so a check can drive a failing provider through the same review
// face as a succeeding one.
func bootDeliveryReviewEndpointHost(t *testing.T, endpoint string, sender assembly.MessageSender, ruler plugin.DeliveryRuler) *assembly.Host {
	t.Helper()
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, endpoint, "model-entry")
	host, err := assembly.Boot(configPath, assembly.WithSender(sender), assembly.WithDeliveryRuler(ruler))
	if err != nil {
		t.Fatalf("boot the delivery review host: %v", err)
	}
	return host
}

// closeDeliveryReviewHost closes the host beside the check and reports a
// refused close without hiding the check's own failure.
func closeDeliveryReviewHost(t *testing.T, host *assembly.Host, ctx context.Context) {
	t.Helper()
	if err := host.Close(ctx); err != nil {
		t.Errorf("close the delivery review host: %v", err)
	}
}

// deliveryRefusalFact reports the refusal feedback fact recorded at one
// address, failing beside the committed facts when the audit fact is absent.
func deliveryRefusalFact(t *testing.T, host *assembly.Host, addr event.Address) event.Event {
	t.Helper()
	for _, fact := range host.Run().SessionFacts(addr) {
		if fact.Type == event.TypeFeedback && fact.Meta[event.MetaKeyEventSummary] == "delivery refused" {
			return fact
		}
	}
	t.Fatalf("no delivery refusal fact was committed at %+v", addr)
	return event.Event{}
}

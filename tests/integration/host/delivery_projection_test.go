// The delivery checks of the host: a user-address turn delivers the assistant
// bodies its committed facts carry past the invocation baseline, the session a
// declaration maps onto a projection included, and no other address delivers
// anything.
package host

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestUserTurnDeliversThroughProjectionSession pins the delivery of a user turn whose declaration registers a replay projection:
//   - the entry agent declares the model window that maps the session chain onto a projection
//   - the projection carries the assistant answer while the session transcript stays empty
//   - the user-address turn delivers that answer through the message sender
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestUserTurnDeliversThroughProjectionSession(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	const answer = "the answer the projection session owes its user"
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf(answer) }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-a",
		"    max_tokens: 32000\n"))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-delivery-projection"}
	if turn := invokeScriptedTurn(t, ctx, host, string(addr.SessionID)); turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the scripted answer", turn.Terminal)
	}
	if history := host.Run().SessionHistory(addr); len(history) != 0 {
		t.Fatalf("session transcript = %+v, want the projection to carry the ended range instead", history)
	}
	entries, mapped := host.Run().ReplayEntries(addr.SessionID)
	if !mapped {
		t.Fatal("the session registered no projection, want the declared window to map the chain")
	}
	projected := false
	for _, entry := range entries {
		if entry.Content == answer {
			projected = true
		}
	}
	if !projected {
		t.Fatalf("projection entries = %+v, want the sequence to carry the answer %q", entries, answer)
	}
	delivered := sender.deliveries()
	if len(delivered) != 1 || delivered[0].content != answer || delivered[0].target != addr {
		t.Fatalf("deliveries = %+v, want the answer %q addressed to %+v", delivered, answer, addr)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestUserTurnDeliversOnlyThisInvocation pins the baseline each delivery reads:
//   - the first turn delivers the first answer alone
//   - the second turn delivers the second answer alone, never the first again
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestUserTurnDeliversOnlyThisInvocation(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	const (
		firstAnswer  = "the answer the first invocation owes"
		secondAnswer = "the answer the second invocation owes"
	)
	provider := newScriptedProvider(t)
	provider.answer = func(index int) string {
		if index == 0 {
			return scriptedAnswerOf(firstAnswer)
		}
		return scriptedAnswerOf(secondAnswer)
	}
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-a",
		"    max_tokens: 32000\n"))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	invokeScriptedTurn(t, ctx, host, "session-delivery-baseline")
	if bodies := sender.bodies(); len(bodies) != 1 || bodies[0] != firstAnswer {
		t.Fatalf("bodies after the first turn = %q, want the first answer alone", bodies)
	}
	invokeScriptedTurn(t, ctx, host, "session-delivery-baseline")
	if bodies := sender.bodies(); len(bodies) != 2 || bodies[0] != firstAnswer || bodies[1] != secondAnswer {
		t.Fatalf("bodies after the second turn = %q, want the second answer appended beside no repeat", bodies)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// TestHeadlessAddressDeliversNothing pins the address gate every delivery reads:
//   - a turn at a declared agent that is not the entry still answers a body
//   - the sender sees no delivery at all
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestHeadlessAddressDeliversNothing(t *testing.T) {
	t.Setenv("TAGENT_TEST_API_KEY", "test-key")
	t.Setenv("TAGENT_HTTP_PORT", "0")
	t.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	provider := newScriptedProvider(t)
	provider.answer = func(int) string { return scriptedAnswerOf("the answer a headless turn produces") }
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-a")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgentSections(root, provider.endpoint(), "model-a",
		"  user: {}\n"+
			"  knowledge: {}\n"))
	sender := &deliveryRecorder{}
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	turn, err := host.Invoke(ctx, assembly.InvokeRequest{
		Address: event.Address{AgentID: "knowledge", SessionID: "session-headless-delivery"},
		Input:   []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("invoke the headless address: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("turn terminal = %v, want the completed turn of the scripted answer", turn.Terminal)
	}
	if bodies := sender.bodies(); len(bodies) != 0 {
		t.Fatalf("delivered bodies = %q, want no delivery outside the entry address", bodies)
	}
	if err := host.Close(ctx); err != nil {
		t.Fatalf("close the host: %v", err)
	}
}

// scriptedAnswerOf renders the terminal answer the scripted provider serves
// whose assistant body is the given text, so a check reads back exactly the
// body it scripted.
func scriptedAnswerOf(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic("host: encode the scripted assistant body: " + err.Error())
	}
	return `{"id":"scripted","object":"chat.completion","created":1699200000,"choices":[{"index":0,"message":{"role":"assistant","content":` + string(encoded) + `},"finish_reason":"stop"}]}`
}

// deliveryRecorder is the outbound message face of the delivery checks: every
// accepted body is recorded beside the address it was delivered to, so a check
// compares what the host delivered with what the provider answered.
type deliveryRecorder struct {
	mu        sync.Mutex
	delivered []deliveredBody
}

// deliveredBody is one body the recorder accepted beside the address it was
// addressed to.
type deliveredBody struct {
	target  event.Address
	content string
}

// Send records one body and reports the accepted delivery.
func (r *deliveryRecorder) Send(_ context.Context, target event.Address, content string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delivered = append(r.delivered, deliveredBody{target: target, content: content})
	return nil
}

// bodies reports the delivered bodies in delivery order, as a copy the caller
// owns.
func (r *deliveryRecorder) bodies() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.delivered))
	for _, body := range r.delivered {
		out = append(out, body.content)
	}
	return out
}

// deliveries reports the delivered bodies beside their addresses, in delivery
// order, as a copy the caller owns.
func (r *deliveryRecorder) deliveries() []deliveredBody {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]deliveredBody(nil), r.delivered...)
}

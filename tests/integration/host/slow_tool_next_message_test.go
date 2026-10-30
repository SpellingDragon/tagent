// The slow-tool anchor of the consumer: a message that arrives while the turn
// in flight still runs its tool must be answered by the model, not parked until
// that tool returns. The tool is a real tool on the outbound face — the send
// tool parked inside the sender — so the check reads the failure the user reads:
// the second message lands, the call answers a receipt, and no answer follows
// until the first turn's tool finishes.
//
// 契约: docs/wiki/agent/event-flow.md
package host

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// slowToolInputOne and the markers beside it spell the two turns of the anchor:
// the first turn issues the send call the sender parks, and the second message
// arrives while that call is still running.
const (
	slowToolInputOne  = "slow-tool turn one input marker"
	slowToolNote      = "slow-tool note carried by the parked send"
	slowToolInputTwo  = "slow-tool message that arrives while the tool runs"
	slowToolAnswerTwo = "slow-tool answer of the message that arrived while the tool ran"
	slowToolCallID    = "call-slow-tool"
)

// slowToolProbe is how long the anchor waits for the answer of the arriving
// message while the tool it does not wait for still runs. A consumer that reads
// the message answers inside this window; one that parks the message until the
// tool returns answers after it.
const slowToolProbe = 5 * time.Second

// TestSlowToolDoesNotBlockNextConsumption requires a message that arrives while a tool still runs to be answered without waiting for that tool.
//   - positive: the arriving message is read by a model request and its answer reaches the sender before the tool returns
//   - negative: the practice of the call — a receipt of admission — is not an answer, so the anchor fails while no body follows the receipt
//   - consumer: the formal assembly booted from a real document, driven through Host.Invoke beside a real sender that parks the tool on the outbound face
//
// 契约: docs/wiki/agent/event-flow.md
func TestSlowToolDoesNotBlockNextConsumption(t *testing.T) {
	reproductionEnv(t)
	provider := newScriptedProvider(t)
	provider.decide = func(request scriptedRequest) (string, error) {
		if slowToolRequestCarries(request, slowToolInputTwo) {
			return scriptedAnswerOf(slowToolAnswerTwo), nil
		}
		if slowToolRequestCarries(request, slowToolInputOne) {
			return settleToolCallAnswer("send", slowToolCallID, `{"content":"`+slowToolNote+`"}`), nil
		}
		return scriptedAnswerOf("the first turn ends after its tool settles"), nil
	}

	sender := newSlowToolSender(newRecordingSender())
	t.Cleanup(sender.letGo)
	root := t.TempDir()
	configPath := writeScriptedHostDocument(t, root, provider.endpoint(), "model-entry")
	writeHostDocumentFile(t, configPath, scriptedHostDocumentWithAgent(root, provider.endpoint(), "model-entry",
		"    tools:\n"+
			"      - { kind: tool, id: send }\n"))
	host, err := assembly.Boot(configPath, assembly.WithSender(sender))
	if err != nil {
		t.Fatalf("boot the slow-tool host: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	addr := event.Address{AgentID: "user", SessionID: "session-slow-tool"}

	ended := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, slowToolInputOne)
		ended <- integrityOutcome{turn: turn, err: err}
	}()
	select {
	case <-sender.arrived:
	case <-time.After(60 * time.Second):
		t.Fatalf("the tool of the first turn never reached the outbound face")
	}

	arrived := make(chan integrityOutcome, 1)
	go func() {
		turn, err := driveIntegrityTurn(ctx, host, addr, slowToolInputTwo)
		arrived <- integrityOutcome{turn: turn, err: err}
	}()

	// The answer, not the receipt: a call reported as admitted while the tool
	// runs satisfies the admission and leaves the user without one, so the
	// anchor reads the sender for the body of the arriving message.
	var receipt integrityOutcome
	answered := false
	deadline := time.After(slowToolProbe)
	for !answered {
		select {
		case body := <-sender.delivered:
			if body == slowToolAnswerTwo {
				answered = true
			}
		case out := <-arrived:
			receipt = out
		case <-deadline:
			if receipt.turn.Terminal == model.KindUnknown {
				t.Fatalf("the message that arrived while the tool ran got the receipt %v of the range %q and no answer in %v: "+
					"the consumer parked the message behind the tool the first turn still runs",
					receipt.turn.Terminal, receipt.turn.InvocationID, slowToolProbe)
			}
			t.Fatalf("the message that arrived while the tool ran got no answer in %v: the consumer waits for the tool of the turn in flight", slowToolProbe)
		}
	}

	carried := 0
	for _, request := range provider.servedRequests() {
		if slowToolRequestCarries(request, slowToolInputTwo) {
			carried++
		}
	}
	if carried != 1 {
		t.Fatalf("requests carrying the arriving message = %d, want exactly 1: the answer above must come from one decision that read it", carried)
	}

	sender.letGo()
	if first := awaitIntegrityDrive(t, ended, "the turn whose tool ran"); first.err != nil {
		t.Fatalf("the turn whose tool ran ended by error: %v", first.err)
	}
}

// slowToolRequestCarries reports whether one request carried the named user
// content anywhere in its messages, so the anchor reads a message the layout
// keeps beside a late system segment instead of reading only the tail.
func slowToolRequestCarries(request scriptedRequest, content string) bool {
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) && message.Content == content {
			return true
		}
	}
	return false
}

// slowToolSender parks the first outbound body until the check releases it, so
// the tool of the first turn still runs on the outbound face while the next
// message arrives. Every body past the first leaves for the stand-in, and the
// bodies that left are reported in delivery order.
type slowToolSender struct {
	base      recordingSender
	arrived   chan struct{}
	delivered chan string
	release   chan struct{}
	parkOnce  sync.Once
	letOnce   sync.Once
}

// newSlowToolSender returns the sender beside the gate it parks the first body at.
func newSlowToolSender(base recordingSender) *slowToolSender {
	return &slowToolSender{
		base:      base,
		arrived:   make(chan struct{}),
		delivered: make(chan string, 8),
		release:   make(chan struct{}),
	}
}

// Send parks the first body until the check releases it and hands every body to
// the stand-in, so the tool reads as running and every delivered body is read.
func (s *slowToolSender) Send(ctx context.Context, target event.Address, body string) error {
	s.parkOnce.Do(func() {
		close(s.arrived)
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	})
	err := s.base.Send(ctx, target, body)
	if err == nil {
		select {
		case s.delivered <- body:
		default:
		}
	}
	return err
}

// letGo releases the parked body once, so a failing check still lets the server
// it leaves behind return.
func (s *slowToolSender) letGo() {
	s.letOnce.Do(func() { close(s.release) })
}

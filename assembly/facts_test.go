// The hot window checks of the live fact ledger: a chain that runs long keeps the
// material a drive may still read, and the delivery of a turn reads that material
// by the identity of the invocation rather than by a position of the chain.
package assembly

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// factWindowAnswer is the assistant body the scripted provider answers every turn
// of these checks with.
const factWindowAnswer = "the answer the hot window keeps"

// factWindowAddr is the session the hot window checks commit their facts to.
var factWindowAddr = event.Address{AgentID: "agent-a", SessionID: "session-fact-window"}

// TestFactsStayInTheirSessionWindow pins the bound of the live fact ledger.
// - a chain whose last agent_output closes its segment keeps the most recent window of entries
// - a segment in flight is kept whole beside the turn-end fact that opened it
// - a chain beside no turn-end fact is one segment in flight and is kept whole
// - a chain replayed from the durable log converges to the same window
// - each session keeps its own window, so one chain never drops a neighbour's material
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestFactsStayInTheirSessionWindow(t *testing.T) {
	ctx := context.Background()
	t.Run("a closed chain keeps the most recent window", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		const seeded = factWindow + 32
		for at := 0; at < seeded; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("closed-%05d", at), "inv-window-closed", "the material of the closed chain", event.TypeExternalInput))
		}
		commitWindowFact(t, ctx, run, addr, windowEvent(addr,
			"closed-closing", "inv-window-closed", "the answer that closed the chain", event.TypeAgentOutput))

		facts := run.SessionFacts(addr)
		if len(facts) != factWindow {
			t.Fatalf("the closed chain = %d facts, want the window %d", len(facts), factWindow)
		}
		if want := event.EventID("closed-00033"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want %s", facts[0].Key, want)
		}
		if _, held := run.factOf(event.EventID("closed-00032")); held {
			t.Fatalf("the fact outside the window is still held by the live ledger")
		}
		if got := run.CommittedFacts(); got != factWindow {
			t.Fatalf("the live ledger holds %d facts, want the window %d", got, factWindow)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("a segment in flight is kept whole", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		commitWindowFact(t, ctx, run, addr, windowEvent(addr,
			"flight-opening", "inv-window-flight", "the answer that closed the previous segment", event.TypeAgentOutput))
		for at := 0; at < 2*factWindow; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("flight-%05d", at), "inv-window-flight", "the material of the segment in flight", event.TypeExternalInput))
		}

		facts := run.SessionFacts(addr)
		if want := 2*factWindow + 1; len(facts) != want {
			t.Fatalf("the chain in flight = %d facts, want the opening fact beside the whole segment %d", len(facts), want)
		}
		if want := event.EventID("flight-opening"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want the opening fact %s", facts[0].Key, want)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("a chain beside no turn-end is kept whole", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		for at := 0; at < factWindow+64; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("open-%05d", at), "inv-window-open", "the material of the only segment", event.TypeExternalInput))
		}

		facts := run.SessionFacts(addr)
		if want := factWindow + 64; len(facts) != want {
			t.Fatalf("the chain beside no turn-end = %d facts, want the whole segment %d", len(facts), want)
		}
		if want := event.EventID("open-00000"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want the head of the segment %s", facts[0].Key, want)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("a replayed chain converges to the window", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		for at := 0; at < factWindow+64; at++ {
			run.seedFact(windowEvent(addr, fmt.Sprintf("replay-%05d", at), "inv-window-replay", "the replayed material", event.TypeExternalInput))
		}
		run.seedFact(windowEvent(addr, "replay-closing", "inv-window-replay", "the answer the earlier process committed", event.TypeAgentOutput))
		run.trimSeededFacts()

		facts := run.SessionFacts(addr)
		if len(facts) != factWindow {
			t.Fatalf("the replayed chain = %d facts, want the window %d", len(facts), factWindow)
		}
		if want := event.EventID("replay-00065"); facts[0].Key != want {
			t.Fatalf("the oldest kept fact = %s, want %s", facts[0].Key, want)
		}
		requireFactOrderMatchesLedger(t, run)
	})

	t.Run("each session keeps its own window", func(t *testing.T) {
		run := factWindowTestRun(t, ctx)
		addr := factWindowAddr
		other := event.Address{AgentID: "agent-a", SessionID: "session-fact-window-neighbour"}
		for at := 0; at < factWindow+16; at++ {
			commitWindowFact(t, ctx, run, addr, windowEvent(addr,
				fmt.Sprintf("long-%05d", at), "inv-window-long", "the material of the long chain", event.TypeExternalInput))
		}
		commitWindowFact(t, ctx, run, addr, windowEvent(addr,
			"long-closing", "inv-window-long", "the answer that closed the long chain", event.TypeAgentOutput))
		for at := 0; at < 6; at++ {
			commitWindowFact(t, ctx, run, other, windowEvent(other,
				fmt.Sprintf("neighbour-%02d", at), "inv-window-neighbour", "the material of the short chain", event.TypeExternalInput))
		}
		if got := len(run.SessionFacts(other)); got != 6 {
			t.Fatalf("the short chain = %d facts, want its six facts kept whole", got)
		}
		if got := len(run.SessionFacts(addr)); got != factWindow {
			t.Fatalf("the long chain = %d facts, want its window %d kept", got, factWindow)
		}
	})
}

// TestDeliveryAfterRestartKeepsTheTurnAnswer pins the delivery of the first turn a restarted host drives over a trimmed chain.
// - the answer of that turn reaches the outbound face although the chain is trimmed as it commits
// - the body the sender takes is the fact the turn committed under its own invocation
// - the chain stays within the window beside the segment the turn flew
//
// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
func TestDeliveryAfterRestartKeepsTheTurnAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := factWindowTestRun(t, ctx)
	addr := factWindowAddr
	for at := 0; at < factWindow+64; at++ {
		run.seedFact(windowEvent(addr, fmt.Sprintf("restart-%05d", at), "inv-window-earlier", "the material the restart replays", event.TypeExternalInput))
	}
	run.seedFact(windowEvent(addr, "restart-closing", "inv-window-earlier",
		"the answer the earlier process delivered", event.TypeAgentOutput))
	run.trimSeededFacts()
	if got := len(run.SessionFacts(addr)); got != factWindow {
		t.Fatalf("the replayed chain = %d facts, want the window %d", got, factWindow)
	}

	channel := &windowTestChannel{}
	host := &Host{
		run:               run,
		entry:             "agent-a",
		sender:            channel,
		userAddresses:     map[event.SessionID]event.Address{},
		pendingDeliveries: map[string]*heldDelivery{},
	}
	turn, err := host.Invoke(ctx, InvokeRequest{
		Address:       addr,
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "the first turn after the restart"}},
	})
	if err != nil {
		t.Fatalf("invoke the first turn after the restart: %v", err)
	}
	if turn.Terminal != model.KindCompleted || turn.InvocationID == "" {
		t.Fatalf("the turn = (%v, %q), want a completed turn beside its invocation", turn.Terminal, turn.InvocationID)
	}
	if bodies := channel.bodies(); len(bodies) != 1 || bodies[0] != factWindowAnswer {
		t.Fatalf("delivered bodies = %q, want the one answer of the turn %q", bodies, factWindowAnswer)
	}
	bodies := generatedBodiesOf(run.SessionFacts(addr), turn.InvocationID)
	if len(bodies) != 1 || bodies[0].content != factWindowAnswer {
		t.Fatalf("the bodies the turn invocation names = %+v, want the one delivered answer", bodies)
	}
	if got := len(run.SessionFacts(addr)); got > factWindow {
		t.Fatalf("the chain beside the turn = %d facts, want no more than the window %d", got, factWindow)
	}
	requireFactOrderMatchesLedger(t, run)
}

// factWindowTestRun assembles one runtime whose provider answers a turn with one
// plain assistant body and no tool call, so a drive commits its input fact beside
// its turn-end fact alone.
func factWindowTestRun(t *testing.T, ctx context.Context) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}},
			Done:    true,
		}
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	natives := map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}
	run, err := Assemble(ctx, decl, natives, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	return run
}

// windowEvent builds one fact of these checks under the invocation it belongs to.
func windowEvent(addr event.Address, key, invocation, body, kind string) event.Event {
	role := model.RoleUser
	if kind == event.TypeAgentOutput {
		role = model.RoleAssistant
	}
	return event.Event{
		Key:          event.EventID(key),
		Type:         kind,
		Timestamp:    time.Now(),
		Message:      model.Message{Role: role, Content: body},
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: event.InvocationID(invocation),
	}
}

// commitWindowFact commits one fact through the live commit entry of the run.
func commitWindowFact(t *testing.T, ctx context.Context, run *Run, addr event.Address, evt event.Event) {
	t.Helper()
	if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
		t.Fatalf("commit the fact %s: %v", evt.Key, err)
	}
}

// requireFactOrderMatchesLedger requires the commit order of the run to name only
// facts the live ledger still holds, and every one of them once.
func requireFactOrderMatchesLedger(t *testing.T, run *Run) {
	t.Helper()
	run.mu.Lock()
	defer run.mu.Unlock()
	if len(run.factOrder) != len(run.facts) {
		t.Fatalf("the commit order names %d facts beside the %d the ledger holds", len(run.factOrder), len(run.facts))
	}
	for _, key := range run.factOrder {
		if _, held := run.facts[key]; !held {
			t.Fatalf("the commit order names the dropped fact %s", key)
		}
	}
}

// windowTestChannel is the outbound face these checks hand the host: it keeps
// every body it accepted and accepts all of them.
type windowTestChannel struct {
	mu   sync.Mutex
	sent []string
}

// Send records one accepted body and reports the delivery as landed.
func (c *windowTestChannel) Send(_ context.Context, _ event.Address, content string) error {
	c.mu.Lock()
	c.sent = append(c.sent, content)
	c.mu.Unlock()
	return nil
}

// bodies reports the bodies the channel accepted, in the order it took them.
func (c *windowTestChannel) bodies() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}

package execution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/subagent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestDelegatedTurnRunsOnItsOwnInstance drives one call_subagent call through the consumption pipeline.
// - the decision that issues a call answers with the receipt while the delegated round still waits on its model
// - the round of a delegated call flies a segment of its own execution site, never a segment of the caller
// - negative: the terminal of the one admitted task arrives exactly once, however long the round ran
//   - the body the round produced is the one the notification carries, so the result returned as an event of the site's chain
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegatedTurnRunsOnItsOwnInstance(t *testing.T) {
	t.Run("the issuing decision answers while the delegated round is held", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer", gate: make(chan struct{}), started: make(chan struct{})}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		caller := &convModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-review", subagent.ToolName, string(reviewCallArgs())))
			}
			return convFinalTurn("the delegation was admitted")
		}}
		rig.start(t, caller, []trpctool.Tool{tool}, issuer)

		issuing := make(chan agent.Turn, 1)
		go func() {
			turn, err := rig.submit(ctx, issuer, "review the change")
			if err != nil {
				t.Errorf("drive the issuing round: %v", err)
			}
			issuing <- turn
		}()

		select {
		case <-delegated.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the delegated round never started on its own instance")
		}
		if got := len(caller.recorded()); got != 1 {
			t.Fatalf("issuing requests = %d, want the one decision that answered the receipt while the delegated round is held", got)
		}
		if got := delegated.count(); got != 1 {
			t.Fatalf("delegated turns = %d, want exactly the one round of the admitted call", got)
		}
		if pending := rig.ledger.Pending(); pending != 1 {
			t.Fatalf("pending delegations = %d, want the admitted call still booked beside the answered issuing decision", pending)
		}
		select {
		case turn := <-issuing:
			t.Fatalf("issuing turn = %+v, want it unended while the delegated round holds its model", turn)
		default:
		}
		close(delegated.gate)
		awaitEnvelope(t, rig.arrived)
		select {
		case turn := <-issuing:
			if turn.Terminal != model.KindCompleted || turn.DecisionCount != 2 {
				t.Fatalf("issuing turn = (%v, %d decisions), want the follow-up decision the settlement woke", turn.Terminal, turn.DecisionCount)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the issuing turn never ended after the delegated round settled")
		}
	})

	t.Run("the delegated round flies a segment of its own site", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer"}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		rig.start(t, &convModel{name: "model-caller", answer: func(*model.Request) *model.Response {
			return convFinalTurn("the caller decides alone")
		}}, nil, issuer)

		if _, err := tool.Call(task.WithCallFrame(ctx, subagentCallFrame(issuer, "call-review")), reviewCallArgs()); err != nil {
			t.Fatalf("call the tool = %v, want the admission receipt", err)
		}
		envelope := awaitEnvelope(t, rig.arrived)
		view := rig.ledger.View(rig.invocationID)
		if len(view) != 1 || view[0].Status != task.StatusSettled {
			t.Fatalf("delegation view = %+v, want the one call landed at its terminal", view)
		}
		site := view[0].Sites.Target
		outputs := rig.facts.outputsAt(site)
		if len(outputs) != 1 {
			t.Fatalf("segments at the execution site = %d, want exactly the one round the site flew", len(outputs))
		}
		if want := fmt.Sprintf("%s#c1", site.SessionID); string(outputs[0].InvocationID) != want {
			t.Fatalf("site segment = %q, want the instance segment %q of the site's own session", outputs[0].InvocationID, want)
		}
		if outputs[0].Message.Content != "review recorded" {
			t.Fatalf("site segment body = %q, want the body the delegated model produced", outputs[0].Message.Content)
		}
		if !strings.Contains(envelope.Notice.Text, "review recorded") {
			t.Fatalf("notification text = %q, want the body the site's segment produced", envelope.Notice.Text)
		}
		if got := rig.facts.outputsAt(issuer); len(got) != 0 {
			t.Fatalf("segments at the issuing address = %d, want the delegated round to fly none of them", len(got))
		}
	})

	t.Run("settle_fires_exactly_once", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer"}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		rig.start(t, &convModel{name: "model-caller", answer: func(*model.Request) *model.Response {
			return convFinalTurn("the caller decides alone")
		}}, nil, issuer)

		if _, err := tool.Call(task.WithCallFrame(ctx, subagentCallFrame(issuer, "call-review")), reviewCallArgs()); err != nil {
			t.Fatalf("call the tool = %v, want the admission receipt", err)
		}
		first := awaitEnvelope(t, rig.arrived)
		if first.Notice.Kind != task.NoticeSettled {
			t.Fatalf("first notification = %+v, want the settled terminal of the one admitted call", first.Notice)
		}
		time.Sleep(100 * time.Millisecond)
		if delivered := countEnvelopes(rig.arrived); delivered != 0 {
			t.Fatalf("further notifications = %d, want one settle for one admitted round", delivered)
		}
		view := rig.ledger.View(rig.invocationID)
		if len(view) != 1 || view[0].Status != task.StatusSettled {
			t.Fatalf("delegation view = %+v, want the one round landed once", view)
		}
		if got := delegated.count(); got != 1 {
			t.Fatalf("delegated turns = %d, want the one round the settlement closed", got)
		}
	})
}

// delegatedFactLedger is the chain face one delegated round commits through: every fact as
// it committed, in commit order, read back per agent session the way the run's own ledger
// answers a chain.
//
// It keeps one record per commit and judges no identity: a commit under a key the ledger
// already names lands a second record here. So this fixture proves the order and the shape
// of the batch a round commits, never the same-key semantics of the real ledger — the
// same-key and restart anchors of the delegated materials run on the real assembly
// (tests/integration/capabilities TestDelegatedMaterialReachesTheRequest,
// tests/integration/host TestDelegatedMaterialSurvivesRestart).
type delegatedFactLedger struct {
	mu    sync.Mutex
	seq   int
	facts []event.Event
}

// commit keeps one fact as it stands, minting the identity of a fact that carries none.
func (l *delegatedFactLedger) commit(evt event.Event) (event.EventID, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if evt.Key == "" {
		l.seq++
		evt.Key = event.EventID(fmt.Sprintf("fact-delegated-%d", l.seq))
	}
	l.facts = append(l.facts, evt)
	return evt.Key, nil
}

// SessionHistory reports the transcript one session accumulated across the drives that
// ended on it: the case keeps its material on the chain, so the chain is the history.
func (l *delegatedFactLedger) SessionHistory(event.Address) []model.Message { return nil }

// CommitHistory keeps the transcript one ended drive accumulated: the chain already holds
// every message of it, so nothing is appended a second time here.
func (l *delegatedFactLedger) CommitHistory(event.Address, []model.Message) {}

// SessionFacts reports the facts one agent session committed, in commit order.
func (l *delegatedFactLedger) SessionFacts(addr event.Address) []event.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]event.Event, 0, len(l.facts))
	for _, evt := range l.facts {
		if chainSessionOf(evt) != addr.SessionID {
			continue
		}
		if addr.AgentID != "" && chainAgentOf(evt) != addr.AgentID {
			continue
		}
		out = append(out, evt)
	}
	return out
}

// outputsAt reports the output facts one agent session committed: the segments the
// instances of that address closed.
func (l *delegatedFactLedger) outputsAt(addr event.Address) []event.Event {
	var out []event.Event
	for _, evt := range l.SessionFacts(addr) {
		if evt.Type == event.TypeAgentOutput {
			out = append(out, evt)
		}
	}
	return out
}

// chainSessionOf reports the session one fact belongs to: the first address of the fact
// that names one.
func chainSessionOf(evt event.Event) event.SessionID {
	for _, addr := range []event.Address{evt.Owner, evt.Target, evt.Source} {
		if addr.SessionID != "" {
			return addr.SessionID
		}
	}
	return ""
}

// chainAgentOf reports the agent one fact belongs to: the first address of the fact that
// names one.
func chainAgentOf(evt event.Event) event.AgentID {
	for _, addr := range []event.Address{evt.Owner, evt.Target, evt.Source} {
		if addr.AgentID != "" {
			return addr.AgentID
		}
	}
	return ""
}

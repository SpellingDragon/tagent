package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegationAddressSeparation pins the two sites one delegated call keeps apart:
// - negative: the execution site the admitted task runs in is never the caller's own address
// - negative: the terminal notification returns to the address that issued the call, once
//   - the execution site names the assembled sub-agent inside the caller's session, keyed by the call that minted it
//   - the settle envelope carries the issuing address beside the settled identity of that one task
//
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegationAddressSeparation(t *testing.T) {
	t.Run("delegation_target_is_not_the_caller_address", func(t *testing.T) {
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer", gate: make(chan struct{}), started: make(chan struct{})}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		rig.start(t, &convModel{name: "model-caller", answer: func(*model.Request) *model.Response {
			return convFinalTurn("the caller decides alone")
		}}, nil, issuer)

		if _, err := tool.Call(task.WithCallFrame(context.Background(), subagentCallFrame(issuer, "call-review")), reviewCallArgs()); err != nil {
			t.Fatalf("call the tool = %v, want the admission receipt", err)
		}
		view := rig.ledger.View(rig.invocationID)
		if len(view) != 1 {
			t.Fatalf("delegation view = %+v, want the one admitted call", view)
		}
		site := view[0].Sites.Target
		if site == issuer {
			t.Fatalf("execution site = %v, want a site of its own beside the caller it was issued from", site)
		}
		if site.AgentID != "reviewer" {
			t.Fatalf("execution site agent = %q, want the assembled sub-agent %q", site.AgentID, "reviewer")
		}
		if !strings.Contains(string(site.SessionID), "#sub:call-review") {
			t.Fatalf("execution site session = %q, want the caller session keyed by the call that minted it", site.SessionID)
		}
		if !site.IsComplete() {
			t.Fatalf("execution site = %v, want a complete agent session", site)
		}
		if view[0].Sites.Issuer != issuer {
			t.Fatalf("issuer = %v, want the address that issued the call %v", view[0].Sites.Issuer, issuer)
		}
		close(delegated.gate)
		awaitEnvelope(t, rig.arrived)
	})

	t.Run("notice_returns_to_the_issuer", func(t *testing.T) {
		issuer := event.Address{AgentID: "agent-caller", SessionID: "session-caller"}
		delegated := &heldServeModel{name: "model-reviewer", gate: make(chan struct{}), started: make(chan struct{})}
		rig := newSubagentCallRig(t)
		tool := rig.newTool(t, delegatedBuilder(delegated))
		rig.start(t, &convModel{name: "model-caller", answer: func(*model.Request) *model.Response {
			return convFinalTurn("the caller decides alone")
		}}, nil, issuer)

		if _, err := tool.Call(task.WithCallFrame(context.Background(), subagentCallFrame(issuer, "call-review")), reviewCallArgs()); err != nil {
			t.Fatalf("call the tool = %v, want the admission receipt", err)
		}
		view := rig.ledger.View(rig.invocationID)
		if len(view) != 1 {
			t.Fatalf("delegation view = %+v, want the one admitted call", view)
		}
		select {
		case <-delegated.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the delegated turn never started beside the receipt")
		}
		close(delegated.gate)
		envelope := awaitEnvelope(t, rig.arrived)
		if envelope.Target != issuer {
			t.Fatalf("notification target = %v, want the address that issued the call %v", envelope.Target, issuer)
		}
		if envelope.Target == view[0].Sites.Target {
			t.Fatalf("notification target = %v, want it told apart from the execution site the turn ran in", envelope.Target)
		}
		if envelope.Source != issuer {
			t.Fatalf("notification source = %v, want the address that issued the call %v", envelope.Source, issuer)
		}
		if envelope.Notice.TaskID != view[0].TaskID || envelope.Notice.Kind != task.NoticeSettled {
			t.Fatalf("notification = %+v, want the settled identity of the one admitted call %s", envelope.Notice, view[0].TaskID)
		}
		if delivered := countEnvelopes(rig.arrived); delivered != 0 {
			t.Fatalf("further notifications = %d, want the settle to arrive exactly once", delivered)
		}
	})
}

// countEnvelopes reports how many further envelopes the deliverer already holds.
func countEnvelopes(ch chan task.Envelope) int {
	return len(ch)
}

package execution

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/query"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestReceiptAndBackgroundStaySeparate holds the admission receipt and the settled background result apart on one shared session.
// - one decision admits every call before any settlement wait, so a slow call never blocks the admission of its sibling
// - the receipt pairs each call with its admitted task before that task settles, beside no settled body
// - the settled body enters the next decision as its own observation: call identity preserved, body byte-identical, no second synchronous tool response
// - the next invocation of the session reads the shared history the drives already ended committed
// - the settled notification is booked acceptance, claim and consumption in the state mailbox
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestReceiptAndBackgroundStaySeparate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	slow := &sepSlowTool{entered: make(chan struct{}), gate: make(chan struct{})}
	fast := &sepFastTool{entered: make(chan struct{})}
	provider := &convModel{name: "model-sep", answer: func(req *model.Request) *model.Response {
		last := req.Messages[len(req.Messages)-1]
		switch {
		case len(req.Messages) == 1:
			return convToolTurn(convCall("call-a", "slow-tool", `{"wait":true}`), convCall("call-b", "fast-tool", `{}`))
		case last.Content == "turn-2":
			return convFinalTurn("done-b")
		default:
			return convFinalTurn("done-a")
		}
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-sep"}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{slow, fast}},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-sep"}
	scope := query.AuthorizationScope{Agents: []event.AgentID{"agent-a"}, Sessions: []event.Address{addr}}

	type driven struct {
		turn agent.Turn
		err  error
	}
	result := make(chan driven, 1)
	go func() {
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-sep-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "turn-1"}},
		})
		result <- driven{turn: turn, err: err}
	}()

	select {
	case <-slow.entered:
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the slow tool to enter execution")
	}
	select {
	case <-fast.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the sibling call was not admitted and executed while the slow call was still in flight: the admission loop awaited settlement serially")
	}
	live, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectTask}})
	if err != nil {
		t.Fatalf("query the live scene while the slow call is in flight: %v", err)
	}
	if len(live.Details) != 2 {
		t.Fatalf("task entries while the slow call is in flight = %d, want both calls of the decision already admitted before either settled", len(live.Details))
	}
	close(slow.gate)

	var turn agent.Turn
	select {
	case one := <-result:
		if one.err != nil {
			t.Fatalf("invoke the separation range: %v", one.err)
		}
		turn = one.turn
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the separation turn")
	}
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("turn = (%v, %d decisions), want the two decisions of the range ending completed", turn.Terminal, len(turn.Decisions))
	}
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("first invocation requests = %d, want one request per decision", len(requests))
	}
	replay := requests[1].Messages
	if len(replay) != 6 {
		t.Fatalf("second decision messages = %d (%+v), want the user input, the tool call turn, two receipts and two observations", len(replay), replay)
	}
	if calls := replay[1].ToolCalls; len(calls) != 2 || calls[0].ID != "call-a" || calls[1].ID != "call-b" {
		t.Fatalf("tool call turn = %+v, want the two native calls replayed in their own order", replay[1])
	}
	for i, want := range []struct {
		callID string
		tool   string
	}{{"call-a", "slow-tool"}, {"call-b", "fast-tool"}} {
		receipt := replay[2+i]
		if receipt.Role != model.RoleTool || receipt.ToolID != want.callID || receipt.ToolName != want.tool {
			t.Fatalf("receipt of %s = %+v, want the admission receipt paired under its own call identity", want.callID, receipt)
		}
		if !strings.Contains(receipt.Content, "accepted task-") || !strings.Contains(receipt.Content, "(running)") {
			t.Fatalf("receipt of %s = %q, want the admitted task named in its running state before any settlement", want.callID, receipt.Content)
		}
		if strings.Contains(receipt.Content, "bg-slow") || strings.Contains(receipt.Content, "bg-fast") {
			t.Fatalf("receipt of %s = %q, want no settled body beside the receipt: the receipt answers the call before its task settles", want.callID, receipt.Content)
		}
		if strings.Contains(receipt.Content, "the call was answered with") {
			t.Fatalf("receipt of %s = %q, want the paired content byte-identical beside no wrapper prefix", want.callID, receipt.Content)
		}
	}
	for i, want := range []struct {
		callID string
		tool   string
		body   string
	}{{"call-a", "slow-tool", "bg-slow"}, {"call-b", "fast-tool", "bg-fast"}} {
		obs := replay[4+i]
		if obs.Content != want.body {
			t.Fatalf("observation of %s = %q, want the settled body byte-identical beside no wrapper prefix", want.callID, obs.Content)
		}
		if obs.ToolID != want.callID || obs.ToolName != want.tool {
			t.Fatalf("observation of %s = %+v, want the native call identity preserved on the observation", want.callID, obs)
		}
		if obs.Role == model.RoleTool {
			t.Fatalf("observation of %s = %+v, want a new observation event beside no second synchronous tool response under the same call identity", want.callID, obs)
		}
	}
	for _, callID := range []string{"call-a", "call-b"} {
		answered := 0
		for _, msg := range replay {
			if msg.Role == model.RoleTool && msg.ToolID == callID {
				answered++
			}
		}
		if answered != 1 {
			t.Fatalf("tool role messages under %s = %d, want the single admission receipt and no second synchronous response", callID, answered)
		}
	}
	if slow.calls() != 1 || fast.calls() != 1 {
		t.Fatalf("tool executions = (%d, %d), want each admitted call executed exactly once", slow.calls(), fast.calls())
	}

	turnTwo, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-sep-2",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "turn-2"}},
	})
	if err != nil {
		t.Fatalf("invoke the second range of the session: %v", err)
	}
	if turnTwo.Terminal != model.KindCompleted || len(turnTwo.Decisions) != 1 {
		t.Fatalf("second turn = (%v, %d decisions), want one completed decision over the shared session history", turnTwo.Terminal, len(turnTwo.Decisions))
	}
	requests = provider.recorded()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want one request per decision of both invocations", len(requests))
	}
	shared := requests[2].Messages
	if len(shared) != 8 {
		t.Fatalf("second invocation messages = %d (%+v), want the committed history of the first drive beside its own input", len(shared), shared)
	}
	if tail := shared[len(shared)-1]; tail.Role != model.RoleUser || tail.Content != "turn-2" {
		t.Fatalf("second invocation tail = %+v, want its own input at the end beside no neighbour input", tail)
	}
	counts := map[string]int{}
	for _, msg := range shared {
		switch {
		case msg.Role == model.RoleUser && msg.Content == "turn-1":
			counts["first input"]++
		case msg.Role == model.RoleUser && msg.Content == "turn-2":
			counts["second input"]++
		case msg.Role == model.RoleAssistant && len(msg.ToolCalls) == 2:
			counts["tool call turn"]++
		case msg.Role == model.RoleTool && strings.Contains(msg.Content, "accepted task-"):
			counts["receipt"]++
		case msg.Content == "bg-slow":
			counts["slow observation"]++
		case msg.Content == "bg-fast":
			counts["fast observation"]++
		case msg.Role == model.RoleAssistant && msg.Content == "done-a":
			counts["final answer"]++
		default:
			counts["unexpected"]++
		}
	}
	wantCounts := map[string]int{"first input": 1, "second input": 1, "tool call turn": 1, "receipt": 2, "slow observation": 1, "fast observation": 1, "final answer": 1}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("second invocation message ledger = %v, want the full history of the first drive read back exactly once each", counts)
	}

	notices, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectNotice}})
	if err != nil {
		t.Fatalf("query the notice dimension: %v", err)
	}
	if len(notices.Details) != 2 {
		t.Fatalf("notice entries = %d, want one booked notification per settled call", len(notices.Details))
	}
	booked := map[event.TaskID]bool{}
	for _, detail := range notices.Details {
		if detail.Ref.Kind != query.ObjectNotice || detail.Ref.Notice.TaskID == "" {
			t.Fatalf("notice detail = %+v, want the notification identity of one settled call", detail.Ref)
		}
		booked[detail.Ref.Notice.TaskID] = true
		if detail.State != query.DetailRetired {
			t.Fatalf("notice %v state = %v, want consumed: only the state mailbox's own acceptance, claim and consumption records derive that phase, so a result that reached only the in-memory intake reads open here", detail.Ref.Notice, detail.State)
		}
	}
	if len(booked) != 2 {
		t.Fatalf("booked notification tasks = %v, want the two settled calls booked beside their own task identities", booked)
	}
}

// sepSlowTool is the native tool stand-in whose call blocks until the sub-case
// opens its gate, so its settlement lands after the sibling call of the same
// decision was already admitted and answered.
type sepSlowTool struct {
	mu      sync.Mutex
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
	count   int
}

// Declaration returns the declaration naming the slow tool.
func (t *sepSlowTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "slow-tool"}
}

// Call signals its entry, waits for the gate and reports its settled body.
func (t *sepSlowTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	t.count++
	t.mu.Unlock()
	t.once.Do(func() { close(t.entered) })
	<-t.gate
	return "bg-slow", nil
}

// calls reports how often the slow tool executed.
func (t *sepSlowTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// sepFastTool is the native tool stand-in whose call settles immediately, so
// its execution is observable while the slow call of the same decision is still
// in flight.
type sepFastTool struct {
	mu      sync.Mutex
	entered chan struct{}
	once    sync.Once
	count   int
}

// Declaration returns the declaration naming the fast tool.
func (t *sepFastTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "fast-tool"}
}

// Call signals its entry and reports its settled body.
func (t *sepFastTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	t.count++
	t.mu.Unlock()
	t.once.Do(func() { close(t.entered) })
	return "bg-fast", nil
}

// calls reports how often the fast tool executed.
func (t *sepFastTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

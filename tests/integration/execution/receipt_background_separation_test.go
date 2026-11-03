package execution

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestReceiptAndBackgroundStaySeparate holds the admission receipt and the settled background result apart on one shared session.
// - one decision admits every call before any settlement wait, so a slow call never blocks the admission of its sibling
// - the receipt pairs each call with its admitted task before that task settles, beside no settled body
// - the settled body enters the next decision as its own observation: call identity preserved, body byte-identical, no second synchronous tool response
// - the next invocation of the session reads the shared history the drives already ended committed
// - the settled notification is booked acceptance, claim and consumption in the state mailbox
// - R23 迁移：requests[2] 断言随 RoleOf 契约（request_assembly.go "a replayed settled fact renders under the tool role"），意图保留于 requests[1]
// 契约: docs/wiki/agent/execution-generations.md#turn-local-execution-face
func TestReceiptAndBackgroundStaySeparate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	slow := &sepSlowTool{entered: make(chan struct{}), gate: make(chan struct{})}
	fast := &sepFastTool{entered: make(chan struct{})}
	provider := &convModel{name: "model-sep", answer: func(req *model.Request) *model.Response {
		if len(req.Messages) == 1 {
			return convToolTurn(convCall("call-a", "slow-tool", `{"wait":true}`), convCall("call-b", "fast-tool", `{}`))
		}
		switch latestSepInput(req.Messages) {
		case "turn-2":
			return convFinalTurn("done-b")
		case "turn-3":
			return convFinalTurn("done-c")
		}
		if sepCarries(req.Messages, "done-a") {
			return convFinalTurn("done-settled")
		}
		return convFinalTurn("done-a")
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-sep"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{slow, fast}},
	}, assembly.WithGovernance(passPolicy{}))
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
		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
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
	if turn.Terminal != model.KindCompleted || turn.DecisionCount < 2 {
		t.Fatalf("turn = (%v, %d decisions), want the two decisions of the range ending completed", turn.Terminal, turn.DecisionCount)
	}
	requests := provider.recorded()
	if len(requests) < 2 || len(requests) > 3 {
		t.Fatalf("first invocation requests = %d, want one request per decision beside at most one closing decision", len(requests))
	}
	// Full-duplex consumption may land a late settlement in the closing decision
	// rather than the receipts round, so the pairs are read across the requests
	// that follow the first decision.
	var replay []model.Message
	for _, req := range requests[1:] {
		replay = append(replay, req.Messages...)
	}
	var callTurn model.Message
	for _, msg := range replay {
		if len(msg.ToolCalls) == 2 && msg.ToolCalls[0].ID == "call-a" && msg.ToolCalls[1].ID == "call-b" {
			callTurn = msg
			break
		}
	}
	if callTurn.Role != model.RoleAssistant {
		t.Fatalf("tool call turn = %+v, want the two native calls replayed in their own order", callTurn)
	}
	for _, want := range []struct {
		callID string
		tool   string
	}{{"call-a", "slow-tool"}, {"call-b", "fast-tool"}} {
		var receipt model.Message
		for _, msg := range replay {
			if msg.Role == model.RoleTool && msg.ToolID == want.callID {
				receipt = msg
				break
			}
		}
		if receipt.Role != model.RoleTool || receipt.ToolID != want.callID || receipt.ToolName != want.tool {
			t.Fatalf("receipt of %s = %+v, want the admission receipt paired under its own call identity", want.callID, receipt)
		}
		if receipt.Content != "ok" {
			t.Fatalf("receipt of %s = %q, want the minimal ok confirmation before any settlement", want.callID, receipt.Content)
		}
		if strings.Contains(receipt.Content, "bg-slow") || strings.Contains(receipt.Content, "bg-fast") {
			t.Fatalf("receipt of %s = %q, want no settled body beside the receipt: the receipt answers the call before its task settles", want.callID, receipt.Content)
		}
		if strings.Contains(receipt.Content, "the call was answered with") {
			t.Fatalf("receipt of %s = %q, want the paired content byte-identical beside no wrapper prefix", want.callID, receipt.Content)
		}
	}
	for _, want := range []struct {
		callID string
		tool   string
		body   string
	}{{"call-a", "slow-tool", "bg-slow"}, {"call-b", "fast-tool", "bg-fast"}} {
		var obs model.Message
		for _, msg := range replay {
			if msg.Role != model.RoleTool && msg.ToolID == want.callID && msg.Content == want.body {
				obs = msg
				break
			}
		}
		if obs.ToolID == "" {
			continue
		}
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
	for at, req := range requests {
		slots := map[string]int{}
		for _, msg := range req.Messages {
			if msg.Role == model.RoleTool && msg.ToolID != "" {
				slots[msg.ToolID]++
			}
		}
		for callID, n := range slots {
			if n > 1 {
				t.Fatalf("request %d carries %d tool-role messages under call %s, want at most one slot per call per request", at, n, callID)
			}
		}
	}
	closingView := requests[len(requests)-1].Messages
	for _, callID := range []string{"call-a", "call-b"} {
		answered := 0
		for _, msg := range closingView {
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

	turnTwo, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-sep-2",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "turn-2"}},
	})
	if err != nil {
		t.Fatalf("invoke the second range of the session: %v", err)
	}
	if turnTwo.Terminal != model.KindCompleted || turnTwo.DecisionCount < 1 {
		t.Fatalf("second turn = (%v, %d decisions), want one completed decision over the shared session history", turnTwo.Terminal, turnTwo.DecisionCount)
	}
	turnThree, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-sep-3",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "turn-3"}},
	})
	if err != nil {
		t.Fatalf("invoke the flush range of the session: %v", err)
	}
	if turnThree.Terminal != model.KindCompleted {
		t.Fatalf("flush turn = (%v, %d decisions), want the completed turn that reads the restored history", turnThree.Terminal, turnThree.DecisionCount)
	}
	requests = provider.recorded()
	if len(requests) < 3 {
		t.Fatalf("requests = %d, want the decisions of the driven invocations", len(requests))
	}
	shared := awaitSepFlush(t, provider)
	latest := ""
	for _, msg := range shared {
		if msg.Role == model.RoleUser && (msg.Content == "turn-1" || msg.Content == "turn-2" || msg.Content == "turn-3") {
			latest = msg.Content
		}
	}
	if latest != "turn-3" {
		t.Fatalf("latest user input of the flush view = %q, want its own input beside no neighbour input after it", latest)
	}
	for _, msg := range shared {
		if msg.Content == "ok" {
			t.Fatalf("restored message = %+v, want no admission receipt restored on the projection face: the receipt is the in-flight confirmation requests[1] holds, not a committed fact", msg)
		}
	}
	counts := map[string]int{}
	answers := 0
	for _, msg := range shared {
		switch {
		case msg.Role == model.RoleUser && msg.Content == "turn-1":
			counts["first input"]++
		case msg.Role == model.RoleUser && msg.Content == "turn-2":
			counts["second input"]++
		case msg.Role == model.RoleUser && msg.Content == "turn-3":
			counts["third input"]++
		case msg.Role == model.RoleAssistant && len(msg.ToolCalls) == 2:
			counts["tool call turn"]++
		case msg.Role == model.RoleTool && msg.ToolID == "call-a" && msg.ToolName == "slow-tool" && msg.Content == "bg-slow":
			counts["slow settled body"]++
		case msg.Role == model.RoleTool && msg.ToolID == "call-b" && msg.ToolName == "fast-tool" && msg.Content == "bg-fast":
			counts["fast settled body"]++
		case msg.Role == model.RoleAssistant:
			answers++
		default:
			counts["unexpected"]++
		}
	}
	wantCounts := map[string]int{"first input": 1, "second input": 1, "third input": 1, "tool call turn": 1, "slow settled body": 1, "fast settled body": 1}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("second invocation message ledger = %v, want the full history of the first drive read back exactly once each", counts)
	}
	if answers < 2 {
		t.Fatalf("history answers = %d, want the driven turns the session already ended", answers)
	}
	for _, body := range []string{"done-a", "done-b"} {
		if !sepCarries(shared, body) {
			t.Fatalf("history carries no %q answer of the driven turns", body)
		}
	}

	// The consumption receipts of a settlement the loop still drives land a
	// beat after its delivery, so the booked phase is awaited inside a bound
	// rather than read at the instant the last input answered.
	var notices query.Snapshot
	deadline := time.Now().Add(15 * time.Second)
	for {
		notices, err = run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectNotice}})
		if err != nil {
			t.Fatalf("query the notice dimension: %v", err)
		}
		settled := len(notices.Details) == 2
		for _, detail := range notices.Details {
			if detail.State != query.DetailRetired {
				settled = false
			}
		}
		if settled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("notice entries = %d, want one booked notification per settled call beside its consumption receipt", len(notices.Details))
		}
		time.Sleep(2 * time.Millisecond)
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

// awaitSepFlush reports the last request view that presents the three inputs
// beside both settled bodies, waiting for the loop to present them: a
// settlement-driven range may stand beside the flush range, so the view the
// session ends on is decided by the material it carries, never by arrival order.
func awaitSepFlush(t *testing.T, provider *convModel) []model.Message {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		requests := provider.recorded()
		for at := len(requests) - 1; at >= 0; at-- {
			msgs := requests[at].Messages
			if sepCarries(msgs, "turn-1") && sepCarries(msgs, "turn-2") && sepCarries(msgs, "turn-3") &&
				sepCarries(msgs, "bg-slow") && sepCarries(msgs, "bg-fast") {
				return msgs
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no request presents the three inputs beside both settled bodies within the wait")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// sepCarries reports whether one request already carries a body or input, so
// the history reads one authority for the material it pins.
func sepCarries(messages []model.Message, content string) bool {
	for _, message := range messages {
		if message.Content == content {
			return true
		}
	}
	return false
}

// latestSepInput reports the latest user turn one request carries, so the
// scripted answer names the round it answers whatever settlement material the
// full-duplex loop appended after that round's input.
func latestSepInput(messages []model.Message) string {
	for at := len(messages) - 1; at >= 0; at-- {
		if messages[at].Role != model.RoleUser {
			continue
		}
		switch messages[at].Content {
		case "turn-2", "turn-3":
			return messages[at].Content
		}
	}
	return ""
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

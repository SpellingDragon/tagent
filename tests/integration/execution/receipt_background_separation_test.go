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
// - each settled call lands the delivery fact of its own task as a settle line of its own
// - it stands beside the body the call returned, so a call answers once and is noticed once
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
		slots := sepToolSlots(req.Messages)
		for callID, n := range slots {
			if n > 1 {
				t.Fatalf("request %d carries %d tool-role messages under call %s, want at most one slot per call per request", at, n, callID)
			}
		}
	}
	closingView := requests[len(requests)-1].Messages
	closingSlots := sepToolSlots(closingView)
	for _, callID := range []string{"call-a", "call-b"} {
		if closingSlots[callID] != 1 {
			t.Fatalf("tool role messages under %s = %d, want the single admission receipt and no second synchronous response", callID, closingSlots[callID])
		}
	}
	if slow.calls() != 1 || fast.calls() != 1 {
		t.Fatalf("tool executions = (%d, %d), want each admitted call executed exactly once", slow.calls(), fast.calls())
	}
	awaitSepNoticesBooked(t, ctx, run, scope)

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
	for callID, n := range sepToolSlots(shared) {
		if n > 1 {
			t.Fatalf("the flush view carries %d tool-role messages under call %s, want at most one slot per call", n, callID)
		}
	}
	latest := ""
	for _, msg := range shared {
		if material := withoutPresentedTicket(msg.Content); msg.Role == model.RoleUser &&
			(material == "turn-1" || material == "turn-2" || material == "turn-3") {
			latest = material
		}
	}
	if latest != "turn-3" {
		t.Fatalf("latest user input of the flush view = %q, want its own input beside no neighbour input after it", latest)
	}
	for _, msg := range shared {
		if withoutPresentedTicket(msg.Content) == "ok" {
			t.Fatalf("restored message = %+v, want no admission receipt restored on the projection face: the receipt is the in-flight confirmation requests[1] holds, not a committed fact", msg)
		}
	}
	counts := map[string]int{}
	answers := 0
	settleNotices := map[string]event.TaskID{}
	for _, msg := range shared {
		material := withoutPresentedTicket(msg.Content)
		switch {
		case msg.Role == model.RoleUser && material == "turn-1":
			counts["first input"]++
		case msg.Role == model.RoleUser && material == "turn-2":
			counts["second input"]++
		case msg.Role == model.RoleUser && material == "turn-3":
			counts["third input"]++
		case msg.Role == model.RoleAssistant && len(msg.ToolCalls) == 2:
			counts["tool call turn"]++
		case msg.Role == model.RoleTool && msg.ToolID == "call-a" && msg.ToolName == "slow-tool" && material == "bg-slow":
			counts["slow settled body"]++
		case msg.Role == model.RoleTool && msg.ToolID == "call-b" && msg.ToolName == "fast-tool" && material == "bg-fast":
			counts["fast settled body"]++
		case msg.Role == model.RoleTool && msg.ToolID != "" && sepPresentedTicketKind(msg.Content) == event.TypeTaskSettled:
			noticeTask, ok := sepSettleNotice(msg)
			if !ok {
				t.Fatalf("settle notice under %s = %+v, want the settle line of its own task beside no result text", msg.ToolID, msg)
			}
			settleNotices[msg.ToolID] = noticeTask
			counts[msg.ToolID+" settle notice"]++
		case msg.Role == model.RoleAssistant:
			answers++
		default:
			counts["unexpected"]++
		}
	}
	wantCounts := map[string]int{"first input": 1, "second input": 1, "third input": 1, "tool call turn": 1,
		"slow settled body": 1, "fast settled body": 1, "call-a settle notice": 1, "call-b settle notice": 1}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("second invocation message ledger = %v, want the full history of the first drive read back exactly once each", counts)
	}
	if settleNotices["call-a"] == settleNotices["call-b"] {
		t.Fatalf("settle notices read as %v, want the task of its own under each settled call", settleNotices)
	}
	if answers < 2 {
		t.Fatalf("history answers = %d, want the driven turns the session already ended", answers)
	}
	for _, body := range []string{"done-a", "done-b"} {
		if !sepCarries(shared, body) {
			t.Fatalf("history carries no %q answer of the driven turns", body)
		}
	}

	notices := awaitSepNoticesBooked(t, ctx, run, scope)
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

// sepToolSlots counts the tool-role messages a view carries under each call, leaving the
// settle notice of a call's own task out: that line is the delivery fact of the settlement
// rather than an answer to the call, so the one-slot rule counts answers only.
func sepToolSlots(view []model.Message) map[string]int {
	slots := map[string]int{}
	for _, msg := range view {
		if msg.Role != model.RoleTool || msg.ToolID == "" {
			continue
		}
		if sepPresentedTicketKind(msg.Content) == event.TypeTaskSettled {
			continue
		}
		slots[msg.ToolID]++
	}
	return slots
}

// sepPresentedTicketKind reports the vocabulary a presented message opens with: the ticket
// names the fact and the kind names what it states, so a check reads one family of messages
// out of a view that carries several. A message opening with no ticket carries none.
func sepPresentedTicketKind(content string) string {
	if !strings.HasPrefix(content, "[evt_") {
		return ""
	}
	bar := strings.Index(content, "|")
	end := strings.Index(content, "] ")
	if bar < 0 || end < 0 || end < bar {
		return ""
	}
	return content[bar+1 : end]
}

// sepSettleNotice reports the task the settle notice of one call stands for. The line
// states the task and the kind alone: the result the call returned already stands under the
// call identity, so a notice carrying a result text is no settle line and is refused here.
func sepSettleNotice(msg model.Message) (event.TaskID, bool) {
	const line = "tagent: task "
	body := withoutPresentedTicket(msg.Content)
	if !strings.HasPrefix(body, line) || !strings.HasSuffix(body, " settled") {
		return "", false
	}
	if strings.Contains(body, "result:") {
		return "", false
	}
	taskID := event.TaskID(strings.TrimSuffix(strings.TrimPrefix(body, line), " settled"))
	if taskID == "" {
		return "", false
	}
	return taskID, true
}

// awaitSepFlush reports the last request view that presents the three inputs,
// both settled bodies and the settle line of each call's own task, waiting for
// the loop to present them: a settlement-driven range may stand beside the flush
// range, so the view the session ends on is decided by the material it carries,
// never by arrival order. The families waited for are the ones the ledger below
// reads: a view that answers this wait but misses a family would fail the ledger
// on arrival order alone.
func awaitSepFlush(t *testing.T, provider *convModel) []model.Message {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		requests := provider.recorded()
		for at := len(requests) - 1; at >= 0; at-- {
			msgs := requests[at].Messages
			if sepCarries(msgs, "turn-1") && sepCarries(msgs, "turn-2") && sepCarries(msgs, "turn-3") &&
				sepCarries(msgs, "bg-slow") && sepCarries(msgs, "bg-fast") &&
				sepCarriesSettleNotice(msgs, "call-a") && sepCarriesSettleNotice(msgs, "call-b") {
				return msgs
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no request presents the three inputs beside both settled bodies and their settle notices within the wait")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// sepCarriesSettleNotice reports whether one request already carries the settle line of the
// call's own task under its call identity: a view that holds the settled body but not that
// line stands before the delivery landed, so it is not the flush view the ledger reads.
func sepCarriesSettleNotice(messages []model.Message, callID string) bool {
	for _, message := range messages {
		if message.Role != model.RoleTool || message.ToolID != callID {
			continue
		}
		if sepPresentedTicketKind(message.Content) != event.TypeTaskSettled {
			continue
		}
		if _, ok := sepSettleNotice(message); ok {
			return true
		}
	}
	return false
}

// awaitSepNoticesBooked waits until the state mailbox reports both settled calls
// booked and consumed, and reports the snapshot it read: the booked phase lands a
// beat after the delivery, so a reader that needs it waits inside a bound rather
// than sampling the instant the answer landed.
func awaitSepNoticesBooked(t *testing.T, ctx context.Context, run *assembly.Run, scope query.AuthorizationScope) query.Snapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		notices, err := run.Query(ctx, query.ListFilter{Scope: scope, Kinds: []query.ObjectKind{query.ObjectNotice}})
		if err != nil {
			t.Fatalf("query the notice dimension: %v", err)
		}
		booked := len(notices.Details) == 2
		for _, detail := range notices.Details {
			if detail.State != query.DetailRetired {
				booked = false
			}
		}
		if booked {
			return notices
		}
		if time.Now().After(deadline) {
			t.Fatalf("notice entries = %d, want one booked notification per settled call beside its consumption receipt", len(notices.Details))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// sepCarries reports whether one request already carries a body or input, so
// the history reads one authority for the material it pins.
func sepCarries(messages []model.Message, content string) bool {
	for _, message := range messages {
		if withoutPresentedTicket(message.Content) == content {
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
		switch material := withoutPresentedTicket(messages[at].Content); material {
		case "turn-2", "turn-3":
			return material
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

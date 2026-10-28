package execution

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// passPolicy is the deterministic stand-in of the real governance policy the
// formal assembly is configured with in these checks: the fixed passing
// dispositions are mechanism inputs carrying no quality conclusion, and the
// formal quality assessment lives in the structural evaluation gate.
type passPolicy struct{}

// Check implements state.Governance with one passing disposition per required
// check.
func (passPolicy) Check(context.Context, state.StatePlan) (map[state.CheckName]state.CheckAnswer, error) {
	return map[state.CheckName]state.CheckAnswer{
		"eval":       passingCheck("eval"),
		"benchmark":  passingCheck("benchmark"),
		"apply-gate": passingCheck("apply-gate"),
	}, nil
}

// passingCheck reports the passing disposition of one named mechanism check.
func passingCheck(name state.CheckName) state.CheckAnswer {
	return state.CheckAnswer{Name: name, Verdict: state.CheckPass, Reason: "the stand-in passes the mechanism check", Scope: "assembly", Evidence: []string{"deterministic stand-in"}}
}

// TestNativeConversationRoundTrip checks the native conversation roundtrip of the persistent event loop through the minimal public assembly entry.
// - a native multi-turn tool call roundtrip keeps the messages and the pairings, each call answered by its own paired receipt
// - a paired receipt carries the admission and the task id of its call, never the settled result of that call
// - two invocations of one session decide independently: their requests are never batched and a failed terminal never carries its neighbour down
// - each request ends on its own invocation's input while the session history already committed may sit in front of it
// - a settled task backflow drives the next decision of the invocation that issued the call on the binding generation effective when that decision begins
// - each settled result enters the next decision as its own observation under the preserved call identifier, never a second tool response
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestNativeConversationRoundTrip(t *testing.T) {
	t.Run("a native multi-turn tool-call roundtrip keeps the messages and the pairings", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(
					convCall("call-1", "echo-a", `{"x":1}`),
					convCall("call-2", "echo-b", `{"y":"two"}`),
				)
			}
			return convFinalTurn("settled")
		}}
		toolA := &convTool{name: "echo-a", output: "out-a"}
		toolB := &convTool{name: "echo-b", output: "out-b"}
		decl := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
		}
		run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{toolA, toolB}},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}

		turn, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-roundtrip",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-roundtrip"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the roundtrip: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if turn.InvocationID != "inv-roundtrip" {
			t.Fatalf("turn invocation = %q, want the invocation the drive was admitted for", turn.InvocationID)
		}

		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want one request per decision", len(requests))
		}
		first := requests[0].Messages
		if len(first) != 1 || first[0].Role != model.RoleUser || first[0].Content != "hello" {
			t.Fatalf("first request messages = %+v, want the one native user message kept as it stands", first)
		}
		if len(requests[0].Tools) != 2 || requests[0].Tools["echo-a"] == nil || requests[0].Tools["echo-b"] == nil {
			t.Fatalf("first request tools = %v, want both tools of the borrowed generation visible to the model", requests[0].Tools)
		}

		second := requests[1].Messages
		if len(second) != 6 {
			t.Fatalf("second request messages = %d (%+v), want user, assistant, one admission receipt and one settled observation per call", len(second), second)
		}
		if second[0].Role != model.RoleUser || second[0].Content != "hello" {
			t.Fatalf("second request first message = %+v, want the original user message replayed unchanged", second[0])
		}
		assistant := second[1]
		if assistant.Role != model.RoleAssistant || len(assistant.ToolCalls) != 2 {
			t.Fatalf("second request assistant message = %+v, want the native assistant message carrying both tool calls", assistant)
		}
		if assistant.ToolCalls[0].ID != "call-1" || assistant.ToolCalls[0].Function.Name != "echo-a" ||
			string(assistant.ToolCalls[0].Function.Arguments) != `{"x":1}` {
			t.Fatalf("first tool call = %+v, want the native call kept byte-identical", assistant.ToolCalls[0])
		}
		if assistant.ToolCalls[1].ID != "call-2" || assistant.ToolCalls[1].Function.Name != "echo-b" ||
			string(assistant.ToolCalls[1].Function.Arguments) != `{"y":"two"}` {
			t.Fatalf("second tool call = %+v, want the native call kept byte-identical", assistant.ToolCalls[1])
		}
		if receipt := second[2]; receipt.Role != model.RoleTool || receipt.ToolID != "call-1" || receipt.ToolName != "echo-a" ||
			receipt.Content != "ok" ||
			strings.Contains(receipt.Content, "out-a") || strings.Contains(receipt.Content, "the call was answered with") {
			t.Fatalf("first paired tool message = %+v, want the admission receipt of call-1 paired under its own tool id", receipt)
		}
		if receipt := second[3]; receipt.Role != model.RoleTool || receipt.ToolID != "call-2" || receipt.ToolName != "echo-b" ||
			receipt.Content != "ok" ||
			strings.Contains(receipt.Content, "out-b") || strings.Contains(receipt.Content, "the call was answered with") {
			t.Fatalf("second paired tool message = %+v, want the admission receipt of call-2 paired under its own tool id", receipt)
		}
		if obs := second[4]; obs.Content != "out-a" || obs.ToolID != "call-1" || obs.ToolName != "echo-a" || obs.Role == model.RoleTool {
			t.Fatalf("first settled observation = %+v, want the result of call-1 kept byte-identical as its own observation", obs)
		}
		if obs := second[5]; obs.Content != "out-b" || obs.ToolID != "call-2" || obs.ToolName != "echo-b" || obs.Role == model.RoleTool {
			t.Fatalf("second settled observation = %+v, want the result of call-2 kept byte-identical as its own observation", obs)
		}
		if toolA.calls() != 1 || toolB.calls() != 1 {
			t.Fatalf("tool executions = (%d, %d), want each admitted call executed exactly once", toolA.calls(), toolB.calls())
		}
		if toolA.args() != `{"x":1}` || toolB.args() != `{"y":"two"}` {
			t.Fatalf("tool arguments = (%q, %q), want the native arguments delivered byte-identical", toolA.args(), toolB.args())
		}
		if turn.Decisions[0].Terminal != model.KindCompleted || turn.Decisions[0].Binding.Model != provider {
			t.Fatalf("first decision = %+v, want the completed terminal of the borrowed generation", turn.Decisions[0])
		}
		if turn.Decisions[1].Terminal != model.KindCompleted || turn.Decisions[1].Binding.Model != provider {
			t.Fatalf("second decision = %+v, want the completed terminal of the same generation", turn.Decisions[1])
		}
	})

	t.Run("two invocations of one session decide independently and never batch", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &convModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if last := req.Messages[len(req.Messages)-1]; last.Content == "turn-a" {
				return convFailTurn("provider refused")
			}
			return convFinalTurn("b-done")
		}}
		decl := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
		}
		run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: provider},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}

		address := event.Address{AgentID: "agent-a", SessionID: "session-shared"}
		type driven struct {
			invocation event.InvocationID
			turn       agent.Turn
			err        error
		}
		results := make(chan driven, 2)
		for _, req := range []struct {
			id    event.InvocationID
			input string
		}{
			{"inv-a", "turn-a"},
			{"inv-b", "turn-b"},
		} {
			go func(id event.InvocationID, input string) {
				turn, err := run.Invoke(ctx, assembly.InvokeRequest{
					InvocationID: id,
					Address:      address,
					Input:        []model.Message{{Role: model.RoleUser, Content: input}},
				})
				results <- driven{invocation: id, turn: turn, err: err}
			}(req.id, req.input)
		}
		got := map[event.InvocationID]driven{}
		for i := 0; i < 2; i++ {
			select {
			case one := <-results:
				if one.err != nil {
					t.Fatalf("invoke %s: %v", one.invocation, one.err)
				}
				got[one.invocation] = one
			case <-time.After(20 * time.Second):
				t.Fatal("timed out waiting for the two invocations of the shared session")
			}
		}

		turnA, turnB := got["inv-a"].turn, got["inv-b"].turn
		if turnA.Terminal != model.KindFailed || len(turnA.Decisions) != 1 || turnA.Decisions[0].Terminal != model.KindFailed {
			t.Fatalf("turn of inv-a = (%v, %+v), want its own failed terminal and no neighbour attached", turnA.Terminal, turnA.Decisions)
		}
		if turnB.Terminal != model.KindCompleted || len(turnB.Decisions) != 1 || turnB.Decisions[0].Terminal != model.KindCompleted {
			t.Fatalf("turn of inv-b = (%v, %+v), want its own completed terminal beside the neighbour's failure", turnB.Terminal, turnB.Decisions)
		}
		if turnA.InvocationID != "inv-a" || turnB.InvocationID != "inv-b" {
			t.Fatalf("turn invocations = (%q, %q), want each decision recorded under its own range", turnA.InvocationID, turnB.InvocationID)
		}

		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want one request per invocation's own decision", len(requests))
		}
		seen := map[string]bool{}
		for _, req := range requests {
			if len(req.Messages) == 0 {
				t.Fatalf("request messages = %+v, want the invocation's own input at the end of its request", req.Messages)
			}
			tail := req.Messages[len(req.Messages)-1]
			if tail.Role != model.RoleUser {
				t.Fatalf("request tail = %+v, want the invocation's own user input at the end of its request", tail)
			}
			seen[tail.Content] = true
		}
		if !seen["turn-a"] || !seen["turn-b"] {
			t.Fatalf("request contents = %v, want the input of each invocation in its own request", seen)
		}
	})

	t.Run("a settled task backflow drives the next decision on the generation effective then", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		gated := &convGatedTool{name: "gated-tool", entered: make(chan struct{}), gate: make(chan struct{})}
		first := &convModel{name: "model-gen-1", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return convToolTurn(convCall("call-g1", "gated-tool", `{"wait":true}`))
			}
			return convFinalTurn("unexpected second request on gen-1")
		}}
		second := &convModel{name: "model-gen-2", answer: func(*model.Request) *model.Response {
			return convFinalTurn("settled at gen-2")
		}}
		declOne := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-gen-1"}},
		}
		declTwo := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-gen-2"}},
		}
		run, err := assembly.Assemble(ctx, declOne, map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: first, Tools: []trpctool.CallableTool{gated}},
		}, assembly.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}

		type driven struct {
			turn agent.Turn
			err  error
		}
		result := make(chan driven, 1)
		go func() {
			turn, err := run.Invoke(ctx, assembly.InvokeRequest{
				InvocationID: "inv-backflow",
				Address:      event.Address{AgentID: "agent-a", SessionID: "session-backflow"},
				Input:        []model.Message{{Role: model.RoleUser, Content: "run"}},
			})
			result <- driven{turn: turn, err: err}
		}()
		select {
		case <-gated.entered:
		case <-time.After(20 * time.Second):
			t.Fatal("timed out waiting for the gated tool to be entered")
		}

		published, err := run.Publish(ctx, declTwo, map[event.AgentID]assembly.NativeAgent{
			"agent-a": {Model: second, Tools: []trpctool.CallableTool{&convTool{name: "gated-tool", output: "never"}}},
		})
		if err != nil {
			t.Fatalf("publish the second generation: %v", err)
		}
		if published.Result != state.PublishApplied {
			t.Fatalf("publication result = %v, want the changed declaration applied as one new generation", published.Result)
		}
		close(gated.gate)

		var turn agent.Turn
		select {
		case one := <-result:
			if one.err != nil {
				t.Fatalf("invoke the backflow range: %v", one.err)
			}
			turn = one.turn
		case <-time.After(20 * time.Second):
			t.Fatal("timed out waiting for the backflow-driven turn")
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the settled backflow driving one more completed decision", turn.Terminal, len(turn.Decisions))
		}
		if turn.Decisions[0].Binding.Model != first {
			t.Fatalf("first decision model = %v, want the generation effective when the turn began", turn.Decisions[0].Binding.Model)
		}
		if turn.Decisions[1].Binding.Model != second {
			t.Fatalf("backflow decision model = %v, want the generation effective when the backflow decision began", turn.Decisions[1].Binding.Model)
		}
		requests := second.recorded()
		if len(requests) != 1 {
			t.Fatalf("second-generation requests = %d, want the backflow decision served by the current generation", len(requests))
		}
		replay := requests[0].Messages
		if len(replay) != 4 {
			t.Fatalf("backflow request messages = %d (%+v), want user, assistant tool call, its admission receipt and the settled observation", len(replay), replay)
		}
		if replay[0].Role != model.RoleUser || replay[0].Content != "run" {
			t.Fatalf("backflow request first message = %+v, want the original user message", replay[0])
		}
		if replay[1].Role != model.RoleAssistant || len(replay[1].ToolCalls) != 1 || replay[1].ToolCalls[0].ID != "call-g1" {
			t.Fatalf("backflow request assistant message = %+v, want the native tool call replayed", replay[1])
		}
		if receipt := replay[2]; receipt.Role != model.RoleTool || receipt.ToolID != "call-g1" || receipt.ToolName != "gated-tool" ||
			receipt.Content != "ok" || strings.Contains(receipt.Content, "released") {
			t.Fatalf("backflow request receipt = %+v, want the admission receipt of call-g1 paired under its own tool id", receipt)
		}
		if obs := replay[3]; obs.Content != "released" || obs.ToolID != "call-g1" || obs.ToolName != "gated-tool" || obs.Role == model.RoleTool {
			t.Fatalf("backflow request observation = %+v, want the settled body of call-g1 kept byte-identical as its observation", obs)
		}
		if first.recorded() == nil || len(first.recorded()) != 1 {
			t.Fatalf("first-generation requests = %d, want exactly the first decision of the turn", len(first.recorded()))
		}
		if gated.calls() != 1 {
			t.Fatalf("gated tool executions = %d, want the admitted call executed once", gated.calls())
		}
	})
}

// convModel is the fake provider stand-in of the roundtrip: it records every
// native request it is driven with and answers each with the scripted response
// the sub-case names. It never reaches a real endpoint.
type convModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the native request and streams the scripted response.
func (m *convModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the fake provider's descriptor.
func (m *convModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the fake provider was driven with.
func (m *convModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// convTool is the native tool stand-in of the roundtrip: it records how often
// it ran and which arguments it received and answers with its output.
type convTool struct {
	mu       sync.Mutex
	name     string
	output   string
	count    int
	lastArgs string
}

// Declaration returns the declaration naming the fake tool.
func (t *convTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the fake tool once and reports its output.
func (t *convTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count++
	t.lastArgs = string(jsonArgs)
	return t.output, nil
}

// calls reports how often the fake tool executed.
func (t *convTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// args reports the native arguments the fake tool last received.
func (t *convTool) args() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastArgs
}

// convGatedTool is the native tool stand-in whose call blocks until the
// sub-case opens its gate, so a publication can land between the admission and
// the settled backflow.
type convGatedTool struct {
	mu      sync.Mutex
	name    string
	entered chan struct{}
	gate    chan struct{}
	count   int
}

// Declaration returns the declaration naming the gated tool.
func (t *convGatedTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call signals its entry and waits for the gate before reporting its output.
func (t *convGatedTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	t.mu.Lock()
	t.count++
	t.mu.Unlock()
	close(t.entered)
	<-t.gate
	return "released", nil
}

// calls reports how often the gated tool executed.
func (t *convGatedTool) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// convCall builds one native tool call of a scripted assistant message.
func convCall(id, name, args string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      name,
			Arguments: []byte(args),
		},
	}
}

// convToolTurn builds the scripted terminal response carrying native tool calls.
func convToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Done:    true,
	}
}

// convFinalTurn builds the scripted terminal response carrying the final answer.
func convFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

// convFailTurn builds the scripted terminal response carrying a provider error.
func convFailTurn(message string) *model.Response {
	return &model.Response{
		Done:  true,
		Error: &model.ResponseError{Message: message, Type: model.ErrorTypeAPIError},
	}
}

package remote

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// TestToolRefusesEveryCallItCannotAddress pins that the tool refuses a call before the dispatcher sees it.
// - a tool constructed without a dispatcher is refused by name
// - a call naming no agent is refused by name and dispatches nothing
// - a call naming an agent the declaration does not carry is refused by name and dispatches nothing
// - a call beside no declaration is refused by name and dispatches nothing
// - arguments that do not decode are refused before any dispatch
// - every refusal leaves the dispatcher untouched
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func TestToolRefusesEveryCallItCannotAddress(t *testing.T) {
	ctx := context.Background()
	alpha := Target{AgentID: "alpha", AgentURL: "http://127.0.0.1:1/a2a"}
	endpoints := mustEndpoints(t, alpha)

	if _, err := NewTool(nil, endpoints, nil); !errors.Is(err, ErrDispatcherMissing) {
		t.Fatalf("tool without a dispatcher = %v, want ErrDispatcherMissing", err)
	}

	dispatcher := &recordingDispatcher{}
	tool := mustTool(t, dispatcher, endpoints, nil)

	if _, err := tool.Call(ctx, []byte(`{"message":"hello"}`)); !errors.Is(err, ErrToolTargetMissing) {
		t.Fatalf("call naming no agent = %v, want ErrToolTargetMissing", err)
	}
	if _, err := tool.Call(ctx, []byte(`{"agent":"beta","message":"hello"}`)); !errors.Is(err, ErrTargetUndeclared) {
		t.Fatalf("call naming an undeclared agent = %v, want ErrTargetUndeclared", err)
	}
	undeclaredTool := mustTool(t, dispatcher, nil, nil)
	if _, err := undeclaredTool.Call(ctx, []byte(`{"agent":"alpha","message":"hello"}`)); !errors.Is(err, ErrTargetUndeclared) {
		t.Fatalf("call beside no declaration = %v, want ErrTargetUndeclared", err)
	}
	if _, err := tool.Call(ctx, []byte(`{"agent":`)); err == nil || !strings.Contains(err.Error(), "do not decode") {
		t.Fatalf("unparsable arguments = %v, want the decode refusal", err)
	}
	if got := dispatcher.callCount(); got != 0 {
		t.Fatalf("refused calls reached the dispatcher %d times, want 0", got)
	}
}

// TestToolAttributesEveryCallFromDeclarationAndResolver pins where each part of a dispatch comes from.
// - the declaration requires the agent and message arguments and describes both
// - the default name is remote_call and a configured name replaces it while a blank one does not
// - the dispatcher receives the target the declaration resolves for the named agent
// - an argument carrying its own destination cannot change the resolved target
// - the dispatcher receives the identity the resolver reports
// - a resolver reporting no identity hands over the zero identity
// - a call without a resolver hands over the zero identity
// - the message text and the user role reach the dispatcher, and the reply text is the tool answer
// - a dispatcher failure is answered unchanged
// - result text joins the text parts of a message in order
// - result text reads the message a task status carries
// - result text stays empty for no result, no answer, and a task status without a message
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func TestToolAttributesEveryCallFromDeclarationAndResolver(t *testing.T) {
	ctx := context.Background()
	alpha := Target{AgentID: "alpha", AgentURL: "http://127.0.0.1:1/a2a"}
	endpoints := mustEndpoints(t, alpha)

	dispatcher := &recordingDispatcher{result: messageResult("the reply")}
	identity := CallIdentity{InvocationID: "inv-1", DecisionID: "dec-1", CallID: "call-1"}
	tool := mustTool(t, dispatcher, endpoints, func(context.Context) (CallIdentity, bool) { return identity, true })

	declaration := tool.Declaration()
	if declaration.Name != "remote_call" {
		t.Fatalf("default name = %q, want remote_call", declaration.Name)
	}
	for _, required := range []string{"agent", "message"} {
		if _, ok := declaration.InputSchema.Properties[required]; !ok {
			t.Fatalf("declaration describes no %q argument: %+v", required, declaration.InputSchema)
		}
	}
	if len(declaration.InputSchema.Required) != 2 {
		t.Fatalf("required arguments = %v, want agent and message", declaration.InputSchema.Required)
	}
	if named := mustTool(t, dispatcher, endpoints, nil, WithToolName("ask_remote")); named.Declaration().Name != "ask_remote" {
		t.Fatalf("configured name = %q, want ask_remote", named.Declaration().Name)
	}
	if blank := mustTool(t, dispatcher, endpoints, nil, WithToolName("")); blank.Declaration().Name != "remote_call" {
		t.Fatalf("blank configured name = %q, want the default kept", blank.Declaration().Name)
	}

	answer, err := tool.Call(ctx, []byte(`{"agent":"alpha","message":"hello","url":"http://elsewhere"}`))
	if err != nil {
		t.Fatalf("call = %v", err)
	}
	if answer != "the reply" {
		t.Fatalf("call answer = %v, want the reply text", answer)
	}
	if got := dispatcher.callCount(); got != 1 {
		t.Fatalf("dispatcher saw %d calls, want 1", got)
	}
	first := dispatcher.callAt(t, 0)
	if first.target != alpha {
		t.Fatalf("dispatched target = %+v, want the declared %+v", first.target, alpha)
	}
	if first.identity != identity {
		t.Fatalf("dispatched identity = %+v, want the resolved %+v", first.identity, identity)
	}
	if first.params.Message.Role != a2aprotocol.MessageRoleUser {
		t.Fatalf("dispatched role = %q, want the user role", first.params.Message.Role)
	}
	if text := dispatcher.messageText(t, 0); text != "hello" {
		t.Fatalf("dispatched text = %q, want the argument text", text)
	}

	zeroIdentity := mustTool(t, dispatcher, endpoints, func(context.Context) (CallIdentity, bool) {
		return CallIdentity{}, false
	})
	if _, err := zeroIdentity.Call(ctx, []byte(`{"agent":"alpha","message":"hello"}`)); err != nil {
		t.Fatalf("call beside a resolver reporting nothing = %v", err)
	}
	if got := dispatcher.callAt(t, 1).identity; got != (CallIdentity{}) {
		t.Fatalf("identity beside a resolver reporting nothing = %+v, want the zero identity", got)
	}
	if _, err := mustTool(t, dispatcher, endpoints, nil).Call(ctx, []byte(`{"agent":"alpha","message":"hello"}`)); err != nil {
		t.Fatalf("call without a resolver = %v", err)
	}
	if got := dispatcher.callAt(t, 2).identity; got != (CallIdentity{}) {
		t.Fatalf("identity without a resolver = %+v, want the zero identity", got)
	}

	refused := &recordingDispatcher{err: errors.New("the remote agent is busy")}
	if _, err := mustTool(t, refused, endpoints, nil).Call(ctx, []byte(`{"agent":"alpha","message":"hello"}`)); !errors.Is(err, refused.err) {
		t.Fatalf("dispatcher failure = %v, want it answered unchanged", err)
	}

	firstPart := a2aprotocol.NewTextPart("a")
	message := messageResult("ab")
	message.Result = &a2aprotocol.Message{
		Kind:      a2aprotocol.KindMessage,
		MessageID: "m-1",
		Role:      a2aprotocol.MessageRoleAgent,
		Parts:     []a2aprotocol.Part{a2aprotocol.NewTextPart("a"), firstPart},
	}
	if text := ResultText(message); text != "aa" {
		t.Fatalf("message text = %q, want the parts joined in order", text)
	}
	pointerPart := a2aprotocol.NewTextPart("b")
	message.Result = &a2aprotocol.Message{Parts: []a2aprotocol.Part{&pointerPart}}
	if text := ResultText(message); text != "b" {
		t.Fatalf("pointer part text = %q, want b", text)
	}

	task := a2aprotocol.NewTask("task-1", "ctx-1")
	task.Status.Message = &a2aprotocol.Message{Parts: []a2aprotocol.Part{a2aprotocol.NewTextPart("task reply")}}
	if text := ResultText(&a2aprotocol.MessageResult{Result: task}); text != "task reply" {
		t.Fatalf("task text = %q, want the status message text", text)
	}
	if text := ResultText(nil); text != "" {
		t.Fatalf("text of no result = %q, want empty", text)
	}
	if text := ResultText(&a2aprotocol.MessageResult{}); text != "" {
		t.Fatalf("text of no answer = %q, want empty", text)
	}
	if text := ResultText(&a2aprotocol.MessageResult{Result: a2aprotocol.NewTask("task-2", "ctx-2")}); text != "" {
		t.Fatalf("text of a task without a status message = %q, want empty", text)
	}
}

type recordedCall struct {
	target   Target
	identity CallIdentity
	params   a2aprotocol.SendMessageParams
}

type recordingDispatcher struct {
	mu     sync.Mutex
	calls  []recordedCall
	result *a2aprotocol.MessageResult
	err    error
}

func (d *recordingDispatcher) Send(_ context.Context, target Target, identity CallIdentity, params a2aprotocol.SendMessageParams, _ ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, recordedCall{target: target, identity: identity, params: params})
	return d.result, d.err
}

func (d *recordingDispatcher) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func (d *recordingDispatcher) callAt(t *testing.T, index int) recordedCall {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if index >= len(d.calls) {
		t.Fatalf("dispatcher saw %d calls, want call %d", len(d.calls), index)
	}
	return d.calls[index]
}

func (d *recordingDispatcher) messageText(t *testing.T, index int) string {
	t.Helper()
	message := d.callAt(t, index).params.Message
	return messageText(&message)
}

func messageResult(text string) *a2aprotocol.MessageResult {
	return &a2aprotocol.MessageResult{Result: &a2aprotocol.Message{
		Kind:      a2aprotocol.KindMessage,
		MessageID: "m-1",
		Role:      a2aprotocol.MessageRoleAgent,
		Parts:     []a2aprotocol.Part{a2aprotocol.NewTextPart(text)},
	}}
}

func mustTool(t *testing.T, dispatcher Dispatcher, endpoints *Endpoints, identity IdentityResolver, options ...ToolOption) *Tool {
	t.Helper()
	tool, err := NewTool(dispatcher, endpoints, identity, options...)
	if err != nil {
		t.Fatalf("tool = %v", err)
	}
	return tool
}

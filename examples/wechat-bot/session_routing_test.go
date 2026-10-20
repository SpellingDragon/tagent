// session_routing_test 钉住 微信宿主的会话路由红线：一个 chat 身份对应一个显式会址，各会话的
// 收发互不串台，显式 send 只达本会址绑定的会话，审批裁决只在摘要唯一时生效。文件内的装配替身
// 走与进程相同的装配入口，只把模型与通道换成确定性替身。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// testSessionPrefix 是用例会址的会话前缀：与装配期的会话标签一致，便于从会址读出 chat 身份。
const testSessionPrefix = "routing-test:tagent:"

// testAddress 拼出一个 chat 身份在用例命名空间里的显式会址。
func testAddress(chat string) event.Address {
	return event.Address{AgentID: "tagent", SessionID: event.SessionID(testSessionPrefix + chat)}
}

// frameCtx 携带一次调用派发会写下的显式会址，工具据此确定送达目标。
func frameCtx(ctx context.Context, addr event.Address) context.Context {
	return task.WithCallFrame(ctx, task.CallFrame{Address: addr})
}

// testDocument 造一份宿主文档：入口 agent 声明 send 工具，其余 agent 不声明任何工具。
func testDocument(entry string, peers ...string) *hostDocument {
	doc := &hostDocument{Entry: entry, Model: "test-model"}
	doc.Agents = map[string]agentEntry{
		entry: {Model: "test-model", Tools: []hostToolRef{{Kind: "tool", ID: sendToolID}}},
	}
	for _, peer := range peers {
		doc.Agents[peer] = agentEntry{Model: "test-model"}
	}
	return doc
}

// scriptModel 按决策顺序回答脚本：未编排的决策仍给出收尾答复，让调用计数偏离的用例在断言处失败
// 而不是挂在驱动里。
type scriptModel struct {
	mu       sync.Mutex
	queue    []func(*model.Request) *model.Response
	calls    int
	requests []model.Request
}

// GenerateContent records one native request and streams its scripted answer.
func (m *scriptModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	step := func(*model.Request) *model.Response { return finalTurn("(unscripted)") }
	if len(m.queue) > 0 {
		step = m.queue[0]
		m.queue = m.queue[1:]
	}
	m.calls++
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	ch <- step(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted model's descriptor.
func (m *scriptModel) Info() model.Info { return model.Info{Name: "session-routing-model"} }

// then queues the scripted decisions of the following drives.
func (m *scriptModel) then(responses ...*model.Response) {
	m.mu.Lock()
	for _, resp := range responses {
		answer := resp
		m.queue = append(m.queue, func(*model.Request) *model.Response { return answer })
	}
	m.mu.Unlock()
}

// callCount reports how many decisions the model was driven through.
func (m *scriptModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// recorded reports the native requests the model was driven with.
func (m *scriptModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// finalTurn builds the scripted response carrying one assistant answer.
func finalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

// sendTurn builds the scripted response calling the send tool with one body.
func sendTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
			ID:   "call-send-1",
			Type: "function",
			Function: model.FunctionDefinitionParam{
				Name:      sendToolID,
				Arguments: []byte(fmt.Sprintf(`{"content":%q}`, content)),
			},
		}}}}},
		Done: true,
	}
}

// outboundEvent 是替身通道记录的一笔侧效：类别、目标与正文。
type outboundEvent struct {
	Kind   string
	Target string
	Body   string
}

// mockOutbound 是替身通道：记录每一笔文本/媒体/typing/context-token 接触，不触碰真实微信。
type mockOutbound struct {
	mu        sync.Mutex
	events    []outboundEvent
	tokenText string
	failText  error
}

var _ wechatOutbound = (*mockOutbound)(nil)

// record appends one side effect of the stand-in channel.
func (m *mockOutbound) record(kind, target, body string) {
	m.mu.Lock()
	m.events = append(m.events, outboundEvent{Kind: kind, Target: target, Body: body})
	m.mu.Unlock()
}

// SendTextToUser records one text delivery.
func (m *mockOutbound) SendTextToUser(_ context.Context, to, text string) error {
	if m.failText != nil {
		return m.failText
	}
	m.record("text", to, text)
	return nil
}

// SendImageFromPath records one image delivery.
func (m *mockOutbound) SendImageFromPath(_ context.Context, to, path string) error {
	m.record("image", to, path)
	return nil
}

// SendVoiceFromPath records one voice delivery.
func (m *mockOutbound) SendVoiceFromPath(_ context.Context, to, path string, _ int) error {
	m.record("voice", to, path)
	return nil
}

// SendVideoFromPath records one video delivery.
func (m *mockOutbound) SendVideoFromPath(_ context.Context, to, path string) error {
	m.record("video", to, path)
	return nil
}

// SendFileFromPath records one file delivery.
func (m *mockOutbound) SendFileFromPath(_ context.Context, to, path string) error {
	m.record("file", to, path)
	return nil
}

// StopTyping records one typing cleanup.
func (m *mockOutbound) StopTyping(_ context.Context, to string) error {
	m.record("stop-typing", to, "")
	return nil
}

// GetContextToken reports the configured context token of the stand-in channel.
func (m *mockOutbound) GetContextToken(string) (string, error) { return m.tokenText, nil }

// recorded reports the side effects in arrival order.
func (m *mockOutbound) recorded() []outboundEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]outboundEvent(nil), m.events...)
}

// texts reports the text deliveries in arrival order.
func (m *mockOutbound) texts() []outboundEvent {
	var out []outboundEvent
	for _, e := range m.recorded() {
		if e.Kind == "text" {
			out = append(out, e)
		}
	}
	return out
}

// has reports whether one recorded side effect matches the kind, the target and
// the body substring.
func (m *mockOutbound) has(kind, target, body string) bool {
	for _, e := range m.recorded() {
		if e.Kind != kind || e.Target != target {
			continue
		}
		if body == "" || strings.Contains(e.Body, body) {
			return true
		}
	}
	return false
}

// routingRig 是一台挂在替身通道上的宿主：装配入口与进程相同，模型与通道为确定性替身。
type routingRig struct {
	host  *hostRuntime
	model *scriptModel
	seam  *hostSendSeam
	out   *mockOutbound
}

// newRoutingRig assembles one document beside a scripted model and a recording
// channel, and binds the outbound face the passive and the active channel share.
func newRoutingRig(t *testing.T, doc *hostDocument) *routingRig {
	t.Helper()
	t.Setenv("TAGENT_SESSION_ID", "routing-test")

	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("declare the test document: %v", err)
	}
	script := &scriptModel{}
	seam := &hostSendSeam{}
	natives := make(map[event.AgentID]tagent.NativeAgent, len(decl.Agents))
	for _, spec := range decl.Agents {
		native := tagent.NativeAgent{Model: script}
		if doc.declaresSend(spec.ID) {
			native.Tools = []trpctool.CallableTool{sendToolFactory(seam)}
		}
		natives[event.AgentID(spec.ID)] = native
	}

	out := &mockOutbound{}
	host, err := assembleHostFrom(context.Background(), doc, nil, NewSeenStore(t.TempDir(), slog.Default()), seam, natives, nil)
	if err != nil {
		t.Fatalf("assemble the host: %v", err)
	}
	host.sender = &hostSender{
		bot: out,
		longText: func(_ context.Context, chatID, content, _ string) error {
			out.record("long", chatID, fmt.Sprintf("%d bytes", len(content)))
			return nil
		},
		typingActive: &sync.Map{},
		router:       host.router,
		inj:          host.inbox,
		workspaceDir: t.TempDir(),
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Logf("close the host: %v", err)
		}
	})
	return &routingRig{host: host, model: script, seam: seam, out: out}
}

// historyCarries reports whether any message of one session transcript carries
// the named body.
func historyCarries(history []model.Message, body string) bool {
	for _, msg := range history {
		if strings.Contains(msg.Content, body) {
			return true
		}
	}
	return false
}

// requestCarries reports whether any message of one model request carries the
// named body.
func requestCarries(req model.Request, body string) bool {
	return historyCarries(req.Messages, body)
}

// TestSessionRouting 钉住 微信宿主的会话路由红线：会址隔离、单次驱动、显式寻址不猜、审批按摘要唯一定位。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestSessionRouting(t *testing.T) {
	ctx := context.Background()

	t.Run("addresses_are_injective_and_histories_isolated", func(t *testing.T) {
		rig := newRoutingRig(t, testDocument("tagent"))
		router := rig.host.router

		a, err := router.AddressFor("chat-a")
		if err != nil {
			t.Fatalf("address chat-a: %v", err)
		}
		b, err := router.AddressFor("chat-b")
		if err != nil {
			t.Fatalf("address chat-b: %v", err)
		}
		if a == b {
			t.Fatalf("two chats collapsed onto one address: %v", a)
		}
		if again, err := router.AddressFor("chat-a"); err != nil || again != a {
			t.Fatalf("address of one chat is not stable: (%v,%v), want %v", again, err, a)
		}
		if _, err := router.AddressFor("   "); !errors.Is(err, ErrNoChatAddress) {
			t.Fatalf("blank chat identity = %v, want ErrNoChatAddress", err)
		}
		slash, err := router.AddressFor("team/alpha")
		if err != nil {
			t.Fatal(err)
		}
		escaped, err := router.AddressFor("team%2Falpha")
		if err != nil {
			t.Fatal(err)
		}
		if slash == escaped {
			t.Fatalf("distinct chats collapsed through escaping: %v", slash)
		}

		rig.model.then(finalTurn("reply-a"))
		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "iso-a", ChatID: "chat-a", Text: "hello-a"}); err != nil {
			t.Fatalf("drive chat-a: %v", err)
		}
		rig.model.then(finalTurn("reply-b"))
		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "iso-b", ChatID: "chat-b", Text: "hello-b"}); err != nil {
			t.Fatalf("drive chat-b: %v", err)
		}

		histA := rig.host.run.SessionHistory(a)
		if !historyCarries(histA, "hello-a") || !historyCarries(histA, "reply-a") {
			t.Fatalf("chat-a transcript = %+v, want its own input and answer", histA)
		}
		if historyCarries(histA, "hello-b") || historyCarries(histA, "reply-b") {
			t.Fatalf("chat-b turn leaked into chat-a transcript: %+v", histA)
		}
		histB := rig.host.run.SessionHistory(b)
		if !historyCarries(histB, "hello-b") || !historyCarries(histB, "reply-b") {
			t.Fatalf("chat-b transcript = %+v, want its own input and answer", histB)
		}
		if historyCarries(histB, "hello-a") || historyCarries(histB, "reply-a") {
			t.Fatalf("chat-a turn leaked into chat-b transcript: %+v", histB)
		}
	})

	t.Run("duplicate_delivery_drives_one_turn", func(t *testing.T) {
		rig := newRoutingRig(t, testDocument("tagent"))
		rig.model.then(finalTurn("once"))

		in := inboundMessage{Key: "dup-key", ChatID: "chat-a", Text: "hi"}
		if _, err := rig.host.HandleInbound(ctx, in); err != nil {
			t.Fatalf("first delivery: %v", err)
		}
		if _, err := rig.host.HandleInbound(ctx, in); !errors.Is(err, ErrDuplicateInbound) {
			t.Fatalf("duplicate delivery = %v, want ErrDuplicateInbound", err)
		}
		if got := rig.model.callCount(); got != 1 {
			t.Fatalf("duplicate delivery drove %d turns, want exactly one", got)
		}
		if got := len(rig.out.texts()); got != 1 {
			t.Fatalf("duplicate delivery produced %d answers, want exactly one", got)
		}
	})

	t.Run("missing_chat_identity_is_refused", func(t *testing.T) {
		rig := newRoutingRig(t, testDocument("tagent"))

		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "no-chat", ChatID: "   ", Text: "hi"}); !errors.Is(err, ErrNoChatAddress) {
			t.Fatalf("missing chat identity = %v, want ErrNoChatAddress", err)
		}
		if got := rig.model.callCount(); got != 0 {
			t.Fatalf("an addressless delivery drove %d turns, want none", got)
		}
		if got := len(rig.out.recorded()); got != 0 {
			t.Fatalf("an addressless delivery produced %d side effects, want none: %v", got, rig.out.recorded())
		}
	})

	t.Run("reply_lands_in_the_speaking_session_only", func(t *testing.T) {
		rig := newRoutingRig(t, testDocument("tagent"))
		rig.model.then(finalTurn("answer-for-a"))
		rig.model.then(finalTurn("answer-for-b"))

		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "reply-a", ChatID: "chat-a", Text: "q-a"}); err != nil {
			t.Fatalf("drive chat-a: %v", err)
		}
		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "reply-b", ChatID: "chat-b", Text: "q-b"}); err != nil {
			t.Fatalf("drive chat-b: %v", err)
		}

		texts := rig.out.texts()
		if len(texts) != 2 {
			t.Fatalf("text deliveries = %v, want one answer per chat", texts)
		}
		for _, delivery := range texts {
			switch delivery.Target {
			case "chat-a":
				if delivery.Body != "answer-for-a" {
					t.Fatalf("chat-a received %q, want its own answer", delivery.Body)
				}
			case "chat-b":
				if delivery.Body != "answer-for-b" {
					t.Fatalf("chat-b received %q, want its own answer", delivery.Body)
				}
			default:
				t.Fatalf("answer delivered to an unknown target %q: %v", delivery.Target, texts)
			}
		}
	})

	t.Run("internal_notices_wait_for_their_own_session", func(t *testing.T) {
		rig := newRoutingRig(t, testDocument("tagent"))
		a, err := rig.host.router.AddressFor("chat-a")
		if err != nil {
			t.Fatal(err)
		}
		rig.host.inbox.InjectInternal(a, receiptLineage, model.Message{Content: "notice-for-a"})

		rig.model.then(finalTurn("b-answer"))
		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "notice-b", ChatID: "chat-b", Text: "q-b"}); err != nil {
			t.Fatalf("drive chat-b: %v", err)
		}
		if requestCarries(rig.model.recorded()[0], "notice-for-a") {
			t.Fatalf("a notice addressed to chat-a entered the chat-b request: %+v", rig.model.recorded()[0].Messages)
		}

		rig.model.then(finalTurn("a-answer"))
		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "notice-a", ChatID: "chat-a", Text: "q-a"}); err != nil {
			t.Fatalf("drive chat-a: %v", err)
		}
		requests := rig.model.recorded()
		if len(requests) != 2 || !requestCarries(requests[1], "notice-for-a") {
			t.Fatalf("the notice did not reach its own session: %+v", requests)
		}
	})

	t.Run("explicit_send_reaches_only_the_bound_session", func(t *testing.T) {
		rig := newRoutingRig(t, testDocument("tagent"))
		rig.model.then(sendTurn("poke-a"))
		rig.model.then(finalTurn("closing-a"))
		rig.model.then(finalTurn("closing-b"))

		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "send-a", ChatID: "chat-a", Text: "q-a"}); err != nil {
			t.Fatalf("drive chat-a: %v", err)
		}
		if !rig.out.has("text", "chat-a", "poke-a") {
			t.Fatalf("the explicit send did not reach chat-a: %v", rig.out.recorded())
		}
		if rig.out.has("text", "chat-b", "") {
			t.Fatalf("the chat-a turn delivered into chat-b: %v", rig.out.recorded())
		}
		requests := rig.model.recorded()
		if len(requests) != 2 || !requestCarries(requests[1], "[send_ok]") || !requestCarries(requests[1], "目标 chat-a") {
			t.Fatalf("the success receipt did not return to the model: %+v", requests)
		}

		if _, err := rig.host.HandleInbound(ctx, inboundMessage{Key: "send-b", ChatID: "chat-b", Text: "q-b"}); err != nil {
			t.Fatalf("drive chat-b: %v", err)
		}
		if rig.out.has("text", "chat-b", "poke-a") {
			t.Fatalf("the chat-a send body leaked into chat-b: %v", rig.out.recorded())
		}
		if !rig.out.has("text", "chat-b", "closing-b") {
			t.Fatalf("chat-b did not receive its own answer: %v", rig.out.recorded())
		}
	})

	t.Run("explicit_send_refuses_an_unbound_address", func(t *testing.T) {
		rig := newRoutingRig(t, testDocument("tagent"))
		owner, err := rig.host.router.AddressFor("chat-owner")
		if err != nil {
			t.Fatal(err)
		}
		if err := rig.host.router.Bind(owner, "chat-owner"); err != nil {
			t.Fatal(err)
		}
		stray := event.Address{AgentID: "tagent", SessionID: "never-bound"}

		res, err := sendToolFactory(rig.seam).Call(frameCtx(ctx, stray), []byte(`{"content":"leak"}`))
		if err != nil {
			t.Fatalf("the refusal must stay a result text: %v", err)
		}
		text, ok := res.(string)
		if !ok || !strings.HasPrefix(text, "[send_denied] 无可解析的送达目标") {
			t.Fatalf("unbound address result = %v, want a named refusal", res)
		}
		if got := rig.out.recorded(); len(got) != 0 {
			t.Fatalf("an unbound address fell back to another chat: %v", got)
		}
		receipts := rig.host.inbox.Drain(stray)
		if len(receipts) != 1 || !strings.Contains(receipts[0].Content, "no-target") {
			t.Fatalf("receipts = %+v, want one no-target receipt for the unbound address", receipts)
		}
	})

	t.Run("approval_rulings_bind_one_digest_and_respect_the_whitelist", func(t *testing.T) {
		gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, []string{"boss"})
		manager := gateway.Manager()
		held, err := manager.Request(ctx, "req-unique", "exec", []byte(`{"cmd":"ls"}`), plugin.RiskCritical, "critical", "held critical call")
		if err != nil {
			t.Fatalf("register the held call: %v", err)
		}
		twinArgs := []byte(`{"cmd":"rm"}`)
		for _, id := range []string{"req-twin-1", "req-twin-2"} {
			if _, err := manager.Request(ctx, id, "exec", twinArgs, plugin.RiskCritical, "critical", "held critical call"); err != nil {
				t.Fatalf("register %s: %v", id, err)
			}
		}

		if handled, _, err := gateway.Resolve("stranger", "approve "+held.ArgsDigest[:6]); !handled || !errors.Is(err, ErrApprovalNotApprover) {
			t.Fatalf("a ruling outside the whitelist = (%v,%v), want ErrApprovalNotApprover", handled, err)
		}
		if got := len(manager.Pending()); got != 3 {
			t.Fatalf("pending after a refused ruling = %d, want all three requests untouched", got)
		}
		if handled, _, err := gateway.Resolve("boss", "你好"); handled || err != nil {
			t.Fatalf("a plain chat message = (%v,%v), want it to continue as a chat turn", handled, err)
		}
		if handled, _, err := gateway.Resolve("boss", "approve zzzz"); !handled || !errors.Is(err, approval.ErrApprovalNotFound) {
			t.Fatalf("an unknown digest = (%v,%v), want ErrApprovalNotFound", handled, err)
		}
		if handled, _, err := gateway.Resolve("boss", "approve "+approval.ArgsDigest(twinArgs)[:6]); !handled || !errors.Is(err, ErrApprovalAmbiguous) {
			t.Fatalf("a digest naming two requests = (%v,%v), want ErrApprovalAmbiguous", handled, err)
		}
		if got := len(manager.Pending()); got != 3 {
			t.Fatalf("pending after an ambiguous ruling = %d, want the requests untouched", got)
		}

		handled, reply, err := gateway.Resolve("boss", "approve "+held.ArgsDigest[:8])
		if !handled || err != nil {
			t.Fatalf("the unique ruling = (%v,%v), want it applied", handled, err)
		}
		if !strings.Contains(reply, "approved") || !strings.Contains(reply, "req-unique") {
			t.Fatalf("ruling reply = %q, want the decided identity and status", reply)
		}
		pending := manager.Pending()
		if len(pending) != 2 {
			t.Fatalf("pending after the ruling = %d, want the two twins only", len(pending))
		}
		for _, req := range pending {
			if req.ID == "req-unique" {
				t.Fatalf("the decided request is still pending: %+v", req)
			}
		}
		if handled, _, err := gateway.Resolve("boss", "approve "+held.ArgsDigest[:8]); !handled || !errors.Is(err, approval.ErrApprovalNotFound) {
			t.Fatalf("a repeated ruling = (%v,%v), want it refused as decided", handled, err)
		}
	})
}

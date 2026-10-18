// send_tool_test 钉住 主动送达面的模型可见契约与宿主通路复用：参数封闭、8KiB 上界、拒绝与成功文案、
// 授予即授权 的装配面，以及主动/被动两路汇于同一发送实现的闭环。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestSendToolDeclaration 钉住 声明面只教模型一件事：说什么。
// - 工具名 send，required 恰为 content，属性表只有一位；
// - 表里没有目标位与通道位：发送目标与通道由装配固定。
func TestSendToolDeclaration(t *testing.T) {
	d := NewSendTool(nil).Declaration()
	if d.Name != sendToolID {
		t.Fatalf("declaration name = %q, want %q", d.Name, sendToolID)
	}
	if len(d.InputSchema.Properties) != 1 || d.InputSchema.Properties["content"] == nil {
		t.Fatalf("properties = %v, want only content", d.InputSchema.Properties)
	}
	if len(d.InputSchema.Required) != 1 || d.InputSchema.Required[0] != "content" {
		t.Fatalf("required = %v, want [content]", d.InputSchema.Required)
	}
	for _, forbidden := range []string{"chat_id", "target", "to", "user_id", "channel", "token"} {
		if _, ok := d.InputSchema.Properties[forbidden]; ok {
			t.Errorf("declaration exposes %q — 目标与通道不得经参数出入", forbidden)
		}
	}
}

// seamRecorder 是发送缝替身：记录每次调用携带的会址与正文，标签与失败由用例给定。
type seamRecorder struct {
	mu     sync.Mutex
	addrs  []event.Address
	bodies []string
	label  string
	given  error
}

// send implements SendFunc.
func (s *seamRecorder) send(_ context.Context, addr event.Address, content string) (string, error) {
	s.mu.Lock()
	s.addrs = append(s.addrs, addr)
	s.bodies = append(s.bodies, content)
	s.mu.Unlock()
	return s.label, s.given
}

// calls reports how many times the seam was reached.
func (s *seamRecorder) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

// mustCall 调一次工具并核对"拒绝不断回合"的前提：err 为空。
func mustCall(t *testing.T, tool *SendTool, ctx context.Context, args string) string {
	t.Helper()
	res, err := tool.Call(ctx, []byte(args))
	if err != nil {
		t.Fatalf("Call(%s) 期望结果文本而非回合错误: %v", args, err)
	}
	text, ok := res.(string)
	if !ok {
		t.Fatalf("Call(%s) 结果应为字符串, got %T", args, res)
	}
	return text
}

// TestSendToolContentBound 钉住 8KiB 上界的两侧：等界通过、超一字节具名拒绝且缝未被触碰。
func TestSendToolContentBound(t *testing.T) {
	seam := &seamRecorder{label: "chat-1"}
	tool := NewSendTool(seam.send)
	ctx := frameCtx(context.Background(), testAddress("chat-1"))

	res := mustCall(t, tool, ctx, fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("x", contentMaxBytes+1)))
	if !strings.Contains(res, "[send_denied] content exceeds 8KiB 上界（实际 8193 字节）") {
		t.Fatalf("超限文案 = %q", res)
	}
	if seam.calls() != 0 {
		t.Fatalf("超限不得触缝, got %d calls", seam.calls())
	}

	res = mustCall(t, tool, ctx, fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("x", contentMaxBytes)))
	if !strings.Contains(res, "[send_ok]") {
		t.Fatalf("等界内容应合法送达, got %q", res)
	}
}

// TestSendToolEmptyContentRefused 钉住 参数校验：缺位与空串都是具名拒绝，缝未被触碰。
func TestSendToolEmptyContentRefused(t *testing.T) {
	seam := &seamRecorder{label: "chat-1"}
	tool := NewSendTool(seam.send)
	ctx := frameCtx(context.Background(), testAddress("chat-1"))

	for _, args := range []string{`{}`, `{"content":""}`} {
		res := mustCall(t, tool, ctx, args)
		if !strings.Contains(res, "[send_denied] content 不能为空") {
			t.Fatalf("空正文文案(%s) = %q", args, res)
		}
	}
	if seam.calls() != 0 {
		t.Fatalf("被拒的调用不得触缝, got %d calls", seam.calls())
	}
}

// TestSendToolForgedTargetRefused 钉住 身份不可伪造：夹带目标/通道字段的调用具名拒绝，缝未被触碰。
func TestSendToolForgedTargetRefused(t *testing.T) {
	seam := &seamRecorder{label: "chat-1"}
	tool := NewSendTool(seam.send)
	ctx := frameCtx(context.Background(), testAddress("chat-1"))

	res := mustCall(t, tool, ctx, `{"content":"c","chat_id":"victim","channel":"webhook"}`)
	if !strings.HasPrefix(res, "[send_denied] 参数含声明之外的字段 'channel', 'chat_id'") {
		t.Fatalf("夹带字段文案 = %q", res)
	}
	if !strings.Contains(res, "不可指定或覆写") {
		t.Fatalf("文案须说明身份固定, got %q", res)
	}
	if seam.calls() != 0 {
		t.Fatalf("被拒的调用不得触缝, got %d calls", seam.calls())
	}
}

// TestSendToolRefusesWithoutExplicitAddress 钉住 调用帧缺席或不完整时会址不可猜，工具具名拒绝且缝未被触碰。
func TestSendToolRefusesWithoutExplicitAddress(t *testing.T) {
	seam := &seamRecorder{label: "chat-1"}
	tool := NewSendTool(seam.send)

	res := mustCall(t, tool, context.Background(), `{"content":"c"}`)
	if !strings.Contains(res, "[send_denied] 本次调用不携带显式会话地址") {
		t.Fatalf("无帧文案 = %q", res)
	}

	incomplete := frameCtx(context.Background(), event.Address{AgentID: "tagent"})
	res = mustCall(t, tool, incomplete, `{"content":"c"}`)
	if !strings.Contains(res, "[send_denied] 本次调用不携带显式会话地址") {
		t.Fatalf("不完整会址文案 = %q", res)
	}
	if seam.calls() != 0 {
		t.Fatalf("缺会址的调用不得触缝, got %d calls", seam.calls())
	}
}

// TestSendToolUnboundSeamRefuses 钉住 晚绑定窗口的失败模式：缝未接线时具名拒绝而非 panic。
func TestSendToolUnboundSeamRefuses(t *testing.T) {
	ctx := frameCtx(context.Background(), testAddress("chat-1"))

	res := mustCall(t, NewSendTool(nil), ctx, `{"content":"c"}`)
	if !strings.Contains(res, "[send_denied] 宿主未接线发送缝") {
		t.Fatalf("未接线文案 = %q", res)
	}

	res = mustCall(t, NewSendTool((&hostSendSeam{}).Send), ctx, `{"content":"c"}`)
	if !strings.Contains(res, "[send_denied] 宿主发送通路尚未就绪") {
		t.Fatalf("未绑定的缝须具名拒绝, got %q", res)
	}
}

// TestSendToolSuccessReceiptNamesTarget 钉住 成功回执：正文原样进缝、会址随调用下传、送达目标回模型。
func TestSendToolSuccessReceiptNamesTarget(t *testing.T) {
	seam := &seamRecorder{label: "chat-7"}
	tool := NewSendTool(seam.send)
	addr := testAddress("chat-7")

	res := mustCall(t, tool, frameCtx(context.Background(), addr), `{"content":"整理完毕：三项已收口"}`)
	if !strings.Contains(res, "[send_ok] 已送达（目标 chat-7）") {
		t.Fatalf("成功回执 = %q", res)
	}
	if seam.calls() != 1 || seam.bodies[0] != "整理完毕：三项已收口" {
		t.Fatalf("正文须原样进缝, got %v", seam.bodies)
	}
	if seam.addrs[0] != addr {
		t.Fatalf("缝收到的会址 = %v, want %v", seam.addrs[0], addr)
	}
}

// TestSendToolSendFailureReturnsNamedReason 钉住 失败面：通路拒绝的具名原因回模型，目标规则随文案点名。
func TestSendToolSendFailureReturnsNamedReason(t *testing.T) {
	seam := &seamRecorder{label: "", given: errNoSendTarget}
	tool := NewSendTool(seam.send)

	res := mustCall(t, tool, frameCtx(context.Background(), testAddress("chat-1")), `{"content":"想说的一句话"}`)
	if !strings.HasPrefix(res, "[send_denied] 无可解析的送达目标") {
		t.Fatalf("无目标文案 = %q", res)
	}
	if !strings.Contains(res, "目标规则：本会话地址绑定的会话（不经最近活跃回退）") {
		t.Fatalf("拒绝文案须点名目标规则, got %q", res)
	}
}

// TestSendToolProtocolError 钉住 err 的留守领地：解不出参数形态才是协议错。
func TestSendToolProtocolError(t *testing.T) {
	tool := NewSendTool(nil)
	res, err := tool.Call(frameCtx(context.Background(), testAddress("chat-1")), []byte(`{"content":`))
	if res != nil {
		t.Fatalf("协议错不得产出结果, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "send: malformed arguments") {
		t.Fatalf("协议错文案缺失: %v", err)
	}
}

// TestSendToolAssemblyOnlyDeclaredAgentsSeeSend 钉住 能力即授权：声明了 send 的 agent 才看得见它。
// - 入口 agent 声明 {kind: tool, id: send}，其模型请求的工具面里有它；
// - peer 未声明，同一次装配里它的模型请求工具面里没有 send。
func TestSendToolAssemblyOnlyDeclaredAgentsSeeSend(t *testing.T) {
	doc := testDocument("host", "peer")
	rig := newRoutingRig(t, doc)
	if !doc.declaresSend("host") || doc.declaresSend("peer") {
		t.Fatalf("授予面 = host:%v peer:%v, want host only", doc.declaresSend("host"), doc.declaresSend("peer"))
	}

	rig.model.then(finalTurn("host answered"))
	if _, err := rig.host.HandleInbound(context.Background(), inboundMessage{Key: "grant-host", ChatID: "chat-host", Text: "hi"}); err != nil {
		t.Fatalf("drive the entry agent: %v", err)
	}
	peer := event.Address{AgentID: "peer", SessionID: "peer-session"}
	if _, err := rig.host.run.Invoke(context.Background(), tagent.InvokeRequest{
		InvocationID: "inv-peer-grant",
		Address:      peer,
		Input:        []model.Message{{Role: model.RoleUser, Content: "peer turn"}},
	}); err != nil {
		t.Fatalf("invoke the peer agent: %v", err)
	}

	requests := rig.model.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want the entry and the peer decision", len(requests))
	}
	if requests[0].Tools[sendToolID] == nil {
		t.Fatalf("entry tool face = %v, want send declared and visible", requests[0].Tools)
	}
	if requests[1].Tools[sendToolID] != nil {
		t.Fatalf("peer tool face = %v, want send absent", requests[1].Tools)
	}
}

// TestSendToolDocumentGrantsEntryAndMeditator 钉住 默认示例的授予面：入口与 meditator 声明 send。
// - 其余子 agent 未声明，模型不可见；
// - 授予来自 yaml 的 tool 引用，装配期不硬塞给任何 agent。
func TestSendToolDocumentGrantsEntryAndMeditator(t *testing.T) {
	doc, err := loadHostDocument("tagent.yaml")
	if err != nil {
		t.Fatalf("load tagent.yaml: %v", err)
	}
	for _, name := range []string{"tagent", "meditator"} {
		if !doc.declaresSend(name) {
			t.Errorf("agent %q 应声明 {kind: tool, id: send}", name)
		}
	}
	for _, name := range []string{"knowledge", "recall", "action", "plan"} {
		if doc.declaresSend(name) {
			t.Errorf("agent %q 未获授予，却声明了 send", name)
		}
	}
}

// TestSendToolBothChannelsFunnelThroughOneSender 钉住 零第二通路：两路共用 sendToUser 一个实现。
// - 主动通道（工具）与被动通道（回合交付）都只增加同一到访计数；
// - typing 清理、长文分片、附件投递在两路的副作用形态一致。
func TestSendToolBothChannelsFunnelThroughOneSender(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("附件正文"), 0o644); err != nil {
		t.Fatal(err)
	}
	bot := &mockOutbound{tokenText: "ctx-token"}
	router, err := NewSessionRouter("tagent", "funnel:")
	if err != nil {
		t.Fatal(err)
	}
	active, err := router.AddressFor("chat-active")
	if err != nil {
		t.Fatal(err)
	}
	passive, err := router.AddressFor("chat-passive")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		addr event.Address
		chat string
	}{{active, "chat-active"}, {passive, "chat-passive"}} {
		if err := router.Bind(pair.addr, pair.chat); err != nil {
			t.Fatal(err)
		}
	}
	sender := &hostSender{
		bot: bot,
		longText: func(_ context.Context, chatID, content, _ string) error {
			bot.record("long", chatID, fmt.Sprintf("%d bytes", len(content)))
			return nil
		},
		typingActive: &sync.Map{},
		router:       router,
		inj:          &recordingInjector{},
		workspaceDir: dir,
	}
	seam := &hostSendSeam{}
	seam.bind(func(ctx context.Context, addr event.Address, content string) (string, error) {
		return sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: content})
	})

	sender.typingActive.Store("chat-active", time.Now())
	sender.typingActive.Store("chat-passive", time.Now())

	res := mustCall(t, NewSendTool(seam.Send), frameCtx(context.Background(), active), fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("长", 900)))
	if !strings.Contains(res, "[send_ok]") {
		t.Fatalf("主动通道应送达, got %q", res)
	}
	if _, err := sender.sendToUser(context.Background(), sendRequest{
		addr: passive, userName: "alice", triggerSource: "user", content: "被动一条 " + note,
	}); err != nil {
		t.Fatalf("被动通道 sendToUser: %v", err)
	}

	if got := sender.sendCalls(); got != 2 {
		t.Fatalf("两路应各到访同一实现一次, got %d", got)
	}
	for _, want := range []outboundEvent{
		{Kind: "stop-typing", Target: "chat-active"},
		{Kind: "stop-typing", Target: "chat-passive"},
		{Kind: "long", Target: "chat-active"},
		{Kind: "file", Target: "chat-passive", Body: "note.txt"},
	} {
		if !bot.has(want.Kind, want.Target, want.Body) {
			t.Errorf("两路共用的通路应留下 %+v, got %v", want, bot.recorded())
		}
	}
}

// TestSendToolPassiveBranchCallsSharedSender 钉住 被动通道的出网面只剩共享函数。
// - deliverTurn 的分支体只调 sendToUser；
// - 文本、长文、附件、typing 的直接调用一律不留在分支里（零第二通路）。
func TestSendToolPassiveBranchCallsSharedSender(t *testing.T) {
	src, err := os.ReadFile("host_runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	seg := between(string(src), "func (h *hostRuntime) deliverTurn(", "// deliver is the one outbound entry")
	if seg == "" {
		t.Fatal("host_runtime.go 里找不到被动投递函数")
	}
	if !strings.Contains(seg, "sendToUser(") {
		t.Fatalf("被动分支应经共享发送函数出网, got %q", seg)
	}
	for _, forbidden := range []string{"SendTextToUser(", "SendLongText(", "DeliverFiles(", "StopTyping(", "GetContextToken("} {
		if strings.Contains(seg, forbidden) {
			t.Errorf("被动分支内不得留下直接出网调用 %q", forbidden)
		}
	}
}

// between 取两个标记之间的正文（找不到标记时返回空串）。
func between(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	j := strings.Index(text[i:], end)
	if j < 0 {
		return ""
	}
	return text[i+len(start) : i+j]
}

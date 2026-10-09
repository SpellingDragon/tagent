// send_tool_test 钉住 主动送达面的模型可见契约与宿主通路复用：参数封闭、8KiB 上界、拒绝与成功文案、
// 授予即授权的装配面，以及主动/被动两路汇于同一发送实现的端到端闭环。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// eventLog 是替身通道的记录本：并发下可复核的事件清单（-race 友好）。
type eventLog struct {
	mu    sync.Mutex
	items []string
}

func (l *eventLog) add(format string, args ...any) {
	l.mu.Lock()
	l.items = append(l.items, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.items...)
}

func (l *eventLog) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.items)
}

// joined 把清单并成一个串，供"某副作用是否发生过"的粗粒度断言。
func (l *eventLog) joined() string { return strings.Join(l.all(), "\n") }

// mockOutbound 是替身通道：记录每一笔文本/媒体/typing/context-token 接触，不触碰真实微信。
type mockOutbound struct {
	events    eventLog
	tokenText string
	failText  error
}

var _ wechatOutbound = (*mockOutbound)(nil)

func (m *mockOutbound) SendTextToUser(_ context.Context, to, text string) error {
	if m.failText != nil {
		return m.failText
	}
	m.events.add("text|%s|%s", to, truncateLogN(text, 40))
	return nil
}

func (m *mockOutbound) SendImageFromPath(_ context.Context, to, path string) error {
	m.events.add("image|%s|%s", to, filepath.Base(path))
	return nil
}

func (m *mockOutbound) SendVoiceFromPath(_ context.Context, to, path string, _ int) error {
	m.events.add("voice|%s|%s", to, filepath.Base(path))
	return nil
}

func (m *mockOutbound) SendVideoFromPath(_ context.Context, to, path string) error {
	m.events.add("video|%s|%s", to, filepath.Base(path))
	return nil
}

func (m *mockOutbound) SendFileFromPath(_ context.Context, to, path string) error {
	m.events.add("file|%s|%s", to, filepath.Base(path))
	return nil
}

func (m *mockOutbound) StopTyping(_ context.Context, to string) error {
	m.events.add("stop-typing|%s", to)
	return nil
}

func (m *mockOutbound) GetContextToken(string) (string, error) { return m.tokenText, nil }

// fakeInjector 收投递终态回执：通路在失败时是否照既有纪律记档，断言在此取材。
type fakeInjector struct {
	events eventLog
}

func (f *fakeInjector) InjectMessageWithSource(source string, msg model.Message) {
	f.events.add("%s|%s", source, truncateLogN(msg.Content, 60))
}

// newTestSender 造一条挂在替身通道上的发送通路：lastActive 是唯一的可回退目标。
func newTestSender(bot *mockOutbound, inj *fakeInjector, lastActive, workspaceDir string) *hostSender {
	typing := &sync.Map{}
	active := &sync.Map{}
	if lastActive != "" {
		active.Store("latest", lastActive)
	}
	return &hostSender{
		bot: bot,
		longText: func(_ context.Context, chatID, content, _ string) error {
			bot.events.add("long|%s|%d", chatID, len(content))
			return nil
		},
		typingActive:   typing,
		lastActiveChat: active,
		inj:            inj,
		workspaceDir:   workspaceDir,
	}
}

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

// recordingSeam 返回发送缝替身与其记录本：label 是假装送达的目标，given 是假装发生的失败。
func recordingSeam(label string, given error) (SendFunc, *eventLog) {
	var log eventLog
	return func(_ context.Context, content string) (string, error) {
		log.add("seam|%s", content)
		return label, given
	}, &log
}

// mustCall 调一次工具并核对"拒绝不断回合"的前提：err 为空。
func mustCall(t *testing.T, tool *SendTool, args string) string {
	t.Helper()
	res, err := tool.Call(context.Background(), []byte(args))
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
	seam, log := recordingSeam("chat-1", nil)
	tool := NewSendTool(seam)

	res := mustCall(t, tool, fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("x", contentMaxBytes+1)))
	if !strings.Contains(res, "[send_denied] content exceeds 8KiB 上界（实际 8193 字节）") {
		t.Fatalf("超限文案 = %q", res)
	}
	if log.len() != 0 {
		t.Fatalf("超限不得触缝, got %v", log.all())
	}

	res = mustCall(t, tool, fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("x", contentMaxBytes)))
	if !strings.Contains(res, "[send_ok]") {
		t.Fatalf("等界内容应合法送达, got %q", res)
	}
}

// TestSendToolEmptyContentRefused 钉住 参数校验：缺位与空串都是具名拒绝，缝未被触碰。
func TestSendToolEmptyContentRefused(t *testing.T) {
	seam, log := recordingSeam("chat-1", nil)
	tool := NewSendTool(seam)

	for _, args := range []string{`{}`, `{"content":""}`} {
		res := mustCall(t, tool, args)
		if !strings.Contains(res, "[send_denied] content 不能为空") {
			t.Fatalf("空正文文案(%s) = %q", args, res)
		}
	}
	if log.len() != 0 {
		t.Fatalf("被拒的调用不得触缝, got %v", log.all())
	}
}

// TestSendToolNoTargetRefusal 钉住 目标三级解析落空的拒绝面：回合继续、文案点名目标规则。
func TestSendToolNoTargetRefusal(t *testing.T) {
	seam, _ := recordingSeam("", errNoSendTarget)
	tool := NewSendTool(seam)

	res := mustCall(t, tool, `{"content":"想说的一句话"}`)
	if !strings.HasPrefix(res, "[send_denied] 无可解析的送达目标") {
		t.Fatalf("无目标文案 = %q", res)
	}
	if !strings.Contains(res, "目标规则：本会话盖章 > 最近活跃") {
		t.Fatalf("拒绝文案须点名目标规则, got %q", res)
	}
}

// TestSendToolSuccessReceiptNamesTarget 钉住 成功回执：送达目标随文案回模型。
func TestSendToolSuccessReceiptNamesTarget(t *testing.T) {
	seam, log := recordingSeam("chat-7", nil)
	tool := NewSendTool(seam)

	res := mustCall(t, tool, `{"content":"整理完毕：三项已收口"}`)
	if !strings.Contains(res, "[send_ok] 已送达（目标 chat-7）") {
		t.Fatalf("成功回执 = %q", res)
	}
	if got := log.all(); len(got) != 1 || got[0] != "seam|整理完毕：三项已收口" {
		t.Fatalf("正文须原样进缝, got %v", got)
	}
}

// TestSendToolForgedTargetRefused 钉住 身份不可伪造：夹带目标/通道字段的调用具名拒绝，缝未被触碰。
func TestSendToolForgedTargetRefused(t *testing.T) {
	seam, log := recordingSeam("chat-1", nil)
	tool := NewSendTool(seam)

	res := mustCall(t, tool, `{"content":"c","chat_id":"victim","channel":"webhook"}`)
	if !strings.HasPrefix(res, "[send_denied] 参数含声明之外的字段 'channel', 'chat_id'") {
		t.Fatalf("夹带字段文案 = %q", res)
	}
	if !strings.Contains(res, "不可指定或覆写") {
		t.Fatalf("文案须说明身份固定, got %q", res)
	}
	if log.len() != 0 {
		t.Fatalf("被拒的调用不得触缝, got %v", log.all())
	}
}

// TestSendToolUnboundSeamRefuses 钉住 晚绑定窗口的失败模式：缝未接线时具名拒绝而非 panic。
func TestSendToolUnboundSeamRefuses(t *testing.T) {
	tool := NewSendTool(nil)
	res := mustCall(t, tool, `{"content":"c"}`)
	if !strings.Contains(res, "[send_denied] 宿主未接线发送缝") {
		t.Fatalf("未接线文案 = %q", res)
	}

	seamTool := NewSendTool((&hostSendSeam{}).send)
	res = mustCall(t, seamTool, `{"content":"c"}`)
	if !strings.Contains(res, "[send_denied] 宿主发送通路尚未就绪") {
		t.Fatalf("未绑定的缝须具名拒绝, got %q", res)
	}

	seam, log := recordingSeam("chat-1", nil)
	seamTool.SetSend(seam)
	res = mustCall(t, seamTool, `{"content":"c"}`)
	if !strings.Contains(res, "[send_ok]") {
		t.Fatalf("SetSend 之后应直达发送缝, got %q", res)
	}
	if log.len() != 1 {
		t.Fatalf("SetSend 之后调用直达发送缝, got %v", log.all())
	}
}

// TestSendToolProtocolError 钉住 err 的留守领地：解不出参数形态才是协议错。
func TestSendToolProtocolError(t *testing.T) {
	tool := NewSendTool(nil)
	res, err := tool.Call(context.Background(), []byte(`{"content":`))
	if res != nil {
		t.Fatalf("协议错不得产出结果, got %v", res)
	}
	if err == nil || !strings.Contains(err.Error(), "send: malformed arguments") {
		t.Fatalf("协议错文案缺失: %v", err)
	}
}

// testConfigFor 给装配用例一套两 agent 组织：host 声明 send，peer 什么工具都不声明。
func testConfigFor(dir string) tagent.Config {
	return tagent.Config{
		Entry: "host",
		Agents: map[string]tagent.AgentConfig{
			"host": {
				SystemPrompt: tagent.PromptConfig{Inline: "SEND-E2E-HOST-PROMPT"},
				Memory:       tagent.MemoryConfig{Type: "memory", Path: dir},
				Tools: []tagent.ToolRef{
					{Kind: tagent.ToolKindTool, ID: sendToolID},
					{Kind: tagent.ToolKindAgent, AgentID: "peer", Description: "同进程伙伴"},
				},
			},
			"peer": {
				SystemPrompt: tagent.PromptConfig{Inline: "SEND-E2E-PEER-PROMPT"},
				Memory:       tagent.MemoryConfig{Type: "memory", Path: dir},
			},
		},
	}
}

// toolNames 取一个 agent 的工具声明名清单。
func toolNames(a *agent.TagentAgent) []string {
	var out []string
	for _, tl := range a.Tools() {
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// TestSendToolAssemblyOnlyDeclaredAgentsSeeSend 钉住 能力即授权：声明 ToolRef 的 agent 才看得见 send。
// - host 声明了 {kind: tool, id: send}，其工具面里有它；
// - peer 未声明，其工具面里没有——同一次装配、同一注册表。
func TestSendToolAssemblyOnlyDeclaredAgentsSeeSend(t *testing.T) {
	bindSendSeamForTest(t, newTestSender(&mockOutbound{}, &fakeInjector{}, "chat-1", t.TempDir()))

	entry, err := tagent.New(testConfigFor(t.TempDir()), tagent.WithModel(&sendScriptModel{}))
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}
	t.Cleanup(func() { _ = entry.Close() })

	if !containsName(toolNames(entry), sendToolID) {
		t.Fatalf("声明了 send 的 agent 工具面须含它, got %v", toolNames(entry))
	}
	peer := entry.ResidentTable()["peer"]
	if peer == nil {
		t.Fatal("peer 未进常驻表，装配拓扑不成立")
	}
	peerNames := toolNames(peer)
	if containsName(peerNames, sendToolID) {
		t.Fatalf("未声明 send 的 agent 不得看见它, got %v", peerNames)
	}
}

// TestSendToolCapabilityYamlGrantsEntryAndMeditator 钉住 默认示例的授予面：entry 与 meditator 声明 send。
// - 其余子 agent 未声明，模型不可见；
// - 声明走 yaml ToolRef，装配期不硬塞给任何 agent。
func TestSendToolCapabilityYamlGrantsEntryAndMeditator(t *testing.T) {
	cfg, err := tagent.LoadConfig("tagent.yaml")
	if err != nil {
		t.Fatalf("load tagent.yaml: %v", err)
	}
	for _, name := range []string{"tagent", "meditator"} {
		if !declaresSend(cfg.Agents[name].Tools) {
			t.Errorf("agent %q 应声明 {kind: tool, id: send}", name)
		}
	}
	for _, name := range []string{"knowledge", "recall", "action", "plan"} {
		if declaresSend(cfg.Agents[name].Tools) {
			t.Errorf("agent %q 未获授予，却声明了 send", name)
		}
	}
}

// declaresSend 报告工具引用清单里是否有 {kind: tool, id: send}。
func declaresSend(refs []tagent.ToolRef) bool {
	for _, tr := range refs {
		if tr.Kind == tagent.ToolKindTool && tr.ID == sendToolID {
			return true
		}
	}
	return false
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// e2eSeam 与 e2eRegistryOnce：注册表同 id 重复登记会 panic，故进程内只登记一次，
// 每条用例把缝重绑到本轮的替身通路。
var (
	e2eSeam         = &hostSendSeam{}
	e2eRegistryOnce sync.Once
)

func bindSendSeamForTest(t *testing.T, h *hostSender) {
	t.Helper()
	e2eRegistryOnce.Do(func() {
		tagent.GetRegistry().RegisterPlainTool(sendToolID, sendToolFactory(e2eSeam))
	})
	e2eSeam.bind(h)
}

// sendScriptModel 给 host 侧首个回合编排一次 send 调用，后续回合计文本。
type sendScriptModel struct {
	mu       sync.Mutex
	calls    int
	texts    []string
	args     string
	final    string
	toolSent bool
}

func (m *sendScriptModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	var b strings.Builder
	m.mu.Lock()
	for _, msg := range req.Messages {
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	m.calls++
	m.texts = append(m.texts, b.String())
	var resp *model.Response
	switch {
	case m.calls == 1 && !m.toolSent:
		m.toolSent = true
		resp = &model.Response{ID: "send-call-1", Done: true, Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
				Type: "function", ID: "send-call-1",
				Function: model.FunctionDefinitionParam{Name: sendToolID, Arguments: []byte(m.args)},
			}}},
		}}}
	default:
		resp = &model.Response{ID: "send-final-1", Done: true, Choices: []model.Choice{{
			Message: model.Message{Role: model.RoleAssistant, Content: m.finalText()},
		}}}
	}
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *sendScriptModel) finalText() string {
	if m.final != "" {
		return m.final
	}
	return "回合收尾：内部叙述不外泄"
}

func (m *sendScriptModel) Info() model.Info { return model.Info{Name: "send-e2e-model"} }

func (m *sendScriptModel) script(args, final string) {
	m.mu.Lock()
	m.args, m.final = args, final
	m.mu.Unlock()
}

// seen 报告模型的任一请求文本里是否出现过 sub。
func (m *sendScriptModel) seen(sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.texts {
		if strings.Contains(t, sub) {
			return true
		}
	}
	return false
}

func (m *sendScriptModel) turnCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// sendE2ERig 是宿主侧最小进程：真实装配的 agent 挂在替身通道上，回合输出被引流到此取证。
type sendE2ERig struct {
	model    *sendScriptModel
	entry    *agent.TagentAgent
	out      *mockOutbound
	receipts *fakeInjector
	sender   *hostSender
	lineage  eventLog
}

// newSendE2ERig 装配 entry、起循环并收集输出事件的血统判定。
func newSendE2ERig(t *testing.T, lastActive string) *sendE2ERig {
	t.Helper()
	rig := &sendE2ERig{
		model:    &sendScriptModel{},
		out:      &mockOutbound{},
		receipts: &fakeInjector{},
	}
	rig.sender = newTestSender(rig.out, rig.receipts, lastActive, t.TempDir())
	bindSendSeamForTest(t, rig.sender)

	entry, err := tagent.New(testConfigFor(t.TempDir()), tagent.WithModel(rig.model))
	if err != nil {
		t.Fatalf("assembly failed: %v", err)
	}
	t.Cleanup(func() { _ = entry.Close() })
	rig.entry = entry

	out, err := entry.StartLoop("u", "send-e2e-session")
	if err != nil {
		t.Fatalf("start loop: %v", err)
	}
	go func() {
		for evt := range out {
			if evt == nil || !evt.IsFinalResponse() || evt.Response == nil || len(evt.Response.Choices) == 0 {
				continue
			}
			meta := tagentevent.ParseEventMeta(evt)
			_, deliverable := resolveTriggerSource(meta.TriggerSource)
			rig.lineage.add("%s|%v", meta.TriggerSource, deliverable)
		}
	}()
	return rig
}

// TestSendToolE2EActiveSendReachesUserInNonUserTurn 钉住 非用户回合的显式送达闭环。
// - 脚本化模型在 housekeeping 谱系的回合里调 send，替身通道收到正文；
// - 成功回执回到模型，回合继续；
// - 同一回合的 final 输出仍被被动门判定为不可投递（双通道并存，互不代劳）。
func TestSendToolE2EActiveSendReachesUserInNonUserTurn(t *testing.T) {
	rig := newSendE2ERig(t, "chat-owner")
	rig.model.script(`{"content":"整理完毕：本周三项已收口"}`, "收尾叙述：本轮内部结论")

	rig.entry.InjectMessageWithSource(tagentevent.LineageConsolidationHint,
		model.NewUserMessage("把收口结论告诉用户"))

	eventually(t, "主动通道的正文经宿主通路送达用户", func() bool {
		return strings.Contains(rig.out.events.joined(), "text|chat-owner|整理完毕：本周三项已收口")
	})
	eventually(t, "成功回执回到模型且回合继续", func() bool {
		return rig.model.seen("[send_ok] 已送达（目标 chat-owner）") && rig.model.turnCount() >= 2
	})
	eventually(t, "回合以 final 输出结算", func() bool {
		return rig.lineage.len() >= 1
	})
	if !strings.Contains(rig.lineage.joined(), "|false") {
		t.Fatalf("该回合的被动投递判定应为不可投递, got %v", rig.lineage.all())
	}
	if strings.Contains(rig.out.events.joined(), "收尾叙述") {
		t.Fatalf("被动通道不得替非用户回合把收尾叙述送出去: %v", rig.out.events.all())
	}
}

// TestSendToolE2ENoTargetDeniesAndKeepsTurn 钉住 无可解析目标时的拒绝面。
// - 目标三级落空：[send_denied] 具名文本回到模型，回合继续；
// - 替身通道一笔未记（无发送发生）。
func TestSendToolE2ENoTargetDeniesAndKeepsTurn(t *testing.T) {
	rig := newSendE2ERig(t, "")
	rig.model.script(`{"content":"无人可投的一句话"}`, "收尾叙述")

	rig.entry.InjectMessageWithSource(tagentevent.LineageConsolidationHint,
		model.NewUserMessage("想说点什么"))

	eventually(t, "拒绝文本回到模型且回合继续", func() bool {
		return rig.model.seen("[send_denied] 无可解析的送达目标") && rig.model.turnCount() >= 2
	})
	if got := rig.out.events.all(); len(got) != 0 {
		t.Fatalf("无目标时不得发生任何发送, got %v", got)
	}
}

// TestSendToolE2EBothChannelsFunnelThroughOneSender 钉住 零第二通路：两路共用 sendToUser 一个实现。
// - 主动通道（工具）与被动通道（输出循环的同款调用）都只增加同一到访计数；
// - typing 清理、长文分片、附件投递在两路的副作用形态一致。
func TestSendToolE2EBothChannelsFunnelThroughOneSender(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("附件正文"), 0o644); err != nil {
		t.Fatal(err)
	}
	bot := &mockOutbound{tokenText: "ctx-token"}
	sender := newTestSender(bot, &fakeInjector{}, "chat-owner", dir)
	seam := &hostSendSeam{}
	seam.bind(sender)
	tool := NewSendTool(seam.send)

	sender.typingActive.Store("chat-owner", time.Now())
	sender.typingActive.Store("chat-stamped", time.Now())

	res := mustCall(t, tool, fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("长", 900)))
	if !strings.Contains(res, "[send_ok]") {
		t.Fatalf("主动通道应送达, got %q", res)
	}
	if _, err := sender.sendToUser(context.Background(), sendRequest{
		chatIDHint: "chat-stamped", userName: "alice", triggerSource: "user",
		content: "被动一条 " + note,
	}); err != nil {
		t.Fatalf("被动通道 sendToUser: %v", err)
	}

	if got := sender.sendCalls(); got != 2 {
		t.Fatalf("两路应各到访同一实现一次, got %d", got)
	}
	events := bot.events.joined()
	for _, want := range []string{
		"stop-typing|chat-owner", "stop-typing|chat-stamped",
		"long|chat-owner|", "file|chat-stamped|note.txt",
	} {
		if !strings.Contains(events, want) {
			t.Errorf("两路共用的通路应留下 %q, got %v", want, bot.events.all())
		}
	}
}

// TestSendToolE2ESendFailureStillReceipts 钉住 通路失败面在两路一致：send-failed 回执照记，原因回模型。
func TestSendToolE2ESendFailureStillReceipts(t *testing.T) {
	bot := &mockOutbound{failText: errors.New("wechat: session expired")}
	inj := &fakeInjector{}
	sender := newTestSender(bot, inj, "chat-owner", t.TempDir())
	seam := &hostSendSeam{}
	seam.bind(sender)

	res := mustCall(t, NewSendTool(seam.send), `{"content":"发不出去的一句话"}`)
	if !strings.Contains(res, "[send_denied] wechat: session expired") {
		t.Fatalf("通道失败应带具名原因回模型, got %q", res)
	}
	if !strings.Contains(inj.events.joined(), "send-failed") {
		t.Fatalf("失败终态须按既有纪律记回执, got %v", inj.events.all())
	}
}

// TestSendToolPassiveBranchCallsSharedSender 钉住 被动通道的出网面只剩共享函数。
// - main.go 的 deliverable 分支体只调 sendToUser；
// - 文本、长文、附件、typing 的直接调用一律不留在分支里（零第二通路）。
func TestSendToolPassiveBranchCallsSharedSender(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	seg := between(string(src),
		"case \"user\", \"task\", \"reincarnation\", \"system_alert\":",
		"\n\t\t\t\tdefault:")
	if seg == "" {
		t.Fatal("main.go 里找不到被动投递分支")
	}
	if !strings.Contains(seg, "sender.sendToUser(") {
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

// eventually 轮询等待条件成立，超时即失败：回合驱动是异步的，断言在结果一侧取材。
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// wechat_test 钉住 examples/wechat-bot 的协议适配语义：入站分类与落盘、去重与会话路由、
// 出站唯一通路与文件规则、send 工具契约、审批裁决、启动通知、HTTP 面、定时策展投递，
// 以及 registerHandlers 的真实挂点链路（审批→去重→分类→typing→Drive→清理）。
// 装配走与进程相同的入口，模型与通道换成确定性替身。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// testSessionPrefix 是用例会址的会话前缀：与装配期的会话标签一致。
const testSessionPrefix = "routing-test:tagent:"

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

// meditationDocument 造一份启用定时策展的宿主文档：meditator 观察 tagent 的
// 分区，把反射回流给 tagent；两者共用临时存储根，锚点因此落在被观察事实旁。
func meditationDocument(t *testing.T) *hostDocument {
	t.Helper()
	root := t.TempDir()
	prompt := filepath.Join(t.TempDir(), "meditation.md")
	if err := os.WriteFile(prompt, []byte("本轮只看业务分区有没有值得回流的新结论。"), 0o644); err != nil {
		t.Fatalf("write the meditation prompt: %v", err)
	}
	storage := config.MemorySpec{Type: string(ledger.StorageLocalFile), Path: root}
	doc := &hostDocument{Entry: "tagent", Model: "test-model", PromptDir: t.TempDir()}
	doc.Agents = map[string]agentEntry{
		"tagent": {Model: "test-model", Memory: storage},
		"meditator": {
			Model:  "test-model",
			Memory: config.MemorySpec{Type: storage.Type, Path: storage.Path, ReadNamespaces: []string{"tagent"}},
			Meditation: config.MeditationSpec{
				Enabled:            true,
				Interval:           time.Minute,
				PromptFile:         prompt,
				ObservedNamespaces: []string{"tagent"},
				DeliverTo:          []string{"tagent"},
			},
		},
	}
	return doc
}

// TestMeditationDriverDeliversTheDeclaredReflection 钉住宿主侧定时策展的端到端语义。
//
//   - 计划按声明解析：节奏、观察面、白名单与锚点。
//   - 一次 tick 走同一条反射链，把策展正文送到声明目标的已绑定会话。
//   - 第二次 tick 因无新观察，不产生模型调用与投递。
func TestMeditationDriverDeliversTheDeclaredReflection(t *testing.T) {
	ctx := context.Background()
	doc := meditationDocument(t)
	rig := newRoutingRig(t, doc)

	plan := rig.host.meditationPlan
	if plan == nil {
		t.Fatal("the document enables meditation but the assembly resolved no plan")
	}
	if plan.agent != "meditator" || plan.interval != time.Minute || plan.minGap != 0 {
		t.Fatalf("plan = %s/%s/%s, want meditator at the declared one-minute cadence", plan.agent, plan.interval, plan.minGap)
	}
	wantAnchors := filepath.Join(doc.Agents["tagent"].Memory.Path, "meditation", "meditator", "anchors.json")
	if plan.anchors != wantAnchors {
		t.Fatalf("anchors path = %q, want %q", plan.anchors, wantAnchors)
	}
	if len(plan.observed) != 1 || plan.observed[0] != "tagent" {
		t.Fatalf("observed namespaces = %v, want the declared tagent face", plan.observed)
	}
	if len(plan.targets) != 1 || plan.targets[0] != "tagent" {
		t.Fatalf("delivery white list = %v, want the declared tagent target", plan.targets)
	}

	rig.model.then(finalTurn("先把旧选题放一放。"))
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "这条回答还是老选题"}); err != nil {
		t.Fatalf("drive the observed business turn: %v", err)
	}

	rig.model.then(finalTurn("建议把选题换成用户真正问过的问题。"))
	if err := rig.host.startMeditation(ctx); err != nil {
		t.Fatalf("start the scheduled reflection: %v", err)
	}
	if rig.host.meditation == nil {
		t.Fatal("startMeditation built no driver beside an enabled declaration")
	}
	rig.host.meditation.tick(ctx)

	requests := rig.model.recorded()
	if len(requests) < 2 {
		t.Fatalf("model calls = %d, want the business turn beside the reflection turn", len(requests))
	}
	if !requestCarries(requests[1], "本轮只看业务分区有没有值得回流的新结论。") {
		t.Fatalf("the reflection request does not carry the declared prompt: %+v", requests[1].Messages)
	}
	if !requestCarries(requests[1], "[reflection]") {
		t.Fatalf("the reflection request does not carry the reflection digest: %+v", requests[1].Messages)
	}

	delivered := false
	for _, text := range rig.out.texts() {
		if text.Target == "chat-a" && text.Body == "建议把选题换成用户真正问过的问题。" {
			delivered = true
		}
	}
	if !delivered {
		t.Fatalf("text deliveries = %+v, want the reflection delivered to chat-a", rig.out.texts())
	}
	if _, err := os.Stat(wantAnchors); err != nil {
		t.Fatalf("the anchor record was not persisted at %s: %v", wantAnchors, err)
	}

	beforeCalls := rig.model.callCount()
	beforeTexts := len(rig.out.texts())
	rig.host.meditation.tick(ctx)
	if got := rig.model.callCount(); got != beforeCalls {
		t.Fatalf("the second tick made %d more model call(s), want none without new observation", got-beforeCalls)
	}
	if got := len(rig.out.texts()); got != beforeTexts {
		t.Fatalf("the second tick produced %d more delivery(ies), want none", got-beforeTexts)
	}
}

// scriptModel 按决策顺序回答脚本：未编排的决策仍给出收尾答复，让调用计数偏离
// 的用例在断言处失败而不是挂在驱动里。
type scriptModel struct {
	mu       sync.Mutex
	queue    []func(*model.Request) *model.Response
	calls    int
	requests []model.Request
}

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

func (m *scriptModel) Info() model.Info { return model.Info{Name: "wechat-test-model"} }

func (m *scriptModel) then(responses ...*model.Response) {
	m.mu.Lock()
	for _, resp := range responses {
		answer := resp
		m.queue = append(m.queue, func(*model.Request) *model.Response { return answer })
	}
	m.mu.Unlock()
}

func (m *scriptModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *scriptModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

func finalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Done:    true,
	}
}

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

func (m *mockOutbound) record(kind, target, body string) {
	m.mu.Lock()
	m.events = append(m.events, outboundEvent{Kind: kind, Target: target, Body: body})
	m.mu.Unlock()
}

func (m *mockOutbound) SendTextToUser(_ context.Context, to, text string) error {
	if m.failText != nil {
		return m.failText
	}
	m.record("text", to, text)
	return nil
}

func (m *mockOutbound) SendImageFromPath(_ context.Context, to, path string) error {
	m.record("image", to, path)
	return nil
}

func (m *mockOutbound) SendVoiceFromPath(_ context.Context, to, path string, _ int) error {
	m.record("voice", to, path)
	return nil
}

func (m *mockOutbound) SendVideoFromPath(_ context.Context, to, path string) error {
	m.record("video", to, path)
	return nil
}

func (m *mockOutbound) SendFileFromPath(_ context.Context, to, path string) error {
	m.record("file", to, path)
	return nil
}

func (m *mockOutbound) StopTyping(_ context.Context, to string) error {
	m.record("stop-typing", to, "")
	return nil
}

func (m *mockOutbound) GetContextToken(string) (string, error) { return m.tokenText, nil }

func (m *mockOutbound) recorded() []outboundEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]outboundEvent(nil), m.events...)
}

func (m *mockOutbound) texts() []outboundEvent {
	var out []outboundEvent
	for _, e := range m.recorded() {
		if e.Kind == "text" {
			out = append(out, e)
		}
	}
	return out
}

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

// fakeDownloader 是入站媒体下载面的替身：记录被下载的次数并按需写出载荷。
type fakeDownloader struct {
	mu      sync.Mutex
	payload []byte
	err     error
	calls   int
}

func (d *fakeDownloader) download(w io.Writer) (int64, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	if d.err != nil {
		return 0, d.err
	}
	n, err := w.Write(d.payload)
	return int64(n), err
}

func (d *fakeDownloader) DownloadImageFromItemTo(_ context.Context, _ string, _ *wechat.ImageItem, w io.Writer, _ wechat.DownloadOptions) (int64, error) {
	return d.download(w)
}

func (d *fakeDownloader) DownloadVoiceTo(_ context.Context, _ *wechat.VoiceItem, _ string, w io.Writer, _ wechat.DownloadOptions) (int64, error) {
	return d.download(w)
}

func (d *fakeDownloader) DownloadFileFromItemTo(_ context.Context, _ *wechat.FileItem, _ string, w io.Writer, _ wechat.DownloadOptions) (int64, error) {
	return d.download(w)
}

func (d *fakeDownloader) DownloadVideoFromItemTo(_ context.Context, _ *wechat.VideoItem, _ string, w io.Writer, _ wechat.DownloadOptions) (int64, error) {
	return d.download(w)
}

func (d *fakeDownloader) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// fakeChannel 是挂点注册用的通道替身：出网侧效、入站钩子与 typing 一并记录。
type fakeChannel struct {
	*mockOutbound
	*fakeDownloader

	mu      sync.Mutex
	handler wechat.MessageHandler
	typing  int
	cdn     string
}

var _ wechatChannel = (*fakeChannel)(nil)

func newFakeChannel() *fakeChannel {
	return &fakeChannel{mockOutbound: &mockOutbound{}, fakeDownloader: &fakeDownloader{}, cdn: "https://cdn.example"}
}

func (f *fakeChannel) OnMessage(handler wechat.MessageHandler) {
	f.mu.Lock()
	f.handler = handler
	f.mu.Unlock()
}

func (f *fakeChannel) SendTyping(_ context.Context, to string) error {
	f.mu.Lock()
	f.typing++
	f.mu.Unlock()
	f.record("typing", to, "")
	return nil
}

func (f *fakeChannel) CDNBaseURL() string { return f.cdn }

func (f *fakeChannel) emit(ctx context.Context, msg *wechat.Message) error {
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	if handler == nil {
		return errors.New("wechat-test: no inbound handler registered")
	}
	return handler(ctx, msg)
}

func (f *fakeChannel) typingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.typing
}

// routingRig 是一台挂在替身通道上的宿主：装配入口与进程相同，模型与通道为确定性替身。
type routingRig struct {
	host  *hostRuntime
	model *scriptModel
	seam  *hostSendSeam
	out   *mockOutbound
}

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
			native.Tools = []trpctool.CallableTool{newSendTool(seam)}
		}
		natives[event.AgentID(spec.ID)] = native
	}

	out := &mockOutbound{}
	host, err := assembleHostFrom(context.Background(), doc, nil, NewSeenStore(t.TempDir(), discardLogger()), seam, natives, nil)
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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func textMessage(from, text string) *wechat.Message {
	return &wechat.Message{FromUserID: from, ItemList: []wechat.MessageItem{{
		Type:     wechat.ItemTypeText,
		TextItem: &wechat.TextItem{Text: text},
	}}}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func historyCarries(history []model.Message, body string) bool {
	for _, msg := range history {
		if strings.Contains(msg.Content, body) {
			return true
		}
	}
	return false
}

func requestCarries(req model.Request, body string) bool {
	return historyCarries(req.Messages, body)
}

func TestClassifyInbound(t *testing.T) {
	long := strings.Repeat("字", longTextThreshold+1)
	cases := []struct {
		name      string
		msg       *wechat.Message
		workspace bool
		want      InboundKind
	}{
		{"empty", &wechat.Message{}, true, InboundIgnore},
		{"short", textMessage("u", "hello"), true, InboundShortText},
		{"long with workspace", textMessage("u", long), true, InboundLongText},
		{"long without workspace", textMessage("u", long), false, InboundShortText},
		{"media", &wechat.Message{ItemList: []wechat.MessageItem{{
			Type: wechat.ItemTypeImage, ImageItem: &wechat.ImageItem{MidSize: 10},
		}}}, true, InboundMedia},
	}
	for _, tc := range cases {
		if got := ClassifyInbound(tc.msg, tc.workspace); got != tc.want {
			t.Errorf("%s: ClassifyInbound = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestInboundFileSaving(t *testing.T) {
	ws := t.TempDir()
	now := time.Now()

	path, err := buildSavePath(ws, "../../chat", "../../etc/passwd", now)
	if err != nil {
		t.Fatalf("build a traversal path: %v", err)
	}
	base := filepath.Join(ws, uploadsSubdir)
	if !strings.HasPrefix(filepath.Clean(path), base+string(filepath.Separator)) {
		t.Fatalf("path %q escapes the uploads dir %q", path, base)
	}
	if strings.Contains(path, "..") {
		t.Fatalf("path %q retains a traversal segment", path)
	}
	if got := sanitizeName(".."); got != "file" {
		t.Fatalf("sanitizeName(..) = %q, want file", got)
	}

	saved, err := saveInboundFile(ws, "chat-a", "notes.txt", []byte("hello"), now)
	if err != nil {
		t.Fatalf("save an inbound file: %v", err)
	}
	if data, err := os.ReadFile(saved.Path); err != nil || string(data) != "hello" {
		t.Fatalf("saved bytes = %q (err=%v), want hello", data, err)
	}
	if info, err := os.Stat(saved.Path); err != nil || info.Mode().Perm()&0o111 != 0 {
		t.Fatalf("saved file mode = %v (err=%v), want no execute bit", info, err)
	}

	again, err := saveInboundFile(ws, "chat-a", "notes.txt", []byte("hello"), now)
	if err != nil {
		t.Fatalf("save a colliding inbound file: %v", err)
	}
	if again.Path == saved.Path || !strings.HasSuffix(again.Path, "-1.txt") {
		t.Fatalf("collision path = %q, want the -1 suffix beside %q", again.Path, saved.Path)
	}

	if _, err := saveInboundFile(ws, "chat-a", "evil.exe", []byte("MZ"), now); err == nil ||
		!strings.Contains(err.Error(), "可执行文件不允许接收") {
		t.Fatalf("executable intake = %v, want the named refusal", err)
	}
}

func TestIntakeMediaAndLongText(t *testing.T) {
	ws := t.TempDir()
	now := time.Now()
	dl := &fakeDownloader{payload: []byte("PNGDATA")}
	msg := &wechat.Message{FromUserID: "chat-a", ItemList: []wechat.MessageItem{
		{Type: wechat.ItemTypeImage, ImageItem: &wechat.ImageItem{MidSize: 10}},
		{Type: wechat.ItemTypeFile, FileItem: &wechat.FileItem{FileName: "setup.exe"}},
	}}
	outcome := IntakeMedia(context.Background(), dl, "https://cdn", ws, "chat-a", msg, now)
	if len(outcome.Saved) != 1 || outcome.Saved[0].Size != len("PNGDATA") {
		t.Fatalf("intake saved = %+v, want the one image", outcome.Saved)
	}
	if !strings.Contains(outcome.Saved[0].Path, filepath.Join(uploadsSubdir, "chat-a")) {
		t.Fatalf("saved path = %q, want it under uploads/chat-a", outcome.Saved[0].Path)
	}
	if len(outcome.Rejects) != 1 || !strings.Contains(outcome.Rejects[0], "已拒收可执行文件") {
		t.Fatalf("intake rejects = %v, want the executable refusal", outcome.Rejects)
	}

	declared := &fakeDownloader{}
	big := &wechat.Message{ItemList: []wechat.MessageItem{{
		Type: wechat.ItemTypeImage, ImageItem: &wechat.ImageItem{HDSize: maxInboundFileSize + 1},
	}}}
	rejected := IntakeMedia(context.Background(), declared, "https://cdn", ws, "chat-a", big, now)
	if len(rejected.Rejects) != 1 || !strings.Contains(rejected.Rejects[0], "超过大小上限") {
		t.Fatalf("oversize intake rejects = %v", rejected.Rejects)
	}
	if declared.callCount() != 0 {
		t.Fatalf("an oversize item was downloaded %d times, want none", declared.callCount())
	}

	long := strings.Repeat("a", longTextThreshold+10)
	saved, inject, err := SaveLongText(ws, "chat-a", long, "input", now)
	if err != nil {
		t.Fatalf("save a long text: %v", err)
	}
	if !strings.Contains(inject, "已存为文件") || !strings.Contains(inject, saved.Path) {
		t.Fatalf("long-text inject = %q, want the saved path", inject)
	}
	composed := ComposeMediaInject(IntakeOutcome{Saved: []SavedFile{saved}, Transcript: "voice text"}, "附言", ws, "chat-a", now)
	for _, want := range []string{"[用户发来附件]", saved.Path, "voice text", "附言"} {
		if !strings.Contains(composed, want) {
			t.Fatalf("media inject = %q, missing %q", composed, want)
		}
	}
}

// TestDedupKeyAndSeenStore 钉住去重键分层、跨重启见集与损坏文件降级。
//
//   - A corrupted seen.json degrades to an empty set instead of panicking.
func TestDedupKeyAndSeenStore(t *testing.T) {
	withClient := textMessage("u", "same")
	withClient.ClientID = "cid-1"
	if got := DedupKey(withClient); got != "cid:cid-1" {
		t.Fatalf("DedupKey with client id = %q, want cid:cid-1", got)
	}
	same := DedupKey(textMessage("u", "same"))
	if same != DedupKey(textMessage("u", "same")) {
		t.Fatal("the same (sender, text) produced two dedup keys")
	}
	if same == DedupKey(textMessage("u", "other")) {
		t.Fatal("distinct bodies collapsed onto one dedup key")
	}
	if !strings.HasPrefix(same, "uid:u#") || len(same) != len("uid:u#")+16 {
		t.Fatalf("text dedup key = %q, want uid:<sender>#<16 hex>", same)
	}

	dir := t.TempDir()
	store := NewSeenStore(dir, discardLogger())
	if !store.CheckAndMark("key-1") {
		t.Fatal("the first admission was refused")
	}
	if store.CheckAndMark("key-1") {
		t.Fatal("the duplicate admission was accepted")
	}
	reloaded := NewSeenStore(dir, discardLogger())
	if reloaded.CheckAndMark("key-1") {
		t.Fatal("dedup identity did not survive the restart")
	}
	if _, err := os.Stat(filepath.Join(dir, "seen.json")); err != nil {
		t.Fatalf("seen.json was not persisted: %v", err)
	}

	corrupt := t.TempDir()
	if err := os.WriteFile(filepath.Join(corrupt, "seen.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	NewSeenStore(corrupt, discardLogger())
	if _, err := os.Stat(filepath.Join(corrupt, "seen.json.corrupt")); err != nil {
		t.Fatalf("the corrupted store was not quarantined: %v", err)
	}
}

func TestSessionRouter(t *testing.T) {
	router, err := NewSessionRouter("tagent", testSessionPrefix)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	a, err := router.AddressFor("chat-a")
	if err != nil {
		t.Fatalf("address chat-a: %v", err)
	}
	b, err := router.AddressFor("chat-b")
	if err != nil || a == b {
		t.Fatalf("two chats collapsed onto one address: %v / %v", a, b)
	}
	if again, err := router.AddressFor("chat-a"); err != nil || again != a {
		t.Fatalf("address of one chat is not stable: (%v,%v)", again, err)
	}
	if _, err := router.AddressFor("   "); !errors.Is(err, ErrNoChatAddress) {
		t.Fatalf("blank chat identity = %v, want ErrNoChatAddress", err)
	}
	slash, _ := router.AddressFor("team/alpha")
	escaped, _ := router.AddressFor("team%2Falpha")
	if slash == escaped {
		t.Fatalf("distinct chats collapsed through escaping: %v", slash)
	}
	if _, err := NewSessionRouter("tagent", ""); !errors.Is(err, ErrSessionPrefixMissing) {
		t.Fatalf("empty prefix = %v, want ErrSessionPrefixMissing", err)
	}

	if err := router.Bind(a, "chat-a"); err != nil {
		t.Fatalf("bind chat-a: %v", err)
	}
	if err := router.Bind(a, "chat-b"); !errors.Is(err, ErrTargetRebind) {
		t.Fatalf("rebind = %v, want ErrTargetRebind", err)
	}
	if target, ok := router.Target(a); !ok || target != "chat-a" {
		t.Fatalf("target = (%q,%v), want the first identity", target, ok)
	}
	if _, ok := router.Target(testAddress("never-bound")); ok {
		t.Fatal("an unbound address resolved a target")
	}
	if got := router.BoundAddresses("tagent"); len(got) != 1 || got[0] != a {
		t.Fatalf("bound addresses = %v, want [%v]", got, a)
	}
	if got := router.BoundAddresses("other"); got != nil {
		t.Fatalf("bound addresses of another agent = %v, want nil", got)
	}
	if first, second := router.NextInvocation(a), router.NextInvocation(a); first == second {
		t.Fatalf("two drives minted one invocation: %v", first)
	}

	history := []model.Message{{Content: "one"}, {Content: "two"}}
	if fresh := router.TakeUndelivered(a, history); len(fresh) != 2 {
		t.Fatalf("first take = %d messages, want 2", len(fresh))
	}
	if fresh := router.TakeUndelivered(a, history); len(fresh) != 0 {
		t.Fatalf("second take = %d messages, want none", len(fresh))
	}
	history = append(history, model.Message{Content: "three"})
	if fresh := router.TakeUndelivered(a, history); len(fresh) != 1 || fresh[0].Content != "three" {
		t.Fatalf("incremental take = %+v, want only the new message", fresh)
	}
}

func TestFileDeliveryRules(t *testing.T) {
	ws := t.TempDir()
	abs := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(abs, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(ws, "out", "note.txt")
	if err := os.WriteFile(note, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(ws, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh"), 0o644); err != nil {
		t.Fatal(err)
	}

	text := fmt.Sprintf("attach %s and ./out/note.txt, again ./out/note.txt, then ./run.sh", abs)
	got := classifyFilePaths(text, ws)
	if len(got) != 3 {
		t.Fatalf("candidates = %+v, want the three distinct files", got)
	}
	if got[0].Path != abs || got[1].Path != note {
		t.Fatalf("candidate order = %+v, want first-seen order", got)
	}
	if got[2].Path != script || got[2].Refusal != executableRefusal {
		t.Fatalf("executable candidate = %+v, want the named refusal", got[2])
	}
	if bare := classifyFilePaths("./out/note.txt", ""); len(bare) != 0 {
		t.Fatalf("relative candidate beside no workspace = %+v, want none", bare)
	}

	ctx := context.Background()
	img := filepath.Join(ws, "x.png")
	if err := os.WriteFile(img, []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	small := filepath.Join(ws, "v.mp3")
	if err := os.WriteFile(small, []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(ws, "big.mp3")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(voiceMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	vid := filepath.Join(ws, "v.mp4")
	if err := os.WriteFile(vid, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(ws, "d.pdf")
	if err := os.WriteFile(doc, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ path, kind string }{
		{img, "image"}, {small, "voice"}, {big, "file"}, {vid, "video"}, {doc, "file"},
	} {
		out := &mockOutbound{}
		if err := selectSendFn(filepath.Ext(tc.path))(out, ctx, "chat-a", tc.path); err != nil {
			t.Fatalf("send %s: %v", tc.path, err)
		}
		events := out.recorded()
		if len(events) != 1 || events[0].Kind != tc.kind {
			t.Fatalf("send %s = %+v, want one %s event", tc.path, events, tc.kind)
		}
	}
}

func TestHostSenderIsTheOnlyOutboundPath(t *testing.T) {
	ctx := context.Background()
	router, err := NewSessionRouter("tagent", testSessionPrefix)
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := router.AddressFor("chat-a")
	inbox := newInternalInbox()
	out := &mockOutbound{}
	sender := &hostSender{bot: out, router: router, typingActive: &sync.Map{}, inj: inbox, workspaceDir: t.TempDir()}

	if _, err := sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: "hi"}); !errors.Is(err, errNoSendTarget) {
		t.Fatalf("unbound send = %v, want errNoSendTarget", err)
	}
	receipts := inbox.Drain(addr)
	if len(receipts) != 1 || !strings.Contains(receipts[0].Content, "no-target") {
		t.Fatalf("unbound receipts = %+v, want one no-target receipt", receipts)
	}

	if err := router.Bind(addr, "chat-a"); err != nil {
		t.Fatal(err)
	}
	sender.typingActive.Store("chat-a", time.Now())
	if _, err := sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: "hello"}); err != nil {
		t.Fatalf("bound send: %v", err)
	}
	if !out.has("text", "chat-a", "hello") || !out.has("stop-typing", "chat-a", "") {
		t.Fatalf("bound send events = %+v, want text and typing cleanup", out.recorded())
	}
	if _, ok := sender.typingActive.Load("chat-a"); ok {
		t.Fatal("the typing entry survived its delivery")
	}

	out.tokenText = "token-1"
	longCalls := 0
	sender.longText = func(_ context.Context, chatID, content, token string) error {
		longCalls++
		if token != "token-1" {
			t.Errorf("long-text token = %q, want token-1", token)
		}
		out.record("long", chatID, content)
		return nil
	}
	textsBefore := len(out.texts())
	body := strings.Repeat("x", 2001)
	if _, err := sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: body}); err != nil {
		t.Fatalf("long send: %v", err)
	}
	if longCalls != 1 || len(out.texts()) != textsBefore {
		t.Fatalf("long send = %d long calls, %d texts after; want one long call and no plain text", longCalls, len(out.texts())-textsBefore)
	}

	out.tokenText = ""
	fresh := strings.Repeat("y", 2500)
	if _, err := sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: fresh}); err != nil {
		t.Fatalf("truncated send: %v", err)
	}
	texts := out.texts()
	last := texts[len(texts)-1]
	if !strings.HasSuffix(last.Body, "[Message truncated]") || !strings.HasPrefix(last.Body, strings.Repeat("y", 2000)) {
		t.Fatalf("truncated body = %q, want 2000 bytes plus the marker", last.Body)
	}

	out.failText = errors.New("network down")
	if _, err := sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: "fail"}); err == nil {
		t.Fatal("a failed text send reported success")
	}
	receipts = inbox.Drain(addr)
	if len(receipts) != 1 || !strings.Contains(receipts[0].Content, "send-failed") {
		t.Fatalf("failed-send receipts = %+v, want one send-failed receipt", receipts)
	}
	out.failText = nil

	file := filepath.Join(sender.workspaceDir, "report.txt")
	if err := os.WriteFile(file, []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: "see " + file}); err != nil {
		t.Fatalf("file send: %v", err)
	}
	if !out.has("file", "chat-a", file) {
		t.Fatalf("file delivery events = %+v, want %s", out.recorded(), file)
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

func (s *seamRecorder) send(_ context.Context, addr event.Address, content string) (string, error) {
	s.mu.Lock()
	s.addrs = append(s.addrs, addr)
	s.bodies = append(s.bodies, content)
	s.mu.Unlock()
	return s.label, s.given
}

func (s *seamRecorder) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func mustCall(t *testing.T, tool *SendTool, ctx context.Context, args string) string {
	t.Helper()
	res, err := tool.Call(ctx, []byte(args))
	if err != nil {
		t.Fatalf("Call(%s) returned a turn error: %v", args, err)
	}
	text, ok := res.(string)
	if !ok {
		t.Fatalf("Call(%s) result = %T, want string", args, res)
	}
	return text
}

func TestSendToolContract(t *testing.T) {
	decl := NewSendTool(nil).Declaration()
	if decl.Name != sendToolID || len(decl.InputSchema.Properties) != 1 || decl.InputSchema.Properties["content"] == nil {
		t.Fatalf("declaration = %+v, want only content", decl)
	}
	if len(decl.InputSchema.Required) != 1 || decl.InputSchema.Required[0] != "content" {
		t.Fatalf("required = %v, want [content]", decl.InputSchema.Required)
	}
	for _, forbidden := range []string{"chat_id", "target", "to", "user_id", "channel", "token"} {
		if _, ok := decl.InputSchema.Properties[forbidden]; ok {
			t.Errorf("declaration exposes %q; target and channel must stay host-fixed", forbidden)
		}
	}

	rec := &seamRecorder{label: "chat-a"}
	tool := NewSendTool(rec.send)
	ctx := frameCtx(context.Background(), testAddress("chat-a"))

	if res := mustCall(t, tool, context.Background(), `{"content":"x"}`); !strings.Contains(res, "不携带显式会话地址") {
		t.Fatalf("addressless call = %q, want a named refusal", res)
	}
	if res := mustCall(t, tool, ctx, `{"content":"x","to":"chat-b"}`); !strings.Contains(res, "参数含声明之外的字段") || !strings.Contains(res, "'to'") {
		t.Fatalf("forged target = %q, want a named refusal", res)
	}
	for _, args := range []string{`{}`, `{"content":""}`} {
		if res := mustCall(t, tool, ctx, args); !strings.Contains(res, "content 不能为空") {
			t.Fatalf("empty content (%s) = %q", args, res)
		}
	}
	oversize := fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("x", contentMaxBytes+1))
	if res := mustCall(t, tool, ctx, oversize); !strings.Contains(res, "content exceeds 8KiB 上界（实际 8193 字节）") {
		t.Fatalf("oversize = %q", res)
	}
	if rec.calls() != 0 {
		t.Fatalf("refused calls reached the seam %d times", rec.calls())
	}
	if res := mustCall(t, tool, ctx, fmt.Sprintf(`{"content":"%s"}`, strings.Repeat("x", contentMaxBytes))); !strings.Contains(res, "[send_ok]") {
		t.Fatalf("boundary content = %q, want send_ok", res)
	}
	if rec.calls() != 1 || rec.addrs[0] != testAddress("chat-a") || len(rec.bodies[0]) != contentMaxBytes {
		t.Fatalf("seam call = %+v, want one call at the frame address with the full body", rec)
	}

	rec.given = errNoSendTarget
	if res := mustCall(t, tool, ctx, `{"content":"poke"}`); !strings.Contains(res, "[send_denied]") || !strings.Contains(res, errNoSendTarget.Error()) {
		t.Fatalf("seam refusal = %q", res)
	}
	if _, err := tool.Call(ctx, []byte(`not-json`)); err == nil {
		t.Fatal("malformed arguments did not return a protocol error")
	}
	if res := mustCall(t, NewSendTool(nil), ctx, `{"content":"x"}`); !strings.Contains(res, "宿主未接线发送缝") {
		t.Fatalf("unbound tool = %q", res)
	}
}

// TestApprovalGateway 钉住审批请求的下行广播、白名单裁决与无审批人的可见 gap。
//
//   - The announcement fans out to every configured approver.
//   - A gateway beside no approver records a visible delivery gap and stays answerable.
func TestApprovalGateway(t *testing.T) {
	ctx := context.Background()
	gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, []string{"boss"})
	out := &mockOutbound{}
	gateway.SetSender(out)
	manager := gateway.Manager()

	held, err := manager.Request(ctx, "req-unique", "exec", []byte(`{"cmd":"ls"}`), plugin.RiskCritical, "critical", "held critical call")
	if err != nil {
		t.Fatalf("register the held call: %v", err)
	}
	if len(out.texts()) != 1 || !strings.Contains(out.texts()[0].Body, "审批请求") ||
		!strings.Contains(out.texts()[0].Body, shortDigest(held.ArgsDigest)) {
		t.Fatalf("approval announcement = %+v, want the digest over the approver chat", out.texts())
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
	if handled, _, err := gateway.Resolve("boss", "你好"); handled || err != nil {
		t.Fatalf("a plain chat message = (%v,%v), want it to continue as a chat turn", handled, err)
	}
	if handled, _, err := gateway.Resolve("boss", "approve zzzz"); !handled || !errors.Is(err, approval.ErrApprovalNotFound) {
		t.Fatalf("an unknown digest = (%v,%v), want ErrApprovalNotFound", handled, err)
	}
	if handled, _, err := gateway.Resolve("boss", "approve "+approval.ArgsDigest(twinArgs)[:6]); !handled || !errors.Is(err, ErrApprovalAmbiguous) {
		t.Fatalf("an ambiguous digest = (%v,%v), want ErrApprovalAmbiguous", handled, err)
	}
	if got := len(manager.Pending()); got != 3 {
		t.Fatalf("pending after refusals = %d, want all three untouched", got)
	}

	handled, reply, err := gateway.Resolve("boss", "approve "+held.ArgsDigest[:8])
	if !handled || err != nil || !strings.Contains(reply, "approved") || !strings.Contains(reply, "req-unique") {
		t.Fatalf("the unique ruling = (%v,%q,%v)", handled, reply, err)
	}
	if handled, _, err := gateway.Resolve("boss", "approve "+held.ArgsDigest[:8]); !handled || !errors.Is(err, approval.ErrApprovalNotFound) {
		t.Fatalf("a repeated ruling = (%v,%v), want it refused as decided", handled, err)
	}

	if status, prefix, ok := parseApprovalReply("批准 abcd"); !ok || status != approval.ApprovalApproved || prefix != "abcd" {
		t.Fatalf("Chinese approval = (%v,%q,%v)", status, prefix, ok)
	}
	if status, _, ok := parseApprovalReply("拒绝 abcd"); !ok || status != approval.ApprovalDenied {
		t.Fatalf("Chinese rejection = (%v,%v)", status, ok)
	}
	if _, _, ok := parseApprovalReply("hello"); ok {
		t.Fatal("a plain message parsed as a ruling")
	}

	fanout := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, []string{"boss", "ops"})
	fanoutOut := &mockOutbound{}
	fanout.SetSender(fanoutOut)
	req, err := fanout.Manager().Request(ctx, "req-fanout", "exec", []byte(`{"cmd":"pwd"}`), plugin.RiskCritical, "critical", "held")
	if err != nil {
		t.Fatalf("register for fanout: %v", err)
	}
	if texts := fanoutOut.texts(); len(texts) != 2 || texts[0].Target != "boss" || texts[1].Target != "ops" {
		t.Fatalf("approval fanout = %+v, want one text per approver", texts)
	}
	for _, text := range fanoutOut.texts() {
		if !strings.Contains(text.Body, shortDigest(req.ArgsDigest)) || !strings.Contains(text.Body, "exec") {
			t.Fatalf("approval text = %q, want digest and tool", text.Body)
		}
	}

	silent := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, nil)
	if _, err := silent.Manager().Request(ctx, "req-silent", "exec", []byte(`{"cmd":"id"}`), plugin.RiskCritical, "critical", "held"); err != nil {
		t.Fatalf("register beside no approver: %v", err)
	}
	gaps := silent.Manager().DeliveryGaps()
	if len(gaps) != 1 || !strings.Contains(gaps[0].Err.Error(), "no approval recipient") {
		t.Fatalf("delivery gaps = %+v, want one no-recipient gap", gaps)
	}

	if handled, code := runApprovalCLI(silent, []string{"approve", approval.ArgsDigest([]byte(`{"cmd":"id"}`))[:8]}); !handled || code != 0 {
		t.Fatalf("CLI ruling = (%v,%d), want it applied", handled, code)
	}
	if handled, _ := runApprovalCLI(silent, []string{"chat"}); handled {
		t.Fatal("a non-ruling CLI invocation was handled as a ruling")
	}
	if handled, code := runApprovalCLI(silent, []string{"approve", "zzzz"}); !handled || code != 1 {
		t.Fatalf("unknown CLI ruling = (%v,%d), want exit 1", handled, code)
	}
}

func TestReincarnationNotice(t *testing.T) {
	dir := t.TempDir()
	notice := filepath.Join(dir, "REINCARNATION_NOTICE")
	if err := os.WriteFile(notice, []byte("pid: 42\ngen: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !detectReincarnation(notice, time.Now()) {
		t.Fatal("a fresh notice was not detected")
	}
	old := time.Now().Add(-2 * restartDoneFreshWindow)
	if err := os.Chtimes(notice, old, old); err != nil {
		t.Fatal(err)
	}
	if detectReincarnation(notice, time.Now()) {
		t.Fatal("a stale notice was detected as a reincarnation")
	}
	if err := os.Chtimes(notice, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	inbox := newInternalInbox()
	maybeInjectReincarnationNotice(inbox, nil, "tagent", dir, 0)
	msgs := inbox.Drain(event.Address{})
	if len(msgs) != 1 {
		t.Fatalf("reincarnation notices = %d, want one", len(msgs))
	}
	for _, want := range []string{"转世通报", "pid: 42", "现场块不可用"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Fatalf("notice = %q, missing %q", msgs[0].Content, want)
		}
	}
	if _, err := os.Stat(notice + noticeConsumedSuffix); err != nil {
		t.Fatalf("the consumed marker was not written: %v", err)
	}
	maybeInjectReincarnationNotice(inbox, nil, "tagent", dir, 0)
	if got := len(inbox.Drain(event.Address{})); got != 0 {
		t.Fatalf("a consumed notice was injected again (%d messages)", got)
	}

	if got := buildNoticeText(map[string]string{"pid": "1"}, []memory.EventReference{{
		Key: "e1", Type: "action_command", Summary: strings.Repeat("旧", 200), Timestamp: time.Now().UnixMilli(),
	}}, nil); !strings.Contains(got, "断点判定") || !strings.Contains(got, "中断于此") || !strings.Contains(got, "…") {
		t.Fatalf("open-breakpoint notice = %q", got)
	}
	if got := buildNoticeText(nil, nil, errors.New("wal down")); !strings.Contains(got, "NOTICE 档案缺失") || !strings.Contains(got, "wal down") {
		t.Fatalf("degraded notice = %q", got)
	}
	if got := buildNoticeText(map[string]string{}, []memory.EventReference{{Key: "e2", Type: "agent_output", Summary: "done"}}, nil); !strings.Contains(got, "已闭环") {
		t.Fatalf("closed-turn notice = %q", got)
	}
}

func TestSystemAlertConsumption(t *testing.T) {
	dir := t.TempDir()
	alert := filepath.Join(dir, "SYSTEM_ALERT")
	if err := os.WriteFile(alert, []byte("raw failure"), 0o644); err != nil {
		t.Fatal(err)
	}
	inbox := newInternalInbox()
	maybeConsumeSystemAlert(inbox, dir, 0)
	msgs := inbox.Drain(event.Address{})
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "raw failure") {
		t.Fatalf("system alerts = %+v, want the staged failure", msgs)
	}
	if _, err := os.Stat(alert + systemAlertConsumedSuffix); err != nil {
		t.Fatalf("the consumed marker was not written: %v", err)
	}
	maybeConsumeSystemAlert(inbox, dir, 0)
	if got := len(inbox.Drain(event.Address{})); got != 0 {
		t.Fatalf("a consumed alert was injected again (%d messages)", got)
	}
}

type fakeFace struct {
	chat   string
	driven chan inboundMessage
}

func newFakeFace(chat string) *fakeFace {
	return &fakeFace{chat: chat, driven: make(chan inboundMessage, 1)}
}

func (f *fakeFace) Drive(_ context.Context, in inboundMessage) (event.Address, error) {
	f.driven <- in
	return event.Address{AgentID: "tagent", SessionID: "injected"}, nil
}

func (f *fakeFace) mostRecentChat() string { return f.chat }

func TestTaskFace(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	drive := newFakeFace("chat-a")
	handler := newTaskFace(context.Background(), drive, "secret").Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct {
		name   string
		auth   string
		body   string
		remote string
		want   int
	}{
		{"no credential", "", body, "192.0.2.1:1000", http.StatusUnauthorized},
		{"wrong credential", "Bearer nope", body, "192.0.2.1:1000", http.StatusUnauthorized},
		{"malformed body", "Bearer secret", `{`, "192.0.2.1:1000", http.StatusBadRequest},
		{"empty user content", "Bearer secret", `{"messages":[{"role":"user","content":"   "}]}`, "192.0.2.1:1000", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(tc.body))
		req.RemoteAddr = tc.remote
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}

	empty := newFakeFace("")
	req := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	newTaskFace(context.Background(), empty, "secret").Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("injection beside no chat = %d, want 503", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("accepted injection = %d, want 202", rec.Code)
	}
	select {
	case in := <-drive.driven:
		if in.ChatID != "chat-a" || in.Text != "hello" {
			t.Fatalf("injected turn = %+v, want chat-a/hello", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the accepted injection never reached the drive face")
	}

	loopback := newFakeFace("chat-a")
	req = httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	rec = httptest.NewRecorder()
	newTaskFace(context.Background(), loopback, "").Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("loopback injection beside no credential = %d, want 202", rec.Code)
	}
}

func TestHostToolGrantsSendOnlyByDeclaration(t *testing.T) {
	doc := testDocument("tagent", "quiet")
	seam := &hostSendSeam{}
	tools, err := doc.hostTools(context.Background(), "tagent", seam, nil)
	if err != nil {
		t.Fatalf("host tools of the entry agent: %v", err)
	}
	if len(tools) != 1 || tools[0].Declaration().Name != sendToolID {
		t.Fatalf("entry tools = %+v, want only send", tools)
	}
	quiet, err := doc.hostTools(context.Background(), "quiet", seam, nil)
	if err != nil {
		t.Fatalf("host tools of an undeclared agent: %v", err)
	}
	if len(quiet) != 0 {
		t.Fatalf("an agent beside no declaration received %+v", quiet)
	}
}

// TestRegisteredHandlersDriveTheChannel 钉住真实挂点链路：去重、长文落盘与审批裁决。
//
//   - A long text is tokenized into a file before the turn reads it.
//   - A ruling from the whitelisted approver is answered and never becomes a turn.
func TestRegisteredHandlersDriveTheChannel(t *testing.T) {
	rig := newRoutingRig(t, testDocument("tagent"))
	channel := newFakeChannel()
	gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, []string{"boss"})
	cfg := WechatAppConfig{WorkspaceDir: t.TempDir()}
	typing := bindChannel(rig.host, channel, cfg)
	registerHandlers(rig.host, channel, gateway, cfg, typing)

	rig.model.then(finalTurn("answer-a"))
	msg := textMessage("chat-a", "hello")
	if err := channel.emit(context.Background(), msg); err != nil {
		t.Fatalf("inbound delivery: %v", err)
	}
	waitFor(t, "the passive answer", func() bool { return channel.has("text", "chat-a", "answer-a") })
	waitFor(t, "typing cleanup", func() bool { return channel.has("stop-typing", "chat-a", "") })
	if channel.typingCount() != 1 || !channel.has("typing", "chat-a", "") {
		t.Fatalf("typing events = %d (%+v), want one start and one stop", channel.typingCount(), channel.recorded())
	}

	if err := channel.emit(context.Background(), msg); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	if got := rig.model.callCount(); got != 1 {
		t.Fatalf("duplicate delivery drove %d turns, want one", got)
	}

	rig.model.then(finalTurn("long-answer"))
	long := strings.Repeat("长", longTextThreshold+1)
	if err := channel.emit(context.Background(), textMessage("chat-a", long)); err != nil {
		t.Fatalf("long-text delivery: %v", err)
	}
	waitFor(t, "the long-text answer", func() bool { return channel.has("text", "chat-a", "long-answer") })
	requests := rig.model.recorded()
	if len(requests) != 2 || !requestCarries(requests[1], "已存为文件") {
		t.Fatalf("long-text request = %+v, want the saved-file inject", requests)
	}
	files, err := filepath.Glob(filepath.Join(cfg.WorkspaceDir, uploadsSubdir, "chat-a", "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("long-text files = %v (err=%v), want one saved input", files, err)
	}

	gateway.SetSender(channel)
	held, err := gateway.Manager().Request(context.Background(), "req-hook", "exec", []byte(`{"cmd":"ls"}`), plugin.RiskCritical, "critical", "held")
	if err != nil {
		t.Fatalf("register a held call: %v", err)
	}
	if err := channel.emit(context.Background(), textMessage("boss", "approve "+shortDigest(held.ArgsDigest))); err != nil {
		t.Fatalf("approval delivery: %v", err)
	}
	if !channel.has("text", "boss", "审批已记录") {
		t.Fatalf("approval reply = %+v, want the recorded ruling", channel.texts())
	}
	if got := rig.model.callCount(); got != 2 {
		t.Fatalf("an approval ruling drove %d model turns, want none", got-2)
	}
}

// TestHostDrivesSessionsAndTheActiveChannel 钉住会话隔离与主动通道的唯一发送面。
//
//   - Internal notices wait for the session they name.
//   - The active channel leaves through the same sender and only its own chat.
func TestHostDrivesSessionsAndTheActiveChannel(t *testing.T) {
	rig := newRoutingRig(t, testDocument("tagent"))
	ctx := context.Background()
	a, err := rig.host.router.AddressFor("chat-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := rig.host.router.AddressFor("chat-b")
	if err != nil {
		t.Fatal(err)
	}

	rig.model.then(finalTurn("answer-a"))
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "q-a"}); err != nil {
		t.Fatalf("drive chat-a: %v", err)
	}
	if !rig.out.has("text", "chat-a", "answer-a") {
		t.Fatalf("chat-a answer = %+v", rig.out.recorded())
	}

	rig.model.then(finalTurn("answer-b"))
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-b", Text: "q-b"}); err != nil {
		t.Fatalf("drive chat-b: %v", err)
	}
	if historyCarries(rig.host.run.SessionHistory(a), "answer-b") {
		t.Fatal("the chat-b turn leaked into the chat-a transcript")
	}
	if !rig.out.has("text", "chat-b", "answer-b") || rig.out.has("text", "chat-b", "answer-a") {
		t.Fatalf("chat-b deliveries = %+v, want its own answer only", rig.out.recorded())
	}

	rig.host.inbox.InjectInternal(a, receiptLineage, model.Message{Content: "notice-for-a"})
	rig.model.then(finalTurn("answer-b2"))
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-b", Text: "q-b2"}); err != nil {
		t.Fatalf("drive chat-b again: %v", err)
	}
	if requestCarries(rig.model.recorded()[2], "notice-for-a") {
		t.Fatal("a notice addressed to chat-a entered the chat-b request")
	}
	rig.model.then(finalTurn("answer-a2"))
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "q-a2"}); err != nil {
		t.Fatalf("drive chat-a again: %v", err)
	}
	if !requestCarries(rig.model.recorded()[3], "notice-for-a") {
		t.Fatal("the notice did not reach its own session")
	}

	rig.model.then(sendTurn("poke-a"))
	rig.model.then(finalTurn("closing-a"))
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "poke"}); err != nil {
		t.Fatalf("drive the active channel: %v", err)
	}
	if !rig.out.has("text", "chat-a", "poke-a") {
		t.Fatalf("the explicit send did not reach chat-a: %+v", rig.out.recorded())
	}
	if rig.out.has("text", "chat-b", "poke-a") {
		t.Fatalf("the chat-a send leaked into chat-b: %+v", rig.out.recorded())
	}
	requests := rig.model.recorded()
	if len(requests) != 6 || !requestCarries(requests[5], "[send_ok]") || !requestCarries(requests[5], "目标 chat-a") {
		t.Fatalf("the send receipt did not return to the model: %d requests", len(requests))
	}

	stray := event.Address{AgentID: "tagent", SessionID: "never-bound"}
	res, err := newSendTool(rig.seam).Call(frameCtx(ctx, stray), []byte(`{"content":"leak"}`))
	if err != nil {
		t.Fatalf("the refusal must stay a result text: %v", err)
	}
	if text, ok := res.(string); !ok || !strings.HasPrefix(text, "[send_denied] 无可解析的送达目标") {
		t.Fatalf("unbound address result = %v, want a named refusal", res)
	}
	if receipts := rig.host.inbox.Drain(stray); len(receipts) != 1 || !strings.Contains(receipts[0].Content, "no-target") {
		t.Fatalf("stray receipts = %+v, want one no-target receipt", receipts)
	}
	if b == a {
		t.Fatal("two chats collapsed onto one address")
	}
}

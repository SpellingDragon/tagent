// wechat_test.go 钉住应用侧仍然自有的微信语义：入站分类与落盘、文件投递规则、去重与
// 会话路由、唯一出站通路、审批裁决与审批席位、两条启动通知，以及 registerHandlers 的
// 真实挂点链路（审批→去重→分类→typing→Drive→清理）。
//
// 装配（声明→agents→models→工具面→storage→meditation→注入面）已由 tagent.Boot 承担，
// 其核心测试在根包；这里只驱动应用侧运行时，模型回答以脚本替身给出。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// testSessionPrefix 是用例会址的会话前缀：与装配期的会话标签一致。
const testSessionPrefix = "routing-test:tagent:"

func testAddress(chat string) event.Address {
	return event.Address{AgentID: "tagent", SessionID: event.SessionID(testSessionPrefix + chat)}
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

// fakeDownloader 是入站下载替身：把固定载荷写进 Writer，并记录被调次数。
type fakeDownloader struct {
	payload []byte
	given   error
	mu      sync.Mutex
	calls   int
}

func (d *fakeDownloader) download(w io.Writer) (int64, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	if d.given != nil {
		return 0, d.given
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

// routingRig 是一台挂在替身通道上的应用侧运行时：Drive 的模型回答由脚本给出，
// 回答经同一个 sender 端口投递，与核心的路由一致。
type routingRig struct {
	host *hostRuntime
	out  *mockOutbound

	mu       sync.Mutex
	answers  []string
	requests []tagent.InvokeRequest
}

func newRoutingRig(t *testing.T) *routingRig {
	t.Helper()
	t.Setenv("TAGENT_SESSION_ID", "routing-test")

	router, err := NewSessionRouter("tagent", testSessionPrefix)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	out := &mockOutbound{}
	inbox := newInternalInbox()
	sender := &hostSender{
		bot:          out,
		router:       router,
		typingActive: &sync.Map{},
		inj:          inbox,
		workspaceDir: t.TempDir(),
		longText: func(_ context.Context, chatID, content, _ string) error {
			out.record("long", chatID, fmt.Sprintf("%d bytes", len(content)))
			return nil
		},
	}
	rig := &routingRig{out: out}
	rig.host = &hostRuntime{
		router: router,
		inbox:  inbox,
		sender: sender,
		seen:   NewSeenStore(t.TempDir(), discardLogger()),
	}
	rig.host.invoke = func(ctx context.Context, req tagent.InvokeRequest) (bool, error) {
		rig.mu.Lock()
		rig.requests = append(rig.requests, req)
		answer := ""
		if len(rig.answers) > 0 {
			answer, rig.answers = rig.answers[0], rig.answers[1:]
		}
		rig.mu.Unlock()
		if answer == "" {
			return false, nil
		}
		return false, sender.Send(ctx, req.Address, answer)
	}
	return rig
}

func (r *routingRig) then(answers ...string) {
	r.mu.Lock()
	r.answers = append(r.answers, answers...)
	r.mu.Unlock()
}

func (r *routingRig) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *routingRig) recorded() []tagent.InvokeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tagent.InvokeRequest(nil), r.requests...)
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

func requestCarries(req tagent.InvokeRequest, body string) bool {
	for _, msg := range req.Input {
		if strings.Contains(msg.Content, body) {
			return true
		}
	}
	return false
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

	failing := &fakeDownloader{given: errors.New("cdn down")}
	broken := &wechat.Message{ItemList: []wechat.MessageItem{{
		Type: wechat.ItemTypeImage, ImageItem: &wechat.ImageItem{MidSize: 10},
	}}}
	failed := IntakeMedia(context.Background(), failing, "https://cdn", ws, "chat-broken", broken, now)
	if len(failed.Saved) != 0 || len(failed.Rejects) != 1 || !strings.Contains(failed.Rejects[0], "下载失败") {
		t.Fatalf("failed intake = %+v, want the named download refusal", failed)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(ws, uploadsSubdir, "chat-broken", "*")); len(leftovers) != 0 {
		t.Fatalf("a failed download left %v on disk", leftovers)
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

	fresh = strings.Repeat("中", 800)
	if _, err := sender.sendToUser(ctx, sendRequest{addr: addr, triggerSource: sendToolLineage, content: fresh}); err != nil {
		t.Fatalf("multibyte truncated send: %v", err)
	}
	texts = out.texts()
	last = texts[len(texts)-1]
	if !utf8.ValidString(last.Body) || !strings.HasSuffix(last.Body, "[Message truncated]") {
		t.Fatalf("multibyte truncated body is not valid UTF-8: %q", last.Body)
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

// TestApprovalRuling pins the approval gateway rulings.
func TestApprovalRuling(t *testing.T) {
	ctx := context.Background()
	gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, []string{"boss"})
	gateway.SetSender(&mockOutbound{})
	ruling := approvalRuling(gateway)
	args := []byte(`{"cmd":"ls"}`)
	pass := plugin.CallPass{
		Final:         trpcmodel.ToolCall{Function: trpcmodel.FunctionDefinitionParam{Name: "exec", Arguments: args}},
		Risk:          plugin.RiskCritical,
		TriggerSource: "user",
	}
	callCtx := governance.WithCallPass(ctx, pass)

	held, err := ruling(callCtx, "exec", args)
	if err != nil || held.Kind != task.RulingHold {
		t.Fatalf("critical ruling = (%+v,%v), want a hold", held, err)
	}
	if _, err := gateway.Manager().Request(ctx, "approval-1", "exec", args, plugin.RiskCritical, "critical", "held"); err != nil {
		t.Fatalf("register the held call: %v", err)
	}
	if _, _, err := gateway.Resolve("boss", "approve "+shortDigest(approval.ArgsDigest(args))); err != nil {
		t.Fatalf("approve the held call: %v", err)
	}
	approved, err := ruling(callCtx, "exec", args)
	if err != nil || approved.Kind != task.RulingRun {
		t.Fatalf("approved ruling = (%+v,%v), want run", approved, err)
	}

	deniedArgs := []byte(`{"cmd":"rm -rf /"}`)
	deniedPass := plugin.CallPass{
		Final: trpcmodel.ToolCall{Function: trpcmodel.FunctionDefinitionParam{Name: "exec", Arguments: deniedArgs}},
		Risk:  plugin.RiskCritical,
	}
	if _, err := gateway.Manager().Request(ctx, "approval-2", "exec", deniedArgs, plugin.RiskCritical, "critical", "held"); err != nil {
		t.Fatalf("register the second call: %v", err)
	}
	if _, _, err := gateway.Resolve("boss", "reject "+shortDigest(approval.ArgsDigest(deniedArgs))); err != nil {
		t.Fatalf("reject the second call: %v", err)
	}
	denied, err := ruling(governance.WithCallPass(ctx, deniedPass), "exec", deniedArgs)
	if err != nil || denied.Kind != task.RulingDeny {
		t.Fatalf("denied ruling = (%+v,%v), want deny", denied, err)
	}

	if _, err := ruling(ctx, "exec", args); err == nil {
		t.Fatal("a call beside no Before pass was ruled instead of refused closed")
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

// TestRegisteredHandlersDriveTheChannel 钉住真实挂点链路：去重、长文落盘与审批裁决。
//
//   - A long text is tokenized into a file before the turn reads it.
//   - A ruling from the whitelisted approver is answered and never becomes a turn.
func TestRegisteredHandlersDriveTheChannel(t *testing.T) {
	rig := newRoutingRig(t)
	channel := newFakeChannel()
	gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, []string{"boss"})
	cfg := WechatAppConfig{WorkspaceDir: t.TempDir()}
	typing := bindChannel(rig.host, channel, cfg)
	registerHandlers(rig.host, channel, gateway, cfg, typing)

	rig.then("answer-a")
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
	if got := rig.callCount(); got != 1 {
		t.Fatalf("duplicate delivery drove %d turns, want one", got)
	}

	rig.then("long-answer")
	long := strings.Repeat("长", longTextThreshold+1)
	if err := channel.emit(context.Background(), textMessage("chat-a", long)); err != nil {
		t.Fatalf("long-text delivery: %v", err)
	}
	waitFor(t, "the long-text answer", func() bool { return channel.has("text", "chat-a", "long-answer") })
	requests := rig.recorded()
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
	if got := rig.callCount(); got != 2 {
		t.Fatalf("an approval ruling drove %d model turns, want none", got-2)
	}
}

// TestHostDrivesSessionsAndTheActiveChannel 钉住会话隔离与唯一发送面。
//
//   - Internal notices wait for the session they name.
//   - The one sender reaches only its own chat.
func TestHostDrivesSessionsAndTheActiveChannel(t *testing.T) {
	rig := newRoutingRig(t)
	ctx := context.Background()
	a, err := rig.host.router.AddressFor("chat-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := rig.host.router.AddressFor("chat-b")
	if err != nil {
		t.Fatal(err)
	}

	rig.then("answer-a")
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "q-a"}); err != nil {
		t.Fatalf("drive chat-a: %v", err)
	}
	if !rig.out.has("text", "chat-a", "answer-a") {
		t.Fatalf("chat-a answer = %+v", rig.out.recorded())
	}

	rig.then("answer-b")
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-b", Text: "q-b"}); err != nil {
		t.Fatalf("drive chat-b: %v", err)
	}
	if !rig.out.has("text", "chat-b", "answer-b") || rig.out.has("text", "chat-b", "answer-a") {
		t.Fatalf("chat-b deliveries = %+v, want its own answer only", rig.out.recorded())
	}
	requests := rig.recorded()
	if requests[0].Address == requests[1].Address {
		t.Fatalf("two chats collapsed onto one session address: %v", requests[0].Address)
	}

	rig.host.inbox.InjectInternal(a, receiptLineage, tagentmodel.Message{Content: "notice-for-a"})
	rig.then("answer-b2")
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-b", Text: "q-b2"}); err != nil {
		t.Fatalf("drive chat-b again: %v", err)
	}
	if requestCarries(rig.recorded()[2], "notice-for-a") {
		t.Fatal("a notice addressed to chat-a entered the chat-b request")
	}
	rig.then("answer-a2")
	if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "q-a2"}); err != nil {
		t.Fatalf("drive chat-a again: %v", err)
	}
	if !requestCarries(rig.recorded()[3], "notice-for-a") {
		t.Fatal("the notice did not reach its own session")
	}

	if err := rig.host.sender.Send(ctx, a, "poke-a"); err != nil {
		t.Fatalf("the explicit send: %v", err)
	}
	if !rig.out.has("text", "chat-a", "poke-a") {
		t.Fatalf("the explicit send did not reach chat-a: %+v", rig.out.recorded())
	}
	if rig.out.has("text", "chat-b", "poke-a") {
		t.Fatalf("the chat-a send leaked into chat-b: %+v", rig.out.recorded())
	}

	if b == a {
		t.Fatal("two chats collapsed onto one address")
	}
}

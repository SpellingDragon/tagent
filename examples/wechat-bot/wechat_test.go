// wechat_test.go 钉住应用侧仍然自有的微信语义：入站分类与落盘、文件投递规则、去重与
// 会话路由、唯一出站通路、审批裁决与审批席位、两条启动通知，以及 registerHandlers 的
// 真实挂点链路（审批→去重→分类→typing→Drive→清理）。
//
// 装配（声明→agents→models→工具面→storage→meditation→注入面）已由 assembly.Boot 承担，
// 其核心测试在根包；这里只驱动应用侧运行时，模型回答以脚本替身给出。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"encoding/json"
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

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"github.com/SpellingDragon/wechat-robot-go/wechat"
	"trpc.group/trpc-go/trpc-agent-go/log"
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
	requests []assembly.InvokeRequest
}

func newRoutingRig(t *testing.T) *routingRig {
	t.Helper()
	t.Setenv("TAGENT_SESSION_ID", "routing-test")

	router, err := NewSessionRouter("tagent", testSessionPrefix)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	out := &mockOutbound{}
	inbox := memoryInbox(t)
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
	rig.host.invoke = func(ctx context.Context, req assembly.InvokeRequest) (bool, error) {
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

func (r *routingRig) recorded() []assembly.InvokeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]assembly.InvokeRequest(nil), r.requests...)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// memoryInbox opens an inbox that keeps its admission records in memory alone: a
// check that reads the inbox inside one process needs no directory.
func memoryInbox(t *testing.T) *internalInbox {
	t.Helper()
	inbox, err := newInternalInbox("")
	if err != nil {
		t.Fatalf("open the memory inbox: %v", err)
	}
	return inbox
}

// durableInbox opens the inbox under the named directory, the way a restarted
// process opens the one its predecessor left behind.
func durableInbox(t *testing.T, dir string) *internalInbox {
	t.Helper()
	inbox, err := newInternalInbox(dir)
	if err != nil {
		t.Fatalf("open the notice inbox %s: %v", dir, err)
	}
	return inbox
}

func textMessage(from, text string) *wechat.Message {
	return &wechat.Message{FromUserID: from, ItemList: []wechat.MessageItem{{
		Type:     wechat.ItemTypeText,
		TextItem: &wechat.TextItem{Text: text},
	}}}
}

// wechatDeliveryWindow bounds how long one channel delivery may wait before the lock is read as spanning the turn.
const wechatDeliveryWindow = 5 * time.Second

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

func requestCarries(req assembly.InvokeRequest, body string) bool {
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
	inbox := memoryInbox(t)
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

// sharedDigestPrefixArgs answers two argument bodies that differ but whose digests
// share the prefix a ruling may name, beside that prefix: the one shape a prefix
// ambiguity is a real ambiguity, since two registrations of one body read the
// standing request back and are reported once.
func sharedDigestPrefixArgs(t *testing.T, width int) (first, second []byte, prefix string) {
	t.Helper()
	seen := make(map[string][]byte, 512)
	for at := 0; at < 4096; at++ {
		args := []byte(fmt.Sprintf(`{"cmd":"rm","round":%d}`, at))
		digest := approval.ArgsDigest(args)
		if held, ok := seen[digest[:width]]; ok {
			return held, args, digest[:width]
		}
		seen[digest[:width]] = args
	}
	t.Fatalf("no two argument bodies share a %d-character digest prefix", width)
	return nil, nil, ""
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
	twinHeld, twinArgs, twinPrefix := sharedDigestPrefixArgs(t, minApprovalPrefix)
	for at, args := range [][]byte{twinHeld, twinArgs} {
		id := fmt.Sprintf("req-twin-%d", at+1)
		if _, err := manager.Request(ctx, id, "exec", args, plugin.RiskCritical, "critical", "held critical call"); err != nil {
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
	if handled, _, err := gateway.Resolve("boss", "approve "+twinPrefix); !handled || !errors.Is(err, ErrApprovalAmbiguous) {
		t.Fatalf("an ambiguous digest = (%v,%v), want ErrApprovalAmbiguous", handled, err)
	}
	if got := len(manager.Pending()); got != 3 {
		t.Fatalf("pending after refusals = %d, want the three standing questions untouched", got)
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
	callCtx := plugin.WithCallPass(ctx, pass)

	held, err := ruling(callCtx, "exec", args)
	if err != nil || held.Kind != task.RulingHold {
		t.Fatalf("critical ruling = (%+v,%v), want a hold", held, err)
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
	denied, err := ruling(plugin.WithCallPass(ctx, deniedPass), "exec", deniedArgs)
	if err != nil || denied.Kind != task.RulingDeny {
		t.Fatalf("denied ruling = (%+v,%v), want deny", denied, err)
	}

	if _, err := ruling(ctx, "exec", args); err == nil {
		t.Fatal("a call beside no Before pass was ruled instead of refused closed")
	}
}

// TestEventIntegrityApprovalWeChat pins the wechat approval face at the leaf boundary:
// - the reply grammar in both spellings and the request fanout to every bound approver
// - the three named refusals (ambiguous prefix, unknown digest, non-approver)
// - the consumer entry runApprovalCLI writing the ruling back into the response file the governance gate reads through Lookup
func TestEventIntegrityApprovalWeChat(t *testing.T) {
	ctx := context.Background()

	readStored := func(t *testing.T, dir, id string) approval.ApprovalRequest {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, "approvals", id+".json"))
		if err != nil {
			t.Fatalf("read the response file of %s: %v", id, err)
		}
		var stored approval.ApprovalRequest
		if err := json.Unmarshal(raw, &stored); err != nil {
			t.Fatalf("decode the response file of %s: %v", id, err)
		}
		return stored
	}

	requirePending := func(t *testing.T, dir, id string) {
		t.Helper()
		if stored := readStored(t, dir, id); stored.Status != approval.ApprovalPending || stored.DecidedBy != "" {
			t.Fatalf("request %s after a refusal = (%s by %q), want pending and undecided", id, stored.Status, stored.DecidedBy)
		}
	}

	t.Run("parse_approve_reject", func(t *testing.T) {
		dir := t.TempDir()
		gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: dir}, []string{"boss"})
		gateway.SetSender(&mockOutbound{})

		cases := []struct {
			id   string
			args string
			verb string
			want approval.ApprovalStatus
		}{
			{"req-approve", `{"cmd":"ls"}`, "approve", approval.ApprovalApproved},
			{"req-reject", `{"cmd":"rm -rf /"}`, "reject", approval.ApprovalDenied},
			{"req-approve-zh", `{"cmd":"pwd"}`, "批准", approval.ApprovalApproved},
			{"req-reject-zh", `{"cmd":"id"}`, "拒绝", approval.ApprovalDenied},
		}
		for _, tc := range cases {
			args := []byte(tc.args)
			req, err := gateway.Manager().Request(ctx, tc.id, "exec", args, plugin.RiskCritical, "critical", "held")
			if err != nil {
				t.Fatalf("register %s: %v", tc.id, err)
			}
			handled, reply, err := gateway.Resolve("boss", tc.verb+" "+shortDigest(req.ArgsDigest))
			if !handled || err != nil {
				t.Fatalf("%s ruling on %s = (%v,%v), want it handled and applied", tc.verb, tc.id, handled, err)
			}
			if !strings.Contains(reply, tc.id) || !strings.Contains(reply, string(tc.want)) {
				t.Fatalf("%s ruling reply = %q, want the request id and status %s", tc.verb, reply, tc.want)
			}
			if ruled := gateway.Manager().Lookup("exec", approval.ArgsDigest(args)); ruled == nil || ruled.ID != tc.id || ruled.Status != tc.want {
				t.Fatalf("lookup after %q = %+v, want %s in status %s", tc.verb, ruled, tc.id, tc.want)
			}
			if stored := readStored(t, dir, tc.id); stored.Status != tc.want || stored.DecidedBy != "wechat:boss" {
				t.Fatalf("response file of %s = (%s by %q), want %s by wechat:boss", tc.id, stored.Status, stored.DecidedBy, tc.want)
			}
		}
	})

	t.Run("approval_request_fanout", func(t *testing.T) {
		dir := t.TempDir()
		gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: dir}, []string{"boss", "ops"})
		out := &mockOutbound{}
		gateway.SetSender(out)

		req, err := gateway.Manager().Request(ctx, "req-fanout", "exec", []byte(`{"cmd":"pwd"}`), plugin.RiskCritical, "critical", "held")
		if err != nil {
			t.Fatalf("register the fanout request: %v", err)
		}
		texts := out.texts()
		if len(texts) != 2 {
			t.Fatalf("approval fanout = %+v, want one text per bound approver", texts)
		}
		for i, want := range []string{"boss", "ops"} {
			if texts[i].Target != want {
				t.Fatalf("fanout text %d went to %q, want %q", i, texts[i].Target, want)
			}
			if !strings.Contains(texts[i].Body, shortDigest(req.ArgsDigest)) || !strings.Contains(texts[i].Body, "exec") {
				t.Fatalf("fanout text %d = %q, want digest and tool", i, texts[i].Body)
			}
		}
		for _, event := range out.recorded() {
			if event.Target != "boss" && event.Target != "ops" {
				t.Fatalf("approval outbound reached %q, want only the bound approvers", event.Target)
			}
		}
	})

	t.Run("ambiguous_prefix_refused", func(t *testing.T) {
		dir := t.TempDir()
		gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: dir}, []string{"boss"})
		gateway.SetSender(&mockOutbound{})

		heldArgs, twinArgs, prefix := sharedDigestPrefixArgs(t, minApprovalPrefix)
		for at, args := range [][]byte{heldArgs, twinArgs} {
			id := fmt.Sprintf("req-twin-%d", at+1)
			if _, err := gateway.Manager().Request(ctx, id, "exec", args, plugin.RiskCritical, "critical", "held"); err != nil {
				t.Fatalf("register %s: %v", id, err)
			}
		}
		handled, reply, err := gateway.Resolve("boss", "approve "+prefix)
		if !handled || !errors.Is(err, ErrApprovalAmbiguous) {
			t.Fatalf("an ambiguous prefix = (%v,%v), want ErrApprovalAmbiguous", handled, err)
		}
		if !strings.Contains(reply, "审批失败") {
			t.Fatalf("ambiguous refusal reply = %q, want the named refusal text", reply)
		}
		if pending := gateway.Manager().Pending(); len(pending) != 2 {
			t.Fatalf("pending after the ambiguous refusal = %d, want both requests untouched", len(pending))
		}
		requirePending(t, dir, "req-twin-1")
		requirePending(t, dir, "req-twin-2")
	})

	t.Run("unknown_digest_refused", func(t *testing.T) {
		dir := t.TempDir()
		gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: dir}, []string{"boss"})
		gateway.SetSender(&mockOutbound{})

		args := []byte(`{"cmd":"whoami"}`)
		req, err := gateway.Manager().Request(ctx, "req-known", "exec", args, plugin.RiskCritical, "critical", "held")
		if err != nil {
			t.Fatalf("register the known request: %v", err)
		}
		for _, verb := range []string{"approve", "reject"} {
			handled, reply, err := gateway.Resolve("boss", verb+" deadbeef")
			if !handled || !errors.Is(err, approval.ErrApprovalNotFound) {
				t.Fatalf("%s over an unknown digest = (%v,%v), want ErrApprovalNotFound", verb, handled, err)
			}
			if !strings.Contains(reply, "审批失败") {
				t.Fatalf("unknown digest refusal reply = %q, want the named refusal text", reply)
			}
		}
		if pending := gateway.Manager().Pending(); len(pending) != 1 || pending[0].ID != req.ID {
			t.Fatalf("pending after the unknown ruling = %+v, want %s untouched", pending, req.ID)
		}
		if ruled := gateway.Manager().Lookup("exec", approval.ArgsDigest(args)); ruled == nil || ruled.Status != approval.ApprovalPending {
			t.Fatalf("lookup after the unknown ruling = %+v, want the known request still pending", ruled)
		}
		requirePending(t, dir, req.ID)
		entries, err := os.ReadDir(filepath.Join(dir, "approvals"))
		if err != nil {
			t.Fatalf("read the approvals directory: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("response files after the unknown ruling = %d, want only the known request", len(entries))
		}
	})

	t.Run("non_approver_refused", func(t *testing.T) {
		dir := t.TempDir()
		gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: dir}, []string{"boss"})
		out := &mockOutbound{}
		gateway.SetSender(out)

		args := []byte(`{"cmd":"uname -a"}`)
		req, err := gateway.Manager().Request(ctx, "req-held", "exec", args, plugin.RiskCritical, "critical", "held")
		if err != nil {
			t.Fatalf("register the held request: %v", err)
		}
		delivered := len(out.texts())
		handled, reply, err := gateway.Resolve("stranger", "approve "+shortDigest(req.ArgsDigest))
		if !handled || !errors.Is(err, ErrApprovalNotApprover) {
			t.Fatalf("a ruling outside the whitelist = (%v,%v), want ErrApprovalNotApprover", handled, err)
		}
		if !strings.Contains(reply, "没有审批权限") {
			t.Fatalf("non-approver refusal reply = %q, want the named refusal text", reply)
		}
		if texts := out.texts(); len(texts) != delivered {
			t.Fatalf("outbound after the non-approver reply = %d texts, want only the %d delivery text(s)", len(texts), delivered)
		}
		for _, event := range out.recorded() {
			if event.Target != "boss" {
				t.Fatalf("approval outbound reached %q, want only the bound approver", event.Target)
			}
		}
		if pending := gateway.Manager().Pending(); len(pending) != 1 || pending[0].ID != req.ID {
			t.Fatalf("pending after the non-approver reply = %+v, want %s untouched", pending, req.ID)
		}
		requirePending(t, dir, req.ID)

		silent := newApprovalGateway(governanceEntry{Enabled: true, Dir: t.TempDir()}, nil)
		silent.SetSender(&mockOutbound{})
		silentReq, err := silent.Manager().Request(ctx, "req-silent-held", "exec", []byte(`{"cmd":"date"}`), plugin.RiskCritical, "critical", "held")
		if err != nil {
			t.Fatalf("register beside no approver: %v", err)
		}
		if handled, _, err := silent.Resolve("boss", "approve "+shortDigest(silentReq.ArgsDigest)); !handled || !errors.Is(err, ErrApprovalNotApprover) {
			t.Fatalf("a ruling beside no approver = (%v,%v), want ErrApprovalNotApprover", handled, err)
		}
		if pending := silent.Manager().Pending(); len(pending) != 1 {
			t.Fatalf("pending beside no approver = %d, want the request untouched", len(pending))
		}
	})

	t.Run("wechat_cli_entry", func(t *testing.T) {
		dir := t.TempDir()
		gateway := newApprovalGateway(governanceEntry{Enabled: true, Dir: dir, ApprovalTTLMinutes: 30}, []string{"boss"})
		out := &mockOutbound{}
		gateway.SetSender(out)

		args := []byte(`{"cmd":"ls"}`)
		req, err := gateway.Manager().Request(ctx, "req-wechat", "exec", args, plugin.RiskCritical, "critical", "held")
		if err != nil {
			t.Fatalf("register the wechat request: %v", err)
		}
		if texts := out.texts(); len(texts) != 1 || texts[0].Target != "boss" || !strings.Contains(texts[0].Body, shortDigest(req.ArgsDigest)) {
			t.Fatalf("approval announcement = %+v, want the digest over the approver chat", texts)
		}

		if handled, _, err := gateway.Resolve("boss", "approve "+shortDigest(req.ArgsDigest)); !handled || err != nil {
			t.Fatalf("the wechat ruling = (%v,%v), want it applied", handled, err)
		}
		if stored := readStored(t, dir, req.ID); stored.Status != approval.ApprovalApproved || stored.DecidedBy != "wechat:boss" {
			t.Fatalf("wechat response file = (%s by %q), want approved by wechat:boss", stored.Status, stored.DecidedBy)
		}
		if ruled := gateway.Manager().Lookup("exec", approval.ArgsDigest(args)); ruled == nil || ruled.ID != req.ID || ruled.Status != approval.ApprovalApproved {
			t.Fatalf("lookup after the wechat ruling = %+v, want %s approved", ruled, req.ID)
		}

		cliArgs := []byte(`{"cmd":"date"}`)
		cliReq, err := gateway.Manager().Request(ctx, "req-cli-approve", "exec", cliArgs, plugin.RiskCritical, "critical", "held")
		if err != nil {
			t.Fatalf("register the CLI request: %v", err)
		}
		if handled, code := runApprovalCLI(gateway, []string{"approve", shortDigest(cliReq.ArgsDigest)}); !handled || code != 0 {
			t.Fatalf("approve CLI entry = (%v,%d), want it applied", handled, code)
		}
		if stored := readStored(t, dir, cliReq.ID); stored.Status != approval.ApprovalApproved || stored.DecidedBy != "wechat:cli" {
			t.Fatalf("CLI response file = (%s by %q), want approved by wechat:cli", stored.Status, stored.DecidedBy)
		}
		if ruled := gateway.Manager().Lookup("exec", approval.ArgsDigest(cliArgs)); ruled == nil || ruled.ID != cliReq.ID || ruled.Status != approval.ApprovalApproved {
			t.Fatalf("lookup after the approve CLI entry = %+v, want %s approved", ruled, cliReq.ID)
		}

		rejectArgs := []byte(`{"cmd":"rm -rf /"}`)
		rejectReq, err := gateway.Manager().Request(ctx, "req-cli-reject", "exec", rejectArgs, plugin.RiskCritical, "critical", "held")
		if err != nil {
			t.Fatalf("register the CLI rejection: %v", err)
		}
		if handled, code := runApprovalCLI(gateway, []string{"reject", shortDigest(rejectReq.ArgsDigest)}); !handled || code != 0 {
			t.Fatalf("reject CLI entry = (%v,%d), want it applied", handled, code)
		}
		if stored := readStored(t, dir, rejectReq.ID); stored.Status != approval.ApprovalDenied || stored.DecidedBy != "wechat:cli" {
			t.Fatalf("CLI rejection file = (%s by %q), want denied by wechat:cli", stored.Status, stored.DecidedBy)
		}
		if ruled := gateway.Manager().Lookup("exec", approval.ArgsDigest(rejectArgs)); ruled == nil || ruled.ID != rejectReq.ID || ruled.Status != approval.ApprovalDenied {
			t.Fatalf("lookup after the reject CLI entry = %+v, want %s denied", ruled, rejectReq.ID)
		}
	})
}

// TestReincarnationNoticePreserved pins the body of the reincarnation notice: every word the deployment reads survives the move to the fixed ops target.
// - the freshness window decides what a reincarnation is: a marker new at the window is one, a marker older than the window is not (restartDoneFreshWindow)
// - the body keeps its three sections: the [转世通报] opening, the == 元数据 == archive (pid: …) and the scene block, degraded by name when no face reads it
// - the log keeps the [reincarnation] keyword the deployment greps for
// - the consumed marker is the confirmation's word alone, and a redelivery after the confirmation opens no second record
func TestReincarnationNoticePreserved(t *testing.T) {
	ops := opsNoticeAddress(t)
	dir := t.TempDir()
	notice := writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
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

	logs := captureNoticeLogs(t)
	inbox := memoryInbox(t)
	maybeInjectReincarnationNotice(inbox, noticeOps{address: ops}, nil, "tagent", dir, 0)
	msgs, ids := inbox.take(ops)
	if len(msgs) != 1 {
		t.Fatalf("reincarnation notices the ops session reads = %d, want one", len(msgs))
	}
	for _, want := range []string{"[转世通报]", "== 元数据 ==", "pid: 42", "现场块不可用"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Fatalf("notice = %q, missing %q", msgs[0].Content, want)
		}
	}
	if !strings.Contains(logs.String(), "[reincarnation]") {
		t.Fatalf("the log carried no [reincarnation] keyword: %s", logs.String())
	}
	if _, err := os.Stat(notice + noticeConsumedSuffix); err == nil {
		t.Fatal("the consumed marker was written beside the admission, want it only after the confirmation")
	}
	inbox.Confirm(ids)
	if _, err := os.Stat(notice + noticeConsumedSuffix); err != nil {
		t.Fatalf("the consumed marker was not written by the confirmation: %v", err)
	}
	maybeInjectReincarnationNotice(inbox, noticeOps{address: ops}, nil, "tagent", dir, 0)
	if got := len(inbox.Drain(ops)); got != 0 {
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

// captureNoticeLogs swaps the logger the notice logs through for one the check
// reads, so a keyword the deployment greps for is pinned in the test rather than
// trusted to survive a rewrite.
func captureNoticeLogs(t *testing.T) *strings.Builder {
	t.Helper()
	logs := &strings.Builder{}
	prior := log.Default
	log.Default = noticeLogRecorder{logs: logs}
	t.Cleanup(func() { log.Default = prior })
	return logs
}

// noticeLogRecorder is the logger one check reads: every grade lands in the same
// buffer, so a keyword is found whichever grade carried it.
type noticeLogRecorder struct{ logs *strings.Builder }

func (r noticeLogRecorder) Debug(args ...any)                 { r.line(args...) }
func (r noticeLogRecorder) Debugf(format string, args ...any) { r.linef(format, args...) }
func (r noticeLogRecorder) Info(args ...any)                  { r.line(args...) }
func (r noticeLogRecorder) Infof(format string, args ...any)  { r.linef(format, args...) }
func (r noticeLogRecorder) Warn(args ...any)                  { r.line(args...) }
func (r noticeLogRecorder) Warnf(format string, args ...any)  { r.linef(format, args...) }
func (r noticeLogRecorder) Error(args ...any)                 { r.line(args...) }
func (r noticeLogRecorder) Errorf(format string, args ...any) { r.linef(format, args...) }
func (r noticeLogRecorder) Fatal(args ...any)                 { r.line(args...) }
func (r noticeLogRecorder) Fatalf(format string, args ...any) { r.linef(format, args...) }

func (r noticeLogRecorder) line(args ...any) {
	fmt.Fprintln(r.logs, args...)
}

func (r noticeLogRecorder) linef(format string, args ...any) {
	fmt.Fprintf(r.logs, format, args...)
	r.logs.WriteString("\n")
}

func TestSystemAlertConsumption(t *testing.T) {
	ops := opsNoticeAddress(t)
	dir := t.TempDir()
	alert := writeNoticeMarker(t, dir, "SYSTEM_ALERT", "raw failure")
	inbox := memoryInbox(t)
	maybeConsumeSystemAlert(inbox, noticeOps{address: ops}, dir, 0)
	msgs, ids := inbox.take(ops)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content, "raw failure") {
		t.Fatalf("system alerts = %+v, want the staged failure", msgs)
	}
	if _, err := os.Stat(alert + systemAlertConsumedSuffix); err == nil {
		t.Fatal("the consumed marker was written beside the admission, want it only after the confirmation")
	}
	inbox.Confirm(ids)
	if _, err := os.Stat(alert + systemAlertConsumedSuffix); err != nil {
		t.Fatalf("the consumed marker was not written by the confirmation: %v", err)
	}
	maybeConsumeSystemAlert(inbox, noticeOps{address: ops}, dir, 0)
	if got := len(inbox.Drain(ops)); got != 0 {
		t.Fatalf("a consumed alert was injected again (%d messages)", got)
	}
}

// opsNoticeAddress is the address the two startup notices are admitted for: the
// ops session every chat address of this host is spelled beside. The check
// spells it from the same prefix rule the router uses, so a production resolver
// that drifts from that rule is red here.
func opsNoticeAddress(t *testing.T) event.Address {
	t.Helper()
	t.Setenv("TAGENT_SESSION_ID", "routing-test")
	return testAddress("ops")
}

// TestNoticeOpsFixedTarget pins the fixed target of the two startup notices: the ops session address this host declares.
// - both startup notices wait for the ops session, and an unaddressed read takes none of them
// - the declared ops session spells its address from the one prefix rule a chat of this host gets its session from
// - 负锚 no_pending_for_ops_notice: an admission that names no complete address is refused by name instead of landing in a pending queue
// - the target is the one production wiring reports, so a resolver that drifts from the ops session is red here
// - the declared ops session is the session its chat identity gets: a document naming a chat as its ops session lands the notices in that chat
// - it is never the empty address a later reading session would take by arrival order
func TestNoticeOpsFixedTarget(t *testing.T) {
	ops := opsNoticeAddress(t)
	dir := t.TempDir()
	writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
	writeNoticeMarker(t, dir, "SYSTEM_ALERT", "raw failure")
	inbox := durableInbox(t, filepath.Join(dir, testNoticeInboxDir))
	app := &wechatApp{cfg: wechatConfig{Entry: "tagent"}}
	app.host = &hostRuntime{}
	target, scene := app.noticeWiring(nil)

	maybeInjectReincarnationNotice(inbox, target, scene, "tagent", dir, 0)
	maybeConsumeSystemAlert(inbox, target, dir, 0)

	if got := len(inbox.Drain(event.Address{})); got != 0 {
		t.Fatalf("an unaddressed read took %d notices, want the fixed target to decide", got)
	}
	msgs, ids := inbox.take(ops)
	if len(msgs) != 2 || len(ids) != 2 {
		t.Fatalf("the ops session reads %d notices under %d records, want the two startup notices", len(msgs), len(ids))
	}
	for i, want := range []string{"[转世通报]", "raw failure"} {
		if !strings.Contains(msgs[i].Content, want) {
			t.Fatalf("notice %d = %q, missing %q", i, msgs[i].Content, want)
		}
	}
	for _, id := range ids {
		if addr := inbox.records[id].addressOf(); addr != ops {
			t.Fatalf("the notice %s waits for %v, want the ops session %v", id, addr, ops)
		}
	}

	t.Run("declared_ops_session_keeps_the_chat_spelling", func(t *testing.T) {
		router, err := NewSessionRouter("tagent", sessionPrefixOf("tagent"))
		if err != nil {
			t.Fatalf("new router: %v", err)
		}
		chat, err := router.AddressFor("wxid_ops")
		if err != nil {
			t.Fatalf("the chat address: %v", err)
		}
		declaring := &wechatApp{cfg: wechatConfig{Entry: "tagent"}}
		declaring.host = &hostRuntime{}
		declaring.cfg.App.Wechat.OpsSession = "wxid_ops"
		declared, _ := declaring.noticeWiring(nil)
		if declared.address != chat {
			t.Fatalf("the declared ops session = %v, want the address that chat identity gets %v", declared.address, chat)
		}
		if own := opsSessionAddress("tagent", ""); own != ops {
			t.Fatalf("the ops session of a document that declares none = %v, want %v", own, ops)
		}
	})

	t.Run("no_pending_for_ops_notice", func(t *testing.T) {
		fresh := memoryInbox(t)
		_, err := fresh.AdmitNotice(event.Address{}, "reincarnation", tagentmodel.Message{Content: "the notice"}, "", "")
		if err == nil {
			t.Fatal("an admission naming no complete address was accepted, want the named refusal")
		}
		if !errors.Is(err, ErrNoticeAddressIncomplete) {
			t.Fatalf("the refusal = %v, want it named as an incomplete address", err)
		}
		fresh.InjectInternal(event.Address{}, "delivery_receipt", tagentmodel.Message{Content: "the receipt"})
		if got := fresh.Drain(event.Address{}); len(got) != 0 {
			t.Fatalf("an unaddressed admission landed %d notices in a pending queue", len(got))
		}
		if len(fresh.records) != 0 {
			t.Fatalf("records after two refused admissions = %d, want none", len(fresh.records))
		}
	})
}

// TestNoticeSymptomOpsWakeupFixedTarget is the symptom anchor N2.
// - without any user input, a startup notice drives the ops session through one round
// - the session that reads it is the declared one rather than whichever session spoke last
// - the notice alone drives one round, at the ops address, carrying the notice and the source it came from
// - 负锚 arrival_order_does_not_decide_target: two sessions drive in one order and the notice still reaches the ops session, in either order
func TestNoticeSymptomOpsWakeupFixedTarget(t *testing.T) {
	ops := opsNoticeAddress(t)

	t.Run("no user input drives one round", func(t *testing.T) {
		dir := t.TempDir()
		marker := writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
		rig := newRoutingRig(t)

		maybeInjectReincarnationNotice(rig.host.inbox, noticeOps{address: ops, wake: rig.host}, nil, "tagent", dir, 0)

		waitFor(t, "the ops session turn", func() bool { return rig.callCount() == 1 })
		req := rig.recorded()[0]
		if req.Address != ops {
			t.Fatalf("the notice drove %v, want the ops session %v", req.Address, ops)
		}
		if req.TriggerSource != "reincarnation" {
			t.Fatalf("round trigger source = %q, want the notice source", req.TriggerSource)
		}
		if !requestCarries(req, "[转世通报]") {
			t.Fatalf("the ops turn carried no notice: %+v", req.Input)
		}
		if got := req.InputMeta[0][event.MetaKeyNoticeSource]; got != "reincarnation" {
			t.Fatalf("notice source handed to the host = %q, want the source it was admitted from", got)
		}
		if _, err := os.Stat(marker + noticeConsumedSuffix); err != nil {
			t.Fatalf("the woken ops turn wrote no consumed marker: %v", err)
		}
	})

	t.Run("arrival_order_does_not_decide_target", func(t *testing.T) {
		ctx := context.Background()
		for _, order := range [][]string{{"chat-a", "chat-b"}, {"chat-b", "chat-a"}} {
			dir := t.TempDir()
			writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
			rig := newRoutingRig(t)
			for _, chat := range order {
				rig.then("answer-" + chat)
				if _, err := rig.host.Drive(ctx, inboundMessage{ChatID: chat, Text: "q-" + chat}); err != nil {
					t.Fatalf("drive %s: %v", chat, err)
				}
			}

			maybeInjectReincarnationNotice(rig.host.inbox, noticeOps{address: ops, wake: rig.host}, nil, "tagent", dir, 0)
			waitFor(t, "the ops session turn beside two active sessions", func() bool { return rig.callCount() == 3 })

			for _, req := range rig.recorded() {
				if requestCarries(req, "[转世通报]") && req.Address != ops {
					t.Fatalf("the notice reached %v after arrival order %v, want the ops session %v", req.Address, order, ops)
				}
			}
			if !requestCarries(rig.recorded()[2], "[转世通报]") || rig.recorded()[2].Address != ops {
				t.Fatalf("the third turn = %+v, want the notice at the ops session", rig.recorded()[2])
			}
		}
	})
}

// TestNoticeSymptomSceneRealOrDegraded is the symptom anchor N4.
// - the notice the next generation reads carries the recent scene of the previous one: real events, restricted to a few
// - it says by name what it could not read instead of printing an empty block
// - the real store answers the tail: the block carries the last walTailEventLimit events of the agent, newest first
// - 负锚 nil_scene_path_absent: the production wiring never leaves the notices beside a nil face, and a run that observes no tier refuses the read by name
// - 负锚 degraded_reported_clearly: a read that fails is reported in the block with its cause, never silently omitted
func TestNoticeSymptomSceneRealOrDegraded(t *testing.T) {
	ops := opsNoticeAddress(t)

	t.Run("nil_scene_path_absent", func(t *testing.T) {
		app := &wechatApp{cfg: wechatConfig{Entry: "tagent"}}
		app.host = &hostRuntime{}
		for _, host := range []*assembly.Host{nil, {}} {
			target, scene := app.noticeWiring(host)
			if scene == nil {
				t.Fatal("the notices were started beside a nil scene face, want a face that reports the degradation")
			}
			if target.wake == nil {
				t.Fatal("the notices were started beside no wake path, want the host drive face")
			}
			if target.address != ops {
				t.Fatalf("the notices wait for %v, want the ops session %v", target.address, ops)
			}
			if _, err := fetchWALTail(context.Background(), scene, "tagent", walTailEventLimit); !errors.Is(err, errSceneUnwired) {
				t.Fatalf("the read of a run that observes no tier = %v, want the named degradation", err)
			}
		}
	})

	t.Run("real_tail_restricted", func(t *testing.T) {
		dir := t.TempDir()
		writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
		store := memory.NewMemoryEventQuery()
		partition := memory.PartitionIDFromName("tagent")
		now := time.Now().UnixMilli()
		for i := 0; i < walTailEventLimit+3; i++ {
			record := memory.EventRecord{
				Key:         event.EventID(fmt.Sprintf("scene-%02d", i)),
				PartitionID: partition,
				Type:        "agent_output",
				Summary:     fmt.Sprintf("the work of %02d", i),
				Timestamp:   now + int64(i),
			}
			if err := store.Store(context.Background(), record); err != nil {
				t.Fatalf("store the event %d: %v", i, err)
			}
		}

		inbox := memoryInbox(t)
		maybeInjectReincarnationNotice(inbox, noticeOps{address: ops}, sceneFace{read: store.QueryEvents}, "tagent", dir, 0)
		msgs, _ := inbox.take(ops)
		if len(msgs) != 1 {
			t.Fatalf("the ops session reads %d notices, want the one reincarnation notice", len(msgs))
		}
		block := msgs[0].Content
		if strings.Contains(block, "现场块不可用") {
			t.Fatalf("the notice of a readable store degraded: %q", block)
		}
		if got := strings.Count(block, "- [evt_"); got != walTailEventLimit {
			t.Fatalf("the scene block carried %d events, want the last %d", got, walTailEventLimit)
		}
		newest := fmt.Sprintf("the work of %02d", walTailEventLimit+2)
		if !strings.Contains(block, newest) {
			t.Fatalf("the scene block = %q, missing the newest event %q", block, newest)
		}
	})

	t.Run("degraded_reported_clearly", func(t *testing.T) {
		dir := t.TempDir()
		writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
		down := sceneFace{read: func(context.Context, memory.QueryOptions) ([]memory.EventReference, error) {
			return nil, errors.New("wal down")
		}}
		inbox := memoryInbox(t)
		maybeInjectReincarnationNotice(inbox, noticeOps{address: ops}, down, "tagent", dir, 0)
		msgs, _ := inbox.take(ops)
		if len(msgs) != 1 {
			t.Fatalf("the degraded notice = %d messages, want the one the ops session reads", len(msgs))
		}
		for _, want := range []string{"现场块不可用", "wal down"} {
			if !strings.Contains(msgs[0].Content, want) {
				t.Fatalf("degraded notice = %q, missing %q", msgs[0].Content, want)
			}
		}
	})
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

// TestASecondWechatMessageIsNotHeldByTheChannelLock 钉住同一会话的第二条消息不被渠道锁挡在门外。
//
//   - the second delivery of one chat reaches the host while the first turn is still running
//   - that delivery answers no error and no failed turn, which the channel never reads as a failure
//   - the chain read is pinned where the real host runs: TestInflightUserMessageIsAcceptedNotQueued, since this rig fakes the host
func TestASecondWechatMessageIsNotHeldByTheChannelLock(t *testing.T) {
	rig := newRoutingRig(t)
	ctx := context.Background()

	var mu sync.Mutex
	var calls []assembly.InvokeRequest
	entered := make(chan struct{})
	release := make(chan struct{})
	rig.host.invoke = func(ctx context.Context, req assembly.InvokeRequest) (bool, error) {
		mu.Lock()
		calls = append(calls, req)
		index := len(calls)
		mu.Unlock()
		if index == 1 {
			close(entered)
			<-release
		}
		return false, nil
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "第一句"})
		firstDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(wechatDeliveryWindow):
		t.Fatal("the first delivery never reached the host")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := rig.host.Drive(ctx, inboundMessage{ChatID: "chat-a", Text: "第二句"})
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("the second delivery beside the running turn = %v, want the accepted receipt beside no error", err)
		}
	case <-time.After(wechatDeliveryWindow):
		t.Fatal("the second delivery of one chat waited for the first turn to end: the channel lock still spans the whole turn")
	}

	close(release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("the first delivery = %v, want the turn it drove answered beside no error", err)
		}
	case <-time.After(wechatDeliveryWindow):
		t.Fatal("the first delivery never ended after its turn was released")
	}

	mu.Lock()
	recorded := append([]assembly.InvokeRequest(nil), calls...)
	mu.Unlock()
	if len(recorded) != 2 {
		t.Fatalf("invocations = %d, want the two deliveries of one chat", len(recorded))
	}
	if !requestCarries(recorded[0], "第一句") || !requestCarries(recorded[1], "第二句") {
		t.Fatalf("requests = %+v, want each delivery admitted with its own text", recorded)
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

// testNoticeInboxDir is the directory under one check's root that keeps the
// admission records, the way the configuration directory keeps them.
const testNoticeInboxDir = "inbox"

// writeNoticeMarker lands one startup marker the way the restart script left it:
// fresh, at the path the notice hook polls.
func writeNoticeMarker(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the %s marker: %v", name, err)
	}
	return path
}

// noticeDriveRig is one application-side runtime whose turns answer beside the
// material they read: a check counts the turns that read a notice instead of
// trusting the inbox the notices were taken from.
type noticeDriveRig struct {
	inbox *internalInbox
	host  *hostRuntime

	mu       sync.Mutex
	requests []assembly.InvokeRequest
	refusal  error
}

// newNoticeDriveRig builds the runtime over the inbox under check.
func newNoticeDriveRig(t *testing.T, inbox *internalInbox) *noticeDriveRig {
	t.Helper()
	t.Setenv("TAGENT_SESSION_ID", "notice-drive-test")
	router, err := NewSessionRouter("tagent", testSessionPrefix)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	rig := &noticeDriveRig{inbox: inbox}
	rig.host = &hostRuntime{
		router: router,
		inbox:  inbox,
		seen:   NewSeenStore(t.TempDir(), discardLogger()),
	}
	rig.host.invoke = func(_ context.Context, req assembly.InvokeRequest) (bool, error) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		rig.requests = append(rig.requests, req)
		if rig.refusal != nil {
			err := rig.refusal
			rig.refusal = nil
			return false, err
		}
		return false, nil
	}
	return rig
}

// refuseNext makes the next turn answer the named host error.
func (r *noticeDriveRig) refuseNext(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refusal = err
}

// turns counts the turns whose material carried the named body.
func (r *noticeDriveRig) turns(body string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, req := range r.requests {
		if requestCarries(req, body) {
			count++
		}
	}
	return count
}

// drive runs one delivery of the named chat through the runtime.
func (r *noticeDriveRig) drive(t *testing.T, chat, text string) error {
	t.Helper()
	_, err := r.host.Drive(context.Background(), inboundMessage{ChatID: chat, Text: text})
	return err
}

// TestNoticeDurableAdmission pins the durable admission of one startup notice: the notice is a record under a stable identity before any turn reads it.
// - the consumed marker is written only by the confirmation of the turn that read it
// - the marker stays where the admission left it, so the consumed suffix is the confirmation's word alone (负锚 rename_before_confirm_absent)
// - a second process opened on the same directory reads the owed notice back, body and source alike
// - a redelivery of one notice opens no second record
// - a restarted hook rebuilds the same marker with a body the last process never wrote, and the marker rather than the body is the identity
// - a receipt carries no marker: its identity is the source beside the body, and the same receipt read twice is one record
func TestNoticeDurableAdmission(t *testing.T) {
	ops := opsNoticeAddress(t)
	t.Run("rename_before_confirm_absent", func(t *testing.T) {
		dir := t.TempDir()
		marker := writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\ngen: 2\n")
		inbox := durableInbox(t, filepath.Join(dir, testNoticeInboxDir))
		maybeInjectReincarnationNotice(inbox, noticeOps{address: ops}, nil, "tagent", dir, 0)

		if _, err := os.Stat(marker + noticeConsumedSuffix); err == nil {
			t.Fatal("the consumed marker was written beside the admission, want the confirmation to write it")
		}
		msgs, ids := inbox.take(ops)
		if len(msgs) != 1 || len(ids) != 1 {
			t.Fatalf("the admitted notice = %d messages under %d records, want one", len(msgs), len(ids))
		}
		if _, err := os.Stat(marker + noticeConsumedSuffix); err == nil {
			t.Fatal("the read wrote the consumed marker, want the confirmation to write it")
		}
		inbox.Confirm(ids)
		if _, err := os.Stat(marker + noticeConsumedSuffix); err != nil {
			t.Fatalf("the confirmation wrote no consumed marker: %v", err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("the source marker survived its confirmation")
		}
	})

	t.Run("the owed notice reads back in the next process", func(t *testing.T) {
		dir := t.TempDir()
		inboxDir := filepath.Join(dir, testNoticeInboxDir)
		marker := writeNoticeMarker(t, dir, "SYSTEM_ALERT", "raw failure")
		first := durableInbox(t, inboxDir)
		if _, err := first.AdmitNotice(testAddress("chat-a"), "system_alert", tagentmodel.Message{Content: "the staged failure"}, marker, systemAlertConsumedSuffix); err != nil {
			t.Fatalf("admit the alert: %v", err)
		}

		second := durableInbox(t, inboxDir)
		msgs, ids := second.take(testAddress("chat-a"))
		if len(msgs) != 1 || msgs[0].Content != "the staged failure" {
			t.Fatalf("the notice a second process read = %+v, want the one the first admitted", msgs)
		}
		if got := second.records[ids[0]].Source; got != "system_alert" {
			t.Fatalf("the admitted source = %q, want the source the notice came from", got)
		}
	})

	t.Run("a redelivery opens no second record", func(t *testing.T) {
		dir := t.TempDir()
		marker := writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\n")
		inbox := durableInbox(t, filepath.Join(dir, testNoticeInboxDir))
		admit := func(source, body, at string) string {
			id, err := inbox.AdmitNotice(testAddress("chat-a"), source, tagentmodel.Message{Content: body}, at, noticeConsumedSuffix)
			if err != nil {
				t.Fatalf("admit the %s notice: %v", source, err)
			}
			return id
		}

		first := admit("reincarnation", "the notice of the previous generation", marker)
		second := admit("reincarnation", "the notice of the previous generation, rebuilt", marker)
		if first != second {
			t.Fatalf("the redelivery opened a second record (%s then %s)", first, second)
		}
		receipt := admit("delivery_receipt", "the receipt of an undelivered body", "")
		if again := admit("delivery_receipt", "the receipt of an undelivered body", ""); again != receipt {
			t.Fatalf("the receipt redelivery opened a second record (%s then %s)", receipt, again)
		}
		if len(inbox.records) != 2 {
			t.Fatalf("records = %d, want the startup notice and the receipt", len(inbox.records))
		}
	})
}

// TestNoticeRedeliveryIdempotent pins the idempotence of the redelivery: the same notice admitted again keeps one record and reaches exactly one turn.
// - the duplicate admission in this process and the one in the next process both keep the single record
// - the process stops after the admission and before the confirmation, and the next one admits the same marker again to read the notice it owes
func TestNoticeRedeliveryIdempotent(t *testing.T) {
	const body = "the notice the previous generation owed"

	t.Run("duplicate_admission_single_effect", func(t *testing.T) {
		dir := t.TempDir()
		inboxDir := filepath.Join(dir, testNoticeInboxDir)
		marker := writeNoticeMarker(t, dir, "REINCARNATION_NOTICE", "pid: 42\n")

		first := durableInbox(t, inboxDir)
		if _, err := first.AdmitNotice(testAddress("chat-a"), "reincarnation", tagentmodel.Message{Content: body}, marker, noticeConsumedSuffix); err != nil {
			t.Fatalf("admit the notice: %v", err)
		}
		second := durableInbox(t, inboxDir)
		if _, err := second.AdmitNotice(testAddress("chat-a"), "reincarnation", tagentmodel.Message{Content: body + " (rebuilt)"}, marker, noticeConsumedSuffix); err != nil {
			t.Fatalf("admit the redelivery: %v", err)
		}
		if len(second.records) != 1 {
			t.Fatalf("records after the redelivery = %d, want the one the first process admitted", len(second.records))
		}

		rig := newNoticeDriveRig(t, second)
		if err := rig.drive(t, "chat-a", "the question after the restart"); err != nil {
			t.Fatalf("drive the recovered turn: %v", err)
		}
		if got := rig.turns(body); got != 1 {
			t.Fatalf("turns that read the redelivered notice = %d, want exactly one effective processing", got)
		}
		if _, err := os.Stat(marker + noticeConsumedSuffix); err != nil {
			t.Fatalf("the recovered notice wrote no consumed marker: %v", err)
		}

		third := durableInbox(t, inboxDir)
		if len(third.records) != 0 {
			t.Fatalf("records read back after the confirmation = %d, want none", len(third.records))
		}
		maybeInjectReincarnationNotice(third, noticeOps{address: opsNoticeAddress(t)}, nil, "tagent", dir, 0)
		if len(third.records) != 0 {
			t.Fatalf("a consumed notice was admitted again (%d records)", len(third.records))
		}
	})

	t.Run("a consumed notice reads back once across restarts", func(t *testing.T) {
		dir := t.TempDir()
		inboxDir := filepath.Join(dir, testNoticeInboxDir)
		marker := writeNoticeMarker(t, dir, "SYSTEM_ALERT", "raw failure")
		inbox := durableInbox(t, inboxDir)
		if _, err := inbox.AdmitNotice(testAddress("chat-a"), "system_alert", tagentmodel.Message{Content: body}, marker, systemAlertConsumedSuffix); err != nil {
			t.Fatalf("admit the alert: %v", err)
		}
		rig := newNoticeDriveRig(t, inbox)
		if err := rig.drive(t, "chat-a", "the first question"); err != nil {
			t.Fatalf("the first turn: %v", err)
		}
		if err := rig.drive(t, "chat-a", "the second question"); err != nil {
			t.Fatalf("the second turn: %v", err)
		}
		if got := rig.turns(body); got != 1 {
			t.Fatalf("turns that read the consumed notice = %d, want the one turn that took it", got)
		}
	})
}

// TestNoticeDrainAckSemantics pins the three steps of one notice.
// - the drain hands back copies and leaves the record admitted
// - a host that refuses the material keeps the notice owed
// - only the confirmation of a turn the host took retires the record and writes the consumed marker
// - the notice waits for the session the turn drives, so the ack semantics are the subject here rather than the address a startup notice is fixed to
func TestNoticeDrainAckSemantics(t *testing.T) {
	t.Run("failed_invoke_keeps_notice", func(t *testing.T) {
		dir := t.TempDir()
		marker := writeNoticeMarker(t, dir, "SYSTEM_ALERT", "raw failure")
		inbox := durableInbox(t, filepath.Join(dir, testNoticeInboxDir))
		if _, err := inbox.AdmitNotice(testAddress("chat-a"), "system_alert", tagentmodel.Message{Content: "raw failure"}, marker, systemAlertConsumedSuffix); err != nil {
			t.Fatalf("admit the alert: %v", err)
		}
		rig := newNoticeDriveRig(t, inbox)

		rig.refuseNext(errors.New("the host refused the material"))
		if err := rig.drive(t, "chat-a", "the first question"); err == nil {
			t.Fatal("a refused submit answered no error")
		}
		if got := rig.turns("raw failure"); got != 1 {
			t.Fatalf("turns that read the notice beside the refused submit = %d, want the one that was refused", got)
		}
		if len(inbox.records) != 1 {
			t.Fatalf("records after a refused submit = %d, want the notice still admitted", len(inbox.records))
		}
		if _, err := os.Stat(marker + systemAlertConsumedSuffix); err == nil {
			t.Fatal("a refused submit wrote the consumed marker")
		}

		if err := rig.drive(t, "chat-a", "the second question"); err != nil {
			t.Fatalf("the accepted submit: %v", err)
		}
		if got := rig.turns("raw failure"); got != 2 {
			t.Fatalf("turns that read the notice = %d, want the refused one and the one that took it", got)
		}
		if len(inbox.records) != 0 {
			t.Fatalf("records after the accepted submit = %d, want the notice confirmed", len(inbox.records))
		}
		if _, err := os.Stat(marker + systemAlertConsumedSuffix); err != nil {
			t.Fatalf("the confirmation wrote no consumed marker: %v", err)
		}
	})

	t.Run("the drain hands back copies and keeps the record admitted", func(t *testing.T) {
		inbox := memoryInbox(t)
		if _, err := inbox.AdmitNotice(testAddress("chat-a"), "delivery_receipt", tagentmodel.Message{Content: "the receipt"}, "", ""); err != nil {
			t.Fatalf("admit the receipt: %v", err)
		}
		msgs, ids := inbox.take(testAddress("chat-a"))
		if len(msgs) != 1 || len(ids) != 1 {
			t.Fatalf("the drain = %d messages under %d records, want one", len(msgs), len(ids))
		}
		msgs[0].Content = "tampered"
		if got := inbox.records[ids[0]].Content; got != "the receipt" {
			t.Fatalf("the drain handed back the record itself: %q", got)
		}
		if again, _ := inbox.take(testAddress("chat-a")); len(again) != 0 {
			t.Fatalf("a second turn read the notice the first one holds: %+v", again)
		}
		inbox.Release(ids)
		replayed, _ := inbox.take(testAddress("chat-a"))
		if len(replayed) != 1 || replayed[0].Content != "the receipt" {
			t.Fatalf("the released notice reads = %+v, want the one the failed turn left owed", replayed)
		}
	})
}

// lastRequest answers the request of the last turn the rig drove.
func (r *noticeDriveRig) lastRequest(t *testing.T) assembly.InvokeRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("the rig drove no turn")
	}
	return r.requests[len(r.requests)-1]
}

// TestNoticeSourceReachesTheTurnMeta pins the app side of the source split: the notice a turn reads reaches the host beside its admitted source.
// - one entry per message, while the round trigger source stays the user lineage
// - the user message of the same delivery carries no notice source
func TestNoticeSourceReachesTheTurnMeta(t *testing.T) {
	inbox := memoryInbox(t)
	if _, err := inbox.AdmitNotice(testAddress("chat-a"), "reincarnation", tagentmodel.Message{Content: "the notice"}, "", ""); err != nil {
		t.Fatalf("admit the notice: %v", err)
	}
	rig := newNoticeDriveRig(t, inbox)
	if err := rig.drive(t, "chat-a", "the question"); err != nil {
		t.Fatalf("drive the turn: %v", err)
	}
	req := rig.lastRequest(t)
	if len(req.Input) != 2 || len(req.InputMeta) != 2 {
		t.Fatalf("the turn carried %d messages under %d meta entries, want the notice and the question", len(req.Input), len(req.InputMeta))
	}
	if got := req.InputMeta[0][event.MetaKeyNoticeSource]; got != "reincarnation" {
		t.Fatalf("notice source handed to the host = %q, want the source it was admitted from", got)
	}
	if got, ok := req.InputMeta[1][event.MetaKeyNoticeSource]; ok {
		t.Fatalf("the user message carried the notice source %q", got)
	}
	if req.TriggerSource != "user" {
		t.Fatalf("round trigger source = %q, want the user lineage", req.TriggerSource)
	}
}

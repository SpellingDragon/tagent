// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/workspace"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

type fileCall struct {
	method   string
	toUserID string
	path     string
	duration int
}

type mockFileSender struct {
	mu       sync.Mutex
	calls    []fileCall
	failPath string
}

func (m *mockFileSender) record(method, toUserID, path string, duration int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, fileCall{method, toUserID, path, duration})
}

func (m *mockFileSender) maybeFail(path string) error {
	if m.failPath != "" && path == m.failPath {
		return errors.New("injected send failure")
	}
	return nil
}

func (m *mockFileSender) callsOf(method string) []fileCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []fileCall
	for _, c := range m.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func (m *mockFileSender) totalCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockFileSender) SendTextToUser(_ context.Context, toUserID, _ string) error {
	m.record("SendTextToUser", toUserID, "", 0)
	return nil
}

func (m *mockFileSender) SendImageFromPath(_ context.Context, toUserID, imagePath string) error {
	m.record("SendImageFromPath", toUserID, imagePath, 0)
	return m.maybeFail(imagePath)
}

func (m *mockFileSender) SendVoiceFromPath(_ context.Context, toUserID, voicePath string, duration int) error {
	m.record("SendVoiceFromPath", toUserID, voicePath, duration)
	return m.maybeFail(voicePath)
}

func (m *mockFileSender) SendVideoFromPath(_ context.Context, toUserID, videoPath string) error {
	m.record("SendVideoFromPath", toUserID, videoPath, 0)
	return m.maybeFail(videoPath)
}

func (m *mockFileSender) SendFileFromPath(_ context.Context, toUserID, filePath string) error {
	m.record("SendFileFromPath", toUserID, filePath, 0)
	return m.maybeFail(filePath)
}

// compile-time assertion: mock 满足 FileSender 接口
var _ FileSender = (*mockFileSender)(nil)

// writeTempFile 在 dir 下创建名为 name 的文件（自动创建父目录），返回绝对路径。
func writeTempFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte("payload"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if mode&0111 != 0 {
		if err := os.Chmod(full, mode); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
	}
	abs, err := filepath.Abs(full)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	return abs
}

func TestExtractFilePaths(t *testing.T) {
	ws := t.TempDir()
	absPDF := writeTempFile(t, ws, "report.pdf", 0644)
	_ = writeTempFile(t, ws, "data/out.png", 0644)
	relPNG := filepath.Join(ws, "data/out.png")

	execByPerm := writeTempFile(t, ws, "script.txt", 0755)
	execByExt := writeTempFile(t, ws, "run.sh", 0644)

	tests := []struct {
		name  string
		text  string
		wsDir string
		want  []string
	}{
		{
			name:  "绝对路径被识别",
			text:  "报告见 " + absPDF,
			wsDir: "",
			want:  []string{absPDF},
		},
		{
			name:  "相对路径按 workspace 解析",
			text:  "图片在 ./data/out.png",
			wsDir: ws,
			want:  []string{relPNG},
		},
		{
			name:  "URL 被排除",
			text:  "远程图 http://example.com/pic.png 忽略",
			wsDir: "",
			want:  nil,
		},
		{
			name:  "不存在路径被排除",
			text:  "文件 /no/such/path/x.txt",
			wsDir: "",
			want:  nil,
		},
		{
			name:  "无扩展名路径被排除",
			text:  "笔记在 /home/user/docs",
			wsDir: "",
			want:  nil,
		},
		{
			name:  "去重并保持首次出现顺序",
			text:  absPDF + " 与 " + absPDF,
			wsDir: "",
			want:  []string{absPDF},
		},
		{
			name:  "workspace 为空时仅绝对路径生效",
			text:  "绝对 " + absPDF + " 相对 ./data/out.png",
			wsDir: "",
			want:  []string{absPDF},
		},
		{
			name:  "权限位可执行文件被排除",
			text:  "脚本 " + execByPerm,
			wsDir: "",
			want:  nil,
		},
		{
			name:  "扩展名可执行文件被排除",
			text:  "脚本 " + execByExt,
			wsDir: "",
			want:  nil,
		},
		{
			name:  "裸文件名（带扩展名但无路径分隔符）被排除",
			text:  "请查看 report.pdf",
			wsDir: "",
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractFilePaths(tc.text, tc.wsDir)
			if len(got) != len(tc.want) {
				t.Fatalf("期望 %d 个路径，实际 %d 个: %v", len(tc.want), len(got), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("索引 %d: 期望 %q，实际 %q", i, tc.want[i], got[i])
				}
			}
		})
	}
}

func TestSelectSendFn(t *testing.T) {
	cases := []struct {
		ext          string
		wantMethod   string
		wantDuration int
	}{
		{".png", "SendImageFromPath", 0},
		{".jpg", "SendImageFromPath", 0},
		{".jpeg", "SendImageFromPath", 0},
		{".gif", "SendImageFromPath", 0},
		{".webp", "SendImageFromPath", 0},
		{".bmp", "SendImageFromPath", 0},
		{".PNG", "SendImageFromPath", 0},
		{".mp3", "SendVoiceFromPath", 0},
		{".wav", "SendVoiceFromPath", 0},
		{".amr", "SendVoiceFromPath", 0},
		{".m4a", "SendVoiceFromPath", 0},
		{".mp4", "SendVideoFromPath", 0},
		{".mov", "SendVideoFromPath", 0},
		{".avi", "SendVideoFromPath", 0},
		{".webm", "SendVideoFromPath", 0},
		{".mkv", "SendVideoFromPath", 0},
		{".csv", "SendFileFromPath", 0},
		{".txt", "SendFileFromPath", 0},
		{".unknown", "SendFileFromPath", 0},
	}

	for _, c := range cases {
		t.Run(c.ext, func(t *testing.T) {
			m := &mockFileSender{}
			fn := selectSendFn(c.ext)
			path := "/tmp/x" + c.ext
			if err := fn(m, context.Background(), "u1", path); err != nil {
				t.Fatalf("ext %s: 意外错误: %v", c.ext, err)
			}
			calls := m.callsOf(c.wantMethod)
			if len(calls) != 1 {
				t.Fatalf("ext %s: 期望 1 次 %s 调用，实际 %d 次", c.ext, c.wantMethod, len(calls))
			}
			if calls[0].path != path {
				t.Errorf("ext %s: path 不匹配: 期望 %q 实际 %q", c.ext, path, calls[0].path)
			}
			if c.wantMethod == "SendVoiceFromPath" && calls[0].duration != 0 {
				t.Errorf("voice duration 应为 0，实际 %d", calls[0].duration)
			}
		})
	}
}

func TestDeliverFiles(t *testing.T) {
	ws := t.TempDir()
	imgPath := writeTempFile(t, ws, "a.png", 0644)
	docPath := writeTempFile(t, ws, "b.txt", 0644)

	t.Run("图片路径调用 SendImageFromPath", func(t *testing.T) {
		m := &mockFileSender{}
		if err := DeliverFiles(m, context.Background(), "u1", "看图 "+imgPath, ""); err != nil {
			t.Fatalf("意外错误: %v", err)
		}
		if got := m.callsOf("SendImageFromPath"); len(got) != 1 {
			t.Fatalf("期望 1 次 SendImageFromPath，实际 %d", len(got))
		}
		if got := m.callsOf("SendFileFromPath"); len(got) != 0 {
			t.Fatalf("期望 0 次 SendFileFromPath，实际 %d", len(got))
		}
	})

	t.Run("无路径不调用任何 Send*FromPath", func(t *testing.T) {
		m := &mockFileSender{}
		_ = DeliverFiles(m, context.Background(), "u1", "这里没有文件", "")
		if n := m.totalCalls(); n != 0 {
			t.Fatalf("期望 0 次调用，实际 %d", n)
		}
	})

	t.Run("多路径按类型分别投递", func(t *testing.T) {
		m := &mockFileSender{}
		text := "图片 " + imgPath + " 文档 " + docPath
		_ = DeliverFiles(m, context.Background(), "u1", text, "")
		if got := m.callsOf("SendImageFromPath"); len(got) != 1 {
			t.Fatalf("期望 1 次 SendImageFromPath，实际 %d", len(got))
		}
		if got := m.callsOf("SendFileFromPath"); len(got) != 1 {
			t.Fatalf("期望 1 次 SendFileFromPath，实际 %d", len(got))
		}
	})

	t.Run("某文件失败其余仍发送", func(t *testing.T) {
		m := &mockFileSender{failPath: imgPath}
		text := "图片 " + imgPath + " 文档 " + docPath
		_ = DeliverFiles(m, context.Background(), "u1", text, "")
		if got := m.callsOf("SendImageFromPath"); len(got) != 1 {
			t.Fatalf("期望图片仍被尝试 1 次，实际 %d", len(got))
		}
		if got := m.callsOf("SendFileFromPath"); len(got) != 1 {
			t.Fatalf("期望文档仍被发送 1 次，实际 %d", len(got))
		}
	})
}

// fileChannel 是替身通道的文件面：投递时读回文件字节并记录，可插入一次投递期动作，
// 也可按路径注入失败；文本、媒体与 typing 面沿用既有替身。
type fileChannel struct {
	*mockOutbound

	mu         sync.Mutex
	readBytes  map[string][]byte
	readErrs   map[string]error
	failPath   string
	failErr    error
	duringSend func()
}

// SendFileFromPath 记录一次文件投递，连同通道在该时刻读到的字节。
func (c *fileChannel) SendFileFromPath(ctx context.Context, to, path string) error {
	during := c.duringSend
	if during != nil {
		during()
	}
	body, readErr := os.ReadFile(path)
	c.mu.Lock()
	c.readBytes[path] = body
	c.readErrs[path] = readErr
	failed := c.failPath != "" && path == c.failPath
	cause := c.failErr
	c.mu.Unlock()
	if failed {
		if cause == nil {
			cause = errors.New("wechat: upload rejected")
		}
		return cause
	}
	return c.mockOutbound.SendFileFromPath(ctx, to, path)
}

// readOf 报告通道在投递该路径时读到的字节与读取结果。
func (c *fileChannel) readOf(path string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readBytes[path], c.readErrs[path]
}

// deliveredFiles 报告按到达顺序交给通道的文件路径。
func (c *fileChannel) deliveredFiles() []string {
	var out []string
	for _, e := range c.recorded() {
		if e.Kind == "file" {
			out = append(out, e.Body)
		}
	}
	return out
}

// fileRig 是一台经正式装配入口驱动的宿主：模型、通道与副本生命周期都在装配内，
// 只有通道另一端是确定性替身。
type fileRig struct {
	host         *hostRuntime
	model        *scriptModel
	out          *fileChannel
	store        *ledger.MemoryOriginalStore
	replicas     *workspaceReplicas
	workspaceDir string
}

// newFileRig 以给定 workspace 与模型窗口口径装配一台宿主，并接上投递副本生命周期。
func newFileRig(t *testing.T, workspaceDir string, maxTokens int) *fileRig {
	t.Helper()
	t.Setenv("TAGENT_SESSION_ID", "routing-test")

	doc := testDocument("tagent")
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
	host, err := assembleHostFrom(context.Background(), doc, nil, NewSeenStore(t.TempDir(), slog.Default()), seam, natives)
	if err != nil {
		t.Fatalf("assemble the host: %v", err)
	}

	store := ledger.NewMemoryOriginalStore()
	replicas, err := newWorkspaceReplicas(workspaceDir, deliveryOwner(doc.Entry), store, maxTokens)
	if err != nil {
		t.Fatalf("wire the delivery replicas: %v", err)
	}
	out := &fileChannel{
		mockOutbound: &mockOutbound{},
		readBytes:    map[string][]byte{},
		readErrs:     map[string]error{},
	}
	host.sender = &hostSender{
		bot:          out,
		typingActive: &sync.Map{},
		router:       host.router,
		inj:          host.inbox,
		workspaceDir: workspaceDir,
		replicas:     replicas,
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			t.Logf("close the host: %v", err)
		}
		if err := replicas.CleanupStartup(ctx); err != nil {
			t.Logf("reclaim the replicas: %v", err)
		}
	})
	return &fileRig{host: host, model: script, out: out, store: store, replicas: replicas, workspaceDir: workspaceDir}
}

// drive 经宿主投递面驱动一轮会话：入站投递路由到会话，回合答复按宿主投递面送出。
func (r *fileRig) drive(t *testing.T, reply string) event.Address {
	t.Helper()
	r.model.then(finalTurn(reply))
	addr, err := r.host.Drive(context.Background(), inboundMessage{
		ChatID:   "chat-1",
		UserName: "alice",
		Text:     "请把结果发给我",
	})
	if err != nil {
		t.Fatalf("drive one turn: %v", err)
	}
	return addr
}

// receipts 返回该会址收到的回执正文（一并消费）。
func (r *fileRig) receipts(addr event.Address) []string {
	var out []string
	for _, msg := range r.host.inbox.Drain(addr) {
		out = append(out, msg.Content)
	}
	return out
}

// writtenFile 在目录下写一个文件并返回其路径，模式在写入后落地（不受 umask 影响）。
func writtenFile(t *testing.T, dir, name string, body []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", name, err)
	}
	return path
}

// filesUnder 报告目录下全部普通文件（有序），用于一次投递前后的快照差集。
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// newFiles 报告 after 中 before 没有的文件。
func newFiles(before, after []string) []string {
	seen := make(map[string]bool, len(before))
	for _, path := range before {
		seen[path] = true
	}
	var out []string
	for _, path := range after {
		if !seen[path] {
			out = append(out, path)
		}
	}
	return out
}

// carriesAll 报告任一正文是否同时携带全部给定片段。
func carriesAll(bodies []string, wants ...string) bool {
	for _, body := range bodies {
		matched := true
		for _, want := range wants {
			if !strings.Contains(body, want) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// TestFileRoundTrip 钉住 宿主真实文件收发的四条边界。
//
//   - 类型识别与媒体分派: 图片走 image、文档走 file，正文照常送达
//   - 大结果副本: 大结果经 workspace 唯一副本投递，投递期受保护、结束后可回收
//   - 可执行拒绝: 扩展名与权限位命中的可执行文件都不投递，各留一条具名回执
//   - 失败不静默: 通道拒绝一件时该件入具名回执，文本与其余文件照常送达
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
func TestFileRoundTrip(t *testing.T) {
	t.Run("S1 类型识别与媒体分派", func(t *testing.T) {
		ws := t.TempDir()
		rig := newFileRig(t, ws, 0)
		picture := writtenFile(t, ws, "chart.png", []byte("png bytes"), 0o644)
		document := writtenFile(t, ws, "report.txt", []byte("text bytes"), 0o644)

		rig.drive(t, "结果在 "+picture+" 与 "+document)

		if got := rig.out.texts(); len(got) != 1 || !strings.Contains(got[0].Body, "结果在") {
			t.Fatalf("text deliveries = %v, want the reply text delivered once", got)
		}
		if !rig.out.has("image", "chat-1", picture) {
			t.Fatalf("recorded channel events = %v, want the image delivery at its own path", rig.out.recorded())
		}
		if !rig.out.has("file", "chat-1", document) {
			t.Fatalf("recorded channel events = %v, want the document delivery at its own path", rig.out.recorded())
		}
	})

	t.Run("S2 大结果经唯一副本投递并受活跃保护", func(t *testing.T) {
		ws := t.TempDir()
		rig := newFileRig(t, ws, 64)
		body := bytes.Repeat([]byte("大结果字节"), 200)
		source := writtenFile(t, ws, "result.txt", body, 0o644)
		before := filesUnder(t, ws)

		rig.out.duringSend = func() {
			if err := rig.replicas.CleanupStartup(context.Background()); err != nil {
				t.Errorf("reclamation during the send: %v", err)
			}
		}
		rig.drive(t, "完整结果见 "+source)
		rig.out.duringSend = nil

		delivered := rig.out.deliveredFiles()
		if len(delivered) != 1 {
			t.Fatalf("file deliveries = %v, want the one large result", delivered)
		}
		replicaPath := delivered[0]
		replicaArea := filepath.Join(ws, replicaArea, "replica") + string(os.PathSeparator)
		if !strings.HasPrefix(replicaPath, replicaArea) {
			t.Fatalf("large result handed over at %s, want a replica under %s", replicaPath, replicaArea)
		}
		read, readErr := rig.out.readOf(replicaPath)
		if readErr != nil {
			t.Fatalf("the channel could not read the replica at %s: %v", replicaPath, readErr)
		}
		if !bytes.Equal(read, body) {
			t.Fatalf("the channel read %d bytes at %s, want the %d bytes of the result", len(read), replicaPath, len(body))
		}
		ref := workspace.OriginalRefFor(rig.replicas.scope, body)
		recalled := rig.store.Get(context.Background(), ref.Key)
		if recalled.Outcome != ledger.RecallFound || !bytes.Equal(recalled.Body, body) {
			t.Fatalf("the original of the large result reads back as %v, want the committed bytes", recalled.Outcome)
		}
		if err := rig.replicas.CleanupStartup(context.Background()); err != nil {
			t.Fatalf("reclaim the replica once the delivery ended: %v", err)
		}
		if _, err := os.Lstat(replicaPath); !os.IsNotExist(err) {
			t.Fatalf("replica %s still stands (Lstat err=%v), want it reclaimable once the delivery ended", replicaPath, err)
		}

		rig.drive(t, "再发一次 "+source)
		again := rig.out.deliveredFiles()
		if len(again) != 2 {
			t.Fatalf("file deliveries = %v, want the large result delivered again", again)
		}
		if again[1] != replicaPath {
			t.Fatalf("re-materialisation resolved to %s, want the stable replica path %s", again[1], replicaPath)
		}
		read, readErr = rig.out.readOf(replicaPath)
		if readErr != nil {
			t.Fatalf("the channel could not read the re-materialised replica at %s: %v", replicaPath, readErr)
		}
		if !bytes.Equal(read, body) {
			t.Fatalf("the re-materialised replica holds %d bytes, want the %d bytes of the original", len(read), len(body))
		}
		if added := newFiles(before, filesUnder(t, ws)); len(added) != 1 || added[0] != replicaPath {
			t.Fatalf("files added under the workspace = %v, want only the single replica %s", added, replicaPath)
		}
	})

	t.Run("S3 可执行文件硬拒绝并留具名回执", func(t *testing.T) {
		ws := t.TempDir()
		rig := newFileRig(t, ws, 0)
		byExtension := writtenFile(t, ws, "cleanup.sh", []byte("#!/bin/sh\n"), 0o644)
		byPermission := writtenFile(t, ws, "tool.txt", []byte("#!/bin/sh\n"), 0o755)
		notes := writtenFile(t, ws, "notes.txt", []byte("notes"), 0o644)

		addr := rig.drive(t, "脚本 "+byExtension+" 程序 "+byPermission+" 笔记 "+notes)

		for _, refused := range []string{byExtension, byPermission} {
			if rig.out.has("file", "chat-1", refused) || rig.out.has("image", "chat-1", refused) {
				t.Fatalf("executable %s reached the channel: %v", refused, rig.out.recorded())
			}
		}
		if !rig.out.has("file", "chat-1", notes) {
			t.Fatalf("recorded channel events = %v, want the plain file delivered beside the refusals", rig.out.recorded())
		}
		if got := rig.out.texts(); len(got) != 1 {
			t.Fatalf("text deliveries = %v, want the reply text delivered once", got)
		}
		bodies := rig.receipts(addr)
		if !carriesAll(bodies, "file-denied", "可执行文件不投递") {
			t.Fatalf("receipts = %v, want a named refusal of the executable rule", bodies)
		}
		if count := len(bodies); count != 2 {
			t.Fatalf("receipt count = %d (%v), want one receipt per refused file", count, bodies)
		}
	})

	t.Run("S4 单文件失败不静默且不阻断", func(t *testing.T) {
		ws := t.TempDir()
		rig := newFileRig(t, ws, 0)
		broken := writtenFile(t, ws, "broken.txt", []byte("broken"), 0o644)
		healthy := writtenFile(t, ws, "healthy.csv", []byte("a,b"), 0o644)
		rig.out.failPath = broken
		rig.out.failErr = errors.New("wechat: upload rejected")

		addr := rig.drive(t, "两个文件 "+broken+" 与 "+healthy)

		if got := rig.out.texts(); len(got) != 1 {
			t.Fatalf("text deliveries = %v, want the text delivered despite the failed file", got)
		}
		if !rig.out.has("file", "chat-1", healthy) {
			t.Fatalf("recorded channel events = %v, want the healthy file delivered beside the failure", rig.out.recorded())
		}
		if rig.out.has("file", "chat-1", broken) {
			t.Fatalf("a failed send counted as a delivery: %v", rig.out.recorded())
		}
		bodies := rig.receipts(addr)
		if !carriesAll(bodies, "file-failed", "upload rejected") {
			t.Fatalf("receipts = %v, want the named failure of the refused file", bodies)
		}
	})
}

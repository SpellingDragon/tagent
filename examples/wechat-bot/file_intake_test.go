package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/wechat-robot-go/wechat"
)

// ---------------------------------------------------------------------------
// mock MediaDownloader（任务 4.1 / 6.1）
//
// 流式签名：按 item 类型把预置字节写入调用方 Writer，或注入失败/流中超限。
// 全程不触发真实网络 / 微信登录 / CDN / LLM。
// ---------------------------------------------------------------------------

type mockDownloader struct {
	imageData []byte
	voiceData []byte
	fileData  []byte
	videoData []byte
	failAll   bool
	overLimit bool // 模拟流中超限：写入一半后返回 wechat.ErrMaxSizeExceeded
	calls     []string
	lastOpts  wechat.DownloadOptions
}

func (m *mockDownloader) serve(kind string, data []byte, w io.Writer, opts wechat.DownloadOptions) (int64, error) {
	m.calls = append(m.calls, kind)
	m.lastOpts = opts
	if m.failAll {
		return 0, errors.New("injected download failure")
	}
	if m.overLimit {
		n, _ := w.Write(data[:len(data)/2])
		return int64(n), wechat.ErrMaxSizeExceeded
	}
	n, err := w.Write(data)
	return int64(n), err
}

func (m *mockDownloader) DownloadImageFromItemTo(_ context.Context, _ string, _ *wechat.ImageItem, w io.Writer, opts wechat.DownloadOptions) (int64, error) {
	return m.serve("image", m.imageData, w, opts)
}

func (m *mockDownloader) DownloadVoiceTo(_ context.Context, _ *wechat.VoiceItem, _ string, w io.Writer, opts wechat.DownloadOptions) (int64, error) {
	return m.serve("voice", m.voiceData, w, opts)
}

func (m *mockDownloader) DownloadFileFromItemTo(_ context.Context, _ *wechat.FileItem, _ string, w io.Writer, opts wechat.DownloadOptions) (int64, error) {
	return m.serve("file", m.fileData, w, opts)
}

func (m *mockDownloader) DownloadVideoFromItemTo(_ context.Context, _ *wechat.VideoItem, _ string, w io.Writer, opts wechat.DownloadOptions) (int64, error) {
	return m.serve("video", m.videoData, w, opts)
}

var testNow = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

func fileMsg(name, length string) *wechat.Message {
	return &wechat.Message{ItemList: []wechat.MessageItem{
		{Type: wechat.ItemTypeFile, FileItem: &wechat.FileItem{FileName: name, Length: length}},
	}}
}

// ---------------------------------------------------------------------------
// 落盘、命名、多 item、注入文本（任务 4.1）
// ---------------------------------------------------------------------------

func TestIntakeMediaSavesFileWithRelativePath(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{fileData: []byte("hello pdf")}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "user1", fileMsg("报告 v1.pdf", "9"), testNow)

	if len(out.Rejects) != 0 {
		t.Fatalf("unexpected rejects: %v", out.Rejects)
	}
	if len(out.Saved) != 1 {
		t.Fatalf("expected 1 saved file, got %d", len(out.Saved))
	}
	f := out.Saved[0]
	if f.OrigName != "报告 v1.pdf" || f.Size != 9 {
		t.Errorf("unexpected saved meta: %+v", f)
	}
	if !strings.HasPrefix(f.Path, filepath.Join(ws, uploadsSubdir, "user1")+string(filepath.Separator)) {
		t.Errorf("path not under uploads/user1: %s", f.Path)
	}
	if !strings.HasSuffix(f.Path, ".pdf") {
		t.Errorf("extension lost: %s", f.Path)
	}
	data, err := os.ReadFile(f.Path)
	if err != nil || string(data) != "hello pdf" {
		t.Errorf("file content mismatch: %v %q", err, data)
	}
	info, _ := os.Stat(f.Path)
	if info.Mode().Perm()&0111 != 0 {
		t.Errorf("saved file has executable bits: %v", info.Mode())
	}
}

func TestIntakeMediaMultipleItems(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{imageData: []byte("img"), fileData: []byte("doc")}
	msg := &wechat.Message{ItemList: []wechat.MessageItem{
		{Type: wechat.ItemTypeImage, ImageItem: &wechat.ImageItem{MidSize: 3}},
		{Type: wechat.ItemTypeFile, FileItem: &wechat.FileItem{FileName: "a.txt", Length: "3"}},
	}}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", msg, testNow)
	if len(out.Saved) != 2 {
		t.Fatalf("expected 2 saved, got %d (rejects: %v)", len(out.Saved), out.Rejects)
	}
	inject := ComposeMediaInject(out, "请看附件", ws, "u", testNow)
	for _, f := range out.Saved {
		if !strings.Contains(inject, f.Path) {
			t.Errorf("inject text missing path %s:\n%s", f.Path, inject)
		}
	}
	if !strings.Contains(inject, "请看附件") {
		t.Errorf("inject text missing user text:\n%s", inject)
	}
	if !strings.Contains(inject, "[用户发来附件]") {
		t.Errorf("inject text missing header:\n%s", inject)
	}
}

func TestIntakeMediaDownloadFailure(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{failAll: true}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", fileMsg("a.txt", "3"), testNow)
	if len(out.Saved) != 0 || len(out.Rejects) != 1 {
		t.Fatalf("expected 0 saved / 1 reject, got %d/%d", len(out.Saved), len(out.Rejects))
	}
	if inject := ComposeMediaInject(out, "", ws, "u", testNow); inject != "" {
		t.Errorf("expected empty inject when all rejected, got %q", inject)
	}
}

// ---------------------------------------------------------------------------
// 安全用例（任务 4.2）
// ---------------------------------------------------------------------------

func TestIntakeMediaRejectsExecutableWithoutDownload(t *testing.T) {
	ws := t.TempDir()
	for _, name := range []string{"virus.exe", "run.sh", "tool.bin"} {
		dl := &mockDownloader{fileData: []byte("x")}
		out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", fileMsg(name, "1"), testNow)
		if len(out.Saved) != 0 || len(out.Rejects) != 1 {
			t.Errorf("%s: expected reject, got saved=%d rejects=%v", name, len(out.Saved), out.Rejects)
		}
		if len(dl.calls) != 0 {
			t.Errorf("%s: download should not be attempted for executables", name)
		}
	}
}

func TestIntakeMediaRejectsOversizeByMetadataWithoutDownload(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{}
	declared := fmt.Sprintf("%d", maxInboundFileSize+1)
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", fileMsg("big.zip", declared), testNow)
	if len(out.Saved) != 0 || len(out.Rejects) != 1 {
		t.Fatalf("expected metadata-based reject, got saved=%d rejects=%v", len(out.Saved), out.Rejects)
	}
	if len(dl.calls) != 0 {
		t.Error("download should not be attempted when declared size exceeds limit")
	}
}

func TestSaveInboundFileSizeBoundary(t *testing.T) {
	ws := t.TempDir()
	// 上限整 50MB 允许
	if _, err := saveInboundFile(ws, "u", "exact.dat", bytes.Repeat([]byte("a"), maxInboundFileSize), testNow); err != nil {
		t.Errorf("exact-limit file should be accepted: %v", err)
	}
	// 超 1 字节拒绝（元数据谎报小尺寸时的实际字节复核）
	if _, err := saveInboundFile(ws, "u", "over.dat", bytes.Repeat([]byte("a"), maxInboundFileSize+1), testNow); err == nil {
		t.Error("over-limit file should be rejected")
	}
}

func TestBuildSavePathTraversalSafety(t *testing.T) {
	ws := t.TempDir()
	base := filepath.Join(ws, uploadsSubdir)
	for _, name := range []string{"../../etc/passwd", "..\\..\\win.ini", "a/b/c.txt", "....//x.txt", "/abs/path.txt"} {
		path, err := buildSavePath(ws, "u", name, testNow)
		if err != nil {
			continue // 拒绝也算安全
		}
		clean := filepath.Clean(path)
		if !strings.HasPrefix(clean, base+string(filepath.Separator)) {
			t.Errorf("name %q escaped uploads dir: %s", name, path)
		}
	}
	// chatID 同样不可信
	path, err := buildSavePath(ws, "../evil", "a.txt", testNow)
	if err == nil && !strings.HasPrefix(filepath.Clean(path), base+string(filepath.Separator)) {
		t.Errorf("chatID escaped uploads dir: %s", path)
	}
}

func TestSaveInboundFileCollision(t *testing.T) {
	ws := t.TempDir()
	s1, err := saveInboundFile(ws, "u", "a.txt", []byte("one"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := saveInboundFile(ws, "u", "a.txt", []byte("two"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if s1.Path == s2.Path {
		t.Fatalf("same-second same-name collision not avoided: %s", s2.Path)
	}
	if got, _ := os.ReadFile(s1.Path); string(got) != "one" {
		t.Errorf("first file overwritten: %q", got)
	}
	if got, _ := os.ReadFile(s2.Path); string(got) != "two" {
		t.Errorf("second file content wrong: %q", got)
	}
}

// 并发同秒同名落盘：O_EXCL 原子创建保证互不覆盖、各得其所。
func TestSaveInboundFileConcurrentCollision(t *testing.T) {
	ws := t.TempDir()
	const n = 8
	paths := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := saveInboundFile(ws, "u", "a.txt", []byte{byte('0' + i)}, testNow)
			if err != nil {
				t.Errorf("save %d: %v", i, err)
				return
			}
			paths[i] = s.Path
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if seen[p] {
			t.Fatalf("duplicate path from concurrent saves: %s", p)
		}
		seen[p] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d distinct files, got %d", n, len(seen))
	}
}

// 元数据缺失（DeclaredSize=0）的文件/视频正常接收：内存风险已由 SDK 流式
// MaxSize 闸门兜底，不再以"缺少大小信息"拒收。
func TestIntakeMediaAcceptsUnknownSizeFileAndVideo(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{fileData: []byte("x"), videoData: []byte("y")}
	msg := &wechat.Message{ItemList: []wechat.MessageItem{
		{Type: wechat.ItemTypeFile, FileItem: &wechat.FileItem{FileName: "big.zip", Length: ""}},
		{Type: wechat.ItemTypeVideo, VideoItem: &wechat.VideoItem{VideoSize: 0}},
	}}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", msg, testNow)
	if len(out.Rejects) != 0 {
		t.Fatalf("unexpected rejects: %v", out.Rejects)
	}
	if len(out.Saved) != 2 {
		t.Fatalf("expected 2 saved, got %d", len(out.Saved))
	}
	if int64(maxInboundFileSize) != dl.lastOpts.MaxSize {
		t.Errorf("MaxSize not passed to SDK: %+v", dl.lastOpts)
	}
}

// 流中超限（元数据缺失或谎报）：SDK 返回 ErrMaxSizeExceeded 后，
// 残留的不完整文件被删除，用户收到超限文案而非通用失败文案。
func TestIntakeMediaStreamOverLimitCleansResidue(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{fileData: []byte("0123456789"), overLimit: true}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", fileMsg("big.bin.gz", ""), testNow)
	if len(out.Saved) != 0 {
		t.Fatalf("expected 0 saved, got %+v", out.Saved)
	}
	if len(out.Rejects) != 1 || !strings.Contains(out.Rejects[0], "超过大小上限") {
		t.Fatalf("expected over-limit reject message, got %v", out.Rejects)
	}
	assertNoResidue(t, ws)
}

// 下载失败同样不留残留文件。
func TestIntakeMediaDownloadFailureCleansResidue(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{failAll: true}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", fileMsg("a.txt", "3"), testNow)
	if len(out.Saved) != 0 || len(out.Rejects) != 1 {
		t.Fatalf("expected 0 saved / 1 reject, got %d/%d", len(out.Saved), len(out.Rejects))
	}
	if strings.Contains(out.Rejects[0], "超过大小上限") {
		t.Errorf("generic failure should not use over-limit message: %v", out.Rejects)
	}
	assertNoResidue(t, ws)
}

// assertNoResidue 断言 uploads 目录下没有任何残留文件。
func assertNoResidue(t *testing.T, ws string) {
	t.Helper()
	filepath.Walk(filepath.Join(ws, uploadsSubdir), func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			t.Errorf("residue file left behind: %s", path)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// 长文本用例（任务 4.3）
// ---------------------------------------------------------------------------

func TestClassifyInboundLongTextBoundary(t *testing.T) {
	textMsg := func(n int) *wechat.Message {
		return &wechat.Message{ItemList: []wechat.MessageItem{
			{Type: wechat.ItemTypeText, TextItem: &wechat.TextItem{Text: strings.Repeat("中", n)}},
		}}
	}
	cases := []struct {
		runes int
		want  InboundKind
	}{
		{999, InboundShortText},
		{1000, InboundShortText}, // 阈值本身不转换（>1000 才转）
		{1001, InboundLongText},
	}
	for _, c := range cases {
		if got := ClassifyInbound(textMsg(c.runes), true); got != c.want {
			t.Errorf("runes=%d: got %v, want %v", c.runes, got, c.want)
		}
	}
	// workspace 未配置时长文本降级为 ShortText
	if got := ClassifyInbound(textMsg(5000), false); got != InboundShortText {
		t.Errorf("without workspace: got %v, want InboundShortText", got)
	}
	// 空消息
	if got := ClassifyInbound(&wechat.Message{}, true); got != InboundIgnore {
		t.Errorf("empty message: got %v, want InboundIgnore", got)
	}
}

func TestSaveLongTextPreviewAndCount(t *testing.T) {
	ws := t.TempDir()
	text := strings.Repeat("汉", 1500) // 多字节字符，验证 rune 安全截断
	saved, inject, err := SaveLongText(ws, "u", text, "input", testNow)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(saved.Path)
	if err != nil || string(data) != text {
		t.Fatalf("saved content mismatch: %v", err)
	}
	if !strings.Contains(inject, saved.Path) {
		t.Errorf("inject missing path:\n%s", inject)
	}
	if !strings.Contains(inject, "共 1500 字") {
		t.Errorf("inject missing rune count:\n%s", inject)
	}
	if !strings.Contains(inject, strings.Repeat("汉", previewRunes)) {
		t.Errorf("inject missing %d-rune preview", previewRunes)
	}
	if strings.Contains(inject, strings.Repeat("汉", previewRunes+1)) {
		t.Errorf("preview longer than %d runes", previewRunes)
	}
}

// ---------------------------------------------------------------------------
// 语音用例（任务 4.4）
// ---------------------------------------------------------------------------

func voiceMsg(transcript string) *wechat.Message {
	return &wechat.Message{ItemList: []wechat.MessageItem{
		{Type: wechat.ItemTypeVoice, VoiceItem: &wechat.VoiceItem{Text: transcript, FileSize: 4}},
	}}
}

func TestIntakeVoiceWithTranscript(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{voiceData: []byte("silk")}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", voiceMsg("你好世界"), testNow)
	if out.Transcript != "你好世界" {
		t.Errorf("transcript not captured: %q", out.Transcript)
	}
	if len(out.Saved) != 1 {
		t.Fatalf("audio not saved: rejects=%v", out.Rejects)
	}
	inject := ComposeMediaInject(out, "", ws, "u", testNow)
	if !strings.Contains(inject, "语音转写: 你好世界") {
		t.Errorf("inject missing transcript:\n%s", inject)
	}
	if !strings.Contains(inject, out.Saved[0].Path) {
		t.Errorf("inject missing audio path:\n%s", inject)
	}
}

func TestIntakeVoiceLongTranscriptConvertedToFile(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{voiceData: []byte("silk")}
	long := strings.Repeat("讲", 1200)
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", voiceMsg(long), testNow)
	inject := ComposeMediaInject(out, "", ws, "u", testNow)
	if strings.Contains(inject, long) {
		t.Error("long transcript should not be injected in full")
	}
	if !strings.Contains(inject, "voice_transcript") || !strings.Contains(inject, "共 1200 字") {
		t.Errorf("long transcript not converted to file:\n%s", inject)
	}
}

func TestIntakeVoiceWithoutTranscript(t *testing.T) {
	ws := t.TempDir()
	dl := &mockDownloader{voiceData: []byte("silk")}
	out := IntakeMedia(context.Background(), dl, "cdn", ws, "u", voiceMsg(""), testNow)
	inject := ComposeMediaInject(out, "", ws, "u", testNow)
	if strings.Contains(inject, "语音转写") {
		t.Errorf("should not mention transcript when empty:\n%s", inject)
	}
	if len(out.Saved) != 1 || !strings.Contains(inject, out.Saved[0].Path) {
		t.Errorf("audio path missing:\n%s", inject)
	}
}

// ---------------------------------------------------------------------------
// 文件名清洗
// ---------------------------------------------------------------------------

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"报告.pdf":         "_.pdf", // 中文清洗为 _（原名在注入文本中保留）
		"a b.txt":        "a_b.txt",
		"../../etc/pass": "_._etc_pass", // 分隔符与 ".." 均已消除
		"...":            "file",
		"":               "file",
		"normal-1.2.txt": "normal-1.2.txt",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// mock FileSender（任务 5.1）
//
// 记录每个发送方法的调用（方法名、toUserID、path、duration），并支持对指定
// 路径注入错误，用于验证"单文件失败不阻断其余文件"的隔离行为。
// 全程不触发真实网络 / 微信登录 / CDN。
// ---------------------------------------------------------------------------

type fileCall struct {
	method   string
	toUserID string
	path     string
	duration int
}

type mockFileSender struct {
	mu       sync.Mutex
	calls    []fileCall
	failPath string // 若某 Send* 命中此 path，返回错误
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

// ---------------------------------------------------------------------------
// 任务 5.2：ExtractFilePaths 单测
// ---------------------------------------------------------------------------

func TestExtractFilePaths(t *testing.T) {
	ws := t.TempDir()
	absPDF := writeTempFile(t, ws, "report.pdf", 0644)
	_ = writeTempFile(t, ws, "data/out.png", 0644) // 相对 ws 的图片
	relPNG := filepath.Join(ws, "data/out.png")

	execByPerm := writeTempFile(t, ws, "script.txt", 0755) // 权限位可执行
	execByExt := writeTempFile(t, ws, "run.sh", 0644)      // 扩展名在拒绝列表

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

// ---------------------------------------------------------------------------
// 任务 5.3：selectSendFn 单测（按扩展名映射发送接口）
// ---------------------------------------------------------------------------

func TestSelectSendFn(t *testing.T) {
	cases := []struct {
		ext          string
		wantMethod   string
		wantDuration int // 仅 voice 需要校验
	}{
		{".png", "SendImageFromPath", 0},
		{".jpg", "SendImageFromPath", 0},
		{".jpeg", "SendImageFromPath", 0},
		{".gif", "SendImageFromPath", 0},
		{".webp", "SendImageFromPath", 0},
		{".bmp", "SendImageFromPath", 0},
		{".PNG", "SendImageFromPath", 0}, // 大小写不敏感
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

// ---------------------------------------------------------------------------
// 任务 5.4：DeliverFiles 集成式 mock 测试
// ---------------------------------------------------------------------------

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
		// DeliverFiles 不返回错误（单文件失败仅记日志），故此处不应 panic/返回错误
		_ = DeliverFiles(m, context.Background(), "u1", text, "")
		// 两个文件都应被尝试发送
		if got := m.callsOf("SendImageFromPath"); len(got) != 1 {
			t.Fatalf("期望图片仍被尝试 1 次，实际 %d", len(got))
		}
		if got := m.callsOf("SendFileFromPath"); len(got) != 1 {
			t.Fatalf("期望文档仍被发送 1 次，实际 %d", len(got))
		}
	})
}

package prompt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestLoader_LoadFromFile(t *testing.T) {
	dir := t.TempDir()

	promptPath := filepath.Join(dir, "command.md")
	content := "你是一个命令执行助手"
	err := os.WriteFile(promptPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	loader := NewLoader(dir)
	result, err := loader.LoadFromFile("command.md")
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}

	result, err = loader.LoadFromFile(promptPath)
	if err != nil {
		t.Fatalf("LoadFromFile with absolute path failed: %v", err)
	}
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}

	_, err = loader.LoadFromFile("nonexistent.md")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}

	_, err = loader.LoadFromFile("")
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}
}

func TestLoader_LoadFromDir(t *testing.T) {
	dir := t.TempDir()

	promptDir := filepath.Join(dir, "prompts")
	err := os.MkdirAll(promptDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create prompt dir: %v", err)
	}

	files := map[string]string{
		"01_command.md":   "命令执行prompt",
		"02_recall.md":    "回忆prompt",
		"03_knowledge.md": "知识prompt",
		"ignore.txt":      "应该被忽略",
	}

	for name, content := range files {
		path := filepath.Join(promptDir, name)
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	loader := NewLoader(dir)
	result, err := loader.LoadFromDir("prompts")
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	expected := "命令执行prompt\n\n回忆prompt\n\n知识prompt"
	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}

	if result == "应该被忽略" {
		t.Error("TXT file should have been ignored")
	}

	_, err = loader.LoadFromDir("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

func TestLoader_LoadFiles(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "prompt1.md")
	file2 := filepath.Join(dir, "prompt2.md")
	file3 := filepath.Join(dir, "empty.md")

	err := os.WriteFile(file1, []byte("prompt 1"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	err = os.WriteFile(file2, []byte("prompt 2"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	err = os.WriteFile(file3, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create file3: %v", err)
	}

	loader := NewLoader(dir)
	result, err := loader.LoadFiles([]string{"prompt1.md", "prompt2.md"})
	if err != nil {
		t.Fatalf("LoadFiles failed: %v", err)
	}

	expected := "prompt 1\n\nprompt 2"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	result, err = loader.LoadFiles([]string{"prompt1.md", "empty.md", "prompt2.md"})
	if err != nil {
		t.Fatalf("LoadFiles with empty file failed: %v", err)
	}

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	result, err = loader.LoadFiles([]string{"", "prompt1.md", " ", "prompt2.md"})
	if err != nil {
		t.Fatalf("LoadFiles with empty paths failed: %v", err)
	}

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestLoader_LoadFiles_SkipsAbsentOptionalFile 锁定"存在即加载"语义：清单中的文件在磁盘
//
// 契约: docs/wiki/prompt/prompt-architecture.md#load-files
func TestLoader_LoadFiles_SkipsAbsentOptionalFile(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents ctx"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TOOLS.md"), []byte("tools ctx"), 0644); err != nil {
		t.Fatalf("Failed to create TOOLS.md: %v", err)
	}

	loader := NewLoader(dir)
	result, err := loader.LoadFiles([]string{"AGENTS.md", "USER.md", "TOOLS.md"})
	if err != nil {
		t.Fatalf("LoadFiles should skip absent optional file, got error: %v", err)
	}
	expected := "agents ctx\n\ntools ctx"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	result, err = loader.LoadFiles([]string{"NOPE1.md", "NOPE2.md"})
	if err != nil {
		t.Fatalf("LoadFiles with all-absent files should not error: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty result for all-absent files, got %q", result)
	}
}

func TestLoader_LoadComposite(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "inline_file.md")
	err := os.WriteFile(file1, []byte("inline file content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	promptDir := filepath.Join(dir, "prompts")
	err = os.MkdirAll(promptDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(promptDir, "01_first.md"), []byte("first"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	err = os.WriteFile(filepath.Join(promptDir, "02_second.md"), []byte("second"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	loader := NewLoader(dir)
	result, err := loader.LoadComposite(
		"inline prompt",
		[]string{"inline_file.md"},
		"prompts",
	)
	if err != nil {
		t.Fatalf("LoadComposite failed: %v", err)
	}

	expected := "inline prompt\n\ninline file content\n\nfirst\n\nsecond"
	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}

	result, err = loader.LoadComposite("", []string{"inline_file.md"}, "")
	if err != nil {
		t.Fatalf("LoadComposite with empty inline failed: %v", err)
	}

	expected = "inline file content"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single value",
			input:    "file1.md",
			expected: []string{"file1.md"},
		},
		{
			name:     "multiple values",
			input:    "file1.md,file2.md,file3.md",
			expected: []string{"file1.md", "file2.md", "file3.md"},
		},
		{
			name:     "with whitespace",
			input:    " file1.md , file2.md , file3.md ",
			expected: []string{"file1.md", "file2.md", "file3.md"},
		},
		{
			name:     "with empty elements",
			input:    "file1.md,,file2.md,",
			expected: []string{"file1.md", "file2.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SplitCSV(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d elements, got %d", len(tt.expected), len(result))
				return
			}
			for i, v := range tt.expected {
				if result[i] != v {
					t.Errorf("Element %d: expected %q, got %q", i, v, result[i])
				}
			}
		})
	}
}

func TestLoader_LoadFromDir_SubdirsIgnored(t *testing.T) {
	dir := t.TempDir()
	promptDir := filepath.Join(dir, "prompts")

	subdir := filepath.Join(promptDir, "subdir")
	err := os.MkdirAll(subdir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	err = os.WriteFile(filepath.Join(subdir, "ignored.md"), []byte("ignored"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file in subdir: %v", err)
	}

	err = os.WriteFile(filepath.Join(promptDir, "valid.md"), []byte("valid"), 0644)
	if err != nil {
		t.Fatalf("Failed to create valid file: %v", err)
	}

	loader := NewLoader(dir)
	result, err := loader.LoadFromDir("prompts")
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	if result != "valid" {
		t.Errorf("Expected 'valid', got %q", result)
	}
}

func TestLoader_LoadFromDir_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	loader := NewLoader(dir)
	_, err := loader.LoadFromDir(".")
	if err == nil {
		t.Error("Expected error for empty directory, got nil")
	}
}

func TestLoader_LoadBootstrap(t *testing.T) {
	dir := t.TempDir()
	bootstrapDir := filepath.Join(dir, "bootstrap")

	err := os.MkdirAll(bootstrapDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create bootstrap dir: %v", err)
	}

	bootstrapFiles := map[string]string{
		"AGENTS.md":    "Agent instructions",
		"SOUL.md":      "Agent soul/personality",
		"USER.md":      "User information",
		"TOOLS.md":     "Available tools",
		"HEARTBEAT.md": "Heartbeat config",
		"MEMORY.md":    "Memory settings",
	}

	for name, content := range bootstrapFiles {
		path := filepath.Join(bootstrapDir, name)
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create %s: %v", name, err)
		}
	}

	loader := NewLoader(dir)
	result, err := loader.LoadBootstrap("bootstrap")
	if err != nil {
		t.Fatalf("LoadBootstrap failed: %v", err)
	}

	expected := "Agent instructions\n\nAgent soul/personality\n\nUser information\n\nAvailable tools\n\nHeartbeat config\n\nMemory settings"
	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}

	err = os.Remove(filepath.Join(bootstrapDir, "SOUL.md"))
	if err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}

	result, err = loader.LoadBootstrap("bootstrap")
	if err != nil {
		t.Fatalf("LoadBootstrap with missing file failed: %v", err)
	}

	if result == "Agent soul/personality" {
		t.Error("Missing file should be skipped")
	}

	_, err = loader.LoadBootstrap("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

// TestLoader_LoadBootstrap_OrderFileMissingSkipsOtherFailureAborts 钉住顺序表条目读失败时的两种去向：不存在才跳过并继续装配其余条目，其他读失败必须整体中止。
//
// - SOUL.md 缺失：结果不含该篇内容，其余五篇逐篇仍在
// - SOUL.md 是目录：读失败但不属于 os.ErrNotExist，LoadBootstrap 返回错误而不是被静默跳过
func TestLoader_LoadBootstrap_OrderFileMissingSkipsOtherFailureAborts(t *testing.T) {
	dir := t.TempDir()
	bootstrapDir := filepath.Join(dir, "bootstrap")
	if err := os.MkdirAll(bootstrapDir, 0o755); err != nil {
		t.Fatalf("MkdirAll bootstrap dir: %v", err)
	}
	for _, name := range BootstrapLoadOrder {
		if name == "SOUL.md" {
			continue
		}
		if err := os.WriteFile(filepath.Join(bootstrapDir, name), []byte("body of "+name), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	loader := NewLoader(dir)

	result, err := loader.LoadBootstrap("bootstrap")
	if err != nil {
		t.Fatalf("a missing order file must be skipped, got error: %v", err)
	}
	expected := "body of AGENTS.md\n\nbody of USER.md\n\nbody of TOOLS.md\n\nbody of HEARTBEAT.md\n\nbody of MEMORY.md"
	if result != expected {
		t.Errorf("assembled result mismatch\n want: %q\n  got: %q", expected, result)
	}

	if err := os.Mkdir(filepath.Join(bootstrapDir, "SOUL.md"), 0o755); err != nil {
		t.Fatalf("Mkdir SOUL.md: %v", err)
	}
	if _, err := loader.LoadBootstrap("bootstrap"); err == nil {
		t.Error("a read failure that is not os.ErrNotExist must abort assembly, got nil error")
	}
}

func TestLoader_LoadBootstrap_WithExtraFiles(t *testing.T) {
	dir := t.TempDir()
	bootstrapDir := filepath.Join(dir, "bootstrap")

	err := os.MkdirAll(bootstrapDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create bootstrap dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(bootstrapDir, "AGENTS.md"), []byte("agents"), 0644)
	if err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	err = os.WriteFile(filepath.Join(bootstrapDir, "custom.md"), []byte("custom"), 0644)
	if err != nil {
		t.Fatalf("Failed to create custom.md: %v", err)
	}

	err = os.WriteFile(filepath.Join(bootstrapDir, "extra.md"), []byte("extra"), 0644)
	if err != nil {
		t.Fatalf("Failed to create extra.md: %v", err)
	}

	loader := NewLoader(dir)
	result, err := loader.LoadBootstrap("bootstrap")
	if err != nil {
		t.Fatalf("LoadBootstrap failed: %v", err)
	}

	if result != "agents\n\ncustom\n\nextra" {
		t.Errorf("Expected 'agents\\n\\ncustom\\n\\nextra', got %q", result)
	}
}

func embeddedFallback() fstest.MapFS {
	return fstest.MapFS{
		"resources/prompts/shared.md":   {Data: []byte("EMBEDDED SHARED")},
		"resources/prompts/bundle/a.md": {Data: []byte("A")},
		"resources/prompts/bundle/b.md": {Data: []byte("B")},
	}
}

// TestLoader_FallbackFile_DiskMissing disk missing → resolves from embedded fallback.
func TestLoader_FallbackFile_DiskMissing(t *testing.T) {
	l := NewLoader(t.TempDir(), WithFallback(embeddedFallback(), "resources/prompts"))
	got, err := l.LoadFromFile("shared.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "EMBEDDED SHARED" {
		t.Errorf("got %q, want embedded default", got)
	}
}

// TestLoader_FallbackFile_DiskOverrides disk present → overrides embedded (disk wins).
func TestLoader_FallbackFile_DiskOverrides(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shared.md"), []byte("DISK SHARED"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(dir, WithFallback(embeddedFallback(), "resources/prompts"))
	got, err := l.LoadFromFile("shared.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "DISK SHARED" {
		t.Errorf("disk should override embedded, got %q", got)
	}
}

// TestLoader_FallbackFile_AbsoluteNoFallback absolute path → never falls back.
func TestLoader_FallbackFile_AbsoluteNoFallback(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, WithFallback(embeddedFallback(), "resources/prompts"))
	if _, err := l.LoadFromFile(filepath.Join(dir, "shared.md")); err == nil {
		t.Error("absolute missing path must error, not fall back to embedded")
	}
}

// TestLoader_NoFallback_MissingErrors no fallback configured → missing file errors as before (NotExist).
func TestLoader_NoFallback_MissingErrors(t *testing.T) {
	l := NewLoader(t.TempDir())
	_, err := l.LoadFromFile("shared.md")
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("without fallback, missing file should be NotExist error, got %v", err)
	}
}

// TestLoader_FallbackDir_DiskMissing dir missing on disk → scans embedded dir of same base name.
func TestLoader_FallbackDir_DiskMissing(t *testing.T) {
	l := NewLoader(t.TempDir(), WithFallback(embeddedFallback(), "resources/prompts"))
	got, err := l.LoadFromDir("bundle")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "A\n\nB" {
		t.Errorf("got %q, want %q from embedded dir", got, "A\n\nB")
	}
}

// TestLoader_FallbackDir_DiskWinsNoMerge dir present on disk → disk wins, no per-file merge with embedded.
func TestLoader_FallbackDir_DiskWinsNoMerge(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "bundle")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "only.md"), []byte("DISK ONLY"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(dir, WithFallback(embeddedFallback(), "resources/prompts"))
	got, err := l.LoadFromDir("bundle")
	if err != nil {
		t.Fatal(err)
	}
	if got != "DISK ONLY" {
		t.Errorf("disk dir should win with no merge, got %q", got)
	}
}

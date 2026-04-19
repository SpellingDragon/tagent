package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadFromFile(t *testing.T) {
	dir := t.TempDir()

	// Create a test prompt file
	promptPath := filepath.Join(dir, "command.md")
	content := "你是一个命令执行助手"
	err := os.WriteFile(promptPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test loading file
	loader := NewLoader(dir)
	result, err := loader.LoadFromFile("command.md")
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}

	// Test loading with absolute path
	result, err = loader.LoadFromFile(promptPath)
	if err != nil {
		t.Fatalf("LoadFromFile with absolute path failed: %v", err)
	}
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}

	// Test loading non-existent file
	_, err = loader.LoadFromFile("nonexistent.md")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}

	// Test loading empty path
	_, err = loader.LoadFromFile("")
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}
}

func TestLoader_LoadFromDir(t *testing.T) {
	dir := t.TempDir()

	// Create prompt directory
	promptDir := filepath.Join(dir, "prompts")
	err := os.MkdirAll(promptDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create prompt dir: %v", err)
	}

	// Create multiple .md files
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

	// Load from directory
	loader := NewLoader(dir)
	result, err := loader.LoadFromDir("prompts")
	if err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	// Verify order (alphabetical) and content
	expected := "命令执行prompt\n\n回忆prompt\n\n知识prompt"
	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}

	// Verify .txt file was ignored
	if result == "应该被忽略" {
		t.Error("TXT file should have been ignored")
	}

	// Test non-existent directory
	_, err = loader.LoadFromDir("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

func TestLoader_LoadFiles(t *testing.T) {
	dir := t.TempDir()

	// Create multiple files
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

	// Load multiple files
	loader := NewLoader(dir)
	result, err := loader.LoadFiles([]string{"prompt1.md", "prompt2.md"})
	if err != nil {
		t.Fatalf("LoadFiles failed: %v", err)
	}

	expected := "prompt 1\n\nprompt 2"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Test with empty file (should skip)
	result, err = loader.LoadFiles([]string{"prompt1.md", "empty.md", "prompt2.md"})
	if err != nil {
		t.Fatalf("LoadFiles with empty file failed: %v", err)
	}

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// Test with empty paths (should skip)
	result, err = loader.LoadFiles([]string{"", "prompt1.md", " ", "prompt2.md"})
	if err != nil {
		t.Fatalf("LoadFiles with empty paths failed: %v", err)
	}

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestLoader_LoadComposite(t *testing.T) {
	dir := t.TempDir()

	// Create files
	file1 := filepath.Join(dir, "inline_file.md")
	err := os.WriteFile(file1, []byte("inline file content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Create directory
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

	// Test composite loading
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

	// Test with empty inline
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

	// Create subdirectory (should be ignored)
	subdir := filepath.Join(promptDir, "subdir")
	err := os.MkdirAll(subdir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	err = os.WriteFile(filepath.Join(subdir, "ignored.md"), []byte("ignored"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file in subdir: %v", err)
	}

	// Create valid file
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

	// Create bootstrap files in load order
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

	// Load bootstrap
	loader := NewLoader(dir)
	result, err := loader.LoadBootstrap("bootstrap")
	if err != nil {
		t.Fatalf("LoadBootstrap failed: %v", err)
	}

	// Verify order (should follow BootstrapLoadOrder)
	expected := "Agent instructions\n\nAgent soul/personality\n\nUser information\n\nAvailable tools\n\nHeartbeat config\n\nMemory settings"
	if result != expected {
		t.Errorf("Expected:\n%s\n\nGot:\n%s", expected, result)
	}

	// Test with missing files (should skip)
	err = os.Remove(filepath.Join(bootstrapDir, "SOUL.md"))
	if err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}

	result, err = loader.LoadBootstrap("bootstrap")
	if err != nil {
		t.Fatalf("LoadBootstrap with missing file failed: %v", err)
	}

	// SOUL.md should be skipped
	if result == "Agent soul/personality" {
		t.Error("Missing file should be skipped")
	}

	// Test with non-existent directory
	_, err = loader.LoadBootstrap("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent directory, got nil")
	}
}

func TestLoader_LoadBootstrap_WithExtraFiles(t *testing.T) {
	dir := t.TempDir()
	bootstrapDir := filepath.Join(dir, "bootstrap")

	err := os.MkdirAll(bootstrapDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create bootstrap dir: %v", err)
	}

	// Create standard bootstrap files
	err = os.WriteFile(filepath.Join(bootstrapDir, "AGENTS.md"), []byte("agents"), 0644)
	if err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	// Create extra .md files (should be loaded after standard files)
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

	// Should contain both standard and extra files
	if result != "agents\n\ncustom\n\nextra" {
		t.Errorf("Expected 'agents\\n\\ncustom\\n\\nextra', got %q", result)
	}
}

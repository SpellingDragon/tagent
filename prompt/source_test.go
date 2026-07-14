package prompt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSource_StaticSource(t *testing.T) {
	src := NewStaticSource("hello world")
	got, err := src.Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}
}

func TestSource_StaticSourceEmpty(t *testing.T) {
	src := NewStaticSource("")
	if !src.IsEmpty() {
		t.Fatal("expected empty source")
	}
	got, err := src.Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSource_NilSource(t *testing.T) {
	var src *Source
	if !src.IsEmpty() {
		t.Fatal("expected nil source to be empty")
	}
}

func TestSource_HotReload(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Write initial file
	filePath := filepath.Join(dir, "test.md")
	if err := os.WriteFile(filePath, []byte("initial content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := NewSource(loader, CompositeConfig{Files: []string{"test.md"}})
	if src.IsEmpty() {
		t.Fatal("expected non-empty source")
	}

	// First read
	got, err := src.Get()
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if got != "initial content" {
		t.Fatalf("expected 'initial content', got %q", got)
	}

	// Second read — should use cache (no file change)
	got2, err := src.Get()
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if got2 != "initial content" {
		t.Fatalf("expected cached 'initial content', got %q", got2)
	}

	// Modify file (must wait for mtime granularity — typically 1s on most filesystems)
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("updated content"), 0644); err != nil {
		t.Fatalf("write updated file: %v", err)
	}

	// Third read — should detect change and re-read
	got3, err := src.Get()
	if err != nil {
		t.Fatalf("third Get: %v", err)
	}
	if got3 != "updated content" {
		t.Fatalf("expected 'updated content', got %q", got3)
	}
}

func TestSource_GracefulDegradationOnReadError(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Write initial file
	filePath := filepath.Join(dir, "test.md")
	if err := os.WriteFile(filePath, []byte("initial"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := NewSource(loader, CompositeConfig{Files: []string{"test.md"}})

	// First read succeeds
	got, err := src.Get()
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if got != "initial" {
		t.Fatalf("expected 'initial', got %q", got)
	}

	// Delete the file
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	// Second read — should return cached content (graceful degradation)
	got2, err := src.Get()
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	if got2 != "initial" {
		t.Fatalf("expected cached 'initial', got %q", got2)
	}
}

func TestSource_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Write two files
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("file A"), 0644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("file B"), 0644); err != nil {
		t.Fatalf("write b.md: %v", err)
	}

	src := NewSource(loader, CompositeConfig{Files: []string{"a.md", "b.md"}})

	got, err := src.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// LoadComposite joins files with double newlines
	expected := "file A\n\nfile B"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestSource_DirScan(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Write files
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("alpha"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.md"), []byte("beta"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Non-md file should be ignored
	if err := os.WriteFile(filepath.Join(dir, "gamma.txt"), []byte("gamma"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	src := NewSource(loader, CompositeConfig{Dir: "."})

	got, err := src.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Files sorted alphabetically: alpha.md, beta.md
	expected := "alpha\n\nbeta"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

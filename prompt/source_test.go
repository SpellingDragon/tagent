package prompt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSource_StaticSource 覆盖静态源契约：内容固定、不监听文件，重复读取返回同一内容。
//
// 契约: docs/wiki/prompt/prompt-architecture.md#source-hotreload
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

	filePath := filepath.Join(dir, "test.md")
	if err := os.WriteFile(filePath, []byte("initial content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := NewSource(loader, CompositeConfig{Files: []string{"test.md"}})
	if src.IsEmpty() {
		t.Fatal("expected non-empty source")
	}

	got, err := src.Get()
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if got != "initial content" {
		t.Fatalf("expected 'initial content', got %q", got)
	}

	got2, err := src.Get()
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if got2 != "initial content" {
		t.Fatalf("expected cached 'initial content', got %q", got2)
	}

	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("updated content"), 0644); err != nil {
		t.Fatalf("write updated file: %v", err)
	}

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

	filePath := filepath.Join(dir, "test.md")
	if err := os.WriteFile(filePath, []byte("initial"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	src := NewSource(loader, CompositeConfig{Files: []string{"test.md"}})

	got, err := src.Get()
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if got != "initial" {
		t.Fatalf("expected 'initial', got %q", got)
	}

	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove file: %v", err)
	}

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
	expected := "file A\n\nfile B"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestSource_DirScan(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("alpha"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.md"), []byte("beta"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gamma.txt"), []byte("gamma"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	src := NewSource(loader, CompositeConfig{Dir: "."})

	got, err := src.Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	expected := "alpha\n\nbeta"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

// TestCheckModTimesIgnoresFilesOlderThanLastLoad 钉住 缓存条件：修改时间早于上次记录加载时间的文件不得算作已变更。
func TestCheckModTimesIgnoresFilesOlderThanLastLoad(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("hello"), 0o644))
	st, err := os.Stat(filepath.Join(dir, "a.md"))
	require.NoError(t, err)
	lastLoad := st.ModTime().Add(2 * time.Hour)

	s := NewSource(NewLoader(dir), CompositeConfig{Files: []string{"a.md"}})
	s.modTime = lastLoad
	s.cached = "cached-content"

	_, changed, err := s.checkModTimes()
	require.NoError(t, err)
	require.False(t, changed, "a file older than the last recorded load must not count as changed")

	got, err := s.Get()
	require.NoError(t, err)
	require.Equal(t, "cached-content", got, "no change must be served from cache, not from disk")
}

// TestCheckModTimesDetectsFileNewerThanLastLoad is the other side of the same boundary.
func TestCheckModTimesDetectsFileNewerThanLastLoad(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("hello"), 0o644))
	st, err := os.Stat(filepath.Join(dir, "a.md"))
	require.NoError(t, err)

	s := NewSource(NewLoader(dir), CompositeConfig{Files: []string{"a.md"}})
	s.modTime = st.ModTime().Add(-2 * time.Hour)
	s.cached = "cached-content"

	_, changed, err := s.checkModTimes()
	require.NoError(t, err)
	require.True(t, changed, "a file newer than the last recorded load must trigger a re-read")
}

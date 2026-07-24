package prompt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func embeddedFallback() fstest.MapFS {
	return fstest.MapFS{
		"resources/prompts/shared.md":   {Data: []byte("EMBEDDED SHARED")},
		"resources/prompts/bundle/a.md": {Data: []byte("A")},
		"resources/prompts/bundle/b.md": {Data: []byte("B")},
	}
}

// disk missing → resolves from embedded fallback.
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

// disk present → overrides embedded (disk wins).
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

// absolute path → never falls back.
func TestLoader_FallbackFile_AbsoluteNoFallback(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, WithFallback(embeddedFallback(), "resources/prompts"))
	if _, err := l.LoadFromFile(filepath.Join(dir, "shared.md")); err == nil {
		t.Error("absolute missing path must error, not fall back to embedded")
	}
}

// no fallback configured → missing file errors as before (NotExist).
func TestLoader_NoFallback_MissingErrors(t *testing.T) {
	l := NewLoader(t.TempDir())
	_, err := l.LoadFromFile("shared.md")
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("without fallback, missing file should be NotExist error, got %v", err)
	}
}

// dir missing on disk → scans embedded dir of same base name.
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

// dir present on disk → disk wins, no per-file merge with embedded.
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

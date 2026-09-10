package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// D3: rotatePipeFile copytruncates an overgrown pipe log — content moves to
// pf+".1", live file becomes empty (pipe-pane's O_APPEND fd keeps writing
// from offset 0). Under-threshold and missing files are no-ops.
func TestRotatePipeFile_CopyTruncate(t *testing.T) {
	old := pipeRotateBytes
	pipeRotateBytes = 100
	defer func() { pipeRotateBytes = old }()

	dir := t.TempDir()
	pf := filepath.Join(dir, "pipe.log")
	big := strings.Repeat("x", 250)
	if err := os.WriteFile(pf, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}

	rotatePipeFile(pf)

	rot, err := os.ReadFile(pf + ".1")
	if err != nil {
		t.Fatalf(".1 missing: %v", err)
	}
	if string(rot) != big {
		t.Fatalf(".1 content mismatch: %d bytes", len(rot))
	}
	live, err := os.Stat(pf)
	if err != nil {
		t.Fatal(err)
	}
	if live.Size() != 0 {
		t.Fatalf("live file not truncated: %d bytes", live.Size())
	}

	// Under threshold: no-op.
	os.WriteFile(pf, []byte("small"), 0o600)
	os.Remove(pf + ".1")
	rotatePipeFile(pf)
	if _, err := os.Stat(pf + ".1"); !os.IsNotExist(err) {
		t.Fatal("under-threshold file must not rotate")
	}

	// Missing file: no-op, no panic.
	rotatePipeFile(filepath.Join(dir, "absent.log"))
}

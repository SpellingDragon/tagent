package action

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsTmuxAvailable_RealPathProbe (implementation-hardening 4.3): the probe
// must reflect the system truth — with a PATH that has no tmux binary it must
// return false (the old implementation was always true: it checked a
// constructor that never returns nil).
func TestIsTmuxAvailable_RealPathProbe(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)
	if IsTmuxAvailable() {
		t.Fatal("IsTmuxAvailable must be false when PATH has no tmux binary")
	}
}

// TestIsTmuxAvailable_TrueWhenOnPath: with a dir containing an executable
// named tmux, the probe returns true (LookPath requires the exec bit).
func TestIsTmuxAvailable_TrueWhenOnPath(t *testing.T) {
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "tmux"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)
	if !IsTmuxAvailable() {
		t.Fatal("IsTmuxAvailable must be true when a tmux executable is on PATH")
	}
}

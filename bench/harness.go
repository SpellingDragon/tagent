// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RunFileName is the run record file name.
const RunFileName = "run.json"

// MetricsFileName is the five-metric document file name.
const MetricsFileName = "metrics.json"

// ReportFileName is the rendered comparison report file name.
const ReportFileName = "report.md"

// TaskDirName is the per-attempt detail directory.
const TaskDirName = "tasks"

// CapabilitiesFileName is the tool-face capability map file name.
const CapabilitiesFileName = "capabilities.json"

// WriteRun writes one run directory under root and returns its absolute path.
// Every key of files is a workspace-relative path; a key that escapes the run
// directory is rejected before anything is written.
func WriteRun(root, runID string, files map[string][]byte) (string, error) {
	if strings.TrimSpace(runID) == "" || strings.ContainsAny(runID, "/\\") || runID == "." || runID == ".." {
		return "", fmt.Errorf("bench run: run id %q is not a directory name", runID)
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("bench run: run root is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("bench run: run root %q: %w", root, err)
	}
	dir := filepath.Join(absRoot, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("bench run: create %s: %w", dir, err)
	}
	for name, content := range files {
		clean, err := safeRelativePath(name)
		if err != nil {
			return "", fmt.Errorf("bench run: file %q: %w", name, err)
		}
		target := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("bench run: create %s: %w", filepath.Dir(target), err)
		}
		temp, err := os.CreateTemp(filepath.Dir(target), ".bench-*")
		if err != nil {
			return "", fmt.Errorf("bench run: write %s: %w", clean, err)
		}
		if _, err := temp.Write(content); err != nil {
			temp.Close()
			os.Remove(temp.Name())
			return "", fmt.Errorf("bench run: write %s: %w", clean, err)
		}
		if err := temp.Close(); err != nil {
			os.Remove(temp.Name())
			return "", fmt.Errorf("bench run: write %s: %w", clean, err)
		}
		if err := os.Rename(temp.Name(), target); err != nil {
			os.Remove(temp.Name())
			return "", fmt.Errorf("bench run: write %s: %w", clean, err)
		}
	}
	return dir, nil
}

// WorkspaceReadPath resolves an existing workspace file and proves its real
// path stays inside the workspace.
func WorkspaceReadPath(workspace, rel string) (string, error) {
	return workspaceReadPath(workspace, rel)
}

// WorkspaceWritePath resolves a workspace file that may not exist yet, creating
// its parent inside the workspace.
func WorkspaceWritePath(workspace, rel string) (string, error) {
	return workspaceWritePath(workspace, rel)
}

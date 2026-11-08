// 契约: docs/wiki/rl/rl-architecture.md#live-benchmark
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// safeRelativePath rejects an empty, absolute or parent-escaping task path.
// The workspace is the only directory a verifier or a task setup may touch.
func safeRelativePath(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("empty workspace path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("workspace path %q is absolute", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace path %q escapes the workspace", rel)
	}
	return clean, nil
}

// workspaceReadPath resolves an existing file and proves its real path stays
// inside the workspace, so a symlink cannot turn a read into host-file access.
func workspaceReadPath(workspace, rel string) (string, error) {
	clean, err := safeRelativePath(rel)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("workspace is empty")
	}
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", workspace, err)
	}
	full := filepath.Join(realWorkspace, clean)
	realFile, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(realWorkspace, realFile)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace path %q escapes the workspace", rel)
	}
	return realFile, nil
}

// workspaceWritePath resolves a file that may not exist yet, creating its
// parent inside the workspace and proving the parent stays inside it.
func workspaceWritePath(workspace, rel string) (string, error) {
	clean, err := safeRelativePath(rel)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(workspace) == "" {
		return "", fmt.Errorf("workspace is empty")
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", workspace, err)
	}
	realWorkspace, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", workspace, err)
	}
	parent := filepath.Dir(filepath.Join(realWorkspace, clean))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create %q: %w", parent, err)
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("parent of %q: %w", rel, err)
	}
	within, err := filepath.Rel(realWorkspace, realParent)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace path %q escapes the workspace", rel)
	}
	return filepath.Join(realParent, filepath.Base(clean)), nil
}

// sortedKeys makes task setup deterministic: a file that is a prefix of another
// path is written before the nested one, independent of map iteration order.
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

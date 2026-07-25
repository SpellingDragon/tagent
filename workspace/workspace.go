// Package workspace centralizes tagent's on-disk scratch space so that tool
// outputs and tmux command working directories live under one root, and provides
// a periodic cleaner that bounds the accumulated files (by age and count).
//
// Layout (under Root):
//
//	<root>/tool-output/   oversized tool outputs saved by OutputLimitTool
//	<root>/exec/          tmux command working directory (ActionTool)
//
// The Cleaner MUST only ever target the tool-output directory. exec/ is a
// task working directory whose lifecycle belongs to the task layer; cleaning
// it by file age/count would delete live-task artifacts.
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// DefaultRoot is the default unified workspace root (relative to the process
// working directory).
const DefaultRoot = ".tagent-workspace"

// Subdirectories under the root.
const (
	ToolOutputDir = "tool-output" // oversized tool outputs
	ExecDir       = "exec"        // tmux command working directory
)

// Root normalizes a workspace root, falling back to DefaultRoot when empty.
func Root(root string) string {
	if root == "" {
		return DefaultRoot
	}
	return root
}

// ToolOutputPath returns the directory for oversized tool outputs.
func ToolOutputPath(root string) string { return filepath.Join(Root(root), ToolOutputDir) }

// ExecPath returns the tmux command working directory.
func ExecPath(root string) string { return filepath.Join(Root(root), ExecDir) }

// Cleaner periodically bounds the files under a workspace root by age and count.
type Cleaner struct {
	root     string
	interval time.Duration
	maxAge   time.Duration
	maxFiles int
	now      func() time.Time // injectable clock (tests); defaults to time.Now
}

// NewCleaner creates a Cleaner. Non-positive interval/maxAge disable the
// corresponding dimension; maxFiles<=0 disables the count cap.
func NewCleaner(root string, interval, maxAge time.Duration, maxFiles int) *Cleaner {
	return &Cleaner{root: Root(root), interval: interval, maxAge: maxAge, maxFiles: maxFiles, now: time.Now}
}

// Start runs the cleaner on a ticker until ctx is cancelled. It performs an
// immediate pass, then one per interval.
func (c *Cleaner) Start(ctx context.Context) {
	if c.interval <= 0 {
		return
	}
	go func() {
		c.RunOnce()
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.RunOnce()
			}
		}
	}()
}

// RunOnce performs a single cleanup pass (exported for tests / manual trigger).
func (c *Cleaner) RunOnce() {
	now := c.now
	if now == nil {
		now = time.Now
	}
	type fileEntry struct {
		path    string
		modTime time.Time
	}
	var files []fileEntry
	_ = filepath.Walk(c.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		files = append(files, fileEntry{path: path, modTime: info.ModTime()})
		return nil
	})

	removed := 0
	absRoot, _ := filepath.Abs(c.root)
	withinRoot := func(path string) bool {
		if absRoot == "" {
			return true
		}
		abs, err := filepath.Abs(path)
		return err == nil && strings.HasPrefix(abs, absRoot+string(os.PathSeparator))
	}
	// 1. Age-based removal.
	if c.maxAge > 0 {
		cutoff := now().Add(-c.maxAge)
		kept := files[:0]
		for _, f := range files {
			if f.modTime.Before(cutoff) && withinRoot(f.path) {
				if rmErr := os.Remove(f.path); rmErr == nil {
					removed++
					continue
				}
			}
			kept = append(kept, f)
		}
		files = kept
	}
	// 2. Count-based removal (newest first; drop the oldest beyond the cap).
	if c.maxFiles > 0 && len(files) > c.maxFiles {
		sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
		for _, f := range files[c.maxFiles:] {
			if withinRoot(f.path) {
				if rmErr := os.Remove(f.path); rmErr == nil {
					removed++
				}
			}
		}
	}
	if removed > 0 {
		log.Infof("[workspace] cleaned %d file(s) under %s", removed, c.root)
	}
}

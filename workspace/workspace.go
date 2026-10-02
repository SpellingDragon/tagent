// Package workspace centralizes tagent on-disk scratch space (oversized tool outputs)
// under one root and provides a periodic cleaner bounding accumulation by age and count.
//
// - Layout under Root: tool-output/ holds oversized outputs of OutputLimitTool and ActionTool.
// - Command working directories are NOT scratch space: exec inherits the process working directory so its relative paths stay consistent with the file tools base directory.
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
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

// DefaultRoot 是默认的工作区根目录（相对进程工作目录）。
const DefaultRoot = ".tagent-workspace"

// ToolOutputDir 是根目录下存放超大工具输出的子目录名。
const (
	ToolOutputDir = "tool-output"
)

// Root 归一化工作区根目录：为空时回落到 DefaultRoot。
func Root(root string) string {
	if root == "" {
		return DefaultRoot
	}
	return root
}

// ToolOutputPath 返回超大工具输出的目录。
func ToolOutputPath(root string) string { return filepath.Join(Root(root), ToolOutputDir) }

// Cleaner 周期性把工作区根目录下的文件按「年龄 ＋ 数量」两个维度限幅。
type Cleaner struct {
	root     string
	interval time.Duration
	maxAge   time.Duration
	maxFiles int
	// now 是可注入时钟（测试用），为零时取 time.Now。
	now func() time.Time
}

// NewCleaner 构造 Cleaner。interval/maxAge 非正表示关掉对应维度，maxFiles<=0 关掉数量上限。
func NewCleaner(root string, interval, maxAge time.Duration, maxFiles int) *Cleaner {
	return &Cleaner{root: Root(root), interval: interval, maxAge: maxAge, maxFiles: maxFiles, now: time.Now}
}

// Start 在自己的 goroutine 里按 ticker 清理，直到 ctx 取消；需要观察协程退出的调用方用 Run。
func (c *Cleaner) Start(ctx context.Context) { go c.Run(ctx) }

// Run is Start's blocking form: it returns once ctx is cancelled and the ticker
// is released. The agent's close sequence waits on it, so "the maintenance
// producer stopped" is a fact the host can confirm rather than an assumption
// about a goroutine it cannot see.
func (c *Cleaner) Run(ctx context.Context) {
	if c.interval <= 0 {
		return
	}
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

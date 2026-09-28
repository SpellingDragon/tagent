package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Source 是一组提示词文件的运行期视图：按配置读取并拼接文件，文件更新后自动重读。
// 可并发使用；未配置文件时内容固定不变。
//
// 契约: docs/wiki/prompt/prompt-architecture.md#source-hotreload
type Source struct {
	loader *Loader
	config CompositeConfig

	mu      sync.RWMutex
	cached  string
	modTime time.Time
}

// NewSource 返回监听 config 所列文件的提示词源。config 不含文件时内容只加载一次。
func NewSource(loader *Loader, config CompositeConfig) *Source {
	return &Source{
		loader: loader,
		config: config,
	}
}

// NewStaticSource 返回内容固定的提示词源：不监听文件，不做重读。
func NewStaticSource(content string) *Source {
	return &Source{
		cached: content,
	}
}

// Get 返回当前生效的提示词内容。
//
// 所有配置文件的 mtime 均不晚于上次成功载入时刻时命中缓存，不重读磁盘；任一文件更晚
// 则重读并刷新缓存。静态源（loader 为 nil）直接返回固定内容。
// stat 或重读失败时返回既有缓存（若存在），无缓存才返回错误。
// nil 接收者返回空内容。
func (s *Source) Get() (string, error) {
	if s == nil {
		return "", nil
	}
	if s.loader == nil {
		return s.cached, nil
	}

	latestMod, changed, err := s.checkModTimes()
	if err != nil {
		s.mu.RLock()
		cached := s.cached
		s.mu.RUnlock()
		if cached != "" {
			return cached, nil
		}
		return "", fmt.Errorf("prompt source: check mod times: %w", err)
	}

	if !changed {
		s.mu.RLock()
		cached := s.cached
		s.mu.RUnlock()
		return cached, nil
	}

	content, err := s.loader.LoadComposite(s.config.Inline, s.config.Files, s.config.Dir)
	if err != nil {
		s.mu.RLock()
		cached := s.cached
		s.mu.RUnlock()
		if cached != "" {
			return cached, nil
		}
		return "", fmt.Errorf("prompt source: reload: %w", err)
	}

	s.mu.Lock()
	s.cached = content
	s.modTime = latestMod
	s.mu.Unlock()

	return content, nil
}

// IsEmpty 报告是否未配置任何提示词内容或文件。nil 接收者视为空。
func (s *Source) IsEmpty() bool {
	if s == nil {
		return true
	}
	if s.loader == nil {
		return s.cached == ""
	}
	return s.config.IsEmpty()
}

// checkModTimes 返回配置文件中最新的 mtime、是否有文件晚于上次成功载入时刻，以及首个 stat 错误。
// 配置了 Dir 时，该目录下的 .md 一并纳入比较。
func (s *Source) checkModTimes() (latestMod time.Time, changed bool, err error) {
	s.mu.RLock()
	lastMod := s.modTime
	s.mu.RUnlock()

	var paths []string
	for _, f := range s.config.Files {
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) && s.loader.BaseDir != "" {
			f = filepath.Join(s.loader.BaseDir, f)
		}
		paths = append(paths, f)
	}

	if s.config.Dir != "" {
		dir := s.config.Dir
		if !filepath.IsAbs(dir) && s.loader.BaseDir != "" {
			dir = filepath.Join(s.loader.BaseDir, dir)
		}
		entries, readErr := os.ReadDir(dir)
		if readErr == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.ToLower(filepath.Ext(entry.Name())) == ".md" {
					paths = append(paths, filepath.Join(dir, entry.Name()))
				}
			}
		}
	}

	if len(paths) == 0 {
		return lastMod, false, nil
	}

	for _, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return latestMod, false, fmt.Errorf("stat %s: %w", path, statErr)
		}
		mt := info.ModTime()
		if mt.After(lastMod) {
			changed = true
		}
		if mt.After(latestMod) {
			latestMod = mt
		}
	}

	return latestMod, changed, nil
}

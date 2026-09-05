package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Source is a hot-reloadable prompt reference.
// It stores the loader and file paths, and re-reads from disk when files
// are modified. Thread-safe for concurrent access.
//
// Usage:
//
//	src := NewSource(loader, CompositeConfig{Files: []string{"AGENTS.md", "SOUL.md"}})
//	content, err := src.Get() // reads from disk, caches result
//	// ... later, after file is modified ...
//	content, err = src.Get() // detects mtime change, re-reads
type Source struct {
	loader *Loader
	config CompositeConfig

	mu      sync.RWMutex
	cached  string
	modTime time.Time // latest mtime among all source files at last load
}

// NewSource creates a hot-reloadable prompt source.
// If the config has no files (inline-only), the content is loaded once and cached.
func NewSource(loader *Loader, config CompositeConfig) *Source {
	return &Source{
		loader: loader,
		config: config,
	}
}

// NewStaticSource creates a prompt source with a fixed string (no file watching).
func NewStaticSource(content string) *Source {
	return &Source{
		cached: content,
	}
}

// Get returns the current prompt content.
// For file-based sources, it checks file modification times and re-reads
// if any file has changed since the last load.
// For static sources, returns the cached content directly.
func (s *Source) Get() (string, error) {
	if s == nil {
		return "", nil // nil-receiver 安全：typed-nil *Source 装入 prompt.Getter 接口时不 panic（TC0 迁移）
	}
	if s.loader == nil {
		// Static source — no file watching
		return s.cached, nil
	}

	// Check if any source file has been modified
	latestMod, changed, err := s.checkModTimes()
	if err != nil {
		// On error, return cached content (graceful degradation)
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

	// Files changed — re-read
	content, err := s.loader.LoadComposite(s.config.Inline, s.config.Files, s.config.Dir)
	if err != nil {
		// On read error, return cached content (graceful degradation)
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

// IsEmpty returns true if no prompt source is configured.
func (s *Source) IsEmpty() bool {
	if s == nil {
		return true
	}
	if s.loader == nil {
		return s.cached == ""
	}
	return s.config.IsEmpty()
}

// checkModTimes checks if any source file has been modified since the last load.
// Returns the latest mtime, whether any file changed, and any error.
func (s *Source) checkModTimes() (latestMod time.Time, changed bool, err error) {
	s.mu.RLock()
	lastMod := s.modTime
	s.mu.RUnlock()

	// Collect all file paths
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

	// Also scan directory if configured
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
		// No files to check — treat as unchanged
		return lastMod, false, nil
	}

	// Check modification times
	for _, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return latestMod, false, fmt.Errorf("stat %s: %w", path, statErr)
		}
		mt := info.ModTime()
		if mt.After(latestMod) {
			changed = true
		}
		if mt.After(latestMod) {
			latestMod = mt
		}
	}

	return latestMod, changed, nil
}

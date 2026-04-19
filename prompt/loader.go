package prompt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CompositeConfig describes how to load a prompt in bootstrap style.
// Aligned with nanobot's BOOTSTRAP_FILES pattern (AGENTS.md, SOUL.md, USER.md, TOOLS.md).
//
// Prompt composition order: inline → files (in order) → directory scan.
// All parts are joined with double newlines.
type CompositeConfig struct {
	Inline string   `json:"inline,omitempty" yaml:"inline,omitempty"` // Direct inline prompt text
	Files  []string `json:"files,omitempty"  yaml:"files,omitempty"`  // Ordered file list (e.g., AGENTS.md, SOUL.md)
	Dir    string   `json:"dir,omitempty"    yaml:"dir,omitempty"`    // Scan all .md files in directory
}

// IsEmpty returns true if no prompt source is configured.
func (pc CompositeConfig) IsEmpty() bool {
	return pc.Inline == "" && len(pc.Files) == 0 && pc.Dir == ""
}

// Loader loads prompt templates from files or directories.
type Loader struct {
	// BaseDir is the base directory for relative paths.
	BaseDir string
}

// NewLoader creates a new prompt loader.
func NewLoader(baseDir string) *Loader {
	return &Loader{
		BaseDir: baseDir,
	}
}

// LoadFromFile loads a single prompt file.
// Supports both absolute and relative paths.
func (l *Loader) LoadFromFile(path string) (string, error) {
	if path == "" {
		return "", errors.New("prompt file path is empty")
	}

	// If path is relative, resolve against BaseDir
	if !filepath.IsAbs(path) && l.BaseDir != "" {
		path = filepath.Join(l.BaseDir, path)
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("prompt file path is empty after trimming")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// Return original error (os.ErrNotExist etc.)
		return "", fmt.Errorf("read prompt file %s: %w", path, err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", nil // Return empty string instead of error for empty files
	}

	return content, nil
}

// LoadFromDir loads all .md prompt files from a directory.
// Files are sorted alphabetically for deterministic order.
// Subdirectories are skipped.
func (l *Loader) LoadFromDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("prompt directory path is empty")
	}

	// If path is relative, resolve against BaseDir
	if !filepath.IsAbs(dir) && l.BaseDir != "" {
		dir = filepath.Join(l.BaseDir, dir)
	}

	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New("prompt directory path is empty after trimming")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read prompt directory %s: %w", dir, err)
	}

	// Collect .md files
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no .md prompt files in directory %s", dir)
	}

	// Sort for deterministic order
	sort.Strings(files)

	// Load and concatenate
	parts := make([]string, 0, len(files))
	for _, file := range files {
		content, err := l.LoadFromFile(file)
		if err != nil {
			return "", err
		}
		if content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

// LoadFiles loads multiple prompt files and concatenates them.
// Files are separated by double newlines.
// Empty paths or empty content are skipped.
func (l *Loader) LoadFiles(paths []string) (string, error) {
	parts := make([]string, 0, len(paths))

	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		content, err := l.LoadFromFile(path)
		if err != nil {
			return "", err
		}

		if content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

// LoadComposite loads prompts from multiple sources:
// 1. Inline prompt (if not empty)
// 2. Multiple files (if provided)
// 3. Directory (if provided)
//
// All parts are joined with double newlines.
func (l *Loader) LoadComposite(inline string, files []string, dir string) (string, error) {
	parts := make([]string, 0, 1+len(files))

	// 1. Inline prompt
	if v := strings.TrimSpace(inline); v != "" {
		parts = append(parts, v)
	}

	// 2. Multiple files
	if len(files) > 0 {
		fileContent, err := l.LoadFiles(files)
		if err != nil {
			return "", err
		}
		if fileContent != "" {
			parts = append(parts, fileContent)
		}
	}

	// 3. Directory
	dir = strings.TrimSpace(dir)
	if dir != "" {
		dirContent, err := l.LoadFromDir(dir)
		if err != nil {
			return "", err
		}
		if dirContent != "" {
			parts = append(parts, dirContent)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

// SplitCSV splits a comma-separated string into a slice of strings.
// Trims whitespace from each element.
func SplitCSV(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// LoadBootstrap loads bootstrap documents from a directory.
// Bootstrap files are loaded in a specific order defined by BootstrapLoadOrder.
// This is used for loading system prompts for agents.
func (l *Loader) LoadBootstrap(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("bootstrap directory is empty")
	}

	// If path is relative, resolve against BaseDir
	if !filepath.IsAbs(dir) && l.BaseDir != "" {
		dir = filepath.Join(l.BaseDir, dir)
	}

	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("bootstrap directory %s does not exist", dir)
	}

	var results []string

	// Load files in defined order
	for _, filename := range BootstrapLoadOrder {
		path := filepath.Join(dir, filename)
		content, err := l.LoadFromFile(path)
		if err != nil {
			// Skip if file doesn't exist
			if errors.Unwrap(err) != nil && errors.Is(errors.Unwrap(err), os.ErrNotExist) {
				continue
			}
			// Also check the wrapped error
			if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "file does not exist") {
				continue
			}
			return "", err
		}

		if content != "" {
			results = append(results, content)
		}
	}

	// Also load any remaining .md files in the directory
	entries, err := os.ReadDir(dir)
	if err == nil {
		loaded := make(map[string]bool)
		for _, name := range BootstrapLoadOrder {
			loaded[name] = true
		}

		for _, entry := range entries {
			if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
				continue
			}
			if loaded[entry.Name()] {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			content, err := l.LoadFromFile(path)
			if err == nil && content != "" {
				results = append(results, content)
			}
		}
	}

	return strings.Join(results, "\n\n"), nil
}

// BootstrapLoadOrder defines the order in which bootstrap files are loaded.
var BootstrapLoadOrder = []string{
	"AGENTS.md",
	"SOUL.md",
	"USER.md",
	"TOOLS.md",
	"HEARTBEAT.md",
	"MEMORY.md",
}

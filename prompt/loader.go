package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// CompositeConfig 描述一份提示词由哪些来源组成。组装顺序固定为 inline → Files（按给定
// 顺序）→ Dir，各段以空行拼接。
//
// 契约: docs/wiki/prompt/prompt-architecture.md#load-composite
type CompositeConfig struct {
	Inline string   `json:"inline,omitempty" yaml:"inline,omitempty"`
	Files  []string `json:"files,omitempty"  yaml:"files,omitempty"`
	Dir    string   `json:"dir,omitempty"    yaml:"dir,omitempty"`
}

// IsEmpty 报告是否未指定任何提示词来源。
func (pc CompositeConfig) IsEmpty() bool {
	return pc.Inline == "" && len(pc.Files) == 0 && pc.Dir == ""
}

// Loader 从文件或目录读取提示词；配置了内嵌回退 FS 时，磁盘缺失者由该 FS 补齐。
//
// 契约: docs/wiki/prompt/prompt-architecture.md#loader-methods
type Loader struct {
	// BaseDir 是相对路径的解析基准目录，空串表示不做解析。
	BaseDir string

	// fallbackFS 为空表示不启用内嵌回退；非空时仅在磁盘未命中时读取。
	fallbackFS fs.FS
	// fallbackPrefix 是提示词在 fallbackFS 内的根路径（正斜杠分隔）。
	fallbackPrefix string
}

// LoaderOption configures a Loader.
type LoaderOption func(*Loader)

// WithFallback 指定内嵌提示词 FS 及其中的根路径：磁盘未命中时由它补齐，磁盘命中项始终优先。
func WithFallback(fsys fs.FS, prefix string) LoaderOption {
	return func(l *Loader) {
		l.fallbackFS = fsys
		l.fallbackPrefix = strings.Trim(prefix, "/")
	}
}

// NewLoader 创建提示词加载器。不传选项时只读磁盘 `BaseDir`。
func NewLoader(baseDir string, opts ...LoaderOption) *Loader {
	l := &Loader{
		BaseDir: baseDir,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// LoadFromFile 读取单个提示词文件；相对路径按 BaseDir 解析。
// 空文件返回空串而非错误；磁盘未命中且配置了内嵌 FS 时由该 FS 补齐，绝对路径不回退。
func (l *Loader) LoadFromFile(path string) (string, error) {
	if path == "" {
		return "", errors.New("prompt file path is empty")
	}

	orig := path

	if !filepath.IsAbs(path) && l.BaseDir != "" {
		path = filepath.Join(l.BaseDir, path)
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("prompt file path is empty after trimming")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if content, ok := l.fallbackFile(orig, err); ok {
			return content, nil
		}
		return "", fmt.Errorf("read prompt file %s: %w", path, err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", nil
	}

	return content, nil
}

// fallbackFile 按文件名从内嵌 FS 取提示词，仅在磁盘返回 ErrNotExist 时生效；
// 绝对路径不回退。命中时返回内容与 true。
func (l *Loader) fallbackFile(orig string, diskErr error) (string, bool) {
	if l.fallbackFS == nil || filepath.IsAbs(orig) || !errors.Is(diskErr, os.ErrNotExist) {
		return "", false
	}
	full := l.fallbackPrefix + "/" + filepath.Base(orig)
	data, ferr := fs.ReadFile(l.fallbackFS, full)
	if ferr != nil {
		return "", false
	}
	log.Debugf("[prompt] %q absent on disk; using embedded default %q", orig, full)
	return strings.TrimSpace(string(data)), true
}

// fallbackDir 按同名目录整体扫描内嵌 FS，仅在磁盘目录不存在时生效，命中时返回按文件名
// 排序拼接的内容与 true。不与磁盘目录做逐文件合并。
func (l *Loader) fallbackDir(orig string, diskErr error) (string, bool) {
	if l.fallbackFS == nil || filepath.IsAbs(orig) || !errors.Is(diskErr, os.ErrNotExist) {
		return "", false
	}
	sub := l.fallbackPrefix + "/" + filepath.Base(orig)
	entries, ferr := fs.ReadDir(l.fallbackFS, sub)
	if ferr != nil {
		return "", false
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".md" {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "", false
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		data, e := fs.ReadFile(l.fallbackFS, sub+"/"+n)
		if e != nil {
			continue
		}
		if c := strings.TrimSpace(string(data)); c != "" {
			parts = append(parts, c)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	log.Debugf("[prompt] dir %q absent on disk; using embedded %q", orig, sub)
	return strings.Join(parts, "\n\n"), true
}

// LoadFromDir 读取目录一层的 .md 提示词并按文件名排序，子目录跳过，非 .md 文件忽略。
// 目录内无 .md 时返回错误。
func (l *Loader) LoadFromDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("prompt directory path is empty")
	}

	orig := dir

	if !filepath.IsAbs(dir) && l.BaseDir != "" {
		dir = filepath.Join(l.BaseDir, dir)
	}

	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New("prompt directory path is empty after trimming")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if content, ok := l.fallbackDir(orig, err); ok {
			return content, nil
		}
		return "", fmt.Errorf("read prompt directory %s: %w", dir, err)
	}

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

	sort.Strings(files)

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

// LoadFiles 按序读取多个提示词文件并以空行拼接。空路径与空内容跳过；磁盘上不存在的文件
// 也跳过（可选上下文文件在干净检出中合理地缺失），但真实读错误仍向上传播。
//
// 契约: docs/wiki/prompt/prompt-architecture.md#load-files
func (l *Loader) LoadFiles(paths []string) (string, error) {
	parts := make([]string, 0, len(paths))

	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		content, err := l.LoadFromFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				log.Infof("[prompt] optional file %q absent, skipping (load-if-present)", path)
				continue
			}
			return "", err
		}

		if content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

// LoadComposite 按 inline → files → dir 的顺序加载各来源并以空行拼接；缺省的来源跳过。
func (l *Loader) LoadComposite(inline string, files []string, dir string) (string, error) {
	parts := make([]string, 0, 1+len(files))

	if v := strings.TrimSpace(inline); v != "" {
		parts = append(parts, v)
	}

	if len(files) > 0 {
		fileContent, err := l.LoadFiles(files)
		if err != nil {
			return "", err
		}
		if fileContent != "" {
			parts = append(parts, fileContent)
		}
	}

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

// SplitCSV 按逗号切分并去除各元素首尾空白，空元素丢弃；入参为空串时返回 nil。
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

// LoadBootstrap 按 BootstrapLoadOrder 给定的顺序装配目录中的文档，顺序表之外的 .md
// 追加在末尾；缺失的文件跳过，目录不存在时报错。
func (l *Loader) LoadBootstrap(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("bootstrap directory is empty")
	}

	if !filepath.IsAbs(dir) && l.BaseDir != "" {
		dir = filepath.Join(l.BaseDir, dir)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("bootstrap directory %s does not exist", dir)
	}

	var results []string

	for _, filename := range BootstrapLoadOrder {
		path := filepath.Join(dir, filename)
		content, err := l.LoadFromFile(path)
		if err != nil {
			if errors.Unwrap(err) != nil && errors.Is(errors.Unwrap(err), os.ErrNotExist) {
				continue
			}
			if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "file does not exist") {
				continue
			}
			return "", err
		}

		if content != "" {
			results = append(results, content)
		}
	}

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

// BootstrapLoadOrder 是装配文档的加载顺序，也是该序列的唯一真源。
var BootstrapLoadOrder = []string{
	"AGENTS.md",
	"SOUL.md",
	"USER.md",
	"TOOLS.md",
	"HEARTBEAT.md",
	"MEMORY.md",
}

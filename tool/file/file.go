// Package file wraps trpc-agent-go's built-in file operation tools for tagent.
//
// - read_file, save_file, list_file and friends register as plain tools for agent YAML.
// - base_dir falls back to the agent working root, then ".", sharing one filesystem view with the exec tool.
// 契约: docs/wiki/platform/agent-behavior-matrix.md#working-dir
package file

import (
	"context"
	"fmt"
	"sync"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"

	"github.com/SpellingDragon/tagent/agent"
)

// fileToolNames lists the tool names exposed by trpc-agent-go's file toolset.
var fileToolNames = []string{
	"read_file",
	"save_file",
	"list_file",
	"search_file",
	"search_content",
	"read_multiple_files",
	"replace_content",
}

// toolSetCache holds file.ToolSet instances keyed by base directory.
// File tools sharing the same base_dir reuse the same ToolSet.
var (
	toolSetCache = map[string]trpctool.ToolSet{}
	toolSetMu    sync.Mutex
)

var registerOnce sync.Once

// RegisterTools registers all built-in file operation tools as plain tools.
// Should be called once during tagent's built-in tool registration.
// Uses sync.Once for idempotency — safe to call multiple times.
func RegisterTools() {
	registerOnce.Do(func() {
		for _, name := range fileToolNames {
			name := name
			agent.RegisterPlainTool(name, makeFileToolFactory(name))
		}
	})
}

// makeFileToolFactory returns a plain tool factory for the given file tool name.
func makeFileToolFactory(name string) agent.PlainToolFactory {
	return func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		baseDir := resolveBaseDir(cfg.Properties, cfg.WorkingDir)

		ts, err := getToolSet(baseDir)
		if err != nil {
			return nil, fmt.Errorf("file tool %q: create file toolset: %w", name, err)
		}

		for _, t := range ts.Tools(context.Background()) {
			decl := t.Declaration()
			if decl == nil || decl.Name != name {
				continue
			}
			ct, ok := t.(trpctool.CallableTool)
			if !ok {
				return nil, fmt.Errorf("file tool %q: registered tool %T does not implement CallableTool", name, t)
			}
			return ct, nil
		}

		return nil, fmt.Errorf("file tool %q: not found in file toolset", name)
	}
}

// resolveBaseDir 解析 file 工具的根目录。优先级:显式 properties.base_dir > agent 级 WorkingDir
// (config.working_dir / TAGENT_WORKING_DIR) > "."(进程 cwd,现状默认)。与 exec 命令 cwd 走同一
// 优先级(builtin.go actionFactory),二者始终一致 → 模型看到单一文件系统视图。
func resolveBaseDir(props map[string]any, workingDir string) string {
	if props != nil {
		if v, ok := props["base_dir"].(string); ok && v != "" {
			return v
		}
	}
	if workingDir != "" {
		return workingDir
	}
	return "."
}

// getToolSet returns a cached file.ToolSet for the given base directory.
func getToolSet(baseDir string) (trpctool.ToolSet, error) {
	toolSetMu.Lock()
	defer toolSetMu.Unlock()

	if ts, ok := toolSetCache[baseDir]; ok {
		return ts, nil
	}

	ts, err := file.NewToolSet(file.WithBaseDir(baseDir))
	if err != nil {
		return nil, err
	}

	toolSetCache[baseDir] = ts
	return ts, nil
}

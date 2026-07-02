// Package file wraps trpc-agent-go's built-in file operation tools for tagent.
//
// It registers individual file tools (read_file, save_file, list_file, etc.)
// as plain tools so they can be referenced directly from agent YAML configs.
//
// Configuration (via ToolRef.Properties):
//   - base_dir: root directory for file operations (default: current working directory ".")
//
// Example YAML:
//
//	tools:
//	  - kind: tool
//	    id: read_file
//	    description_file: read_file_tool_desc.md
//	    properties:
//	      base_dir: "./workspace"
package file

import (
	"context"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/agent"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"
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

// RegisterTools registers all built-in file operation tools as plain tools.
// Should be called once during tagent's built-in tool registration.
// Uses sync.Once for idempotency — safe to call multiple times.
var registerOnce sync.Once

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
		baseDir := resolveBaseDir(cfg.Properties)

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

// resolveBaseDir extracts the base directory from tool properties.
func resolveBaseDir(props map[string]any) string {
	if props == nil {
		return "."
	}
	v, ok := props["base_dir"].(string)
	if !ok || v == "" {
		return "."
	}
	return v
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

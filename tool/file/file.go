// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
//
// Package file is the model-facing file surface of an action agent: the file
// tools one declaration grants, confined to the base directory and the read
// roots the same declaration fixes. The package owns no file system of its
// own: every tool is the tRPC file tool set, so this package only selects the
// granted tools and keeps their request prefix in declaration order.
package file

import (
	"context"
	"errors"
	"fmt"
	"strings"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	trpcfile "trpc.group/trpc-go/trpc-agent-go/tool/file"
)

// SaveFileToolName and the six sibling names are the seven names the
// declaration and the action prompt share. They are frozen: a rename is a
// prompt change and a declaration change in one breath.
const (
	// SaveFileToolName is the name of the file write tool.
	SaveFileToolName = "save_file"
	// ReadFileToolName is the name of the single file read tool.
	ReadFileToolName = "read_file"
	// ReadMultipleFilesToolName is the name of the multi-file read tool.
	ReadMultipleFilesToolName = "read_multiple_files"
	// ListFileToolName is the name of the directory listing tool.
	ListFileToolName = "list_file"
	// SearchFileToolName is the name of the file name search tool.
	SearchFileToolName = "search_file"
	// SearchContentToolName is the name of the file content search tool.
	SearchContentToolName = "search_content"
	// ReplaceContentToolName is the name of the in-place edit tool.
	ReplaceContentToolName = "replace_content"
)

// ErrUnknownTool reports a declared tool id the file surface does not carry,
// so a declaration that drifts from the frozen seven stops the assembly
// instead of exposing a name nothing answers.
var ErrUnknownTool = errors.New("file: the declaration names no such file tool")

// ErrDuplicateTool reports a declared tool id named twice: one grant is one
// entry of the request prefix.
var ErrDuplicateTool = errors.New("file: the declaration names the same file tool twice")

// Names returns the seven names in the frozen order the tool set carries them.
func Names() []string {
	return []string{
		SaveFileToolName,
		ReadFileToolName,
		ReadMultipleFilesToolName,
		ListFileToolName,
		SearchFileToolName,
		SearchContentToolName,
		ReplaceContentToolName,
	}
}

// Options fixes the face one file surface is assembled over: the directory
// every path resolves under, the roots a read may name absolutely, and the
// tools the declaration grants.
type Options struct {
	// BaseDir is the directory every relative path resolves under; an empty
	// directory means the current directory.
	BaseDir string
	// ReadRoots are the additional roots an absolute read may name; writes
	// never reach outside BaseDir.
	ReadRoots []string
	// Tools selects the granted tools in declaration order; an empty list
	// grants all seven in the frozen order.
	Tools []string
}

// Surface is the file tool set of one agent: the granted tools over the
// declared base directory, in the order the declaration names them.
type Surface struct {
	baseDir string
	name    string
	inner   trpctool.ToolSet
	tools   []trpctool.Tool
}

// New assembles the file surface the options fix. The base directory must
// exist when the surface is built, so a deployment whose declared directory
// is missing is refused at assembly rather than at the first call.
func New(opts Options) (*Surface, error) {
	base := strings.TrimSpace(opts.BaseDir)
	if base == "" {
		base = "."
	}
	granted, err := selectTools(opts.Tools)
	if err != nil {
		return nil, err
	}
	inner, err := trpcfile.NewToolSet(
		trpcfile.WithBaseDir(base),
		trpcfile.WithReadOnlyDirs(opts.ReadRoots...),
	)
	if err != nil {
		return nil, fmt.Errorf("file: open the base directory %q: %w", base, err)
	}
	carried := make(map[string]trpctool.Tool, len(granted))
	for _, tool := range inner.Tools(context.Background()) {
		carried[declaredName(tool)] = tool
	}
	tools := make([]trpctool.Tool, 0, len(granted))
	for _, name := range granted {
		tool, ok := carried[name]
		if !ok {
			_ = inner.Close()
			return nil, fmt.Errorf("file: the native file tool set carries no %q tool", name)
		}
		tools = append(tools, tool)
	}
	return &Surface{baseDir: base, name: inner.Name(), inner: inner, tools: tools}, nil
}

// selectTools resolves the granted names: no declaration grants all seven in
// the frozen order, and a written declaration grants exactly its entries in
// its own order.
func selectTools(declared []string) ([]string, error) {
	if len(declared) == 0 {
		return Names(), nil
	}
	granted := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, raw := range declared {
		name := strings.TrimSpace(raw)
		if !known(name) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownTool, raw)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTool, name)
		}
		seen[name] = true
		granted = append(granted, name)
	}
	return granted, nil
}

// known reports whether one name belongs to the frozen seven.
func known(name string) bool {
	for _, candidate := range Names() {
		if candidate == name {
			return true
		}
	}
	return false
}

// declaredName reports the name one native tool declares.
func declaredName(tool trpctool.Tool) string {
	if tool == nil || tool.Declaration() == nil {
		return ""
	}
	return tool.Declaration().Name
}

// Tools implements the tool set interface: the granted tools in declaration
// order.
func (s *Surface) Tools(context.Context) []trpctool.Tool {
	return s.tools
}

// Close implements the tool set interface: the surface holds no resource the
// native tool set does not already release.
func (s *Surface) Close() error {
	return s.inner.Close()
}

// Name implements the tool set interface.
func (s *Surface) Name() string {
	return s.name
}

// BaseDir reports the directory every relative path resolves under.
func (s *Surface) BaseDir() string {
	return s.baseDir
}

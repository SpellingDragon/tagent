package file

import (
	"context"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
//
// Skeleton compile-time assertions for the file surface: the implementation
// relationships and the exact method shapes. A renamed method, a changed
// parameter or a changed result stops these blocks from compiling.
var (
	_ trpctool.ToolSet = (*Surface)(nil)
)

// The declared names must stay compile-time constants of type string: a
// variable or a renamed constant stops this block from compiling.
const (
	_ string = SaveFileToolName
	_ string = ReadFileToolName
	_ string = ReadMultipleFilesToolName
	_ string = ListFileToolName
	_ string = SearchFileToolName
	_ string = SearchContentToolName
	_ string = ReplaceContentToolName
)

// Every declared function keeps its exact shape.
var (
	_ func(Options) (*Surface, error)                 = New
	_ func() []string                                 = Names
	_ func(*Surface, context.Context) []trpctool.Tool = (*Surface).Tools
	_ func(*Surface) error                            = (*Surface).Close
	_ func(*Surface) string                           = (*Surface).Name
	_ func(*Surface) string                           = (*Surface).BaseDir
)

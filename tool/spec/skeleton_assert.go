package spec

// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
//
// Skeleton compile-time assertions for the declarations of this package: the
// backend port keeps its exact method shapes, every operation stays a constant
// of its declared type, the argument shape keeps every field a call may carry,
// and the tool keeps the native call surface the runtime admits it through. A
// renamed method, a changed parameter or a changed result stops these blocks
// from compiling.
import (
	"context"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// Every declared operation is a constant of its declared type.
const (
	_ Op = OpInit
	_ Op = OpNew
	_ Op = OpStatus
	_ Op = OpValidate
	_ Op = OpArchive
	_ Op = OpInstructions
	_ Op = OpList
	_ Op = ""
)

// The backend port keeps its method shapes.
var (
	_ func(Backend, context.Context, Request) (Result, error) = Backend.Run
	_ func(Backend) string                                    = Backend.Name
)

// The tool keeps the native call surface and the plan backend implements the
// port it is declared over.
var (
	_ trpctool.CallableTool = (*Tool)(nil)
	_ trpctool.Tool         = (*Tool)(nil)
	_ Backend               = (*openspecBackend)(nil)
)

// The request and the result keep every documented field.
var (
	_ = Request{
		Op:       "",
		Name:     "",
		Artifact: "",
		JSON:     false,
	}

	_ = Result{
		Op:       "",
		ExitCode: 0,
		Output:   "",
		OK:       false,
		Hint:     "",
	}
)

// The tool name is a constant of its declared type.
const _ string = ToolName

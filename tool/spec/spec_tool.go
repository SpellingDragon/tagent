// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
package spec

import (
	"context"
	"encoding/json"
	"fmt"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolName is the tool name the declaration and the plan prompt share. It is
// frozen: a rename is a prompt change and a declaration change in one breath.
const ToolName = "spec"

// Tool is the model-visible face of one plan backend: the closed operation set
// beside the fields each operation consumes, and nothing else.
type Tool struct {
	backend Backend
}

// The tool satisfies the native callable tool port, so the runtime admits it
// under the same registration wrapper as every other native tool.
var _ trpctool.CallableTool = (*Tool)(nil)

// NewTool returns the spec tool over the given backend.
func NewTool(backend Backend) *Tool { return &Tool{backend: backend} }

// specArgs is the argument shape one call carries: the operation beside the
// fields it may consume.
type specArgs struct {
	// Op is the operation to run; it must belong to the closed set.
	Op string `json:"op"`
	// Name is the plan the operation addresses.
	Name string `json:"name,omitempty"`
	// Artifact names the artifact whose instructions are asked for.
	Artifact string `json:"artifact,omitempty"`
	// JSON asks for the machine-readable form of status and list.
	JSON bool `json:"json,omitempty"`
}

// Declaration reports the tool declaration: the closed operation set as an
// enumerated field beside the fields the operations consume, so the model reads
// the whole surface before it calls.
func (t *Tool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: ToolName,
		Description: "manage specification-driven work plans without a shell: one typed operation per call. " +
			"op=init makes the workspace ready (idempotent); op=new creates a plan (name required); " +
			"op=status reports the artifact state of a plan (name optional) or of the workspace; " +
			"op=validate checks a plan under the strict rule set (name required, plans carrying no spec deltas fail by design); " +
			"op=archive closes a plan whose tasks are all checked (name required); " +
			"op=instructions returns the writing instructions of one artifact (artifact required: proposal/specs/design/tasks); " +
			"op=list lists the plans of the workspace. Plan files are written and read through the file tools, not through this one.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"op": {
					Type:        "string",
					Description: "the plan operation to run",
					Enum: []any{
						string(OpInit),
						string(OpNew),
						string(OpStatus),
						string(OpValidate),
						string(OpArchive),
						string(OpInstructions),
						string(OpList),
					},
				},
				"name": {
					Type:        "string",
					Description: "the plan name (kebab-case); required by new, validate and archive",
				},
				"artifact": {
					Type:        "string",
					Description: "the artifact whose instructions are asked for: proposal, specs, design or tasks",
				},
				"json": {
					Type:        "boolean",
					Description: "ask for the machine-readable form of status and list",
				},
			},
			Required: []string{"op"},
		},
	}
}

// Call runs one operation through the backend and answers with its result. An
// operation outside the closed set is refused by name before the backend is
// reached, and a tool beside no backend refuses every call by name.
func (t *Tool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args specArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadArguments, err)
	}
	req := Request{
		Op:       Op(args.Op),
		Name:     args.Name,
		Artifact: args.Artifact,
		JSON:     args.JSON,
	}
	if !Valid(req.Op) {
		return nil, fmt.Errorf("%w: %q (allowed: %s)", ErrUnknownOp, args.Op, allowedOps)
	}
	if t.backend == nil {
		return nil, ErrBackendMissing
	}
	return t.backend.Run(ctx, req)
}

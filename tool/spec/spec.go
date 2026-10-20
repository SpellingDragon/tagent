// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
//
// Package spec is the model-facing face of work-plan management: one typed
// operation set over a plan backend, so plan management reaches the model
// without handing it a general shell.
//
// - The plan format sits behind Backend, so a deployment swaps the backend without changing the model-visible surface: the declarations, the hints and the refusals speak of plans and their workspace, never of the backend a deployment wired, and the workspace root is a core semantic the backend reports rather than a deployment setting.
package spec

import (
	"context"
	"errors"
)

// Op enumerates the plan operations the model may invoke. The set is closed:
// an operation outside it is refused by name before any backend is reached, so
// the model can only ever invoke a known operation.
type Op string

const (
	// OpInit makes the plan workspace ready; the operation is idempotent.
	OpInit Op = "init"
	// OpNew creates one plan under Name.
	OpNew Op = "new"
	// OpStatus reports the artifact state of one plan, or of the workspace when Name is empty.
	OpStatus Op = "status"
	// OpValidate checks one plan under the strict rule set.
	OpValidate Op = "validate"
	// OpArchive closes one plan under the backend's own completion rule.
	OpArchive Op = "archive"
	// OpInstructions returns the writing instructions of one artifact.
	OpInstructions Op = "instructions"
	// OpList lists the plans of the workspace.
	OpList Op = "list"
)

// allowedOps renders the closed set in declaration order for the refusals that
// name it.
const allowedOps = "init, new, status, validate, archive, instructions, list"

// validOps is the dispatch set: every operation the tool admits.
var validOps = map[Op]bool{
	OpInit:         true,
	OpNew:          true,
	OpStatus:       true,
	OpValidate:     true,
	OpArchive:      true,
	OpInstructions: true,
	OpList:         true,
}

// ErrUnknownOp reports an operation outside the closed set: the call is refused
// by name and nothing is dispatched.
var ErrUnknownOp = errors.New("spec: unknown operation")

// ErrBackendMissing reports a tool assembled beside no plan backend: the call is
// refused by name instead of dispatching anywhere.
var ErrBackendMissing = errors.New("spec: the deployment wires no plan backend")

// ErrBadArguments reports a call whose arguments are not the declared shape, so
// no operation is read out of them.
var ErrBadArguments = errors.New("spec: the call arguments are not the declared shape")

// Valid reports whether one operation belongs to the closed set.
func Valid(op Op) bool { return validOps[op] }

// Request is one plan operation: the operation beside the fields it consumes.
// A field the operation does not read is not an error, so one argument shape
// serves the whole set.
type Request struct {
	// Op is the operation to run.
	Op Op `json:"op"`
	// Name is the plan the operation addresses; empty addresses the workspace.
	Name string `json:"name,omitempty"`
	// Artifact names the artifact whose instructions are asked for.
	Artifact string `json:"artifact,omitempty"`
	// JSON asks for the machine-readable form of status and list.
	JSON bool `json:"json,omitempty"`
}

// Result is the outcome of one plan operation: the exit code and the output
// beside the actionable hint of a failure.
type Result struct {
	// Op is the operation the result belongs to.
	Op Op `json:"op"`
	// ExitCode is the exit code the backend reported.
	ExitCode int `json:"exit_code"`
	// Output is what the backend printed.
	Output string `json:"output"`
	// OK reports whether the operation succeeded.
	OK bool `json:"ok"`
	// Hint is the actionable guidance a failed operation carries.
	Hint string `json:"hint,omitempty"`
}

// Backend abstracts a plan-management system: the operations it answers and the
// storage root its plan files live under. Swapping the plan format means
// providing another Backend, and neither the tool nor the model changes.
//
// 红线：
// MUST pass every argument as a discrete argv entry of a fixed program.
// MUST NOT run a model-controlled string through a shell.
// MUST report a failed operation as a Result beside no error, and an operation it cannot dispatch at all as a named error.
type Backend interface {
	// Run executes one operation and answers with what the backend reported. A
	// failure the backend observed is a Result with OK false, not an error; an
	// error means the operation never ran.
	Run(ctx context.Context, req Request) (Result, error)
	// Name identifies the backend for logging and diagnostics by what it does
	// rather than by what implements it, so one diagnostic line reads the same
	// whichever backend a deployment wires.
	Name() string
	// WorkspaceRoot reports the directory the plan files live under. The host
	// points the file tools of the plan agent at this root, so the deployment's
	// own configuration never names the backend's storage directory.
	WorkspaceRoot() string
}

package spec // import "github.com/SpellingDragon/tagent/tool/spec"

Package spec is the model-facing face of specification-driven work plans:
one typed operation set over a plan backend, so plan management reaches the
model without handing it a general shell.

- The plan format sits behind Backend, so a deployment swaps

CONSTANTS

const ToolName = "spec"
    ToolName is the tool name the declaration and the plan prompt share. It is
    frozen: a rename is a prompt change and a declaration change in one breath.

VARIABLES

var ErrBackendMissing = errors.New("spec: the deployment wires no plan backend")
    ErrBackendMissing reports a tool assembled beside no plan backend: the call
    is refused by name instead of dispatching anywhere.

var ErrBadArguments = errors.New("spec: the call arguments are not the declared shape")
    ErrBadArguments reports a call whose arguments are not the declared shape,
    so no operation is read out of them.

var ErrUnknownOp = errors.New("spec: unknown operation")
    ErrUnknownOp reports an operation outside the closed set: the call is
    refused by name and nothing is dispatched.

FUNCTIONS

func Valid(op Op) bool
    Valid reports whether one operation belongs to the closed set.

TYPES

type Backend interface {
	// Run executes one operation and answers with what the backend reported. A
	// failure the backend observed is a Result with OK false, not an error; an
	// error means the operation never ran.
	Run(ctx context.Context, req Request) (Result, error)
	// Name identifies the backend for logging and diagnostics.
	Name() string
}
    Backend abstracts a plan-management system. The implementation beside this
    declaration runs the openspec CLI; swapping the plan format means providing
    another Backend, and neither the tool nor the model changes.

    红线： MUST pass every argument as a discrete argv entry of a fixed program.
    MUST NOT run a model-controlled string through a shell. MUST report a failed
    operation as a Result beside no error, and an operation it cannot dispatch
    at all as a named error.

func NewOpenSpecBackend(opts ...OpenSpecOption) Backend
    NewOpenSpecBackend returns the backend over the openspec CLI.

type Op string
    Op enumerates the plan operations the model may invoke. The set is closed:
    an operation outside it is refused by name before any backend is reached,
    so the model can only ever invoke a known operation.

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
type OpenSpecOption func(*openspecBackend)
    OpenSpecOption configures one openspec backend.

func WithOpenSpecBin(bin string) OpenSpecOption
    WithOpenSpecBin names the CLI binary or path; an empty name keeps the
    default.

func WithWorkDir(dir string) OpenSpecOption
    WithWorkDir sets the directory the CLI runs in — the directory that holds
    the openspec workspace. An empty directory runs in the working directory of
    the process.

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
    Request is one plan operation: the operation beside the fields it consumes.
    A field the operation does not read is not an error, so one argument shape
    serves the whole set.

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
    Result is the outcome of one plan operation: the exit code and the output
    beside the actionable hint of a failure.

type Tool struct {
	// Has unexported fields.
}
    Tool is the model-visible face of one plan backend: the closed operation set
    beside the fields each operation consumes, and nothing else.

func NewTool(backend Backend) *Tool
    NewTool returns the spec tool over the given backend.

func (t *Tool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call runs one operation through the backend and answers with its result.
    An operation outside the closed set is refused by name before the backend is
    reached, and a tool beside no backend refuses every call by name.

func (t *Tool) Declaration() *trpctool.Declaration
    Declaration reports the tool declaration: the closed operation set as an
    enumerated field beside the fields the operations consume, so the model
    reads the whole surface before it calls.

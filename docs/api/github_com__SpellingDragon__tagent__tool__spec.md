package spec // import "github.com/SpellingDragon/tagent/tool/spec"

Package spec provides an LLM-facing tool for managing specification-driven work
plans (create / status / validate / archive / …) without handing the agent a
general shell.

Design intent: the plan agent must only ever touch the spec workspace.
File reads/writes go through the sandboxed file tools (base_dir locked);
spec management goes through this typed tool. There is deliberately NO exec
in the plan agent's toolset — "only spec commands" is a structural fact,
not a prompt-level hope.

The actual plan format is abstracted behind the Backend interface so the current
openspec implementation can be swapped for another spec system without changing
the tool surface the model sees.

FUNCTIONS

func NewSpecTool(backend Backend) trpctool.Tool
    NewSpecTool creates the spec-management function tool over a Backend.
    The tool surface is backend-agnostic; swapping the plan format only changes
    the Backend injected here.

func RegisterTool()
    RegisterTool registers the spec tool as a built-in plain tool, backed by the
    openspec CLI. Agents opt in via config. Properties: - bin: openspec binary
    name/path - work_dir: working directory containing openspec/ (default:
    process cwd)


TYPES

type Backend interface {
	// Run executes one spec operation. Implementations must never run
	// model-controlled strings through a shell; arguments are passed as
	// discrete argv entries to a fixed program.
	Run(ctx context.Context, req Request) (Result, error)
	// Name identifies the backend (for logging / diagnostics).
	Name() string
}
    Backend abstracts a spec-management system. The current implementation is
    openspecBackend; swapping the plan format means providing another Backend —
    the tool and the model never change.

func NewOpenSpecBackend(opts ...OpenSpecOption) Backend
    NewOpenSpecBackend creates a Backend backed by the openspec CLI.

type Op string
    Op enumerates the spec-management operations exposed to the model. Keeping
    this a closed set (validated before dispatch) is what makes the tool safe:
    the model can only ever invoke a known operation, never an arbitrary
    command.

const (
	OpInit         Op = "init"
	OpNew          Op = "new"
	OpStatus       Op = "status"
	OpValidate     Op = "validate"
	OpArchive      Op = "archive"
	OpInstructions Op = "instructions"
	OpList         Op = "list"
)
type OpenSpecOption func(*openspecBackend)
    OpenSpecOption configures an openspec backend.

func WithOpenSpecBin(bin string) OpenSpecOption
    WithOpenSpecBin overrides the CLI binary name/path.

func WithWorkDir(dir string) OpenSpecOption
    WithWorkDir sets the working directory for openspec invocations (the
    directory that contains openspec/). Defaults to the process cwd.

type Request struct {
	Op       Op     `json:"op"`
	Name     string `json:"name,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	JSON     bool   `json:"json,omitempty"`
}
    Request is a single spec operation. Fields are optional per op; the Backend
    documents which it consumes.

type Result struct {
	Op       Op     `json:"op"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	OK       bool   `json:"ok"`
	Hint     string `json:"hint,omitempty"`
}
    Result is the outcome of a spec operation.


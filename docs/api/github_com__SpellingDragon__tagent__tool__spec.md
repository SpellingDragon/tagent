package spec // import "github.com/SpellingDragon/tagent/tool/spec"

Package spec provides an LLM-facing tool for specification-driven work plans
(create / status / validate / archive) without handing the agent a general
shell.

- The plan format sits behind the Backend interface, so the openspec
implementation is swappable without changing the model-visible surface.

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
	// OpInit 初始化 spec 工作区（argv：init --tools none）。
	OpInit Op = "init"
	// OpNew 新建一个 change，Name 必填。
	OpNew Op = "new"
	// OpStatus 查询状态；Name 选填（限定单个 change），JSON 选填。
	OpStatus Op = "status"
	// OpValidate 以 --strict 校验指定 change，Name 必填。
	OpValidate Op = "validate"
	// OpArchive 归档指定 change，Name 必填。
	OpArchive Op = "archive"
	// OpInstructions 取某类产物的写作指引，Artifact 必填（proposal/specs/design/tasks），Name 选填。
	OpInstructions Op = "instructions"
	// OpList 列出 change；JSON 选填。
	OpList Op = "list"
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

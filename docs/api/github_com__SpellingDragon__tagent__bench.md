package bench // import "github.com/SpellingDragon/tagent/bench"

Package bench owns the live-benchmark task set, its task declaration and the
machine-readable acceptance verifiers the three runners share.

A task is data, not a test: the JSON fixtures under bench/tasks/ declare the
prompts, the required capability set, the setup files, the verifier and one
positive/negative example pair. The exported Task and Verifier surface is
importable (features/rl/sample reuses it) and never depends on a runner.

CONSTANTS

const (
	CategoryQA    = "qa"
	CategoryFile  = "file"
	CategoryExec  = "exec"
	CategoryMulti = "multi"
)
    CategoryQA, CategoryFile, CategoryExec and CategoryMulti name the task
    categories; a category fixes the number of prompts a fixture declares:
    qa/file/exec are single-turn, multi is two or more dependent turns.

const (
	ToolFileRead  = "file.read"
	ToolFileWrite = "file.write"
	ToolExec      = "exec"
)
    ToolFileRead, ToolFileWrite and ToolExec name the capabilities a task may
    declare; runners map each capability onto their own tool surface, so the
    names are the mapping contract, not tool names.

const CapabilitiesFileName = "capabilities.json"
    CapabilitiesFileName is the tool-face capability map file name.

const MetricsFileName = "metrics.json"
    MetricsFileName is the five-metric document file name.

const ReportFileName = "report.md"
    ReportFileName is the rendered comparison report file name.

const RunFileName = "run.json"
    RunFileName is the run record file name.

const TaskDirName = "tasks"
    TaskDirName is the per-attempt detail directory.

FUNCTIONS

func WorkspaceReadPath(workspace, rel string) (string, error)
    WorkspaceReadPath resolves an existing workspace file and proves its real
    path stays inside the workspace.

func WorkspaceWritePath(workspace, rel string) (string, error)
    WorkspaceWritePath resolves a workspace file that may not exist yet,
    creating its parent inside the workspace.

func WriteRun(root, runID string, files map[string][]byte) (string, error)
    WriteRun writes one run directory under root and returns its absolute path.
    Every key of files is a workspace-relative path; a key that escapes the run
    directory is rejected before anything is written.

TYPES

type Task struct {
	// ID is the stable fixture id, equal to the fixture file base name.
	ID string
	// Category is one of the Category constants.
	Category string
	// Prompts are the user turns in order; single-turn tasks carry one.
	Prompts []string
	// Tools is the capability set the runner must cover.
	Tools []string
	// Verifier decides success from the workspace and the final response.
	Verifier Verifier

	// Has unexported fields.
}
    Task is one benchmark task declaration: the prompts a runner sends,
    the capabilities the task needs, the isolated setup it starts from,
    and the machine-readable verifier that decides success. A Task never carries
    a runner or a model; the same declaration drives all three runners.

func All() ([]Task, error)
    All returns the embedded 12-task fixture set, sorted by task id.

func Load(dir string) ([]Task, error)
    Load reads task fixtures from a directory on disk.

func LoadFS(fsys fs.FS) ([]Task, error)
    LoadFS reads every *.json fixture directly under fsys, in name order,
    and rejects duplicate ids, unknown fields, unsafe paths and incomplete
    verifiers.

func (t Task) Prepare(workspace string) error
    Prepare writes the task's declared setup files into workspace. The setup
    is part of the fixture, not model output: every runner starts from the same
    tree.

func (t Task) Verify(workspace, output string) error
    Verify runs the task's verifier over an artifact. It is the runner-facing
    shorthand; the verifier itself stays replaceable through the Task field.

type Verifier interface {
	// Verify accepts or rejects one task artifact. workspace is the isolated
	// task directory; output is the final response text of the run. Every
	// declared check must pass; the returned error names the failing check.
	Verify(workspace, output string) error
}
    Verifier 是任务产物的机读验收口：同一份实现供 bench 三方 runner 与 07 训练 样本的独立验收复用，成功判定不依赖人眼。

    验收红线清单（2 条）： MUST 只依据工作区内的可复核证据（响应文本 / 文件 / 命令输出）判定 MUST NOT
    信任被测模型的自我声明，也不读取工作区以外的路径

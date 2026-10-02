// Package spec provides an LLM-facing tool for managing specification-driven
// work plans (create / status / validate / archive / …) without handing the
// agent a general shell.
//
// Design intent: the plan agent must only ever touch the spec workspace. File
// reads/writes go through the sandboxed file tools (base_dir locked); spec
// management goes through this typed tool. There is deliberately NO exec in
// the plan agent's toolset — "only spec commands" is a structural fact, not a
// prompt-level hope.
//
// The actual plan format is abstracted behind the Backend interface so the
// current openspec implementation can be swapped for another spec system
// without changing the tool surface the model sees.
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
package spec

import "context"

// Op enumerates the spec-management operations exposed to the model. Keeping
// this a closed set (validated before dispatch) is what makes the tool safe:
// the model can only ever invoke a known operation, never an arbitrary command.
type Op string

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

// validOps is the dispatch whitelist.
var validOps = map[Op]bool{
	OpInit: true, OpNew: true, OpStatus: true, OpValidate: true,
	OpArchive: true, OpInstructions: true, OpList: true,
}

// Request is a single spec operation. Fields are optional per op; the Backend
// documents which it consumes.
type Request struct {
	Op       Op     `json:"op"`
	Name     string `json:"name,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	JSON     bool   `json:"json,omitempty"`
}

// Result is the outcome of a spec operation.
type Result struct {
	Op       Op     `json:"op"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	OK       bool   `json:"ok"`
	Hint     string `json:"hint,omitempty"`
}

// Backend abstracts a spec-management system. The current implementation is
// openspecBackend; swapping the plan format
// means providing another Backend — the tool and the model never change.
type Backend interface {
	// Run executes one spec operation. Implementations must never run
	// model-controlled strings through a shell; arguments are passed as
	// discrete argv entries to a fixed program.
	Run(ctx context.Context, req Request) (Result, error)
	// Name identifies the backend (for logging / diagnostics).
	Name() string
}

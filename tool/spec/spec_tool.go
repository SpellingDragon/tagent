package spec

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/agent"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// specArgs is the LLM-facing argument shape: a closed op plus optional fields.
type specArgs struct {
	Op       string `json:"op" jsonschema:"required,description=操作: init(初始化工作区,幂等) / new(新建计划) / status(查询状态) / validate(校验) / archive(归档) / instructions(取 artifact 模板) / list(列出计划)"`
	Name     string `json:"name,omitempty" jsonschema:"description=计划名(kebab-case),new/status/validate/archive 需要"`
	Artifact string `json:"artifact,omitempty" jsonschema:"description=instructions 专用: proposal/specs/design/tasks"`
	JSON     bool   `json:"json,omitempty" jsonschema:"description=status/list 是否要 JSON 输出"`
}

// NewSpecTool creates the spec-management function tool over a Backend.
// The tool surface is backend-agnostic; swapping the plan format only changes
// the Backend injected here.
func NewSpecTool(backend Backend) trpctool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args specArgs) (Result, error) {
			req := Request{
				Op:       Op(args.Op),
				Name:     args.Name,
				Artifact: args.Artifact,
				JSON:     args.JSON,
			}
			if !validOps[req.Op] {
				return Result{}, fmt.Errorf("未知操作 %q。允许: init / new / status / validate / archive / instructions / list", args.Op)
			}
			return backend.Run(ctx, req)
		},
		function.WithName("spec"),
		function.WithDescription("规格化工作计划管理（无 shell，仅类型化操作）。op 取值：init=确保工作区就绪(幂等)；new=新建计划(需 name)；status=查询计划状态(可加 name/--json)；validate=严格校验(需 name)；archive=归档已完成计划(需 name)；instructions=取 artifact 模板(需 artifact: proposal/specs/design/tasks)；list=列出已有计划。计划文件的读写请改用 file 工具（已锁定在 openspec/ 目录）。本工具不含任何通用命令执行能力。"),
	)
}

// RegisterTool registers the spec tool as a built-in plain tool ("spec"),
// backed by the openspec CLI. Agents opt in via config (kind: tool, id: spec).
// Properties:
//   - bin: openspec binary name/path (default "openspec")
//   - work_dir: working directory containing openspec/ (default: process cwd)
func RegisterTool() {
	agent.RegisterPlainTool("spec", func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		var opts []OpenSpecOption
		if cfg.Properties != nil {
			if bin, ok := cfg.Properties["bin"].(string); ok {
				opts = append(opts, WithOpenSpecBin(bin))
			}
			if dir, ok := cfg.Properties["work_dir"].(string); ok {
				opts = append(opts, WithWorkDir(dir))
			}
		}
		ct, ok := NewSpecTool(NewOpenSpecBackend(opts...)).(trpctool.CallableTool)
		if !ok {
			return nil, fmt.Errorf("spec: tool does not implement CallableTool")
		}
		return ct, nil
	})
}

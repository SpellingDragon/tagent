// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
package spec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// defaultBin is the CLI name the backend runs when the deployment names none.
const defaultBin = "openspec"

// openspecBackend runs the openspec CLI: every operation maps onto one fixed
// subcommand, and the typed fields of a Request become discrete argument
// entries, so the model controls values and never the command structure.
type openspecBackend struct {
	bin string
	dir string
}

// OpenSpecOption configures one openspec backend.
type OpenSpecOption func(*openspecBackend)

// WithOpenSpecBin names the CLI binary or path; an empty name keeps the
// default.
func WithOpenSpecBin(bin string) OpenSpecOption {
	return func(b *openspecBackend) {
		if bin != "" {
			b.bin = bin
		}
	}
}

// WithWorkDir sets the directory the CLI runs in — the directory that holds the
// openspec workspace. An empty directory runs in the working directory of the process.
func WithWorkDir(dir string) OpenSpecOption {
	return func(b *openspecBackend) {
		if dir != "" {
			b.dir = dir
		}
	}
}

// NewOpenSpecBackend returns the backend over the openspec CLI.
func NewOpenSpecBackend(opts ...OpenSpecOption) Backend {
	b := &openspecBackend{bin: defaultBin}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Name reports the backend identity.
func (b *openspecBackend) Name() string { return "openspec" }

// Run maps one request onto an openspec invocation and answers with what the
// CLI reported. An operation the backend cannot dispatch is a named error; an
// operation the CLI ran and refused is a Result carrying its exit code, its
// output and the hint that names the next step.
func (b *openspecBackend) Run(ctx context.Context, req Request) (Result, error) {
	if !Valid(req.Op) {
		return Result{}, fmt.Errorf("%w: %q (allowed: %s)", ErrUnknownOp, req.Op, allowedOps)
	}
	argv, err := b.buildArgv(req)
	if err != nil {
		return Result{}, err
	}

	cmd := exec.CommandContext(ctx, b.bin, argv...)
	if b.dir != "" {
		cmd.Dir = b.dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := cmd.Run()
	res := Result{Op: req.Op, Output: strings.TrimSpace(out.String())}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	res.OK = res.ExitCode == 0 && runErr == nil

	if runErr != nil && cmd.ProcessState == nil {
		return res, fmt.Errorf("spec: cannot run %q: %w (is the openspec CLI installed? provision it in the deployment image or have the upper layer install it — the plan agent has no shell to self-install)", b.bin, runErr)
	}
	if !res.OK {
		res.Hint = hintFor(req.Op, res.Output)
	}
	return res, nil
}

// buildArgv assembles the discrete argument vector of one operation. Arguments
// are appended as separate entries and never interpolated into a shell string.
//
// archive carries no confirmation flag: the CLI refuses to close a plan whose
// tasks stay unchecked, and that refusal is the completion rule the plan agent
// is held to rather than a prompt to answer.
func (b *openspecBackend) buildArgv(req Request) ([]string, error) {
	switch req.Op {
	case OpInit:
		return []string{"init", "--tools", "none"}, nil
	case OpNew:
		if req.Name == "" {
			return nil, fmt.Errorf("spec: op %q requires name", req.Op)
		}
		return []string{"new", "change", req.Name}, nil
	case OpStatus:
		args := []string{"status"}
		if req.Name != "" {
			args = append(args, "--change", req.Name)
		}
		if req.JSON {
			args = append(args, "--json")
		}
		return args, nil
	case OpValidate:
		if req.Name == "" {
			return nil, fmt.Errorf("spec: op %q requires name", req.Op)
		}
		return []string{"validate", req.Name, "--strict"}, nil
	case OpArchive:
		if req.Name == "" {
			return nil, fmt.Errorf("spec: op %q requires name", req.Op)
		}
		return []string{"archive", req.Name}, nil
	case OpInstructions:
		if req.Artifact == "" {
			return nil, fmt.Errorf("spec: op %q requires artifact (proposal/specs/design/tasks)", req.Op)
		}
		args := []string{"instructions", req.Artifact}
		if req.Name != "" {
			args = append(args, "--change", req.Name)
		}
		return args, nil
	case OpList:
		args := []string{"list"}
		if req.JSON {
			args = append(args, "--json")
		}
		return args, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownOp, req.Op)
	}
}

// hintFor returns the actionable guidance of a failure the CLI reported, so the
// model corrects itself inside the closed set instead of guessing.
func hintFor(op Op, output string) string {
	low := strings.ToLower(output)
	switch {
	case strings.Contains(low, "already initialized"):
		return "workspace already initialized — this is safe to ignore; proceed with the next step"
	case strings.Contains(low, "already exists"):
		return "the plan already exists; use status to inspect it or pick a new name"
	case op == OpValidate && strings.Contains(low, "at least one delta"):
		return "无 specs deltas 的 A 级计划不应调用 validate（结构上必然失败）——改用 status(json=true) 确认 proposal 与 tasks 均为 done 即合规；仅 B 级（含 specs/design）以 validate 收尾"
	case op == OpArchive && strings.Contains(low, "incomplete task"):
		return "归档被拒：tasks.md 仍有未勾选项。先按证据逐项勾选再归档；若某步无法核实，向上级要证据，不要绕过这项检查"
	case op == OpInstructions && strings.Contains(low, "artifact"):
		return "instructions requires an artifact argument: proposal, specs, design, or tasks"
	case strings.Contains(low, "not found") || strings.Contains(low, "no such"):
		return "the plan was not found; run op=list to see existing plan names"
	default:
		return ""
	}
}

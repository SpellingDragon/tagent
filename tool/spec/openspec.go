package spec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// openspecBackend implements Backend on top of the openspec CLI
// (npm package @fission-ai/openspec). Every operation maps to a fixed
// `openspec <subcommand> [args…]` invocation; the model never controls the
// program or the argument structure, only the typed fields of Request.
type openspecBackend struct {
	bin string // CLI binary (default "openspec")
	dir string // working directory (must contain openspec/)
}

// OpenSpecOption configures an openspec backend.
type OpenSpecOption func(*openspecBackend)

// WithOpenSpecBin overrides the CLI binary name/path.
func WithOpenSpecBin(bin string) OpenSpecOption {
	return func(b *openspecBackend) {
		if bin != "" {
			b.bin = bin
		}
	}
}

// WithWorkDir sets the working directory for openspec invocations (the
// directory that contains openspec/). Defaults to the process cwd.
func WithWorkDir(dir string) OpenSpecOption {
	return func(b *openspecBackend) {
		if dir != "" {
			b.dir = dir
		}
	}
}

// NewOpenSpecBackend creates a Backend backed by the openspec CLI.
func NewOpenSpecBackend(opts ...OpenSpecOption) Backend {
	b := &openspecBackend{bin: "openspec"}
	for _, o := range opts {
		o(b)
	}
	return b
}

func (b *openspecBackend) Name() string { return "openspec" }

// Run maps a Request to an openspec CLI invocation and executes it.
func (b *openspecBackend) Run(ctx context.Context, req Request) (Result, error) {
	if !validOps[req.Op] {
		return Result{}, fmt.Errorf("spec: unknown op %q (allowed: init, new, status, validate, archive, instructions, list)", req.Op)
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
	cmd.Stderr = &out // merge so the model sees errors in context

	runErr := cmd.Run()
	res := Result{Op: req.Op, Output: strings.TrimSpace(out.String())}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	res.OK = res.ExitCode == 0 && runErr == nil

	// Exec-level failure (binary missing) is distinct from a non-zero exit.
	if runErr != nil && cmd.ProcessState == nil {
		return res, fmt.Errorf("spec: cannot run %q: %w (is the openspec CLI installed? provision it in the deployment image or have the upper layer install it — the plan agent has no shell to self-install)", b.bin, runErr)
	}
	if !res.OK {
		res.Hint = hintFor(req.Op, res.Output)
	}
	return res, nil
}

// buildArgv assembles the discrete argument vector for an op. Arguments are
// appended as separate entries — never interpolated into a shell string.
func (b *openspecBackend) buildArgv(req Request) ([]string, error) {
	switch req.Op {
	case OpInit:
		// --tools none: only create core dirs (tagent isn't in openspec's
		// supported tool list). Idempotent.
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
		return nil, fmt.Errorf("spec: unhandled op %q", req.Op)
	}
}

// hintFor returns actionable guidance for common failures, so the model can
// self-correct within its allowed operations instead of guessing.
func hintFor(op Op, output string) string {
	low := strings.ToLower(output)
	switch {
	case strings.Contains(low, "already initialized"):
		return "workspace already initialized — this is safe to ignore; proceed with the next step"
	case strings.Contains(low, "already exists"):
		return "the change already exists; use status to inspect it or pick a new name"
	case op == OpInstructions && strings.Contains(low, "artifact"):
		return "instructions requires an artifact argument: proposal, specs, design, or tasks"
	case strings.Contains(low, "not found") || strings.Contains(low, "no such"):
		return "the change was not found; run op=list to see existing change names"
	default:
		return ""
	}
}

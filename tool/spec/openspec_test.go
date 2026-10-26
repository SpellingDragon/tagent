package spec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// realOpenSpec resolves the real CLI or skips the test: the backend's contract
// is only witnessed against the program it dispatches to.
func realOpenSpec(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(defaultBin); err != nil {
		t.Skipf("the real openspec CLI is not on PATH: %v", err)
	}
}

// runOne drives one operation against dir and answers with the result.
func runOne(t *testing.T, dir string, req Request) Result {
	t.Helper()
	res, err := NewOpenSpecBackend(WithWorkDir(dir)).Run(context.Background(), req)
	if err != nil {
		t.Fatalf("op %q = %v, want the CLI result", req.Op, err)
	}
	return res
}

// TestBuildArgvKeepsEveryValueOneDiscreteEntry pins that every value stays one argv entry, so shell syntax cannot change the command the CLI runs.
// - every operation maps onto one fixed subcommand with the documented flags
// - a name or artifact carrying shell syntax remains exactly one entry
// - operations that require a name or an artifact refuse the empty form before dispatch
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestBuildArgvKeepsEveryValueOneDiscreteEntry(t *testing.T) {
	b := &openspecBackend{bin: defaultBin}
	cases := []struct {
		req  Request
		want []string
	}{
		{Request{Op: OpInit}, []string{"init", "--tools", "none"}},
		{Request{Op: OpNew, Name: "my-plan"}, []string{"new", "change", "my-plan"}},
		{Request{Op: OpNew, Name: "p; rm -rf /"}, []string{"new", "change", "p; rm -rf /"}},
		{Request{Op: OpStatus, Name: "p", JSON: true}, []string{"status", "--change", "p", "--json"}},
		{Request{Op: OpStatus}, []string{"status"}},
		{Request{Op: OpValidate, Name: "p"}, []string{"validate", "p", "--strict"}},
		{Request{Op: OpArchive, Name: "p"}, []string{"archive", "p"}},
		{Request{Op: OpInstructions, Artifact: "specs", Name: "p"}, []string{"instructions", "specs", "--change", "p"}},
		{Request{Op: OpList, JSON: true}, []string{"list", "--json"}},
	}
	for _, c := range cases {
		got, err := b.buildArgv(c.req)
		if err != nil {
			t.Fatalf("buildArgv(%+v): %v", c.req, err)
		}
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Errorf("buildArgv(%+v) = %q, want %q", c.req, got, c.want)
		}
	}
	for _, op := range []Op{OpNew, OpValidate, OpArchive} {
		if _, err := b.buildArgv(Request{Op: op}); err == nil {
			t.Errorf("op %q without name must be refused", op)
		}
	}
	if _, err := b.buildArgv(Request{Op: OpInstructions}); err == nil {
		t.Error("instructions without artifact must be refused")
	}
}

// TestRunRefusesAnOperationOutsideTheSet pins that the dispatch whitelist answers before any process starts.
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestRunRefusesAnOperationOutsideTheSet(t *testing.T) {
	_, err := NewOpenSpecBackend().Run(context.Background(), Request{Op: Op("rm -rf /")})
	if err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("operation outside the set = %v, want a refusal by name", err)
	}
}

// TestRunMissingBinaryIsActionable pins that a deployment beside no CLI fails with guidance instead of a panic.
// - the refusal is the named backend-unavailable error
// - the guidance names the provisioning step and masks the program, so the model reads no implementation name
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestRunMissingBinaryIsActionable(t *testing.T) {
	const missing = "definitely-not-a-real-binary-xyz"
	_, err := NewOpenSpecBackend(WithOpenSpecBin(missing)).Run(
		context.Background(), Request{Op: OpList})
	if err == nil || !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("missing binary = %v, want %v", err, ErrBackendUnavailable)
	}
	if !strings.Contains(err.Error(), "provision it in the deployment image") {
		t.Fatalf("missing binary = %v, want guidance toward provisioning", err)
	}
	if strings.Contains(err.Error(), missing) {
		t.Fatalf("missing binary = %v, want the program name masked out of the model-visible refusal", err)
	}
}

// TestBackendAnswersToItsPlanIdentity pins the semantic identity the port exposes.
// - the name says what the backend does, not what implements it
// - the workspace root is the storage root under the directory the CLI runs in
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestBackendAnswersToItsPlanIdentity(t *testing.T) {
	dir := t.TempDir()
	backend := NewOpenSpecBackend(WithWorkDir(dir))
	if got := backend.Name(); got != "plan" {
		t.Fatalf("backend name = %q, want the semantic plan identity", got)
	}
	if got, want := backend.WorkspaceRoot(), filepath.Join(dir, "openspec"); got != want {
		t.Fatalf("workspace root = %q, want %q", got, want)
	}
	if got := NewOpenSpecBackend().WorkspaceRoot(); got != workspaceDir {
		t.Fatalf("workspace root beside no work directory = %q, want %q", got, workspaceDir)
	}
}

// TestBackendKeepsTheModelFaceInvariant pins that wiring this backend adds nothing to the declaration.
// - the declaration equals the one a bare port stub yields: no backend identity reaches the model face
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestBackendKeepsTheModelFaceInvariant(t *testing.T) {
	stub := NewTool(&recordingBackend{}).Declaration()
	wired := NewTool(NewOpenSpecBackend()).Declaration()
	if !reflect.DeepEqual(stub, wired) {
		t.Fatalf("the declaration differs by backend: %+v vs %+v", stub, wired)
	}
}

// TestHintsSpeakThePlanVocabulary pins every hint the backend hands a failed call, so a correction steers the model inside the closed set.
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestHintsSpeakThePlanVocabulary(t *testing.T) {
	cases := []struct {
		op     Op
		output string
		want   string
	}{
		{OpInit, "Workspace already initialized", "workspace already initialized — this is safe to ignore; proceed with the next step"},
		{OpNew, "Error: change already exists", "the plan already exists; use status to inspect it or pick a new name"},
		{OpValidate, "must have at least one delta", "无 specs deltas 的 A 级计划不应调用 validate（结构上必然失败）——改用 status(json=true) 确认 proposal 与 tasks 均为 done 即合规；仅 B 级（含 specs/design）以 validate 收尾"},
		{OpArchive, "1 incomplete task(s) found", "归档被拒：tasks.md 仍有未勾选项。先按证据逐项勾选再归档；若某步无法核实，向上级要证据，不要绕过这项检查"},
		{OpInstructions, "missing required argument: artifact", "instructions requires an artifact argument: proposal, specs, design, or tasks"},
		{OpStatus, "change not found", "the plan was not found; run op=list to see existing plan names"},
		{OpList, "everything is fine", ""},
	}
	for _, c := range cases {
		if got := hintFor(c.op, c.output); got != c.want {
			t.Errorf("hintFor(%q, %q) = %q, want %q", c.op, c.output, got, c.want)
		}
	}
}

// TestOpenSpecBackendRunsTheWholePlanLifecycle drives the real CLI through the plan operations in a throwaway workspace.
// - init, new, status, instructions and list each report the CLI's own success
// - archive refuses a plan with an unchecked task and carries the completion hint
// - archive closes the plan once every task is checked, and the plan leaves the active set
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestOpenSpecBackendRunsTheWholePlanLifecycle(t *testing.T) {
	realOpenSpec(t)
	dir := t.TempDir()
	const name = "plan-lifecycle"

	if res := runOne(t, dir, Request{Op: OpInit}); !res.OK || res.ExitCode != 0 {
		t.Fatalf("init = %+v, want the CLI's success", res)
	}
	if res := runOne(t, dir, Request{Op: OpInit}); !res.OK {
		t.Fatalf("a repeated init = %+v, want the same success", res)
	}
	if res := runOne(t, dir, Request{Op: OpNew, Name: name}); !res.OK {
		t.Fatalf("new = %+v, want the CLI's success", res)
	}
	if res := runOne(t, dir, Request{Op: OpStatus, Name: name, JSON: true}); !res.OK || !strings.Contains(res.Output, `"changeName": "`+name+`"`) {
		t.Fatalf("status = %+v, want the change's machine-readable state", res)
	}
	if res := runOne(t, dir, Request{Op: OpInstructions, Artifact: "tasks", Name: name}); !res.OK || !strings.Contains(res.Output, `artifact id="tasks"`) {
		t.Fatalf("instructions = %+v, want the tasks artifact instructions", res)
	}
	if res := runOne(t, dir, Request{Op: OpList, JSON: true}); !res.OK || !strings.Contains(res.Output, name) {
		t.Fatalf("list = %+v, want the created plan", res)
	}

	tasks := filepath.Join(dir, "openspec", "changes", name, "tasks.md")
	if err := os.WriteFile(tasks, []byte("## 1. Tasks\n\n- [ ] 1.1 do it\n"), 0o644); err != nil {
		t.Fatalf("write tasks = %v", err)
	}
	res := runOne(t, dir, Request{Op: OpArchive, Name: name})
	if res.OK || res.ExitCode == 0 || !strings.Contains(res.Hint, "归档被拒") {
		t.Fatalf("archive over an unchecked task = %+v, want the CLI's refusal beside the completion hint", res)
	}

	if err := os.WriteFile(tasks, []byte("## 1. Tasks\n\n- [x] 1.1 done\n"), 0o644); err != nil {
		t.Fatalf("rewrite tasks = %v", err)
	}
	res = runOne(t, dir, Request{Op: OpArchive, Name: name})
	if !res.OK || !strings.Contains(res.Output, "archived as") {
		t.Fatalf("archive over checked tasks = %+v, want the CLI's success", res)
	}
	archived, err := filepath.Glob(filepath.Join(dir, "openspec", "changes", "archive", "*-"+name))
	if err != nil || len(archived) != 1 {
		t.Fatalf("archived plan = %v (%v), want exactly one dated directory", archived, err)
	}
}

// TestOpenSpecBackendValidateReportsTheStrictRule pins the strict refusal of a plan without deltas and its hint.
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestOpenSpecBackendValidateReportsTheStrictRule(t *testing.T) {
	realOpenSpec(t)
	dir := t.TempDir()
	const name = "plan-without-deltas"
	runOne(t, dir, Request{Op: OpInit})
	if res := runOne(t, dir, Request{Op: OpNew, Name: name}); !res.OK {
		t.Fatalf("new = %+v, want the CLI's success", res)
	}
	proposal := filepath.Join(dir, "openspec", "changes", name, "proposal.md")
	if err := os.WriteFile(proposal, []byte("# Proposal\n\nwhy\n"), 0o644); err != nil {
		t.Fatalf("write proposal = %v", err)
	}
	res := runOne(t, dir, Request{Op: OpValidate, Name: name})
	if res.OK || res.ExitCode == 0 {
		t.Fatalf("validate = %+v, want the strict refusal of a plan without deltas", res)
	}
	if !strings.Contains(res.Output, "at least one delta") {
		t.Fatalf("validate output = %q, want the CLI's delta rule", res.Output)
	}
	if !strings.Contains(res.Hint, "validate") {
		t.Fatalf("validate hint = %q, want guidance away from the structurally failing call", res.Hint)
	}
}

package tagent

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Dependency-layer assertions (implementation-hardening 7A.1; spec:
// architecture-guardrails / 唯一编排发布权与中性契约). Per the revised
// introduce-durable-workflow-engine (第二轮收敛 D10/D12), the organization
// orchestration is realized on the EXISTING YAML hierarchy by the root package
// alone: there is NO separate workflow/ graph-compiler package any more (the
// standalone workflow/v1 DSL prototype was withdrawn as un-wired scaffolding).
// The inner runtime keeps root → agent → plugin → memory (event a pure leaf);
// agent must never depend on any workflow/* package (guard against
// reintroducing a second organization representation or a durable engine), and
// the module must not grow a parallel orchestration compiler directory again.
// The Go compiler only rejects CYCLES — these tests reject LAYER VIOLATIONS
// (a reverse/upward edge, or a resurrected second representation).
//
// module prefix
const mod = "github.com/SpellingDragon/tagent"

// internalDeps returns the set of internal packages (module-scoped) that pkg
// transitively depends on. Uses `go list -deps` — slow-ish (a few seconds),
// acceptable for one CI job.
func internalDeps(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	deps := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == mod:
			deps["."] = true // the ROOT package itself (agent must not import it)
		case strings.HasPrefix(line, mod+"/"):
			deps[strings.TrimPrefix(line, mod+"/")] = true
		}
	}
	return deps
}

func assertNoDeps(t *testing.T, pkg string, deps map[string]bool, forbidden ...string) {
	t.Helper()
	if len(forbidden) == 0 {
		t.Fatalf("assertNoDeps(%s): empty forbidden set — the assertion is a no-op (self-check against silent guard rot)", pkg)
	}
	for _, f := range forbidden {
		if deps[f] {
			t.Errorf("layer violation: %s imports %s (revised direction, introduce-durable-workflow-engine: root composes {agent → plugin → memory → event}; there is NO workflow/graph layer — see TestArch_NoSecondOrchestrationRepresentation)", pkg, f)
		}
	}
}

// assertNoDepsPrefix is the family-level guard: it flags any internal dep at or
// under each prefix (agent, agent/task, ...; memory, memory/embedder, ...), so
// the assertion does not depend on enumerating every current subpackage — new
// subpackages under a forbidden family are caught automatically. The package's
// own subtree is exempt (so asserting "workflow" on a workflow/* package never
// self-hits).
func assertNoDepsPrefix(t *testing.T, pkg string, deps map[string]bool, prefixes ...string) {
	t.Helper()
	if len(prefixes) == 0 {
		t.Fatalf("assertNoDepsPrefix(%s): empty prefix set — the assertion is a no-op", pkg)
	}
	for dep := range deps {
		if dep == pkg || strings.HasPrefix(dep, pkg+"/") {
			continue // the package's own subtree
		}
		for _, p := range prefixes {
			if dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("layer violation: %s imports %s (forbidden family %q; agent must NOT depend on any workflow/*, workflow/* must NOT depend on agent/memory/plugin/tool/root)", pkg, dep, p)
				break
			}
		}
	}
}

func TestArch_LayeredDependencyDirection(t *testing.T) {
	if testing.Short() {
		t.Skip("go list -deps is not short-mode friendly")
	}

	// event: pure leaf — imports no internal package at all.
	assertNoDeps(t, mod+"/event", internalDeps(t, mod+"/event"),
		"agent", "plugin", "memory", "tool", "rl", "evolution")

	// memory (+engine/kv/embedder): may depend on event only.
	for _, pkg := range []string{mod + "/memory", mod + "/memory/kv", mod + "/memory/engine", mod + "/memory/embedder"} {
		deps := internalDeps(t, pkg)
		delete(deps, "event")
		assertNoDeps(t, pkg, deps, "agent", "plugin", "tool", "rl", "evolution")
	}

	// plugin: may depend on event + memory; not agent/root/tool.
	deps := internalDeps(t, mod+"/plugin")
	delete(deps, "event")
	delete(deps, "memory")
	assertNoDeps(t, mod+"/plugin", deps, "agent", "tool", "rl", "evolution")

	// agent (+task/compress/governance/reliability): the inner runtime. It must
	// NOT reach the root package (review P1-1: the empty-string forbidden made
	// this a no-op — root is collected as "."), and — per the revised
	// architecture-guardrails — it must NOT depend on ANY workflow/* package:
	// reception/completion/task/recovery stay inside the original protocols, and
	// the org version reaches them only through the minimal injection contract
	// owned by the root, never through an imported orchestration layer.
	for _, pkg := range []string{mod + "/agent", mod + "/agent/task", mod + "/agent/compress", mod + "/agent/governance", mod + "/agent/reliability"} {
		deps := internalDeps(t, pkg)
		delete(deps, "event")
		delete(deps, "memory")
		delete(deps, "plugin")
		assertNoDeps(t, pkg, deps, ".")
		assertNoDepsPrefix(t, pkg, deps, "workflow")
	}
}

// TestArch_NoSecondOrchestrationRepresentation guards the 第二轮收敛 outcome:
// the withdrawn standalone graph-compiler must not resurrect (neither as a
// package nor as an import anywhere in the module), and no gray dispatch keys
// may reappear. Organization orchestration is composed by the ROOT over the
// existing YAML hierarchy only (spec workflow-config-compilation「单一编排表示」).
func TestArch_NoSecondOrchestrationRepresentation(t *testing.T) {
	if _, err := os.Stat("workflow"); err == nil {
		t.Errorf("a top-level workflow/ package exists again: the standalone graph DSL was withdrawn (second-round scope) — orchestration must be composed by the root over the existing YAML hierarchy")
	}
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./...: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == mod+"/workflow" || strings.HasPrefix(line, mod+"/workflow/") {
			t.Errorf("module still depends on the withdrawn orchestration package %q", line)
		}
	}
}

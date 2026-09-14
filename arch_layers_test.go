package tagent

import (
	"os/exec"
	"strings"
	"testing"
)

// Dependency-layer assertions (implementation-hardening 7A.1; spec:
// architecture-guardrails / 分层依赖方向可机械断言). The declared direction is
// root → agent → plugin → memory, with event as a pure leaf. The Go compiler
// only rejects CYCLES — these tests reject LAYER VIOLATIONS (a lower layer
// reaching upward), which currently do not exist (verified 2026-09-14) and
// must never be introduced silently.
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
		if strings.HasPrefix(line, mod+"/") {
			deps[strings.TrimPrefix(line, mod+"/")] = true
		}
	}
	return deps
}

func assertNoDeps(t *testing.T, pkg string, deps map[string]bool, forbidden ...string) {
	t.Helper()
	for _, f := range forbidden {
		if deps[f] {
			t.Errorf("layer violation: %s imports %s (declared direction: root → agent → plugin → memory; event is a leaf)", pkg, f)
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
	delete(deps, "event"); delete(deps, "memory")
	assertNoDeps(t, mod+"/plugin", deps, "agent", "tool", "rl", "evolution")

	// agent (+task/compress/governance/reliability): may depend on event +
	// memory + plugin; not the root package.
	for _, pkg := range []string{mod + "/agent", mod + "/agent/task", mod + "/agent/compress", mod + "/agent/governance", mod + "/agent/reliability"} {
		deps := internalDeps(t, pkg)
		delete(deps, "event"); delete(deps, "memory"); delete(deps, "plugin")
		assertNoDeps(t, pkg, deps, "")
	}
}

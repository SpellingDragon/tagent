package tagent

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// mod is the module path prefix that the layering assertions below expand into
// concrete package paths. The Go compiler only rejects import cycles, so a reverse
// or upward edge between our own packages needs these mechanical assertions.
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
			deps["."] = true
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
			continue
		}
		for _, p := range prefixes {
			if dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("layer violation: %s imports %s (forbidden family %q; agent must NOT depend on any workflow/*, workflow/* must NOT depend on agent/memory/plugin/tool/root)", pkg, dep, p)
				break
			}
		}
	}
}

// TestArch_LayeredDependencyDirection asserts the layering mechanically: each package must not import the families its assertion lists.
// - The agent family is the inner runtime: it must not reach the root package nor import any workflow/* package.
// - Reception, completion, task and recovery stay inside the original protocols.
// - The org version reaches them only through the minimal injection contract owned by the root, never through an imported orchestration layer.
//
// 契约: docs/wiki/agent/agent-architecture.md#package-layout
func TestArch_LayeredDependencyDirection(t *testing.T) {
	if testing.Short() {
		t.Skip("go list -deps is not short-mode friendly")
	}

	assertNoDeps(t, mod+"/event", internalDeps(t, mod+"/event"),
		"agent", "plugin", "memory", "tool", "rl", "evolution")

	for _, pkg := range []string{mod + "/memory", mod + "/memory/kv", mod + "/memory/engine", mod + "/memory/embedder"} {
		deps := internalDeps(t, pkg)
		delete(deps, "event")
		assertNoDeps(t, pkg, deps, "agent", "plugin", "tool", "rl", "evolution")
	}

	deps := internalDeps(t, mod+"/plugin")
	delete(deps, "event")
	delete(deps, "memory")
	assertNoDeps(t, mod+"/plugin", deps, "agent", "tool", "rl", "evolution")

	assertNoDeps(t, mod+"/config", internalDeps(t, mod+"/config"), ".")
	assertNoDeps(t, mod+"/agent", internalDeps(t, mod+"/agent"), "config")

	for _, pkg := range []string{mod + "/agent", mod + "/agent/task", mod + "/agent/compress", mod + "/agent/governance", mod + "/agent/reliability"} {
		deps := internalDeps(t, pkg)
		delete(deps, "event")
		delete(deps, "memory")
		delete(deps, "plugin")
		assertNoDeps(t, pkg, deps, ".")
		assertNoDepsPrefix(t, pkg, deps, "workflow")
	}
}

// TestArch_NoSecondOrchestrationRepresentation guards that no second orchestration representation, graph-compiler package or gray dispatch key ever reappears.
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

// TestEliminationList_ZeroLegacySymbols statically checks that every mechanism on the elimination list stays absent from non-test Go files.
// - The list governs live code: a comment may record that a symbol is gone, so only non-comment lines are scanned.
// - The elimination list closes with two jointly-sufficient proofs: this static check and the three-boot-state dynamic test.
// - The static half catches deleted mechanisms left uncalled but present; the dynamic half proves prior-format data is read-not, consumed-not, wiped-not.
func TestEliminationList_ZeroLegacySymbols(t *testing.T) {
	banned := []struct{ pattern, why string }{
		{"task_stale_after", "10.5: stale observation wall deleted; TTL is the only age path"},
		{"task_job_deadline", "10.5: second age wall merged into task_default_ttl"},
		{"TaskMaxDetachedAge", "10.5: compat remapping removed, no aliases"},
		{"type SpillStore", "決策10: legacy spill overflow format removed (files are inert transitional data)"},
		{"func NewSpillStore", "決策10: same"},
		{"loopTerminated ", "6.1: replaced by the loopState machine (loopTerminatedNow helper)"},
		{"loopActive ", "6.1: same"},
		{"collectUnconfirmedReceipts", "5.7: old collect-chain replaced by direct envelope reconcile"},
		{"persistInboxReceipt(", "5.3: fresh-key receipt minting deleted (reserved-key commit only)"},
		{"ConfirmDurableByRequestID", "5.4: bare request-ID confirm removed (verified receipt credential only)"},
		{"ReconcileDurableReceipts", "5.7: harvest-style receipt reconcile removed (per-envelope fixed-key reconcile)"},
		{"PathForReceiptKey", "5.7: receipt-key→path reverse index removed (envelopes carry their own fixed key)"},
		{"ReceiptNote", "5.4: free-text receipt note replaced by reliability.ReceiptCredential"},
		{"ErrLegacySpillNotDrained", "3.7: legacy drain-as-boot-precondition removed (inert transitional data)"},
		{"checkUpgradeGates", "3.7: whole-tree upgrade gate removed (only current-format quarantine blocks reopen)"},
		{"ReadyCh", "3.2: zero-consumer cold-start readiness signal removed"},
	}
	// RECURSIVE over the whole repo (review 7677c07 #1: top-level-only scans
	// left agent/reliability etc. unguarded — the exact places old mechanisms
	// would resurrect). Dot-dirs, openspec docs and tests are excluded.
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "openspec") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, b := range banned {
		for _, f := range files {
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			for i, line := range strings.Split(string(src), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				require.NotContains(t, line, b.pattern,
					"§8.7 elimination list: %s must not appear in live code (%s) — found in %s:%d",
					b.pattern, b.why, f, i+1)
			}
		}
	}
}

const triPhaseEnv = "TAGENT_TRI_PHASE"

// TestLatestPathOnly_ThreeBootStates runs the dynamic half: three independent boots, each driving one real durable turn.
// - The three states (fresh, restart over the current state, post-reset) run as independent processes: a boot is only evidenced by a real process start.
// - Same-process multi-generation runner boot/Close churn is avoided: it triggers a framework-internal state-lifecycle race.
// - No production path performs that churn, so running each state in its own process costs no coverage.
// - Prior-format markers are planted only after the first child exited, so no live writer can consume them.
// - The parent orchestrates dirs, marker planting and the managed reset between the children.
func TestLatestPathOnly_ThreeBootStates(t *testing.T) {
	if phase := os.Getenv(triPhaseEnv); phase != "" {
		triChildPhase(t, phase)
		return
	}
	root := t.TempDir()
	env := append(os.Environ(),
		"TAGENT_TRI_STORE="+filepath.Join(root, "store"),
		"TAGENT_TRI_SPILL="+filepath.Join(root, "spill"),
		"TAGENT_TRI_ANCHOR="+filepath.Join(root, "anchor"))
	runChild := func(phase string) {
		runBootChild(t, env, triPhaseEnv+"="+phase, "TestLatestPathOnly_ThreeBootStates$")
	}

	runChild("1")

	spill := filepath.Join(root, "spill")
	legacyV1 := filepath.Join(spill, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyV1), 0o755))
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(spill, "tagent", "job.spill"), []byte("spill"), 0o644))

	runChild("2")
	require.Equal(t, `{"version":1}`, readFile(t, legacyV1), "legacy v1 item stays inert across a real restart")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "anchor"), 0o755))
	_, err := drillResetManagedUnits(filepath.Join(root, "store"), spill, filepath.Join(root, "anchor"), "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "the authorized reset cleared the legacy set")

	runChild("3")
}

// triRaceOnlyFramework CLASSIFIES a child failure whose EVERY DATA RACE block involves
// zero tagent frames - a trpc-agent-go lifecycle race family known from upstream.
//
// - Diagnostic label only: it never suppresses a child failure; runBootChild fails acceptance on ANY DATA RACE regardless of this verdict.
// - Deliberately conservative: one tagent frame anywhere, or any assertion or panic, yields false.
// - Every race block stack must be framework-internal; the two registered families are exempt as a family because product code holds zero references to the raced queues.
// - Only the accessor sections are scanned: the trailing created-at origin stacks inevitably name ancestor test frames and must not veto.
func triRaceOnlyFramework(out []byte) bool {
	text := string(out)
	if !strings.Contains(text, "DATA RACE") || !strings.Contains(text, "--- FAIL") {
		return false
	}
	steerFamily := []string{"internal/state/steer.(*Queue).Close", "cloneState"}
	sessionFamily := []string{"session.(*Session).Clone", "UpdateUserSession"}
	famOK := familyExemptionEnabled()
	matchesAll := func(block string, sig []string) bool {
		for _, fr := range sig {
			if !strings.Contains(block, fr) {
				return false
			}
		}
		return true
	}
	for _, block := range strings.Split(text, "WARNING: DATA RACE")[1:] {
		block, _, _ = strings.Cut(block, "==================")
		if famOK && (matchesAll(block, steerFamily) || matchesAll(block, sessionFamily)) {
			continue
		}
		if i := strings.Index(block, "created at:"); i >= 0 {
			block = block[:i]
		}
		if strings.Contains(block, "github.com/SpellingDragon/tagent") {
			return false
		}
	}
	inFail := false
	inOrigin := false
	for _, ln := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(ln, "--- FAIL"):
			inFail, inOrigin = true, false
			continue
		case ln == "FAIL", ln == "PASS", strings.HasPrefix(ln, "ok "), strings.HasPrefix(ln, "--- PASS"), strings.HasPrefix(ln, "=== "):
			inFail, inOrigin = false, false
			continue
		}
		if !inFail {
			continue
		}
		if strings.HasPrefix(ln, "panic:") || strings.HasPrefix(ln, "fatal error:") {
			return false
		}
		if t := strings.TrimSpace(ln); t == "" {
			continue
		}
		if strings.Contains(ln, "created at:") {
			inOrigin = true
			continue
		}
		if inOrigin && strings.HasPrefix(ln, "  ") {
			continue
		}
		if t := strings.TrimSpace(ln); !strings.Contains(t, "race detected during execution of test") {
			return false
		}
	}
	return true
}

// registeredFamilyVersion is the trpc-agent-go release the exempted framework
// race families (steer.Queue.Close × invocation-state clone; session.Clone) were
// verified against. The exemption is bound to it so a framework upgrade can
// never silently widen or mislabel the exempt set: bump go.mod and the family
// exemption lapses until the races are re-registered here. Both families are
// FIXED upstream in v1.11.2 (steer: #1926 queue-cancel signal and #2165/#2462
// clone elimination; session: UpdateUserSession's EventMu widened over
// UpdatedAt) — the registration stays at v1.10.0 deliberately, so on the
// upgraded link the exemption stays LAPSED and any reappearance fails as an
// unknown (new) race.
const registeredFamilyVersion = "v1.10.0"

// familyExemptionEnabled gates the registered-race-family exemption on the linked
// framework version. A var so the classifier self-test deterministically
// exercises the upgrade (mismatch) path.
var familyExemptionEnabled = func() bool {
	return trpcAgentVersion() == registeredFamilyVersion
}

// trpcAgentVersion reports the linked trpc-agent-go module version, preferring
// build info and falling back to the go.mod require (the deterministic pin) when
// the test binary carries no dependency list. "" only when neither is readable.
func trpcAgentVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "trpc.group/trpc-go/trpc-agent-go" {
				return d.Version
			}
		}
	}
	return trpcAgentVersionFromGoMod()
}

// trpcAgentVersionFromGoMod reads the exact base-module require line
// ("trpc.group/trpc-go/trpc-agent-go vX") from go.mod, ignoring submodule
// requires whose path extends the base (…/model/provider etc.).
func trpcAgentVersionFromGoMod() string {
	src, err := os.ReadFile("go.mod")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(src), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) == 2 && f[0] == "trpc.group/trpc-go/trpc-agent-go" {
			return f[1]
		}
	}
	return ""
}

// TestTriRaceOnlyFrameworkClassifier exercises the diagnostic label only; acceptance is decided by TestBootChildVerdictNoExemption.
// - The family exemption is version-bound: it lapses once the linked trpc-agent-go differs from the registered release.
// - The exemption predicate is stubbed here and restored via t.Cleanup, so a panicking assertion cannot leak the stub.
func TestTriRaceOnlyFrameworkClassifier(t *testing.T) {
	race := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX (0.1s)\n    testing.go:1: race detected during execution of test\nFAIL\n"
	require.True(t, triRaceOnlyFramework([]byte(race)), "pure framework race is classified as the upstream family")
	tagentFrame := strings.Replace(race, "trpc.group/x/runner.Close()", "github.com/SpellingDragon/tagent/agent.go:1 x()", 1)
	origin := strings.Replace(race, "FAIL\n", "Goroutine 1 (running) created at:\n  github.com/SpellingDragon/tagent/test.go:1 t()\nFAIL\n", 1)
	require.True(t, triRaceOnlyFramework([]byte(origin)), "created-at ancestor test frames do not change the framework-family classification")
	require.False(t, triRaceOnlyFramework([]byte(tagentFrame)), "an unknown race with a tagent frame is never classified as framework-only")
	fam := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/trpc-go/trpc-agent-go/internal/state/steer.(*Queue).Close()\n==================\nRead at 0x1:\n  trpc.group/x/agent.cloneStateReflectValue()\n  github.com/SpellingDragon/tagent/agent.executionGateModel.GenerateContentIter.func1()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n"
	require.True(t, triRaceOnlyFramework([]byte(fam)), "family signature + wrapper frame is classified as framework (when the family is registered)")
	assertion := strings.Replace(race, "testing.go:1: race detected during execution of test", "Error: Should be true", 1)
	require.False(t, triRaceOnlyFramework([]byte(assertion)), "an assertion failure is never exempt")
	require.False(t, triRaceOnlyFramework([]byte("--- FAIL: TestX\n    Error: boom\n")), "non-race failure not exempt")

	blankHide := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n\n    main_test.go:99: Error: want 1 got 2\nFAIL\n"
	require.False(t, triRaceOnlyFramework([]byte(blankHide)), "a failure after a blank line is never exempt")

	panicHide := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\npanic: runtime error: index out of range\n\tgx/y.go:1 +0x1\nexit status 2\n"
	require.False(t, triRaceOnlyFramework([]byte(panicHide)), "a panic is never exempt behind a race")

	familyWithTagent := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/trpc-go/trpc-agent-go/internal/state/steer.(*Queue).Close()\n  trpc.group/x/agent.cloneStateReflectValue()\n  github.com/SpellingDragon/tagent/agent.executionGateModel.GenerateContentIter.func1()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n"
	require.False(t, familyExemptionEnabled(), "linked trpc-agent-go must NOT equal the v1.10.0 registration after the upgrade: both families are fixed upstream, so the classifier's family branch is lapsed and a reappearance is labeled unknown")
	orig := familyExemptionEnabled
	t.Cleanup(func() { familyExemptionEnabled = orig })
	familyExemptionEnabled = func() bool { return true }
	require.True(t, triRaceOnlyFramework([]byte(familyWithTagent)), "at the registered version the classifier recognizes the family + wrapper frame")
	familyExemptionEnabled = func() bool { return false }
	require.False(t, triRaceOnlyFramework([]byte(familyWithTagent)), "when the version no longer matches, the classifier stops recognizing the family + wrapper frame")
}

// TestBootChildVerdictNoExemption asserts the boot-child verdict rejects every data race shape, including the pure-upstream family.
func TestBootChildVerdictNoExemption(t *testing.T) {
	exit := errors.New("exit status 1")
	frameworkRace := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX (0.1s)\n    testing.go:1: race detected during execution of test\nFAIL\n"
	require.True(t, triRaceOnlyFramework([]byte(frameworkRace)), "classifier labels the pure-upstream family")
	ok, _ := childOutcome([]byte(frameworkRace), exit)
	require.False(t, ok, "§6.6: a pure-upstream race must FAIL acceptance (the old exempt path returned true)")

	originRace := strings.Replace(frameworkRace, "FAIL\n", "Goroutine 1 (running) created at:\n  github.com/SpellingDragon/tagent/test.go:1 t()\nFAIL\n", 1)
	ok, _ = childOutcome([]byte(originRace), exit)
	require.False(t, ok, "created-at ancestor tagent frames do not rescue a race from failing")

	tagentRace := strings.Replace(frameworkRace, "trpc.group/x/runner.Close()", "github.com/SpellingDragon/tagent/agent.go:1 x()", 1)
	ok, _ = childOutcome([]byte(tagentRace), exit)
	require.False(t, ok, "a race with a tagent accessor frame fails")

	assertionOnly := "--- FAIL: TestX\n    main_test.go:9: Error: want 1 got 2\nFAIL\n"
	ok, _ = childOutcome([]byte(assertionOnly), exit)
	require.False(t, ok, "a non-race child failure still fails")

	clean := "=== RUN   TestX\n--- PASS: TestX (0.00s)\nPASS\nok  \tgithub.com/SpellingDragon/tagent\t0.01s\n"
	ok, note := childOutcome([]byte(clean), nil)
	require.True(t, ok, "a clean child passes: "+note)
}

// childOutcome decides a boot child's acceptance: NO data race — pure-upstream
// family included — may pass; every DATA RACE fails. triRaceOnlyFramework is
// consulted ONLY to label a sighting for triage; it does not change the verdict.
// Non-race non-zero exits also fail.
func childOutcome(out []byte, err error) (ok bool, note string) {
	if bytes.Contains(out, []byte("DATA RACE")) {
		if triRaceOnlyFramework(out) {
			return false, "known upstream framework race family (diagnostic label only; §6.6: any race still fails acceptance)"
		}
		return false, "unclassified data race"
	}
	if err != nil {
		return false, "child exited non-zero without a race"
	}
	return true, ""
}

// runBootChild executes a boot child process and requires a CLEAN run: zero data
// races and a zero exit. The acceptance path keeps NO race exemption — a race of
// any stack shape fails with the full log. v1.11.2 fixed the steer/session
// families upstream, and since producer-done is not a stream-close, tagent must
// not paper over lifecycle races either.
func runBootChild(t *testing.T, baseEnv []string, kv string, testFilter string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", testFilter, "-test.timeout", "120s")
	cmd.Env = append(baseEnv, kv)
	out, err := cmd.CombinedOutput()
	require.NotContainsf(t, string(out), "no tests to run",
		"boot child filter %q matched no test — the gate would pass vacuously:\n%s", testFilter, out)
	ok, note := childOutcome(out, err)
	require.Truef(t, ok, "boot child must pass with ZERO races (§6.6: no exemption) — %s:\n%s", note, out)
}

// triChildPhase runs one boot state inside its own process.
//
// driveTurn is local: it starts the loop, injects and waits for the model sighting, and
// returns a wait that drains the output reader — the caller must Close the agent before
// calling it. The child finishes by returning, so the test binary exits normally.
func triChildPhase(t *testing.T, phase string) {
	storeDir := os.Getenv("TAGENT_TRI_STORE")
	spillDir := os.Getenv("TAGENT_TRI_SPILL")
	anchorDir := os.Getenv("TAGENT_TRI_ANCHOR")
	boot := func(m *drillModel) *agent.TagentAgent {
		ta, err := New(Config{
			Entry: "tagent",
			Agents: map[string]AgentConfig{"tagent": {
				SystemPrompt: PromptConfig{Inline: "three-state"},
				MaxTokens:    4000,
				Memory:       MemoryConfig{Type: "localfile", Path: storeDir},
			}},
			Reliability: ReliabilityConfig{BusSpillDir: spillDir, MeditationAnchorDir: anchorDir},
		}, WithModel(m))
		require.NoError(t, err)
		return ta
	}
	driveTurn := func(ta *agent.TagentAgent, m *drillModel, tag string) func() {
		out, err := ta.StartLoop("u", "tri-session")
		require.NoError(t, err)
		drop := make(chan struct{})
		go func() {
			defer close(drop)
			for range out {
			}
		}()
		_, err = ta.InjectMessageContext(context.Background(), "user", model.NewUserMessage(tag))
		require.NoError(t, err)
		require.Eventually(t, func() bool { return drillSaw(m, tag) }, 30*time.Second, 20*time.Millisecond,
			"boot must run the CURRENT path to the model (tag %s)", tag)
		return func() { <-drop }
	}
	switch phase {
	case "1":
		m := &drillModel{}
		ta := boot(m)
		wait := driveTurn(ta, m, "tri-fresh-turn")
		require.NoError(t, ta.Close())
		wait()
	case "2":
		m := &drillModel{}
		ta := boot(m)
		wait := driveTurn(ta, m, "tri-restart-turn")
		require.True(t, drillSaw(m, "tri-fresh-turn"), "current-state facts ARE recovered into the projection")
		require.NoError(t, ta.Close())
		wait()
		legacy := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
		require.Equal(t, `{"version":1}`, readFile(t, legacy), "legacy v1 item is inert: never read, rewritten or removed")
	case "3":
		m := &drillModel{}
		ta := boot(m)
		s, err := ta.ReconcileOutstanding()
		require.NoError(t, err)
		require.Equal(t, agent.ReconcileSummary{}, s, "reset-then-boot recovers nothing (authorized discard, no half-state)")
		wait := driveTurn(ta, m, "tri-postreset-turn")
		require.False(t, drillSaw(m, "tri-fresh-turn") || drillSaw(m, "tri-restart-turn"),
			"post-reset projection carries NO pre-reset input — clearing old data is never passed off as processed")
		require.NoError(t, ta.Close())
		wait()
	default:
		t.Fatalf("unknown phase %q", phase)
	}
	os.Exit(0)
}

func drillSaw(m *drillModel, sub string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, req := range m.reqs {
		for _, msg := range req {
			if strings.Contains(msg.Content, sub) {
				return true
			}
		}
	}
	return false
}

func readFile(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}

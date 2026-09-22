package tagent

// §8.7 — the elimination list closes HERE with two jointly-sufficient proofs
// (design 決策10 L189): (a) a STATIC dependency check that deleted legacy
// mechanisms stay deleted (no "uncalled but present" residue), and (b) the
// THREE BOOT STATES — brand-new dir, restart-over-current-state, boot after
// the managed reset — each running one real turn on the CURRENT path, with
// legacy markers planted beside the data proving they are read-NOT,
// consumed-not, and wiped-not.

import (
	"context"
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

// TestEliminationList_ZeroLegacySymbols — static check over every NON-TEST
// Go file: each pattern was a live mechanism before this change and its
// removal was ordered by 決策10's list (old parsing/aliases/confirm APIs,
// compatibility-only wrappers, weak fallbacks, duplicate owners, WAL/fsync
// claims of the minimized localfile backend).
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
		// §8.7 / resident-review-fixes 5.3: the request-ID/weak-fallback confirm +
		// receipt APIs and the legacy-spill drain gate are gone (reserved-key
		// credential + direct-inventory reconcile replaced them); ReadyCh was a
		// zero-consumer readiness signal (3.2). Locked against resurrection.
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
			// Comments legitimately RECORD the deletion ("the old
			// task_stale_after is gone"); the elimination list governs LIVE
			// code, so scan only non-comment lines.
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

// TestLatestPathOnly_ThreeBootStates runs the dynamic half: fresh boot,
// current-state restart, and post-reset boot — one real durable turn each,
// with legacy markers that must remain untouched inert.
// The three boot states run as THREE INDEPENDENT PROCESSES (xproc pattern,
// §8 discipline: a boot is only evidenced by a real process start, and
// same-process multi-generation runner boot/Close churn happens to trigger a
// framework-internal state-lifecycle race that no production path performs).
// Parent orchestrates dirs, marker planting and the managed reset in between.

const triPhaseEnv = "TAGENT_TRI_PHASE"

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
		runRaceExemptChild(t, env, triPhaseEnv+"="+phase, "TestLatestPathOnly_ThreeBootStates$")
	}

	// STATE 1 — brand-new dirs (child process): boot + one real durable turn.
	runChild("1")

	// Plant LEGACY markers while the writer is gone.
	spill := filepath.Join(root, "spill")
	legacyV1 := filepath.Join(spill, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyV1), 0o755))
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(spill, "tagent", "job.spill"), []byte("spill"), 0o644))

	// STATE 2 — independent process restarting over the CURRENT state with
	// legacy markers alongside (child asserts: prior fact in projection,
	// markers byte-identical).
	runChild("2")
	require.Equal(t, `{"version":1}`, readFile(t, legacyV1), "legacy v1 item stays inert across a real restart")

	// Managed unit reset between processes (parent side, §8.6 orchestration).
	require.NoError(t, os.MkdirAll(filepath.Join(root, "anchor"), 0o755))
	_, err := drillResetManagedUnits(filepath.Join(root, "store"), spill, filepath.Join(root, "anchor"), "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "the authorized reset cleared the legacy set")

	// STATE 3 — independent process booting post-reset (child asserts:
	// reconcile empty, new turn works, NO pre-reset input resurfaces).
	runChild("3")
}

// triRaceOnlyFramework reports a child failure whose EVERY DATA RACE block
// involves zero tagent frames — the pre-existing trpc-agent-go lifecycle
// family. Deliberately conservative: one tagent frame anywhere vetoes the
// exemption and the run fails with the full log.
func triRaceOnlyFramework(out []byte) bool {
	text := string(out)
	if !strings.Contains(text, "DATA RACE") || !strings.Contains(text, "--- FAIL") {
		return false
	}
	// (1) every race block's stacks must be framework-internal...
	// Registered framework-internal race signatures (evidence §8.1/§8.8/§8.9;
	// product code holds ZERO references to steer or invocation state queues
	// — grep-verified — so a wrapper frame merely riding the model-call chain
	// of a matched block cannot own the raced object):
	//   steerFamily: v1.10.0 steer.(*Queue).Close × invocation-state clone
	//   sessionFamily: Session.Clone snapshot read × session write
	steerFamily := []string{"internal/state/steer.(*Queue).Close", "cloneState"}
	sessionFamily := []string{"session.(*Session).Clone", "UpdateUserSession"}
	// The family exemption is version-bound (resident-review-fixes 2.3): it
	// applies ONLY while the linked trpc-agent-go equals the registered
	// version. After a framework upgrade the exemption lapses and these
	// families must be re-verified — a tagent accessor frame riding a
	// previously-exempted block is then treated like any unknown race.
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
			continue // registered framework-owned object, exempt as a family
		}
		// Anything else: only the ACCESSOR sections may not carry a tagent
		// frame (the trailing "created at:" origin stacks inevitably do).
		if i := strings.Index(block, "created at:"); i >= 0 {
			block = block[:i]
		}
		if strings.Contains(block, "github.com/SpellingDragon/tagent") {
			return false
		}
	}
	// (2) ...AND every FAIL detail block must contain NOTHING but the
	// race-detector verdict (and the race report's own origin-stack lines) — a
	// real assertion failure or panic must never hide behind a benign race in
	// the same child log. Non-indented lines no longer close the block: only a
	// top-level boundary does, so a failure printed after a blank line (or a
	// column-0 "panic:") is not skipped (resident-review-fixes 2.3(a)).
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
			return false // a crash is never exempt
		}
		if t := strings.TrimSpace(ln); t == "" {
			continue // a blank line does NOT close the block (fixes the 穿透)
		}
		if strings.Contains(ln, "created at:") {
			inOrigin = true // the race report's goroutine origin follows
			continue
		}
		if inOrigin && strings.HasPrefix(ln, "  ") {
			continue // an origin-stack frame — ancestor test frames do not veto
		}
		if t := strings.TrimSpace(ln); !strings.Contains(t, "race detected during execution of test") {
			return false
		}
	}
	return true
}

// registeredFamilyVersion is the trpc-agent-go release the exempted framework
// race families (steer.Queue.Close × invocation-state clone; session.Clone) were
// verified against (evidence §8.1/§8.8/§8.9). The exemption is bound to it so a
// framework upgrade can never silently widen or mislabel the exempt set: bump
// go.mod and the family exemption lapses until the races are re-registered here.
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

func TestTriRaceOnlyFrameworkClassifier(t *testing.T) {
	race := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX (0.1s)\n    testing.go:1: race detected during execution of test\nFAIL\n"
	require.True(t, triRaceOnlyFramework([]byte(race)), "pure framework race is exempt")
	tagentFrame := strings.Replace(race, "trpc.group/x/runner.Close()", "github.com/SpellingDragon/tagent/agent.go:1 x()", 1)
	origin := strings.Replace(race, "FAIL\n", "Goroutine 1 (running) created at:\n  github.com/SpellingDragon/tagent/test.go:1 t()\nFAIL\n", 1)
	require.True(t, triRaceOnlyFramework([]byte(origin)), "created-at ancestor test frames do not veto the framework family")
	require.False(t, triRaceOnlyFramework([]byte(tagentFrame)), "an unknown race with a tagent frame is never exempt")
	// The steer family IS exempt even when our model wrapper rides the call
	// chain — the raced object is framework-private and product-free.
	fam := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/trpc-go/trpc-agent-go/internal/state/steer.(*Queue).Close()\n==================\nRead at 0x1:\n  trpc.group/x/agent.cloneStateReflectValue()\n  github.com/SpellingDragon/tagent/agent.executionGateModel.GenerateContentIter.func1()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n"
	require.True(t, triRaceOnlyFramework([]byte(fam)), "family signature + wrapper frame stays exempt")
	assertion := strings.Replace(race, "testing.go:1: race detected during execution of test", "Error: Should be true", 1)
	require.False(t, triRaceOnlyFramework([]byte(assertion)), "an assertion failure is never exempt")
	require.False(t, triRaceOnlyFramework([]byte("--- FAIL: TestX\n    Error: boom\n")), "non-race failure not exempt")

	// (2.3a) blank-line 穿透: a real assertion failure printed after a blank
	// line must NOT hide behind the benign race. fail-before (old code reset the
	// block on the non-indented blank line, skipping this failure → exempt/true).
	blankHide := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n\n    main_test.go:99: Error: want 1 got 2\nFAIL\n"
	require.False(t, triRaceOnlyFramework([]byte(blankHide)), "a failure after a blank line is never exempt")

	// (2.3a) panic 逃逸: a column-0 panic after the race verdict must veto the
	// exemption. fail-before (old code reset the block on the non-indented
	// panic line, never inspecting it → exempt/true).
	panicHide := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/x/runner.Close()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\npanic: runtime error: index out of range\n\tgx/y.go:1 +0x1\nexit status 2\n"
	require.False(t, triRaceOnlyFramework([]byte(panicHide)), "a panic is never exempt behind a race")

	// (2.3b) version binding: a tagent wrapper frame riding a REGISTERED family
	// block is exempt only while the linked version matches the registration.
	familyWithTagent := "WARNING: DATA RACE\nWrite at 0x1:\n  trpc.group/trpc-go/trpc-agent-go/internal/state/steer.(*Queue).Close()\n  trpc.group/x/agent.cloneStateReflectValue()\n  github.com/SpellingDragon/tagent/agent.executionGateModel.GenerateContentIter.func1()\n==================\n--- FAIL: TestX\n    testing.go:1: race detected during execution of test\n"
	require.True(t, familyExemptionEnabled(), "linked trpc-agent-go must equal the registered family version for the exemption to hold")
	require.True(t, triRaceOnlyFramework([]byte(familyWithTagent)), "at the registered version the family exemption covers the wrapper frame")
	orig := familyExemptionEnabled
	familyExemptionEnabled = func() bool { return false } // simulate a framework upgrade
	defer func() { familyExemptionEnabled = orig }()
	require.False(t, triRaceOnlyFramework([]byte(familyWithTagent)), "when the version no longer matches, the family exemption lapses and the wrapper frame vetoes")
}

// runRaceExemptChild executes a boot child process. The ONLY tolerated child
// failure is the registered pre-existing framework race family (v1.10.0
// steer.Queue.Close × invocation-state clone, evidence §8.1/§8.8/§8.9) whose
// accessor stacks are framework-internal; any assertion failure, panic or
// race with a tagent accessor frame fails hard with the full log.
func runRaceExemptChild(t *testing.T, baseEnv []string, kv string, testFilter string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", testFilter, "-test.timeout", "120s")
	cmd.Env = append(baseEnv, kv)
	out, err := cmd.CombinedOutput()
	if err != nil && triRaceOnlyFramework(out) {
		t.Log("registered framework-internal race family sighted in child (exempt per evidence)")
		return
	}
	require.NoErrorf(t, err, "child failed:\n%s", out)
	require.NotContains(t, string(out), "DATA RACE", "child hit an unregistered data race:\n%s", out)
}

// triChildPhase runs one boot state inside its own process.
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
	// driveTurn starts the loop, injects and waits for the model sighting;
	// the returned wait drains the output reader after the caller Closes.
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
	case "1": // brand-new dirs
		m := &drillModel{}
		ta := boot(m)
		wait := driveTurn(ta, m, "tri-fresh-turn")
		require.NoError(t, ta.Close())
		wait()
	case "2": // restart over the current state, legacy markers planted outside
		m := &drillModel{}
		ta := boot(m)
		wait := driveTurn(ta, m, "tri-restart-turn")
		require.True(t, drillSaw(m, "tri-fresh-turn"), "current-state facts ARE recovered into the projection")
		require.NoError(t, ta.Close())
		wait()
		legacy := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
		require.Equal(t, `{"version":1}`, readFile(t, legacy), "legacy v1 item is inert: never read, rewritten or removed")
	case "3": // boot after the managed unit reset
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
	// Child processes finish by exiting the test binary normally.
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

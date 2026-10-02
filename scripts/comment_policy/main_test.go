package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// chdirToRepoRoot makes repository-relative index targets resolvable, which is how
// the gate is invoked in CI and from the repository root.
func chdirToRepoRoot(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir("../.."))
	t.Cleanup(func() { require.NoError(t, os.Chdir(wd)) })
}

// rulesIn returns the rule names reported for a fixture.
func rulesIn(t *testing.T, path string) map[string]int {
	t.Helper()
	findings, err := checkFile(path)
	require.NoError(t, err)
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Rule]++
	}
	return counts
}

// TestCleanFixtureIsNotFlagged guards the false-positive side: a compliant file must produce nothing, otherwise batches will drown in exemptions.
func TestCleanFixtureIsNotFlagged(t *testing.T) {
	chdirToRepoRoot(t)
	require.Empty(t, rulesIn(t, "scripts/comment_policy/testdata/clean.go"))
}

// TestEachRuleFires proves every documented rule actually catches its planted case, so a passing gate means compliance rather than a broken matcher.
func TestEachRuleFires(t *testing.T) {
	chdirToRepoRoot(t)
	got := rulesIn(t, "scripts/comment_policy/testdata/violations.go")
	for _, rule := range []string{
		"audit-marker", "rationale", "mechanism-narrative",
		"process-artifact-ref", "unindexed-path-ref", "missing-symbol-doc", "free-standing",
	} {
		require.GreaterOrEqualf(t, got[rule], 1, "rule %s did not fire: %v", rule, got)
	}
}

// TestIndexRulesAcceptLiveTargetAndRejectDeadOrProcessPaths pins both directions of the documentation-index rule.
func TestIndexRulesAcceptLiveTargetAndRejectDeadOrProcessPaths(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()
	for name, body := range map[string]string{
		"live.go":      "// Package p holds one index.\npackage p\n\n// P is documented.\n//\n// 契约: docs/wiki/README.md\nfunc P() {}\n",
		"dead.go":      "// Package p holds one dead index.\npackage p\n\n// P is documented.\n//\n// 契约: docs/wiki/nope-missing.md\nfunc P() {}\n",
		"process.go":   "// Package p points at process artifacts.\npackage p\n\n// P refers to openspec/changes/foo/tasks.md.\nfunc P() {}\n",
		"outside.go":   "// Package p indexes outside the allowed roots.\npackage p\n\n// P is documented.\n//\n// 契约: README.md\nfunc P() {}\n",
		"specdir.go":   "// Package p indexes a requirements file, which is not a legal index target.\npackage p\n\n// P is documented.\n//\n// 契约: openspec/specs/code-documentation/spec.md\nfunc P() {}\n",
		"directive.go": "// Package p is exempt.\npackage p\n\n//go:build ignore\n\n// P is documented.\nfunc P() {}\n",
		"todo.go":      "// Package p carries a todo.\npackage p\n\n// P is documented.\n//\n// TODO(maintainer): widen the unit test for negative size.\nfunc P() {}\n",
		"testfile.go":  "// Package p is a test-like file.\npackage p\n\n// P is documented.\nfunc P() {}\n",
	} {
		path := dir + "/" + name
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	cases := []struct {
		file     string
		wantRule string
		wantNone string
	}{
		{file: "live.go", wantNone: "index-target-missing"},
		{file: "dead.go", wantRule: "index-target-missing"},
		{file: "process.go", wantRule: "process-artifact-ref"},
		{file: "outside.go", wantRule: "index-root"},
		{file: "specdir.go", wantRule: "index-root"},
		{file: "directive.go", wantNone: "free-standing"},
		{file: "todo.go", wantNone: "free-standing"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got := rulesIn(t, dir+"/"+tc.file)
			if tc.wantRule != "" {
				require.GreaterOrEqualf(t, got[tc.wantRule], 1, "expected %s in %v", tc.wantRule, got)
			}
			if tc.wantNone != "" {
				require.Zerof(t, got[tc.wantNone], "unexpected %s in %v", tc.wantNone, got)
			}
		})
	}
}

// TestTestFileMustDeclareResponsibility pins the layout rule a consolidation batch relies on: a test file names the production responsibility it covers.
func TestTestFileMustDeclareResponsibility(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()
	bare := dir + "/bare_test.go"
	require.NoError(t, os.WriteFile(bare, []byte("// Package p tests things.\npackage p\n\nimport \"testing\"\n\n// TestA verifies one contract.\nfunc TestA(t *testing.T) {}\n"), 0o644))
	require.GreaterOrEqual(t, rulesIn(t, bare)["missing-test-responsibility"], 1)

	indexed := dir + "/declared_test.go"
	require.NoError(t, os.WriteFile(indexed, []byte("// Package p tests things.\npackage p\n\nimport \"testing\"\n\n// TestA verifies one contract.\n//\n// 契约: docs/wiki/README.md\nfunc TestA(t *testing.T) {}\n"), 0o644))
	require.Equal(t, 0, rulesIn(t, indexed)["missing-test-responsibility"])
}

// TestRatchetBlocksIncreasesAndAllowsKnownCounts pins the direction of the ratchet.
// - An increase is always a failure, a known level passes, and a decrease is reported so the baseline can be lowered rather than silently drifting.
func TestRatchetBlocksIncreasesAndAllowsKnownCounts(t *testing.T) {
	base := map[string]int{"free-standing": 2}

	reg, imp := compareRatchet(map[string]int{"free-standing": 2}, base)
	require.Zero(t, reg, "matching the recorded level must pass")
	require.Zero(t, imp)

	reg, imp = compareRatchet(map[string]int{"free-standing": 3}, base)
	require.Equal(t, 1, reg, "one new violation must be a regression")
	require.Zero(t, imp)

	reg, imp = compareRatchet(map[string]int{"free-standing": 1}, base)
	require.Zero(t, reg)
	require.Equal(t, 1, imp, "a lowered slot must be visible so the baseline can follow")

	reg, _ = compareRatchet(map[string]int{"rationale": 1}, base)
	require.Equal(t, 1, reg, "an unrecorded rule must count from zero")
}

// TestBaselineRoundTrip guards the hand-rolled writer: unreadable JSON would turn every CI run into a hard error or, worse, an empty baseline.
func TestBaselineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	counts := map[string]int{
		"free-standing":      7,
		"missing-symbol-doc": 1,
		"unindexed-path-ref": 12,
	}
	dirs := []string{".", "examples/wechat-bot"}
	require.NoError(t, writeBaseline(path, counts, dirs, false))
	got, err := readBaseline(path)
	require.NoError(t, err)
	require.Equal(t, counts, got.Counts)
	require.Equal(t, dirs, got.Dirs)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "A batch may only LOWER")
}

func TestPackageDocIsPerPackageNotPerFile(t *testing.T) {
	dir := t.TempDir()
	withDoc := filepath.Join(dir, "doc.go")
	noDoc := filepath.Join(dir, "other.go")
	require.NoError(t, os.WriteFile(withDoc, []byte("// Package p does a thing.\npackage p\n"), 0o600))
	require.NoError(t, os.WriteFile(noDoc, []byte("package p\n\nfunc F() {}\n"), 0o600))

	fs2, err := checkFile(noDoc)
	require.NoError(t, err)
	require.Equal(t, 1, countRule(fs2, "missing-package-doc"), "the file itself has no package doc")

	kept := dropRedundantPackageDocs(append(mustCheck(t, withDoc), fs2...))
	require.Zero(t, countRule(kept, "missing-package-doc"),
		"one package doc in the package covers every file of that package")
}

func mustCheck(t *testing.T, path string) []finding {
	t.Helper()
	fs2, err := checkFile(path)
	require.NoError(t, err)
	return fs2
}

func countRule(fs2 []finding, rule string) int {
	n := 0
	for _, f := range fs2 {
		if f.Rule == rule {
			n++
		}
	}
	return n
}

// TestDocPathRefIgnoresRuntimeAssetNames pins the boundary of the path-reference rule: a prompt asset file named in prose is data, not a citation.
// - Only path-shaped documentation references must use the index form; runtime asset names inside a sentence stay free.
func TestDocPathRefIgnoresRuntimeAssetNames(t *testing.T) {
	require.False(t, docPathRef.MatchString(`// Prompt loading: overrides PromptDir + "recall_agent.md" if set`),
		"runtime asset names must not be treated as doc citations")
	require.True(t, docPathRef.MatchString(`// see docs/wiki/tool/tool-architecture.md for details`),
		"path-shaped documentation references must still be caught")
}

// TestDeclaredNameIsNotItselfResidue pins the precision of the content rules: a declared identifier may legitimately contain a residue word.
// - Quoting that identifier at the head of its own doc comment must not be reported.
// - Residue narration in the prose around it still is: the rule targets narration, not which characters appear inside a Go identifier.
func TestDeclaredNameIsNotItselfResidue(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()

	named := dir + "/named_test.go"
	require.NoError(t, os.WriteFile(named, []byte(
		"package p\n\nimport \"testing\"\n\n"+
			"// TestQueryEvents_LegacyWideSegmentNeverPruned 钉住 压实后的宽窗口段不被剪枝。\n//\n"+
			"// 契约: docs/wiki/README.md\n"+
			"func TestQueryEvents_LegacyWideSegmentNeverPruned(t *testing.T) {}\n"), 0o644))
	require.Equal(t, 0, rulesIn(t, named)["audit-marker"],
		"the declared identifier's own name must not count as change residue")

	prose := dir + "/prose_test.go"
	require.NoError(t, os.WriteFile(prose, []byte(
		"package p\n\nimport \"testing\"\n\n"+
			"// TestEnvelope 钉住 legacy segments lack an envelope。\nfunc TestEnvelope(t *testing.T) {}\n"), 0o644))
	require.GreaterOrEqual(t, rulesIn(t, prose)["audit-marker"], 1,
		"residue in prose must still be reported")
}

// TestUsedToRequiresWordBoundaries pins that residue words match as whole words, not as substrings.
// - "refused to adopt" and "still-used tool closers" contain the phrase by accident and must not be reported, while an unambiguous residue word still is.
func TestUsedToRequiresWordBoundaries(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()

	notResidue := dir + "/boundaries.go"
	require.NoError(t, os.WriteFile(notResidue, []byte(
		"package p\n\n"+
			"// Gate 在任务层拒收时转入等待（refused to adopt），并关闭 still-used tool closers。\n"+
			"func Gate() {}\n"), 0o644))
	require.Equal(t, 0, rulesIn(t, notResidue)["audit-marker"],
		"substring hits inside other words must not count as change residue")

	residue := dir + "/habit.go"
	require.NoError(t, os.WriteFile(residue, []byte(
		"package p\n\n"+
			"// Counter 汇总计数，RunFlow no longer keeps it here after the split.\n"+
			"func Counter() {}\n"), 0o644))
	require.GreaterOrEqual(t, rulesIn(t, residue)["audit-marker"], 1,
		"an unambiguous residue word must still be reported")
}

// TestUsedToIsNotAResidueWord pins that the phrase "used to" is not a residue marker.
// - It cannot be distinguished lexically from the purpose clause "used to pass/render"（用于）, so matching it would report compliance prose.
// - Unambiguous residue words still match, and English alternatives must match as whole words rather than as substrings.
func TestUsedToIsNotAResidueWord(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()

	purpose := dir + "/purpose.go"
	require.NoError(t, os.WriteFile(purpose, []byte(
		"package p\n\n"+
			"// ExternalContextKey is the RuntimeState key used to pass external context,\n"+
			"// and closes still-used tool closers when refusing to adopt a claim.\n"+
			"func ExternalContextKey() {}\n"), 0o644))
	require.Equal(t, 0, rulesIn(t, purpose)["audit-marker"],
		"purpose-clause `used to` must not be reported as residue")

	unambiguous := dir + "/unambiguous.go"
	require.NoError(t, os.WriteFile(unambiguous, []byte(
		"package p\n\n"+
			"// Flag is the marker; it previously lived only in the in-memory StateDelta.\n"+
			"func Flag() {}\n"), 0o644))
	require.GreaterOrEqual(t, rulesIn(t, unambiguous)["audit-marker"], 1,
		"`previously` is unambiguous and must still be reported")
}

// TestAuditMarkerReportsMatchedToken pins that the finding names the residue word it matched.
// - Without the token, an operator facing dozens of findings must open every comment group by hand to find which word tripped the rule.
func TestAuditMarkerReportsMatchedToken(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()
	p := dir + "/residue.go"
	require.NoError(t, os.WriteFile(p, []byte(
		"package p\n\n"+
			"// Widget 装配部件。\n//\n"+
			"// 它不再依赖全局注册表。\n"+
			"func Widget() {}\n"), 0o644))
	for _, f := range mustCheck(t, p) {
		if f.Rule == "audit-marker" {
			require.Contains(t, f.Note, "不再",
				"the finding must quote the matched residue word")
			return
		}
	}
	t.Fatal("expected an audit-marker finding")
}

// ruleHits returns the Text of every finding fired by the given rule.
func ruleHits(fs []finding, rule string) []string {
	var out []string
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f.Text)
		}
	}
	return out
}

func scanSource(t *testing.T, name, src string) []finding {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	fs, err := checkFile(path)
	require.NoError(t, err)
	return fs
}

// TestDocMustStartWithDeclaredName pins that a doc comment starts with the identifier it documents.
// - A mechanically moved trailing comment above a struct field satisfies the slot rule but not this one, so the scanner must catch it.
func TestDocMustStartWithDeclaredName(t *testing.T) {
	src := "package p\n\nvar X int\n\ntype T struct {\n\t// count of things\n\tn int\n}\n\n// Foo does the thing.\nfunc Foo() {}\n"
	fs := scanSource(t, "a.go", src)
	require.Contains(t, ruleHits(fs, "doc-not-name-prefixed"), "n",
		"a field doc that does not start with the field name must be flagged")
	require.NotContains(t, ruleHits(fs, "doc-not-name-prefixed"), "Foo",
		"a name-prefixed function doc must pass")
}

// TestTestFileDocIsOneIntentLinePlusIndex pins that a test doc slot holds exactly one intent line plus the index.
// - Argument and pitfall narration belongs in assertion messages or in the wiki, not in the doc slot.
func TestTestFileDocIsOneIntentLinePlusIndex(t *testing.T) {
	ok := "package p\n\nimport \"testing\"\n\n// TestX 验证契约甲。\n//\n// 契约: docs/wiki/a.md\nfunc TestX(t *testing.T) {}\n"
	bad := "package p\n\nimport \"testing\"\n\n// TestX 验证契约甲：\n// 没有这条就会形同虚设，因为某机制如何如何。\n//\n// 契约: docs/wiki/a.md\nfunc TestX(t *testing.T) {}\n"
	require.Empty(t, ruleHits(scanSource(t, "ok_test.go", ok), "test-doc-not-one-sentence"))
	require.Contains(t, ruleHits(scanSource(t, "bad_test.go", bad), "test-doc-not-one-sentence"), "TestX",
		"a multi-line test doc must be flagged")
}

// TestTestDocAllowsWrappedBulletList pins that the intent line may wrap as a bullet list of parallel points.
// - Only prose continuation is a shape violation, and index lines never count toward the shape.
func TestTestDocAllowsWrappedBulletList(t *testing.T) {
	src := "package p\n\nimport \"testing\"\n\n// TestX 钉住 契约甲。\n// 契约: docs/wiki/a.md#x\n// - 要点一\n// - 要点二\nfunc TestX(t *testing.T) {}\n"
	require.Empty(t, ruleHits(scanSource(t, "a_test.go", src), "test-doc-not-one-sentence"),
		"a wrapped bullet list must be an acceptable multi-line test doc shape")
}

func TestTestDocProseContinuationStillFlagged(t *testing.T) {
	src := "package p\n\nimport \"testing\"\n\n// TestX 钉住 契约甲。\n// 第二条要点没有列表标记，属散文续行。\nfunc TestX(t *testing.T) {}\n"
	require.Contains(t, ruleHits(scanSource(t, "a_test.go", src), "test-doc-not-one-sentence"), "TestX",
		"a prose continuation line must stay a violation")
}

func TestTestDocLineLengthIsCapped(t *testing.T) {
	long := "// TestX " + strings.Repeat("钉", 200) + "。\n"
	src := "package p\n\nimport \"testing\"\n\n" + long + "func TestX(t *testing.T) {}\n"
	require.Contains(t, ruleHits(scanSource(t, "a_test.go", src), "test-doc-line-too-long"), "TestX",
		"an over-long intent line must be flagged rather than crammed into one physical line")
}

func TestTestDocIndexLineIsExemptFromLengthCap(t *testing.T) {
	src := "package p\n\nimport \"testing\"\n\n// TestX 钉住 契约甲。\n// 契约: docs/wiki/agent/" + strings.Repeat("very-long-anchor-", 20) + "#x\nfunc TestX(t *testing.T) {}\n"
	require.Empty(t, ruleHits(scanSource(t, "a_test.go", src), "test-doc-line-too-long"),
		"index lines carry paths and are exempt from the length cap")
}

// TestExternalCoordReferenceIsFlagged pins that a comment must not cite a change name or a planning coordinate.
// - Those references only make sense inside a change artifact, where the plan they point at actually lives.
func TestExternalCoordReferenceIsFlagged(t *testing.T) {
	for _, src := range []string{
		"package p\n\n// Foo 钉住 design line 169 的判定。\nfunc Foo() {}\n",
		"package p\n\n// Foo 见 restrict-comments-to-godoc-and-index 的处理。\nfunc Foo() {}\n",
		"package p\n\n// Foo 覆盖 7.2\u2461 场景。\nfunc Foo() {}\n",
	} {
		hits := ruleHits(scanSource(t, "a.go", src), "external-coord-ref")
		require.NotEmpty(t, hits, "a planning coordinate or change name must be flagged: %q", src)
	}
}

func TestExternalCoordExemptsIndexLinesAndPlainProse(t *testing.T) {
	src := "package p\n\n// Foo 钉住 契约甲，判定见注册表。\n// 契约: docs/wiki/agent/task-lifecycle.md#status-machine\nfunc Foo() {}\n"
	require.Empty(t, ruleHits(scanSource(t, "a.go", src), "external-coord-ref"),
		"index lines carry paths and plain prose must not trip the coordinate check")
}

// writeTree lays out a fixture repo under root: each key is a path, each value its body.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
}

// TestBaselineRoundTripStampsItsScanSet pins that a baseline carries the directory set it was generated over.
// - the set must survive the round trip, otherwise a changed scope cannot be detected at all;
// - the regenerate hint must name `bash scripts/lint.sh --update-baseline`: a bare scope is how a nested module drops out of the gate.
func TestBaselineRoundTripStampsItsScanSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	counts := map[string]int{"free-standing": 3, "audit-marker": 2}
	require.NoError(t, writeBaseline(path, counts, []string{".", "examples/wechat-bot"}, false))

	got, err := readBaseline(path)
	require.NoError(t, err)
	require.Equal(t, counts, got.Counts)
	require.Equal(t, []string{".", "examples/wechat-bot"}, got.Dirs,
		"the scan set must survive the round trip, otherwise a mismatch cannot be detected")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "bash scripts/lint.sh --update-baseline",
		"the regenerate hint must not teach the partial invocation")
}

// TestScopeGuardRefusesMismatchedScanSet pins that a run may neither read nor write the ratchet over another scope.
// - scanning less tree lowers every rule's total, which looks exactly like a batch that cleaned findings;
// - the error must name the recorded set so the caller knows where to retarget.
func TestScopeGuardRefusesMismatchedScanSet(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.go":     "package p\n",
		"m/go.mod": "module m\n",
		"m/b.go":   "package m\n",
	})
	err := checkScanSet(root, []string{".", "m"}, []string{"."})
	require.Error(t, err, "a run that scans a subset must not get to read or write the ratchet")
	require.Contains(t, err.Error(), "generated over [. m]",
		"the error must name the recorded set so the caller can retarget")
}

// TestScopeGuardRefusesUncoveredNestedModule pins that agreeing with the recorded set is not sufficient.
// - a recorded set that itself omits a module in the tree stays a hole and must still be refused;
// - the error must name the uncovered module directory.
func TestScopeGuardRefusesUncoveredNestedModule(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.go":     "package p\n",
		"m/go.mod": "module m\n",
		"m/b.go":   "package m\n",
	})
	err := checkScanSet(root, []string{"."}, []string{"."})
	require.Error(t, err, "a nested module left out of the scan set must be an error, not a smaller baseline")
	require.Contains(t, err.Error(), "m", "the error must name the uncovered module")
}

// TestScopeGuardAcceptsCompleteSetAndIgnoresOrder pins both directions of the scope comparison.
// - ordering and spelling noise must not look like a different scan set, or the canonical entry point gets refused;
// - listing a directory together with its own module parent counts those files twice and inflates every rule they touch.
func TestScopeGuardAcceptsCompleteSetAndIgnoresOrder(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.go":     "package p\n",
		"m/go.mod": "module m\n",
		"m/b.go":   "package m\n",
	})
	require.NoError(t, checkScanSet(root, []string{".", "m"}, []string{"m", "."}),
		"ordering and spelling noise must not look like a different scope")

	writeTree(t, root, map[string]string{"sub/c.go": "package sub\n"})
	err := checkScanSet(root, []string{".", "sub"}, []string{".", "sub"})
	require.Error(t, err, "sub lives in the root module, so listing both counts sub/c.go twice")
	require.Contains(t, strings.ToLower(err.Error()), "twice")
}

// TestIgnoredGoFilesOutsideGitRepoIsEmpty pins the fallback that keeps a scan outside a work tree behaving as it did before.
// - No git work tree means no exclusions, so the counts still cover every Go file on disk.
func TestIgnoredGoFilesOutsideGitRepoIsEmpty(t *testing.T) {
	require.Empty(t, ignoredGoFiles(t.TempDir()))
}

// TestIgnoredGoFilesReportsGitIgnoredSources pins what the ratchet must refuse to count.
// - A git-ignored source is absent from a checkout, so a baseline holding its findings is not reproducible.
// - Scope is decided relative to the scan root, and nested modules are covered like the root module.
// - The run is skipped when git is unavailable, because the contract is git's own exclusion scope.
func TestIgnoredGoFilesReportsGitIgnoredSources(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required to exercise the repository-scope filter")
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".gitignore":       "wip.go\nvendored/\n",
		"kept.go":          "package p\n",
		"wip.go":           "package p\n",
		"m/go.mod":         "module m\n",
		"m/wip.go":         "package m\n",
		"vendored/deep.go": "package p\n",
	})
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	got := ignoredGoFiles(root)
	require.True(t, isIgnored(got, "wip.go"), "an ignored source must leave the ratchet")
	require.True(t, isIgnored(got, filepath.Join("m", "wip.go")), "a nested module must be covered too")
	require.False(t, isIgnored(got, "kept.go"), "a source git does not ignore stays gated")
	files, err := repoGoFiles(root, root, got)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(root, "kept.go")}, files,
		"a wholly ignored directory must take the sources inside it out of the scan")
}

// TestRepoGoFilesDropsIgnoredSources pins the seam the scan loop uses.
// - An ignored file leaves the list, while an empty ignored set leaves the walk untouched.
// - A directory key takes everything beneath it out of the scan.
func TestRepoGoFilesDropsIgnoredSources(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"kept.go": "package p\n", "sub/deep.go": "package sub\n", "gen/ib.go": "package gen\n"})
	files, err := repoGoFiles(root, root, map[string]bool{filepath.Join("sub", "deep.go"): true, "gen": true})
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(root, "kept.go")}, files)
	all, err := repoGoFiles(root, root, map[string]bool{})
	require.NoError(t, err)
	require.Len(t, all, 3, "an empty ignored set must leave the walk untouched")
}

// TestLicenseHeaderIsNotFreeStanding pins that a license header is not a violation.
// - A header above the package clause is not a documentation slot, so the ratchet may not call it free-standing.
func TestLicenseHeaderIsNotFreeStanding(t *testing.T) {
	chdirToRepoRoot(t)
	p := filepath.Join(t.TempDir(), "head.go")
	require.NoError(t, os.WriteFile(p, []byte(
		"// Copyright 2025 tagent authors. All rights reserved.\n"+
			"// Use of this source code is governed by a BSD-style license.\n"+
			"\n"+
			"// Package p does things.\n"+
			"package p\n"), 0o644))
	require.Zero(t, rulesIn(t, p)["free-standing"], "a license header must stay outside the violation set")
}

// TestBlankIdentifierGroupNeedsNoNamePrefix pins that compile-time assertions escape the name check.
// - A doc for `var _ Iface = (*T)(nil)` cannot start with the blank identifier, so the requirement is unsatisfiable.
func TestBlankIdentifierGroupNeedsNoNamePrefix(t *testing.T) {
	chdirToRepoRoot(t)
	p := filepath.Join(t.TempDir(), "assert.go")
	require.NoError(t, os.WriteFile(p, []byte(
		"package p\n\nimport \"io\"\n\n"+
			"// compile-time assertion: T implements io.Closer.\n"+
			"var _ io.Closer = (*T)(nil)\n\n"+
			"type T struct{}\n\n"+
			"func (T) Close() error { return nil }\n"), 0o644))
	require.Zero(t, rulesIn(t, p)["doc-not-name-prefixed"], "the blank identifier must not be demanded as a doc prefix")
}

// TestFindingTextTruncationKeepsValidUTF8 pins that shortening a finding never splits a rune.
// - Chinese comment text is the norm here, so a byte cut mid-rune yields unreadable output.
func TestFindingTextTruncationKeepsValidUTF8(t *testing.T) {
	chdirToRepoRoot(t)
	long := strings.Repeat("记忆", 100)
	got := firstLine(long)
	require.True(t, utf8.ValidString(got), "truncated finding text must stay valid UTF-8")
	require.LessOrEqual(t, utf8.RuneCountInString(got), 121)
}

// TestIndexRootNoteNamesOnlyLegalRoots pins that the note cannot advertise an illegal root.
// - The note once advertised a root the whitelist rejects, so following it loops back to the same rejection.
func TestIndexRootNoteNamesOnlyLegalRoots(t *testing.T) {
	chdirToRepoRoot(t)
	p := filepath.Join(t.TempDir(), "idx.go")
	require.NoError(t, os.WriteFile(p, []byte(
		"package p\n\n"+
			"// Foo does things.\n"+
			"// 契约: openspec/specs/x.md\n"+
			"func Foo() {}\n"), 0o644))
	fs, err := checkFile(p)
	require.NoError(t, err)
	var hit []string
	for _, f := range fs {
		if f.Rule == "index-root" {
			hit = append(hit, f.Note)
		}
	}
	require.NotEmpty(t, hit, "an index outside the allowed roots must still be reported")
	for _, n := range hit {
		roots := strings.SplitN(strings.TrimPrefix(n, "index target must live under "), ": ", 2)[0]
		require.NotContains(t, roots, "openspec", "the roots clause may not advertise a root the whitelist rejects")
		require.Contains(t, roots, "docs/", "the roots clause must name the real whitelist")
	}
}

// TestIndexTargetResolvesAgainstRepoRootNotCWD pins that index checks are location-independent.
// - A target is repo-root relative by convention, so running from a package directory may not invent missing-file findings.
func TestIndexTargetResolvesAgainstRepoRootNotCWD(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "openspec", "changes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "x.md"), []byte("# X\n\nsome text\n"), 0o644))
	p := filepath.Join(root, "pkg", "a.go")
	require.NoError(t, os.WriteFile(p, []byte(
		"package pkg\n\n"+
			"// A does things.\n"+
			"// 契约: docs/x.md\n"+
			"func A() {}\n"), 0o644))
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join(root, "pkg")))
	defer func() { require.NoError(t, os.Chdir(wd)) }()
	require.Zero(t, rulesIn(t, p)["index-target-missing"], "a target that exists at the repo root must not read as missing")
}

// TestWriteBaselineRefusesToRaiseCounts pins that the ratchet may only be lowered.
// - A raise must be named and refused unless the caller passes an explicit allow-raise.
func TestWriteBaselineRefusesToRaiseCounts(t *testing.T) {
	chdirToRepoRoot(t)
	p := filepath.Join(t.TempDir(), "baseline.json")
	require.NoError(t, writeBaseline(p, map[string]int{"free-standing": 1}, []string{"."}, false))
	err := writeBaseline(p, map[string]int{"free-standing": 2}, []string{"."}, false)
	require.Error(t, err, "raising a slot may not pass silently")
	require.Contains(t, err.Error(), "free-standing")
	require.NoError(t, writeBaseline(p, map[string]int{"free-standing": 2}, []string{"."}, true))
	require.NoError(t, writeBaseline(p, map[string]int{"free-standing": 0}, []string{"."}, false), "lowering stays allowed")
}

// TestDirectiveLineDoesNotExemptProseInSameGroup pins that a directive cannot shield its neighbours.
// - One //nolint line once exempted the whole group, turning any content rule off with a single comment.
func TestDirectiveLineDoesNotExemptProseInSameGroup(t *testing.T) {
	chdirToRepoRoot(t)
	p := filepath.Join(t.TempDir(), "mix.go")
	require.NoError(t, os.WriteFile(p, []byte(
		"package p\n\n"+
			"// Foo keeps the legacy path available for callers.\n"+
			"//nolint:gocritic\n"+
			"func Foo() {}\n"), 0o644))
	require.NotZero(t, rulesIn(t, p)["audit-marker"], "prose in a group holding a directive must still be checked")
}

// TestChangeNamesReportsAMissingTree pins that a failed enumeration is visible, not silent.
// - Without openspec the change-name axis of external-coord simply stops applying, which must be said out loud.
func TestChangeNamesReportsAMissingTree(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	defer func() { require.NoError(t, os.Chdir(wd)) }()
	changeNames = nil
	loadChangeNames()
	require.Empty(t, changeNames, "a tree without openspec yields no change names")
	require.False(t, changeNamesFound, "the enumeration must report that it found no openspec tree")
	changeNamesOnce = sync.Once{}
	changeNames = nil
	changeNamesFound = false
}

// TestBlindSpotIsAnnouncedBeforeTheWalk pins that a dropped axis is reported in every mode.
// - A measure-only run returns before the ratchet is consulted, so an announcement placed after the walk never reaches it.
// - With the openspec tree present the announcement must stay silent, or a clean run reads as a gap.
func TestBlindSpotIsAnnouncedBeforeTheWalk(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	reset := func() {
		changeNamesOnce = sync.Once{}
		changeNames = nil
		changeNamesFound = false
	}
	defer reset()
	logged := func(t *testing.T) string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "stderr")
		require.NoError(t, err)
		orig := os.Stderr
		os.Stderr = f
		announceScanBlindSpots()
		require.NoError(t, f.Sync())
		os.Stderr = orig
		require.NoError(t, f.Close())
		b, err := os.ReadFile(f.Name())
		require.NoError(t, err)
		return string(b)
	}

	reset()
	require.NoError(t, os.Chdir(t.TempDir()))
	out := logged(t)
	require.NoError(t, os.Chdir(wd))
	require.False(t, changeNamesFound, "the announcement must report the enumeration it just ran")
	require.Contains(t, out, "change-name axis", "a run without the openspec tree must name the axis it dropped")

	reset()
	out = logged(t)
	require.True(t, changeNamesFound, "a run from the repository must locate the tree")
	require.NotContains(t, out, "change-name axis", "a run that located the tree must not warn")
}

// colocWrite writes one file into dir and returns its path.
func colocWrite(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// colocBody renders a test file with the given anchor and test count, optionally tagged.
func colocBody(anchor, tag string, tests int) string {
	var b strings.Builder
	if tag != "" {
		b.WriteString("//go:build " + tag + "\n\n")
	}
	b.WriteString("// Package p.\n// 契约: " + anchor + "\npackage p\n\nimport \"testing\"\n\n")
	for i := 0; i < tests; i++ {
		b.WriteString(fmt.Sprintf("func Test%d(t *testing.T) {}\n", i))
	}
	return b.String()
}

// colocFacts collects the co-location facts of every test file in dir.
func colocFacts(t *testing.T, dir string) []*fileFact {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var facts []*fileFact
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fact, err := collectTestFact(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		if fact != nil {
			facts = append(facts, fact)
		}
	}
	return facts
}

// TestCollectTestFactCapturesTheFourTuple pins the fact collector: build universe, first index target, test count and mirror verdict.
func TestCollectTestFactCapturesTheFourTuple(t *testing.T) {
	dir := t.TempDir()
	betaPath := colocWrite(t, dir, "beta_test.go", "// Package p.\n// 契约: docs/wiki/a.md#first\n// 契约: docs/wiki/b.md#second\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\nfunc TestTwo(t *testing.T) {}\n")
	soakPath := colocWrite(t, dir, "soak_test.go", colocBody("docs/wiki/a.md#first", "soak", 1))
	otherPath := colocWrite(t, dir, "other_test.go", "// Package p.\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")

	beta, err := collectTestFact(betaPath)
	require.NoError(t, err)
	require.NotNil(t, beta)
	require.Equal(t, "", beta.tag)
	require.Equal(t, "docs/wiki/a.md#first", beta.anchor)
	require.Equal(t, 2, beta.tests)
	require.False(t, beta.mirrors)

	soak, err := collectTestFact(soakPath)
	require.NoError(t, err)
	require.Equal(t, "soak", soak.tag)

	other, err := collectTestFact(otherPath)
	require.NoError(t, err)
	require.Equal(t, "", other.anchor)
}

// TestCollectTestFactMirrorVerdict pins exact mirroring, the frozen _real tolerance and the deliberate non-tolerance of prefix similarity.
func TestCollectTestFactMirrorVerdict(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "alpha.go", "package p\n")
	colocWrite(t, dir, "zhipu.go", "package p\n")
	colocWrite(t, dir, "action_tool.go", "package p\n")
	exact := colocWrite(t, dir, "alpha_test.go", colocBody("docs/wiki/a.md#x", "", 1))
	variant := colocWrite(t, dir, "zhipu_real_test.go", colocBody("docs/wiki/a.md#x", "", 1))
	prefix := colocWrite(t, dir, "action_test.go", colocBody("docs/wiki/a.md#x", "", 1))

	facts := map[string]bool{}
	for _, p := range []string{exact, variant, prefix} {
		f, err := collectTestFact(p)
		require.NoError(t, err)
		facts[f.stem()] = f.mirrors
	}
	require.True(t, facts["alpha"])
	require.True(t, facts["zhipu_real"])
	require.False(t, facts["action"], "prefix similarity must not launder a missing mirror")
}

// TestCoLocationSparesMirroredGroups pins the zero false-positive side: files that each mirror a production file share an anchor legally.
func TestCoLocationSparesMirroredGroups(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "a.go", "package p\n")
	colocWrite(t, dir, "b.go", "package p\n")
	colocWrite(t, dir, "a_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	colocWrite(t, dir, "b_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	require.Empty(t, checkResponsibilityCoLocation(colocFacts(t, dir)))
}

// TestCoLocationSeparatesBuildUniverses pins that a tagged file and a default file never share a group key.
func TestCoLocationSeparatesBuildUniverses(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "p_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	colocWrite(t, dir, "q_test.go", colocBody("docs/wiki/x.md#y", "soak", 1))
	require.Empty(t, checkResponsibilityCoLocation(colocFacts(t, dir)))
}

// TestCoLocationDropsTestSupportFiles pins that a file holding no top-level Test function never participates and never reports.
func TestCoLocationDropsTestSupportFiles(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "p_test.go", "// Package p.\n// 契约: docs/wiki/x.md#y\npackage p\n\nfunc Helper() {}\n")
	colocWrite(t, dir, "a_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	colocWrite(t, dir, "b_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	got := checkResponsibilityCoLocation(colocFacts(t, dir))
	require.Len(t, got, 2)
	for _, f := range got {
		require.NotContains(t, f.Path, "p_test.go")
	}
}

// TestCoLocationFlagsFragmentedPair pins the core catch: the unmirrored member of a same-anchor pair is reported and the mirror is named as the merge target.
func TestCoLocationFlagsFragmentedPair(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "a.go", "package p\n")
	colocWrite(t, dir, "a_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	frag := colocWrite(t, dir, "frag_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	got := checkResponsibilityCoLocation(colocFacts(t, dir))
	require.Len(t, got, 1)
	require.Equal(t, frag, got[0].Path)
	require.Equal(t, "responsibility-fragmentation", got[0].Rule)
	require.Equal(t, "docs/wiki/x.md#y", got[0].Text)
}

// TestCoLocationNoteNamesThreeExits pins that every finding states the merge target, the rename exit and the docs-side exit.
func TestCoLocationNoteNamesThreeExits(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "a.go", "package p\n")
	colocWrite(t, dir, "a_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	colocWrite(t, dir, "frag_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	got := checkResponsibilityCoLocation(colocFacts(t, dir))
	require.Len(t, got, 1)
	require.Contains(t, got[0].Note, "(1) merge into a_test.go")
	require.Contains(t, got[0].Note, "(2) rename frag_test.go")
	require.Contains(t, got[0].Note, "(3) converge the anchor on the docs side")

	colocWrite(t, dir, "lonely_test.go", colocBody("docs/wiki/x.md#y", "", 1))
	got = checkResponsibilityCoLocation(colocFacts(t, dir))
	require.Len(t, got, 2)
	for _, f := range got {
		require.Contains(t, f.Note, "(1) merge into")
	}
}

// TestCoLocationIgnoresUndeclaredAnchors pins that declaration duty stays with missing-test-responsibility, not with this rule.
func TestCoLocationIgnoresUndeclaredAnchors(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "p_test.go", "// Package p.\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")
	colocWrite(t, dir, "q_test.go", "// Package p.\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")
	require.Empty(t, checkResponsibilityCoLocation(colocFacts(t, dir)))
}

// TestCoLocationSeparatesCompilationUnits pins that the internal and external test package of one directory never share a group.
func TestCoLocationSeparatesCompilationUnits(t *testing.T) {
	dir := t.TempDir()
	colocWrite(t, dir, "internal_test.go", "// Package p.\n// 契约: docs/wiki/x.md#y\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n")
	colocWrite(t, dir, "external_test.go", "// Package p.\n// 契约: docs/wiki/x.md#y\npackage p_test\n\nimport (\n\t\"testing\"\n\n\t\"x.example/mod/p\"\n)\n\nfunc TestTwo(t *testing.T) { _ = p.Q }\n")
	require.Empty(t, checkResponsibilityCoLocation(colocFacts(t, dir)))
}

// TestMissingFileResponsibility pins the production file-level index duty.
// - A production file without any 契约:/规格: index fires the rule exactly once.
// - An index in any documentation slot satisfies it, mirroring the test-side detection.
func TestMissingFileResponsibility(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()
	bare := filepath.Join(dir, "bare.go")
	require.NoError(t, os.WriteFile(bare, []byte("package p\n\n// Foo does things.\nfunc Foo() {}\n"), 0o644))
	require.Equal(t, 1, rulesIn(t, bare)["missing-file-responsibility"])
	indexed := filepath.Join(dir, "indexed.go")
	require.NoError(t, os.WriteFile(indexed, []byte("package p\n\n// Foo does things.\n//\n// 契约: docs/wiki/x.md\nfunc Foo() {}\n"), 0o644))
	require.Zero(t, rulesIn(t, indexed)["missing-file-responsibility"])
}

// TestResponsibilityExemptions pins who stays outside the file-level duty.
// - Test files belong to the test-side declaration rule, never to this one.
// - The gate's own tooling under scripts/ has no wiki home, on relative paths only.
func TestResponsibilityExemptions(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()
	tf := filepath.Join(dir, "p_test.go")
	require.NoError(t, os.WriteFile(tf, []byte("package p\n\nimport \"testing\"\n\n// TestFoo verifies one contract.\nfunc TestFoo(t *testing.T) {}\n"), 0o644))
	require.Zero(t, rulesIn(t, tf)["missing-file-responsibility"])
	require.True(t, exemptFromResponsibility("scripts/tool/main.go"))
	require.True(t, exemptFromResponsibility("./scripts/tool/main.go"))
	require.True(t, exemptFromResponsibility("agent/x_test.go"))
	require.False(t, exemptFromResponsibility("agent/x.go"))
	require.False(t, exemptFromResponsibility("examples/wechat-bot/main.go"))
}

// TestDocNotBriefBudget pins the doc-shape rule for declaration documentation.
// - A wrapped two-line sentence and a second short paragraph stay within budget.
// - A third prose paragraph is narrative depth; bullets and index lines never count.
func TestDocNotBriefBudget(t *testing.T) {
	chdirToRepoRoot(t)
	dir := t.TempDir()
	thin := filepath.Join(dir, "thin.go")
	require.NoError(t, os.WriteFile(thin, []byte("package p\n\n// Foo does things.\n// It also refuses nil input.\nfunc Foo() {}\n"), 0o644))
	require.Zero(t, rulesIn(t, thin)["doc-not-brief"])
	twopara := filepath.Join(dir, "twopara.go")
	require.NoError(t, os.WriteFile(twopara, []byte("package p\n\n// Foo does things.\n//\n// It also refuses nil input.\nfunc Foo() {}\n"), 0o644))
	require.Zero(t, rulesIn(t, twopara)["doc-not-brief"])
	thick := filepath.Join(dir, "thick.go")
	require.NoError(t, os.WriteFile(thick, []byte("package p\n\n// Foo does things.\n//\n// It also refuses nil input.\n//\n// And it retries once on transport errors.\nfunc Foo() {}\n"), 0o644))
	require.Equal(t, 1, rulesIn(t, thick)["doc-not-brief"])
	bulleted := filepath.Join(dir, "bullets.go")
	require.NoError(t, os.WriteFile(bulleted, []byte("package p\n\n// Foo does things.\n// - first point\n// - second point\n// - third point\n//\n// 契约: docs/wiki/x.md\nfunc Foo() {}\n"), 0o644))
	require.Zero(t, rulesIn(t, bulleted)["doc-not-brief"])
}

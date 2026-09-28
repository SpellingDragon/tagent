package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestCleanFixtureIsNotFlagged guards the false-positive side: a compliant file
// must produce nothing, otherwise batches will drown in exemptions.
func TestCleanFixtureIsNotFlagged(t *testing.T) {
	chdirToRepoRoot(t)
	require.Empty(t, rulesIn(t, "scripts/comment_policy/testdata/clean.go"))
}

// TestEachRuleFires proves every documented rule actually catches its planted
// case, so a passing gate means compliance rather than a broken matcher.
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

// TestIndexRulesAcceptLiveTargetAndRejectDeadOrProcessPaths pins both directions
// of the documentation-index rule.
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

// TestTestFileMustDeclareResponsibility pins the layout rule a consolidation batch
// relies on: a test file names the production responsibility it covers.
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

// TestRatchetBlocksIncreasesAndAllowsKnownCounts pins the direction of the gate:
// an increase is always a failure, a known level passes, and a decrease is reported
// so the baseline can be lowered rather than silently drifting.
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

// TestBaselineRoundTrip guards the hand-rolled writer: unreadable JSON would turn
// every CI run into a hard error or, worse, an empty baseline.
func TestBaselineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	counts := map[string]int{
		"x_test.go|free-standing": 7,
		"y.go|missing-symbol-doc": 1,
		"z.go|unindexed-path-ref": 12,
	}
	require.NoError(t, writeBaseline(path, counts))
	got, err := readBaseline(path)
	require.NoError(t, err)
	require.Equal(t, counts, got)

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

func TestDocPathRefIgnoresRuntimeAssetNames(t *testing.T) {
	// A prompt asset file named in prose (recall_agent.md) is data, not a
	// documentation citation; only path-shaped documentation references need the
	// index form.
	require.False(t, docPathRef.MatchString(`// Prompt loading: overrides PromptDir + "recall_agent.md" if set`),
		"runtime asset names must not be treated as doc citations")
	require.True(t, docPathRef.MatchString(`// see docs/wiki/tool/tool-architecture.md for details`),
		"path-shaped documentation references must still be caught")
}

// TestDeclaredNameIsNotItselfResidue pins the precision of the content rules: a
// declared identifier may legitimately contain a residue word (…Legacy…), and
// quoting that name at the head of its own doc comment must not be reported.
// Residue in the prose next to it still is — the rule is about narration, not
// about which characters appear inside a Go identifier.
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

// TestUsedToRequiresWordBoundaries pins that residue words match as whole words, not
// as substrings: "refused to adopt" and "still-used tool closers" contain "used to" by
// accident and must not be reported, while an unambiguous residue word still is.
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

// TestUsedToIsNotAResidueWord pins the D-22 decision: "used to" cannot be
// distinguished lexically from the purpose clause "used to pass/render" (= 用于),
// so it no longer marks change residue. Unambiguous words still do, and English
// alternatives must match as whole words, not as substrings of other words.
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

// TestAuditMarkerReportsMatchedToken pins that the finding names the residue word it
// matched. Without it, an operator facing dozens of findings must open every comment
// group by hand to find which word tripped the rule.
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

func TestDocMustStartWithDeclaredName(t *testing.T) {
	// Spec: a doc comment SHALL start with the identifier it documents. A
	// mechanically moved trailing comment ("// count of things" above field n)
	// satisfies the slot rule but not this one, so the scanner must catch it.
	src := "package p\n\nvar X int\n\ntype T struct {\n\t// count of things\n\tn int\n}\n\n// Foo does the thing.\nfunc Foo() {}\n"
	fs := scanSource(t, "a.go", src)
	require.Contains(t, ruleHits(fs, "doc-not-name-prefixed"), "n",
		"a field doc that does not start with the field name must be flagged")
	require.NotContains(t, ruleHits(fs, "doc-not-name-prefixed"), "Foo",
		"a name-prefixed function doc must pass")
}

func TestTestFileDocIsOneIntentLinePlusIndex(t *testing.T) {
	// Spec: a test doc slot holds exactly one intent line plus the index; the
	// argument and pitfall narration belong in assertion messages or in wiki.
	ok := "package p\n\nimport \"testing\"\n\n// TestX 验证契约甲。\n//\n// 契约: docs/wiki/a.md\nfunc TestX(t *testing.T) {}\n"
	bad := "package p\n\nimport \"testing\"\n\n// TestX 验证契约甲：\n// 没有这条就会形同虚设，因为某机制如何如何。\n//\n// 契约: docs/wiki/a.md\nfunc TestX(t *testing.T) {}\n"
	require.Empty(t, ruleHits(scanSource(t, "ok_test.go", ok), "test-doc-not-one-sentence"))
	require.Contains(t, ruleHits(scanSource(t, "bad_test.go", bad), "test-doc-not-one-sentence"), "TestX",
		"a multi-line test doc must be flagged")
}

func TestTestDocAllowsWrappedBulletList(t *testing.T) {
	// Norm D-26: the intent line may wrap as a bullet list of parallel points;
	// only prose continuation is a shape violation. Index lines never count.
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

func TestExternalCoordReferenceIsFlagged(t *testing.T) {
	// D-28 merged with D-25: a comment MUST NOT cite a change name or a planning
	// coordinate — those only make sense inside a change artifact.
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

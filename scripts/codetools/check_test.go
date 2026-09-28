package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeFileTree lays out a fake module tree under a temp dir and returns its root.
func writeFileTree(t *testing.T, base string, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), base)
	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

// captureStdout runs fn while collecting everything it prints, so the tests can
// assert on the violation kinds the gate reports.
func captureStdout(t *testing.T, fn func() int) (code int, out string) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	code = fn()
	require.NoError(t, w.Close())
	os.Stdout = saved
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stdout pipe did not drain")
	}
	return code, out
}

// baseTestsFile is one test, one helper and a t.Parallel call: the surface the
// merge gate must preserve.
const baseTestsFile = "package p\n\n" +
	"import \"testing\"\n\n" +
	"// TestAlpha covers the happy path.\n" +
	"func TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\n" +
	"// helper builds a fixture.\nfunc helper() int { return 1 }\n"

// TestCommentCheckIgnoresDocsButNotCodeOrDirectives pins both directions of the
// comment gate: documentation text is invisible, code and compiler directives are
// not. A gate that only ever passed would prove nothing, so every violation case
// asserts the failing exit code and the reported kind.
func TestCommentCheckIgnoresDocsButNotCodeOrDirectives(t *testing.T) {
	cases := []struct {
		name      string
		base      string
		head      string
		wantCode  int
		wantInOut string
	}{
		{
			name:     "documentation rewrite keeps code identical",
			base:     "package p\n\n// Foo returns one.\nfunc Foo() int {\n\t// a step\n\treturn 1 // trailing\n}\n",
			head:     "package p\n\n// Foo carries a long essay about why the earlier shape was wrong,\n// what the review found and what we learned.\nfunc Foo() int {\n\treturn 1\n}\n",
			wantCode: 0,
		},
		{
			name:      "dropping a build constraint is a code change",
			base:      "package p\n\n//go:build integration\n\nfunc Foo() int { return 1 }\n",
			head:      "package p\n\nfunc Foo() int { return 1 }\n",
			wantCode:  1,
			wantInOut: "CODE-CHANGED",
		},
		{
			name:      "statement change is a code change",
			base:      "package p\n\nfunc Foo() int { return 1 }\n",
			head:      "package p\n\nfunc Foo() int { return 2 }\n",
			wantCode:  1,
			wantInOut: "CODE-CHANGED",
		},
		{
			name:      "helper extraction is a code change",
			base:      "package p\n\nfunc Foo() int { return 1 }\n",
			head:      "package p\n\nfunc one() int { return 1 }\n\nfunc Foo() int { return one() }\n",
			wantCode:  1,
			wantInOut: "CODE-CHANGED",
		},
		{
			name:      "deleted file is a violation, not a silent skip",
			base:      "package p\n\nfunc Foo() int { return 1 }\n",
			head:      "",
			wantCode:  1,
			wantInOut: "MISSING-HEAD",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rel := "pkg/a.go"
			head := map[string]string{rel: tc.head}
			if tc.head == "" {
				head = map[string]string{}
			}
			b := writeFileTree(t, "base", map[string]string{rel: tc.base})
			h := writeFileTree(t, "head", head)
			code, out := captureStdout(t, func() int {
				return runCommentCheck([]string{"--base-root", b, "--head-root", h, rel})
			})
			require.Equal(t, tc.wantCode, code, out)
			if tc.wantInOut != "" {
				require.Contains(t, out, tc.wantInOut)
			}
		})
	}
}

// TestMergeCheckGuardsTheTestSurface pins each invariant a test-file consolidation
// batch must preserve, including the two escape hatches (rename map, explain list)
// and the rule that they are required — a move without them is a violation.
func TestMergeCheckGuardsTheTestSurface(t *testing.T) {
	cases := []struct {
		name      string
		head      string
		mapBody   string
		explain   string
		wantCode  int
		wantInOut string
	}{
		{
			name:     "moving a test into another file is invisible to the gate",
			head:     "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc helper() int { return 1 }\n",
			wantCode: 0,
		},
		{
			name:      "dropping an assertion is reported",
			head:      "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n}\n\nfunc helper() int { return 1 }\n",
			wantCode:  1,
			wantInOut: `"kind":"body-changed"`,
		},
		{
			name:      "losing a test is reported",
			head:      "package p\n\nfunc helper() int { return 1 }\n",
			wantCode:  1,
			wantInOut: `"kind":"missing-test"`,
		},
		{
			name:      "an extra test with no baseline counterpart is reported",
			head:      "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc TestBeta(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n}\n\nfunc helper() int { return 1 }\n",
			wantCode:  1,
			wantInOut: `"kind":"extra-test"`,
		},
		{
			name:     "a renamed test passes only when the map declares it",
			head:     "package p\n\nimport \"testing\"\n\nfunc TestAlphaHappyPath(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc helper() int { return 1 }\n",
			mapBody:  "TestAlpha\tTestAlphaHappyPath\n",
			wantCode: 0,
		},
		{
			name:      "a renamed test without the map is reported twice, not quietly accepted",
			head:      "package p\n\nimport \"testing\"\n\nfunc TestAlphaHappyPath(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc helper() int { return 1 }\n",
			wantCode:  1,
			wantInOut: `"kind":"missing-test"`,
		},
		{
			name:      "an unexplained helper deletion is reported",
			head:      "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n",
			wantCode:  1,
			wantInOut: `"kind":"missing-helper"`,
		},
		{
			name:    "a helper deletion recorded in the explain list passes",
			head:    "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n",
			explain: "helper\n", wantCode: 0,
		},
		{
			name:      "losing t.Parallel is reported (scheduling is semantics)",
			head:      "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc helper() int { return 1 }\n",
			wantCode:  1,
			wantInOut: `"kind":"body-changed"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
			h := writeFileTree(t, "head", map[string]string{"pkg/z_test.go": tc.head})
			var args []string
			if tc.mapBody != "" {
				mf := filepath.Join(t.TempDir(), "map.tsv")
				require.NoError(t, os.WriteFile(mf, []byte(tc.mapBody), 0o644))
				args = append(args, "--map", mf)
			}
			if tc.explain != "" {
				ef := filepath.Join(t.TempDir(), "explain.txt")
				require.NoError(t, os.WriteFile(ef, []byte(tc.explain), 0o644))
				args = append(args, "--explain", ef)
			}
			code, out := captureStdout(t, func() int {
				return runMergeCheck(append([]string{"--base-root", b, "--head-root", h},
					append(args, "pkg")...))
			})
			require.Equal(t, tc.wantCode, code, out)
			if tc.wantInOut != "" {
				require.Contains(t, out, tc.wantInOut)
			}
		})
	}
}

// TestMergeCheckComparesCodeNotComments proves a moved test survives a rewrite of
// its documentation, so the two batches stay independent.
func TestMergeCheckComparesCodeNotComments(t *testing.T) {
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
	head := "package p\n\nimport \"testing\"\n\n// TestAlpha now carries a rewritten contract comment.\nfunc TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc helper() int { return 1 }\n"
	h := writeFileTree(t, "head", map[string]string{"pkg/z_test.go": head})
	code, out := captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h, "pkg"})
	})
	require.Equal(t, 0, code, out)
	require.True(t, strings.Contains(out, "1 package(s) intact"), out)
}

// TestMergeCheckRenameNormalizationStaysBlindToNothing guards the failure mode the
// normalizer can introduce: if declared renames were applied so broadly that every
// body hashed to the same value, the gate would report "intact" forever.
func TestMergeCheckRenameNormalizationStaysBlindToNothing(t *testing.T) {
	const base = "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc helper() int { return 1 }\n"
	// Renamed test whose body also lost an assertion: the rename must not excuse it.
	const withDrop = "package p\n\nimport \"testing\"\n\nfunc TestBeta(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n}\n\nfunc helperRenamed() int { return 1 }\n"
	// Renamed test and renamed helper it calls, nothing else changed: a pure rename.
	const pureRename = "package p\n\nimport \"testing\"\n\nfunc TestBeta(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n\nfunc helperRenamed() int { return 1 }\n"

	mf := filepath.Join(t.TempDir(), "map.tsv")
	require.NoError(t, os.WriteFile(mf, []byte("TestAlpha\tTestBeta\nhelper\thelperRenamed\n"), 0o644))
	empty := filepath.Join(t.TempDir(), "explain.txt")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))

	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/z_test.go": withDrop})
	code, out := captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h, "--map", mf, "--explain", empty, "pkg"})
	})
	require.Equalf(t, 1, code, "a declared rename must not excuse a dropped assertion:\n%s", out)
	require.Contains(t, out, `"name":"TestAlpha"`)
	require.NotContains(t, out, "intact")

	b2 := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": base})
	h2 := writeFileTree(t, "head", map[string]string{"pkg/z_test.go": pureRename})
	code, out = captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b2, "--head-root", h2, "--map", mf, "--explain", empty, "pkg"})
	})
	require.Equalf(t, 0, code, "renaming a test and an identifier it calls is a pure rename:\n%s", out)
}

// TestMergeCheckAllowsStrengtheningOnly pins the monotone direction: adding an
// assertion to a moved test is welcome, removing one is not.
func TestMergeCheckAllowsStrengtheningOnly(t *testing.T) {
	base := "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n}\n"
	stronger := "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n\trequire.NotNil(t, 1)\n}\n"
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/z_test.go": stronger})
	code, out := captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h, "pkg"})
	})
	require.Equalf(t, 1, code, "bodies differ (one assertion added) so the move is not a pure move:\n%s", out)
	require.Contains(t, out, `"kind":"body-changed"`)
}

func TestMergeCheckRejectsRenameThatStopsBeingATest(t *testing.T) {
	// A rename that drops the Test prefix turns coverage into dead code: `go test`
	// reports success and the body hash reports intact, so only the gate can see it.
	base := "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n}\n"
	mangled := "package p\n\nimport \"testing\"\n\nfunc alpha(t *testing.T) {\n\trequire.Equal(t, 1, 1)\n}\n"
	mapFile := filepath.Join(t.TempDir(), "map.tsv")
	require.NoError(t, os.WriteFile(mapFile, []byte("TestAlpha\talpha\n"), 0o600))
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": mangled})
	code, out := captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h, "--map", mapFile, "pkg"})
	})
	require.Equalf(t, 1, code, "a renamed-away test must not pass as a move:\n%s", out)
	require.Contains(t, out, `"kind":"test-name-mangled"`)
}

func TestApplyRenamesIgnoresStringLiterals(t *testing.T) {
	// A rename describes identifiers. A fixture that happens to contain the old name as
	// text (a YAML key, a log substring) must not be rewritten, otherwise the gate
	// manufactures differences that no rename declared.
	src := "func f() {\n\tyaml := \"kind: agent\\n  agent: sub1\\n\"\n\tcall(agent, yaml)\n}\n"
	got := applyRenames(src, map[string]string{"agent": "trpcagent"})
	require.Contains(t, got, `"kind: agent\n  agent: sub1\n"`, "string literal must survive untouched")
	require.Contains(t, got, "call(trpcagent, yaml)", "identifier outside the literal must be renamed")
	require.NotContains(t, got, "call(agent,")
}

func TestApplyRenamesHandlesRawAndRuneLiterals(t *testing.T) {
	src := "var x = `agent: raw`\nvar r = 'a'\nvar y = agent.Field\n"
	got := applyRenames(src, map[string]string{"agent": "trpcagent"})
	require.Contains(t, got, "`agent: raw`")
	require.Contains(t, got, "'a'")
	require.Contains(t, got, "trpcagent.Field")
}

func TestMergeCheckFollowsReceiverTypeRename(t *testing.T) {
	// Renaming a fixture type must not surface each of its methods as a body change.
	base := "package p\n\nfunc (oldType) Do() int { return 1 }\n"
	head := "package p\n\nfunc (newType) Do() int { return 1 }\n"
	mapFile := filepath.Join(t.TempDir(), "map.tsv")
	require.NoError(t, os.WriteFile(mapFile, []byte("oldType\tnewType\n"), 0o600))
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": head})
	code, out := captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h, "--map", mapFile, "pkg"})
	})
	require.Equalf(t, 0, code, "a receiver rename is a declared rename, not a body change:\n%s", out)
}

func TestMergeCheckIgnoresRenameInducedReflow(t *testing.T) {
	// A longer replacement name can push a one-line body past gofmt's width and make it
	// expand. That is layout, not a semantic change, so the witness must not see it.
	base := "package p\n\nfunc f() int {\n\treturn 1\n}\n"
	head := "package p\n\nfunc f() int { return 1 }\n"
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": head})
	code, out := captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h, "pkg"})
	})
	require.Equalf(t, 0, code, "same tokens in different layout must not be a body change:\n%s", out)
}

func TestCommentCheckStripsFieldComments(t *testing.T) {
	// A comment on a struct field is still a comment: changing one must not read as a
	// code change, so the stripping step has to clear field documentation too.
	base := "package p\n\ntype T struct {\n\tA int // first field\n}\n"
	rewritten := "package p\n\ntype T struct {\n\tA int // second wording for the same field\n}\n"
	b := writeFileTree(t, "base", map[string]string{"pkg/a.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/a.go": rewritten})
	code, out := captureStdout(t, func() int {
		return runCommentCheck([]string{"--base-root", b, "--head-root", h, "pkg/a.go"})
	})
	require.Equalf(t, 0, code, "only a field comment differs, so the change is comment-only:\n%s", out)
}

func TestFoldLayoutCollapsesPaddingOnlyDifferences(t *testing.T) {
	// The printer pads alignment groups with tabs whose count depends on neighbouring
	// lines, so identical code can render differently once a comment merges two groups.
	// foldLayout is what makes the witness token-based rather than text-based.
	a := "func f() int {\n\treturn 1\n}"
	b := "func f() int {\n\treturn\t\t1\n}"
	require.NotEqual(t, a, b, "the raw text must actually differ, else the test proves nothing")
	require.Equal(t, foldLayout(a), foldLayout(b), "padding-only differences must fold away")
	require.NotEqual(t, foldLayout(a), foldLayout("func f() int {\n\treturn 2\n}"),
		"a real token change must survive folding")
}

func TestNameCheckFlagsIterationNumbers(t *testing.T) {
	// Guardrail spec: test identifiers must not carry iteration numbers. Domain
	// vocabulary that happens to contain digits must not be caught, or the check
	// becomes noise nobody keeps enabled.
	dir := t.TempDir()
	src := `package p

import "testing"

func TestI1ConcurrentDelegation(t *testing.T) {}
func TestW4Closeout(t *testing.T) {}
func TestParseJudgeVerdict_MissingScoreConservative(t *testing.T) {}
func TestInt64EventKeyRoundTrip(t *testing.T) {}
func TestL1L2L3TierSelection(t *testing.T) {}
func TestSnowflakeV2Format(t *testing.T) {}
func TestMD5And401Handling(t *testing.T) {}
`
	path := dir + "/a_test.go"
	if err := writeFileForTest(path, src); err != nil {
		t.Fatal(err)
	}
	vs := nameViolations(dir)
	got := map[string]bool{}
	for _, v := range vs {
		got[v.Name] = true
	}
	require.True(t, got["TestI1ConcurrentDelegation"], "I1 前缀是迭代编号，必须被拦")
	require.True(t, got["TestW4Closeout"], "W4 批次号必须被拦")
	for _, keep := range []string{"TestParseJudgeVerdict_MissingScoreConservative",
		"TestInt64EventKeyRoundTrip", "TestL1L2L3TierSelection", "TestSnowflakeV2Format", "TestMD5And401Handling"} {
		require.False(t, got[keep], "%s 属领域词汇，不得误报", keep)
	}
}

func writeFileForTest(path, src string) error { return os.WriteFile(path, []byte(src), 0o644) }

func TestDocRefsDetectsDanglingFileCitations(t *testing.T) {
	// Guardrail: docs cite code files as evidence (the D-8 drift class). A citation
	// to a path that no longer exists is drift the comment gates cannot see.
	dir := t.TempDir()
	doc := filepath.Join(dir, "docs", "d.md")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent", "live.go"), []byte("package pkg\n"), 0o644))
	require.NoError(t, os.WriteFile(doc, []byte("佐证：`agent/live.go` 与 `agent/gone_test.go`（用例甲）\n"+
		"泛称不算引用：`SKILL.md`、`config.toml`、`{pid}:evt`、https://example.com/a.go\n"), 0o644))
	got := docDanglingRefs(doc, dir)
	require.Equal(t, []string{"agent/gone_test.go"}, got,
		"only path-like citations that no longer exist may be reported")
}

func TestProcRefsIgnoresScannerSelfReferences(t *testing.T) {
	// The rule must catch instructions that send readers to process artifacts,
	// while not firing on the scanners that implement it (they have to name the
	// pattern). Both sides are load-bearing: without the exemption the gate is
	// noise and would be turned off.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "build.sh"),
		[]byte("# see openspec/changes/add-x/design.md for the contract\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "check-openspec.sh"),
		[]byte("# reports changes under openspec/changes/ but does not fail\n"), 0o644))
	hits := procRefViolations(filepath.Join(dir, "scripts"))
	require.Equal(t, []string{"scripts/build.sh:1"}, hits, "tooling self-reference must not be reported")
}

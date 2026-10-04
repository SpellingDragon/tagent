// 契约: docs/comment-gate-tooling.md#table-discipline
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
	return captureStream(t, &os.Stdout, fn)
}

// captureStderr runs fn while collecting what it writes to stderr, which is where
// a usage line is stated.
func captureStderr(t *testing.T, fn func() int) (code int, out string) {
	t.Helper()
	return captureStream(t, &os.Stderr, fn)
}

// captureStream redirects one standard stream while fn runs and returns its code
// and everything written to that stream.
func captureStream(t *testing.T, target **os.File, fn func() int) (code int, out string) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := *target
	*target = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	code = fn()
	require.NoError(t, w.Close())
	*target = saved
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

// TestCommentCheckIgnoresDocsButNotCodeOrDirectives pins both directions of the comment gate.
// - Documentation text is invisible; code and compiler directives are not.
// - A gate that only ever passed would prove nothing, so every violation case asserts the failing exit code and the reported kind.
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

// TestMergeCheckGuardsTheTestSurface pins each invariant a test-file consolidation batch must preserve.
// - Both escape hatches are covered (rename map, explain list), and they are required: a move without them is a violation.
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

// TestMergeCheckComparesCodeNotComments proves a moved test survives a rewrite of its documentation.
// - That independence is what lets the comment batch and the consolidation batch land separately.
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

// TestMergeCheckRenameNormalizationStaysBlindToNothing guards the failure mode the normalizer can introduce.
// - If declared renames were applied so broadly that every body hashed to the same value, the gate would report "intact" forever.
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

// TestMergeCheckAllowsStrengtheningOnly pins the monotone direction of a moved test.
// - Adding an assertion is welcome, removing one is not.
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

// TestMergeCheckRejectsRenameThatStopsBeingATest pins that a rename dropping the Test prefix is a violation.
// - Such a body turns into dead code: go test reports success and the body hash reports intact, so only the gate can see it.
func TestMergeCheckRejectsRenameThatStopsBeingATest(t *testing.T) {
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

// TestApplyRenamesIgnoresStringLiterals pins that a rename describes identifiers, never string data.
// - A fixture holding the old name as text (a YAML key, a log substring) must stay untouched, or the gate manufactures differences no rename declared.
func TestApplyRenamesIgnoresStringLiterals(t *testing.T) {
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

// TestMergeCheckFollowsReceiverTypeRename pins that renaming a fixture type does not surface each of its methods as a body change.
func TestMergeCheckFollowsReceiverTypeRename(t *testing.T) {
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

// TestMergeCheckIgnoresRenameInducedReflow pins that rename-induced gofmt reflow is layout, not a semantic change.
// - A longer replacement name can push a one-line body past gofmt width and make it expand; the witness must not see that.
func TestMergeCheckIgnoresRenameInducedReflow(t *testing.T) {
	base := "package p\n\nfunc f() int {\n\treturn 1\n}\n"
	head := "package p\n\nfunc f() int { return 1 }\n"
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": head})
	code, out := captureStdout(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h, "pkg"})
	})
	require.Equalf(t, 0, code, "same tokens in different layout must not be a body change:\n%s", out)
}

// TestCommentCheckStripsFieldComments pins that the comment-stripping step clears field documentation too.
// - A comment on a struct field is still a comment, so changing one must not read as a code change.
func TestCommentCheckStripsFieldComments(t *testing.T) {
	base := "package p\n\ntype T struct {\n\tA int // first field\n}\n"
	rewritten := "package p\n\ntype T struct {\n\tA int // second wording for the same field\n}\n"
	b := writeFileTree(t, "base", map[string]string{"pkg/a.go": base})
	h := writeFileTree(t, "head", map[string]string{"pkg/a.go": rewritten})
	code, out := captureStdout(t, func() int {
		return runCommentCheck([]string{"--base-root", b, "--head-root", h, "pkg/a.go"})
	})
	require.Equalf(t, 0, code, "only a field comment differs, so the change is comment-only:\n%s", out)
}

// TestFoldLayoutCollapsesPaddingOnlyDifferences pins why the witness is token-based rather than text-based.
// - The printer pads alignment groups with tabs whose count depends on neighbouring lines, so identical code can render differently once a comment merges groups.
func TestFoldLayoutCollapsesPaddingOnlyDifferences(t *testing.T) {
	a := "func f() int {\n\treturn 1\n}"
	b := "func f() int {\n\treturn\t\t1\n}"
	require.NotEqual(t, a, b, "the raw text must actually differ, else the test proves nothing")
	require.Equal(t, foldLayout(a), foldLayout(b), "padding-only differences must fold away")
	require.NotEqual(t, foldLayout(a), foldLayout("func f() int {\n\treturn 2\n}"),
		"a real token change must survive folding")
}

// TestFoldLayoutKeepsWhitespaceInsideStringLiterals pins that layout folding stops at a literal's edge.
// - Fixture text is content: folding its whitespace makes a changed fixture read as unchanged, the one false pass a witness must never allow.
// - Layout outside literals must still fold, or rename-induced reflow returns as a phantom body change.
func TestFoldLayoutKeepsWhitespaceInsideStringLiterals(t *testing.T) {
	const padded = "var s = `line one\nline two   spaced`\nvar x = 1\n"
	const plain = "var s = `line one\nline two spaced`\nvar x = 1\n"
	require.NotEqual(t, padded, plain, "the raw text must actually differ, else the test proves nothing")
	require.NotEqual(t, foldLayout(padded), foldLayout(plain),
		"whitespace inside a literal is content and must survive folding")

	const quotedPadded = "var s = \"a   b\"\nvar x = 1\n"
	const quotedPlain = "var s = \"a b\"\nvar x = 1\n"
	require.NotEqual(t, foldLayout(quotedPadded), foldLayout(quotedPlain),
		"an interpreted string's spacing is content too")

	const reflowA = "var x = 1\nvar y\t= 2\n"
	const reflowB = "var x\t=  1\nvar y = 2\n"
	require.NotEqual(t, reflowA, reflowB, "the raw text must actually differ")
	require.Equal(t, foldLayout(reflowA), foldLayout(reflowB), "layout outside literals must still fold away")
}

// TestDropBlankLinesKeepsLiteralLines pins that blank-line and trailing-space removal stop at a literal's edge.
// - A raw fixture's empty lines and padded lines are content; eating them turns a fixture edit into a no-op.
// - Blank lines and trailing spaces outside literals remain layout and must still go.
func TestDropBlankLinesKeepsLiteralLines(t *testing.T) {
	const withBlank = "var s = `one\n\ntwo`\n\nvar x = 1\n"
	const withoutBlank = "var s = `one\ntwo`\nvar x = 1\n"
	require.NotEqual(t, dropBlankLines(withBlank), dropBlankLines(withoutBlank),
		"a blank line inside a literal must survive")

	const padded = "var s = `one   \ntwo`\nvar x = 1   \n"
	require.Contains(t, dropBlankLines(padded), "`one   \ntwo`", "trailing spaces inside a literal must survive")
	require.Contains(t, dropBlankLines(padded), "var x = 1\n", "trailing spaces outside a literal must be trimmed")

	require.Equal(t, "var x = 1\nvar y = 2\n", dropBlankLines("var x = 1\n\nvar y = 2\n"),
		"a blank line outside literals is still dropped")
}

// TestQualifierBlindKeepsLiteralContent pins that alias blinding stops at a literal's edge.
// - Blinding a qualified path inside a fixture erases a real key change, so the blinding must be scoped to code.
// - The blinding itself stays: an alias spelled differently outside a literal must remain comparable.
func TestQualifierBlindKeepsLiteralContent(t *testing.T) {
	require.NotEqual(t, qualifierBlind("var s = `agent.name: x`"), qualifierBlind("var s = `other.name: x`"),
		"a qualified path inside a literal is content")
	require.Equal(t, qualifierBlind("return require.Equal(t, 1)"), qualifierBlind("return assert.Equal(t, 1)"),
		"alias choice outside literals must stay irrelevant to the witness")
}

// TestCommentCheckStripsTrailingCommentsOnEverySpecSlot pins each slot a trailing comment may occupy.
// - A defined type or alias holds one in TypeSpec.Comment and an import spec in ImportSpec.Comment; an uncleared slot reads as CODE-CHANGED.
// - Field and value slots were already cleared, so they are pinned here too: the rule is every comment slot, not the common ones.
// - A body-level trailing comment reaches the output by another route than a spec slot, so it is pinned as well to keep the boundary explicit.
func TestCommentCheckStripsTrailingCommentsOnEverySpecSlot(t *testing.T) {
	cases := []struct {
		name string
		base string
		head string
	}{
		{name: "defined type", base: "package p\n\ntype Duration int64 // milliseconds\n", head: "package p\n\ntype Duration int64\n"},
		{name: "type alias", base: "package p\n\ntype Millis = int64 // milliseconds\n", head: "package p\n\ntype Millis = int64\n"},
		{
			name: "import spec",
			base: "package p\n\nimport (\n\t\"fmt\" // for printing\n)\n\nfunc f() { fmt.Println(\"x\") }\n",
			head: "package p\n\nimport (\n\t\"fmt\"\n)\n\nfunc f() { fmt.Println(\"x\") }\n",
		},
		{name: "struct field", base: "package p\n\ntype S struct {\n\tA int // milliseconds\n}\n", head: "package p\n\ntype S struct {\n\tA int\n}\n"},
		{name: "const spec", base: "package p\n\nconst (\n\tA = 1 // milliseconds\n)\n", head: "package p\n\nconst (\n\tA = 1\n)\n"},
		{name: "statement label", base: "package p\n\nfunc f() int {\nloop: // why\n\tfor {\n\t\tbreak loop\n\t}\n\treturn 1\n}\n", head: "package p\n\nfunc f() int {\nloop:\n\tfor {\n\t\tbreak loop\n\t}\n\treturn 1\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, tc.base, tc.head, "the fixture pair must actually differ")
			b := writeFileTree(t, "base", map[string]string{"pkg/a.go": tc.base})
			h := writeFileTree(t, "head", map[string]string{"pkg/a.go": tc.head})
			code, out := captureStdout(t, func() int {
				return runCommentCheck([]string{"--base-root", b, "--head-root", h, "pkg/a.go"})
			})
			require.Equalf(t, 0, code, "only a trailing comment differs, so the batch is comment-only:\n%s", out)
		})
	}
}

// TestCommentCheckReportsLiteralContentDifferences pins the direction the whole-file witness was blind to.
// - Whitespace-only edits inside a fixture are content edits, so a batch claiming "comments only" must fail on them.
func TestCommentCheckReportsLiteralContentDifferences(t *testing.T) {
	cases := []struct {
		name string
		base string
		head string
	}{
		{
			name: "raw string padding",
			base: "package p\n\nfunc f() string {\n\treturn `line one\nline two   spaced`\n}\n",
			head: "package p\n\nfunc f() string {\n\treturn `line one\nline two spaced`\n}\n",
		},
		{
			name: "interpreted string padding",
			base: "package p\n\nfunc f() string {\n\treturn \"a   b\"\n}\n",
			head: "package p\n\nfunc f() string {\n\treturn \"a b\"\n}\n",
		},
		{
			name: "blank line inside a raw string",
			base: "package p\n\nfunc f() string {\n\treturn `one\n\ntwo`\n}\n",
			head: "package p\n\nfunc f() string {\n\treturn `one\ntwo`\n}\n",
		},
		{
			name: "trailing spaces inside a raw string",
			base: "package p\n\nfunc f() string {\n\treturn `one   \ntwo`\n}\n",
			head: "package p\n\nfunc f() string {\n\treturn `one\ntwo`\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, tc.base, tc.head, "the fixture pair must actually differ")
			b := writeFileTree(t, "base", map[string]string{"pkg/a.go": tc.base})
			h := writeFileTree(t, "head", map[string]string{"pkg/a.go": tc.head})
			code, out := captureStdout(t, func() int {
				return runCommentCheck([]string{"--base-root", b, "--head-root", h, "pkg/a.go"})
			})
			require.Equalf(t, 1, code, "a literal-content difference is a code change, not layout:\n%s", out)
			require.Contains(t, out, "CODE-CHANGED")
		})
	}
}

// TestMergeCheckReportsLiteralContentDifferences pins that the per-declaration body hash sees fixtures.
// - The body hash folds the same way as the whole-file witness, so a fixture edit inside a test must surface as body-changed.
// - A fixture change must not be waivable: explain never exempts a test declaration.
func TestMergeCheckReportsLiteralContentDifferences(t *testing.T) {
	cases := []struct {
		name string
		base string
		head string
	}{
		{
			name: "whitespace inside a fixture",
			base: "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tsrc := `a   b`\n\tt.Fatal(src)\n}\n",
			head: "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tsrc := `a b`\n\tt.Fatal(src)\n}\n",
		},
		{
			name: "qualified path inside a fixture",
			base: "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tsrc := `agent.name: x`\n\tt.Fatal(src)\n}\n",
			head: "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tsrc := `other.name: x`\n\tt.Fatal(src)\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, tc.base, tc.head, "the fixture pair must actually differ")
			b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": tc.base})
			h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": tc.head})
			code, out := captureStdout(t, func() int {
				return runMergeCheck([]string{"--base-root", b, "--head-root", h, "pkg"})
			})
			require.Equalf(t, 1, code, "a changed fixture is a changed body:\n%s", out)
			require.Contains(t, out, `"kind":"body-changed"`)
		})
	}
}

// TestMergeCheckUsageStatesTheFlagShape pins that the flag pitfall is stated where a caller meets it.
// - A repeated --map is a usage error, not a silent last-wins override that drops the first table's renames.
// - The usage text is the only surface reachable before a reading is trusted, so it must carry the per-package pairing and the non-test limit of --explain.
func TestMergeCheckUsageStatesTheFlagShape(t *testing.T) {
	code, out := captureStderr(t, func() int {
		return runMergeCheck(nil)
	})
	require.Equalf(t, 2, code, "a call with no roots must be refused:\n%s", out)
	for _, want := range []string{"ONE file", "usage error", "per package", "exempts only non-test"} {
		require.Contains(t, out, want, "usage must state the flag shape")
	}
}

// TestNameCheckFlagsIterationNumbers pins the naming guardrail: test identifiers must not carry iteration numbers.
// - Domain vocabulary that happens to contain digits must not be caught, or the check becomes noise nobody keeps enabled.
func TestNameCheckFlagsIterationNumbers(t *testing.T) {
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

// TestDocRefsDetectsDanglingFileCitations pins that docs citing code files as evidence stay resolvable.
// - A citation whose target path is absent from the tree is drift the comment-shape gates cannot see.
func TestDocRefsDetectsDanglingFileCitations(t *testing.T) {
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

// TestProcRefsIgnoresScannerSelfReferences pins both sides of the process-reference rule.
// - Instructions sending readers to process artifacts must be caught.
// - The scanners implementing it must name the pattern without tripping it: without that exemption the gate is noise and gets switched off.
func TestProcRefsIgnoresScannerSelfReferences(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "build.sh"),
		[]byte("# see openspec/changes/add-x/design.md for the contract\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "check-openspec.sh"),
		[]byte("# reports changes under openspec/changes/ but does not fail\n"), 0o644))
	hits := procRefViolations(filepath.Join(dir, "scripts"))
	require.Equal(t, []string{"scripts/build.sh:1"}, hits, "tooling self-reference must not be reported")
}

// TestMergeCheckRepeatedFlagFails pins the hard-fail contract for repeated single-value flags.
// - A repeated --map silently shadowing the first table is how a batch once shipped with a dead catalog argument.
func TestMergeCheckRepeatedFlagFails(t *testing.T) {
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
	h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": baseTestsFile})
	code, _ := captureStderr(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h,
			"--map", "a.tsv", "--map", "b.tsv", "pkg"})
	})
	require.Equal(t, 2, code, "repeated --map must be a usage error, not silent last-wins")
	code2, _ := captureStderr(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h,
			"--explain", "a.txt", "--explain", "b.txt", "pkg"})
	})
	require.Equal(t, 2, code2, "repeated --explain must be a usage error, not silent last-wins")
}

// writeExplain lays out an --explain list file and returns its path.
func writeExplain(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "explain.txt")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

// TestMergeCheckDeadExplainFails pins that an --explain entry exempting nothing in the run is a hard error, not harmless cruft.
// - A stale or over-broad waiver lets a wrong mapping table read as reviewed, because it conceals a difference it never excused.
// - A waiver that suppresses a real report stays accepted, so the guard cannot be dodged by emptying --explain.
func TestMergeCheckDeadExplainFails(t *testing.T) {
	const headNoHelper = "package p\n\nimport \"testing\"\n\nfunc TestAlpha(t *testing.T) {\n\tt.Parallel()\n\trequire.Equal(t, 1, 1)\n\tassert.True(t, true)\n}\n"

	t.Run("entry naming nothing at all is refused", func(t *testing.T) {
		b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
		h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": baseTestsFile})
		ef := writeExplain(t, "ghostHelper\n")
		code, out := captureStderr(t, func() int {
			return runMergeCheck([]string{"--base-root", b, "--head-root", h, "--explain", ef, "pkg"})
		})
		require.Equalf(t, 2, code, "an entry that exempted nothing must fail the run:\n%s", out)
		require.Contains(t, out, "DEAD-EXPLAIN ghostHelper")
	})

	t.Run("entry naming an unchanged declaration is refused", func(t *testing.T) {
		b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
		h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": baseTestsFile})
		ef := writeExplain(t, "helper\n")
		code, out := captureStderr(t, func() int {
			return runMergeCheck([]string{"--base-root", b, "--head-root", h, "--explain", ef, "pkg"})
		})
		require.Equalf(t, 2, code, "helper is present and identical on both sides, so no exemption was needed:\n%s", out)
		require.Contains(t, out, "DEAD-EXPLAIN helper")
	})

	t.Run("entry naming a test is refused since tests are never exemptible", func(t *testing.T) {
		b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
		h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": baseTestsFile})
		ef := writeExplain(t, "TestAlpha\n")
		code, out := captureStderr(t, func() int {
			return runMergeCheck([]string{"--base-root", b, "--head-root", h, "--explain", ef, "pkg"})
		})
		require.Equalf(t, 2, code, "a test can never be waived, so registering one is a dead entry:\n%s", out)
		require.Contains(t, out, "DEAD-EXPLAIN TestAlpha")
	})

	t.Run("entry that suppresses a real helper deletion stays live", func(t *testing.T) {
		b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
		h := writeFileTree(t, "head", map[string]string{"pkg/z_test.go": headNoHelper})
		ef := writeExplain(t, "helper\n")
		code, out := captureStderr(t, func() int {
			return runMergeCheck([]string{"--base-root", b, "--head-root", h, "--explain", ef, "pkg"})
		})
		require.Equalf(t, 0, code, "the deletion was exempted by the entry, so it is live:\n%s", out)
		require.NotContains(t, out, "DEAD-EXPLAIN")
	})
}

// TestMergeCheckUnreadableTableFails pins that an explicitly passed table that cannot be read is refused, never treated as empty.
// - A typo'd --explain path loading as an empty set wipes the whole exemption surface, the exact form the dead-entry guard exists to prevent.
// - --map shares the hole and fails later as misleading missing-test attributions, so both flags are covered.
func TestMergeCheckUnreadableTableFails(t *testing.T) {
	b := writeFileTree(t, "base", map[string]string{"pkg/a_test.go": baseTestsFile})
	h := writeFileTree(t, "head", map[string]string{"pkg/a_test.go": baseTestsFile})
	code, out := captureStderr(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h,
			"--explain", filepath.Join(t.TempDir(), "missing.txt"), "pkg"})
	})
	require.Equalf(t, 2, code, "an unreadable --explain must fail the run, not act as an empty table:\n%s", out)
	require.Contains(t, out, "cannot read")
	code2, out2 := captureStderr(t, func() int {
		return runMergeCheck([]string{"--base-root", b, "--head-root", h,
			"--map", filepath.Join(t.TempDir(), "missing.tsv"), "pkg"})
	})
	require.Equalf(t, 2, code2, "an unreadable --map must fail the run, not act as an empty catalog:\n%s", out2)
	require.Contains(t, out2, "cannot read")
}

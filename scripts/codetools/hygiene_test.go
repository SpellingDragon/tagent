// 本文件负责追踪卫生门的判定形状：四条具名拒绝各自可达，干净索引不误报。
// 规格: docs/comment-gate-tooling.md#tracked-hygiene
package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTree maps a tracked path to its size; a missing key means the working tree
// does not have it.
func fakeTree(sizes map[string]int64) func(string) (int64, error) {
	return func(p string) (int64, error) {
		n, ok := sizes[p]
		if !ok {
			return 0, errors.New("no such file")
		}
		return n, nil
	}
}

func rules(t *testing.T, files []string, sizes map[string]int64) []string {
	t.Helper()
	var got []string
	for _, f := range checkTrackedHygiene(files, fakeTree(sizes)) {
		got = append(got, f.path+"="+f.rule)
	}
	return got
}

// TestTrackedHygieneRejectsEachDebrisShape 钉住 四条具名拒绝各自可达，且一条路径命中多条规则时逐条都报。
func TestTrackedHygieneRejectsEachDebrisShape(t *testing.T) {
	got := rules(t, []string{
		"agent/a.go",
		"hottest-sub1/relations.journal",
		"hottest-sub2/.tagent-writer.lock",
		"docs/empty.md",
		"tool/gone.go",
		"newdir/x.go",
	}, map[string]int64{"agent/a.go": 12, "docs/empty.md": 0, "newdir/x.go": 5})
	assert.ElementsMatch(t, []string{
		"hottest-sub1/relations.journal=runtime-artifact-tracked",
		"hottest-sub1/relations.journal=unregistered-top-level",
		"hottest-sub2/.tagent-writer.lock=runtime-artifact-tracked",
		"hottest-sub2/.tagent-writer.lock=unregistered-top-level",
		"docs/empty.md=empty-tracked-file",
		"tool/gone.go=tracked-path-missing",
		"newdir/x.go=unregistered-top-level",
	}, got)
}

func TestTrackedHygienePassesACleanIndex(t *testing.T) {
	findings := checkTrackedHygiene([]string{"agent/a.go", "docs/b.md", ".github/workflows/ci.yml"},
		fakeTree(map[string]int64{"agent/a.go": 3, "docs/b.md": 4, ".github/workflows/ci.yml": 5}))
	require.Empty(t, findings)
}

func TestTrackedHygieneReportsOneFindingPerTopDir(t *testing.T) {
	got := rules(t, []string{"bogus/a.go", "bogus/b.go"}, map[string]int64{"bogus/a.go": 1, "bogus/b.go": 1})
	require.Len(t, got, 1, "an unregistered top directory is reported once, not per file")
}

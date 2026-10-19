// 本文件负责包布局门的判定形状：三条具名拒绝各自可达，干净树不误报。
// 规格: docs/comment-gate-tooling.md#package-layout
package main

import (
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// layoutFiles turns slash paths into the file facts the live walk produces.
func layoutFiles(paths ...string) []layoutFile {
	var out []layoutFile
	for _, p := range paths {
		out = append(out, layoutFile{
			path: p,
			dir:  path.Dir(p),
			base: path.Base(p),
			test: path.Ext(p) == ".go" && len(p) > 8 && p[len(p)-8:] == "_test.go",
		})
	}
	return out
}

// rulesOf renders findings as "path=rule" so a case states the exact verdicts.
func rulesOf(files []layoutFile) []string {
	var got []string
	for _, f := range checkPackageLayout(files) {
		got = append(got, f.path+"="+f.rule)
	}
	return got
}

// TestPackageLayoutRejectsEachViolationShape 钉住 三条具名拒绝各自可达，且一包两份骨架文件同时报变体名与撞名。
func TestPackageLayoutRejectsEachViolationShape(t *testing.T) {
	got := rulesOf(layoutFiles(
		"agent/skeleton_assert.go",
		"agent/reflection_skeleton_assert.go",
		"config/BadName.go",
		"task/restore.go",
		"task/handoff_restore_test.go",
	))
	assert.ElementsMatch(t, []string{
		"agent/reflection_skeleton_assert.go=skeleton-variant",
		"agent=skeleton-collision",
		"config/BadName.go=file-name-not-lower-snake",
		"task/handoff_restore_test.go=test-mirror-missing",
	}, got)
}

// TestPackageLayoutPassesACleanTree 钉住 精确镜像、冻结变体后缀、正名骨架文件与豁免树都不误报。
func TestPackageLayoutPassesACleanTree(t *testing.T) {
	got := rulesOf(layoutFiles(
		"agent/loop.go",
		"agent/loop_test.go",
		"agent/skeleton_assert.go",
		"examples/wechat-bot/main.go",
		"examples/wechat-bot/main_real_test.go",
		"examples/contract/plugin/Governor_example.go",
		"prototype/agent.go",
		"prototype/agent_integration_test.go",
		"tests/integration/acceptance/scenario_s01_test.go",
		"bench/throughput_bench_test.go",
		"scripts/codetools/check.go",
		"scripts/codetools/check_test.go",
	))
	require.Empty(t, got)
}

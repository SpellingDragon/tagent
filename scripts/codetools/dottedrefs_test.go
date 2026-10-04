// 本文件负责点号引用门的位置精确性与判定形状：只查会被 import 的位置，未解析即红。
// 规格: docs/comment-gate-tooling.md#dotted-refs
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractModuleRefsOnlyHitsImportedPositions(t *testing.T) {
	cases := []struct {
		path, line string
		want       []string
	}{
		{"run.sh", `python3 -m train.rl.infra.rpc.rpc_server`, []string{"train.rl.infra.rpc.rpc_server"}},
		{"run.sh", `torchrun --nproc_per_node=8 python -m pkg.mod.entry`, []string{"pkg.mod.entry"}},
		{"run.sh", `curl -s -m 3 "$HEALTH"`, nil},
		{"run.sh", `sudo install -m 644 deploy/x.service /etc/systemd/system/`, nil},
		{"run.sh", `AREAL_EXTRA_ARGS="scheduler.type=local" ./run.sh fg`, nil},
		{"a.yaml", `workflow: train.rl.tagent_adapter.TagentARealAdapter`, []string{"train.rl.tagent_adapter.TagentARealAdapter"}},
		{"a.yaml", `  workflow: scripts.areal.bridge`, []string{"scripts.areal.bridge"}},
		{"a.yaml", `workflow: ${SOME_ENV}`, nil},
		{"a.yaml", `scheduler:`, nil},
	}
	for _, c := range cases {
		var got []string
		for _, r := range extractModuleRefs(c.path, c.line, 1) {
			got = append(got, r.module)
		}
		assert.Equal(t, c.want, got, "%s: %s", c.path, c.line)
	}
}

func TestCheckDottedRefsFlagsUnresolvedAndHonoursResolution(t *testing.T) {
	sites := []refSite{
		{"a.yaml", 3, "train.rl.tagent_adapter.TagentARealAdapter"},
		{"run.sh", 7, "scripts.helpers.setup"},
		{"run.sh", 9, "pkg.alive"},
	}
	findings := checkDottedRefs(sites, func(mod string) bool { return mod == "pkg.alive" })
	require.Len(t, findings, 2)
	assert.Contains(t, findings[0], "a.yaml:3: train.rl.tagent_adapter")
	assert.Contains(t, findings[1], "run.sh:7: scripts.helpers.setup")
}

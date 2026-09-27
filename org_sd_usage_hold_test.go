package tagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// sdUsageYAML renders the G1/G2 pair for the deferred-delegation anchor: G1
// routes main → sub1, G2 drops that route (making sub1 unrouted).
func sdUsageYAML(t testing.TB, routeSub1 bool) string {
	t.Helper()
	ref := ""
	if routeSub1 {
		ref = `      - kind: agent
        agent: sub1
        description: "sub1"
`
	}
	// No model/providers section: the host-injected mock serves every agent, which
	// is what lets a real delegation turn run without a live endpoint.
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
%s  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
`, ref, testStore(t, "hottest-sd-usage"))
}

// TestSD_DeferredDelegationIsProtectedByUsageRight is the §3.2 red anchor named
// in the task ("A 持 G1、尚未调用 B 时 G2 删 B，之后 G1 真调 B 仍成功"), per D8:
// a version holds the usage rights of its locally-callable closure INCLUDING the
// B it has not actually called yet, because G1 remains a legitimate caller until
// its own references drain.
//
// Today's sweep asks only the owner about ITS obligations (executions /
// invocations / live tasks). A never-called sub1 is Idle by that measure, so it
// gets closed while G1 still routes to it — the deferred delegation then fails.
// The usage right is not a second task domain (J7) and not a parallel routing
// table: it is derived from the live bindings' own published faces, which are
// already the single routing truth (§4.2).
func TestSD_DeferredDelegationIsProtectedByUsageRight(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdUsageYAML(t, true))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	// delegModel serves a toolless agent with a plain final answer, so a real
	// delegation turn inside sub1 completes instead of reaching a live endpoint.
	entry, err := New(*cfg, WithModel(&delegModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	main := residentCacheForTest(entry)["main"]
	require.NotNil(t, main, "G1：main 常驻")
	require.NotNil(t, residentCacheForTest(entry)["sub1"], "G1：sub1 常驻")

	// A request accepted on G1 — its business turn pins the generation while the
	// model has NOT yet emitted the delegation to sub1.
	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	g1Wrapper := lease.SubagentWrapper("sub1")
	require.NotNil(t, g1Wrapper, "G1 自己的面上必须解析得出 sub1（同一版本真源）")

	// G2 removes the route: sub1 becomes unrouted and enters the retirement ledger.
	write(sdUsageYAML(t, false))
	entry.CheckOrgReload()

	require.Nil(t, entry.ContextManager().SubagentWrapper("sub1"),
		"前提：现效代确实不再路由 sub1")
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"§3.2 红锚：G1 仍保有 sub1 使用权（尚未调用也受保护），sweep 不得提前退役")

	// The deferred delegation itself must still work — host result, not a flag.
	_, err = g1Wrapper.Call(context.Background(), []byte(`{"request":"deferred call from G1"}`))
	require.NoError(t, err, "G1 在 G2 删除路由之后真调 sub1 仍须成功")

	// Boundedness (§4.1/D8): a released usage right must not hold the owner
	// forever. The release itself carries the drain forward — no extra business
	// turn is required (TestSD_ReleaseContinuesRetirementWithoutAnotherTurn pins
	// that without supplying any traffic at all).
	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["sub1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"使用权释放后 sub1 须有界退役（不永久保有）")
}

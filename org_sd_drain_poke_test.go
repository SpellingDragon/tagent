package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// sdOneRouteYAML renders entry main with exactly one routed sub-agent name.
func sdOneRouteYAML(t testing.TB, routed string, store string) string {
	t.Helper()
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: %s
        description: %q
  %s:
    system_prompt:
      inline: %q
    memory:
      type: localfile
      path: %q
`, routed, routed, routed, routed, store)
}

// TestSD_ReleaseContinuesRetirementWithoutAnotherTurn is the §4.3 requirement
// the previous round left on the books: 「释放使用权/任务收尾经原生命周期轻量通知
// 继续退役，不必须再来一个业务 turn」.
//
// A pending retirement that was held ONLY by a usage right gets unblocked by the
// release itself. If the assembly waited for the next business turn to notice,
// an idle org would keep a closed-in-principle owner resident indefinitely — the
// very「invisible 持有」this change set out to remove. So after the last reference
// on the holding generation drains, the owner must exit on its own, bounded, with
// NO traffic of any kind supplied afterwards.
func TestSD_ReleaseContinuesRetirementWithoutAnotherTurn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := testStore(t, "hottest-sd-poke")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdOneRouteYAML(t, "sub1", store))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&delegModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	main := residentCacheForTest(entry)["main"]
	require.NotNil(t, main)

	// One generation pinned by a single turn lease: it is the ONLY holder of sub1
	// (which never gets called, so its own three obligation axes stay zero).
	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	write(sdOneRouteYAML(t, "sub2", store))
	entry.CheckOrgReload()
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"前提：G1 持有使用权时 sub1 不得退役")

	// Release, then supply NOTHING — no turn, no inject, no reload call.
	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["sub1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"§4.3：使用权释放本身须续排退役，不得依赖再来的业务 turn")
}

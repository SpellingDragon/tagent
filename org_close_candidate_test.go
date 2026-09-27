package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sdCloseYAML routes `routed` from main, giving every routed agent its OWN
// localfile store so the final lock state is observable from outside the org.
func sdCloseYAML(t testing.TB, routed []string, storeOf func(name string) string) string {
	t.Helper()
	var tools string
	for _, r := range routed {
		tools += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r)
	}
	defs := ""
	for _, r := range routed {
		defs += fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: localfile\n      path: %q\n", r, r, storeOf(r))
	}
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n    tools:\n" + tools + defs
}

// assertStoreWriterFree proves the owner's store lease was ACTUALLY handed back:
// the single-writer lock on that path can be taken exclusively by this test.
// Releasing the lease is the resource-exit event §4.3/D8 demands on Close —
// "之后恰一次释放组件、store lease 和登记，无需下个用户请求" — and a lock still
// held would mean either a leak or a still-live writer.
func assertStoreWriterFree(t *testing.T, path string) {
	t.Helper()
	probe, err := os.OpenFile(filepath.Join(canonicalize(path), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer probe.Close()
	require.NoError(t, flockExclusive(probe),
		"关闭后 %s 的写锁必须已归还（恰一次释放，不待下一个用户请求）", path)
}

// TestOrgClose_CoversCandidatePublishedDuringDrain is the last piece of §4.3's
// 「最终 org Close 覆盖所有 owner/候选/共享资源」: the orgOwnerCloser is registered
// AFTER the reload stopper precisely because a build still in flight can Add an
// owner right after the closer's snapshot — and then escape the sweep forever.
//
// It parks a candidate build under the reload mutex (the existing test seam),
// starts Close, then lets the build publish. Every owner, including the one that
// landed during the Close drain, must be closed exactly once, and each owner's
// store lease must be genuinely handed back.
func TestOrgClose_CoversCandidatePublishedDuringDrain(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	storeOf := func(name string) string { return filepath.Join(dir, "store-"+name) }
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdCloseYAML(t, []string{"sub1"}, storeOf))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	// Belt for the failure path: a parked build must never survive a bail-out,
	// or the deferred Close would hang the package (the round-76 lesson).
	park := newBuildPark()
	defer park.disarm()
	t.Cleanup(func() { _ = entry.Close() })

	// Structural hot-add; the next business acquire schedules the build, which
	// parks while holding `mu`.
	write(sdCloseYAML(t, []string{"sub1", "sub2"}, storeOf))
	_ = acquireWithin(t, entry, 2*time.Second)
	park.waitEntered(t)

	closed := make(chan error, 1)
	go func() { closed <- entry.Close() }()
	time.Sleep(50 * time.Millisecond) // let Close reach its bounded drain
	park.letGo()

	select {
	case err := <-closed:
		require.NoError(t, err, "org Close must converge on its own")
	case <-time.After(25 * time.Second):
		t.Fatal("org Close never finished with a candidate in flight — the drain or the sweep is unbounded")
	}

	owners := residentCacheForTest(entry)
	require.Contains(t, owners, "sub2",
		"precondition: the drained candidate really published during the Close sequence")
	for _, name := range []string{"sub1", "sub2"} {
		o := owners[name]
		require.NotNilf(t, o, "owner %q must be listed for the sweep", name)
		require.Truef(t, o.CloseStarted(), "owner %q escaped the org sweep — it was never closed", name)
	}
	assertStoreWriterFree(t, storeOf("sub1"))
	assertStoreWriterFree(t, storeOf("sub2"))
}

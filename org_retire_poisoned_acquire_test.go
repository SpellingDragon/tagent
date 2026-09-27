package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sealThePath produces a REAL sealed store, not a mocked error: it drives
// RuntimeResources' own §6.5 rule (an unconfirmed reclaim must HOLD the writer
// flock and seal the path), so afterwards a live single-writer lock genuinely
// sits on that directory. Any org that later tries to open the same path must
// therefore collide with a possibly-half-live backend — the exact condition
// design D8 forbids papering over (「backend/锁退出失败继续原 poisoned 规则，
// 不自动解封」), and the org-side half of §4.3's「真实 poisoned acquire」.
func sealThePath(t *testing.T, rr *RuntimeResources, path string) {
	t.Helper()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, _, err := rr.acquire("localfile", path, fp, func() (openedResource, error) {
		return openedResource{}, fmt.Errorf("%w: kv close hung", ErrReclaimUnconfirmed)
	})
	require.ErrorIs(t, err, ErrReclaimUnconfirmed, "precondition: the seal must come from the real reclaim rule")
}

// assertPathStillSealed checks the seal WITHOUT touching the writer lock: an
// flock probe in the same process would take/convert the lock and release it on
// close (macOS flock semantics — measured: a second `-count` iteration then found
// the path free), so the seal is verified through the registry's own rule instead:
// a re-acquire on a sealed path must be refused with ErrResourcePoisoned without
// ever running open(). A second writer therefore cannot exist, because the single
// writer slot was never handed out.
func assertPathStillSealed(t *testing.T, rr *RuntimeResources, path string) {
	t.Helper()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, _, err := rr.acquire("localfile", path, fp, func() (openedResource, error) {
		t.Error("open must NOT run again on a sealed path")
		return openedResource{}, nil
	})
	require.ErrorIs(t, err, ErrResourcePoisoned, "seal must persist — poisoned paths are never auto-unsealed")
}

func sdPoisonYAML(t testing.TB, routed []string, sealed string) string {
	t.Helper()
	var tools string
	for _, r := range routed {
		tools += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r)
	}
	sub1 := fmt.Sprintf("  sub1:\n    system_prompt:\n      inline: \"PROMPT-sub1\"\n    memory:\n      type: localfile\n      path: %q\n", testStore(t, "sd-poison-sub1"))
	sub2 := fmt.Sprintf("  sub2:\n    system_prompt:\n      inline: \"PROMPT-sub2\"\n    memory:\n      type: localfile\n      path: %q\n", sealed)
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n    tools:\n" +
		tools + sub1 + sub2
}

// TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing is §4.3's
// 「真实 poisoned acquire」at the org boundary: a hot-add whose store path is
// sealed by a live writer must be refused BEFORE any candidate is published, the
// current generation must keep serving intact, and the refusal must not quietly
// expire (no auto-unseal on a later apply).
func TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	sealed := filepath.Join(dir, "sealed-store")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	// The seal lives in its own registry (a different live holder of the path's
	// single-writer lock — cross-process contention modelled in-process).
	sealer := NewRuntimeResources()
	sealThePath(t, sealer, sealed)
	t.Cleanup(func() { assertPathStillSealed(t, sealer, sealed) })

	write(sdPoisonYAML(t, []string{"sub1"}, sealed))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	residentBefore := residentCacheForTest(entry)
	sub1Before := residentBefore["sub1"]
	require.NotNil(t, sub1Before, "baseline: sub1 serves")
	genBefore := entry.OrgDiagnostics()["generation"]

	// Hot-add sub2 on the SEALED path.
	write(sdPoisonYAML(t, []string{"sub1", "sub2"}, sealed))
	entry.CheckOrgReload()

	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"§4.3：poisoned 路径上的热增必须被拒绝，绝不发布半相候选")
	require.Nil(t, residentCacheForTest(entry)["sub2"],
		"被封路径上不得出现第二个写者 owner")
	require.NotContains(t, entryToolNames(entry), "sub2", "旧代的工具面不得被部分改写")
	require.Same(t, sub1Before, residentCacheForTest(entry)["sub1"],
		"沿用旧配置＝同一存活 owner，不是重建的替身")
	require.Same(t, sub1Before.MemStore(), residentCacheForTest(entry)["sub1"].MemStore(),
		"存活 owner 的存储身份不漂移")
	require.False(t, sub1Before.CloseStarted(), "旧代 owner 不能被失败的回退牵连关闭")
	assertPathStillSealed(t, sealer, sealed)

	// Positive proof the check actually ran and was refused on the way to building
	// sub2 — without this, every assertion above would pass vacuously if the reload
	// had simply not reached the hot-add branch at all.
	lf, ok := entry.OrgDiagnostics()["lastFailure"]
	require.True(t, ok && lf != nil, "被拒的热增必须留下可见失败记录（不是静默无操作）")
	require.Contains(t, fmt.Sprint(lf), "sub2", "记录须点出被封的那个名字")
	require.Contains(t, fmt.Sprint(lf), sealed, "and name the path it collided with")

	// 不自动解封：再一次 apply（另一处无关变更）仍须拒绝同一路径。
	write(sdPoisonYAML(t, []string{"sub1", "sub2"}, sealed))
	entry.CheckOrgReload()
	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"poisoned 规则不因后续热更自动解封")
	require.Nil(t, residentCacheForTest(entry)["sub2"])
}

package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// txnYAML renders the S-B fixture: entry main → sub1; hot-add cases inject
// extra referenced agents. failZzz points zzz_probe's memory at /dev/null so
// its store creation fails DETERMINISTICALLY mid-candidate (after aaa_probe,
// which sorts first, has been built successfully) — the refused-candidate
// cleanup path then has TWO acquired responsibilities to unwind.
func txnYAML(t testing.TB, mainModel string, addProbes, failZzz bool) string {
	t.Helper()
	probes := ""
	if addProbes {
		zzzPath := fmt.Sprintf("%q", testStore(t, "hottest-zzz"))
		if failZzz {
			zzzPath = `"/dev/null/zzz-probe-cannot-create"`
		}
		probes = fmt.Sprintf(`      - kind: agent
        agent: aaa_probe
        description: "aaa"
      - kind: agent
        agent: zzz_probe
        description: "zzz"
  aaa_probe:
    system_prompt:
      inline: "aaa"
    memory:
      type: localfile
      path: %q
  zzz_probe:
    system_prompt:
      inline: "zzz"
    memory:
      type: localfile
      path: %s
`, testStore(t, "hottest-aaa"), zzzPath)
	}
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    model: %s
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
%s  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
`, mainModel, probes, testStore(t, "hottest-sub1"))
}

// TestTxn_RefusedCandidateDiscardsInReverseAcquisitionOrder is the S-B red
// anchor: when a candidate acquires responsibilities in a deterministic order
// (sorted newNames: aaa_probe, then zzz_probe) and a LATER step fails
// (zzz_probe's store creation), the discard must unwind in REVERSE acquisition
// order — zzz_probe's partial registration first, then aaa_probe's built agent.
// The map-iteration order of the current diff-based rollbackAdds cannot
// guarantee this; the ordered responsibility table (2.3 事务) makes it a
// contract. Observed via the TEST-only discard-order probe.
func TestTxn_RefusedCandidateDiscardsInReverseAcquisitionOrder(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	write(txnYAML(t, "model-a", false, false))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err, "LoadConfig baseline")

	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err, "New")
	t.Cleanup(func() { _ = entry.Close() })

	write(txnYAML(t, "model-a", true, true)) // hot-add aaa_probe + fail zzz_probe
	entry.CheckOrgReload()

	order := orgLastDiscardOrder()
	require.NotEmpty(t, order,
		"refused candidate must record its discard order (probe) — cleanup ran without the responsibility table")
	// aaa_probe 是唯一完整建成的 owner（zzz 在 store 创建处失败，仅登记）；逆序
	// 要求 zzz 的部分登记先于 aaa 的完整 Close 撤销。
	require.Equal(t, []string{"zzz_probe", "aaa_probe"}, order,
		"S-B: discard must unwind in REVERSE acquisition order (zzz partial first, then aaa)")
}

// TestTxn_RefusedCandidateLeavesNoOwnerOrTableResidue re-asserts the R01 leak
// contract THROUGH the new table path (the old diff-based test stays green, but
// the discard must be rebuilt on the table without losing it).
func TestTxn_RefusedCandidateLeavesNoOwnerOrTableResidue(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	write(txnYAML(t, "model-a", false, false))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	before := entry.StoreOwnerSnapshot()
	write(txnYAML(t, "model-a", true, true))
	entry.CheckOrgReload()

	after := entry.StoreOwnerSnapshot()
	require.Equal(t, before, after,
		"refused candidate must revoke every owner registration it made")
}

// orgLastDiscardOrder moved to its only consumer (2026-09-27 dead-code audit):
// TEST-only introspection of the candidate transaction order.
// orgLastDiscardOrder returns the most recent candidate-discard order (TEST
// introspection only; nil before the first discard).
func orgLastDiscardOrder() []string {
	if v, ok := lastDiscardOrder.Load().([]string); ok {
		return v
	}
	return nil
}

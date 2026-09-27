package tagent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// introduce-durable-workflow-engine §6.4（D4）root 面：装配/热更通路必须把每个
// owner 的「同身份热参快照」轮转到位——新发起子调用私有 CM 的播种源就是它。
// 私有 CM 播种与在途边界应用的机制面在 agent 包 `TestM34_*` 钉死；这里验的是
// reloader 经单一提交点（ApplyOrgHotParams）驱动这条通路后，快照与真消费者同代，
// 且回滚把快照一并恢复（draining-owner 语义由 §4.3 既有测覆盖，勿与此混淆）。

func m34YAML(prompt string, subMax int, subThreshold float64) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"model: test-model\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: " + strconv.Quote(prompt) + "\n" +
		"    memory:\n      type: memory\n" +
		"    tools:\n      - kind: agent\n        agent: sub1\n        description: \"sub1\"\n" +
		"  sub1:\n" +
		"    system_prompt:\n      inline: \"sub1\"\n" +
		"    max_tokens: " + strconv.Itoa(subMax) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(subThreshold, 'f', -1, 64) + "\n" +
		"    keep_recent_tasks: 2\n" +
		"    memory:\n      type: memory\n"
}

func TestM34_SnapshotRotatesWithReloaderCommitPoint(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(m34YAML("A", 4000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	table := residentCacheForTest(entry)
	require.Contains(t, table, "sub1")
	sub1 := table["sub1"]

	// Construction seed at the root: every owner starts with a snapshot equal
	// to its parsed values — the seeding source for sub-invocations exists
	// from second zero, not only after the first hot edit.
	p, ok := sub1.HotSnapshot()
	require.True(t, ok, "New must seed every owner's hot snapshot")
	require.Equal(t, 4000, p.MaxTokens)
	require.InDelta(t, 0.5, p.ThresholdPct, 1e-9)
	require.Equal(t, 2000, sub1.OrgBudgetLine(), "snapshot and resident compressor agree")

	// Numeric-only edit (same prompt/structure): the reloader's per-owner
	// commit point must rotate the SNAPSHOT together with the resident
	// consumer — that rotation is what a freshly-built private CM seeds from.
	write(m34YAML("A", 9000, 0.9))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, di64(t, d, "generation"), "numeric-only: no structural bump")
	require.EqualValues(t, 1, di64(t, d, "revision"))
	require.Equal(t, 8100, sub1.OrgBudgetLine(), "resident consumer took the new values")
	p, ok = sub1.HotSnapshot()
	require.True(t, ok)
	require.Equal(t, 9000, p.MaxTokens, "snapshot rotated at the SAME commit point (D4)")
	require.InDelta(t, 0.9, p.ThresholdPct, 1e-9)

	// Rollback restores the snapshot through the same single point, so the
	// NEXT sub-call seeds the pre-edit generation, not just the resident.
	entry.Rollback()
	p, _ = sub1.HotSnapshot()
	require.Equal(t, 4000, p.MaxTokens, "rollback restores the seeding source")
	require.Equal(t, 2000, sub1.OrgBudgetLine())
}

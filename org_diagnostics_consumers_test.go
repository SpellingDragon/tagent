package tagent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// §5.1（design D9）：诊断必须反映**实际消费者**，不能只证明 resident setter 被调用。
// 逐 agent 回执（payload["agents"]）今天回显的是**请求下发**的那组数值
// （applyHotAll 里的 `p`），而不是从真实消费者读回。本测钉住这条 5.1 独有的、
// 尚未被 §2.4/§6.4 覆盖的契约：一次 numeric-only 热更后，回执所报的
// MaxTokens×ThresholdPct、KeepRecentTasks 必须与该 agent **真实 compressor 消费值**
// 逐一对齐；TaskManager 终态 TTL 这一消费者必须在同一轮 numeric-only 后确实移动；
// 被移除的 draining owner 回执不带 applied 值、且其真实消费者保持最后有效值不被改成默认。
//
// 与既存测的关系（避免重复冒充）：§2.4 已从 config 期望值断言真实预算/TTL；
// §4.3 已从真实消费者断言 draining 的 keepRecent；§3.2/§4.1（d6_lease_test）已证
// InFlightTurns 单计数。本测补齐的是「**回执 ↔ 真实消费者**」这一互证腿。

// d51YAML 渲染 main→sub1 的两 agent 拓扑。四行数值（keep/max/threshold/terminal）
// 全部热可应用（不进 org 指纹），system_prompt 固定 → 只改数值即走 numeric-only 路径。
func d51YAML(keepMain, maxMain int, thrMain float64, termMain string, keepSub, maxSub int, thrSub float64) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: \"diag entry\"\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keepMain) + "\n" +
		"    max_tokens: " + strconv.Itoa(maxMain) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(thrMain, 'f', -1, 64) + "\n" +
		"    task_terminal_ttl: " + strconv.Quote(termMain) + "\n" +
		"    tools:\n      - kind: agent\n        agent: sub1\n        description: \"sub1\"\n" +
		"    memory:\n      type: memory\n" +
		"  sub1:\n" +
		"    system_prompt:\n      inline: \"sub1\"\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keepSub) + "\n" +
		"    max_tokens: " + strconv.Itoa(maxSub) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(thrSub, 'f', -1, 64) + "\n" +
		"    memory:\n      type: memory\n"
}

// d51YAMLNoSub renders main-only (sub1 dropped from BOTH the tool list and the
// agents table). Removing a routed agent is a structural change; held by an
// in-flight reference, sub1's owner stays resident → draining receipt (§4.3).
func d51YAMLNoSub(keepMain, maxMain int, thrMain float64, termMain string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: \"diag entry\"\n" +
		"    keep_recent_tasks: " + strconv.Itoa(keepMain) + "\n" +
		"    max_tokens: " + strconv.Itoa(maxMain) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(thrMain, 'f', -1, 64) + "\n" +
		"    task_terminal_ttl: " + strconv.Quote(termMain) + "\n" +
		"    memory:\n      type: memory\n"
}

// budgetOf mirrors ContextCompressor.BudgetLine() exactly — int(float64(max)*thr)
// — so the receipt↔consumer cross-validation compares like-for-like and is immune
// to the float truncation of a non-exact threshold on either side.
func budgetOf(max int, thr float64) int { return int(float64(max) * thr) }

// d51Receipts reads the per-agent receipt set off the diagnostics payload.
func d51Receipts(t *testing.T, d map[string]any) map[string]OrgAgentApply {
	t.Helper()
	rec, ok := d["agents"].([]OrgAgentApply)
	require.True(t, ok, "per-agent receipts must be observable on the payload")
	byName := map[string]OrgAgentApply{}
	for _, r := range rec {
		byName[r.Name] = r
	}
	return byName
}

// TestD51_ReceiptIsBackedByRealConsumers is the §5.1 core contract: after one
// numeric-only reload of a routed multi-agent org, every applied agent's receipt
// figures must reproduce what its OWN real compressor/TaskManager now uses — not
// merely what the reloader asked for (the receipt today echoes the requested `p`).
func TestD51_ReceiptIsBackedByRealConsumers(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	// Startup: main budget = 4000×0.5 = 2000 (exact), keep 2; sub1 budget = 8000×0.5 = 4000, keep 3.
	write(d51YAML(2, 4000, 0.5, "1m", 3, 8000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, 2000, entry.OrgBudgetLine())
	require.Equal(t, 4000, residentCacheForTest(entry)["sub1"].OrgBudgetLine())

	// One numeric-only apply. main: keep 2→7, max 4000→9000, thr stays 0.5 (budget 4500,
	// exact), terminal 1m→5m. sub1: keep 3→5, max 8000→10000, thr 0.5→0.6 (inexact →
	// validated via the mirrored formula, not a hardcoded number).
	write(d51YAML(7, 9000, 0.5, "5m", 5, 10000, 0.6))
	entry.CheckOrgReload()

	// It really took the numeric-only path: generation frozen, revision advanced.
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, di64(t, d, "generation"), "numeric-only must not bump the structural generation")
	require.NotZero(t, di64(t, d, "revision"), "numeric-only is a full apply")

	byName := d51Receipts(t, d)
	require.Equal(t, "applied", byName["main"].Outcome)
	require.Equal(t, "applied", byName["sub1"].Outcome)

	// === The §5.1 leg: the receipt's REPORTED figures must reproduce the REAL consumer. ===
	// Entry — threshold stays exact, so both the number and the formula are pinned.
	require.Equal(t, 9000, byName["main"].MaxTokens)
	require.InDelta(t, 0.5, byName["main"].ThresholdPct, 1e-9)
	require.Equal(t, 7, byName["main"].KeepRecentTasks)
	require.Equal(t, 7, entry.OrgKeepRecent(), "receipt keep == live compressor keepRecent (real consumer)")
	require.Equal(t, 4500, entry.OrgBudgetLine(), "live compressor moved to 9000×0.5")
	require.Equal(t, budgetOf(byName["main"].MaxTokens, byName["main"].ThresholdPct), entry.OrgBudgetLine(),
		"the receipt's own budget figures must reproduce what the compressor actually uses — 不只证明 setter 被调用")

	// Sub-agent — same cross-validation against ITS OWN compressor (threshold 0.6 is
	// float-inexact, so compare via the mirrored formula rather than a literal).
	sub := residentCacheForTest(entry)["sub1"]
	require.Equal(t, 10000, byName["sub1"].MaxTokens)
	require.InDelta(t, 0.6, byName["sub1"].ThresholdPct, 1e-9)
	require.Equal(t, 5, byName["sub1"].KeepRecentTasks)
	require.Equal(t, 5, sub.OrgKeepRecent(), "sub receipt keep == sub live compressor keepRecent")
	require.Equal(t, budgetOf(byName["sub1"].MaxTokens, byName["sub1"].ThresholdPct), sub.OrgBudgetLine(),
		"a routed sub-agent's receipt budget must match its own compressor, not the entry's")
	require.NotEqual(t, entry.OrgBudgetLine(), sub.OrgBudgetLine(),
		"the two agents really did move to distinct values (guards against a shared/global consumer)")

	// The fifth axis (terminal TTL) has no receipt slot by design (bounded D9 shape);
	// §5.1 still requires it to have really moved at its consumer on this same apply.
	require.Equal(t, 5*time.Minute, entry.TaskManager().TerminalTTL(),
		"task_terminal_ttl reached its real consumer (TaskManager) on this numeric-only apply")
}

// TestD51_DrainingReceiptTracksHeldConsumer proves the drain leg of the same
// cross-validation: an owner this generation no longer routes keeps a receipt that
// carries NO applied value AND its real consumer holds its last effective value
// (not silently re-defaulted) while it drains.
func TestD51_DrainingReceiptTracksHeldConsumer(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(d51YAML(2, 4000, 0.5, "1m", 3, 8000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	sub := residentCacheForTest(entry)["sub1"]
	heldKeep, heldBudget := sub.OrgKeepRecent(), sub.OrgBudgetLine()
	require.Equal(t, 3, heldKeep)
	require.Equal(t, 4000, heldBudget)

	// Make the drain window real: hold an in-flight reference on sub1 so §4.3 keeps
	// its owner resident after it stops being routed (otherwise it retires immediately).
	draining := sub.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer draining.Release()

	// Drop sub1 from the topology while raising main's numerics.
	write(d51YAMLNoSub(7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	byName := d51Receipts(t, d)
	require.Equal(t, "applied", byName["main"].Outcome)
	require.Equal(t, 7, byName["main"].KeepRecentTasks)
	require.Equal(t, 4500, entry.OrgBudgetLine(), "the still-routed entry really moved")

	require.Equal(t, "draining", byName["sub1"].Outcome, "the unrouted owner reports a deliberate no-op")
	require.Zero(t, byName["sub1"].MaxTokens, "a draining receipt carries no applied value")
	require.Zero(t, byName["sub1"].KeepRecentTasks)
	// Its real consumer must still hold the last effective values, not parsed defaults.
	require.Equal(t, heldKeep, sub.OrgKeepRecent(), "draining owner keeps its live keepRecent (not defaulted)")
	require.Equal(t, heldBudget, sub.OrgBudgetLine(), "draining owner keeps its live budget, not defaulted")
}

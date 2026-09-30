// 代际诊断的有界可诊断结果，契约分两层：协调器层（status 的一致快照与拷贝
// 语义）与装配层（拒绝与成功都能经宿主持有的 agent 实例读到，无需重启）。
package tagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// diagYAML renders an org whose entry delegates to `subs`（拓扑由 subs 决定，
// 便于在同一测里做出“结构变更”与“memory 段变更”两种候选）。
func diagYAML(model string, subs ...string) string {
	return diagYAMLMem(model, "      type: memory\n", subs...)
}

// diagYAMLMem additionally overrides the ENTRY agent's memory block: an
// owner-held agent changing its storage must stay refused.
func diagYAMLMem(model, mainMem string, subs ...string) string {
	var b strings.Builder
	b.WriteString("entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n    model: " + model + "\n" +
		"    system_prompt:\n      inline: \"diag entry\"\n    tools:\n")
	for _, s := range subs {
		b.WriteString("      - kind: agent\n        agent: " + s + "\n        description: \"" + s + "\"\n")
	}
	b.WriteString("    memory:\n" + mainMem)
	for _, s := range subs {
		b.WriteString("  " + s + ":\n    model: " + model +
			"\n    system_prompt:\n      inline: \"" + s + "\"\n    memory:\n      type: memory\n")
	}
	return b.String()
}

// TestOrgCoordinator_StatusIsConsistentCopy 钉住 代际状态是一次一致快照，交出的是拷贝。
// - 失败记录带所在代与其 desired；被拒不进序号、不清 effective，成功发布才清除失败记录并记时间；
// - desired 与 fingerprint 分歧是运维可见信号，无新候选时失败记录报当前指纹，不造幻影缺口；
// - 逐 agent 回执整块替换（只保留最近一轮），改返回拷贝改不动内部状态。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestOrgCoordinator_StatusIsConsistentCopy(t *testing.T) {
	c := newOrgCoordinator()
	c.init("fp0", &Config{})

	st := c.status()
	require.Equal(t, int64(0), st.Generation, "init records generation 0")
	require.Equal(t, "fp0", st.Fingerprint)
	require.Nil(t, st.LastFailure, "no rejection recorded yet")
	require.True(t, st.LastApplied.IsZero(), "nothing applied yet")

	c.recordFailure(errSentinel{})
	st = c.status()
	require.NotNil(t, st.LastFailure)
	require.Equal(t, 0, st.LastFailure.Generation, "a rejection locates the generation it was attempted against")
	require.Equal(t, "sentinel", st.LastFailure.Error)
	require.Equal(t, st.Fingerprint, st.LastFailure.Desired,
		"without a fresh candidate in this cycle the failure reports the current fingerprint — no phantom gap")
	require.Equal(t, int64(0), st.Generation, "a rejection does not advance the generation")

	c.noteDesired("fp9")
	c.recordFailure(errSentinel{})
	st = c.status()
	require.Equal(t, "fp9", st.LastFailure.Desired)
	require.NotEqual(t, st.Fingerprint, st.LastFailure.Desired, "desired ≠ effective is the ops-facing signal")

	st.LastFailure.Error = "tampered"
	require.Equal(t, "sentinel", c.status().LastFailure.Error, "status hands out a copy, not the live record")

	_, gen := c.swap("fp1", &Config{}, nil)
	st = c.status()
	require.Equal(t, int64(1), st.Generation)
	require.Equal(t, "fp1", st.Fingerprint)
	require.Nil(t, st.LastFailure, "a successful publish clears the rejection record")
	require.Equal(t, st.Fingerprint, st.Desired, "success converges desired onto effective — no phantom pending change")
	require.False(t, st.LastApplied.IsZero())
	require.Equal(t, 1, gen.seq)

	c.recordApply([]OrgAgentApply{{Name: "a", Outcome: "applied"}, {Name: "b", Outcome: "draining"}})
	require.Len(t, c.status().Agents, 2)
	c.recordApply([]OrgAgentApply{{Name: "a", Outcome: "applied"}})
	st = c.status()
	require.Len(t, st.Agents, 1, "the receipt set is replaced wholesale — no unbounded history")
	st.Agents[0].Outcome = "tampered"
	require.Equal(t, "applied", c.status().Agents[0].Outcome, "status hands out a copy of the receipts too")
}

// TestOrgDiagnostics_FingerprintLabelIsBounded 钉住 指纹在诊断里只作截断标签，够对齐哪一次候选、不够当业务键。
// - 执行路径不得据截断指纹选版：它不是应用可见的身份（resident-continuity）。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestOrgDiagnostics_FingerprintLabelIsBounded(t *testing.T) {
	c := newOrgCoordinator()
	long := strings.Repeat("a", 64)
	c.init(long, &Config{})
	require.Len(t, c.status().Fingerprint, 8, "diagnostic label is the truncated fingerprint")
}

// TestOrgDiagnostics_EndToEnd 钉住 被拒候选与成功发布都经同一诊断访问器可观测，序号只随成功前进。
// - 载荷有界：字段是约定的固定集合，实时欠账组与关闭态各自成组、不冒充提交记录的原子快照；
// - 拒绝可具名到被拒的那次候选（报其 desired、不前进序号），成功发布前进序号并清除失败记录；
// - 逐 agent 回执与引用面就在同一载荷上，无需第二套抓取协议。
// - 每次写入都把 mtime 显式推进 2s：文件系统时间戳粒度会吞掉快速连续写入，不严格变新则换代不会发生。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestOrgDiagnostics_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(diagYAML("gpt-x", "helper"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	payload := func() map[string]any {
		t.Helper()
		p := entry.OrgDiagnostics()
		require.NotNil(t, p, "the assembly layer must register a diagnostics provider")
		_, err := json.Marshal(p)
		require.NoError(t, err, "the payload must be diagnostic-serializable as-is")
		for k := range p {
			switch k {
			case "generation", "revision", "fingerprint", "desired", "agents", "configPath", "lastAppliedAt", "lastPublishedAt", "lastFailure", "liveDebt", "close":
			default:
				t.Fatalf("unexpected diagnostics key %q — the payload is bounded by contract", k)
			}
		}
		return p
	}

	require.Equal(t, int64(0), payload()["generation"])

	entry.CheckOrgReload()
	st := payload()
	require.Equal(t, int64(0), st["generation"], "the startup generation stays 0")
	require.NotEmpty(t, st["fingerprint"], "the first check records the effective fingerprint")
	require.Nil(t, st["lastFailure"])

	write("entry: [broken")
	entry.CheckOrgReload()
	st = payload()
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "a rejection must surface a structured failure record, got %T", st["lastFailure"])
	require.Contains(t, fail.Error, "config parse")
	require.Equal(t, 0, fail.Generation)
	require.Empty(t, fail.Desired, "a config that cannot be parsed has no desired fingerprint — no phantom claim")
	require.Equal(t, int64(0), st["generation"], "a rejected candidate never advances the generation")

	write(diagYAML("gpt-w", "helper"))
	entry.CheckOrgReload()
	st = payload()
	require.Equal(t, int64(1), st["generation"], "the successful reload is generation 1")
	require.Nil(t, st["lastFailure"], "success clears the stale rejection record")
	require.NotNil(t, st["lastAppliedAt"])
	require.Equal(t, st["fingerprint"], st["desired"],
		"after a successful publish there must be no leftover desired-vs-effective gap")

	rec, ok := st["agents"].([]OrgAgentApply)
	require.True(t, ok, "per-agent receipts must be observable")
	outcomes := map[string]string{}
	for _, r := range rec {
		outcomes[r.Name] = r.Outcome
	}
	require.Equal(t, "applied", outcomes["main"], "the entry reports its own application")
	require.Equal(t, "applied", outcomes["helper"], "every routed agent reports its own application")
	debt, ok := st["liveDebt"].(OrgLiveDebt)
	require.True(t, ok, "the live reference debt must be its own declared group")
	require.False(t, debt.CapturedAt.IsZero(), "a live read must say when it was taken (§5.1: 不冒充记录快照)")
	refs := debt.Executors
	require.Zero(t, refs.PendingRetirees, "nothing is in flight here, so the superseded runner is already reclaimed")

	write(diagYAMLMem("gpt-w", "      type: localfile\n      path: diag-moved\n", "helper", "late"))
	entry.CheckOrgReload()
	st = payload()
	fail, ok = st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "a refused candidate must be diagnosable")
	require.Contains(t, fail.Error, "memory section changed")
	require.NotEmpty(t, fail.Desired, "the refusal must say WHICH candidate it refused")
	require.NotEqual(t, st["fingerprint"], fail.Desired,
		"desired ≠ effective is the direct evidence behind 「我改了为什么没生效」")
	require.Equal(t, int64(1), st["generation"], "the refusal keeps serving generation 1")
}

// hotParamsYAML 渲染 main→sub1 的两 agent 拓扑。四行数值（keep/max/threshold/terminal）
// 全部热可应用（不进 org 指纹），system_prompt 固定 → 只改数值即走 numeric-only 路径。
func hotParamsYAML(keepMain, maxMain int, thrMain float64, termMain string, keepSub, maxSub int, thrSub float64) string {
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

// hotParamsYAMLNoSub renders main-only (sub1 dropped from BOTH the tool list and the
// agents table). Removing a routed agent is a structural change; held by an
// in-flight reference, sub1's owner stays resident → draining receipt.
func hotParamsYAMLNoSub(keepMain, maxMain int, thrMain float64, termMain string) string {
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

// diagnosticsReceipts reads the per-agent receipt set off the diagnostics payload.
func diagnosticsReceipts(t *testing.T, d map[string]any) map[string]OrgAgentApply {
	t.Helper()
	rec, ok := d["agents"].([]OrgAgentApply)
	require.True(t, ok, "per-agent receipts must be observable on the payload")
	byName := map[string]OrgAgentApply{}
	for _, r := range rec {
		byName[r.Name] = r
	}
	return byName
}

// TestReceiptIsBackedByRealConsumers 钉住 数值热更后每个 applied 回执所报数字等于该 agent 真实消费者现用的值，而非请求下发值的回声。
// - 预算线、keepRecent 逐一对齐其 live compressor 的实际消费；阈值非精确时用镜像公式比，不写死数字；
// - 路由子用其自己的压缩器，两个 agent 取到不同值——共享一个消费者就会露馅；
// - 无回执槽的轴（terminal TTL）在同一轮数值热更后于其消费者处证明确实移动。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestReceiptIsBackedByRealConsumers(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(hotParamsYAML(2, 4000, 0.5, "1m", 3, 8000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, 2000, entry.OrgBudgetLine())
	require.Equal(t, 4000, residentCacheForTest(entry)["sub1"].OrgBudgetLine())

	write(hotParamsYAML(7, 9000, 0.5, "5m", 5, 10000, 0.6))
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"), "numeric-only must not bump the structural generation")
	require.NotZero(t, diagInt64(t, d, "revision"), "numeric-only is a full apply")

	byName := diagnosticsReceipts(t, d)
	require.Equal(t, "applied", byName["main"].Outcome)
	require.Equal(t, "applied", byName["sub1"].Outcome)

	require.Equal(t, 9000, byName["main"].MaxTokens)
	require.InDelta(t, 0.5, byName["main"].ThresholdPct, 1e-9)
	require.Equal(t, 7, byName["main"].KeepRecentTasks)
	require.Equal(t, 7, entry.OrgKeepRecent(), "receipt keep == live compressor keepRecent (real consumer)")
	require.Equal(t, 4500, entry.OrgBudgetLine(), "live compressor moved to 9000×0.5")
	require.Equal(t, budgetOf(byName["main"].MaxTokens, byName["main"].ThresholdPct), entry.OrgBudgetLine(),
		"the receipt's own budget figures must reproduce what the compressor actually uses — 不只证明 setter 被调用")

	sub := residentCacheForTest(entry)["sub1"]
	require.Equal(t, 10000, byName["sub1"].MaxTokens)
	require.InDelta(t, 0.6, byName["sub1"].ThresholdPct, 1e-9)
	require.Equal(t, 5, byName["sub1"].KeepRecentTasks)
	require.Equal(t, 5, sub.OrgKeepRecent(), "sub receipt keep == sub live compressor keepRecent")
	require.Equal(t, budgetOf(byName["sub1"].MaxTokens, byName["sub1"].ThresholdPct), sub.OrgBudgetLine(),
		"a routed sub-agent's receipt budget must match its own compressor, not the entry's")
	require.NotEqual(t, entry.OrgBudgetLine(), sub.OrgBudgetLine(),
		"the two agents really did move to distinct values (guards against a shared/global consumer)")

	require.Equal(t, 5*time.Minute, entry.TaskManager().TerminalTTL(),
		"task_terminal_ttl reached its real consumer (TaskManager) on this numeric-only apply")
}

// TestDrainingReceiptTracksHeldConsumer 钉住 本代不路由的属主：回执为 draining 且数值字段为零，其真实消费者保持末次生效值。
// - 用真实在途引用（租约）造成排水窗口，使其属主留驻；
// - draining 回执不携带 applied 值，含义是本轮未评估，零配置是另一种事实；
// - 该属主的 live keepRecent 与预算仍是末次生效值，不被重新默认。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestDrainingReceiptTracksHeldConsumer(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(hotParamsYAML(2, 4000, 0.5, "1m", 3, 8000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	sub := residentCacheForTest(entry)["sub1"]
	heldKeep, heldBudget := sub.OrgKeepRecent(), sub.OrgBudgetLine()
	require.Equal(t, 3, heldKeep)
	require.Equal(t, 4000, heldBudget)

	draining := sub.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer draining.Release()

	write(hotParamsYAMLNoSub(7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	byName := diagnosticsReceipts(t, d)
	require.Equal(t, "applied", byName["main"].Outcome)
	require.Equal(t, 7, byName["main"].KeepRecentTasks)
	require.Equal(t, 4500, entry.OrgBudgetLine(), "the still-routed entry really moved")

	require.Equal(t, "draining", byName["sub1"].Outcome, "the unrouted owner reports a deliberate no-op")
	require.Zero(t, byName["sub1"].MaxTokens, "a draining receipt carries no applied value")
	require.Zero(t, byName["sub1"].KeepRecentTasks)
	require.Equal(t, heldKeep, sub.OrgKeepRecent(), "draining owner keeps its live keepRecent (not defaulted)")
	require.Equal(t, heldBudget, sub.OrgBudgetLine(), "draining owner keeps its live budget, not defaulted")
}

// routedSub2YAML renders main→(sub1,sub2) with sub2→leaf. sub2's own numeric knobs are
// parameters so a structural publish can introduce it with values that differ from
// the host's on every axis under test (budget inputs AND task TTL).
func routedSub2YAML(routeSub2 bool, keepSub2, maxSub2 int, thrSub2 float64, ttlSub2 string, ttlMain string) string {
	sub2Ref := ""
	if routeSub2 {
		sub2Ref = "      - kind: agent\n        agent: sub2\n        description: \"delegate-sub2\"\n"
	}
	sub2Def := ""
	if routeSub2 {
		sub2Def = "  sub2:\n" +
			"    system_prompt:\n      inline: \"SUB2-DIAG\"\n" +
			"    keep_recent_tasks: " + strconv.Itoa(keepSub2) + "\n" +
			"    max_tokens: " + strconv.Itoa(maxSub2) + "\n" +
			"    compress_threshold: " + strconv.FormatFloat(thrSub2, 'f', -1, 64) + "\n" +
			"    task_default_ttl: " + strconv.Quote(ttlSub2) + "\n" +
			"    memory:\n      type: memory\n" +
			"    tools:\n      - kind: agent\n        agent: leaf\n        description: \"delegate-leaf\"\n"
	}
	return "entry: main\nagents:\n  main:\n" +
		"    system_prompt:\n      inline: \"MAIN-DIAG\"\n" +
		"    keep_recent_tasks: 2\n    max_tokens: 4000\n    compress_threshold: 0.5\n" +
		"    task_default_ttl: " + strconv.Quote(ttlMain) + "\n" +
		"    memory:\n      type: memory\n" +
		"    tools:\n      - kind: agent\n        agent: sub1\n        description: \"delegate-sub1\"\n" + sub2Ref +
		"  sub1:\n    system_prompt:\n      inline: \"SUB1-DIAG\"\n    memory:\n      type: memory\n" +
		sub2Def +
		"  leaf:\n    system_prompt:\n      inline: \"LEAF-DIAG\"\n    memory:\n      type: memory\n"
}

func writeRoutedConfig(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// liveDebtOf / closeOf read the two cross-validation groups off the payload: each
// group carries its own collection instant, so a reading is never a snapshot stitched from several unlocked getters.
func liveDebtOf(t *testing.T, d map[string]any) OrgLiveDebt {
	t.Helper()
	debt, ok := d["liveDebt"].(OrgLiveDebt)
	require.True(t, ok, "the live reference debt must be reported as its own group")
	return debt
}

func closeOf(t *testing.T, d map[string]any) OrgCloseState {
	t.Helper()
	st, ok := d["close"].(OrgCloseState)
	require.True(t, ok, "the close phase must be reported as its own group")
	return st
}

// TestHotAddedOwnerReceiptMatchesRealConsumption 钉住 结构发布新增的 owner：回执等于其自身真实消费者的值，TTL 取其自己记录里的解析。
// - 回执预算对齐其 live compressor，且两个 agent 取到不同值（共享消费者会露馅）；
// - 它真实派生的子任务收到的是它自己的 manager 默认 TTL——非宿主的、非内置默认、非 setter 回声；
// - 同轮另一新增 owner 无 TTL 配置，取到第三个不同值，证明数值按属主分离而非广播。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestHotAddedOwnerReceiptMatchesRealConsumption(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	writeRoutedConfig(t, yamlPath, routedSub2YAML(false, 0, 0, 0, "3m", "9m"), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &chainDelegModel{prefer: []string{"sub2", "sub1", "leaf"}}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Nil(t, residentCacheForTest(entry)["sub2"], "precondition: sub2 has no owner before it is routed")

	writeRoutedConfig(t, yamlPath, routedSub2YAML(true, 7, 7000, 0.75, "3m", "9m"), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.NotZero(t, diagInt64(t, d, "generation"), "routing a new owner is structural: the generation advanced")

	rec := diagnosticsReceipts(t, d)
	sub2Rec, ok := rec["sub2"]
	require.Truef(t, ok, "the newly added owner must carry its own receipt (got %v)", rec["sub2"])
	require.Equal(t, "applied", sub2Rec.Outcome)
	require.Equal(t, 7000, sub2Rec.MaxTokens)
	require.Equal(t, 7, sub2Rec.KeepRecentTasks)

	sub2 := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, sub2)
	require.Equal(t, budgetOf(sub2Rec.MaxTokens, sub2Rec.ThresholdPct), sub2.OrgBudgetLine(),
		"§5.1：回执报的预算必须等于新 owner **自己真实 compressor** 的消费值，而非请求下发值的回声")
	require.Equal(t, 7, sub2.OrgKeepRecent(), "and the same for keepRecent at the live compressor")
	require.NotEqual(t, entry.OrgBudgetLine(), sub2.OrgBudgetLine(),
		"两个 agent 确实取到了不同的值（否则共享一个消费者也能通过本断言）")

	out, err := entry.StartLoop("u", "d53-receipt-consumer")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("do the work"))
	require.NoError(t, err)
	waitFor(t, "sub2 really delegated and adopted the run on its OWN board", func() bool {
		for _, tk := range sub2.TaskManager().List() {
			if tk.Spec.Kind == "subagent" {
				return true
			}
		}
		return false
	})

	// The spawned task carries no per-task override (TTL 0 = "inherit MY owner's
	// manager default" by design), so its lifetime is governed by sub2's own
	// resolved value — read it at that consumer's boundary.
	var specTTL time.Duration
	for _, tk := range sub2.TaskManager().List() {
		if tk.Spec.Kind == "subagent" {
			specTTL = tk.Spec.TTL
			break
		}
	}
	require.Zero(t, specTTL, "the subagent spawn inherits its owner's manager default by design (0 = no override)")
	require.Equal(t, 3*time.Minute, sub2.TaskManager().DefaultTTL(),
		"§5.1：热新增 owner 的 TTL 消费者必须解析到它自己的已提交记录（9m 是宿主的，10m 是内置默认）")

	leaf := residentCacheForTest(entry)["leaf"]
	require.NotNil(t, leaf)
	require.Equal(t, 10*time.Minute, leaf.TaskManager().DefaultTTL(),
		"the other new owner keeps the configured default — proving the values are per-owner, not a broadcast")
	require.Equal(t, 9*time.Minute, entry.TaskManager().DefaultTTL(), "the host's own value is a third distinct figure")
	require.Equal(t, budgetOf(7000, 0.75), sub2.OrgBudgetLine())

	debt := liveDebtOf(t, entry.OrgDiagnostics())
	require.False(t, debt.CapturedAt.IsZero(), "实时债务组必须自带采集时刻")
	require.GreaterOrEqual(t, debt.Executors.InFlightTurns, int64(0))
}

// TestCloseInitiatedIsDistinguishableFromResourcesExited 钉住 「关闭已发起」与「资源已退出」两个事实必须可区分。
// - 仍有引用持住时 Close 有界返回，载荷随即报 initiated、未 exited；
// - 收尾的延后尾巴跑完才翻到 exited；未发起过时该对读作（未发起、已退出）；
// - 造该状态用的是真实在途引用（租约），走的是回收门同一套账面。
// 契约: docs/wiki/platform/org-hot-reload.md#diagnostics
func TestCloseInitiatedIsDistinguishableFromResourcesExited(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeRoutedConfig(t, yamlPath, routedSub2YAML(true, 5, 5000, 0.5, "4m", "9m"), &tick)

	entry := bootForDiagnostics(t, yamlPath)

	pristine := closeOf(t, entry.OrgDiagnostics())
	require.False(t, pristine.Initiated, "precondition: nothing has been closed yet")
	require.Truef(t, pristine.ResourcesExited,
		"precondition: nothing was ever deferred, so an unclosed owner is trivially not-stuck (got %+v)", pristine)

	held := entry.ContextManager().AcquireLease(agent.LeaseBackground)
	during := closeOf(t, entry.OrgDiagnostics())
	require.False(t, during.Initiated, "a held reference is not a close")
	require.Falsef(t, during.ResourcesExited,
		"work is outstanding, so the teardown cannot be reported as done (got %+v)", during)

	closed := make(chan error, 1)
	go func() { closed <- entry.Close() }()

	var mid OrgCloseState
	waitFor(t, "Close has been initiated while the reference is still held", func() bool {
		mid = closeOf(t, entry.OrgDiagnostics())
		return mid.Initiated && !mid.ResourcesExited
	})
	require.Falsef(t, mid.ResourcesExited,
		"§5.1：有界返回不得被读成收尾完成（已发起=%v 已退出=%v）", mid.Initiated, mid.ResourcesExited)

	held.Release()
	var fin OrgCloseState
	waitFor(t, "the deferred tail took every remaining exit", func() bool {
		fin = closeOf(t, entry.OrgDiagnostics())
		return fin.Initiated && fin.ResourcesExited
	})
	require.NoError(t, <-closed)
}

func bootForDiagnostics(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&chainDelegModel{prefer: []string{"sub2", "leaf"}}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

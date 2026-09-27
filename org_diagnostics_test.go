package tagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// §5.1（design D9）：代际诊断的有界可诊断结果。契约两层——
// 协调器层（status 的一致快照 + 拷贝语义）与装配层（拒绝/成功都能经宿主持有的
// agent 实例读到，无需重启、无需新抓取协议）。

// diagYAML renders an org whose entry delegates to `subs`（拓扑由 subs 决定，
// 便于在同一测里做出“结构变更”与“memory 段变更”两种候选）。
func diagYAML(model string, subs ...string) string {
	return diagYAMLMem(model, "      type: memory\n", subs...)
}

// diagYAMLMem additionally overrides the ENTRY agent's memory block (§4.3: an
// owner-held agent changing its storage must stay refused).
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

// TestOrgCoordinator_StatusIsConsistentCopy 钉 status 的三条簿记语义：
// 失败记录带所在代且不清空 effective；成功发布清除失败记录并记时间；返回的是
// 拷贝（调用方改不动内部状态）。
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

	// A rejection must be able to say WHICH candidate it refused (D9: desired fingerprint).
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

	// 逐 agent 回执：整块替换（只保留最近一轮，无历史累积）且交出的是拷贝。
	c.recordApply([]OrgAgentApply{{Name: "a", Outcome: "applied"}, {Name: "b", Outcome: "draining"}})
	require.Len(t, c.status().Agents, 2)
	c.recordApply([]OrgAgentApply{{Name: "a", Outcome: "applied"}})
	st = c.status()
	require.Len(t, st.Agents, 1, "the receipt set is replaced wholesale — no unbounded history")
	st.Agents[0].Outcome = "tampered"
	require.Equal(t, "applied", c.status().Agents[0].Outcome, "status hands out a copy of the receipts too")
}

// shortFingerprintIsBounded 是 guardrail 的一部分：诊断标签被截断，够对齐“哪一次
// 候选”，不够被误当业务键（resident-continuity：指纹不是应用可见 identity）。
func TestOrgDiagnostics_FingerprintLabelIsBounded(t *testing.T) {
	c := newOrgCoordinator()
	long := strings.Repeat("a", 64)
	c.init(long, &Config{})
	require.Len(t, c.status().Fingerprint, 8, "diagnostic label is the truncated fingerprint")
}

// TestOrgDiagnostics_EndToEnd 通过生产入口（tagent.New + CheckOrgReload）验证：
// 一次被拒的候选与一次成功的发布都在**同一个** agent 诊断访问器上可观测，且代际
// 序号只随成功前进。
func TestOrgDiagnostics_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		// Strictly increasing mtime: FS granularity can swallow rapid writes.
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
			// §5.1: the committed-record fields stay flat, while the LIVE
			// reference debt and the close phase are their own groups — a reader
			// must not be able to mistake an instantaneous read for part of the
			// atomic status() snapshot.
			case "generation", "revision", "fingerprint", "desired", "agents", "configPath", "lastAppliedAt", "lastPublishedAt", "lastFailure", "liveDebt", "close":
			default:
				t.Fatalf("unexpected diagnostics key %q — the payload is bounded by contract", k)
			}
		}
		return p
	}

	// 0) before any check the coordinator has no recorded generation.
	require.Equal(t, int64(0), payload()["generation"])

	entry.CheckOrgReload()
	st := payload()
	require.Equal(t, int64(0), st["generation"], "the startup generation stays 0")
	require.NotEmpty(t, st["fingerprint"], "the first check records the effective fingerprint")
	require.Nil(t, st["lastFailure"])

	// 1) a rejected candidate is observable WITH its generation and reason.
	write("entry: [broken")
	entry.CheckOrgReload()
	st = payload()
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "a rejection must surface a structured failure record, got %T", st["lastFailure"])
	require.Contains(t, fail.Error, "config parse")
	require.Equal(t, 0, fail.Generation)
	require.Empty(t, fail.Desired, "a config that cannot be parsed has no desired fingerprint — no phantom claim")
	require.Equal(t, int64(0), st["generation"], "a rejected candidate never advances the generation")

	// 2) a successful publish clears the failure and moves to the next generation.
	write(diagYAML("gpt-w", "helper")) // model is fingerprinted -> structural change
	entry.CheckOrgReload()
	st = payload()
	require.Equal(t, int64(1), st["generation"], "the successful reload is generation 1")
	require.Nil(t, st["lastFailure"], "success clears the stale rejection record")
	require.NotNil(t, st["lastAppliedAt"])
	require.Equal(t, st["fingerprint"], st["desired"],
		"after a successful publish there must be no leftover desired-vs-effective gap")

	// 2b) 逐 agent 回执与引用面就在同一 payload 上（D9：不新增抓取协议）。
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

	// 3) a candidate that BOTH hot-adds an agent and changes an owner-held agent's
	// storage stays refused (memory migration), and the refusal is diagnosable.
	// (Before §4.3 this step used “add an agent” as the refusal — that shape now
	// publishes, so the refusal has to come from the rule that still holds.)
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

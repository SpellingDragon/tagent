package tagent

// 轮一百零三（evidence §5.59）：§5.1 剩下的两条腿。
//
//	②「热增／结构发布后的真实子调用预算与 TTL（非 getter 回声）」——
//	  `TestD51_ReceiptIsBackedByRealConsumers` 钉的是**已路由**拓扑上的 numeric-only
//	  应用；这里补的是**刚被结构发布新增的 owner**：它的回执数字必须等于它自己
//	  真实消费者的值，且它**真实派生**的任务拿到的是**它自己**记录里的 TTL
//	  （不是宿主的、不是默认值、也不是 setter 被调用的回声）。
//	③「关闭已发起」与「资源已退出」必须可区分——§4.1 的有界返回不等于收尾完成。
//	  用真实的在途引用（租约）造成该状态，而不是自造阻塞 closer。
//
// 两条都同时读**载荷形状**本身：`liveDebt`（自带采集时刻的实时债务组）与 `close`
// （两态分离）是本轮按 §5.1 立的新契约，断言即钉住「不把多次无锁 getter 拼成
// 原子成功快照」的正向表达。

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// d53YAML renders main→(sub1,sub2) with sub2→leaf. sub2's own numeric knobs are
// parameters so a structural publish can introduce it with values that differ from
// the host's on every axis under test (budget inputs AND task TTL).
func d53YAML(routeSub2 bool, keepSub2, maxSub2 int, thrSub2 float64, ttlSub2 string, ttlMain string) string {
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

func d53Write(t *testing.T, path, content string, tick *time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, *tick, *tick))
}

// liveDebtOf / closeOf read the two §5.1 groups off the payload.
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

// TestD53_HotAddedOwnerReceiptMatchesRealConsumption pins leg ②: after a
// STRUCTURAL publish that introduces a new owner, that owner's receipt figures
// must reproduce (a) its own live compressor's budget line and (b) the TTL a task
// it REALLY spawned received — the value the record says for THAT agent, not the
// host's and not a default.
func TestD53_HotAddedOwnerReceiptMatchesRealConsumption(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	d53Write(t, yamlPath, d53YAML(false, 0, 0, 0, "3m", "9m"), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &chainDelegModel{prefer: []string{"sub2", "sub1", "leaf"}}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Nil(t, residentCacheForTest(entry)["sub2"], "precondition: sub2 has no owner before it is routed")

	// Structural publish: route sub2, giving it numbers distinct from the host on
	// every axis under test (7×0.75=5 vs host 4000×0.5=2000; TTL 3m vs 9m).
	d53Write(t, yamlPath, d53YAML(true, 7, 7000, 0.75, "3m", "9m"), &tick)
	entry.CheckOrgReload()

	d := entry.OrgDiagnostics()
	require.NotZero(t, di64(t, d, "generation"), "routing a new owner is structural: the generation advanced")

	rec := d51Receipts(t, d)
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

	// The TTL axis has no receipt slot by design (bounded D9 shape), so it is
	// proven where it is actually consumed: a task sub2 really spawns.
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

	// Non-echo guard: the SAME publish installed leaf too, whose config sets no TTL
	// at all — so a global setter broadcast could not have produced two different
	// per-owner values in one round.
	leaf := residentCacheForTest(entry)["leaf"]
	require.NotNil(t, leaf)
	require.Equal(t, 10*time.Minute, leaf.TaskManager().DefaultTTL(),
		"the other new owner keeps the configured default — proving the values are per-owner, not a broadcast")
	require.Equal(t, 9*time.Minute, entry.TaskManager().DefaultTTL(), "the host's own value is a third distinct figure")
	require.Equal(t, budgetOf(7000, 0.75), sub2.OrgBudgetLine())

	// And the live-debt group reports itself as live, not as part of the record.
	debt := liveDebtOf(t, entry.OrgDiagnostics())
	require.False(t, debt.CapturedAt.IsZero(), "实时债务组必须自带采集时刻")
	require.GreaterOrEqual(t, debt.Executors.InFlightTurns, int64(0))
}

// TestD53_CloseInitiatedIsDistinguishableFromResourcesExited pins leg ③: while a
// reference is still held, Close returns BOUNDED (§4.1) — the payload must then
// say "initiated, not exited", and only flip to exited once the deferred tail ran.
// The unfinished work is a real in-flight reference (a lease), not a synthetic
// blocking closer, so this exercises the same accounting the reclaim gate reads.
func TestD53_CloseInitiatedIsDistinguishableFromResourcesExited(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	d53Write(t, yamlPath, d53YAML(true, 5, 5000, 0.5, "4m", "9m"), &tick)

	entry := d53Boot(t, yamlPath)

	// BEFORE anything is outstanding: no close was ever issued, so the tail has
	// nothing to wait for and the pair reads (false, true).
	pristine := closeOf(t, entry.OrgDiagnostics())
	require.False(t, pristine.Initiated, "precondition: nothing has been closed yet")
	require.Truef(t, pristine.ResourcesExited,
		"precondition: nothing was ever deferred, so an unclosed owner is trivially not-stuck (got %+v)", pristine)

	// Hold a real reference on the entry's owner: Close must return before the
	// resources are gone.
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

func d53Boot(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&chainDelegModel{prefer: []string{"sub2", "leaf"}}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

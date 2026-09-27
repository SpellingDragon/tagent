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

// scHotAddNumericYAML: entry main → sub1; sub3 block controlled by addSub3.
// B's max_tokens/keep_recent move between renders (numeric-only for sub3).
func scHotAddNumericYAML(t testing.TB, sub3 bool, sub3MaxTokens int, sub1Keep int) string {
	t.Helper()
	sub3Tool, sub3Block := "", ""
	if sub3 {
		sub3Tool = `      - kind: agent
        agent: sub3
        description: "sub3"
`
		sub3Block = fmt.Sprintf(`  sub3:
    system_prompt:
      inline: "sub3"
    max_tokens: %d
    memory:
      type: localfile
      path: %q
`, sub3MaxTokens, testStore(t, "hottest-sc3"))
	}
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
%s  sub1:
    system_prompt:
      inline: "sub1"
    keep_recent_tasks: %d
    memory:
      type: localfile
      path: %q
%s`, sub3Tool, sub1Keep, testStore(t, "hottest-sc1"), sub3Block)
}

// TestSC_HotAddedAgentNumericOnlySeedsNextCall is the S-C red anchor 1
// witness: after a structural hot-add of sub3, a FOLLOW-UP numeric-only edit of
// sub3's max_tokens must reach sub3's FRESH sub-calls (seeded from the rotated
// owner snapshot at the reloader's single commit point). The mechanism
// (applyHotAll over routable residents, which now includes the hot-added owner)
// should already satisfy this — this test PINS it for the hot-add path, which
// the m34 family only covered for cold-start agents.
func TestSC_HotAddedAgentNumericOnlySeedsNextCall(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	// 基线：无 sub3。
	write(scHotAddNumericYAML(t, false, 0, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	// 结构热增 sub3（max_tokens=4096 初值）。
	write(scHotAddNumericYAML(t, true, 4096, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "热增须发布（否则后续断言空洞）")

	// numeric-only：sub3 max_tokens 4096→8100（sub1 keep 不动→不轮转指纹的其他轴）。
	write(scHotAddNumericYAML(t, true, 8100, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "numeric-only 不加代")

	// 断言：热增 owner 的 effective 快照已随提交点轮转到新值——真实消费者读取
	// （OrgBudgetLine 读 compressor 真值，非 getter 回声）。
	table := residentCacheForTest(entry)
	sub3 := table["sub3"]
	require.NotNil(t, sub3, "sub3 须在常驻表")
	// OrgBudgetLine = maxTokens × threshold（0.8 默认）——8100 到达即 6480。
	require.Equal(t, 6480, sub3.OrgBudgetLine(),
		"S-C 锚1：热增→numeric-only 后，热增 owner 的新调用种子须读新值（8100×0.8；提交点单写者轮转）")
}

// scHot reads an owner's record-backed hot bundle (HotSnapshot resolves through
// the injected currentHotFor, i.e. the single committed application record; it
// falls back to the construction snapshot only with no record entry).
func scHot(t *testing.T, a *agent.TagentAgent) agent.OrgHotParams {
	t.Helper()
	require.NotNil(t, a, "owner 须在常驻表")
	p, ok := a.HotSnapshot()
	require.True(t, ok, "HotSnapshot 须可解析（记录源或构造快照）")
	return p
}

// TestSC_RecordCommitsAtomicallyWithVersion pins S-C's record-commit contract:
// the applied record rotates ONLY at the single commit critical section — NOT
// when applyHotAll pushes the new compressor values. A prior numeric commit
// first populates the record for sub3; a second numeric edit then parks at the
// commit-point barrier. Inside the park the record-backed reader (HotSnapshot via
// the injected currentHotFor) must STILL observe the PREVIOUS commit's 5000 —
// 「半提交不可见」. After release, recordHotApply rotates the record and the
// reader converges to 8100.
//
// S-E (design §5「回归测迁移：改源旋转语义」) changed the COMPRESSOR half of this
// anchor. In the push era the mid-window showed an observable divergence
// (compressor already at 8100-derived 6480 while the record still said 5000), which
// proved the record rotates atomically. With the §6.4 pull wiring the compressor
// resolves its numeric group THROUGH the record at every boundary, so the same
// mid-window must now show 5000-derived 4000 — the source shadowing the values
// applyHotAll still writes into the construction atomics. Convergence is the
// point: the read authority is the record, not the push. After release both move to
// 8100/6480 together, with no separate push step to sequence against.
//
// If the numeric-only path does not park at the commit gate (record rotation not
// aligned with the single critical section), this test fails at the bounded wait
// — a red witness of the missing commit barrier, not a hang.
func TestSC_RecordCommitsAtomicallyWithVersion(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	// 启动基线：sub3 max_tokens=4096（冷启动常驻，非热增）。
	write(scHotAddNumericYAML(t, true, 4096, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	sub3 := func() *agent.TagentAgent { return residentCacheForTest(entry)["sub3"] }

	// 第一次 numeric 提交（4096→5000，无屏障）：让唯一已提交记录里出现 sub3 条目。
	write(scHotAddNumericYAML(t, true, 5000, 2))
	entry.CheckOrgReload()
	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens, "前置：第一次 numeric 提交后记录须轮转到 5000")

	// 第二次 numeric 编辑（5000→8100）停在提交点：applyHotAll 已 push（compressor=8100），
	// 但 recordHotApply 未跑——记录读者必仍见 5000。
	parked := make(chan struct{})
	release := make(chan struct{})
	park := func() {
		close(parked)
		<-release
	}
	orgCommitBarrier.Store(&park)
	t.Cleanup(func() { orgCommitBarrier.Store(nil) })
	// Teardown safety for every barrier-park test: a mid-window assertion failure
	// must not leave the rebuild goroutine parked, or the deferred Close's drain
	// waits the `mu` that parked reload still holds (hang, not failure). Cleanups
	// run LIFO, so this release runs before the Close cleanup registered above.
	releaseAll := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	t.Cleanup(releaseAll)

	done := make(chan struct{})
	go func() {
		defer close(done)
		write(scHotAddNumericYAML(t, true, 8100, 2))
		entry.CheckOrgReload()
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		releaseAll()
		<-done
		t.Fatal("S-C 红：数值-only 提交点未触发 orgCommitBarrier——记录轮转未落在同一提交闸门（半提交不可见契约缺实现）")
	}

	// 屏障内：记录读者仍见上一次提交 5000（半提交不可见）；S-E 起压缩器亦经同一记录
	// 解析预算线 → 4000（5000×0.8），遮蔽 applyHotAll 已写入构造原子字段的新值。
	// 读权威唯一：不再有独立的 push 轴可与记录分歧。
	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens,
		"S-C 锚2：提交点内、记录轮转前，记录读者仍见上一次的 5000（半提交不可见）")
	require.Equal(t, 4000, sub3().OrgBudgetLine(),
		"S-E 锚：压缩器边界读经记录解析（源遮蔽 push 写入的原子字段），非 push 真值")

	releaseAll()
	<-done

	require.Equal(t, 8100, scHot(t, sub3()).MaxTokens, "提交完成后记录轮转到新值 8100")
	require.Equal(t, 6480, sub3().OrgBudgetLine(), "记录轮转后压缩器经源解析到新代预算线（8100×0.8），两轴同代")
}

// TestSE_RecordReadIsLockFree pins design §2's S-E requirement: the applied
// record's READ face never takes the coordinator lock. S-C shipped a lock-held
// read on the explicit premise that hot params are consumed only at
// compression/budget boundaries; S-E makes that premise false-by-omission — the
// compressor now resolves its numeric group from the record at EVERY boundary of
// EVERY live CM (resident + each invocation-private), so a lock-held read puts the
// commit critical section on the compression read path (contention today,
// re-entrancy the moment any commit-time code reads an owner's hot view).
// The anchor therefore reads the record from a live goroutine WHILE the test holds
// coord.mu: a lock-held implementation cannot make progress (bounded red, not a
// hang), a lock-free one returns immediately.
func TestSE_RecordReadIsLockFree(t *testing.T) {
	coord := newOrgCoordinator()
	coord.init("", nil)
	_, gen := coord.swap("fp1", nil, []appliedAgent{{Name: "x", Hot: agent.OrgHotParams{MaxTokens: 7}}})
	require.NotNil(t, gen)

	var got agent.OrgHotParams
	done := make(chan struct{})
	coord.mu.Lock()
	go func() {
		got, _ = coord.currentHotFor("x")
		close(done) // happens-before edge for the read below (-race clean)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		coord.mu.Unlock()
		t.Fatal("S-E 红：currentHotFor 仍依赖 coord.mu——记录读面未与提交临界区解耦（design §2 要求 S-E 无锁化，与 compressor 侧同源）")
	}
	coord.mu.Unlock()

	require.Equal(t, 7, got.MaxTokens, "无锁读仍须读到已提交记录")
}

// org_hotreload_lockfree_read 读面无锁域：应用记录与代际同临界区整体轮转、读侧无锁。
// 契约: docs/wiki/platform/org-hot-reload.md#lockfree-read
package tagent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// TestAppliedRecordCommitsAtomicallyWithVersion 钉住 记录只在唯一提交临界区轮转，半提交对读者不可见。
// - 构建停在提交闸门内时，记录读者必须仍见上一次提交的值；
// - 压缩器的预算线经同一条记录解析，屏障内不得出现"新值已可见、记录仍旧值"的两轴分歧；
// - 放行后两轴同代收敛；纯数值路径若不在提交闸门停车，就以有界失败暴露而不是挂住。
// 契约: docs/wiki/platform/org-hot-reload.md#lockfree-read
func TestAppliedRecordCommitsAtomicallyWithVersion(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	write(scHotAddNumericYAML(t, true, 4096, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	sub3 := func() *agent.TagentAgent { return residentCacheForTest(entry)["sub3"] }

	write(scHotAddNumericYAML(t, true, 5000, 2))
	entry.CheckOrgReload()
	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens, "前置：第一次 numeric 提交后记录须轮转到 5000")

	parked := make(chan struct{})
	release := make(chan struct{})
	park := func() {
		close(parked)
		<-release
	}
	orgCommitBarrier.Store(&park)
	t.Cleanup(func() { orgCommitBarrier.Store(nil) })
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

	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens,
		"S-C 锚2：提交点内、记录轮转前，记录读者仍见上一次的 5000（半提交不可见）")
	require.Equal(t, 4000, sub3().OrgBudgetLine(),
		"S-E 锚：压缩器边界读经记录解析（源遮蔽 push 写入的原子字段），非 push 真值")

	releaseAll()
	<-done

	require.Equal(t, 8100, scHot(t, sub3()).MaxTokens, "提交完成后记录轮转到新值 8100")
	require.Equal(t, 6480, sub3().OrgBudgetLine(), "记录轮转后压缩器经源解析到新代预算线（8100×0.8），两轴同代")
}

// TestAppliedRecordReadIsLockFree 钉住 记录读面绝不触碰协调器锁。
// - 压缩器在每个存活上下文管理器的每个消费边界解析数值组，持锁读会把提交临界区放到压缩读路径上；
// - 装置在测试持有该锁时从活协程读记录：持锁实现只能有界失败，无锁实现立刻返回。
// 契约: docs/wiki/platform/org-hot-reload.md#lockfree-read
func TestAppliedRecordReadIsLockFree(t *testing.T) {
	coord := newOrgCoordinator()
	coord.init("", nil)
	_, gen := coord.swap("fp1", nil, []appliedAgent{{Name: "x", Hot: agent.OrgHotParams{MaxTokens: 7}}})
	require.NotNil(t, gen)

	var got agent.OrgHotParams
	done := make(chan struct{})
	coord.mu.Lock()
	go func() {
		got, _ = coord.currentHotFor("x")
		close(done)
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

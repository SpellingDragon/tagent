// org_candidate_close_drain 关闭排空域：关闭必须覆盖排空期间落位的属主。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
package tagent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// TestOrgClose_CoversCandidatePublishedDuringDrain 钉住 最终关闭必须覆盖所有属主、候选与共享资源。
// - 属主关闭器登记在停止重载之后：仍在进行中的构建可能在快照之后又新增一个属主，从而永远躲过清扫；
// - 场景是把一次构建停在重载互斥之内，启动关闭，再放它发布；
// - 每个属主，含排空期间落位的这一个，都必须被恰好关闭一次，其存储租约真正归还。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
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
	park := newBuildPark()
	defer park.disarm()
	t.Cleanup(func() { _ = entry.Close() })

	write(sdCloseYAML(t, []string{"sub1", "sub2"}, storeOf))
	_ = acquireWithin(t, entry, 2*time.Second)
	park.waitEntered(t)

	closed := make(chan error, 1)
	go func() { closed <- entry.Close() }()
	time.Sleep(50 * time.Millisecond)
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

// TestFactoryBuiltReleasesItsStoreLease 钉住 同一契约的工厂侧：为该名字获取的租约经与配置路径同一个出口归还。
// - 出口只在最后一步交出，绝不在未收敛时交；
// - 可观察的见证是叶子自身路径上的 writer 锁：工厂分支不得从旁门再次取得属主责任。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestFactoryBuiltReleasesItsStoreLease(t *testing.T) {
	const leafName = "g33_fact_leaf"
	agent.RegisterToolAgent(leafName, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		return &agent.TagentConfig{
			Name:         leafName,
			Model:        fc.Model,
			MemoryStore:  fc.MemoryStore,
			SystemPrompt: "factory-built",
		}, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-factory-built")
	require.NoError(t, os.WriteFile(yamlPath, []byte(factoryLeafYAML(leafName, store)), 0o644))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)

	leaf := residentCacheForTest(entry)[leafName]
	require.NotNil(t, leaf, "precondition: the factory owner is a resident owner too")
	require.Equal(t, leafName, leaf.Info().Name, "precondition: the factory's declared identity is adopted verbatim")
	require.NoError(t, entry.Close())
	require.NoError(t, takeOverStore(t, store),
		"3.3 工厂门：工厂分支取了 store 租约就必须负责归还，否则该路径永久泄漏写者名额")
}

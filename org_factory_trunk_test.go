package tagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// 轮九十一（evidence §5.47）：② 工厂公开合同一次迁移的失败契约。
//
// 轮九十把发布改为「每个可达 owner 都 stage→wire→activate」，而旧合同的工厂产物是
// **整只 agent**（装配中段不产配置，cfg 为 nil），于是暴露出两类工厂 owner 特有缺陷
// ——这两条测在合同迁移前实测为红：
//
//	D-f1 每次结构发布，为工厂 owner 装配 face 时都会**再构造一个整只
//	     TagentAgent**（旧合同要求工厂自己 NewTagentAgent），它只被用来抄
//	     ExecutorConfig，随即成为无人 Close 的孤儿（bus/TaskManager/runner 全
//	     新建）。违背 D1「修改 B 不复制它的 bus/TaskManager」与 2.3 准出。
//	D-f2 staged 代的 runCfg 对工厂 owner 恒为 nil ⇒ 被钉的委派调用回退
//	     `*ta.config`（**构造期**配置），工厂声明变了也永不到达——正是
//	     「不按陈旧 ta.config 先造后补」在工厂分支的未兑现半。
//
// 合同迁移（工厂返回 *TagentConfig，构造归唯一 wireAgent 路径）后，两条都必须绿。

// factoryTrunkYAML routes `leaf` from main with async:false (a sync delegation:
// the witness is the call itself, not a settle-driven extra turn) and makes the
// leaf's OWN max_tool_iterations the varied field — it is fingerprinted, so
// changing it publishes a new generation, and it also reaches the factory via
// ToolAgentFactoryConfig, which is how the declaration changes.
func factoryTrunkYAML(leaf string, iters int) string {
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "MAIN"
    memory:
      type: memory
    tools:
      - kind: agent
        agent: %s
        description: delegate-leaf
        async: false
  %s:
    system_prompt:
      inline: "DECLARED-IGNORED"
    memory:
      type: memory
    max_tool_iterations: %d
`, leaf, leaf, iters)
}

func writeFactoryTrunk(t *testing.T, path, content string, tick time.Time) time.Time {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	tick = tick.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, tick, tick))
	return tick
}

// TestFactory33_ReloadConstructsNoOrphanAgents pins D-f1: a structural publish
// that touches a factory-owned agent must construct ZERO agents. Today the face
// assembly calls the old-contract factory, which builds a whole TagentAgent just
// to copy its ExecutorConfig out — nobody closes it. With the migrated contract
// (factory returns a *TagentConfig) the same publish advances the owner through
// the one face path every other owner uses, and constructs nothing.
func TestFactory33_ReloadConstructsNoOrphanAgents(t *testing.T) {
	const leaf = "g33_orphan_leaf" // unique: RegisterToolAgent panics on a duplicate id
	agent.RegisterToolAgent(leaf, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		// The migrated contract: a declaration, never a constructed agent.
		return &agent.TagentConfig{
			Name:        leaf,
			Model:       fc.Model,
			MemoryStore: fc.MemoryStore,
		}, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 2), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	require.NotNil(t, residentCacheForTest(entry)[leaf], "precondition: the factory owner is resident")

	before := agent.TagentAgentsConstructed()
	tick = writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 3), tick)
	entry.CheckOrgReload()

	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"D-f1：工厂 owner 的结构发布不得整只再造一个 agent——它只被用来抄 face，随后成为无人 Close 的孤儿（D1「修改 B 不复制它的 bus/TaskManager」）")
}

// TestFactory33_FactoryConfigChangeReachesDelegations pins D-f2: after a publish
// in which the factory's declaration changed, a FRESH delegated call must run
// the NEW declaration. The wrapper resolves the target through the declared
// generation's own execution view — for a factory owner that view must carry the
// new assembled config, which is exactly what the old whole-agent product (cfg
// nil ⇒ runCfg nil) could not provide: declared calls fell back to the
// construction-time ta.config and served the old prompt forever.
func TestFactory33_FactoryConfigChangeReachesDelegations(t *testing.T) {
	const leaf = "g33_stale_leaf"
	agent.RegisterToolAgent(leaf, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		// The factory's declaration is keyed to the iteration budget it is handed,
		// so a structural publish that changes it changes the factory OUTPUT too.
		return &agent.TagentConfig{
			Name:         leaf,
			Model:        fc.Model,
			MemoryStore:  fc.MemoryStore,
			SystemPrompt: fmt.Sprintf("FACTORY-%d", fc.MaxToolIterations),
		}, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 2), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	// Close via DEFER (runs before t.Cleanup on a FailNow): if an assertion below
	// fails mid-loop, StopLoop still happens first and the drain below cannot hang.
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "factory-trunk-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("first"))
	require.NoError(t, err)
	waitFor(t, "the factory leaf served on G1", func() bool {
		return countServed(m.snapshot(), "FACTORY-2") >= 1
	})

	tick = writeFactoryTrunk(t, yamlPath, factoryTrunkYAML(leaf, 3), tick)
	entry.CheckOrgReload() // publish G2: the factory declaration changed with it

	_, err = entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("second"))
	require.NoError(t, err)
	waitFor(t, "a fresh delegation serves the new factory declaration", func() bool {
		return countServed(m.snapshot(), "FACTORY-3") >= 1
	})
}

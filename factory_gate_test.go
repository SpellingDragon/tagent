package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
)

// 3.3 工厂前置能力门（evidence §5.45，合同迁移 §5.47）：ToolAgentFactory 在组织
// 装配里只有一处生产消费点（build_agent.go 的 `registry.GetToolAgentFactory(name)`）。
// 旧合同下其产物**整只返回**、跳过 wireAgent 尾段——轮八十九据此测出 store 租约永久
// 泄漏并当场修复；轮九十一经用户裁决把合同一次迁移为「工厂交配置、组织负责构造」，
// 该泄漏类从此**结构上不可能**（租约走 TagentConfig.MemStoreRelease 同一退出槽）。
// 本文件因此双职：特征测继续钉住**迁移后也不得改变**的承诺（内置名保护、菱形单调用、
// 失败包装形状、工厂自持声明不被 Tools 构建覆盖），并把租约规则留成回归臂——
// 规则＝「store 只由 RuntimeResources 退出」，任何构造分支都不得留下一张没人接手、
// 也无法归还的租约。

// factoryLeafYAML routes `leafName` from main and gives it its OWN localfile store, so the
// writer lock on that path is an outside-observable witness of whether the lease came back.
func factoryLeafYAML(leafName, storePath string) string {
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
      inline: "LEAF"
    memory:
      type: localfile
      path: %q
`, leafName, leafName, storePath)
}

// takeOverStore asks a FRESH registry to open the path: success proves the previous holder
// handed the writer slot back exactly once; ErrStoreLocked means a lease leaked, and
// ErrResourcePoisoned means it was released uncleanly.
func takeOverStore(t *testing.T, path string) error {
	t.Helper()
	fresh := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, rel, err := fresh.acquire("localfile", path, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	if err == nil {
		require.NoError(t, rel())
	}
	return err
}

// TestFactory33_ConfigBuiltReleasesItsStoreLease is the CONTROL arm: a config-built owner
// hands its lease back on org Close. Without this, the factory arm below could pass simply
// because the harness never observes leases.
func TestFactory33_ConfigBuiltReleasesItsStoreLease(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := filepath.Join(dir, "store-config-built")
	require.NoError(t, os.WriteFile(yamlPath, []byte(factoryLeafYAML("g33_ctrl_leaf", store)), 0o644))

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)

	require.NotNil(t, residentCacheForTest(entry)["g33_ctrl_leaf"], "precondition: the leaf is a resident owner")
	require.NoError(t, entry.Close())
	require.NoError(t, takeOverStore(t, store), "config-built owner: 关闭后租约必须恰一次归还")
}

// TestFactory33_FactoryBuiltReleasesItsStoreLease is the factory arm of the same contract.
//
// Under the migrated contract the factory delivers a declaration, so the lease the org
// acquired for this name arrives through TagentConfig.MemStoreRelease — the SAME exit slot
// the config path uses (last step, never while unconverged, §4.1). This arm is kept as the
// regression: it is the outside-observable witness (writer lock on the leaf's own path) that
// the factory branch never re-acquires owner responsibility through a side door. Under the
// OLD contract this is exactly where a permanently-held writer slot lived (round 89 measured
// it red before the AdoptMemStoreRelease seam, and the seam itself died with round 91).
func TestFactory33_FactoryBuiltReleasesItsStoreLease(t *testing.T) {
	const leafName = "g33_fact_leaf" // unique: RegisterToolAgent panics on a duplicate id
	agent.RegisterToolAgent(leafName, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		// What real factories do now: describe the agent from what the config gives them.
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

// TestFactory33_CommittedBehavior pins what the gate may not silently change: builtin-name
// protection, verbatim use of the product, declared AgentConfig.Tools being ignored on this
// branch, diamond memoization (one factory call for two parents), and the error shape the
// hot-reload fail-closed path matches on.
func TestFactory33_CommittedBehavior(t *testing.T) {
	rc := &runtimeConfig{model: &stubModel{name: "m"}}
	loader := prompt.NewLoader("")

	t.Run("product is used verbatim and its declared tools are not consulted", func(t *testing.T) {
		const name = "g33_verbatim"
		calls := 0
		agent.RegisterToolAgent(name, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
			calls++
			return &agent.TagentConfig{Name: name, Model: fc.Model}, nil
		})
		cfg := Config{Agents: map[string]AgentConfig{
			name: {
				SystemPrompt: PromptConfig{Inline: "declared prompt"},
				Memory:       MemoryConfig{Type: "memory"},
				// A tool id that does not exist: the config-driven branch would fail here,
				// the factory branch never looks at it.
				Tools: []ToolRef{{Kind: ToolKindTool, ID: "no-such-tool-id-g33"}},
			},
		}}
		cache := map[string]*agent.TagentAgent{}
		ta, err := buildAgent(name, cfg.Agents[name], cfg, rc, loader, cache, buildModeResident)
		require.NoError(t, err, "工厂分支不构建声明的 Tools（既有承诺）")
		require.Same(t, cache[name], ta, "产物即缓存值")
		require.Equal(t, 1, calls)
	})

	t.Run("two parents share one factory call", func(t *testing.T) {
		const name = "g33_shared"
		calls := 0
		agent.RegisterToolAgent(name, func(fc agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
			calls++
			return &agent.TagentConfig{Name: name, Model: fc.Model}, nil
		})
		parent := func(kids ...ToolRef) AgentConfig {
			return AgentConfig{SystemPrompt: PromptConfig{Inline: "p"}, Memory: MemoryConfig{Type: "memory"}, Tools: kids}
		}
		kid := ToolRef{Kind: ToolKindAgent, AgentID: name, Description: "d"}
		cfg := Config{Agents: map[string]AgentConfig{
			name: {SystemPrompt: PromptConfig{Inline: "leaf"}, Memory: MemoryConfig{Type: "memory"}},
			"p1": parent(kid),
			"p2": parent(kid),
		}}
		cache := map[string]*agent.TagentAgent{}
		for _, pn := range []string{"p1", "p2"} {
			_, err := buildAgent(pn, cfg.Agents[pn], cfg, rc, loader, cache, buildModeResident)
			require.NoError(t, err)
		}
		require.Equal(t, 1, calls, "同一缓存内菱形只允许构建一次")
	})

	t.Run("factory failure keeps the wrapped error shape", func(t *testing.T) {
		const name = "g33_failing"
		agent.RegisterToolAgent(name, func(agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
			return nil, fmt.Errorf("boom")
		})
		cfg := Config{Agents: map[string]AgentConfig{
			name: {SystemPrompt: PromptConfig{Inline: "x"}, Memory: MemoryConfig{Type: "memory"}},
		}}
		_, err := buildAgent(name, cfg.Agents[name], cfg, rc, loader, map[string]*agent.TagentAgent{}, buildModeResident)
		require.ErrorContains(t, err, fmt.Sprintf("agent %q: factory failed:", name),
			"错误形状是热更 fail-closed 的匹配面，改它等于改拒绝语义")
		require.ErrorContains(t, err, "boom", "工厂原始原因必须保留")
	})
}

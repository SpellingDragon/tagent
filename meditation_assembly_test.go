// meditation_assembly_test 覆盖组合根对冥想观察面的装配：缺省回落授权集、越界具名拒绝、形态搬运，以及外部形态产出的分区隔离。
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
package tagent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
)

// assembleTestAgent 以给定 store 装配单个 agent 并返回其运行时配置，供观察面与分区隔离断言复用。
func assembleTestAgent(t *testing.T, name string, acfg AgentConfig, cfg Config, store memory.MemoryStore) (*agent.TagentConfig, error) {
	t.Helper()
	rc := &runtimeConfig{model: &stubModel{name: "m"}}
	loader := prompt.NewLoader("", prompt.WithFallback(defaultPromptsFS, DefaultPromptsPrefix))
	cache := map[string]*agent.TagentAgent{}
	assembled, err := assembleAgentConfig(name, acfg, cfg, rc, loader, store, nil, nil, cache, buildModeResident, map[string]bool{}, nil)
	if assembled == nil {
		return nil, err
	}
	return assembled.cfg, err
}

// TestMeditationAssemblyObservationSurface 钉住 观察面在装配期回落授权集、越界具名拒绝启动、显式声明搬运进运行时配置。
// - 缺省回落：未声明 observed 时取 read_namespaces 派生的最终集合；
// - 越界拒绝：显式 observed 落在 read_namespaces 授权集之外即拒绝启动并点名越界项；
// - 形态搬运：显式 observed 与 deliver_to 原样进入运行时冥想配置；
// - 未配置即 in-loop：既无 observed 又无 read 时观察面为空，形态与行为不变。
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestMeditationAssemblyObservationSurface(t *testing.T) {
	t.Run("缺省回落 read_namespaces", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory", ReadNamespaces: []string{"target"}},
			Meditation:   MeditationConfig{Enabled: true},
		}
		got, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.NoError(t, err)
		require.Equal(t, []string{"target"}, got.Meditation.ObservedNamespaces, "未声明观察面必须回落 read_namespaces")
	})

	t.Run("越界具名拒绝启动", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory", ReadNamespaces: []string{"target"}},
			Meditation:   MeditationConfig{Enabled: true, ObservedNamespaces: []string{"ghost"}},
		}
		_, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.Error(t, err, "未授权分区必须拒绝启动而非静默进入判据")
		require.Contains(t, err.Error(), "ghost", "拒绝须点名越界的分区")
		require.Contains(t, err.Error(), "not authorized", "拒绝须说明越界理由")
	})

	t.Run("显式声明与 deliver_to 搬运", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory", ReadNamespaces: []string{"target", "recall"}},
			Meditation:   MeditationConfig{Enabled: true, ObservedNamespaces: []string{"target", "recall"}, DeliverTo: []string{"recall"}},
		}
		got, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.NoError(t, err)
		require.Equal(t, []string{"target", "recall"}, got.Meditation.ObservedNamespaces, "授权内的显式观察面须原样搬运")
		require.Equal(t, []string{"recall"}, got.Meditation.DeliverTo, "deliver_to 声明须搬运进运行时配置")
	})

	t.Run("未配置即 in-loop", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory"},
			Meditation:   MeditationConfig{Enabled: true},
		}
		got, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.NoError(t, err)
		require.Empty(t, got.Meditation.ObservedNamespaces, "无观察面与读授权即 in-loop 自体维护形态")
	})
}

// TestMeditationExternalFormOwnPartition 钉住 外部形态冥想的产出只落自身分区，被观察分区零写入且不新增压缩摘要事件。
// - 双 agent 共享同一事实链，观察面授权解析为被观察分区；
// - 按框架分区盖章写进自身分区的普通产出对目标分区不可见，目标分区事件数不因产出而增；
// - 目标分区既有的压缩摘要锚点不因冥想产出而新增。
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestMeditationExternalFormOwnPartition(t *testing.T) {
	shared := memory.NewInMemoryStore()
	targetPID := memory.PartitionIDFromName("target")
	meditatorPID := memory.PartitionIDFromName("meditator")

	meditatorCfg := AgentConfig{
		SystemPrompt: PromptConfig{Inline: "p"},
		Memory:       MemoryConfig{Type: "memory", ReadNamespaces: []string{"target"}},
		Meditation:   MeditationConfig{Enabled: true, ObservedNamespaces: []string{"target"}},
	}
	targetAgentCfg := AgentConfig{
		SystemPrompt: PromptConfig{Inline: "p"},
		Memory:       MemoryConfig{Type: "memory"},
	}
	meditatorRuntime, err := assembleTestAgent(t, "meditator", meditatorCfg, Config{Entry: "meditator"}, shared)
	require.NoError(t, err)
	_, err = assembleTestAgent(t, "target", targetAgentCfg, Config{Entry: "meditator"}, shared)
	require.NoError(t, err)
	require.Equal(t, []string{"target"}, meditatorRuntime.Meditation.ObservedNamespaces, "外部形态观察面须解析为被授权的目标分区")

	seed := func(pid int, key int64, eventType string, ts int64) {
		require.NoError(t, shared.StoreEvent(key, memory.FullEvent{
			EventKey:     key,
			PartitionID:  pid,
			EventType:    eventType,
			EventSummary: "seed",
			Content:      "body",
			Timestamp:    ts,
		}))
	}
	seed(targetPID, memory.NewSnowflakeEventKey(targetPID, 1000), "user", 1000)
	seed(targetPID, memory.NewSnowflakeEventKey(targetPID, 2000), tagentevent.TypeContextCompressSummary, 2000)
	seed(meditatorPID, memory.NewSnowflakeEventKey(meditatorPID, 3000), "user", 3000)

	beforeTarget, err := shared.QueryEvents(memory.QueryOptions{PartitionIDs: []int{targetPID}, Limit: 100})
	require.NoError(t, err)
	beforeCompact, err := shared.QueryEvents(memory.QueryOptions{PartitionIDs: []int{targetPID}, EventTypes: []string{tagentevent.TypeContextCompressSummary}, Limit: 100})
	require.NoError(t, err)

	seed(meditatorPID, memory.NewSnowflakeEventKey(meditatorPID, 4000), "memory_consolidate", 4000)

	afterTarget, err := shared.QueryEvents(memory.QueryOptions{PartitionIDs: []int{targetPID}, Limit: 100})
	require.NoError(t, err)
	afterCompact, err := shared.QueryEvents(memory.QueryOptions{PartitionIDs: []int{targetPID}, EventTypes: []string{tagentevent.TypeContextCompressSummary}, Limit: 100})
	require.NoError(t, err)
	require.Equal(t, len(beforeTarget), len(afterTarget), "被观察分区必须零写入")
	require.Equal(t, len(beforeCompact), len(afterCompact), "被观察分区不得新增压缩摘要事件")

	ownEvents, err := shared.QueryEvents(memory.QueryOptions{PartitionIDs: []int{meditatorPID}, Limit: 100})
	require.NoError(t, err)
	require.Len(t, ownEvents, 2, "冥想产出落在自身分区")
}

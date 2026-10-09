// meditation_assembly_test 覆盖组合根对冥想观察面的装配：缺省即自身分区、授权边界具名拒绝、声明搬运，以及反思产出的分区隔离。
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
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

// TestMeditationAssemblyObservationSurface 钉住 观察面在装配期缺省为自身分区、授权内原样搬运、越界具名拒绝启动。
// - 缺省自察：未声明 observed 时取 [自身]，与 read_namespaces 无涉（自身恒合法，无需授权）；
// - 混合合法：observed 同含自身与授权内的他人分区，装配通过且原样搬运；
// - 越界拒绝：他人分区落在 read_namespaces 之外即拒绝启动并点名越界项；
// - 声明搬运：显式 observed 与 deliver_to 原样进入运行时冥想配置。
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestMeditationAssemblyObservationSurface(t *testing.T) {
	t.Run("缺省观察面即自身分区", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory", ReadNamespaces: []string{"target"}},
			Meditation:   MeditationConfig{Enabled: true},
		}
		got, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.NoError(t, err)
		require.Equal(t, []string{"meditator"}, got.Meditation.ObservedNamespaces,
			"未声明观察面即自身分区，授权面不等于观察面")
	})

	t.Run("无任何读授权仍缺省自察", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory"},
			Meditation:   MeditationConfig{Enabled: true},
		}
		got, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.NoError(t, err)
		require.Equal(t, []string{"meditator"}, got.Meditation.ObservedNamespaces, "自身分区恒合法，无需读授权")
	})

	t.Run("自身与授权他人混合合法", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory", ReadNamespaces: []string{"target"}},
			Meditation:   MeditationConfig{Enabled: true, ObservedNamespaces: []string{"meditator", "target"}},
		}
		got, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.NoError(t, err)
		require.Equal(t, []string{"meditator", "target"}, got.Meditation.ObservedNamespaces,
			"read_namespaces ∪ {自身} 之内的声明原样搬运，自身那一项不占授权")
	})

	t.Run("越界具名拒绝启动", func(t *testing.T) {
		ac := AgentConfig{
			SystemPrompt: PromptConfig{Inline: "p"},
			Memory:       MemoryConfig{Type: "memory", ReadNamespaces: []string{"target"}},
			Meditation:   MeditationConfig{Enabled: true, ObservedNamespaces: []string{"planner"}},
		}
		_, err := assembleTestAgent(t, "meditator", ac, Config{Entry: "meditator"}, memory.NewInMemoryStore())
		require.Error(t, err, "未授权分区必须拒绝启动而非静默进入判据")
		require.Contains(t, err.Error(), "planner", "拒绝须点名越界的分区")
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
}

// TestMeditationExternalFormOwnPartition 钉住 观察他人分区的冥想产出只落自身分区，被观察分区零写入且不新增压缩摘要事件。
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
	require.Equal(t, []string{"target"}, meditatorRuntime.Meditation.ObservedNamespaces, "显式观察面须解析为被授权的目标分区")

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

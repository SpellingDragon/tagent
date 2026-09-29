package tagent

import (
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// findCollisionPair brute-forces two DIFFERENT names hashing to the same
// 10-bit partition id (FNV-1a). With 1024 slots, a pair appears quickly.
func findCollisionPair(t *testing.T) (string, string) {
	t.Helper()
	seen := make(map[int]string)
	for i := 0; i < 1_000_000; i++ {
		name := fmt.Sprintf("agent-%d", i)
		pid := memory.PartitionIDFromName(name)
		if prev, dup := seen[pid]; dup {
			return prev, name
		}
		seen[pid] = name
	}
	t.Fatal("no collision pair found in 1M names — impossible for 10-bit hash")
	return "", ""
}

// TestPartitionCollision_FailsClosed：共享同一持久 store 的两个 agent 名哈希到同一 pid → 构造期 fail-closed，绝不静默合并记忆命名空间，绝不自动迁移历史。
// - nameB 必须经引用被递归构建，它的 store owner 才会登记，"同一 store 上的 pid 冲突"这一前提方成立。
func TestPartitionCollision_FailsClosed(t *testing.T) {
	nameA, nameB := findCollisionPair(t)
	dir := t.TempDir()

	cfg := Config{
		Entry: nameA,
		Providers: map[string]ProviderConfig{
			"openai": {APIEndpoint: "http://localhost:1"},
		},
		Agents: map[string]AgentConfig{
			nameA: {
				SystemPrompt: PromptConfig{Inline: "a"},
				Memory:       MemoryConfig{Type: "localfile", Path: dir},
				Tools: []ToolRef{
					{Kind: "agent", AgentID: nameB, Description: "b"},
				},
			},
			nameB: {
				SystemPrompt: PromptConfig{Inline: "b"},
				Memory:       MemoryConfig{Type: "localfile", Path: dir},
			},
		},
	}
	_, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.Error(t, err, "same-store pid collision must fail closed")
	require.Contains(t, err.Error(), "partition id collision")
	require.Contains(t, err.Error(), nameA)
	require.Contains(t, err.Error(), nameB)
}

// TestPartitionCollision_IsolatedStoresNoFalsePositive：同 pid 但各自隔离 store（空 path）互不影响 → 构造成功，守卫不得误报。
func TestPartitionCollision_IsolatedStoresNoFalsePositive(t *testing.T) {
	nameA, nameB := findCollisionPair(t)

	cfg := Config{
		Entry: nameA,
		Providers: map[string]ProviderConfig{
			"openai": {APIEndpoint: "http://localhost:1"},
		},
		Agents: map[string]AgentConfig{
			nameA: {SystemPrompt: PromptConfig{Inline: "a"}},
			nameB: {SystemPrompt: PromptConfig{Inline: "b"}},
		},
	}
	ta, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	require.NoError(t, ta.Close())
}

var _ model.Model = (*stubModel)(nil)

// TestRemoteDeclarationOnlyKeepsTheGate pins that the remote-declaration-only skip applies only where no owner can ever exist.
// - The gate exists to refuse a genuinely missing definition: a name reached non-remotely as well must still be refused.
// - A locally defined name is never declaration-only, and a remote block without a URL must not be skipped here.
func TestRemoteDeclarationOnlyKeepsTheGate(t *testing.T) {
	remoteRef := ToolRef{Kind: ToolKindAgent, AgentID: "ghost", Remote: &RemoteConfig{URL: "http://127.0.0.1:1"}}
	localRef := ToolRef{Kind: ToolKindAgent, AgentID: "ghost"}

	onlyRemote := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a": {Tools: []ToolRef{remoteRef}},
	}}
	require.True(t, remoteDeclarationOnly(onlyRemote, "ghost"),
		"a name reached solely as a remote reference has no owner to build")

	mixed := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a": {Tools: []ToolRef{remoteRef}},
		"b": {Tools: []ToolRef{localRef}},
	}}
	require.False(t, remoteDeclarationOnly(mixed, "ghost"),
		"a non-remote reference to the same name needs a real owner: the gate must still fail closed")

	defined := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a":     {Tools: []ToolRef{remoteRef}},
		"ghost": {},
	}}
	require.False(t, remoteDeclarationOnly(defined, "ghost"),
		"a locally defined agent is never treated as declaration-only")

	blankURL := &Config{Entry: "a", Agents: map[string]AgentConfig{
		"a": {Tools: []ToolRef{{Kind: ToolKindAgent, AgentID: "ghost", Remote: &RemoteConfig{}}}},
	}}
	require.False(t, remoteDeclarationOnly(blankURL, "ghost"),
		"a remote block without a URL is the mismatch §5.44 refuses at validation — it must not be skipped here either")
}

// TestBuildAgent_ReadPartitionsIncludeOwnNamespace pins that a built agent read scope always carries its OWN namespace partition first.
func TestBuildAgent_ReadPartitionsIncludeOwnNamespace(t *testing.T) {
	var captured agent.PlainToolFactoryConfig
	agent.RegisterPlainTool("test_read_partitions", func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		captured = cfg
		return &mockCallableTool{name: cfg.ID}, nil
	})

	own := memory.PartitionIDFromName("tagent")
	crossA := memory.PartitionIDFromName("recall")
	crossB := memory.PartitionIDFromName("knowledge")

	cases := []struct {
		name           string
		agentName      string
		readNamespaces []string
		wantPartitions []int
	}{
		{
			name:           "no read_namespaces still reads own timeline",
			agentName:      "tagent",
			readNamespaces: nil,
			wantPartitions: []int{own},
		},
		{
			name:           "read_namespaces appended after own",
			agentName:      "tagent",
			readNamespaces: []string{"recall", "knowledge"},
			wantPartitions: []int{own, crossA, crossB},
		},
		{
			name:           "own namespace listed in read_namespaces is deduped",
			agentName:      "tagent",
			readNamespaces: []string{"tagent", "recall"},
			wantPartitions: []int{own, crossA},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				Agents: map[string]AgentConfig{
					tc.agentName: {
						SystemPrompt: PromptConfig{Inline: "prompt"},
						Memory:       MemoryConfig{Type: "memory", ReadNamespaces: tc.readNamespaces},
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "test_read_partitions"},
						},
					},
				},
			}
			rc := &runtimeConfig{model: &factoryMockModel{}}
			loader := prompt.NewLoader("")
			cache := make(map[string]*agent.TagentAgent)

			_, err := buildAgent(tc.agentName, cfg.Agents[tc.agentName], cfg, rc, loader, cache, buildModeResident)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPartitions, captured.ReadPartitionIDs,
				"read partitions must always include the agent's own namespace first")
		})
	}
}

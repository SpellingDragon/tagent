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

// TestPartitionCollision_FailsClosed：共享同一
// 持久 store 的两个 agent 名哈希到同一 pid → 构造期 fail-closed，绝不静默合并
// 记忆命名空间，绝不自动迁移历史。
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
				Tools: []ToolRef{ // 引用 nameB → 递归构建并登记其 store owner
					{Kind: "agent", AgentID: nameB, Description: "b"},
				},
			},
			nameB: {
				SystemPrompt: PromptConfig{Inline: "b"},
				Memory:       MemoryConfig{Type: "localfile", Path: dir}, // SAME shared store
			},
		},
	}
	_, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.Error(t, err, "same-store pid collision must fail closed")
	require.Contains(t, err.Error(), "partition id collision")
	require.Contains(t, err.Error(), nameA)
	require.Contains(t, err.Error(), nameB)
}

// TestPartitionCollision_IsolatedStoresNoFalsePositive：同 pid 但各自隔离
// store（空 path）互不影响 → 构造成功。
func TestPartitionCollision_IsolatedStoresNoFalsePositive(t *testing.T) {
	nameA, nameB := findCollisionPair(t)

	cfg := Config{
		Entry: nameA,
		Providers: map[string]ProviderConfig{
			"openai": {APIEndpoint: "http://localhost:1"},
		},
		Agents: map[string]AgentConfig{
			nameA: {SystemPrompt: PromptConfig{Inline: "a"}}, // isolated (no path)
			nameB: {SystemPrompt: PromptConfig{Inline: "b"}},
		},
	}
	ta, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	require.NoError(t, ta.Close())
}

var _ model.Model = (*stubModel)(nil)

// TestRemoteDeclarationOnlyKeepsTheGate guards the skip that lets a remote-only
// reference survive a hot reload: it must apply ONLY where no owner can ever
// exist. §5.11's gate exists to refuse a genuinely missing definition, so a name
// that is also reached non-remotely — or that IS defined locally — must still be
// refused, not silently published without an owner.
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

// TestBuildAgent_ReadPartitionsIncludeOwnNamespace is the regression test for
// the silent-empty timeline recall incident (2026-08-25 wechat-bot):
//
// Events are written to PartitionIDFromName(agentName), but readPartitionIDs
// used to contain ONLY read_namespaces partitions. For an agent without
// read_namespaces (the main agent), query-mode recall on FileSegmentStore
// therefore scanned ZERO partitions (resolvePartitions treats "no partitions"
// as "scan nothing" per event-segment-store isolation) and silently returned
// count=0 for every query — the model then wrongly concluded the backend had
// no history. The fix: the agent's OWN namespace partition always comes first.
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

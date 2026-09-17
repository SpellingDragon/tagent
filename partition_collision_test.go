package tagent

import (
	"fmt"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
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

// TestPartitionCollision_FailsClosed（resident-readiness-plan 4.4）：共享同一
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

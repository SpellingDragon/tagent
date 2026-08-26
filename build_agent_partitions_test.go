package tagent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

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

			_, err := buildAgent(tc.agentName, cfg.Agents[tc.agentName], cfg, rc, loader, cache)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPartitions, captured.ReadPartitionIDs,
				"read partitions must always include the agent's own namespace first")
		})
	}
}

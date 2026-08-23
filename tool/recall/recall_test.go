package recall

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// TestUnifiedRecall_Routing (stable-context-compaction D7): the unified entry
// routes by parameter shape — items tickets / turn_key causal chain / query
// semantic search / orchestrate reserved form.
func TestUnifiedRecall_Routing(t *testing.T) {
	tl := NewRecallTool(seedUnifiedStore(t), nil).(tool.CallableTool)
	call := func(args string) memoryRecallResult {
		out, err := tl.Call(context.Background(), []byte(args))
		require.NoError(t, err)
		return out.(memoryRecallResult)
	}

	t.Run("items takes precedence", func(t *testing.T) {
		res := call(`{"items":[{"key":"` + tagentevent.FormatEventKey(kA) + `"}]}`)
		assert.Equal(t, "items", res.Mode)
		require.Len(t, res.Entries, 1)
		assert.Contains(t, res.Entries[0].Content, "alpha content")
	})

	t.Run("turn_key walks the causal chain", func(t *testing.T) {
		res := call(`{"turn_key":"` + tagentevent.FormatEventKey(kOut) + `"}`)
		assert.Equal(t, "turn", res.Mode)
		require.NotEmpty(t, res.Entries)
		// Oldest → newest, ending at the agent_output anchor.
		last := res.Entries[len(res.Entries)-1]
		assert.Equal(t, tagentevent.FormatEventKey(kOut), last.Key)
	})

	t.Run("query uses the retrieval layer", func(t *testing.T) {
		res := call(`{"query":"alpha"}`)
		assert.Equal(t, "query", res.Mode)
	})

	t.Run("orchestrate returns explicit guidance, never silent fallback", func(t *testing.T) {
		res := call(`{"orchestrate":true}`)
		assert.Equal(t, "orchestrate", res.Mode)
		assert.Contains(t, res.Message, "未接线")
		assert.Empty(t, res.Entries, "orchestrate must not silently degrade to a deterministic shape")
	})

	t.Run("no shape is an error", func(t *testing.T) {
		_, err := tl.Call(context.Background(), []byte(`{}`))
		assert.Error(t, err)
	})
}

// Seed keys: input → thinking_plan → action_command → agent_output chain.
var (
	kIn  = int64(0x1201aa00000001)
	kTp  = int64(0x1201aa00000002)
	kAc  = int64(0x1201aa00000003)
	kOut = int64(0x1201aa00000004)
	kA   = int64(0x1201aa00000005)
)

func seedUnifiedStore(t *testing.T) *memory.InMemoryStore {
	t.Helper()
	store := memory.NewInMemoryStore()
	put := func(key int64, typ, summary, content string) {
		require.NoError(t, store.StoreEvent(key, memory.FullEvent{
			EventKey: key, EventType: typ, EventSummary: summary, Content: content, Timestamp: key,
		}))
	}
	put(kIn, tagentevent.TypeExternalInput, "alpha input", "alpha content")
	put(kTp, tagentevent.TypeThinkingPlan, "plan", "plan prose")
	put(kAc, tagentevent.TypeActionCommand, "exec", "tool result")
	put(kOut, tagentevent.TypeAgentOutput, "done", "final answer")
	put(kA, tagentevent.TypeExternalInput, "alpha", "alpha content")
	rs := store.RelationStore()
	require.NoError(t, rs.SetParent(kTp, kIn))
	require.NoError(t, rs.SetParent(kAc, kTp))
	require.NoError(t, rs.SetParent(kOut, kAc))
	return store
}

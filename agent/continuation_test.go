package agent

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestContinuationCommitsOneLogicalOutcomeAfterBackgroundCompletion(t *testing.T) {
	count := 0
	sequence := &sequenceMockModel{callCount: &count, responses: []*model.Response{
		toolCallResponse("spawn", "work"), finalTextResponse("initial", "started"), finalTextResponse("result", "finished"),
	}}
	spawned := make(chan *task.ManualDetector, 1)
	ta, err := NewTagentAgent(&TagentConfig{Name: "continuation-outcome", Model: sequence, Tools: []trpctool.Tool{&bgProbeTool{spawned: spawned}}})
	require.NoError(t, err)
	defer ta.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("start")))
	ch, err := ta.Run(ctx, inv)
	require.NoError(t, err)
	det := <-spawned
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "producer result"})
	det.Done()
	logical := 0
	var outcome, result string
	for evt := range ch {
		if value, ok := evt.StateDelta["delegation_outcome"]; ok {
			logical++
			outcome, result = string(value), string(evt.StateDelta["delegation_result"])
			require.True(t, evt.Response.Done)
			require.Nil(t, evt.Response.Choices)
		}
	}
	if logical != 1 {
		t.Logf("routing=%+v", ta.settleSinks.snapshotRouting(inv.InvocationID))
		for _, tk := range ta.taskManager.TasksSnapshot() {
			t.Logf("task=%s round=%d producer=%d status=%s", tk.ID, tk.RoundID(), tk.ProducerRound(), tk.Status())
		}
	}
	require.Equal(t, 1, logical)
	require.Equal(t, "completed", outcome)
	require.Equal(t, "finished", result)
}

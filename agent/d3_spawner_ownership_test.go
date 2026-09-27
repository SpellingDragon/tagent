package agent

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
)

// d3ProbeTool spawns one task through whatever task.TaskSpawner its call context
// carries. Its Declaration matches the shared toolCallResponse helper ("action"
// with a "command" arg) so the scripted model reaches it via the normal tool
// dispatch path.
type d3ProbeTool struct{ sawSpawner bool }

func (p *d3ProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "action",
		Description: "probe",
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{"command": {Type: "string"}},
			Required:   []string{"command"},
		},
	}
}

func (p *d3ProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	p.sawSpawner = ok
	if !ok {
		return "no spawner", nil
	}
	spawner.Spawn(
		task.TaskSpec{Kind: "probe", Desc: "ownership probe", Key: "probe-ownership"},
		task.NewFuncSettleDetector(context.Background(),
			func(context.Context) (string, error) { return "done", nil },
			10*time.Millisecond),
	)
	return "spawned", nil
}

// TestD3_SubagentSpawnerOwnership pins design D3.3: "继承调用版本／来源不等于继承父任务管理器".
//
// When B is invoked as a callee (its Run driven by a parent whose RunFlow already
// injected the PARENT's spawner onto the context), a task B spawns must land in
// B's OWN task manager — not the parent's. B's private invocation CM must carry
// B's own taskController so the flow re-injects B's spawner, letting it override
// the inherited one (the "父 spawner 遮蔽" fix). Before the fix, the private CM had
// no taskController, the parent spawner leaked through, and B's work silently
// registered in the caller's task domain.
func TestD3_SubagentSpawnerOwnership(t *testing.T) {
	parentTM := task.NewTaskManager(task.TaskManagerConfig{})
	ctx := task.WithTaskSpawner(context.Background(), parentTM) // the parent's injected spawner

	callCount := 0
	seqModel := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("call-1", "spawn something"),
			finalTextResponse("call-2", "done"),
		},
	}
	probe := &d3ProbeTool{}

	b, err := NewTagentAgent(&TagentConfig{
		Model:             seqModel,
		SystemPrompt:      "You are callee B.",
		Name:              "callee-b",
		Description:       "callee",
		MaxToolIterations: 5,
		Tools:             []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do your work")),
	)
	eventCh, err := b.Run(ctx, inv)
	require.NoError(t, err)
	for range eventCh { // drain to turn completion
	}

	require.True(t, probe.sawSpawner, "callee flow should carry a task spawner")

	// B's spawn belongs to B's manager, and the parent's manager stays clean.
	require.Len(t, b.taskManager.List(), 1,
		"callee-spawned task must register in the CALLEE's own task manager (design D3.3)")
	require.Empty(t, parentTM.List(),
		"父 spawner 遮蔽: the callee's task leaked into the PARENT's manager")
}

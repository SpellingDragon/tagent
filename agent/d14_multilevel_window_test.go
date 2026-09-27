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

// chainProbeTool spawns ONE background task per Call through whatever
// task.TaskSpawner its call context carries (detach → background), handing each
// detector to the test AFTER the sync window closes. It is invoked on the FIRST
// turn AND again on a CONTINUATION turn, so the second spawn exercises the
// property S3m-c's single-pipeline (c.2) depends on: a越窗 continuation turn's OWN
// background spawn must still be attributed to the same invocation id (threaded by
// the shell's ctx, NOT re-guessed from the settle batch, which the W-2 gate
// strips). Uses an EMPTY Key so consecutive spawns are not deduped. Declaration
// matches the shared toolCallResponse helper ("action"/"command").
type chainProbeTool struct{ spawned chan *task.ManualDetector }

func (p *chainProbeTool) Declaration() *trpctool.Declaration {
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

func (p *chainProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	if !ok {
		return "no spawner", nil
	}
	det := task.NewManualDetectorDetach(20 * time.Millisecond)
	spawner.Spawn(task.TaskSpec{Kind: "probe", Desc: "chain job"}, det)
	p.spawned <- det
	return "spawned", nil
}

// TestS3mC2_MultiLevelWindowCrossing locks the越窗 continuation chain across TWO
// hops: first turn spawns A → its late settle drives a continuation turn that
// spawns B → B's late settle drives a FINAL continuation turn. Every answer must
// reach the SAME call channel, in order, and the channel must only close after
// BOTH越窗 settles were consumed. This is the behavior-preservation contract for
// c.2 (the delegation input is consumed by the shared shell off invBus, so "first
// answer" and "each continuation" are just successive iterations of ONE loop, each
// keeping the invocation identity the shell holds). Red would be: a continuation
// turn's spawn losing attribution (settle B straying to the shared bus, channel
// closing after only "second").
func TestS3mC2_MultiLevelWindowCrossing(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1a", "spawn A"),        // turn 1 → spawn A
			finalTextResponse("t1b", "first answer"),  // turn 1 final
			toolCallResponse("t2a", "spawn B"),        // continuation 1 → spawn B
			finalTextResponse("t2b", "second answer"), // continuation 1 final
			finalTextResponse("t3", "third answer"),   // continuation 2 final (from B)
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &chainProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-chain", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("go")))
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)

	// Collect answers until the channel closes.
	var texts []string
	closed := make(chan struct{})
	go func() {
		for evt := range ch {
			if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
				continue
			}
			m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
			if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
				texts = append(texts, m.Content)
			}
		}
		close(closed)
	}()

	// Hop 1: turn 1 spawned A → deliver A's settle late.
	detA := <-spawned
	detA.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "A done"})
	detA.Done()

	// Hop 2: the A-driven continuation turn spawned B → deliver B's settle late.
	select {
	case detB := <-spawned:
		detB.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "B done"})
		detB.Done()
	case <-time.After(5 * time.Second):
		t.Fatalf("continuation turn's own spawn (B) never appeared — attribution lost across hops; texts=%v", texts)
	}

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatalf("channel did not close after both越窗 settles — barrier never quiesced; texts=%v", texts)
	}

	require.Equal(t, []string{"first answer", "second answer", "third answer"}, texts,
		"c.2: three successive same-loop turns (first + two continuations) must all reach the ONE call channel in order")
}

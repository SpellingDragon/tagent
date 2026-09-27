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

// TestS4_CallerCancelClosesChannelNotCallee locks D-c (取消/超时传播), which S3m-b's
// tail already implements: the caller hanging up (ctx cancel/timeout) must terminate
// ONLY that call's loop and channel — it must NOT close the callee, must NOT
// cascade-cancel the callee's in-flight background task, and a settle that arrives
// after the sink was unregistered must fall back to the bus as a safe drop (no panic).
//
// Scenario: the sub-call spawns a background task (detached, so the tail parks
// waiting for its settle), then the caller cancels WITHOUT the task ever settling.
func TestS4_CallerCancelClosesChannelNotCallee(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1", "go"),
			finalTextResponse("t1", "first answer"),
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &bgProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-s4-cancel", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	// NOTE: no `defer b.Close()` — we assert the callee is still alive (its task
	// survives the caller's cancel) BEFORE closing it manually at the end.

	ctx, cancel := context.WithCancel(context.Background())
	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do the work")),
	)
	ch, err := b.Run(ctx, inv)
	require.NoError(t, err)

	det := <-spawned // spawned + detached (background): the tail is now parked waiting

	// Caller hangs up without the task ever settling.
	cancel()

	closed := make(chan struct{})
	go func() {
		for range ch { // must terminate purely on ctx cancel, not on a settle
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("caller cancel did not close the invocation channel — the tail must exit on ctx")
	}

	// D-c core: hanging up the CALLER does not reach into the CALLEE's task domain.
	require.NotEmpty(t, b.taskManager.List(),
		"D-c: cancel terminates only the call's loop+channel — never the callee's in-flight task")

	// Late settle AFTER the sink was unregistered: falls back to the bus, no panic
	// (safe drop — the owning loop is already gone).
	require.NotPanics(t, func() {
		det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "late"})
		det.Done()
	})

	b.Close()
}

// TestS4_CalleeTimeoutViaCtx proves the same termination is reachable via a DEADLINE
// (not just explicit cancel): a short-timeout ctx ends the tail even while a
// background task is still outstanding, and the channel closes. This is the "超时"
// half of D-c and confirms the tail never outlives the caller's bounded ctx.
func TestS4_CalleeTimeoutViaCtx(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1", "go"),
			finalTextResponse("t1", "first answer"),
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &bgProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "callee", Name: "callee-s4-timeout",
		Description: "c", MaxToolIterations: 5, Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("work")))
	ch, err := b.Run(ctx, inv)
	require.NoError(t, err)

	<-spawned // background task spawned, no settle → without a ctx bound the tail would park forever

	closed := make(chan struct{})
	go func() {
		for range ch { // channel must close at the deadline, not hang on the unsettled task
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx deadline did not end the tail — the loop must be bounded by the caller's ctx")
	}
}

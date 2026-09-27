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

// bgProbeTool spawns one task through whatever task.TaskSpawner its call context
// carries, using a manual detector whose sync-wait window CLOSES quickly (detach),
// and hands that detector to the test only AFTER Spawn has returned from that window.
// So by the time the test holds the detector the spawn is provably a BACKGROUND one
// (SpawnResult.Settled == false) → its later Emit drives OnSettle → route → the M2
// tail. Declaration matches the shared toolCallResponse helper ("action"/"command").
type bgProbeTool struct{ spawned chan *task.ManualDetector }

func (p *bgProbeTool) Declaration() *trpctool.Declaration {
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

func (p *bgProbeTool) Call(ctx context.Context, _ []byte) (any, error) {
	spawner, ok := task.TaskSpawnerFromContext(ctx)
	if !ok {
		return "no spawner", nil
	}
	det := task.NewManualDetectorDetach(20 * time.Millisecond)
	spawner.Spawn(task.TaskSpec{Kind: "probe", Desc: "bg job", Key: "bg-1"}, det)
	p.spawned <- det // after Spawn returns detached → the test's Emit is post-window (background)
	return "spawned", nil
}

// TestS3mB_WindowCrossingContinuation is the ② 主子同构越窗 gate: a sub-call whose
// turn spawns a BACKGROUND task must, after its initial answer, keep the SAME returned
// channel open, receive that task's late settle (routed by S3m-a to the call's sink),
// run a continuation turn on its own invocation CM (isomorphic to the entry owner),
// and only then quiesce and close — delivering both the initial and the continuation
// answers to the caller. This is the behavior change S3m-b; before it the channel
// closed after the first answer and the越窗 result was lost to the shared bus.
func TestS3mB_WindowCrossingContinuation(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("t1", "go"),                   // turn 1 → tool spawns a background task
			finalTextResponse("t1", "first answer"),        // turn 1 final (initial ACK-stage answer)
			finalTextResponse("t2", "continuation answer"), // continuation turn final (after the late settle)
		},
	}
	spawned := make(chan *task.ManualDetector, 4)
	probe := &bgProbeTool{spawned: spawned}

	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-s3mb", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do the work")),
	)
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)

	// The spawn has detached (background) by the time we get the detector. Deliver the
	// late result now — it routes to the still-registered sink and wakes the tail.
	det := <-spawned
	det.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "async result"})
	det.Done()

	var texts []string
	closed := make(chan struct{})
	go func() {
		for evt := range ch { // ch closes only after the tail quiesces
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

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("invocation channel did not close within 5s — the M2 tail did not quiesce")
	}

	require.Contains(t, texts, "first answer", "the initial response reaches the caller")
	require.Contains(t, texts, "continuation answer",
		"the越窗 background settle drove a continuation turn delivered on the SAME call channel")
	require.Less(t, indexOfStr(texts, "first answer"), indexOfStr(texts, "continuation answer"),
		"the continuation answer must come after the initial answer (two-stage ACK→补最终, D-a)")
}

func indexOfStr(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// TestS3mB_RunDoesNotStompSharedSessionContext locks the family-3 去共享写 fix: a
// sub-call Run derives its session context LOCALLY and must never overwrite the
// owner's shared ta.lastUserID/lastSessionID. Before the fix Run called
// setSessionContext on every invocation, so a concurrent Run — or a Run while the
// entry owner's StartLoop is live — clobbered the owner's idle session context (the
// same shared-mutable-state anti-pattern S2m avoided for the CM). Red→green: setting
// a sentinel then running a delegation must leave both shared fields untouched.
func TestS3mB_RunDoesNotStompSharedSessionContext(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{finalTextResponse("t1", "answer")},
	}
	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "callee", Name: "callee-sessionishare",
		Description: "c", MaxToolIterations: 5,
	})
	require.NoError(t, err)
	defer b.Close()

	// Simulate the entry owner's live session context (StartLoop's writer).
	b.setSessionContext("owner-user", "owner-sentinel-session")

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("hi")))
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)
	for range ch {
	}

	require.Equal(t, "owner-user", b.lastUserID,
		"family-3: a delegation must not overwrite the shared owner userID")
	require.Equal(t, "owner-sentinel-session", b.lastSessionID,
		"family-3: a delegation must not stomp the shared owner session context")
}

// TestS3mB_ContainmentNonAsyncCallClosesAfterFirstAnswer proves the tail is a no-op
// for a sub-call that spawns no background task: with nothing booked, tryFinish
// reports quiescence immediately, so the channel closes right after the single turn —
// byte-for-byte the pre-S3m-b request/response behavior (no new events, no hang).
func TestS3mB_ContainmentNonAsyncCallClosesAfterFirstAnswer(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			finalTextResponse("t1", "plain answer"),
			finalTextResponse("t2", "should never run"), // would only appear if the tail spuriously continued
		},
	}
	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-s3mb-contain", Description: "c", MaxToolIterations: 5,
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("hi")))
	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)

	var texts []string
	for evt := range ch {
		if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
			continue
		}
		m := evt.Response.Choices[len(evt.Response.Choices)-1].Message
		if m.Role == model.RoleAssistant && m.Content != "" && len(m.ToolCalls) == 0 {
			texts = append(texts, m.Content)
		}
	}
	require.Equal(t, []string{"plain answer"}, texts,
		"a non-background-spawning sub-call runs exactly one turn and closes (containment)")
}

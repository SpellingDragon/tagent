package agent

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
)

// TestS2m_DelegationOriginCarriesInvocationID pins the S2m contract: when B serves
// a delegation carrying correlation handle X (the S1 invocation id) and spawns a
// task during that turn, the spawned task's opaque Origin must carry X. This is the
// routing baggage a越窗 task_settled reuses (Origin→Metadata verbatim, event_bus.go)
// so M2's per-invocation loop can route the late result back to the invocation that
// spawned it. Behavior-neutral: no consumer reads Origin.invocation_id yet.
func TestS2m_DelegationOriginCarriesInvocationID(t *testing.T) {
	callCount := 0
	seq := &sequenceMockModel{
		callCount: &callCount,
		responses: []*model.Response{
			toolCallResponse("call-1", "spawn something"),
			finalTextResponse("call-2", "done"),
		},
	}
	probe := &d3ProbeTool{}
	b, err := NewTagentAgent(&TagentConfig{
		Model: seq, SystemPrompt: "You are callee B.",
		Name: "callee-b-s2m", Description: "c", MaxToolIterations: 5,
		Tools: []trpctool.Tool{probe},
	})
	require.NoError(t, err)
	defer b.Close()

	inv := trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("do your work")),
	)
	inv.InvocationID = "deleg-x-42" // the correlation handle under test

	ch, err := b.Run(context.Background(), inv)
	require.NoError(t, err)
	for range ch { // drain to completion
	}

	require.True(t, probe.sawSpawner, "callee flow must carry a task spawner")
	require.Len(t, b.taskManager.List(), 1, "D3: the task registers in B's own manager")

	tk := b.taskManager.List()[0]
	require.Equal(t, "deleg-x-42", tk.Spec.Origin[metaKeyInvocationID],
		"S2m: spawned task Origin must carry the delegation invocation id for越窗 routing (M2)")
}

// TestS2m_ControlKeyNotModelVisible confirms the change is additive + model-safe:
// normal routing baggage (chat_id) still propagates to invocation metadata, while the
// control key stays out of the model-visible pipeline (S1 invariant, unchanged by S2m).
func TestS2m_ControlKeyNotModelVisible(t *testing.T) {
	events := []*AgentEvent{
		{
			Type:     tagentevent.TypeExternalInput,
			Metadata: map[string]any{"chat_id": "c-1", metaKeyInvocationID: "deleg-x-42"},
		},
	}
	md := extractRootMetadata(events)
	require.Equal(t, "c-1", md["chat_id"], "chat_id must propagate to invocation metadata")
	_, leaked := md[metaKeyInvocationID]
	require.False(t, leaked,
		"S1/S2m: invocation_id is a control key — must NEVER reach meta_*/model-visible metadata")

	// Routing reads it via ctx/extractor, not the filtered metadata.
	require.Equal(t, "deleg-x-42", extractDelegationInvocationID(events))
}

// TestExtractDelegationInvocationID locks the extractor's behavior-neutral edges.
func TestExtractDelegationInvocationID(t *testing.T) {
	require.Equal(t, "", extractDelegationInvocationID(nil), "nil events → empty")
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{nil}), "nil event → empty")
	plain := &AgentEvent{Type: tagentevent.TypeExternalInput, Metadata: map[string]any{"chat_id": "c"}}
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{plain}), "no handle → empty (non-delegation turn)")
	// A non-external-input event must be ignored even if it carries the key.
	other := &AgentEvent{Type: "response_done", Metadata: map[string]any{metaKeyInvocationID: "zzz"}}
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{other}))
	// W-2: a background-settle reclaim event (Source==SourceTask) that inherits a
	// sub-call's invocation_id via the Origin→Metadata copy must NOT re-propagate it.
	// Otherwise an entry-owner reclaim turn would stamp a sub-call's id onto its own
	// continuation-spawned tasks (mis-attribution + future misdelivery).
	settle := &AgentEvent{
		Type:     tagentevent.TypeExternalInput,
		Source:   SourceTask,
		Metadata: map[string]any{metaKeyInvocationID: "deleg-x-42", "task_id": "t1"},
	}
	require.Equal(t, "", extractDelegationInvocationID([]*AgentEvent{settle}),
		"W-2: settle/reclaim events must not carry invocation_id into a new turn")
	// But a genuine delegation input (Source=="" user external-input) still does.
	input := &AgentEvent{
		Type:     tagentevent.TypeExternalInput,
		Metadata: map[string]any{metaKeyInvocationID: "deleg-x-42"},
	}
	require.Equal(t, "deleg-x-42", extractDelegationInvocationID([]*AgentEvent{input}))
}

// TestS2m_SequentialDelegationsPerCallAttribution guards against the invocation_id
// becoming sticky across calls: two sequential delegations to the SAME callee with
// different invocation ids must each attribute their spawned task to their own id —
// the value is read from the per-call ctx, never cached on shared manager state.
func TestS2m_SequentialDelegationsPerCallAttribution(t *testing.T) {
	newRun := func(id string) *task.Task {
		callCount := 0
		seq := &sequenceMockModel{
			callCount: &callCount,
			responses: []*model.Response{
				toolCallResponse("c1", "spawn"),
				finalTextResponse("c2", "done"),
			},
		}
		probe := &d3ProbeTool{}
		b, err := NewTagentAgent(&TagentConfig{
			Model: seq, SystemPrompt: "callee", Name: "callee-seq-" + id,
			Description: "c", MaxToolIterations: 5, Tools: []trpctool.Tool{probe},
		})
		require.NoError(t, err)
		defer b.Close()
		inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("work")))
		inv.InvocationID = id
		ch, err := b.Run(context.Background(), inv)
		require.NoError(t, err)
		for range ch {
		}
		tks := b.taskManager.List()
		require.Len(t, tks, 1)
		return tks[0]
	}
	require.Equal(t, "inv-AAA", newRun("inv-AAA").Spec.Origin[metaKeyInvocationID])
	require.Equal(t, "inv-BBB", newRun("inv-BBB").Spec.Origin[metaKeyInvocationID])
}

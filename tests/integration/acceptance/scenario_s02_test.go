package acceptance

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestScenario02DualInvocationIsolation drives two invocations of one session through the public assembly entry.
// - each invocation decides on its own input beside the committed session history
// - cancelling one invocation leaves its neighbour's terminal untouched
// - each admitted call's task and settled feedback belong to the invocation that issued it
// 契约: docs/wiki/agent/event-flow.md#multi-user-isolation
func TestScenario02DualInvocationIsolation(t *testing.T) {
	ctx := context.Background()
	scenario, err := ScenarioOf("S02")
	if err != nil {
		t.Fatalf("read the scenario contract: %v", err)
	}
	observations := map[string]any{}
	refs := []string{}

	gated := &scenarioGatedTool{name: "gate-tool", output: "gate-output", entered: make(chan struct{}), gate: make(chan struct{})}
	provider := &scenarioModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		if len(req.Messages) == 0 {
			return scenarioFinalTurn("empty")
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Content == "turn-c" {
			return scenarioToolTurn(scenarioCall("call-c", "gate-tool", `{}`))
		}
		return scenarioFinalTurn("answer:" + last.Content)
	}}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}}
	governance := &scenarioGovernance{answers: scenarioPassingChecks("eval", "benchmark", "apply-gate")}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{gated}},
	}, tagent.WithGovernance(governance))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	address := event.Address{AgentID: "agent-a", SessionID: "session-shared"}
	firstTurn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-a",
		Address:      address,
		Input:        []model.Message{{Role: model.RoleUser, Content: "turn-a"}},
	})
	if err != nil {
		t.Fatalf("invoke the first invocation: %v", err)
	}
	secondTurn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-b",
		Address:      address,
		Input:        []model.Message{{Role: model.RoleUser, Content: "turn-b"}},
	})
	if err != nil {
		t.Fatalf("invoke the second invocation: %v", err)
	}
	requests := provider.recorded()
	observations["decisions_per_session"] = len(firstTurn.Decisions) + len(secondTurn.Decisions)
	shared := false
	for _, req := range requests[1:] {
		for _, message := range req.Messages {
			if message.Content == "turn-a" {
				shared = true
			}
		}
	}
	observations["shared_session_history"] = shared
	refs = append(refs, scenarioDecisionIDs(firstTurn.Decisions)...)
	refs = append(refs, scenarioDecisionIDs(secondTurn.Decisions)...)

	cancelCtx, cancel := context.WithCancel(ctx)
	doneC := make(chan error, 1)
	go func() {
		_, err := run.Invoke(cancelCtx, tagent.InvokeRequest{
			InvocationID: "inv-c",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-cancel"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "turn-c"}},
		})
		doneC <- err
	}()
	awaitSignal(t, gated.entered, "the cancelled invocation's tool to enter")
	cancel()
	var cancelErr error
	select {
	case cancelErr = <-doneC:
	case <-time.After(scenarioTimeout):
		t.Fatal("timed out waiting for the cancelled invocation")
	}
	neighbour, neighbourErr := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-d",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-cancel"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "turn-d"}},
	})
	close(gated.gate)
	observations["cancel_isolation"] = cancelErr != nil &&
		neighbourErr == nil && neighbour.Terminal == model.KindCompleted

	rig := newScenarioTaskRig()
	recorder := &scenarioRecorder{}
	mailbox := &scenarioMailbox{arrived: make(chan task.Notice, 4)}
	toolA := &scenarioTool{name: "echo-a", output: "out-a"}
	toolB := &scenarioTool{name: "echo-b", output: "out-b"}
	wrappedA := task.Wrap(rig.manager, toolA, task.WithResultRecorder(recorder.record), task.WithMailbox(mailbox.accept))
	wrappedB := task.Wrap(rig.manager, toolB, task.WithResultRecorder(recorder.record), task.WithMailbox(mailbox.accept))
	callA, err := wrappedA.Call(task.WithCallFrame(ctx, scenarioFrame("invocation-a", "call-a")), nil)
	if err != nil {
		t.Fatalf("admit the first invocation's call: %v", err)
	}
	callB, err := wrappedB.Call(task.WithCallFrame(ctx, scenarioFrame("invocation-b", "call-b")), nil)
	if err != nil {
		t.Fatalf("admit the second invocation's call: %v", err)
	}
	receiptA, okA := callA.(task.AdmissionReceipt)
	receiptB, okB := callB.(task.AdmissionReceipt)
	if !okA || !okB || receiptA.TaskID == receiptB.TaskID {
		t.Fatalf("receipts = (%+v, %+v), want one distinct task per invocation", callA, callB)
	}
	specA, _, errA := rig.registry.Lookup(ctx, receiptA.TaskID)
	specB, _, errB := rig.registry.Lookup(ctx, receiptB.TaskID)
	observations["tool_call_owner_distinct"] = errA == nil && errB == nil &&
		specA.InvocationID == "invocation-a" && specB.InvocationID == "invocation-b" &&
		specA.CallID == "call-a" && specB.CallID == "call-b"
	awaitIdle(t, rig.manager, receiptA.TaskID)
	awaitIdle(t, rig.manager, receiptB.TaskID)
	notes := mailbox.snapshot()
	factsByCall := map[string]event.EventID{}
	for at, result := range recorder.results() {
		factsByCall[result.CallID] = recorder.factID(at)
	}
	ownA, ownB, crossed := 0, 0, 0
	for _, notice := range notes {
		switch notice.TaskID {
		case receiptA.TaskID:
			ownA++
			if notice.Event != "" && notice.Event != factsByCall["call-a"] {
				crossed++
			}
		case receiptB.TaskID:
			ownB++
			if notice.Event != "" && notice.Event != factsByCall["call-b"] {
				crossed++
			}
		}
	}
	observations["feedback_owner_distinct"] = ownA > 0 && ownB > 0 && crossed == 0
	refs = append(refs, string(receiptA.TaskID), string(receiptB.TaskID))

	EmitScenarioRecord(t, scenario, refs, observations)
}

package agentexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// singleDecision is the canonical execution face: one model decision per drive, the
// sub-delegation of that same execution admitted as a task, and a close that reports
// the drain it reached.
type singleDecision struct{}

// Decide runs one model decision over the ready input of the invocation and reduces the
// model stream to its single terminal.
func (singleDecision) Decide(_ context.Context, inv agent.Invocation) (agent.Decision, error) {
	return agent.Decision{
		DecisionID:   event.DecisionID("decision-1"),
		InvocationID: inv.InvocationID,
		Terminal:     model.KindCompleted,
	}, nil
}

// Delegate admits one sub-agent call as an asynchronous task and reports its admission
// receipt, which states admission rather than a result.
func (singleDecision) Delegate(_ context.Context, req agent.Delegation) (task.AdmissionReceipt, error) {
	return task.AdmissionReceipt{
		CallID:   req.CallID,
		TaskID:   req.TaskID,
		Accepted: true,
		Kind:     task.AdmissionFresh,
	}, nil
}

// Close refuses new input, settles what was already accepted and reports whether the
// drain reached real termination.
func (singleDecision) Close(context.Context, agent.CloseRequest) (agent.DrainReport, error) {
	return agent.DrainReport{Drained: true}, nil
}

var _ agent.Agent = singleDecision{}

// ExampleAgent drives one invocation through the three faces of its execution: the
// decision entry, the delegation it admits and the close that ends it.
//
// 契约: docs/wiki/agent/agent-architecture.md#module-position
func ExampleAgent() {
	ctx := context.Background()
	face := singleDecision{}
	inv := agent.Invocation{
		InvocationID: "inv-1",
		Address:      event.Address{AgentID: "assistant", SessionID: "session-1"},
	}

	decision, err := face.Decide(ctx, inv)
	if err != nil {
		return
	}
	receipt, err := face.Delegate(ctx, agent.Delegation{
		InvocationID: inv.InvocationID,
		CallID:       "call-1",
		TaskID:       "task-1",
	})
	if err != nil {
		return
	}
	report, err := face.Close(ctx, agent.CloseRequest{Address: inv.Address})
	if err != nil {
		return
	}
	_ = decision.Terminal
	_ = receipt.Accepted
	_ = report.Drained
}

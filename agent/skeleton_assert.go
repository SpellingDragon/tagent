package agent

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#package-layout
//
// Skeleton compile-time assertions for the phase 1 execution declarations.
//
// The phase 2 types that will satisfy Agent do not exist yet, so the placeholder
// below is the implementation assertion this skeleton can write: the assertions
// pin every declared method's exact shape and every declared field's name and
// type.
var (
	_ func(Agent, context.Context, Invocation) (Decision, error) = Agent.Decide

	_ func(Agent, context.Context, Delegation) (task.AdmissionReceipt, error) = Agent.Delegate

	_ func(Agent, context.Context, CloseRequest) (DrainReport, error) = Agent.Close
)

// The declared execution face is an interface type.
var _ Agent = (Agent)(nil)

// The scheduling record, the decision, the delegation and the close report keep
// every documented field name and type.
var (
	_ = Invocation{
		InvocationID: "",
		Address:      event.Address{},
		Instance:     "",
		Ready:        []event.Event{},
		Slot:         DecisionSlot{},
		Terminal:     model.KindUnknown,
	}

	_ = DecisionSlot{}

	_ = Decision{
		DecisionID:   "",
		InvocationID: "",
		Binding:      state.AgentBinding{},
		Request:      model.Request{},
		Terminal:     model.KindUnknown,
	}

	_ = Delegation{
		InvocationID: "",
		DecisionID:   "",
		CallID:       "",
		TaskID:       "",
		Target:       event.Address{},
		Status:       task.StatusUnknown,
		Progress:     []task.Notice{},
	}

	_ = CloseRequest{
		Address:  event.Address{},
		Deadline: time.Time{},
	}

	_ = DrainReport{
		Drained:   false,
		Unsettled: []UnsettledObligation{},
	}

	_ = UnsettledObligation{
		TaskID: "",
		Reason: "",
	}
)

// errNotImplemented is the sentinel every declared stub returns while the phase 2
// implementation is absent.
var errNotImplemented = errors.New("phase 2: not implemented")

// skeletonAgent is a compile-time placeholder for the execution face. It carries
// the stub methods that satisfy Agent and holds no logic.
type skeletonAgent struct{}

// Decide implements Agent.Decide as a phase 2 stub.
func (skeletonAgent) Decide(ctx context.Context, inv Invocation) (Decision, error) {
	return Decision{}, errNotImplemented
}

// Delegate implements Agent.Delegate as a phase 2 stub.
func (skeletonAgent) Delegate(ctx context.Context, req Delegation) (task.AdmissionReceipt, error) {
	return task.AdmissionReceipt{}, errNotImplemented
}

// Close implements Agent.Close as a phase 2 stub.
func (skeletonAgent) Close(ctx context.Context, req CloseRequest) (DrainReport, error) {
	return DrainReport{}, errNotImplemented
}

// The execution face is satisfied by its skeleton placeholder.
var _ Agent = (*skeletonAgent)(nil)

// The declared method signatures are pinned exactly.
var (
	_ func(context.Context, Invocation) (Decision, error)              = skeletonAgent{}.Decide
	_ func(context.Context, Delegation) (task.AdmissionReceipt, error) = skeletonAgent{}.Delegate
	_ func(context.Context, CloseRequest) (DrainReport, error)         = skeletonAgent{}.Close
	_ func([]event.Event) error                                        = (*Invocation)(nil).Aggregate
)

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
		Ref:          state.BindingRef{},
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

// The sub-agent delegation ledger keeps its declared shape: the task face it
// settles its delegations through, the constructor over that face and the state
// owner's borrowing face, and every entry the ledger exposes — the admission,
// the observation, the settle, the reconciliation, the owner-scoped view, the
// owner lookup, the booking count and the borrowing face of the delegated turn.
var (
	_ DelegationTasks = (DelegationTasks)(nil)

	_ func(DelegationTasks, state.Borrow, event.InstanceID) *Delegator = NewDelegator

	_ func(*Delegator, context.Context, Delegation) (task.AdmissionReceipt, error) = (*Delegator).Delegate

	_ func(*Delegator, context.Context, task.Notice) error = (*Delegator).Observe

	_ func(*Delegator, context.Context, event.TaskID, task.Status, event.EventID) (task.Notice, error) = (*Delegator).Complete

	_ func(*Delegator, context.Context, event.TaskID) (task.Status, error) = (*Delegator).Reconcile

	_ func(*Delegator, event.InvocationID) []Delegation = (*Delegator).View

	_ func(*Delegator, event.TaskID) (event.InvocationID, bool) = (*Delegator).OwnerOf

	_ func(*Delegator) int = (*Delegator).Pending

	_ func(*Delegator, event.TaskID) (state.Borrow, error) = (*Delegator).TurnBorrow
)

// The close face keeps its declared shape: the gate new input is refused at over
// the task face it fronts, the setter of the ledger it lands terminals through,
// the constructor beside it, the producer registration with its named refusals,
// and the named unsettled state the close reports beside real settlement.
var (
	_ DelegationTasks = (*Shutdown)(nil)

	_ func(DelegationTasks) *Shutdown = NewShutdown

	_ func(*Shutdown, Reconciler) = (*Shutdown).SetReconciler

	_ func(*Shutdown, event.TaskID, func() bool) error = (*Shutdown).Watch

	_ func(*Shutdown, event.TaskID) = (*Shutdown).Track

	_ func(*Shutdown, context.Context, CloseRequest) (DrainReport, error) = (*Shutdown).Close

	_ Reconciler = (Reconciler)(nil)

	_ error = ErrShutdownRefused
	_ error = ErrDrainUnsettled
	_ error = ErrProducerIdentityMissing
	_ error = ErrProducerMissing
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

// The persistent event loop keeps its declared shape: the turn one drive
// reports, the drive and backflow entries themselves, the constructor over the
// state owner's borrowing face, and the fact resolver the tool pairing reads
// the native message through.
var (
	_ = Turn{
		InvocationID: "",
		Decisions:    []Decision{},
		Terminal:     model.KindUnknown,
	}

	_ func(state.Borrow, *state.Occupancy, event.InstanceID, FactResolver) *Loop = NewLoop

	_ func(*Loop, context.Context, Invocation) (Turn, error) = (*Loop).Drive

	_ func(*Loop, event.InvocationID, task.Notice) = (*Loop).Backflow

	// The training record production face is attached through the setter, so the
	// declared construction shape above stays exactly as it is. The face crosses
	// built-in, event and model types only and receives its observations
	// scattered, so the record production side attaches with no package edge
	// into this package.
	_ func(*Loop, Recorder) = (*Loop).SetRecorder

	// The session transcript face is attached through its own setter, so the
	// declared construction shape above stays exactly as it is: the face crosses
	// the address and the native messages only, and the loop shares one
	// session's history across the invocations that ended on it.
	_ func(*Loop, HistoryFace) = (*Loop).SetHistory

	// The After pass presentation is handed to the attached sink through its own
	// setter, so the declared construction shape above stays exactly as it is:
	// the pass runs once per committed fact and a nil sink drops the presentation
	// only, never the fact.
	_ func(*Loop, PresentationSink) = (*Loop).SetPresentations

	// The producer registration is attached through its own setter, so the
	// declared construction shape above stays exactly as it is: the loop
	// registers each in-flight production beside the work identity it runs
	// behind and the close confirms its real stop through the predicate.
	_ func(*Loop, func(event.TaskID, func() bool) error) = (*Loop).SetWatch

	// The safe request snapshot face is attached through its own setter, so the
	// declared construction shape above stays exactly as it is: the face is
	// fixed beside the request before the model entry is called and crosses
	// built-in, event, model and state types only.
	_ RequestSnapshotFace = (RequestSnapshotFace)(nil)

	_ func(context.Context, event.DecisionID, string, string, []string, state.AgentBinding, model.Request) error = RequestSnapshotFace(nil)

	_ func(*Loop, RequestSnapshotFace) = (*Loop).SetRequestSnapshotFace

	_ HistoryFace = (HistoryFace)(nil)

	_ func(HistoryFace, event.Address) []model.Message = HistoryFace.SessionHistory

	_ func(HistoryFace, event.Address, []model.Message) = HistoryFace.CommitHistory

	_ Recorder = (Recorder)(nil)

	_ func(Recorder, context.Context, event.EpisodeID, event.SessionID, time.Time) error = Recorder.RecordEpisode

	_ func(Recorder, context.Context, event.DecisionID, event.InvocationID, event.EpisodeID, []event.EventID, string, string, []string, time.Time) error = Recorder.RecordDecision

	_ func(Recorder, context.Context, event.AttemptID, event.DecisionID, event.EpisodeID, model.Outcome) error = Recorder.RecordAttempt

	// The model view material the decision record is written from keeps its
	// documented fields.
	_ = modelView{modelName: "", promptVersion: "", visibleTools: []string{}}

	_ func(event.EventID) (model.Message, bool) = FactResolver(nil)

	// The training-record gap account keeps its declared shape: one counted
	// loss beside the record kind, identity and named failure it carries.
	_ = RecordingLoss{Kind: "", ID: "", Err: nil}

	_ func(*Loop) []RecordingLoss = (*Loop).RecordingLosses
)

// The request-boundary assembly keeps its declared shape: the causal chain
// identity the assembly is isolated under, the boundary material the request
// face reads, the view it returns, and the face setter beside the frozen
// event-type→role table. No projection entry shape is mirrored here: the face
// crosses built-in, event, model and state types only.
var (
	_ = RequestChain{PartitionID: 0, SessionID: ""}

	_ = RequestBoundary{Agent: "", Chain: RequestChain{PartitionID: 0, SessionID: ""}, Binding: state.AgentBinding{}}

	_ = BoundaryView{Messages: []model.Message{}, PromptVersion: ""}

	_ RequestFace = (RequestFace)(nil)

	_ func(*Loop, RequestFace) = (*Loop).SetRequestFace

	_ func(string) model.Role = RoleOf

	_ error = ErrRequestChainSessionRequired
)

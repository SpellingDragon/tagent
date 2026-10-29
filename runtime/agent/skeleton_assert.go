package agent

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/agent/agent-architecture.md#package-layout
//
// Compile-time assertions for the execution face and the records it carries:
// the declared method shapes, every declared field name and type, and the named
// refusals. A renamed field or a widened signature stops this file from
// compiling.
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
	}

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

// The persistent event loop keeps its declared shape: the turn one drive
// reports, the drive and backflow entries themselves, the constructor over the
// state owner's borrowing face, and the fact resolver the tool pairing reads
// the native message through.
var (
	_ = Turn{
		InvocationID: "",
		Decisions:    []Decision{},
		Terminal:     model.KindUnknown,
		FinalRound:   FinalRoundNone,
	}

	_ func(state.Borrow, *state.Occupancy, event.InstanceID, FactResolver) *Loop = NewLoop

	_ func(*Loop, context.Context, Invocation) (Turn, error) = (*Loop).Drive

	_ func(*Loop, event.InvocationID, task.Notice) = (*Loop).Backflow

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

	// The watchdog budget is attached through its own setter, so the declared
	// construction shape above stays exactly as it is: a zero policy keeps every
	// drive unbounded, and the reasons a closing round runs under are named.
	_ func(*Loop, FinalRoundPolicy) = (*Loop).SetFinalRound

	// The per-agent watchdog budget is attached through its own setter, so one
	// drive reads the budget its own agent declares and every other drive reads
	// the default above.
	_ func(*Loop, event.AgentID, FinalRoundPolicy) = (*Loop).SetAgentFinalRound

	_ func(*Loop, map[event.AgentID]FinalRoundPolicy) = (*Loop).SetAgentFinalRounds

	_ = FinalRoundPolicy{MaxRounds: 0, Budget: 0, Timeout: 0, Prompt: ""}

	_ FinalRoundReason = FinalRoundNone

	_ FinalRoundReason = FinalRoundRounds

	_ FinalRoundReason = FinalRoundBudget

	_ HistoryFace = (HistoryFace)(nil)

	_ func(HistoryFace, event.Address) []model.Message = HistoryFace.SessionHistory

	_ func(HistoryFace, event.Address, []model.Message) = HistoryFace.CommitHistory

	// The chain read of the same face is the third method: the drive reads the
	// facts of its range at every decision boundary, and the signature is the one
	// the run's own chain read carries.
	_ func(HistoryFace, event.Address) []event.Event = HistoryFace.SessionFacts

	// The model view material the request snapshot is fixed from keeps its
	// documented fields.
	_ = modelView{modelName: "", promptVersion: "", visibleTools: []string{}, visibleSkills: []string{}}

	_ func(event.EventID) (model.Message, bool) = FactResolver(nil)

	// The commit face and the pipeline face are attached through their own
	// setters, so the declared construction shape above stays exactly as it is:
	// every fact the loop produces lands in the one ledger of the run, and the
	// After pass of a fact committed outside a dispatch is assembled from the
	// published generation of its agent.
	_ FactCommit = (FactCommit)(nil)

	_ func(event.Event) (event.EventID, error) = FactCommit(nil)

	_ func(*Loop, FactCommit) = (*Loop).SetFactCommit

	_ FactPipelines = (FactPipelines)(nil)

	_ func(event.AgentID) (plugin.Pipeline, error) = FactPipelines(nil)

	_ func(*Loop, FactPipelines) = (*Loop).SetFactPipelines

	_ error = ErrFactPipelineUnwired

	// The training-record gap account keeps its declared shape: one counted
	// loss beside the record kind, identity and named failure it carries.
	_ = RecordingLoss{Kind: "", ID: "", Err: nil}

	_ func(*Loop) []RecordingLoss = (*Loop).RecordingLosses
)

// The handoff envelope keeps its declared shape: the five fields the four-segment
// handoff contract maps onto, the validation that refuses an envelope with no
// intent or no acceptance constraint, and the external-context block a delegated
// turn carries in front of its request.
var (
	_ = HandoffEnvelope{
		Intent:           "",
		Constraints:      []string{},
		Grants:           []string{},
		PendingDecisions: []string{},
		Refs:             []event.EventID{},
	}

	_ func(HandoffEnvelope) error = HandoffEnvelope.Validate

	_ func(HandoffEnvelope) string = HandoffEnvelope.ExternalContext

	_ error = ErrHandoffIntentMissing
	_ error = ErrHandoffConstraintsMissing
)

// The request-boundary assembly keeps its declared shape: the causal chain
// identity the assembly is isolated under, the boundary material the request
// face reads, the view it returns, and the face setter beside the closed
// event-type→role table that refuses an undeclared type. No projection entry
// shape is mirrored here: the face crosses built-in, event, model and state
// types only.
var (
	_ = RequestChain{PartitionID: 0, SessionID: ""}

	_ = RequestBoundary{Agent: "", Chain: RequestChain{PartitionID: 0, SessionID: ""}, Binding: state.AgentBinding{}, FinalRound: false}

	_ = BoundaryView{Messages: []model.Message{}, PromptVersion: "", VisibleSkills: []string{}}

	_ RequestFace = (RequestFace)(nil)

	_ func(*Loop, RequestFace) = (*Loop).SetRequestFace

	_ func(string) (model.Role, error) = RoleOf

	_ error = ErrRequestChainSessionRequired

	_ error = ErrProjectionTypeUnmapped
)

package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestDelegationCompletion drives the sub-agent delegation ledger over the real task face and binding holder:
// - a delegated chain binds the initiating generation and the application-selected target, never the global latest table, and keeps each owner's view apart
// - the delivery barrier books before the spawn and pairs with every new obligation
// - a dedup or a refused admission withdraws only its own pre-registration and leaves existing obligations untouched
// - the terminal is reported only after the obligations clear
// - a multi-level notification locates the owner that issued its own call, and a repeated transport records no second fact
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestDelegationCompletion(t *testing.T) {
	t.Run("a delegated chain binds the initiating generation and keeps each owner's view apart", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		modelA1, modelB1, modelC1 := delegationModels("gen-1")
		modelA2, modelB2, modelC2 := delegationModels("gen-2")
		rig := newDelegationRig(t, 0)
		rig.publish(t, map[event.AgentID]model.Model{
			"agent-a": modelA1, "agent-b": modelB1, "agent-c": modelC1,
		})
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		addrC := event.Address{AgentID: "agent-c", SessionID: "session-c"}

		turnA := rig.driveTurn(t, ctx, rig.bindings, "inv-a", event.Address{AgentID: "agent-a", SessionID: "session-a"}, "run a")
		if len(turnA.Decisions) != 1 || turnA.Decisions[0].Binding.Model != modelA1 {
			t.Fatalf("turn a = %d decisions on %v, want one decision on the first generation", len(turnA.Decisions), turnA.Decisions)
		}
		handleA, err := rig.bindings.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow the initiating generation: %v", err)
		}
		refA := handleA.Ref

		receiptA, err := rig.ledger.Delegate(ctx, agent.Delegation{
			InvocationID: "inv-a",
			DecisionID:   turnA.Decisions[0].DecisionID,
			CallID:       "call-a-1",
			Target:       addrB,
			Ref:          refA,
		})
		if err != nil {
			t.Fatalf("delegate a to b: %v", err)
		}
		if receiptA.Kind != task.AdmissionFresh || !receiptA.Accepted {
			t.Fatalf("receipt a = %+v, want one fresh admission", receiptA)
		}
		t1 := receiptA.TaskID
		if err := handleA.Release(ctx); err != nil {
			t.Fatalf("release the initiating borrow: %v", err)
		}

		rig.publish(t, map[event.AgentID]model.Model{
			"agent-a": modelA2, "agent-b": modelB2, "agent-c": modelC2,
		})
		recordA := rig.ledger.View("inv-a")
		if len(recordA) != 1 {
			t.Fatalf("view of inv-a = %+v, want exactly the one delegation it issued", recordA)
		}
		rec := recordA[0]
		if rec.TaskID != t1 || rec.InvocationID != "inv-a" || rec.DecisionID != turnA.Decisions[0].DecisionID ||
			rec.CallID != "call-a-1" || rec.Target != addrB || rec.Ref != refA {
			t.Fatalf("delegation record = %+v, want the call association of the admitted call on target %v", rec, addrB)
		}
		if rec.Status != task.StatusAccepted {
			t.Fatalf("delegation status = %v, want the accepted admission as its start", rec.Status)
		}
		if _, err := rig.bindings.BorrowDerived(ctx, state.BindingRef{
			AgentID: "agent-b", InstanceID: refA.InstanceID, Generation: refA.Generation,
		}); err != nil {
			t.Fatalf("borrow the initiating generation behind the pin: %v", err)
		}

		borrowB, err := rig.ledger.TurnBorrow(t1)
		if err != nil {
			t.Fatalf("take the delegated turn's borrowing face: %v", err)
		}
		turnB := rig.driveTurn(t, ctx, borrowB, "inv-b", addrB, "run b")
		if len(turnB.Decisions) != 1 || turnB.Decisions[0].Binding.Model != modelB1 {
			t.Fatalf("turn b bound %v, want the generation the initiating reference named", turnB.Decisions)
		}
		latest, err := rig.bindings.Borrow(ctx, "agent-b")
		if err != nil {
			t.Fatalf("borrow the latest generation: %v", err)
		}
		if latest.Binding.Model != modelB2 {
			t.Fatalf("root borrow bound %v, want the latest generation beside the delegated one", latest.Binding.Model)
		}
		if err := latest.Release(ctx); err != nil {
			t.Fatalf("release the latest borrow: %v", err)
		}

		handleB, err := borrowB.Borrow(ctx, "agent-b")
		if err != nil {
			t.Fatalf("borrow the delegated generation for the nested reference: %v", err)
		}
		refB := handleB.Ref
		receiptB, err := rig.ledger.Delegate(ctx, agent.Delegation{
			InvocationID: "inv-b",
			DecisionID:   turnB.Decisions[0].DecisionID,
			CallID:       "call-b-1",
			Target:       addrC,
			Ref:          refB,
		})
		if err != nil {
			t.Fatalf("delegate b to c: %v", err)
		}
		t2 := receiptB.TaskID
		if err := handleB.Release(ctx); err != nil {
			t.Fatalf("release the nested initiating borrow: %v", err)
		}
		borrowC, err := rig.ledger.TurnBorrow(t2)
		if err != nil {
			t.Fatalf("take the nested turn's borrowing face: %v", err)
		}
		turnC := rig.driveTurn(t, ctx, borrowC, "inv-c", addrC, "run c")
		if len(turnC.Decisions) != 1 || turnC.Decisions[0].Binding.Model != modelC1 {
			t.Fatalf("turn c bound %v, want the initiating generation carried through the chain", turnC.Decisions)
		}

		if got := rig.ledger.View("inv-b"); len(got) != 1 || got[0].TaskID != t2 {
			t.Fatalf("view of inv-b = %+v, want exactly the delegation it issued", got)
		}
		if got := rig.ledger.View("inv-c"); len(got) != 0 {
			t.Fatalf("view of inv-c = %+v, want no delegation of its own", got)
		}
		if owner, ok := rig.ledger.OwnerOf(t1); !ok || owner != "inv-a" {
			t.Fatalf("owner of %s = (%s, %v), want the invocation that issued its call", t1, owner, ok)
		}
		if owner, ok := rig.ledger.OwnerOf(t2); !ok || owner != "inv-b" {
			t.Fatalf("owner of %s = (%s, %v), want the invocation that issued its call", t2, owner, ok)
		}

		notice2, err := rig.ledger.Complete(ctx, t2, task.StatusSettled, "fact-t2")
		if err != nil {
			t.Fatalf("complete the nested delegation: %v", err)
		}
		if notice2.Kind != task.NoticeSettled || notice2.TaskID != t2 {
			t.Fatalf("settle notice of %s = %+v, want the terminal notification of the nested call", t2, notice2)
		}
		if got := rig.ledger.View("inv-b")[0].Status; got != task.StatusSettled {
			t.Fatalf("nested delegation status = %v, want the terminal landed once its obligations cleared", got)
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations = %d, want the first-level delegation still booked", rig.ledger.Pending())
		}
		if _, err := rig.ledger.Complete(ctx, t1, task.StatusSettled, "fact-t1"); err != nil {
			t.Fatalf("complete the first-level delegation: %v", err)
		}
		if got := rig.ledger.View("inv-a")[0].Status; got != task.StatusSettled {
			t.Fatalf("first-level delegation status = %v, want its terminal landed too", got)
		}
		if rig.ledger.Pending() != 0 {
			t.Fatalf("pending delegations = %d, want every booking closed by a landed terminal", rig.ledger.Pending())
		}
	})

	t.Run("a booking pairs with every new obligation and lands only once the obligations clear", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		modelA, modelB, _ := delegationModels("gen-1")
		rig := newDelegationRig(t, 1)
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		req := agent.Delegation{
			InvocationID: "inv-a",
			DecisionID:   "decision-a",
			CallID:       "call-1",
			Target:       addrB,
			Ref:          refA,
		}

		if _, err := rig.ledger.Delegate(ctx, agent.Delegation{Target: addrB, Ref: refA}); !errors.Is(err, agent.ErrDelegationIdentityMissing) {
			t.Fatalf("delegate without a call identity = %v, want the named refusal", err)
		}
		incomplete := req
		incomplete.CallID = "call-incomplete"
		incomplete.Target = event.Address{AgentID: "agent-b"}
		if _, err := rig.ledger.Delegate(ctx, incomplete); !errors.Is(err, agent.ErrDelegationAddressIncomplete) {
			t.Fatalf("delegate to an incomplete target = %v, want the named refusal", err)
		}
		if rig.ledger.Pending() != 0 {
			t.Fatalf("pending delegations = %d, want no booking beside a refused admission", rig.ledger.Pending())
		}

		receipt, err := rig.ledger.Delegate(ctx, req)
		if err != nil {
			t.Fatalf("delegate the fresh call: %v", err)
		}
		if receipt.Kind != task.AdmissionFresh || !receipt.Accepted {
			t.Fatalf("receipt = %+v, want one fresh admission", receipt)
		}
		t1 := receipt.TaskID
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations = %d, want the fresh admission booked", rig.ledger.Pending())
		}
		open := rig.obligations(t, ctx, t1)
		if open.Count != 1 {
			t.Fatalf("obligations after admission = %+v, want the one obligation the admission opened", open)
		}

		rig.deliverProgress(t, ctx, t1, 0)
		rig.deliverProgress(t, ctx, t1, 1)
		record := rig.ledger.View("inv-a")[0]
		if record.Status != task.StatusProgress || len(record.Progress) != 2 {
			t.Fatalf("record after progress = (status %v, %d notices), want the running phase under both identities", record.Status, len(record.Progress))
		}
		if err := rig.ledger.Observe(ctx, delegationNotice(t1, task.NoticeProgress, 0)); err != nil {
			t.Fatalf("re-deliver one progress notification: %v", err)
		}
		if got := len(rig.ledger.View("inv-a")[0].Progress); got != 2 {
			t.Fatalf("progress notices after a repeated delivery = %d, want no second fact under the same identity", got)
		}

		baseline := rig.obligations(t, ctx, t1)
		dedup, err := rig.ledger.Delegate(ctx, req)
		if err != nil {
			t.Fatalf("re-delegate the same call: %v", err)
		}
		if dedup.Kind != task.AdmissionDedup || dedup.TaskID != t1 {
			t.Fatalf("dedup receipt = %+v, want the task of the first admission", dedup)
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations = %d, want the dedup booking withdrawn", rig.ledger.Pending())
		}
		if got := rig.obligations(t, ctx, t1); got.Count != baseline.Count {
			t.Fatalf("obligations after dedup = %+v, want the existing obligations untouched (%+v)", got, baseline)
		}

		refused := req
		refused.CallID = "call-gated"
		gate, err := rig.ledger.Delegate(ctx, refused)
		if gate.Accepted || gate.Kind != task.AdmissionRejected || !errors.Is(err, task.ErrConcurrencyLimit) {
			t.Fatalf("gate receipt = (%+v, %v), want the named refusal beside a rejected receipt", gate, err)
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations = %d, want the refused booking withdrawn", rig.ledger.Pending())
		}
		if got := rig.obligations(t, ctx, t1); got.Count != baseline.Count {
			t.Fatalf("obligations after refusal = %+v, want the existing obligations untouched (%+v)", got, baseline)
		}

		rig.delivery.set(task.DeliveryPending)
		notice, err := rig.ledger.Complete(ctx, t1, task.StatusSettled, "fact-t1")
		if err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		if notice.Kind != task.NoticeSettled || notice.TaskID != t1 {
			t.Fatalf("settle notice = %+v, want the terminal notification of the call", notice)
		}
		if got := rig.ledger.View("inv-a")[0].Status; got == task.StatusSettled {
			t.Fatalf("terminal reported while its delivery obligation was still open")
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations = %d, want the unsettled delegation still booked", rig.ledger.Pending())
		}
		if got := rig.obligations(t, ctx, t1); got.Count == 0 {
			t.Fatalf("obligations after an unaccepted delivery = %+v, want the delivery obligation kept", got)
		}

		rig.delivery.set(task.DeliveryAccepted)
		if err := rig.tasks.Flush(ctx, t1); err != nil {
			t.Fatalf("flush the settled notification: %v", err)
		}
		status, err := rig.ledger.Reconcile(ctx, t1)
		if err != nil {
			t.Fatalf("reconcile the settled delegation: %v", err)
		}
		if status != task.StatusSettled {
			t.Fatalf("reconciled status = %v, want the terminal landed after the obligations cleared", status)
		}
		if rig.ledger.Pending() != 0 {
			t.Fatalf("pending delegations = %d, want the booking closed by the landed terminal", rig.ledger.Pending())
		}
		if got := rig.obligations(t, ctx, t1); got.Count != 0 {
			t.Fatalf("obligations after landing = %+v, want the reconciliation barrier cleared", got)
		}

		if err := rig.ledger.Observe(ctx, notice); err != nil {
			t.Fatalf("re-deliver the settle notification: %v", err)
		}
		if got := len(rig.ledger.View("inv-a")[0].Progress); got != 3 {
			t.Fatalf("progress notices after a late repeat = %d, want no second fact under the same identity", got)
		}
		if _, err := rig.ledger.Complete(ctx, t1, task.StatusSettled, "fact-t1"); !errors.Is(err, agent.ErrDelegationTerminal) {
			t.Fatalf("second settle = %v, want the named refusal of a delegation that already reached a terminal", err)
		}
	})

	t.Run("a multi-level notification locates the owner that issued its call", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		modelA, modelB, modelC := delegationModels("gen-1")
		rig := newDelegationRig(t, 0)
		rig.publish(t, map[event.AgentID]model.Model{
			"agent-a": modelA, "agent-b": modelB, "agent-c": modelC,
		})
		refA := rig.borrowRef(t, ctx, "agent-a")
		refB := rig.borrowRef(t, ctx, "agent-b")

		receipt1, err := rig.ledger.Delegate(ctx, agent.Delegation{
			InvocationID: "inv-a", DecisionID: "decision-a", CallID: "call-a-1",
			Target: event.Address{AgentID: "agent-b", SessionID: "session-b"}, Ref: refA,
		})
		if err != nil {
			t.Fatalf("delegate the first level: %v", err)
		}
		receipt2, err := rig.ledger.Delegate(ctx, agent.Delegation{
			InvocationID: "inv-b", DecisionID: "decision-b", CallID: "call-b-1",
			Target: event.Address{AgentID: "agent-c", SessionID: "session-c"}, Ref: refB,
		})
		if err != nil {
			t.Fatalf("delegate the second level: %v", err)
		}
		t1, t2 := receipt1.TaskID, receipt2.TaskID

		reentry := delegationNotice(t2, task.NoticeProgress, 0)
		if err := rig.ledger.Observe(ctx, reentry); err != nil {
			t.Fatalf("deliver the multi-level notification: %v", err)
		}
		if owner, ok := rig.ledger.OwnerOf(t2); !ok || owner != "inv-b" {
			t.Fatalf("owner of %s = (%s, %v), want the invocation that issued its call", t2, owner, ok)
		}
		if got := rig.ledger.View("inv-b"); len(got) != 1 || got[0].TaskID != t2 || len(got[0].Progress) != 1 {
			t.Fatalf("view of inv-b = %+v, want the notification under its own delegation", got)
		}
		if got := rig.ledger.View("inv-a"); len(got) != 1 || got[0].TaskID != t1 || len(got[0].Progress) != 0 {
			t.Fatalf("view of inv-a = %+v, want no notification of a task it never issued", got)
		}

		if err := rig.ledger.Observe(ctx, reentry); err != nil {
			t.Fatalf("re-deliver the multi-level notification: %v", err)
		}
		if got := len(rig.ledger.View("inv-b")[0].Progress); got != 1 {
			t.Fatalf("progress notices after a repeated reentry = %d, want no second fact under the same identity", got)
		}

		if _, err := rig.ledger.Complete(ctx, t2, task.StatusSettled, "fact-t2"); err != nil {
			t.Fatalf("complete the second-level delegation: %v", err)
		}
		if got := rig.ledger.View("inv-b")[0].Status; got != task.StatusSettled {
			t.Fatalf("second-level status = %v, want its terminal landed in its own owner's view", got)
		}
		if got := rig.ledger.View("inv-a")[0].Status; got != task.StatusAccepted {
			t.Fatalf("first-level status = %v, want the untouched admission of the other owner", got)
		}
		if _, err := rig.ledger.Complete(ctx, t1, task.StatusFailed, "fact-t1"); err != nil {
			t.Fatalf("complete the first-level delegation: %v", err)
		}
		if got := rig.ledger.View("inv-a")[0].Status; got != task.StatusFailed {
			t.Fatalf("first-level status = %v, want its own terminal landed in its own owner's view", got)
		}
		late := delegationNotice(t2, task.NoticeSettled, 0)
		if err := rig.ledger.Observe(ctx, late); err != nil {
			t.Fatalf("deliver a late repeat of the nested terminal: %v", err)
		}
		if got := len(rig.ledger.View("inv-b")[0].Progress); got != 2 {
			t.Fatalf("progress notices after a late terminal repeat = %d, want no second fact under the same identity", got)
		}
		if rig.ledger.Pending() != 0 {
			t.Fatalf("pending delegations = %d, want every booking closed by a landed terminal", rig.ledger.Pending())
		}
	})
}

// delegationRig assembles the real task face, binding holder and delegation
// ledger around the scripted models one sub-case publishes: the ledger settles
// its delegations through the manager and registry beside it, and every
// notification the task face delivers routes back into the ledger.
type delegationRig struct {
	instance  event.InstanceID
	bindings  *state.Bindings
	occupancy *state.Occupancy
	tasks     *delegationFace
	ledger    *agent.Delegator
	delivery  *delegationDelivery
}

// delegationFace is the task face the ledger settles its delegations through:
// the manager's admission, obligations and flush beside the registry's status
// transition, as one face.
type delegationFace struct {
	*task.MemoryManager
	*task.MemoryRegistry
}

// delegationDelivery reports the delivery-layer answer of the rig's deliverer,
// so a sub-case can flip between an accepted and an unaccepted delivery between
// flushes.
type delegationDelivery struct {
	mu       sync.Mutex
	accepted task.DeliveryAcceptance
}

// set changes the delivery-layer answer the deliverer reports.
func (d *delegationDelivery) set(accepted task.DeliveryAcceptance) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accepted = accepted
}

// outcome reports the delivery-layer answer the deliverer gives.
func (d *delegationDelivery) outcome() task.DeliveryAcceptance {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.accepted
}

// newDelegationRig assembles the task face and the delegation ledger of one
// sub-case. A positive limit stands at the admission gate the sub-case drives a
// refusal through.
func newDelegationRig(t *testing.T, limit int) *delegationRig {
	t.Helper()
	rig := &delegationRig{
		instance:  event.InstanceID("instance-delegation"),
		bindings:  state.NewBindings(),
		occupancy: state.NewOccupancy(),
		delivery:  &delegationDelivery{accepted: task.DeliveryAccepted},
	}
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission, task.WithConcurrencyLimit(limit))
	manager := task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		if err := rig.ledger.Observe(ctx, notice); err != nil {
			return task.DeliveryRejected, err
		}
		return rig.delivery.outcome(), nil
	})
	rig.tasks = &delegationFace{MemoryManager: manager, MemoryRegistry: registry}
	rig.ledger = agent.NewDelegator(rig.tasks, rig.bindings, rig.instance)
	return rig
}

// publish holds the given models as the generation the next borrow takes.
func (r *delegationRig) publish(t *testing.T, models map[event.AgentID]model.Model) {
	t.Helper()
	agents := make([]config.AgentSpec, 0, len(models))
	bindings := make(map[event.AgentID]state.AgentBinding, len(models))
	for _, id := range []event.AgentID{"agent-a", "agent-b", "agent-c"} {
		provider, ok := models[id]
		if !ok {
			continue
		}
		agents = append(agents, config.AgentSpec{ID: string(id), Model: provider.Info().Name})
		bindings[id] = state.AgentBinding{
			Model:        provider,
			PromptSource: state.PromptSource{Version: "prompt-delegation-v1"},
		}
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: &config.Declaration{Entry: "agent-a", Agents: agents}}, bindings, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	if err := r.bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
}

// driveTurn drives one real invocation on the given borrowing face to its
// terminal and reports the turn it ran.
func (r *delegationRig) driveTurn(t *testing.T, ctx context.Context, borrow state.Borrow, invocation event.InvocationID, addr event.Address, input string) agent.Turn {
	t.Helper()
	loop := agent.NewLoop(borrow, r.occupancy, r.instance, delegationFact)
	turn, err := loop.Drive(ctx, agent.Invocation{
		InvocationID: invocation,
		Address:      addr,
		Instance:     r.instance,
		Ready: []event.Event{{
			Key:       event.EventID("ev-" + string(invocation)),
			Type:      event.TypeExternalInput,
			Timestamp: time.Now(),
			Message:   model.Message{Role: model.RoleUser, Content: input},
		}},
	})
	if err != nil {
		t.Fatalf("drive the turn of %s: %v", invocation, err)
	}
	return turn
}

// borrowRef takes one borrow of the current generation and reports the stable
// reference it carries before the borrow ends again.
func (r *delegationRig) borrowRef(t *testing.T, ctx context.Context, agentID event.AgentID) state.BindingRef {
	t.Helper()
	handle, err := r.bindings.Borrow(ctx, agentID)
	if err != nil {
		t.Fatalf("borrow the current generation of %s: %v", agentID, err)
	}
	ref := handle.Ref
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("release the borrow of %s: %v", agentID, err)
	}
	return ref
}

// deliverProgress moves one progress notification of a task through the real
// task face: the transition carries the notification beside its status change,
// and the flush delivers it back into the ledger under the frozen routing.
func (r *delegationRig) deliverProgress(t *testing.T, ctx context.Context, taskID event.TaskID, seq int64) {
	t.Helper()
	spec, _, err := r.tasks.Lookup(ctx, taskID)
	if err != nil {
		t.Fatalf("look up the delegated task: %v", err)
	}
	notice := delegationNotice(taskID, task.NoticeProgress, seq)
	result, err := r.tasks.Advance(ctx, taskID, task.StatusProgress,
		task.MutationID(fmt.Sprintf("progress:%s:%d", taskID, seq)),
		[]task.NoticeEnvelope{{
			Notice:         notice,
			Target:         spec.Target,
			Source:         spec.Source,
			TargetInstance: spec.InstanceID,
		}})
	if err != nil {
		t.Fatalf("advance the delegated task with a progress notification: %v", err)
	}
	if result != task.CommitCommitted {
		t.Fatalf("progress commit = %v, want the transition stored", result)
	}
	if err := r.tasks.Flush(ctx, taskID); err != nil {
		t.Fatalf("flush the progress notification: %v", err)
	}
}

// obligations reports the obligations one delegated task still owes.
func (r *delegationRig) obligations(t *testing.T, ctx context.Context, taskID event.TaskID) task.Obligations {
	t.Helper()
	open, err := r.tasks.Obligations(ctx, taskID)
	if err != nil {
		t.Fatalf("report the obligations of %s: %v", taskID, err)
	}
	return open
}

// delegationNotice builds one notification of a delegated task under its stable
// identity.
func delegationNotice(taskID event.TaskID, kind task.NoticeKind, seq int64) task.Notice {
	return task.Notice{
		TaskID: taskID,
		Round:  1,
		Kind:   kind,
		Seq:    seq,
		Event:  event.EventID(fmt.Sprintf("fact-%s-%s-%d", taskID, kind, seq)),
	}
}

// delegationFact reports no native message: the turns of these sub-cases drive
// no tool call, so no fact pairing is ever read.
func delegationFact(event.EventID) (model.Message, bool) {
	return model.Message{}, false
}

// delegationModels builds the scripted providers of one generation for the
// three agents of the chain.
func delegationModels(generation string) (*convModel, *convModel, *convModel) {
	answer := func(name string) func(*model.Request) *model.Response {
		return func(*model.Request) *model.Response {
			return convFinalTurn(name + " " + generation)
		}
	}
	return &convModel{name: "model-a-" + generation, answer: answer("a")},
		&convModel{name: "model-b-" + generation, answer: answer("b")},
		&convModel{name: "model-c-" + generation, answer: answer("c")}
}

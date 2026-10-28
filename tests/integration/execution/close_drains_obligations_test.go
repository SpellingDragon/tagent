package execution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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

// TestCloseDrainsObligations drives the close of one agent over the real task face and the producers behind the work:
// - the close refuses new input before its first settlement pass and keeps refusing it afterwards
// - a successful admission keeps its obligation open until real settlement and never frees it beside the receipt
// - a producer answering false is never taken for an end; only a real stop confirmation closes the wait
// - a deadline reports the named reason of every obligation that stayed open, and a cancellation request is never read as an end
// - a repeated close returns the first result and runs no second settlement
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestCloseDrainsObligations(t *testing.T) {
	t.Run("the close refuses new input before its first settlement pass and keeps refusing it afterwards", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
		addrA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}

		t1 := rig.delegate(t, ctx, "call-1", "inv-a", addrB, refA)
		if open := rig.obligations(t, ctx, t1); open.Count != 1 {
			t.Fatalf("obligations beside the receipt = %+v, want the obligation the receipt kept open", open)
		}

		rig.delivery.set(task.DeliveryPending)
		rig.advance(t, ctx, t1, 0)
		probes := make(chan gateProbe, 4096)
		rig.setHook(func(ctx context.Context, _ task.Notice) {
			receipt, err := rig.shutdown.Admit(ctx, task.Spec{CallID: "probe-during-drain"})
			select {
			case probes <- gateProbe{receipt: receipt, err: err}:
			default:
			}
		})

		report, err := rig.shutdown.Close(ctx, agent.CloseRequest{
			Address:  addrA,
			Deadline: time.Now().Add(300 * time.Millisecond),
		})
		rig.setHook(nil)
		if !errors.Is(err, agent.ErrDrainUnsettled) || report.Drained {
			t.Fatalf("close = (%+v, %v), want the named unsettled state beside the unaccepted delivery", report, err)
		}
		close(probes)
		refused := 0
		for probe := range probes {
			if !errors.Is(probe.err, agent.ErrShutdownRefused) || probe.receipt.Accepted {
				t.Fatalf("admission during the drain = (%+v, %v), want a rejected receipt beside the named refusal", probe.receipt, probe.err)
			}
			refused++
		}
		if refused == 0 {
			t.Fatal("the drain ran with no probe of the gate, want new input refused beside the settlement")
		}

		if _, err := rig.ledger.Delegate(ctx, agent.Delegation{
			InvocationID: "inv-a", DecisionID: "decision-2", CallID: "call-2",
			Target: addrB, Ref: refA,
		}); !errors.Is(err, agent.ErrShutdownRefused) {
			t.Fatalf("delegate after the close = %v, want the named refusal of the closed gate", err)
		}
		if receipt, err := rig.shutdown.Admit(ctx, task.Spec{CallID: "call-3"}); !errors.Is(err, agent.ErrShutdownRefused) || receipt.Accepted {
			t.Fatalf("admission after the close = (%+v, %v), want a rejected receipt beside the named refusal", receipt, err)
		}
		if open := rig.obligations(t, ctx, t1); open.Count != 2 {
			t.Fatalf("obligations after the close = %+v, want the accepted work kept where it was", open)
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations after the close = %d, want the booking kept beside the unsettled work", rig.ledger.Pending())
		}
	})

	t.Run("a successful admission keeps its obligation until real settlement and the drain reports one only then", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
		addrA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		t1 := rig.delegate(t, ctx, "call-1", "inv-a", addrB, refA)

		if open := rig.obligations(t, ctx, t1); open.Count != 1 || open.Open[0].Kind != task.ObligationAwaitingTool {
			t.Fatalf("obligations beside the receipt = %+v, want the admission obligation kept open", open)
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations beside the receipt = %d, want the booking kept beside the open obligation", rig.ledger.Pending())
		}

		rig.delivery.set(task.DeliveryPending)
		if _, err := rig.ledger.Complete(ctx, t1, task.StatusSettled, "fact-t1"); err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		if open := rig.obligations(t, ctx, t1); open.Count != 1 || open.Open[0].Kind != task.ObligationAwaitingDelivery {
			t.Fatalf("obligations beside the unaccepted delivery = %+v, want the delivery obligation kept open", open)
		}
		if got := rig.ledger.View("inv-a")[0].Status; got == task.StatusSettled {
			t.Fatal("terminal reported while its delivery obligation was still open")
		}
		if rig.ledger.Pending() != 1 {
			t.Fatalf("pending delegations = %d, want the unsettled delegation still booked", rig.ledger.Pending())
		}

		rig.delivery.set(task.DeliveryAccepted)
		deliveries := rig.deliveryCount()
		report, err := rig.shutdown.Close(ctx, agent.CloseRequest{
			Address:  addrA,
			Deadline: time.Now().Add(10 * time.Second),
		})
		if err != nil || !report.Drained || len(report.Unsettled) != 0 {
			t.Fatalf("close = (%+v, %v), want the drained report beside real settlement", report, err)
		}
		if got := rig.deliveryCount() - deliveries; got < 1 {
			t.Fatalf("deliveries during the close = %d, want the drain to offer the notification it still owed", got)
		}
		if open := rig.obligations(t, ctx, t1); open.Count != 0 {
			t.Fatalf("obligations after the drain = %+v, want every obligation settled for real", open)
		}
		if rig.ledger.Pending() != 0 {
			t.Fatalf("pending delegations after the drain = %d, want every booking closed by a landed terminal", rig.ledger.Pending())
		}
		if got := rig.ledger.View("inv-a")[0].Status; got != task.StatusSettled {
			t.Fatalf("record after the drain = %v, want the terminal landed once its obligations cleared", got)
		}
	})

	t.Run("a non-cooperative producer is never taken for an end", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
		addrA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		t1 := rig.delegate(t, ctx, "call-1", "inv-a", addrB, refA)
		if _, err := rig.ledger.Complete(ctx, t1, task.StatusSettled, "fact-t1"); err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		if open := rig.obligations(t, ctx, t1); open.Count != 0 {
			t.Fatalf("obligations before the close = %+v, want the obligations cleared for real", open)
		}
		if rig.ledger.Pending() != 0 {
			t.Fatalf("pending delegations before the close = %d, want the queue emptied", rig.ledger.Pending())
		}

		if err := rig.shutdown.Watch("", (&closeProducer{}).Stopped); !errors.Is(err, agent.ErrProducerIdentityMissing) {
			t.Fatalf("watch under no task identity = %v, want the named refusal", err)
		}
		if err := rig.shutdown.Watch(t1, nil); !errors.Is(err, agent.ErrProducerMissing) {
			t.Fatalf("watch naming no producer = %v, want the named refusal", err)
		}

		stuck := &closeProducer{}
		if err := rig.shutdown.Watch(t1, stuck.Stopped); err != nil {
			t.Fatalf("watch the producer: %v", err)
		}
		report, err := rig.shutdown.Close(ctx, agent.CloseRequest{
			Address:  addrA,
			Deadline: time.Now().Add(150 * time.Millisecond),
		})
		if !errors.Is(err, agent.ErrDrainUnsettled) || report.Drained {
			t.Fatalf("close = (%+v, %v), want the named unsettled state beside a producer that never stops", report, err)
		}
		want := []agent.UnsettledObligation{{TaskID: t1, Reason: "the producer has not confirmed its stop"}}
		if !reflect.DeepEqual(report.Unsettled, want) {
			t.Fatalf("unsettled = %+v, want the producer named beside its reason", report.Unsettled)
		}
		if calls := stuck.calls(); calls < 2 {
			t.Fatalf("producer polls = %d, want the close to keep polling until the deadline", calls)
		}

		flipRig := newCloseRig(t)
		flipRig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		flipRef := flipRig.borrowRef(t, ctx, "agent-a")
		flipTask := flipRig.delegate(t, ctx, "call-1", "inv-a", addrB, flipRef)
		if _, err := flipRig.ledger.Complete(ctx, flipTask, task.StatusSettled, "fact-flip"); err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		flipping := &closeProducer{flipAt: 3}
		if err := flipRig.shutdown.Watch(flipTask, flipping.Stopped); err != nil {
			t.Fatalf("watch the producer: %v", err)
		}
		flipReport, flipErr := flipRig.shutdown.Close(ctx, agent.CloseRequest{
			Address:  addrA,
			Deadline: time.Now().Add(10 * time.Second),
		})
		if flipErr != nil || !flipReport.Drained || len(flipReport.Unsettled) != 0 {
			t.Fatalf("close = (%+v, %v), want the drain beside the confirmed stop", flipReport, flipErr)
		}
		if calls := flipping.calls(); calls < 3 {
			t.Fatalf("producer polls = %d, want the false answers polled until the real stop", calls)
		}
	})

	t.Run("a deadline reports the named reason of every obligation that stayed open", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
		addrA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		t1 := rig.delegate(t, ctx, "call-1", "inv-a", addrB, refA)

		rig.delivery.set(task.DeliveryRejected)
		rig.setDeliverErr(errors.New("the target refused the delivery"))
		rig.advance(t, ctx, t1, 0)

		receipt, err := rig.tasks.RequestCancel(ctx, t1)
		if err != nil || !receipt.Requested {
			t.Fatalf("request the cancellation = (%+v, %v), want the control receipt recorded", receipt, err)
		}

		report, err := rig.shutdown.Close(ctx, agent.CloseRequest{
			Address:  addrA,
			Deadline: time.Now().Add(150 * time.Millisecond),
		})
		if !errors.Is(err, agent.ErrDrainUnsettled) || report.Drained {
			t.Fatalf("close = (%+v, %v), want the named unsettled state beside the deadline", report, err)
		}
		want := []agent.UnsettledObligation{
			{TaskID: t1, Reason: "the awaiting-tool obligation has not settled"},
			{TaskID: t1, Reason: "the target refused the delivery"},
		}
		if !reflect.DeepEqual(report.Unsettled, want) {
			t.Fatalf("unsettled = %+v, want the named reason of every obligation that stayed open", report.Unsettled)
		}
		if _, status, err := rig.tasks.Lookup(ctx, t1); err != nil || status != task.StatusRunning {
			t.Fatalf("task state after the cancellation request = (%v, %v), want the work standing where the request found it", status, err)
		}
	})

	t.Run("a repeated close returns the first result and runs no second settlement", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
		addrA := event.Address{AgentID: "agent-a", SessionID: "session-a"}
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		t1 := rig.delegate(t, ctx, "call-1", "inv-a", addrB, refA)
		rig.delivery.set(task.DeliveryPending)
		rig.advance(t, ctx, t1, 0)

		first, errFirst := rig.shutdown.Close(ctx, agent.CloseRequest{
			Address:  addrA,
			Deadline: time.Now().Add(120 * time.Millisecond),
		})
		if !errors.Is(errFirst, agent.ErrDrainUnsettled) || first.Drained {
			t.Fatalf("first close = (%+v, %v), want the named unsettled state beside the deadline", first, errFirst)
		}
		deliveries := rig.deliveryCount()

		rig.delivery.set(task.DeliveryAccepted)
		second, errSecond := rig.shutdown.Close(ctx, agent.CloseRequest{
			Address:  addrA,
			Deadline: time.Now().Add(10 * time.Second),
		})
		if !reflect.DeepEqual(second, first) || errSecond != errFirst {
			t.Fatalf("second close = (%+v, %v), want the very first result (%+v, %v)", second, errSecond, first, errFirst)
		}
		if got := rig.deliveryCount(); got != deliveries {
			t.Fatalf("deliveries beside the second close = %d, want no second settlement (%d)", got, deliveries)
		}
		if open := rig.obligations(t, ctx, t1); open.Count != 2 {
			t.Fatalf("obligations beside the second close = %+v, want the accepted work untouched", open)
		}

		raceRig := newCloseRig(t)
		raceRig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		raceRef := raceRig.borrowRef(t, ctx, "agent-a")
		raceTask := raceRig.delegate(t, ctx, "call-1", "inv-a", addrB, raceRef)
		if _, err := raceRig.ledger.Complete(ctx, raceTask, task.StatusSettled, "fact-race"); err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		flipping := &closeProducer{flipAt: 8}
		if err := raceRig.shutdown.Watch(raceTask, flipping.Stopped); err != nil {
			t.Fatalf("watch the producer: %v", err)
		}
		results := make([]struct {
			report agent.DrainReport
			err    error
		}, 2)
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i].report, results[i].err = raceRig.shutdown.Close(ctx, agent.CloseRequest{
					Address:  addrA,
					Deadline: time.Now().Add(10 * time.Second),
				})
			}(i)
		}
		wg.Wait()
		for i := range results {
			if !reflect.DeepEqual(results[i], results[0]) {
				t.Fatalf("concurrent close %d = (%+v, %v), want the very first result (%+v, %v)", i, results[i].report, results[i].err, results[0].report, results[0].err)
			}
		}
		if !results[0].report.Drained || results[0].err != nil {
			t.Fatalf("concurrent close = (%+v, %v), want the drain beside the confirmed stop", results[0].report, results[0].err)
		}
	})
}

// closeRig assembles the close of one agent over the real task face and the
// delegation ledger: the ledger admits through the close face, so the gate
// stands exactly where a new obligation would open and the close settles
// exactly what the gate accepted.
type closeRig struct {
	instance event.InstanceID
	bindings *state.Bindings
	tasks    *delegationFace
	shutdown *agent.Shutdown
	ledger   *agent.Delegator
	delivery *delegationDelivery

	mu         sync.Mutex
	hook       func(context.Context, task.Notice)
	deliverErr error
	deliveries int
}

// newCloseRig assembles the task face, the close face and the delegation ledger
// of one sub-case. The ledger admits through the close face and the close lands
// the terminals of admitted calls through the ledger.
func newCloseRig(t *testing.T) *closeRig {
	t.Helper()
	rig := &closeRig{
		instance: event.InstanceID("instance-close"),
		bindings: state.NewBindings(),
		delivery: &delegationDelivery{accepted: task.DeliveryAccepted},
	}
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	manager := task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		rig.mu.Lock()
		rig.deliveries++
		hook, deliverErr := rig.hook, rig.deliverErr
		rig.mu.Unlock()
		if hook != nil {
			hook(ctx, notice)
		}
		if err := rig.ledger.Observe(ctx, notice); err != nil {
			return task.DeliveryRejected, err
		}
		return rig.delivery.outcome(), deliverErr
	})
	rig.tasks = &delegationFace{MemoryManager: manager, MemoryRegistry: registry}
	rig.shutdown = agent.NewShutdown(rig.tasks)
	rig.ledger = agent.NewDelegator(rig.shutdown, rig.bindings, rig.instance)
	rig.shutdown.SetReconciler(rig.ledger)
	return rig
}

// publish holds the given models as the generation the next borrow takes.
func (r *closeRig) publish(t *testing.T, models map[event.AgentID]model.Model) {
	t.Helper()
	agents := make([]config.AgentSpec, 0, len(models))
	bindings := make(map[event.AgentID]state.AgentBinding, len(models))
	for _, id := range []event.AgentID{"agent-a", "agent-b"} {
		provider, ok := models[id]
		if !ok {
			continue
		}
		agents = append(agents, config.AgentSpec{ID: string(id), Model: provider.Info().Name})
		bindings[id] = state.AgentBinding{
			Model:        provider,
			PromptSource: state.PromptSource{Version: "prompt-close-v1"},
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

// borrowRef takes one borrow of the current generation and reports the stable
// reference it carries before the borrow ends again.
func (r *closeRig) borrowRef(t *testing.T, ctx context.Context, agentID event.AgentID) state.BindingRef {
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

// delegate admits one delegated call through the close face and reports the
// task it was admitted as.
func (r *closeRig) delegate(t *testing.T, ctx context.Context, callID string, invocation event.InvocationID, target event.Address, ref state.BindingRef) event.TaskID {
	t.Helper()
	receipt, err := r.ledger.Delegate(ctx, agent.Delegation{
		InvocationID: invocation,
		DecisionID:   event.DecisionID("decision-" + callID),
		CallID:       callID,
		Target:       target,
		Ref:          ref,
	})
	if err != nil {
		t.Fatalf("delegate %s: %v", callID, err)
	}
	if receipt.Kind != task.AdmissionFresh || !receipt.Accepted {
		t.Fatalf("receipt of %s = %+v, want one fresh admission", callID, receipt)
	}
	return receipt.TaskID
}

// advance moves one task on beside a progress notification and delivers that
// notification: whatever the delivery answers, the call returns only when the
// delivery face had its say.
func (r *closeRig) advance(t *testing.T, ctx context.Context, taskID event.TaskID, seq int64) {
	t.Helper()
	spec, _, err := r.tasks.Lookup(ctx, taskID)
	if err != nil {
		t.Fatalf("look up the delegated task: %v", err)
	}
	notice := delegationNotice(taskID, task.NoticeProgress, seq)
	result, err := r.tasks.Advance(ctx, taskID, task.StatusRunning,
		task.MutationID(fmt.Sprintf("progress:%s:%d", taskID, seq)),
		[]task.Envelope{{
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
func (r *closeRig) obligations(t *testing.T, ctx context.Context, taskID event.TaskID) task.Obligations {
	t.Helper()
	open, err := r.tasks.Obligations(ctx, taskID)
	if err != nil {
		t.Fatalf("report the obligations of %s: %v", taskID, err)
	}
	return open
}

// setHook installs the probe that stands beside the settlement passes of a
// drain.
func (r *closeRig) setHook(hook func(context.Context, task.Notice)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hook = hook
}

// setDeliverErr records the refusal reason the deliverer answers with.
func (r *closeRig) setDeliverErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliverErr = err
}

// deliveryCount reports how often the deliverer carried one notification.
func (r *closeRig) deliveryCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deliveries
}

// gateProbe is one new input offered beside a drain and the answer the gate
// gave it.
type gateProbe struct {
	receipt task.AdmissionReceipt
	err     error
}

// closeProducer stands behind one admitted call as the producer whose real stop
// the close must confirm. It answers false until its flip arrives; a zero flip
// never confirms a stop, which is exactly the non-cooperative case.
type closeProducer struct {
	mu     sync.Mutex
	flipAt int
	polls  int
}

// Stopped reports this producer's stop confirmation and counts every poll the
// close makes.
func (p *closeProducer) Stopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	return p.flipAt > 0 && p.polls >= p.flipAt
}

// calls reports how often the close polled this producer for its stop.
func (p *closeProducer) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.polls
}

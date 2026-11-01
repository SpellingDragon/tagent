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

// closeCriterion is the rest criterion of the close case: the task face of the
// rig, beside the mailbox answer no notification of this case stands in.
type closeCriterion struct {
	*task.MemoryManager
	*task.MemoryRegistry
}

// Unconsumed reports the notifications a mailbox accepted and nobody consumed;
// this case settles its notifications through the manager alone.
func (closeCriterion) Unconsumed(context.Context) ([]task.UnsettledNotice, error) {
	return nil, nil
}

// TestCloseDrainsObligations drives the close of one agent over the real task face and the writers behind the work:
// - a successful admission keeps its obligation open until real settlement and the close reports one only then
// - a writer answering no stop is never taken for an end: the close stands while no stop is confirmed and converges only once the real stop arrives
// - a spent budget reports the criterion as it stands, keeping the work where the request found it
// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
func TestCloseDrainsObligations(t *testing.T) {
	t.Run("a successful admission keeps its obligation until real settlement and the close reports one only then", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
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
		if read := task.ReadUnsettled(ctx, rig.criterion(), nil); read.Resting() {
			t.Fatalf("criterion = %+v, want the unaccepted delivery read as work not at rest", read)
		}

		rig.delivery.set(task.DeliveryAccepted)
		deliveries := rig.deliveryCount()
		report, err := rig.close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)})
		if err != nil || !report.Drained {
			t.Fatalf("close = (%+v, %v), want the close reached on the real settlement", report, err)
		}
		if got := rig.deliveryCount(); got <= deliveries {
			t.Fatalf("deliveries beside the close = %d, want the owed notification offered (%d before)", got, deliveries)
		}
		if open := rig.obligations(t, ctx, t1); open.Count != 0 {
			t.Fatalf("obligations after the close = %+v, want the accepted delivery to clear it", open)
		}
		if rig.ledger.Pending() != 0 {
			t.Fatalf("pending delegations after the close = %d, want the landed terminal withdrawn", rig.ledger.Pending())
		}
	})

	t.Run("a writer that has not confirmed its stop is never taken for an end", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
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
		if read := task.ReadUnsettled(ctx, rig.criterion(), nil); !read.Resting() {
			t.Fatalf("criterion before the close = %+v, want the work at rest", read)
		}

		stuck := rig.writers.Begin("producer:call-1")
		defer stuck()
		report, err := rig.close(ctx, agent.CloseRequest{Timeout: time.Now().Add(150 * time.Millisecond)})
		if !errors.Is(err, agent.ErrDrainUnsettled) || report.Drained {
			t.Fatalf("close = (%+v, %v), want the named unsettled state beside a writer that never stops", report, err)
		}
		want := []agent.UnsettledObligation{{Reason: "this writer has not confirmed its stop: producer:call-1"}}
		if !reflect.DeepEqual(report.Unsettled, want) {
			t.Fatalf("unsettled = %+v, want the writer named beside its reason", report.Unsettled)
		}

		flipRig := newCloseRig(t)
		flipRig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		flipRef := flipRig.borrowRef(t, ctx, "agent-a")
		flipTask := flipRig.delegate(t, ctx, "call-1", "inv-a", addrB, flipRef)
		if _, err := flipRig.ledger.Complete(ctx, flipTask, task.StatusSettled, "fact-flip"); err != nil {
			t.Fatalf("settle the delegation: %v", err)
		}
		stop := flipRig.writers.Begin("producer:call-1")
		flipped := make(chan closeOutcome, 1)
		go func() {
			report, err := flipRig.close(ctx, agent.CloseRequest{Timeout: time.Now().Add(10 * time.Second)})
			flipped <- closeOutcome{report: report, err: err}
		}()
		select {
		case got := <-flipped:
			t.Fatalf("close = (%+v, %v) beside a writer that has confirmed no stop, want the close to stand until the real stop arrives", got.report, got.err)
		case <-time.After(50 * time.Millisecond):
		}
		stop()
		select {
		case got := <-flipped:
			if got.err != nil || !got.report.Drained || len(got.report.Unsettled) != 0 {
				t.Fatalf("close = (%+v, %v), want the close reached on the real stop", got.report, got.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the close stood past the confirmed stop, want the close reached once the writer confirmed its stop")
		}
	})

	t.Run("a spent budget reports the criterion as it stands", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rig := newCloseRig(t)
		modelA, modelB, _ := delegationModels("gen-1")
		rig.publish(t, map[event.AgentID]model.Model{"agent-a": modelA, "agent-b": modelB})
		refA := rig.borrowRef(t, ctx, "agent-a")
		addrB := event.Address{AgentID: "agent-b", SessionID: "session-b"}
		t1 := rig.delegate(t, ctx, "call-1", "inv-a", addrB, refA)

		rig.delivery.set(task.DeliveryRejected)
		rig.setDeliverErr(errors.New("the target refused the delivery"))
		rig.advance(t, ctx, t1, 0)

		receipt, err := rig.tasks.RequestCancel(ctx, t1)
		if err != nil || !receipt.Requested {
			t.Fatalf("request the cancellation = (%+v, %v), want the control receipt recorded", receipt, err)
		}

		report, err := rig.close(ctx, agent.CloseRequest{Timeout: time.Now().Add(150 * time.Millisecond)})
		if !errors.Is(err, agent.ErrDrainUnsettled) || report.Drained {
			t.Fatalf("close = (%+v, %v), want the named unsettled state beside the spent budget", report, err)
		}
		named := false
		for _, open := range report.Unsettled {
			if open.TaskID == t1 {
				named = true
			}
		}
		if !named {
			t.Fatalf("unsettled = %+v, want the work that stayed open named beside the report", report.Unsettled)
		}
		read := task.ReadUnsettled(ctx, rig.criterion(), nil)
		if len(read.Work) != 1 || read.Work[0].TaskID != t1 {
			t.Fatalf("criterion work = %+v, want the one task that stayed open", read.Work)
		}
		if _, status, err := rig.tasks.Lookup(ctx, t1); err != nil || status != task.StatusRunning {
			t.Fatalf("task state after the cancellation request = (%v, %v), want the work standing where the request found it", status, err)
		}
	})
}

// closeRig assembles the close of one agent over the real task face and the
// delegation ledger: the ledger admits through the task face and lands the
// terminals it holds, so the close settles exactly what the ledger admitted.
type closeRig struct {
	instance event.InstanceID
	bindings *state.Bindings
	tasks    *delegationFace
	manager  *task.MemoryManager
	registry *task.MemoryRegistry
	writers  *agent.Writers
	ledger   *agent.Delegator
	delivery *delegationDelivery

	mu         sync.Mutex
	known      []event.TaskID
	deliverErr error
	deliveries int
}

// newCloseRig assembles the task face, the writer ledger and the delegation
// ledger of one sub-case. The ledger admits through the task face and the close
// lands the terminals of admitted calls through the ledger.
func newCloseRig(t *testing.T) *closeRig {
	t.Helper()
	rig := &closeRig{
		instance: event.InstanceID("instance-close"),
		bindings: state.NewBindings(),
		delivery: &delegationDelivery{accepted: task.DeliveryAccepted},
		writers:  agent.NewWriters(),
	}
	admission := task.NewMemoryAdmission()
	rig.registry = task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	rig.manager = task.NewMemoryManager(rig.registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		rig.mu.Lock()
		rig.deliveries++
		deliverErr := rig.deliverErr
		rig.mu.Unlock()
		if err := rig.ledger.Observe(ctx, notice); err != nil {
			return task.DeliveryRejected, err
		}
		return rig.delivery.outcome(), deliverErr
	})
	rig.tasks = &delegationFace{MemoryManager: rig.manager, MemoryRegistry: rig.registry}
	rig.ledger = agent.NewDelegator(rig.tasks, rig.bindings, rig.instance)
	return rig
}

// criterion reports the rest criterion of the rig: the task face and the mailbox
// answer this case stands in.
func (r *closeRig) criterion() closeCriterion {
	return closeCriterion{MemoryManager: r.manager, MemoryRegistry: r.registry}
}

// close converges the rig to rest the way the run's own close does: it reads the
// one criterion beside the writer ledger and the bookings the delegation ledger
// still holds, offers the notifications still owed, lands the terminals whose
// obligations cleared and reads again. It runs while the run is not at rest, its
// writers have not all stopped or a terminal is still unlanded, and answers the
// criterion as it stands once the budget is spent.
func (r *closeRig) close(ctx context.Context, req agent.CloseRequest) (agent.DrainReport, error) {
	timeout, err := req.Resolve()
	if err != nil {
		return agent.DrainReport{Outcome: agent.CloseBroken}, err
	}
	var read task.Unsettled
	drained := false
	for {
		read = task.ReadUnsettled(ctx, r.criterion(), nil)
		stopped, _ := r.writers.Stopped()
		if read.Resting() && stopped && r.ledger.Pending() == 0 {
			drained = true
			break
		}
		if ctx.Err() != nil || !time.Now().Before(timeout) {
			break
		}
		r.offer(ctx)
		select {
		case <-ctx.Done():
		case <-time.After(time.Millisecond):
		}
	}
	stopped, open := r.writers.Stopped()
	report := agent.DrainReport{Drained: drained, Unsettled: writerlessUnsettled(read)}
	if !stopped {
		report.Unsettled = append(report.Unsettled, agent.WriterObligations(open)...)
	}
	if drained {
		report.Outcome = agent.CloseQuiesced
		return report, nil
	}
	report.Outcome = agent.CloseCut
	return report, agent.ErrDrainUnsettled
}

// offer offers the notifications every task of this rig still owes and lands the
// terminal of each whose obligations cleared.
func (r *closeRig) offer(ctx context.Context) {
	r.mu.Lock()
	known := append([]event.TaskID(nil), r.known...)
	r.mu.Unlock()
	for _, taskID := range known {
		if err := r.tasks.Flush(ctx, taskID); err != nil {
			continue
		}
		if _, err := r.ledger.Reconcile(ctx, taskID); err != nil {
			continue
		}
	}
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
		t.Fatalf("borrow %s: %v", agentID, err)
	}
	ref := handle.Ref
	if err := handle.Release(ctx); err != nil {
		t.Fatalf("release the borrow of %s: %v", agentID, err)
	}
	return ref
}

// delegate admits one delegated call through the task face and reports the task
// it was admitted as.
func (r *closeRig) delegate(t *testing.T, ctx context.Context, callID string, invocation event.InvocationID, target event.Address, ref state.BindingRef) event.TaskID {
	t.Helper()
	receipt, err := r.ledger.Delegate(ctx, agent.Delegation{
		InvocationID: invocation,
		DecisionID:   event.DecisionID("decision-" + callID),
		CallID:       callID,
		Sites:        agent.DelegationSites{Target: target},
		Ref:          ref,
	})
	if err != nil {
		t.Fatalf("delegate %s: %v", callID, err)
	}
	if receipt.Kind != task.AdmissionFresh || !receipt.Accepted {
		t.Fatalf("receipt of %s = %+v, want one fresh admission", callID, receipt)
	}
	r.mu.Lock()
	r.known = append(r.known, receipt.TaskID)
	r.mu.Unlock()
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

// closeOutcome is the result of one close read from beside the writer it stands
// on.
type closeOutcome struct {
	report agent.DrainReport
	err    error
}
